package primitives_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

// This gate isolates the new maintenance reads from canonical mutation cost.
// Tombstoned history and unrelated history must not enlarge an admitted read.
func TestPerformanceWatcherHealthReads(t *testing.T) {
	if os.Getenv("ANX_PERFORMANCE_TEST") != "1" {
		t.Skip("set ANX_PERFORMANCE_TEST=1")
	}
	var budget struct {
		Queries, Rows int
		VM            uint64
		History       int
	}
	raw, err := os.ReadFile("testdata/watcher_health_budget.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &budget); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	writer := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	now := time.Now().UTC()
	w, err := writer.CreateWork(ctx, "writer", "", map[string]any{"title": "Loop", "cadence": map[string]any{"every_seconds": 60}})
	if err != nil {
		t.Fatal(err)
	}
	id := w["id"].(string)
	for i, outcome := range []string{"acted", "infra_error"} {
		event, err := writer.AppendEvent(ctx, "writer", map[string]any{"type": "run_recorded", "summary": "Receipt", "refs": []string{"card:" + id}, "payload": map[string]any{"run_id": outcome, "outcome": outcome}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = ws.DB().Exec(`UPDATE events SET ts=? WHERE id=?`, now.Add(time.Duration(i-1)*time.Minute).Format(time.RFC3339Nano), event["id"]); err != nil {
			t.Fatal(err)
		}
	}
	for _, history := range []bool{false, true} {
		if history {
			// Both unrelated and newer archived history exercise partial index admission.
			_, err = ws.DB().Exec(`WITH RECURSIVE n(i) AS(SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<?) INSERT INTO events(id,handle,type,ts,actor_id,refs_json,payload_json,archived_at) SELECT 'health-history-'||i,'health-history-'||i,'run_recorded',?,'writer','[]',json_object('subject_ref',CASE WHEN i%2=0 THEN ? ELSE 'card:unrelated' END,'payload',json_object('outcome','acted','run_id','history-'||i)),'2099-01-02T00:00:00Z' FROM n`, budget.History, now.Add(-30*time.Second).Format(time.RFC3339Nano), "card:"+id)
			if err != nil {
				t.Fatal(err)
			}
		}
		db, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath)
		if err != nil {
			t.Fatal(err)
		}
		s := primitives.NewTestStore(db, ws.Layout().ArtifactContentDir)
		capture.Start()
		runs, effect, err := s.WatcherLoopFactsForTest(ctx, id, now)
		stmts, queries, rows := capture.Stop()
		work := capture.Work()
		if err != nil || capture.WorkError() != nil {
			t.Fatal(err, capture.WorkError())
		}
		if runs != 2 || !effect || queries > budget.Queries || rows > budget.Rows || work.VMSteps > budget.VM || work.FullScanSteps != 0 {
			t.Fatalf("history=%v q=%d rows=%d work=%+v", history, queries, rows, work)
		}
		for _, stmt := range stmts {
			plan, err := perfguard.Explain(ctx, ws.DB(), stmt)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(strings.Join(plan, "\n"), "SCAN events") {
				t.Fatal("history scan", plan)
			}
		}
		t.Logf("history=%v queries=%d rows=%d VM=%d scans=%d", history, queries, rows, work.VMSteps, work.FullScanSteps)
		db.Close()
	}
}
