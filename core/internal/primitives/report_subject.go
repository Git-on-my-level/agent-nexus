package primitives

import (
	"context"
	"encoding/json"
	"strings"
)

// ReportSubjectSnapshot reads the same scoped canonical row as the full getter.
// Reports need its title and lifecycle, without hydrating child associations.
func (s *Store) ReportSubjectSnapshot(ctx context.Context, kind, id string) (map[string]any, error) {
	switch kind {
	case "board":
		row, err := s.getBoardRow(ctx, id)
		if err != nil {
			return nil, err
		}
		out := map[string]any{"id": row.ID, "title": row.Title, "thread_id": row.ThreadID}
		lifecycleFieldsFromSQLColumns(row.ArchivedAt, row.ArchivedBy, row.TrashedAt, row.TrashedBy, row.TrashReason).apply(out)
		return out, nil
	case "topic":
		row, err := s.getTopicRow(ctx, id)
		if err != nil {
			return nil, err
		}
		out := map[string]any{"id": row.ID, "title": row.Title.String, "thread_id": row.ThreadID.String}
		lifecycleFieldsFromSQLColumns(row.ArchivedAt, row.ArchivedBy, row.TrashedAt, row.TrashedBy, row.TrashReason).apply(out)
		return out, nil
	}
	return nil, ErrInvalidResourceRef
}

// ReportSubjectSnapshots batches lifecycle/title/thread reads by primary key or
// unique handle. It returns only rows from scoped canonical relations.
func (s *Store) ReportSubjectSnapshots(ctx context.Context, refs []string) (map[string]map[string]any, error) {
	if len(refs) > 200 {
		return nil, workInvalid("at most 200 report refs")
	}
	out := map[string]map[string]any{}
	groups := map[string][]string{}
	for _, ref := range refs {
		kind, value, ok := strings.Cut(ref, ":")
		if ok && (kind == "board" || kind == "topic" || kind == "document" || kind == "thread") {
			groups[kind] = append(groups[kind], value)
		}
	}
	requests := []map[string]string{}
	for kind, values := range groups {
		for _, value := range values {
			requests = append(requests, map[string]string{"kind": kind, "value": value})
		}
	}
	aliases := map[string]string{}
	if len(requests) > 0 {
		raw, _ := json.Marshal(requests)
		rows, err := s.db.QueryContext(ctx, `SELECT a.resource_type,a.alias_handle,a.resource_id FROM json_each(?) j JOIN resource_handle_aliases a ON a.resource_type=json_extract(j.value,'$.kind') AND a.alias_handle=json_extract(j.value,'$.value')`, string(raw))
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var kind, alias, id string
			if err = rows.Scan(&kind, &alias, &id); err != nil {
				rows.Close()
				return nil, err
			}
			aliases[kind+":"+alias] = kind + ":" + id
			groups[kind] = append(groups[kind], id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	for _, kind := range []string{"board", "topic", "document", "thread"} {
		values := groups[kind]
		if len(values) == 0 {
			continue
		}
		raw, _ := json.Marshal(values)
		table := map[string]string{"board": "boards", "topic": "topics", "document": "documents", "thread": "threads"}[kind]
		query := `SELECT id,COALESCE(handle,''),COALESCE(title,''),COALESCE(thread_id,''),COALESCE(archived_at,''),COALESCE(trashed_at,'') FROM ` + table + ` WHERE id IN (SELECT value FROM json_each(?)) OR (handle IS NOT NULL AND trim(handle)<>'' AND handle IN (SELECT value FROM json_each(?)))`
		args := []any{string(raw), string(raw)}
		if kind == "thread" {
			query = `SELECT id,COALESCE(handle,''),COALESCE(json_extract(body_json,'$.title'),''),id,COALESCE(archived_at,''),COALESCE(trashed_at,'') FROM threads WHERE id IN (SELECT value FROM json_each(?)) OR (handle IS NOT NULL AND trim(handle)<>'' AND handle IN (SELECT value FROM json_each(?)))`
		}
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, handle, title, thread, archived, trashed string
			if err = rows.Scan(&id, &handle, &title, &thread, &archived, &trashed); err != nil {
				rows.Close()
				return nil, err
			}
			row := map[string]any{"id": id, "ref": kind + ":" + firstNonEmptyString(handle, id), "title": title, "thread_id": thread, "archived_at": archived, "trashed_at": trashed}
			out[kind+":"+id] = row
			if handle != "" {
				out[kind+":"+handle] = row
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	for alias, target := range aliases {
		if row := out[target]; row != nil {
			out[alias] = row
		}
	}
	return out, nil
}
