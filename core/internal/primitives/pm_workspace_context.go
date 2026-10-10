package primitives

import (
	"context"
	"encoding/json"
)

// PMWorkspaceContext admits fixed routing windows before scoped hydration.
// Hidden candidates never trigger a refill or affect a public partial marker.
// Cards are ordered by blocked/review/active/ready/backlog attention, then recency.
func (s *Store) PMWorkspaceContext(ctx context.Context, reader string) (refs []string, asks, activity []any, err error) {
	rows, err := s.db.QueryContext(ctx, `WITH candidates AS MATERIALIZED (
 SELECT id,attention,updated_at FROM pm_workspace_card_positions ORDER BY attention,updated_at DESC,id DESC LIMIT 64
 ) SELECT c.id FROM candidates p CROSS JOIN cards c ON c.id=p.id
 LEFT JOIN card_plans cp ON cp.card_id=c.id LEFT JOIN work_metadata m ON m.card_id=c.id
 LEFT JOIN boards b ON b.id=c.board_id
 WHERE c.archived_at IS NULL AND c.trashed_at IS NULL
 AND b.archived_at IS NULL AND b.trashed_at IS NULL
 AND (cp.card_id IS NOT NULL OR json_type(m.metadata_json,'$.plan')='object' OR b.role='initiatives')
 ORDER BY p.attention,p.updated_at DESC,p.id DESC LIMIT 15`)
	if err != nil {
		return nil, nil, nil, err
	}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		refs = append(refs, "card:"+id)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, nil, nil, err
	}
	rows, err = s.db.QueryContext(ctx, `WITH candidates AS MATERIALIZED (
 SELECT id,trigger_at FROM (SELECT id,trigger_at FROM pm_workspace_ask_positions WHERE category='ask' AND recipient=? ORDER BY trigger_at,id LIMIT 16)
 UNION ALL SELECT id,trigger_at FROM (SELECT id,trigger_at FROM pm_workspace_ask_positions WHERE category='ask' AND recipient='' ORDER BY trigger_at,id LIMIT 16)
 ) SELECT i.id,i.data_json FROM candidates p CROSS JOIN derived_inbox_items i ON i.id=p.id ORDER BY p.trigger_at,p.id LIMIT 15`, reader)
	if err != nil {
		return nil, nil, nil, err
	}
	for rows.Next() {
		var id, raw string
		if err = rows.Scan(&id, &raw); err != nil {
			break
		}
		var data map[string]any
		if err = json.Unmarshal([]byte(raw), &data); err != nil {
			break
		}
		out := map[string]any{"ref": "inbox:" + id}
		for _, key := range []string{"title", "question", "summary", "subject_ref", "options", "recommended_answer", "body", "response_proposals"} {
			if v, ok := data[key]; ok {
				out[key] = v
			}
		}
		asks = append(asks, out)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, nil, nil, err
	}
	rows, err = s.db.QueryContext(ctx, `WITH candidates AS MATERIALIZED (
 SELECT id,ts FROM pm_workspace_event_positions ORDER BY ts DESC,id DESC LIMIT 32
 ) SELECT e.id,COALESCE(e.handle,''),e.type,e.ts,e.payload_json FROM candidates p CROSS JOIN events e ON e.id=p.id
 WHERE e.archived_at IS NULL AND e.trashed_at IS NULL ORDER BY p.ts DESC,p.id DESC LIMIT 15`)
	if err != nil {
		return nil, nil, nil, err
	}
	for rows.Next() {
		var id, handle, kind, at, raw string
		if err = rows.Scan(&id, &handle, &kind, &at, &raw); err != nil {
			break
		}
		var data map[string]any
		if err = json.Unmarshal([]byte(raw), &data); err != nil {
			break
		}
		if handle == "" {
			handle = id
		}
		out := map[string]any{"ref": "event:" + handle, "type": kind, "at": at}
		if summary, ok := data["summary"]; ok {
			out["summary"] = summary
		}
		content, _ := data["payload"].(map[string]any)
		for _, key := range []string{"text", "body", "title"} {
			if v, ok := content[key]; ok {
				out[key] = v
			}
		}
		activity = append(activity, out)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	return refs, asks, activity, err
}
