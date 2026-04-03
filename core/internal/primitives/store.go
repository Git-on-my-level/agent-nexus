package primitives

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"organization-autorunner-core/internal/blob"
	"organization-autorunner-core/internal/schedule"
)

var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("conflict")
var ErrNotTombstoned = errors.New("entity is not tombstoned")
var ErrNotArchived = errors.New("entity is not archived")
var ErrAlreadyTombstoned = errors.New("entity is tombstoned")
var ErrArtifactInUse = errors.New("artifact is referenced by document revisions")
var ErrInvalidCommitmentTransition = errors.New("invalid commitment transition")
var ErrInvalidArtifactID = errors.New("invalid artifact id")
var ErrInvalidDocumentRequest = errors.New("invalid document request")
var ErrInvalidCursor = errors.New("invalid cursor")

const actorStatementEventIDPlaceholder = "<event_id>"

type ArtifactListFilter struct {
	Q                 string
	Limit             *int
	Kind              string
	ThreadID          string
	CreatedBefore     string
	CreatedAfter      string
	IncludeTombstoned bool
	TombstonedOnly    bool
	IncludeArchived   bool
	ArchivedOnly      bool
}

type DocumentListFilter struct {
	ThreadID          string
	IncludeTombstoned bool
	TombstonedOnly    bool
	IncludeArchived   bool
	ArchivedOnly      bool
	Query             string
	Limit             *int
	Cursor            string
}

type ThreadListFilter struct {
	Status            string
	Priority          string
	Tag               string
	Tags              []string
	Cadences          []string
	Stale             *bool
	Now               time.Time
	Query             string
	Limit             *int
	Cursor            string
	IncludeArchived   bool
	ArchivedOnly      bool
	IncludeTombstoned bool
	TombstonedOnly    bool
}

type EventListFilter struct {
	Types []string
}

type EventCursor struct {
	TS string
	ID string
}

type CommitmentListFilter struct {
	ThreadID  string
	Owner     string
	Status    string
	DueBefore string
	DueAfter  string
}

type Store struct {
	db       *sql.DB
	blob     blob.Backend
	blobRoot string
	quota    WorkspaceQuota
	quotaMu  sync.Mutex
}

type eventExec interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type preparedEvent struct {
	Body        map[string]any
	Type        string
	ThreadID    string
	RefsJSON    string
	PayloadJSON string
	BodyJSON    string
}

type PatchSnapshotResult struct {
	Snapshot map[string]any
	Event    map[string]any
}

func NewStore(db *sql.DB, blobBackend blob.Backend, blobRoot string, options ...Option) *Store {
	store := &Store{db: db, blob: blobBackend, blobRoot: blobRoot}
	for _, option := range options {
		option(store)
	}
	return store
}

func (s *Store) AppendEvent(ctx context.Context, actorID string, event map[string]any) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}

	prepared, err := prepareEventForInsert(actorID, event)
	if err != nil {
		return nil, err
	}
	if err := insertPreparedEvent(ctx, s.db, prepared); err != nil {
		return nil, err
	}

	return prepared.Body, nil
}

func overlayEventLifecycleFromSQLColumns(body map[string]any, archivedAt, archivedBy, tombstonedAt, tombstonedBy, tombstoneReason sql.NullString) {
	delete(body, "archived_at")
	delete(body, "archived_by")
	delete(body, "tombstoned_at")
	delete(body, "tombstoned_by")
	delete(body, "tombstone_reason")
	if tombstonedAt.Valid && strings.TrimSpace(tombstonedAt.String) != "" {
		body["tombstoned_at"] = tombstonedAt.String
		if tombstonedBy.Valid && strings.TrimSpace(tombstonedBy.String) != "" {
			body["tombstoned_by"] = tombstonedBy.String
		}
		if tombstoneReason.Valid && strings.TrimSpace(tombstoneReason.String) != "" {
			body["tombstone_reason"] = tombstoneReason.String
		}
		return
	}
	if archivedAt.Valid && strings.TrimSpace(archivedAt.String) != "" {
		body["archived_at"] = archivedAt.String
		if archivedBy.Valid && strings.TrimSpace(archivedBy.String) != "" {
			body["archived_by"] = archivedBy.String
		}
	}
}

func decodeEventBodyFromRow(
	eventID, typeValue, ts, actorID string,
	threadID sql.NullString,
	refsJSON, payloadJSON string,
	bodyJSON sql.NullString,
) (map[string]any, error) {
	if bodyJSON.Valid && strings.TrimSpace(bodyJSON.String) != "" && bodyJSON.String != "{}" {
		var body map[string]any
		if err := json.Unmarshal([]byte(bodyJSON.String), &body); err != nil {
			return nil, fmt.Errorf("decode event body: %w", err)
		}
		return body, nil
	}

	var refs []string
	if err := json.Unmarshal([]byte(refsJSON), &refs); err != nil {
		return nil, fmt.Errorf("decode event refs: %w", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return nil, fmt.Errorf("decode event payload: %w", err)
	}

	out := map[string]any{
		"id":       eventID,
		"type":     typeValue,
		"ts":       ts,
		"actor_id": actorID,
		"refs":     refs,
		"payload":  payload,
	}
	if threadID.Valid {
		out["thread_id"] = threadID.String
	}

	return out, nil
}

func (s *Store) GetEvent(ctx context.Context, id string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}

	var (
		eventID         string
		typeValue       string
		ts              string
		actorID         string
		threadID        sql.NullString
		refsJSON        string
		payloadJSON     string
		bodyJSON        sql.NullString
		archivedAt      sql.NullString
		archivedBy      sql.NullString
		tombstonedAt    sql.NullString
		tombstonedBy    sql.NullString
		tombstoneReason sql.NullString
	)
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json, body_json,
			archived_at, archived_by, tombstoned_at, tombstoned_by, tombstone_reason
		 FROM events WHERE id = ?`,
		id,
	).Scan(&eventID, &typeValue, &ts, &actorID, &threadID, &refsJSON, &payloadJSON, &bodyJSON,
		&archivedAt, &archivedBy, &tombstonedAt, &tombstonedBy, &tombstoneReason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query event: %w", err)
	}

	body, err := decodeEventBodyFromRow(eventID, typeValue, ts, actorID, threadID, refsJSON, payloadJSON, bodyJSON)
	if err != nil {
		return nil, err
	}
	overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, tombstonedAt, tombstonedBy, tombstoneReason)
	return body, nil
}

func (s *Store) CreateArtifact(ctx context.Context, actorID string, artifact map[string]any, content any, contentType string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	if s.blob == nil {
		return nil, fmt.Errorf("blob backend is not configured")
	}
	if s.quota.enabled() {
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
	artifactThreadID := firstThreadRefValue(refs)

	stagedContent, err := s.blob.Write(ctx, contentHash, encodedContent)
	if err != nil {
		return nil, fmt.Errorf("stage artifact content: %w", err)
	}
	defer func() { _ = stagedContent.Cleanup() }()

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

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO artifacts(id, kind, thread_id, created_at, created_by, content_type, content_hash, refs_json, metadata_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		metadata["id"],
		kind,
		nullableString(artifactThreadID),
		metadata["created_at"],
		actorID,
		contentType,
		contentHash,
		string(refsJSON),
		string(metadataJSON),
	); err != nil {
		_ = tx.Rollback()
		if isUniqueViolation(err) {
			return nil, ErrConflict
		}
		return nil, fmt.Errorf("insert artifact: %w", err)
	}

	if err := s.applyBlobLedgerWritePlanTx(ctx, tx, blobPlan); err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	if err := stagedContent.Promote(); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("finalize artifact content: %w", err)
	}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("commit artifact transaction: %w", err)
	}

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

	encodedContent, err := encodeContent(content)
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

	stagedContent, err := s.blob.Write(ctx, contentHash, encodedContent)
	if err != nil {
		return nil, nil, fmt.Errorf("stage artifact content: %w", err)
	}
	defer func() { _ = stagedContent.Cleanup() }()

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

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO artifacts(id, kind, thread_id, created_at, created_by, content_type, content_hash, refs_json, metadata_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		metadata["id"],
		kind,
		nullableString(artifactThreadID),
		metadata["created_at"],
		actorID,
		contentType,
		contentHash,
		string(artifactRefsJSON),
		string(artifactMetadataJSON),
	); err != nil {
		_ = tx.Rollback()
		if isUniqueViolation(err) {
			return nil, nil, ErrConflict
		}
		return nil, nil, fmt.Errorf("insert artifact: %w", err)
	}

	if err := insertPreparedEvent(ctx, tx, preparedEvent); err != nil {
		_ = tx.Rollback()
		return nil, nil, err
	}

	if err := s.applyBlobLedgerWritePlanTx(ctx, tx, blobPlan); err != nil {
		_ = tx.Rollback()
		return nil, nil, err
	}

	if err := stagedContent.Promote(); err != nil {
		_ = tx.Rollback()
		return nil, nil, fmt.Errorf("finalize artifact content: %w", err)
	}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return nil, nil, fmt.Errorf("commit transaction: %w", err)
	}

	return metadata, preparedEvent.Body, nil
}

func (s *Store) GetArtifact(ctx context.Context, id string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}

	var metadataJSON string
	err := s.db.QueryRowContext(ctx, `SELECT metadata_json FROM artifacts WHERE id = ?`, id).Scan(&metadataJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query artifact metadata: %w", err)
	}

	return decodeArtifactMetadataJSON(metadataJSON)
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

func (s *Store) ListArtifacts(ctx context.Context, filter ArtifactListFilter) ([]map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}

	query, args := buildListArtifactsQuery(filter)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query artifacts: %w", err)
	}
	defer rows.Close()

	artifacts := make([]map[string]any, 0)
	for rows.Next() {
		var metadataJSON string
		if err := rows.Scan(&metadataJSON); err != nil {
			return nil, fmt.Errorf("scan artifact row: %w", err)
		}

		metadata, err := decodeArtifactMetadataJSON(metadataJSON)
		if err != nil {
			return nil, err
		}

		artifacts = append(artifacts, metadata)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate artifact rows: %w", err)
	}

	return artifacts, nil
}

func (s *Store) TombstoneArtifact(ctx context.Context, actorID string, artifactID string, reason string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, fmt.Errorf("actor_id is required")
	}
	artifactID = strings.TrimSpace(artifactID)
	if artifactID == "" {
		return nil, fmt.Errorf("artifact_id is required")
	}

	var metadataJSON string
	var tombstonedAt sql.NullString
	var tombstonedBy sql.NullString
	var tombstoneReason sql.NullString
	err := s.db.QueryRowContext(
		ctx,
		`SELECT metadata_json, tombstoned_at, tombstoned_by, tombstone_reason FROM artifacts WHERE id = ?`,
		artifactID,
	).Scan(&metadataJSON, &tombstonedAt, &tombstonedBy, &tombstoneReason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query artifact for tombstone: %w", err)
	}

	metadata, err := decodeArtifactMetadataJSON(metadataJSON)
	if err != nil {
		return nil, err
	}
	if tombstonedAt.Valid && strings.TrimSpace(tombstonedAt.String) != "" {
		metadata["tombstoned_at"] = tombstonedAt.String
		if tombstonedBy.Valid && strings.TrimSpace(tombstonedBy.String) != "" {
			metadata["tombstoned_by"] = tombstonedBy.String
		}
		if tombstoneReason.Valid && strings.TrimSpace(tombstoneReason.String) != "" {
			metadata["tombstone_reason"] = tombstoneReason.String
		}
		return metadata, nil
	}

	delete(metadata, "archived_at")
	delete(metadata, "archived_by")

	now := time.Now().UTC().Format(time.RFC3339Nano)
	metadata["tombstoned_at"] = now
	metadata["tombstoned_by"] = actorID
	if strings.TrimSpace(reason) != "" {
		metadata["tombstone_reason"] = reason
	}

	updatedMetadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("encode tombstoned artifact metadata: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE artifacts SET tombstoned_at = ?, tombstoned_by = ?, tombstone_reason = ?, archived_at = NULL, archived_by = NULL, metadata_json = ? WHERE id = ?`,
		now, actorID, strings.TrimSpace(reason), string(updatedMetadataJSON), artifactID,
	)
	if err != nil {
		return nil, fmt.Errorf("tombstone artifact: %w", err)
	}

	return metadata, nil
}

func (s *Store) ArchiveArtifact(ctx context.Context, actorID, artifactID string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, fmt.Errorf("actor_id is required")
	}
	artifactID = strings.TrimSpace(artifactID)
	if artifactID == "" {
		return nil, fmt.Errorf("artifact_id is required")
	}

	var metadataJSON string
	var tombstonedAt sql.NullString
	var archivedAt sql.NullString
	var archivedBy sql.NullString
	err := s.db.QueryRowContext(
		ctx,
		`SELECT metadata_json, tombstoned_at, archived_at, archived_by FROM artifacts WHERE id = ?`,
		artifactID,
	).Scan(&metadataJSON, &tombstonedAt, &archivedAt, &archivedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query artifact for archive: %w", err)
	}

	if tombstonedAt.Valid && strings.TrimSpace(tombstonedAt.String) != "" {
		return nil, ErrAlreadyTombstoned
	}

	metadata, err := decodeArtifactMetadataJSON(metadataJSON)
	if err != nil {
		return nil, err
	}

	if archivedAt.Valid && strings.TrimSpace(archivedAt.String) != "" {
		metadata["archived_at"] = archivedAt.String
		if archivedBy.Valid && strings.TrimSpace(archivedBy.String) != "" {
			metadata["archived_by"] = archivedBy.String
		}
		return metadata, nil
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	metadata["archived_at"] = now
	metadata["archived_by"] = actorID

	updatedMetadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("encode archived artifact metadata: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE artifacts SET archived_at = ?, archived_by = ?, metadata_json = ? WHERE id = ?`,
		now, actorID, string(updatedMetadataJSON), artifactID,
	)
	if err != nil {
		return nil, fmt.Errorf("archive artifact: %w", err)
	}

	return metadata, nil
}

