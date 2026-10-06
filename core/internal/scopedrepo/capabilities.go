package scopedrepo

import (
	"context"
	"database/sql"
	"errors"
	"sync"

	"agent-nexus-core/internal/scopes"
)

// Reader is render-only. Production business code must not combine it with a
// Writer/factory; replacing the foundation import gate requires that analyzer.
type Reader interface {
	CoveredScopes() ([]scopes.ID, error)
	ResourceIdentity(scope scopes.ID, kind, resourceID string) (scopes.ResourceIdentity, error)
	DocumentTitle(scope scopes.ID, resourceID string) (string, error)
}

// Writer exposes opaque derived values within one scope. It has no query method,
// raw database access or plaintext getter. Computation implements this interface.
type Writer interface {
	DocumentTitle(resourceID string) (Derived, error)
	Constant(string) (Derived, error)
	Persist(destination scopes.ID, key string, value Derived) error
}

type reader struct {
	identity func(scopes.ID, string, string) (scopes.ResourceIdentity, error)
	covered  func() ([]scopes.ID, error)
	title    func(scopes.ID, string) (string, error)
}

func (r *reader) CoveredScopes() ([]scopes.ID, error)                      { return r.covered() }
func (r *reader) DocumentTitle(scope scopes.ID, id string) (string, error) { return r.title(scope, id) }

func (r *reader) ResourceIdentity(scope scopes.ID, kind, id string) (scopes.ResourceIdentity, error) {
	return r.identity(scope, kind, id)
}

// Read authorizes the complete selection before executing a render callback.
// Callbacks get a capability, never the Store or a reusable authorization result.
func (s *Store) Read(ctx context.Context, request scopes.RequestSelection, fn func(Reader) error) error {
	if len(request.ScopeIDs) == 0 || len(request.ScopeIDs) > scopes.MaxScopes || fn == nil {
		return scopes.ErrBudget
	}
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return e
	}
	defer tx.Rollback()
	ids := append([]scopes.ID(nil), request.ScopeIDs...)
	selected := map[scopes.ID]bool{}
	for _, id := range ids {
		if selected[id] {
			return scopes.ErrBudget
		}
		selected[id] = true
		if _, e = role(ctx, tx, request.Principal, id); e != nil {
			return e
		}
	}
	var mu sync.Mutex
	alive := true
	ops := 0
	r := &reader{}
	r.covered = func() ([]scopes.ID, error) {
		mu.Lock()
		defer mu.Unlock()
		if !alive {
			return nil, scopes.ErrClosed
		}
		return append([]scopes.ID(nil), ids...), nil
	}
	r.title = func(scope scopes.ID, id string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if !alive {
			return "", scopes.ErrClosed
		}
		if !selected[scope] {
			return "", scopes.ErrDenied
		}
		ops++
		if ops > scopes.MaxComputationOps {
			return "", scopes.ErrBudget
		}
		var title sql.NullString
		e := tx.QueryRowContext(ctx, query_title, scope, id).Scan(&title)
		if errors.Is(e, sql.ErrNoRows) {
			return "", scopes.ErrDenied
		}
		if e != nil {
			return "", e
		}
		if !title.Valid {
			return "", scopes.ErrBudget
		}
		return title.String, nil
	}
	r.identity = func(scope scopes.ID, kind, id string) (scopes.ResourceIdentity, error) {
		mu.Lock()
		defer mu.Unlock()
		out := scopes.ResourceIdentity{}
		if !alive {
			return out, scopes.ErrClosed
		}
		if !selected[scope] {
			return out, scopes.ErrDenied
		}
		ops++
		if ops > scopes.MaxComputationOps || len(kind) > 32 || len(id) > 512 {
			return out, scopes.ErrBudget
		}
		e := tx.QueryRowContext(ctx, query_identity, scope, kind, id).Scan(&out.ScopeID, &out.Kind, &out.ResourceID, &out.RID, &out.CanonicalID, &out.CanonicalVersion)
		if errors.Is(e, sql.ErrNoRows) {
			return scopes.ResourceIdentity{}, scopes.ErrDenied
		}
		return out, e
	}
	closeCap := func() { mu.Lock(); alive = false; mu.Unlock() }
	defer closeCap()
	if e = fn(r); e != nil {
		return e
	}
	closeCap()
	return tx.Commit()
}

// Write deliberately accepts exactly one scope even if the principal can read
// and write many. Cross-scope publication is a separate future operation.
func (s *Store) Write(ctx context.Context, request scopes.RequestSelection, fn func(Writer) error) error {
	if len(request.ScopeIDs) != 1 || fn == nil {
		return scopes.ErrBudget
	}
	return s.Compute(ctx, request.Principal, request.ScopeIDs[0], func(c *Computation) error { return fn(c) })
}
