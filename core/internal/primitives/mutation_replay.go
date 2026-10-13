package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"agent-nexus-core/internal/resourceaccess"
)

// ExecuteWithReplay atomically commits an audited mutation and its public result.
// Nested primitive transactions remain scoped savepoints, including default-board
// creation, revision content and projection maintenance. No legacy ID adoption.
func (s *Store) ExecuteWithReplay(ctx context.Context, scope, actor, key, hash string, status int, run func(context.Context) (map[string]any, error)) (IdempotencyReplay, error) {
	if s.quota.enabled() {
		s.quotaMu.Lock()
		defer s.quotaMu.Unlock()
		ctx = context.WithValue(ctx, replayQuotaKey{}, s)
	}
	var out IdempotencyReplay
	err := s.db.WriteTransaction(ctx, func(ctx context.Context, tx *resourceaccess.Tx) error {
		// Establish a write snapshot before reading a replay, avoiding read-upgrade
		// contention among simultaneous exact retries.
		if _, err := tx.ExecContext(ctx, `UPDATE idempotency_replays SET request_hash=request_hash WHERE scope=? AND actor_id=? AND request_key=?`, scope, actor, key); err != nil {
			return err
		}
		var raw string
		err := tx.QueryRowContext(ctx, `SELECT request_hash,response_status,response_json FROM idempotency_replays WHERE scope=? AND actor_id=? AND request_key=?`, scope, actor, key).Scan(&out.RequestHash, &out.Status, &raw)
		if err == nil {
			if out.RequestHash != hash {
				return ErrConflict
			}
			return json.Unmarshal([]byte(raw), &out.Response)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		response, err := run(ctx)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(response)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO idempotency_replays(scope,actor_id,request_key,request_hash,response_status,response_json) VALUES(?,?,?,?,?,?)`, scope, actor, key, hash, status, string(encoded)); err != nil {
			return err
		}
		out = IdempotencyReplay{RequestHash: hash, Status: status, Response: response}
		return nil
	})
	return out, err
}

type replayQuotaKey struct{}

func replayOwnsQuota(ctx context.Context, s *Store) bool { return ctx.Value(replayQuotaKey{}) == s }
