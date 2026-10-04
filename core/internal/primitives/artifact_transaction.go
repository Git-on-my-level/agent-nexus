package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"agent-nexus-core/internal/blob"
)

// preparedArtifactWrite stages content before a caller's transaction and then
// records its metadata, refs, and blob ledger entry inside that transaction.
// Promote happens before commit: a crash may leave an unreferenced blob, but
// cannot leave a committed artifact row pointing at a missing blob.
type preparedArtifactWrite struct {
	actorID          string
	kind             string
	artifactID       string
	artifactThreadID string
	contentType      string
	contentHash      string
	refs             []string
	refsJSON         []byte
	metadata         map[string]any
	metadataJSON     []byte
	blobPlan         blobLedgerWritePlan
	stagedContent    blob.StagedWrite
}

func (s *Store) prepareArtifactWrite(ctx context.Context, actorID string, artifact map[string]any, content any, contentType string) (*preparedArtifactWrite, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	if s.blob == nil {
		return nil, fmt.Errorf("blob backend is not configured")
	}
	kind, ok := artifact["kind"].(string)
	if !ok || strings.TrimSpace(kind) == "" {
		return nil, fmt.Errorf("artifact.kind is required")
	}
	refs, err := normalizeStringSlice(artifact["refs"])
	if err != nil {
		return nil, fmt.Errorf("artifact.refs: %w", err)
	}
	encodedContent, err := encodeContent(content)
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
	refsJSON, err := json.Marshal(refs)
	if err != nil {
		return nil, fmt.Errorf("marshal artifact refs: %w", err)
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal artifact metadata: %w", err)
	}
	stagedContent, err := s.blob.Write(ctx, contentHash, encodedContent)
	if err != nil {
		return nil, fmt.Errorf("stage artifact content: %w", err)
	}
	return &preparedArtifactWrite{
		actorID: actorID, kind: kind, artifactID: artifactID,
		artifactThreadID: firstThreadRefValue(refs), contentType: contentType,
		contentHash: contentHash, refs: refs, refsJSON: refsJSON,
		metadata: metadata, metadataJSON: metadataJSON,
		blobPlan: blobPlan, stagedContent: stagedContent,
	}, nil
}

func (s *Store) insertPreparedArtifactTx(ctx context.Context, tx *sql.Tx, prepared *preparedArtifactWrite) error {
	handle, err := uniqueHandleTx(ctx, tx, "artifact", firstNonEmpty(anyStringValue(prepared.metadata["title"]), anyStringValue(prepared.metadata["summary"]), prepared.kind), "artifact-"+prepared.artifactID)
	if err != nil {
		return fmt.Errorf("allocate artifact handle: %w", err)
	}
	prepared.metadata["handle"] = handle
	prepared.metadata["ref"] = "artifact:" + handle
	prepared.metadata["refs"] = publicTypedRefs(ctx, tx, prepared.refs)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO artifacts(id, handle, kind, thread_id, created_at, created_by, content_type, content_hash, refs_json, metadata_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		prepared.artifactID, handle, prepared.kind, nullableString(prepared.artifactThreadID),
		prepared.metadata["created_at"], prepared.actorID, prepared.contentType, prepared.contentHash,
		string(prepared.refsJSON), string(prepared.metadataJSON)); err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("insert artifact: %w", err)
	}
	if err := replaceRefEdges(ctx, tx, "artifact", prepared.artifactID, typedRefEdgeTargets(refEdgeTypeRef, prepared.refs)); err != nil {
		return err
	}
	if err := s.applyBlobLedgerWritePlanTx(ctx, tx, prepared.blobPlan); err != nil {
		return err
	}
	if err := prepared.stagedContent.Promote(); err != nil {
		return fmt.Errorf("finalize artifact content: %w", err)
	}
	return nil
}
