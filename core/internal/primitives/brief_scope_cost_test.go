package primitives

import (
	"context"
	"database/sql"
	"testing"

	"agent-nexus-core/internal/resourceaccess"
)

func TestBriefPinnedSQLPreparationAllocationBudget(t *testing.T) {
	// No database I/O: isolate authorization SQL preparation for a pinned brief
	// read, including the duplicate preflight previously built then discarded.
	db := new(sql.DB)
	scope := AccessScope{ActorID: "brief-reader"}
	snapshot := &denialSnapshot{epoch: 1, rows: "[]"}
	ctx := canonicalRequestScope(context.Background(), scope)
	ctx = context.WithValue(ctx, pinnedDenialKey{}, pinnedDenial{db: db, scope: scope, snapshot: snapshot})
	policy, _ := resourceaccess.PolicyFrom(ctx)
	query := briefAskSelect + ` WHERE a.state='open' ORDER BY a.created_at LIMIT ?`
	var got string
	// Relative to the old double-build path, so compiler/runtime versions do
	// not turn a deterministic allocation regression into a timing test.
	legacyAllocs := testing.AllocsPerRun(20, func() {
		got = scopeReadSnapshot(ctx, query, snapshot, true)
		if accessCTEs(scope, query) == "" {
			t.Fatal("missing scoped relation")
		}
	})
	allocs := testing.AllocsPerRun(20, func() {
		var args []any
		got, args = policy.ReadOnDB(ctx, db, query, []any{60})
		if len(args) != 2 || args[0] != "[]" || args[1] != 60 {
			t.Fatal(args)
		}
	})
	if want := scopeReadSnapshot(ctx, query, snapshot, true); got != want {
		t.Fatal("scoped SQL changed")
	}
	t.Logf("pinned brief SQL allocations=%.0f legacy=%.0f", allocs, legacyAllocs)
	if allocs > legacyAllocs*0.75 {
		t.Fatalf("pinned SQL preparation still duplicates the rewrite: %.0f / %.0f allocations", allocs, legacyAllocs)
	}
}

func TestPinnedSQLPreparationRetainsBindingShape(t *testing.T) {
	db := new(sql.DB)
	scope := AccessScope{ActorID: "brief-reader"}
	snapshot := &denialSnapshot{epoch: 1, rows: "[]", pointToken: 7}
	ctx := canonicalRequestScope(context.Background(), scope)
	ctx = context.WithValue(ctx, pinnedDenialKey{}, pinnedDenial{db: db, scope: scope, snapshot: snapshot})
	ctx = context.WithValue(ctx, summaryPointReadKey{}, true)
	policy, _ := resourceaccess.PolicyFrom(ctx)
	for _, query := range []string{"SELECT ?", "WITH value(x) AS (SELECT ?) SELECT x FROM value"} {
		got, args := policy.ReadOnDB(ctx, db, query, []any{60})
		if got != query || len(args) != 1 || args[0] != 60 {
			t.Fatalf("identity query binding changed: %s %v", got, args)
		}
	}
	_, args := policy.ReadOnDB(ctx, db, "SELECT id FROM cards WHERE id=?", []any{"card-id"})
	if len(args) != 2 || args[0] != int64(7) || args[1] != "card-id" {
		t.Fatalf("point token binding changed: %v", args)
	}
}
