package primitives

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"organization-autorunner-core/internal/blob"
)

type documentRow struct {
	ID              string
	ThreadID        sql.NullString
	Title           sql.NullString
	Slug            sql.NullString
	Status          sql.NullString
	LabelsJSON      string
	SupersedesJSON  string
	HeadRevisionID  string
	HeadRevisionNum int
	CreatedAt       string
	CreatedBy       string
	UpdatedAt       string
	UpdatedBy       string
	TombstonedAt    sql.NullString
	TombstonedBy    sql.NullString
	TombstoneReason sql.NullString
	ArchivedAt      sql.NullString
	ArchivedBy      sql.NullString
	HeadArtifactID  sql.NullString
	HeadContentType sql.NullString
	HeadCreatedAt   sql.NullString
	HeadCreatedBy   sql.NullString
}

func buildListDocumentsQuery(filter DocumentListFilter) (string, []any) {
	query := `SELECT d.id, d.thread_id, d.title, d.slug, d.status, d.labels_json, d.supersedes_json,
		d.head_revision_id, d.head_revision_number, d.created_at, d.created_by, d.updated_at, d.updated_by,
		d.tombstoned_at, d.tombstoned_by, d.tombstone_reason,
		d.archived_at, d.archived_by,
		dr.artifact_id, a.content_type, dr.created_at, dr.created_by
		FROM documents d
		LEFT JOIN document_revisions dr ON dr.revision_id = d.head_revision_id
		LEFT JOIN artifacts a ON a.id = dr.artifact_id`
	conditions := make([]string, 0, 6)
	args := make([]any, 0, 6)
	if threadID := strings.TrimSpace(filter.ThreadID); threadID != "" {
		conditions = append(conditions, "d.thread_id = ?")
		args = append(args, threadID)
	}
	if filter.TombstonedOnly {
		conditions = append(conditions, "d.tombstoned_at IS NOT NULL")
	} else if !filter.IncludeTombstoned {
		conditions = append(conditions, "d.tombstoned_at IS NULL")
	}
	if filter.ArchivedOnly {
		conditions = append(conditions, "d.archived_at IS NOT NULL AND d.tombstoned_at IS NULL")
	} else if !filter.IncludeArchived {
		conditions = append(conditions, "d.archived_at IS NULL")
	}
	if q := strings.TrimSpace(filter.Query); q != "" {
		searchPattern := "%" + strings.ToLower(q) + "%"
		conditions = append(conditions, "(LOWER(d.id) LIKE ? OR LOWER(d.title) LIKE ?)")
		args = append(args, searchPattern, searchPattern)
	}
	if len(conditions) > 0 {
		query += ` WHERE ` + strings.Join(conditions, ` AND `)
	}
	query += ` ORDER BY d.updated_at DESC, d.id ASC`
	if filter.Limit != nil && *filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, *filter.Limit+1)
		if filter.Cursor != "" {
			if offset, err := decodeCursor(filter.Cursor); err == nil && offset > 0 {
				query += ` OFFSET ?`
				args = append(args, offset)
			}
		}
	}
	return query, args
}

func (s *Store) ListDocuments(ctx context.Context, filter DocumentListFilter) ([]map[string]any, string, error) {
	if s == nil || s.db == nil {
		return nil, "", fmt.Errorf("primitives store database is not initialized")
	}
	if filter.Cursor != "" {
		if _, err := decodeCursor(filter.Cursor); err != nil {
			return nil, "", fmt.Errorf("%w: %v", ErrInvalidCursor, err)
		}
	}

	query, args := buildListDocumentsQuery(filter)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("query documents: %w", err)
	}
	defer rows.Close()

	documents := make([]map[string]any, 0)
	for rows.Next() {
		var row documentRow
		if err := rows.Scan(
			&row.ID,
			&row.ThreadID,
			&row.Title,
			&row.Slug,
			&row.Status,
			&row.LabelsJSON,
			&row.SupersedesJSON,
			&row.HeadRevisionID,
			&row.HeadRevisionNum,
			&row.CreatedAt,
			&row.CreatedBy,
			&row.UpdatedAt,
			&row.UpdatedBy,
			&row.TombstonedAt,
			&row.TombstonedBy,
			&row.TombstoneReason,
			&row.ArchivedAt,
			&row.ArchivedBy,
			&row.HeadArtifactID,
			&row.HeadContentType,
			&row.HeadCreatedAt,
			&row.HeadCreatedBy,
		); err != nil {
			return nil, "", fmt.Errorf("scan document row: %w", err)
		}
		documents = append(documents, row.toMap())
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate document rows: %w", err)
	}

	var nextCursor string
	if filter.Limit != nil && len(documents) > *filter.Limit {
		documents = documents[:*filter.Limit]
		offset := 0
		if filter.Cursor != "" {
			offset, _ = decodeCursor(filter.Cursor)
		}
		nextCursor = encodeCursor(offset + *filter.Limit)
	}

	return documents, nextCursor, nil
}

