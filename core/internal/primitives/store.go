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
	"log"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"agent-nexus-core/internal/blob"
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

const provenanceEventIDPlaceholder = "<event_id>"
const (
	bridgeCheckedInEventType       = "agent_bridge_checked_in"
	bridgeCheckinEventsRetainLimit = 20
)

type ArtifactListFilter struct {
	States []string

	Q             string
	Limit         *int
	Kind          string
	ThreadID      string
	CreatedBefore string
	CreatedAfter  string
}

type DocumentListFilter struct {
	States []string

	ThreadID string
	Query    string
	Limit    *int
	Cursor   string
}

type ThreadListFilter struct {
	States []string

	Query  string
	Limit  *int
	Cursor string
}

type TopicListFilter struct {
	States []string

	Query  string
	Limit  *int
	Cursor string
}

type EventListFilter struct {
	Types []string
}

// HumanAttentionRespondedPageParams configures newest-first pagination over human_attention_responded events.
type HumanAttentionRespondedPageParams struct {
	CursorTS string
	CursorID string
	Limit    int

	// KindFilter limits rows by payload.kind: "", "ask", "review", "escalate", or "unknown".
	KindFilter string

	// SinceRFC3339Nano, when non-empty, restricts to events with ts >= this value (30-day window, etc.).
	SinceRFC3339Nano string
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
	if prepared.Type == bridgeCheckedInEventType {
		if err := s.pruneOldBridgeCheckinEvents(ctx, prepared); err != nil {
			return nil, err
		}
	}

	return prepared.Body, nil
}

