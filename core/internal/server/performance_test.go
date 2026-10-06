package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/commandcenter"
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
	"agent-nexus-core/internal/series"
	"agent-nexus-core/internal/testutil/perfguard"
)

type routeBudget struct {
	Method             string          `json:"method"`
	Path               string          `json:"path"`
	Query              string          `json:"query,omitempty"`
	P95MS              int             `json:"p95_ms"`
	MaxQueries         int             `json:"max_queries"`
	MaxRows            int             `json:"max_rows"`
	AuthorizedStatus   int             `json:"authorized_status"`
	UnauthorizedStatus int             `json:"unauthorized_status"`
	Body               json.RawMessage `json:"body,omitempty"`
	MustContain        string          `json:"must_contain,omitempty"`
	AuthorizedAs       string          `json:"authorized_as,omitempty"`
	Reason             string          `json:"reason"`
}

func performanceBudgets(t *testing.T) []routeBudget {
	t.Helper()
	raw, err := os.ReadFile("testdata/performance_routes.json")
	if err != nil {
		t.Fatal(err)
	}
	var budgets []routeBudget
	if err := json.Unmarshal(raw, &budgets); err != nil {
		t.Fatal(err)
	}
	covered := map[string]bool{}
	for _, b := range budgets {
		key := b.Method + " " + b.Path
		if covered[key] {
			t.Errorf("duplicate route budget: %s", key)
		}
		covered[key] = true
		if b.P95MS <= 0 || b.P95MS > 1000 || b.MaxQueries <= 0 || b.MaxRows <= 0 || b.AuthorizedStatus != 200 || (b.UnauthorizedStatus != 200 && b.UnauthorizedStatus != 403 && b.UnauthorizedStatus != 404 && !(b.Path == "/auth/hosts/enrollments/{enrollment_id}" && b.UnauthorizedStatus == 401)) || b.Reason == "" || (b.AuthorizedAs != "" && b.AuthorizedAs != "agent") {
			t.Errorf("incomplete or excessive route budget: %s", key)
		}
	}

	raw, err = os.ReadFile("testdata/performance_write_routes.json")
	if err != nil {
		t.Fatal(err)
	}
	var writes []routeBudget
	if err := json.Unmarshal(raw, &writes); err != nil {
		t.Fatal(err)
	}
	for _, w := range writes {
		key := w.Method + " " + w.Path
		if covered[key] || w.Method == http.MethodGet || performanceReadPOST(w.Method, w.Path) || w.Reason == "" {
			t.Errorf("invalid read/write classification: %s", key)
		}
		covered[key] = true
	}
	for _, p := range loadPrivacyRouteMatrix(t) {
		key := p.Method + " " + p.Path
		if !covered[key] {
			t.Errorf("new route needs a performance budget or explicit write classification: %s", key)
		}
		delete(covered, key)
	}
	for key := range covered {
		t.Errorf("stale route budget: %s", key)
	}
	return budgets
}

func performanceReadPOST(method, path string) bool {
	return method == http.MethodPost && (path == "/refs/resolve" || path == "/reports/preview" || path == "/pm/turns/{turn_id}/context")
}

func TestPerformanceRouteInventory(t *testing.T) { performanceBudgets(t) }

// This suite is explicitly selected by the performance CI job, so it neither
// duplicates core's full integration job nor enters the pre-push short tier.
func requirePerformanceTest(t *testing.T) {
	t.Helper()
	if testing.Short() || os.Getenv("ANX_PERFORMANCE_TEST") != "1" {
		t.Skip("run ANX_PERFORMANCE_TEST=1 go test -run TestPerformance ./internal/server")
	}
}

type performanceEnv struct {
	handler          http.Handler
	db               *sql.DB // uninstrumented EXPLAIN connection
	capture          *perfguard.Capture
	principals       []lockoutPrincipalSeed
	replace          *strings.Replacer
	hub              *agentChangeHub
	agent            lockoutPrincipalSeed
	documentRevision string
}

