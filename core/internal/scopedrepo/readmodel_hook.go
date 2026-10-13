package scopedrepo

import (
	"context"
	"encoding/json"

	"agent-nexus-core/internal/readmodel"
	"agent-nexus-core/internal/scopes"
)

// Disabled integration foundation. Do not install before complete canonical capture,
// reviewed exact SQL templates, and generation projection coverage exist.
const readModelHookIdentity = `SELECT d.state,d.generation,r.version,r.canonical_id,k.rid
 FROM scope_domains d JOIN scope_resources r ON r.scope_id=d.id
 JOIN scope_resource_rids k ON k.scope_id=r.scope_id AND k.kind=r.kind AND k.resource_id=r.id
 WHERE d.id=? AND r.kind=? AND r.id=?`

type hookExecutor struct {
	tx      MutationTx
	failure error
}

func registeredHookWrite(query string) bool {
	switch query {
	case readmodel.InsertFeed, readmodel.DeleteFeed,
		readmodel.InsertPayload, readmodel.DeletePayload,
		readmodel.IncrementCounter, readmodel.DecrementCounter:
		return true
	default:
		return false
	}
}

func (e *hookExecutor) Exec(ctx context.Context, q string, args ...any) (int64, error) {
	if e.failure != nil {
		return 0, e.failure
	}
	// Exact constants only: no normalization, appended statements, or arbitrary SQL.
	switch q {
	case readmodel.InsertFeed, readmodel.DeleteFeed, readmodel.InsertPayload, readmodel.DeletePayload, readmodel.IncrementCounter, readmodel.DecrementCounter:
	default:
		e.failure = readmodel.ErrProjection
		return 0, e.failure
	}
	r, err := e.tx.ExecContext(ctx, q, args...)
	if err != nil {
		e.failure = err
		return 0, err
	}
	n, err := r.RowsAffected()
	if err != nil {
		e.failure = err
	}
	return n, err
}

// Candidate for A: identity comes from the private registry in the SAME source
// transaction. This check is necessary, but complete canonical before/after
// provenance and capture coverage must still be supplied by A's actual writers.
type ReadModelCanonicalHook struct{ Capture readmodel.Capture }

func (h ReadModelCanonicalHook) ApplyCanonical(ctx context.Context, tx MutationTx, m scopes.CanonicalMutation) error {
	if err := m.Validate(); err != nil {
		return err
	}
	i := m.Identity
	rows, err := tx.QueryContext(ctx, readModelHookIdentity, i.ScopeID, i.Kind, i.ResourceID)
	if err != nil {
		return err
	}
	var state, canonical string
	var generation, version, rid int64
	if !rows.Next() {
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		return readmodel.ErrProjection
	}
	if err = rows.Scan(&state, &generation, &version, &canonical, &rid); err != nil {
		rows.Close()
		return err
	}
	extra := rows.Next()
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if extra || state != "active" || generation < 1 || version != i.CanonicalVersion || canonical != i.CanonicalID || rid != i.RID {
		return readmodel.ErrProjection
	}
	old, next, payloads, err := readmodel.CaptureCanonical(m, generation, func(c scopes.Change, p scopes.Projection) (readmodel.Entry, json.RawMessage, error) {
		if h.Capture == nil {
			return readmodel.Entry{}, nil, readmodel.ErrProjection
		}
		if c.Before != nil {
			v := *c.Before
			c.Before = &v
		}
		if c.After != nil {
			v := *c.After
			c.After = &v
		}
		return h.Capture(c, p)
	})
	if err != nil {
		return err
	}
	// Never discard semantic affected-row errors: returning them is what makes
	// ApplyCanonicalHooks roll back the source, including earlier adapters.
	executor := &hookExecutor{tx: tx}
	return readmodel.ApplyProjection(ctx, executor, old, next, payloads, false)
}

var _ CanonicalHook = ReadModelCanonicalHook{}
