package pm

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"
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
	out := make([]T, 0)
	var after int64
	for {
		rows, err := s.db.QueryContext(ctx, `SELECT rowid,body FROM pm_records WHERE kind=? AND workspace_id=? AND (?='' OR actor_id=?) AND (?='' OR parent_id=?) AND rowid>? ORDER BY rowid LIMIT 200`, kind, ws, actor, actor, parent, parent, after)
		if err != nil {
			return nil, err
		}
		count := 0
		for rows.Next() {
			var raw []byte
			var item T
			if err = rows.Scan(&after, &raw); err != nil {
				rows.Close()
				return nil, err
			}
			if err = json.Unmarshal(raw, &item); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, item)
			count++
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		if count < 200 {
			return out, nil
		}
	}

}

// listOpenTurns returns every pending, sending or unknown turn, including those past the
// bounded listRecords window. Claim and deadline expiry must see current work,
// not the oldest 200 historical rows.
func listOpenTurns(ctx context.Context, s *Store, ws string) ([]Turn, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT body FROM pm_records WHERE kind='turn' AND workspace_id=? AND json_extract(body,'$.status') IN ('pending_delivery','sending','unknown') ORDER BY rowid`, ws)
	if err != nil {
		return nil, err
	}
	return scanBodies[Turn](rows)
}

func scanBodies[T any](rows *sql.Rows) ([]T, error) {
	defer rows.Close()
	out := make([]T, 0)
	for rows.Next() {
		var b []byte
		var v T
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(b, &v); err != nil {
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

// insertTurn enforces capacity and session serialization in the same SQLite
// statement as admission, including across multiple Service instances.
func (s *Store) insertTurn(ctx context.Context, t Turn, maxConcurrent int) (bool, error) {
	b, err := json.Marshal(t)
	if err != nil {
		return false, err
	}
	r, err := s.db.ExecContext(ctx, `INSERT INTO pm_records(kind,id,workspace_id,actor_id,parent_id,revision,body)
 SELECT 'turn',?,?,?,?,1,? WHERE
 (SELECT count(*) FROM pm_records WHERE kind='turn' AND workspace_id=?
 AND json_extract(body,'$.status') IN ('pending_delivery','sending')
 AND julianday(json_extract(body,'$.deadline'))>julianday('now')) < ?
 AND NOT EXISTS(SELECT 1 FROM pm_records WHERE kind='turn' AND workspace_id=? AND parent_id=?
 AND json_extract(body,'$.status') IN ('pending_delivery','sending','unknown')
 AND julianday(json_extract(body,'$.deadline'))>julianday('now'))
 ON CONFLICT(kind,id) DO NOTHING`, t.ID, t.WorkspaceID, t.ActorID, t.ConversationID, b, t.WorkspaceID, maxConcurrent, t.WorkspaceID, t.ConversationID)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n == 1, err
}

// proposeDecision serializes proposal deduplication and turn linkage in SQLite,
// including across Service instances. Only identical intent is reused; changed
// intent supersedes the awaiting decision in the same transaction as its replacement.
func (s *Store) proposeDecision(ctx context.Context, d Decision, turnID string) (Decision, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Decision{}, false, err
	}
	defer tx.Rollback()
	// Acquire the write lock before reading the dedupe key.
	if _, err = tx.ExecContext(ctx, "UPDATE pm_records SET revision=revision WHERE kind='turn' AND id=?", turnID); err != nil {
		return Decision{}, false, err
	}
	var raw []byte
	inserted := false
	err = tx.QueryRowContext(ctx, "SELECT body FROM pm_records WHERE kind='decision' AND id=?", d.ID).Scan(&raw)
	if err == nil {
		var prior Decision
		if err = json.Unmarshal(raw, &prior); err != nil {
			return Decision{}, false, err
		}
		if prior.ProposedBy != d.ProposedBy || prior.OriginKind != d.OriginKind || prior.WorkRef != d.WorkRef || prior.Instruction != d.Instruction || prior.Scope != d.Scope || prior.TargetRevision != d.TargetRevision || !sameOrigin(prior.Origin, d.Origin) || !reflect.DeepEqual(prior.Payload, d.Payload) {
			return Decision{}, false, &DecisionConflict{ExistingDecisionID: prior.ID}
		}
		d = prior
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Decision{}, false, err
	} else {
		err = tx.QueryRowContext(ctx, `SELECT body FROM pm_records WHERE kind='decision' AND workspace_id=? AND actor_id=? AND json_extract(body,'$.work_ref')=? AND json_extract(body,'$.scope')=? AND json_extract(body,'$.status')='awaiting_answer' ORDER BY rowid LIMIT 1`, d.WorkspaceID, d.ActorID, d.WorkRef, d.Scope).Scan(&raw)
		reuse := false
		if err == nil {
			var prior Decision
			if err = json.Unmarshal(raw, &prior); err != nil {
				return Decision{}, false, err
			}
			if prior.ProposedBy == d.ProposedBy && prior.OriginKind == d.OriginKind && prior.Instruction == d.Instruction && prior.TargetRevision == d.TargetRevision && reflect.DeepEqual(prior.Payload, d.Payload) && sameOrigin(prior.Origin, d.Origin) {
				d = prior
				reuse = true
			} else {
				prior.Status = Superseded
				prior.SupersededBy = d.ID
				prior.SupersededReason = "Replaced by a proposal with changed payload, instruction, target revision, or origin"
				prior.CanAnswer = false
				prior.Revision++
				raw, err = json.Marshal(prior)
				if err != nil {
					return Decision{}, false, err
				}
				if _, err = tx.ExecContext(ctx, "UPDATE pm_records SET revision=revision+1,body=? WHERE kind='decision' AND id=?", raw, prior.ID); err != nil {
					return Decision{}, false, err
				}
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return Decision{}, false, err
		}
		if !reuse {
			raw, err = json.Marshal(d)
			if err != nil {
				return Decision{}, false, err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO pm_records(kind,id,workspace_id,actor_id,parent_id,revision,body) VALUES('decision',?,?,?,'',1,?)`, d.ID, d.WorkspaceID, d.ActorID, raw); err != nil {
				return Decision{}, false, err
			}
			inserted = true
		}
	}
	if turnID != "" {
		var t Turn
		if err = tx.QueryRowContext(ctx, "SELECT body FROM pm_records WHERE kind='turn' AND id=?", turnID).Scan(&raw); err != nil {
			return Decision{}, false, err
		}
		if err = json.Unmarshal(raw, &t); err != nil {
			return Decision{}, false, err
		}
		if t.WorkspaceID != d.WorkspaceID || t.ActorID != d.ActorID {
			return Decision{}, false, ErrForbidden
		}
		found := false
		for _, id := range t.DecisionIDs {
			if id == d.ID {
				found = true
			}
		}
		if !found {
			t.DecisionIDs = append(t.DecisionIDs, d.ID)
			t.Revision++
			raw, err = json.Marshal(t)
			if err != nil {
				return Decision{}, false, err
			}
			if _, err = tx.ExecContext(ctx, "UPDATE pm_records SET revision=revision+1,body=? WHERE kind='turn' AND id=?", raw, t.ID); err != nil {
				return Decision{}, false, err
			}
		}
	}
	return d, inserted, tx.Commit()
}

