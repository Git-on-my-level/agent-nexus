package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/commandcenter"
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/testsql"
	"agent-nexus-core/internal/testutil/perfguard"
)

// Reproduce payload-derived edge density, not just live-card cardinality.
// All canonical imports pass through the production SQLite write triggers.
func TestPerformanceOverviewDenseAccessWorkspaceLatency(t *testing.T) {
	requirePerformanceTest(t)
	// Serial: performance samples must not compete with parallel fixtures.
	if testing.Short() {
		t.Skip("dense full-stack fixture")
	}
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	reader := seedMachinePrincipalForLockoutTest(t, ctx, env.workspace.DB(), "dense-reader", "dense-reader-actor", "dense-reader", "dense-token")
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
		values := []string{}
		n := 9
		bodyMap := map[string]any{}
		if i%25 != 0 {
			n = 7
			bodyMap["pm_actor_id"] = reader.ActorID
			if i >= 474 {
				bodyMap["pm_actor_id"] = "dense-agent-1-actor"
			}
		}
		for j := 0; j < n; j++ {
			values = append(values, fmt.Sprintf("thread-value-%d-%d", i, j))
		}
		bodyMap["snapshot"] = values
		raw, _ := json.Marshal(bodyMap)
		body := string(raw)
		exec(`INSERT INTO threads(id,handle,updated_at,updated_by,body_json) VALUES(?,?,'2026-01-01T00:00:00Z','fixture',?)`, fmt.Sprintf("dense-thread-%d", i), fmt.Sprintf("dense-thread-%d", i), body)
	}
	exec(`INSERT INTO boards(id,title,thread_id,column_schema_json,created_at,created_by,updated_at,updated_by) VALUES('dense-board','Portfolio','dense-thread-0','[]','now','fixture','now','fixture')`)
	for i := 0; i < 433; i++ {
		id := fmt.Sprintf("dense-card-%d", i)
		title := "Visible dense work"

		exec(`INSERT INTO cards(id,handle,title,thread_id,board_id,created_at,created_by,updated_at,updated_by,head_revision_id) VALUES(?,?,?,?,'dense-board','now','fixture','now','fixture','fixture')`, id, id, title, fmt.Sprintf("dense-thread-%d", i))
	}
	for i := 0; i < 370; i++ {
		values := []string{}
		for j := 0; j < 27; j++ {
			values = append(values, fmt.Sprintf("metadata-value-%d-%d", i, j))
		}
		raw, _ := json.Marshal(map[string]any{"snapshot": values})
		exec(`INSERT INTO work_metadata(card_id,metadata_json,updated_at,updated_by) VALUES(?,?,'now','fixture')`, fmt.Sprintf("dense-card-%d", i), string(raw))
	}

	for i := 0; i < 816; i++ {
		id := fmt.Sprintf("dense-artifact-%d", i)
		prose := 6
		if i < 364 {
			prose++
		}
		body := densePayload(i, prose, 147-5*prose)
		manifest := resourceaccess.ContentReferenceAtomsJSON(body, "structured")
		exec(`INSERT INTO artifacts(id,handle,kind,created_at,created_by,content_type,content_hash,content_refs_json,metadata_json) VALUES(?,?,'note','now','fixture','structured','fixture',?,'{}')`, id, id, manifest)
	}
	for i := 0; i < 1501; i++ {
		exec(`INSERT INTO card_revisions(revision_id,card_id,revision_number,prev_revision_id,artifact_id,created_at,created_by) VALUES(?,?,?,?,?,'now','fixture')`, fmt.Sprintf("dense-revision-%d", i), fmt.Sprintf("dense-card-%d", i%433), i/433+1, fmt.Sprintf("dense-prev-%d", i), fmt.Sprintf("dense-artifact-%d", i%816))
	}
	for i := 0; i < 433; i++ {
		values := []string{}
		n := 4
		if i < 258 {
			n++
		}
		for j := 0; j < n; j++ {
			values = append(values, fmt.Sprintf("card-value-%d-%d", i, j))
		}
		raw, _ := json.Marshal(values)
		exec(`UPDATE cards SET definition_of_done_json=? WHERE id=?`, string(raw), fmt.Sprintf("dense-card-%d", i))
	}

	// Artifacts dominate the supplied production ledger; events are smaller.
	for i := 0; i < 2765; i++ {
		payload := densePayload(i, 2, 19)
		exec(`INSERT INTO events(id,handle,type,thread_id,ts,actor_id,refs_json,payload_json) VALUES(?,?,'message_posted',?,'2026-01-01T00:00:00Z','fixture','[]',?)`, fmt.Sprintf("dense-event-%d", i), fmt.Sprintf("dense-event-%d", i), fmt.Sprintf("dense-thread-%d", i%499), string(payload))
	}
	for i := 0; i < 589; i++ {
		exec(`INSERT INTO work_observations(id,card_id,idempotency_key,digest,observed_at,received_at,status,body_json) VALUES(?,?,?,'fixture','now','now','accepted',?)`, fmt.Sprintf("dense-observation-%d", i), fmt.Sprintf("dense-card-%d", i%370), fmt.Sprint(i), densePayload(i, 1, 33))
	}
	for i := 0; i < 227; i++ {
		exec(`INSERT INTO work_evidence_records(card_id,slot,evidence_json) VALUES(?,?,?)`, fmt.Sprintf("dense-card-%d", i%370), fmt.Sprintf("fixture-%d", i), denseEvidencePayload(i))
	}

	for i := 0; i < 12491; i++ {
		exec(`INSERT INTO ref_edges(id,source_type,source_id,target_type,target_id,edge_type,created_at,metadata_json) VALUES(?,'event',?,'card',?,'ref','now','{}')`, fmt.Sprintf("dense-ref-%d", i), fmt.Sprintf("dense-event-%d", i%2765), fmt.Sprintf("dense-card-%d", i%433))
	}
	for i := 0; i < 3203; i++ {
		exec(`INSERT INTO resource_handle_aliases(resource_type,alias_handle,resource_id,canonical_handle,created_at) VALUES('card',?,?,?,'now')`, fmt.Sprintf("historical-dense-%d", i), fmt.Sprintf("dense-card-%d", i%433), fmt.Sprintf("dense-card-%d", i%433))
	}
	// Historical credentials are indexed authentication state, not payload edges.
	for i := 25; i < 16262; i++ {
		exec(`INSERT INTO auth_access_tokens(id,agent_id,token_hash,created_at,expires_at) VALUES(?,?,?,'2026-01-01T00:00:00Z','2099-01-01T00:00:00Z')`, fmt.Sprintf("dense-token-%d", i), "dense-reader", fmt.Sprintf("synthetic-token-hash-%d", i))
	}
	for i := 0; i < 14997; i++ {
		exec(`INSERT INTO auth_refresh_sessions(id,agent_id,token_hash,created_at,expires_at) VALUES(?,?,?,'2026-01-01T00:00:00Z','2099-01-01T00:00:00Z')`, fmt.Sprintf("dense-refresh-%d", i), "dense-reader", fmt.Sprintf("synthetic-refresh-hash-%d", i))
	}
	for i := 0; i < 3126; i++ {
		exec(`INSERT INTO idempotency_replays(scope,actor_id,request_key,request_hash,response_status,response_json) VALUES('fixture','fixture',?,'fixture',200,'{}')`, fmt.Sprint(i))
	}
	for i := 0; i < 959; i++ {
		exec(`INSERT INTO work_evidence_index(card_id,lookup_key,evidence_id) SELECT card_id,?,id FROM work_evidence_records WHERE slot=?`, fmt.Sprintf("card:dense-card-%d", i%433), fmt.Sprintf("fixture-%d", i%227))
	}
	for i := 0; i < 1280; i++ {
		exec(`INSERT INTO inbox_hidden_subject_refs(owner_kind,owner_id,ref) VALUES('card',?,?)`, fmt.Sprintf("dense-card-%d", i%433), fmt.Sprintf("card:historical-hidden-%d", i))
	}
	// Live projection payloads matter even with only twelve inbox rows: freshness
	// queries filter these through the same scoped relation as payload readers.
	for i := 0; i < 499; i++ {
		exec(`INSERT INTO derived_topic_views(thread_id,generated_at,data_json) VALUES(?,'now',?)`, fmt.Sprintf("dense-thread-%d", i), densePayload(i, 2, 20))
	}
	for i := 0; i < 12; i++ {
		exec(`INSERT INTO derived_inbox_items(id,thread_id,category,trigger_at,generated_at,data_json) VALUES(?,'dense-thread-0','ask','2026-01-01T00:00:00Z','now','{}')`, fmt.Sprintf("dense-inbox-%d", i))
	}
	// A thirteenth stored item belongs to another principal's thread. Keeping
	// twelve visible items must depend on authorization, not an empty fixture.
	exec(`INSERT INTO derived_inbox_items(id,thread_id,category,trigger_at,generated_at,data_json) VALUES('private-dense-inbox','dense-thread-474','ask','2026-01-01T00:00:00Z','now','{"title":"PrivateDenseSecret"}')`)
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var privateControls int
	if err := env.workspace.DB().QueryRow(`SELECT count(*) FROM derived_inbox_items i JOIN threads t ON t.id=i.thread_id WHERE i.id='private-dense-inbox' AND json_extract(i.data_json,'$.title')='PrivateDenseSecret' AND json_extract(t.body_json,'$.pm_actor_id')=?`, "dense-agent-1-actor").Scan(&privateControls); err != nil || privateControls != 1 {
		t.Fatalf("missing private inbox control: count=%d err=%v", privateControls, err)
	}
	for _, table := range []string{"resource_access_edges", "resource_access_exact_edges", "resource_access_mention_buckets", "resource_access_mentions", "resource_access_external_edges", "resource_access_identities", "ref_edges", "events", "threads", "cards", "artifacts", "actors", "agents", "derived_inbox_items", "work_observations", "work_metadata", "work_evidence_records", "auth_access_tokens", "auth_refresh_sessions", "inbox_hidden_subject_refs", "work_evidence_index", "idempotency_replays", "derived_topic_views"} {
		var n int
		if err = env.workspace.DB().QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		t.Logf("shape %s=%d", table, n)
		for target, want := range map[string]int{"resource_access_mention_buckets": 130709, "resource_access_external_edges": 34797, "resource_access_identities": 11726} {
			if table == target && (n < want*98/100 || n > want*102/100) {
				t.Fatalf("%s count=%d outside 2%% of %d", table, n, want)
			}
		}
		if table == "resource_access_mentions" && (n < 17037*95/100 || n > 17037*105/100) {
			t.Fatalf("mention shape %d outside 5%% of 17037", n)
		}
		for target, want := range map[string]int{"auth_access_tokens": 16262, "auth_refresh_sessions": 14997, "inbox_hidden_subject_refs": 1280, "work_evidence_index": 959, "idempotency_replays": 3126, "ref_edges": 12491, "events": 2765, "threads": 499, "cards": 433, "artifacts": 816, "derived_topic_views": 499} {
			if table == target && n != want {
				t.Fatalf("%s=%d want %d", table, n, want)
			}
		}
		if table == "resource_access_edges" && (n < 248867*98/100 || n > 248867*102/100) {
			t.Fatalf("total edge shape=%d outside 2%% of 248867", n)
		}
	}
	// Cardinality/work bounds remain fatal; elapsed time is advisory on shared hosts.
	for kind, want := range map[string]int{"artifact": 121024, "event": 78024, "work_observation": 22144, "work_metadata": 11180, "work_evidence_record": 6810, "thread": 5017, "card_revision": 3002, "card": 2423} {
		var n int
		if err := env.workspace.DB().QueryRow(`SELECT count(*) FROM resource_access_edges WHERE source_kind=?`, kind).Scan(&n); err != nil {
			t.Fatal(err)
		}
		t.Logf("source_kind=%s edges=%d production=%d", kind, n, want)
		if n < want*98/100 || n > want*102/100 {
			t.Fatalf("%s edge shape %d outside 2%% of %d", kind, n, want)
		}
	}

	dsn := "file:" + env.workspace.Layout().DatabasePath
	workDB, work, err := perfguard.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { workDB.Close() })
	db, counter := testsql.OpenDriver(dsn, workDB.Driver())
	t.Cleanup(func() { db.Close() })
	store := primitives.NewTestStore(db, env.workspace.Layout().ArtifactContentDir)
	authStore := auth.NewStore(db)
	runtime, err := newOnboardedPMRuntime(t, db, store, authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewHandler("test", WithPrimitiveStore(store), WithAuthStore(authStore), WithActorRegistry(actors.NewStore(db)), WithRunStore(commandcenter.NewStore(db, commandcenter.SQLIdentities{DB: db})), WithPMRuntime(runtime)))
	t.Cleanup(server.Close)
	for _, mode := range []string{"legacy", "scoped", "amplified"} {
		if mode == "amplified" {
			// Separate upper envelope: 300 rows per artifact in EACH exact ledger,
			// rather than about 300 combined across the two production-sized ledgers.
			tx, err := env.workspace.DB().BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 816; i++ {
				manifest := resourceaccess.ContentReferenceAtomsJSON(densePayload(i, 6, 299-5*6), "structured")
				if _, err = tx.ExecContext(ctx, `UPDATE artifacts SET content_refs_json=? WHERE id=?`, manifest, fmt.Sprintf("dense-artifact-%d", i)); err != nil {
					tx.Rollback()
					t.Fatal(err)
				}
			}
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
			var n int
			if err = env.workspace.DB().QueryRow(`SELECT count(*) FROM resource_access_edges WHERE source_kind='artifact'`).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 816*300 {
				t.Fatalf("amplified artifact edges=%d want %d", n, 816*300)
			}
			t.Logf("amplified artifact edges=%d", n)
		}
		if mode == "scoped" {
			for done := false; !done; {
				done, err = env.workspace.MaintainScopeInboxBatch(ctx)
				if err != nil {
					t.Fatal(err)
				}
			}
			primitives.WithScopedInboxReader(true)(store)
		}
		t.Run(mode, func(t *testing.T) {
			for _, path := range []string{"/inbox", "/overview", "/overview?work_view=summary"} {
				t.Run(path, func(t *testing.T) {
					var samples []time.Duration
					for i := 0; i < 7; i++ {
						if i == 6 {
							// Measure epoch invalidation separately from steady-state p95.
							if _, err := env.workspace.DB().Exec(`UPDATE resource_access_epoch SET version=version+1`); err != nil {
								t.Fatal(err)
							}
							t.Log("authorization epoch invalidated before this request")
						}
						counter.Reset()
						work.Start()
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
						work.Stop()
						if err := work.WorkError(); err != nil {
							t.Fatal(err)
						}
						stats := work.Work()
						t.Logf("work vm_steps=%d full_scan_steps=%d", stats.VMSteps, stats.FullScanSteps)
						if stats.VMSteps > 2000000 {
							t.Fatalf("unbounded SQLite work: %d VM steps", stats.VMSteps)
						}
						if err != nil || resp.StatusCode != 200 {
							t.Fatalf("status=%d err=%v body=%s", resp.StatusCode, err, body)
						}
						if counter.Count() > 32 || counter.ReturnedRows() > 350 {
							t.Fatalf("unbounded handler work: statements=%d rows=%d", counter.Count(), counter.ReturnedRows())
						}

						var payload map[string]any
						if err := json.Unmarshal(body, &payload); err != nil {
							t.Fatal(err)
						}
						if path == "/inbox" && len(payload["items"].([]any)) != 12 {
							t.Fatal("fixture lost visible inbox items")
						}
						if strings.HasPrefix(path, "/overview") && len(payload["work"].(map[string]any)["items"].([]any)) != 100 {
							t.Fatal("fixture lost visible work window")
						}
						logRequestSQLTiming(t, counter, elapsed, len(body), strings.Join(resp.Header.Values("Server-Timing"), ", "))
						if i == 0 || i == 2 {
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
						assertOverviewPrivatePayloadExcluded(t, body)
						if i > 1 && i < 6 {
							samples = append(samples, elapsed)
						}
					}
					sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
					t.Logf("warm median=%s", (samples[1]+samples[2])/2)
					p95 := performanceP95(samples)
					t.Logf("p95=%s", p95)
					if p95 > 300*time.Millisecond {
						t.Logf("advisory: dense workspace p95 exceeds 300ms: %s", p95)
					}
				})
			}
		})
	}
}

