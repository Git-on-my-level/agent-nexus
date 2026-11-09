package primitives

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"agent-nexus-core/internal/actors"
	"github.com/google/uuid"
)

// WatcherHealthConfig controls alarm delivery, never the promise/health fold.
type WatcherHealthConfig struct {
	PMOfflineAfter                                time.Duration
	LoopDigestCadences, LoopDecideCadences        int
	AgentActivityWithin, RepeatInitial, RepeatMax time.Duration
}

func DefaultWatcherHealthConfig() WatcherHealthConfig {
	return WatcherHealthConfig{time.Hour, 2, 4, time.Hour, time.Hour, 24 * time.Hour}
}

type watcherAlarm struct {
	Key, Card, Since, Level, Recipient, Ask, Next string
	Episode, Raises                               int
}

// Keep the quota -> SQLite lock order used by ordinary mutations. Nested
// event/attention helpers inherit ownership rather than reacquiring the mutex.
func (s *Store) watcherWriteTransaction(ctx context.Context, run func(context.Context, *accessTx) error) error {
	if s.quota.enabled() && !replayOwnsQuota(ctx, s) {
		s.quotaMu.Lock()
		defer s.quotaMu.Unlock()
		ctx = context.WithValue(ctx, replayQuotaKey{}, s)
	}
	return s.db.WriteTransaction(ctx, run)
}

func watcherID(key string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("watcher-health:"+key)).String()
}

// MaintainLoopHealth walks indexed loop identities and existing alarm rows in
// pages of 25. The durable cursor rotates, so an unhealthy prefix cannot starve
// later loops. Each card is evaluated and changed in one writer snapshot.
func (s *Store) MaintainLoopHealth(ctx context.Context, now time.Time, cfg WatcherHealthConfig) error {
	var after string
	if err := s.db.QueryRowContext(ctx, `SELECT after_card_id FROM watcher_health_cursor WHERE singleton=1`).Scan(&after); err != nil {
		return err
	}
	ids, err := queryStrings(ctx, s.db, `SELECT card_id FROM work_metadata INDEXED BY idx_work_metadata_health WHERE card_id>? AND (json_type(metadata_json,'$.cadence')='object' OR json_type(metadata_json,'$.goal')='object') ORDER BY card_id LIMIT 25`, after)
	if err != nil {
		return err
	}
	active, err := queryStrings(ctx, s.db, `SELECT card_id FROM watcher_health INDEXED BY idx_watcher_health_card WHERE card_id<>'' AND card_id>? ORDER BY card_id LIMIT 25`, after)
	if err != nil {
		return err
	}
	ids = append(ids, active...)
	sort.Strings(ids)
	unique := []string{}
	for _, id := range ids {
		if len(unique) == 0 || unique[len(unique)-1] != id {
			unique = append(unique, id)
		}
	}
	if len(unique) > 25 {
		unique = unique[:25]
	}
	var failures []error
	for _, id := range unique {
		if err = s.maintainWatcherLoop(ctx, id, now, cfg); err != nil {
			failures = append(failures, err)
		}
		// A failed card rolls back its own episode, but cannot pin the cursor
		// and indefinitely suppress alarms for the rest of the workspace.
		after = id
	}
	if len(unique) < 25 {
		after = ""
	}
	_, err = s.db.ExecContext(ctx, `UPDATE watcher_health_cursor SET after_card_id=? WHERE singleton=1`, after)
	return errors.Join(append(failures, err)...)
}

func loadWatcherAlarm(ctx context.Context, tx *accessTx, key, card string) (watcherAlarm, error) {
	a := watcherAlarm{Key: key, Card: card}
	// Establish a writer before reading; competing ticks serialize on this row.
	if _, err := tx.ExecContext(ctx, `INSERT INTO watcher_health(alarm_key,card_id) VALUES(?,?) ON CONFLICT DO NOTHING`, key, card); err != nil {
		return a, err
	}
	err := tx.QueryRowContext(ctx, `SELECT unhealthy_since,episode,level,recipient,ask_id,raise_count,next_raise_at FROM watcher_health WHERE alarm_key=?`, key).Scan(&a.Since, &a.Episode, &a.Level, &a.Recipient, &a.Ask, &a.Raises, &a.Next)
	return a, err
}
func saveWatcherAlarm(ctx context.Context, tx *accessTx, a watcherAlarm) error {
	// Internal clock state never leaves maintenance; recipient-scoped content
	// writes are checked separately before the state advances.
	ctx = CanonicalMaintenanceContext(ctx)
	_, err := tx.ExecContext(ctx, `UPDATE watcher_health SET unhealthy_since=?,episode=?,level=?,recipient=?,ask_id=?,raise_count=?,next_raise_at=? WHERE alarm_key=?`, a.Since, a.Episode, a.Level, a.Recipient, a.Ask, a.Raises, a.Next, a.Key)
	return err
}

