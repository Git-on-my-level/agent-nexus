// Event list queries: filtered pages and keyset cursors, per-thread reads,
// and backing-scope filtering of event types.

package primitives

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type EventListFilter struct {
	ReportEventIDs []string
	// ReportSubjects filters payload subjects and related refs before report budgets.
	ReportSubjects bool
	Types          []string
	BackingScope   string
	Preset         string
	TopicID        string
	TopicIDs       []string
	ThreadID       string
	ThreadIDs      []string
	ActorID        string
	ActorIDs       []string
	ActorKind      string
	Query          string
	Since          string
	// SinceExclusiveID, when set together with Since, restricts to rows strictly after the (Since, SinceExclusiveID) tuple (ts DESC pagination / read-cursor lower bounds).
	SinceExclusiveID string
	Until            string
	Limit            int
	Cursor           string
}

type EventPage struct {
	Events     []map[string]any
	NextCursor string
}

type EventCursor struct {
	TS string
	ID string
}

var BackingEventTypes = []string{
	"document_created",
	"document_revised",
	"document_restored",
	"document_trashed",
	"card_created",
	"card_updated",
	"card_moved",
	"card_archived",
	"card_trashed",
	"card_resolved",
}

var BackingArtifactKinds = []string{"doc", "card"}

func (s *Store) ListEvents(ctx context.Context, filter EventListFilter) ([]map[string]any, error) {
	// State-derivation and replay callers use ListEvents without a limit and expect every
	// matching row. HTTP and UI pagination use ListEventsPage (or ListEvents with Limit set).
	if filter.Limit > 0 {
		page, err := s.ListEventsPage(ctx, filter)
		if err != nil {
			return nil, err
		}
		return page.Events, nil
	}
	const pageSize = 200
	var out []map[string]any
	cursor := strings.TrimSpace(filter.Cursor)
	for {
		f := filter
		f.Limit = pageSize
		f.Cursor = cursor
		page, err := s.ListEventsPage(ctx, f)
		if err != nil {
			return nil, err
		}
		out = append(out, page.Events...)
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	return out, nil
}

func (s *Store) ListEventsPage(ctx context.Context, filter EventListFilter) (EventPage, error) {
	if s == nil || s.db == nil {
		return EventPage{}, fmt.Errorf("primitives store database is not initialized")
	}

	query := `SELECT id, COALESCE(handle, ''), type, ts, actor_id, thread_id, refs_json, payload_json,
		archived_at, archived_by, trashed_at, trashed_by, trash_reason
		FROM events
		WHERE COALESCE(trashed_at, '') = ''`
	if filter.ReportSubjects {
		query += ` AND ` + reportEventLifecycleSQL(ctx)
	}
	args := make([]any, 0)
	if filter.ReportEventIDs != nil {
		encoded, _ := json.Marshal(filter.ReportEventIDs)
		query += ` AND id IN (SELECT value FROM json_each(?))`
		args = append(args, string(encoded))
	}
	filterTypes := filter.Types
	explicitTypeFilter := len(dedupeStrings(filter.Types)) > 0
	if strings.EqualFold(strings.TrimSpace(filter.Preset), "home_feed") {
		explicit := dedupeStrings(filter.Types)
		if len(explicit) > 0 {
			allowed := map[string]struct{}{}
			for _, eventType := range HomeFeedEventTypes {
				allowed[eventType] = struct{}{}
			}
			intersect := make([]string, 0, len(explicit))
			for _, eventType := range explicit {
				if _, ok := allowed[eventType]; ok {
					intersect = append(intersect, eventType)
				}
			}
			filterTypes = intersect
		} else {
			filterTypes = HomeFeedEventTypes
		}
	}
	filterTypes, excludeBacking := applyEventBackingScope(filterTypes, filter.BackingScope)

	if explicitTypeFilter && len(filterTypes) == 0 {
		query += ` AND 1=0`
	}
	if len(filterTypes) > 0 {
		placeholders := make([]string, 0, len(filterTypes))
		for _, eventType := range filterTypes {
			placeholders = append(placeholders, "?")
			args = append(args, eventType)
		}
		query += ` AND type IN (` + strings.Join(placeholders, ",") + `)`
	}
	if excludeBacking {
		placeholders := make([]string, 0, len(BackingEventTypes))
		for _, eventType := range BackingEventTypes {
			placeholders = append(placeholders, "?")
			args = append(args, eventType)
		}
		query += ` AND type NOT IN (` + strings.Join(placeholders, ",") + `)`
	}
	if threadIDs := eventFilterIDs(filter.ThreadID, filter.ThreadIDs); len(threadIDs) > 0 {
		query += ` AND thread_id IN (` + placeholders(len(threadIDs)) + `)`
		for _, threadID := range threadIDs {
			args = append(args, threadID)
		}
	}
	if topicIDs := eventFilterIDs(filter.TopicID, filter.TopicIDs); len(topicIDs) > 0 {
		query += ` AND EXISTS (
			SELECT 1 FROM json_each(events.refs_json)
			WHERE json_each.value IN (` + placeholders(len(topicIDs)) + `)
		)`
		for _, topicID := range topicIDs {
			args = append(args, "topic:"+topicID)
		}
	}
	if actorID := strings.TrimSpace(filter.ActorID); actorID != "" {
		query += ` AND actor_id = ?`
		args = append(args, actorID)
	}
	if len(filter.ActorIDs) > 0 {
		ids := dedupeStrings(filter.ActorIDs)
		if len(ids) > 0 {
			placeholders := make([]string, 0, len(ids))
			for _, actorID := range ids {
				placeholders = append(placeholders, "?")
				args = append(args, actorID)
			}
			query += ` AND actor_id IN (` + strings.Join(placeholders, ",") + `)`
		}
	}
	if since := strings.TrimSpace(filter.Since); since != "" {
		if sid := strings.TrimSpace(filter.SinceExclusiveID); sid != "" {
			query += ` AND (ts > ? OR (ts = ? AND id > ?))`
			args = append(args, since, since, sid)
		} else {
			query += ` AND ts >= ?`
			args = append(args, since)
		}
	}
	if until := strings.TrimSpace(filter.Until); until != "" {
		query += ` AND ts <= ?`
		args = append(args, until)
	}
	if q := strings.TrimSpace(filter.Query); q != "" {
		like := "%" + strings.ToLower(q) + "%"
		query += ` AND (
			lower(id) LIKE ? OR lower(type) LIKE ? OR lower(actor_id) LIKE ? OR
			lower(COALESCE(thread_id, '')) LIKE ? OR lower(refs_json) LIKE ? OR
			lower(payload_json) LIKE ?
		)`
		args = append(args, like, like, like, like, like, like)
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	keyset, keyTS, keyID, offset, err := parseEventListPageCursor(strings.TrimSpace(filter.Cursor))
	if err != nil {
		return EventPage{}, fmt.Errorf("%w: %v", ErrInvalidCursor, err)
	}
	if keyset {
		query += ` AND ((ts < ?) OR (ts = ? AND id < ?))`
		args = append(args, keyTS, keyTS, keyID)
	}
	query += ` ORDER BY ts DESC, id DESC`
	if keyset {
		query += ` LIMIT ?`
		args = append(args, limit+1)
	} else {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, limit+1, offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return EventPage{}, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	stored := []storedEventRow{}
	for rows.Next() {
		var row storedEventRow
		if err := rows.Scan(&row.id, &row.handle, &row.kind, &row.ts, &row.actor, &row.thread, &row.refs, &row.payload, &row.archivedAt, &row.archivedBy, &row.trashedAt, &row.trashedBy, &row.trashReason); err != nil {
			rows.Close()
			return EventPage{}, err
		}
		stored = append(stored, row)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return EventPage{}, err
	}
	// Release the collection cursor before reference hydration (including on
	// single-connection imports), then resolve only this page's reference IDs.
	values := []any{}
	for _, row := range stored {
		var refs, payload any
		if err := json.Unmarshal([]byte(row.refs), &refs); err != nil {
			return EventPage{}, err
		}
		if err := json.Unmarshal([]byte(row.payload), &payload); err != nil {
			return EventPage{}, err
		}
		values = append(values, map[string]any{"refs": refs, "thread_ref": "thread:" + row.thread.String, "payload": payload})
	}
	ctx, err = withBatchPublicRefs(ctx, s.db, values)
	if err != nil {
		return EventPage{}, err
	}
	events := make([]map[string]any, 0, len(stored))
	for _, row := range stored {
		body, err := decodeEventBodyFromRow(ctx, s.db, row.id, row.handle, row.kind, row.ts, row.actor, row.thread, row.refs, row.payload)
		if err != nil {
			return EventPage{}, err
		}
		overlayEventLifecycleFromSQLColumns(body, row.archivedAt, row.archivedBy, row.trashedAt, row.trashedBy, row.trashReason)
		events = append(events, body)
	}

	nextCursor := ""
	if len(events) > limit {
		events = events[:limit]
		last := events[len(events)-1]
		nextCursor = encodeEventKeysetCursor(anyStringValue(last["ts"]), anyStringValue(last["id"]))
	}
	return EventPage{Events: events, NextCursor: nextCursor}, nil
}

func (s *Store) ListEventsByThread(ctx context.Context, threadID string) ([]map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, COALESCE(handle, ''), type, ts, actor_id, thread_id, refs_json, payload_json,
			archived_at, archived_by, trashed_at, trashed_by, trash_reason
		 FROM events
		 WHERE thread_id = ?
		 ORDER BY ts ASC, id ASC`,
		threadID,
	)
	if err != nil {
		return nil, fmt.Errorf("query thread events: %w", err)
	}
	defer rows.Close()

	events := make([]map[string]any, 0)
	for rows.Next() {
		var (
			eventID     string
			eventHandle string
			typeValue   string
			ts          string
			actorID     string
			thread      sql.NullString
			refsJSON    string
			payloadJSON string
			archivedAt  sql.NullString
			archivedBy  sql.NullString
			trashedAt   sql.NullString
			trashedBy   sql.NullString
			trashReason sql.NullString
		)
		if err := rows.Scan(&eventID, &eventHandle, &typeValue, &ts, &actorID, &thread, &refsJSON, &payloadJSON,
			&archivedAt, &archivedBy, &trashedAt, &trashedBy, &trashReason); err != nil {
			return nil, fmt.Errorf("scan thread event: %w", err)
		}

		body, err := decodeEventBodyFromRow(ctx, s.db, eventID, eventHandle, typeValue, ts, actorID, thread, refsJSON, payloadJSON)
		if err != nil {
			return nil, err
		}
		overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, trashedAt, trashedBy, trashReason)
		events = append(events, body)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate thread events: %w", err)
	}

	return events, nil
}

// BatchCountMessagePostedEventsByThreadIDs counts timeline message_posted events per backing thread.
// Rows with a non-null trashed_at are excluded. Every requested distinct thread ID gets a map entry,
// zero when no qualifying events exist or when thread_id matches no rows (including empty thread IDs).
func (s *Store) BatchCountMessagePostedEventsByThreadIDs(ctx context.Context, threadIDs []string) (map[string]int, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}

	uniq := make([]string, 0, len(threadIDs))
	seen := make(map[string]struct{}, len(threadIDs))
	for _, raw := range threadIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}

	out := make(map[string]int, len(uniq)+1)
	for _, id := range uniq {
		out[id] = 0
	}
	if len(uniq) == 0 {
		return out, nil
	}

	ph := strings.Repeat("?,", len(uniq))
	ph = ph[:len(ph)-1]
	args := make([]any, 0, len(uniq)+1)
	args = append(args, "message_posted")
	for _, id := range uniq {
		args = append(args, id)
	}

	q := fmt.Sprintf(
		`SELECT trim(COALESCE(thread_id,'')) AS tid, COUNT(*) FROM events
			WHERE type = ?
			  AND COALESCE(trim(thread_id),'') <> ''
			  AND COALESCE(trim(trashed_at),'') = ''
			  AND trim(COALESCE(thread_id,'')) IN (%s)
			GROUP BY tid`,
		ph,
	)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("batch count message_posted events: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var tid string
		var n int64
		if err := rows.Scan(&tid, &n); err != nil {
			return nil, fmt.Errorf("scan message_posted counts: %w", err)
		}
		key := strings.TrimSpace(tid)
		if key == "" {
			continue
		}
		if _, ok := out[key]; !ok {
			continue
		}
		out[key] = int(n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate message_posted counts: %w", err)
	}

	return out, nil
}

func (s *Store) ListRecentEventsByThread(ctx context.Context, threadID string, limit int) ([]map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	if limit <= 0 {
		return []map[string]any{}, nil
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, COALESCE(handle, ''), type, ts, actor_id, thread_id, refs_json, payload_json,
			archived_at, archived_by, trashed_at, trashed_by, trash_reason
		 FROM events
		 WHERE thread_id = ?
		 ORDER BY ts DESC, id DESC
		 LIMIT ?`,
		threadID,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query recent thread events: %w", err)
	}
	defer rows.Close()

	recentDescending := make([]map[string]any, 0, limit)
	for rows.Next() {
		var (
			eventID     string
			eventHandle string
			typeValue   string
			ts          string
			actorID     string
			thread      sql.NullString
			refsJSON    string
			payloadJSON string
			archivedAt  sql.NullString
			archivedBy  sql.NullString
			trashedAt   sql.NullString
			trashedBy   sql.NullString
			trashReason sql.NullString
		)
		if err := rows.Scan(&eventID, &eventHandle, &typeValue, &ts, &actorID, &thread, &refsJSON, &payloadJSON,
			&archivedAt, &archivedBy, &trashedAt, &trashedBy, &trashReason); err != nil {
			return nil, fmt.Errorf("scan recent thread event: %w", err)
		}

		body, err := decodeEventBodyFromRow(ctx, s.db, eventID, eventHandle, typeValue, ts, actorID, thread, refsJSON, payloadJSON)
		if err != nil {
			return nil, err
		}
		overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, trashedAt, trashedBy, trashReason)
		recentDescending = append(recentDescending, body)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent thread events: %w", err)
	}

	return reverseEvents(recentDescending), nil
}

func reverseEvents(events []map[string]any) []map[string]any {
	if len(events) <= 1 {
		return events
	}
	for left, right := 0, len(events)-1; left < right; left, right = left+1, right-1 {
		events[left], events[right] = events[right], events[left]
	}
	return events
}

func (s *Store) ListEventsAfter(ctx context.Context, filter EventListFilter, cursor EventCursor, limit int) ([]map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	if limit <= 0 {
		limit = 100
	}

	query := `SELECT id, COALESCE(handle, ''), type, ts, actor_id, thread_id, refs_json, payload_json,
		archived_at, archived_by, trashed_at, trashed_by, trash_reason
		FROM events
		WHERE 1=1`
	args := make([]any, 0, len(filter.Types)+3)

	explicitTypeFilter := len(dedupeStrings(filter.Types)) > 0
	filterTypes, excludeBacking := applyEventBackingScope(filter.Types, filter.BackingScope)
	if explicitTypeFilter && len(filterTypes) == 0 {
		query += ` AND 1=0`
	}
	if len(filterTypes) > 0 {
		placeholders := make([]string, 0, len(filter.Types))
		for _, eventType := range filterTypes {
			placeholders = append(placeholders, "?")
			args = append(args, eventType)
		}
		query += ` AND type IN (` + strings.Join(placeholders, ",") + `)`
	}
	if excludeBacking {
		placeholders := make([]string, 0, len(BackingEventTypes))
		for _, eventType := range BackingEventTypes {
			placeholders = append(placeholders, "?")
			args = append(args, eventType)
		}
		query += ` AND type NOT IN (` + strings.Join(placeholders, ",") + `)`
	}
	if strings.TrimSpace(cursor.TS) != "" {
		query += ` AND (julianday(ts) > julianday(?) OR (julianday(ts) = julianday(?) AND id > ?))`
		args = append(args, strings.TrimSpace(cursor.TS), strings.TrimSpace(cursor.TS), strings.TrimSpace(cursor.ID))
	}
	query += ` ORDER BY julianday(ts) ASC, id ASC LIMIT ?`
	args = append(args, limit)

	// The background router selects one type. Seek its canonical position
	// index before loading payloads, so even an empty tail avoids a history scan.
	_, routerScoped := accessScopeFrom(ctx)
	if !routerScoped && len(filterTypes) == 1 && filterTypes[0] == "message_posted" && !excludeBacking {
		query, args = routerEventsAfterSQL(cursor, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query events after cursor: %w", err)
	}
	defer rows.Close()

	events := make([]map[string]any, 0)
	for rows.Next() {
		var (
			eventID     string
			eventHandle string
			typeValue   string
			ts          string
			actorID     string
			thread      sql.NullString
			refsJSON    string
			payloadJSON string
			archivedAt  sql.NullString
			archivedBy  sql.NullString
			trashedAt   sql.NullString
			trashedBy   sql.NullString
			trashReason sql.NullString
		)
		if err := rows.Scan(&eventID, &eventHandle, &typeValue, &ts, &actorID, &thread, &refsJSON, &payloadJSON,
			&archivedAt, &archivedBy, &trashedAt, &trashedBy, &trashReason); err != nil {
			return nil, fmt.Errorf("scan event after cursor: %w", err)
		}

		body, err := decodeEventBodyFromRow(ctx, s.db, eventID, eventHandle, typeValue, ts, actorID, thread, refsJSON, payloadJSON)
		if err != nil {
			return nil, err
		}
		overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, trashedAt, trashedBy, trashReason)
		events = append(events, body)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events after cursor: %w", err)
	}
	return events, nil
}

func encodeEventKeysetCursor(ts, id string) string {
	ts = strings.TrimSpace(ts)
	id = strings.TrimSpace(id)
	if ts == "" || id == "" {
		return ""
	}
	payload, err := json.Marshal(map[string]string{"ts": ts, "id": id})
	if err != nil {
		return ""
	}
	inner := append([]byte("evk:"), payload...)
	return base64.StdEncoding.EncodeToString(inner)
}

// parseEventListPageCursor decodes ListEventsPage cursors: keyset (evk:...) or legacy offset cursors.
func parseEventListPageCursor(cursor string) (keyset bool, ts, id string, offset int, err error) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return false, "", "", 0, nil
	}
	raw, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return false, "", "", 0, fmt.Errorf("invalid cursor encoding: %w", err)
	}
	s := string(raw)
	if strings.HasPrefix(s, "evk:") {
		var obj struct {
			TS string `json:"ts"`
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(s, "evk:")), &obj); err != nil {
			return false, "", "", 0, fmt.Errorf("invalid events keyset cursor: %w", err)
		}
		if strings.TrimSpace(obj.TS) == "" || strings.TrimSpace(obj.ID) == "" {
			return false, "", "", 0, fmt.Errorf("invalid events keyset cursor: empty ts or id")
		}
		return true, obj.TS, obj.ID, 0, nil
	}
	parts := strings.Split(s, ":")
	if len(parts) != 2 || parts[0] != "offset" {
		return false, "", "", 0, fmt.Errorf("invalid cursor format")
	}
	off, err := strconv.Atoi(parts[1])
	if err != nil {
		return false, "", "", 0, fmt.Errorf("invalid cursor offset: %w", err)
	}
	if off <= 0 {
		return false, "", "", 0, fmt.Errorf("invalid cursor offset: must be greater than zero")
	}
	return false, "", "", off, nil
}