func (s *Store) UnarchiveArtifact(ctx context.Context, actorID, artifactID string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, fmt.Errorf("actor_id is required")
	}
	artifactID = strings.TrimSpace(artifactID)
	if artifactID == "" {
		return nil, fmt.Errorf("artifact_id is required")
	}

	var metadataJSON string
	var tombstonedDiscard sql.NullString
	var archivedAt sql.NullString
	var archivedByDiscard sql.NullString
	err := s.db.QueryRowContext(
		ctx,
		`SELECT metadata_json, tombstoned_at, archived_at, archived_by FROM artifacts WHERE id = ?`,
		artifactID,
	).Scan(&metadataJSON, &tombstonedDiscard, &archivedAt, &archivedByDiscard)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query artifact for unarchive: %w", err)
	}

	if !archivedAt.Valid || strings.TrimSpace(archivedAt.String) == "" {
		return nil, ErrNotArchived
	}

	metadata, err := decodeArtifactMetadataJSON(metadataJSON)
	if err != nil {
		return nil, err
	}
	delete(metadata, "archived_at")
	delete(metadata, "archived_by")

	updatedMetadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("encode unarchived artifact metadata: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE artifacts SET archived_at = NULL, archived_by = NULL, metadata_json = ? WHERE id = ?`,
		string(updatedMetadataJSON), artifactID,
	)
	if err != nil {
		return nil, fmt.Errorf("unarchive artifact: %w", err)
	}

	return metadata, nil
}

func (s *Store) RestoreArtifact(ctx context.Context, actorID, artifactID string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, fmt.Errorf("actor_id is required")
	}
	artifactID = strings.TrimSpace(artifactID)
	if artifactID == "" {
		return nil, fmt.Errorf("artifact_id is required")
	}

	var metadataJSON string
	var tombstonedAt sql.NullString
	var tombstonedBy sql.NullString
	var tombstoneReason sql.NullString
	err := s.db.QueryRowContext(
		ctx,
		`SELECT metadata_json, tombstoned_at, tombstoned_by, tombstone_reason FROM artifacts WHERE id = ?`,
		artifactID,
	).Scan(&metadataJSON, &tombstonedAt, &tombstonedBy, &tombstoneReason)
	_ = tombstonedBy
	_ = tombstoneReason
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query artifact for restore: %w", err)
	}

	if !tombstonedAt.Valid || strings.TrimSpace(tombstonedAt.String) == "" {
		return nil, ErrNotTombstoned
	}

	metadata, err := decodeArtifactMetadataJSON(metadataJSON)
	if err != nil {
		return nil, err
	}
	delete(metadata, "tombstoned_at")
	delete(metadata, "tombstoned_by")
	delete(metadata, "tombstone_reason")

	updatedMetadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("encode restored artifact metadata: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE artifacts SET tombstoned_at = NULL, tombstoned_by = NULL, tombstone_reason = NULL, metadata_json = ? WHERE id = ?`,
		string(updatedMetadataJSON), artifactID,
	)
	if err != nil {
		return nil, fmt.Errorf("restore artifact: %w", err)
	}

	return metadata, nil
}

func (s *Store) collectMessageDescendantIDs(ctx context.Context, threadID, parentID string) ([]string, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	threadID = strings.TrimSpace(threadID)
	parentID = strings.TrimSpace(parentID)
	if threadID == "" || parentID == "" {
		return nil, nil
	}

	queue := []string{parentID}
	seen := map[string]bool{parentID: true}
	var descendants []string

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		refPattern := fmt.Sprintf(`"event:%s"`, current)
		rows, err := s.db.QueryContext(ctx,
			`SELECT id FROM events WHERE thread_id = ? AND type = 'message_posted' AND refs_json LIKE ?`,
			threadID, "%"+refPattern+"%",
		)
		if err != nil {
			return nil, fmt.Errorf("query message descendants: %w", err)
		}
		for rows.Next() {
			var childID string
			if err := rows.Scan(&childID); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("scan message descendant: %w", err)
			}
			if !seen[childID] {
				seen[childID] = true
				descendants = append(descendants, childID)
				queue = append(queue, childID)
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("iterate message descendants: %w", err)
		}
		if err := rows.Close(); err != nil {
			return nil, fmt.Errorf("close message descendant rows: %w", err)
		}
	}
	return descendants, nil
}

func (s *Store) archiveEventCascadeChild(ctx context.Context, actorID, childID string) error {
	var (
		eventIDScan     string
		typeValue       string
		ts              string
		actorIDScan     string
		threadID        sql.NullString
		refsJSON        string
		payloadJSON     string
		bodyJSON        sql.NullString
		archivedAt      sql.NullString
		archivedBy      sql.NullString
		tombstonedAt    sql.NullString
		tombstonedBy    sql.NullString
		tombstoneReason sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json, body_json,
			archived_at, archived_by, tombstoned_at, tombstoned_by, tombstone_reason
		 FROM events WHERE id = ?`,
		childID,
	).Scan(&eventIDScan, &typeValue, &ts, &actorIDScan, &threadID, &refsJSON, &payloadJSON, &bodyJSON,
		&archivedAt, &archivedBy, &tombstonedAt, &tombstonedBy, &tombstoneReason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("query event for cascade archive: %w", err)
	}
	if tombstonedAt.Valid && strings.TrimSpace(tombstonedAt.String) != "" {
		return nil
	}
	if archivedAt.Valid && strings.TrimSpace(archivedAt.String) != "" {
		return nil
	}

	body, err := decodeEventBodyFromRow(eventIDScan, typeValue, ts, actorIDScan, threadID, refsJSON, payloadJSON, bodyJSON)
	if err != nil {
		return err
	}
	overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, tombstonedAt, tombstonedBy, tombstoneReason)

	now := time.Now().UTC().Format(time.RFC3339Nano)
	body["archived_at"] = now
	body["archived_by"] = actorID
	updatedBodyJSON, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode cascade archived event body: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE events SET archived_at = ?, archived_by = ?, body_json = ? WHERE id = ?`,
		now, actorID, string(updatedBodyJSON), childID,
	)
	if err != nil {
		return fmt.Errorf("cascade archive event: %w", err)
	}
	return nil
}

func (s *Store) unarchiveEventCascadeChild(ctx context.Context, childID string) error {
	var (
		eventIDScan     string
		typeValue       string
		ts              string
		actorIDScan     string
		threadID        sql.NullString
		refsJSON        string
		payloadJSON     string
		bodyJSON        sql.NullString
		archivedAt      sql.NullString
		archivedBy      sql.NullString
		tombstonedAt    sql.NullString
		tombstonedBy    sql.NullString
		tombstoneReason sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json, body_json,
			archived_at, archived_by, tombstoned_at, tombstoned_by, tombstone_reason
		 FROM events WHERE id = ?`,
		childID,
	).Scan(&eventIDScan, &typeValue, &ts, &actorIDScan, &threadID, &refsJSON, &payloadJSON, &bodyJSON,
		&archivedAt, &archivedBy, &tombstonedAt, &tombstonedBy, &tombstoneReason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("query event for cascade unarchive: %w", err)
	}
	if !archivedAt.Valid || strings.TrimSpace(archivedAt.String) == "" {
		return nil
	}

	body, err := decodeEventBodyFromRow(eventIDScan, typeValue, ts, actorIDScan, threadID, refsJSON, payloadJSON, bodyJSON)
	if err != nil {
		return err
	}
	overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, tombstonedAt, tombstonedBy, tombstoneReason)
	delete(body, "archived_at")
	delete(body, "archived_by")

	updatedBodyJSON, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode cascade unarchived event body: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE events SET archived_at = NULL, archived_by = NULL, body_json = ? WHERE id = ?`,
		string(updatedBodyJSON), childID,
	)
	if err != nil {
		return fmt.Errorf("cascade unarchive event: %w", err)
	}
	return nil
}

func (s *Store) tombstoneEventCascadeChild(ctx context.Context, actorID, childID, reason string) error {
	var (
		eventIDScan     string
		typeValue       string
		ts              string
		actorIDScan     string
		threadID        sql.NullString
		refsJSON        string
		payloadJSON     string
		bodyJSON        sql.NullString
		archivedAt      sql.NullString
		archivedBy      sql.NullString
		tombstonedAt    sql.NullString
		tombstonedBy    sql.NullString
		tombstoneReason sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json, body_json,
			archived_at, archived_by, tombstoned_at, tombstoned_by, tombstone_reason
		 FROM events WHERE id = ?`,
		childID,
	).Scan(&eventIDScan, &typeValue, &ts, &actorIDScan, &threadID, &refsJSON, &payloadJSON, &bodyJSON,
		&archivedAt, &archivedBy, &tombstonedAt, &tombstonedBy, &tombstoneReason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("query event for cascade tombstone: %w", err)
	}
	if tombstonedAt.Valid && strings.TrimSpace(tombstonedAt.String) != "" {
		return nil
	}

	body, err := decodeEventBodyFromRow(eventIDScan, typeValue, ts, actorIDScan, threadID, refsJSON, payloadJSON, bodyJSON)
	if err != nil {
		return err
	}
	overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, tombstonedAt, tombstonedBy, tombstoneReason)

	delete(body, "archived_at")
	delete(body, "archived_by")

	now := time.Now().UTC().Format(time.RFC3339Nano)
	body["tombstoned_at"] = now
	body["tombstoned_by"] = actorID
	if strings.TrimSpace(reason) != "" {
		body["tombstone_reason"] = strings.TrimSpace(reason)
	}

	updatedBodyJSON, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode cascade tombstoned event body: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE events SET tombstoned_at = ?, tombstoned_by = ?, tombstone_reason = ?, archived_at = NULL, archived_by = NULL, body_json = ? WHERE id = ?`,
		now, actorID, strings.TrimSpace(reason), string(updatedBodyJSON), childID,
	)
	if err != nil {
		return fmt.Errorf("cascade tombstone event: %w", err)
	}
	return nil
}

func (s *Store) restoreEventCascadeChild(ctx context.Context, childID string) error {
	var (
		eventIDScan     string
		typeValue       string
		ts              string
		actorIDScan     string
		threadID        sql.NullString
		refsJSON        string
		payloadJSON     string
		bodyJSON        sql.NullString
		archivedAt      sql.NullString
		archivedBy      sql.NullString
		tombstonedAt    sql.NullString
		tombstonedBy    sql.NullString
		tombstoneReason sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json, body_json,
			archived_at, archived_by, tombstoned_at, tombstoned_by, tombstone_reason
		 FROM events WHERE id = ?`,
		childID,
	).Scan(&eventIDScan, &typeValue, &ts, &actorIDScan, &threadID, &refsJSON, &payloadJSON, &bodyJSON,
		&archivedAt, &archivedBy, &tombstonedAt, &tombstonedBy, &tombstoneReason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("query event for cascade restore: %w", err)
	}
	if !tombstonedAt.Valid || strings.TrimSpace(tombstonedAt.String) == "" {
		return nil
	}

	body, err := decodeEventBodyFromRow(eventIDScan, typeValue, ts, actorIDScan, threadID, refsJSON, payloadJSON, bodyJSON)
	if err != nil {
		return err
	}
	overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, tombstonedAt, tombstonedBy, tombstoneReason)
	delete(body, "tombstoned_at")
	delete(body, "tombstoned_by")
	delete(body, "tombstone_reason")

	updatedBodyJSON, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode cascade restored event body: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE events SET tombstoned_at = NULL, tombstoned_by = NULL, tombstone_reason = NULL, body_json = ? WHERE id = ?`,
		string(updatedBodyJSON), childID,
	)
	if err != nil {
		return fmt.Errorf("cascade restore event: %w", err)
	}
	return nil
}

