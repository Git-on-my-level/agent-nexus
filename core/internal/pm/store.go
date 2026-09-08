package pm

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// Store uses the core SQLite database, not a second local or channel database.
// JSON bodies retain exact instructions/origins; indexed columns provide scope,
// stable deduplication and compare-and-swap state transitions across processes.
type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, ErrInvalid
	}
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS pm_records (
 kind TEXT NOT NULL, id TEXT NOT NULL, workspace_id TEXT NOT NULL, actor_id TEXT NOT NULL,
 parent_id TEXT NOT NULL DEFAULT '', revision INTEGER NOT NULL, body BLOB NOT NULL,
 PRIMARY KEY(kind,id));
 CREATE INDEX IF NOT EXISTS pm_records_scope ON pm_records(kind,workspace_id,actor_id,parent_id);`)
	if err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}
func stableID(parts ...string) string {
	b, _ := json.Marshal(parts)
	sum := sha256.Sum256(b)
	return "pm_" + hex.EncodeToString(sum[:16])
}
func (s *Store) get(ctx context.Context, kind, id string, out any) error {
	var b []byte
	err := s.db.QueryRowContext(ctx, `SELECT body FROM pm_records WHERE kind=? AND id=?`, kind, id).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
func (s *Store) insert(ctx context.Context, kind, id, ws, actor, parent string, value any) (bool, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return false, err
	}
	r, err := s.db.ExecContext(ctx, `INSERT INTO pm_records(kind,id,workspace_id,actor_id,parent_id,revision,body) VALUES(?,?,?,?,?,1,?) ON CONFLICT(kind,id) DO NOTHING`, kind, id, ws, actor, parent, b)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n == 1, err
}
func (s *Store) cas(ctx context.Context, kind, id string, revision int, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	r, err := s.db.ExecContext(ctx, `UPDATE pm_records SET revision=revision+1,body=? WHERE kind=? AND id=? AND revision=?`, b, kind, id, revision)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	return nil
}
func listRecords[T any](ctx context.Context, s *Store, kind, ws, actor, parent string) ([]T, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT body FROM pm_records WHERE kind=? AND workspace_id=? AND actor_id=? AND (?='' OR parent_id=?) ORDER BY rowid LIMIT 200`, kind, ws, actor, parent, parent)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]T, 0)
	for rows.Next() {
		var b []byte
		var v T
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// answer commits exact approval and durable handoff intent in one transaction.
func (s *Store) answer(ctx context.Context, d Decision, a *Action, expected int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	r, err := tx.ExecContext(ctx, `UPDATE pm_records SET revision=revision+1,body=? WHERE kind='decision' AND id=? AND revision=?`, b, d.ID, expected)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	if a != nil {
		b, err = json.Marshal(a)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO pm_records(kind,id,workspace_id,actor_id,parent_id,revision,body) VALUES('action',?,?,?,?,1,?)`, a.ID, a.WorkspaceID, a.ActorID, d.ID, b)
		if err != nil {
			return fmt.Errorf("persist action intent: %w", err)
		}
	}
	return tx.Commit()
}
