// Backing-thread maintenance for documents: ensuring the backing thread
// exists and keeping its subject in sync with the document title.

package primitives

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func normalizeDocumentBackingThreadID(documentID, threadID string) string {
	threadID = strings.TrimSpace(threadID)
	if threadID != "" {
		return threadID
	}
	return strings.TrimSpace(documentID)
}

func ensureDocumentBackingThreadTx(ctx context.Context, tx *accessTx, actorID, documentID, threadID, title, updatedAt string) (string, error) {
	threadID = normalizeDocumentBackingThreadID(documentID, threadID)
	if threadID == "" {
		return "", invalidDocumentRequest("document.thread_id is required")
	}

	subjectRef := "document:" + strings.TrimSpace(documentID)
	row, err := getThreadRowFromQueryRower(ctx, tx, threadID, "threads")
	if errors.Is(err, ErrNotFound) {
		body := buildDocumentBackingThreadBody(documentID, threadID, title)
		bodyJSON, marshalErr := json.Marshal(body)
		if marshalErr != nil {
			return "", fmt.Errorf("marshal document backing thread: %w", marshalErr)
		}
		threadHandle, handleErr := uniqueHandleTx(ctx, tx, "thread", title, "thread-"+threadID)
		if handleErr != nil {
			return "", fmt.Errorf("allocate document backing thread handle: %w", handleErr)
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
				return "", ErrConflict
			}
			return "", fmt.Errorf("insert document backing thread: %w", execErr)
		}
		if err := replaceRefEdges(ctx, tx, "thread", threadID, typedRefEdgeTargets(refEdgeTypeRef, []string{subjectRef})); err != nil {
			return "", err
		}
		return threadID, nil
	}
	if err != nil {
		return "", err
	}

	threadBody, err := row.ToThreadMap()
	if err != nil {
		return "", err
	}
	existingSubjectRef := threadSubjectRef(threadBody)
	if existingSubjectRef != "" && existingSubjectRef != subjectRef {
		return "", invalidDocumentRequest(fmt.Sprintf("document.thread_id %q is already bound to %q", threadID, existingSubjectRef))
	}

	provenance := cloneProvenance(threadBody["provenance"])
	if len(provenance) == 0 {
		provenance = map[string]any{"sources": []string{"inferred"}}
	}
	existingBody := cloneMap(threadBody)
	threadBody = buildDocumentBackingThreadBody(documentID, threadID, title)
	for _, key := range []string{"current_summary", "key_artifacts"} {
		if value, exists := existingBody[key]; exists && value != nil {
			threadBody[key] = value
		}
	}
	threadBody["provenance"] = provenance

	bodyJSON, err := json.Marshal(threadBody)
	if err != nil {
		return "", fmt.Errorf("marshal document backing thread update: %w", err)
	}
	provenanceJSON, err := json.Marshal(provenance)
	if err != nil {
		return "", fmt.Errorf("marshal document backing thread provenance: %w", err)
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
		return "", fmt.Errorf("update document backing thread: %w", err)
	}
	if err := replaceRefEdges(ctx, tx, "thread", threadID, typedRefEdgeTargets(refEdgeTypeRef, []string{subjectRef})); err != nil {
		return "", err
	}
	return threadID, nil
}

func clearDocumentBackingThreadSubjectTx(ctx context.Context, tx *accessTx, actorID, threadID, documentID, updatedAt string) error {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}
	row, err := getThreadRowFromQueryRower(ctx, tx, threadID, "threads")
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	threadBody, err := row.ToThreadMap()
	if err != nil {
		return err
	}
	if threadSubjectRef(threadBody) != "document:"+strings.TrimSpace(documentID) {
		return nil
	}

	delete(threadBody, "id")
	delete(threadBody, "updated_at")
	delete(threadBody, "updated_by")
	provenance := cloneProvenance(threadBody["provenance"])
	delete(threadBody, "provenance")
	delete(threadBody, "subject_ref")
	if len(provenance) == 0 {
		provenance = map[string]any{"sources": []string{"inferred"}}
	}

	bodyJSON, err := json.Marshal(threadBody)
	if err != nil {
		return fmt.Errorf("marshal cleared document backing thread: %w", err)
	}
	provenanceJSON, err := json.Marshal(provenance)
	if err != nil {
		return fmt.Errorf("marshal cleared document backing thread provenance: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE threads
		    SET updated_at = ?, updated_by = ?, body_json = ?, provenance_json = ?
		  WHERE id = ?`,
		updatedAt,
		actorID,
		string(bodyJSON),
		string(provenanceJSON),
		threadID,
	); err != nil {
		return fmt.Errorf("clear document backing thread subject: %w", err)
	}
	return replaceRefEdges(ctx, tx, "thread", threadID, nil)
}

func buildDocumentBackingThreadBody(documentID, threadID, title string) map[string]any {
	return map[string]any{
		"id":          strings.TrimSpace(threadID),
		"subject_ref": "document:" + strings.TrimSpace(documentID),
		"title":       documentBackingThreadTitle(documentID, title),
		"provenance":  map[string]any{"sources": []string{"inferred"}},
	}
}

func documentBackingThreadTitle(documentID, title string) string {
	title = strings.TrimSpace(title)
	if title != "" {
		return title
	}
	documentID = strings.TrimSpace(documentID)
	if documentID != "" {
		return "Document " + documentID
	}
	return "Document"
}

func threadSubjectRef(threadBody map[string]any) string {
	if threadBody == nil {
		return ""
	}
	return strings.TrimSpace(anyStringValue(threadBody["subject_ref"]))
}
