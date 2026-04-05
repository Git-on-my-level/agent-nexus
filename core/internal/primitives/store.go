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
var ErrNotTrashed = errors.New("entity is not trashed")
var ErrNotArchived = errors.New("entity is not archived")
var ErrAlreadyTrashed = errors.New("entity is trashed")
var ErrArtifactInUse = errors.New("artifact is referenced by document revisions")
var ErrInvalidArtifactID = errors.New("invalid artifact id")
var ErrInvalidDocumentRequest = errors.New("invalid document request")
var ErrInvalidCursor = errors.New("invalid cursor")

const actorStatementEventIDPlaceholder = "<event_id>"

type ArtifactListFilter struct {
	Q               string
	Limit           *int
	Kind            string
	ThreadID        string
	CreatedBefore   string
	CreatedAfter    string
	IncludeTrashed  bool
	TrashedOnly     bool
	IncludeArchived bool
	ArchivedOnly    bool
}

type DocumentListFilter struct {
	ThreadID        string
	IncludeTrashed  bool
	TrashedOnly     bool
	IncludeArchived bool
	ArchivedOnly    bool
	Query           string
	Limit           *int
	Cursor          string
}

type ThreadListFilter struct {
	Status          string
	Priority        string
	Tag             string
	Tags            []string
	Cadences        []string
	Stale           *bool
	Now             time.Time
	Query           string
	Limit           *int
	Cursor          string
	IncludeArchived bool
	ArchivedOnly    bool
	IncludeTrashed  bool
	TrashedOnly     bool
}

type TopicListFilter struct {
	Type            string
	Status          string
	Query           string
	Limit           *int
	Cursor          string
	IncludeArchived bool
	ArchivedOnly    bool
	IncludeTrashed  bool
	TrashedOnly     bool
}

type EventListFilter struct {
	Types []string
}

type EventCursor struct {
	TS string
	ID string
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
	RefTargets  []refEdgeTarget
	PayloadJSON string
	BodyJSON    string
}

