// Paginated listing of human_attention_responded events for the human
// attention surfaces.

package primitives

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// HumanAttentionRespondedPageParams configures newest-first pagination over human_attention_responded events.
type HumanAttentionRespondedPageParams struct {
	CursorTS string
	CursorID string
	Limit    int

	// KindFilter limits rows by payload.kind: "", "ask", "review", "escalate", or "unknown".
	KindFilter string

	// SinceRFC3339Nano, when non-empty, restricts to events with ts >= this value (30-day window, etc.).
	SinceRFC3339Nano string
}

const humanAttentionRespondedEventType = "human_attention_responded"

// ListHumanAttentionRespondedPage returns up to Limit human_attention_responded events ordered newest first.
// CursorTS/CursorID describe the last row from the previous page (exclusive): rows strictly older than that tuple are returned.
func (s *Store) ListHumanAttentionRespondedPage(ctx context.Context, params HumanAttentionRespondedPageParams) ([]map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	query := `SELECT id, COALESCE(handle, ''), type, ts, actor_id, thread_id, refs_json, payload_json,
		archived_at, archived_by, trashed_at, trashed_by, trash_reason
		FROM events
		WHERE type = ? AND COALESCE(trashed_at, '') = ''`
	args := []any{humanAttentionRespondedEventType}

	if ts := strings.TrimSpace(params.SinceRFC3339Nano); ts != "" {
		query += ` AND ts >= ?`
		args = append(args, ts)
	}

	kind := strings.TrimSpace(strings.ToLower(params.KindFilter))
	switch kind {
	case "", "all":
	case "unknown":
		query += ` AND (json_extract(payload_json, '$.payload.kind') IS NULL OR json_extract(payload_json, '$.payload.kind') NOT IN ('ask','review','escalate'))`
	default:
		query += ` AND lower(trim(json_extract(payload_json, '$.payload.kind'))) = ?`
		args = append(args, kind)
	}

	cursorTS := strings.TrimSpace(params.CursorTS)
	cursorID := strings.TrimSpace(params.CursorID)
	if cursorTS != "" && cursorID != "" {
		query += ` AND ((ts < ?) OR (ts = ? AND id < ?))`
		args = append(args, cursorTS, cursorTS, cursorID)
	}

	query += ` ORDER BY ts DESC, id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query human_attention_responded page: %w", err)
	}
	defer rows.Close()

	stored := []storedEventRow{}
	for rows.Next() {
		var row storedEventRow
		if err := rows.Scan(&row.id, &row.handle, &row.kind, &row.ts, &row.actor, &row.thread, &row.refs, &row.payload,
			&row.archivedAt, &row.archivedBy, &row.trashedAt, &row.trashedBy, &row.trashReason); err != nil {
			return nil, fmt.Errorf("scan human_attention_responded row: %w", err)
		}
		stored = append(stored, row)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, fmt.Errorf("iterate human_attention_responded page: %w", err)
	}
	values := make([]any, 0, len(stored)*2)
	for _, row := range stored {
		var refs, payload any
		if err := json.Unmarshal([]byte(row.refs), &refs); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(row.payload), &payload); err != nil {
			return nil, err
		}
		values = append(values, map[string]any{"refs": refs, "thread_ref": "thread:" + row.thread.String}, payload)
	}
	ctx, err = withBatchPublicRefs(ctx, s.db, values)
	if err != nil {
		return nil, err
	}
	events := make([]map[string]any, 0, len(stored))
	for _, row := range stored {
		body, err := decodeEventBodyFromRow(ctx, s.db, row.id, row.handle, row.kind, row.ts, row.actor, row.thread, row.refs, row.payload)
		if err != nil {
			return nil, err
		}
		overlayEventLifecycleFromSQLColumns(body, row.archivedAt, row.archivedBy, row.trashedAt, row.trashedBy, row.trashReason)
		events = append(events, body)
	}

	return events, nil
}
