package primitives

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"

	"agent-nexus-core/internal/resourceaccess"
)

type responseMutationReadKey struct{}
type responseMutationProof struct {
	mu       sync.Mutex
	closed   bool
	tx       *sql.Tx
	scope    AccessScope
	snapshot *denialSnapshot
}

// Answer transactions refresh canonical denial evidence on the transaction's
// current epoch before every operation. This removes recursive SQL preparation
// from each small point/body check; it never reuses the request's pre-write
// closure. The consuming statement retains an epoch guard and fails closed on
// any same-transaction race. Ordinary mutations keep their original full policy.
// Admission is local to the serial append; it must never escape that operation
// or survive a savepoint rollback followed by continued writes (epochs can repeat).
func withResponseMutationReads(ctx context.Context) (context.Context, func()) {
	policy, ok := resourceaccess.PolicyFrom(ctx)
	if !ok {
		return ctx, func() {}
	}
	state := &responseMutationProof{}
	ctx = context.WithValue(ctx, responseMutationReadKey{}, state)
	policy.TransactionRead = func(ctx context.Context, q resourceaccess.QueryRower, query string, args []any) (string, []any) {
		if scoped, bound, ok := responseMutationRead(ctx, q, query, args); ok {
			return scoped, bound
		}
		return resourceaccess.ReadQuery(ctx, query), args
	}
	ctx = resourceaccess.WithPolicy(ctx, policy)
	return ctx, func() { state.mu.Lock(); state.closed = true; state.snapshot = nil; state.tx = nil; state.mu.Unlock() }
}

func responseMutationRead(ctx context.Context, q resourceaccess.QueryRower, query string, args []any) (string, []any, bool) {
	state, ok := ctx.Value(responseMutationReadKey{}).(*responseMutationProof)
	scope, scoped := accessScopeFrom(ctx)
	tx, inTransaction := q.(*sql.Tx)
	if !ok || !scoped || !inTransaction || !resourceaccess.AnonymousSQLParameters(query) {
		return query, args, false
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.closed {
		return query, args, false
	}
	var epoch int64
	if err := tx.QueryRowContext(ctx, `SELECT version FROM resource_access_epoch WHERE singleton=1`).Scan(&epoch); err != nil {
		return query, args, false
	}
	snapshot := state.snapshot
	if snapshot == nil || state.tx != tx || state.scope != scope || snapshot.epoch != epoch {
		snapshot = &denialSnapshot{epoch: epoch, mutation: true}
		proof := `WITH RECURSIVE ` + ownershipClosure("_anx_denied", deniedRootSQL(scope), false) + ` SELECT COALESCE((SELECT json_group_array(json_array(kind,id)) FROM _anx_denied),'[]')`
		if err := tx.QueryRowContext(ctx, proof).Scan(&snapshot.rows); err != nil {
			return query, args, false
		}
		state.tx, state.scope, state.snapshot = tx, scope, snapshot
	}
	if accessCTEs(scope, query) == "" {
		return scopeRead(ctx, query), args, true
	}
	// A value-check SELECT must deny on epoch mismatch, independently of the
	// relation shadows. Keeping the complete canonical alias/prose policy here
	// preserves arbitrary legacy and structured reference semantics.
	if stringsValueCheck(query) {
		query = `SELECT CASE WHEN COALESCE((SELECT version FROM main.resource_access_epoch WHERE singleton=1),-1)=` + fmt.Sprint(epoch) + ` THEN (` + query + `) ELSE 1 END`
	} else {
		// Guard the complete result, including relation shadows built outside
		// add(). An empty pinned denial relation must never grant a read.
		query = `SELECT * FROM (` + query + `) WHERE COALESCE((SELECT version FROM main.resource_access_epoch WHERE singleton=1),-1)=` + fmt.Sprint(epoch)
	}
	return scopeReadSnapshot(ctx, query, snapshot, true), append([]any{snapshot.rows}, args...), true
}

func stringsValueCheck(query string) bool {
	// Only the shared argument validator reads these internal relations.
	return strings.Contains(query, "_anx_denied")
}
