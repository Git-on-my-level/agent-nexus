package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

type IdempotencyReplay struct {
	RequestHash string
	Status      int
	Response    map[string]any
}

func (s *Store) GetIdempotencyReplay(ctx context.Context, scope string, actorID string, requestKey string) (IdempotencyReplay, error) {
	if s == nil || s.db == nil {
		return IdempotencyReplay{}, fmt.Errorf("primitives store database is not initialized")
	}

	var (
		requestHash  string
		status       int
		responseJSON string
	)
	err := s.db.QueryRowContext(
		ctx,
		`SELECT request_hash, response_status, response_json
		 FROM idempotency_replays
		 WHERE scope = ? AND actor_id = ? AND request_key = ?`,
		scope,
		actorID,
		requestKey,
	).Scan(&requestHash, &status, &responseJSON)
	if err == sql.ErrNoRows {
		return IdempotencyReplay{}, ErrNotFound
	}
	if err != nil {
		return IdempotencyReplay{}, fmt.Errorf("query idempotency replay: %w", err)
	}

	response := map[string]any{}
	if err := json.Unmarshal([]byte(responseJSON), &response); err != nil {
		return IdempotencyReplay{}, fmt.Errorf("decode idempotency replay response: %w", err)
	}

	return IdempotencyReplay{
		RequestHash: requestHash,
		Status:      status,
		Response:    response,
	}, nil
}

func (s *Store) PutIdempotencyReplay(ctx context.Context, scope string, actorID string, requestKey string, requestHash string, status int, response map[string]any) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("primitives store database is not initialized")
	}

	responseJSON, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("marshal idempotency replay response: %w", err)
	}

	if _, err := s.db.ExecContext(
		ctx,
		`INSERT INTO idempotency_replays(scope, actor_id, request_key, request_hash, response_status, response_json)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		scope,
		actorID,
		requestKey,
		requestHash,
		status,
		string(responseJSON),
	); err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("insert idempotency replay: %w", err)
	}

	return nil
}
