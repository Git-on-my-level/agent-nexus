package primitives

import (
	"agent-nexus-core/internal/resourceaccess"
	"context"
	"database/sql"
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

type denialCacheKey struct {
	db    *sql.DB
	scope AccessScope
	epoch int64
}

// Hold at most 32 immutable closures and 8 MiB of encoded rows, evicting oldest
// insertions. DB identity separates
// workspaces even when their epochs and principal names happen to coincide.
var readDenials = struct {
	sync.Mutex
	keys  []denialCacheKey
	rows  map[denialCacheKey]*denialSnapshot
	bytes int
}{rows: make(map[denialCacheKey]*denialSnapshot)}

func cachedReadDenial(db *sql.DB, scope AccessScope, epoch int64) *denialSnapshot {
	readDenials.Lock()
	defer readDenials.Unlock()
	return readDenials.rows[denialCacheKey{db, scope, epoch}]
}

func rememberReadDenial(db *sql.DB, scope AccessScope, snapshot *denialSnapshot) {
	if db == nil || len(snapshot.rows) > 8<<20 {
		return
	}
	key := denialCacheKey{db, scope, snapshot.epoch}
	readDenials.Lock()
	defer readDenials.Unlock()
	if readDenials.rows[key] != nil {
		return
	}
	for len(readDenials.keys) >= 32 || readDenials.bytes+len(snapshot.rows) > 8<<20 {
		old := readDenials.keys[0]
		readDenials.keys = readDenials.keys[1:]
		readDenials.bytes -= len(readDenials.rows[old].rows)
		delete(readDenials.rows, old)
	}
	readDenials.keys = append(readDenials.keys, key)
	readDenials.rows[key] = snapshot
	readDenials.bytes += len(snapshot.rows)
}

// WithRequestAccessScope caches the denial closure for a regular read request.
// Every consuming statement validates the authorization epoch IN its own SQL
// snapshot and falls back to the canonical graph if any ownership write occurred.
// Business transactions, visit writes and streams evaluate the canonical graph
// directly. Shared read snapshots are bounded and isolated by DB, scope and epoch.
func WithRequestAccessScope(ctx context.Context, scope AccessScope) context.Context {
	ctx = WithAccessScope(ctx, scope)
	state := &denialRequestState{}
	ctx = context.WithValue(ctx, denialRequestKey{}, state)
	p, _ := resourceaccess.PolicyFrom(ctx)
	p.ReadOnDB = func(c context.Context, db resourceaccess.QueryRower, query string, args []any) (string, []any) {
		// Identity/service SQL with no resource relation needs no closure.
		if accessCTEs(scope, query) == "" {
			return scopeRead(c, query), args
		}
		state.Lock()
		if state.snapshot == nil {
			// Lookup only on ordinary DB reads. Transactions continue to use
			// their current graph (or the visit's existing request snapshot).
			raw, _ := db.(*sql.DB)
			var epoch int64
			if raw != nil && db.QueryRowContext(c, `SELECT version FROM main.resource_access_epoch WHERE singleton=1`).Scan(&epoch) == nil {
				state.snapshot = cachedReadDenial(raw, scope, epoch)
			}
			if state.snapshot == nil {
				candidate := &denialSnapshot{}
				sql := `WITH RECURSIVE ` + ownershipClosure("_anx_denied", deniedRootSQL(scope), false) + ` SELECT (SELECT version FROM main.resource_access_epoch WHERE singleton=1),COALESCE((SELECT json_group_array(json_array(kind,id)) FROM _anx_denied),'[]')`
				if err := db.QueryRowContext(c, sql).Scan(&candidate.epoch, &candidate.rows); err == nil {
					state.snapshot = candidate
					rememberReadDenial(raw, scope, candidate)
				}
			}
		}
		state.Unlock()
		if snapshot := denialSnapshotFrom(c); snapshot != nil && resourceaccess.AnonymousSQLParameters(query) {
			// The snapshot is data, never SQL syntax. Its one binding precedes
			// the statement's anonymous bindings, preserving their order.
			bound := make([]any, 0, len(args)+1)
			bound = append(bound, snapshot.rows)
			bound = append(bound, args...)
			return scopeReadWithSnapshot(c, query, snapshot), bound
		}
		return scopeRead(c, query), args
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