func (s *Store) pruneOldBridgeCheckinEvents(ctx context.Context, latest preparedEvent) error {
	payload, ok := latest.Body["payload"].(map[string]any)
	if !ok {
		return nil
	}
	actorID := strings.TrimSpace(anyStringValue(payload["actor_id"]))
	handle := strings.TrimSpace(anyStringValue(payload["handle"]))
	latestID := strings.TrimSpace(anyStringValue(latest.Body["id"]))
	if actorID == "" || handle == "" || latestID == "" {
		return nil
	}

	// Bridge readiness is authoritative on the agent registration. Keep only a
	// compact raw check-in history for diagnostics so polling bridges cannot grow
	// the event log forever.
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id
		 FROM events
		 WHERE type = ?
		   AND id <> ?
		   AND json_extract(payload_json, '$.payload.actor_id') = ?
		   AND json_extract(payload_json, '$.payload.handle') = ?
		 ORDER BY ts DESC, id DESC
		 LIMIT -1 OFFSET ?`,
		bridgeCheckedInEventType,
		latestID,
		actorID,
		handle,
		bridgeCheckinEventsRetainLimit-1,
	)
	if err != nil {
		return fmt.Errorf("query old bridge check-in events: %w", err)
	}
	defer rows.Close()

	oldIDs := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan old bridge check-in event: %w", err)
		}
		id = strings.TrimSpace(id)
		if id == "" || id == latestID {
			continue
		}
		oldIDs = append(oldIDs, id)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate old bridge check-in events: %w", err)
	}
	if len(oldIDs) == 0 {
		return nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(oldIDs)), ",")
	args := make([]any, 0, len(oldIDs)+1)
	args = append(args, "event")
	for _, id := range oldIDs {
		args = append(args, id)
	}
	if _, err := s.db.ExecContext(
		ctx,
		`DELETE FROM ref_edges WHERE source_type = ? AND source_id IN (`+placeholders+`)`,
		args...,
	); err != nil {
		return fmt.Errorf("delete old bridge check-in ref edges: %w", err)
	}
	args = args[:0]
	for _, id := range oldIDs {
		args = append(args, id)
	}
	if _, err := s.db.ExecContext(
		ctx,
		`DELETE FROM events WHERE id IN (`+placeholders+`)`,
		args...,
	); err != nil {
		return fmt.Errorf("delete old bridge check-in events: %w", err)
	}
	return nil
}

func overlayEventLifecycleFromSQLColumns(body map[string]any, archivedAt, archivedBy, trashedAt, trashedBy, trashReason sql.NullString) {
	lifecycleFieldsFromSQLColumns(archivedAt, archivedBy, trashedAt, trashedBy, trashReason).apply(body)
}

func decodeEventBodyFromRow(
	eventID, typeValue, ts, actorID string,
	threadID sql.NullString,
	refsJSON, payloadJSON string,
) (map[string]any, error) {
	var refsList []string
	if err := json.Unmarshal([]byte(refsJSON), &refsList); err != nil {
		return nil, fmt.Errorf("decode event refs: %w", err)
	}
	// Use []any for refs so API consumers that walk raw maps (e.g. thread ref discovery) match
	// json.Unmarshal into map[string]any, which decodes JSON arrays as []any.
	refs := make([]any, len(refsList))
	for i, r := range refsList {
		refs[i] = r
	}

	var wrapper map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &wrapper); err != nil {
		return nil, fmt.Errorf("decode event payload: %w", err)
	}
	if wrapper == nil {
		wrapper = map[string]any{}
	}

	out := map[string]any{
		"id":       eventID,
		"type":     typeValue,
		"ts":       ts,
		"actor_id": actorID,
		"refs":     refs,
	}
	if threadID.Valid {
		out["thread_id"] = threadID.String
	}

	if inner, ok := wrapper["payload"].(map[string]any); ok {
		out["payload"] = inner
		for k, v := range wrapper {
			if k == "payload" {
				continue
			}
			out[k] = v
		}
		return out, nil
	}

	// Legacy: payload_json was only the inner object (per-type fields), or a flat mix; strip known
	// top-level fields into the map root and keep the rest as payload.
	flat := make(map[string]any, len(wrapper))
	for k, v := range wrapper {
		flat[k] = v
	}
	for _, k := range []string{"summary", "provenance"} {
		if v, ok := flat[k]; ok {
			out[k] = v
			delete(flat, k)
		}
	}
	if len(flat) == 0 {
		out["payload"] = map[string]any{}
	} else {
		out["payload"] = flat
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
		archivedAt  sql.NullString
		archivedBy  sql.NullString
		trashedAt   sql.NullString
		trashedBy   sql.NullString
		trashReason sql.NullString
	)
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json,
			archived_at, archived_by, trashed_at, trashed_by, trash_reason
		 FROM events WHERE id = ?`,
		id,
	).Scan(&eventID, &typeValue, &ts, &actorID, &threadID, &refsJSON, &payloadJSON,
		&archivedAt, &archivedBy, &trashedAt, &trashedBy, &trashReason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query event: %w", err)
	}

	body, err := decodeEventBodyFromRow(eventID, typeValue, ts, actorID, threadID, refsJSON, payloadJSON)
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
	filter.States = NormalizeListLifecycleStates(filter.States)

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
	var trashedAt, archivedAt sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT trashed_at, archived_at FROM events WHERE id = ?`,
		childID,
	).Scan(&trashedAt, &archivedAt)
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

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx,
		`UPDATE events SET archived_at = ?, archived_by = ? WHERE id = ?`,
		now, actorID, childID,
	)
	if err != nil {
		return fmt.Errorf("cascade archive event: %w", err)
	}
	return nil
}

func (s *Store) unarchiveEventCascadeChild(ctx context.Context, childID string) error {
	var archivedAt sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT archived_at FROM events WHERE id = ?`,
		childID,
	).Scan(&archivedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("query event for cascade unarchive: %w", err)
	}
	if !archivedAt.Valid || strings.TrimSpace(archivedAt.String) == "" {
		return nil
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE events SET archived_at = NULL, archived_by = NULL WHERE id = ?`,
		childID,
	)
	if err != nil {
		return fmt.Errorf("cascade unarchive event: %w", err)
	}
	return nil
}

func (s *Store) trashEventCascadeChild(ctx context.Context, actorID, childID, reason string) error {
	var trashedAt sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT trashed_at FROM events WHERE id = ?`,
		childID,
	).Scan(&trashedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("query event for cascade trash: %w", err)
	}
	if trashedAt.Valid && strings.TrimSpace(trashedAt.String) != "" {
		return nil
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx,
		`UPDATE events SET trashed_at = ?, trashed_by = ?, trash_reason = ?, archived_at = NULL, archived_by = NULL WHERE id = ?`,
		now, actorID, strings.TrimSpace(reason), childID,
	)
	if err != nil {
		return fmt.Errorf("cascade trash event: %w", err)
	}
	return nil
}

