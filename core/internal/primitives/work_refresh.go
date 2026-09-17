package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ClaimWorkRefresh acquires a durable, expiring lease. Lease tokens fence stale
// workers after a restart or timeout. RequestWorkRefresh coalesces active jobs.
func (s *Store) ClaimWorkRefresh(ctx context.Context, identifier, worker string, ttl time.Duration) (map[string]any, error) {
	if worker == "" || ttl < time.Second || ttl > 10*time.Minute {
		return nil, workInvalid("worker and lease TTL 1s..10m required")
	}
	card, err := s.getWorkCard(ctx, identifier)
	if err != nil {
		return nil, err
	}
	id := workString(card["id"])
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE work_metadata SET card_id=card_id WHERE card_id=?`, id); err != nil {
		return nil, err
	}
	var raw string
	if err = tx.QueryRowContext(ctx, `SELECT refresh_json FROM work_metadata WHERE card_id=?`, id).Scan(&raw); err != nil {
		return nil, err
	}
	var refresh map[string]any
	if err = json.Unmarshal([]byte(raw), &refresh); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	state := workString(refresh["state"])
	if state == "running" {
		expires, err := workTimestamp(refresh["lease_expires_at"])
		if err == nil && expires.After(now) {
			return nil, ErrConflict
		}
	} else if state != "queued" {
		return nil, ErrConflict
	}
	refresh["state"] = "running"
	refresh["lease_token"] = uuid.NewString()
	refresh["lease_owner"] = worker
	refresh["lease_expires_at"] = now.Add(ttl).Format(time.RFC3339Nano)
	refresh["last_attempt_at"] = now.Format(time.RFC3339Nano)
	if _, err = tx.ExecContext(ctx, `UPDATE work_metadata SET refresh_json=? WHERE card_id=?`, workJSON(refresh), id); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return refresh, nil
}

// FinishWorkRefresh releases only the caller's live lease. A success requires a
// persisted good observation produced after this attempt began.
func (s *Store) FinishWorkRefresh(ctx context.Context, identifier, token string, result map[string]any) (map[string]any, error) {
	state := workString(result["state"])
	if token == "" || (state != "failed" && state != "succeeded") {
		return nil, workInvalid("lease token and terminal refresh state required")
	}
	if next := workString(result["next_due_at"]); next != "" {
		if _, err := workTimestamp(next); err != nil {
			return nil, workInvalid("next_due_at must be RFC3339")
		}
	}
	card, err := s.getWorkCard(ctx, identifier)
	if err != nil {
		return nil, err
	}
	id := workString(card["id"])
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE work_metadata SET card_id=card_id WHERE card_id=?`, id); err != nil {
		return nil, err
	}
	var raw string
	if err = tx.QueryRowContext(ctx, `SELECT refresh_json FROM work_metadata WHERE card_id=?`, id).Scan(&raw); err != nil {
		return nil, err
	}
	var refresh map[string]any
	if err = json.Unmarshal([]byte(raw), &refresh); err != nil {
		return nil, err
	}
	expires, e := workTimestamp(refresh["lease_expires_at"])
	if workString(refresh["lease_token"]) != token || refresh["state"] != "running" || e != nil || !expires.After(time.Now()) {
		return nil, ErrConflict
	}
	if state == "succeeded" {
		var received sql.NullString
		if err = tx.QueryRowContext(ctx, `SELECT o.received_at FROM work_metadata w LEFT JOIN work_observations o ON o.id=w.latest_observation_id WHERE w.card_id=?`, id).Scan(&received); err != nil {
			return nil, err
		}
		stamp, e := workTimestamp(received.String)
		attempt, e2 := workTimestamp(refresh["last_attempt_at"])
		if e != nil || e2 != nil || stamp.Before(attempt) {
			return nil, workInvalid("successful refresh requires a persisted observation for this attempt")
		}
		refresh["last_success_at"] = received.String
		delete(refresh, "last_error")
	}
	refresh["state"] = state
	for _, key := range []string{"next_due_at", "last_error", "failures"} {
		if value, ok := result[key]; ok {
			refresh[key] = value
		}
	}
	for _, key := range []string{"lease_token", "lease_owner", "lease_expires_at"} {
		delete(refresh, key)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE work_metadata SET refresh_json=? WHERE card_id=?`, workJSON(refresh), id); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return refresh, nil
}
