package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"agent-nexus-core/internal/curation"
	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/resourceaccess"
)

// Curation reads are bounded by these caps, independent of workspace size:
// one indexed page of open asks, one page of open initiatives, and one batched
// ref resolution shared by every moot evaluation.
const (
	curationAskLimit  = curation.MaxAsks
	curationCardLimit = curation.MaxCards
	curationRefLimit  = 200
	// Metrics folds read at most this many asks and notes per window.
	curationMetricAskLimit = 2000
	// Promise reads: rows per indexed seek, and distinct cards whose notes
	// are folded (one seek of plans.MaxPromiseNotes rows each).
	curationMetricPromiseLimit = 2000
	curationMetricPromiseCards = 200
)

// Curation proposal states in curation_proposals. A key is proposed at most
// once while open and never again once dismissed.
const (
	CurationProposalOpen      = "open"
	CurationProposalAccepted  = "accepted"
	CurationProposalDismissed = "dismissed"
)

var ErrCurationTargetGone = errors.New("curation_target_gone")

// CurationProposalRecord is the dedupe row behind one proposal ask.
type CurationProposalRecord struct {
	Key             string   `json:"key"`
	ProposalEventID string   `json:"event_id"`
	Action          string   `json:"action"`
	Source          string   `json:"source"`
	Status          string   `json:"status"`
	Targets         []string `json:"targets"`
}

// CurationCandidates runs the formula over the open asks and initiatives the
// visible func admits. Private (PM-owned) threads are never candidates, so a
// proposal never names a private ref or title, whoever reads it.
func (s *Store) CurationCandidates(ctx context.Context, visible func(string, string) bool, now time.Time) ([]curation.Candidate, []string, error) {
	strict := func(thread, owner string) bool {
		return owner == "" && (visible == nil || visible(thread, owner))
	}
	// The system pass has no reader scope; ref facts then resolve unfiltered
	// (sources are only counted, never named) while asks and cards keep the
	// strict rule.
	resolveVisible := strict
	if visible == nil {
		resolveVisible = nil
	}
	limitations := []string{}
	asks, truncated, err := s.curationOpenAsks(ctx, strict)
	if err != nil {
		return nil, nil, err
	}
	if truncated {
		limitations = append(limitations, fmt.Sprintf("reviewed the %d open asks due soonest", curationAskLimit))
	}
	cards, err := s.curationOpenCards(ctx, strict)
	if err != nil {
		return nil, nil, err
	}
	if len(cards) >= curationCardLimit {
		limitations = append(limitations, fmt.Sprintf("reviewed the %d most recently updated open initiatives", curationCardLimit))
	}
	moot, skipped, err := s.curationMootFunc(ctx, asks, resolveVisible, now)
	if err != nil {
		return nil, nil, err
	}
	if skipped {
		limitations = append(limitations, fmt.Sprintf("linked sources beyond %d refs were not resolved; those asks are not proposed", curationRefLimit))
	}
	out := curation.MootCandidates(asks, moot)
	out = append(out, curation.DuplicateCandidates(cards, curation.DefaultDuplicateThreshold)...)
	out = append(out, curation.BatchCandidates(asks)...)
	return out, limitations, nil
}