// Use synthetic content through the same body-to-manifest extractor as blob writers.
func densePayload(source, prose, scalars int) string {
	values := []string{}
	for j := 0; j < prose; j++ {
		values = append(values, fmt.Sprintf("Evidence for card:dense-card-%d.", (source+j)%433))
	}
	for j := 0; j < scalars; j++ {
		values = append(values, fmt.Sprintf("snapshot-value-%d-%d", source, j))
	}
	raw, _ := json.Marshal(map[string]any{"snapshot": values})
	return string(raw)
}

func denseEvidencePayload(source int) string {
	values := []string{}
	for j := 0; j < 29; j++ {
		values = append(values, fmt.Sprintf("card:dense-card-%d", (source+j)%433))
	}
	raw, _ := json.Marshal(map[string]any{"aliases": values})
	return string(raw)
}

// Keep payload disclosure and epoch invalidation in the gate without the ledger scale.
func TestOverviewPrivatePayloadExcludedAcrossEpochsAndInboxReaders(t *testing.T) {
	t.Parallel()
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	reader := seedMachinePrincipalForLockoutTest(t, ctx, env.workspace.DB(), "dense-reader", "dense-reader-actor", "dense-reader", "dense-token")
	store := env.primitiveStore.(*primitives.Store)
	board, err := store.CreateBoard(ctx, reader.ActorID, map[string]any{"title": "Public portfolio"})
	if err != nil {
		t.Fatal(err)
	}
	public := anyString(board["thread_id"])
	if _, err = store.CreateWork(ctx, reader.ActorID, anyString(board["id"]), map[string]any{"title": "Visible work"}); err != nil {
		t.Fatal(err)
	}
	private := seedStreamPrivacyThread(t, store, "private-owner", true)
	items := make([]primitives.DerivedInboxItem, 0, 32)
	for i := 0; i < 32; i++ {
		item := streamPrivacyInboxItem(private, fmt.Sprintf("private-dense-inbox-%d", i), "PrivateDenseSecret")
		item.Data["title"] = "PrivateDenseSecret"
		items = append(items, item)
	}
	seedStreamPrivacyInbox(t, store, private, items...)
	items = nil
	for i := 0; i < 4; i++ {
		items = append(items, streamPrivacyInboxItem(public, fmt.Sprintf("visible-dense-inbox-%d", i), "Public ask"))
	}
	seedStreamPrivacyInbox(t, store, public, items...)
	assertPrivateControls := func(t *testing.T) {
		t.Helper()
		var privateControls int
		if err := env.workspace.DB().QueryRow(`SELECT count(*) FROM derived_inbox_items WHERE thread_id=? AND json_extract(data_json,'$.title')='PrivateDenseSecret'`, private).Scan(&privateControls); err != nil || privateControls != 32 {
			t.Fatalf("missing private payload controls: %d %v", privateControls, err)
		}
	}
	assertPrivateControls(t)
	runtime, err := newOnboardedPMRuntime(t, env.workspace.DB(), store, env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewHandler("test", WithPrimitiveStore(store), WithAuthStore(env.authStore), WithActorRegistry(env.registry), WithRunStore(commandcenter.NewStore(env.workspace.DB(), commandcenter.SQLIdentities{DB: env.workspace.DB()})), WithPMRuntime(runtime)))
	t.Cleanup(server.Close)
	for _, mode := range []string{"legacy", "scoped"} {
		if mode == "scoped" {
			for done := false; !done; {
				done, err = env.workspace.MaintainScopeInboxBatch(ctx)
				if err != nil {
					t.Fatal(err)
				}
			}
			primitives.WithScopedInboxReader(true)(store)
		}
		t.Run(mode, func(t *testing.T) {
			assertPrivateControls(t)
			for _, invalidated := range []bool{false, true} {
				if invalidated {
					if _, err := env.workspace.DB().Exec(`UPDATE resource_access_epoch SET version=version+1`); err != nil {
						t.Fatal(err)
					}
				}
				for _, path := range []string{"/inbox", "/overview", "/overview?work_view=summary"} {
					req, err := http.NewRequest("GET", server.URL+path, nil)
					if err != nil {
						t.Fatal(err)
					}
					req.Header.Set("Authorization", "Bearer "+reader.AccessToken)
					resp, err := server.Client().Do(req)
					if err != nil {
						t.Fatal(err)
					}
					body, err := io.ReadAll(resp.Body)
					resp.Body.Close()
					if err != nil || resp.StatusCode != http.StatusOK {
						t.Fatalf("%s status=%d body=%s err=%v", path, resp.StatusCode, body, err)
					}
					assertOverviewPrivatePayloadExcluded(t, body)
					var payload map[string]any
					if err := json.Unmarshal(body, &payload); err != nil {
						t.Fatal(err)
					}
					if path == "/inbox" && len(payload["items"].([]any)) != 4 {
						t.Fatalf("lost public inbox items: %s", body)
					}
					if strings.HasPrefix(path, "/overview") && len(payload["work"].(map[string]any)["items"].([]any)) != 1 {
						t.Fatalf("lost public work: %s", body)
					}
				}
			}
		})
	}
}

func assertOverviewPrivatePayloadExcluded(t *testing.T, body []byte) {
	t.Helper()
	if strings.Contains(string(body), "PrivateDenseSecret") || strings.Contains(string(body), "private-dense-inbox") {
		t.Fatal("private work leaked")
	}
}