func newPerformanceEnv(t *testing.T) performanceEnv {
	t.Helper()
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, db, "scale-owner", "scale-owner-actor", "scale.owner", "scale-owner-token")
	agent := seedMachinePrincipalForLockoutTest(t, ctx, db, "scale-authorized", "scale-authorized-actor", "scale.authorized", "scale-authorized-token")
	stranger := seedMachinePrincipalForLockoutTest(t, ctx, db, "scale-stranger", "scale-stranger-actor", "scale.stranger", "scale-stranger-token")
	store := env.primitiveStore.(*primitives.Store)
	// Real API writes provide valid point selectors. Bulk synthetic rows then
	// supply the unrelated corpus; fixture setup is outside all request budgets.
	board, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Private scale target"})
	if err != nil {
		t.Fatal(err)
	}
	boardID := anyString(board["id"])
	if _, err := store.PatchThread(ctx, owner.ActorID, anyString(board["thread_id"]), map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	card, err := store.CreateBoardCard(ctx, owner.ActorID, boardID, primitives.AddBoardCardInput{Title: "Private scale target", ColumnKey: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	cardID, threadID := anyString(card.Card["id"]), anyString(card.Card["thread_id"])
	doc, _, err := store.CreateDocument(ctx, owner.ActorID, map[string]any{"title": "Scale report"}, map[string]any{"kind": "anx.visual-report", "schema_version": 1, "title": "Scale report", "summary": "Scale evidence", "generated_at": "2026-01-01T00:00:00Z", "sources": []any{}, "projects": []any{map[string]any{"id": "workspace", "title": "Workspace", "summary": "Synthetic", "outcome": "Evidence"}}, "panels": []any{map[string]any{"id": "activity", "type": "live-activity", "title": "Activity", "project_id": "workspace", "data": map[string]any{}, "author": "ANX", "provenance": "reported", "observed_at": nil, "freshness": "unavailable", "source_ids": []any{}}}}, "structured", []string{"card:" + cardID})
	if err != nil {
		t.Fatal(err)
	}
	event, err := store.AppendEvent(ctx, owner.ActorID, map[string]any{"type": "message_posted", "thread_id": threadID, "refs": []string{}, "payload": map[string]any{"text": "Scale target"}})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := store.CreateArtifact(ctx, owner.ActorID, map[string]any{"kind": "note", "thread_id": threadID, "refs": []string{}}, map[string]any{"text": "Scale target"}, "structured")
	if err != nil {
		t.Fatal(err)
	}
	topic, err := store.CreateTopic(ctx, owner.ActorID, map[string]any{"title": "Scale target", "summary": "Scale target", "related_refs": []string{"card:" + cardID}})
	if err != nil {
		t.Fatal(err)
	}
	seedStreamPrivacyInbox(t, store, threadID, streamPrivacyInboxItem(threadID, "scale-target-inbox", "Scale target"))
	seedReceiptStreamWakeup(t, store, primitives.AgentWakeup{WakeupID: "scale-target-wakeup", ThreadID: threadID, TargetActorID: owner.ActorID, TargetHandle: owner.Username, Status: primitives.AgentWakeupStatusRequested, TriggerText: "Scale target", Refs: []string{"card:" + cardID}})
	if err := perfguard.Seed(ctx, db, env.workspace.Layout().ArtifactContentDir, owner.ActorID, time.Now().UTC().Truncate(time.Hour)); err != nil {
		t.Fatal(err)
	}
	sequence := int64(0)
	session, err := store.UpsertSession(ctx, owner.AgentID, owner.ActorID, primitives.SessionRegistration{Provider: "test", HostScope: "scale-host", NativeSessionID: "synthetic-session", Sequence: &sequence, Activity: "active", Capabilities: primitives.SessionCapabilities{Resume: "unknown", History: "unknown", Logs: "unknown"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO host_enrollments(id,user_code,poll_token_hash,public_key,requested_slug,os_user,hostname,discovered_adapters_json,request_nonce,adoptions_json,requesting_ip,status,created_at,expires_at) VALUES('scale-target-enrollment','SCALE',?,'synthetic','scale-enrollment','test','test','[]','synthetic','[]','127.0.0.1','pending',?,?)`, authHashTokenForLockoutTest("synthetic-enrollment-poll"), perfguard.Timestamp, "2030-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO runs(id,handle,launcher,external_id,host_id,agent_id,adapter,state,liveness,result_collected,labels_json,card_ref,last_observed_at) VALUES('scale-target-run','scale-target-run','test','synthetic','scale-host','scale-agent-0','test','running','alive',0,'[]',?,?)`, "card:"+cardID, perfguard.Timestamp); err != nil {
		t.Fatal(err)
	}

	observed, capture, err := perfguard.Open("file:" + env.workspace.Layout().DatabasePath + "?_pragma=busy_timeout(20000)&_pragma=journal_mode(WAL)&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { observed.Close() })
	ps := primitives.NewStore(observed, blob.NewFilesystemBackend(env.workspace.Layout().ArtifactContentDir), env.workspace.Layout().ArtifactContentDir)
	as := auth.NewStore(observed)
	runtime, err := NewPMRuntime(observed, ps, as, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main", AgentActorID: agent.ActorID}})
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := runtime.Service.CreateConversation(ctx, pm.Principal{WorkspaceID: "ws_main", ActorID: owner.ActorID, Human: true}, pm.CreateConversation{RequestKey: "scale", Title: "Scale target", WorkRef: "card:" + cardID})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for kind, value := range map[string]any{
		"decision": pm.Decision{ID: "scale-target-decision", WorkspaceID: "ws_main", ActorID: owner.ActorID, WorkRef: "card:" + cardID, Status: pm.AwaitingAnswer, Revision: 1, CreatedAt: now},
		"action":   pm.Action{ID: "scale-target-action", WorkspaceID: "ws_main", ActorID: owner.ActorID, WorkRef: "card:" + cardID, DecisionID: "scale-target-decision", Status: pm.Pending},
		"turn":     pm.Turn{ID: "scale-target-turn", ConversationID: conversation.ID, WorkspaceID: "ws_main", ActorID: owner.ActorID, Text: "Scale target", Status: pm.Pending, AgentActorID: agent.ActorID, Deadline: now.Add(time.Hour), LeaseToken: "synthetic-lease", LeaseOwner: agent.ActorID, LeaseExpiresAt: now.Add(time.Hour)},
	} {
		raw, _ := json.Marshal(value)
		var v map[string]any
		_ = json.Unmarshal(raw, &v)
		if _, err := db.ExecContext(ctx, `INSERT INTO pm_records(kind,id,workspace_id,actor_id,parent_id,revision,body) VALUES(?,?,?,?,?,1,?)`, kind, v["id"], "ws_main", owner.ActorID, conversation.ID, raw); err != nil {
			t.Fatal(err)
		}
	}
	contract, err := schema.Load("../../../contracts/anx-schema.yaml")
	if err != nil {
		t.Fatal(err)
	}
	hub := newAgentChangeHub()
	handler := NewHandler("scale-test", WithPrimitiveStore(ps), WithAuthStore(as), WithActorRegistry(actors.NewStore(observed)), WithSchemaContract(contract), WithHealthCheck(env.workspace.Ping), WithPMRuntime(runtime), WithSeriesStore(&series.Store{DB: observed, Auth: as}), WithRunStore(commandcenter.NewStore(observed, commandcenter.SQLIdentities{DB: observed})), WithStreamPollInterval(5*time.Millisecond), func(o *handlerOptions) { o.agentChanges = hub })
	return performanceEnv{agent: agent, handler: handler, db: db, capture: capture, principals: []lockoutPrincipalSeed{owner, stranger}, hub: hub, documentRevision: anyString(doc["head_revision_id"]), replace: strings.NewReplacer(
		"{board_id}", boardID, "{card_id}", cardID, "{card_ref}", "card:"+cardID, "{thread_id}", threadID, "{document_id}", anyString(doc["id"]), "{revision_id}", strings.TrimPrefix(anyString(card.Card["head_revision_ref"]), "card_revision:"), "{event_id}", anyString(event["id"]), "{artifact_id}", anyString(artifact["id"]), "{topic_id}", anyString(topic.Topic["id"]), "{inbox_id}", "scale-target-inbox", "{host_id}", "scale-host", "{agent_id}", "scale-owner", "{conversation_id}", conversation.ID, "{decision_id}", "scale-target-decision", "{action_id}", "scale-target-action", "{turn_id}", "scale-target-turn", "{name}", "scale.series", "{command_id}", "work.list", "{concept_name}", "cards", "{session_id}", session.SessionID, "{enrollment_id}", "scale-target-enrollment", "{run_id}", "scale-target-run")}
}

type performanceWriter struct {
	*httptest.ResponseRecorder
	cancel  context.CancelFunc
	flushes []time.Time
	onFlush func()
}

func (w *performanceWriter) Flush() {
	w.ResponseRecorder.Flush()
	w.flushes = append(w.flushes, time.Now())
	if len(w.flushes) == 2 && w.onFlush != nil {
		w.onFlush()
	}
	if len(w.flushes) >= 3 {
		w.cancel()
	}
}

func TestPerformanceRoutes(t *testing.T) {
	requirePerformanceTest(t)
	budgets := performanceBudgets(t)
	env := newPerformanceEnv(t) // cached once for every route, principal and sample
	large, err := perfguard.LargeTables(context.Background(), env.db)
	if err != nil {
		t.Fatal(err)
	}
	functions, err := perfguard.CustomFunctions(context.Background(), env.db)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/performance_plan_allowlist.json")
	if err != nil {
		t.Fatal(err)
	}
	var exceptions []perfguard.PlanException
	if err := json.Unmarshal(raw, &exceptions); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{}
	for _, e := range exceptions {
		if len(e.SQLHash) != 64 || e.Finding == "" || e.Reason == "" {
			t.Fatal("plan exception needs exact SQL hash, finding and justification")
		}
		allowed[e.SQLHash+"\n"+e.Finding] = true
	}
	checked := map[string]bool{}
	for pi, principal := range env.principals {
		label := "authorized"
		if pi == 1 {
			label = "unauthorized"
		}
		for _, b := range budgets {
			t.Run(label+"/"+b.Method+" "+b.Path, func(t *testing.T) {
				path := env.replace.Replace(b.Path)
				if strings.Contains(path, "{") {
					t.Fatalf("missing selector fixture: %s", path)
				}
				if strings.HasPrefix(b.Path, "/docs/") && strings.HasSuffix(b.Path, "/{revision_id}") {
					path = strings.Replace(path, env.replace.Replace("{revision_id}"), env.documentRevision, 1)
				}
				query := env.replace.Replace(b.Query)
				if query != "" {
					path += "?" + query
				}
				want := b.AuthorizedStatus
				if pi == 1 {
					want = b.UnauthorizedStatus
				}
				var durations []time.Duration
				for sample := 0; sample < 5; sample++ {
					ctx, cancel := context.WithTimeout(context.Background(), time.Duration(b.P95MS)*time.Millisecond*2)
					req := httptest.NewRequest(b.Method, path, bytes.NewBufferString(env.replace.Replace(string(b.Body)))).WithContext(ctx)
					req.Header.Set("Authorization", "Bearer "+principal.AccessToken)
					if pi == 0 && b.AuthorizedAs == "agent" {
						req.Header.Set("Authorization", "Bearer "+env.agent.AccessToken)
					}
					if pi == 0 && b.Path == "/auth/hosts/enrollments/{enrollment_id}" {
						req.Header.Set("X-ANX-Enrollment-Token", "synthetic-enrollment-poll")
					}
					req.Header.Set("Content-Type", "application/json")
					w := &performanceWriter{ResponseRecorder: httptest.NewRecorder(), cancel: cancel}
					if b.Path == "/stream/agents" {
						w.onFlush = env.hub.publish
					}
					env.capture.Start()
					start := time.Now()
					env.handler.ServeHTTP(w, req)
					elapsed := time.Since(start)
					statements, queries, rows := env.capture.Stop()
					deadline := ctx.Err() == context.DeadlineExceeded
					cancel()
					if sample > 0 {
						durations = append(durations, elapsed)
					} // one warmup
					for _, s := range statements {
						// EXPLAIN does not execute SQL. Deduplicate identical SQL shapes
						// and bound arguments across samples, retaining all query counts.
						encoded, _ := json.Marshal(s.Args)
						key := perfguard.SQLHash(s.SQL) + perfguard.SQLHash(string(encoded))
						if checked[key] {
							continue
						}
						checked[key] = true
						explainCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
						plan, err := perfguard.Explain(explainCtx, env.db, s)
						stop()
						if err != nil {
							t.Errorf("EXPLAIN %s: %v", perfguard.SQLHash(s.SQL), err)
							continue
						}
						for _, finding := range perfguard.Findings(s.SQL, plan, large, functions) {
							hash := perfguard.SQLHash(s.SQL)
							if !allowed[hash+"\n"+finding] {
								t.Errorf("unreviewed query plan: sql_sha256=%s finding=%q", hash, finding)
							}
						}
					}
					if deadline {
						t.Fatalf("request exceeded deadline (%v); status=%d SQL=%d rows=%d", elapsed, w.Code, queries, rows)
					}
					if w.Code != want {
						t.Fatalf("fixture returned %d, want %d: %.500s", w.Code, want, w.Body.String())
					}
					if pi == 0 && b.MustContain != "" && !strings.Contains(w.Body.String(), env.replace.Replace(b.MustContain)) {
						t.Errorf("successful response omitted positive fixture %q", b.MustContain)
					}
					if strings.HasPrefix(b.Path, "/stream/") && strings.Contains(w.Body.String(), "event: error") {
						t.Errorf("stream returned an error event: %.500s", w.Body.String())
					}
					if want == 200 && strings.HasPrefix(b.Path, "/stream/") && len(w.flushes) < 3 {
						t.Fatal("stream did not exercise a second poll/invalidation")
					}
					if queries > b.MaxQueries || rows > b.MaxRows {
						t.Errorf("unbounded read: SQL=%d (budget %d), rows=%d (budget %d)", queries, b.MaxQueries, rows, b.MaxRows)
					}
				}
				sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
				p95 := durations[len(durations)-1] // nearest-rank p95 of four measured samples
				t.Logf("p95=%v budget=%dms", p95, b.P95MS)
				if p95 > time.Duration(b.P95MS)*time.Millisecond {
					t.Errorf("p95 %v exceeds %dms", p95, b.P95MS)
				}
			})
		}
	}
}

// Header readiness must not count as either of the two measured stream polls.
func TestPerformanceStreamSampleRequiresTwoDataFlushes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := 0
	w := &performanceWriter{ResponseRecorder: httptest.NewRecorder(), cancel: cancel, onFlush: func() { ticks++ }}
	w.Flush() // prepareSSE headers
	if ctx.Err() != nil || ticks != 0 {
		t.Fatal("header flush counted as a poll")
	}
	w.WriteString("event: snapshot\ndata: {}\n\n")
	w.Flush()
	if ctx.Err() != nil || ticks != 1 {
		t.Fatal("first poll canceled before invalidation")
	}
	w.WriteString("event: snapshot\ndata: {}\n\n")
	w.Flush()
	if ctx.Err() != context.Canceled || len(w.flushes) != 3 {
		t.Fatal("second poll was not measured")
	}
}
