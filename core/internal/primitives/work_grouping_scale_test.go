package primitives_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

func TestGroupingReadsBoundedWithTenThousandUnrelatedCards(t *testing.T) {
	if testing.Short() {
		t.Skip("real 10k-card SQL scale fixture; run the focused scale gate without -short")
	}
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	p, err := s.CreateWork(ctx, "writer", "", map[string]any{"title": "P76 Parent"})
	if err != nil {
		t.Fatal(err)
	}
	// Native fixtures retain real canonical ownership/reference triggers.
	_, err = ws.DB().ExecContext(ctx, `WITH RECURSIVE n(value) AS (SELECT 1 UNION ALL SELECT value+1 FROM n WHERE value<10200)
	 INSERT INTO cards(id,handle,board_id,title,summary,column_key,created_at,created_by,updated_at,updated_by)
	 SELECT 'grouping-scale-'||value,'grouping-scale-'||value,?,'Scale card '||value,'','ready','2000-01-01T00:00:00Z','writer','2000-01-01T00:00:00Z','writer' FROM n`, p["board_id"])
	if err != nil {
		t.Fatal(err)
	}
	plan := plans.Plan{Steps: []plans.Step{}}
	for i := 1; i <= 200; i++ {
		plan.Steps = append(plan.Steps, plans.Step{ID: fmt.Sprintf("child-%d", i), Title: "Child", Ref: fmt.Sprintf("card:grouping-scale-%d", i)})
	}
	if err = s.SetCardPlan(ctx, "writer", p["id"].(string), p["updated_at"].(string), plan); err != nil {
		t.Fatal(err)
	}
	db, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	reader := primitives.NewTestStore(db, ws.Layout().ArtifactContentDir)
	raw, err := os.ReadFile("testdata/work_grouping_budget.json")
	if err != nil {
		t.Fatal(err)
	}
	var budgets map[string]struct {
		Queries int    `json:"max_queries"`
		Rows    int    `json:"max_rows"`
		VMSteps uint64 `json:"max_vm_steps"`
	}
	if err = json.Unmarshal(raw, &budgets); err != nil {
		t.Fatal(err)
	}
	for _, scoped := range []bool{false, true} {
		readCtx := ctx
		if scoped {
			readCtx = primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "reader"})
		}
		for _, name := range []string{"homes", "hierarchy"} {
			t.Run(fmt.Sprintf("%s/scoped=%v", name, scoped), func(t *testing.T) {
				capture.Start()
				if name == "homes" {
					_, _, err = reader.UngroupedWork(readCtx, 20)
				} else {
					err = reader.EnrichWorkHierarchy(readCtx, []map[string]any{{"id": p["id"]}}, "reader", true, time.Now())
				}
				statements, queries, rows := capture.Stop()
				work := capture.Work()
				b := budgets[name]
				if name == "homes" {
					for _, statement := range statements {
						if strings.Contains(statement.SQL, "ORDER BY created_at DESC,id DESC LIMIT 200") {
							if plan, e := perfguard.Explain(readCtx, ws.DB(), statement); e == nil {
								t.Logf("plan: %s", strings.Join(plan, "\n"))
							}
						}
					}
				}
				if err != nil || capture.WorkError() != nil || queries > b.Queries || rows > b.Rows || work.VMSteps > b.VMSteps {
					t.Fatalf("err=%v instrument=%v queries=%d rows=%d work=%+v", err, capture.WorkError(), queries, rows, work)
				}
				if !scoped && name == "homes" {
					found := false
					for _, stmt := range statements {
						if !strings.Contains(stmt.SQL, "ORDER BY created_at DESC,id DESC LIMIT 200") {
							continue
						}
						plan, e := perfguard.Explain(ctx, ws.DB(), stmt)
						if e != nil {
							t.Fatal(e)
						}
						found = strings.Contains(strings.Join(plan, "\n"), "idx_cards_grouping_recent")
					}
					if !found {
						t.Fatal("recent candidate admission did not use its ordered index")
					}
				}
				t.Logf("queries=%d rows=%d VM=%d full_scans=%d", queries, rows, work.VMSteps, work.FullScanSteps)
			})
		}
	}
}
