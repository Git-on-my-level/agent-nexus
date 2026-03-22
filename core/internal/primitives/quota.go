package primitives

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type Option func(*Store)

type WorkspaceQuota struct {
	MaxBlobBytes         int64 `json:"max_blob_bytes"`
	MaxArtifacts         int64 `json:"max_artifacts"`
	MaxDocuments         int64 `json:"max_documents"`
	MaxDocumentRevisions int64 `json:"max_document_revisions"`
	MaxUploadBytes       int64 `json:"max_upload_bytes"`
}

type QuotaViolation struct {
	Code      string
	Metric    string
	Limit     int64
	Current   int64
	Projected int64
}

func (q *QuotaViolation) Error() string {
	if q == nil {
		return "workspace quota violation"
	}
	switch q.Code {
	case "request_too_large":
		return fmt.Sprintf("request exceeds max upload size for %s", q.Metric)
	case "workspace_quota_exceeded":
		return fmt.Sprintf("workspace quota exceeded for %s", q.Metric)
	default:
		return "workspace quota violation"
	}
}

func WithWorkspaceQuota(quota WorkspaceQuota) Option {
	return func(store *Store) {
		store.quota = quota
	}
}

func (q WorkspaceQuota) enabled() bool {
	return q.MaxBlobBytes > 0 || q.MaxArtifacts > 0 || q.MaxDocuments > 0 || q.MaxDocumentRevisions > 0 || q.MaxUploadBytes > 0
}

type quotaWriteDelta struct {
	artifacts int64
	documents int64
	revisions int64
}

type workspaceUsage struct {
	blobBytes   int64
	blobObjects int64
	artifacts   int64
	documents   int64
	revisions   int64
}

type WorkspaceUsageSummary struct {
	Usage       WorkspaceUsage `json:"usage"`
	Quota       WorkspaceQuota `json:"quota"`
	GeneratedAt string         `json:"generated_at"`
}

type WorkspaceUsage struct {
	BlobBytes   int64 `json:"blob_bytes"`
	BlobObjects int64 `json:"blob_objects"`
	Artifacts   int64 `json:"artifact_count"`
	Documents   int64 `json:"document_count"`
	Revisions   int64 `json:"document_revision_count"`
}

func (s *Store) checkWorkspaceWriteQuota(ctx context.Context, uploadBytes int64, contentHash string, delta quotaWriteDelta) error {
	if s == nil || !s.quota.enabled() {
		return nil
	}

	if s.quota.MaxUploadBytes > 0 && uploadBytes > s.quota.MaxUploadBytes {
		return &QuotaViolation{
			Code:      "request_too_large",
			Metric:    "upload_bytes",
			Limit:     s.quota.MaxUploadBytes,
			Current:   0,
			Projected: uploadBytes,
		}
	}

	usage, err := s.currentWorkspaceUsage(ctx, s.quota.MaxBlobBytes > 0)
	if err != nil {
		return err
	}

	growth := int64(0)
	if uploadBytes > 0 {
		growth = uploadBytes
	}
	if contentHash != "" && s.blob != nil {
		exists, err := s.blob.Exists(ctx, contentHash)
		if err != nil {
			return fmt.Errorf("check blob existence: %w", err)
		}
		if exists {
			growth = 0
		}
	}

	if s.quota.MaxBlobBytes > 0 {
		projected := usage.blobBytes + growth
		if projected > s.quota.MaxBlobBytes {
			return &QuotaViolation{
				Code:      "workspace_quota_exceeded",
				Metric:    "blob_bytes",
				Limit:     s.quota.MaxBlobBytes,
				Current:   usage.blobBytes,
				Projected: projected,
			}
		}
	}
	if s.quota.MaxArtifacts > 0 {
		projected := usage.artifacts + delta.artifacts
		if projected > s.quota.MaxArtifacts {
			return &QuotaViolation{
				Code:      "workspace_quota_exceeded",
				Metric:    "artifact_count",
				Limit:     s.quota.MaxArtifacts,
				Current:   usage.artifacts,
				Projected: projected,
			}
		}
	}
	if s.quota.MaxDocuments > 0 {
		projected := usage.documents + delta.documents
		if projected > s.quota.MaxDocuments {
			return &QuotaViolation{
				Code:      "workspace_quota_exceeded",
				Metric:    "document_count",
				Limit:     s.quota.MaxDocuments,
				Current:   usage.documents,
				Projected: projected,
			}
		}
	}
	if s.quota.MaxDocumentRevisions > 0 {
		projected := usage.revisions + delta.revisions
		if projected > s.quota.MaxDocumentRevisions {
			return &QuotaViolation{
				Code:      "workspace_quota_exceeded",
				Metric:    "document_revision_count",
				Limit:     s.quota.MaxDocumentRevisions,
				Current:   usage.revisions,
				Projected: projected,
			}
		}
	}

	return nil
}

func (s *Store) currentWorkspaceUsage(ctx context.Context, includeBlobBytes bool) (workspaceUsage, error) {
	if s == nil || s.db == nil {
		return workspaceUsage{}, fmt.Errorf("primitives store database is not initialized")
	}

	usage := workspaceUsage{}
	if includeBlobBytes {
		if s.blob == nil {
			return workspaceUsage{}, fmt.Errorf("blob backend is not configured")
		}
		blobUsage, err := s.blob.Usage(ctx)
		if err != nil {
			return workspaceUsage{}, fmt.Errorf("measure blob usage: %w", err)
		}
		usage.blobBytes = blobUsage.Bytes
		usage.blobObjects = blobUsage.Objects
	}

	var err error
	if usage.artifacts, err = countTableRows(ctx, s.db, "artifacts"); err != nil {
		return workspaceUsage{}, err
	}
	if usage.documents, err = countTableRows(ctx, s.db, "documents"); err != nil {
		return workspaceUsage{}, err
	}
	if usage.revisions, err = countTableRows(ctx, s.db, "document_revisions"); err != nil {
		return workspaceUsage{}, err
	}

	return usage, nil
}

func (s *Store) GetWorkspaceUsageSummary(ctx context.Context) (WorkspaceUsageSummary, error) {
	if s == nil || s.db == nil {
		return WorkspaceUsageSummary{}, fmt.Errorf("primitives store database is not initialized")
	}
	usage, err := s.currentWorkspaceUsage(ctx, true)
	if err != nil {
		return WorkspaceUsageSummary{}, err
	}
	return WorkspaceUsageSummary{
		Usage: WorkspaceUsage{
			BlobBytes:   usage.blobBytes,
			BlobObjects: usage.blobObjects,
			Artifacts:   usage.artifacts,
			Documents:   usage.documents,
			Revisions:   usage.revisions,
		},
		Quota:       s.quota,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}, nil
}

func countTableRows(ctx context.Context, db *sql.DB, table string) (int64, error) {
	var count int64
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
		return 0, fmt.Errorf("count %s rows: %w", table, err)
	}
	return count, nil
}

func isQuotaViolationCode(err error, code string) bool {
	var violation *QuotaViolation
	if !errors.As(err, &violation) || violation == nil {
		return false
	}
	return violation.Code == code
}

func quotaViolationDetails(err error) (QuotaViolation, bool) {
	var violation *QuotaViolation
	if !errors.As(err, &violation) || violation == nil {
		return QuotaViolation{}, false
	}
	return *violation, true
}
