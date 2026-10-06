package primitives

import (
	"agent-nexus-core/internal/resourceaccess"
	"context"
	"sync"
)

type denialSnapshot struct {
	epoch int64
	rows  string
}
type denialRequestState struct {
	sync.Mutex
	snapshot *denialSnapshot
}
type denialRequestKey struct{}

// WithRequestAccessScope caches the denial closure for a regular read request.
// Every consuming statement validates the authorization epoch IN its own SQL
// snapshot and falls back to the canonical graph if any ownership write occurred.
// Transactions and streams always evaluate the canonical graph directly.
func WithRequestAccessScope(ctx context.Context, scope AccessScope) context.Context {
	ctx = WithAccessScope(ctx, scope)
	state := &denialRequestState{}
	ctx = context.WithValue(ctx, denialRequestKey{}, state)
	p, _ := resourceaccess.PolicyFrom(ctx)
	p.ReadOnDB = func(c context.Context, db resourceaccess.QueryRower, query string) string {
		// Identity/service SQL with no resource relation needs no closure.
		if accessCTEs(scope, query) == "" {
			return scopeRead(c, query)
		}
		state.Lock()
		if state.snapshot == nil {
			candidate := &denialSnapshot{}
			sql := `WITH RECURSIVE ` + ownershipClosure("_anx_denied", deniedRootSQL(scope), false) + ` SELECT (SELECT version FROM main.resource_access_epoch WHERE singleton=1),COALESCE((SELECT json_group_array(json_array(kind,id)) FROM _anx_denied),'[]')`
			if err := db.QueryRowContext(c, sql).Scan(&candidate.epoch, &candidate.rows); err == nil {
				state.snapshot = candidate
			}
		}
		state.Unlock()
		return scopeRead(c, query)
	}
	return resourceaccess.WithPolicy(ctx, p)
}
func denialSnapshotFrom(ctx context.Context) *denialSnapshot {
	state, ok := ctx.Value(denialRequestKey{}).(*denialRequestState)
	if !ok {
		return nil
	}
	state.Lock()
	defer state.Unlock()
	return state.snapshot
}
