package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// HumanAttentionAnswerWakeBatch is the durable debounce window for answers sent
// to one requesting agent. It is stored in the workspace database so a core
// restart does not lose a pending wake.
type HumanAttentionAnswerWakeBatch struct {
	TargetActorID    string
	TargetHandle     string
	WorkspaceID      string
	BatchID          string
	FirstAnsweredAt  string
	LastAnsweredAt   string
	ThreadID         string
	TriggerEventID   string
	TriggerCreatedAt string
	AnswerCount      int
	AskEventIDs      []string
	AnswerEventIDs   []string
	Refs             []string
}

func queueHumanAttentionAnswerWakeBatchTx(ctx context.Context, tx *sql.Tx, sourceEventID string, event map[string]any, notify map[string]any) error {
	if notify["requested"] != true {
		return nil
	}
	actorID := strings.TrimSpace(anyStringValue(notify["target_actor_id"]))
	responseEventID := strings.TrimSpace(anyStringValue(event["id"]))
	if actorID == "" || responseEventID == "" || strings.TrimSpace(sourceEventID) == "" {
		return nil
	}
	answerAt := strings.TrimSpace(anyStringValue(event["ts"]))
	if answerAt == "" {
		answerAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	workspaceID := strings.TrimSpace(anyStringValue(notify["workspace_id"]))
	if workspaceID == "" {
		workspaceID = "ws_main"
	}
	threadID := strings.TrimSpace(anyStringValue(notify["thread_id"]))
	refs := []string{"event:" + sourceEventID, "event:" + responseEventID}
	if threadID != "" {
		refs = append(refs, "thread:"+threadID)
	}
	if subjectRef := strings.TrimSpace(anyStringValue(notify["subject_ref"])); subjectRef != "" {
		refs = append(refs, subjectRef)
	}

	batch, err := scanHumanAttentionAnswerWakeBatch(tx.QueryRowContext(ctx, `SELECT target_actor_id,target_handle,workspace_id,batch_id,
		first_answered_at,last_answered_at,thread_id,trigger_event_id,trigger_created_at,answer_count,
		ask_event_ids_json,answer_event_ids_json,refs_json
		FROM human_attention_answer_wake_batches WHERE target_actor_id=?`, actorID))
	if err != nil && err != ErrNotFound {
		return fmt.Errorf("load answer wake batch: %w", err)
	}
	if err == ErrNotFound {
		batch = HumanAttentionAnswerWakeBatch{
			TargetActorID:   actorID,
			TargetHandle:    strings.TrimSpace(anyStringValue(notify["target_handle"])),
			WorkspaceID:     workspaceID,
			BatchID:         responseEventID,
			FirstAnsweredAt: answerAt,
			ThreadID:        threadID,
		}
	}
	if batch.TargetHandle == "" {
		batch.TargetHandle = strings.TrimSpace(anyStringValue(notify["target_handle"]))
	}
	if batch.WorkspaceID == "" {
		batch.WorkspaceID = workspaceID
	}
	if batch.ThreadID != threadID {
		batch.ThreadID = ""
	}
	batch.LastAnsweredAt = answerAt
	batch.TriggerEventID = responseEventID
	batch.TriggerCreatedAt = answerAt
	batch.AnswerCount++
	batch.AskEventIDs = appendUnique(batch.AskEventIDs, sourceEventID)
	batch.AnswerEventIDs = appendUnique(batch.AnswerEventIDs, responseEventID)
	for _, ref := range refs {
		batch.Refs = appendUnique(batch.Refs, ref)
	}

	askIDsJSON, err := json.Marshal(batch.AskEventIDs)
	if err != nil {
		return err
	}
	answerIDsJSON, err := json.Marshal(batch.AnswerEventIDs)
	if err != nil {
		return err
	}
	refsJSON, err := json.Marshal(batch.Refs)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO human_attention_answer_wake_batches (
		target_actor_id,target_handle,workspace_id,batch_id,first_answered_at,last_answered_at,
		thread_id,trigger_event_id,trigger_created_at,answer_count,ask_event_ids_json,
		answer_event_ids_json,refs_json,updated_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	ON CONFLICT(target_actor_id) DO UPDATE SET target_handle=excluded.target_handle,
		workspace_id=excluded.workspace_id,last_answered_at=excluded.last_answered_at,
		thread_id=excluded.thread_id,trigger_event_id=excluded.trigger_event_id,
		trigger_created_at=excluded.trigger_created_at,answer_count=excluded.answer_count,
		ask_event_ids_json=excluded.ask_event_ids_json,answer_event_ids_json=excluded.answer_event_ids_json,
		refs_json=excluded.refs_json,updated_at=excluded.updated_at`,
		batch.TargetActorID, batch.TargetHandle, batch.WorkspaceID, batch.BatchID, batch.FirstAnsweredAt, batch.LastAnsweredAt,
		batch.ThreadID, batch.TriggerEventID, batch.TriggerCreatedAt, batch.AnswerCount, string(askIDsJSON), string(answerIDsJSON), string(refsJSON), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("queue answer wake batch: %w", err)
	}
	return nil
}

func (s *Store) ListHumanAttentionAnswerWakeBatches(ctx context.Context) ([]HumanAttentionAnswerWakeBatch, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT target_actor_id,target_handle,workspace_id,batch_id,
		first_answered_at,last_answered_at,thread_id,trigger_event_id,trigger_created_at,answer_count,
		ask_event_ids_json,answer_event_ids_json,refs_json FROM human_attention_answer_wake_batches
		ORDER BY last_answered_at,target_actor_id`)
	if err != nil {
		return nil, fmt.Errorf("list answer wake batches: %w", err)
	}
	defer rows.Close()
	out := []HumanAttentionAnswerWakeBatch{}
	for rows.Next() {
		batch, err := scanHumanAttentionAnswerWakeBatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, batch)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate answer wake batches: %w", err)
	}
	return out, nil
}

