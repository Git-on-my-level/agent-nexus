package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"agent-nexus-core/internal/blob"

	"github.com/google/uuid"
)

// MaxAskBatchMembers bounds one filed ask batch. Per-request cost on every
// batch path (filing, derivation, answering) is bounded by this constant.
const MaxAskBatchMembers = 10
const MinAskBatchMembers = 2

// AppendTaskAttentionEventBatch files N related asks as one atomic batch: the
// server-assigned batch id lands in every member's payload, and either every
// member commits or none does. Callers must have validated the member events;
// bodies are cloned so the caller's maps are never mutated.
func (s *Store) AppendTaskAttentionEventBatch(ctx context.Context, actor string, events []map[string]any) (string, []map[string]any, error) {
	if len(events) < MinAskBatchMembers || len(events) > MaxAskBatchMembers {
		return "", nil, fmt.Errorf("ask batch must contain %d to %d asks", MinAskBatchMembers, MaxAskBatchMembers)
	}
	bodies := make([]map[string]any, 0, len(events))
	dbBytes := 0
	for _, event := range events {
		body := cloneMap(event)
		if strings.TrimSpace(anyStringValue(asMapValue(body["payload"])["subject_ref"])) == "" {
			return "", nil, workInvalid("subject_ref is required")
		}
		if strings.TrimSpace(anyStringValue(asMapValue(body["payload"])["batch_id"])) != "" {
			return "", nil, workInvalid("batch_id is assigned by the server")
		}
		prepared, err := prepareEventForInsert(actor, body)
		if err != nil {
			return "", nil, err
		}
		dbBytes += len(prepared.PayloadJSON) + len(prepared.RefsJSON) + 4096
		bodies = append(bodies, body)
	}
	batchID := "askbatch_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	for _, body := range bodies {
		// cloneMap is shallow: re-clone the payload so the caller's maps are
		// never mutated (a retry's idempotency hash must see identical input).
		payload := cloneMap(asMapValue(body["payload"]))
		payload["batch_id"] = batchID
		body["payload"] = payload
	}
	quotaErr := func() error {
		return s.checkWorkspaceWriteQuota(ctx, 0, quotaWriteDelta{dbBytes: int64(dbBytes)}, blobLedgerWritePlan{})
	}
	if s.quota.enabled() {
		s.quotaMu.Lock()
		if s.quota.MaxBlobBytes > 0 {
			if err := s.ensureBlobUsageLedgerInitialized(ctx); err != nil {
				s.quotaMu.Unlock()
				return "", nil, err
			}
		}
		err := quotaErr()
		s.quotaMu.Unlock()
		if err != nil {
			return "", nil, err
		}
	} else if err := quotaErr(); err != nil {
		return "", nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", nil, err
	}
	defer tx.Rollback()
	stored := make([]map[string]any, 0, len(bodies))
	var stagedWrites []blob.StagedWrite
	defer func() {
		for _, staged := range stagedWrites {
			_ = staged.Cleanup()
		}
	}()
	for _, body := range bodies {
		storedBody, staged, _, err := s.appendTaskAttentionEventTx(ctx, tx, actor, body)
		if err != nil {
			return "", nil, err
		}
		if staged != nil {
			stagedWrites = append(stagedWrites, staged)
		}
		stored = append(stored, storedBody)
	}
	if err = tx.Commit(); err != nil {
		return "", nil, err
	}
	for _, staged := range stagedWrites {
		if err = staged.Promote(); err != nil {
			return "", nil, err
		}
	}
	return batchID, stored, nil
}

// HumanAttentionBatchResponseMember is one member answer inside an atomic
// batch response. Notify carries that member's wake decision; delivery=none
// members pass requested=false so no wake batch row is queued for them.
type HumanAttentionBatchResponseMember struct {
	SourceEventID string
	InboxItemID   string
	Event         map[string]any
	Notify        map[string]any
}