func (s *Store) ArchiveEvent(ctx context.Context, actorID, eventID string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, fmt.Errorf("actor_id is required")
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return nil, fmt.Errorf("event_id is required")
	}

	var (
		eventIDScan     string
		typeValue       string
		ts              string
		actorIDScan     string
		threadID        sql.NullString
		refsJSON        string
		payloadJSON     string
		bodyJSON        sql.NullString
		archivedAt      sql.NullString
		archivedBy      sql.NullString
		tombstonedAt    sql.NullString
		tombstonedBy    sql.NullString
		tombstoneReason sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json, body_json,
			archived_at, archived_by, tombstoned_at, tombstoned_by, tombstone_reason
		 FROM events WHERE id = ?`,
		eventID,
	).Scan(&eventIDScan, &typeValue, &ts, &actorIDScan, &threadID, &refsJSON, &payloadJSON, &bodyJSON,
		&archivedAt, &archivedBy, &tombstonedAt, &tombstonedBy, &tombstoneReason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query event for archive: %w", err)
	}

	if tombstonedAt.Valid && strings.TrimSpace(tombstonedAt.String) != "" {
		return nil, ErrAlreadyTombstoned
	}

	body, err := decodeEventBodyFromRow(eventIDScan, typeValue, ts, actorIDScan, threadID, refsJSON, payloadJSON, bodyJSON)
	if err != nil {
		return nil, err
	}
	overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, tombstonedAt, tombstonedBy, tombstoneReason)

	if archivedAt.Valid && strings.TrimSpace(archivedAt.String) != "" {
		return body, nil
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	body["archived_at"] = now
	body["archived_by"] = actorID
	updatedBodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode archived event body: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE events SET archived_at = ?, archived_by = ?, body_json = ? WHERE id = ?`,
		now, actorID, string(updatedBodyJSON), eventID,
	)
	if err != nil {
		return nil, fmt.Errorf("archive event: %w", err)
	}

	if typeValue == "message_posted" && threadID.Valid && strings.TrimSpace(threadID.String) != "" {
		desc, err := s.collectMessageDescendantIDs(ctx, threadID.String, eventID)
		if err != nil {
			return nil, err
		}
		for _, childID := range desc {
			if err := s.archiveEventCascadeChild(ctx, actorID, childID); err != nil {
				return nil, err
			}
		}
	}

	return body, nil
}

func (s *Store) UnarchiveEvent(ctx context.Context, actorID, eventID string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, fmt.Errorf("actor_id is required")
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return nil, fmt.Errorf("event_id is required")
	}

	var (
		eventIDScan     string
		typeValue       string
		ts              string
		actorIDScan     string
		threadID        sql.NullString
		refsJSON        string
		payloadJSON     string
		bodyJSON        sql.NullString
		archivedAt      sql.NullString
		archivedBy      sql.NullString
		tombstonedAt    sql.NullString
		tombstonedBy    sql.NullString
		tombstoneReason sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json, body_json,
			archived_at, archived_by, tombstoned_at, tombstoned_by, tombstone_reason
		 FROM events WHERE id = ?`,
		eventID,
	).Scan(&eventIDScan, &typeValue, &ts, &actorIDScan, &threadID, &refsJSON, &payloadJSON, &bodyJSON,
		&archivedAt, &archivedBy, &tombstonedAt, &tombstonedBy, &tombstoneReason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query event for unarchive: %w", err)
	}

	if !archivedAt.Valid || strings.TrimSpace(archivedAt.String) == "" {
		return nil, ErrNotArchived
	}

	body, err := decodeEventBodyFromRow(eventIDScan, typeValue, ts, actorIDScan, threadID, refsJSON, payloadJSON, bodyJSON)
	if err != nil {
		return nil, err
	}
	overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, tombstonedAt, tombstonedBy, tombstoneReason)
	delete(body, "archived_at")
	delete(body, "archived_by")

	updatedBodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode unarchived event body: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE events SET archived_at = NULL, archived_by = NULL, body_json = ? WHERE id = ?`,
		string(updatedBodyJSON), eventID,
	)
	if err != nil {
		return nil, fmt.Errorf("unarchive event: %w", err)
	}

	if typeValue == "message_posted" && threadID.Valid && strings.TrimSpace(threadID.String) != "" {
		desc, err := s.collectMessageDescendantIDs(ctx, threadID.String, eventID)
		if err != nil {
			return nil, err
		}
		for _, childID := range desc {
			if err := s.unarchiveEventCascadeChild(ctx, childID); err != nil {
				return nil, err
			}
		}
	}

	return body, nil
}

func (s *Store) TombstoneEvent(ctx context.Context, actorID, eventID, reason string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, fmt.Errorf("actor_id is required")
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return nil, fmt.Errorf("event_id is required")
	}

	var (
		eventIDScan     string
		typeValue       string
		ts              string
		actorIDScan     string
		threadID        sql.NullString
		refsJSON        string
		payloadJSON     string
		bodyJSON        sql.NullString
		archivedAt      sql.NullString
		archivedBy      sql.NullString
		tombstonedAt    sql.NullString
		tombstonedBy    sql.NullString
		tombstoneReason sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json, body_json,
			archived_at, archived_by, tombstoned_at, tombstoned_by, tombstone_reason
		 FROM events WHERE id = ?`,
		eventID,
	).Scan(&eventIDScan, &typeValue, &ts, &actorIDScan, &threadID, &refsJSON, &payloadJSON, &bodyJSON,
		&archivedAt, &archivedBy, &tombstonedAt, &tombstonedBy, &tombstoneReason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query event for tombstone: %w", err)
	}

	body, err := decodeEventBodyFromRow(eventIDScan, typeValue, ts, actorIDScan, threadID, refsJSON, payloadJSON, bodyJSON)
	if err != nil {
		return nil, err
	}
	overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, tombstonedAt, tombstonedBy, tombstoneReason)

	if tombstonedAt.Valid && strings.TrimSpace(tombstonedAt.String) != "" {
		return body, nil
	}

	delete(body, "archived_at")
	delete(body, "archived_by")

	now := time.Now().UTC().Format(time.RFC3339Nano)
	body["tombstoned_at"] = now
	body["tombstoned_by"] = actorID
	if strings.TrimSpace(reason) != "" {
		body["tombstone_reason"] = strings.TrimSpace(reason)
	}

	updatedBodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode tombstoned event body: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE events SET tombstoned_at = ?, tombstoned_by = ?, tombstone_reason = ?, archived_at = NULL, archived_by = NULL, body_json = ? WHERE id = ?`,
		now, actorID, strings.TrimSpace(reason), string(updatedBodyJSON), eventID,
	)
	if err != nil {
		return nil, fmt.Errorf("tombstone event: %w", err)
	}

	if typeValue == "message_posted" && threadID.Valid && strings.TrimSpace(threadID.String) != "" {
		desc, err := s.collectMessageDescendantIDs(ctx, threadID.String, eventID)
		if err != nil {
			return nil, err
		}
		for _, childID := range desc {
			if err := s.tombstoneEventCascadeChild(ctx, actorID, childID, reason); err != nil {
				return nil, err
			}
		}
	}

	return body, nil
}

func (s *Store) RestoreEvent(ctx context.Context, actorID, eventID string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, fmt.Errorf("actor_id is required")
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return nil, fmt.Errorf("event_id is required")
	}

	var (
		eventIDScan     string
		typeValue       string
		ts              string
		actorIDScan     string
		threadID        sql.NullString
		refsJSON        string
		payloadJSON     string
		bodyJSON        sql.NullString
		archivedAt      sql.NullString
		archivedBy      sql.NullString
		tombstonedAt    sql.NullString
		tombstonedBy    sql.NullString
		tombstoneReason sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json, body_json,
			archived_at, archived_by, tombstoned_at, tombstoned_by, tombstone_reason
		 FROM events WHERE id = ?`,
		eventID,
	).Scan(&eventIDScan, &typeValue, &ts, &actorIDScan, &threadID, &refsJSON, &payloadJSON, &bodyJSON,
		&archivedAt, &archivedBy, &tombstonedAt, &tombstonedBy, &tombstoneReason)
	_ = tombstonedBy
	_ = tombstoneReason
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query event for restore: %w", err)
	}

	if !tombstonedAt.Valid || strings.TrimSpace(tombstonedAt.String) == "" {
		return nil, ErrNotTombstoned
	}

	body, err := decodeEventBodyFromRow(eventIDScan, typeValue, ts, actorIDScan, threadID, refsJSON, payloadJSON, bodyJSON)
	if err != nil {
		return nil, err
	}
	overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, tombstonedAt, tombstonedBy, tombstoneReason)
	delete(body, "tombstoned_at")
	delete(body, "tombstoned_by")
	delete(body, "tombstone_reason")

	updatedBodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode restored event body: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE events SET tombstoned_at = NULL, tombstoned_by = NULL, tombstone_reason = NULL, body_json = ? WHERE id = ?`,
		string(updatedBodyJSON), eventID,
	)
	if err != nil {
		return nil, fmt.Errorf("restore event: %w", err)
	}

	if typeValue == "message_posted" && threadID.Valid && strings.TrimSpace(threadID.String) != "" {
		desc, err := s.collectMessageDescendantIDs(ctx, threadID.String, eventID)
		if err != nil {
			return nil, err
		}
		for _, childID := range desc {
			if err := s.restoreEventCascadeChild(ctx, childID); err != nil {
				return nil, err
			}
		}
	}

	return body, nil
}

