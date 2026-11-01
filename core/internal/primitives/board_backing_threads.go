// Backing-thread maintenance for boards, cards, and documents referenced from
// boards: ensuring the backing thread exists and keeping its title in sync.

package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func ensureThreadExists(ctx context.Context, rower queryRower, threadID string) error {
	threadRow, err := getThreadRowFromQueryRower(ctx, rower, strings.TrimSpace(threadID), "threads")
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if threadRow.Kind != "thread" {
		return ErrNotFound
	}
	return nil
}

func ensureDocumentExists(ctx context.Context, rower queryRower, documentID string) error {
	documentID = strings.TrimSpace(documentID)
	if documentID == "" {
		return nil
	}

	var found string
	err := rower.QueryRowContext(
		ctx,
		`SELECT id FROM documents WHERE id = ? AND trashed_at IS NULL`,
		documentID,
	).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("query document row: %w", err)
	}
	return nil
}

func loadThreadTitleForBoardCard(ctx context.Context, rower queryRower, threadID string) (string, error) {
	threadRow, err := getThreadRowFromQueryRower(ctx, rower, strings.TrimSpace(threadID), "threads")
	if err != nil {
		return "", err
	}
	body := map[string]any{}
	if strings.TrimSpace(threadRow.BodyJSON) != "" {
		if err := json.Unmarshal([]byte(threadRow.BodyJSON), &body); err != nil {
			return "", fmt.Errorf("decode board card thread body: %w", err)
		}
	}
	title := strings.TrimSpace(anyStringValue(body["title"]))
	if title == "" {
		return strings.TrimSpace(threadID), nil
	}
	return title, nil
}

