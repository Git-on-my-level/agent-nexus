package primitives

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
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
}

func (s *Store) CreateDocument(ctx context.Context, actorID string, document map[string]any, content any, contentType string, refs []string) (map[string]any, map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, nil, fmt.Errorf("primitives store database is not initialized")
	}
	if strings.TrimSpace(s.artifactContentDir) == "" {
		return nil, nil, fmt.Errorf("artifact content directory is not configured")
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

	revisionRefs := append([]string(nil), refs...)
	if threadID != "" {
		revisionRefs = append(revisionRefs, "thread:"+threadID)
	}
	revisionRefs = uniqueStrings(revisionRefs)
	sortStringsStable(revisionRefs)

	artifactMetadata := map[string]any{
		"id":               artifactID,
		"kind":             "doc",
		"created_at":       now,
		"created_by":       actorID,
		"content_type":     contentType,
		"content_path":     filepath.Join(s.artifactContentDir, artifactID),
		"refs":             revisionRefs,
		"document_id":      documentID,
		"revision_id":      revisionID,
		"revision_number":  revisionNumber,
		"prev_revision_id": nil,
	}
	if title != "" {
		artifactMetadata["summary"] = title
	}
	contentPath := artifactMetadata["content_path"].(string)

	file, err := os.OpenFile(contentPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("create document content file: %w", err)
	}
	if _, err := file.Write(encodedContent); err != nil {
		_ = file.Close()
		_ = os.Remove(contentPath)
		return nil, nil, fmt.Errorf("write document content: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(contentPath)
		return nil, nil, fmt.Errorf("close document content file: %w", err)
	}

	refsJSON, err := json.Marshal(revisionRefs)
	if err != nil {
		_ = os.Remove(contentPath)
		return nil, nil, fmt.Errorf("marshal document refs: %w", err)
	}
	artifactMetadataJSON, err := json.Marshal(artifactMetadata)
	if err != nil {
		_ = os.Remove(contentPath)
		return nil, nil, fmt.Errorf("marshal document artifact metadata: %w", err)
	}
	labelsJSON, err := json.Marshal(labels)
	if err != nil {
		_ = os.Remove(contentPath)
		return nil, nil, fmt.Errorf("marshal document labels: %w", err)
	}
	supersedesJSON, err := json.Marshal(supersedes)
	if err != nil {
		_ = os.Remove(contentPath)
		return nil, nil, fmt.Errorf("marshal document supersedes: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		_ = os.Remove(contentPath)
		return nil, nil, fmt.Errorf("begin document create transaction: %w", err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO artifacts(id, kind, created_at, created_by, content_type, content_path, refs_json, metadata_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		artifactID,
		"doc",
		now,
		actorID,
		contentType,
		contentPath,
		string(refsJSON),
		string(artifactMetadataJSON),
	); err != nil {
		_ = tx.Rollback()
		_ = os.Remove(contentPath)
		if isUniqueViolation(err) {
			return nil, nil, ErrConflict
		}
		return nil, nil, fmt.Errorf("insert document artifact: %w", err)
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
		_ = os.Remove(contentPath)
		if isUniqueViolation(err) {
			return nil, nil, ErrConflict
		}
		return nil, nil, fmt.Errorf("insert document: %w", err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO document_revisions(
			revision_id, document_id, revision_number, prev_revision_id, artifact_id, thread_id, refs_json, created_at, created_by
		) VALUES (?, ?, ?, NULL, ?, ?, ?, ?, ?)`,
		revisionID,
		documentID,
		revisionNumber,
		artifactID,
		nullableString(threadID),
		string(refsJSON),
		now,
		actorID,
	); err != nil {
		_ = tx.Rollback()
		_ = os.Remove(contentPath)
		if isUniqueViolation(err) {
			return nil, nil, ErrConflict
		}
		return nil, nil, fmt.Errorf("insert document revision: %w", err)
	}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		_ = os.Remove(contentPath)
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
	if strings.TrimSpace(s.artifactContentDir) == "" {
		return nil, nil, fmt.Errorf("artifact content directory is not configured")
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

	revisionRefs := append([]string(nil), refs...)
	if nextThreadID != "" {
		revisionRefs = append(revisionRefs, "thread:"+nextThreadID)
	}
	revisionRefs = append(revisionRefs, "artifact:"+doc.HeadRevisionID)
	revisionRefs = uniqueStrings(revisionRefs)
	sortStringsStable(revisionRefs)

	artifactMetadata := map[string]any{
		"id":               artifactID,
		"kind":             "doc",
		"created_at":       now,
		"created_by":       actorID,
		"content_type":     contentType,
		"content_path":     filepath.Join(s.artifactContentDir, artifactID),
		"refs":             revisionRefs,
		"document_id":      documentID,
		"revision_id":      revisionID,
		"revision_number":  nextRevisionNumber,
		"prev_revision_id": doc.HeadRevisionID,
	}
	if nextTitle != "" {
		artifactMetadata["summary"] = nextTitle
	}
	contentPath := artifactMetadata["content_path"].(string)

	file, err := os.OpenFile(contentPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("create document content file: %w", err)
	}
	if _, err := file.Write(encodedContent); err != nil {
		_ = file.Close()
		_ = os.Remove(contentPath)
		return nil, nil, fmt.Errorf("write document content: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(contentPath)
		return nil, nil, fmt.Errorf("close document content file: %w", err)
	}

	refsJSON, err := json.Marshal(revisionRefs)
	if err != nil {
		_ = os.Remove(contentPath)
		return nil, nil, fmt.Errorf("marshal document refs: %w", err)
	}
	artifactMetadataJSON, err := json.Marshal(artifactMetadata)
	if err != nil {
		_ = os.Remove(contentPath)
		return nil, nil, fmt.Errorf("marshal document artifact metadata: %w", err)
	}
	labelsJSON, err := json.Marshal(nextLabels)
	if err != nil {
		_ = os.Remove(contentPath)
		return nil, nil, fmt.Errorf("marshal document labels: %w", err)
	}
	supersedesJSON, err := json.Marshal(nextSupersedes)
	if err != nil {
		_ = os.Remove(contentPath)
		return nil, nil, fmt.Errorf("marshal document supersedes: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		_ = os.Remove(contentPath)
		return nil, nil, fmt.Errorf("begin document update transaction: %w", err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO artifacts(id, kind, created_at, created_by, content_type, content_path, refs_json, metadata_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		artifactID,
		"doc",
		now,
		actorID,
		contentType,
		contentPath,
		string(refsJSON),
		string(artifactMetadataJSON),
	); err != nil {
		_ = tx.Rollback()
		_ = os.Remove(contentPath)
		if isUniqueViolation(err) {
			return nil, nil, ErrConflict
		}
		return nil, nil, fmt.Errorf("insert document artifact: %w", err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO document_revisions(
			revision_id, document_id, revision_number, prev_revision_id, artifact_id, thread_id, refs_json, created_at, created_by
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		revisionID,
		documentID,
		nextRevisionNumber,
		doc.HeadRevisionID,
		artifactID,
		nullableString(nextThreadID),
		string(refsJSON),
		now,
		actorID,
	); err != nil {
		_ = tx.Rollback()
		_ = os.Remove(contentPath)
		if isUniqueViolation(err) {
			return nil, nil, ErrConflict
		}
		return nil, nil, fmt.Errorf("insert document revision: %w", err)
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
		_ = os.Remove(contentPath)
		return nil, nil, fmt.Errorf("update document head: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		_ = tx.Rollback()
		_ = os.Remove(contentPath)
		return nil, nil, fmt.Errorf("read document head update result: %w", err)
	}
	if affected == 0 {
		_ = tx.Rollback()
		_ = os.Remove(contentPath)
		return nil, nil, ErrConflict
	}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		_ = os.Remove(contentPath)
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

func (s *Store) loadDocumentRow(ctx context.Context, documentID string) (documentRow, error) {
	documentID = strings.TrimSpace(documentID)
	if err := validateDocumentID(documentID); err != nil {
		return documentRow{}, err
	}

	var row documentRow
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, thread_id, title, slug, status, labels_json, supersedes_json,
			 head_revision_id, head_revision_number, created_at, created_by, updated_at, updated_by
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
		createdAt        string
		createdBy        string
		artifactMetaJSON string
		contentType      string
		contentPath      string
	)

	err := s.db.QueryRowContext(
		ctx,
		`SELECT dr.document_id, dr.revision_id, dr.revision_number, dr.prev_revision_id, dr.artifact_id, dr.thread_id, dr.refs_json, dr.created_at, dr.created_by,
		        a.metadata_json, a.content_type, a.content_path
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
		&createdAt,
		&createdBy,
		&artifactMetaJSON,
		&contentType,
		&contentPath,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query document revision: %w", err)
	}

	refs := decodeJSONListOrEmpty(refsJSON)
	var artifact map[string]any
	if err := json.Unmarshal([]byte(artifactMetaJSON), &artifact); err != nil {
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
		"artifact":        artifact,
	}
	if prevRevisionID.Valid && strings.TrimSpace(prevRevisionID.String) != "" {
		revision["prev_revision_id"] = prevRevisionID.String
	}
	if threadID.Valid && strings.TrimSpace(threadID.String) != "" {
		revision["thread_id"] = threadID.String
	}

	if includeContent {
		contentBytes, err := os.ReadFile(contentPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
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
	return out
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
