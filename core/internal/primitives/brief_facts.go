package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"agent-nexus-core/internal/plans"
)

// Materialized brief facts (storage migration 89). Triggers record asks and
// notes on write and queue touched cards in brief_dirty; RefreshBriefInitiatives
// folds queued cards into brief_initiatives with plans.FoldPromise. The fold
// is time-independent, so reads compute only durations.

const briefRefreshBatch = 200

// RefreshBriefInitiatives folds at most limit queued cards. It reports whether
// the queue is drained.
func (s *Store) RefreshBriefInitiatives(ctx context.Context, limit int) (bool, error) {
	if limit <= 0 || limit > briefRefreshBatch {
		limit = briefRefreshBatch
	}
	ctx = CanonicalMaintenanceContext(ctx)
	tx, cleanup, err := s.db.BeginMaintenanceChunk(ctx)
	if err != nil {
		return false, err
	}
	defer cleanup()
	ids, err := queryStrings(ctx, tx, `SELECT card_id FROM brief_dirty ORDER BY card_id LIMIT ?`, limit)
	if err != nil || len(ids) == 0 {
		return len(ids) == 0, err
	}
	encoded, _ := json.Marshal(ids)
	rows, err := tx.QueryContext(ctx, `SELECT c.id,COALESCE(c.handle,''),c.column_key,COALESCE(c.archived_at,''),COALESCE(c.trashed_at,''),`+effectiveCardDueSQL+`,
		COALESCE(json_extract(m.metadata_json,'$.wake_condition'),''),COALESCE(p.body_json,''),COALESCE(p.updated_at,''),
		COALESCE(json_extract(o.body_json,'$.meaningful_progress_at'),''),COALESCE(bi.step_status_sig,''),COALESCE(bi.step_change_at,'')
		FROM cards c LEFT JOIN work_metadata m ON m.card_id=c.id LEFT JOIN card_plans p ON p.card_id=c.id
		LEFT JOIN work_observations o ON o.id=m.latest_observation_id LEFT JOIN brief_initiatives bi ON bi.card_id=c.id
		WHERE c.id IN (SELECT value FROM json_each(?))`, string(encoded))
	if err != nil {
		return false, err
	}
	type cardFacts struct {
		id, handle, phase, archived, trashed, due, wake, planRaw, planAt, progressAt, sig, stepChangeAt string
	}
	facts := []cardFacts{}
	for rows.Next() {
		var f cardFacts
		if err = rows.Scan(&f.id, &f.handle, &f.phase, &f.archived, &f.trashed, &f.due, &f.wake, &f.planRaw, &f.planAt, &f.progressAt, &f.sig, &f.stepChangeAt); err != nil {
			rows.Close()
			return false, err
		}
		facts = append(facts, f)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return false, err
	}
	notes, err := promiseNotesFor(ctx, tx, ids)
	if err != nil {
		return false, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, f := range facts {
		ref := "card:" + firstNonEmptyString(f.handle, f.id)
		in := plans.PromiseInput{Ref: ref, Notes: notes[f.id], CardDue: plans.ParseDue(f.due), Wake: plans.WakeTime(f.wake)}
		in.Done = plans.Status(f.phase) == "done" || f.phase == "cancelled"
		open := 1
		if in.Done {
			open = 0
		}
		if f.archived != "" || f.trashed != "" {
			open = -1
		}
		sig := ""
		if f.planRaw != "" {
			var p plans.Plan
			if json.Unmarshal([]byte(f.planRaw), &p) == nil {
				parts := make([]string, 0, len(p.Steps))
				for _, step := range p.Steps {
					parts = append(parts, step.ID+"="+step.Status)
					if step.Status != "done" {
						if due := plans.ParseDue(step.Due); !due.IsZero() {
							in.Steps = append(in.Steps, plans.PromiseStep{ID: step.ID, Title: step.Title, Due: due})
						}
					}
				}
				sig = strings.Join(parts, ";")
			}
		}
		// A plan-step state change is a reset: the plan's update time when
		// its step statuses differ from the last fold.
		stepChangeAt := f.stepChangeAt
		if sig != f.sig && f.planAt != "" && (f.sig != "" || strings.Contains(sig, "=done")) {
			stepChangeAt = f.planAt
		}
		if at, e := time.Parse(time.RFC3339Nano, stepChangeAt); e == nil {
			in.StepChanges = []time.Time{at}
		}
		if at, e := time.Parse(time.RFC3339Nano, f.progressAt); e == nil {
			in.Observations = []time.Time{at}
		}
		fold := plans.FoldPromise(in)
		var promiseAt, lastEvidence any
		what, source, sourceRef, author := "", "", "", ""
		if fold.Promise != nil {
			promiseAt = fold.Promise.At.UTC().Format(time.RFC3339)
			what, source, sourceRef, author = fold.Promise.What, fold.Promise.Source, fold.Promise.SourceRef, fold.Promise.Author
		}
		if !fold.LastEvidenceAt.IsZero() {
			lastEvidence = fold.LastEvidenceAt.UTC().Format(time.RFC3339Nano)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO brief_initiatives(card_id,open,promise_at,promise_what,promise_source,promise_source_ref,promise_author,promise_moves,last_evidence_at,step_status_sig,step_change_at,computed_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,NULLIF(?,''),?) ON CONFLICT(card_id) DO UPDATE SET open=excluded.open,promise_at=excluded.promise_at,promise_what=excluded.promise_what,
			promise_source=excluded.promise_source,promise_source_ref=excluded.promise_source_ref,promise_author=excluded.promise_author,promise_moves=excluded.promise_moves,
			last_evidence_at=excluded.last_evidence_at,step_status_sig=excluded.step_status_sig,step_change_at=excluded.step_change_at,computed_at=excluded.computed_at`,
			f.id, open, promiseAt, what, source, sourceRef, author, fold.Moves, lastEvidence, sig, stepChangeAt, now); err != nil {
			return false, err
		}
	}
	// Cards that no longer exist leave the projection too.
	if _, err = tx.ExecContext(ctx, `DELETE FROM brief_initiatives WHERE card_id IN (SELECT value FROM json_each(?)) AND card_id NOT IN (SELECT id FROM cards)`, string(encoded)); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM brief_dirty WHERE card_id IN (SELECT value FROM json_each(?))`, string(encoded)); err != nil {
		return false, err
	}
	return len(ids) < limit, tx.Commit()
}

// RunBriefMaintenance drains the brief dirty queue in bounded batches.
func (s *Store) RunBriefMaintenance(ctx context.Context, report func(error)) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.RefreshBriefInitiatives(ctx, briefRefreshBatch); err != nil && report != nil && ctx.Err() == nil {
				report(err)
			}
		}
	}
}

