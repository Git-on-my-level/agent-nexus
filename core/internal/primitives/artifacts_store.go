// Artifact create and fetch: artifact-plus-event creation, content encoding,
// owner projection, and artifact id validation.

package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/resourceaccess"
)

type ArtifactListFilter struct {
	States []string

	// IDs limits listing to explicit artifact ids (comma-split upstream); capped in query builder.
	IDs           []string
	Q             string
	Limit         *int
	Kind          string
	BackingScope  string
	ThreadID      string
	ThreadIDs     []string
	CreatedBefore string
	CreatedAfter  string
}

func (s *Store) CreateArtifact(ctx context.Context, actorID string, artifact map[string]any, content any, contentType string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	if s.blob == nil {
		return nil, fmt.Errorf("blob backend is not configured")
	}
	if s.quota.enabled() && !replayOwnsQuota(ctx, s) {
		s.quotaMu.Lock()
		defer s.quotaMu.Unlock()
	}

	kind, ok := artifact["kind"].(string)
	if !ok || strings.TrimSpace(kind) == "" {
		return nil, fmt.Errorf("artifact.kind is required")
	}

	refs, err := normalizeStringSlice(artifact["refs"])
	if err != nil {
		return nil, fmt.Errorf("artifact.refs: %w", err)
	}

	encodedContent, err := encodeContent(content, contentType)
	if err != nil {
		return nil, err
	}

	metadata := cloneMap(artifact)
	artifactID, _ := metadata["id"].(string)
	artifactID = strings.TrimSpace(artifactID)
	if artifactID == "" {
		artifactID = uuid.NewString()
	} else if err := validateArtifactID(artifactID); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidArtifactID, err)
	}
	contentHash := sha256Hex(encodedContent)
	blobPlan, err := s.prepareBlobLedgerWritePlan(ctx, contentHash, int64(len(encodedContent)))
	if err != nil {
		return nil, err
	}
	if err := s.checkWorkspaceWriteQuota(ctx, int64(len(encodedContent)), quotaWriteDelta{artifacts: 1}, blobPlan); err != nil {
		return nil, err
	}

	metadata["id"] = artifactID
	metadata["created_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	metadata["created_by"] = actorID
	metadata["content_type"] = contentType
	metadata["content_hash"] = contentHash
	artifactThreadID := firstThreadRefValue(refs)

	stagedContent, err := s.blob.Write(ctx, contentHash, encodedContent)
	if err != nil {
		return nil, fmt.Errorf("stage artifact content: %w", err)
	}
	defer func() { _ = stagedContent.Cleanup() }()
	preload, err := s.preloadStagedBlobs(ctx, stagedBlob{hash: contentHash, write: stagedContent})
	if err != nil {
		return nil, fmt.Errorf("preload artifact content: %w", err)
	}
	defer preload.discardUnlessCommitted()

	refsJSON, err := json.Marshal(refs)
	if err != nil {
		return nil, fmt.Errorf("marshal artifact refs: %w", err)
	}

	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal artifact metadata: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin artifact transaction: %w", err)
	}
	artifactHandle, err := uniqueHandleTx(ctx, tx, "artifact", firstNonEmpty(anyStringValue(metadata["title"]), anyStringValue(metadata["summary"]), kind), "artifact-"+artifactID)
	if err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("allocate artifact handle: %w", err)
	}
	metadata["handle"] = artifactHandle
	metadata["ref"] = "artifact:" + artifactHandle
	metadata["refs"] = publicTypedRefs(ctx, tx, refs)

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO artifacts(id, handle, kind, thread_id, created_at, created_by, content_type, content_hash, refs_json, metadata_json, content_refs_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		metadata["id"],
		artifactHandle,
		kind,
		nullableString(artifactThreadID),
		metadata["created_at"],
		actorID,
		contentType,
		contentHash,
		string(refsJSON),
		string(metadataJSON),
		resourceaccess.ContentReferenceAtomsJSON(string(encodedContent), contentType),
	); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		if isUniqueViolation(err) {
			return nil, ErrConflict
		}
		return nil, fmt.Errorf("insert artifact: %w", err)
	}
	if err := replaceRefEdges(ctx, tx, "artifact", artifactID, typedRefEdgeTargets(refEdgeTypeRef, refs)); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, err
	}

	if err := s.applyBlobLedgerWritePlanTx(ctx, tx, blobPlan); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, err
	}

	if err := stagedContent.Promote(); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, fmt.Errorf("finalize artifact content: %w", err)
	}

	if err := tx.Commit(); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, fmt.Errorf("commit artifact transaction: %w", err)
	}
	preload.markCommitted()

	return metadata, nil
}

