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

func TestWatchWorkstreamRollupBudget(t *testing.T) {
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
	root, err := s.CreateWork(ctx, "writer", "", map[string]any{"title": "Root"})
	if err != nil {
		t.Fatal(err)
	}
	// Ten streams with ten children each, plus unrelated cards and history.
	_, err = ws.DB().Exec(`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<4206)
 INSERT INTO cards(id,handle,board_id,title,summary,column_key,created_at,created_by,updated_at,updated_by)
 SELECT 'watch-budget-'||x,'watch-budget-'||x,?,'P76: measured','','done','2000-01-01','writer','2000-01-01','writer' FROM n`, root["board_id"])
	if err != nil {
		t.Fatal(err)
	}
	_, err = ws.DB().Exec(`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<4206)
 INSERT INTO events(id,type,ts,actor_id,payload_json)
 SELECT 'watch-terminal-'||x,'card_moved','2026-10-25T00:00:00Z','writer',json_object('payload',json_object('card_id','watch-budget-'||x,'column_key','done','from_column_key','ready')) FROM n`)
	if err != nil {
		t.Fatal(err)
	}
	// Streams must be open to own children; ordinary leaves stay done.
	if _, err = ws.DB().Exec(`UPDATE cards SET column_key='ready' WHERE id IN (SELECT 'watch-budget-'||value FROM json_each('[1,2,3,4,5,6,7,8,9,10]'))`); err != nil {
		t.Fatal(err)
	}
	p := plans.Plan{Steps: []plans.Step{}}
	for i := 1; i <= 10; i++ {
		id := fmt.Sprintf("watch-budget-%d", i)
		p.Steps = append(p.Steps, plans.Step{ID: id, Title: id, Ref: "card:" + id})
		steps := []plans.Step{}
		for j := 1; j <= 10; j++ {
			leaf := fmt.Sprintf("watch-budget-%d", 10+(i-1)*10+j)
			steps = append(steps, plans.Step{ID: leaf, Title: leaf, Ref: "card:" + leaf})
		}
		encoded, _ := json.Marshal(plans.Plan{Steps: steps})
		if _, err = ws.DB().Exec(`INSERT INTO card_plans(card_id,body_json,updated_at) VALUES(?,?,'now')`, id, string(encoded)); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.SetCardPlan(ctx, "writer", root["id"].(string), root["updated_at"].(string), p); err != nil {
		t.Fatal(err)
	}
	db, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	measured := primitives.NewTestStore(db, ws.Layout().ArtifactContentDir)
	raw, err := os.ReadFile("testdata/work_grouping_budget.json")
	if err != nil {
		t.Fatal(err)
	}
	var budgets map[string]struct {
		Queries int    `json:"max_queries"`
		Rows    int    `json:"max_rows"`
		VM      uint64 `json:"max_vm_steps"`
		Scans   uint64 `json:"max_full_scan_steps"`
	}
	if err = json.Unmarshal(raw, &budgets); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{"writer", "stranger"} {
		t.Run(actor, func(t *testing.T) {
			readCtx := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: actor})
			item := map[string]any{"id": root["id"]}
			capture.Start()
			err := measured.EnrichWorkHierarchy(readCtx, []map[string]any{item}, actor, true, time.Now())
			statements, queries, rows := capture.Stop()
			work := capture.Work()
			budget := budgets["workstreams"]
			if work.VMSteps > budget.VM {
				for _, stmt := range statements {
					if strings.Contains(stmt.SQL, "work_done_event_positions") {
						p, e := perfguard.Explain(readCtx, ws.DB(), stmt)
						t.Logf("plan err=%v: %s", e, strings.Join(p, "\n"))
					}
				}
			}
			if err != nil || capture.WorkError() != nil || queries > budget.Queries || rows > budget.Rows || work.VMSteps > budget.VM || work.FullScanSteps > budget.Scans {
				t.Fatalf("err=%v instrument=%v queries=%d rows=%d work=%+v", err, capture.WorkError(), queries, rows, work)
			}
			r := item["rollup"].(*primitives.WorkRollup)
			if len(r.Workstreams) != 10 || r.Workstreams[0].Rollup.Total != 10 || r.Workstreams[0].Rollup.Children[0].DoneAt == nil {
				t.Fatalf("unpopulated rollup: %+v", r)
			}
			found := false
			// Native plans must use the completion index even with the scoped relation.
			if !found {
				for _, stmt := range statements {
					if !strings.Contains(stmt.SQL, "work_done_event_positions") {
						continue
					}
					plan, e := perfguard.Explain(readCtx, ws.DB(), stmt)
					if e != nil {
						t.Fatal(e)
					}
					if strings.Contains(strings.Join(plan, "\n"), "SEARCH events USING INDEX idx_events_card_done") {
						found = true
					}
				}
			}
			if !found {
				t.Fatal("completion read did not seek its lifecycle index")
			}
			t.Logf("queries=%d rows=%d VM=%d scans=%d", queries, rows, work.VMSteps, work.FullScanSteps)
		})
	}
}
