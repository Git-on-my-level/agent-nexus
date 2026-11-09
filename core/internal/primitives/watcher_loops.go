package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

func (s *Store) maintainWatcherLoop(ctx context.Context, id string, now time.Time, cfg WatcherHealthConfig) error {
	return s.watcherWriteTransaction(ctx, func(ctx context.Context, tx *accessTx) error {
		a, err := loadWatcherAlarm(ctx, tx, "loop:"+id, id)
		if err != nil {
			return err
		}
		card, err := s.loadBoardCardByGlobalID(ctx, tx, id, true)
		subject := "card:" + id
		if errors.Is(err, ErrNotFound) {
			return s.reconcileWatcherAlarm(ctx, tx, a, "", "", "", "", subject, "", nil, now, cfg)
		}
		if err != nil {
			return err
		}
		thread := card.ThreadID.String
		var raw string
		err = tx.QueryRowContext(ctx, `SELECT metadata_json FROM work_metadata WHERE card_id=?`, id).Scan(&raw)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		m := watcherMetadata(raw)
		cadence := workMap(m["cadence"])
		if card.ColumnKey == "done" || card.ColumnKey == "cancelled" || card.ArchivedAt.Valid || card.TrashedAt.Valid || len(cadence) == 0 && len(workMap(m["goal"])) == 0 {
			return s.reconcileWatcherAlarm(ctx, tx, a, "", "", "", thread, subject, "", nil, now, cfg)
		}
		recipient, humanOwner, err := watcherRecipient(ctx, tx, firstCardLeaseAssignee(card.Assignee.String), id, now, cfg.AgentActivityWithin, false)
		if err != nil {
			return err
		}
		if recipient == "" {
			return s.reconcileWatcherAlarm(ctx, tx, a, "", "", "", thread, subject, "", nil, now, cfg)
		}
		scoped := WithAccessScope(ctx, AccessScope{ActorID: recipient})
		facts, stop, err := watcherLoopFacts(scoped, tx, id, now)
		if errors.Is(err, errWatcherEvidenceUnavailable) {
			return saveWatcherAlarm(ctx, tx, a)
		}
		if err != nil {
			return err
		}
		loop := computeLoop(loopCard{id: id, phase: card.ColumnKey, created: card.CreatedAt, metadata: m}, facts.loopFacts, now)
		state := workString(loop["health"])
		known := watcherHealthKnown(facts, state, len(workMap(m["goal"])) > 0)
		unhealthy := known && (state == "failing" || state == "overdue" || state == "stalled")
		every, _ := workInt(cadence["every_seconds"])
		if every == 0 {
			every, _ = workInt(cadence["progress_budget_seconds"])
			if every == 0 {
				every = 604800
			}
		}
		interval := time.Duration(every) * time.Second
		if unhealthy && a.Since == "" {
			since := now
			// Missed receipt boundaries are known without observing every prior tick.
			if state == "overdue" {
				since = parseWatcherTime(workString(loop["next_run_due_at"])).Add(interval)
			}
			a.Since = loopTS(since)
		}
		stopAfter, _ := workInt(cadence["stop_after_unhealthy_seconds"])
		if unhealthy && stopAfter > 0 && now.Sub(parseWatcherTime(a.Since)) >= time.Duration(stopAfter)*time.Second {
			stop = true
		}
		if stop {
			// Asks are for humans, even while the loop has an active agent owner.
			human, _, err := watcherRecipient(ctx, tx, firstCardLeaseAssignee(card.Assignee.String), id, now, cfg.AgentActivityWithin, true)
			if err != nil {
				return err
			}
			if human == "" {
				return saveWatcherAlarm(ctx, tx, a)
			}
			// Don't forward a private stop assertion to a less privileged human.
			humanCtx := WithAccessScope(ctx, AccessScope{ActorID: human})
			hf, hs, err := watcherLoopFacts(humanCtx, tx, id, now)
			if errors.Is(err, errWatcherEvidenceUnavailable) {
				return saveWatcherAlarm(ctx, tx, a)
			}
			if err != nil {
				return err
			}
			hloop := computeLoop(loopCard{id: id, phase: card.ColumnKey, created: card.CreatedAt, metadata: m}, hf.loopFacts, now)
			hu := watcherHealthKnown(hf, workString(hloop["health"]), len(workMap(m["goal"])) > 0) && (hloop["health"] == "failing" || hloop["health"] == "overdue" || hloop["health"] == "stalled")
			if !hs && !(stopAfter > 0 && hu && now.Sub(parseWatcherTime(a.Since)) >= time.Duration(stopAfter)*time.Second) {
				return saveWatcherAlarm(ctx, tx, a)
			}
			reason := "The loop stop rule has tripped. Decide whether to retire it or fix and resume it."
			return s.reconcileWatcherAlarm(humanCtx, tx, a, "decide", human, "stop", thread, subject, reason, hf.refs, now, cfg)
		}
		if !known {
			return saveWatcherAlarm(ctx, tx, a)
		}
		if !unhealthy {
			return s.reconcileWatcherAlarm(ctx, tx, a, "", "", "", thread, subject, "", nil, now, cfg)
		}
		age := now.Sub(parseWatcherTime(a.Since))
		if age.Seconds()/interval.Seconds() < float64(cfg.LoopDigestCadences) {
			return saveWatcherAlarm(ctx, tx, a)
		}
		level, kind := "digest", ""
		if age.Seconds()/interval.Seconds() >= float64(cfg.LoopDecideCadences) {
			level = "decide"
			// One recovery ask, not a card placement an attention agent can demote.
			if humanOwner {
				kind = "recover"
			}
		}
		reason := "Loop is " + state + ": " + workString(loop["health_reason"]) + ". Record recovery or hand off."
		return s.reconcileWatcherAlarm(scoped, tx, a, level, recipient, kind, thread, subject, reason, facts.refs, now, cfg)
	})
}

