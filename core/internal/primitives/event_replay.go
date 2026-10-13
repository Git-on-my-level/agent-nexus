package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// AppendEventWithReplay commits an ordinary event and its original HTTP result
// together. The replay key is scoped to the authenticated actor, not an event ID
// supplied by the caller. Existing events without a replay are never claimed.
func (s *Store) AppendEventWithReplay(ctx context.Context, actorID, key, hash string, event, responseExtras map[string]any) (IdempotencyReplay, bool, error) {
	if key == "" || hash == "" {
		return IdempotencyReplay{}, false, fmt.Errorf("event replay key and hash are required")
	}
	if s.quota.enabled() {
		s.quotaMu.Lock()
		defer s.quotaMu.Unlock()
	}
	prepared, err := prepareEventForInsert(actorID, event)
	if err != nil {
		return IdempotencyReplay{}, false, err
	}
	if err := s.checkWorkspaceWriteQuota(ctx, 0, quotaWriteDelta{dbBytes: int64(len(prepared.PayloadJSON) + len(prepared.RefsJSON) + 512)}, blobLedgerWritePlan{}); err != nil {
		return IdempotencyReplay{}, false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return IdempotencyReplay{}, false, err
	}
	defer tx.Rollback()
	var replay IdempotencyReplay
	var raw string
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_status,response_json FROM idempotency_replays WHERE scope='events.create' AND actor_id=? AND request_key=?`, actorID, key).Scan(&replay.RequestHash, &replay.Status, &raw)
	if err == nil {
		if replay.RequestHash != hash {
			return IdempotencyReplay{}, false, ErrConflict
		}
		if err := json.Unmarshal([]byte(raw), &replay.Response); err != nil {
			return IdempotencyReplay{}, false, err
		}
		return replay, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return IdempotencyReplay{}, false, err
	}
	if err := insertPreparedEvent(ctx, tx, prepared); err != nil {
		return IdempotencyReplay{}, false, err
	}
	response := cloneMap(responseExtras)
	response["event"] = prepared.Body
	encoded, err := json.Marshal(response)
	if err != nil {
		return IdempotencyReplay{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO idempotency_replays(scope,actor_id,request_key,request_hash,response_status,response_json) VALUES('events.create',?,?,?,201,?)`, actorID, key, hash, string(encoded)); err != nil {
		return IdempotencyReplay{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return IdempotencyReplay{}, false, err
	}
	return IdempotencyReplay{RequestHash: hash, Status: 201, Response: response}, false, nil
}
