package primitives

import (
	"context"
	"encoding/json"
)

// CurationSuggestions admits a bounded window through the open-state index,
// then hydrates it through scoped canonical events. Hidden rows never refill
// the window. The internal ledger is admission only, never returned directly.
func (s *Store) CurationSuggestions(ctx context.Context, visible func(string, string) bool) ([]map[string]any, bool, error) {
	rows, err := s.db.QueryContext(curationInternal(ctx), `SELECT proposal_event_id FROM curation_proposals WHERE status='open' ORDER BY created_at,key LIMIT ?`, BriefSectionLimit+1)
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
	truncated := len(ids) > BriefSectionLimit
	// A scoped reader must not infer hidden ledger occupancy from this flag.
	// It always denotes a bounded, potentially partial window for that reader.
	if _, scoped := accessScopeFrom(ctx); scoped {
		truncated = true
	}
	if len(ids) > BriefSectionLimit {
		ids = ids[:BriefSectionLimit]
	}
	out := []map[string]any{}
	if len(ids) == 0 {
		return out, truncated, nil
	}
	encoded, _ := json.Marshal(ids)
	rows, err = s.db.QueryContext(ctx, `SELECT e.id,COALESCE(e.thread_id,''),COALESCE(json_extract(t.body_json,'$.pm_actor_id'),''),e.payload_json
 FROM json_each(?) admitted CROSS JOIN events e ON e.id=admitted.value CROSS JOIN threads t ON t.id=e.thread_id WHERE e.type='curation_suggested' AND COALESCE(e.trashed_at,'')='' ORDER BY e.ts,e.id`, string(encoded))
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, thread, owner, body string
		if err = rows.Scan(&id, &thread, &owner, &body); err != nil {
			return nil, false, err
		}
		if visible != nil && !visible(thread, owner) {
			continue
		}
		var wrapper map[string]any
		if err = json.Unmarshal([]byte(body), &wrapper); err != nil {
			return nil, false, err
		}
		payload := asMapValue(wrapper["payload"])
		c := cloneMap(asMapValue(payload["curation"]))
		c["event_id"], c["title"], c["body"] = id, payload["title"], payload["body"]
		// argv is an array so clients can execute it without shell interpolation.
		for _, resolution := range []string{"apply", "dismiss"} {
			request, _ := json.Marshal(map[string]any{"action": c["action"], "targets": c["targets"], "resolution": resolution, "rationale": "Agent reviewed suggestion: " + resolution})
			c[resolution+"_argv"] = []string{"anx", "api", "createCurationProposal", "--json-body", string(request)}
		}
		out = append(out, c)
	}
	return out, truncated, rows.Err()
}
