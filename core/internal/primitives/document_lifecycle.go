// Document lifecycle mutations: archive, trash, restore, unarchive, purge,
// the shared lifecycle entry point, and the lifecycle events they emit.

package primitives

import (
	"agent-nexus-core/internal/resourceaccess"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

// ApplyDocumentLifecycle checks the metadata base under the same writer as the
// lifecycle change. Replay callers borrow the writer and quota lock; unkeyed
// token-aware callers get the same serialization without creating a receipt.
func (s *Store) ApplyDocumentLifecycle(ctx context.Context, actorID, documentID, action, reason string, ifUpdatedAt *string) (map[string]any, map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, nil, fmt.Errorf("primitives store database is not initialized")
	}
	if s.quota.enabled() && !replayOwnsQuota(ctx, s) {
		s.quotaMu.Lock()
		defer s.quotaMu.Unlock()
		ctx = context.WithValue(ctx, replayQuotaKey{}, s)
	}
	var document, revision map[string]any
	err := s.db.WriteTransaction(ctx, func(ctx context.Context, tx *resourceaccess.Tx) error {
		// Acquire the writer before reading the base, avoiding deferred read upgrades.
		if _, err := tx.ExecContext(ctx, `UPDATE documents SET updated_at=updated_at WHERE id=?`, documentID); err != nil {
			return err
		}
		current, err := s.loadDocumentRow(ctx, documentID)
		if err != nil {
			return err
		}
		if err := ensureUpdatedAtMatches(current.UpdatedAt, ifUpdatedAt); err != nil {
			return err
		}
		switch action {
		case "archive":
			document, revision, err = s.archiveDocumentIfUpdatedAt(ctx, actorID, documentID, ifUpdatedAt)
		case "unarchive":
			document, revision, err = s.unarchiveDocument(ctx, actorID, documentID)
		case "trash":
			document, revision, err = s.trashDocument(ctx, actorID, documentID, reason)
		case "restore":
			document, revision, err = s.restoreDocument(ctx, actorID, documentID, reason)
		default:
			return invalidDocumentRequest("unsupported lifecycle action")
		}
		return err
	})
	return document, revision, err
}

func (s *Store) TrashDocument(ctx context.Context, actorID string, documentID string, reason string) (map[string]any, map[string]any, error) {
	return s.ApplyDocumentLifecycle(ctx, actorID, documentID, "trash", reason, nil)
}

func (s *Store) trashDocument(ctx context.Context, actorID string, documentID string, reason string) (map[string]any, map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, nil, fmt.Errorf("primitives store database is not initialized")
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, nil, invalidDocumentRequest("actorID is required")
	}
	documentID = strings.TrimSpace(documentID)
	if documentID == "" {
		return nil, nil, invalidDocumentRequest("document_id is required")
	}

	doc, err := s.loadDocumentRow(ctx, documentID)
	if err != nil {
		return nil, nil, err
	}
	if doc.TrashedAt.Valid && strings.TrimSpace(doc.TrashedAt.String) != "" {
		revision, err := s.loadDocumentRevision(ctx, documentID, doc.HeadRevisionID, true)
		if err != nil {
			return nil, nil, err
		}
		docMap, err := doc.toMap()
		if err != nil {
			return nil, nil, err
		}
		return docMap, revision, nil
	}

	revision, err := s.loadDocumentRevision(ctx, documentID, doc.HeadRevisionID, true)
	if err != nil {
		return nil, nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("begin document trash transaction: %w", err)
	}
	_, err = tx.ExecContext(ctx,
		`UPDATE documents SET trashed_at = ?, trashed_by = ?, trash_reason = ?, archived_at = NULL, archived_by = NULL, updated_at = ?, updated_by = ? WHERE id = ?`,
		now, actorID, strings.TrimSpace(reason), now, actorID, documentID,
	)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, nil, fmt.Errorf("trash document: %w", err)
	}

	lifecycleEvent, err := prepareEventForInsert(actorID, buildDocumentLifecycleEvent(
		"document_trashed",
		nullStringValue(doc.ThreadID),
		documentID,
		doc.HeadRevisionID,
		anyStringValue(revision["artifact_id"]),
		asIntValue(revision["revision_number"]),
		nullStringValue(doc.Title),
		map[string]any{
			"reason": strings.TrimSpace(reason),
		},
	))
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, nil, err
	}
	if err := s.checkWorkspaceWriteQuota(ctx, 0, quotaWriteDelta{dbBytes: int64(len(lifecycleEvent.PayloadJSON) + len(lifecycleEvent.RefsJSON) + 512)}, blobLedgerWritePlan{}); err != nil {
		_ = tx.Rollback()
		return nil, nil, err
	}
	if err := insertPreparedEvent(ctx, tx, lifecycleEvent); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, nil, fmt.Errorf("commit document trash transaction: %w", err)
	}

	doc.UpdatedAt, doc.UpdatedBy = now, actorID
	doc.TrashedAt = nullableString(now)
	doc.TrashedBy = nullableString(actorID)
	doc.TrashReason = nullableString(reason)
	doc.ArchivedAt = sql.NullString{}
	doc.ArchivedBy = sql.NullString{}

	docMap, err := doc.toMap()
	if err != nil {
		return nil, nil, err
	}
	return docMap, revision, nil
}