func (s *Store) CreateArtifactAndEvent(ctx context.Context, actorID string, artifact map[string]any, content any, contentType string, event map[string]any) (map[string]any, map[string]any, error) {
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

	kind, ok := artifact["kind"].(string)
	if !ok || strings.TrimSpace(kind) == "" {
		return nil, nil, fmt.Errorf("artifact.kind is required")
	}

	artifactRefs, err := normalizeStringSlice(artifact["refs"])
	if err != nil {
		return nil, nil, fmt.Errorf("artifact.refs: %w", err)
	}

	encodedContent, err := encodeContent(content, contentType)
	if err != nil {
		return nil, nil, err
	}

	metadata := cloneMap(artifact)
	artifactID, _ := metadata["id"].(string)
	artifactID = strings.TrimSpace(artifactID)
	if artifactID == "" {
		artifactID = uuid.NewString()
	} else if err := validateArtifactID(artifactID); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrInvalidArtifactID, err)
	}
	contentHash := sha256Hex(encodedContent)
	blobPlan, err := s.prepareBlobLedgerWritePlan(ctx, contentHash, int64(len(encodedContent)))
	if err != nil {
		return nil, nil, err
	}
	if err := s.checkWorkspaceWriteQuota(ctx, int64(len(encodedContent)), quotaWriteDelta{artifacts: 1}, blobPlan); err != nil {
		return nil, nil, err
	}

	metadata["id"] = artifactID
	metadata["created_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	metadata["created_by"] = actorID
	metadata["content_type"] = contentType
	metadata["content_hash"] = contentHash
	artifactThreadID := firstThreadRefValue(artifactRefs)
	if artifactThreadID == "" {
		artifactThreadID = strings.TrimSpace(anyStringValue(event["thread_id"]))
	}

	stagedContent, err := s.blob.Write(ctx, contentHash, encodedContent)
	if err != nil {
		return nil, nil, fmt.Errorf("stage artifact content: %w", err)
	}
	defer func() { _ = stagedContent.Cleanup() }()
	preload, err := s.preloadStagedBlobs(ctx, stagedBlob{hash: contentHash, write: stagedContent})
	if err != nil {
		return nil, nil, fmt.Errorf("preload artifact content: %w", err)
	}
	defer preload.discardUnlessCommitted()

	artifactRefsJSON, err := json.Marshal(artifactRefs)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal artifact refs: %w", err)
	}
	artifactMetadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal artifact metadata: %w", err)
	}

	preparedEvent, err := prepareEventForInsert(actorID, event)
	if err != nil {
		return nil, nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("begin transaction: %w", err)
	}
	artifactHandle, err := uniqueHandleTx(ctx, tx, "artifact", firstNonEmpty(anyStringValue(metadata["title"]), anyStringValue(metadata["summary"]), kind), "artifact-"+artifactID)
	if err != nil {
		_ = tx.Rollback()
		return nil, nil, fmt.Errorf("allocate artifact handle: %w", err)
	}
	metadata["handle"] = artifactHandle
	metadata["ref"] = "artifact:" + artifactHandle
	metadata["refs"] = publicTypedRefs(ctx, tx, artifactRefs)

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO artifacts(id, handle, kind, thread_id, created_at, created_by, content_type, content_hash, refs_json, metadata_json, content_refs_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		metadata["id"],
		artifactHandle,
		kind,
		nullableString(artifactThreadID),
		metadata["created_at"],
		actorID,
		contentType,
		contentHash,
		string(artifactRefsJSON),
		string(artifactMetadataJSON),
		resourceaccess.ContentReferenceAtomsJSON(string(encodedContent), contentType),
	); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		if isUniqueViolation(err) {
			return nil, nil, ErrConflict
		}
		return nil, nil, fmt.Errorf("insert artifact: %w", err)
	}
	if err := replaceRefEdges(ctx, tx, "artifact", artifactID, typedRefEdgeTargets(refEdgeTypeRef, artifactRefs)); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, nil, err
	}

	if err := insertPreparedEvent(ctx, tx, preparedEvent); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, nil, err
	}

	if err := s.applyBlobLedgerWritePlanTx(ctx, tx, blobPlan); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, nil, err
	}

	if err := stagedContent.Promote(); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, nil, fmt.Errorf("finalize artifact content: %w", err)
	}

	if err := tx.Commit(); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, nil, fmt.Errorf("commit transaction: %w", err)
	}
	preload.markCommitted()

	return metadata, preparedEvent.Body, nil
}

