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

// The inherited reference-set expansion remains linear in denied identities.
// Placement membership must use indexed equality probes, not scan that set for
// each card in Overview's 100-card window.
func TestPerformanceOverviewReferenceMembership(t *testing.T) {
	requirePerformanceTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	raw := env.workspace.DB()
	db, capture, err := perfguard.Open("file:" + env.workspace.Layout().DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := primitives.NewTestStore(db, env.workspace.Layout().ArtifactContentDir)
	scoped := resourceaccess.NewDB(db)
	for i := 0; i < 100; i++ {
		if _, err := raw.Exec(`INSERT INTO ref_edges(id,source_type,source_id,target_type,target_id,edge_type,created_at,metadata_json) VALUES(?,'board','public','card',?,'board_card','now','{}')`, fmt.Sprintf("placement-%03d", i), fmt.Sprintf("public-%03d", i)); err != nil {
			t.Fatal(err)
		}
	}
	previous := 0
	for _, size := range []int{1024, 4096} {
		tx, err := raw.Begin()
		if err != nil {
			t.Fatal(err)
		}
		for i := previous; i < size; i++ {
			if _, err := tx.Exec(`INSERT INTO resource_access_tombstones(kind,id,ref,owner) VALUES('document',?,?,'other')`, fmt.Sprintf("denied-%d", i), fmt.Sprintf("denied-%d", i)); err != nil {
				tx.Rollback()
				t.Fatal(err)
			}
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		previous = size
		pin, closeRead, err := store.BeginResourceRead(primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "reader"}))
		if err != nil {
			t.Fatal(err)
		}
		capture.Start()
		rows, err := scoped.QueryContext(pin, `SELECT id FROM ref_edges WHERE source_type='board' AND source_id=? AND target_type='card' AND edge_type='board_card' ORDER BY id LIMIT 101`, "public")
		if err != nil {
			closeRead()
			t.Fatal(err)
		}
		count := 0
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			count++
		}
		err = rows.Err()
		rows.Close()
		statements, queries, returned := capture.Stop()
		work := capture.Work()
		closeRead()
		if err != nil || capture.WorkError() != nil || count != 100 || queries != 1 || returned != 100 {
			t.Fatalf("count=%d queries=%d rows=%d err=%v instrumentation=%v", count, queries, returned, err, capture.WorkError())
		}
		// Covers one reference-set construction plus 200 membership probes. The
		// former correlated OR exceeds this ceiling by orders of magnitude.
		if work.VMSteps > uint64(size*160+10000) {
			t.Fatalf("denied=%d unbounded placement membership: %+v", size, work)
		}
		for _, statement := range statements {
			plan, err := perfguard.Explain(ctx, raw, statement)
			if err != nil {
				t.Fatal(err)
			}
			text := strings.Join(plan, "\n")
			// The constant source key permits a partial automatic index; the
			// correlated target key uses a full one. Both are equality searches.
			text = strings.ReplaceAll(text, "AUTOMATIC PARTIAL COVERING INDEX", "AUTOMATIC COVERING INDEX")
			if strings.Count(text, "SEARCH _anx_denied_refs USING AUTOMATIC COVERING INDEX (ref=?)") != 2 {
				t.Fatalf("placement endpoints lost indexed membership: %s", text)
			}
			t.Logf("denied=%d work=%+v plan=%v", size, work, plan)
		}
	}
}