func (s *Store) PurgeTombstonedArtifact(ctx context.Context, artifactID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("primitives store database is not initialized")
	}
	artifactID = strings.TrimSpace(artifactID)
	if artifactID == "" {
		return fmt.Errorf("artifact_id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin purge transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var contentHash string
	err = tx.QueryRowContext(ctx,
		`SELECT content_hash FROM artifacts WHERE id = ? AND tombstoned_at IS NOT NULL`,
		artifactID,
	).Scan(&contentHash)
	if errors.Is(err, sql.ErrNoRows) {
		var one int
		err2 := tx.QueryRowContext(ctx, `SELECT 1 FROM artifacts WHERE id = ?`, artifactID).Scan(&one)
		if errors.Is(err2, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err2 != nil {
			return fmt.Errorf("check artifact existence: %w", err2)
		}
		return ErrNotTombstoned
	}
	if err != nil {
		return fmt.Errorf("select tombstoned artifact: %w", err)
	}

	var ref int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM document_revisions WHERE artifact_id = ? LIMIT 1`, artifactID).Scan(&ref)
	if err == nil {
		return ErrArtifactInUse
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("check document revisions referencing artifact: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM artifacts WHERE id = ?`, artifactID); err != nil {
		return fmt.Errorf("delete artifact: %w", err)
	}

	contentHash = strings.TrimSpace(contentHash)
	var shouldDeleteBlob bool
	if contentHash != "" {
		var cnt int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM artifacts WHERE content_hash = ?`, contentHash).Scan(&cnt); err != nil {
			return fmt.Errorf("count artifact blob references: %w", err)
		}
		if cnt == 0 {
			if err := s.removeBlobLedgerEntryTx(ctx, tx, contentHash); err != nil {
				return err
			}
			shouldDeleteBlob = true
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit purge transaction: %w", err)
	}

	if shouldDeleteBlob && s.blob != nil {
		if err := s.blob.Delete(ctx, contentHash); err != nil && !errors.Is(err, blob.ErrBlobNotFound) {
			return fmt.Errorf("delete blob object: %w", err)
		}
	}

	return nil
}

func (s *Store) GetSnapshot(ctx context.Context, id string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}

	row, err := s.getSnapshotRow(ctx, id, "threads")
	if err == nil {
		return row.ToSnapshotMap()
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	row, err = s.getSnapshotRow(ctx, id, "commitments")
	if err != nil {
		return nil, err
	}
	return row.ToSnapshotMap()
}

func (s *Store) PatchSnapshot(ctx context.Context, actorID string, id string, patch map[string]any, ifUpdatedAt *string) (PatchSnapshotResult, error) {
	if s == nil || s.db == nil {
		return PatchSnapshotResult{}, fmt.Errorf("primitives store database is not initialized")
	}
	if strings.TrimSpace(actorID) == "" {
		return PatchSnapshotResult{}, fmt.Errorf("actorID is required")
	}
	if patch == nil {
		return PatchSnapshotResult{}, fmt.Errorf("snapshot patch is required")
	}

	var (
		snapshotID     string
		snapshotKind   string
		threadID       sql.NullString
		provenanceJSON string
		bodyJSON       string
	)
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, kind, thread_id, provenance_json, body_json FROM threads WHERE id = ?`,
		id,
	).Scan(&snapshotID, &snapshotKind, &threadID, &provenanceJSON, &bodyJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return PatchSnapshotResult{}, ErrNotFound
	}
	if err != nil {
		return PatchSnapshotResult{}, fmt.Errorf("query snapshot before patch: %w", err)
	}

	current := map[string]any{}
	if strings.TrimSpace(bodyJSON) != "" {
		if err := json.Unmarshal([]byte(bodyJSON), &current); err != nil {
			return PatchSnapshotResult{}, fmt.Errorf("decode current snapshot body: %w", err)
		}
	}

	currentProvenance := map[string]any{}
	if strings.TrimSpace(provenanceJSON) != "" {
		if err := json.Unmarshal([]byte(provenanceJSON), &currentProvenance); err != nil {
			return PatchSnapshotResult{}, fmt.Errorf("decode current snapshot provenance: %w", err)
		}
	}

	bodyPatch := cloneMap(patch)
	nextProvenance := cloneMap(currentProvenance)
	provenanceChanged := false
	if rawProvenance, hasProvenance := bodyPatch["provenance"]; hasProvenance {
		provenancePatch, ok := rawProvenance.(map[string]any)
		if !ok {
			return PatchSnapshotResult{}, fmt.Errorf("snapshot.provenance must be an object")
		}
		nextProvenance = cloneMap(provenancePatch)
		delete(bodyPatch, "provenance")
		provenanceChanged = !reflect.DeepEqual(currentProvenance, nextProvenance)
	}

	changedFields := make([]string, 0, len(bodyPatch)+1)
	for key, incoming := range bodyPatch {
		existing, exists := current[key]
		if !exists || !reflect.DeepEqual(existing, incoming) {
			changedFields = append(changedFields, key)
		}
		current[key] = incoming
	}
	if provenanceChanged {
		changedFields = append(changedFields, "provenance")
	}
	sort.Strings(changedFields)

	updatedAt := time.Now().UTC().Format(time.RFC3339Nano)

	updatedBodyJSON, err := json.Marshal(current)
	if err != nil {
		return PatchSnapshotResult{}, fmt.Errorf("encode patched snapshot body: %w", err)
	}
	updatedProvenanceJSON, err := json.Marshal(nextProvenance)
	if err != nil {
		return PatchSnapshotResult{}, fmt.Errorf("encode patched snapshot provenance: %w", err)
	}
	filterColumns := snapshotFilterColumnsForKind(snapshotKind, current)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PatchSnapshotResult{}, fmt.Errorf("begin snapshot patch transaction: %w", err)
	}

	var updateResult sql.Result
	if ifUpdatedAt != nil {
		updateResult, err = tx.ExecContext(
			ctx,
			`UPDATE threads
			 SET body_json = ?, provenance_json = ?, updated_at = ?, updated_by = ?,
			     filter_status = ?, filter_priority = ?, filter_owner = ?, filter_due_at = ?,
			     filter_cadence = ?, filter_cadence_preset = ?, filter_tags_json = ?
			 WHERE id = ? AND updated_at = ?`,
			string(updatedBodyJSON),
			string(updatedProvenanceJSON),
			updatedAt,
			actorID,
			nullableString(filterColumns.Status),
			nullableString(filterColumns.Priority),
			nullableString(filterColumns.Owner),
			nullableString(filterColumns.DueAt),
			nullableString(filterColumns.Cadence),
			nullableString(filterColumns.CadencePreset),
			filterColumns.TagsJSON,
			snapshotID,
			*ifUpdatedAt,
		)
	} else {
		updateResult, err = tx.ExecContext(
			ctx,
			`UPDATE threads
			 SET body_json = ?, provenance_json = ?, updated_at = ?, updated_by = ?,
			     filter_status = ?, filter_priority = ?, filter_owner = ?, filter_due_at = ?,
			     filter_cadence = ?, filter_cadence_preset = ?, filter_tags_json = ?
			 WHERE id = ?`,
			string(updatedBodyJSON),
			string(updatedProvenanceJSON),
			updatedAt,
			actorID,
			nullableString(filterColumns.Status),
			nullableString(filterColumns.Priority),
			nullableString(filterColumns.Owner),
			nullableString(filterColumns.DueAt),
			nullableString(filterColumns.Cadence),
			nullableString(filterColumns.CadencePreset),
			filterColumns.TagsJSON,
			snapshotID,
		)
	}
	if err != nil {
		_ = tx.Rollback()
		return PatchSnapshotResult{}, fmt.Errorf("update snapshot: %w", err)
	}
	if ifUpdatedAt != nil {
		rowsAffected, err := updateResult.RowsAffected()
		if err != nil {
			_ = tx.Rollback()
			return PatchSnapshotResult{}, fmt.Errorf("read patch snapshot rows affected: %w", err)
		}
		if rowsAffected == 0 {
			_ = tx.Rollback()
			return PatchSnapshotResult{}, ErrConflict
		}
	}

	eventPayload := map[string]any{
		"changed_fields": changedFields,
	}
	event := map[string]any{
		"type":       "snapshot_updated",
		"refs":       []string{"snapshot:" + snapshotID},
		"summary":    "snapshot updated",
		"payload":    eventPayload,
		"provenance": actorStatementProvenance(),
	}
	if threadID.Valid {
		event["thread_id"] = threadID.String
	}

	preparedEvent, err := prepareEventForInsert(actorID, event)
	if err != nil {
		_ = tx.Rollback()
		return PatchSnapshotResult{}, fmt.Errorf("prepare snapshot_updated event: %w", err)
	}
	if err := insertPreparedEvent(ctx, tx, preparedEvent); err != nil {
		_ = tx.Rollback()
		return PatchSnapshotResult{}, fmt.Errorf("emit snapshot_updated event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return PatchSnapshotResult{}, fmt.Errorf("commit snapshot patch transaction: %w", err)
	}

	current["id"] = snapshotID
	if _, hasType := current["type"]; !hasType {
		current["type"] = snapshotKind
	}
	current["updated_at"] = updatedAt
	current["updated_by"] = actorID
	if threadID.Valid {
		current["thread_id"] = threadID.String
	}
	current["provenance"] = nextProvenance

	return PatchSnapshotResult{
		Snapshot: current,
		Event:    preparedEvent.Body,
	}, nil
}

func (s *Store) CreateThread(ctx context.Context, actorID string, thread map[string]any) (PatchSnapshotResult, error) {
	if s == nil || s.db == nil {
		return PatchSnapshotResult{}, fmt.Errorf("primitives store database is not initialized")
	}
	if strings.TrimSpace(actorID) == "" {
		return PatchSnapshotResult{}, fmt.Errorf("actorID is required")
	}
	if thread == nil {
		return PatchSnapshotResult{}, fmt.Errorf("thread is required")
	}

	threadID, _ := thread["id"].(string)
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		threadID = uuid.NewString()
	}
	updatedAt := time.Now().UTC().Format(time.RFC3339Nano)

	body := cloneMap(thread)
	delete(body, "id")
	delete(body, "provenance")
	body["open_commitments"] = []string{}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return PatchSnapshotResult{}, fmt.Errorf("marshal thread snapshot body: %w", err)
	}

	provenance := map[string]any{}
	if rawProvenance, ok := thread["provenance"].(map[string]any); ok {
		provenance = rawProvenance
	}
	provenanceJSON, err := json.Marshal(provenance)
	if err != nil {
		return PatchSnapshotResult{}, fmt.Errorf("marshal thread provenance: %w", err)
	}
	filterColumns := snapshotFilterColumnsForKind("thread", body)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PatchSnapshotResult{}, fmt.Errorf("begin thread create transaction: %w", err)
	}

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO threads(id, kind, thread_id, updated_at, updated_by, body_json, provenance_json, filter_status, filter_priority, filter_owner, filter_due_at, filter_cadence, filter_cadence_preset, filter_tags_json)
		 VALUES (?, 'thread', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		threadID,
		threadID,
		updatedAt,
		actorID,
		string(bodyJSON),
		string(provenanceJSON),
		nullableString(filterColumns.Status),
		nullableString(filterColumns.Priority),
		nil,
		nil,
		nullableString(filterColumns.Cadence),
		nullableString(filterColumns.CadencePreset),
		filterColumns.TagsJSON,
	)
	if err != nil {
		_ = tx.Rollback()
		if isUniqueViolation(err) {
			return PatchSnapshotResult{}, ErrConflict
		}
		return PatchSnapshotResult{}, fmt.Errorf("insert thread snapshot: %w", err)
	}

	changedFields := make([]string, 0, len(body))
	for key := range body {
		changedFields = append(changedFields, key)
	}
	changedFields = append(changedFields, "provenance")
	sort.Strings(changedFields)

	event := map[string]any{
		"type":       "snapshot_updated",
		"thread_id":  threadID,
		"refs":       []string{"snapshot:" + threadID},
		"summary":    "thread snapshot created",
		"payload":    map[string]any{"changed_fields": changedFields},
		"provenance": actorStatementProvenance(),
	}
	preparedEvent, err := prepareEventForInsert(actorID, event)
	if err != nil {
		_ = tx.Rollback()
		return PatchSnapshotResult{}, fmt.Errorf("prepare thread snapshot_updated event: %w", err)
	}
	if err := insertPreparedEvent(ctx, tx, preparedEvent); err != nil {
		_ = tx.Rollback()
		return PatchSnapshotResult{}, fmt.Errorf("emit thread snapshot_updated event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return PatchSnapshotResult{}, fmt.Errorf("commit thread create transaction: %w", err)
	}

	out := cloneMap(body)
	out["id"] = threadID
	// Thread domain `type` is provided by caller (thread_type enum).
	out["thread_id"] = threadID
	out["updated_at"] = updatedAt
	out["updated_by"] = actorID
	out["provenance"] = provenance

	return PatchSnapshotResult{
		Snapshot: out,
		Event:    preparedEvent.Body,
	}, nil
}

func (s *Store) GetThread(ctx context.Context, id string) (map[string]any, error) {
	row, err := s.getSnapshotRow(ctx, id, "threads")
	if err != nil {
		return nil, err
	}
	if row.Kind != "thread" {
		return nil, ErrNotFound
	}
	return row.ToSnapshotMap()
}

func (s *Store) PatchThread(ctx context.Context, actorID string, id string, patch map[string]any, ifUpdatedAt *string) (PatchSnapshotResult, error) {
	row, err := s.getSnapshotRow(ctx, id, "threads")
	if err != nil {
		return PatchSnapshotResult{}, err
	}
	if row.Kind != "thread" {
		return PatchSnapshotResult{}, ErrNotFound
	}
	return s.PatchSnapshot(ctx, actorID, id, patch, ifUpdatedAt)
}

func (s *Store) ListThreads(ctx context.Context, filter ThreadListFilter) ([]map[string]any, string, error) {
	if s == nil || s.db == nil {
		return nil, "", fmt.Errorf("primitives store database is not initialized")
	}
	if filter.Cursor != "" {
		if _, err := decodeCursor(filter.Cursor); err != nil {
			return nil, "", fmt.Errorf("%w: %v", ErrInvalidCursor, err)
		}
	}

	query, args := buildListThreadsQuery(filter)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("query threads: %w", err)
	}
	defer rows.Close()

	threads := make([]map[string]any, 0)
	for rows.Next() {
		row, err := scanSnapshotRow(rows)
		if err != nil {
			return nil, "", err
		}
		snapshot, err := row.ToSnapshotMap()
		if err != nil {
			return nil, "", err
		}

		threads = append(threads, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate threads: %w", err)
	}

	var nextCursor string
	if filter.Limit != nil && len(threads) > *filter.Limit {
		threads = threads[:*filter.Limit]
		offset := 0
		if filter.Cursor != "" {
			offset, _ = decodeCursor(filter.Cursor)
		}
		nextCursor = encodeCursor(offset + *filter.Limit)
	}

	return threads, nextCursor, nil
}

func (s *Store) ArchiveThread(ctx context.Context, actorID, threadID string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, fmt.Errorf("actor_id is required")
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil, fmt.Errorf("thread_id is required")
	}
	row, err := s.getSnapshotRow(ctx, threadID, "threads")
	if err != nil {
		return nil, err
	}
	if row.Kind != "thread" {
		return nil, ErrNotFound
	}
	if row.TombstonedAt.Valid && strings.TrimSpace(row.TombstonedAt.String) != "" {
		return nil, ErrAlreadyTombstoned
	}
	if row.ArchivedAt.Valid && strings.TrimSpace(row.ArchivedAt.String) != "" {
		return row.ToSnapshotMap()
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.db.ExecContext(ctx,
		`UPDATE threads SET archived_at = ?, archived_by = ? WHERE id = ?`,
		now, actorID, threadID,
	); err != nil {
		return nil, fmt.Errorf("archive thread: %w", err)
	}
	row, err = s.getSnapshotRow(ctx, threadID, "threads")
	if err != nil {
		return nil, err
	}
	return row.ToSnapshotMap()
}

func (s *Store) UnarchiveThread(ctx context.Context, actorID, threadID string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, fmt.Errorf("actor_id is required")
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil, fmt.Errorf("thread_id is required")
	}
	row, err := s.getSnapshotRow(ctx, threadID, "threads")
	if err != nil {
		return nil, err
	}
	if row.Kind != "thread" {
		return nil, ErrNotFound
	}
	if !row.ArchivedAt.Valid || strings.TrimSpace(row.ArchivedAt.String) == "" {
		return nil, ErrNotArchived
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE threads SET archived_at = NULL, archived_by = NULL WHERE id = ?`,
		threadID,
	); err != nil {
		return nil, fmt.Errorf("unarchive thread: %w", err)
	}
	row, err = s.getSnapshotRow(ctx, threadID, "threads")
	if err != nil {
		return nil, err
	}
	return row.ToSnapshotMap()
}

func (s *Store) TombstoneThread(ctx context.Context, actorID, threadID, reason string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, fmt.Errorf("actor_id is required")
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil, fmt.Errorf("thread_id is required")
	}
	row, err := s.getSnapshotRow(ctx, threadID, "threads")
	if err != nil {
		return nil, err
	}
	if row.Kind != "thread" {
		return nil, ErrNotFound
	}
	if row.TombstonedAt.Valid && strings.TrimSpace(row.TombstonedAt.String) != "" {
		return row.ToSnapshotMap()
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.db.ExecContext(ctx,
		`UPDATE threads SET tombstoned_at = ?, tombstoned_by = ?, tombstone_reason = ?, archived_at = NULL, archived_by = NULL WHERE id = ?`,
		now, actorID, strings.TrimSpace(reason), threadID,
	); err != nil {
		return nil, fmt.Errorf("tombstone thread: %w", err)
	}
	row, err = s.getSnapshotRow(ctx, threadID, "threads")
	if err != nil {
		return nil, err
	}
	return row.ToSnapshotMap()
}

func (s *Store) RestoreThread(ctx context.Context, actorID, threadID string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, fmt.Errorf("actor_id is required")
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil, fmt.Errorf("thread_id is required")
	}
	row, err := s.getSnapshotRow(ctx, threadID, "threads")
	if err != nil {
		return nil, err
	}
	if row.Kind != "thread" {
		return nil, ErrNotFound
	}
	if !row.TombstonedAt.Valid || strings.TrimSpace(row.TombstonedAt.String) == "" {
		return nil, ErrNotTombstoned
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE threads SET tombstoned_at = NULL, tombstoned_by = NULL, tombstone_reason = NULL WHERE id = ?`,
		threadID,
	); err != nil {
		return nil, fmt.Errorf("restore thread: %w", err)
	}
	row, err = s.getSnapshotRow(ctx, threadID, "threads")
	if err != nil {
		return nil, err
	}
	return row.ToSnapshotMap()
}

