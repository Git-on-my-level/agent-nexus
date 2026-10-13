package primitives

import (
	"context"
	"encoding/json"
	"fmt"
)

// GetEventsByRefs hydrates a bounded collection of event IDs or public handles
// through the same scoped relation as GetEvent. Missing/hidden refs are omitted.
func (s *Store) GetEventsByRefs(ctx context.Context, refs []string) (map[string]map[string]any, error) {
	if len(refs) > 200 {
		return nil, fmt.Errorf("event reference batch exceeds 200")
	}
	keys := []string{}
	for _, ref := range refs {
		kind, value, ok := normalizeTypedRef(ref)
		if ok && kind == "event" {
			keys = append(keys, value)
		}
	}
	result := map[string]map[string]any{}
	byID := map[string]map[string]any{}
	if len(keys) == 0 {
		return result, nil
	}
	data, _ := json.Marshal(uniqueSortedStrings(keys))
	rows, err := s.db.QueryContext(ctx, `SELECT id,COALESCE(handle,''),type,ts,actor_id,thread_id,refs_json,payload_json,
 archived_at,archived_by,trashed_at,trashed_by,trash_reason FROM events
 WHERE id IN (SELECT value FROM json_each(?)) OR (handle IS NOT NULL AND trim(handle) <> '' AND handle IN (SELECT value FROM json_each(?)))`, string(data), string(data))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stored := []storedEventRow{}
	values := []any{}
	for rows.Next() {
		var row storedEventRow
		if err = rows.Scan(&row.id, &row.handle, &row.kind, &row.ts, &row.actor, &row.thread, &row.refs, &row.payload,
			&row.archivedAt, &row.archivedBy, &row.trashedAt, &row.trashedBy, &row.trashReason); err != nil {
			return nil, err
		}
		stored = append(stored, row)
		var refs, payload any
		if err = json.Unmarshal([]byte(row.refs), &refs); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(row.payload), &payload); err != nil {
			return nil, err
		}
		values = append(values, map[string]any{"refs": refs, "thread_ref": "thread:" + row.thread.String}, payload)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	ctx, err = withBatchPublicRefs(ctx, s.db, values)
	if err != nil {
		return nil, err
	}
	for _, row := range stored {
		body, err := decodeEventBodyFromRow(ctx, s.db, row.id, row.handle, row.kind, row.ts, row.actor, row.thread, row.refs, row.payload)
		if err != nil {
			return nil, err
		}
		overlayEventLifecycleFromSQLColumns(body, row.archivedAt, row.archivedBy, row.trashedAt, row.trashedBy, row.trashReason)
		result[body["ref"].(string)] = body
		byID["event:"+row.id] = body
	}
	for ref, body := range byID {
		result[ref] = body
	}
	return result, nil
}
