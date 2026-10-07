package server

import (
	"context"
	"encoding/json"
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

// Reproduce payload-derived edge density, not just live-card cardinality.
// All canonical imports pass through the production SQLite write triggers.
func TestOverviewDenseAccessWorkspaceLatency(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	reader := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "dense-reader", "dense-reader-actor", "dense-reader", "dense-token")
	for i := 1; i < 25; i++ {
		id := fmt.Sprintf("dense-agent-%d", i)
		seedMachinePrincipalForLockoutTest(t, ctx, env.workspace.DB(), id, id+"-actor", id, id+"-token")
	}
	tx, err := env.workspace.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 499; i++ {
		body := "{}"
		if i%25 != 0 {
			body = fmt.Sprintf(`{"pm_actor_id":"dense-agent-%d-actor"}`, i%25)
		}
		exec(`INSERT INTO threads(id,handle,updated_at,updated_by,body_json) VALUES(?,?,'2026-01-01T00:00:00Z','fixture',?)`, fmt.Sprintf("dense-thread-%d", i), fmt.Sprintf("dense-thread-%d", i), body)
	}
	exec(`INSERT INTO boards(id,title,thread_id,column_schema_json,created_at,created_by,updated_at,updated_by) VALUES('dense-board','Portfolio','dense-thread-0','[]','now','fixture','now','fixture')`)
	for i := 0; i < 433; i++ {
		id := fmt.Sprintf("dense-card-%d", i)
		title := "Visible dense work"
		if i%25 != 0 {
			title = "PrivateDenseSecret"
		}
		exec(`INSERT INTO cards(id,handle,title,thread_id,board_id,created_at,created_by,updated_at,updated_by,head_revision_id) VALUES(?,?,?,?,'dense-board','now','fixture','now','fixture','fixture')`, id, id, title, fmt.Sprintf("dense-thread-%d", i))
	}
	for i := 0; i < 433; i++ {
		values := []string{}
		for j := 0; j < 75; j++ {
			values = append(values, fmt.Sprintf("metadata-value-%d-%d", i, j))
		}
		raw, _ := json.Marshal(map[string]any{"snapshot": values})
		exec(`INSERT INTO work_metadata(card_id,metadata_json,updated_at,updated_by) VALUES(?,?,'now','fixture')`, fmt.Sprintf("dense-card-%d", i), string(raw))
	}

	for i := 0; i < 806; i++ {
		id := fmt.Sprintf("dense-artifact-%d", i)
		exec(`INSERT INTO artifacts(id,handle,kind,created_at,created_by,content_type,content_hash,content_refs_json,metadata_json) VALUES(?,?,'note','now','fixture','text','fixture','[]','{}')`, id, id)
	}
	// Six prose references produce six resolved mentions and 48 prefix buckets;
	// Snapshot scalar values supply additional atoms; work metadata contributes
	// the remaining exact/external edges of the supplied workspace shape.
	for i := 0; i < 2759; i++ {
		values := []string{}
		for j := 0; j < 6; j++ {
			values = append(values, fmt.Sprintf("Evidence for card:dense-card-%d.", (i+j)%433))
		}
		for j := 0; j < 52; j++ {
			values = append(values, fmt.Sprintf("snapshot-value-%d-%d", i, j))
		}
		payload, _ := json.Marshal(map[string]any{"snapshot": values})
		exec(`INSERT INTO events(id,handle,type,thread_id,ts,actor_id,refs_json,payload_json) VALUES(?,?,'message_posted',?,'2026-01-01T00:00:00Z','fixture','[]',?)`, fmt.Sprintf("dense-event-%d", i), fmt.Sprintf("dense-event-%d", i), fmt.Sprintf("dense-thread-%d", i%499), string(payload))
	}
	for i := 0; i < 12428; i++ {
		exec(`INSERT INTO ref_edges(id,source_type,source_id,target_type,target_id,edge_type,created_at,metadata_json) VALUES(?,'event',?,'card',?,'ref','now','{}')`, fmt.Sprintf("dense-ref-%d", i), fmt.Sprintf("dense-event-%d", i%2759), fmt.Sprintf("dense-card-%d", i%433))
	}
	for i := 0; i < 6731; i++ {
		exec(`INSERT INTO resource_handle_aliases(resource_type,alias_handle,resource_id,canonical_handle,created_at) VALUES('card',?,?,?,'now')`, fmt.Sprintf("historical-dense-%d", i), fmt.Sprintf("dense-card-%d", i%433), fmt.Sprintf("dense-card-%d", i%433))
	}
	for i := 0; i < 12; i++ {
		exec(`INSERT INTO derived_inbox_items(id,thread_id,category,trigger_at,generated_at,data_json) VALUES(?,'dense-thread-0','ask','2026-01-01T00:00:00Z','now','{}')`, fmt.Sprintf("dense-inbox-%d", i))
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"resource_access_edges", "resource_access_exact_edges", "resource_access_mention_buckets", "resource_access_mentions", "resource_access_external_edges", "resource_access_identities", "ref_edges", "events", "threads", "cards", "artifacts", "actors", "agents", "derived_inbox_items"} {
		var n int
		if err = env.workspace.DB().QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		t.Logf("shape %s=%d", table, n)
	}
	db, counter := testsql.Open("file:" + env.workspace.Layout().DatabasePath)
	t.Cleanup(func() { db.Close() })
	store := primitives.NewTestStore(db, env.workspace.Layout().ArtifactContentDir)
	authStore := auth.NewStore(db)
	runtime, err := NewPMRuntime(db, store, authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewHandler("test", WithPrimitiveStore(store), WithAuthStore(authStore), WithActorRegistry(actors.NewStore(db)), WithRunStore(commandcenter.NewStore(db, commandcenter.SQLIdentities{DB: db})), WithPMRuntime(runtime)))
	t.Cleanup(server.Close)
	for _, path := range []string{"/inbox", "/overview"} {
		t.Run(path, func(t *testing.T) {
			var samples []time.Duration
			for i := 0; i < 13; i++ {
				if i == 12 {
					// Measure epoch invalidation separately from steady-state p95.
					if _, err := env.workspace.DB().Exec(`UPDATE resource_access_epoch SET version=version+1`); err != nil {
						t.Fatal(err)
					}
					t.Log("authorization epoch invalidated before this request")
				}
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
				logRequestSQLTiming(t, counter, elapsed, len(body), strings.Join(resp.Header.Values("Server-Timing"), ", "))
				if i == 0 {
					for _, s := range counter.Statements() {
						if s.Elapsed > 20*time.Millisecond {
							sql := s.SQL
							if len(sql) > 600 {
								sql = sql[len(sql)-600:]
							}
							t.Logf("slow=%s rows=%d sql=%s", s.Elapsed, s.Rows, sql)
						}
					}
				}
				if strings.Contains(string(body), "PrivateDenseSecret") {
					t.Fatal("private work leaked")
				}
				if i > 1 && i < 12 {
					samples = append(samples, elapsed)
				}
			}
			p95 := performanceP95(samples)
			t.Logf("p95=%s", p95)
			if p95 > 300*time.Millisecond {
				t.Logf("advisory: dense workspace p95 exceeds 300ms: %s", p95)
			}
		})
	}
}
