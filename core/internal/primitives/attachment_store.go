package primitives

import (
	"agent-nexus-core/internal/resourceaccess"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"agent-nexus-core/internal/blob"
)

// DefaultAttachmentMaxUploadBytes is the default per-request attachment payload ceiling (50 MiB).
const DefaultAttachmentMaxUploadBytes int64 = 50 << 20

var (
	// ErrAttachmentMimeNotAllowed is returned when the declared or sniffed MIME is not permitted.
	ErrAttachmentMimeNotAllowed = errors.New("attachment mime type is not allowed")
	// ErrAttachmentFileMissing is returned when multipart upload has no file part.
	ErrAttachmentFileMissing = errors.New("attachment file field is required")
)

// AllowAttachmentMIME reports whether uploads are permitted for the given MIME type (parameters ignored).
func AllowAttachmentMIME(mimeType string) bool {
	base := strings.TrimSpace(strings.Split(mimeType, ";")[0])
	base = strings.ToLower(base)
	if base == "" {
		return false
	}
	if strings.HasPrefix(base, "image/") {
		return true
	}
	switch base {
	case "text/plain", "text/markdown", "text/x-markdown", "text/csv", "application/pdf", "application/json":
		return true
	default:
		return false
	}
}

func sanitizeOriginalFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	base := filepath.Base(name)
	if len(base) > 255 {
		base = base[:255]
	}
	return base
}

func ChooseAttachmentMIME(declared string, detected string) (string, error) {
	declared = strings.TrimSpace(strings.Split(declared, ";")[0])
	detected = strings.TrimSpace(strings.Split(detected, ";")[0])

	if AllowAttachmentMIME(declared) {
		return strings.ToLower(declared), nil
	}
	if AllowAttachmentMIME(detected) {
		return strings.ToLower(detected), nil
	}
	if declared != "" || detected != "" {
		return "", fmt.Errorf("%w: declared %q detected %q", ErrAttachmentMimeNotAllowed, declared, detected)
	}
	return "", fmt.Errorf("%w: empty content type", ErrAttachmentMimeNotAllowed)
}

func attachmentPreferInline(mimeType string) bool {
	base := strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
	if base == "image/svg+xml" {
		return false
	}
	if strings.HasPrefix(base, "image/") {
		return true
	}
	switch base {
	case "text/plain", "text/markdown", "text/x-markdown", "text/csv":
		return true
	}
	return false
}

// ArtifactContentHTTP carries decoded blob bytes plus renderer-facing response metadata.
type ArtifactContentHTTP struct {
	Body               []byte
	ContentType        string
	ContentDisposition string
	ETag               string
	LastModified       string
	ContentLength      int64
	CacheControl       string
	DBContentType      string // artifacts.content_type column (legacy callers / JSON)
}