func ensureBoardBackingThreadTx(ctx context.Context, tx *accessTx, actorID, boardID, threadID, title, updatedAt string) error {
	boardID = strings.TrimSpace(boardID)
	threadID = strings.TrimSpace(threadID)
	title = strings.TrimSpace(title)
	updatedAt = strings.TrimSpace(updatedAt)
	if boardID == "" || threadID == "" {
		return invalidBoardRequest("board thread is required")
	}
	subjectRef := "board:" + boardID

	row, err := getThreadRowFromQueryRower(ctx, tx, threadID, "threads")
	if errors.Is(err, ErrNotFound) {
		body := buildBoardBackingThreadBody(boardID, threadID, title)
		bodyJSON, marshalErr := json.Marshal(body)
		if marshalErr != nil {
			return fmt.Errorf("marshal board backing thread: %w", marshalErr)
		}
		threadHandle, handleErr := uniqueHandleTx(ctx, tx, "thread", title, "thread-"+threadID)
		if handleErr != nil {
			return fmt.Errorf("allocate board backing thread handle: %w", handleErr)
		}
		if _, execErr := tx.ExecContext(
			ctx,
			`INSERT INTO threads(id, handle, kind, thread_id, updated_at, updated_by, body_json, provenance_json)
			 VALUES (?, ?, 'thread', ?, ?, ?, ?, ?)`,
			threadID,
			threadHandle,
			threadID,
			updatedAt,
			actorID,
			string(bodyJSON),
			inferredProvenanceJSON(),
		); execErr != nil {
			if isUniqueViolation(execErr) {
				return ErrConflict
			}
			return fmt.Errorf("insert board backing thread: %w", execErr)
		}
		return replaceRefEdges(ctx, tx, "thread", threadID, typedRefEdgeTargets(refEdgeTypeRef, []string{subjectRef}))
	}
	if err != nil {
		return err
	}

	threadBody, err := row.ToThreadMap()
	if err != nil {
		return err
	}
	existingSubjectRef := threadSubjectRef(threadBody)
	if existingSubjectRef != "" && existingSubjectRef != subjectRef {
		return invalidBoardRequest(fmt.Sprintf("board.thread_id %q is already bound to %q", threadID, existingSubjectRef))
	}

	provenance := cloneProvenance(threadBody["provenance"])
	if len(provenance) == 0 {
		provenance = map[string]any{"sources": []string{"inferred"}}
	}
	threadBody = buildBoardBackingThreadBody(boardID, threadID, title)
	threadBody["provenance"] = provenance

	bodyJSON, err := json.Marshal(threadBody)
	if err != nil {
		return fmt.Errorf("marshal board backing thread update: %w", err)
	}
	provenanceJSON, err := json.Marshal(provenance)
	if err != nil {
		return fmt.Errorf("marshal board backing thread provenance: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE threads
		    SET thread_id = ?, updated_at = ?, updated_by = ?, body_json = ?, provenance_json = ?
		  WHERE id = ?`,
		threadID,
		updatedAt,
		actorID,
		string(bodyJSON),
		string(provenanceJSON),
		threadID,
	); err != nil {
		return fmt.Errorf("update board backing thread: %w", err)
	}
	return replaceRefEdges(ctx, tx, "thread", threadID, typedRefEdgeTargets(refEdgeTypeRef, []string{subjectRef}))
}

func ensureCardBackingThreadTx(ctx context.Context, tx *accessTx, actorID, cardID, threadID, title, updatedAt string) error {
	cardID = strings.TrimSpace(cardID)
	threadID = strings.TrimSpace(threadID)
	title = strings.TrimSpace(title)
	updatedAt = strings.TrimSpace(updatedAt)
	if cardID == "" || threadID == "" {
		return invalidBoardRequest("card thread is required")
	}
	subjectRef := "card:" + cardID

	row, err := getThreadRowFromQueryRower(ctx, tx, threadID, "threads")
	if errors.Is(err, ErrNotFound) {
		body := buildCardBackingThreadBody(cardID, threadID, title)
		bodyJSON, marshalErr := json.Marshal(body)
		if marshalErr != nil {
			return fmt.Errorf("marshal card backing thread: %w", marshalErr)
		}
		threadHandle, handleErr := uniqueHandleTx(ctx, tx, "thread", title, "thread-"+threadID)
		if handleErr != nil {
			return fmt.Errorf("allocate card backing thread handle: %w", handleErr)
		}
		if _, execErr := tx.ExecContext(
			ctx,
			`INSERT INTO threads(id, handle, kind, thread_id, updated_at, updated_by, body_json, provenance_json)
			 VALUES (?, ?, 'thread', ?, ?, ?, ?, ?)`,
			threadID,
			threadHandle,
			threadID,
			updatedAt,
			actorID,
			string(bodyJSON),
			inferredProvenanceJSON(),
		); execErr != nil {
			if isUniqueViolation(execErr) {
				return ErrConflict
			}
			return fmt.Errorf("insert card backing thread: %w", execErr)
		}
		return replaceRefEdges(ctx, tx, "thread", threadID, typedRefEdgeTargets(refEdgeTypeRef, []string{subjectRef}))
	}
	if err != nil {
		return err
	}

	threadBody, err := row.ToThreadMap()
	if err != nil {
		return err
	}
	existingSubjectRef := threadSubjectRef(threadBody)
	if existingSubjectRef != "" && existingSubjectRef != subjectRef {
		return invalidBoardRequest(fmt.Sprintf("card.thread_id %q is already bound to %q", threadID, existingSubjectRef))
	}

	provenance := cloneProvenance(threadBody["provenance"])
	if len(provenance) == 0 {
		provenance = map[string]any{"sources": []string{"inferred"}}
	}
	threadBody = buildCardBackingThreadBody(cardID, threadID, title)
	threadBody["provenance"] = provenance

	bodyJSON, err := json.Marshal(threadBody)
	if err != nil {
		return fmt.Errorf("marshal card backing thread update: %w", err)
	}
	provenanceJSON, err := json.Marshal(provenance)
	if err != nil {
		return fmt.Errorf("marshal card backing thread provenance: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE threads
		    SET thread_id = ?, updated_at = ?, updated_by = ?, body_json = ?, provenance_json = ?
		  WHERE id = ?`,
		threadID,
		updatedAt,
		actorID,
		string(bodyJSON),
		string(provenanceJSON),
		threadID,
	); err != nil {
		return fmt.Errorf("update card backing thread: %w", err)
	}
	return replaceRefEdges(ctx, tx, "thread", threadID, typedRefEdgeTargets(refEdgeTypeRef, []string{subjectRef}))
}

func buildCardBackingThreadBody(cardID, threadID, title string) map[string]any {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Card " + strings.TrimSpace(cardID)
	}
	return map[string]any{
		"id":          strings.TrimSpace(threadID),
		"subject_ref": "card:" + strings.TrimSpace(cardID),
		"title":       title,
		"provenance":  map[string]any{"sources": []string{"inferred"}},
	}
}

func buildBoardBackingThreadBody(boardID, threadID, title string) map[string]any {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Board " + strings.TrimSpace(boardID)
	}
	return map[string]any{
		"id":          strings.TrimSpace(threadID),
		"subject_ref": "board:" + strings.TrimSpace(boardID),
		"title":       title,
		"provenance":  map[string]any{"sources": []string{"inferred"}},
	}
}