type briefQueryer interface {
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
}

func queryStrings(ctx context.Context, q briefQueryer, query string, args ...any) ([]string, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err = rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// PromiseNotesFor returns the newest plans.MaxPromiseNotes notes per card for
// at most 200 card ids, one indexed seek per card.
func (s *Store) PromiseNotesFor(ctx context.Context, cardIDs []string) (map[string][]plans.PromiseNote, error) {
	return promiseNotesFor(ctx, s.db, cardIDs)
}

func promiseNotesFor(ctx context.Context, q briefQueryer, cardIDs []string) (map[string][]plans.PromiseNote, error) {
	out := map[string][]plans.PromiseNote{}
	if len(cardIDs) > briefRefreshBatch {
		cardIDs = cardIDs[:briefRefreshBatch]
	}
	for _, id := range cardIDs {
		rows, err := q.QueryContext(ctx, `SELECT event_id,author,ts,COALESCE(next_at,''),next_what,evidence FROM brief_notes WHERE card_id=? ORDER BY ts DESC LIMIT ?`, id, plans.MaxPromiseNotes)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var n plans.PromiseNote
			var ts, next string
			if err = rows.Scan(&n.EventID, &n.Author, &ts, &next, &n.NextWhat, &n.Evidence); err != nil {
				rows.Close()
				return nil, err
			}
			n.At, _ = time.Parse(time.RFC3339Nano, ts)
			n.NextAt, _ = time.Parse(time.RFC3339Nano, next)
			out[id] = append(out[id], n)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// PromiseFolds reads the materialized folds for at most 200 cards.
func (s *Store) PromiseFolds(ctx context.Context, cardIDs []string) (map[string]plans.PromiseFold, error) {
	out := map[string]plans.PromiseFold{}
	if len(cardIDs) == 0 {
		return out, nil
	}
	if len(cardIDs) > briefRefreshBatch {
		cardIDs = cardIDs[:briefRefreshBatch]
	}
	encoded, _ := json.Marshal(cardIDs)
	rows, err := s.db.QueryContext(ctx, `SELECT card_id,open,COALESCE(promise_at,''),promise_what,promise_source,promise_source_ref,promise_author,promise_moves,COALESCE(last_evidence_at,'') FROM brief_initiatives WHERE card_id IN (SELECT value FROM json_each(?))`, string(encoded))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r briefInitiativeRow
		if err = rows.Scan(&r.cardID, &r.open, &r.promiseAt, &r.what, &r.source, &r.sourceRef, &r.author, &r.moves, &r.lastEvidence); err != nil {
			return nil, err
		}
		out[r.cardID] = r.fold()
	}
	return out, rows.Err()
}

type briefInitiativeRow struct {
	cardID, promiseAt, what, source, sourceRef, author, lastEvidence string
	open, moves                                                     int
}

func (r briefInitiativeRow) fold() plans.PromiseFold {
	f := plans.PromiseFold{Done: r.open == 0, Moves: r.moves}
	f.LastEvidenceAt, _ = time.Parse(time.RFC3339Nano, r.lastEvidence)
	if at, err := time.Parse(time.RFC3339Nano, r.promiseAt); err == nil {
		f.Promise = &plans.Promise{At: at, What: r.what, Source: r.source, SourceRef: r.sourceRef, Author: r.author}
	}
	return f
}

// BriefAsk is one materialized ask fact.
type BriefAsk struct {
	AskID, InboxItemID, Kind, Requester, Recipient, SubjectRef, Title, Priority, State string
	CreatedAt                                                                        time.Time
	DecideBy                                                                         *time.Time
	Blocks                                                                           []string
	BlocksCount                                                                      int
}

// OpenAsksDueBefore seeks open asks whose decide_by is at or before `before`,
// earliest first, through idx_brief_asks_open_decide_by. Content-free callers
// (escalation, heartbeat due counts) use it; it applies no reader scope.
func (s *Store) OpenAsksDueBefore(ctx context.Context, before time.Time, limit int) ([]BriefAsk, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	rows, err := s.db.QueryContext(CanonicalMaintenanceContext(ctx), `SELECT `+briefAskColumns+` FROM brief_asks a INDEXED BY idx_brief_asks_open_decide_by
		WHERE a.state='open' AND a.decide_by IS NOT NULL AND a.decide_by<=? ORDER BY a.decide_by,a.ask_id LIMIT ?`, before.UTC().Format(time.RFC3339), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBriefAsks(rows)
}

const briefAskColumns = `a.ask_id,a.inbox_item_id,a.kind,a.requester,a.recipient,a.subject_ref,a.title,a.priority,a.state,a.created_at,COALESCE(a.decide_by,''),a.blocks_json,a.blocks_count`

func scanBriefAsks(rows *sql.Rows) ([]BriefAsk, error) {
	out := []BriefAsk{}
	for rows.Next() {
		var a BriefAsk
		var created, decide, blocks string
		if err := rows.Scan(&a.AskID, &a.InboxItemID, &a.Kind, &a.Requester, &a.Recipient, &a.SubjectRef, &a.Title, &a.Priority, &a.State, &created, &decide, &blocks, &a.BlocksCount); err != nil {
			return nil, err
		}
		a.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		if at, err := time.Parse(time.RFC3339Nano, decide); err == nil {
			a.DecideBy = &at
		}
		var refs []any
		_ = json.Unmarshal([]byte(blocks), &refs)
		for _, r := range refs {
			if text, ok := r.(string); ok && strings.TrimSpace(text) != "" && len(a.Blocks) < 20 {
				a.Blocks = append(a.Blocks, strings.TrimSpace(text))
			}
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

var briefPriorityRank = map[string]int{"critical": 0, "p0": 0, "urgent": 0, "high": 1, "p1": 1, "medium": 2, "p2": 2, "normal": 2, "low": 3, "p3": 3}

func briefPriority(p string) int {
	if r, ok := briefPriorityRank[strings.ToLower(strings.TrimSpace(p))]; ok {
		return r
	}
	return 4
}

// sortBriefAsks applies the brief decision order: decide_by (absent last),
// blocks desc, priority, created_at, id.
func sortBriefAsks(asks []BriefAsk) {
	sort.SliceStable(asks, func(i, j int) bool {
		a, b := asks[i], asks[j]
		if (a.DecideBy == nil) != (b.DecideBy == nil) {
			return a.DecideBy != nil
		}
		if a.DecideBy != nil && !a.DecideBy.Equal(*b.DecideBy) {
			return a.DecideBy.Before(*b.DecideBy)
		}
		if a.BlocksCount != b.BlocksCount {
			return a.BlocksCount > b.BlocksCount
		}
		if pa, pb := briefPriority(a.Priority), briefPriority(b.Priority); pa != pb {
			return pa < pb
		}
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.Before(b.CreatedAt)
		}
		return a.AskID < b.AskID
	})
}

// SortBriefDecisions orders rendered decision rows with the brief order, for
// callers that merge rows from another source (PM decision records).
func SortBriefDecisions(items []map[string]any) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		da, _ := a["decide_by"].(string)
		db, _ := b["decide_by"].(string)
		if (da == "") != (db == "") {
			return da != ""
		}
		if da != db {
			return da < db
		}
		ba, _ := a["blocks_count"].(int)
		bb, _ := b["blocks_count"].(int)
		if ba != bb {
			return ba > bb
		}
		pa, _ := a["priority"].(string)
		pb, _ := b["priority"].(string)
		if ra, rb := briefPriority(pa), briefPriority(pb); ra != rb {
			return ra < rb
		}
		ca, _ := a["created_at"].(string)
		cb, _ := b["created_at"].(string)
		if ca != cb {
			return ca < cb
		}
		ra, _ := a["ref"].(string)
		rb, _ := b["ref"].(string)
		return ra < rb
	})
}