// GetArtifactContentHTTP loads artifact bytes and HTTP response hints for GET /artifacts/{id}/content.
func (s *Store) GetArtifactContentHTTP(ctx context.Context, id string) (ArtifactContentHTTP, error) {
	var out ArtifactContentHTTP
	if s == nil || s.db == nil {
		return out, fmt.Errorf("primitives store database is not initialized")
	}
	if s.blob == nil {
		return out, fmt.Errorf("blob backend is not configured")
	}

	var (
		kind         string
		contentType  string
		contentHash  string
		createdAt    string
		metadataJSON string
	)
	err := s.db.QueryRowContext(
		ctx,
		`SELECT kind, content_type, content_hash, created_at, metadata_json FROM artifacts WHERE id = ?`,
		id,
	).Scan(&kind, &contentType, &contentHash, &createdAt, &metadataJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, fmt.Errorf("query artifact for content http: %w", err)
	}
	contentHash = strings.TrimSpace(contentHash)
	if contentHash == "" {
		return out, ErrNotFound
	}

	body, err := s.blob.Read(ctx, contentHash)
	if err != nil {
		if errors.Is(err, blob.ErrBlobNotFound) {
			return out, ErrNotFound
		}
		return out, fmt.Errorf("read artifact content: %w", err)
	}

	out.Body = body
	out.ContentLength = int64(len(body))
	out.DBContentType = contentType
	out.ETag = `"` + contentHash + `"`
	out.CacheControl = "private, max-age=0"

	if tm, perr := time.Parse(time.RFC3339Nano, createdAt); perr == nil {
		out.LastModified = tm.UTC().Format(http.TimeFormat)
	}

	kind = strings.TrimSpace(strings.ToLower(kind))
	if kind == "attachment" {
		meta, _ := decodeArtifactMetadataJSON(metadataJSON)
		mimeType := strings.TrimSpace(anyStringValue(meta["mime_type"]))
		if mimeType == "" {
			mimeType = strings.TrimSpace(contentType)
		}
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		out.ContentType = mimeType

		fname := sanitizeOriginalFilename(anyStringValue(meta["original_filename"]))
		dispositionType := "attachment"
		if attachmentPreferInline(mimeType) {
			dispositionType = "inline"
		}
		if fname != "" {
			if cd := mime.FormatMediaType(dispositionType, map[string]string{"filename": fname}); cd != "" {
				out.ContentDisposition = cd
			}
		} else {
			out.ContentDisposition = dispositionType
		}
		return out, nil
	}

	switch strings.TrimSpace(contentType) {
	case "structured":
		out.ContentType = "application/json"
	case "text":
		out.ContentType = "text/plain; charset=utf-8"
	default:
		out.ContentType = "application/octet-stream"
	}
	return out, nil
}

// CreateArtifactAttachment streams an attachment body into the blob store and inserts an artifacts row.
func (s *Store) CreateArtifactAttachment(ctx context.Context, actorID string, artifact map[string]any, mimeType string, originalFilename string, src io.Reader, maxUploadBytes int64) (map[string]any, error) {
	return s.createArtifactAttachment(ctx, actorID, artifact, mimeType, originalFilename, src, maxUploadBytes, "")
}

// CreateArtifactAttachmentWithReplay commits metadata, blob ledger and the original
// HTTP result together. Content-addressed staging occurs before the transaction;
// an interrupted upload never leaves a canonical artifact or replay receipt.
func (s *Store) CreateArtifactAttachmentWithReplay(ctx context.Context, actorID string, artifact map[string]any, mimeType, originalFilename string, src io.Reader, maxUploadBytes int64, key string) (map[string]any, error) {
	return s.createArtifactAttachment(ctx, actorID, artifact, mimeType, originalFilename, src, maxUploadBytes, key)
}

