package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/commandcenter"
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testsql"
)

// Measure a small workspace through the full authenticated handler, rather
// than extrapolating from a CLI process or an unscoped projection.
func TestPerformanceOverviewSmallWorkspaceLatency(t *testing.T) {
	requirePerformanceTest(t)
	// Serial: performance samples must not compete with parallel fixtures.
	testOverviewWorkspaceLatency(t, 10, 1)
}

// Cardinalities observed through a read-only Overview of the personal
// workspace; all fixture names, content and credentials remain synthetic.
func TestPerformanceOverviewPersonalSizedWorkspaceLatency(t *testing.T) {
	requirePerformanceTest(t)
	// Serial: performance samples must not compete with parallel fixtures.
	testOverviewWorkspaceLatency(t, 49, 23)
}

func testOverviewWorkspaceLatency(t *testing.T, cards, principals int) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	reader := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "latency-reader", "latency-reader-actor", "latency-reader", "latency-token")
	for i := 1; i < principals; i++ {
		id := fmt.Sprintf("latency-agent-%d", i)
		seedMachinePrincipalForLockoutTest(t, ctx, env.workspace.DB(), id, id+"-actor", id, id+"-token")
	}
	store := env.primitiveStore.(*primitives.Store)
	board, err := store.CreateBoard(ctx, reader.ActorID, map[string]any{"title": "Portfolio"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < cards; i++ {
		if _, err = store.CreateWork(ctx, reader.ActorID, anyString(board["id"]), map[string]any{"title": fmt.Sprintf("Initiative %d", i), "summary": "Deliver an outcome\n- [x] Design\n- [ ] Verify"}); err != nil {
			t.Fatal(err)
		}
	}
	db, counter := testsql.Open("file:" + env.workspace.Layout().DatabasePath)
	t.Cleanup(func() { db.Close() })
	measured := primitives.NewTestStore(db, env.workspace.Layout().ArtifactContentDir)
	authStore := auth.NewStore(db)
	runtime, err := newOnboardedPMRuntime(t, db, measured, authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewHandler("test", WithPrimitiveStore(measured), WithAuthStore(authStore), WithActorRegistry(actors.NewStore(db)), WithRunStore(commandcenter.NewStore(db, commandcenter.SQLIdentities{DB: db})), WithPMRuntime(runtime)))
	t.Cleanup(server.Close)
	for _, path := range []string{"/overview", "/inbox", "/inbox/summary"} {
		t.Run(path, func(t *testing.T) {
			var samples []time.Duration
			for i := 0; i < 12; i++ {
				counter.Reset()
				req, _ := http.NewRequest("GET", server.URL+path, nil)
				req.Header.Set("Authorization", "Bearer "+reader.AccessToken)
				started := time.Now()
				resp, err := server.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				elapsed := time.Since(started)
				if err != nil || resp.StatusCode != 200 {
					t.Fatalf("status=%d err=%v body=%s", resp.StatusCode, err, body)
				}
				timings := strings.Join(resp.Header.Values("Server-Timing"), ", ")
				if !strings.Contains(timings, "auth;dur=") || !strings.Contains(timings, "serialize;dur=") || !strings.Contains(timings, "core;dur=") {
					t.Fatalf("missing phases: %s", timings)
				}
				if i == 0 || i == 2 {
					logRequestSQLTiming(t, counter, elapsed, len(body), timings)
				}
				if i >= 2 {
					samples = append(samples, elapsed)
				}
			}
			p95 := performanceP95(samples)
			t.Logf("p95=%s", p95)
			if p95 > 500*time.Millisecond {
				t.Fatalf("small workspace exceeds 500ms: %s", p95)
			}
		})
	}
}

func logRequestSQLTiming(t *testing.T, counter *testsql.Counter, elapsed time.Duration, bytes int, phases string) {
	t.Helper()
	var snapshot, sql time.Duration
	for _, statement := range counter.Statements() {
		if strings.Contains(statement.SQL, "json_group_array(json_array(kind,id))") {
			snapshot += statement.Elapsed
		} else {
			sql += statement.Elapsed
		}
	}
	// Counter timings include row consumption. Phase timings overlap these SQL
	// timings; they are alternative breakdowns, not quantities to add together.
	t.Logf("request=%s snapshot=%s other_sql=%s statements=%d rows=%d bytes=%d phases=[%s]", elapsed, snapshot, sql, counter.Count(), counter.ReturnedRows(), bytes, phases)
}
