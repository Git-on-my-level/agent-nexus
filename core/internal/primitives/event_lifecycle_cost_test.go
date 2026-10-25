package primitives_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

func TestEventLifecycleBoundedCascadePlanAndCost(t *testing.T) {
	if testing.Short() {
		t.Skip("synthetic lifecycle scale fixture")
	}
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	topic, err := s.CreateTopic(ctx, "owner", map[string]any{"title": "Lifecycle cost", "summary": "Synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	thread := fmt.Sprint(topic.Topic["thread_id"])
	if _, err = s.PatchThread(ctx, "owner", thread, map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	root, err := s.AppendEvent(ctx, "owner", map[string]any{"id": "cascade-root", "thread_id": thread, "type": "message_posted", "refs": []string{}, "payload": map[string]any{"text": "Parent"}})
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the actual rewritten scoped consuming query, not a hand-copied SQL.
	pool, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	measured := primitives.NewTestStore(pool, ws.Layout().ArtifactContentDir)
	scope := func() context.Context {
		return primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "owner"})
	}
	for _, corpus := range []int{0, primitivesCorrectnessCorpusRows()} {
		if corpus != 0 {
			if err = perfguard.SeedRows(ctx, ws.DB(), ws.Layout().ArtifactContentDir, "owner", time.Now().UTC(), corpus); err != nil {
				t.Fatal(err)
			}
		}
		// Warm only access admission; every measured action still gets a fresh request.
		if _, err = measured.GetEvent(scope(), fmt.Sprint(root["id"])); err != nil {
			t.Fatal(err)
		}
		capture.Start()
		_, affected, e := measured.ApplyEventLifecycle(scope(), "owner", "cascade-root", "archive", "", &primitives.EventLifecycleBase{})
		statements, calls, rows := capture.Stop()
		if e != nil || len(affected) != 1 {
			t.Fatal(affected, e)
		}
		if e = capture.WorkError(); e != nil {
			t.Fatal(e)
		}
		work := capture.Work()
		t.Logf("corpus=%d calls=%d rows=%d VM=%d fullscan=%d", corpus, calls, rows, work.VMSteps, work.FullScanSteps)
		if calls > 20 || rows > 32 || work.VMSteps > 50000 || work.FullScanSteps > 128 {
			t.Fatalf("unbounded lifecycle read: %+v", work)
		}
		graphPlans := 0
		for _, statement := range statements {
			if !strings.Contains(statement.SQL, "descendants(id, overflow)") {
				continue
			}
			graphPlans++
			plan, e := perfguard.Explain(ctx, ws.DB(), statement)
			if e != nil {
				t.Fatal(e)
			}
			joined := strings.Join(plan, "\n")
			// Access scoping adds its own indexed point proofs to this plan.
			if !strings.Contains(joined, "idx_ref_edges_work_placement") && !strings.Contains(joined, "idx_ref_edges_target") {
				t.Fatal("missing target index", joined)
			}
			for _, line := range plan {
				if strings.Contains(line, "SCAN events") || strings.Contains(line, "SCAN ref_edges") || strings.Contains(line, "SCAN main.events") || strings.Contains(line, "SCAN main.ref_edges") {
					t.Fatal("unbounded graph table scan", line)
				}
			}
		}
		if graphPlans != 1 {
			t.Fatal("expected one cascade read", graphPlans)
		}
		if _, err = s.UnarchiveEvent(ctx, "owner", "cascade-root"); err != nil {
			t.Fatal(err)
		}
	}
	// Cycles are visited once. Reply lifecycle changes preserve original payload.
	child, err := s.AppendEvent(ctx, "owner", map[string]any{"id": "cascade-child", "thread_id": thread, "type": "message_posted", "refs": []string{"event:cascade-root"}, "payload": map[string]any{"text": "Child"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`INSERT INTO ref_edges(id,source_type,source_id,target_type,target_id,edge_type,created_at) VALUES('cycle','event','cascade-root','event','cascade-child','ref','now')`); err != nil {
		t.Fatal(err)
	}
	_, affected, err := s.ApplyEventLifecycle(ctx, "owner", "cascade-root", "archive", "", &primitives.EventLifecycleBase{})
	if err != nil || len(affected) != 2 {
		t.Fatal(affected, err)
	}
	current, err := s.GetEvent(ctx, "cascade-child")
	if err != nil || fmt.Sprint(current["payload"]) != fmt.Sprint(child["payload"]) {
		t.Fatal("payload changed", current, err)
	}
	if _, err = s.UnarchiveEvent(ctx, "owner", "cascade-root"); err != nil {
		t.Fatal(err)
	}
	// Large raw fanout must fail before materializing all indexed candidates,
	// even when many candidates are unrelated non-message events.
	private, err := s.CreateTopic(ctx, "stranger", map[string]any{"title": "Hidden sources", "summary": "Synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	privateThread := fmt.Sprint(private.Topic["thread_id"])
	if _, err = s.PatchThread(ctx, "stranger", privateThread, map[string]any{"pm_actor_id": "stranger"}, nil); err != nil {
		t.Fatal(err)
	}
	var previous uint64
	for _, fanout := range []int{3000, primitivesFixtureRows(6000, 30000)} {
		tx, e := ws.DB().BeginTx(ctx, nil)
		if e != nil {
			t.Fatal(e)
		}
		// Retain every source/edge and its canonical trigger work. Both tiers
		// exceed the 2,001-candidate limit at each phase; performance uses 33k.
		// Two fixture statements avoid recompiling the trigger programs for
		// each imported row; this test measures the bounded consuming read.
		if _, e = tx.Exec(`WITH RECURSIVE fixture(n) AS (SELECT 0 UNION ALL SELECT n+1 FROM fixture WHERE n+1<?)
		 INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json)
		 SELECT 'fanout-'||?||'-'||n,'message_posted','2026-01-01T00:00:00Z','stranger',?,'[]','{}' FROM fixture`, fanout, fanout, privateThread); e != nil {
			t.Fatal(e)
		}
		if _, e = tx.Exec(`WITH RECURSIVE fixture(n) AS (SELECT 0 UNION ALL SELECT n+1 FROM fixture WHERE n+1<?)
		 INSERT INTO ref_edges(id,source_type,source_id,target_type,target_id,edge_type,created_at)
		 SELECT 'fanout-'||?||'-'||n,'event','fanout-'||?||'-'||n,'event','cascade-root','ref','now' FROM fixture`, fanout, fanout, fanout); e != nil {
			t.Fatal(e)
		}
		if e = tx.Commit(); e != nil {
			t.Fatal(e)
		}
		request := scope()
		// The HTTP handler performs this current scoped parent read before the
		// keyed mutation. The new cascade consuming query must remain bounded
		// independently of inherited canonical business-write validation.
		if _, e = measured.GetEvent(request, "cascade-root"); e != nil {
			t.Fatal(e)
		}
		capture.Start()
		_, e = primitives.EventDescendantsForTest(measured, request, thread, "cascade-root")
		_, calls, rows := capture.Stop()
		work := capture.Work()
		if !errors.Is(e, primitives.ErrConflict) {
			t.Fatal("expected cap conflict", e)
		}
		if e = capture.WorkError(); e != nil {
			t.Fatal(e)
		}
		t.Logf("fanout=%d calls=%d rows=%d VM=%d fullscan=%d", fanout, calls, rows, work.VMSteps, work.FullScanSteps)
		if work.VMSteps > 250000 || work.FullScanSteps > 128 || (previous > 0 && work.VMSteps > previous*2) {
			t.Fatal("fanout increased bounded lookup work", work)
		}
		previous = work.VMSteps
		if _, _, e = s.ApplyEventLifecycle(ctx, "owner", "cascade-root", "archive", "", &primitives.EventLifecycleBase{}); !errors.Is(e, primitives.ErrConflict) {
			t.Fatal("cap must abort the writer", e)
		}
		current, e := s.GetEvent(ctx, "cascade-root")
		if e != nil || current["archived_at"] != nil {
			t.Fatal("cap partially committed", current, e)
		}
	}
}
