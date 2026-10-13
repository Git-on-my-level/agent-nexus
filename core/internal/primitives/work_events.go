package primitives

import (
	"context"
)

// insertWorkEvent shares the work transaction: mutations cannot lose their
// event history on restart. Routine unchanged polls deliberately do not call it.
func insertWorkEvent(ctx context.Context, tx *accessTx, actor string, card map[string]any, eventType, summary string, payload map[string]any) error {
	_, err := insertWorkEventRef(ctx, tx, actor, card, eventType, summary, payload)
	return err
}

func insertWorkEventRef(ctx context.Context, tx *accessTx, actor string, card map[string]any, eventType, summary string, payload map[string]any) (string, error) {
	id := workString(card["id"])
	threadID := workString(card["thread_id"])
	boardID := workString(card["board_id"])
	payload["card_id"] = id
	payload["board_id"] = boardID
	event := map[string]any{"type": eventType, "thread_id": threadID, "summary": summary, "refs": []string{"card:" + id, "board:" + boardID, "thread:" + threadID}, "payload": payload}
	prepared, err := prepareEventForInsert(actor, event)
	if err != nil {
		return "", err
	}
	if err = insertPreparedEvent(ctx, tx, prepared); err != nil {
		return "", err
	}
	return workString(prepared.Body["ref"]), nil
}
