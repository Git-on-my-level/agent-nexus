package primitives

import (
	"context"
	"encoding/json"
	"strings"

	"agent-nexus-core/internal/handles"
	"agent-nexus-core/internal/schema"
)

// AgentSummaryCards selects a fixed candidate window before card hydration.
// Current/recent-run refs precede assigned cards; each assignment spelling has
// its own indexed, limited seek so a sort never materializes all assignments.
// The internal positions view contains traversal keys only: scope filtering
// belongs to the subsequent canonical hydration, after the candidate bound.
func (s *Store) AgentSummaryCards(ctx context.Context, actorRef string, refs []string) ([]map[string]any, bool, error) {
	ctx, closeRead, err := s.beginSummaryRead(ctx)
	if err != nil {
		return nil, false, err
	}
	defer closeRead()
	selectors := []map[string]string{}
	for _, ref := range refs {
		if len(selectors) == summaryBatchSize+1 {
			break
		}
		kind, value, err := schema.SplitTypedRef(ref)
		if err == nil && kind == "card" {
			selectors = append(selectors, map[string]string{"value": value, "handle": handles.Normalize(value)})
		}
	}
	raw, _ := json.Marshal(selectors)
	rows, err := s.db.QueryContext(ctx, `WITH
 referenced AS MATERIALIZED (SELECT COALESCE(
 (SELECT id FROM agent_summary_card_positions WHERE handle=json_extract(j.value,'$.handle') AND handle IS NOT NULL AND trim(handle)<>'' LIMIT 1),
 (SELECT id FROM agent_summary_card_positions WHERE id=json_extract(j.value,'$.value'))) AS id,CAST(j.key AS INTEGER) AS priority FROM json_each(?) j),
 assigned_raw AS MATERIALIZED (SELECT id FROM agent_summary_card_positions WHERE assignee=? ORDER BY id LIMIT 51),
 assigned_ref AS MATERIALIZED (SELECT id FROM agent_summary_card_positions WHERE assignee=? ORDER BY id LIMIT 51),
 candidates AS (SELECT id,priority FROM referenced WHERE id IS NOT NULL UNION ALL SELECT id,51 FROM assigned_raw UNION ALL SELECT id,51 FROM assigned_ref)
 SELECT id FROM candidates GROUP BY id ORDER BY min(priority),id LIMIT 51`, string(raw), strings.TrimPrefix(actorRef, "actor:"), actorRef)
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
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, false, err
	}
	truncated := len(ids) > summaryBatchSize
	if len(ids) > summaryBatchSize {
		ids = ids[:summaryBatchSize]
	}
	// A scoped preview cannot certify completeness. Never reveal whether the
	// internal window filled with hidden assignment/identity positions.
	if _, scoped := accessScopeFrom(ctx); scoped {
		truncated = true
	}
	if len(ids) == 0 {
		return []map[string]any{}, truncated, nil
	}
	cards, err := s.ListCards(ctx, CardListFilter{ids: ids})
	return cards, truncated, err
}
