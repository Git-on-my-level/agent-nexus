package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	"agent-nexus-core/internal/secrets"
	"agent-nexus-core/internal/series"
	"agent-nexus-core/internal/testutil/perfguard"
)

type routeBudget struct {
	Method                  string          `json:"method"`
	Path                    string          `json:"path"`
	Query                   string          `json:"query,omitempty"`
	P95MS                   int             `json:"p95_ms"`
	MaxQueries              int             `json:"max_queries"`
	MaxRows                 int             `json:"max_rows"`
	AuthorizedStatus        int             `json:"authorized_status"`
	UnauthorizedStatus      int             `json:"unauthorized_status"`
	Body                    json.RawMessage `json:"body,omitempty"`
	MustContain             string          `json:"must_contain,omitempty"`
	UnauthorizedMustContain string          `json:"unauthorized_must_contain,omitempty"`
	AuthorizedAs            string          `json:"authorized_as,omitempty"`
	Reason                  string          `json:"reason"`
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
		rowCeiling := 1024
		if b.Method == http.MethodGet && (b.Path == "/series/{name}" || b.Path == "/series/{name}/query") {
			rowCeiling = series.MaxRawQueryPoints + 512 // fixed API cap plus scoped metadata
		}
		if b.P95MS <= 0 || b.P95MS > 500 || b.MaxQueries <= 0 || b.MaxQueries > 100 || b.MaxRows <= 0 || b.MaxRows > rowCeiling || b.AuthorizedStatus != 200 || (b.UnauthorizedStatus != 200 && b.UnauthorizedStatus != 403 && b.UnauthorizedStatus != 404 && !(b.Path == "/auth/hosts/enrollments/{enrollment_id}" && b.UnauthorizedStatus == 401)) || b.Reason == "" || (b.AuthorizedAs != "" && b.AuthorizedAs != "agent") {
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
	return method == http.MethodPost && (path == "/refs/resolve" || path == "/reports/preview" || path == "/pm/turns/{turn_id}/context" || path == "/secrets/{secret_id}/reveal" || path == "/secrets/reveal-batch")
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
	store            *primitives.Store
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
	// Unrelated private controls retain the hotfix's leak sentinel.
	for i := 0; i < 10; i++ {
		b, err := store.CreateBoard(ctx, "scale-private-control-owner", map[string]any{"title": "Private control"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = store.PatchThread(ctx, "scale-private-control-owner", anyString(b["thread_id"]), map[string]any{"pm_actor_id": "scale-private-control-owner"}, nil); err != nil {
			t.Fatal(err)
		}
		if _, err = store.CreateBoardCard(ctx, "scale-private-control-owner", anyString(b["id"]), primitives.AddBoardCardInput{Title: "PrivatePerformanceSecret", ColumnKey: "ready"}); err != nil {
			t.Fatal(err)
		}
	}
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
	encryptor, err := secrets.NewEncryptor(strings.Repeat("11", 32), "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	secretStore := secrets.NewStore(observed, encryptor)
	secret, err := secretStore.Create(ctx, secrets.CreateSecretInput{Name: "scale-secret", Value: "SyntheticScaleSecret", ActorID: owner.ActorID})
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
	var configured *handlerOptions
	handler := NewHandler("scale-test", WithPrimitiveStore(ps), WithAuthStore(as), WithSecretsStore(secretStore), WithActorRegistry(actors.NewStore(observed)), WithSchemaContract(contract), WithHealthCheck(env.workspace.Ping), WithPMRuntime(runtime), WithSeriesStore(&series.Store{DB: observed, Auth: as}), WithRunStore(commandcenter.NewStore(observed, commandcenter.SQLIdentities{DB: observed})), WithStreamPollInterval(5*time.Millisecond), func(o *handlerOptions) { configured = o })
	return performanceEnv{store: store, agent: agent, handler: handler, db: db, capture: capture, principals: []lockoutPrincipalSeed{owner, stranger}, hub: configured.agentChanges, documentRevision: anyString(doc["head_revision_id"]), replace: strings.NewReplacer(
		"{secret_id}", secret.ID, "{board_id}", boardID, "{card_id}", cardID, "{card_ref}", "card:"+cardID, "{thread_id}", threadID, "{document_id}", anyString(doc["id"]), "{revision_id}", strings.TrimPrefix(anyString(card.Card["head_revision_ref"]), "card_revision:"), "{event_id}", anyString(event["id"]), "{artifact_id}", anyString(artifact["id"]), "{topic_id}", anyString(topic.Topic["id"]), "{inbox_id}", "scale-target-inbox", "{host_id}", "scale-host", "{agent_id}", "scale-owner", "{conversation_id}", conversation.ID, "{decision_id}", "scale-target-decision", "{action_id}", "scale-target-action", "{turn_id}", "scale-target-turn", "{name}", "scale.series", "{command_id}", "work.list", "{concept_name}", "cards", "{session_id}", session.SessionID, "{enrollment_id}", "scale-target-enrollment", "{run_id}", "scale-target-run")}
}

type performanceWriter struct {
	*httptest.ResponseRecorder
	cancel  context.CancelFunc
	flushes []time.Time
	onFlush func()
	closed  bool
}

func (w *performanceWriter) Write(b []byte) (int, error) {
	if w.closed {
		return 0, context.Canceled
	}
	return w.ResponseRecorder.Write(b)
}
func (w *performanceWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }
func (w *performanceWriter) Flush() {
	if w.closed {
		return
	}
	w.ResponseRecorder.Flush()
	w.flushes = append(w.flushes, time.Now())
	if len(w.flushes) == 2 && w.onFlush != nil {
		w.onFlush()
	}
	if len(w.flushes) >= 3 {
		w.closed = true
		w.cancel()
	}
}

func TestPerformanceRoutes(t *testing.T) {
	requirePerformanceTest(t)
	budgets := performanceBudgets(t)
	diagnostic := os.Getenv("ANX_PERFORMANCE_DIAGNOSTIC") == "1"
	if diagnostic {
		t.Error("diagnostic sampling is not an acceptance run")
	}
	selector := os.Getenv("ANX_PERFORMANCE_DIAGNOSTIC_ROUTE")
	if selector != "" {
		if !diagnostic {
			t.Fatal("a diagnostic route selector cannot skip acceptance coverage")
		}
		found := false
		for _, b := range budgets {
			found = found || selector == b.Method+" "+b.Path
		}
		if !found {
			t.Fatal("unknown diagnostic route selector")
		}
	}
	reportPath := os.Getenv("ANX_PERFORMANCE_REPORT")
	if reportPath != "" {
		if !filepath.IsAbs(reportPath) {
			t.Fatal("ANX_PERFORMANCE_REPORT must be an absolute path (go test runs in the package directory)")
		}
		if err := os.MkdirAll(filepath.Dir(reportPath), 0755); err != nil {
			t.Fatal(err)
		}
	}
	allowed := performancePlanExceptions(t)
	baseline := performanceBaselineBudgets(t, budgets) // expire pins before the costly corpus
	env := newPerformanceEnv(t)                        // cached once for every route, principal and sample
	large, err := perfguard.LargeTables(context.Background(), env.db)
	if err != nil {
		t.Fatal(err)
	}
	functions, err := perfguard.CustomFunctions(context.Background(), env.db)
	if err != nil {
		t.Fatal(err)
	}
	checked := map[string]bool{}
	reported := map[string]bool{}
	report := struct {
		Plans   map[string]map[string]any `json:"plans"`
		Samples []map[string]any          `json:"samples"`
	}{Plans: map[string]map[string]any{}}
	persistReport := func() {
		if path := reportPath; path != "" {
			b, err := json.MarshalIndent(report, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, b, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	defer persistReport()

	for pi, principal := range env.principals {
		label := "authorized"
		if pi == 1 {
			label = "unauthorized"
		}
		for _, b := range budgets {
			if selector != "" && selector != b.Method+" "+b.Path {
				continue
			}
			t.Run(label+"/"+b.Method+" "+b.Path, func(t *testing.T) {
				defer persistReport()
				slowBaseline := false
				if e, ok := baseline[b.Method+" "+b.Path+" "+label]; ok {
					slowBaseline = e.P95MS >= 10000
					b.P95MS = e.P95MS
					b.MaxQueries = e.MaxQueries
					b.MaxRows = e.MaxRows
					t.Logf("existing baseline exception %s: %s", e.Issue, e.Reason)
				}
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
				samples := 4 // one warmup and three measured requests
				if slowBaseline {
					samples = 2 // both measured: include the first/cold request
				}
				if diagnostic {
					samples = 1
				}
				for sample := 0; sample < samples; sample++ {
					deadlineBudget := time.Duration(b.P95MS) * time.Millisecond * 2
					if diagnostic {
						deadlineBudget = 10 * time.Minute
					}
					ctx, cancel := context.WithTimeout(context.Background(), deadlineBudget)
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
					report.Samples = append(report.Samples, map[string]any{"method": b.Method, "path": b.Path, "principal": label, "sample": sample, "elapsed_ms": float64(elapsed) / float64(time.Millisecond), "queries": queries, "rows": rows, "status": w.Code})
					deadline := ctx.Err() == context.DeadlineExceeded
					cancel()
					if sample > 0 || diagnostic || slowBaseline {
						durations = append(durations, elapsed)
					}
					for _, s := range statements {
						// EXPLAIN does not execute SQL. Retain every typed argument
						// variant for STAT4-dependent plans while deduplicating repeats;
						// execution and row counts still include every call.
						key := perfguard.PlanMemoKey(s)
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
						findings := perfguard.Findings(s.SQL, plan, large, functions)
						hash := perfguard.PlanSQLHash(s.SQL)
						planHash := perfguard.PlanHash(plan)
						reportKey := hash + "/" + planHash
						if len(findings) > 0 {
							entry := report.Plans[reportKey]
							if entry == nil {
								entry = map[string]any{"sql": s.SQL, "sql_sha256": hash, "plan_sha256": planHash, "plan": plan, "method": b.Method, "path": b.Path, "principal": label, "findings": map[string]bool{}}
								report.Plans[reportKey] = entry
							}
							for _, f := range findings {
								entry["findings"].(map[string]bool)[f] = true
							}
						}
						for _, finding := range findings {
							hash := perfguard.PlanSQLHash(s.SQL)
							if !allowed[hash+"\n"+planHash+"\n"+finding] && !reported[hash+"\n"+planHash+"\n"+finding] {
								reported[hash+"\n"+planHash+"\n"+finding] = true
								t.Errorf("unreviewed query plan: sql_sha256=%s plan_sha256=%s finding=%q", hash, planHash, finding)
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
					if pi == 1 && b.UnauthorizedMustContain != "" && !strings.Contains(w.Body.String(), env.replace.Replace(b.UnauthorizedMustContain)) {
						t.Errorf("denied collection omitted expected filtered result %q", b.UnauthorizedMustContain)
					}
					if strings.HasPrefix(b.Path, "/stream/") && strings.Contains(w.Body.String(), "event: error") {
						t.Errorf("stream returned an error event: %.500s", w.Body.String())
					}
					if want == 200 && strings.HasPrefix(b.Path, "/stream/") && len(w.flushes) < 3 {
						t.Fatal("stream did not exercise a second poll/invalidation")
					}
					if pi == 1 && want == 200 {
						if strings.Contains(w.Body.String(), "Private scale target") {
							t.Error("unauthorized response exposed private target title")
						}
						for _, selector := range []string{"{board_id}", "{card_id}", "{document_id}", "{artifact_id}", "{event_id}", "{thread_id}", "{topic_id}", "{conversation_id}", "{decision_id}", "{action_id}", "{turn_id}"} {
							if strings.Contains(w.Body.String(), env.replace.Replace(selector)) {
								t.Errorf("unauthorized response exposed private target %s", selector)
							}
						}
					}
					if !(pi == 0 && b.AuthorizedAs == "agent") && strings.Contains(w.Body.String(), "PrivatePerformanceSecret") {
						t.Error("unrelated private control leaked")
					}
					if queries > b.MaxQueries || rows > b.MaxRows {
						t.Errorf("unbounded read: SQL=%d (budget %d), rows=%d (budget %d)", queries, b.MaxQueries, rows, b.MaxRows)
					}
				}
				sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
				p95 := durations[len(durations)-1] // nearest-rank p95 is max for N=2 or N=3
				t.Logf("p95=%v measured_samples=%d budget=%dms", p95, len(durations), b.P95MS)
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
	if n, err := w.WriteString("event: error\ndata: disconnected\n\n"); n != 0 || err != context.Canceled || strings.Contains(w.Body.String(), "event: error") {
		t.Fatal("closed client accepted a post-disconnect event")
	}
	if ctx.Err() != context.Canceled || len(w.flushes) != 3 {
		t.Fatal("second poll was not measured")
	}
}
