package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

const EventStreamPageSize = 200
const EventStreamChunkCandidateBudget = 2000

// EventStreamPage carries server-local traversal state. Cursor can refer to a
// hidden row and MUST NOT be serialized to a client (including SSE IDs).
type EventStreamPage struct {
	Events  []map[string]any
	Cursor  EventCursor `json:"-"`
	HasMore bool
}

// EventStreamCursor accepts only a currently visible event as client-supplied
// continuation provenance. Hidden, trashed and unknown IDs all seed head.
// Internal progress over hidden positions stays within the SSE connection.
func (s *Store) EventStreamCursor(ctx context.Context, lastEventID string) (EventCursor, error) {
	var cursor EventCursor
	if id := strings.TrimSpace(lastEventID); id != "" {
		readCtx := withEventPageAccessScope(ctx, []string{id})
		predicate, args := eventStreamLegacyPMPredicate(readCtx)
		args = append([]any{id}, args...)
		err := s.db.QueryRowContext(readCtx, `SELECT e.ts,e.id FROM events e WHERE e.id=? AND COALESCE(e.trashed_at,'')=''`+predicate, args...).Scan(&cursor.TS, &cursor.ID)
		if err == nil {
			return cursor, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return cursor, err
		}
	}
	err := s.db.QueryRowContext(ctx, `SELECT ts,id FROM event_stream_positions ORDER BY anx_timestamp_key(ts) DESC,id DESC LIMIT 1`).Scan(&cursor.TS, &cursor.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return EventCursor{}, nil
	}
	return cursor, err
}

// ListEventStreamPage seeks a bounded canonical position window, then joins
// the scoped events relation. Hidden, trashed and nonmatching rows advance the
// internal cursor, but their payloads are never loaded or decoded. The extra
// position is a has-more probe and is revisited on the next tick.
func (s *Store) ListEventStreamPage(ctx context.Context, filter EventListFilter, cursor EventCursor) (EventStreamPage, error) {
	// Bound each index range before merging it. Ordering an unbounded UNION
	// branch by its constant expression key makes SQLite sort every timestamp
	// tie, even when the outer LIMIT is small.
	query := `WITH stream_same AS MATERIALIZED (
		SELECT id,ts,anx_timestamp_key(ts) AS sort_key FROM event_stream_positions
		WHERE anx_timestamp_key(ts)=anx_timestamp_key(?) AND id>?
		ORDER BY id LIMIT ?
	), stream_later AS MATERIALIZED (
		SELECT id,ts,anx_timestamp_key(ts) AS sort_key FROM event_stream_positions
		WHERE anx_timestamp_key(ts)>anx_timestamp_key(?)
		ORDER BY anx_timestamp_key(ts),id LIMIT ?
	), stream_candidates AS MATERIALIZED (
		SELECT * FROM stream_same UNION ALL SELECT * FROM stream_later
		ORDER BY sort_key,id LIMIT ?
	) SELECT ts,id FROM stream_candidates ORDER BY sort_key,id`
	args := []any{cursor.TS, cursor.ID, EventStreamPageSize + 1, cursor.TS, EventStreamPageSize + 1, EventStreamPageSize + 1}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return EventStreamPage{}, err
	}
	page := EventStreamPage{Cursor: cursor, Events: []map[string]any{}}
	ids := []string{}
	for rows.Next() {
		var position EventCursor
		if err := rows.Scan(&position.TS, &position.ID); err != nil {
			rows.Close()
			return EventStreamPage{}, err
		}
		if len(ids) == EventStreamPageSize {
			page.HasMore = true
			continue
		}
		ids = append(ids, position.ID)
		page.Cursor = position
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return EventStreamPage{}, err
	}
	if len(ids) == 0 {
		return page, nil
	}
	ctx = withEventPageAccessScope(ctx, ids)
	keys, _ := json.Marshal(ids)
	query = `SELECT e.id,COALESCE(e.handle,''),e.type,e.ts,e.actor_id,e.thread_id,e.refs_json,e.payload_json,
		e.archived_at,e.archived_by,e.trashed_at,e.trashed_by,e.trash_reason
		FROM events e WHERE e.id IN (SELECT value FROM json_each(?)) AND COALESCE(e.trashed_at,'')=''`
	args = []any{string(keys)}
	if ids := eventFilterIDs(filter.ThreadID, filter.ThreadIDs); len(ids) > 0 {
		query += ` AND e.thread_id IN (` + placeholders(len(ids)) + `)`
		for _, id := range ids {
			args = append(args, id)
		}
	}
	if types := dedupeStrings(filter.Types); len(types) > 0 {
		query += ` AND e.type IN (` + placeholders(len(types)) + `)`
		for _, typ := range types {
			args = append(args, typ)
		}
	}
	predicate, legacyArgs := eventStreamLegacyPMPredicate(ctx)
	query += predicate
	args = append(args, legacyArgs...)
	query += ` ORDER BY anx_timestamp_key(e.ts),e.id`
	rows, err = s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return EventStreamPage{}, err
	}
	stored := []storedEventRow{}
	for rows.Next() {
		var row storedEventRow
		if err := rows.Scan(&row.id, &row.handle, &row.kind, &row.ts, &row.actor, &row.thread, &row.refs, &row.payload,
			&row.archivedAt, &row.archivedBy, &row.trashedAt, &row.trashedBy, &row.trashReason); err != nil {
			rows.Close()
			return EventStreamPage{}, err
		}
		stored = append(stored, row)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return EventStreamPage{}, err
	}
	values := []any{}
	for _, row := range stored {
		var refs, payload any
		if err := json.Unmarshal([]byte(row.refs), &refs); err != nil {
			return EventStreamPage{}, err
		}
		if err := json.Unmarshal([]byte(row.payload), &payload); err != nil {
			return EventStreamPage{}, err
		}
		values = append(values, map[string]any{"refs": refs, "thread_ref": "thread:" + row.thread.String}, payload)
	}
	ctx, err = withBatchPublicRefs(ctx, s.db, values)
	if err != nil {
		return EventStreamPage{}, err
	}
	for _, row := range stored {
		body, err := decodeEventBodyFromRow(ctx, s.db, row.id, row.handle, row.kind, row.ts, row.actor, row.thread, row.refs, row.payload)
		if err != nil {
			return EventStreamPage{}, err
		}
		overlayEventLifecycleFromSQLColumns(body, row.archivedAt, row.archivedBy, row.trashedAt, row.trashedBy, row.trashReason)
		page.Events = append(page.Events, body)
	}
	return page, nil
}

// Apply the same orphan-PM protection to payloads and client resume provenance.
// Canonical thread/ref inheritance is enforced by the scoped events relation.
func eventStreamLegacyPMPredicate(ctx context.Context) (string, []any) {
	if scope, ok := accessScopeFrom(ctx); ok && (scope.ActorID == "" || scope.ActorID != scope.PMActorID) {
		return ` AND (e.actor_id=? AND e.actor_id<>'' OR NOT (
			COALESCE(trim(json_extract(e.payload_json,'$.pm_conversation_id')),'')<>''
			OR COALESCE(json_type(e.payload_json,'$.payload.pm_turn_id'),'null')<>'null'
			OR COALESCE(json_type(e.payload_json,'$.payload.pm_execution'),'null')<>'null'
		) OR EXISTS (SELECT 1 FROM threads t WHERE t.id=e.thread_id
			OR t.id IN (SELECT substr(value,8) FROM json_each(e.refs_json) WHERE value LIKE 'thread:%')))`, []any{scope.ActorID}
	}
	return "", nil
}
