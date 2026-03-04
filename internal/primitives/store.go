package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrNotFound = errors.New("not found")

type ArtifactListFilter struct {
	Kind          string
	ThreadID      string
	CreatedBefore string
	CreatedAfter  string
}

type Store struct {
	db                 *sql.DB
	artifactContentDir string
}

func NewStore(db *sql.DB, artifactContentDir string) *Store {
	return &Store{db: db, artifactContentDir: artifactContentDir}
}

func (s *Store) AppendEvent(ctx context.Context, actorID string, event map[string]any) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}

	body := cloneMap(event)
	body["id"] = uuid.NewString()
	body["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
	body["actor_id"] = actorID

	typeValue, _ := body["type"].(string)
	threadID, _ := body["thread_id"].(string)
	refs, err := normalizeStringSlice(body["refs"])
	if err != nil {
		return nil, fmt.Errorf("event.refs: %w", err)
	}

	refsJSON, err := json.Marshal(refs)
	if err != nil {
		return nil, fmt.Errorf("marshal event refs: %w", err)
	}

	payload := map[string]any{}
	if rawPayload, ok := body["payload"]; ok && rawPayload != nil {
		switch p := rawPayload.(type) {
		case map[string]any:
			payload = p
		default:
			return nil, fmt.Errorf("event.payload must be an object when provided")
		}
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal event payload: %w", err)
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal event body: %w", err)
	}

	_, err = s.db.ExecContext(
		ctx,
		`INSERT INTO events(id, type, ts, actor_id, thread_id, refs_json, payload_json, body_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		body["id"],
		typeValue,
		body["ts"],
		actorID,
		threadID,
		string(refsJSON),
		string(payloadJSON),
		string(bodyJSON),
	)
	if err != nil {
		return nil, fmt.Errorf("insert event: %w", err)
	}

	return body, nil
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
	)
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, type, ts, actor_id, thread_id, refs_json, payload_json, body_json FROM events WHERE id = ?`,
		id,
	).Scan(&eventID, &typeValue, &ts, &actorID, &threadID, &refsJSON, &payloadJSON, &bodyJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query event: %w", err)
	}

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

func (s *Store) CreateArtifact(ctx context.Context, actorID string, artifact map[string]any, content any, contentType string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	if strings.TrimSpace(s.artifactContentDir) == "" {
		return nil, fmt.Errorf("artifact content directory is not configured")
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
	metadata["id"] = uuid.NewString()
	metadata["created_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	metadata["created_by"] = actorID
	metadata["content_type"] = contentType
	metadata["content_path"] = filepath.Join(s.artifactContentDir, metadata["id"].(string))

	contentPath := metadata["content_path"].(string)
	file, err := os.OpenFile(contentPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("create artifact content file: %w", err)
	}

	if _, err := file.Write(encodedContent); err != nil {
		_ = file.Close()
		_ = os.Remove(contentPath)
		return nil, fmt.Errorf("write artifact content: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(contentPath)
		return nil, fmt.Errorf("close artifact content file: %w", err)
	}

	refsJSON, err := json.Marshal(refs)
	if err != nil {
		_ = os.Remove(contentPath)
		return nil, fmt.Errorf("marshal artifact refs: %w", err)
	}

	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		_ = os.Remove(contentPath)
		return nil, fmt.Errorf("marshal artifact metadata: %w", err)
	}

	_, err = s.db.ExecContext(
		ctx,
		`INSERT INTO artifacts(id, kind, created_at, created_by, content_type, content_path, refs_json, metadata_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		metadata["id"],
		kind,
		metadata["created_at"],
		actorID,
		contentType,
		contentPath,
		string(refsJSON),
		string(metadataJSON),
	)
	if err != nil {
		_ = os.Remove(contentPath)
		return nil, fmt.Errorf("insert artifact: %w", err)
	}

	return metadata, nil
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

	var metadata map[string]any
	if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
		return nil, fmt.Errorf("decode artifact metadata: %w", err)
	}

	return metadata, nil
}

func (s *Store) GetArtifactContent(ctx context.Context, id string) ([]byte, string, error) {
	if s == nil || s.db == nil {
		return nil, "", fmt.Errorf("primitives store database is not initialized")
	}

	var contentPath string
	var contentType string
	err := s.db.QueryRowContext(ctx, `SELECT content_path, content_type FROM artifacts WHERE id = ?`, id).Scan(&contentPath, &contentType)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", fmt.Errorf("query artifact content path: %w", err)
	}

	body, err := os.ReadFile(contentPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
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

	query := `SELECT metadata_json FROM artifacts WHERE 1=1`
	args := make([]any, 0)

	if filter.Kind != "" {
		query += ` AND kind = ?`
		args = append(args, filter.Kind)
	}
	if filter.CreatedAfter != "" {
		query += ` AND created_at >= ?`
		args = append(args, filter.CreatedAfter)
	}
	if filter.CreatedBefore != "" {
		query += ` AND created_at <= ?`
		args = append(args, filter.CreatedBefore)
	}
	query += ` ORDER BY created_at ASC, id ASC`

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

		var metadata map[string]any
		if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
			return nil, fmt.Errorf("decode artifact metadata: %w", err)
		}

		if filter.ThreadID != "" {
			refs, err := normalizeStringSlice(metadata["refs"])
			if err != nil {
				return nil, fmt.Errorf("decode artifact refs for filter: %w", err)
			}
			if !containsThreadRef(refs, filter.ThreadID) {
				continue
			}
		}

		artifacts = append(artifacts, metadata)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate artifact rows: %w", err)
	}

	return artifacts, nil
}

func (s *Store) GetSnapshot(ctx context.Context, id string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}

	var (
		snapshotID     string
		typeValue      string
		threadID       sql.NullString
		updatedAt      string
		updatedBy      string
		bodyJSON       string
		provenanceJSON string
	)

	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, kind, thread_id, updated_at, updated_by, body_json, provenance_json FROM snapshots WHERE id = ?`,
		id,
	).Scan(&snapshotID, &typeValue, &threadID, &updatedAt, &updatedBy, &bodyJSON, &provenanceJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query snapshot: %w", err)
	}

	body := map[string]any{}
	if strings.TrimSpace(bodyJSON) != "" {
		if err := json.Unmarshal([]byte(bodyJSON), &body); err != nil {
			return nil, fmt.Errorf("decode snapshot body: %w", err)
		}
	}

	provenance := map[string]any{}
	if strings.TrimSpace(provenanceJSON) != "" {
		if err := json.Unmarshal([]byte(provenanceJSON), &provenance); err != nil {
			return nil, fmt.Errorf("decode snapshot provenance: %w", err)
		}
	}

	body["id"] = snapshotID
	body["type"] = typeValue
	body["updated_at"] = updatedAt
	body["updated_by"] = updatedBy
	if threadID.Valid {
		body["thread_id"] = threadID.String
	}
	body["provenance"] = provenance

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

func containsThreadRef(refs []string, threadID string) bool {
	target := "thread:" + threadID
	for _, ref := range refs {
		if ref == target {
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
