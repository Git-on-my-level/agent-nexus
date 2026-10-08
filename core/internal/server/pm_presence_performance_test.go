package server

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/testutil/perfguard"
)

// This new route has no historical baseline exception. Exercise its own strict
// budget on the shared 4096-record fixture, independently of old route pins.
func TestPerformancePMPresence(t *testing.T) {
	requirePerformanceTest(t)
	env := newPerformanceEnv(t)
	tx, err := env.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT INTO pm_presence(workspace_id,actor_id,last_seen_at,signal) VALUES('ws_main',?,'2020-01-01T00:00:00Z','heartbeat')`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4096; i++ {
		if _, err = stmt.Exec(fmt.Sprintf("unrelated-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = stmt.Exec(env.agent.ActorID); err != nil {
		t.Fatal(err)
	}
	stmt.Close()
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var budget routeBudget
	for _, b := range performanceBudgets(t) {
		if b.Path == "/pm/presence" {
			budget = b
		}
	}
	if budget.MaxQueries == 0 {
		t.Fatal("missing presence budget")
	}
	for i, p := range env.principals {
		handler, capture, _, closePool := env.fresh(t)
		for sample := 0; sample < 7; sample++ {
			if sample == 6 {
				invalidatePerformanceCache(t, env)
			}
			req := httptest.NewRequest("GET", "/pm/presence", nil)
			req.Header.Set("Authorization", "Bearer "+p.AccessToken)
			w := httptest.NewRecorder()
			capture.Start()
			start := time.Now()
			handler.ServeHTTP(w, req)
			elapsed := time.Since(start)
			statements, queries, rows := capture.Stop()
			work := capture.Work()
			t.Logf("principal=%d sample=%d queries=%d rows=%d vm=%d elapsed=%s", i, sample, queries, rows, work.VMSteps, elapsed)
			if err := capture.WorkError(); err != nil {
				t.Fatal(err)
			}
			if w.Code != 200 || (!strings.Contains(w.Body.String(), `"connected":false`) || !strings.Contains(w.Body.String(), `"last_seen_at":"2020-01-01T00:00:00Z"`)) {
				t.Fatalf("presence: %d %s", w.Code, w.Body.String())
			}
			if queries > budget.MaxQueries || rows > budget.MaxRows || work.VMSteps > budget.MaxVMSteps || elapsed > time.Duration(budget.LatencyMS)*time.Millisecond {
				for _, statement := range statements {
					t.Logf("SQL: %s", statement.SQL)
				}
				t.Fatalf("presence exceeds strict budget")
			}
			for _, statement := range statements {
				if !strings.Contains(statement.SQL, "FROM pm_presence") {
					continue
				}
				plan, err := perfguard.Explain(context.Background(), env.db, statement)
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, line := range plan {
					if strings.Contains(line, "SEARCH pm_presence USING INDEX sqlite_autoindex_pm_presence_1") {
						found = true
					}
					if strings.Contains(line, "SCAN pm_presence") {
						t.Fatal("presence scan")
					}
				}
				if !found {
					t.Fatalf("presence lacks indexed point plan: %+v", plan)
				}
			}
		}
		closePool()
	}
}
