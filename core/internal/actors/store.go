package actors

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var ErrAlreadyExists = errors.New("actor already exists")
var ErrActorNotFound = errors.New("actor not found")
var ErrInvalidCursor = errors.New("invalid cursor")

const (
	// SystemActorID is the canonical actor id for automated core writes (projections,
	// router sidecar, etc.). It is not valid as a client-selected identity.
	SystemActorID = "system"

	legacySystemActorID = "anx-core"
)

type Actor struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"display_name"`
	Tags        []string `json:"tags"`
	CreatedAt   string   `json:"created_at"`
}

type ActorListFilter struct {
	Query  string
	Limit  *int
	Cursor string
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Register(ctx context.Context, actor Actor) (Actor, error) {
	if s == nil || s.db == nil {
		return Actor{}, fmt.Errorf("actor store database is not initialized")
	}

	tags := actor.Tags
	if tags == nil {
		tags = []string{}
	}

	tagsJSON, err := json.Marshal(tags)
	if err != nil {
		return Actor{}, fmt.Errorf("marshal actor tags: %w", err)
	}

	_, err = s.db.ExecContext(
		ctx,
		`INSERT INTO actors(id, display_name, tags_json, created_at, metadata_json) VALUES (?, ?, ?, ?, '{}')`,
		actor.ID,
		actor.DisplayName,
		string(tagsJSON),
		actor.CreatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: actors.id") {
			return Actor{}, ErrAlreadyExists
		}
		return Actor{}, fmt.Errorf("insert actor: %w", err)
	}

	actor.Tags = tags
	return actor, nil
}

func (s *Store) EnsureRegistered(ctx context.Context, actor Actor) (Actor, error) {
	if s == nil || s.db == nil {
		return Actor{}, fmt.Errorf("actor store database is not initialized")
	}

	tags := actor.Tags
	if tags == nil {
		tags = []string{}
	}

	tagsJSON, err := json.Marshal(tags)
	if err != nil {
		return Actor{}, fmt.Errorf("marshal actor tags: %w", err)
	}

	_, err = s.db.ExecContext(
		ctx,
		`INSERT INTO actors(id, display_name, tags_json, created_at, metadata_json)
		 VALUES (?, ?, ?, ?, '{}')
		 ON CONFLICT(id) DO NOTHING`,
		actor.ID,
		actor.DisplayName,
		string(tagsJSON),
		actor.CreatedAt,
	)
	if err != nil {
		return Actor{}, fmt.Errorf("upsert actor: %w", err)
	}

	actor.Tags = tags
	return actor, nil
}

// IsReservedServiceActorID reports whether id refers to the reserved system actor,
// including legacy workspace rows or typed actor: refs.
func IsReservedServiceActorID(raw string) bool {
	s := strings.TrimSpace(raw)
	if s == "" {
		return false
	}
	if strings.Contains(s, ":") {
		prefix, id, _ := strings.Cut(s, ":")
		prefix = strings.TrimSpace(prefix)
		id = strings.TrimSpace(id)
		switch strings.ToLower(prefix) {
		case "actor", "human", "agent":
			s = id
		default:
			return false
		}
	}
	switch s {
	case SystemActorID, legacySystemActorID:
		return true
	default:
		return false
	}
}

// IsReservedServiceActorAssigneeRef checks assignee_refs-style values (bare ids or typed refs).
func IsReservedServiceActorAssigneeRef(raw string) bool {
	return IsReservedServiceActorID(raw)
}

func (s *Store) migrateLegacySystemActorRow(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("actor store database is not initialized")
	}
	var legacyCount, canonCount int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM actors WHERE id = ?`, legacySystemActorID).Scan(&legacyCount); err != nil {
		return fmt.Errorf("query legacy system actor: %w", err)
	}
	if legacyCount == 0 {
		return nil
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM actors WHERE id = ?`, SystemActorID).Scan(&canonCount); err != nil {
		return fmt.Errorf("query canonical system actor: %w", err)
	}
	if canonCount > 0 {
		_, err := s.db.ExecContext(ctx, `DELETE FROM actors WHERE id = ?`, legacySystemActorID)
		if err != nil {
			return fmt.Errorf("delete duplicate legacy system actor: %w", err)
		}
		return nil
	}
	tagsJSON, err := json.Marshal([]string{"system"})
	if err != nil {
		return fmt.Errorf("marshal system actor tags: %w", err)
	}
	_, err = s.db.ExecContext(
		ctx,
		`UPDATE actors SET id = ?, display_name = ?, tags_json = ? WHERE id = ?`,
		SystemActorID,
		"System",
		string(tagsJSON),
		legacySystemActorID,
	)
	if err != nil {
		return fmt.Errorf("migrate legacy system actor id: %w", err)
	}
	return nil
}

func (s *Store) EnsureSystemActor(ctx context.Context, now time.Time) (Actor, error) {
	if err := s.migrateLegacySystemActorRow(ctx); err != nil {
		return Actor{}, err
	}
	createdAt := now.UTC().Format(time.RFC3339Nano)
	return s.EnsureRegistered(ctx, Actor{
		ID:          SystemActorID,
		DisplayName: "System",
		Tags:        []string{"system"},
		CreatedAt:   createdAt,
	})
}

func (s *Store) List(ctx context.Context, filter ActorListFilter) ([]Actor, string, error) {
	if s == nil || s.db == nil {
		return nil, "", fmt.Errorf("actor store database is not initialized")
	}
	if filter.Cursor != "" {
		if _, err := decodeActorCursor(filter.Cursor); err != nil {
			return nil, "", fmt.Errorf("%w: %v", ErrInvalidCursor, err)
		}
	}

	query := `SELECT id, display_name, tags_json, created_at FROM actors WHERE 1=1`
	args := make([]any, 0, 3)

	if q := strings.TrimSpace(filter.Query); q != "" {
		searchPattern := "%" + strings.ToLower(q) + "%"
		query += ` AND (LOWER(id) LIKE ? OR LOWER(display_name) LIKE ?)`
		args = append(args, searchPattern, searchPattern)
	}

	query += ` ORDER BY created_at ASC, id ASC`

	if filter.Limit != nil && *filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, *filter.Limit+1)
		if filter.Cursor != "" {
			if offset, err := decodeActorCursor(filter.Cursor); err == nil && offset > 0 {
				query += ` OFFSET ?`
				args = append(args, offset)
			}
		}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("query actors: %w", err)
	}
	defer rows.Close()

	actors := make([]Actor, 0)
	for rows.Next() {
		var actor Actor
		var tagsJSON string
		if err := rows.Scan(&actor.ID, &actor.DisplayName, &tagsJSON, &actor.CreatedAt); err != nil {
			return nil, "", fmt.Errorf("scan actor row: %w", err)
		}
		if err := json.Unmarshal([]byte(tagsJSON), &actor.Tags); err != nil {
			return nil, "", fmt.Errorf("decode actor tags: %w", err)
		}
		if actor.Tags == nil {
			actor.Tags = []string{}
		}
		actors = append(actors, actor)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate actor rows: %w", err)
	}

	var nextCursor string
	if filter.Limit != nil && len(actors) > *filter.Limit {
		actors = actors[:*filter.Limit]
		offset := 0
		if filter.Cursor != "" {
			offset, _ = decodeActorCursor(filter.Cursor)
		}
		nextCursor = encodeActorCursor(offset + *filter.Limit)
	}

	return actors, nextCursor, nil
}

func encodeActorCursor(offset int) string {
	if offset <= 0 {
		return ""
	}
	cursor := fmt.Sprintf("offset:%d", offset)
	return base64.StdEncoding.EncodeToString([]byte(cursor))
}

func decodeActorCursor(cursor string) (int, error) {
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

func (s *Store) Exists(ctx context.Context, actorID string) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("actor store database is not initialized")
	}

	var value int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM actors WHERE id = ? LIMIT 1`, actorID).Scan(&value)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("query actor existence: %w", err)
	}

	return true, nil
}

// Get returns a single actor by id, or ErrActorNotFound.
func (s *Store) Get(ctx context.Context, actorID string) (Actor, error) {
	if s == nil || s.db == nil {
		return Actor{}, fmt.Errorf("actor store database is not initialized")
	}

	var actor Actor
	var tagsJSON string
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, display_name, tags_json, created_at FROM actors WHERE id = ?`,
		actorID,
	).Scan(&actor.ID, &actor.DisplayName, &tagsJSON, &actor.CreatedAt)
	if err == sql.ErrNoRows {
		return Actor{}, ErrActorNotFound
	}
	if err != nil {
		return Actor{}, fmt.Errorf("get actor: %w", err)
	}
	if err := json.Unmarshal([]byte(tagsJSON), &actor.Tags); err != nil {
		return Actor{}, fmt.Errorf("decode actor tags: %w", err)
	}
	if actor.Tags == nil {
		actor.Tags = []string{}
	}
	return actor, nil
}
