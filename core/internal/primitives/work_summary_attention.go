package primitives

import (
	"context"
	"encoding/json"
	"time"
)

const summaryBatchSize = 50
const summaryAskCandidates = 11

// Sample at most eleven oldest candidates per alias and audience, before
// authorization. An arbitrarily large denied prefix cannot increase the work.
// Counts are exact for complete windows and lower bounds otherwise.
func (s *Store) enrichSummaryAttention(ctx context.Context, inputs map[string]cardHealthInput, summaries map[string]*WorkSummary, now time.Time) error {
	if len(summaries) == 0 {
		return nil
	}
	recipient := ""
	_, scopeApplied := accessScopeFrom(ctx)
	if scope, ok := accessScopeFrom(ctx); ok {
		recipient = scope.ActorID
	}
	audiences := []string{""}
	if recipient != "" {
		audiences = append(audiences, recipient)
	}
	type selected struct {
		ID   string   `json:"id"`
		Refs []string `json:"refs"`
	}
	selectedCards := []selected{}
	for id := range summaries {
		refs := []string{"card:" + id}
		if handle := inputs[id].Handle; handle != "" && handle != id {
			refs = append(refs, "card:"+handle)
		}
		selectedCards = append(selectedCards, selected{ID: id, Refs: refs})
	}
	var ready bool
	if err := s.db.QueryRowContext(ctx, `SELECT done FROM work_summary_asks_job WHERE singleton=1`).Scan(&ready); err != nil {
		return err
	}
	for start := 0; start < len(selectedCards); start += summaryBatchSize {
		end := start + summaryBatchSize
		if end > len(selectedCards) {
			end = len(selectedCards)
		}
		raw, _ := json.Marshal(selectedCards[start:end])
		rows, err := s.db.QueryContext(ctx, `WITH candidates AS MATERIALIZED (
 SELECT json_extract(c.value,'$.id') card_id,ids.value inbox_id,ref.value subject_ref,audience.value recipient
 FROM json_each(?) c CROSS JOIN json_each(c.value,'$.refs') ref CROSS JOIN json_each(?) audience
 CROSS JOIN json_each((SELECT json_group_array(inbox_id) FROM (
 SELECT inbox_id FROM work_summary_asks WHERE subject_ref=ref.value AND recipient_actor_id=audience.value
 ORDER BY trigger_at,inbox_id LIMIT 11))) ids
 ) SELECT candidate.card_id,candidate.inbox_id,i.trigger_at FROM candidates candidate
 LEFT JOIN derived_inbox_items i ON i.id=candidate.inbox_id
 AND COALESCE(json_extract(i.data_json,'$.kind'),i.category)='ask'
 AND COALESCE(json_extract(i.data_json,'$.subject_ref'),'')=candidate.subject_ref
 AND COALESCE(json_extract(i.data_json,'$.recipient_actor_id'),'')=candidate.recipient`, string(raw), workJSONList(audiences))
		if err != nil {
			return err
		}
		counts := map[string]int{}
		// A card has at most four windows (UUID/handle x broadcast/recipient).
		windows := map[string]int{}
		seen := map[string]bool{}
		oldest := map[string]time.Time{}
		for rows.Next() {
			var id, inbox string
			var trigger *string
			if err = rows.Scan(&id, &inbox, &trigger); err != nil {
				rows.Close()
				return err
			}
			windows[id]++
			if trigger == nil || seen[id+":"+inbox] {
				continue
			}
			seen[id+":"+inbox] = true
			counts[id]++
			at, _ := time.Parse(time.RFC3339Nano, *trigger)
			if !at.IsZero() && (oldest[id].IsZero() || at.Before(oldest[id])) {
				oldest[id] = at
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, c := range selectedCards[start:end] {
			out := summaries[c.ID]
			// Conservatively mark potentially full windows; never claim an exact count
			// from a capped index sample or an unfinished historical backfill.
			// Scoped readers always receive this uncertainty marker, independent of
			// hidden candidate counts, so it cannot reveal private ask existence.
			partial := !ready || scopeApplied || windows[c.ID] >= summaryAskCandidates
			out.AttentionTruncated = partial
			if counts[c.ID] > 0 {
				age := int64(0)
				if !oldest[c.ID].IsZero() {
					age = ageSeconds(now, oldest[c.ID])
				}
				out.Attention = &SummaryAttention{Count: counts[c.ID], OldestAge: age, Truncated: partial}
			}
		}
	}
	return nil
}
func workJSONList(v []string) string { raw, _ := json.Marshal(v); return string(raw) }