func (s *Store) createArtifactAttachment(ctx context.Context, actorID string, artifact map[string]any, mimeType, originalFilename string, src io.Reader, maxUploadBytes int64, key string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	if s.blob == nil {
		return nil, fmt.Errorf("blob backend is not configured")
	}
	if maxUploadBytes <= 0 {
		maxUploadBytes = DefaultAttachmentMaxUploadBytes
	}
	if s.quota.enabled() {
		s.quotaMu.Lock()
		defer s.quotaMu.Unlock()
	}

	kind := strings.TrimSpace(anyStringValue(artifact["kind"]))
	if kind != "" && !strings.EqualFold(kind, "attachment") {
		return nil, fmt.Errorf("artifact.kind must be attachment for this endpoint")
	}

	refs, err := normalizeStringSlice(artifact["refs"])
	if err != nil {
		return nil, fmt.Errorf("artifact.refs: %w", err)
	}

	metadata := cloneMap(artifact)
	metadata["kind"] = "attachment"

	artifactID, _ := metadata["id"].(string)
	artifactID = strings.TrimSpace(artifactID)
	if artifactID == "" {
		artifactID = uuid.NewString()
	} else if err := validateArtifactID(artifactID); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidArtifactID, err)
	}

	mimeType = strings.TrimSpace(strings.Split(mimeType, ";")[0])
	if !AllowAttachmentMIME(mimeType) {
		return nil, fmt.Errorf("%w: %q", ErrAttachmentMimeNotAllowed, mimeType)
	}
	origName := sanitizeOriginalFilename(originalFilename)

	var captured bytes.Buffer
	contentHash, size, staged, err := s.blob.WriteStream(ctx, io.TeeReader(src, &captured), maxUploadBytes)
	if err != nil {
		if errors.Is(err, blob.ErrUploadTooLarge) {
			return nil, err
		}
		return nil, fmt.Errorf("stage attachment content: %w", err)
	}
	defer func() { _ = staged.Cleanup() }()
	if err := resourceaccess.ValidateContent(captured.Bytes(), mimeType); err != nil {
		return nil, err
	}

	var requestHash string
	if key != "" {
		encoded, err := json.Marshal(struct {
			Artifact                    map[string]any
			MIME, Filename, ContentHash string
			Size                        int64
		}{artifact, mimeType, originalFilename, contentHash, size})
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(encoded)
		requestHash = hex.EncodeToString(sum[:])
		prior, err := s.GetIdempotencyReplay(ctx, "artifacts.attachments.create", actorID, key)
		if err == nil {
			if prior.RequestHash != requestHash {
				return nil, ErrConflict
			}
			return prior.Response["artifact"].(map[string]any), nil
		}
		if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
	}

	blobPlan, err := s.prepareBlobLedgerWritePlan(ctx, contentHash, size)
	if err != nil {
		return nil, err
	}
	if err := s.checkWorkspaceWriteQuota(ctx, size, quotaWriteDelta{artifacts: 1}, blobPlan); err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	metadata["id"] = artifactID
	metadata["created_at"] = now
	metadata["created_by"] = actorID
	metadata["content_type"] = mimeType
	metadata["mime_type"] = mimeType
	metadata["content_hash"] = contentHash
	metadata["original_filename"] = origName
	metadata["size_bytes"] = size
	metadata["preview_status"] = "ready"
	metadata["scan_status"] = "not_scanned"
	artifactThreadID := firstThreadRefValue(refs)

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
		return nil, fmt.Errorf("begin attachment transaction: %w", err)
	}
	defer tx.Rollback()
	if key != "" {
		// Acquire the write lock before reading so identical concurrent requests
		// serialize rather than upgrading a stale read snapshot.
		if _, err := tx.ExecContext(ctx, `UPDATE idempotency_replays SET request_hash=request_hash WHERE scope='artifacts.attachments.create' AND actor_id=? AND request_key=?`, actorID, key); err != nil {
			return nil, err
		}
		var hash, raw string
		err := tx.QueryRowContext(ctx, `SELECT request_hash,response_json FROM idempotency_replays WHERE scope='artifacts.attachments.create' AND actor_id=? AND request_key=?`, actorID, key).Scan(&hash, &raw)
		if err == nil {
			if hash != requestHash {
				return nil, ErrConflict
			}
			var response struct {
				Artifact map[string]any `json:"artifact"`
			}
			if err := json.Unmarshal([]byte(raw), &response); err != nil {
				return nil, err
			}
			return response.Artifact, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	artifactHandle, err := uniqueHandleTx(ctx, tx, "artifact", firstNonEmpty(origName, "attachment"), "artifact-"+artifactID)
	if err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("allocate attachment handle: %w", err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO artifacts(id, handle, kind, thread_id, created_at, created_by, content_type, content_hash, refs_json, metadata_json, content_refs_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		metadata["id"],
		artifactHandle,
		"attachment",
		nullableString(artifactThreadID),
		metadata["created_at"],
		actorID,
		mimeType,
		contentHash,
		string(refsJSON),
		string(metadataJSON),
		resourceaccess.ContentReferenceAtomsJSON(captured.String(), mimeType),
	); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		if isUniqueViolation(err) {
			return nil, ErrConflict
		}
		return nil, fmt.Errorf("insert attachment artifact: %w", err)
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

	if key != "" {
		raw, err := json.Marshal(map[string]any{"artifact": metadata})
		if err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO idempotency_replays(scope,actor_id,request_key,request_hash,response_status,response_json) VALUES('artifacts.attachments.create',?,?,?,201,?)`, actorID, key, requestHash, string(raw)); err != nil {
			return nil, err
		}
	}

	if err := staged.Promote(); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, fmt.Errorf("finalize attachment content: %w", err)
	}

	if err := tx.Commit(); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, fmt.Errorf("commit attachment transaction: %w", err)
	}

	return metadata, nil
}
