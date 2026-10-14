package pm

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"agent-nexus-core/internal/resourceaccess"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// ControlInput opts into a frozen, original-receipt control without changing
// legacy callers. Dispatch's receipt is admission, never proof of delivery.
type ControlInput struct {
	RequestKey       string `json:"request_key,omitempty"`
	IfRevision       *int   `json:"if_revision,omitempty"`
	IfActionRevision *int   `json:"if_action_revision,omitempty"`
}
type controlKey struct{}
type controlReplay struct {
	scope, actor, key, hash string
	action                  Action
	decision                Decision
	revision                int
	receipt                 *Action
}

func (s *Service) control(ctx context.Context, p Principal, operation, id string, in ControlInput) (Action, error) {
	run := func(ctx context.Context) (Action, error) {
		switch operation {
		case "dispatch":
			return s.DispatchDecision(ctx, p, id)
		case "reconcile":
			return s.ReconcileAction(ctx, p, id)
		case "acknowledge":
			return s.AcknowledgeAction(ctx, p, id)
		}
		return Action{}, ErrInvalid
	}
	if in.RequestKey == "" {
		if in.IfRevision != nil || in.IfActionRevision != nil {
			return Action{}, ErrInvalid
		}
		return run(ctx)
	}
	if !p.Human {
		return Action{}, ErrForbidden
	}
	if !validText(in.RequestKey, 256) || strings.TrimSpace(in.RequestKey) == "" || strings.ContainsRune(in.RequestKey, 0) || in.IfRevision == nil || *in.IfRevision < 1 {
		return Action{}, ErrInvalid
	}
	var a Action
	var d Decision
	var err error
	if operation == "dispatch" {
		if in.IfActionRevision == nil || *in.IfActionRevision < 1 {
			return Action{}, ErrInvalid
		}
		d, err = s.decision(ctx, p, id, "pm.approve")
		if err != nil {
			return Action{}, err
		}
		a, err = s.action(ctx, p, d.ActionID, "pm.action."+d.Scope)
	} else {
		if in.IfActionRevision != nil {
			return Action{}, ErrInvalid
		}
		a, err = s.action(ctx, p, id, "pm.read")
		if err == nil {
			d, err = s.decision(ctx, p, a.DecisionID, "pm.approve")
		}
	}
	// DispatchDecision terminalizes a still-pending unsent action when the
	// approved work is gone. Keep that closure on the keyed path too.
	if err != nil && (operation != "dispatch" || a.ID == "") {
		return Action{}, err
	}
	if a.ActorID != p.ActorID || d.ActorID != p.ActorID || d.ActionID != a.ID {
		return Action{}, ErrForbidden
	}
	raw, _ := json.Marshal(struct {
		ID    string
		Input ControlInput
	}{id, in})
	sum := sha256.Sum256(raw)
	c := &controlReplay{scope: "pm.controls." + operation + ":" + p.WorkspaceID, actor: p.ActorID, key: in.RequestKey, hash: hex.EncodeToString(sum[:]), action: a, decision: d, revision: *in.IfRevision}
	if operation == "dispatch" {
		c.revision = *in.IfActionRevision
	}
	if out, found, e := s.store.readControl(ctx, c); e != nil || found {
		return out, e
	}
	if a.Revision != c.revision || operation == "dispatch" && d.Revision != *in.IfRevision {
		return Action{}, ErrConflict
	}
	ctx = context.WithValue(ctx, controlKey{}, c)
	out, err := run(ctx)
	if c.receipt != nil {
		return *c.receipt, nil
	}
	// A concurrent request may have committed this exact key. Never rerun its
	// external handoff, nor substitute the current action for its original receipt.
	if prior, found, e := s.store.readControl(ctx, c); e != nil || found {
		return prior, e
	}
	if err != nil {
		return Action{}, err
	}
	// Verified/acknowledged controls can be no-ops. Journal that observation under
	// the same frozen versions, so a later retry cannot act on a different state.
	err = s.store.commitControl(ctx, c, func(context.Context) error { return nil }, out)
	if err != nil {
		// Identical no-op keys can both observe the current action and then race
		// on journaling. The loser must return the stored receipt, not conflict.
		if prior, found, e := s.store.readControl(ctx, c); e != nil || found {
			return prior, e
		}
		return Action{}, err
	}
	if c.receipt != nil {
		return *c.receipt, nil
	}
	return out, nil
}

func (s *Store) readControl(ctx context.Context, c *controlReplay) (Action, bool, error) {
	var hash string
	var raw []byte
	err := s.database().QueryRowContext(ctx, `SELECT request_hash,response_json FROM idempotency_replays WHERE scope=? AND actor_id=? AND request_key=?`, c.scope, c.actor, c.key).Scan(&hash, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return Action{}, false, nil
	}
	if err != nil {
		return Action{}, false, err
	}
	if hash != c.hash {
		return Action{}, false, ErrConflict
	}
	var a Action
	if err = json.Unmarshal(raw, &a); err != nil {
		return Action{}, false, err
	}
	if a.ID != c.action.ID || a.ActorID != c.actor || a.WorkspaceID != c.action.WorkspaceID {
		return Action{}, false, ErrForbidden
	}
	return a, true, nil
}

func (s *Store) commitControl(ctx context.Context, c *controlReplay, write func(context.Context) error, out Action) error {
	err := s.database().WriteTransaction(ctx, func(ctx context.Context, _ *resourceaccess.Tx) error {
		// Acquire the writer before reading this key, avoiding read-upgrade races
		// between tabs/processes that admitted the same immutable request.
		if _, e := s.database().ExecContext(ctx, `UPDATE idempotency_replays SET request_hash=request_hash WHERE scope=? AND actor_id=? AND request_key=?`, c.scope, c.actor, c.key); e != nil {
			return e
		}

		if _, found, e := s.readControl(ctx, c); e != nil {
			return e
		} else if found {
			return ErrConflict
		}
		var a Action
		var d Decision
		if e := s.get(ctx, "action", c.action.ID, &a); e != nil {
			return e
		}
		if e := s.get(ctx, "decision", c.decision.ID, &d); e != nil {
			return e
		}
		// The whole approval/action pair is fenced, including a concurrently
		// superseded decision. These are bounded primary-key reads in this writer.
		if a.Revision != c.revision || d.Revision != c.decision.Revision || d.ActionID != a.ID || a.ActorID != c.actor || d.ActorID != c.actor {
			return ErrConflict
		}
		if e := write(ctx); e != nil {
			return e
		}
		raw, e := json.Marshal(out)
		if e != nil {
			return e
		}
		_, e = s.database().ExecContext(ctx, `INSERT INTO idempotency_replays(scope,actor_id,request_key,request_hash,response_status,response_json) VALUES(?,?,?,?,200,?)`, c.scope, c.actor, c.key, c.hash, string(raw))
		var constraint *sqlite.Error
		if errors.As(e, &constraint) && (constraint.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE || constraint.Code() == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY) {
			return ErrConflict
		}

		return e
	})
	if err == nil {
		raw, _ := json.Marshal(out)
		var frozen Action
		_ = json.Unmarshal(raw, &frozen)
		c.receipt = &frozen
	}
	return err
}
