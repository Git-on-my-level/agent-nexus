package primitives

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// nextAskMootFromNotes evaluates the latest-note ask rule for open cards.
// notes maps card id to the citation text of that card's newest live note
// (the note brief.v1 shows). The lookup runs only when a note cites an
// event ref: one statement per 200 distinct refs, each an indexed seek on
// brief_asks and, for handle spellings, idx_events_handle_unique. The bound
// is the page (at most nextAskRefsPerNote refs per note, briefMootRefBudget
// refs in all), never workspace size. An ask the scoped events relation or
// visible hides is omitted, so its decision cannot flag the card or appear
// in the evidence.
func (s *Store) nextAskMootFromNotes(ctx context.Context, notes map[string]string, visible func(string, string) bool, now time.Time) (map[string]PossiblyMoot, error) {
	out := map[string]PossiblyMoot{}
	refsByCard := map[string][]string{}
	tokens := []string{}
	seen := map[string]bool{}
	for id, text := range notes {
		refs := eventRefsInNote(text)
		if len(refs) == 0 {
			continue
		}
		refsByCard[id] = refs
		for _, ref := range refs {
			if seen[ref] || len(tokens) >= briefMootRefBudget {
				continue
			}
			seen[ref] = true
			tokens = append(tokens, ref)
		}
	}
	if len(tokens) == 0 {
		return out, nil
	}
	asks, err := s.visibleCitedAsks(ctx, tokens, visible)
	if err != nil {
		return nil, err
	}
	for id, refs := range refsByCard {
		subset := map[string]citedAsk{}
		for _, ref := range refs {
			if ask, ok := asks[ref]; ok {
				subset[ref] = ask
				subset[strings.ToLower(ref)] = ask
			}
		}
		if moot := nextAskDecidedMoot(notes[id], subset, now); moot.Flagged {
			out[id] = moot
		}
	}
	return out, nil
}

func (s *Store) visibleCitedAsks(ctx context.Context, tokens []string, visible func(string, string) bool) (map[string]citedAsk, error) {
	out := map[string]citedAsk{}
	for start := 0; start < len(tokens); start += 200 {
		end := start + 200
		if end > len(tokens) {
			end = len(tokens)
		}
		batch, err := s.visibleCitedAsksBatch(ctx, tokens[start:end], visible)
		if err != nil {
			return nil, err
		}
		for key, ask := range batch {
			out[key] = ask
		}
	}
	return out, nil
}

func (s *Store) visibleCitedAsksBatch(ctx context.Context, tokens []string, visible func(string, string) bool) (map[string]citedAsk, error) {
	encoded, err := json.Marshal(tokens)
	if err != nil {
		return nil, err
	}
	// ask_id is the brief_asks primary key. Handle spellings resolve through
	// the partial unique handle index, then the same primary key. Both sides
	// of the union are bounded by this batch.
	rows, err := s.db.QueryContext(ctx, `SELECT a.ask_id, COALESCE(e.handle,''), a.kind, a.state, COALESCE(a.answered_at,''),
		COALESCE(e.thread_id,''),
		COALESCE((SELECT json_extract(t.body_json,'$.pm_actor_id') FROM threads t WHERE t.id=e.thread_id),''),
		COALESCE(json_extract(e.payload_json,'$.payload.expires_at'),''),
		e.payload_json,
		COALESCE(json_extract(r.payload_json,'$.payload.outcome'),''),
		COALESCE(json_extract(r.payload_json,'$.payload.reason'),'')
		FROM brief_asks a
		JOIN events e ON e.id=a.ask_id
		LEFT JOIN events r ON r.id=a.response_event_id AND a.response_event_id<>''
		WHERE COALESCE(e.trashed_at,'')=''
		AND a.ask_id IN (
			SELECT value FROM json_each(?)
			UNION
			SELECT ev.id FROM events ev INDEXED BY idx_events_handle_unique
			WHERE ev.handle IN (SELECT value FROM json_each(?)) AND ev.handle IS NOT NULL AND trim(ev.handle)<>''
		)`, string(encoded), string(encoded))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]citedAsk{}
	for rows.Next() {
		var id, handle, kind, state, answered, thread, owner, expires, payload, outcome, reason string
		if err = rows.Scan(&id, &handle, &kind, &state, &answered, &thread, &owner, &expires, &payload, &outcome, &reason); err != nil {
			return nil, err
		}
		if visible != nil && !visible(thread, owner) {
			continue
		}
		var wrapper map[string]any
		_ = json.Unmarshal([]byte(payload), &wrapper)
		decisionKind := briefDecisionKind(kind, asMapValue(wrapper["payload"]))
		if decisionKind != "ask" && decisionKind != "review" {
			continue
		}
		ask := citedAsk{
			ID: id, Kind: decisionKind, State: state, Outcome: outcome, Reason: reason,
			AnsweredAt: answered, ExpiresAt: expires, Visible: true,
		}
		out[id] = ask
		if lower := strings.ToLower(id); lower != id {
			out[lower] = ask
		}
		if handle != "" && handle != id {
			out[handle] = ask
		}
	}
	return out, rows.Err()
}