func (s *Store) GetArtifact(ctx context.Context, id string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}

	var metadataJSON, handle string
	err := s.db.QueryRowContext(ctx, `SELECT metadata_json, COALESCE(handle, '') FROM artifacts WHERE id = ?`, id).Scan(&metadataJSON, &handle)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query artifact metadata: %w", err)
	}

	metadata, err := decodeArtifactMetadataJSON(metadataJSON)
	if err != nil {
		return nil, err
	}
	publicizeArtifactMetadata(ctx, s.db, metadata, id, handle)
	if owner, err := s.loadArtifactOwner(ctx, s.db, id); err != nil {
		return nil, err
	} else if owner.Kind != "" {
		owner.applyPublic(ctx, s.db, metadata)
	}
	return metadata, nil
}

type artifactOwner struct {
	Kind       string
	ResourceID string
	RevisionID string
}

func (o artifactOwner) apply(metadata map[string]any) {
	if metadata == nil || strings.TrimSpace(o.Kind) == "" {
		return
	}
	metadata["system_owned"] = true
	metadata["owner_ref"] = strings.TrimSpace(o.Kind) + ":" + strings.TrimSpace(o.ResourceID)
	metadata["owner_revision_ref"] = strings.TrimSpace(o.Kind) + "_revision:" + strings.TrimSpace(o.RevisionID)
}

func (o artifactOwner) applyPublic(ctx context.Context, q queryRower, metadata map[string]any) {
	o.apply(metadata)
	if metadata == nil || strings.TrimSpace(o.Kind) == "" {
		return
	}
	metadata["owner_ref"] = makePublicTypedRef(ctx, q, strings.TrimSpace(o.Kind), strings.TrimSpace(o.ResourceID))
	metadata["owner_revision_ref"] = makePublicTypedRef(ctx, q, strings.TrimSpace(o.Kind)+"_revision", strings.TrimSpace(o.RevisionID))
}

func (s *Store) loadArtifactOwner(ctx context.Context, q queryRower, artifactID string) (artifactOwner, error) {
	artifactID = strings.TrimSpace(artifactID)
	if artifactID == "" {
		return artifactOwner{}, nil
	}
	var docID, revisionID string
	err := q.QueryRowContext(ctx,
		`SELECT document_id, revision_id FROM document_revisions WHERE artifact_id = ? LIMIT 1`,
		artifactID,
	).Scan(&docID, &revisionID)
	if err == nil {
		return artifactOwner{Kind: "document", ResourceID: docID, RevisionID: revisionID}, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return artifactOwner{}, fmt.Errorf("query document artifact owner: %w", err)
	}
	err = q.QueryRowContext(ctx,
		`SELECT card_id, revision_id FROM card_revisions WHERE artifact_id = ? LIMIT 1`,
		artifactID,
	).Scan(&docID, &revisionID)
	if err == nil {
		return artifactOwner{Kind: "card", ResourceID: docID, RevisionID: revisionID}, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		if strings.Contains(err.Error(), "no such table: card_revisions") {
			return artifactOwner{}, nil
		}
		return artifactOwner{}, fmt.Errorf("query card artifact owner: %w", err)
	}
	return artifactOwner{}, nil
}

func (s *Store) GetArtifactContent(ctx context.Context, id string) ([]byte, string, error) {
	if s == nil || s.db == nil {
		return nil, "", fmt.Errorf("primitives store database is not initialized")
	}
	if s.blob == nil {
		return nil, "", fmt.Errorf("blob backend is not configured")
	}

	var contentHash string
	var contentType string
	err := s.db.QueryRowContext(ctx, `SELECT content_hash, content_type FROM artifacts WHERE id = ?`, id).Scan(&contentHash, &contentType)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", fmt.Errorf("query artifact content metadata: %w", err)
	}
	if contentHash == "" {
		return nil, "", ErrNotFound
	}

	body, err := s.blob.Read(ctx, contentHash)
	if err != nil {
		if errors.Is(err, blob.ErrBlobNotFound) {
			return nil, "", ErrNotFound
		}
		return nil, "", fmt.Errorf("read artifact content: %w", err)
	}

	return body, contentType, nil
}

func encodeContent(content any, contentType string) ([]byte, error) {
	if err := resourceaccess.ValidateContent(content, contentType); err != nil {
		return nil, err
	}
	switch value := content.(type) {
	case string:
		return []byte(value), nil
	case []byte:
		return value, nil
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("encode artifact content: %w", err)
		}
		return encoded, nil
	}
}

func NormalizeArtifactIDFilter(ids []string, max int) []string {
	if max <= 0 {
		max = 48
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
		if len(out) >= max {
			break
		}
	}
	return out
}

func validateArtifactID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("artifact.id must be non-empty")
	}
	if filepath.IsAbs(id) {
		return fmt.Errorf("artifact.id must not be absolute")
	}
	if id == "." || id == ".." {
		return fmt.Errorf("artifact.id must not be . or ..")
	}
	if strings.Contains(id, "/") || strings.Contains(id, `\`) {
		return fmt.Errorf("artifact.id must not contain path separators")
	}
	return nil
}
