package decide

import (
	"agent-nexus-core/internal/contentusage"
	"agent-nexus-core/internal/resourceaccess"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
)

// Store uses the core SQLite database, not a second local or channel database.
// JSON bodies retain exact instructions; indexed columns provide scope, stable
// deduplication and compare-and-swap state transitions across processes.
type Store struct{ db *sql.DB }

func (s *Store) database() *resourceaccess.DB { return resourceaccess.NewDB(s.db) }

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, ErrInvalid
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`CREATE TABLE IF NOT EXISTS idempotency_replays (
 scope TEXT NOT NULL, actor_id TEXT NOT NULL, request_key TEXT NOT NULL, request_hash TEXT NOT NULL,
 response_status INTEGER NOT NULL, response_json TEXT NOT NULL, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
 PRIMARY KEY(scope,actor_id,request_key));
 CREATE INDEX IF NOT EXISTS idx_idempotency_replays_created_at ON idempotency_replays(created_at);
 CREATE TABLE IF NOT EXISTS pm_records (
 kind TEXT NOT NULL, id TEXT NOT NULL, workspace_id TEXT NOT NULL, actor_id TEXT NOT NULL,
 parent_id TEXT NOT NULL DEFAULT '', revision INTEGER NOT NULL, body BLOB NOT NULL,
 PRIMARY KEY(kind,id));
 CREATE VIEW IF NOT EXISTS pm_card_decision_positions AS SELECT kind,workspace_id,id,json_extract(body,'$.work_ref') AS card_ref,json_extract(body,'$.status') AS status FROM pm_records;
 CREATE VIEW IF NOT EXISTS pm_reader_decision_positions AS SELECT kind,workspace_id,actor_id,id,json_extract(body,'$.status') AS status FROM pm_records;
 CREATE INDEX IF NOT EXISTS pm_records_reader_awaiting ON pm_records(kind,workspace_id,actor_id,json_extract(body,'$.status'),id);
 CREATE INDEX IF NOT EXISTS pm_records_card_awaiting ON pm_records(kind,workspace_id,json_extract(body,'$.work_ref'),json_extract(body,'$.status'),id);
 CREATE INDEX IF NOT EXISTS pm_records_proposal_awaiting ON pm_records(workspace_id,actor_id,json_extract(body,'$.work_ref'),json_extract(body,'$.scope')) WHERE kind='decision' AND json_extract(body,'$.status')='awaiting_answer';
 CREATE INDEX IF NOT EXISTS pm_records_proposal_human ON pm_records(workspace_id,actor_id,json_extract(body,'$.work_ref'),json_extract(body,'$.scope')) WHERE kind='decision' AND json_extract(body,'$.status')='awaiting_answer' AND json_extract(body,'$.origin_kind')='human';
 CREATE INDEX IF NOT EXISTS pm_records_scope ON pm_records(kind,workspace_id,actor_id,parent_id);
 CREATE INDEX IF NOT EXISTS pm_records_page ON pm_records(kind,workspace_id,rtrim(COALESCE(json_extract(body,'$.created_at'),''),'Z'));
 CREATE INDEX IF NOT EXISTS pm_records_owner_page ON pm_records(kind,workspace_id,actor_id,rtrim(COALESCE(json_extract(body,'$.created_at'),''),'Z'));
 CREATE INDEX IF NOT EXISTS pm_records_status ON pm_records(kind,workspace_id,json_extract(body,'$.status'));
 CREATE INDEX IF NOT EXISTS pm_records_legacy_actions ON pm_records(kind,workspace_id,parent_id) WHERE kind='action' AND json_extract(body,'$.created_at') IS NULL;
 CREATE INDEX IF NOT EXISTS pm_records_changes ON pm_records(kind,workspace_id,julianday(json_extract(body,'$.created_at')));
 CREATE INDEX IF NOT EXISTS pm_records_overview_owner ON pm_records(kind,workspace_id,actor_id,json_extract(body,'$.status'));
 CREATE INDEX IF NOT EXISTS pm_records_parent_status ON pm_records(kind,workspace_id,parent_id,json_extract(body,'$.status'));`)
	if err != nil {
		return nil, err
	}
	if err := contentusage.InstallTable(context.Background(), tx, "pm_records"); err != nil {
		return nil, err
	}
	if err := resourceaccess.InstallPMAccess(context.Background(), tx, false); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
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
	err := s.database().QueryRowContext(ctx, `SELECT body FROM pm_records WHERE kind=? AND id=?`, kind, id).Scan(&b)
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
	r, err := s.database().ExecContext(ctx, `INSERT INTO pm_records(kind,id,workspace_id,actor_id,parent_id,revision,body) VALUES(?,?,?,?,?,1,?) ON CONFLICT(kind,id) DO NOTHING`, kind, id, ws, actor, parent, b)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n == 1, err
}
func (s *Store) cas(ctx context.Context, kind, id string, revision int, value any) error {
	if c, ok := ctx.Value(controlKey{}).(*controlReplay); ok && c.receipt == nil && kind == "action" && id == c.action.ID {
		out, ok := value.(Action)
		if !ok {
			return ErrInvalid
		}
		return s.commitControl(ctx, c, func(ctx context.Context) error {
			return s.cas(context.WithValue(ctx, controlKey{}, nil), kind, id, revision, value)
		}, out)
	}

	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	r, err := s.database().ExecContext(ctx, `UPDATE pm_records SET revision=revision+1,body=? WHERE kind=? AND id=? AND revision=?`, b, kind, id, revision)
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
		query := `SELECT rowid,body FROM pm_records WHERE kind=? AND workspace_id=?`
		args := []any{kind, ws}
		if actor != "" {
			query += ` AND actor_id=?`
			args = append(args, actor)
		}
		if parent != "" {
			query += ` AND parent_id=?`
			args = append(args, parent)
		}
		query += ` AND rowid>? ORDER BY rowid LIMIT 200`
		args = append(args, after)
		rows, err := s.database().QueryContext(ctx, query, args...)
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
func (s *Store) answer(ctx context.Context, d Decision, a *Action, expected int, replays ...*answerReplay) error {
	tx, err := s.database().BeginTx(ctx, nil)
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
	if len(replays) > 0 {
		if err := writeAnswerReplayTx(ctx, tx, replays[0]); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// proposeDecision serializes proposal deduplication in SQLite, including across
// Service instances. Only identical intent is reused; changed intent may
// supersede an awaiting decision, but non-human proposals cannot displace
// pending human intent. All checks and replacement writes are atomic.
func (s *Store) proposeDecision(ctx context.Context, d Decision) (Decision, bool, error) {
	tx, err := s.database().BeginTx(ctx, nil)
	if err != nil {
		return Decision{}, false, err
	}
	defer tx.Rollback()
	// Acquire the write lock before reading the dedupe key.
	if _, err = tx.ExecContext(ctx, "UPDATE pm_records SET revision=revision WHERE kind='decision' AND id=?", d.ID); err != nil {
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
		if !sameDecisionIntent(prior, d) || (prior.Status != AwaitingAnswer && (prior.ProposedBy != d.ProposedBy || prior.OriginKind != d.OriginKind)) {
			return Decision{}, false, &DecisionConflict{ExistingDecisionID: prior.ID}
		}
		d = prior
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Decision{}, false, err
	} else {
		err = tx.QueryRowContext(ctx, proposalCandidateSQL, d.WorkspaceID, d.ActorID, d.WorkRef, d.Scope).Scan(&raw)
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
	return d, inserted, tx.Commit()
}

// Proposer and delivery origin are provenance, not proposal intent.
func sameDecisionIntent(a, b Decision) bool {
	return a.WorkspaceID == b.WorkspaceID && a.ActorID == b.ActorID && a.WorkRef == b.WorkRef && a.Scope == b.Scope && a.Instruction == b.Instruction && a.TargetRevision == b.TargetRevision && reflect.DeepEqual(a.Payload, b.Payload)
}

func rejectPendingHumanProposal(ctx context.Context, tx *resourceaccess.Tx, d Decision) error {
	if d.OriginKind == "human" {
		return nil
	}
	var id string
	err := tx.QueryRowContext(ctx, pendingHumanProposalSQL, d.WorkspaceID, d.ActorID, d.WorkRef, d.Scope).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return &HumanProposalPendingError{PendingDecisionID: id}
}