func (s *Store) ArchiveDocument(ctx context.Context, actorID, documentID string) (map[string]any, map[string]any, error) {
	return s.ArchiveDocumentIfUpdatedAt(ctx, actorID, documentID, nil)
}

func (s *Store) ArchiveDocumentIfUpdatedAt(ctx context.Context, actorID, documentID string, ifUpdatedAt *string) (map[string]any, map[string]any, error) {
	return s.ApplyDocumentLifecycle(ctx, actorID, documentID, "archive", "", ifUpdatedAt)
}

func (s *Store) archiveDocumentIfUpdatedAt(ctx context.Context, actorID, documentID string, ifUpdatedAt *string) (map[string]any, map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, nil, fmt.Errorf("primitives store database is not initialized")
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, nil, invalidDocumentRequest("actorID is required")
	}
	documentID = strings.TrimSpace(documentID)
	if documentID == "" {
		return nil, nil, invalidDocumentRequest("document_id is required")
	}

	doc, err := s.loadDocumentRow(ctx, documentID)
	if err != nil {
		return nil, nil, err
	}
	if err := ensureUpdatedAtMatches(doc.UpdatedAt, ifUpdatedAt); err != nil {
		return nil, nil, err
	}
	if doc.TrashedAt.Valid && strings.TrimSpace(doc.TrashedAt.String) != "" {
		return nil, nil, ErrAlreadyTrashed
	}
	if doc.ArchivedAt.Valid && strings.TrimSpace(doc.ArchivedAt.String) != "" {
		revision, err := s.loadDocumentRevision(ctx, documentID, doc.HeadRevisionID, true)
		if err != nil {
			return nil, nil, err
		}
		docMap, err := doc.toMap()
		if err != nil {
			return nil, nil, err
		}
		return docMap, revision, nil
	}

	revision, err := s.loadDocumentRevision(ctx, documentID, doc.HeadRevisionID, true)
	if err != nil {
		return nil, nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	query := `UPDATE documents SET archived_at = ?, archived_by = ?, updated_at = ?, updated_by = ? WHERE id = ?`
	args := []any{now, actorID, now, actorID, documentID}
	if ifUpdatedAt != nil {
		query += ` AND updated_at = ?`
		args = append(args, *ifUpdatedAt)
	}
	var result sql.Result
	result, err = tx.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("archive document: %w", err)
	}
	if ifUpdatedAt != nil {
		rows, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return nil, nil, rowsErr
		}
		if rows != 1 {
			return nil, nil, ErrConflict
		}
	}

	if err := markTopicProjectionsDirtyTx(ctx, tx, []string{doc.ThreadID.String}, time.Now().UTC()); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}

	doc.UpdatedAt, doc.UpdatedBy = now, actorID
	doc.ArchivedAt = nullableString(now)
	doc.ArchivedBy = nullableString(actorID)
	docMap, err := doc.toMap()
	if err != nil {
		return nil, nil, err
	}
	return docMap, revision, nil
}

func (s *Store) UnarchiveDocument(ctx context.Context, actorID, documentID string) (map[string]any, map[string]any, error) {
	return s.ApplyDocumentLifecycle(ctx, actorID, documentID, "unarchive", "", nil)
}

