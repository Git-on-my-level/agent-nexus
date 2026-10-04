package primitives

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type HumanAttentionInboxPage struct {
	Items      []map[string]any
	NextCursor string
}

// ListHumanAttentionInboxAsksPage returns the authenticated requester's open
// and answered asks, with answer read state joined from its own durable table.
func (s *Store) ListHumanAttentionInboxAsksPage(ctx context.Context, requesterActorID, cursor string, limit int) (HumanAttentionInboxPage, error) {
	if s == nil || s.db == nil {
		return HumanAttentionInboxPage{}, fmt.Errorf("primitives store database is not initialized")
	}
	requesterActorID = strings.TrimSpace(requesterActorID)
	if requesterActorID == "" {
		return HumanAttentionInboxPage{}, fmt.Errorf("requester actor id is required")
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 200 {
		limit = 200
	}
	keyset, cursorTS, cursorID, _, err := parseEventListPageCursor(cursor)
	if err != nil || cursor != "" && !keyset {
		return HumanAttentionInboxPage{}, ErrInvalidCursor
	}
	query := `SELECT ask.id, COALESCE(ask.handle,''), ask.ts, ask.actor_id, ask.thread_id, ask.refs_json, ask.payload_json,
		response.id, COALESCE(response.handle,''), response.ts, response.actor_id, response.thread_id, response.refs_json, response.payload_json,
		read_state.answer_event_id
		FROM events AS ask
		LEFT JOIN human_attention_request_resolutions AS resolution ON resolution.request_event_id=ask.id
		LEFT JOIN events AS response ON response.id=resolution.resolution_event_id
			AND response.type='human_attention_responded' AND resolution.resolution_type='answered'
		LEFT JOIN human_attention_answer_reads AS read_state ON read_state.answer_event_id=response.id
			AND read_state.requester_actor_id=?
		WHERE ask.type='human_attention_requested'
		  AND trim(COALESCE(json_extract(ask.payload_json,'$.payload.requester_actor_id'),''))=?
		  AND COALESCE(ask.trashed_at,'')=''
		  AND (resolution.resolution_type IS NULL OR resolution.resolution_type='answered')`
	args := []any{requesterActorID, requesterActorID}
	if keyset {
		query += ` AND (ask.ts < ? OR (ask.ts = ? AND ask.id < ?))`
		args = append(args, cursorTS, cursorTS, cursorID)
	}
	query += ` ORDER BY ask.ts DESC, ask.id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return HumanAttentionInboxPage{}, fmt.Errorf("query human attention inbox asks: %w", err)
	}
	defer rows.Close()

	items := make([]map[string]any, 0, limit+1)
	for rows.Next() {
		var askID, askHandle, askTS, askActor, askRefsJSON, askPayloadJSON string
		var askThread sql.NullString
		var answerID, answerHandle, answerTS, answerActor, answerRefsJSON, answerPayloadJSON sql.NullString
		var answerThread, readAnswerID sql.NullString
		if err := rows.Scan(&askID, &askHandle, &askTS, &askActor, &askThread, &askRefsJSON, &askPayloadJSON,
			&answerID, &answerHandle, &answerTS, &answerActor, &answerThread, &answerRefsJSON, &answerPayloadJSON, &readAnswerID); err != nil {
			return HumanAttentionInboxPage{}, fmt.Errorf("scan human attention inbox ask: %w", err)
		}
		askEvent, err := decodeEventBodyFromRow(ctx, s.db, askID, askHandle, "human_attention_requested", askTS, askActor, askThread, askRefsJSON, askPayloadJSON)
		if err != nil {
			return HumanAttentionInboxPage{}, err
		}
		askPayload := asMapValue(askEvent["payload"])
		item := map[string]any{
			"ask_id":        "event:" + askID,
			"title":         askPayload["title"],
			"subject_ref":   askPayload["subject_ref"],
			"requested_at":  askTS,
			"status":        "open",
			"answer":        nil,
			"answer_unread": false,
		}
		if answerID.Valid {
			answerEvent, err := decodeEventBodyFromRow(ctx, s.db, answerID.String, answerHandle.String, "human_attention_responded", answerTS.String, answerActor.String, answerThread, answerRefsJSON.String, answerPayloadJSON.String)
			if err != nil {
				return HumanAttentionInboxPage{}, err
			}
			answerPayload := asMapValue(answerEvent["payload"])
			item["status"] = "answered"
			item["answer"] = map[string]any{
				"text": answerPayload["response_text"], "outcome": answerPayload["outcome"],
				"responder": answerPayload["responding_actor_id"], "at": answerTS.String,
				"response_event_id": answerID.String, "response_event_ref": "event:" + answerID.String,
			}
			item["answer_unread"] = !readAnswerID.Valid
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return HumanAttentionInboxPage{}, fmt.Errorf("iterate human attention inbox asks: %w", err)
	}
	page := HumanAttentionInboxPage{Items: items}
	if len(items) > limit {
		last := items[limit-1]
		askID := strings.TrimPrefix(anyStringValue(last["ask_id"]), "event:")
		page.NextCursor = encodeEventKeysetCursor(anyStringValue(last["requested_at"]), askID)
		page.Items = items[:limit]
	}
	return page, nil
}

// MarkHumanAttentionAnswerRead persists read state for one response event.
// The event must be an answer to an ask owned by requesterActorID.
func (s *Store) MarkHumanAttentionAnswerRead(ctx context.Context, requesterActorID, answerEventID string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	requesterActorID = strings.TrimSpace(requesterActorID)
	answerEventID = strings.TrimPrefix(strings.TrimSpace(answerEventID), "event:")
	if requesterActorID == "" || answerEventID == "" {
		return nil, fmt.Errorf("requester actor id and answer event id are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin answer read transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var existingReadAt string
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(read_state.read_at,'')
		FROM events AS answer
		JOIN human_attention_request_resolutions AS resolution ON resolution.resolution_event_id=answer.id
			AND resolution.resolution_type='answered'
		LEFT JOIN human_attention_answer_reads AS read_state ON read_state.answer_event_id=answer.id
			AND read_state.requester_actor_id=?
		WHERE answer.id=? AND answer.type='human_attention_responded'
		  AND trim(COALESCE(json_extract(answer.payload_json,'$.payload.requester_actor_id'),''))=?`,
		requesterActorID, answerEventID, requesterActorID).Scan(&existingReadAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load human attention answer read state: %w", err)
	}
	if existingReadAt != "" {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit answer read transaction: %w", err)
		}
		return map[string]any{"answer_event_id": answerEventID, "read": true, "already_read": true, "read_at": existingReadAt}, nil
	}
	readAt := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO human_attention_answer_reads(answer_event_id,requester_actor_id,read_at) VALUES(?,?,?)`, answerEventID, requesterActorID, readAt); err != nil {
		return nil, fmt.Errorf("mark human attention answer read: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT read_at FROM human_attention_answer_reads WHERE answer_event_id=? AND requester_actor_id=?`, answerEventID, requesterActorID).Scan(&readAt); err != nil {
		return nil, fmt.Errorf("load marked answer read state: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit answer read transaction: %w", err)
	}
	return map[string]any{"answer_event_id": answerEventID, "read": true, "already_read": false, "read_at": readAt}, nil
}

func asMapValue(value any) map[string]any {
	if result, ok := value.(map[string]any); ok && result != nil {
		return result
	}
	return map[string]any{}
}
