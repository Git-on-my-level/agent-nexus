// Document revision reads: history listing, revision fetch by id, revision
// loading and public projection, and revision ref extraction.

package primitives

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"agent-nexus-core/internal/blob"
)

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
		documentHandle   string
		artifactMetaJSON string
		contentType      string
		contentHash      string
		artifactHandle   sql.NullString
	)

	err := s.db.QueryRowContext(
		ctx,
		`SELECT dr.document_id, dr.revision_id, dr.revision_number, dr.prev_revision_id, dr.artifact_id, dr.thread_id, dr.refs_json, dr.revision_hash, dr.created_at, dr.created_by,
		        COALESCE(d.handle, d.id, dr.document_id), a.metadata_json, a.content_type, a.content_hash, a.handle
		 FROM document_revisions dr
		 LEFT JOIN documents d ON d.id = dr.document_id
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
		&documentHandle,
		&artifactMetaJSON,
		&contentType,
		&contentHash,
		&artifactHandle,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query document revision: %w", err)
	}

	refs, err := decodeStoredJSONList(refsJSON, "document_revision.refs")
	if err != nil {
		return nil, err
	}
	artifact, err := decodeArtifactMetadataJSON(artifactMetaJSON)
	if err != nil {
		return nil, fmt.Errorf("decode document revision artifact: %w", err)
	}

	publicRevisionHandle := revisionHandle(documentHandle, revisionNumber)
	publicArtifactHandle := firstNonEmpty(strings.TrimSpace(artifactHandle.String), artifactID)
	publicizeArtifactMetadata(ctx, s.db, artifact, artifactID, publicArtifactHandle)
	revision := map[string]any{
		"ref":             "document_revision:" + publicRevisionHandle,
		"handle":          publicRevisionHandle,
		"document_id":     outDocumentID,
		"document_ref":    "document:" + documentHandle,
		"revision_id":     outRevisionID,
		"artifact_id":     artifactID,
		"artifact_ref":    "artifact:" + publicArtifactHandle,
		"revision_number": revisionNumber,
		"refs":            publicTypedRefs(ctx, s.db, refs),
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
		revision["prev_revision_ref"] = makePublicTypedRef(ctx, s.db, "document_revision", strings.TrimSpace(prevRevisionID.String))
	}
	if threadID.Valid && strings.TrimSpace(threadID.String) != "" {
		revision["thread_id"] = threadID.String
		revision["thread_ref"] = makePublicTypedRef(ctx, s.db, "thread", threadID.String)
	}
	if summary := strings.TrimSpace(anyStringValue(artifact["summary"])); summary != "" {
		revision["summary"] = summary
	}
	if raw, ok := artifact["provenance"]; ok {
		revision["provenance"] = cloneProvenance(raw)
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

func canonicalDocumentSubjectRef(threadID string) string {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return ""
	}
	return "thread:" + threadID
}

func applyDocumentRevisionAliases(revision map[string]any) map[string]any {
	if revision == nil {
		return nil
	}
	if documentID := strings.TrimSpace(anyStringValue(revision["document_id"])); documentID != "" {
		revision["document_ref"] = "document:" + documentID
	}
	if artifactID := strings.TrimSpace(anyStringValue(revision["artifact_id"])); artifactID != "" {
		revision["artifact_ref"] = "artifact:" + artifactID
	}
	if prevRevisionID := strings.TrimSpace(anyStringValue(revision["prev_revision_id"])); prevRevisionID != "" {
		revision["prev_revision_ref"] = "document_revision:" + prevRevisionID
	}
	if summary := strings.TrimSpace(anyStringValue(revision["summary"])); summary != "" {
		revision["summary"] = summary
	} else if artifact, _ := revision["artifact"].(map[string]any); artifact != nil {
		if summary := strings.TrimSpace(anyStringValue(artifact["summary"])); summary != "" {
			revision["summary"] = summary
		}
	}
	return revision
}

func publicizeDocumentRevisionMap(ctx context.Context, q queryRower, revision map[string]any) {
	if revision == nil {
		return
	}
	revisionID := strings.TrimSpace(anyStringValue(revision["revision_id"]))
	if revisionID != "" {
		if resolved, err := resolveRevisionResourceRef(ctx, q, "document_revision", revisionID); err == nil {
			revision["handle"] = resolved.Handle
			revision["ref"] = resolved.CanonicalRef
		}
	}
	if documentID := strings.TrimSpace(anyStringValue(revision["document_id"])); documentID != "" {
		revision["document_ref"] = makePublicTypedRef(ctx, q, "document", documentID)
	}
	if artifactID := strings.TrimSpace(anyStringValue(revision["artifact_id"])); artifactID != "" {
		revision["artifact_ref"] = makePublicTypedRef(ctx, q, "artifact", artifactID)
		if artifact, _ := revision["artifact"].(map[string]any); artifact != nil {
			publicizeArtifactMetadata(ctx, q, artifact, artifactID, "")
		}
	}
	if prevRevisionID := strings.TrimSpace(anyStringValue(revision["prev_revision_id"])); prevRevisionID != "" {
		revision["prev_revision_ref"] = makePublicTypedRef(ctx, q, "document_revision", prevRevisionID)
	}
	if refs, err := normalizeStringSlice(revision["refs"]); err == nil {
		revision["refs"] = publicTypedRefs(ctx, q, refs)
	}
	if threadID := strings.TrimSpace(anyStringValue(revision["thread_id"])); threadID != "" {
		revision["thread_ref"] = makePublicTypedRef(ctx, q, "thread", threadID)
	}
}

func revisionRefsFromRevision(revision map[string]any) []string {
	refs, err := normalizeStringSlice(revision["refs"])
	if err != nil {
		return nil
	}
	return refs
}