var errWatcherEvidenceUnavailable = errors.New("watcher receipt candidates unavailable to recipient")

// Bounded subject/time index seeks, no aggregate over a receipt window and no
// decoding of receipt bodies, effect text, metrics or historical summaries.
type watcherLoopRead struct {
	loopFacts
	refs              []string
	effectUnavailable bool
}

func watcherLoopFacts(ctx context.Context, tx *accessTx, id string, now time.Time) (watcherLoopRead, bool, error) {
	f := watcherLoopRead{}
	stop := false
	ids, err := queryStrings(ctx, tx, `SELECT id FROM watcher_run_positions WHERE subject_ref=? AND ts<=? ORDER BY ts DESC,id DESC LIMIT 2`, "card:"+id, loopTS(now))
	if err != nil {
		return f, false, err
	}
	encoded, _ := json.Marshal(ids)
	rows, err := tx.QueryContext(ctx, `SELECT id,ts,json_extract(payload_json,'$.payload.outcome'),COALESCE(json_extract(payload_json,'$.payload.stop_rule_triggered'),0) FROM events WHERE id IN(SELECT value FROM json_each(?)) ORDER BY ts DESC,id DESC`, string(encoded))
	if err != nil {
		return f, false, err
	}
	for rows.Next() {
		var eventID, at, outcome string
		var tripped bool
		if err = rows.Scan(&eventID, &at, &outcome, &tripped); err != nil {
			rows.Close()
			return f, false, err
		}
		if len(f.Runs) == 0 {
			f.LastRun = &at
			stop = tripped
		}
		f.Runs = append(f.Runs, map[string]any{"at": at, "outcome": outcome})
		f.refs = appendUnique(f.refs, "event:"+eventID)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return f, false, err
	}
	if len(f.Runs) != len(ids) {
		return f, false, errWatcherEvidenceUnavailable
	}
	effects, err := queryStrings(ctx, tx, `SELECT id FROM watcher_effect_positions WHERE subject_ref=? AND ts<=? ORDER BY ts DESC,id DESC LIMIT 1`, "card:"+id, loopTS(now))
	if err != nil {
		return f, false, err
	}
	if len(effects) > 0 {
		var effect string
		err = tx.QueryRowContext(ctx, `SELECT ts FROM events WHERE id=?`, effects[0]).Scan(&effect)
		if errors.Is(err, sql.ErrNoRows) {
			// An old private effect cannot suppress newer, public failure or
			// missed-run evidence. Only a goal/stall judgment needs it.
			f.effectUnavailable = true
			return f, stop, nil
		}
		if err != nil {
			return f, false, err
		}
		f.LastEffect = &effect
		f.refs = appendUnique(f.refs, "event:"+effects[0])
	}
	return f, stop, nil
}

// Choose responsibility, not authority: candidates must already read the card.
// A human owner remains responsible; agents need recent actual workspace use.
func watcherRecipient(ctx context.Context, tx *accessTx, owner, card string, now time.Time, fresh time.Duration, humanOnly bool) (string, bool, error) {
	if owner != "" {
		actor, err := trackingActor(ctx, tx, owner)
		if err != nil {
			return "", false, err
		}
		var id, kind string
		err = tx.QueryRowContext(ctx, `SELECT id,COALESCE(json_extract(metadata_json,'$.principal_kind'),'agent') FROM agents WHERE actor_id=? AND revoked_at IS NULL`, actor).Scan(&id, &kind)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", false, err
		}
		if err == nil && trackingActorCanRead(ctx, tx, actor, card) {
			if kind == "human" {
				return actor, true, nil
			}
			var active bool
			err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM auth_access_tokens WHERE agent_id=? AND created_at>=? AND revoked_at IS NULL) OR EXISTS(SELECT 1 FROM events WHERE actor_id=? AND ts>=? LIMIT 1) OR EXISTS(SELECT 1 FROM agent_presence WHERE agent_id=? AND observed_at>=?)`, id, loopTS(now.Add(-fresh)), actor, loopTS(now.Add(-fresh)), id, loopTS(now.Add(-fresh))).Scan(&active)
			if err != nil {
				return "", false, err
			}
			if active && !humanOnly {
				return actor, false, nil
			}
		}
	}
	human, err := watcherHumanRecipient(ctx, tx, card)
	return human, human != "", err
}

// Empty card selects an audience for a new human-owned PM health thread.
func watcherHumanRecipient(ctx context.Context, tx *accessTx, card string) (string, error) {
	humans, err := queryStrings(ctx, tx, `SELECT actor_id FROM agents INDEXED BY idx_agents_active_humans WHERE revoked_at IS NULL AND json_extract(metadata_json,'$.principal_kind')='human' ORDER BY actor_id LIMIT 200`)
	if err != nil {
		return "", err
	}
	for _, actor := range humans {
		if card == "" || trackingActorCanRead(ctx, tx, actor, card) {
			return actor, nil
		}
	}
	return "", nil
}

// Receipt failures and cadence misses have precedence over goal effects.
func watcherHealthKnown(f watcherLoopRead, state string, hasGoal bool) bool {
	return !f.effectUnavailable || !hasGoal || state == "failing" || state == "overdue"
}
