// Thread mutations: create, patch, archive/unarchive/trash/restore, purge,
// and the shared patch application.

package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

type ThreadMutationResult struct {
	Thread map[string]any
	Event  map[string]any
}

// applyThreadPatch updates a threads-table row with kind "thread".
func (s *Store) applyThreadPatch(ctx context.Context, actorID string, id string, patch map[string]any, ifUpdatedAt *string) (ThreadMutationResult, error) {
	if s == nil || s.db == nil {
		return ThreadMutationResult{}, fmt.Errorf("primitives store database is not initialized")
	}
	if strings.TrimSpace(actorID) == "" {
		return ThreadMutationResult{}, fmt.Errorf("actorID is required")
	}
	if patch == nil {
		return ThreadMutationResult{}, fmt.Errorf("thread patch is required")
	}

	var (
		rowID          string
		rowKind        string
		threadID       sql.NullString
		provenanceJSON string
		bodyJSON       string
	)
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, kind, thread_id, provenance_json, body_json FROM threads WHERE id = ?`,
		id,
	).Scan(&rowID, &rowKind, &threadID, &provenanceJSON, &bodyJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return ThreadMutationResult{}, ErrNotFound
	}
	if err != nil {
		return ThreadMutationResult{}, fmt.Errorf("query thread before patch: %w", err)
	}
	if strings.TrimSpace(rowKind) != "thread" {
		return ThreadMutationResult{}, ErrNotFound
	}

	current := map[string]any{}
	if strings.TrimSpace(bodyJSON) != "" {
		if err := json.Unmarshal([]byte(bodyJSON), &current); err != nil {
			return ThreadMutationResult{}, fmt.Errorf("decode current thread body: %w", err)
		}
	}

	currentProvenance := map[string]any{}
	if strings.TrimSpace(provenanceJSON) != "" {
		if err := json.Unmarshal([]byte(provenanceJSON), &currentProvenance); err != nil {
			return ThreadMutationResult{}, fmt.Errorf("decode current thread provenance: %w", err)
		}
	}

	bodyPatch := cloneMap(patch)
	nextProvenance := cloneProvenance(currentProvenance)
	provenanceChanged := false
	if rawProvenance, hasProvenance := bodyPatch["provenance"]; hasProvenance {
		provenancePatch, ok := rawProvenance.(map[string]any)
		if !ok {
			return ThreadMutationResult{}, fmt.Errorf("thread.provenance must be an object")
		}
		nextProvenance = cloneMap(provenancePatch)
		delete(bodyPatch, "provenance")
		provenanceChanged = !reflect.DeepEqual(currentProvenance, nextProvenance)
	}

	changedFields := make([]string, 0, len(bodyPatch)+1)
	for key, incoming := range bodyPatch {
		existing, exists := current[key]
		if !exists || !reflect.DeepEqual(existing, incoming) {
			changedFields = append(changedFields, key)
		}
		current[key] = incoming
	}
	if provenanceChanged {
		changedFields = append(changedFields, "provenance")
	}
	sort.Strings(changedFields)

	updatedAt := time.Now().UTC().Format(time.RFC3339Nano)

	updatedBodyJSON, err := json.Marshal(current)
	if err != nil {
		return ThreadMutationResult{}, fmt.Errorf("encode patched thread body: %w", err)
	}
	_, updatedProvenanceJSON, err := marshalProvenance(nextProvenance, "encode patched thread provenance")
	if err != nil {
		return ThreadMutationResult{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ThreadMutationResult{}, fmt.Errorf("begin thread patch transaction: %w", err)
	}

	updateQuery := `UPDATE threads
		 SET body_json = ?, provenance_json = ?, updated_at = ?, updated_by = ?
		 WHERE id = ?`
	updateArgs := []any{
		string(updatedBodyJSON),
		updatedProvenanceJSON,
		updatedAt,
		actorID,
		rowID,
	}
	updateQuery, updateArgs = appendIfUpdatedAtClause(updateQuery, updateArgs, ifUpdatedAt)
	updateResult, err := tx.ExecContext(ctx, updateQuery, updateArgs...)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return ThreadMutationResult{}, fmt.Errorf("update thread: %w", err)
	}
	if err := requireIfUpdatedAtRowsAffected(updateResult, ifUpdatedAt, "patch thread"); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return ThreadMutationResult{}, err
	}

	if err := tx.Commit(); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return ThreadMutationResult{}, fmt.Errorf("commit thread patch transaction: %w", err)
	}

	current["id"] = rowID
	if _, hasType := current["type"]; !hasType {
		current["type"] = rowKind
	}
	current["updated_at"] = updatedAt
	current["updated_by"] = actorID
	if threadID.Valid {
		current["thread_id"] = threadID.String
	}
	current["provenance"] = nextProvenance

	return ThreadMutationResult{
		Thread: current,
	}, nil
}

func (s *Store) CreateThread(ctx context.Context, actorID string, thread map[string]any) (ThreadMutationResult, error) {
	if s == nil || s.db == nil {
		return ThreadMutationResult{}, fmt.Errorf("primitives store database is not initialized")
	}
	if strings.TrimSpace(actorID) == "" {
		return ThreadMutationResult{}, fmt.Errorf("actorID is required")
	}
	if thread == nil {
		return ThreadMutationResult{}, fmt.Errorf("thread is required")
	}

	threadID, _ := thread["id"].(string)
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		threadID = uuid.NewString()
	}
	updatedAt := time.Now().UTC().Format(time.RFC3339Nano)

	body := cloneMap(thread)
	delete(body, "id")
	delete(body, "provenance")

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return ThreadMutationResult{}, fmt.Errorf("marshal thread body: %w", err)
	}

	provenance, provenanceJSON, err := marshalProvenance(thread["provenance"], "marshal thread")
	if err != nil {
		return ThreadMutationResult{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ThreadMutationResult{}, fmt.Errorf("begin thread create transaction: %w", err)
	}
	handle, err := uniqueHandleTx(ctx, tx, "thread", anyStringValue(body["title"]), "thread-"+threadID)
	if err != nil {
		_ = tx.Rollback()
		return ThreadMutationResult{}, fmt.Errorf("allocate thread handle: %w", err)
	}

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO threads(id, handle, kind, thread_id, updated_at, updated_by, body_json, provenance_json)
		 VALUES (?, ?, 'thread', ?, ?, ?, ?, ?)`,
		threadID,
		handle,
		threadID,
		updatedAt,
		actorID,
		string(bodyJSON),
		provenanceJSON,
	)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		if isUniqueViolation(err) {
			return ThreadMutationResult{}, ErrConflict
		}
		return ThreadMutationResult{}, fmt.Errorf("insert thread row: %w", err)
	}

	if err := tx.Commit(); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return ThreadMutationResult{}, fmt.Errorf("commit thread create transaction: %w", err)
	}

	out := cloneMap(body)
	out["id"] = threadID
	out["ref"] = "thread:" + handle
	out["handle"] = handle
	// Thread domain `type` is provided by caller (thread_type enum).
	out["thread_id"] = threadID
	out["updated_at"] = updatedAt
	out["updated_by"] = actorID
	out["provenance"] = provenance

	return ThreadMutationResult{
		Thread: out,
	}, nil
}