// claimTurn counts active leases and allocates one new lease under a single
// SQLite write lock. Claim is allocation, not replay: handing the same lease
// to another worker of the same runner would execute the turn concurrently.
func (s *Store) claimTurn(ctx context.Context, p Principal, runner string, now time.Time, capacity int, ttl time.Duration, maxOutput int) (Turn, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Turn{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE pm_records SET revision=revision WHERE kind='turn' AND workspace_id=?", p.WorkspaceID); err != nil {
		return Turn{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT body FROM pm_records WHERE kind='turn' AND workspace_id=? AND json_extract(body,'$.status') IN ('sending','unknown') ORDER BY rowid`, p.WorkspaceID)
	if err != nil {
		return Turn{}, err
	}
	turns, err := scanBodies[Turn](rows)
	if err != nil {
		return Turn{}, err
	}
	held := 0
	var candidate *Turn
	for i := range turns {
		t := &turns[i]
		if !t.Deadline.After(now) {
			continue
		}
		if leaseHeld(*t, now) {
			held++
			continue
		}
		if candidate == nil && t.AgentActorID == p.ActorID {
			candidate = t
		}
	}
	if held >= capacity || candidate == nil {
		return Turn{}, ErrEmpty
	}
	t := *candidate
	t.LeaseToken = newLeaseToken()
	t.LeaseOwner = runner
	t.ClaimedAt = &now
	t.LeaseExpiresAt = leaseDeadline(t.Deadline, now, ttl)
	t.MaxOutputBytes = maxOutput
	t.Revision++
	raw, err := json.Marshal(t)
	if err != nil {
		return Turn{}, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE pm_records SET revision=revision+1,body=? WHERE kind='turn' AND id=?", raw, t.ID); err != nil {
		return Turn{}, err
	}
	return t, tx.Commit()
}