func (s *Store) unarchiveDocument(ctx context.Context, actorID, documentID string) (map[string]any, map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, nil, fmt.Errorf("primitives store database is not initialized")
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, nil, invalidDocumentRequest("actorID is required")
	}
	documentID = strings.TrimSpace(documentID)
	if documentID == "" {
		return nil, nil, invalidDocumentRequest("document_id is required")
	}

	doc, err := s.loadDocumentRow(ctx, documentID)
	if err != nil {
		return nil, nil, err
	}
	if !doc.ArchivedAt.Valid || strings.TrimSpace(doc.ArchivedAt.String) == "" {
		return nil, nil, ErrNotArchived
	}

	revision, err := s.loadDocumentRevision(ctx, documentID, doc.HeadRevisionID, true)
	if err != nil {
		return nil, nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx,
		`UPDATE documents SET archived_at = NULL, archived_by = NULL, updated_at = ?, updated_by = ? WHERE id = ?`,
		now, actorID, documentID,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("unarchive document: %w", err)
	}

	if err := markTopicProjectionsDirtyTx(ctx, tx, []string{doc.ThreadID.String}, time.Now().UTC()); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}

	doc.UpdatedAt, doc.UpdatedBy = now, actorID
	doc.ArchivedAt = sql.NullString{}
	doc.ArchivedBy = sql.NullString{}
	docMap, err := doc.toMap()
	if err != nil {
		return nil, nil, err
	}
	return docMap, revision, nil
}

func (s *Store) RestoreDocument(ctx context.Context, actorID, documentID string, reason string) (map[string]any, map[string]any, error) {
	return s.ApplyDocumentLifecycle(ctx, actorID, documentID, "restore", reason, nil)
}

func (s *Store) restoreDocument(ctx context.Context, actorID, documentID string, reason string) (map[string]any, map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, nil, fmt.Errorf("primitives store database is not initialized")
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, nil, invalidDocumentRequest("actorID is required")
	}
	documentID = strings.TrimSpace(documentID)
	if documentID == "" {
		return nil, nil, invalidDocumentRequest("document_id is required")
	}

	doc, err := s.loadDocumentRow(ctx, documentID)
	if err != nil {
		return nil, nil, err
	}
	if !doc.TrashedAt.Valid || strings.TrimSpace(doc.TrashedAt.String) == "" {
		return nil, nil, ErrNotTrashed
	}

	revision, err := s.loadDocumentRevision(ctx, documentID, doc.HeadRevisionID, true)
	if err != nil {
		return nil, nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("begin document restore transaction: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx,
		`UPDATE documents SET trashed_at = NULL, trashed_by = NULL, trash_reason = NULL, updated_at = ?, updated_by = ? WHERE id = ?`,
		now, actorID, documentID,
	)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, nil, fmt.Errorf("restore document: %w", err)
	}

	lifecycleEvent, err := prepareEventForInsert(actorID, buildDocumentLifecycleEvent(
		"document_restored",
		nullStringValue(doc.ThreadID),
		documentID,
		doc.HeadRevisionID,
		anyStringValue(revision["artifact_id"]),
		asIntValue(revision["revision_number"]),
		nullStringValue(doc.Title),
		map[string]any{
			"reason": strings.TrimSpace(reason),
		},
	))
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, nil, err
	}
	if err := s.checkWorkspaceWriteQuota(ctx, 0, quotaWriteDelta{dbBytes: int64(len(lifecycleEvent.PayloadJSON) + len(lifecycleEvent.RefsJSON) + 512)}, blobLedgerWritePlan{}); err != nil {
		_ = tx.Rollback()
		return nil, nil, err
	}
	if err := insertPreparedEvent(ctx, tx, lifecycleEvent); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, nil, fmt.Errorf("commit document restore transaction: %w", err)
	}

	doc.UpdatedAt, doc.UpdatedBy = now, actorID
	doc.TrashedAt = sql.NullString{}
	doc.TrashedBy = sql.NullString{}
	doc.TrashReason = sql.NullString{}
	docMap, err := doc.toMap()
	if err != nil {
		return nil, nil, err
	}
	return docMap, revision, nil
}

