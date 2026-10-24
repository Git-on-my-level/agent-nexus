package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	contractenums "agent-nexus-contracts-go-client/enums"

	"agent-nexus-core/internal/plans"
)

// Materialized brief facts (storage migration 89). Triggers record asks and
// notes on write and queue touched cards in brief_dirty; RefreshBriefInitiatives
// folds queued cards into brief_initiatives with plans.FoldPromise. The fold
// preserves promise facts; loop receipts are folded with expiry at window boundaries.

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
	if _, err = tx.ExecContext(ctx, `INSERT INTO brief_dirty(card_id) SELECT card_id FROM brief_initiatives INDEXED BY idx_brief_loop_expiry WHERE loop_valid_until IS NOT NULL AND loop_valid_until<=? ORDER BY loop_valid_until,card_id LIMIT ? ON CONFLICT DO NOTHING`, time.Now().UTC().Format(time.RFC3339Nano), limit); err != nil {
		return false, err
	}
	ids, err := queryStrings(ctx, tx, `SELECT card_id FROM brief_dirty ORDER BY card_id LIMIT ?`, limit)
	if err != nil || len(ids) == 0 {
		return len(ids) == 0, err
	}
	inputs, err := briefPromiseInputs(ctx, tx, ids, func(ids []string) (map[string][]plans.PromiseNote, error) { return promiseNotesFor(ctx, tx, ids) })
	if err != nil {
		return false, err
	}
	encoded, _ := json.Marshal(ids)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, c := range inputs {
		f := struct{ id string }{c.id}
		open, sig, stepChangeAt := c.open, c.sig, c.stepChangeAt
		fold := plans.FoldPromise(c.in)
		var promiseAt, lastEvidence any
		what, source, sourceRef, author := "", "", "", ""
		if fold.Promise != nil {
			promiseAt = fold.Promise.At.UTC().Format(time.RFC3339)
			what, source, sourceRef, author = fold.Promise.What, fold.Promise.Source, fold.Promise.SourceRef, fold.Promise.Author
		}
		if !fold.LastEvidenceAt.IsZero() {
			lastEvidence = fold.LastEvidenceAt.UTC().Format(time.RFC3339Nano)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO brief_initiatives(card_id,open,promise_at,promise_what,promise_source,promise_source_ref,promise_author,promise_moves,last_evidence_at,step_status_sig,step_change_at,computed_at,progress_done,progress_total,plan_provenance_json)
			VALUES(?,?,?,?,?,?,?,?,?,?,NULLIF(?,''),?,?,?,?) ON CONFLICT(card_id) DO UPDATE SET open=excluded.open,promise_at=excluded.promise_at,promise_what=excluded.promise_what,
			promise_source=excluded.promise_source,promise_source_ref=excluded.promise_source_ref,promise_author=excluded.promise_author,promise_moves=excluded.promise_moves,
			last_evidence_at=excluded.last_evidence_at,step_status_sig=excluded.step_status_sig,step_change_at=excluded.step_change_at,computed_at=excluded.computed_at,progress_done=excluded.progress_done,progress_total=excluded.progress_total,plan_provenance_json=excluded.plan_provenance_json`,
			f.id, open, promiseAt, what, source, sourceRef, author, fold.Moves, lastEvidence, sig, stepChangeAt, now, c.progress.Done, c.progress.Total, c.planProvenance); err != nil {
			return false, err
		}
	}
	if err = foldLoopFacts(ctx, tx, ids, time.Now().UTC()); err != nil {
		return false, err
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
// SQLITE_BUSY and SQLITE_LOCKED from the 0ms admission wait are the designed
// yield and are not reported. Every other error is reported at most once a
// minute per distinct text.
func (s *Store) RunBriefMaintenance(ctx context.Context, report func(error)) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	gate := newBriefMaintenanceGate(time.Minute)
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if _, err := s.RefreshBriefInitiatives(ctx, briefRefreshBatch); err != nil && ctx.Err() == nil {
				gate.maybe(err, now, report)
			}
		}
	}
}

type briefMaintenanceGate struct {
	mu       sync.Mutex
	last     map[string]time.Time
	interval time.Duration
}

func newBriefMaintenanceGate(interval time.Duration) *briefMaintenanceGate {
	if interval <= 0 {
		interval = time.Minute
	}
	return &briefMaintenanceGate{last: map[string]time.Time{}, interval: interval}
}

func (g *briefMaintenanceGate) maybe(err error, now time.Time, emit func(error)) {
	if g == nil || err == nil || emit == nil || sqliteBusyOrLocked(err) {
		return
	}
	key := err.Error()
	if len(key) > 240 {
		key = key[:240]
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.last == nil {
		g.last = map[string]time.Time{}
	}
	if prev, ok := g.last[key]; ok && now.Sub(prev) < g.interval {
		return
	}
	for seen, at := range g.last {
		if now.Sub(at) >= g.interval {
			delete(g.last, seen)
		}
	}
	if len(g.last) >= briefMaintenanceReportLimit {
		var oldestKey string
		var oldest time.Time
		first := true
		for seen, at := range g.last {
			if first || at.Before(oldest) {
				oldestKey, oldest, first = seen, at, false
			}
		}
		if !first {
			delete(g.last, oldestKey)
		}
	}
	g.last[key] = now
	emit(err)
}

const briefMaintenanceReportLimit = 32

// sqliteBusyOrLocked matches the primary BUSY (5) and LOCKED (6) codes,
// including extended codes. Error text is not consulted: a submitted value can
// mention those words without being the admission yield.
func sqliteBusyOrLocked(err error) bool {
	var coded interface{ Code() int }
	if !errors.As(err, &coded) {
		return false
	}
	code := coded.Code() & 255
	return code == 5 || code == 6
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
		rows, err := q.QueryContext(ctx, `SELECT event_id,author,ts,COALESCE(next_at,''),next_what,evidence FROM brief_notes WHERE card_id=? AND (next_at IS NOT NULL OR evidence=1) ORDER BY ts DESC LIMIT ?`, id, plans.MaxPromiseNotes)
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
	open, moves                                                      int
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
	CreatedAt                                                                          time.Time
	DecideBy                                                                           *time.Time
	Blocks                                                                             []string
	BlocksCount                                                                        int
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

// briefPrioritySQL must stay byte-identical to idx_brief_asks_open_page; its
// ranks are pinned to the generated enum by TestPrioritySQLMatchesContract.
const briefPrioritySQL = `CASE lower(trim(a.priority)) WHEN 'critical' THEN 0 WHEN 'p0' THEN 0 WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'p1' THEN 1 WHEN 'medium' THEN 2 WHEN 'p2' THEN 2 WHEN 'normal' THEN 2 WHEN 'low' THEN 3 WHEN 'p3' THEN 3 ELSE 4 END`

// briefPriority ranks a priority or its alias by contract order (p0 first);
// anything else sorts last. briefPrioritySQL must rank the same way.
func briefPriority(p string) int {
	if canonical, ok := contractenums.NormalizePriority(p); ok {
		return slices.Index(contractenums.PriorityValues, canonical)
	}
	return len(contractenums.PriorityValues)
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

type briefCardInput struct {
	id                string
	open              int
	in                plans.PromiseInput
	sig, stepChangeAt string
	progress          plans.Progress
	planProvenance    string
}

// briefPromiseInputs loads the fold inputs for at most 200 cards: one
// statement for card, plan, metadata and observation facts, bounded notes,
// and indexed ref-resolution batches. Progress and promise dues share the
// effective statuses used by plan_state.
func briefPromiseInputs(ctx context.Context, q briefQueryer, ids []string, notesFor func([]string) (map[string][]plans.PromiseNote, error)) ([]briefCardInput, error) {
	encoded, _ := json.Marshal(ids)
	rows, err := q.QueryContext(ctx, `SELECT c.id,COALESCE(c.handle,''),c.column_key,COALESCE(c.archived_at,''),COALESCE(c.trashed_at,''),`+effectiveCardDueSQL+`,
		COALESCE(json_extract(m.metadata_json,'$.wake_condition'),''),COALESCE(p.body_json,''),COALESCE(p.updated_at,''),
		COALESCE(json_extract(o.body_json,'$.meaningful_progress_at'),''),COALESCE(json_extract(m.metadata_json,'$._anx_source_evidence_at'),''),COALESCE(bi.step_status_sig,''),COALESCE(bi.step_change_at,''),COALESCE(bi.plan_provenance_json,'{}')
		FROM cards c LEFT JOIN work_metadata m ON m.card_id=c.id LEFT JOIN card_plans p ON p.card_id=c.id
		LEFT JOIN work_observations o ON o.id=m.latest_observation_id LEFT JOIN brief_initiatives bi ON bi.card_id=c.id
		WHERE c.id IN (SELECT value FROM json_each(?))`, string(encoded))
	if err != nil {
		return nil, err
	}
	type cardFacts struct {
		id, handle, phase, archived, trashed, due, wake, planRaw, planAt, progressAt, sourceEvidenceAt, sig, stepChangeAt, provenance string
	}
	facts := []cardFacts{}
	for rows.Next() {
		var f cardFacts
		if err = rows.Scan(&f.id, &f.handle, &f.phase, &f.archived, &f.trashed, &f.due, &f.wake, &f.planRaw, &f.planAt, &f.progressAt, &f.sourceEvidenceAt, &f.sig, &f.stepChangeAt, &f.provenance); err != nil {
			rows.Close()
			return nil, err
		}
		facts = append(facts, f)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	notes, err := notesFor(ids)
	if err != nil {
		return nil, err
	}
	// Resolve through the same indexed, scoped reader as plan_state, inside
	// this maintenance transaction. At most 200 plans x 200 steps; no history.
	ps := map[string]plans.Plan{}
	refs := []string{}
	for _, f := range facts {
		var p plans.Plan
		if f.planRaw != "" && json.Unmarshal([]byte(f.planRaw), &p) == nil {
			ps[f.id] = p
			refs = append(refs, planStepRefs(p)...)
		}
	}
	refs = uniqueSortedStrings(refs)
	resolved := map[string]plans.Fact{}
	for start := 0; start < len(refs); start += 200 {
		batch, err := readRefProjection(ctx, q, refs[start:min(start+200, len(refs))], nil, false)
		if err != nil {
			return nil, err
		}
		for _, r := range batch {
			resolved[r.Ref] = plans.Fact{Known: r.Resolvable, Status: r.Phase, MovementAt: r.MovementAt}
		}
	}
	out := make([]briefCardInput, 0, len(facts))
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
		p := ps[f.id]
		state := plans.Compute(p, resolved, time.Time{})
		for i, step := range state.Steps {
			if step.Status != "done" {
				if due := plans.ParseDue(p.Steps[i].Due); !due.IsZero() {
					in.Steps = append(in.Steps, plans.PromiseStep{ID: step.ID, Title: p.Steps[i].Title, Due: due})
				}
			}
		}
		planAt, _ := time.Parse(time.RFC3339Nano, f.planAt)
		sig, clock := planStepProgressClock(state, f.sig, f.stepChangeAt, planAt, true)
		stepChangeAt := f.stepChangeAt
		if !clock.IsZero() {
			stepChangeAt = clock.UTC().Format(time.RFC3339Nano)
			in.StepChanges = []time.Time{clock}
		}
		if at := latestCardActivity(f.progressAt, f.sourceEvidenceAt); !at.IsZero() {
			in.Observations = []time.Time{at}
		}
		provenance, err := briefPlanProvenance(f.planRaw, f.provenance, f.stepChangeAt, stepChangeAt)
		if err != nil {
			return nil, err
		}
		out = append(out, briefCardInput{id: f.id, open: open, in: in, sig: sig, stepChangeAt: stepChangeAt, progress: state.Progress, planProvenance: provenance})
	}
	return out, nil
}

// briefPlanProvenance retains two bounded plans: the current progress/promise
// inputs and the plan responsible for the retained step-change clock. Swapping
// refs without changing effective status must not declassify an old private
// timestamp. Never nest or accumulate previous snapshots.
func briefPlanProvenance(current, previous, oldClock, newClock string) (string, error) {
	var snapshot struct {
		Current json.RawMessage `json:"current"`
		Clock   json.RawMessage `json:"clock,omitempty"`
	}
	if err := json.Unmarshal([]byte(firstNonEmptyString(previous, "{}")), &snapshot); err != nil {
		return "", err
	}
	oldAt, _ := time.Parse(time.RFC3339Nano, oldClock)
	newAt, _ := time.Parse(time.RFC3339Nano, newClock)
	current = firstNonEmptyString(current, "{}")
	if newAt.After(oldAt) {
		snapshot.Clock = json.RawMessage(current)
	} else if !oldAt.IsZero() && len(snapshot.Clock) == 0 {
		// Migration 108 initially seeds a raw plan rather than this wrapper.
		snapshot.Clock = snapshot.Current
		if len(snapshot.Clock) == 0 {
			snapshot.Clock = json.RawMessage(firstNonEmptyString(previous, "{}"))
		}
	}
	snapshot.Current = json.RawMessage(current)
	encoded, err := json.Marshal(snapshot)
	return string(encoded), err
}

// FoldPromisesFromEvents derives the same folds as the materialized path, per
// request, straight from message_posted events (at most plans.MaxPromiseNotes
// per card, through idx_events_thread_ts_id) instead of brief_notes. It is the
// reference the materialized projection is tested against.
func (s *Store) FoldPromisesFromEvents(ctx context.Context, cardIDs []string) (map[string]plans.PromiseFold, error) {
	if len(cardIDs) > briefRefreshBatch {
		cardIDs = cardIDs[:briefRefreshBatch]
	}
	inputs, err := briefPromiseInputs(ctx, s.db, cardIDs, func(ids []string) (map[string][]plans.PromiseNote, error) {
		return s.promiseNotesFromEvents(ctx, ids)
	})
	if err != nil {
		return nil, err
	}
	out := map[string]plans.PromiseFold{}
	for _, c := range inputs {
		if c.open < 0 {
			continue
		}
		out[c.id] = plans.FoldPromise(c.in)
	}
	return out, nil
}

func (s *Store) promiseNotesFromEvents(ctx context.Context, cardIDs []string) (map[string][]plans.PromiseNote, error) {
	out := map[string][]plans.PromiseNote{}
	for _, id := range cardIDs {
		rows, err := s.db.QueryContext(ctx, `SELECT e.id,COALESCE(e.actor_id,''),e.ts,e.payload_json FROM cards c JOIN events e ON e.thread_id=c.thread_id
			WHERE c.id=? AND e.type='message_posted' AND (json_extract(e.payload_json,'$.payload.next.at') IS NOT NULL OR json_array_length(json_extract(e.payload_json,'$.payload.evidence_refs'))>0) ORDER BY e.ts DESC LIMIT ?`, id, plans.MaxPromiseNotes)
		if err != nil {
			return nil, err
		}
		type raw struct{ id, actor, ts, payload string }
		events := []raw{}
		for rows.Next() {
			var r raw
			if err = rows.Scan(&r.id, &r.actor, &r.ts, &r.payload); err != nil {
				rows.Close()
				return nil, err
			}
			events = append(events, r)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return nil, err
		}
		for _, r := range events {
			var event map[string]any
			_ = json.Unmarshal([]byte(r.payload), &event)
			payload, _ := event["payload"].(map[string]any)
			n := plans.PromiseNote{EventID: r.id, Author: r.actor}
			n.At, _ = time.Parse(time.RFC3339Nano, r.ts)
			if next, ok := payload["next"].(map[string]any); ok {
				if at, ok := plans.ParseWhen(anyStringValue(next["at"]), n.At); ok {
					n.NextAt = at.Truncate(time.Second)
				}
				what := []rune(anyStringValue(next["what"]))
				if len(what) > 160 {
					what = what[:160]
				}
				n.NextWhat = string(what)
			}
			if n.Evidence, err = s.evidenceResolves(ctx, payload["evidence_refs"]); err != nil {
				return nil, err
			}
			out[id] = append(out[id], n)
		}
	}
	return out, nil
}

// evidenceResolves mirrors the brief_notes trigger: the first 8 refs, any
// http(s) URL or an existing event, card, artifact or document.
func (s *Store) evidenceResolves(ctx context.Context, raw any) (bool, error) {
	refs, _ := raw.([]any)
	if len(refs) > 8 {
		refs = refs[:8]
	}
	for _, r := range refs {
		ref, ok := r.(string)
		if !ok {
			continue
		}
		if (strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://")) && len(ref) > len("https://") {
			return true, nil
		}
		kind, key, _ := strings.Cut(ref, ":")
		query := map[string]string{
			"event":    `SELECT EXISTS(SELECT 1 FROM events WHERE id=?1 OR handle=?1)`,
			"card":     `SELECT EXISTS(SELECT 1 FROM cards WHERE id=?1 OR handle=?1)`,
			"artifact": `SELECT EXISTS(SELECT 1 FROM artifacts WHERE id=?1)`,
			"document": `SELECT EXISTS(SELECT 1 FROM documents WHERE id=?1 OR handle=?1)`,
		}[kind]
		if query == "" || key == "" {
			continue
		}
		var found bool
		if err := s.db.QueryRowContext(CanonicalMaintenanceContext(ctx), query, key).Scan(&found); err != nil {
			return false, err
		}
		if found {
			return true, nil
		}
	}
	return false, nil
}
