package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"agent-nexus-core/internal/handles"
	"agent-nexus-core/internal/schema"
)

// PM list presentation needs only phase and revision. Resolve and project those
// fields in one scoped read, preserving the point reader's reference precedence
// and card lifecycle without hydrating report-only thread and attempt context.
func (s *Store) decisionWorkSnapshots(ctx context.Context, refs []string) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	if len(refs) == 0 {
		return out, nil
	}
	selectors := []map[string]string{}
	for _, ref := range refs {
		value := strings.TrimSpace(ref)
		if strings.Contains(value, ":") {
			kind, suffix, err := schema.SplitTypedRef(value)
			if err != nil || kind != "card" {
				continue
			}
			value = suffix
		}
		selectors = append(selectors, map[string]string{"ref": ref, "value": value, "handle": handles.Normalize(value)})
	}
	raw, _ := json.Marshal(selectors)
	// Resolve selectors first, then hydrate only those IDs. Keeping handle/alias
	// precedence separate avoids multiplying scoped card relations in one query.
	rows, err := s.db.QueryContext(ctx, `SELECT json_extract(j.value,'$.ref'),
 COALESCE((SELECT h.id FROM cards h WHERE h.handle=json_extract(j.value,'$.handle') AND h.handle IS NOT NULL AND trim(h.handle)<>'' LIMIT 1),aliases.resource_id,(SELECT canonical.id FROM cards canonical WHERE canonical.id=json_extract(j.value,'$.value')))
 FROM json_each(?) j
 LEFT JOIN resource_handle_aliases aliases ON aliases.resource_type='card' AND aliases.alias_handle=json_extract(j.value,'$.handle')
 `, string(raw))
	if err != nil {
		return nil, err
	}
	resolved := []map[string]string{}
	for rows.Next() {
		var ref string
		var id sql.NullString
		if err = rows.Scan(&ref, &id); err != nil {
			rows.Close()
			return nil, err
		}
		if id.Valid {
			resolved = append(resolved, map[string]string{"ref": ref, "id": id.String})
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(resolved) == 0 {
		return out, err
	}
	raw, _ = json.Marshal(resolved)
	rows, err = s.db.QueryContext(ctx, `SELECT json_extract(selected.value,'$.ref'),COALESCE(json_extract(placement.metadata_json,'$.column_key'),?),c.head_revision_number,
 COALESCE(m.metadata_json,'{"source":{"authority":"nexus"}}'),COALESCE(m.version,0),o.body_json
 FROM json_each(?) selected CROSS JOIN cards c ON c.id=json_extract(selected.value,'$.id')
 JOIN ref_edges placement ON placement.id=(SELECT re.id FROM ref_edges re WHERE re.source_type='board' AND re.edge_type='board_card' AND re.target_id=c.id LIMIT 1)
 LEFT JOIN work_metadata m ON m.card_id=c.id LEFT JOIN work_observations o ON o.id=m.latest_observation_id
 WHERE COALESCE(c.archived_at,'')='' AND COALESCE(c.trashed_at,'')=''`, boardDefaultColumn, string(raw))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ref, phase, metadata string
		var head, version int64
		var observation sql.NullString
		if err := rows.Scan(&ref, &phase, &head, &metadata, &version, &observation); err != nil {
			return nil, err
		}
		var m, latest map[string]any
		if err := json.Unmarshal([]byte(metadata), &m); err != nil {
			return nil, err
		}
		if observation.Valid {
			if err := json.Unmarshal([]byte(observation.String), &latest); err != nil {
				return nil, err
			}
		}
		out[ref] = projectWork(map[string]any{"column_key": phase, "head_revision_number": head}, m, version, latest, nil, nil)
	}
	return out, rows.Err()
}
