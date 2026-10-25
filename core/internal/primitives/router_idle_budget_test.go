package primitives_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	p "agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

// The router polls once a second after its durable cursor reaches the tail.
// Same-timestamp history must not turn an empty poll into a prefix scan.
func TestRouterIdleReadBudget(t *testing.T) {
	budget := idleMaintenanceBudget(t)["router_empty"]
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	_, err = ws.DB().ExecContext(ctx, `WITH RECURSIVE s(n) AS (SELECT 0 UNION ALL SELECT n+1 FROM s WHERE n<4095) INSERT INTO events(id,type,ts,actor_id,payload_json) SELECT printf('router-%05d',n),'message_posted','2026-01-01T00:00:00Z','synthetic','{}' FROM s`)
	if err != nil {
		t.Fatal(err)
	}
	db, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := p.NewTestStore(db, ws.Layout().ArtifactContentDir)
	for _, cursor := range []p.EventCursor{{TS: "2026-01-01T00:00:00Z", ID: "router-04095"}, {TS: "2026-01-02T00:00:00Z", ID: "end"}} {
		capture.Start()
		events, err := store.ListEventsAfter(ctx, p.EventListFilter{Types: []string{"message_posted"}}, cursor, 100)
		_, queries, rows := capture.Stop()
		work := capture.Work()
		if err != nil || capture.WorkError() != nil {
			t.Fatalf("read=%v instrumentation=%v", err, capture.WorkError())
		}
		t.Logf("%s queries=%d rows=%d vm=%d scan=%d", cursor.ID, queries, rows, work.VMSteps, work.FullScanSteps)
		if len(events) != 0 || queries > int(budget["queries"]) || rows != int(budget["rows"]) || work.VMSteps > budget["vm"] || work.FullScanSteps > budget["scan"] {
			t.Fatal(fmt.Sprintf("idle tail exceeded bounded read budget: events=%d queries=%d rows=%d vm=%d scan=%d", len(events), queries, rows, work.VMSteps, work.FullScanSteps))
		}
	}
}

func idleMaintenanceBudget(t *testing.T) map[string]map[string]uint64 {
	t.Helper()
	raw, err := os.ReadFile("testdata/idle_maintenance_budget.json")
	if err != nil {
		t.Fatal(err)
	}
	var budget map[string]map[string]uint64
	if err = json.Unmarshal(raw, &budget); err != nil {
		t.Fatal(err)
	}
	return budget
}