type ThreadMutationResult struct {
	Thread map[string]any
	Event  map[string]any
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

func overlayEventLifecycleFromSQLColumns(body map[string]any, archivedAt, archivedBy, trashedAt, trashedBy, trashReason sql.NullString) {
	lifecycleFieldsFromSQLColumns(archivedAt, archivedBy, trashedAt, trashedBy, trashReason).apply(body)
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
		eventID     string
		typeValue   string
		ts          string
		actorID     string
		threadID    sql.NullString
		refsJSON    string
		payloadJSON string
		bodyJSON    sql.NullString
		archivedAt  sql.NullString
		archivedBy  sql.NullString
		trashedAt   sql.NullString
		trashedBy   sql.NullString
		trashReason sql.NullString
	)
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json, body_json,
			archived_at, archived_by, trashed_at, trashed_by, trash_reason
		 FROM events WHERE id = ?`,
		id,
	).Scan(&eventID, &typeValue, &ts, &actorID, &threadID, &refsJSON, &payloadJSON, &bodyJSON,
		&archivedAt, &archivedBy, &trashedAt, &trashedBy, &trashReason)
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
	overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, trashedAt, trashedBy, trashReason)
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
	if err := replaceRefEdges(ctx, tx, "artifact", artifactID, typedRefEdgeTargets(refEdgeTypeRef, refs)); err != nil {
		_ = tx.Rollback()
		return nil, err
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
	if artifactThreadID == "" {
		artifactThreadID = strings.TrimSpace(anyStringValue(event["thread_id"]))
	}

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
	if err := replaceRefEdges(ctx, tx, "artifact", artifactID, typedRefEdgeTargets(refEdgeTypeRef, artifactRefs)); err != nil {
		_ = tx.Rollback()
		return nil, nil, err
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

func (s *Store) TrashArtifact(ctx context.Context, actorID string, artifactID string, reason string) (map[string]any, error) {
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
	var trashedAt sql.NullString
	var trashedBy sql.NullString
	var trashReason sql.NullString
	err := s.db.QueryRowContext(
		ctx,
		`SELECT metadata_json, trashed_at, trashed_by, trash_reason FROM artifacts WHERE id = ?`,
		artifactID,
	).Scan(&metadataJSON, &trashedAt, &trashedBy, &trashReason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query artifact for trash: %w", err)
	}

	metadata, err := decodeArtifactMetadataJSON(metadataJSON)
	if err != nil {
		return nil, err
	}
	if trashedAt.Valid && strings.TrimSpace(trashedAt.String) != "" {
		lifecycleFieldsFromSQLColumns(sql.NullString{}, sql.NullString{}, trashedAt, trashedBy, trashReason).apply(metadata)
		return metadata, nil
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	applyTrashedLifecycle(metadata, now, actorID, reason)

	updatedMetadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("encode trashed artifact metadata: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE artifacts SET trashed_at = ?, trashed_by = ?, trash_reason = ?, archived_at = NULL, archived_by = NULL, metadata_json = ? WHERE id = ?`,
		now, actorID, strings.TrimSpace(reason), string(updatedMetadataJSON), artifactID,
	)
	if err != nil {
		return nil, fmt.Errorf("trash artifact: %w", err)
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
	var trashedAt sql.NullString
	var archivedAt sql.NullString
	var archivedBy sql.NullString
	err := s.db.QueryRowContext(
		ctx,
		`SELECT metadata_json, trashed_at, archived_at, archived_by FROM artifacts WHERE id = ?`,
		artifactID,
	).Scan(&metadataJSON, &trashedAt, &archivedAt, &archivedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query artifact for archive: %w", err)
	}

	if trashedAt.Valid && strings.TrimSpace(trashedAt.String) != "" {
		return nil, ErrAlreadyTrashed
	}

	metadata, err := decodeArtifactMetadataJSON(metadataJSON)
	if err != nil {
		return nil, err
	}

	if archivedAt.Valid && strings.TrimSpace(archivedAt.String) != "" {
		lifecycleFieldsFromSQLColumns(archivedAt, archivedBy, sql.NullString{}, sql.NullString{}, sql.NullString{}).apply(metadata)
		return metadata, nil
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	applyArchivedLifecycle(metadata, now, actorID)

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
	var trashedDiscard sql.NullString
	var archivedAt sql.NullString
	var archivedByDiscard sql.NullString
	err := s.db.QueryRowContext(
		ctx,
		`SELECT metadata_json, trashed_at, archived_at, archived_by FROM artifacts WHERE id = ?`,
		artifactID,
	).Scan(&metadataJSON, &trashedDiscard, &archivedAt, &archivedByDiscard)
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
	clearArchivedLifecycle(metadata)

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
	var trashedAt sql.NullString
	var trashedBy sql.NullString
	var trashReason sql.NullString
	err := s.db.QueryRowContext(
		ctx,
		`SELECT metadata_json, trashed_at, trashed_by, trash_reason FROM artifacts WHERE id = ?`,
		artifactID,
	).Scan(&metadataJSON, &trashedAt, &trashedBy, &trashReason)
	_ = trashedBy
	_ = trashReason
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query artifact for restore: %w", err)
	}

	if !trashedAt.Valid || strings.TrimSpace(trashedAt.String) == "" {
		return nil, ErrNotTrashed
	}

	metadata, err := decodeArtifactMetadataJSON(metadataJSON)
	if err != nil {
		return nil, err
	}
	clearTrashedLifecycle(metadata, "", "")

	updatedMetadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("encode restored artifact metadata: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE artifacts SET trashed_at = NULL, trashed_by = NULL, trash_reason = NULL, metadata_json = ? WHERE id = ?`,
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

		rows, err := s.db.QueryContext(ctx,
			`SELECT events.id
			 FROM ref_edges
			 JOIN events ON events.id = ref_edges.source_id
			 WHERE ref_edges.source_type = 'event'
			   AND ref_edges.target_type = 'event'
			   AND ref_edges.target_id = ?
			   AND ref_edges.edge_type = ?
			   AND events.thread_id = ?
			   AND events.type = 'message_posted'`,
			current, refEdgeTypeRef, threadID,
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
		eventIDScan string
		typeValue   string
		ts          string
		actorIDScan string
		threadID    sql.NullString
		refsJSON    string
		payloadJSON string
		bodyJSON    sql.NullString
		archivedAt  sql.NullString
		archivedBy  sql.NullString
		trashedAt   sql.NullString
		trashedBy   sql.NullString
		trashReason sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json, body_json,
			archived_at, archived_by, trashed_at, trashed_by, trash_reason
		 FROM events WHERE id = ?`,
		childID,
	).Scan(&eventIDScan, &typeValue, &ts, &actorIDScan, &threadID, &refsJSON, &payloadJSON, &bodyJSON,
		&archivedAt, &archivedBy, &trashedAt, &trashedBy, &trashReason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("query event for cascade archive: %w", err)
	}
	if trashedAt.Valid && strings.TrimSpace(trashedAt.String) != "" {
		return nil
	}
	if archivedAt.Valid && strings.TrimSpace(archivedAt.String) != "" {
		return nil
	}

	body, err := decodeEventBodyFromRow(eventIDScan, typeValue, ts, actorIDScan, threadID, refsJSON, payloadJSON, bodyJSON)
	if err != nil {
		return err
	}
	overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, trashedAt, trashedBy, trashReason)

	now := time.Now().UTC().Format(time.RFC3339Nano)
	applyArchivedLifecycle(body, now, actorID)
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
		eventIDScan string
		typeValue   string
		ts          string
		actorIDScan string
		threadID    sql.NullString
		refsJSON    string
		payloadJSON string
		bodyJSON    sql.NullString
		archivedAt  sql.NullString
		archivedBy  sql.NullString
		trashedAt   sql.NullString
		trashedBy   sql.NullString
		trashReason sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json, body_json,
			archived_at, archived_by, trashed_at, trashed_by, trash_reason
		 FROM events WHERE id = ?`,
		childID,
	).Scan(&eventIDScan, &typeValue, &ts, &actorIDScan, &threadID, &refsJSON, &payloadJSON, &bodyJSON,
		&archivedAt, &archivedBy, &trashedAt, &trashedBy, &trashReason)
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
	overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, trashedAt, trashedBy, trashReason)
	clearArchivedLifecycle(body)

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

func (s *Store) trashEventCascadeChild(ctx context.Context, actorID, childID, reason string) error {
	var (
		eventIDScan string
		typeValue   string
		ts          string
		actorIDScan string
		threadID    sql.NullString
		refsJSON    string
		payloadJSON string
		bodyJSON    sql.NullString
		archivedAt  sql.NullString
		archivedBy  sql.NullString
		trashedAt   sql.NullString
		trashedBy   sql.NullString
		trashReason sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json, body_json,
			archived_at, archived_by, trashed_at, trashed_by, trash_reason
		 FROM events WHERE id = ?`,
		childID,
	).Scan(&eventIDScan, &typeValue, &ts, &actorIDScan, &threadID, &refsJSON, &payloadJSON, &bodyJSON,
		&archivedAt, &archivedBy, &trashedAt, &trashedBy, &trashReason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("query event for cascade trash: %w", err)
	}
	if trashedAt.Valid && strings.TrimSpace(trashedAt.String) != "" {
		return nil
	}

	body, err := decodeEventBodyFromRow(eventIDScan, typeValue, ts, actorIDScan, threadID, refsJSON, payloadJSON, bodyJSON)
	if err != nil {
		return err
	}
	overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, trashedAt, trashedBy, trashReason)

	now := time.Now().UTC().Format(time.RFC3339Nano)
	applyTrashedLifecycle(body, now, actorID, strings.TrimSpace(reason))

	updatedBodyJSON, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode cascade trashed event body: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE events SET trashed_at = ?, trashed_by = ?, trash_reason = ?, archived_at = NULL, archived_by = NULL, body_json = ? WHERE id = ?`,
		now, actorID, strings.TrimSpace(reason), string(updatedBodyJSON), childID,
	)
	if err != nil {
		return fmt.Errorf("cascade trash event: %w", err)
	}
	return nil
}

func (s *Store) restoreEventCascadeChild(ctx context.Context, childID string) error {
	var (
		eventIDScan string
		typeValue   string
		ts          string
		actorIDScan string
		threadID    sql.NullString
		refsJSON    string
		payloadJSON string
		bodyJSON    sql.NullString
		archivedAt  sql.NullString
		archivedBy  sql.NullString
		trashedAt   sql.NullString
		trashedBy   sql.NullString
		trashReason sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json, body_json,
			archived_at, archived_by, trashed_at, trashed_by, trash_reason
		 FROM events WHERE id = ?`,
		childID,
	).Scan(&eventIDScan, &typeValue, &ts, &actorIDScan, &threadID, &refsJSON, &payloadJSON, &bodyJSON,
		&archivedAt, &archivedBy, &trashedAt, &trashedBy, &trashReason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("query event for cascade restore: %w", err)
	}
	if !trashedAt.Valid || strings.TrimSpace(trashedAt.String) == "" {
		return nil
	}

	body, err := decodeEventBodyFromRow(eventIDScan, typeValue, ts, actorIDScan, threadID, refsJSON, payloadJSON, bodyJSON)
	if err != nil {
		return err
	}
	overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, trashedAt, trashedBy, trashReason)
	clearTrashedLifecycle(body, archivedAt.String, archivedBy.String)

	updatedBodyJSON, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode cascade restored event body: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE events SET trashed_at = NULL, trashed_by = NULL, trash_reason = NULL, body_json = ? WHERE id = ?`,
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
		eventIDScan string
		typeValue   string
		ts          string
		actorIDScan string
		threadID    sql.NullString
		refsJSON    string
		payloadJSON string
		bodyJSON    sql.NullString
		archivedAt  sql.NullString
		archivedBy  sql.NullString
		trashedAt   sql.NullString
		trashedBy   sql.NullString
		trashReason sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json, body_json,
			archived_at, archived_by, trashed_at, trashed_by, trash_reason
		 FROM events WHERE id = ?`,
		eventID,
	).Scan(&eventIDScan, &typeValue, &ts, &actorIDScan, &threadID, &refsJSON, &payloadJSON, &bodyJSON,
		&archivedAt, &archivedBy, &trashedAt, &trashedBy, &trashReason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query event for archive: %w", err)
	}

	if trashedAt.Valid && strings.TrimSpace(trashedAt.String) != "" {
		return nil, ErrAlreadyTrashed
	}

	body, err := decodeEventBodyFromRow(eventIDScan, typeValue, ts, actorIDScan, threadID, refsJSON, payloadJSON, bodyJSON)
	if err != nil {
		return nil, err
	}
	overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, trashedAt, trashedBy, trashReason)

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
		eventIDScan string
		typeValue   string
		ts          string
		actorIDScan string
		threadID    sql.NullString
		refsJSON    string
		payloadJSON string
		bodyJSON    sql.NullString
		archivedAt  sql.NullString
		archivedBy  sql.NullString
		trashedAt   sql.NullString
		trashedBy   sql.NullString
		trashReason sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json, body_json,
			archived_at, archived_by, trashed_at, trashed_by, trash_reason
		 FROM events WHERE id = ?`,
		eventID,
	).Scan(&eventIDScan, &typeValue, &ts, &actorIDScan, &threadID, &refsJSON, &payloadJSON, &bodyJSON,
		&archivedAt, &archivedBy, &trashedAt, &trashedBy, &trashReason)
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
	overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, trashedAt, trashedBy, trashReason)
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

func (s *Store) TrashEvent(ctx context.Context, actorID, eventID, reason string) (map[string]any, error) {
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
		eventIDScan string
		typeValue   string
		ts          string
		actorIDScan string
		threadID    sql.NullString
		refsJSON    string
		payloadJSON string
		bodyJSON    sql.NullString
		archivedAt  sql.NullString
		archivedBy  sql.NullString
		trashedAt   sql.NullString
		trashedBy   sql.NullString
		trashReason sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json, body_json,
			archived_at, archived_by, trashed_at, trashed_by, trash_reason
		 FROM events WHERE id = ?`,
		eventID,
	).Scan(&eventIDScan, &typeValue, &ts, &actorIDScan, &threadID, &refsJSON, &payloadJSON, &bodyJSON,
		&archivedAt, &archivedBy, &trashedAt, &trashedBy, &trashReason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query event for trash: %w", err)
	}

	body, err := decodeEventBodyFromRow(eventIDScan, typeValue, ts, actorIDScan, threadID, refsJSON, payloadJSON, bodyJSON)
	if err != nil {
		return nil, err
	}
	overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, trashedAt, trashedBy, trashReason)

	if trashedAt.Valid && strings.TrimSpace(trashedAt.String) != "" {
		return body, nil
	}

	delete(body, "archived_at")
	delete(body, "archived_by")

	now := time.Now().UTC().Format(time.RFC3339Nano)
	body["trashed_at"] = now
	body["trashed_by"] = actorID
	if strings.TrimSpace(reason) != "" {
		body["trash_reason"] = strings.TrimSpace(reason)
	}

	updatedBodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode trashed event body: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE events SET trashed_at = ?, trashed_by = ?, trash_reason = ?, archived_at = NULL, archived_by = NULL, body_json = ? WHERE id = ?`,
		now, actorID, strings.TrimSpace(reason), string(updatedBodyJSON), eventID,
	)
	if err != nil {
		return nil, fmt.Errorf("trash event: %w", err)
	}

	if typeValue == "message_posted" && threadID.Valid && strings.TrimSpace(threadID.String) != "" {
		desc, err := s.collectMessageDescendantIDs(ctx, threadID.String, eventID)
		if err != nil {
			return nil, err
		}
		for _, childID := range desc {
			if err := s.trashEventCascadeChild(ctx, actorID, childID, reason); err != nil {
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
		eventIDScan string
		typeValue   string
		ts          string
		actorIDScan string
		threadID    sql.NullString
		refsJSON    string
		payloadJSON string
		bodyJSON    sql.NullString
		archivedAt  sql.NullString
		archivedBy  sql.NullString
		trashedAt   sql.NullString
		trashedBy   sql.NullString
		trashReason sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json, body_json,
			archived_at, archived_by, trashed_at, trashed_by, trash_reason
		 FROM events WHERE id = ?`,
		eventID,
	).Scan(&eventIDScan, &typeValue, &ts, &actorIDScan, &threadID, &refsJSON, &payloadJSON, &bodyJSON,
		&archivedAt, &archivedBy, &trashedAt, &trashedBy, &trashReason)
	_ = trashedBy
	_ = trashReason
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query event for restore: %w", err)
	}

	if !trashedAt.Valid || strings.TrimSpace(trashedAt.String) == "" {
		return nil, ErrNotTrashed
	}

	body, err := decodeEventBodyFromRow(eventIDScan, typeValue, ts, actorIDScan, threadID, refsJSON, payloadJSON, bodyJSON)
	if err != nil {
		return nil, err
	}
	overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, trashedAt, trashedBy, trashReason)
	delete(body, "trashed_at")
	delete(body, "trashed_by")
	delete(body, "trash_reason")

	updatedBodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode restored event body: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE events SET trashed_at = NULL, trashed_by = NULL, trash_reason = NULL, body_json = ? WHERE id = ?`,
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

func (s *Store) PurgeTrashedArtifact(ctx context.Context, artifactID string) error {
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
		`SELECT content_hash FROM artifacts WHERE id = ? AND trashed_at IS NOT NULL`,
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
		return ErrNotTrashed
	}
	if err != nil {
		return fmt.Errorf("select trashed artifact: %w", err)
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

// applyThreadPatch updates a threads-table row with kind "thread" and emits a thread_updated event.
func (s *Store) applyThreadPatch(ctx context.Context, actorID string, id string, patch map[string]any, ifUpdatedAt *string) (ThreadMutationResult, error) {
	if s == nil || s.db == nil {
		return ThreadMutationResult{}, fmt.Errorf("primitives store database is not initialized")
	}
	if strings.TrimSpace(actorID) == "" {
		return ThreadMutationResult{}, fmt.Errorf("actorID is required")
	}
	if patch == nil {
		return ThreadMutationResult{}, fmt.Errorf("thread patch is required")
	}

	var (
		rowID          string
		rowKind        string
		threadID       sql.NullString
		provenanceJSON string
		bodyJSON       string
	)
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, kind, thread_id, provenance_json, body_json FROM threads WHERE id = ?`,
		id,
	).Scan(&rowID, &rowKind, &threadID, &provenanceJSON, &bodyJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return ThreadMutationResult{}, ErrNotFound
	}
	if err != nil {
		return ThreadMutationResult{}, fmt.Errorf("query thread before patch: %w", err)
	}
	if strings.TrimSpace(rowKind) != "thread" {
		return ThreadMutationResult{}, ErrNotFound
	}

	current := map[string]any{}
	if strings.TrimSpace(bodyJSON) != "" {
		if err := json.Unmarshal([]byte(bodyJSON), &current); err != nil {
			return ThreadMutationResult{}, fmt.Errorf("decode current thread body: %w", err)
		}
	}

	currentProvenance := map[string]any{}
	if strings.TrimSpace(provenanceJSON) != "" {
		if err := json.Unmarshal([]byte(provenanceJSON), &currentProvenance); err != nil {
			return ThreadMutationResult{}, fmt.Errorf("decode current thread provenance: %w", err)
		}
	}

	bodyPatch := cloneMap(patch)
	nextProvenance := cloneProvenance(currentProvenance)
	provenanceChanged := false
	if rawProvenance, hasProvenance := bodyPatch["provenance"]; hasProvenance {
		provenancePatch, ok := rawProvenance.(map[string]any)
		if !ok {
			return ThreadMutationResult{}, fmt.Errorf("thread.provenance must be an object")
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
		return ThreadMutationResult{}, fmt.Errorf("encode patched thread body: %w", err)
	}
	_, updatedProvenanceJSON, err := marshalProvenance(nextProvenance, "encode patched thread provenance")
	if err != nil {
		return ThreadMutationResult{}, err
	}
	filterColumns := threadFilterColumnsForKind(rowKind, current)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ThreadMutationResult{}, fmt.Errorf("begin thread patch transaction: %w", err)
	}

	updateQuery := `UPDATE threads
		 SET body_json = ?, provenance_json = ?, updated_at = ?, updated_by = ?,
		     filter_status = ?, filter_priority = ?, filter_owner = ?, filter_due_at = ?,
		     filter_cadence = ?, filter_cadence_preset = ?, filter_tags_json = ?
		 WHERE id = ?`
	updateArgs := []any{
		string(updatedBodyJSON),
		updatedProvenanceJSON,
		updatedAt,
		actorID,
		nullableString(filterColumns.Status),
		nullableString(filterColumns.Priority),
		nullableString(filterColumns.Owner),
		nullableString(filterColumns.DueAt),
		nullableString(filterColumns.Cadence),
		nullableString(filterColumns.CadencePreset),
		filterColumns.TagsJSON,
		rowID,
	}
	updateQuery, updateArgs = appendIfUpdatedAtClause(updateQuery, updateArgs, ifUpdatedAt)
	updateResult, err := tx.ExecContext(ctx, updateQuery, updateArgs...)
	if err != nil {
		_ = tx.Rollback()
		return ThreadMutationResult{}, fmt.Errorf("update thread: %w", err)
	}
	if err := requireIfUpdatedAtRowsAffected(updateResult, ifUpdatedAt, "patch thread"); err != nil {
		_ = tx.Rollback()
		return ThreadMutationResult{}, err
	}

	eventPayload := map[string]any{
		"changed_fields": changedFields,
	}
	event := map[string]any{
		"type":       "thread_updated",
		"refs":       []string{"thread:" + rowID},
		"summary":    "thread updated",
		"payload":    eventPayload,
		"provenance": actorStatementProvenance(),
	}
	if threadID.Valid {
		event["thread_id"] = threadID.String
	}

	preparedEvent, err := prepareEventForInsert(actorID, event)
	if err != nil {
		_ = tx.Rollback()
		return ThreadMutationResult{}, fmt.Errorf("prepare thread_updated event: %w", err)
	}
	if err := insertPreparedEvent(ctx, tx, preparedEvent); err != nil {
		_ = tx.Rollback()
		return ThreadMutationResult{}, fmt.Errorf("emit thread_updated event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return ThreadMutationResult{}, fmt.Errorf("commit thread patch transaction: %w", err)
	}

	current["id"] = rowID
	if _, hasType := current["type"]; !hasType {
		current["type"] = rowKind
	}
	current["updated_at"] = updatedAt
	current["updated_by"] = actorID
	if threadID.Valid {
		current["thread_id"] = threadID.String
	}
	current["provenance"] = nextProvenance

	return ThreadMutationResult{
		Thread: current,
		Event:  preparedEvent.Body,
	}, nil
}

func (s *Store) CreateThread(ctx context.Context, actorID string, thread map[string]any) (ThreadMutationResult, error) {
	if s == nil || s.db == nil {
		return ThreadMutationResult{}, fmt.Errorf("primitives store database is not initialized")
	}
	if strings.TrimSpace(actorID) == "" {
		return ThreadMutationResult{}, fmt.Errorf("actorID is required")
	}
	if thread == nil {
		return ThreadMutationResult{}, fmt.Errorf("thread is required")
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

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return ThreadMutationResult{}, fmt.Errorf("marshal thread body: %w", err)
	}

	provenance, provenanceJSON, err := marshalProvenance(thread["provenance"], "marshal thread")
	if err != nil {
		return ThreadMutationResult{}, err
	}
	filterColumns := threadFilterColumnsForKind("thread", body)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ThreadMutationResult{}, fmt.Errorf("begin thread create transaction: %w", err)
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
		provenanceJSON,
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
			return ThreadMutationResult{}, ErrConflict
		}
		return ThreadMutationResult{}, fmt.Errorf("insert thread row: %w", err)
	}

	changedFields := make([]string, 0, len(body))
	for key := range body {
		changedFields = append(changedFields, key)
	}
	changedFields = append(changedFields, "provenance")
	sort.Strings(changedFields)

	event := map[string]any{
		"type":       "thread_created",
		"thread_id":  threadID,
		"refs":       []string{"thread:" + threadID},
		"summary":    "thread created",
		"payload":    map[string]any{"changed_fields": changedFields},
		"provenance": actorStatementProvenance(),
	}
	preparedEvent, err := prepareEventForInsert(actorID, event)
	if err != nil {
		_ = tx.Rollback()
		return ThreadMutationResult{}, fmt.Errorf("prepare thread_created event: %w", err)
	}
	if err := insertPreparedEvent(ctx, tx, preparedEvent); err != nil {
		_ = tx.Rollback()
		return ThreadMutationResult{}, fmt.Errorf("emit thread_created event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return ThreadMutationResult{}, fmt.Errorf("commit thread create transaction: %w", err)
	}

	out := cloneMap(body)
	out["id"] = threadID
	// Thread domain `type` is provided by caller (thread_type enum).
	out["thread_id"] = threadID
	out["updated_at"] = updatedAt
	out["updated_by"] = actorID
	out["provenance"] = provenance

	return ThreadMutationResult{
		Thread: out,
		Event:  preparedEvent.Body,
	}, nil
}

func (s *Store) GetThread(ctx context.Context, id string) (map[string]any, error) {
	row, err := s.getThreadRow(ctx, id, "threads")
	if err != nil {
		return nil, err
	}
	if row.Kind != "thread" {
		return nil, ErrNotFound
	}
	return row.ToThreadMap()
}

func (s *Store) PatchThread(ctx context.Context, actorID string, id string, patch map[string]any, ifUpdatedAt *string) (ThreadMutationResult, error) {
	return s.applyThreadPatch(ctx, actorID, id, patch, ifUpdatedAt)
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
		row, err := scanThreadRow(rows)
		if err != nil {
			return nil, "", err
		}
		threadMap, err := row.ToThreadMap()
		if err != nil {
			return nil, "", err
		}

		threads = append(threads, threadMap)
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
	row, err := s.getThreadRow(ctx, threadID, "threads")
	if err != nil {
		return nil, err
	}
	if row.Kind != "thread" {
		return nil, ErrNotFound
	}
	if row.TrashedAt.Valid && strings.TrimSpace(row.TrashedAt.String) != "" {
		return nil, ErrAlreadyTrashed
	}
	if row.ArchivedAt.Valid && strings.TrimSpace(row.ArchivedAt.String) != "" {
		return row.ToThreadMap()
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.db.ExecContext(ctx,
		`UPDATE threads SET archived_at = ?, archived_by = ? WHERE id = ?`,
		now, actorID, threadID,
	); err != nil {
		return nil, fmt.Errorf("archive thread: %w", err)
	}
	row, err = s.getThreadRow(ctx, threadID, "threads")
	if err != nil {
		return nil, err
	}
	return row.ToThreadMap()
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
	row, err := s.getThreadRow(ctx, threadID, "threads")
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
	row, err = s.getThreadRow(ctx, threadID, "threads")
	if err != nil {
		return nil, err
	}
	return row.ToThreadMap()
}

func (s *Store) TrashThread(ctx context.Context, actorID, threadID, reason string) (map[string]any, error) {
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
	row, err := s.getThreadRow(ctx, threadID, "threads")
	if err != nil {
		return nil, err
	}
	if row.Kind != "thread" {
		return nil, ErrNotFound
	}
	if row.TrashedAt.Valid && strings.TrimSpace(row.TrashedAt.String) != "" {
		return row.ToThreadMap()
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.db.ExecContext(ctx,
		`UPDATE threads SET trashed_at = ?, trashed_by = ?, trash_reason = ?, archived_at = NULL, archived_by = NULL WHERE id = ?`,
		now, actorID, strings.TrimSpace(reason), threadID,
	); err != nil {
		return nil, fmt.Errorf("trash thread: %w", err)
	}
	row, err = s.getThreadRow(ctx, threadID, "threads")
	if err != nil {
		return nil, err
	}
	return row.ToThreadMap()
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
	row, err := s.getThreadRow(ctx, threadID, "threads")
	if err != nil {
		return nil, err
	}
	if row.Kind != "thread" {
		return nil, ErrNotFound
	}
	if !row.TrashedAt.Valid || strings.TrimSpace(row.TrashedAt.String) == "" {
		return nil, ErrNotTrashed
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE threads SET trashed_at = NULL, trashed_by = NULL, trash_reason = NULL WHERE id = ?`,
		threadID,
	); err != nil {
		return nil, fmt.Errorf("restore thread: %w", err)
	}
	row, err = s.getThreadRow(ctx, threadID, "threads")
	if err != nil {
		return nil, err
	}
	return row.ToThreadMap()
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
		`SELECT id FROM threads WHERE id = ? AND trashed_at IS NOT NULL`,
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
		return ErrNotTrashed
	}
	if err != nil {
		return fmt.Errorf("select trashed thread: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM derived_topic_views WHERE thread_id = ?`, threadID); err != nil {
		return fmt.Errorf("delete derived_topic_views: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM derived_inbox_items WHERE thread_id = ?`, threadID); err != nil {
		return fmt.Errorf("delete derived_inbox_items: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM derived_topic_dirty_queue WHERE thread_id = ?`, threadID); err != nil {
		return fmt.Errorf("delete derived_topic_dirty_queue: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM threads WHERE id = ?`, threadID); err != nil {
		return fmt.Errorf("delete thread row: %w", err)
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
			archived_at, archived_by, trashed_at, trashed_by, trash_reason
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
			eventID     string
			typeValue   string
			ts          string
			actorID     string
			thread      sql.NullString
			refsJSON    string
			payloadJSON string
			bodyJSON    sql.NullString
			archivedAt  sql.NullString
			archivedBy  sql.NullString
			trashedAt   sql.NullString
			trashedBy   sql.NullString
			trashReason sql.NullString
		)
		if err := rows.Scan(&eventID, &typeValue, &ts, &actorID, &thread, &refsJSON, &payloadJSON, &bodyJSON,
			&archivedAt, &archivedBy, &trashedAt, &trashedBy, &trashReason); err != nil {
			return nil, fmt.Errorf("scan thread event: %w", err)
		}

		if bodyJSON.Valid && strings.TrimSpace(bodyJSON.String) != "" && bodyJSON.String != "{}" {
			body := map[string]any{}
			if err := json.Unmarshal([]byte(bodyJSON.String), &body); err != nil {
				return nil, fmt.Errorf("decode event body: %w", err)
			}
			overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, trashedAt, trashedBy, trashReason)
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
		overlayEventLifecycleFromSQLColumns(event, archivedAt, archivedBy, trashedAt, trashedBy, trashReason)
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
			archived_at, archived_by, trashed_at, trashed_by, trash_reason
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
			eventID     string
			typeValue   string
			ts          string
			actorID     string
			thread      sql.NullString
			refsJSON    string
			payloadJSON string
			bodyJSON    sql.NullString
			archivedAt  sql.NullString
			archivedBy  sql.NullString
			trashedAt   sql.NullString
			trashedBy   sql.NullString
			trashReason sql.NullString
		)
		if err := rows.Scan(&eventID, &typeValue, &ts, &actorID, &thread, &refsJSON, &payloadJSON, &bodyJSON,
			&archivedAt, &archivedBy, &trashedAt, &trashedBy, &trashReason); err != nil {
			return nil, fmt.Errorf("scan recent thread event: %w", err)
		}

		if bodyJSON.Valid && strings.TrimSpace(bodyJSON.String) != "" && bodyJSON.String != "{}" {
			body := map[string]any{}
			if err := json.Unmarshal([]byte(bodyJSON.String), &body); err != nil {
				return nil, fmt.Errorf("decode recent thread event body: %w", err)
			}
			overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, trashedAt, trashedBy, trashReason)
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
		overlayEventLifecycleFromSQLColumns(event, archivedAt, archivedBy, trashedAt, trashedBy, trashReason)
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
		RefTargets:  typedRefEdgeTargets(refEdgeTypeRef, refs),
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
	if err := replaceRefEdges(ctx, exec, "event", anyStringValue(prepared.Body["id"]), prepared.RefTargets); err != nil {
		return err
	}

	return nil
}

func (s *Store) ListEvents(ctx context.Context, filter EventListFilter) ([]map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}

	query := `SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json, body_json,
		archived_at, archived_by, trashed_at, trashed_by, trash_reason
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
			eventID     string
			typeValue   string
			ts          string
			actorID     string
			thread      sql.NullString
			refsJSON    string
			payloadJSON string
			bodyJSON    sql.NullString
			archivedAt  sql.NullString
			archivedBy  sql.NullString
			trashedAt   sql.NullString
			trashedBy   sql.NullString
			trashReason sql.NullString
		)
		if err := rows.Scan(&eventID, &typeValue, &ts, &actorID, &thread, &refsJSON, &payloadJSON, &bodyJSON,
			&archivedAt, &archivedBy, &trashedAt, &trashedBy, &trashReason); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}

		if bodyJSON.Valid && strings.TrimSpace(bodyJSON.String) != "" && bodyJSON.String != "{}" {
			body := map[string]any{}
			if err := json.Unmarshal([]byte(bodyJSON.String), &body); err != nil {
				return nil, fmt.Errorf("decode event body: %w", err)
			}
			overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, trashedAt, trashedBy, trashReason)
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
		overlayEventLifecycleFromSQLColumns(event, archivedAt, archivedBy, trashedAt, trashedBy, trashReason)
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
		archived_at, archived_by, trashed_at, trashed_by, trash_reason
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
			eventID     string
			typeValue   string
			ts          string
			actorID     string
			thread      sql.NullString
			refsJSON    string
			payloadJSON string
			bodyJSON    sql.NullString
			archivedAt  sql.NullString
			archivedBy  sql.NullString
			trashedAt   sql.NullString
			trashedBy   sql.NullString
			trashReason sql.NullString
		)
		if err := rows.Scan(&eventID, &typeValue, &ts, &actorID, &thread, &refsJSON, &payloadJSON, &bodyJSON,
			&archivedAt, &archivedBy, &trashedAt, &trashedBy, &trashReason); err != nil {
			return nil, fmt.Errorf("scan event after cursor: %w", err)
		}

		if bodyJSON.Valid && strings.TrimSpace(bodyJSON.String) != "" && bodyJSON.String != "{}" {
			body := map[string]any{}
			if err := json.Unmarshal([]byte(bodyJSON.String), &body); err != nil {
				return nil, fmt.Errorf("decode event body after cursor: %w", err)
			}
			overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, trashedAt, trashedBy, trashReason)
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
		overlayEventLifecycleFromSQLColumns(event, archivedAt, archivedBy, trashedAt, trashedBy, trashReason)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events after cursor: %w", err)
	}
	return events, nil
}

type threadRow struct {
	ID             string
	Kind           string
	ThreadID       sql.NullString
	UpdatedAt      string
	UpdatedBy      string
	BodyJSON       string
	ProvenanceJSON string
	ArchivedAt     sql.NullString
	ArchivedBy     sql.NullString
	TrashedAt      sql.NullString
	TrashedBy      sql.NullString
	TrashReason    sql.NullString
}

type threadFilterColumns struct {
	Status        string
	Priority      string
	Owner         string
	DueAt         string
	Cadence       string
	CadencePreset string
	TagsJSON      string
}

func (s *Store) getThreadRow(ctx context.Context, id string, tableName string) (threadRow, error) {
	if s == nil || s.db == nil {
		return threadRow{}, fmt.Errorf("primitives store database is not initialized")
	}

	return getThreadRowFromQueryRower(ctx, s.db, id, tableName)
}

func getThreadRowFromQueryRower(ctx context.Context, db queryRower, id string, tableName string) (threadRow, error) {
	row := threadRow{}
	err := db.QueryRowContext(
		ctx,
		fmt.Sprintf(`SELECT id, kind, thread_id, updated_at, updated_by, body_json, provenance_json, archived_at, archived_by, trashed_at, trashed_by, trash_reason FROM %s WHERE id = ?`, tableName),
		id,
	).Scan(&row.ID, &row.Kind, &row.ThreadID, &row.UpdatedAt, &row.UpdatedBy, &row.BodyJSON, &row.ProvenanceJSON, &row.ArchivedAt, &row.ArchivedBy, &row.TrashedAt, &row.TrashedBy, &row.TrashReason)
	if errors.Is(err, sql.ErrNoRows) {
		return threadRow{}, ErrNotFound
	}
	if err != nil {
		return threadRow{}, fmt.Errorf("query threads row: %w", err)
	}
	return row, nil
}

func scanThreadRow(scanner interface{ Scan(dest ...any) error }) (threadRow, error) {
	row := threadRow{}
	if err := scanner.Scan(&row.ID, &row.Kind, &row.ThreadID, &row.UpdatedAt, &row.UpdatedBy, &row.BodyJSON, &row.ProvenanceJSON, &row.ArchivedAt, &row.ArchivedBy, &row.TrashedAt, &row.TrashedBy, &row.TrashReason); err != nil {
		return threadRow{}, fmt.Errorf("scan threads row: %w", err)
	}
	return row, nil
}

func (r threadRow) ToThreadMap() (map[string]any, error) {
	body := map[string]any{}
	if strings.TrimSpace(r.BodyJSON) != "" {
		if err := json.Unmarshal([]byte(r.BodyJSON), &body); err != nil {
			return nil, fmt.Errorf("decode thread body: %w", err)
		}
	}

	provenance := map[string]any{}
	if strings.TrimSpace(r.ProvenanceJSON) != "" {
		if err := json.Unmarshal([]byte(r.ProvenanceJSON), &provenance); err != nil {
			return nil, fmt.Errorf("decode thread provenance: %w", err)
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
	if r.TrashedAt.Valid && strings.TrimSpace(r.TrashedAt.String) != "" {
		body["trashed_at"] = r.TrashedAt.String
	}
	if r.TrashedBy.Valid && strings.TrimSpace(r.TrashedBy.String) != "" {
		body["trashed_by"] = r.TrashedBy.String
	}
	if r.TrashReason.Valid && strings.TrimSpace(r.TrashReason.String) != "" {
		body["trash_reason"] = r.TrashReason.String
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

func threadFilterColumnsForKind(kind string, body map[string]any) threadFilterColumns {
	columns := threadFilterColumns{TagsJSON: "[]"}
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
	query := `SELECT threads.id, threads.kind, threads.thread_id, threads.updated_at, threads.updated_by, threads.body_json, threads.provenance_json, threads.archived_at, threads.archived_by, threads.trashed_at, threads.trashed_by, threads.trash_reason
		 FROM threads`
	args := make([]any, 0, 9)
	if filter.TrashedOnly {
		query += ` WHERE threads.trashed_at IS NOT NULL`
	} else if !filter.IncludeTrashed {
		query += ` WHERE threads.trashed_at IS NULL`
	}
	if filter.ArchivedOnly {
		if strings.Contains(query, "WHERE") {
			query += ` AND threads.archived_at IS NOT NULL AND threads.trashed_at IS NULL`
		} else {
			query += ` WHERE threads.archived_at IS NOT NULL AND threads.trashed_at IS NULL`
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
			"FROM threads LEFT JOIN derived_topic_views ON derived_topic_views.thread_id = threads.id",
			1,
		)
		query += ` AND COALESCE(derived_topic_views.stale, 0) = ?`
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

func buildListArtifactsQuery(filter ArtifactListFilter) (string, []any) {
	q := strings.TrimSpace(filter.Q)
	qPattern := "%" + q + "%"

	if threadID := strings.TrimSpace(filter.ThreadID); threadID != "" {
		primaryClauses := []string{"thread_id = ?"}
		secondaryClauses := []string{
			"COALESCE(artifacts.thread_id, '') <> ?",
			"ref_edges.source_type = ?",
			"ref_edges.target_type = ?",
			"ref_edges.target_id = ?",
			"ref_edges.edge_type = ?",
		}
		primaryArgs := []any{threadID}
		secondaryArgs := []any{threadID, "artifact", "thread", threadID, refEdgeTypeRef}
		if filter.TrashedOnly {
			primaryClauses = append(primaryClauses, "trashed_at IS NOT NULL")
			secondaryClauses = append(secondaryClauses, "artifacts.trashed_at IS NOT NULL")
		} else if !filter.IncludeTrashed {
			primaryClauses = append(primaryClauses, "trashed_at IS NULL")
			secondaryClauses = append(secondaryClauses, "artifacts.trashed_at IS NULL")
		}
		if filter.ArchivedOnly {
			primaryClauses = append(primaryClauses, "archived_at IS NOT NULL", "trashed_at IS NULL")
			secondaryClauses = append(secondaryClauses, "artifacts.archived_at IS NOT NULL", "artifacts.trashed_at IS NULL")
		} else if !filter.IncludeArchived {
			primaryClauses = append(primaryClauses, "archived_at IS NULL")
			secondaryClauses = append(secondaryClauses, "artifacts.archived_at IS NULL")
		}
		if kind := strings.TrimSpace(filter.Kind); kind != "" {
			primaryClauses = append(primaryClauses, "kind = ?")
			secondaryClauses = append(secondaryClauses, "artifacts.kind = ?")
			primaryArgs = append(primaryArgs, kind)
			secondaryArgs = append(secondaryArgs, kind)
		}
		if createdAfter := strings.TrimSpace(filter.CreatedAfter); createdAfter != "" {
			primaryClauses = append(primaryClauses, "created_at >= ?")
			secondaryClauses = append(secondaryClauses, "artifacts.created_at >= ?")
			primaryArgs = append(primaryArgs, createdAfter)
			secondaryArgs = append(secondaryArgs, createdAfter)
		}
		if createdBefore := strings.TrimSpace(filter.CreatedBefore); createdBefore != "" {
			primaryClauses = append(primaryClauses, "created_at <= ?")
			secondaryClauses = append(secondaryClauses, "artifacts.created_at <= ?")
			primaryArgs = append(primaryArgs, createdBefore)
			secondaryArgs = append(secondaryArgs, createdBefore)
		}
		if q != "" {
			searchClause := "(id LIKE ? OR kind LIKE ? OR COALESCE(json_extract(metadata_json, '$.summary'), '') LIKE ?)"
			primaryClauses = append(primaryClauses, searchClause)
			secondaryClauses = append(secondaryClauses, "(artifacts.id LIKE ? OR artifacts.kind LIKE ? OR COALESCE(json_extract(artifacts.metadata_json, '$.summary'), '') LIKE ?)")
			primaryArgs = append(primaryArgs, qPattern, qPattern, qPattern)
			secondaryArgs = append(secondaryArgs, qPattern, qPattern, qPattern)
		}
		innerQuery := `SELECT metadata_json, created_at, id FROM artifacts WHERE ` + strings.Join(primaryClauses, " AND ") + `
			UNION ALL
			SELECT artifacts.metadata_json, artifacts.created_at, artifacts.id
			  FROM ref_edges
			  JOIN artifacts ON artifacts.id = ref_edges.source_id
			 WHERE ` + strings.Join(secondaryClauses, " AND ")
		query := `SELECT metadata_json FROM (` + innerQuery + `) ORDER BY created_at ASC, id ASC`
		if filter.Limit != nil && *filter.Limit > 0 {
			query += fmt.Sprintf(` LIMIT %d`, *filter.Limit)
		}
		args := append(primaryArgs, secondaryArgs...)
		return query, args
	}

	query := `SELECT metadata_json FROM artifacts WHERE 1=1`
	args := make([]any, 0, 8)
	if filter.TrashedOnly {
		query += ` AND trashed_at IS NOT NULL`
	} else if !filter.IncludeTrashed {
		query += ` AND trashed_at IS NULL`
	}
	if filter.ArchivedOnly {
		query += ` AND archived_at IS NOT NULL AND trashed_at IS NULL`
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
