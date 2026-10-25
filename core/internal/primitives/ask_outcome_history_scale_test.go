package primitives_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	p "agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

func TestPerformanceAskOutcomeHistoryBudget(t *testing.T) {
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
	ask, err := s.AppendEvent(ctx, "requester", map[string]any{"type": "human_attention_requested", "refs": []string{}, "payload": map[string]any{"title": "Proceed?", "requester_actor_id": "requester"}})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := s.CreateAskSubscription(ctx, "requester", fmt.Sprint(ask["ref"]), p.AskSubscriptionInput{Kind: "await", Label: "current"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AppendHumanAttentionWithdrawal(ctx, "requester", fmt.Sprint(ask["id"]), map[string]any{"payload": map[string]any{"reason": "measured"}}); err != nil {
		t.Fatal(err)
	}
	pool, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath + "?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	measured := p.NewTestStore(pool, ws.Layout().ArtifactContentDir)
	var baselineVM uint64
	for _, size := range []int{0, 4096} {
		if size > 0 {
			if _, err = ws.DB().Exec(`WITH RECURSIVE n(i) AS(VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<4096) INSERT INTO ask_deliveries(id,subscription_id,ask_id,resolution_event_id,kind,state,next_at) SELECT 'old-outcome-'||i,?,?,'old-resolution-'||i,'await','delivered','2000-01-01' FROM n`, sub["id"], ask["id"]); err != nil {
				t.Fatal(err)
			}
			if _, err = ws.DB().Exec(`INSERT INTO ask_delivery_attempts(delivery_id,attempt,status_code,latency_ms,at) SELECT id,1,200,0,'2000-01-01' FROM ask_deliveries WHERE id LIKE 'old-outcome-%'`); err != nil {
				t.Fatal(err)
			}
		}
		scoped := p.WithReadTickSnapshot(p.WithAccessScope(ctx, p.AccessScope{ActorID: "requester"}))
		if _, err = measured.AskOutcome(scoped, fmt.Sprint(ask["id"])); err != nil {
			t.Fatal(err)
		}
		capture.Start()
		out, err := measured.AskOutcome(scoped, fmt.Sprint(ask["id"]))
		_, queries, rows := capture.Stop()
		if err != nil {
			t.Fatal(err)
		}
		work := capture.Work()
		if err = capture.WorkError(); err != nil {
			t.Fatal(err)
		}
		if len(out["delivery"].([]map[string]any)) != 1 {
			t.Fatal(out)
		}
		t.Logf("history=%d queries=%d rows=%d VM=%d scans=%d", size, queries, rows, work.VMSteps, work.FullScanSteps)
		if size == 0 {
			baselineVM = work.VMSteps
		} else if work.VMSteps > baselineVM+256 {
			t.Fatalf("outcome read grew with prior outcomes: baseline VM=%d, history VM=%d", baselineVM, work.VMSteps)
		}
		// Same ceiling as the existing ask delivery/outcome gate, without allowances.
		if queries > 32 || rows > 256 || work.VMSteps > 50000 || work.FullScanSteps > 256 {
			t.Fatalf("outcome history exceeded existing read budget: %+v", work)
		}
	}
}