// HumanAttentionBatchResponseResult is the stored outcome of one member answer.
type HumanAttentionBatchResponseResult struct {
	AskRef      string         `json:"ask_ref"`
	Event       map[string]any `json:"event"`
	TaskOutcome any            `json:"task_outcome"`
}

// AppendHumanAttentionBatchResponse records one answer per batch member in a
// single transaction: every member event, resolution, claim and wake entry
// commits together, and the whole batch replays through one claim row.
func (s *Store) AppendHumanAttentionBatchResponse(
	ctx context.Context,
	actorID string,
	batchID string,
	inboxItemID string,
	key string,
	hash string,
	members []HumanAttentionBatchResponseMember,
) (map[string]any, bool, error) {
	if strings.TrimSpace(batchID) == "" || len(members) == 0 {
		return nil, false, fmt.Errorf("batch id and members are required")
	}
	if s.quota.enabled() {
		s.quotaMu.Lock()
		defer s.quotaMu.Unlock()
	}
	dbBytes := 0
	type preparedMember struct {
		member   HumanAttentionBatchResponseMember
		prepared preparedEvent
	}
	preparedMembers := make([]preparedMember, 0, len(members))
	for _, member := range members {
		prepared, err := prepareEventForInsert(actorID, member.Event)
		if err != nil {
			return nil, false, err
		}
		dbBytes += len(prepared.PayloadJSON) + len(prepared.RefsJSON) + 512
		preparedMembers = append(preparedMembers, preparedMember{member: member, prepared: prepared})
	}
	if err := s.checkWorkspaceWriteQuota(ctx, 0, quotaWriteDelta{dbBytes: int64(dbBytes)}, blobLedgerWritePlan{}); err != nil {
		return nil, false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	results := make([]HumanAttentionBatchResponseResult, 0, len(preparedMembers))
	for _, pm := range preparedMembers {
		payload := asMapValue(pm.prepared.Body["payload"])
		if err := insertPreparedEvent(ctx, tx, pm.prepared); err != nil {
			return nil, false, err
		}
		resolution, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO human_attention_request_resolutions(request_event_id,resolution_event_id,resolution_type,actor_id,created_at) VALUES(?,?,'answered',?,?)`,
			pm.member.SourceEventID, pm.prepared.Body["id"], actorID, pm.prepared.Body["ts"])
		if err != nil {
			return nil, false, err
		}
		inserted, err := resolution.RowsAffected()
		if err != nil {
			return nil, false, err
		}
		if inserted == 0 {
			if err := tx.Rollback(); err != nil {
				return nil, false, err
			}
			if key != "" {
				replay, replayErr := s.HumanAttentionBatchResponseReplay(ctx, actorID, key, hash)
				if replayErr == nil {
					return replay, true, nil
				}
				if !errors.Is(replayErr, ErrNotFound) {
					return nil, false, replayErr
				}
			}
			return nil, false, ErrHumanAttentionAlreadyResponded
		}
		if err := s.applyAccessDecisionTx(ctx, tx, pm.member.SourceEventID, actorID, anyStringValue(payload["outcome"]), anyStringValue(pm.prepared.Body["ts"])); err != nil {
			return nil, false, err
		}
		taskOutcome, err := s.applyAskTaskOutcomeTx(ctx, tx, actorID, pm.member.SourceEventID, pm.prepared.Body)
		if err != nil {
			return nil, false, err
		}
		// Member claims keep request_key empty: the (actor_id, request_key)
		// unique index there is per row, and the batch replays through its own
		// claim row. The row still marks the member answered for claim checks.
		memberResponse := map[string]any{"event": pm.prepared.Body, "task_outcome": taskOutcome}
		memberRaw, err := json.Marshal(memberResponse)
		if err != nil {
			return nil, false, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO human_attention_response_claims(request_event_id,inbox_item_id,actor_id,request_key,request_hash,response_event_id,response_json,created_at) VALUES(?,?,?,'','',?,?,?)`,
			pm.member.SourceEventID, pm.member.InboxItemID, actorID, pm.prepared.Body["id"], string(memberRaw), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return nil, false, err
		}
		if err := queueHumanAttentionAnswerWakeBatchTx(ctx, tx, pm.member.SourceEventID, pm.prepared.Body, pm.member.Notify); err != nil {
			return nil, false, err
		}
		results = append(results, HumanAttentionBatchResponseResult{
			// The ask's own ref, echoing the request's ask_ref so consumers
			// can correlate; the answer event id lives in Event["id"].
			AskRef:      "event:" + pm.member.SourceEventID,
			Event:       pm.prepared.Body,
			TaskOutcome: taskOutcome,
		})
	}
	// The stored and returned shapes must match exactly: normalize through
	// JSON so the claim row and the in-memory response carry the same []any.
	raw, err := json.Marshal(results)
	if err != nil {
		return nil, false, err
	}
	var answersAny []any
	if err := json.Unmarshal(raw, &answersAny); err != nil {
		return nil, false, err
	}
	response := map[string]any{"batch_id": batchID, "answers": answersAny}
	raw, err = json.Marshal(response)
	if err != nil {
		return nil, false, err
	}
	claimResult, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO human_attention_batch_response_claims(batch_id,inbox_item_id,actor_id,request_key,request_hash,response_json,created_at) VALUES(?,?,?,?,?,?,?)`,
		batchID, inboxItemID, actorID, key, hash, string(raw), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, false, err
	}
	claimed, err := claimResult.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	if claimed == 0 {
		if err := tx.Rollback(); err != nil {
			return nil, false, err
		}
		if key != "" {
			replay, replayErr := s.HumanAttentionBatchResponseReplay(ctx, actorID, key, hash)
			if replayErr == nil {
				return replay, true, nil
			}
			if !errors.Is(replayErr, ErrNotFound) {
				return nil, false, replayErr
			}
		}
		return nil, false, ErrHumanAttentionAlreadyResponded
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return response, false, nil
}

// HumanAttentionBatchResponseReplay resolves a retried batch submit before
// consulting the open inbox projection, which the answered batch has left.
func (s *Store) HumanAttentionBatchResponseReplay(ctx context.Context, actorID, key, hash string) (map[string]any, error) {
	if key == "" {
		return nil, ErrNotFound
	}
	var storedHash, raw string
	err := s.db.QueryRowContext(ctx, `SELECT request_hash,response_json FROM human_attention_batch_response_claims WHERE actor_id=? AND request_key=?`, actorID, key).Scan(&storedHash, &raw)
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

// HumanAttentionBatchResponseClaimed reports whether the batch has been
// answered already.
func (s *Store) HumanAttentionBatchResponseClaimed(ctx context.Context, batchID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM human_attention_batch_response_claims WHERE batch_id=?`, strings.TrimSpace(batchID)).Scan(&count)
	return count > 0, err
}

// HumanAttentionBatchResponseRequest recovers the answered batch behind an
// inbox item id so authorization can consult the stored response after the
// open projection row is gone.
func (s *Store) HumanAttentionBatchResponseRequest(ctx context.Context, inboxItemID string) (map[string]any, error) {
	var raw string
	var batchID string
	err := s.db.QueryRowContext(ctx, `SELECT batch_id,response_json FROM human_attention_batch_response_claims WHERE inbox_item_id=?`, strings.TrimSpace(inboxItemID)).Scan(&batchID, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var response map[string]any
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		return nil, err
	}
	response["batch_id"] = batchID
	return response, nil
}

// SaveHumanAttentionBatchResponseResult replaces the stored combined response
// (the handler attaches hygiene and derived fields after commit).
func (s *Store) SaveHumanAttentionBatchResponseResult(ctx context.Context, batchID string, response map[string]any) error {
	raw, err := json.Marshal(response)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE human_attention_batch_response_claims SET response_json=? WHERE batch_id=?`, string(raw), strings.TrimSpace(batchID))
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("batch response claim missing for %s", batchID)
	}
	return nil
}
