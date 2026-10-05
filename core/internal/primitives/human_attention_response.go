package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrHumanAttentionAlreadyResponded = errors.New("human_attention_already_responded")
var ErrHumanAttentionIdempotencyConflict = errors.New("human_attention_idempotency_conflict")

// HumanAttentionResponseRequest preserves authorization of an answered Inbox
// item after its open projection has been removed. The claim is authoritative
// for the original request; callers must apply current resource access to it.
func (s *Store) HumanAttentionResponseRequest(ctx context.Context, inboxItemID string) (map[string]any, error) {
	var requestEventID string
	err := s.db.QueryRowContext(ctx, `SELECT request_event_id FROM human_attention_response_claims WHERE inbox_item_id=?`, inboxItemID).Scan(&requestEventID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.GetEvent(ctx, requestEventID)
}

func (s *Store) HumanAttentionResponseClaimed(ctx context.Context, inboxItemID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM human_attention_response_claims WHERE inbox_item_id=?`, inboxItemID).Scan(&count)
	return count > 0, err
}

// HumanAttentionResponseReplay resolves a retry before consulting the open inbox
// projection. That projection may already have removed the answered item.
func (s *Store) HumanAttentionResponseReplay(ctx context.Context, actorID, key, hash string) (map[string]any, error) {
	if key == "" {
		return nil, ErrNotFound
	}
	var storedHash, raw string
	err := s.db.QueryRowContext(ctx, `SELECT request_hash,response_json FROM human_attention_response_claims WHERE actor_id=? AND request_key=?`, actorID, key).Scan(&storedHash, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if storedHash != hash {
		return nil, ErrHumanAttentionIdempotencyConflict
	}
	var response map[string]any
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		return nil, err
	}
	return response, nil
}

// AppendHumanAttentionResponse serializes the event and its unique source
// request claim in one transaction. A concurrent response loses the unique
// claim and its event insert is rolled back with the transaction.
func (s *Store) AppendHumanAttentionResponse(ctx context.Context, actorID, sourceEventID, inboxItemID, key, hash string, event map[string]any, initialNotify map[string]any) (map[string]any, bool, error) {
	if sourceEventID == "" {
		return nil, false, fmt.Errorf("source event id is required")
	}
	prepared, err := prepareEventForInsert(actorID, event)
	if err != nil {
		return nil, false, err
	}
	if s.quota.enabled() {
		s.quotaMu.Lock()
		defer s.quotaMu.Unlock()
	}
	if err := s.checkWorkspaceWriteQuota(ctx, 0, quotaWriteDelta{dbBytes: int64(len(prepared.PayloadJSON) + len(prepared.RefsJSON) + 512)}, blobLedgerWritePlan{}); err != nil {
		return nil, false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	if err := insertPreparedEvent(ctx, tx, prepared); err != nil {
		return nil, false, err
	}
	var existingResponses int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE type='human_attention_responded' AND id<>? AND (json_extract(payload_json,'$.payload.request_event_ref')=? OR json_extract(payload_json,'$.payload.request_event_ref')='event:' || (SELECT handle FROM events WHERE id=?) OR json_extract(payload_json,'$.payload.request_event_id')=?)`, prepared.Body["id"], "event:"+sourceEventID, sourceEventID, sourceEventID).Scan(&existingResponses); err != nil {
		return nil, false, err
	}
	if existingResponses > 0 {
		if err := tx.Rollback(); err != nil {
			return nil, false, err
		}
		if key != "" {
			replay, replayErr := s.HumanAttentionResponseReplay(ctx, actorID, key, hash)
			if replayErr == nil {
				return replay, true, nil
			}
			if !errors.Is(replayErr, ErrNotFound) {
				return nil, false, replayErr
			}
		}
		return nil, false, ErrHumanAttentionAlreadyResponded
	}
	resolution, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO human_attention_request_resolutions(request_event_id,resolution_event_id,resolution_type,actor_id,created_at) VALUES(?,?,'answered',?,?)`, sourceEventID, prepared.Body["id"], actorID, prepared.Body["ts"])
	if err != nil {
		return nil, false, err
	}
	resolutionInserted, err := resolution.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	if resolutionInserted == 0 {
		if err := tx.Rollback(); err != nil {
			return nil, false, err
		}
		if key != "" {
			replay, replayErr := s.HumanAttentionResponseReplay(ctx, actorID, key, hash)
			if replayErr == nil {
				return replay, true, nil
			}
			if !errors.Is(replayErr, ErrNotFound) {
				return nil, false, replayErr
			}
		}
		return nil, false, ErrHumanAttentionAlreadyResponded
	}
	payload, _ := event["payload"].(map[string]any)
	if err := s.applyAccessDecisionTx(ctx, tx, sourceEventID, actorID, anyStringValue(payload["outcome"]), anyStringValue(prepared.Body["ts"])); err != nil {
		return nil, false, err
	}
	publicNotify := cloneMap(initialNotify)
	delete(publicNotify, "quiet_window_ns")
	response := map[string]any{"event": prepared.Body, "notify": publicNotify}
	raw, err := json.Marshal(response)
	if err != nil {
		return nil, false, err
	}
	result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO human_attention_response_claims(request_event_id,inbox_item_id,actor_id,request_key,request_hash,response_event_id,response_json,created_at) VALUES(?,?,?,?,?,?,?,?)`, sourceEventID, inboxItemID, actorID, key, hash, prepared.Body["id"], string(raw), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	if affected == 0 {
		if err := tx.Rollback(); err != nil {
			return nil, false, err
		}
		if key != "" {
			replay, replayErr := s.HumanAttentionResponseReplay(ctx, actorID, key, hash)
			if replayErr == nil {
				return replay, true, nil
			}
			if !errors.Is(replayErr, ErrNotFound) {
				return nil, false, replayErr
			}
		}
		return nil, false, ErrHumanAttentionAlreadyResponded
	}
	if err := queueHumanAttentionAnswerWakeBatchTx(ctx, tx, sourceEventID, prepared.Body, initialNotify); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return response, false, nil
}

func (s *Store) SaveHumanAttentionResponseResult(ctx context.Context, sourceEventID string, response map[string]any) error {
	raw, err := json.Marshal(response)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE human_attention_response_claims SET response_json=? WHERE request_event_id=?`, string(raw), sourceEventID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("response claim missing for %s", sourceEventID)
	}
	return nil
}
