package server

import (
	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/commandcenter"
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/schema"
	"agent-nexus-core/internal/testsql"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// This deliberately exceeds a medium workspace, with prose-bearing rows in
// every canonical collection. The former closure matched all stored prose once
// per denied identity, making even identity and bounded collection reads stall.
func TestResourceAccessCommonReadPerformance(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	if testing.Short() {
		t.Skip("full HTTP/storage performance fixture")
	}
	ctx := context.Background()
	s := env.primitiveStore.(*primitives.Store)
	if _, err := pm.NewStore(env.workspace.DB()); err != nil {
		t.Fatal(err)
	}
	stranger := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "perf-reader", "perf-reader-actor", "perf-reader", "perf-reader-token")
	privateRefs := []string{}
	for i := 0; i < 10; i++ {
		board, err := s.CreateBoard(ctx, "private-owner", map[string]any{"title": fmt.Sprintf("private-%d", i)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.PatchThread(ctx, "private-owner", anyString(board["thread_id"]), map[string]any{"pm_actor_id": "private-owner"}, nil); err != nil {
			t.Fatal(err)
		}
		private, err := s.CreateBoardCard(ctx, "private-owner", anyString(board["id"]), primitives.AddBoardCardInput{Title: "PrivatePerformanceSecret", Body: "secret", ColumnKey: "ready"})
		if err != nil {
			t.Fatal(err)
		}
		privateRefs = append(privateRefs, anyString(private.Card["ref"]))
	}
	publicBoard, err := s.CreateBoard(ctx, "public-writer", map[string]any{"title": "public fixture"})
	if err != nil {
		t.Fatal(err)
	}
	readerAgent := seedMachinePrincipalForLockoutTest(t, ctx, env.workspace.DB(), "perf-agent", "perf-agent-actor", "perf.agent", "perf-agent-token")
	tx, err := env.workspace.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	queries := []string{
		`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES(?,'2026-01-01T00:00:00Z','public-writer','{}')`,
		`INSERT INTO events(id,type,ts,actor_id,refs_json,payload_json) VALUES(?,'card_updated','2026-01-01T00:00:00Z','public-writer',?,?)`,
		`INSERT INTO artifacts(id,kind,created_at,created_by,content_type,content_hash,content_refs_json,metadata_json) VALUES(?,'note','2026-01-01T00:00:00Z','public-writer','text','fixture','[]',?)`,
		`INSERT INTO documents(id,title,head_revision_id,head_revision_number,created_at,created_by,updated_at,updated_by,search_text) VALUES(?,'Public fixture','fixture',1,'2026-01-01T00:00:00Z','public-writer','2026-01-01T00:00:00Z','public-writer',?)`,
		`INSERT INTO cards(id,title,created_at,created_by,updated_at,updated_by,summary,board_id,handle,head_revision_id) VALUES(?,'Public fixture','2026-01-01T00:00:00Z','public-writer','2026-01-01T00:00:00Z','public-writer',?,?,?,'fixture')`,
	}
	for table, q := range queries {
		stmt, err := tx.PrepareContext(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		count := 4096
		if table == 0 {
			count = 20
		}
		if table == 4 {
			count = 4096
		}
		for i := 0; i < count; i++ {
			id := fmt.Sprintf("%08x-row-%d", i, table)
			text := fmt.Sprintf("Public fixture links to document:%08x-absent. More public text.", i)
			args := []any{id}
			if table > 0 {
				if table == 1 || table == 2 {
					text = fmt.Sprintf(`{"text":%q}`, text)
				}
				if table == 1 {
					refs, _ := json.Marshal([]string{"board:" + anyString(publicBoard["id"]), fmt.Sprintf("card:%08x-row-4", i)})
					args = append(args, string(refs))
				}
				args = append(args, text)
			}
			if table == 4 {
				args = append(args, publicBoard["id"], id)
			}
			if _, err := stmt.ExecContext(ctx, args...); err != nil {
				t.Fatal(err)
			}
		}
		stmt.Close()
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := env.workspace.DB().ExecContext(ctx, `INSERT INTO ref_edges(id,source_type,source_id,target_type,target_id,edge_type,created_at,metadata_json) SELECT 'placement-'||id,'board',board_id,'card',id,'board_card','2026-01-01T00:00:00Z','{}' FROM cards WHERE id LIKE '%-row-4'`); err != nil {
		t.Fatal(err)
	}
	// Real PM record kinds reference public boards, cards and their plans. Both
	// PM bodies and inbox bodies used to take separate read-time scan paths.
	for _, kind := range []string{"conversation", "decision", "action", "turn"} {
		bulk, err := env.workspace.DB().BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		stmt, err := bulk.Prepare(`INSERT INTO pm_records VALUES(?,?,'ws_main','public-writer','',1,?)`)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 4096; i++ {
			ref := fmt.Sprintf("card:%08x-row-4", i)
			if kind == "conversation" {
				ref = "board:" + anyString(publicBoard["id"])
			}
			if kind == "action" {
				ref = fmt.Sprintf("plan:%08x-row-4", i)
			}
			body, _ := json.Marshal(map[string]any{"id": fmt.Sprintf("perf-%s-%d", kind, i), "work_ref": ref, "text": "Public PM evidence", "status": "declined", "created_at": "2026-01-01T00:00:00Z"})
			if _, err = stmt.Exec(kind, fmt.Sprintf("perf-%s-%d", kind, i), body); err != nil {
				t.Fatal(err)
			}
		}
		stmt.Close()
		if err = bulk.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	bulk, err := env.workspace.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4096; i++ {
		if _, err = bulk.Exec(`INSERT INTO card_plans VALUES(?,'{}','now')`, fmt.Sprintf("%08x-row-4", i)); err != nil {
			t.Fatal(err)
		}
		item := streamPrivacyInboxItem(anyString(publicBoard["thread_id"]), fmt.Sprintf("perf-inbox-%d", i), "Public inbox evidence")
		item.SourceEventID = fmt.Sprintf("%08x-row-1", i)
		body, _ := json.Marshal(item.Data)
		if _, err = bulk.Exec(`INSERT INTO derived_inbox_items(id,thread_id,source_event_id,category,trigger_at,generated_at,data_json) VALUES(?,?,?,'ask','2026-01-01T00:00:00Z','now',?)`, item.ID, item.ThreadID, item.SourceEventID, body); err != nil {
			t.Fatal(err)
		}
	}
	if err = bulk.Commit(); err != nil {
		t.Fatal(err)
	}
	// Populated actionable PM window: 101 distinct public subjects plus decisions
	// owned by the reader that inherit ten unrelated private roots.
	for i := 0; i < 101+len(privateRefs); i++ {
		ref, instruction, revision := fmt.Sprintf("card:%08x-row-4", i), "Review public work", "0.1"
		if i >= 101 {
			ref, instruction, revision = privateRefs[i-101], "PrivatePerformanceSecret", "1.1"
		}
		d := pm.Decision{ID: fmt.Sprintf("perf-awaiting-%d", i), WorkspaceID: "ws_main", ActorID: stranger.ActorID, WorkRef: ref, Status: pm.AwaitingAnswer, TargetRevision: revision, Scope: "work.annotate", Instruction: instruction, CreatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}
		body, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = env.workspace.DB().Exec(`INSERT INTO pm_records VALUES('decision',?,'ws_main',?,'',1,?)`, d.ID, stranger.ActorID, body); err != nil {
			t.Fatal(err)
		}
	}
	// Real directories and administrator host/key fanout. No hosted content.
	for _, q := range []string{
		`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<4095) INSERT INTO actors(id,display_name,tags_json,created_at,metadata_json) SELECT 'scale-actor-'||i,'Scale actor '||i,'[]','2026-01-01T00:00:00Z','{}' FROM n`,
		`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<4095) INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) SELECT 'scale-agent-'||i,printf('scale-agent-%04d',i),'scale-actor-'||i,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','{"principal_kind":"agent","auth_admin":true}' FROM n`,
		`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<4095) INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at) SELECT 'scale-host-'||i,'scale-host-'||i,'Scale host','test','test','[]','2026-01-01T00:00:00Z' FROM n`,
		`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<4095) INSERT INTO host_agents(host_id,name,agent_id,identity_kind) SELECT 'scale-host-'||i,'codex','scale-agent-'||i,'derived' FROM n`,
		`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<16383) INSERT INTO host_keys(id,host_id,public_key,created_at,revoked_at) SELECT 'scale-key-'||i,'scale-host-'||(i/4),'fixture',printf('2026-01-01T00:00:%02dZ',i%4),'revoked' FROM n`,
	} {
		if _, err := env.workspace.DB().ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	// Wrap the real request's connection for a statement budget, retaining SQLite
	// functions and resource scopes. Reuse the synthetic database and identities.
	counted, counter := testsql.Open("file:" + env.workspace.Layout().DatabasePath)
	t.Cleanup(func() { counted.Close() })
	measuredStore := primitives.NewTestStore(counted, env.workspace.Layout().ArtifactContentDir)
	measuredAuth := auth.NewStore(counted)
	runtime, err := NewPMRuntime(counted, measuredStore, measuredAuth, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}})
	if err != nil {
		t.Fatal(err)
	}
	contract, err := schema.Load(filepath.Join("..", "..", "..", "contracts", "anx-schema.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	measuredServer := httptest.NewServer(NewHandler("test", WithPrimitiveStore(measuredStore), WithAuthStore(measuredAuth), WithActorRegistry(actors.NewStore(counted)), WithRunStore(commandcenter.NewStore(counted, commandcenter.SQLIdentities{DB: counted})), WithPMRuntime(runtime), WithSchemaContract(contract)))
	t.Cleanup(measuredServer.Close)
	env.server = measuredServer
	t.Run("pm-point", func(t *testing.T) {
		db := resourceaccess.NewDB(env.workspace.DB())
		scope := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: stranger.ActorID})
		var samples []time.Duration
		for i := 0; i < 27; i++ {
			start := time.Now()
			var body []byte
			if err := db.QueryRowContext(scope, `SELECT body FROM pm_records WHERE kind='decision' AND id='perf-decision-0'`).Scan(&body); err != nil {
				t.Fatal(err)
			}
			if i >= 2 {
				samples = append(samples, time.Since(start))
			}
		}
		p95 := performanceP95(samples)
		t.Logf("p95=%s", p95)
		if p95 > 200*time.Millisecond {
			t.Fatalf("PM point p95=%s exceeds 200ms", p95)
		}
	})
	t.Run("selector-and-stream-poll", func(t *testing.T) {
		samples := []time.Duration{}
		for i := 0; i < 27; i++ {
			// A fresh scope on every tick mirrors stream readers, which must not
			// reuse a connection-wide decision after private ownership changes.
			scope := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: stranger.ActorID})
			start := time.Now()
			if err := s.CheckResourceValues(scope, []string{"card:missing-public"}); err != nil {
				t.Fatal(err)
			}
			limit := 50
			if _, err := s.ListEvents(scope, primitives.EventListFilter{Limit: limit}); err != nil {
				t.Fatal(err)
			}
			if i >= 2 {
				samples = append(samples, time.Since(start))
			}
		}
		if p95 := performanceP95(samples); p95 > 200*time.Millisecond {
			t.Fatalf("selector/poll p95=%s exceeds 200ms", p95)
		} else {
			t.Logf("p95=%s", p95)
		}
	})
	if _, err := measuredStore.GetWork(ctx, "00000000-row-4"); err != nil {
		t.Fatalf("point fixture: %v", err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	for _, path := range []string{"/agents/me", "/inbox", "/inbox/summary", "/work?limit=20", "/overview", "/work/00000000-row-4", "/pm/decisions?limit=20", "/pm/actions?limit=20", "/auth/admins?limit=20", "/hosts/scale-host-0", "/events?limit=50", "/reports/preview"} {
		t.Run(path, func(t *testing.T) {
			// Reopen SQLite and construct a fresh handler/runtime for each route:
			// first-request samples include cold application and connection caches.
			measuredServer.Close()
			counted.Close()
			counted, counter = testsql.Open("file:" + env.workspace.Layout().DatabasePath)
			defer counted.Close()
			measuredStore = primitives.NewTestStore(counted, env.workspace.Layout().ArtifactContentDir)
			measuredAuth = auth.NewStore(counted)
			runtime, err = NewPMRuntime(counted, measuredStore, measuredAuth, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}})
			if err != nil {
				t.Fatal(err)
			}
			measuredServer = httptest.NewServer(NewHandler("test", WithPrimitiveStore(measuredStore), WithAuthStore(measuredAuth), WithActorRegistry(actors.NewStore(counted)), WithRunStore(commandcenter.NewStore(counted, commandcenter.SQLIdentities{DB: counted})), WithPMRuntime(runtime), WithSchemaContract(contract)))
			defer measuredServer.Close()
			env.server = measuredServer

			samples := make([]time.Duration, 0, 25)
			var projectionSamples []time.Duration
			iterations := 27
			if path == "/reports/preview" {
				iterations = 3
			}
			for i := 0; i < iterations; i++ {
				// Pair the overview with its canonical projection so sustained CI
				// CPU contention raises both measurements. Bounded selector/PM
				// budgets still catch workspace-wide authorization scans.
				if path == "/overview" {
					start := time.Now()
					baseline, err := s.Overview(ctx, nil, nil)
					if err != nil {
						t.Fatal(err)
					}
					if _, err = json.Marshal(baseline); err != nil {
						t.Fatal(err)
					}
					if i >= 2 {
						projectionSamples = append(projectionSamples, time.Since(start))
					}
				}
				counter.Reset()
				start := time.Now()
				req, err := http.NewRequest("GET", env.server.URL+path, nil)
				requestClient := client
				if path == "/reports/preview" {
					body := `{"report":{"kind":"anx.visual-report","schema_version":1,"title":"Performance","summary":"Synthetic preview","generated_at":"2026-01-01T00:00:00Z","projects":[{"id":"workspace","title":"Workspace","summary":"Scale fixture","outcome":"Read"}],"sources":[],"panels":[{"id":"activity","project_id":"workspace","type":"live-activity","title":"Activity","author":"Test","provenance":"reported","observed_at":null,"freshness":"unknown","source_ids":[],"data":{"limit":10}},{"id":"fleet","project_id":"workspace","type":"live-fleet-health","title":"Fleet","author":"Test","provenance":"reported","observed_at":null,"freshness":"unknown","source_ids":[],"data":{}}]}}`
					req, err = http.NewRequest("POST", env.server.URL+path, strings.NewReader(body))
					req.Header.Set("Content-Type", "application/json")
					requestClient = &http.Client{Timeout: 45 * time.Second}
				}
				if err != nil {
					t.Fatal(err)
				}
				token := stranger.AccessToken
				if path == "/agents/me" {
					token = readerAgent.AccessToken
				}
				req.Header.Set("Authorization", "Bearer "+token)
				resp, err := requestClient.Do(req)
				if err != nil {
					for _, q := range counter.Statements() {
						sql := q.SQL
						if len(sql) > 400 {
							sql = sql[len(sql)-400:]
						}
						t.Logf("sql %s rows=%d time=%s", sql, q.Rows, q.Elapsed)
					}
					t.Fatal(err)
				}
				body, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != 200 {
					t.Fatalf("status=%d: %s", resp.StatusCode, body)
				}
				if path == "/overview" {
					var overview map[string]any
					if err := json.Unmarshal(body, &overview); err != nil {
						t.Fatal(err)
					}
					needs := overview["needs_you"].(map[string]any)
					decisions := 0
					for _, raw := range needs["rows"].([]any) {
						if strings.HasPrefix(raw.(map[string]any)["id"].(string), "decision:perf-awaiting-") {
							decisions++
						}
					}
					if decisions != 100 || needs["truncated"] != true {
						t.Fatalf("populated PM window missing: decisions=%d needs=%v", decisions, needs)
					}
					for _, section := range []string{"work", "needs_you", "agents"} {
						if overview[section].(map[string]any)["status"] != "ok" {
							t.Fatalf("overview %s unavailable: %v", section, overview[section])
						}
					}
				}
				if path == "/reports/preview" {
					var preview map[string]any
					if err := json.Unmarshal(body, &preview); err != nil {
						t.Fatal(err)
					}
					for _, raw := range preview["panels"].([]any) {
						panel := raw.(map[string]any)
						if panel["status"] != "ok" || panel["truncated"] != true {
							t.Fatalf("preview falsely succeeded: %v", panel)
						}
					}
				}
				if strings.Contains(string(body), "PrivatePerformanceSecret") {
					t.Fatal("private content leaked")
				}
				statementBudget := int64(70)
				// PM preserves fresh authority while batching candidate visibility;
				// the bound scales with the requested page, never history.
				if strings.HasPrefix(path, "/pm/") {
					statementBudget = 150
				}
				if path == "/reports/preview" {
					statementBudget = 2500
				}
				if statements := counter.Count(); statements > statementBudget {
					for _, q := range counter.Statements() {
						sql := q.SQL
						if len(sql) > 300 {
							sql = sql[len(sql)-300:]
						}
						t.Logf("sql %s rows=%d time=%s", sql, q.Rows, q.Elapsed)
					}
					t.Fatalf("statements=%d exceeds %d", statements, statementBudget)
				}
				// Summary and the standalone host roster retain exhaustive counts;
				// bounded display reads must not hydrate workspace history.
				rowBudget := 1200
				if path == "/reports/preview" {
					rowBudget = 6000
				}
				if path != "/inbox/summary" && !strings.HasPrefix(path, "/hosts/") && counter.ReturnedRows() > rowBudget {
					for _, q := range counter.Statements() {
						if q.Rows > 100 {
							t.Logf("large read rows=%d elapsed=%s", q.Rows, q.Elapsed)
						}
					}
					t.Fatalf("returned rows=%d exceeds %d bounded-read budget", counter.ReturnedRows(), rowBudget)
				}

				if i == 0 {
					t.Logf("cold app/connection request=%s statements=%d returned_rows=%d", time.Since(start), counter.Count(), counter.ReturnedRows())
					for _, q := range counter.Statements() {
						if q.Elapsed > 25*time.Millisecond {
							query := q.SQL
							if len(query) > 600 {
								query = query[len(query)-600:]
							}
							t.Logf("slow read rows=%d elapsed=%s SQL=%s", q.Rows, q.Elapsed, query)
						}
					}
					logReadPlans(t, counted, counter.Statements())
				}
				if i == 2 {
					t.Logf("warm statements=%d returned_rows=%d duration=%s", counter.Count(), counter.ReturnedRows(), time.Since(start))
					for _, q := range counter.Statements() {
						if q.Elapsed > 25*time.Millisecond {
							query := q.SQL
							if len(query) > 600 {
								query = query[len(query)-600:]
							}
							t.Logf("warm slow rows=%d elapsed=%s SQL=%s", q.Rows, q.Elapsed, query)
						}
					}
				}
				if i >= 2 || path == "/reports/preview" && i >= 1 {
					samples = append(samples, time.Since(start))
				}
			}
			p95 := performanceP95(samples)
			t.Logf("p95=%s", p95)
			// Personal workspace release target: <=500ms p95, including
			// authorization, projection and HTTP serialization.
			budget := 500 * time.Millisecond
			// Newly covered PM pages retain live per-resource authority reads;
			// their own bounded-page gate does not alter overview's 500ms target.
			if strings.HasPrefix(path, "/pm/") {
				budget = 900 * time.Millisecond
			}
			if path == "/reports/preview" {
				budget = 45 * time.Second
			}
			if path == "/overview" {
				t.Logf("canonical projection p95=%s", performanceP95(projectionSamples))
			}
			if p95 > budget {
				t.Fatalf("p95=%s exceeds %s read budget", p95, budget)
			}
		})
	}
}

func performanceP95(samples []time.Duration) time.Duration {
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	return samples[(95*len(samples)+99)/100-1]
}

// Explain the actual captured statements (including request scope rewriting),
// not simplified stand-in SQL that can conceal planner materialization.
func logReadPlans(t *testing.T, db *sql.DB, statements []testsql.Statement) {
	t.Helper()
	for _, statement := range statements {
		closedProbe := strings.Contains(statement.SQL, "column_key IN ('done','cancelled')")
		placementSnapshot := strings.Contains(statement.SQL, "_decision_placements AS MATERIALIZED")
		if !closedProbe && !placementSnapshot && !strings.Contains(statement.SQL, "_work_candidates") && !strings.Contains(statement.SQL, "pm_records") && !strings.Contains(statement.SQL, " FROM host_keys ") && !strings.Contains(statement.SQL, " FROM documents ") && !strings.Contains(statement.SQL, "handle = ? AND handle IS NOT NULL") && !strings.Contains(statement.SQL, "idx_agents_admin_page") {
			continue
		}
		rows, err := db.Query("EXPLAIN QUERY PLAN "+statement.SQL, statement.Args...)
		if err != nil {
			t.Fatal(err)
		}
		details := []string{}
		for rows.Next() {
			var a, b, c int
			var detail string
			if err := rows.Scan(&a, &b, &c, &detail); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			details = append(details, detail)
		}
		err = rows.Err()
		rows.Close()
		if closedProbe && (!strings.Contains(strings.Join(details, " "), "idx_cards_closed_probe") || !strings.Contains(strings.Join(details, " "), "idx_work_metadata_external")) {
			t.Fatalf("closed superset probe lost sparse indexes: %v", details)
		}
		if placementSnapshot && !strings.Contains(strings.Join(details, " "), "idx_ref_edges_work_placement") {
			t.Fatalf("PM snapshot placement lost its indexed target lookup: %v", details)
		}
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("actual read plan (%d rows): %s", statement.Rows, strings.Join(details, "; "))
	}
}
