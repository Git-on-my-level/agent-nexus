// Thread reads: list queries, threadRow loading and scanning, and thread map
// projection.

package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type ThreadListFilter struct {
	States []string

	Query  string
	Limit  *int
	Cursor string
}

func (s *Store) ListThreads(ctx context.Context, filter ThreadListFilter) ([]map[string]any, string, error) {
	if s == nil || s.db == nil {
		return nil, "", fmt.Errorf("primitives store database is not initialized")
	}
	filter.States = NormalizeListLifecycleStates(filter.States)
	if filter.Cursor != "" {
		if _, err := decodeCursor(filter.Cursor); err != nil {
			return nil, "", fmt.Errorf("%w: %v", ErrInvalidCursor, err)
		}
	}

	query, args := buildListThreadsQuery(filter)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("query threads: %w", err)
	}
	defer rows.Close()

	threads := make([]map[string]any, 0)
	for rows.Next() {
		row, err := scanThreadRow(rows)
		if err != nil {
			return nil, "", err
		}
		threadMap, err := row.ToThreadMapPublic(ctx, s.db)
		if err != nil {
			return nil, "", err
		}

		threads = append(threads, threadMap)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate threads: %w", err)
	}

	var nextCursor string
	if filter.Limit != nil && len(threads) > *filter.Limit {
		threads = threads[:*filter.Limit]
		offset := 0
		if filter.Cursor != "" {
			offset, _ = decodeCursor(filter.Cursor)
		}
		nextCursor = encodeCursor(offset + *filter.Limit)
	}

	return threads, nextCursor, nil
}

func buildListThreadsQuery(filter ThreadListFilter) (string, []any) {
	query := `SELECT threads.id, COALESCE(threads.handle, ''), threads.kind, threads.thread_id, threads.updated_at, threads.updated_by, threads.body_json, threads.provenance_json, threads.archived_at, threads.archived_by, threads.trashed_at, threads.trashed_by, threads.trash_reason
		 FROM threads`
	args := make([]any, 0, 9)
	hasWhere := false
	appendClause := func(clause string) {
		if hasWhere {
			query += ` AND ` + clause
			return
		}
		query += ` WHERE ` + clause
		hasWhere = true
	}
	appendClause(LifecycleStatesOrGroup("threads.archived_at", "threads.trashed_at", filter.States))
	if q := strings.TrimSpace(filter.Query); q != "" {
		searchPattern := "%" + strings.ToLower(q) + "%"
		appendClause(`(LOWER(threads.id) LIKE ? OR LOWER(threads.thread_id) LIKE ? OR LOWER(COALESCE(threads.handle, '')) LIKE ? OR LOWER('thread:' || COALESCE(threads.handle, '')) LIKE ? OR LOWER(COALESCE(json_extract(body_json, '$.subject_ref'), json_extract(body_json, '$.topic_ref'), '')) LIKE ? OR LOWER(COALESCE(json_extract(body_json, '$.title'), '')) LIKE ? OR LOWER(COALESCE(json_extract(body_json, '$.current_summary'), '')) LIKE ?)`)
		args = append(args, searchPattern, searchPattern, searchPattern, searchPattern, searchPattern, searchPattern, searchPattern)
	}
	query += ` ORDER BY threads.updated_at DESC, threads.id ASC`
	if filter.Limit != nil && *filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, *filter.Limit+1)
		if filter.Cursor != "" {
			if offset, err := decodeCursor(filter.Cursor); err == nil && offset > 0 {
				query += ` OFFSET ?`
				args = append(args, offset)
			}
		}
	}
	return query, args
}

type threadRow struct {
	ID             string
	Handle         sql.NullString
	Kind           string
	ThreadID       sql.NullString
	UpdatedAt      string
	UpdatedBy      string
	BodyJSON       string
	ProvenanceJSON string
	ArchivedAt     sql.NullString
	ArchivedBy     sql.NullString
	TrashedAt      sql.NullString
	TrashedBy      sql.NullString
	TrashReason    sql.NullString
}

func (s *Store) getThreadRow(ctx context.Context, id string, tableName string) (threadRow, error) {
	if s == nil || s.db == nil {
		return threadRow{}, fmt.Errorf("primitives store database is not initialized")
	}

	return getThreadRowFromQueryRower(ctx, s.db, id, tableName)
}

