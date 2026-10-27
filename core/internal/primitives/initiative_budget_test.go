package primitives_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

func TestInitiativeReadsBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("native scale fixture")
	}
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	b, err := s.CreateBoard(ctx, "writer", map[string]any{"title": "Initiatives", "role": "initiatives", "initiative_target": 2})
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.CreateWork(ctx, "writer", b["id"].(string), map[string]any{"title": "Launch"})
	if err != nil {
		t.Fatal(err)
	}
	// 4k unrelated rows must never enter the bounded read window.
	_, err = ws.DB().Exec(`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<4296) INSERT INTO events(id,type,ts,actor_id,refs_json,payload_json) SELECT 'initiative-scale-'||x,CASE WHEN x<=200 THEN 'initiative_promoted' ELSE 'message_posted' END,'2026-10-25T00:00:00Z','writer',json_array(?),json_object('payload',json_object('subject_ref',?,'title','Launch','reason','Scale','before',json_object(),'after',json_object())) FROM n`, c["ref"], c["ref"])
	if err != nil {
		t.Fatal(err)
	}
	db, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	reader := primitives.NewTestStore(db, ws.Layout().ArtifactContentDir)
	var budgets map[string]struct {
		Queries int    `json:"max_queries"`
		Rows    int    `json:"max_rows"`
		VM      uint64 `json:"max_vm_steps"`
		Scans   uint64 `json:"max_full_scan_steps"`
	}
	raw, err := os.ReadFile("testdata/work_grouping_budget.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &budgets); err != nil {
		t.Fatal(err)
	}
	for _, scoped := range []bool{false, true} {
		readCtx := ctx
		if scoped {
			readCtx = primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "reader"})
		}
		for _, name := range []string{"initiative_changes", "initiative_target"} {
			capture.Start()
			if name == "initiative_changes" {
				_, err = reader.InitiativeChanges(readCtx, 50)
			} else {
				_, err = reader.InitiativeBudget(readCtx)
			}
			_, queries, rows := capture.Stop()
			work := capture.Work()
			budget := budgets[name]
			t.Logf("%s scoped=%v queries=%d rows=%d work=%+v", name, scoped, queries, rows, work)
			if err != nil || capture.WorkError() != nil || queries > budget.Queries || rows > budget.Rows || work.VMSteps > budget.VM || work.FullScanSteps > budget.Scans {
				t.Fatalf("%s scoped=%v err=%v capture=%v queries=%d rows=%d work=%+v", name, scoped, err, capture.WorkError(), queries, rows, work)
			}
		}
	}
}
