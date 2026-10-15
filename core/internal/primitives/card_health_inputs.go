package primitives

import (
	"context"
	"encoding/json"
	"time"

	"agent-nexus-core/internal/plans"
)

type cardHealthInput struct {
	Activity, Created    time.Time
	Due                  string
	Phase, Owner, Handle string
	Source               map[string]any
	Children             []string
	Refs                 []string
	// Wake is the raw wake_condition annotation; Related holds the card's
	// related_refs linked sources.
	Wake    string
	Related []string
	// PlanAt is the last plan edit and Observed the last meaningful source
	// observation (external authority only); both reset the promise clock.
	PlanAt, Observed time.Time
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
	in := plans.HealthInput{Ref: ref, Plan: p, State: state, Facts: facts, Phase: input.Phase, CardDue: input.Due, Wake: input.Wake, PlanEditedAt: input.PlanAt, Notes: input.Notes}
	if !input.Observed.IsZero() {
		in.Observations = []time.Time{input.Observed}
	}
	return plans.HealthFor(in, now)
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
	rows, err := s.db.QueryContext(ctx, `SELECT page.value,n.event_id,n.author,n.ts,COALESCE(n.next_at,''),n.next_what,n.evidence,COALESCE(latest.payload_json,'')
		FROM json_each(?) page
		JOIN brief_notes n ON n.event_id IN (SELECT event_id FROM brief_notes WHERE card_id=page.value ORDER BY ts DESC LIMIT ?)
		LEFT JOIN (
			SELECT x.card_id, e.payload_json
			FROM json_each(?) ids
			JOIN brief_notes x ON x.event_id=(
				SELECT y.event_id FROM brief_notes y JOIN events ev ON ev.id=y.event_id
				WHERE y.card_id=ids.value AND COALESCE(ev.trashed_at,'')='' ORDER BY y.ts DESC LIMIT 1)
			JOIN events e ON e.id=x.event_id
		) latest ON latest.card_id=page.value`, string(encoded), plans.MaxPromiseNotes, string(encoded))
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