func eventFilterIDs(primary string, alternatives []string) []string {
	values := make([]string, 0, len(alternatives)+1)
	if primary = strings.TrimSpace(primary); primary != "" {
		values = append(values, primary)
	}
	values = append(values, alternatives...)
	return dedupeStrings(values)
}

func applyEventBackingScope(types []string, scope string) ([]string, bool) {
	scope = normalizeBackingScope(scope)
	switch scope {
	case "backing_only":
		if len(types) == 0 {
			return BackingEventTypes, false
		}
		allowed := map[string]struct{}{}
		for _, eventType := range BackingEventTypes {
			allowed[eventType] = struct{}{}
		}
		out := make([]string, 0, len(types))
		for _, eventType := range dedupeStrings(types) {
			if _, ok := allowed[eventType]; ok {
				out = append(out, eventType)
			}
		}
		return out, false
	case "standalone":
		if len(types) == 0 {
			return types, true
		}
		blocked := map[string]struct{}{}
		for _, eventType := range BackingEventTypes {
			blocked[eventType] = struct{}{}
		}
		out := make([]string, 0, len(types))
		for _, eventType := range dedupeStrings(types) {
			if _, ok := blocked[eventType]; !ok {
				out = append(out, eventType)
			}
		}
		return out, false
	default:
		return types, false
	}
}