// ReconcilePMHealth is passed a content-free signal from PM's own indexed
// store. It deliberately never copies private turn text, IDs or owner refs.
func (s *Store) ReconcilePMHealth(ctx context.Context, unhealthy bool, now time.Time, cfg WatcherHealthConfig) error {
	thread := watcherID("pm-thread")
	return s.watcherWriteTransaction(ctx, func(ctx context.Context, tx *accessTx) error {
		a, err := loadWatcherAlarm(ctx, tx, "pm", "")
		if err != nil {
			return err
		}
		if !unhealthy {
			return s.reconcileWatcherAlarm(ctx, tx, a, "", "", "", thread, "thread:"+thread, "", nil, now, cfg)
		}
		if _, err = s.CreateThread(ctx, actors.SystemActorID, map[string]any{"id": thread, "title": "PM health"}); err != nil && !errors.Is(err, ErrConflict) {
			return err
		}
		return s.reconcileWatcherAlarm(ctx, tx, a, "decide", "", "pm", thread, "thread:"+thread, "The workspace PM is offline or has missed a turn deadline. Restore the PM runner and verify a successful reply.", nil, now, cfg)
	})
}

func (s *Store) reconcileWatcherAlarm(ctx context.Context, tx *accessTx, a watcherAlarm, level, recipient, askKind string, thread, subject, reason string, evidence []string, now time.Time, cfg WatcherHealthConfig) error {
	ask := askKind != ""
	if level == "" {
		if a.Ask != "" {
			_, err := s.AppendHumanAttentionWithdrawal(CanonicalMaintenanceContext(ctx), actors.SystemActorID, a.Ask, map[string]any{"id": watcherID(a.Ask + ":recovered"), "payload": map[string]any{"reason": "Watcher health recovered or monitoring ended."}})
			if err != nil && !errors.Is(err, ErrHumanAttentionAlreadyResponded) && !errors.Is(err, ErrNotFound) {
				return err
			}
		}
		if err := clearWatcherPlacement(ctx, tx, a, subject); err != nil {
			return err
		}
		if a.Since != "" || a.Level != "" || a.Ask != "" {
			a.Episode++
		}
		a.Since = ""
		a.Level = ""
		a.Recipient = ""
		a.Ask = ""
		a.Next = ""
		a.Raises = 0
		return saveWatcherAlarm(ctx, tx, a)
	}
	if a.Since == "" {
		a.Since = loopTS(now)
	}
	var previousAskKind string
	if a.Ask != "" {
		if err := tx.QueryRowContext(CanonicalMaintenanceContext(ctx), `SELECT COALESCE(json_extract(payload_json,'$.payload.watcher_health_kind'),'') FROM events WHERE id=?`, a.Ask).Scan(&previousAskKind); err != nil {
			return err
		}
	}
	if a.Ask != "" && (!ask || a.Recipient != recipient || previousAskKind != askKind) {
		_, err := s.AppendHumanAttentionWithdrawal(CanonicalMaintenanceContext(ctx), actors.SystemActorID, a.Ask, map[string]any{"payload": map[string]any{"reason": "The stop condition or responsible human changed."}})
		if err != nil && !errors.Is(err, ErrHumanAttentionAlreadyResponded) && !errors.Is(err, ErrNotFound) {
			return err
		}
		a.Ask = ""
	}
	due := a.Level != level || a.Recipient != recipient || a.Next == "" || !parseWatcherTime(a.Next).After(now) || ask && a.Ask == ""
	if !due {
		return saveWatcherAlarm(ctx, tx, a)
	}
	if a.Recipient != recipient || ask {
		if err := clearWatcherPlacement(ctx, tx, a, subject); err != nil {
			return err
		}
	}
	key := fmt.Sprintf("%s:%d:%d", a.Key, a.Episode, a.Raises)
	if ask && a.Ask != "" {
		var resolved bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM human_attention_request_resolutions WHERE request_event_id=?)`, a.Ask).Scan(&resolved); err != nil {
			return err
		}
		if resolved {
			a.Ask = ""
		}
	}
	if ask && a.Ask == "" {
		a.Ask = watcherID(key + ":ask")
		title := "Restore the workspace PM?"
		proposals := []string{"Restore the PM runner", "Disconnect the PM intentionally"}
		if a.Card != "" {
			title = "Restore reliable loop runs?"
			proposals = []string{"Fix the loop and resume reliable runs", "Hand off the loop to an active owner"}
			if askKind == "stop" {
				title = "Retire or resume this loop?"
				proposals = []string{"Resume after fixing the loop", "Retire the loop"}
			}
		}
		_, err := s.AppendEvent(ctx, actors.SystemActorID, map[string]any{"id": a.Ask, "type": "human_attention_requested", "thread_id": thread, "refs": append([]string{"thread:" + thread, subject}, evidence...), "summary": title, "payload": map[string]any{"kind": "ask", "title": title, "subject_ref": subject, "requester_actor_id": actors.SystemActorID, "recipient_actor_id": recipient, "response_proposals": proposals, "situation": reason, "impact": "The watcher cannot provide reliable follow-through until it recovers.", "recommendation": map[string]any{"action": proposals[0], "why": "Restore follow-through or explicitly end the monitoring expectation."}, "request_key": key, "watcher_health_kind": askKind}})
		if err != nil {
			return err
		}
	}
	if recipient != "" && !ask {
		_, err := s.PlaceAttention(ctx, actors.SystemActorID, AttentionPlacement{SubjectRef: subject, For: "actor:" + recipient, To: level, Reason: reason, RequestKey: "watcher:" + key, evidenceRefs: evidence})
		if err != nil {
			return err
		}
	}
	refs := append([]string{subject, "thread:" + thread}, evidence...)
	if a.Ask != "" {
		refs = append(refs, "event:"+a.Ask)
	}
	if _, err := s.AppendEvent(ctx, actors.SystemActorID, map[string]any{"id": watcherID(key + ":reminder"), "type": "message_posted", "thread_id": thread, "refs": refs, "summary": reason, "payload": map[string]any{"text": reason, "automatic": true, "source": "watcher_health", "request_key": key, "surface": level}}); err != nil {
		return err
	}
	interval := cfg.RepeatInitial
	for i := 0; i < a.Raises && interval < cfg.RepeatMax; i++ {
		interval = min(interval*2, cfg.RepeatMax)
	}
	a.Raises++
	a.Level = level
	a.Recipient = recipient
	a.Next = loopTS(now.Add(interval))
	return saveWatcherAlarm(ctx, tx, a)
}
func clearWatcherPlacement(ctx context.Context, tx *accessTx, a watcherAlarm, subject string) error {
	// Routing cleanup must see other placers, including ones whose payload is
	// private to a different reader. It never returns their content.
	ctx = CanonicalMaintenanceContext(ctx)
	if a.Recipient == "" {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM attention_placements WHERE for_actor=? AND subject_ref=? AND placed_by=?`, a.Recipient, subject, "actor:"+actors.SystemActorID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM attention_subjects WHERE for_actor=? AND subject_ref=? AND NOT EXISTS(SELECT 1 FROM attention_placements WHERE for_actor=? AND subject_ref=?)`, a.Recipient, subject, a.Recipient, subject)
	return err
}
func parseWatcherTime(v string) time.Time { t, _ := time.Parse(time.RFC3339Nano, v); return t }

// Only numeric state and scoped receipt facts cross this boundary.
func watcherMetadata(raw string) map[string]any {
	var m map[string]any
	_ = json.Unmarshal([]byte(raw), &m)
	return m
}
