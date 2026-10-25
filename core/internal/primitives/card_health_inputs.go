package primitives

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"agent-nexus-core/internal/plans"
)

type cardHealthInput struct {
	DefaultAt                                                                                    time.Time
	Creator, ReminderEventID, OrphanedAt, FallbackOwner, LeaseExpires, LeaseReleased, LeaseOwner string
	Loop                                                                                         map[string]any
	Activity, Created                                                                            time.Time
	Due                                                                                          string
	Phase, Owner, Handle                                                                         string
	Source                                                                                       map[string]any
	Children                                                                                     []string
	Refs                                                                                         []string
	// Wake is the raw wake_condition annotation; Related holds the card's
	// related_refs linked sources.
	Wake    string
	Related []string
	// PlanAt records authoring; Observed is meaningful source evidence.
	PlanAt, Observed time.Time
	// Only the visible retained projection may supply historical step progress.
	StepStatus, StepChangeAt string
	// Notes are the card's promise notes, loaded once per loadPlans batch
	// through Store.PromiseNotesFor; nil for lightweight summaries.
	Notes []plans.PromiseNote
	// NextNote is the citation text of the newest live note, the same note
	// brief.v1 shows. Empty when the card has no live note.
	NextNote string
}

// health folds this card's promise facts (contracts promises/plan_health).
func (input cardHealthInput) health(ref string, p *plans.Plan, state plans.State, facts map[string]plans.Fact, now time.Time) plans.Health {
	if ref == "" && input.Handle != "" {
		ref = "card:" + input.Handle
	}
	if h, ok := cadenceTrackingHealth(input.Loop, input.Phase, ref, now); ok {
		return h
	}
	in := plans.HealthInput{Created: input.Created, DefaultAt: input.DefaultAt, DefaultAuthor: input.Creator, Ref: ref, Plan: p, State: state, Facts: facts, Phase: input.Phase, CardDue: input.Due, Wake: input.Wake, PlanEditedAt: input.PlanAt, Notes: input.Notes}
	_, clock := planStepProgressClock(state, input.StepStatus, input.StepChangeAt, input.PlanAt, false)
	in.StepChanges = []time.Time{}
	if !clock.IsZero() {
		in.StepChanges = append(in.StepChanges, clock)
	}
	if !input.Observed.IsZero() {
		in.Observations = []time.Time{input.Observed}
	}
	return input.trackingDigest(plans.HealthFor(in, now), now)
}

// Explicit work due annotations, including null/empty clearing, take precedence.
const effectiveCardDueSQL = `CASE WHEN json_type(m.metadata_json,'$.due_at') IS NOT NULL THEN COALESCE(json_extract(m.metadata_json,'$.due_at'),'') ELSE COALESCE(c.due_at,'') END`

// Source polling updates card.updated_at, so only meaningful source timestamps
// count. Native cards use canonical card/metadata updates. Plan/discussion edits are folded in
// by loadPlans for both enrichment and previews.
const effectiveCardActivitySQL = `CASE WHEN COALESCE(m.authority,'nexus')='nexus' THEN CASE WHEN julianday(m.updated_at)>julianday(c.updated_at) THEN m.updated_at ELSE c.updated_at END ELSE COALESCE(CASE WHEN julianday(json_extract(o.body_json,'$.source_activity_at')) > julianday(json_extract(o.body_json,'$.meaningful_progress_at')) THEN json_extract(o.body_json,'$.source_activity_at') END,json_extract(o.body_json,'$.meaningful_progress_at'),json_extract(o.body_json,'$.source_activity_at'),c.created_at) END`

// Wake and related refs live beside the other local annotations.
const cardSignalSQL = `COALESCE(json_extract(m.metadata_json,'$.wake_condition'),''),COALESCE(json_extract(m.metadata_json,'$.related_refs'),'[]')`