func (s *Store) CountOpenHumanAttentionAsks(ctx context.Context, requesterActorID string) (int, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("primitives store database is not initialized")
	}
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events AS ask
		WHERE ask.type='human_attention_requested'
		  AND trim(COALESCE(json_extract(ask.payload_json,'$.payload.requester_actor_id'),''))=?
		  AND NOT EXISTS (
			SELECT 1 FROM events AS answer
			WHERE answer.type='human_attention_responded'
			  AND (json_extract(answer.payload_json,'$.payload.request_event_ref')='event:' || ask.id
			       OR json_extract(answer.payload_json,'$.payload.request_event_id')=ask.id)
		  )`, strings.TrimSpace(requesterActorID)).Scan(&count)
	return count, err
}

func (s *Store) DeleteHumanAttentionAnswerWakeBatch(ctx context.Context, targetActorID, batchID, triggerEventID string) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("primitives store database is not initialized")
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM human_attention_answer_wake_batches
		WHERE target_actor_id=? AND batch_id=? AND trigger_event_id=?`, strings.TrimSpace(targetActorID), strings.TrimSpace(batchID), strings.TrimSpace(triggerEventID))
	if err != nil {
		return false, fmt.Errorf("delete answer wake batch: %w", err)
	}
	count, err := result.RowsAffected()
	return count > 0, err
}

type answerWakeBatchScanner interface {
	Scan(dest ...any) error
}

func scanHumanAttentionAnswerWakeBatch(row answerWakeBatchScanner) (HumanAttentionAnswerWakeBatch, error) {
	var batch HumanAttentionAnswerWakeBatch
	var askJSON, answerJSON, refsJSON string
	err := row.Scan(&batch.TargetActorID, &batch.TargetHandle, &batch.WorkspaceID, &batch.BatchID,
		&batch.FirstAnsweredAt, &batch.LastAnsweredAt, &batch.ThreadID, &batch.TriggerEventID, &batch.TriggerCreatedAt, &batch.AnswerCount,
		&askJSON, &answerJSON, &refsJSON)
	if err == sql.ErrNoRows {
		return HumanAttentionAnswerWakeBatch{}, ErrNotFound
	}
	if err != nil {
		return HumanAttentionAnswerWakeBatch{}, fmt.Errorf("scan answer wake batch: %w", err)
	}
	var decodeErr error
	if batch.AskEventIDs, decodeErr = decodeStoredJSONList(askJSON, "answer_wake_batch.ask_event_ids"); decodeErr != nil {
		return HumanAttentionAnswerWakeBatch{}, decodeErr
	}
	if batch.AnswerEventIDs, decodeErr = decodeStoredJSONList(answerJSON, "answer_wake_batch.answer_event_ids"); decodeErr != nil {
		return HumanAttentionAnswerWakeBatch{}, decodeErr
	}
	if batch.Refs, decodeErr = decodeStoredJSONList(refsJSON, "answer_wake_batch.refs"); decodeErr != nil {
		return HumanAttentionAnswerWakeBatch{}, decodeErr
	}
	return batch, nil
}

func appendUnique(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	values = append(values, value)
	sort.Strings(values)
	return values
}
