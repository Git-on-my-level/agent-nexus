package primitives_test

import (
	"context"
	"os"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

func TestPerformanceCadenceTrackingBudget(t *testing.T) {
	if os.Getenv("ANX_PERFORMANCE_TEST") != "1" {
		t.Skip("set ANX_PERFORMANCE_TEST=1")
	}
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	cards := []map[string]any{}
	for i := 0; i < 50; i++ {
		card, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "Cadence", "cadence": map[string]any{"every_seconds": 3600}})
		if err != nil {
			t.Fatal(err)
		}
		cards = append(cards, card)
	}
	if _, err = ws.DB().Exec(`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<4095) INSERT INTO cards(id,title,summary,column_key,created_at,created_by,updated_at,updated_by) SELECT printf('unrelated-%d',i),'Unrelated','','ready','2026-01-01','owner','2026-01-01','owner' FROM n`); err != nil {
		t.Fatal(err)
	}
	db, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath + "?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	measured := primitives.NewTestStore(db, ws.Layout().ArtifactContentDir)
	for _, cached := range []bool{false, true} {
		if cached {
			for done := false; !done; {
				done, err = s.RefreshBriefInitiatives(ctx, 200)
				if err != nil {
					t.Fatal(err)
				}
			}
		}
		for _, actor := range []string{"owner", "stranger"} {
			for _, size := range []int{1, 50} {
				scope := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: actor})
				scope, closeRead, err := measured.BeginResourceRead(scope)
				if err != nil {
					t.Fatal(err)
				}
				capture.Start()
				err = measured.EnrichCardPlans(scope, cards[:size], nil, time.Now().UTC())
				_, queries, rows := capture.Stop()
				work := capture.Work()
				closeRead()
				if err != nil || capture.WorkError() != nil {
					t.Fatal(err, capture.WorkError())
				}
				t.Logf("actor=%s cached=%v size=%d queries=%d rows=%d VM=%d", actor, cached, size, queries, rows, work.VMSteps)
				if queries > 100 || rows > 1024 || work.VMSteps > 50000 {
					t.Fatalf("cadence summary exceeds existing summary budget: %+v", work)
				}
			}
		}
	}
}
