package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"agent-nexus-core/internal/handles"
	"agent-nexus-core/internal/schema"
)

// SummaryCardSnapshots admits at most 50 timeline refs before resolving handles
// or hydrating bodies. Timeline navigation includes archived/trashed subjects;
// canonical authorization remains independent of lifecycle. Read only primary
// card placement, avoiding history and navigational edge metadata projection.
func (s *Store) SummaryCardSnapshots(ctx context.Context, refs []string) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	if len(refs) > summaryBatchSize {
		return nil, invalidBoardRequest("summary card refs must contain at most 50 strings")
	}
	if len(refs) == 0 {
		return out, nil
	}
	ctx, closeRead, err := s.beginSummaryRead(ctx)
	if err != nil {
		return nil, err
	}
	defer closeRead()
	selectors := []map[string]string{}
	for _, ref := range refs {
		kind, value, err := schema.SplitTypedRef(strings.TrimSpace(ref))
		if err != nil || kind != "card" {
			continue
		}
		selectors = append(selectors, map[string]string{"ref": ref, "value": value, "handle": handles.Normalize(value)})
	}
	raw, _ := json.Marshal(selectors)
	// Event refs use current handles or canonical IDs. Keep this preview on
	// those indexed identities; historical alias reason text belongs to the
	// explicit point resolver, outside this bounded convenience projection.
	rows, err := s.db.QueryContext(ctx, `SELECT json_extract(j.value,'$.ref'),
 COALESCE((SELECT h.id FROM cards h WHERE h.handle=json_extract(j.value,'$.handle') AND h.handle IS NOT NULL AND trim(h.handle)<>'' LIMIT 1),(SELECT canonical.id FROM cards canonical WHERE canonical.id=json_extract(j.value,'$.value')))
 FROM json_each(?) j`, string(raw))
	if err != nil {
		return nil, err
	}
	byRef := map[string]string{}
	ids := []string{}
	for rows.Next() {
		var ref string
		var id sql.NullString
		if err = rows.Scan(&ref, &id); err != nil {
			rows.Close()
			return nil, err
		}
		if id.Valid {
			byRef[ref] = id.String
			ids = append(ids, id.String)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(ids) == 0 {
		return out, err
	}
	cards, err := s.ListCards(ctx, CardListFilter{ids: uniqueSortedStrings(ids), includeLifecycleHidden: true})
	if err != nil {
		return nil, err
	}
	byID := map[string]map[string]any{}
	for _, card := range cards {
		byID[workString(card["id"])] = card
	}
	for ref, id := range byRef {
		if card := byID[id]; card != nil {
			out[ref] = card
		}
	}
	return out, nil
}
