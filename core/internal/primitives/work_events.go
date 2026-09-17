package primitives

import (
	"context"
	"database/sql"
	"time"
)

// insertWorkEvent shares the work transaction: mutations cannot lose their
// event history on restart. Routine unchanged polls deliberately do not call it.
func insertWorkEvent(ctx context.Context, tx *sql.Tx, actor string, card map[string]any, eventType, summary string, payload map[string]any) error {
	id := workString(card["id"])
	threadID := workString(card["thread_id"])
	boardID := workString(card["board_id"])
	payload["card_id"] = id
	payload["board_id"] = boardID
	event := map[string]any{"type": eventType, "thread_id": threadID, "summary": summary, "refs": []string{"card:" + id, "board:" + boardID, "thread:" + threadID}, "payload": payload}
	prepared, err := prepareEventForInsert(actor, event)
	if err != nil {
		return err
	}
	if err = insertPreparedEvent(ctx, tx, prepared); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = tx.ExecContext(ctx, `INSERT INTO derived_topic_dirty_queue(thread_id,dirty_at) VALUES(?,?) ON CONFLICT(thread_id) DO UPDATE SET dirty_at=MIN(dirty_at,excluded.dirty_at)`, threadID, now); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO topic_projection_refresh_status(thread_id,desired_generation,materialized_generation,queued_at,updated_at) VALUES(?,1,0,?,?) ON CONFLICT(thread_id) DO UPDATE SET desired_generation=desired_generation+1,queued_at=COALESCE(queued_at,excluded.queued_at),updated_at=excluded.updated_at`, threadID, now, now)
	return err
}
