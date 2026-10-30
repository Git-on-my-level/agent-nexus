package server

import (
	"agent-nexus-core/internal/testutil/perfguard"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Exercise the new route budgets against the shared authenticated scale corpus.
// Keep the full TestPerformanceRoutes lane's phase/shard coverage in CI.
func TestPerformanceWorkTreeRoutes(t *testing.T) {
	requirePerformanceTest(t)
	env := newPerformanceEnv(t)
	for _, b := range performanceBudgets(t) {
		if b.Path != "/work/tree" && b.Path != "/work/resolve" && b.Path != "/work/where" {
			continue
		}
		for pi, principal := range env.principals {
			label := "owner"
			status := b.AuthorizedStatus
			if pi != 0 {
				label = "stranger"
				status = b.UnauthorizedStatus
			}
			t.Run(b.Path+"/"+label, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, env.replace.Replace(b.Path+"?"+b.Query), nil)
				req.Header.Set("Authorization", "Bearer "+principal.AccessToken)
				// Warm authorization once before measuring the stable request path.
				env.handler.ServeHTTP(httptest.NewRecorder(), req)
				env.capture.Start()
				w := httptest.NewRecorder()
				env.handler.ServeHTTP(w, req)
				statements, queries, rows := env.capture.Stop()
				work := env.capture.Work()
				if w.Code != status {
					t.Fatalf("status=%d want %d: %s", w.Code, status, w.Body.String())
				}
				if err := env.capture.WorkError(); err != nil {
					t.Fatal(err)
				}
				if queries > b.MaxQueries || rows > b.MaxRows || work.VMSteps > b.MaxVMSteps {
					var sequence int
					var name, path string
					if err := env.db.QueryRow(`PRAGMA database_list`).Scan(&sequence, &name, &path); err != nil {
						t.Fatal(err)
					}
					replay, capture, err := perfguard.Open("file:" + path)
					if err != nil {
						t.Fatal(err)
					}
					defer replay.Close()
					for _, statement := range statements {
						capture.Start()
						result, replayErr := replay.Query(statement.SQL, statement.Args...)
						if replayErr == nil {
							for result.Next() {
							}
							replayErr = result.Err()
							result.Close()
						}
						capture.Stop()
						plan, planErr := perfguard.Explain(context.Background(), env.db, statement)
						t.Logf("statement=%s VM=%d replay=%v planErr=%v plan=%s", perfguard.SQLHash(statement.SQL), capture.Work().VMSteps, replayErr, planErr, strings.Join(plan, " | "))
					}
					t.Fatalf("queries=%d rows=%d VM=%d; budget=%+v", queries, rows, work.VMSteps, b)
				}
				if pi != 0 && strings.Contains(w.Body.String(), "Private scale target") {
					t.Fatalf("private target leaked: %s", w.Body.String())
				}
				t.Logf("queries=%d rows=%d VM=%d scans=%d", queries, rows, work.VMSteps, work.FullScanSteps)
			})
		}
	}
}