func (s *Store) PurgeThread(ctx context.Context, threadID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("primitives store database is not initialized")
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return fmt.Errorf("thread_id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin purge thread transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var foundID string
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM threads WHERE id = ? AND tombstoned_at IS NOT NULL`,
		threadID,
	).Scan(&foundID)
	if errors.Is(err, sql.ErrNoRows) {
		var one int
		err2 := tx.QueryRowContext(ctx, `SELECT 1 FROM threads WHERE id = ?`, threadID).Scan(&one)
		if errors.Is(err2, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err2 != nil {
			return fmt.Errorf("check thread existence: %w", err2)
		}
		return ErrNotTombstoned
	}
	if err != nil {
		return fmt.Errorf("select tombstoned thread: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM derived_thread_views WHERE thread_id = ?`, threadID); err != nil {
		return fmt.Errorf("delete derived_thread_views: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM derived_inbox_items WHERE thread_id = ?`, threadID); err != nil {
		return fmt.Errorf("delete derived_inbox_items: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM derived_thread_dirty_queue WHERE thread_id = ?`, threadID); err != nil {
		return fmt.Errorf("delete derived_thread_dirty_queue: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM threads WHERE id = ?`, threadID); err != nil {
		return fmt.Errorf("delete thread snapshot: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM commitments WHERE thread_id = ?`, threadID); err != nil {
		return fmt.Errorf("delete commitments: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit purge thread transaction: %w", err)
	}
	return nil
}

func (s *Store) ListEventsByThread(ctx context.Context, threadID string) ([]map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json, body_json,
			archived_at, archived_by, tombstoned_at, tombstoned_by, tombstone_reason
		 FROM events
		 WHERE thread_id = ?
		 ORDER BY ts ASC, id ASC`,
		threadID,
	)
	if err != nil {
		return nil, fmt.Errorf("query thread events: %w", err)
	}
	defer rows.Close()

	events := make([]map[string]any, 0)
	for rows.Next() {
		var (
			eventID         string
			typeValue       string
			ts              string
			actorID         string
			thread          sql.NullString
			refsJSON        string
			payloadJSON     string
			bodyJSON        sql.NullString
			archivedAt      sql.NullString
			archivedBy      sql.NullString
			tombstonedAt    sql.NullString
			tombstonedBy    sql.NullString
			tombstoneReason sql.NullString
		)
		if err := rows.Scan(&eventID, &typeValue, &ts, &actorID, &thread, &refsJSON, &payloadJSON, &bodyJSON,
			&archivedAt, &archivedBy, &tombstonedAt, &tombstonedBy, &tombstoneReason); err != nil {
			return nil, fmt.Errorf("scan thread event: %w", err)
		}

		if bodyJSON.Valid && strings.TrimSpace(bodyJSON.String) != "" && bodyJSON.String != "{}" {
			body := map[string]any{}
			if err := json.Unmarshal([]byte(bodyJSON.String), &body); err != nil {
				return nil, fmt.Errorf("decode event body: %w", err)
			}
			overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, tombstonedAt, tombstonedBy, tombstoneReason)
			events = append(events, body)
			continue
		}

		refs := make([]string, 0)
		if err := json.Unmarshal([]byte(refsJSON), &refs); err != nil {
			return nil, fmt.Errorf("decode event refs: %w", err)
		}
		payload := map[string]any{}
		if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
			return nil, fmt.Errorf("decode event payload: %w", err)
		}

		event := map[string]any{
			"id":       eventID,
			"type":     typeValue,
			"ts":       ts,
			"actor_id": actorID,
			"refs":     refs,
			"payload":  payload,
		}
		if thread.Valid {
			event["thread_id"] = thread.String
		}
		overlayEventLifecycleFromSQLColumns(event, archivedAt, archivedBy, tombstonedAt, tombstonedBy, tombstoneReason)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate thread events: %w", err)
	}

	return events, nil
}

func (s *Store) ListRecentEventsByThread(ctx context.Context, threadID string, limit int) ([]map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	if limit <= 0 {
		return []map[string]any{}, nil
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json, body_json,
			archived_at, archived_by, tombstoned_at, tombstoned_by, tombstone_reason
		 FROM events
		 WHERE thread_id = ?
		 ORDER BY ts DESC, id DESC
		 LIMIT ?`,
		threadID,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query recent thread events: %w", err)
	}
	defer rows.Close()

	recentDescending := make([]map[string]any, 0, limit)
	for rows.Next() {
		var (
			eventID         string
			typeValue       string
			ts              string
			actorID         string
			thread          sql.NullString
			refsJSON        string
			payloadJSON     string
			bodyJSON        sql.NullString
			archivedAt      sql.NullString
			archivedBy      sql.NullString
			tombstonedAt    sql.NullString
			tombstonedBy    sql.NullString
			tombstoneReason sql.NullString
		)
		if err := rows.Scan(&eventID, &typeValue, &ts, &actorID, &thread, &refsJSON, &payloadJSON, &bodyJSON,
			&archivedAt, &archivedBy, &tombstonedAt, &tombstonedBy, &tombstoneReason); err != nil {
			return nil, fmt.Errorf("scan recent thread event: %w", err)
		}

		if bodyJSON.Valid && strings.TrimSpace(bodyJSON.String) != "" && bodyJSON.String != "{}" {
			body := map[string]any{}
			if err := json.Unmarshal([]byte(bodyJSON.String), &body); err != nil {
				return nil, fmt.Errorf("decode recent thread event body: %w", err)
			}
			overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, tombstonedAt, tombstonedBy, tombstoneReason)
			recentDescending = append(recentDescending, body)
			continue
		}

		refs := make([]string, 0)
		if err := json.Unmarshal([]byte(refsJSON), &refs); err != nil {
			return nil, fmt.Errorf("decode recent thread event refs: %w", err)
		}
		payload := map[string]any{}
		if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
			return nil, fmt.Errorf("decode recent thread event payload: %w", err)
		}

		event := map[string]any{
			"id":       eventID,
			"type":     typeValue,
			"ts":       ts,
			"actor_id": actorID,
			"refs":     refs,
			"payload":  payload,
		}
		if thread.Valid {
			event["thread_id"] = thread.String
		}
		overlayEventLifecycleFromSQLColumns(event, archivedAt, archivedBy, tombstonedAt, tombstonedBy, tombstoneReason)
		recentDescending = append(recentDescending, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent thread events: %w", err)
	}

	return reverseEvents(recentDescending), nil
}

func reverseEvents(events []map[string]any) []map[string]any {
	if len(events) <= 1 {
		return events
	}
	for left, right := 0, len(events)-1; left < right; left, right = left+1, right-1 {
		events[left], events[right] = events[right], events[left]
	}
	return events
}

func prepareEventForInsert(actorID string, event map[string]any) (preparedEvent, error) {
	body := cloneMap(event)
	eventID, _ := body["id"].(string)
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		eventID = uuid.NewString()
	}
	body["id"] = eventID
	body["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
	body["actor_id"] = actorID
	replaceActorStatementProvenancePlaceholder(body, eventID)

	typeValue, _ := body["type"].(string)
	threadID, _ := body["thread_id"].(string)
	refs, err := normalizeStringSlice(body["refs"])
	if err != nil {
		return preparedEvent{}, fmt.Errorf("event.refs: %w", err)
	}

	refsJSON, err := json.Marshal(refs)
	if err != nil {
		return preparedEvent{}, fmt.Errorf("marshal event refs: %w", err)
	}

	payload := map[string]any{}
	if rawPayload, ok := body["payload"]; ok && rawPayload != nil {
		switch p := rawPayload.(type) {
		case map[string]any:
			payload = p
		default:
			return preparedEvent{}, fmt.Errorf("event.payload must be an object when provided")
		}
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return preparedEvent{}, fmt.Errorf("marshal event payload: %w", err)
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return preparedEvent{}, fmt.Errorf("marshal event body: %w", err)
	}

	return preparedEvent{
		Body:        body,
		Type:        typeValue,
		ThreadID:    threadID,
		RefsJSON:    string(refsJSON),
		PayloadJSON: string(payloadJSON),
		BodyJSON:    string(bodyJSON),
	}, nil
}

func insertPreparedEvent(ctx context.Context, exec eventExec, prepared preparedEvent) error {
	if _, err := exec.ExecContext(
		ctx,
		`INSERT INTO events(id, type, ts, actor_id, thread_id, refs_json, payload_json, body_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		prepared.Body["id"],
		prepared.Type,
		prepared.Body["ts"],
		prepared.Body["actor_id"],
		prepared.ThreadID,
		prepared.RefsJSON,
		prepared.PayloadJSON,
		prepared.BodyJSON,
	); err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("insert event: %w", err)
	}

	return nil
}

func (s *Store) ListEvents(ctx context.Context, filter EventListFilter) ([]map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}

	query := `SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json, body_json,
		archived_at, archived_by, tombstoned_at, tombstoned_by, tombstone_reason
		FROM events
		WHERE 1=1`
	args := make([]any, 0)

	if len(filter.Types) > 0 {
		placeholders := make([]string, 0, len(filter.Types))
		for _, eventType := range filter.Types {
			placeholders = append(placeholders, "?")
			args = append(args, eventType)
		}
		query += ` AND type IN (` + strings.Join(placeholders, ",") + `)`
	}
	query += ` ORDER BY ts DESC, id ASC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	events := make([]map[string]any, 0)
	for rows.Next() {
		var (
			eventID         string
			typeValue       string
			ts              string
			actorID         string
			thread          sql.NullString
			refsJSON        string
			payloadJSON     string
			bodyJSON        sql.NullString
			archivedAt      sql.NullString
			archivedBy      sql.NullString
			tombstonedAt    sql.NullString
			tombstonedBy    sql.NullString
			tombstoneReason sql.NullString
		)
		if err := rows.Scan(&eventID, &typeValue, &ts, &actorID, &thread, &refsJSON, &payloadJSON, &bodyJSON,
			&archivedAt, &archivedBy, &tombstonedAt, &tombstonedBy, &tombstoneReason); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}

		if bodyJSON.Valid && strings.TrimSpace(bodyJSON.String) != "" && bodyJSON.String != "{}" {
			body := map[string]any{}
			if err := json.Unmarshal([]byte(bodyJSON.String), &body); err != nil {
				return nil, fmt.Errorf("decode event body: %w", err)
			}
			overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, tombstonedAt, tombstonedBy, tombstoneReason)
			events = append(events, body)
			continue
		}

		refs := make([]string, 0)
		if err := json.Unmarshal([]byte(refsJSON), &refs); err != nil {
			return nil, fmt.Errorf("decode event refs: %w", err)
		}
		payload := map[string]any{}
		if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
			return nil, fmt.Errorf("decode event payload: %w", err)
		}

		event := map[string]any{
			"id":       eventID,
			"type":     typeValue,
			"ts":       ts,
			"actor_id": actorID,
			"refs":     refs,
			"payload":  payload,
		}
		if thread.Valid {
			event["thread_id"] = thread.String
		}
		overlayEventLifecycleFromSQLColumns(event, archivedAt, archivedBy, tombstonedAt, tombstonedBy, tombstoneReason)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}

	return events, nil
}

