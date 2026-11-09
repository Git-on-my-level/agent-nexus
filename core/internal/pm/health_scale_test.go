package pm_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/testutil/perfguard"
)

func TestPerformanceWatcherPMHealthReads(t *testing.T) {
	if os.Getenv("ANX_PERFORMANCE_TEST") != "1" {
		t.Skip("set ANX_PERFORMANCE_TEST=1")
	}
	var budget struct {
		Queries int    `json:"queries"`
		Rows    int    `json:"rows"`
		VM      uint64 `json:"vm"`
		History int    `json:"history"`
	}
	raw, err := os.ReadFile("testdata/health_read_budget.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &budget); err != nil {
		t.Fatal(err)
	}
	db, capture, err := perfguard.Open("file:" + filepath.Join(t.TempDir(), "pm.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	st, err := pm.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	s, err := pm.NewService(st, pm.Config{WorkspaceID: "ws", AgentActorID: "pm"}, pm.Dependencies{Authorize: func(context.Context, pm.Principal, string, string) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().UTC()
	if _, err = db.Exec(`INSERT INTO pm_registration VALUES('ws','pm','runner','host'); INSERT INTO pm_presence(workspace_id,actor_id,last_seen_at,signal,first_seen_at) VALUES('ws','pm',?,'connect',?)`, now.Format(time.RFC3339Nano), now.Add(-time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	insert := func(turn pm.Turn) {
		t.Helper()
		raw, err := json.Marshal(turn)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(`INSERT INTO pm_records(kind,id,workspace_id,actor_id,parent_id,revision,body) VALUES('turn',?,'ws','human','',1,?)`, turn.ID, raw); err != nil {
			t.Fatal(err)
		}
	}
	insert(pm.Turn{ID: "failed", WorkspaceID: "ws", ActorID: "human", AgentActorID: "pm", Status: pm.Failed, FailureKind: "expired", CreatedAt: now.Add(-time.Minute), Deadline: now.Add(-time.Second)})
	for _, history := range []bool{false, true} {
		if history {
			_, err = db.Exec(`WITH RECURSIVE n(i) AS(SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<?) INSERT INTO pm_records(kind,id,workspace_id,actor_id,parent_id,revision,body) SELECT 'turn','old-'||i,'ws','human','',1,json_object('id','old-'||i,'workspace_id','ws','actor_id','human','agent_actor_id','pm','status',CASE WHEN i%2=0 THEN 'failed' ELSE 'delivered' END,'failure_kind','expired','created_at','2020-01-01T00:00:00Z','deadline','2020-01-01T00:01:00Z') FROM n`, budget.History)
			if err != nil {
				t.Fatal(err)
			}
		}
		capture.Start()
		unhealthy, err := s.WatcherUnhealthy(ctx, now, time.Hour)
		statements, queries, rows := capture.Stop()
		work := capture.Work()
		if err != nil || !unhealthy || capture.WorkError() != nil {
			t.Fatal(unhealthy, err, capture.WorkError())
		}
		if queries > budget.Queries || rows > budget.Rows || work.VMSteps > budget.VM || work.FullScanSteps != 0 {
			t.Fatalf("history=%v queries=%d rows=%d work=%+v", history, queries, rows, work)
		}
		plans := []string{}
		for _, stmt := range statements {
			p, err := perfguard.Explain(ctx, db, stmt)
			if err != nil {
				t.Fatal(err)
			}
			plans = append(plans, p...)
		}
		joined := strings.Join(plans, "\n")
		for _, index := range []string{"pm_records_health_expired", "pm_records_health_success"} {
			if !strings.Contains(joined, index) {
				t.Fatal("missing health index", index, joined)
			}
		}
		t.Logf("history=%v queries=%d rows=%d VM=%d scans=%d", history, queries, rows, work.VMSteps, work.FullScanSteps)
	}
	insert(pm.Turn{ID: "recovered", WorkspaceID: "ws", ActorID: "human", AgentActorID: "pm", Status: pm.Delivered, CreatedAt: now, Deadline: now.Add(time.Minute)})
	if unhealthy, err := s.WatcherUnhealthy(ctx, now, time.Hour); err != nil || unhealthy {
		t.Fatal("later request did not recover", unhealthy, err)
	}
}
