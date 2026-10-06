package primitives

import (
	"context"
	"encoding/json"
	"strings"
)

// FilterCardAccess checks the containing card AND board before enrichment. It
// reads only access facts, never plan or source evidence JSON.
func (s *Store) FilterCardAccess(ctx context.Context, cards []map[string]any, visible func(string, string) bool) ([]map[string]any, error) {
	if visible == nil {
		return cards, nil
	}
	allowed := map[string]bool{}
	for start := 0; start < len(cards); start += 200 {
		end := start + 200
		if end > len(cards) {
			end = len(cards)
		}
		ids := []string{}
		for _, c := range cards[start:end] {
			ids = append(ids, strings.TrimPrefix(workString(c["id"]), "card:"))
		}
		encoded, err := json.Marshal(ids)
		if err != nil {
			return nil, err
		}
		rows, err := s.db.QueryContext(ctx, `WITH access_ids AS (
 SELECT id FROM cards WHERE id IN (SELECT value FROM json_each(?))
 UNION SELECT id FROM cards INDEXED BY idx_cards_handle_unique WHERE handle IS NOT NULL AND trim(handle) <> '' AND handle IN (SELECT value FROM json_each(?))
 LIMIT 400)
 SELECT c.id,COALESCE(c.handle,''),COALESCE(NULLIF(trim(c.thread_id),''),trim(c.parent_thread_id),''),COALESCE(json_extract(t.body_json,'$.pm_actor_id'),''),COALESCE(b.thread_id,''),COALESCE(json_extract(bt.body_json,'$.pm_actor_id'),'')
 FROM access_ids r CROSS JOIN cards c ON c.id=r.id LEFT JOIN threads t ON t.id=COALESCE(NULLIF(trim(c.thread_id),''),trim(c.parent_thread_id)) LEFT JOIN boards b ON b.id=c.board_id LEFT JOIN threads bt ON bt.id=b.thread_id`, string(encoded), string(encoded))
		if err != nil {
			return nil, err
		}
		type access struct{ id, handle, thread, owner, boardThread, boardOwner string }
		batch := []access{}
		for rows.Next() {
			var a access
			if err = rows.Scan(&a.id, &a.handle, &a.thread, &a.owner, &a.boardThread, &a.boardOwner); err != nil {
				rows.Close()
				return nil, err
			}
			batch = append(batch, a)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		aliases := map[string][]bool{}
		for _, a := range batch {
			readable := visible(a.thread, a.owner) && visible(a.boardThread, a.boardOwner)
			allowed[a.id] = readable
			for _, alias := range uniqueSortedStrings([]string{a.id, a.handle}) {
				if alias != "" {
					aliases["card:"+alias] = append(aliases["card:"+alias], readable)
				}
			}
		}
		for alias, matches := range aliases {
			allowed[alias] = len(matches) == 1 && matches[0]
		}
	}
	out := []map[string]any{}
	for _, c := range cards {
		if allowed[workString(c["id"])] {
			out = append(out, c)
		}
	}
	return out, nil
}