func (s *Store) CreateDocument(ctx context.Context, actorID string, document map[string]any, content any, contentType string, refs []string) (map[string]any, map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, nil, fmt.Errorf("primitives store database is not initialized")
	}
	if s.blob == nil {
		return nil, nil, fmt.Errorf("blob backend is not configured")
	}
	if s.quota.enabled() {
		s.quotaMu.Lock()
		defer s.quotaMu.Unlock()
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, nil, invalidDocumentRequest("actorID is required")
	}
	if document == nil {
		return nil, nil, invalidDocumentRequest("document is required")
	}
	contentType, err := normalizeDocumentContentType(contentType)
	if err != nil {
		return nil, nil, invalidDocumentRequestError(err)
	}

	documentID := strings.TrimSpace(anyStringValue(document["document_id"]))
	if documentID == "" {
		documentID = strings.TrimSpace(anyStringValue(document["id"]))
	}
	if documentID == "" {
		documentID = uuid.NewString()
	}
	if err := validateDocumentID(documentID); err != nil {
		return nil, nil, invalidDocumentRequestError(err)
	}

	threadID, err := optionalStringField(document, "thread_id")
	if err != nil {
		return nil, nil, err
	}
	title, err := optionalStringField(document, "title")
	if err != nil {
		return nil, nil, err
	}
	slug, err := optionalStringField(document, "slug")
	if err != nil {
		return nil, nil, err
	}
	status, err := optionalStringField(document, "status")
	if err != nil {
		return nil, nil, err
	}
	labels, err := optionalStringListField(document, "labels")
	if err != nil {
		return nil, nil, invalidDocumentRequestError(err)
	}
	supersedes, err := optionalStringListField(document, "supersedes")
	if err != nil {
		return nil, nil, invalidDocumentRequestError(err)
	}

	encodedContent, err := encodeContent(content)
	if err != nil {
		return nil, nil, invalidDocumentRequestError(err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	revisionNumber := 1
	artifactID := uuid.NewString()
	revisionID := artifactID
	contentHash := sha256Hex(encodedContent)
	blobPlan, err := s.prepareBlobLedgerWritePlan(ctx, contentHash, int64(len(encodedContent)))
	if err != nil {
		return nil, nil, err
	}
	if err := s.checkWorkspaceWriteQuota(ctx, int64(len(encodedContent)), quotaWriteDelta{artifacts: 1, documents: 1, revisions: 1}, blobPlan); err != nil {
		return nil, nil, err
	}

	revisionRefs := append([]string(nil), refs...)
	if threadID != "" {
		revisionRefs = append(revisionRefs, "thread:"+threadID)
	}
	revisionRefs = uniqueStrings(revisionRefs)
	sortStringsStable(revisionRefs)
	threadID = documentLifecycleThreadID(threadID, revisionRefs)

	artifactMetadata := map[string]any{
		"id":               artifactID,
		"kind":             "doc",
		"created_at":       now,
		"created_by":       actorID,
		"content_type":     contentType,
		"content_hash":     contentHash,
		"refs":             revisionRefs,
		"document_id":      documentID,
		"revision_id":      revisionID,
		"revision_number":  revisionNumber,
		"prev_revision_id": nil,
	}
	if title != "" {
		artifactMetadata["summary"] = title
	}

	stagedContent, err := s.blob.Write(ctx, contentHash, encodedContent)
	if err != nil {
		return nil, nil, fmt.Errorf("stage document content: %w", err)
	}
	defer func() { _ = stagedContent.Cleanup() }()

	revisionHash := computeRevisionHash(contentHash, "", documentID, revisionNumber, now, actorID)

	refsJSON, err := json.Marshal(revisionRefs)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal document refs: %w", err)
	}
	artifactMetadataJSON, err := json.Marshal(artifactMetadata)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal document artifact metadata: %w", err)
	}
	labelsJSON, err := json.Marshal(labels)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal document labels: %w", err)
	}
	supersedesJSON, err := json.Marshal(supersedes)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal document supersedes: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("begin document create transaction: %w", err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO artifacts(id, kind, thread_id, created_at, created_by, content_type, content_hash, refs_json, metadata_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		artifactID,
		"doc",
		nullableString(threadID),
		now,
		actorID,
		contentType,
		contentHash,
		string(refsJSON),
		string(artifactMetadataJSON),
	); err != nil {
		_ = tx.Rollback()
		if isUniqueViolation(err) {
			return nil, nil, ErrConflict
		}
		return nil, nil, fmt.Errorf("insert document artifact: %w", err)
	}
	if err := replaceRefEdges(ctx, tx, "artifact", artifactID, typedRefEdgeTargets(refEdgeTypeRef, revisionRefs)); err != nil {
		_ = tx.Rollback()
		return nil, nil, err
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO documents(
			id, thread_id, title, slug, status, labels_json, supersedes_json,
			head_revision_id, head_revision_number,
			created_at, created_by, updated_at, updated_by
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		documentID,
		nullableString(threadID),
		nullableString(title),
		nullableString(slug),
		nullableString(status),
		string(labelsJSON),
		string(supersedesJSON),
		revisionID,
		revisionNumber,
		now,
		actorID,
		now,
		actorID,
	); err != nil {
		_ = tx.Rollback()
		if isUniqueViolation(err) {
			return nil, nil, ErrConflict
		}
		return nil, nil, fmt.Errorf("insert document: %w", err)
	}
	documentTargets := appendRefEdgeTarget(nil, refEdgeTypeDocumentThread, "thread", threadID)
	if err := replaceRefEdges(ctx, tx, "document", documentID, documentTargets); err != nil {
		_ = tx.Rollback()
		return nil, nil, err
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO document_revisions(
			revision_id, document_id, revision_number, prev_revision_id, artifact_id, thread_id, refs_json, revision_hash, created_at, created_by
		) VALUES (?, ?, ?, NULL, ?, ?, ?, ?, ?, ?)`,
		revisionID,
		documentID,
		revisionNumber,
		artifactID,
		nullableString(threadID),
		string(refsJSON),
		revisionHash,
		now,
		actorID,
	); err != nil {
		_ = tx.Rollback()
		if isUniqueViolation(err) {
			return nil, nil, ErrConflict
		}
		return nil, nil, fmt.Errorf("insert document revision: %w", err)
	}
	if err := replaceRefEdges(ctx, tx, "document_revision", revisionID, typedRefEdgeTargets(refEdgeTypeRef, revisionRefs)); err != nil {
		_ = tx.Rollback()
		return nil, nil, err
	}

	lifecycleEvent, err := prepareEventForInsert(actorID, buildDocumentLifecycleEvent(
		"document_created",
		documentLifecycleThreadID(threadID, revisionRefs),
		documentID,
		revisionID,
		artifactID,
		revisionNumber,
		title,
		nil,
	))
	if err != nil {
		_ = tx.Rollback()
		return nil, nil, err
	}
	if err := insertPreparedEvent(ctx, tx, lifecycleEvent); err != nil {
		_ = tx.Rollback()
		return nil, nil, err
	}

	if err := s.applyBlobLedgerWritePlanTx(ctx, tx, blobPlan); err != nil {
		_ = tx.Rollback()
		return nil, nil, err
	}

	if err := stagedContent.Promote(); err != nil {
		_ = tx.Rollback()
		return nil, nil, fmt.Errorf("finalize document content: %w", err)
	}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return nil, nil, fmt.Errorf("commit document create transaction: %w", err)
	}

	docMap := documentRow{
		ID:              documentID,
		ThreadID:        nullableString(threadID),
		Title:           nullableString(title),
		Slug:            nullableString(slug),
		Status:          nullableString(status),
		LabelsJSON:      string(labelsJSON),
		SupersedesJSON:  string(supersedesJSON),
		HeadRevisionID:  revisionID,
		HeadRevisionNum: revisionNumber,
		CreatedAt:       now,
		CreatedBy:       actorID,
		UpdatedAt:       now,
		UpdatedBy:       actorID,
	}.toMap()

	revisionMap := map[string]any{
		"document_id":      documentID,
		"revision_id":      revisionID,
		"artifact_id":      artifactID,
		"revision_number":  revisionNumber,
		"prev_revision_id": nil,
		"thread_id":        nullableMapValue(threadID),
		"refs":             revisionRefs,
		"created_at":       now,
		"created_by":       actorID,
		"content_type":     contentType,
		"content_hash":     contentHash,
		"revision_hash":    revisionHash,
		"artifact":         artifactMetadata,
	}
	setDocumentContentValue(revisionMap, encodedContent, contentType)
	return docMap, revisionMap, nil
}

func (s *Store) GetDocument(ctx context.Context, documentID string) (map[string]any, map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, nil, fmt.Errorf("primitives store database is not initialized")
	}
	doc, err := s.loadDocumentRow(ctx, documentID)
	if err != nil {
		return nil, nil, err
	}
	revision, err := s.loadDocumentRevision(ctx, documentID, doc.HeadRevisionID, true)
	if err != nil {
		return nil, nil, err
	}
	return doc.toMap(), revision, nil
}

func (s *Store) UpdateDocument(ctx context.Context, actorID string, documentID string, documentPatch map[string]any, ifBaseRevision string, content any, contentType string, refs []string) (map[string]any, map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, nil, fmt.Errorf("primitives store database is not initialized")
	}
	if s.blob == nil {
		return nil, nil, fmt.Errorf("blob backend is not configured")
	}
	if s.quota.enabled() {
		s.quotaMu.Lock()
		defer s.quotaMu.Unlock()
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, nil, invalidDocumentRequest("actorID is required")
	}
	ifBaseRevision = strings.TrimSpace(ifBaseRevision)
	if ifBaseRevision == "" {
		return nil, nil, invalidDocumentRequest("if_base_revision is required")
	}
	contentType, err := normalizeDocumentContentType(contentType)
	if err != nil {
		return nil, nil, invalidDocumentRequestError(err)
	}
	if err := validateDocumentID(documentID); err != nil {
		return nil, nil, invalidDocumentRequestError(err)
	}

	doc, err := s.loadDocumentRow(ctx, documentID)
	if err != nil {
		return nil, nil, err
	}
	if doc.HeadRevisionID != ifBaseRevision {
		return nil, nil, ErrConflict
	}

	nextThreadID := nullStringValue(doc.ThreadID)
	nextTitle := nullStringValue(doc.Title)
	nextSlug := nullStringValue(doc.Slug)
	nextStatus := nullStringValue(doc.Status)
	nextLabels := decodeJSONListOrEmpty(doc.LabelsJSON)
	nextSupersedes := decodeJSONListOrEmpty(doc.SupersedesJSON)

	if documentPatch != nil {
		if _, exists := documentPatch["id"]; exists {
			return nil, nil, invalidDocumentRequest("document.id cannot be patched")
		}
		if _, exists := documentPatch["document_id"]; exists {
			return nil, nil, invalidDocumentRequest("document.document_id cannot be patched")
		}
		if value, exists := documentPatch["thread_id"]; exists {
			parsed := strings.TrimSpace(anyStringValue(value))
			nextThreadID = parsed
		}
		if value, exists := documentPatch["title"]; exists {
			nextTitle = strings.TrimSpace(anyStringValue(value))
		}
		if value, exists := documentPatch["slug"]; exists {
			nextSlug = strings.TrimSpace(anyStringValue(value))
		}
		if value, exists := documentPatch["status"]; exists {
			nextStatus = strings.TrimSpace(anyStringValue(value))
		}
		if value, exists := documentPatch["labels"]; exists {
			parsed, parseErr := normalizeStringSlice(value)
			if parseErr != nil {
				return nil, nil, invalidDocumentRequest("document.labels must be a list of strings")
			}
			nextLabels = parsed
		}
		if value, exists := documentPatch["supersedes"]; exists {
			parsed, parseErr := normalizeStringSlice(value)
			if parseErr != nil {
				return nil, nil, invalidDocumentRequest("document.supersedes must be a list of strings")
			}
			nextSupersedes = parsed
		}
	}

	encodedContent, err := encodeContent(content)
	if err != nil {
		return nil, nil, invalidDocumentRequestError(err)
	}

	nextRevisionNumber := doc.HeadRevisionNum + 1
	artifactID := uuid.NewString()
	revisionID := artifactID
	now := time.Now().UTC().Format(time.RFC3339Nano)
	contentHash := sha256Hex(encodedContent)
	blobPlan, err := s.prepareBlobLedgerWritePlan(ctx, contentHash, int64(len(encodedContent)))
	if err != nil {
		return nil, nil, err
	}
	if err := s.checkWorkspaceWriteQuota(ctx, int64(len(encodedContent)), quotaWriteDelta{artifacts: 1, revisions: 1}, blobPlan); err != nil {
		return nil, nil, err
	}

	revisionRefs := append([]string(nil), refs...)
	if nextThreadID != "" {
		revisionRefs = append(revisionRefs, "thread:"+nextThreadID)
	}
	revisionRefs = append(revisionRefs, "artifact:"+doc.HeadRevisionID)
	revisionRefs = uniqueStrings(revisionRefs)
	sortStringsStable(revisionRefs)
	nextThreadID = documentLifecycleThreadID(nextThreadID, revisionRefs)

	artifactMetadata := map[string]any{
		"id":               artifactID,
		"kind":             "doc",
		"created_at":       now,
		"created_by":       actorID,
		"content_type":     contentType,
		"content_hash":     contentHash,
		"refs":             revisionRefs,
		"document_id":      documentID,
		"revision_id":      revisionID,
		"revision_number":  nextRevisionNumber,
		"prev_revision_id": doc.HeadRevisionID,
	}
	if nextTitle != "" {
		artifactMetadata["summary"] = nextTitle
	}

	stagedContent, err := s.blob.Write(ctx, contentHash, encodedContent)
	if err != nil {
		return nil, nil, fmt.Errorf("stage document content: %w", err)
	}
	defer func() { _ = stagedContent.Cleanup() }()

	var prevRevisionHash string
	err = s.db.QueryRowContext(ctx,
		`SELECT revision_hash FROM document_revisions WHERE document_id = ? AND revision_id = ?`,
		documentID, doc.HeadRevisionID,
	).Scan(&prevRevisionHash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, fmt.Errorf("load previous revision hash: %w", err)
	}

	revisionHash := computeRevisionHash(contentHash, prevRevisionHash, documentID, nextRevisionNumber, now, actorID)

	refsJSON, err := json.Marshal(revisionRefs)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal document refs: %w", err)
	}
	artifactMetadataJSON, err := json.Marshal(artifactMetadata)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal document artifact metadata: %w", err)
	}
	labelsJSON, err := json.Marshal(nextLabels)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal document labels: %w", err)
	}
	supersedesJSON, err := json.Marshal(nextSupersedes)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal document supersedes: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("begin document update transaction: %w", err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO artifacts(id, kind, thread_id, created_at, created_by, content_type, content_hash, refs_json, metadata_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		artifactID,
		"doc",
		nullableString(nextThreadID),
		now,
		actorID,
		contentType,
		contentHash,
		string(refsJSON),
		string(artifactMetadataJSON),
	); err != nil {
		_ = tx.Rollback()
		if isUniqueViolation(err) {
			return nil, nil, ErrConflict
		}
		return nil, nil, fmt.Errorf("insert document artifact: %w", err)
	}
	if err := replaceRefEdges(ctx, tx, "artifact", artifactID, typedRefEdgeTargets(refEdgeTypeRef, revisionRefs)); err != nil {
		_ = tx.Rollback()
		return nil, nil, err
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO document_revisions(
			revision_id, document_id, revision_number, prev_revision_id, artifact_id, thread_id, refs_json, revision_hash, created_at, created_by
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		revisionID,
		documentID,
		nextRevisionNumber,
		doc.HeadRevisionID,
		artifactID,
		nullableString(nextThreadID),
		string(refsJSON),
		revisionHash,
		now,
		actorID,
	); err != nil {
		_ = tx.Rollback()
		if isUniqueViolation(err) {
			return nil, nil, ErrConflict
		}
		return nil, nil, fmt.Errorf("insert document revision: %w", err)
	}
	if err := replaceRefEdges(ctx, tx, "document_revision", revisionID, typedRefEdgeTargets(refEdgeTypeRef, revisionRefs)); err != nil {
		_ = tx.Rollback()
		return nil, nil, err
	}

	lifecycleEvent, err := prepareEventForInsert(actorID, buildDocumentLifecycleEvent(
		"document_updated",
		documentLifecycleThreadID(nextThreadID, revisionRefs),
		documentID,
		revisionID,
		artifactID,
		nextRevisionNumber,
		nextTitle,
		map[string]any{
			"prev_revision_id": doc.HeadRevisionID,
		},
	))
	if err != nil {
		_ = tx.Rollback()
		return nil, nil, err
	}
	if err := insertPreparedEvent(ctx, tx, lifecycleEvent); err != nil {
		_ = tx.Rollback()
		return nil, nil, err
	}

	result, err := tx.ExecContext(
		ctx,
		`UPDATE documents SET
			thread_id = ?,
			title = ?,
			slug = ?,
			status = ?,
			labels_json = ?,
			supersedes_json = ?,
			head_revision_id = ?,
			head_revision_number = ?,
			updated_at = ?,
			updated_by = ?
		 WHERE id = ? AND head_revision_id = ?`,
		nullableString(nextThreadID),
		nullableString(nextTitle),
		nullableString(nextSlug),
		nullableString(nextStatus),
		string(labelsJSON),
		string(supersedesJSON),
		revisionID,
		nextRevisionNumber,
		now,
		actorID,
		documentID,
		ifBaseRevision,
	)
	if err != nil {
		_ = tx.Rollback()
		return nil, nil, fmt.Errorf("update document head: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		_ = tx.Rollback()
		return nil, nil, fmt.Errorf("read document head update result: %w", err)
	}
	if affected == 0 {
		_ = tx.Rollback()
		return nil, nil, ErrConflict
	}
	documentTargets := appendRefEdgeTarget(nil, refEdgeTypeDocumentThread, "thread", nextThreadID)
	if err := replaceRefEdges(ctx, tx, "document", documentID, documentTargets); err != nil {
		_ = tx.Rollback()
		return nil, nil, err
	}

	if err := s.applyBlobLedgerWritePlanTx(ctx, tx, blobPlan); err != nil {
		_ = tx.Rollback()
		return nil, nil, err
	}

	if err := stagedContent.Promote(); err != nil {
		_ = tx.Rollback()
		return nil, nil, fmt.Errorf("finalize document content: %w", err)
	}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return nil, nil, fmt.Errorf("commit document update transaction: %w", err)
	}

	docMap := documentRow{
		ID:              documentID,
		ThreadID:        nullableString(nextThreadID),
		Title:           nullableString(nextTitle),
		Slug:            nullableString(nextSlug),
		Status:          nullableString(nextStatus),
		LabelsJSON:      string(labelsJSON),
		SupersedesJSON:  string(supersedesJSON),
		HeadRevisionID:  revisionID,
		HeadRevisionNum: nextRevisionNumber,
		CreatedAt:       doc.CreatedAt,
		CreatedBy:       doc.CreatedBy,
		UpdatedAt:       now,
		UpdatedBy:       actorID,
	}.toMap()

	revisionMap := map[string]any{
		"document_id":      documentID,
		"revision_id":      revisionID,
		"artifact_id":      artifactID,
		"revision_number":  nextRevisionNumber,
		"prev_revision_id": doc.HeadRevisionID,
		"thread_id":        nullableMapValue(nextThreadID),
		"refs":             revisionRefs,
		"created_at":       now,
		"created_by":       actorID,
		"content_type":     contentType,
		"content_hash":     contentHash,
		"revision_hash":    revisionHash,
		"artifact":         artifactMetadata,
	}
	setDocumentContentValue(revisionMap, encodedContent, contentType)
	return docMap, revisionMap, nil
}

func (s *Store) ListDocumentHistory(ctx context.Context, documentID string) ([]map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	if err := validateDocumentID(documentID); err != nil {
		return nil, err
	}
	if _, err := s.loadDocumentRow(ctx, documentID); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `SELECT revision_id FROM document_revisions WHERE document_id = ? ORDER BY revision_number ASC`, documentID)
	if err != nil {
		return nil, fmt.Errorf("query document history: %w", err)
	}
	defer rows.Close()

	history := make([]map[string]any, 0)
	for rows.Next() {
		var revisionID string
		if err := rows.Scan(&revisionID); err != nil {
			return nil, fmt.Errorf("scan document history row: %w", err)
		}
		revision, err := s.loadDocumentRevision(ctx, documentID, revisionID, false)
		if err != nil {
			return nil, err
		}
		history = append(history, revision)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate document history rows: %w", err)
	}

	return history, nil
}

func (s *Store) GetDocumentRevision(ctx context.Context, documentID string, revisionID string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	return s.loadDocumentRevision(ctx, documentID, revisionID, true)
}

func (s *Store) GetDocumentRevisionByID(ctx context.Context, revisionID string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}

	revisionID = strings.TrimSpace(revisionID)
	if revisionID == "" {
		return nil, fmt.Errorf("revision_id is required")
	}

	var documentID string
	err := s.db.QueryRowContext(
		ctx,
		`SELECT document_id FROM document_revisions WHERE revision_id = ?`,
		revisionID,
	).Scan(&documentID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query document revision by id: %w", err)
	}

	return s.loadDocumentRevision(ctx, documentID, revisionID, false)
}

func (s *Store) TombstoneDocument(ctx context.Context, actorID string, documentID string, reason string) (map[string]any, map[string]any, error) {
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
	if doc.TombstonedAt.Valid && strings.TrimSpace(doc.TombstonedAt.String) != "" {
		revision, err := s.loadDocumentRevision(ctx, documentID, doc.HeadRevisionID, true)
		if err != nil {
			return nil, nil, err
		}
		return doc.toMap(), revision, nil
	}

	revision, err := s.loadDocumentRevision(ctx, documentID, doc.HeadRevisionID, true)
	if err != nil {
		return nil, nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("begin document tombstone transaction: %w", err)
	}
	_, err = tx.ExecContext(ctx,
		`UPDATE documents SET tombstoned_at = ?, tombstoned_by = ?, tombstone_reason = ?, archived_at = NULL, archived_by = NULL WHERE id = ?`,
		now, actorID, strings.TrimSpace(reason), documentID,
	)
	if err != nil {
		_ = tx.Rollback()
		return nil, nil, fmt.Errorf("tombstone document: %w", err)
	}

	lifecycleEvent, err := prepareEventForInsert(actorID, buildDocumentLifecycleEvent(
		"document_tombstoned",
		documentLifecycleThreadID(nullStringValue(doc.ThreadID), revisionRefsFromRevision(revision)),
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
		_ = tx.Rollback()
		return nil, nil, err
	}
	if err := insertPreparedEvent(ctx, tx, lifecycleEvent); err != nil {
		_ = tx.Rollback()
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return nil, nil, fmt.Errorf("commit document tombstone transaction: %w", err)
	}

	doc.TombstonedAt = nullableString(now)
	doc.TombstonedBy = nullableString(actorID)
	doc.TombstoneReason = nullableString(reason)
	doc.ArchivedAt = sql.NullString{}
	doc.ArchivedBy = sql.NullString{}

	return doc.toMap(), revision, nil
}

func (s *Store) ArchiveDocument(ctx context.Context, actorID, documentID string) (map[string]any, map[string]any, error) {
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
	if doc.TombstonedAt.Valid && strings.TrimSpace(doc.TombstonedAt.String) != "" {
		return nil, nil, ErrAlreadyTombstoned
	}
	if doc.ArchivedAt.Valid && strings.TrimSpace(doc.ArchivedAt.String) != "" {
		revision, err := s.loadDocumentRevision(ctx, documentID, doc.HeadRevisionID, true)
		if err != nil {
			return nil, nil, err
		}
		return doc.toMap(), revision, nil
	}

	revision, err := s.loadDocumentRevision(ctx, documentID, doc.HeadRevisionID, true)
	if err != nil {
		return nil, nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx,
		`UPDATE documents SET archived_at = ?, archived_by = ? WHERE id = ?`,
		now, actorID, documentID,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("archive document: %w", err)
	}

	doc.ArchivedAt = nullableString(now)
	doc.ArchivedBy = nullableString(actorID)
	return doc.toMap(), revision, nil
}

func (s *Store) UnarchiveDocument(ctx context.Context, actorID, documentID string) (map[string]any, map[string]any, error) {
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

	_, err = s.db.ExecContext(ctx,
		`UPDATE documents SET archived_at = NULL, archived_by = NULL WHERE id = ?`,
		documentID,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("unarchive document: %w", err)
	}

	doc.ArchivedAt = sql.NullString{}
	doc.ArchivedBy = sql.NullString{}
	return doc.toMap(), revision, nil
}

func (s *Store) RestoreDocument(ctx context.Context, actorID, documentID string, reason string) (map[string]any, map[string]any, error) {
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
	if !doc.TombstonedAt.Valid || strings.TrimSpace(doc.TombstonedAt.String) == "" {
		return nil, nil, ErrNotTombstoned
	}

	revision, err := s.loadDocumentRevision(ctx, documentID, doc.HeadRevisionID, true)
	if err != nil {
		return nil, nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("begin document restore transaction: %w", err)
	}
	_, err = tx.ExecContext(ctx,
		`UPDATE documents SET tombstoned_at = NULL, tombstoned_by = NULL, tombstone_reason = NULL WHERE id = ?`,
		documentID,
	)
	if err != nil {
		_ = tx.Rollback()
		return nil, nil, fmt.Errorf("restore document: %w", err)
	}

	lifecycleEvent, err := prepareEventForInsert(actorID, buildDocumentLifecycleEvent(
		"document_restored",
		documentLifecycleThreadID(nullStringValue(doc.ThreadID), revisionRefsFromRevision(revision)),
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
		_ = tx.Rollback()
		return nil, nil, err
	}
	if err := insertPreparedEvent(ctx, tx, lifecycleEvent); err != nil {
		_ = tx.Rollback()
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return nil, nil, fmt.Errorf("commit document restore transaction: %w", err)
	}

	doc.TombstonedAt = sql.NullString{}
	doc.TombstonedBy = sql.NullString{}
	doc.TombstoneReason = sql.NullString{}
	return doc.toMap(), revision, nil
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
		`SELECT 1 FROM documents WHERE id = ? AND tombstoned_at IS NOT NULL`,
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
		return ErrNotTombstoned
	}
	if err != nil {
		return fmt.Errorf("select tombstoned document: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM document_revisions WHERE document_id = ?`, documentID); err != nil {
		return fmt.Errorf("delete document revisions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM documents WHERE id = ?`, documentID); err != nil {
		return fmt.Errorf("delete document: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit purge document: %w", err)
	}
	return nil
}

func (s *Store) loadDocumentRow(ctx context.Context, documentID string) (documentRow, error) {
	documentID = strings.TrimSpace(documentID)
	if err := validateDocumentID(documentID); err != nil {
		return documentRow{}, err
	}

	var row documentRow
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, thread_id, title, slug, status, labels_json, supersedes_json,
			 head_revision_id, head_revision_number, created_at, created_by, updated_at, updated_by,
			 tombstoned_at, tombstoned_by, tombstone_reason,
			 archived_at, archived_by
		 FROM documents WHERE id = ?`,
		documentID,
	).Scan(
		&row.ID,
		&row.ThreadID,
		&row.Title,
		&row.Slug,
		&row.Status,
		&row.LabelsJSON,
		&row.SupersedesJSON,
		&row.HeadRevisionID,
		&row.HeadRevisionNum,
		&row.CreatedAt,
		&row.CreatedBy,
		&row.UpdatedAt,
		&row.UpdatedBy,
		&row.TombstonedAt,
		&row.TombstonedBy,
		&row.TombstoneReason,
		&row.ArchivedAt,
		&row.ArchivedBy,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return documentRow{}, ErrNotFound
	}
	if err != nil {
		return documentRow{}, fmt.Errorf("query document: %w", err)
	}
	return row, nil
}

func (s *Store) loadDocumentRevision(ctx context.Context, documentID string, revisionID string, includeContent bool) (map[string]any, error) {
	documentID = strings.TrimSpace(documentID)
	revisionID = strings.TrimSpace(revisionID)
	if err := validateDocumentID(documentID); err != nil {
		return nil, err
	}
	if revisionID == "" {
		return nil, fmt.Errorf("revision_id is required")
	}

	var (
		outDocumentID    string
		outRevisionID    string
		revisionNumber   int
		prevRevisionID   sql.NullString
		artifactID       string
		threadID         sql.NullString
		refsJSON         string
		revisionHashVal  string
		createdAt        string
		createdBy        string
		artifactMetaJSON string
		contentType      string
		contentHash      string
	)

	err := s.db.QueryRowContext(
		ctx,
		`SELECT dr.document_id, dr.revision_id, dr.revision_number, dr.prev_revision_id, dr.artifact_id, dr.thread_id, dr.refs_json, dr.revision_hash, dr.created_at, dr.created_by,
		        a.metadata_json, a.content_type, a.content_hash
		 FROM document_revisions dr
		 JOIN artifacts a ON a.id = dr.artifact_id
		 WHERE dr.document_id = ? AND dr.revision_id = ?`,
		documentID,
		revisionID,
	).Scan(
		&outDocumentID,
		&outRevisionID,
		&revisionNumber,
		&prevRevisionID,
		&artifactID,
		&threadID,
		&refsJSON,
		&revisionHashVal,
		&createdAt,
		&createdBy,
		&artifactMetaJSON,
		&contentType,
		&contentHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query document revision: %w", err)
	}

	refs := decodeJSONListOrEmpty(refsJSON)
	artifact, err := decodeArtifactMetadataJSON(artifactMetaJSON)
	if err != nil {
		return nil, fmt.Errorf("decode document revision artifact: %w", err)
	}

	revision := map[string]any{
		"document_id":     outDocumentID,
		"revision_id":     outRevisionID,
		"artifact_id":     artifactID,
		"revision_number": revisionNumber,
		"refs":            refs,
		"created_at":      createdAt,
		"created_by":      createdBy,
		"content_type":    contentType,
		"revision_hash":   revisionHashVal,
		"artifact":        artifact,
	}
	if contentHash != "" {
		revision["content_hash"] = contentHash
	}
	if prevRevisionID.Valid && strings.TrimSpace(prevRevisionID.String) != "" {
		revision["prev_revision_id"] = prevRevisionID.String
	}
	if threadID.Valid && strings.TrimSpace(threadID.String) != "" {
		revision["thread_id"] = threadID.String
	}

	if includeContent {
		if s.blob == nil {
			return nil, fmt.Errorf("blob backend is not configured")
		}
		contentBytes, err := s.blob.Read(ctx, contentHash)
		if err != nil {
			if errors.Is(err, blob.ErrBlobNotFound) {
				return nil, ErrNotFound
			}
			return nil, fmt.Errorf("read document revision content: %w", err)
		}
		setDocumentContentValue(revision, contentBytes, contentType)
	}

	return revision, nil
}

func (r documentRow) toMap() map[string]any {
	out := map[string]any{
		"id":                   r.ID,
		"labels":               decodeJSONListOrEmpty(r.LabelsJSON),
		"supersedes":           decodeJSONListOrEmpty(r.SupersedesJSON),
		"head_revision_id":     r.HeadRevisionID,
		"head_revision_number": r.HeadRevisionNum,
		"created_at":           r.CreatedAt,
		"created_by":           r.CreatedBy,
		"updated_at":           r.UpdatedAt,
		"updated_by":           r.UpdatedBy,
	}
	headRevision := map[string]any{
		"revision_id":     r.HeadRevisionID,
		"revision_number": r.HeadRevisionNum,
	}
	if r.HeadArtifactID.Valid && strings.TrimSpace(r.HeadArtifactID.String) != "" {
		headRevision["artifact_id"] = r.HeadArtifactID.String
	}
	if r.HeadContentType.Valid && strings.TrimSpace(r.HeadContentType.String) != "" {
		headRevision["content_type"] = r.HeadContentType.String
	}
	if r.HeadCreatedAt.Valid && strings.TrimSpace(r.HeadCreatedAt.String) != "" {
		headRevision["created_at"] = r.HeadCreatedAt.String
	}
	if r.HeadCreatedBy.Valid && strings.TrimSpace(r.HeadCreatedBy.String) != "" {
		headRevision["created_by"] = r.HeadCreatedBy.String
	}
	out["head_revision"] = headRevision
	if r.ThreadID.Valid && strings.TrimSpace(r.ThreadID.String) != "" {
		out["thread_id"] = r.ThreadID.String
	}
	if r.Title.Valid && strings.TrimSpace(r.Title.String) != "" {
		out["title"] = r.Title.String
	}
	if r.Slug.Valid && strings.TrimSpace(r.Slug.String) != "" {
		out["slug"] = r.Slug.String
	}
	if r.Status.Valid && strings.TrimSpace(r.Status.String) != "" {
		out["status"] = r.Status.String
	}
	if r.TombstonedAt.Valid && strings.TrimSpace(r.TombstonedAt.String) != "" {
		out["tombstoned_at"] = r.TombstonedAt.String
	}
	if r.TombstonedBy.Valid && strings.TrimSpace(r.TombstonedBy.String) != "" {
		out["tombstoned_by"] = r.TombstonedBy.String
	}
	if r.TombstoneReason.Valid && strings.TrimSpace(r.TombstoneReason.String) != "" {
		out["tombstone_reason"] = r.TombstoneReason.String
	}
	if r.ArchivedAt.Valid && strings.TrimSpace(r.ArchivedAt.String) != "" {
		out["archived_at"] = r.ArchivedAt.String
	}
	if r.ArchivedBy.Valid && strings.TrimSpace(r.ArchivedBy.String) != "" {
		out["archived_by"] = r.ArchivedBy.String
	}
	return out
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
		"document_created":    "Document created",
		"document_updated":    "Document updated",
		"document_tombstoned": "Document tombstoned",
		"document_restored":   "Document restored",
	}[eventType]
	if summary == "" {
		summary = "Document lifecycle event"
	}

	event := map[string]any{
		"type":       eventType,
		"refs":       refs,
		"summary":    summary + ": " + label,
		"payload":    payload,
		"provenance": actorStatementProvenance(),
	}
	if threadID != "" {
		event["thread_id"] = threadID
	}
	return event
}

func documentLifecycleThreadID(primaryThreadID string, refs []string) string {
	primaryThreadID = strings.TrimSpace(primaryThreadID)
	if primaryThreadID != "" {
		return primaryThreadID
	}

	for _, ref := range refs {
		if !strings.HasPrefix(ref, "thread:") {
			continue
		}
		threadID := strings.TrimSpace(strings.TrimPrefix(ref, "thread:"))
		if threadID != "" {
			return threadID
		}
	}

	return ""
}

func revisionRefsFromRevision(revision map[string]any) []string {
	refs, err := normalizeStringSlice(revision["refs"])
	if err != nil {
		return nil
	}
	return refs
}

func asIntValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func validateDocumentID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("document_id is required")
	}
	if filepath.IsAbs(id) {
		return fmt.Errorf("document_id must not be absolute")
	}
	if id == "." || id == ".." {
		return fmt.Errorf("document_id must not be . or ..")
	}
	if strings.Contains(id, "/") || strings.Contains(id, `\\`) {
		return fmt.Errorf("document_id must not contain path separators")
	}
	return nil
}

func normalizeDocumentContentType(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	switch raw {
	case "text", "structured", "binary":
		return raw, nil
	default:
		return "", fmt.Errorf("content_type must be one of: text, structured, binary")
	}
}

func optionalStringField(values map[string]any, key string) (string, error) {
	raw, exists := values[key]
	if !exists || raw == nil {
		return "", nil
	}
	text := strings.TrimSpace(anyStringValue(raw))
	if text == "" {
		return "", nil
	}
	return text, nil
}

func optionalStringListField(values map[string]any, key string) ([]string, error) {
	raw, exists := values[key]
	if !exists || raw == nil {
		return []string{}, nil
	}
	parsed, err := normalizeStringSlice(raw)
	if err != nil {
		return nil, fmt.Errorf("document.%s must be a list of strings", key)
	}
	return parsed, nil
}

func anyStringValue(raw any) string {
	text, _ := raw.(string)
	return strings.TrimSpace(text)
}

func decodeJSONListOrEmpty(raw string) []string {
	values := make([]string, 0)
	if strings.TrimSpace(raw) == "" {
		return values
	}
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return []string{}
	}
	return values
}

func nullableString(raw string) sql.NullString {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: raw, Valid: true}
}

func nullStringValue(raw sql.NullString) string {
	if !raw.Valid {
		return ""
	}
	return strings.TrimSpace(raw.String)
}

func nullableMapValue(raw string) any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	return raw
}

func setDocumentContentValue(out map[string]any, content []byte, contentType string) {
	switch strings.TrimSpace(contentType) {
	case "structured":
		var parsed any
		if err := json.Unmarshal(content, &parsed); err != nil {
			out["content"] = string(content)
			return
		}
		out["content"] = parsed
	case "binary":
		out["content_base64"] = base64.StdEncoding.EncodeToString(content)
	default:
		out["content"] = string(content)
	}
}

func sortStringsStable(values []string) {
	if len(values) < 2 {
		return
	}
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j-1] > values[j]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(text, "unique constraint") || strings.Contains(text, "constraint failed")
}

func invalidDocumentRequest(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidDocumentRequest, strings.TrimSpace(message))
}

func invalidDocumentRequestError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrInvalidDocumentRequest, strings.TrimSpace(err.Error()))
}