// curationOpenAsks reads one indexed page of open subject asks (ask_subjects
// keeps open current through its resolution trigger), then hydrates only
// those rows. Restricted asks are dropped, not flagged.
func (s *Store) curationOpenAsks(ctx context.Context, visible func(string, string) bool) ([]curation.AskInfo, bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT ask_id FROM ask_subjects WHERE open=1 ORDER BY due_at,ask_id LIMIT ?`, curationAskLimit+1)
	if err != nil {
		return nil, false, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, false, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	truncated := len(ids) > curationAskLimit
	if truncated {
		ids = ids[:curationAskLimit]
	}
	if len(ids) == 0 {
		return nil, false, nil
	}
	encoded, _ := json.Marshal(ids)
	rows, err = s.db.QueryContext(ctx, `SELECT e.id,COALESCE(e.thread_id,''),e.ts,e.payload_json,COALESCE(json_extract(t.body_json,'$.pm_actor_id'),'')
 FROM events e LEFT JOIN threads t ON t.id=e.thread_id
 WHERE e.id IN (SELECT value FROM json_each(?)) AND e.type='human_attention_requested' AND COALESCE(e.trashed_at,'')=''
 AND NOT EXISTS (SELECT 1 FROM human_attention_request_resolutions r WHERE r.request_event_id=e.id)`, string(encoded))
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	asks := []curation.AskInfo{}
	subjects := []map[string]any{}
	for rows.Next() {
		var id, thread, ts, payloadJSON, owner string
		if err = rows.Scan(&id, &thread, &ts, &payloadJSON, &owner); err != nil {
			return nil, false, err
		}
		if !visible(thread, owner) {
			continue
		}
		var wrapper map[string]any
		if json.Unmarshal([]byte(payloadJSON), &wrapper) != nil {
			continue
		}
		payload := asMapValue(wrapper["payload"])
		kind := strings.ToLower(strings.TrimSpace(anyStringValue(payload["kind"])))
		if kind != "" && kind != "ask" && kind != "review" && kind != "escalate" {
			continue
		}
		created, _ := time.Parse(time.RFC3339Nano, ts)
		ask := curation.AskInfo{
			ID: id, Title: anyStringValue(payload["title"]), ThreadID: thread,
			SubjectRef:       strings.TrimSpace(anyStringValue(payload["subject_ref"])),
			RelatedRefs:      anyStringSlice(payload["related_refs"]),
			RequesterActorID: anyStringValue(payload["requester_actor_id"]),
			BatchID:          strings.TrimSpace(anyStringValue(payload["batch_id"])),
			CreatedAt:        created,
			Proposal:         payload["curation"] != nil,
		}
		asks = append(asks, ask)
		if strings.HasPrefix(ask.SubjectRef, "card:") {
			// The prefixed ref keeps handle subjects resolvable: FilterCardAccess
			// reports handle aliases only under their "card:" form.
			subjects = append(subjects, map[string]any{"id": ask.SubjectRef})
		}
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	// The subject card must be readable too: an ask in a public thread about
	// a private card would otherwise name it.
	readable, err := s.FilterCardAccess(ctx, subjects, visible)
	if err != nil {
		return nil, false, err
	}
	ok := map[string]bool{}
	for _, card := range readable {
		ok[workString(card["id"])] = true
	}
	out := asks[:0]
	for _, ask := range asks {
		if strings.HasPrefix(ask.SubjectRef, "card:") && !ok[ask.SubjectRef] {
			continue
		}
		out = append(out, ask)
	}
	return out, truncated, nil
}

// curationOpenCards reads one bounded page of open initiatives through the
// report work path, then drops any card the strict visibility rejects.
func (s *Store) curationOpenCards(ctx context.Context, visible func(string, string) bool) ([]curation.CardInfo, error) {
	page, err := s.ListReportWork(ctx, ReportWorkFilter{Limit: curationCardLimit})
	if err != nil {
		return nil, err
	}
	work, err := s.FilterCardAccess(ctx, page.Work, visible)
	if err != nil {
		return nil, err
	}
	cards := make([]curation.CardInfo, 0, len(work))
	for _, w := range work {
		ref := workString(w["handle"])
		if ref == "" {
			ref = workString(w["id"])
		}
		created, _ := workTimestamp(w["created_at"])
		cards = append(cards, curation.CardInfo{
			Ref: "card:" + strings.TrimPrefix(ref, "card:"), Title: workString(w["title"]),
			Phase: workString(w["phase"]), RelatedRefs: anyStringSlice(w["related_refs"]), CreatedAt: created,
		})
	}
	return cards, nil
}

// curationMootFunc resolves every ask's linked sources in one bounded batch
// and returns the shared possibly_moot rule over those facts. A ref beyond the
// budget stays unresolved, which withholds the flag for its ask.
func (s *Store) curationMootFunc(ctx context.Context, asks []curation.AskInfo, visible func(string, string) bool, now time.Time) (func([]string) curation.MootResult, bool, error) {
	refs := []string{}
	seen := map[string]bool{}
	skipped := false
	for _, ask := range asks {
		for _, ref := range possiblyMootLinked(append([]string{ask.SubjectRef}, ask.RelatedRefs...)) {
			if seen[ref] {
				continue
			}
			if len(refs) >= curationRefLimit {
				skipped = true
				continue
			}
			seen[ref] = true
			refs = append(refs, ref)
		}
	}
	facts := map[string]plans.Fact{}
	if len(refs) > 0 {
		previews, err := s.ResolveRefs(ctx, refs, visible, now)
		if err != nil {
			return nil, false, err
		}
		for _, preview := range previews {
			status := preview.Phase
			if status == "" {
				status = preview.Status
			}
			facts[preview.Ref] = plans.Fact{Known: preview.Resolvable, Status: status}
		}
	}
	return func(linked []string) curation.MootResult {
		m := PossiblyMootFromFacts(linked, facts)
		return curation.MootResult{Flagged: m.Flagged, Reason: m.Reason, Sources: m.Sources}
	}, skipped, nil
}

// curationInternal drops the reader's policy for curation_proposals only: it
// is an internal dedupe index keyed by proposal hashes, never returned to a
// reader except through a proposal ask the reader can already see.
func curationInternal(ctx context.Context) context.Context {
	return resourceaccess.WithoutPolicy(ctx)
}

// CurationProposal returns the dedupe row for a key, or ErrNotFound.
func (s *Store) CurationProposal(ctx context.Context, key string) (CurationProposalRecord, error) {
	var rec CurationProposalRecord
	var targets string
	err := s.db.QueryRowContext(curationInternal(ctx), `SELECT key,proposal_event_id,action,source,status,targets_json FROM curation_proposals WHERE key=?`, key).
		Scan(&rec.Key, &rec.ProposalEventID, &rec.Action, &rec.Source, &rec.Status, &targets)
	if err == sql.ErrNoRows {
		return rec, ErrNotFound
	}
	if err != nil {
		return rec, err
	}
	_ = json.Unmarshal([]byte(targets), &rec.Targets)
	return rec, nil
}

// CurationProposalByEvent finds the proposal behind an ask event, or ErrNotFound.
func (s *Store) CurationProposalByEvent(ctx context.Context, eventID string) (CurationProposalRecord, error) {
	var key string
	err := s.db.QueryRowContext(curationInternal(ctx), `SELECT key FROM curation_proposals WHERE proposal_event_id=?`, eventID).Scan(&key)
	if err == sql.ErrNoRows {
		return CurationProposalRecord{}, ErrNotFound
	}
	if err != nil {
		return CurationProposalRecord{}, err
	}
	return s.CurationProposal(ctx, key)
}

// FileCurationProposal files one proposal ask and its dedupe row. An existing
// open or dismissed key returns that record instead of filing again; the
// returned bool reports whether a new ask was filed.
func (s *Store) FileCurationProposal(ctx context.Context, actor string, c curation.Candidate, source string, event map[string]any) (CurationProposalRecord, bool, error) {
	if existing, err := s.CurationProposal(ctx, c.Key); err == nil {
		if existing.Status != CurationProposalAccepted {
			return existing, false, nil
		}
	} else if !errors.Is(err, ErrNotFound) {
		return CurationProposalRecord{}, false, err
	}
	stored, err := s.AppendTaskAttentionEvent(ctx, actor, event)
	if err != nil {
		return CurationProposalRecord{}, false, err
	}
	rec := CurationProposalRecord{Key: c.Key, ProposalEventID: anyStringValue(stored["id"]), Action: string(c.Action), Source: source, Status: CurationProposalOpen, Targets: c.Targets}
	targets, _ := json.Marshal(c.Targets)
	// Accepted keys may be proposed again (the target came back); the upsert
	// keeps one row per key either way.
	_, err = s.db.ExecContext(curationInternal(ctx), `INSERT INTO curation_proposals(key,proposal_event_id,action,source,status,targets_json,created_at) VALUES(?,?,?,?,?,?,?)
 ON CONFLICT(key) DO UPDATE SET proposal_event_id=excluded.proposal_event_id,status=excluded.status,source=excluded.source,created_at=excluded.created_at,resolved_at=NULL`,
		rec.Key, rec.ProposalEventID, rec.Action, rec.Source, rec.Status, string(targets), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return CurationProposalRecord{}, false, err
	}
	return rec, true, nil
}

// ResolveCurationProposal marks a proposal accepted or dismissed. Only an
// open row moves, so a replayed answer never flips a decision.
func (s *Store) ResolveCurationProposal(ctx context.Context, key, status string) (bool, error) {
	res, err := s.db.ExecContext(curationInternal(ctx), `UPDATE curation_proposals SET status=?,resolved_at=? WHERE key=? AND status='open'`, status, time.Now().UTC().Format(time.RFC3339Nano), key)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// WithdrawAskByProposal withdraws another principal's ask because a human
// accepted a curation proposal for it (invariant I4: the human decides, the
// proposal only proposed). It shares the requester path's atomic resolution
// claim, so a racing answer and this withdrawal never both resolve the ask.
// Access-request asks are never withdrawable.
func (s *Store) WithdrawAskByProposal(ctx context.Context, humanActorID, askEventID, proposalEventID, reason string) (map[string]any, error) {
	var requester string
	err := s.db.QueryRowContext(ctx, `SELECT trim(COALESCE(json_extract(payload_json,'$.payload.requester_actor_id'),'')) FROM events WHERE id=? AND type='human_attention_requested'`, askEventID).Scan(&requester)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	event := map[string]any{"payload": map[string]any{"reason": reason}, "provenance": map[string]any{"sources": []string{"event:" + proposalEventID}}}
	stored, err := s.appendHumanAttentionWithdrawalAs(ctx, requester, humanActorID, askEventID, event)
	if err != nil {
		return nil, err
	}
	return stored, nil
}

// CurationMetrics folds S17, S18 and S20 over a bounded window: the newest
// asks and notes by the (type, ts) index, at most the metric limits each.
func (s *Store) CurationMetrics(ctx context.Context, window time.Duration, now time.Time) (curation.Metrics, error) {
	start := now.Add(-window)
	since := start.UTC().Format(time.RFC3339Nano)
	truncated := false
	rows, err := s.db.QueryContext(ctx, `SELECT e.id,e.ts,e.payload_json,r.resolution_type,COALESCE(x.ts,''),COALESCE(x.payload_json,'')
 FROM events e
 LEFT JOIN human_attention_request_resolutions r ON r.request_event_id=e.id
 LEFT JOIN events x ON x.id=r.resolution_event_id
 WHERE e.type='human_attention_requested' AND e.ts>=? AND COALESCE(e.trashed_at,'')=''
 ORDER BY e.ts DESC,e.id DESC LIMIT ?`, since, curationMetricAskLimit+1)
	if err != nil {
		return curation.Metrics{}, err
	}
	asks := []curation.AskRecord{}
	for rows.Next() {
		var id, ts, payloadJSON, resolutionTS, resolutionJSON string
		var resolution sql.NullString
		if err = rows.Scan(&id, &ts, &payloadJSON, &resolution, &resolutionTS, &resolutionJSON); err != nil {
			rows.Close()
			return curation.Metrics{}, err
		}
		if len(asks) >= curationMetricAskLimit {
			truncated = true
			break
		}
		asks = append(asks, curationAskRecord(id, ts, payloadJSON, resolution.String, resolutionTS, resolutionJSON))
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return curation.Metrics{}, err
	}
	promises, promisesTruncated, err := s.curationPromiseRecords(ctx, start, now)
	if err != nil {
		return curation.Metrics{}, err
	}
	m := curation.ComputeMetrics(asks, promises, start, now)
	m.Truncated = truncated || promisesTruncated
	return m, nil
}

func curationAskRecord(id, ts, payloadJSON, resolution, resolutionTS, resolutionJSON string) curation.AskRecord {
	var wrapper map[string]any
	_ = json.Unmarshal([]byte(payloadJSON), &wrapper)
	payload := asMapValue(wrapper["payload"])
	at, _ := time.Parse(time.RFC3339Nano, ts)
	rec := curation.AskRecord{ID: id, RequestedAt: at, Proposals: anyStringSlice(payload["response_proposals"]), Blocks: len(anyStringSlice(payload["blocks"])), Proposal: payload["curation"] != nil}
	if resolution == "" || resolutionJSON == "" {
		return rec
	}
	var resolved map[string]any
	_ = json.Unmarshal([]byte(resolutionJSON), &resolved)
	rp := asMapValue(resolved["payload"])
	resolvedAt, _ := time.Parse(time.RFC3339Nano, resolutionTS)
	if resolution == "withdrawn" {
		rec.Withdrawal = &curation.AskWithdrawal{At: resolvedAt, Reason: anyStringValue(rp["reason"])}
	} else {
		rec.Response = &curation.AskResponse{At: resolvedAt, Text: anyStringValue(rp["response_text"]), Outcome: anyStringValue(rp["outcome"])}
	}
	return rec
}

// curationPromiseRecords reads the promises that came due in [start, now]
// from core's materialized promise fold (brief_initiatives, brief_notes), never
// from raw events. Two bounded, indexed reads feed it:
//
//   - Misses: open initiatives whose counted promise (the fold's earliest
//     unmet at, any source: note, plan step, due_at, wake) is due in the
//     window, via idx_brief_initiatives_page (open, promise_at). Late evidence
//     ends the slip when the fold recorded it after the promise.
//   - Cleared note promises: notes whose next_at is due in the window, via
//     idx_brief_notes_next_at, for at most briefRefreshBatch distinct cards.
//     Each card's newest plans.MaxPromiseNotes notes (one seek per card) give
//     the author's latest promise; plans.PromiseNote.Evidence and the fold's
//     last_evidence_at say when a reset landed after it. A reset at or before
//     the promised time means met; a later one is a late miss.
//
// Fixed (step, due_at, wake) promises that were met leave no trace in the
// fold, so they are not counted; the metric covers every unmet promise and
// every cleared note promise.
func (s *Store) curationPromiseRecords(ctx context.Context, start, now time.Time) ([]curation.PromiseRecord, bool, error) {
	lo, hi := start.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339)
	out := []curation.PromiseRecord{}
	truncated := false

	rows, err := s.db.QueryContext(ctx, `SELECT promise_at,promise_moves,COALESCE(last_evidence_at,'') FROM brief_initiatives INDEXED BY idx_brief_initiatives_page
 WHERE open=1 AND promise_at>=? AND promise_at<=? ORDER BY promise_at DESC LIMIT ?`, lo, hi, curationMetricPromiseLimit+1)
	if err != nil {
		return nil, false, err
	}
	for rows.Next() {
		var at, evidence string
		var moves int
		if err = rows.Scan(&at, &moves, &evidence); err != nil {
			rows.Close()
			return nil, false, err
		}
		if len(out) >= curationMetricPromiseLimit {
			truncated = true
			break
		}
		rec := curation.PromiseRecord{Moves: moves}
		rec.At, _ = time.Parse(time.RFC3339Nano, at)
		rec.ResolvedAt, _ = time.Parse(time.RFC3339Nano, evidence)
		out = append(out, rec)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, false, err
	}

	rows, err = s.db.QueryContext(ctx, `SELECT card_id FROM brief_notes INDEXED BY idx_brief_notes_next_at
 WHERE next_at>=? AND next_at<=? ORDER BY next_at DESC,ts DESC LIMIT ?`, lo, hi, curationMetricPromiseLimit+1)
	if err != nil {
		return nil, false, err
	}
	cardIDs, seen, read := []string{}, map[string]bool{}, 0
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, false, err
		}
		if read++; read > curationMetricPromiseLimit {
			truncated = true
			break
		}
		if seen[id] {
			continue
		}
		if len(cardIDs) >= curationMetricPromiseCards {
			truncated = true
			break
		}
		seen[id] = true
		cardIDs = append(cardIDs, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	if len(cardIDs) == 0 {
		return out, truncated, nil
	}
	notes, err := s.PromiseNotesFor(ctx, cardIDs)
	if err != nil {
		return nil, false, err
	}
	folds, err := s.PromiseFolds(ctx, cardIDs)
	if err != nil {
		return nil, false, err
	}
	for _, id := range cardIDs {
		out = append(out, clearedNotePromises(notes[id], folds[id].LastEvidenceAt, start, now)...)
	}
	return out, truncated, nil
}

// clearedNotePromises applies the fold's note-promise rule to one card: an
// author's latest note carrying next.at is that author's promise, and it is
// cleared when any reset lands after the note. Open ones are the fold's
// misses, already read from brief_initiatives, so they are skipped here.
func clearedNotePromises(notes []plans.PromiseNote, lastEvidence, start, now time.Time) []curation.PromiseRecord {
	sort.SliceStable(notes, func(i, j int) bool { return notes[i].At.Before(notes[j].At) })
	latest := map[string]int{}
	for i, n := range notes {
		if !n.NextAt.IsZero() {
			latest[n.Author] = i
		}
	}
	authors := make([]string, 0, len(latest))
	for a := range latest {
		authors = append(authors, a)
	}
	sort.Strings(authors)
	var out []curation.PromiseRecord
	for _, author := range authors {
		n := notes[latest[author]]
		if n.NextAt.Before(start) || n.NextAt.After(now) {
			continue
		}
		// Earliest known reset after the note: an evidence note, or the
		// fold's newest reset of any kind.
		var resolved time.Time
		for _, e := range notes {
			if e.Evidence && e.At.After(n.At) && (resolved.IsZero() || e.At.Before(resolved)) {
				resolved = e.At
			}
		}
		if lastEvidence.After(n.At) && (resolved.IsZero() || lastEvidence.Before(resolved)) {
			resolved = lastEvidence
		}
		if resolved.IsZero() {
			continue
		}
		// Moves: this author's next.at changes since the last reset before
		// the promise, as the fold counts them.
		moves, prev, resetAt := 0, time.Time{}, time.Time{}
		for _, e := range notes {
			if e.At.After(n.At) {
				break
			}
			if e.Evidence {
				resetAt = e.At
			}
			if e.Author != author || e.NextAt.IsZero() {
				continue
			}
			if !prev.IsZero() && !prev.Equal(e.NextAt) && !e.Evidence && e.At.After(resetAt) {
				moves++
			}
			prev = e.NextAt
		}
		rec := curation.PromiseRecord{At: n.NextAt, Moves: moves}
		if resolved.After(n.NextAt) {
			rec.ResolvedAt = resolved
		} else {
			rec.Met = true
		}
		out = append(out, rec)
	}
	return out
}

// CurationCardThread returns the backing thread of a card ref, for filing a
// merge proposal where both initiatives' readers already look.
func (s *Store) CurationCardThread(ctx context.Context, ref string) (string, error) {
	resolved, err := s.ResolveResourceRef(ctx, ResourceRefInput{Type: "card", Ref: ref})
	if err != nil {
		return "", err
	}
	var thread string
	err = s.db.QueryRowContext(ctx, `SELECT COALESCE(NULLIF(trim(thread_id),''),trim(COALESCE(parent_thread_id,'')),'') FROM cards WHERE id=?`, resolved.ID).Scan(&thread)
	if err == sql.ErrNoRows {
		return "", ErrNotFound
	}
	return thread, err
}

// ApplyCurationAction performs an accepted proposal as the answering human
// through the same store paths a human uses directly. It returns what undo
// needs (nil when the action has no in-place reversal) and a trail summary
// that names refs, never titles.
func (s *Store) ApplyCurationAction(ctx context.Context, humanActorID, proposalEventID string, rec CurationProposalRecord) (map[string]any, string, error) {
	switch curation.Action(rec.Action) {
	case curation.ActionWithdraw:
		if len(rec.Targets) != 1 || !strings.HasPrefix(rec.Targets[0], "event:") {
			return nil, "", ErrCurationTargetGone
		}
		if _, err := s.WithdrawAskByProposal(ctx, humanActorID, strings.TrimPrefix(rec.Targets[0], "event:"), proposalEventID, "moot: accepted curation proposal"); err != nil {
			return nil, "", err
		}
		// A withdrawal resolves the ask for good; undo is re-asking, which the
		// trail makes possible by naming the original ask.
		return map[string]any{"reask": rec.Targets[0]}, "Withdrew " + rec.Targets[0] + " as moot.", nil
	case curation.ActionMerge:
		if len(rec.Targets) != 2 {
			return nil, "", ErrCurationTargetGone
		}
		resolved, err := s.ResolveResourceRef(ctx, ResourceRefInput{Type: "card", Ref: rec.Targets[0]})
		if err != nil {
			return nil, "", err
		}
		result, err := s.ArchiveBoardCard(ctx, humanActorID, "", resolved.ID, RemoveBoardCardInput{})
		if err != nil {
			return nil, "", err
		}
		undo := map[string]any{"restore_card": rec.Targets[0], "card_id": resolved.ID, "board_id": workString(result.Card["board_id"])}
		return undo, "Archived " + rec.Targets[0] + " as a duplicate of " + rec.Targets[1] + ". Restore the card to undo.", nil
	case curation.ActionBatch:
		// Filed asks keep their own ids; the grouping is stored so brief.v1
		// collapses the open members into one decision.
		if err := s.GroupCurationBatch(ctx, rec); err != nil {
			return nil, "", err
		}
		return map[string]any{"ungroup": rec.Key}, fmt.Sprintf("Grouped %d asks as one decision. Ungroup the batch to undo.", len(rec.Targets)), nil
	}
	return nil, "", ErrCurationTargetGone
}

// GroupCurationBatch records an accepted batch. A member already in another
// batch moves to this one: the newest human decision wins.
func (s *Store) GroupCurationBatch(ctx context.Context, rec CurationProposalRecord) error {
	if len(rec.Targets) < 2 || len(rec.Targets) > curation.MaxBatchMembers {
		return ErrCurationTargetGone
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, target := range rec.Targets {
		id := strings.TrimPrefix(target, "event:")
		if id == target || id == "" {
			return ErrCurationTargetGone
		}
		if _, err := s.db.ExecContext(curationInternal(ctx), `INSERT INTO curation_batch_members(ask_id,batch_key,proposal_event_id,created_at) VALUES(?,?,?,?)
 ON CONFLICT(ask_id) DO UPDATE SET batch_key=excluded.batch_key,proposal_event_id=excluded.proposal_event_id,created_at=excluded.created_at`, id, rec.Key, rec.ProposalEventID, now); err != nil {
			return err
		}
	}
	return nil
}

// ReadableCurationProposal returns the proposal for key only when the
// caller's scoped events relation and the PM-private thread rule admit its
// ask; otherwise ErrNotFound, so an unreadable key is indistinguishable from
// an unknown one.
func (s *Store) ReadableCurationProposal(ctx context.Context, key string, visible func(string, string) bool) (CurationProposalRecord, error) {
	rec, err := s.CurationProposal(ctx, key)
	if err != nil {
		return rec, err
	}
	var thread, owner string
	err = s.db.QueryRowContext(ctx, `SELECT COALESCE(e.thread_id,''),COALESCE((SELECT json_extract(t.body_json,'$.pm_actor_id') FROM threads t WHERE t.id=e.thread_id),'') FROM events e WHERE e.id=? AND COALESCE(e.trashed_at,'')=''`, rec.ProposalEventID).Scan(&thread, &owner)
	if err == sql.ErrNoRows || (err == nil && visible != nil && !visible(thread, owner)) {
		return CurationProposalRecord{}, ErrNotFound
	}
	return rec, err
}

// CurationBatchMemberAsk is one still-open member of an accepted curation batch.
type CurationBatchMemberAsk struct {
	AskID, InboxItemID, Kind, Title, SubjectRef, ThreadID    string
	RequesterActor, RequesterAgent, RequesterLabel, Delivery string
}

// OpenCurationBatchMembers lists open members of a curation batch.
// hidden is true when any open member is outside the caller's scoped events
// relation: the caller must not answer a partial set, and the bool carries
// no member id. The list is capped at curation.MaxBatchMembers.
func (s *Store) OpenCurationBatchMembers(ctx context.Context, key string) ([]CurationBatchMemberAsk, bool, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, false, nil
	}
	const memberSQL = `SELECT a.ask_id,a.inbox_item_id,a.kind,a.title,a.subject_ref,COALESCE(e.thread_id,''),
		COALESCE(json_extract(e.payload_json,'$.payload.requester_actor_id'),''),
		COALESCE(json_extract(e.payload_json,'$.payload.requester_agent_id'),''),
		COALESCE(json_extract(e.payload_json,'$.payload.requester_label'),''),
		COALESCE(json_extract(e.payload_json,'$.payload.delivery'),'')
		FROM curation_batch_members m
		JOIN brief_asks a ON a.ask_id=m.ask_id
		JOIN events e ON e.id=m.ask_id
		WHERE m.batch_key=? AND a.state='open' AND COALESCE(e.trashed_at,'')=''
		ORDER BY a.created_at,a.ask_id
		LIMIT ?`
	var total int
	if err := s.db.QueryRowContext(curationInternal(ctx), `SELECT COUNT(*) FROM curation_batch_members m JOIN brief_asks a ON a.ask_id=m.ask_id JOIN events e ON e.id=m.ask_id WHERE m.batch_key=? AND a.state='open' AND COALESCE(e.trashed_at,'')=''`, key).Scan(&total); err != nil {
		return nil, false, err
	}
	if total > curation.MaxBatchMembers {
		return nil, false, fmt.Errorf("curation batch has %d open members", total)
	}
	rows, err := s.db.QueryContext(ctx, memberSQL, key, curation.MaxBatchMembers)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []CurationBatchMemberAsk{}
	for rows.Next() {
		var m CurationBatchMemberAsk
		if err = rows.Scan(&m.AskID, &m.InboxItemID, &m.Kind, &m.Title, &m.SubjectRef, &m.ThreadID, &m.RequesterActor, &m.RequesterAgent, &m.RequesterLabel, &m.Delivery); err != nil {
			return nil, false, err
		}
		out = append(out, m)
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	if len(out) != total {
		return nil, true, nil
	}
	return out, false, nil
}

// UngroupCurationBatch undoes an accepted batch: its members become separate
// decisions again. It reports whether any grouping was removed.
func (s *Store) UngroupCurationBatch(ctx context.Context, key string) (bool, error) {
	res, err := s.db.ExecContext(curationInternal(ctx), `DELETE FROM curation_batch_members WHERE batch_key=?`, key)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
