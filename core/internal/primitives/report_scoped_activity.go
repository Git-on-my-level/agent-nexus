package primitives

import (
	"context"
	"encoding/json"
	"strings"
)

// ReportScopedEventIDs admits at most 201 visible events per explicit subject.
// Thread seeks include promise notes and resource lifecycle events. Events on
// another backing thread retain that thread's subject. Only bounded rows sort.
func (s *Store) ReportScopedEventIDs(ctx context.Context, refs []string) ([]string, error) {
	if len(refs) > 200 {
		return nil, workInvalid("at most 200 activity subjects")
	}
	requests := []map[string]string{}
	normalized := []string{}
	for _, ref := range refs {
		normalized = append(normalized, strings.Replace(ref, "doc:", "document:", 1))
	}
	snapshots, err := s.ReportSubjectSnapshots(ctx, normalized)
	if err != nil {
		return nil, err
	}
	cards, err := s.ReportWorkSnapshots(ctx, normalized)
	if err != nil {
		return nil, err
	}
	for _, ref := range normalized {
		kind, _, ok := strings.Cut(ref, ":")
		if !ok {
			continue
		}
		row := snapshots[ref]
		if kind == "card" {
			row = cards[ref]
		}
		if row == nil || workString(row["archived_at"]) != "" || workString(row["trashed_at"]) != "" {
			continue
		}
		requests = append(requests, map[string]string{"kind": kind, "id": workString(row["id"]), "subject": firstNonEmptyString(workString(row["ref"]), ref), "thread": firstNonEmptyString(workString(row["thread_id"]), workString(row["parent_thread_id"]))})
	}
	identities := append([]map[string]string{}, requests...)
	for _, r := range identities {
		idRef := r["kind"] + ":" + r["id"]
		if idRef != r["subject"] {
			requests = append(requests, map[string]string{"kind": r["kind"], "id": r["id"], "thread": "", "subject": idRef})
		}
	}
	raw, _ := json.Marshal(requests)
	rows, err := s.db.QueryContext(ctx, `WITH requested AS MATERIALIZED (SELECT json_extract(value,'$.kind') kind,json_extract(value,'$.id') id,json_extract(value,'$.thread') thread,json_extract(value,'$.subject') subject FROM json_each(?)), candidates AS (
 SELECT bounded.value id FROM requested r CROSS JOIN json_each((SELECT json_group_array(id) FROM (SELECT id FROM events WHERE thread_id=r.thread AND COALESCE(trashed_at,'')='' AND COALESCE(archived_at,'')='' ORDER BY ts DESC,id DESC LIMIT 201))) bounded
 UNION SELECT bounded.value id FROM requested r CROSS JOIN json_each((SELECT json_group_array(id) FROM (SELECT id FROM events WHERE COALESCE(json_extract(payload_json,'$.subject_ref'),json_extract(payload_json,'$.payload.subject_ref'))=r.subject AND COALESCE(trashed_at,'')='' AND COALESCE(archived_at,'')='' ORDER BY ts DESC,id DESC LIMIT 201))) bounded
 ) SELECT e.id FROM candidates c JOIN events e ON e.id=c.id ORDER BY e.ts DESC,e.id DESC LIMIT 201`, string(raw))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
