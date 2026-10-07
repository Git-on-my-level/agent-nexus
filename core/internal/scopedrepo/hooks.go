package scopedrepo

import (
	"context"
	"database/sql"
	"sync"

	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/scopes"
)

// MutationTx is restricted to the trusted repository hook implementations. It
// is NOT a business computation capability. It preserves resourceaccess policy
// and exposes neither commit/rollback nor the underlying connection/factory.
type MutationTx interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// CanonicalHook is the constructor-injection contract for A-owned adapters of
// B/C's reviewed templates. Canonical writers supply metadata from source rows;
// neither a handler nor author input may construct provenance or ready flags.
type CanonicalHook interface {
	ApplyCanonical(context.Context, MutationTx, scopes.CanonicalMutation) error
}

type mutationTx struct {
	mu         sync.Mutex
	tx         *resourceaccess.Tx
	alive      bool
	ctx        context.Context
	remaining  *int
	rows       []*sql.Rows
	cancels    []context.CancelFunc
	failure    error
	registered bool
}

// The source context is pinned: hooks cannot drop its authority by supplying a
// fresh context. Supplied deadlines/cancellation can shorten the operation,
// while all values and authority remain pinned to the canonical source context.
func (t *mutationTx) check(ctx context.Context) error {
	if !t.alive {
		return scopes.ErrClosed
	}
	if t.failure != nil {
		return t.failure
	}
	if err := ctx.Err(); err != nil {
		t.failure = err
		return err
	}
	if *t.remaining <= 0 {
		t.failure = scopes.ErrBudget
		return t.failure
	}
	*t.remaining--
	return nil
}
func (t *mutationTx) operationContext(caller context.Context) (context.Context, context.CancelFunc) {
	var ctx context.Context
	var cancel context.CancelFunc
	if deadline, ok := caller.Deadline(); ok {
		ctx, cancel = context.WithDeadline(t.ctx, deadline)
	} else {
		ctx, cancel = context.WithCancel(t.ctx)
	}
	stop := context.AfterFunc(caller, cancel)
	if caller.Err() != nil {
		cancel()
	}
	return ctx, func() { stop(); cancel() }
}
func (t *mutationTx) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.check(ctx); err != nil {
		return nil, err
	}
	if t.registered && !registeredHookWrite(q) {
		t.failure = scopes.ErrDenied
		return nil, t.failure
	}
	opctx, cancel := t.operationContext(ctx)
	defer cancel()
	result, err := t.tx.ExecContext(opctx, q, args...)
	if err != nil {
		t.failure = err
	}
	return result, err
}
func (t *mutationTx) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.check(ctx); err != nil {
		return nil, err
	}
	if t.registered && q != readModelHookIdentity {
		t.failure = scopes.ErrDenied
		return nil, t.failure
	}
	opctx, cancel := t.operationContext(ctx)
	rows, err := t.tx.QueryContext(opctx, q, args...)
	if err != nil {
		cancel()
		t.failure = err
	} else {
		t.cancels = append(t.cancels, cancel)
		t.rows = append(t.rows, rows)
	}
	return rows, err
}
func (t *mutationTx) close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.alive = false
	for _, rows := range t.rows {
		if err := rows.Close(); err != nil && t.failure == nil {
			t.failure = err
		}
		if err := rows.Err(); err != nil && t.failure == nil {
			t.failure = err
		}
	}
	for _, cancel := range t.cancels {
		cancel()
	}
	return t.failure
}

// ApplyCanonicalHooks runs at most four trusted hooks in the EXISTING source
// transaction; orchestration never opens or commits a transaction. Hooks MUST
// issue only reviewed transaction-preserving single statements: no transaction
// control, compound SQL or schema changes. Under that trusted-template contract,
// any failure (including panic) rolls back the source transaction even if its
// caller ignores an error. The proxy caps SQL calls, not arbitrary SQL work.
// Success leaves commit with the canonical writer. Production consumption is
// still forbidden by TestFoundationNotServing until capture completeness,
// derivation isolation, parity and route gates pass. This validates structure,
// not authority or provenance; canonical capture remains required at call sites.
func ApplyCanonicalHooks(ctx context.Context, tx *resourceaccess.Tx, mutation scopes.CanonicalMutation, hooks ...CanonicalHook) (err error) {
	return applyCanonicalHooks(ctx, tx, mutation, false, hooks...)
}

func applyCanonicalHooks(ctx context.Context, tx *resourceaccess.Tx, mutation scopes.CanonicalMutation, registered bool, hooks ...CanonicalHook) (err error) {
	if tx == nil {
		return scopes.ErrDenied
	}
	ok := false
	defer func() {
		if !ok {
			_ = tx.Rollback()
		}
	}()
	if len(hooks) < 1 || len(hooks) > 4 {
		return scopes.ErrBudget
	}
	if err = mutation.Validate(); err != nil {
		return err
	}
	for _, h := range hooks {
		if h == nil {
			return scopes.ErrBudget
		}
		if registered {
			// Exact value types only. Embedding, wrappers and pointers cannot add
			// callbacks or mutate registration after this admission check.
			if _, approved := h.(ReadModelCanonicalHook); !approved {
				return scopes.ErrDenied
			}
		}
	}
	remaining := scopes.MaxComputationOps
	for _, h := range hooks {
		// Detach descriptors so one adapter cannot rewrite another's input.
		m := mutation
		m.Changes = append([]scopes.Change(nil), mutation.Changes...)
		for i := range m.Changes {
			if p := m.Changes[i].Before; p != nil {
				v := *p
				m.Changes[i].Before = &v
			}
			if p := m.Changes[i].After; p != nil {
				v := *p
				m.Changes[i].After = &v
			}
		}
		cap := &mutationTx{tx: tx, alive: true, ctx: ctx, remaining: &remaining, registered: registered}
		err = func() (hookErr error) {
			defer func() {
				if e := cap.close(); hookErr == nil {
					hookErr = e
				}
			}()
			return h.ApplyCanonical(ctx, cap, m)
		}()
		if err != nil {
			return err
		}
	}
	ok = true
	return nil
}
