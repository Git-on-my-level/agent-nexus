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
		var raw []byte
		var current Decision
		if err := tx.QueryRowContext(ctx, `SELECT body FROM pm_records WHERE kind='decision' AND id=?`, d.ID).Scan(&raw); err == nil && json.Unmarshal(raw, &current) == nil && current.Status == Superseded && current.SupersededBy != "" {
			return &SupersededDecisionError{SupersededBy: current.SupersededBy}
		}
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

// insertTurn observes admission and its failure reason under the same write lock,
// including across multiple Service instances. Duplicate IDs are replayed by callers.
func (s *Store) insertTurn(ctx context.Context, t Turn, maxQueued int) (bool, error) {
	b, err := json.Marshal(t)
	if err != nil {
		return false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE pm_records SET revision=revision WHERE kind='turn' AND id=?", t.ID); err != nil {
		return false, err
	}
	var existing string
	err = tx.QueryRowContext(ctx, "SELECT id FROM pm_records WHERE kind='turn' AND id=?", t.ID).Scan(&existing)
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	err = tx.QueryRowContext(ctx, `SELECT id FROM pm_records WHERE kind='turn' AND workspace_id=? AND parent_id=?
 AND json_extract(body,'$.status') IN ('pending_delivery','sending','unknown')
 AND julianday(json_extract(body,'$.deadline'))>julianday('now') ORDER BY rowid LIMIT 1`, t.WorkspaceID, t.ConversationID).Scan(&existing)
	if err == nil {
		return false, &BusyError{Reason: "conversation", TurnID: existing}
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	var queued int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM pm_records WHERE kind='turn' AND workspace_id=?
 AND json_extract(body,'$.status') IN ('pending_delivery','sending','unknown')
 AND julianday(json_extract(body,'$.deadline'))>julianday('now')
 AND NOT (COALESCE(json_extract(body,'$.lease_token'),'') != ''
          AND COALESCE(julianday(json_extract(body,'$.lease_expires_at'))>julianday('now'),0))`, t.WorkspaceID).Scan(&queued)
	if err != nil {
		return false, err
	}
	if queued >= maxQueued {
		return false, &BusyError{Reason: "queue", Queued: queued, Limit: maxQueued}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO pm_records(kind,id,workspace_id,actor_id,parent_id,revision,body) VALUES('turn',?,?,?,?,1,?)`, t.ID, t.WorkspaceID, t.ActorID, t.ConversationID, b); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// proposeDecision serializes proposal deduplication and turn linkage in SQLite,
// including across Service instances. Only identical intent is reused; changed
// intent may supersede an awaiting decision, but non-human proposals cannot
// displace pending human intent. All checks and replacement writes are atomic.
func (s *Store) proposeDecision(ctx context.Context, d Decision, turnID, leaseToken string) (Decision, bool, error) {
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
	// Revalidate under the write lock, including before an idempotent replay.
	if turnID != "" {
		var t Turn
		if err = tx.QueryRowContext(ctx, "SELECT body FROM pm_records WHERE kind='turn' AND id=?", turnID).Scan(&raw); err != nil {
			return Decision{}, false, err
		}
		if err = json.Unmarshal(raw, &t); err != nil {
			return Decision{}, false, err
		}
		if t.WorkspaceID != d.WorkspaceID || t.ActorID != d.ActorID || t.AgentActorID != d.ProposedBy {
			return Decision{}, false, ErrForbidden
		}
		if !t.Deadline.After(time.Now()) || (t.Status != Sending && t.Status != Unknown) {
			return Decision{}, false, closedTurnError(t)
		}
		if err = leaseGuard(t, leaseToken); err != nil {
			return Decision{}, false, err
		}
	}
	inserted := false
	err = tx.QueryRowContext(ctx, "SELECT body FROM pm_records WHERE kind='decision' AND id=?", d.ID).Scan(&raw)
	if err == nil {
		var prior Decision
		if err = json.Unmarshal(raw, &prior); err != nil {
			return Decision{}, false, err
		}
		if !sameDecisionIntent(prior, d) || (prior.Status != AwaitingAnswer && (prior.ProposedBy != d.ProposedBy || prior.OriginKind != d.OriginKind || !sameOrigin(prior.Origin, d.Origin))) {
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
			if sameDecisionIntent(prior, d) {
				d = prior
				reuse = true
			} else {
				if err = rejectPendingHumanProposal(ctx, tx, d); err != nil {
					return Decision{}, false, err
				}
				d.Supersedes = prior.ID
				d.SupersedesProposedBy = prior.ProposedBy
				d.SupersedesOriginKind = prior.OriginKind
				prior.Status = Superseded
				prior.SupersededBy = d.ID
				prior.SupersededByProposedBy = d.ProposedBy
				prior.SupersededByOriginKind = d.OriginKind
				prior.SupersededReason = "Replaced by a proposal with changed payload, instruction, or target revision"
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
			if err = rejectPendingHumanProposal(ctx, tx, d); err != nil {
				return Decision{}, false, err
			}
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

// claimTurn recovers an owned lease before allocating under a SQLite write lock.
// A runner identity must be used by only one serial worker at a time.
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
	held, waiting := 0, 0
	var candidate *Turn
	for i := range turns {
		t := &turns[i]
		if !t.Deadline.After(now) {
			continue
		}
		if leaseHeld(*t, now) {
			held++
			if t.AgentActorID == p.ActorID && t.LeaseOwner == runner {
				return *t, tx.Commit()
			}
			continue
		}
		if t.AgentActorID == p.ActorID {
			waiting++
			if candidate == nil {
				candidate = t
			}
		}
	}
	if candidate == nil {
		return Turn{}, ErrEmpty
	}
	if held >= capacity {
		return Turn{}, &BusyError{Reason: "capacity", InFlight: held, Limit: capacity, Waiting: waiting}
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

// Proposer and delivery origin are provenance, not proposal intent.
func sameDecisionIntent(a, b Decision) bool {
	return a.WorkspaceID == b.WorkspaceID && a.ActorID == b.ActorID && a.WorkRef == b.WorkRef && a.Scope == b.Scope && a.Instruction == b.Instruction && a.TargetRevision == b.TargetRevision && reflect.DeepEqual(a.Payload, b.Payload)
}

func rejectPendingHumanProposal(ctx context.Context, tx *sql.Tx, d Decision) error {
	if d.OriginKind == "human" {
		return nil
	}
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM pm_records WHERE kind='decision' AND workspace_id=? AND json_extract(body,'$.work_ref')=? AND json_extract(body,'$.scope')=? AND json_extract(body,'$.status')='awaiting_answer' AND json_extract(body,'$.origin_kind')='human' ORDER BY rowid LIMIT 1`, d.WorkspaceID, d.WorkRef, d.Scope).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return &HumanProposalPendingError{PendingDecisionID: id}
}