func (s *Store) restoreEventCascadeChild(ctx context.Context, childID string) error {
	var trashedAt sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT trashed_at FROM events WHERE id = ?`,
		childID,
	).Scan(&trashedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("query event for cascade restore: %w", err)
	}
	if !trashedAt.Valid || strings.TrimSpace(trashedAt.String) == "" {
		return nil
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE events SET trashed_at = NULL, trashed_by = NULL, trash_reason = NULL WHERE id = ?`,
		childID,
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
		archivedAt  sql.NullString
		archivedBy  sql.NullString
		trashedAt   sql.NullString
		trashedBy   sql.NullString
		trashReason sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json,
			archived_at, archived_by, trashed_at, trashed_by, trash_reason
		 FROM events WHERE id = ?`,
		eventID,
	).Scan(&eventIDScan, &typeValue, &ts, &actorIDScan, &threadID, &refsJSON, &payloadJSON,
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

	body, err := decodeEventBodyFromRow(eventIDScan, typeValue, ts, actorIDScan, threadID, refsJSON, payloadJSON)
	if err != nil {
		return nil, err
	}
	overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, trashedAt, trashedBy, trashReason)

	if archivedAt.Valid && strings.TrimSpace(archivedAt.String) != "" {
		return body, nil
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx,
		`UPDATE events SET archived_at = ?, archived_by = ? WHERE id = ?`,
		now, actorID, eventID,
	)
	if err != nil {
		return nil, fmt.Errorf("archive event: %w", err)
	}
	body["archived_at"] = now
	body["archived_by"] = actorID

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
		archivedAt  sql.NullString
		archivedBy  sql.NullString
		trashedAt   sql.NullString
		trashedBy   sql.NullString
		trashReason sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json,
			archived_at, archived_by, trashed_at, trashed_by, trash_reason
		 FROM events WHERE id = ?`,
		eventID,
	).Scan(&eventIDScan, &typeValue, &ts, &actorIDScan, &threadID, &refsJSON, &payloadJSON,
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

	body, err := decodeEventBodyFromRow(eventIDScan, typeValue, ts, actorIDScan, threadID, refsJSON, payloadJSON)
	if err != nil {
		return nil, err
	}
	overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, trashedAt, trashedBy, trashReason)
	delete(body, "archived_at")
	delete(body, "archived_by")

	_, err = s.db.ExecContext(ctx,
		`UPDATE events SET archived_at = NULL, archived_by = NULL WHERE id = ?`,
		eventID,
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
		archivedAt  sql.NullString
		archivedBy  sql.NullString
		trashedAt   sql.NullString
		trashedBy   sql.NullString
		trashReason sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json,
			archived_at, archived_by, trashed_at, trashed_by, trash_reason
		 FROM events WHERE id = ?`,
		eventID,
	).Scan(&eventIDScan, &typeValue, &ts, &actorIDScan, &threadID, &refsJSON, &payloadJSON,
		&archivedAt, &archivedBy, &trashedAt, &trashedBy, &trashReason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query event for trash: %w", err)
	}

	body, err := decodeEventBodyFromRow(eventIDScan, typeValue, ts, actorIDScan, threadID, refsJSON, payloadJSON)
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
	_, err = s.db.ExecContext(ctx,
		`UPDATE events SET trashed_at = ?, trashed_by = ?, trash_reason = ?, archived_at = NULL, archived_by = NULL WHERE id = ?`,
		now, actorID, strings.TrimSpace(reason), eventID,
	)
	if err != nil {
		return nil, fmt.Errorf("trash event: %w", err)
	}
	body["trashed_at"] = now
	body["trashed_by"] = actorID
	if strings.TrimSpace(reason) != "" {
		body["trash_reason"] = strings.TrimSpace(reason)
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
		archivedAt  sql.NullString
		archivedBy  sql.NullString
		trashedAt   sql.NullString
		trashedBy   sql.NullString
		trashReason sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json,
			archived_at, archived_by, trashed_at, trashed_by, trash_reason
		 FROM events WHERE id = ?`,
		eventID,
	).Scan(&eventIDScan, &typeValue, &ts, &actorIDScan, &threadID, &refsJSON, &payloadJSON,
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

	body, err := decodeEventBodyFromRow(eventIDScan, typeValue, ts, actorIDScan, threadID, refsJSON, payloadJSON)
	if err != nil {
		return nil, err
	}
	overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, trashedAt, trashedBy, trashReason)
	delete(body, "trashed_at")
	delete(body, "trashed_by")
	delete(body, "trash_reason")

	_, err = s.db.ExecContext(ctx,
		`UPDATE events SET trashed_at = NULL, trashed_by = NULL, trash_reason = NULL WHERE id = ?`,
		eventID,
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

// applyThreadPatch updates a threads-table row with kind "thread".
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

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ThreadMutationResult{}, fmt.Errorf("begin thread patch transaction: %w", err)
	}

	updateQuery := `UPDATE threads
		 SET body_json = ?, provenance_json = ?, updated_at = ?, updated_by = ?
		 WHERE id = ?`
	updateArgs := []any{
		string(updatedBodyJSON),
		updatedProvenanceJSON,
		updatedAt,
		actorID,
		rowID,
	}
	updateQuery, updateArgs = appendIfUpdatedAtClause(updateQuery, updateArgs, ifUpdatedAt)
	updateResult, err := tx.ExecContext(ctx, updateQuery, updateArgs...)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return ThreadMutationResult{}, fmt.Errorf("update thread: %w", err)
	}
	if err := requireIfUpdatedAtRowsAffected(updateResult, ifUpdatedAt, "patch thread"); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return ThreadMutationResult{}, err
	}

	if err := tx.Commit(); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
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

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ThreadMutationResult{}, fmt.Errorf("begin thread create transaction: %w", err)
	}

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO threads(id, kind, thread_id, updated_at, updated_by, body_json, provenance_json)
		 VALUES (?, 'thread', ?, ?, ?, ?, ?)`,
		threadID,
		threadID,
		updatedAt,
		actorID,
		string(bodyJSON),
		provenanceJSON,
	)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		if isUniqueViolation(err) {
			return ThreadMutationResult{}, ErrConflict
		}
		return ThreadMutationResult{}, fmt.Errorf("insert thread row: %w", err)
	}

	if err := tx.Commit(); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
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
	filter.States = NormalizeListLifecycleStates(filter.States)
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
		`SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json,
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
			archivedAt  sql.NullString
			archivedBy  sql.NullString
			trashedAt   sql.NullString
			trashedBy   sql.NullString
			trashReason sql.NullString
		)
		if err := rows.Scan(&eventID, &typeValue, &ts, &actorID, &thread, &refsJSON, &payloadJSON,
			&archivedAt, &archivedBy, &trashedAt, &trashedBy, &trashReason); err != nil {
			return nil, fmt.Errorf("scan thread event: %w", err)
		}

		body, err := decodeEventBodyFromRow(eventID, typeValue, ts, actorID, thread, refsJSON, payloadJSON)
		if err != nil {
			return nil, err
		}
		overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, trashedAt, trashedBy, trashReason)
		events = append(events, body)
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
		`SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json,
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
			archivedAt  sql.NullString
			archivedBy  sql.NullString
			trashedAt   sql.NullString
			trashedBy   sql.NullString
			trashReason sql.NullString
		)
		if err := rows.Scan(&eventID, &typeValue, &ts, &actorID, &thread, &refsJSON, &payloadJSON,
			&archivedAt, &archivedBy, &trashedAt, &trashedBy, &trashReason); err != nil {
			return nil, fmt.Errorf("scan recent thread event: %w", err)
		}

		body, err := decodeEventBodyFromRow(eventID, typeValue, ts, actorID, thread, refsJSON, payloadJSON)
		if err != nil {
			return nil, err
		}
		overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, trashedAt, trashedBy, trashReason)
		recentDescending = append(recentDescending, body)
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
	replaceEventProvenancePlaceholder(body, eventID)

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

	if rawPayload, ok := body["payload"]; ok && rawPayload != nil {
		if _, ok := rawPayload.(map[string]any); !ok {
			return preparedEvent{}, fmt.Errorf("event.payload must be an object when provided")
		}
	}

	wrapper := EventPayloadWrapperFromBodyMap(body)
	payloadJSON, err := json.Marshal(wrapper)
	if err != nil {
		return preparedEvent{}, fmt.Errorf("marshal event payload: %w", err)
	}

	return preparedEvent{
		Body:        body,
		Type:        typeValue,
		ThreadID:    threadID,
		RefsJSON:    string(refsJSON),
		RefTargets:  typedRefEdgeTargets(refEdgeTypeRef, refs),
		PayloadJSON: string(payloadJSON),
	}, nil
}

func insertPreparedEvent(ctx context.Context, exec eventExec, prepared preparedEvent) error {
	if _, err := exec.ExecContext(
		ctx,
		`INSERT INTO events(id, type, ts, actor_id, thread_id, refs_json, payload_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		prepared.Body["id"],
		prepared.Type,
		prepared.Body["ts"],
		prepared.Body["actor_id"],
		prepared.ThreadID,
		prepared.RefsJSON,
		prepared.PayloadJSON,
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

	query := `SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json,
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
			archivedAt  sql.NullString
			archivedBy  sql.NullString
			trashedAt   sql.NullString
			trashedBy   sql.NullString
			trashReason sql.NullString
		)
		if err := rows.Scan(&eventID, &typeValue, &ts, &actorID, &thread, &refsJSON, &payloadJSON,
			&archivedAt, &archivedBy, &trashedAt, &trashedBy, &trashReason); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}

		body, err := decodeEventBodyFromRow(eventID, typeValue, ts, actorID, thread, refsJSON, payloadJSON)
		if err != nil {
			return nil, err
		}
		overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, trashedAt, trashedBy, trashReason)
		events = append(events, body)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}

	return events, nil
}

const humanAttentionRespondedEventType = "human_attention_responded"

// ListHumanAttentionRespondedPage returns up to Limit human_attention_responded events ordered newest first.
// CursorTS/CursorID describe the last row from the previous page (exclusive): rows strictly older than that tuple are returned.
func (s *Store) ListHumanAttentionRespondedPage(ctx context.Context, params HumanAttentionRespondedPageParams) ([]map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	query := `SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json,
		archived_at, archived_by, trashed_at, trashed_by, trash_reason
		FROM events
		WHERE type = ? AND COALESCE(trashed_at, '') = ''`
	args := []any{humanAttentionRespondedEventType}

	if ts := strings.TrimSpace(params.SinceRFC3339Nano); ts != "" {
		query += ` AND ts >= ?`
		args = append(args, ts)
	}

	kind := strings.TrimSpace(strings.ToLower(params.KindFilter))
	switch kind {
	case "", "all":
	case "unknown":
		query += ` AND (json_extract(payload_json, '$.payload.kind') IS NULL OR json_extract(payload_json, '$.payload.kind') NOT IN ('ask','review','escalate'))`
	default:
		query += ` AND lower(trim(json_extract(payload_json, '$.payload.kind'))) = ?`
		args = append(args, kind)
	}

	cursorTS := strings.TrimSpace(params.CursorTS)
	cursorID := strings.TrimSpace(params.CursorID)
	if cursorTS != "" && cursorID != "" {
		query += ` AND ((ts < ?) OR (ts = ? AND id < ?))`
		args = append(args, cursorTS, cursorTS, cursorID)
	}

	query += ` ORDER BY ts DESC, id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query human_attention_responded page: %w", err)
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
			archivedAt  sql.NullString
			archivedBy  sql.NullString
			trashedAt   sql.NullString
			trashedBy   sql.NullString
			trashReason sql.NullString
		)
		if err := rows.Scan(&eventID, &typeValue, &ts, &actorID, &thread, &refsJSON, &payloadJSON,
			&archivedAt, &archivedBy, &trashedAt, &trashedBy, &trashReason); err != nil {
			return nil, fmt.Errorf("scan human_attention_responded row: %w", err)
		}

		body, err := decodeEventBodyFromRow(eventID, typeValue, ts, actorID, thread, refsJSON, payloadJSON)
		if err != nil {
			return nil, err
		}
		overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, trashedAt, trashedBy, trashReason)
		events = append(events, body)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate human_attention_responded page: %w", err)
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

	query := `SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json,
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
			archivedAt  sql.NullString
			archivedBy  sql.NullString
			trashedAt   sql.NullString
			trashedBy   sql.NullString
			trashReason sql.NullString
		)
		if err := rows.Scan(&eventID, &typeValue, &ts, &actorID, &thread, &refsJSON, &payloadJSON,
			&archivedAt, &archivedBy, &trashedAt, &trashedBy, &trashReason); err != nil {
			return nil, fmt.Errorf("scan event after cursor: %w", err)
		}

		body, err := decodeEventBodyFromRow(eventID, typeValue, ts, actorID, thread, refsJSON, payloadJSON)
		if err != nil {
			return nil, err
		}
		overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, trashedAt, trashedBy, trashReason)
		events = append(events, body)
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
	if subjectRef := strings.TrimSpace(anyStringValue(body["subject_ref"])); subjectRef == "" {
		if legacyTopicRef := strings.TrimSpace(anyStringValue(body["topic_ref"])); legacyTopicRef != "" {
			body["subject_ref"] = legacyTopicRef
		}
	}
	delete(body, "topic_ref")

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

	delete(body, "status")
	body["state"] = canonicalLifecycleState(r.ArchivedAt, r.TrashedAt)

	return body, nil
}

// StripThreadPlanningFieldsForAPI removes planning-only fields that belong on topics from a thread
// map before HTTP responses. Internal callers (e.g. projections) use GetThread without stripping.
func StripThreadPlanningFieldsForAPI(m map[string]any) {
	if m == nil {
		return
	}
	delete(m, "priority")
	delete(m, "tags")
	delete(m, "cadence")
	delete(m, "next_check_in_at")
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

func eventProvenance() map[string]any {
	return map[string]any{
		"sources": []string{"event:" + provenanceEventIDPlaceholder},
	}
}

func replaceEventProvenancePlaceholder(body map[string]any, eventID string) {
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
	placeholder := "event:" + provenanceEventIDPlaceholder
	for idx, source := range sources {
		if source == placeholder {
			sources[idx] = "event:" + eventID
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

func buildListThreadsQuery(filter ThreadListFilter) (string, []any) {
	query := `SELECT threads.id, threads.kind, threads.thread_id, threads.updated_at, threads.updated_by, threads.body_json, threads.provenance_json, threads.archived_at, threads.archived_by, threads.trashed_at, threads.trashed_by, threads.trash_reason
		 FROM threads`
	args := make([]any, 0, 9)
	hasWhere := false
	appendClause := func(clause string) {
		if hasWhere {
			query += ` AND ` + clause
			return
		}
		query += ` WHERE ` + clause
		hasWhere = true
	}
	appendClause(LifecycleStatesOrGroup("threads.archived_at", "threads.trashed_at", filter.States))
	if q := strings.TrimSpace(filter.Query); q != "" {
		searchPattern := "%" + strings.ToLower(q) + "%"
		appendClause(`(LOWER(threads.id) LIKE ? OR LOWER(threads.thread_id) LIKE ? OR LOWER(COALESCE(json_extract(body_json, '$.subject_ref'), json_extract(body_json, '$.topic_ref'), '')) LIKE ? OR LOWER(COALESCE(json_extract(body_json, '$.title'), '')) LIKE ? OR LOWER(COALESCE(json_extract(body_json, '$.current_summary'), '')) LIKE ?)`)
		args = append(args, searchPattern, searchPattern, searchPattern, searchPattern, searchPattern)
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
		lifecyclePrimary := LifecycleStatesOrGroup("archived_at", "trashed_at", filter.States)
		lifecycleSecondary := LifecycleStatesOrGroup("artifacts.archived_at", "artifacts.trashed_at", filter.States)
		primaryClauses = append(primaryClauses, lifecyclePrimary)
		secondaryClauses = append(secondaryClauses, lifecycleSecondary)
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
	query += ` AND ` + LifecycleStatesOrGroup("archived_at", "trashed_at", filter.States)
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
