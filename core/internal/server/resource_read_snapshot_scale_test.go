package server

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/testutil/perfguard"
)

// Isolate the changed membership operation from the existing cold admission and
// reference-projection baselines. Growing the denied corpus cannot grow warm
// point-lookup VM work or materialize a temporary denial table.
func TestPerformanceResourceReadSnapshotBudget(t *testing.T) {
	requirePerformanceTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	raw := env.workspace.DB()
	db, capture, err := perfguard.Open("file:" + env.workspace.Layout().DatabasePath + "?_pragma=busy_timeout(20000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := primitives.NewTestStore(db, env.workspace.Layout().ArtifactContentDir)
	doc, _, err := store.CreateDocument(ctx, "owner", map[string]any{"title": "budget control"}, "body", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	scoped := resourceaccess.NewDB(db)
	previous := 0
	for _, size := range []int{0, 1024, 4096} {
		tx, err := raw.Begin()
		if err != nil {
			t.Fatal(err)
		}
		for i := previous; i < size; i++ {
			if _, err = tx.Exec(`INSERT INTO resource_access_tombstones(kind,id,ref,owner) VALUES('document',?,?,'other')`, fmt.Sprintf("denied-%d", i), fmt.Sprintf("denied-%d", i)); err != nil {
				_ = tx.Rollback()
				t.Fatal(err)
			}
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		previous = size
		pin, closeRead, err := store.BeginResourceRead(primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "reader"}))
		if err != nil {
			t.Fatal(err)
		}
		capture.Start()
		var id string
		err = scoped.QueryRowContext(pin, `SELECT id FROM documents WHERE id=?`, doc["id"]).Scan(&id)
		statements, queries, rows := capture.Stop()
		work := capture.Work()
		closeRead()
		if err != nil || id != doc["id"] || capture.WorkError() != nil {
			t.Fatalf("read=%q err=%v instrumentation=%v", id, err, capture.WorkError())
		}
		if queries != 1 || rows != 1 || work.VMSteps > 64 || work.FullScanSteps != 0 || work.Sorts != 0 || work.AutoIndexRows != 0 {
			t.Fatalf("size=%d queries=%d rows=%d work=%+v", size, queries, rows, work)
		}
		for _, stmt := range statements {
			if !strings.Contains(stmt.SQL, "anx_point_snapshot_denied") || strings.Contains(stmt.SQL, "json_each") {
				t.Fatal("full denial materialization returned")
			}
			plan, err := perfguard.Explain(ctx, raw, stmt)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(strings.Join(plan, "\n"), "SEARCH _row USING COVERING INDEX sqlite_autoindex_documents_1 (id=?)") {
				t.Fatalf("lost indexed document lookup: %v", plan)
			}
			t.Logf("denied=%d queries=%d rows=%d work=%+v plan=%v", size, queries, rows, work, plan)
		}
	}
}
