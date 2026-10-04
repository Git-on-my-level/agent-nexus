package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// AppendHumanAttentionWithdrawal records a requester's withdrawal atomically
// with the request's unique resolution claim. A concurrent human response and
// withdrawal can therefore never both resolve the same ask.
func (s *Store) AppendHumanAttentionWithdrawal(ctx context.Context, actorID, sourceEventID string, event map[string]any) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	actorID = strings.TrimSpace(actorID)
	sourceEventID = strings.TrimSpace(sourceEventID)
	if actorID == "" || sourceEventID == "" {
		return nil, fmt.Errorf("request actor and source event id are required")
	}
	if s.quota.enabled() {
		s.quotaMu.Lock()
		defer s.quotaMu.Unlock()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var sourceType, threadID, requesterActorID, refsJSON, payloadJSON string
	err = tx.QueryRowContext(ctx, `SELECT type,COALESCE(thread_id,''),
		trim(COALESCE(json_extract(payload_json,'$.payload.requester_actor_id'),'')),refs_json,payload_json
		FROM events WHERE id=?`, sourceEventID).Scan(&sourceType, &threadID, &requesterActorID, &refsJSON, &payloadJSON)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load human attention request for withdrawal: %w", err)
	}
	if sourceType != "human_attention_requested" {
		return nil, ErrNotFound
	}
	if requesterActorID != actorID {
		return nil, ErrForbidden
	}

	var sourceWrapper map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &sourceWrapper); err != nil {
		return nil, fmt.Errorf("decode human attention request: %w", err)
	}
	sourcePayload, _ := sourceWrapper["payload"].(map[string]any)
	var sourceRefs []string
	if err := json.Unmarshal([]byte(refsJSON), &sourceRefs); err != nil {
		return nil, fmt.Errorf("decode human attention request refs: %w", err)
	}
	requestPayload, _ := event["payload"].(map[string]any)
	reason := strings.TrimSpace(anyStringValue(requestPayload["reason"]))
	if reason == "" {
		return nil, fmt.Errorf("withdrawal reason is required")
	}

	refs := []string{"event:" + sourceEventID}
	if threadID != "" {
		refs = append(refs, "thread:"+threadID)
	}
	if subjectRef := strings.TrimSpace(anyStringValue(sourcePayload["subject_ref"])); subjectRef != "" {
		refs = append(refs, subjectRef)
	}
	event["type"] = "human_attention_withdrawn"
	event["thread_id"] = threadID
	event["summary"] = "Ask withdrawn by requester"
	for _, ref := range sourceRefs {
		refs = appendUnique(refs, ref)
	}
	event["refs"] = refs
	event["payload"] = map[string]any{
		"request_event_id":      sourceEventID,
		"request_event_ref":     "event:" + sourceEventID,
		"requester_actor_id":    actorID,
		"withdrawn_by_actor_id": actorID,
		"reason":                reason,
	}
	prepared, err := prepareEventForInsert(actorID, event)
	if err != nil {
		return nil, err
	}
	if err := s.checkWorkspaceWriteQuota(ctx, 0, quotaWriteDelta{dbBytes: int64(len(prepared.PayloadJSON) + len(prepared.RefsJSON) + 512)}, blobLedgerWritePlan{}); err != nil {
		return nil, err
	}
	if err := insertPreparedEvent(ctx, tx, prepared); err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO human_attention_request_resolutions(request_event_id,resolution_event_id,resolution_type,actor_id,created_at) VALUES(?,?,'withdrawn',?,?)`, sourceEventID, prepared.Body["id"], actorID, prepared.Body["ts"])
	if err != nil {
		return nil, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if inserted == 0 {
		return nil, ErrHumanAttentionAlreadyResponded
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return prepared.Body, nil
}
