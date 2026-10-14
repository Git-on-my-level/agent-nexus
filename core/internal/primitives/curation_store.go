package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"agent-nexus-core/internal/curation"
	"agent-nexus-core/internal/plans"
)

// Curation reads are bounded by these caps, independent of workspace size:
// one indexed page of open asks, one page of open initiatives, and one batched
// ref resolution shared by every moot evaluation.
const (
	curationAskLimit  = curation.MaxAsks
	curationCardLimit = curation.MaxCards
	curationRefLimit  = 200
	// Metrics folds read at most this many asks and notes per window.
	curationMetricAskLimit  = 2000
	curationMetricNoteLimit = 5000
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
	moot, skipped, err := s.curationMootFunc(ctx, asks, strict, now)
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
	rows, err := s.db.QueryContext(ctx, `SELECT ask_id FROM ask_subjects INDEXED BY ask_subjects_due WHERE open=1 ORDER BY due_at,ask_id LIMIT ?`, curationAskLimit+1)
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
			subjects = append(subjects, map[string]any{"id": strings.TrimPrefix(ask.SubjectRef, "card:")})
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
		ok["card:"+workString(card["id"])] = true
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
		previews, err := s.ResolveRefs(ctx, refs, visible, now, 0)
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

// CurationProposal returns the dedupe row for a key, or ErrNotFound.
func (s *Store) CurationProposal(ctx context.Context, key string) (CurationProposalRecord, error) {
	var rec CurationProposalRecord
	var targets string
	err := s.db.QueryRowContext(ctx, `SELECT key,proposal_event_id,action,source,status,targets_json FROM curation_proposals WHERE key=?`, key).
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
	err := s.db.QueryRowContext(ctx, `SELECT key FROM curation_proposals WHERE proposal_event_id=?`, eventID).Scan(&key)
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
	_, err = s.db.ExecContext(ctx, `INSERT INTO curation_proposals(key,proposal_event_id,action,source,status,targets_json,created_at) VALUES(?,?,?,?,?,?,?)
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
	res, err := s.db.ExecContext(ctx, `UPDATE curation_proposals SET status=?,resolved_at=? WHERE key=? AND status='open'`, status, time.Now().UTC().Format(time.RFC3339Nano), key)
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
 FROM events e INDEXED BY idx_events_type_ts_id
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
	notes, notesTruncated, err := s.curationNoteRecords(ctx, since)
	if err != nil {
		return curation.Metrics{}, err
	}
	m := curation.ComputeMetrics(asks, notes, start, now)
	m.Truncated = truncated || notesTruncated
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

// curationNoteRecords reads notes (messages carrying a promise or evidence)
// and card movement in the window. Card updates and moves reset the promise
// clock like evidence does, per the one-clock rule.
func (s *Store) curationNoteRecords(ctx context.Context, since string) ([]curation.NoteRecord, bool, error) {
	notes := []curation.NoteRecord{}
	truncated := false
	// One equality probe per type keeps each read on the (type, ts) index
	// with no sort; the shared cap bounds the whole fold.
	for _, kind := range []string{"message_posted", "card_moved", "card_resolved"} {
		remaining := curationMetricNoteLimit - len(notes)
		if remaining <= 0 {
			truncated = true
			break
		}
		rows, err := s.db.QueryContext(ctx, `SELECT COALESCE(e.thread_id,''),e.ts,e.payload_json FROM events e INDEXED BY idx_events_type_ts_id
 WHERE e.type=? AND e.ts>=? AND COALESCE(e.trashed_at,'')=''
 ORDER BY e.ts DESC,e.id DESC LIMIT ?`, kind, since, remaining+1)
		if err != nil {
			return nil, false, err
		}
		read := 0
		for rows.Next() {
			var thread, ts, payloadJSON string
			if err = rows.Scan(&thread, &ts, &payloadJSON); err != nil {
				rows.Close()
				return nil, false, err
			}
			if read++; read > remaining {
				truncated = true
				break
			}
			at, _ := time.Parse(time.RFC3339Nano, ts)
			rec := curation.NoteRecord{ThreadID: thread, At: at, Reset: kind != "message_posted"}
			if kind == "message_posted" {
				var wrapper map[string]any
				_ = json.Unmarshal([]byte(payloadJSON), &wrapper)
				payload := asMapValue(wrapper["payload"])
				rec.NextAt = curationPromiseAt(asMapValue(payload["next"])["at"])
				rec.Reset = len(anyStringSlice(payload["evidence_refs"])) > 0 || anyStringValue(payload["evidence"]) != "" || len(anyStringSlice(payload["evidence"])) > 0
				if rec.NextAt.IsZero() && !rec.Reset {
					continue
				}
			}
			notes = append(notes, rec)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, false, err
		}
	}
	return notes, truncated, nil
}

func curationPromiseAt(raw any) time.Time {
	value := strings.TrimSpace(anyStringValue(raw))
	if value == "" {
		return time.Time{}
	}
	if at, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return at
	}
	if at, err := time.Parse("2006-01-02", value); err == nil {
		return at
	}
	return time.Time{}
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
		// Filed asks keep their own ids; the grouping lives in the trail so
		// Decide can show the members together. Undo is ignoring the trail.
		return map[string]any{"ungroup": rec.Targets}, fmt.Sprintf("Grouped %d asks as one decision.", len(rec.Targets)), nil
	}
	return nil, "", ErrCurationTargetGone
}