func (s *Store) ListEventsAfter(ctx context.Context, filter EventListFilter, cursor EventCursor, limit int) ([]map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	if limit <= 0 {
		limit = 100
	}

	query := `SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json, body_json,
		archived_at, archived_by, tombstoned_at, tombstoned_by, tombstone_reason
		FROM events
		WHERE 1=1`
	args := make([]any, 0, len(filter.Types)+3)

	if len(filter.Types) > 0 {
		placeholders := make([]string, 0, len(filter.Types))
		for _, eventType := range filter.Types {
			placeholders = append(placeholders, "?")
			args = append(args, eventType)
		}
		query += ` AND type IN (` + strings.Join(placeholders, ",") + `)`
	}
	if strings.TrimSpace(cursor.TS) != "" {
		query += ` AND (julianday(ts) > julianday(?) OR (julianday(ts) = julianday(?) AND id > ?))`
		args = append(args, strings.TrimSpace(cursor.TS), strings.TrimSpace(cursor.TS), strings.TrimSpace(cursor.ID))
	}
	query += ` ORDER BY julianday(ts) ASC, id ASC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query events after cursor: %w", err)
	}
	defer rows.Close()

	events := make([]map[string]any, 0)
	for rows.Next() {
		var (
			eventID         string
			typeValue       string
			ts              string
			actorID         string
			thread          sql.NullString
			refsJSON        string
			payloadJSON     string
			bodyJSON        sql.NullString
			archivedAt      sql.NullString
			archivedBy      sql.NullString
			tombstonedAt    sql.NullString
			tombstonedBy    sql.NullString
			tombstoneReason sql.NullString
		)
		if err := rows.Scan(&eventID, &typeValue, &ts, &actorID, &thread, &refsJSON, &payloadJSON, &bodyJSON,
			&archivedAt, &archivedBy, &tombstonedAt, &tombstonedBy, &tombstoneReason); err != nil {
			return nil, fmt.Errorf("scan event after cursor: %w", err)
		}

		if bodyJSON.Valid && strings.TrimSpace(bodyJSON.String) != "" && bodyJSON.String != "{}" {
			body := map[string]any{}
			if err := json.Unmarshal([]byte(bodyJSON.String), &body); err != nil {
				return nil, fmt.Errorf("decode event body after cursor: %w", err)
			}
			overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, tombstonedAt, tombstonedBy, tombstoneReason)
			events = append(events, body)
			continue
		}

		refs := make([]string, 0)
		if err := json.Unmarshal([]byte(refsJSON), &refs); err != nil {
			return nil, fmt.Errorf("decode event refs after cursor: %w", err)
		}
		payload := map[string]any{}
		if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
			return nil, fmt.Errorf("decode event payload after cursor: %w", err)
		}

		event := map[string]any{
			"id":       eventID,
			"type":     typeValue,
			"ts":       ts,
			"actor_id": actorID,
			"refs":     refs,
			"payload":  payload,
		}
		if thread.Valid {
			event["thread_id"] = thread.String
		}
		overlayEventLifecycleFromSQLColumns(event, archivedAt, archivedBy, tombstonedAt, tombstonedBy, tombstoneReason)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events after cursor: %w", err)
	}
	return events, nil
}

func (s *Store) CreateCommitment(ctx context.Context, actorID string, commitment map[string]any) (PatchSnapshotResult, error) {
	if s == nil || s.db == nil {
		return PatchSnapshotResult{}, fmt.Errorf("primitives store database is not initialized")
	}
	if strings.TrimSpace(actorID) == "" {
		return PatchSnapshotResult{}, fmt.Errorf("actorID is required")
	}
	if commitment == nil {
		return PatchSnapshotResult{}, fmt.Errorf("commitment is required")
	}

	threadID, _ := commitment["thread_id"].(string)
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return PatchSnapshotResult{}, fmt.Errorf("commitment.thread_id is required")
	}

	threadRow, err := s.getSnapshotRow(ctx, threadID, "threads")
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return PatchSnapshotResult{}, ErrNotFound
		}
		return PatchSnapshotResult{}, fmt.Errorf("load thread for commitment: %w", err)
	}
	if threadRow.Kind != "thread" {
		return PatchSnapshotResult{}, ErrNotFound
	}

	commitmentID, _ := commitment["id"].(string)
	commitmentID = strings.TrimSpace(commitmentID)
	if commitmentID == "" {
		commitmentID = uuid.NewString()
	}
	updatedAt := time.Now().UTC().Format(time.RFC3339Nano)

	body := cloneMap(commitment)
	delete(body, "id")
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return PatchSnapshotResult{}, fmt.Errorf("marshal commitment snapshot body: %w", err)
	}

	provenance := map[string]any{}
	if rawProvenance, ok := commitment["provenance"].(map[string]any); ok {
		provenance = rawProvenance
	}
	provenanceJSON, err := json.Marshal(provenance)
	if err != nil {
		return PatchSnapshotResult{}, fmt.Errorf("marshal commitment provenance: %w", err)
	}
	filterColumns := snapshotFilterColumnsForKind("commitment", body)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PatchSnapshotResult{}, fmt.Errorf("begin commitment create transaction: %w", err)
	}

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO commitments(id, kind, thread_id, updated_at, updated_by, body_json, provenance_json, filter_status, filter_priority, filter_owner, filter_due_at, filter_cadence, filter_cadence_preset, filter_tags_json)
		 VALUES (?, 'commitment', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		commitmentID,
		threadID,
		updatedAt,
		actorID,
		string(bodyJSON),
		string(provenanceJSON),
		nullableString(filterColumns.Status),
		nil,
		nullableString(filterColumns.Owner),
		nullableString(filterColumns.DueAt),
		nil,
		nil,
		filterColumns.TagsJSON,
	)
	if err != nil {
		_ = tx.Rollback()
		if isUniqueViolation(err) {
			return PatchSnapshotResult{}, ErrConflict
		}
		return PatchSnapshotResult{}, fmt.Errorf("insert commitment snapshot: %w", err)
	}

	event := map[string]any{
		"type":       "commitment_created",
		"thread_id":  threadID,
		"refs":       []string{"snapshot:" + commitmentID},
		"summary":    "commitment created",
		"payload":    map[string]any{"changed_fields": sortedKeys(body)},
		"provenance": actorStatementProvenance(),
	}
	preparedEvent, err := prepareEventForInsert(actorID, event)
	if err != nil {
		_ = tx.Rollback()
		return PatchSnapshotResult{}, fmt.Errorf("prepare commitment_created event: %w", err)
	}
	if err := insertPreparedEvent(ctx, tx, preparedEvent); err != nil {
		_ = tx.Rollback()
		return PatchSnapshotResult{}, fmt.Errorf("emit commitment_created event: %w", err)
	}

	if err := s.recomputeThreadOpenCommitmentsTx(ctx, tx, actorID, threadID); err != nil {
		_ = tx.Rollback()
		return PatchSnapshotResult{}, fmt.Errorf("recompute thread open_commitments after commitment create: %w", err)
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return PatchSnapshotResult{}, fmt.Errorf("commit commitment create transaction: %w", err)
	}

	out := cloneMap(body)
	out["id"] = commitmentID
	if _, hasType := out["type"]; !hasType {
		out["type"] = "commitment"
	}
	out["thread_id"] = threadID
	out["updated_at"] = updatedAt
	out["updated_by"] = actorID
	out["provenance"] = provenance

	return PatchSnapshotResult{
		Snapshot: out,
		Event:    preparedEvent.Body,
	}, nil
}

func (s *Store) GetCommitment(ctx context.Context, id string) (map[string]any, error) {
	row, err := s.getSnapshotRow(ctx, id, "commitments")
	if err != nil {
		return nil, err
	}
	if row.Kind != "commitment" {
		return nil, ErrNotFound
	}
	return row.ToSnapshotMap()
}

func (s *Store) PatchCommitment(ctx context.Context, actorID string, id string, patch map[string]any, refs []string, ifUpdatedAt *string) (PatchSnapshotResult, error) {
	if s == nil || s.db == nil {
		return PatchSnapshotResult{}, fmt.Errorf("primitives store database is not initialized")
	}
	if strings.TrimSpace(actorID) == "" {
		return PatchSnapshotResult{}, fmt.Errorf("actorID is required")
	}
	if patch == nil || len(patch) == 0 {
		return PatchSnapshotResult{}, fmt.Errorf("commitment patch is required")
	}

	row, err := s.getSnapshotRow(ctx, id, "commitments")
	if err != nil {
		return PatchSnapshotResult{}, err
	}
	if row.Kind != "commitment" {
		return PatchSnapshotResult{}, ErrNotFound
	}

	currentSnapshot, err := row.ToSnapshotMap()
	if err != nil {
		return PatchSnapshotResult{}, err
	}
	threadID, _ := currentSnapshot["thread_id"].(string)
	if strings.TrimSpace(threadID) == "" {
		return PatchSnapshotResult{}, fmt.Errorf("commitment is missing thread_id")
	}
	if rawThreadID, hasThreadID := patch["thread_id"]; hasThreadID {
		patchedThreadID, ok := rawThreadID.(string)
		if !ok || strings.TrimSpace(patchedThreadID) == "" {
			return PatchSnapshotResult{}, fmt.Errorf("commitment.thread_id must be a non-empty string")
		}
		if strings.TrimSpace(patchedThreadID) != threadID {
			return PatchSnapshotResult{}, fmt.Errorf("commitment.thread_id cannot be changed")
		}
	}

	currentBody := cloneMap(currentSnapshot)
	delete(currentBody, "id")
	delete(currentBody, "updated_at")
	delete(currentBody, "updated_by")
	delete(currentBody, "provenance")

	previousStatus, _ := currentBody["status"].(string)

	changedFields := make([]string, 0, len(patch))
	for key, incoming := range patch {
		existing, exists := currentBody[key]
		if !exists || !reflect.DeepEqual(existing, incoming) {
			changedFields = append(changedFields, key)
		}
		currentBody[key] = incoming
	}
	sort.Strings(changedFields)

	newStatus, _ := currentBody["status"].(string)
	statusChanged := containsString(changedFields, "status") && previousStatus != newStatus

	if statusChanged {
		if err := enforceRestrictedCommitmentTransition(newStatus, refs); err != nil {
			return PatchSnapshotResult{}, err
		}
	}

	provenance := map[string]any{}
	if rawProvenance, ok := currentSnapshot["provenance"].(map[string]any); ok {
		provenance = cloneMap(rawProvenance)
	}
	if statusChanged {
		labels := statusEvidenceLabels(newStatus, refs)
		if len(labels) > 0 {
			byField := map[string]any{}
			if rawByField, ok := provenance["by_field"].(map[string]any); ok {
				byField = cloneMap(rawByField)
			}
			byField["status"] = labels
			provenance["by_field"] = byField
		}
	}

	updatedAt := time.Now().UTC().Format(time.RFC3339Nano)
	bodyJSON, err := json.Marshal(currentBody)
	if err != nil {
		return PatchSnapshotResult{}, fmt.Errorf("encode patched commitment body: %w", err)
	}
	provenanceJSON, err := json.Marshal(provenance)
	if err != nil {
		return PatchSnapshotResult{}, fmt.Errorf("encode patched commitment provenance: %w", err)
	}
	filterColumns := snapshotFilterColumnsForKind("commitment", currentBody)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PatchSnapshotResult{}, fmt.Errorf("begin commitment patch transaction: %w", err)
	}

	var updateResult sql.Result
	if ifUpdatedAt != nil {
		updateResult, err = tx.ExecContext(
			ctx,
			`UPDATE commitments
			 SET body_json = ?, provenance_json = ?, updated_at = ?, updated_by = ?,
			     filter_status = ?, filter_owner = ?, filter_due_at = ?
			 WHERE id = ? AND updated_at = ?`,
			string(bodyJSON),
			string(provenanceJSON),
			updatedAt,
			actorID,
			nullableString(filterColumns.Status),
			nullableString(filterColumns.Owner),
			nullableString(filterColumns.DueAt),
			id,
			*ifUpdatedAt,
		)
	} else {
		updateResult, err = tx.ExecContext(
			ctx,
			`UPDATE commitments
			 SET body_json = ?, provenance_json = ?, updated_at = ?, updated_by = ?,
			     filter_status = ?, filter_owner = ?, filter_due_at = ?
			 WHERE id = ?`,
			string(bodyJSON),
			string(provenanceJSON),
			updatedAt,
			actorID,
			nullableString(filterColumns.Status),
			nullableString(filterColumns.Owner),
			nullableString(filterColumns.DueAt),
			id,
		)
	}
	if err != nil {
		_ = tx.Rollback()
		return PatchSnapshotResult{}, fmt.Errorf("update commitment snapshot: %w", err)
	}
	if ifUpdatedAt != nil {
		rowsAffected, err := updateResult.RowsAffected()
		if err != nil {
			_ = tx.Rollback()
			return PatchSnapshotResult{}, fmt.Errorf("read patch commitment rows affected: %w", err)
		}
		if rowsAffected == 0 {
			_ = tx.Rollback()
			return PatchSnapshotResult{}, ErrConflict
		}
	}

	eventType := "snapshot_updated"
	eventSummary := "commitment updated"
	if statusChanged {
		eventType = "commitment_status_changed"
		eventSummary = "commitment status changed"
	}
	eventRefs := append([]string{"snapshot:" + id}, refs...)
	eventRefs = uniqueStrings(eventRefs)

	eventPayload := map[string]any{"changed_fields": changedFields}
	if statusChanged {
		eventPayload["from_status"] = previousStatus
		eventPayload["to_status"] = newStatus
	}
	event := map[string]any{
		"type":       eventType,
		"thread_id":  threadID,
		"refs":       eventRefs,
		"summary":    eventSummary,
		"payload":    eventPayload,
		"provenance": actorStatementProvenance(),
	}
	preparedEvent, err := prepareEventForInsert(actorID, event)
	if err != nil {
		_ = tx.Rollback()
		return PatchSnapshotResult{}, fmt.Errorf("prepare commitment patch event: %w", err)
	}
	if err := insertPreparedEvent(ctx, tx, preparedEvent); err != nil {
		_ = tx.Rollback()
		return PatchSnapshotResult{}, fmt.Errorf("emit commitment patch event: %w", err)
	}

	if statusChanged {
		if err := s.recomputeThreadOpenCommitmentsTx(ctx, tx, actorID, threadID); err != nil {
			_ = tx.Rollback()
			return PatchSnapshotResult{}, fmt.Errorf("recompute thread open_commitments after commitment patch: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return PatchSnapshotResult{}, fmt.Errorf("commit commitment patch transaction: %w", err)
	}

	out := cloneMap(currentBody)
	out["id"] = id
	if _, hasType := out["type"]; !hasType {
		out["type"] = "commitment"
	}
	out["thread_id"] = threadID
	out["updated_at"] = updatedAt
	out["updated_by"] = actorID
	out["provenance"] = provenance

	return PatchSnapshotResult{
		Snapshot: out,
		Event:    preparedEvent.Body,
	}, nil
}

func (s *Store) ListCommitments(ctx context.Context, filter CommitmentListFilter) ([]map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}

	query, args := buildListCommitmentsQuery(filter)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query commitments: %w", err)
	}
	defer rows.Close()

	commitments := make([]map[string]any, 0)
	for rows.Next() {
		row, err := scanSnapshotRow(rows)
		if err != nil {
			return nil, err
		}
		snapshot, err := row.ToSnapshotMap()
		if err != nil {
			return nil, err
		}

		commitments = append(commitments, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate commitments: %w", err)
	}

	return commitments, nil
}

func (s *Store) recomputeThreadOpenCommitments(ctx context.Context, actorID string, threadID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin thread open_commitments recompute transaction: %w", err)
	}
	if err := s.recomputeThreadOpenCommitmentsTx(ctx, tx, actorID, threadID); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("commit thread open_commitments recompute transaction: %w", err)
	}
	return nil
}

func (s *Store) recomputeThreadOpenCommitmentsTx(ctx context.Context, tx *sql.Tx, actorID string, threadID string) error {
	threadRow, err := getSnapshotRowFromQueryRower(ctx, tx, threadID, "threads")
	if err != nil {
		return err
	}
	if threadRow.Kind != "thread" {
		return ErrNotFound
	}

	threadSnapshot, err := threadRow.ToSnapshotMap()
	if err != nil {
		return fmt.Errorf("decode thread snapshot: %w", err)
	}
	threadBody := snapshotBodyFromSnapshotMap(threadSnapshot)

	existing := make([]string, 0)
	if rawExisting, ok := threadBody["open_commitments"]; ok && rawExisting != nil {
		existing, err = normalizeStringSlice(rawExisting)
		if err != nil {
			return fmt.Errorf("decode thread open_commitments: %w", err)
		}
	}

	rows, err := tx.QueryContext(
		ctx,
		`SELECT id, body_json
		 FROM commitments
		 WHERE thread_id = ?
		 ORDER BY updated_at ASC, id ASC`,
		threadID,
	)
	if err != nil {
		return fmt.Errorf("query commitments for thread open_commitments recompute: %w", err)
	}
	defer rows.Close()

	computed := make([]string, 0)
	for rows.Next() {
		var commitmentID string
		var bodyJSON string
		if err := rows.Scan(&commitmentID, &bodyJSON); err != nil {
			return fmt.Errorf("scan commitment row: %w", err)
		}

		body := map[string]any{}
		if strings.TrimSpace(bodyJSON) != "" {
			if err := json.Unmarshal([]byte(bodyJSON), &body); err != nil {
				return fmt.Errorf("decode commitment body: %w", err)
			}
		}

		status, _ := body["status"].(string)
		if status == "open" || status == "blocked" {
			computed = append(computed, commitmentID)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate commitment rows: %w", err)
	}

	sort.Strings(existing)
	sort.Strings(computed)
	if reflect.DeepEqual(existing, computed) {
		return nil
	}

	threadBody["open_commitments"] = computed
	bodyJSON, err := json.Marshal(threadBody)
	if err != nil {
		return fmt.Errorf("encode thread snapshot body: %w", err)
	}

	_, err = tx.ExecContext(
		ctx,
		`UPDATE threads SET body_json = ? WHERE id = ?`,
		string(bodyJSON),
		threadID,
	)
	if err != nil {
		return fmt.Errorf("update thread open_commitments: %w", err)
	}

	return nil
}

type snapshotRow struct {
	ID              string
	Kind            string
	ThreadID        sql.NullString
	UpdatedAt       string
	UpdatedBy       string
	BodyJSON        string
	ProvenanceJSON  string
	ArchivedAt      sql.NullString
	ArchivedBy      sql.NullString
	TombstonedAt    sql.NullString
	TombstonedBy    sql.NullString
	TombstoneReason sql.NullString
}

type snapshotFilterColumns struct {
	Status        string
	Priority      string
	Owner         string
	DueAt         string
	Cadence       string
	CadencePreset string
	TagsJSON      string
}

func (s *Store) getSnapshotRow(ctx context.Context, id string, tableName string) (snapshotRow, error) {
	if s == nil || s.db == nil {
		return snapshotRow{}, fmt.Errorf("primitives store database is not initialized")
	}

	return getSnapshotRowFromQueryRower(ctx, s.db, id, tableName)
}

func getSnapshotRowFromQueryRower(ctx context.Context, db queryRower, id string, tableName string) (snapshotRow, error) {
	row := snapshotRow{}
	err := db.QueryRowContext(
		ctx,
		fmt.Sprintf(`SELECT id, kind, thread_id, updated_at, updated_by, body_json, provenance_json, archived_at, archived_by, tombstoned_at, tombstoned_by, tombstone_reason FROM %s WHERE id = ?`, tableName),
		id,
	).Scan(&row.ID, &row.Kind, &row.ThreadID, &row.UpdatedAt, &row.UpdatedBy, &row.BodyJSON, &row.ProvenanceJSON, &row.ArchivedAt, &row.ArchivedBy, &row.TombstonedAt, &row.TombstonedBy, &row.TombstoneReason)
	if errors.Is(err, sql.ErrNoRows) {
		return snapshotRow{}, ErrNotFound
	}
	if err != nil {
		return snapshotRow{}, fmt.Errorf("query snapshot row: %w", err)
	}
	return row, nil
}

func scanSnapshotRow(scanner interface{ Scan(dest ...any) error }) (snapshotRow, error) {
	row := snapshotRow{}
	if err := scanner.Scan(&row.ID, &row.Kind, &row.ThreadID, &row.UpdatedAt, &row.UpdatedBy, &row.BodyJSON, &row.ProvenanceJSON, &row.ArchivedAt, &row.ArchivedBy, &row.TombstonedAt, &row.TombstonedBy, &row.TombstoneReason); err != nil {
		return snapshotRow{}, fmt.Errorf("scan snapshot row: %w", err)
	}
	return row, nil
}

func (r snapshotRow) ToSnapshotMap() (map[string]any, error) {
	body := map[string]any{}
	if strings.TrimSpace(r.BodyJSON) != "" {
		if err := json.Unmarshal([]byte(r.BodyJSON), &body); err != nil {
			return nil, fmt.Errorf("decode snapshot body: %w", err)
		}
	}

	provenance := map[string]any{}
	if strings.TrimSpace(r.ProvenanceJSON) != "" {
		if err := json.Unmarshal([]byte(r.ProvenanceJSON), &provenance); err != nil {
			return nil, fmt.Errorf("decode snapshot provenance: %w", err)
		}
	}

	body["id"] = r.ID
	if _, hasType := body["type"]; !hasType {
		body["type"] = r.Kind
	}
	body["updated_at"] = r.UpdatedAt
	body["updated_by"] = r.UpdatedBy
	if r.ThreadID.Valid {
		body["thread_id"] = r.ThreadID.String
	}
	body["provenance"] = provenance

	if r.ArchivedAt.Valid && strings.TrimSpace(r.ArchivedAt.String) != "" {
		body["archived_at"] = r.ArchivedAt.String
	}
	if r.ArchivedBy.Valid && strings.TrimSpace(r.ArchivedBy.String) != "" {
		body["archived_by"] = r.ArchivedBy.String
	}
	if r.TombstonedAt.Valid && strings.TrimSpace(r.TombstonedAt.String) != "" {
		body["tombstoned_at"] = r.TombstonedAt.String
	}
	if r.TombstonedBy.Valid && strings.TrimSpace(r.TombstonedBy.String) != "" {
		body["tombstoned_by"] = r.TombstonedBy.String
	}
	if r.TombstoneReason.Valid && strings.TrimSpace(r.TombstoneReason.String) != "" {
		body["tombstone_reason"] = r.TombstoneReason.String
	}

	return body, nil
}

func encodeContent(content any) ([]byte, error) {
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

func actorStatementProvenance() map[string]any {
	return map[string]any{
		"sources": []string{"actor_statement:" + actorStatementEventIDPlaceholder},
	}
}

func replaceActorStatementProvenancePlaceholder(body map[string]any, eventID string) {
	rawProvenance, ok := body["provenance"].(map[string]any)
	if !ok {
		return
	}

	rawSources, hasSources := rawProvenance["sources"]
	if !hasSources {
		return
	}

	sources, err := normalizeStringSlice(rawSources)
	if err != nil {
		return
	}

	changed := false
	placeholder := "actor_statement:" + actorStatementEventIDPlaceholder
	for idx, source := range sources {
		if source == placeholder {
			sources[idx] = "actor_statement:" + eventID
			changed = true
		}
	}
	if !changed {
		return
	}

	provenance := cloneMap(rawProvenance)
	provenance["sources"] = sources
	body["provenance"] = provenance
}

func containsThreadRef(refs []string, threadID string) bool {
	target := "thread:" + threadID
	for _, ref := range refs {
		if ref == target {
			return true
		}
	}
	return false
}

func firstThreadRefValue(refs []string) string {
	for _, ref := range refs {
		prefix, value, ok := splitTypedRef(ref)
		if !ok || prefix != "thread" {
			continue
		}
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func snapshotFilterColumnsForKind(kind string, body map[string]any) snapshotFilterColumns {
	columns := snapshotFilterColumns{TagsJSON: "[]"}
	if body == nil {
		return columns
	}

	switch strings.TrimSpace(kind) {
	case "thread":
		columns.Status = strings.TrimSpace(anyStringValue(body["status"]))
		columns.Priority = strings.TrimSpace(anyStringValue(body["priority"]))
		columns.Cadence = schedule.NormalizeCadence(anyStringValue(body["cadence"]))
		columns.CadencePreset = schedule.CadencePreset(columns.Cadence)
		if columns.CadencePreset == "" && strings.TrimSpace(columns.Cadence) == "" {
			columns.CadencePreset = schedule.CadenceReactive
		}
		if tags, err := normalizeStringSlice(body["tags"]); err == nil {
			sortStringsStable(tags)
			if tagsJSON, err := json.Marshal(tags); err == nil {
				columns.TagsJSON = string(tagsJSON)
			}
		}
	case "commitment":
		columns.Status = strings.TrimSpace(anyStringValue(body["status"]))
		columns.Owner = strings.TrimSpace(anyStringValue(body["owner"]))
		columns.DueAt = strings.TrimSpace(anyStringValue(body["due_at"]))
	}

	return columns
}

func combineThreadTagFilters(filter ThreadListFilter) []string {
	values := make([]string, 0, len(filter.Tags)+1)
	if tag := strings.TrimSpace(filter.Tag); tag != "" {
		values = append(values, tag)
	}
	values = append(values, filter.Tags...)
	return uniqueNormalizedStrings(values)
}

func uniqueNormalizedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func buildThreadCadenceFilterClause(filters []string) (string, []any) {
	normalized := uniqueNormalizedStrings(filters)
	if len(normalized) == 0 {
		return "", nil
	}

	clauses := make([]string, 0, len(normalized)*2)
	args := make([]any, 0, len(normalized)*2)
	for _, raw := range normalized {
		cadence := schedule.NormalizeCadence(raw)
		if cadence == "" {
			continue
		}
		if schedule.IsCronCadence(cadence) {
			clauses = append(clauses, "filter_cadence = ?")
			args = append(args, cadence)
			preset := schedule.CadencePreset(cadence)
			if preset != "" && preset != schedule.CadenceCustom {
				clauses = append(clauses, "filter_cadence_preset = ?")
				args = append(args, preset)
			}
			continue
		}
		if preset := schedule.CadencePreset(cadence); preset != "" {
			clauses = append(clauses, "filter_cadence_preset = ?")
			args = append(args, preset)
		}
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return "(" + strings.Join(clauses, " OR ") + ")", args
}

func buildListThreadsQuery(filter ThreadListFilter) (string, []any) {
	query := `SELECT threads.id, threads.kind, threads.thread_id, threads.updated_at, threads.updated_by, threads.body_json, threads.provenance_json, threads.archived_at, threads.archived_by, threads.tombstoned_at, threads.tombstoned_by, threads.tombstone_reason
		 FROM threads`
	args := make([]any, 0, 9)
	if filter.TombstonedOnly {
		query += ` WHERE threads.tombstoned_at IS NOT NULL`
	} else if !filter.IncludeTombstoned {
		query += ` WHERE threads.tombstoned_at IS NULL`
	}
	if filter.ArchivedOnly {
		if strings.Contains(query, "WHERE") {
			query += ` AND threads.archived_at IS NOT NULL AND threads.tombstoned_at IS NULL`
		} else {
			query += ` WHERE threads.archived_at IS NOT NULL AND threads.tombstoned_at IS NULL`
		}
	} else if !filter.IncludeArchived {
		if strings.Contains(query, "WHERE") {
			query += ` AND threads.archived_at IS NULL`
		} else {
			query += ` WHERE threads.archived_at IS NULL`
		}
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		if strings.Contains(query, "WHERE") {
			query += ` AND filter_status = ?`
		} else {
			query += ` WHERE filter_status = ?`
		}
		args = append(args, status)
	}
	if priority := strings.TrimSpace(filter.Priority); priority != "" {
		if strings.Contains(query, "WHERE") {
			query += ` AND filter_priority = ?`
		} else {
			query += ` WHERE filter_priority = ?`
		}
		args = append(args, priority)
	}
	for _, tag := range combineThreadTagFilters(filter) {
		query += ` AND EXISTS (SELECT 1 FROM json_each(filter_tags_json) WHERE value = ?)`
		args = append(args, tag)
	}
	if cadenceClause, cadenceArgs := buildThreadCadenceFilterClause(filter.Cadences); cadenceClause != "" {
		query += ` AND ` + cadenceClause
		args = append(args, cadenceArgs...)
	}
	if q := strings.TrimSpace(filter.Query); q != "" {
		searchPattern := "%" + strings.ToLower(q) + "%"
		query += ` AND (LOWER(threads.id) LIKE ? OR LOWER(json_extract(body_json, '$.title')) LIKE ?)`
		args = append(args, searchPattern, searchPattern)
	}
	if filter.Stale != nil {
		query = strings.Replace(
			query,
			"FROM threads",
			"FROM threads LEFT JOIN derived_thread_views ON derived_thread_views.thread_id = threads.id",
			1,
		)
		query += ` AND COALESCE(derived_thread_views.stale, 0) = ?`
		args = append(args, boolToInt(*filter.Stale))
	}
	query += ` ORDER BY threads.updated_at DESC, threads.id ASC`
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

func buildListCommitmentsQuery(filter CommitmentListFilter) (string, []any) {
	query := `SELECT id, kind, thread_id, updated_at, updated_by, body_json, provenance_json, archived_at, archived_by, tombstoned_at, tombstoned_by, tombstone_reason
		 FROM commitments`
	args := make([]any, 0, 6)
	hasWhere := false

	if threadID := strings.TrimSpace(filter.ThreadID); threadID != "" {
		query += ` WHERE thread_id = ?`
		hasWhere = true
		args = append(args, threadID)
	}
	if owner := strings.TrimSpace(filter.Owner); owner != "" {
		if hasWhere {
			query += ` AND filter_owner = ?`
		} else {
			query += ` WHERE filter_owner = ?`
			hasWhere = true
		}
		args = append(args, owner)
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		if hasWhere {
			query += ` AND filter_status = ?`
		} else {
			query += ` WHERE filter_status = ?`
			hasWhere = true
		}
		args = append(args, status)
	}
	if dueAfter := strings.TrimSpace(filter.DueAfter); dueAfter != "" {
		if hasWhere {
			query += ` AND filter_due_at >= ?`
		} else {
			query += ` WHERE filter_due_at >= ?`
			hasWhere = true
		}
		args = append(args, dueAfter)
	}
	if dueBefore := strings.TrimSpace(filter.DueBefore); dueBefore != "" {
		if hasWhere {
			query += ` AND filter_due_at <= ?`
		} else {
			query += ` WHERE filter_due_at <= ?`
			hasWhere = true
		}
		args = append(args, dueBefore)
	}
	query += ` ORDER BY updated_at DESC, id ASC`
	return query, args
}

func buildListArtifactsQuery(filter ArtifactListFilter) (string, []any) {
	q := strings.TrimSpace(filter.Q)
	qPattern := "%" + q + "%"

	if threadID := strings.TrimSpace(filter.ThreadID); threadID != "" {
		primaryClauses := []string{"thread_id = ?"}
		secondaryClauses := []string{"COALESCE(thread_id, '') <> ?", "EXISTS (SELECT 1 FROM json_each(refs_json) WHERE value = ?)"}
		primaryArgs := []any{threadID}
		secondaryArgs := []any{threadID, "thread:" + threadID}
		if filter.TombstonedOnly {
			primaryClauses = append(primaryClauses, "tombstoned_at IS NOT NULL")
			secondaryClauses = append(secondaryClauses, "tombstoned_at IS NOT NULL")
		} else if !filter.IncludeTombstoned {
			primaryClauses = append(primaryClauses, "tombstoned_at IS NULL")
			secondaryClauses = append(secondaryClauses, "tombstoned_at IS NULL")
		}
		if filter.ArchivedOnly {
			primaryClauses = append(primaryClauses, "archived_at IS NOT NULL", "tombstoned_at IS NULL")
			secondaryClauses = append(secondaryClauses, "archived_at IS NOT NULL", "tombstoned_at IS NULL")
		} else if !filter.IncludeArchived {
			primaryClauses = append(primaryClauses, "archived_at IS NULL")
			secondaryClauses = append(secondaryClauses, "archived_at IS NULL")
		}
		if kind := strings.TrimSpace(filter.Kind); kind != "" {
			primaryClauses = append(primaryClauses, "kind = ?")
			secondaryClauses = append(secondaryClauses, "kind = ?")
			primaryArgs = append(primaryArgs, kind)
			secondaryArgs = append(secondaryArgs, kind)
		}
		if createdAfter := strings.TrimSpace(filter.CreatedAfter); createdAfter != "" {
			primaryClauses = append(primaryClauses, "created_at >= ?")
			secondaryClauses = append(secondaryClauses, "created_at >= ?")
			primaryArgs = append(primaryArgs, createdAfter)
			secondaryArgs = append(secondaryArgs, createdAfter)
		}
		if createdBefore := strings.TrimSpace(filter.CreatedBefore); createdBefore != "" {
			primaryClauses = append(primaryClauses, "created_at <= ?")
			secondaryClauses = append(secondaryClauses, "created_at <= ?")
			primaryArgs = append(primaryArgs, createdBefore)
			secondaryArgs = append(secondaryArgs, createdBefore)
		}
		if q != "" {
			searchClause := "(id LIKE ? OR kind LIKE ? OR COALESCE(json_extract(metadata_json, '$.summary'), '') LIKE ?)"
			primaryClauses = append(primaryClauses, searchClause)
			secondaryClauses = append(secondaryClauses, searchClause)
			primaryArgs = append(primaryArgs, qPattern, qPattern, qPattern)
			secondaryArgs = append(secondaryArgs, qPattern, qPattern, qPattern)
		}
		innerQuery := `SELECT metadata_json, created_at, id FROM artifacts WHERE ` + strings.Join(primaryClauses, " AND ") + `
			UNION ALL
			SELECT metadata_json, created_at, id FROM artifacts WHERE ` + strings.Join(secondaryClauses, " AND ")
		query := `SELECT metadata_json FROM (` + innerQuery + `) ORDER BY created_at ASC, id ASC`
		if filter.Limit != nil && *filter.Limit > 0 {
			query += fmt.Sprintf(` LIMIT %d`, *filter.Limit)
		}
		args := append(primaryArgs, secondaryArgs...)
		return query, args
	}

	query := `SELECT metadata_json FROM artifacts WHERE 1=1`
	args := make([]any, 0, 8)
	if filter.TombstonedOnly {
		query += ` AND tombstoned_at IS NOT NULL`
	} else if !filter.IncludeTombstoned {
		query += ` AND tombstoned_at IS NULL`
	}
	if filter.ArchivedOnly {
		query += ` AND archived_at IS NOT NULL AND tombstoned_at IS NULL`
	} else if !filter.IncludeArchived {
		query += ` AND archived_at IS NULL`
	}
	if kind := strings.TrimSpace(filter.Kind); kind != "" {
		query += ` AND kind = ?`
		args = append(args, kind)
	}
	if createdAfter := strings.TrimSpace(filter.CreatedAfter); createdAfter != "" {
		query += ` AND created_at >= ?`
		args = append(args, createdAfter)
	}
	if createdBefore := strings.TrimSpace(filter.CreatedBefore); createdBefore != "" {
		query += ` AND created_at <= ?`
		args = append(args, createdBefore)
	}
	if q != "" {
		query += ` AND (id LIKE ? OR kind LIKE ? OR COALESCE(json_extract(metadata_json, '$.summary'), '') LIKE ?)`
		args = append(args, qPattern, qPattern, qPattern)
	}
	query += ` ORDER BY created_at ASC, id ASC`
	if filter.Limit != nil && *filter.Limit > 0 {
		query += fmt.Sprintf(` LIMIT %d`, *filter.Limit)
	}
	return query, args
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func threadIsStale(thread map[string]any, now time.Time) bool {
	cadence, _ := thread["cadence"].(string)
	if schedule.IsReactiveCadence(cadence) {
		return false
	}
	if err := schedule.ValidateCadence(cadence); err != nil {
		return false
	}

	nextCheckInAt, _ := thread["next_check_in_at"].(string)
	if strings.TrimSpace(nextCheckInAt) == "" {
		return false
	}

	nextTime, err := time.Parse(time.RFC3339, nextCheckInAt)
	if err != nil {
		return false
	}

	return now.After(nextTime)
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func scrubArtifactMetadataMap(metadata map[string]any) map[string]any {
	if metadata == nil {
		return nil
	}
	delete(metadata, "content_path")
	return metadata
}

func decodeArtifactMetadataJSON(metadataJSON string) (map[string]any, error) {
	var metadata map[string]any
	if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
		return nil, fmt.Errorf("decode artifact metadata: %w", err)
	}
	return scrubArtifactMetadataMap(metadata), nil
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func enforceRestrictedCommitmentTransition(newStatus string, refs []string) error {
	switch newStatus {
	case "done":
		hasReceiptRef := false
		hasDecisionRef := false
		for _, ref := range refs {
			prefix, ok := typedRefPrefix(ref)
			if !ok {
				continue
			}
			if prefix == "artifact" {
				hasReceiptRef = true
			}
			if prefix == "event" {
				hasDecisionRef = true
			}
		}
		if !hasReceiptRef && !hasDecisionRef {
			return fmt.Errorf("%w: status=done requires refs containing artifact:<receipt_id> or event:<decision_event_id>", ErrInvalidCommitmentTransition)
		}
	case "canceled":
		hasDecisionRef := false
		for _, ref := range refs {
			prefix, ok := typedRefPrefix(ref)
			if !ok {
				continue
			}
			if prefix == "event" {
				hasDecisionRef = true
				break
			}
		}
		if !hasDecisionRef {
			return fmt.Errorf("%w: status=canceled requires refs containing event:<decision_event_id>", ErrInvalidCommitmentTransition)
		}
	}
	return nil
}

func statusEvidenceLabels(newStatus string, refs []string) []string {
	labels := make([]string, 0, len(refs))
	for _, ref := range refs {
		prefix, value, ok := splitTypedRef(ref)
		if !ok {
			continue
		}

		switch newStatus {
		case "done":
			if prefix == "artifact" {
				labels = append(labels, "receipt:"+value)
			}
			if prefix == "event" {
				labels = append(labels, "decision:"+value)
			}
		case "canceled":
			if prefix == "event" {
				labels = append(labels, "decision:"+value)
			}
		}
	}

	labels = uniqueStrings(labels)
	sort.Strings(labels)
	return labels
}

func snapshotBodyFromSnapshotMap(snapshot map[string]any) map[string]any {
	body := cloneMap(snapshot)
	delete(body, "id")
	delete(body, "updated_at")
	delete(body, "updated_by")
	delete(body, "provenance")
	return body
}

func typedRefPrefix(ref string) (string, bool) {
	prefix, _, ok := splitTypedRef(ref)
	return prefix, ok
}

func splitTypedRef(ref string) (string, string, bool) {
	idx := strings.Index(ref, ":")
	if idx <= 0 || idx >= len(ref)-1 {
		return "", "", false
	}
	prefix := strings.TrimSpace(ref[:idx])
	value := strings.TrimSpace(ref[idx+1:])
	if prefix == "" || value == "" {
		return "", "", false
	}
	return prefix, value, true
}

func normalizeStringSlice(raw any) ([]string, error) {
	switch values := raw.(type) {
	case []string:
		out := make([]string, len(values))
		copy(out, values)
		return out, nil
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("must contain only strings")
			}
			out = append(out, text)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("must be a list of strings")
	}
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

func encodeCursor(offset int) string {
	if offset <= 0 {
		return ""
	}
	cursor := fmt.Sprintf("offset:%d", offset)
	return base64.StdEncoding.EncodeToString([]byte(cursor))
}

func decodeCursor(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return 0, fmt.Errorf("invalid cursor encoding: %w", err)
	}
	parts := strings.Split(string(decoded), ":")
	if len(parts) != 2 || parts[0] != "offset" {
		return 0, fmt.Errorf("invalid cursor format")
	}
	offset, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, fmt.Errorf("invalid cursor offset: %w", err)
	}
	if offset <= 0 {
		return 0, fmt.Errorf("invalid cursor offset: must be greater than zero")
	}
	return offset, nil
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func computeRevisionHash(contentHash, prevRevisionHash, documentID string, revisionNumber int, createdAt, createdBy string) string {
	h := sha256.New()
	fmt.Fprintf(h, "content_hash:%s\n", contentHash)
	fmt.Fprintf(h, "prev_revision_hash:%s\n", prevRevisionHash)
	fmt.Fprintf(h, "document_id:%s\n", documentID)
	fmt.Fprintf(h, "revision_number:%d\n", revisionNumber)
	fmt.Fprintf(h, "created_at:%s\n", createdAt)
	fmt.Fprintf(h, "created_by:%s\n", createdBy)
	return hex.EncodeToString(h.Sum(nil))
}
