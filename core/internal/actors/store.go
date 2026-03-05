package actors

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrAlreadyExists = errors.New("actor already exists")

const SystemActorID = "oar-core"

type Actor struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"display_name"`
	Tags        []string `json:"tags"`
	CreatedAt   string   `json:"created_at"`
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

func (s *Store) EnsureSystemActor(ctx context.Context, now time.Time) (Actor, error) {
	createdAt := now.UTC().Format(time.RFC3339Nano)
	return s.EnsureRegistered(ctx, Actor{
		ID:          SystemActorID,
		DisplayName: "OAR Core",
		Tags:        []string{"system"},
		CreatedAt:   createdAt,
	})
}

func (s *Store) List(ctx context.Context) ([]Actor, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("actor store database is not initialized")
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, display_name, tags_json, created_at FROM actors ORDER BY created_at ASC, id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query actors: %w", err)
	}
	defer rows.Close()

	actors := make([]Actor, 0)
	for rows.Next() {
		var actor Actor
		var tagsJSON string
		if err := rows.Scan(&actor.ID, &actor.DisplayName, &tagsJSON, &actor.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan actor row: %w", err)
		}
		if err := json.Unmarshal([]byte(tagsJSON), &actor.Tags); err != nil {
			return nil, fmt.Errorf("decode actor tags: %w", err)
		}
		if actor.Tags == nil {
			actor.Tags = []string{}
		}
		actors = append(actors, actor)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate actor rows: %w", err)
	}

	return actors, nil
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