func (s *Store) GetThread(ctx context.Context, id string) (map[string]any, error) {
	row, err := s.getThreadRow(ctx, id, "threads")
	if err != nil {
		return nil, err
	}
	if row.Kind != "thread" {
		return nil, ErrNotFound
	}
	return row.ToThreadMapPublic(ctx, s.db)
}

func (s *Store) PatchThread(ctx context.Context, actorID string, id string, patch map[string]any, ifUpdatedAt *string) (ThreadMutationResult, error) {
	return s.applyThreadPatch(ctx, actorID, id, patch, ifUpdatedAt)
}

func (s *Store) ArchiveThread(ctx context.Context, actorID, threadID string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, fmt.Errorf("actor_id is required")
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil, fmt.Errorf("thread_id is required")
	}
	row, err := s.getThreadRow(ctx, threadID, "threads")
	if err != nil {
		return nil, err
	}
	if row.Kind != "thread" {
		return nil, ErrNotFound
	}
	if row.TrashedAt.Valid && strings.TrimSpace(row.TrashedAt.String) != "" {
		return nil, ErrAlreadyTrashed
	}
	if row.ArchivedAt.Valid && strings.TrimSpace(row.ArchivedAt.String) != "" {
		return row.ToThreadMapPublic(ctx, s.db)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.db.ExecContext(ctx,
		`UPDATE threads SET archived_at = ?, archived_by = ? WHERE id = ?`,
		now, actorID, threadID,
	); err != nil {
		return nil, fmt.Errorf("archive thread: %w", err)
	}
	row, err = s.getThreadRow(ctx, threadID, "threads")
	if err != nil {
		return nil, err
	}
	return row.ToThreadMapPublic(ctx, s.db)
}