func latestCardActivity(values ...string) time.Time {
	var latest time.Time
	for _, raw := range values {
		at, _ := time.Parse(time.RFC3339Nano, raw)
		if at.After(latest) {
			latest = at
		}
	}
	return latest
}

// promiseNotesBatch loads the newest plans.MaxPromiseNotes promise notes per
// card for one loadPlans batch (at most summaryBatchSize ids) in a single
// statement over idx_brief_notes_card_ts: the bound grows with the batch,
// never with workspace size or note history. The same statement also reads
// the newest live note's payload, one indexed seek per card id, so the
// next-ask moot rule can see event refs without a second query.
func (s *Store) promiseNotesBatch(ctx context.Context, ids []string) (map[string][]plans.PromiseNote, map[string]string, error) {
	out := map[string][]plans.PromiseNote{}
	texts := map[string]string{}
	if len(ids) == 0 {
		return out, texts, nil
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, nil, err
	}
	// Resolve the newest live note once per card, in the materialized page that
	// drives the join. As a LEFT JOINed subquery it was re-evaluated for every
	// returned note row (behind an automatic index), walking each card's hidden
	// notes again behind the visibility filter.
	rows, err := s.db.QueryContext(ctx, `WITH page(card_id,payload_json) AS MATERIALIZED (
			SELECT ids.value,(SELECT e.payload_json FROM events e WHERE e.id=(
				SELECT y.event_id FROM brief_notes y JOIN events ev ON ev.id=y.event_id
				WHERE y.card_id=ids.value AND COALESCE(ev.trashed_at,'')='' ORDER BY y.ts DESC LIMIT 1))
			FROM json_each(?) ids)
		SELECT page.card_id,n.event_id,n.author,n.ts,COALESCE(n.next_at,''),n.next_what,n.evidence,COALESCE(page.payload_json,'')
		FROM page
		JOIN brief_notes n ON n.event_id IN (SELECT event_id FROM brief_notes WHERE card_id=page.card_id AND (next_at IS NOT NULL OR evidence=1) ORDER BY ts DESC LIMIT ?)`, string(encoded), plans.MaxPromiseNotes)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, ts, next, payload string
		var n plans.PromiseNote
		if err = rows.Scan(&id, &n.EventID, &n.Author, &ts, &next, &n.NextWhat, &n.Evidence, &payload); err != nil {
			return nil, nil, err
		}
		n.At, _ = time.Parse(time.RFC3339Nano, ts)
		n.NextAt, _ = time.Parse(time.RFC3339Nano, next)
		out[id] = append(out[id], n)
		if payload != "" && texts[id] == "" {
			texts[id] = noteCitationText(payload)
		}
	}
	return out, texts, rows.Err()
}

// planStepProgressClock is shared by read health and the canonical brief fold.
// Adding/removing steps is authoring. Only an existing step changing effective
// status advances the retained clock. Maintenance may bootstrap completed work;
// a read with no visible historical projection must not infer private progress.
func planStepProgressClock(state plans.State, previousSignature, previousClock string, editedAt time.Time, initialDone bool) (string, time.Time) {
	previous := map[string]string{}
	for _, part := range strings.Split(previousSignature, ";") {
		if id, status, ok := strings.Cut(part, "="); ok && id != "" {
			if status == "" {
				status = "not_started"
			}
			previous[id] = status
		}
	}
	parts := make([]string, 0, len(state.Steps))
	changed, done := false, false
	for _, step := range state.Steps {
		parts = append(parts, step.ID+"="+step.Status)
		if old, ok := previous[step.ID]; ok && old != step.Status {
			changed = true
		}
		done = done || step.Status == "done"
	}
	clock := latestCardActivity(previousClock)
	if changed || (initialDone && previousSignature == "" && done) {
		if editedAt.After(clock) {
			clock = editedAt
		}
		if movement := latestCardActivity(state.LastMovementAt); movement.After(clock) {
			clock = movement
		}
	}
	return strings.Join(parts, ";"), clock
}