func getThreadRowFromQueryRower(ctx context.Context, db queryRower, id string, tableName string) (threadRow, error) {
	row := threadRow{}
	err := db.QueryRowContext(
		ctx,
		fmt.Sprintf(`SELECT id, COALESCE(handle, ''), kind, thread_id, updated_at, updated_by, body_json, provenance_json, archived_at, archived_by, trashed_at, trashed_by, trash_reason FROM %s WHERE id = ?`, tableName),
		id,
	).Scan(&row.ID, &row.Handle, &row.Kind, &row.ThreadID, &row.UpdatedAt, &row.UpdatedBy, &row.BodyJSON, &row.ProvenanceJSON, &row.ArchivedAt, &row.ArchivedBy, &row.TrashedAt, &row.TrashedBy, &row.TrashReason)
	if errors.Is(err, sql.ErrNoRows) {
		return threadRow{}, ErrNotFound
	}
	if err != nil {
		return threadRow{}, fmt.Errorf("query threads row: %w", err)
	}
	return row, nil
}

func scanThreadRow(scanner interface{ Scan(dest ...any) error }) (threadRow, error) {
	row := threadRow{}
	if err := scanner.Scan(&row.ID, &row.Handle, &row.Kind, &row.ThreadID, &row.UpdatedAt, &row.UpdatedBy, &row.BodyJSON, &row.ProvenanceJSON, &row.ArchivedAt, &row.ArchivedBy, &row.TrashedAt, &row.TrashedBy, &row.TrashReason); err != nil {
		return threadRow{}, fmt.Errorf("scan threads row: %w", err)
	}
	return row, nil
}

func (r threadRow) ToThreadMap() (map[string]any, error) {
	body := map[string]any{}
	if strings.TrimSpace(r.BodyJSON) != "" {
		if err := json.Unmarshal([]byte(r.BodyJSON), &body); err != nil {
			return nil, fmt.Errorf("decode thread body: %w", err)
		}
	}
	if subjectRef := strings.TrimSpace(anyStringValue(body["subject_ref"])); subjectRef == "" {
		if legacyTopicRef := strings.TrimSpace(anyStringValue(body["topic_ref"])); legacyTopicRef != "" {
			body["subject_ref"] = legacyTopicRef
		}
	}
	delete(body, "topic_ref")

	provenance := map[string]any{}
	if strings.TrimSpace(r.ProvenanceJSON) != "" {
		if err := json.Unmarshal([]byte(r.ProvenanceJSON), &provenance); err != nil {
			return nil, fmt.Errorf("decode thread provenance: %w", err)
		}
	}

	body["id"] = r.ID
	handle := strings.TrimSpace(r.Handle.String)
	if handle == "" {
		handle = r.ID
	}
	body["ref"] = "thread:" + handle
	body["handle"] = handle
	if _, hasType := body["type"]; !hasType {
		body["type"] = r.Kind
	}
	body["updated_at"] = r.UpdatedAt
	body["updated_by"] = r.UpdatedBy
	if r.ThreadID.Valid {
		body["thread_id"] = r.ThreadID.String
	}
	body["provenance"] = provenance

	if r.ArchivedAt.Valid && strings.TrimSpace(r.ArchivedAt.String) != "" {
		body["archived_at"] = r.ArchivedAt.String
	}
	if r.ArchivedBy.Valid && strings.TrimSpace(r.ArchivedBy.String) != "" {
		body["archived_by"] = r.ArchivedBy.String
	}
	if r.TrashedAt.Valid && strings.TrimSpace(r.TrashedAt.String) != "" {
		body["trashed_at"] = r.TrashedAt.String
	}
	if r.TrashedBy.Valid && strings.TrimSpace(r.TrashedBy.String) != "" {
		body["trashed_by"] = r.TrashedBy.String
	}
	if r.TrashReason.Valid && strings.TrimSpace(r.TrashReason.String) != "" {
		body["trash_reason"] = r.TrashReason.String
	}

	delete(body, "status")
	body["state"] = canonicalLifecycleState(r.ArchivedAt, r.TrashedAt)

	return body, nil
}

func (r threadRow) ToThreadMapPublic(ctx context.Context, q queryRower) (map[string]any, error) {
	body, err := r.ToThreadMap()
	if err != nil {
		return nil, err
	}
	return publicRefsInValue(ctx, q, body).(map[string]any), nil
}

// StripThreadPlanningFieldsForAPI removes planning-only fields that belong on topics from a thread
// map before HTTP responses. Internal callers (e.g. projections) use GetThread without stripping.
func StripThreadPlanningFieldsForAPI(m map[string]any) {
	if m == nil {
		return
	}
	delete(m, "priority")
	delete(m, "tags")
	delete(m, "cadence")
	delete(m, "next_check_in_at")
}

func containsThreadRef(refs []string, threadID string) bool {
	target := "thread:" + threadID
	for _, ref := range refs {
		if ref == target {
			return true
		}
	}
	return false
}

func firstThreadRefValue(refs []string) string {
	for _, ref := range refs {
		prefix, value, ok := splitTypedRef(ref)
		if !ok || prefix != "thread" {
			continue
		}
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
