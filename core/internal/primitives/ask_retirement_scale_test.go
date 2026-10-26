package primitives_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"agent-nexus-core/internal/auth"
	p "agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

func TestPerformanceAskRetirementVisibilityBudget(t *testing.T) {
	if testing.Short() || os.Getenv("ANX_PERFORMANCE_TEST") != "1" {
		t.Skip("scale gate")
	}
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := p.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	card, err := s.CreateWork(ctx, "requester", "", map[string]any{"title": "Visible retirement task"})
	if err != nil {
		t.Fatal(err)
	}
	ask, err := s.AppendEvent(ctx, "requester", map[string]any{"type": "human_attention_requested", "thread_id": card["thread_id"], "refs": []string{fmt.Sprint(card["ref"])}, "payload": map[string]any{"subject_ref": card["ref"], "title": "Visible retirement?"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`INSERT INTO agents(id,username,actor_id,created_at,updated_at) VALUES('requester','requester','requester','now','now')`); err != nil {
		t.Fatal(err)
	}
	retired, err := s.RetireAsk(ctx, auth.Principal{AgentID: "requester", ActorID: "requester", PrincipalKind: "agent"}, fmt.Sprint(ask["ref"]), "measured-retire", "measured-retire", "No longer relevant", []string{fmt.Sprint(card["ref"])}, false, "")
	if err != nil {
		t.Fatal(err)
	}
	// The notice includes the public lifecycle record, so it exercises the
	// complete, longest ancestry of the two persisted public event checks.
	target := strings.TrimPrefix(fmt.Sprint(retired["notification_event_ref"]), "event:")
	pool, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath + "?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	measured := p.NewTestStore(pool, ws.Layout().ArtifactContentDir)
	var budget struct {
		Queries int    `json:"max_queries"`
		Rows    int    `json:"max_rows"`
		VM      uint64 `json:"max_vm_steps"`
		Scans   uint64 `json:"max_full_scan_steps"`
	}
	raw, err := os.ReadFile("testdata/ask_retirement_visibility_budget.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &budget); err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{0, 4096} {
		if size != 0 {
			if _, err = ws.DB().Exec(`WITH RECURSIVE n(i) AS(VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<4096) INSERT INTO threads(id,updated_at,updated_by,body_json) SELECT 'retirement-unrelated-'||i,'now','other',json_object('title','Unrelated private thread','pm_actor_id','other') FROM n`); err != nil {
				t.Fatal(err)
			}
		}
		// Measure a fresh recipient boundary, as used after each actual insert.
		capture.Start()
		err = p.RequireAskVisibleEventForTest(measured, ctx, "requester", target)
		_, queries, rows := capture.Stop()
		work := capture.Work()
		if captureErr := capture.WorkError(); captureErr != nil {
			t.Fatal(captureErr)
		}
		t.Logf("unrelated=%d queries=%d rows=%d VM=%d scans=%d", size, queries, rows, work.VMSteps, work.FullScanSteps)
		if queries > budget.Queries || rows > budget.Rows || work.VMSteps > budget.VM || work.FullScanSteps > budget.Scans {
			t.Fatalf("persisted visibility budget exceeded: queries=%d rows=%d work=%+v", queries, rows, work)
		}
		if err != nil {
			t.Fatal(err)
		}
		if !s.CanAccessResource(p.WithRequestAccessScope(ctx, p.AccessScope{ActorID: "requester"}), "event", fmt.Sprint(ask["ref"])) {
			t.Fatal("public ask disappeared behind unrelated private resources")
		}
	}
}