func (s *Store) PurgeDocument(ctx context.Context, documentID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("primitives store database is not initialized")
	}
	documentID = strings.TrimSpace(documentID)
	if documentID == "" {
		return fmt.Errorf("document_id is required")
	}
	if err := validateDocumentID(documentID); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin purge document transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var one int
	err = tx.QueryRowContext(ctx,
		`SELECT 1 FROM documents WHERE id = ? AND trashed_at IS NOT NULL`,
		documentID,
	).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		var exists int
		err2 := tx.QueryRowContext(ctx, `SELECT 1 FROM documents WHERE id = ?`, documentID).Scan(&exists)
		if errors.Is(err2, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err2 != nil {
			return fmt.Errorf("check document existence: %w", err2)
		}
		return ErrNotTrashed
	}
	if err != nil {
		return fmt.Errorf("select trashed document: %w", err)
	}

	if err := preservePurgedAccess(ctx, tx, "document", documentID); err != nil {
		return err
	}

	type ownedRevisionArtifact struct {
		revisionID  string
		artifactID  string
		contentHash string
	}
	rows, err := tx.QueryContext(ctx,
		`SELECT dr.revision_id, dr.artifact_id, a.content_hash
		   FROM document_revisions dr
		   JOIN artifacts a ON a.id = dr.artifact_id
		  WHERE dr.document_id = ?`,
		documentID,
	)
	if err != nil {
		return fmt.Errorf("query document revision artifacts: %w", err)
	}
	owned := make([]ownedRevisionArtifact, 0)
	for rows.Next() {
		var item ownedRevisionArtifact
		if err := rows.Scan(&item.revisionID, &item.artifactID, &item.contentHash); err != nil {
			rows.Close()
			return fmt.Errorf("scan document revision artifact: %w", err)
		}
		item.revisionID = strings.TrimSpace(item.revisionID)
		item.artifactID = strings.TrimSpace(item.artifactID)
		item.contentHash = strings.TrimSpace(item.contentHash)
		owned = append(owned, item)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close document revision artifacts: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate document revision artifacts: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM ref_edges WHERE source_type = ? AND source_id = ?`, "document", documentID); err != nil {
		return fmt.Errorf("delete document source ref edges: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM ref_edges WHERE target_type = ? AND target_id = ?`, "document", documentID); err != nil {
		return fmt.Errorf("delete document target ref edges: %w", err)
	}
	for _, item := range owned {
		if item.revisionID != "" {
			if _, err := tx.ExecContext(ctx, `DELETE FROM ref_edges WHERE source_type = ? AND source_id = ?`, "document_revision", item.revisionID); err != nil {
				return fmt.Errorf("delete document revision source ref edges: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM ref_edges WHERE target_type = ? AND target_id = ?`, "document_revision", item.revisionID); err != nil {
				return fmt.Errorf("delete document revision target ref edges: %w", err)
			}
		}
		if item.artifactID != "" {
			if _, err := tx.ExecContext(ctx, `DELETE FROM ref_edges WHERE source_type = ? AND source_id = ?`, "artifact", item.artifactID); err != nil {
				return fmt.Errorf("delete document artifact source ref edges: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM ref_edges WHERE target_type = ? AND target_id = ?`, "artifact", item.artifactID); err != nil {
				return fmt.Errorf("delete document artifact target ref edges: %w", err)
			}
		}
	}

	// Delete artifacts before their revision rows: a revision delete restores
	// own access edges to an artifact that survives it.
	for _, item := range owned {
		if item.artifactID == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM artifacts WHERE id = ?`, item.artifactID); err != nil {
			return fmt.Errorf("delete document revision artifact: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM document_revisions WHERE document_id = ?`, documentID); err != nil {
		return fmt.Errorf("delete document revisions: %w", err)
	}

	contentHashes := make([]string, 0, len(owned))
	for _, item := range owned {
		contentHashes = append(contentHashes, item.contentHash)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM documents WHERE id = ?`, documentID); err != nil {
		return fmt.Errorf("delete document: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit purge document: %w", err)
	}

	return s.collectUnreferencedBlobs(ctx, contentHashes)
}

func buildDocumentLifecycleEvent(eventType, threadID, documentID, revisionID, artifactID string, revisionNumber int, title string, extraPayload map[string]any) map[string]any {
	refs := []string{
		"document:" + documentID,
		"document_revision:" + revisionID,
	}
	if threadID != "" {
		refs = append(refs, "thread:"+threadID)
	}
	if artifactID != "" {
		refs = append(refs, "artifact:"+artifactID)
	}
	refs = uniqueStrings(refs)
	sortStringsStable(refs)

	payload := map[string]any{
		"document_id":     documentID,
		"revision_id":     revisionID,
		"artifact_id":     artifactID,
		"revision_number": revisionNumber,
	}
	for key, value := range extraPayload {
		payload[key] = value
	}

	label := strings.TrimSpace(title)
	if label == "" {
		label = documentID
	}

	summary := map[string]string{
		"document_created":  "Document created",
		"document_revised":  "Document revised",
		"document_trashed":  "Document trashed",
		"document_restored": "Document restored",
	}[eventType]
	if summary == "" {
		summary = "Document lifecycle event"
	}

	event := map[string]any{
		"type":       eventType,
		"refs":       refs,
		"summary":    summary + ": " + label,
		"payload":    payload,
		"provenance": eventProvenance(),
	}
	if threadID != "" {
		event["thread_id"] = threadID
	}
	return event
}