func (s *Store) UnarchiveThread(ctx context.Context, actorID, threadID string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, fmt.Errorf("actor_id is required")
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil, fmt.Errorf("thread_id is required")
	}
	row, err := s.getThreadRow(ctx, threadID, "threads")
	if err != nil {
		return nil, err
	}
	if row.Kind != "thread" {
		return nil, ErrNotFound
	}
	if !row.ArchivedAt.Valid || strings.TrimSpace(row.ArchivedAt.String) == "" {
		return nil, ErrNotArchived
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE threads SET archived_at = NULL, archived_by = NULL WHERE id = ?`,
		threadID,
	); err != nil {
		return nil, fmt.Errorf("unarchive thread: %w", err)
	}
	row, err = s.getThreadRow(ctx, threadID, "threads")
	if err != nil {
		return nil, err
	}
	return row.ToThreadMapPublic(ctx, s.db)
}

func (s *Store) TrashThread(ctx context.Context, actorID, threadID, reason string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, fmt.Errorf("actor_id is required")
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil, fmt.Errorf("thread_id is required")
	}
	row, err := s.getThreadRow(ctx, threadID, "threads")
	if err != nil {
		return nil, err
	}
	if row.Kind != "thread" {
		return nil, ErrNotFound
	}
	if row.TrashedAt.Valid && strings.TrimSpace(row.TrashedAt.String) != "" {
		return row.ToThreadMapPublic(ctx, s.db)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.db.ExecContext(ctx,
		`UPDATE threads SET trashed_at = ?, trashed_by = ?, trash_reason = ?, archived_at = NULL, archived_by = NULL WHERE id = ?`,
		now, actorID, strings.TrimSpace(reason), threadID,
	); err != nil {
		return nil, fmt.Errorf("trash thread: %w", err)
	}
	row, err = s.getThreadRow(ctx, threadID, "threads")
	if err != nil {
		return nil, err
	}
	return row.ToThreadMapPublic(ctx, s.db)
}

func (s *Store) RestoreThread(ctx context.Context, actorID, threadID string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, fmt.Errorf("actor_id is required")
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil, fmt.Errorf("thread_id is required")
	}
	row, err := s.getThreadRow(ctx, threadID, "threads")
	if err != nil {
		return nil, err
	}
	if row.Kind != "thread" {
		return nil, ErrNotFound
	}
	if !row.TrashedAt.Valid || strings.TrimSpace(row.TrashedAt.String) == "" {
		return nil, ErrNotTrashed
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE threads SET trashed_at = NULL, trashed_by = NULL, trash_reason = NULL WHERE id = ?`,
		threadID,
	); err != nil {
		return nil, fmt.Errorf("restore thread: %w", err)
	}
	row, err = s.getThreadRow(ctx, threadID, "threads")
	if err != nil {
		return nil, err
	}
	return row.ToThreadMapPublic(ctx, s.db)
}

func (s *Store) PurgeThread(ctx context.Context, threadID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("primitives store database is not initialized")
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return fmt.Errorf("thread_id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin purge thread transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var foundID string
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM threads WHERE id = ? AND trashed_at IS NOT NULL`,
		threadID,
	).Scan(&foundID)
	if errors.Is(err, sql.ErrNoRows) {
		var one int
		err2 := tx.QueryRowContext(ctx, `SELECT 1 FROM threads WHERE id = ?`, threadID).Scan(&one)
		if errors.Is(err2, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err2 != nil {
			return fmt.Errorf("check thread existence: %w", err2)
		}
		return ErrNotTrashed
	}
	if err != nil {
		return fmt.Errorf("select trashed thread: %w", err)
	}

	if err := preservePurgedAccess(ctx, tx, "thread", threadID); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM derived_topic_views WHERE thread_id = ?`, threadID); err != nil {
		return fmt.Errorf("delete derived_topic_views: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM derived_inbox_items WHERE thread_id = ?`, threadID); err != nil {
		return fmt.Errorf("delete derived_inbox_items: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM derived_topic_dirty_queue WHERE thread_id = ?`, threadID); err != nil {
		return fmt.Errorf("delete derived_topic_dirty_queue: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM threads WHERE id = ?`, threadID); err != nil {
		return fmt.Errorf("delete thread row: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit purge thread transaction: %w", err)
	}
	return nil
}
