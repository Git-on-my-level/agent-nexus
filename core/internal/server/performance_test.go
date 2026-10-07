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
	"runtime"
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
	Case                    string            `json:"case,omitempty"`
	Headers                 map[string]string `json:"headers,omitempty"`
	Scenario                string            `json:"scenario,omitempty"`
	MaxVMSteps              uint64            `json:"max_vm_steps"`
	Method                  string            `json:"method"`
	Path                    string            `json:"path"`
	Query                   string            `json:"query,omitempty"`
	LatencyMS               int               `json:"latency_ms"`
	MaxQueries              int               `json:"max_queries"`
	MaxRows                 int               `json:"max_rows"`
	AuthorizedStatus        int               `json:"authorized_status"`
	UnauthorizedStatus      int               `json:"unauthorized_status"`
	Body                    json.RawMessage   `json:"body,omitempty"`
	MustContain             string            `json:"must_contain,omitempty"`
	UnauthorizedMustContain string            `json:"unauthorized_must_contain,omitempty"`
	UnauthorizedExactBody   json.RawMessage   `json:"unauthorized_exact_body,omitempty"`
	AuthorizedAs            string            `json:"authorized_as,omitempty"`
	Reason                  string            `json:"reason"`
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
	cases := map[string]bool{}
	for _, b := range budgets {
		key := b.Method + " " + b.Path
		caseKey := performanceCaseKey(b.Method, b.Path, b.Case)
		if cases[caseKey] {
			t.Errorf("duplicate route case budget: %s", caseKey)
		}
		cases[caseKey] = true
		covered[key] = true
		if len(b.UnauthorizedExactBody) > 0 && (b.Method != http.MethodPost || b.Path != "/refs/resolve" || b.UnauthorizedStatus != http.StatusOK || !performanceExactDenialBody(b.UnauthorizedExactBody, []byte(`{"items":[{"ref":"{card_ref}","resolvable":false}]}`))) {
			t.Errorf("exact echoed-reference denial is restricted to POST /refs/resolve: %s", key)
		}
		rowCeiling := 1024
		if b.Method == http.MethodGet && (b.Path == "/series/{name}" || b.Path == "/series/{name}/query") {
			rowCeiling = series.MaxRawQueryPoints + 512 // fixed API cap plus scoped metadata
		}
		if b.LatencyMS <= 0 || b.LatencyMS > 500 || b.MaxQueries <= 0 || b.MaxQueries > 100 || b.MaxRows <= 0 || b.MaxRows > rowCeiling || b.MaxVMSteps == 0 || b.MaxVMSteps > 50000 || b.AuthorizedStatus != 200 || (b.UnauthorizedStatus != 200 && b.UnauthorizedStatus != 403 && b.UnauthorizedStatus != 404 && !(b.Path == "/auth/hosts/enrollments/{enrollment_id}" && b.UnauthorizedStatus == 401)) || b.Reason == "" || (b.AuthorizedAs != "" && b.AuthorizedAs != "agent") {
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

// Go's JSON response writer uses canonical key ordering. Compare the complete
// denial body, allowing formatting whitespace but rejecting extra/duplicate
// fields, additional references or metadata hidden behind an echoed input ID.
func performanceExactDenialBody(got, want []byte) bool {
	var value any
	if json.Unmarshal(want, &value) != nil {
		return false
	}
	expected, err := json.Marshal(value)
	var actual bytes.Buffer
	return err == nil && json.Compact(&actual, got) == nil && bytes.Equal(actual.Bytes(), expected)
}

func TestPerformanceExactDeniedReferenceEcho(t *testing.T) {
	want := []byte(`{"items":[{"ref":"card:private-input","resolvable":false}]}`)
	for _, tc := range []struct {
		name string
		body string
		ok   bool
	}{
		{"input echo", string(want), true},
		{"whitespace", " { \"items\" : [ { \"ref\" : \"card:private-input\", \"resolvable\" : false } ] } ", true},
		{"metadata", `{"items":[{"ref":"card:private-input","resolvable":false,"title":"Private title"}]}`, false},
		{"additional reference", `{"items":[{"ref":"card:private-input","resolvable":false},{"ref":"card:other-private","resolvable":false}]}`, false},
		{"resolved private input", `{"items":[{"ref":"card:private-input","resolvable":true}]}`, false},
		{"duplicate field", `{"items":[{"ref":"card:other-private","ref":"card:private-input","resolvable":false}]}`, false},
		{"trailing JSON", string(want) + `{}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := performanceExactDenialBody([]byte(tc.body), want); got != tc.ok {
				t.Fatalf("exact denial match = %v, want %v", got, tc.ok)
			}
		})
	}
}

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
	fresh            func(*testing.T) (http.Handler, *perfguard.Capture, *agentChangeHub, func())
	setupHandler     http.Handler
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
	result := performanceEnv{store: store, agent: agent, handler: handler, db: db, capture: capture, principals: []lockoutPrincipalSeed{owner, stranger}, hub: configured.agentChanges, documentRevision: anyString(doc["head_revision_id"]), replace: strings.NewReplacer(
		"{secret_id}", secret.ID, "{board_id}", boardID, "{card_id}", cardID, "{card_ref}", "card:"+cardID, "{thread_id}", threadID, "{document_id}", anyString(doc["id"]), "{revision_id}", strings.TrimPrefix(anyString(card.Card["head_revision_ref"]), "card_revision:"), "{event_id}", anyString(event["id"]), "{artifact_id}", anyString(artifact["id"]), "{topic_id}", anyString(topic.Topic["id"]), "{inbox_id}", "scale-target-inbox", "{host_id}", "scale-host", "{agent_id}", "scale-owner", "{conversation_id}", conversation.ID, "{decision_id}", "scale-target-decision", "{action_id}", "scale-target-action", "{turn_id}", "scale-target-turn", "{name}", "scale.series", "{command_id}", "work.list", "{concept_name}", "cards", "{session_id}", session.SessionID, "{enrollment_id}", "scale-target-enrollment", "{run_id}", "scale-target-run")}
	result.fresh = func(t *testing.T) (http.Handler, *perfguard.Capture, *agentChangeHub, func()) {
		t.Helper()
		pool, capture, err := perfguard.Open("file:" + env.workspace.Layout().DatabasePath + "?_pragma=busy_timeout(20000)&_pragma=journal_mode(WAL)&_txlock=immediate")
		if err != nil {
			t.Fatal(err)
		}
		ps := primitives.NewStore(pool, blob.NewFilesystemBackend(env.workspace.Layout().ArtifactContentDir), env.workspace.Layout().ArtifactContentDir)
		as := auth.NewStore(pool)
		runtime, err := NewPMRuntime(pool, ps, as, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main", AgentActorID: agent.ActorID}})
		if err != nil {
			pool.Close()
			t.Fatal(err)
		}
		var configured *handlerOptions
		h := NewHandler("scale-test", WithPrimitiveStore(ps), WithAuthStore(as), WithSecretsStore(secrets.NewStore(pool, encryptor)), WithActorRegistry(actors.NewStore(pool)), WithSchemaContract(contract), WithHealthCheck(env.workspace.Ping), WithPMRuntime(runtime), WithSeriesStore(&series.Store{DB: pool, Auth: as}), WithRunStore(commandcenter.NewStore(pool, commandcenter.SQLIdentities{DB: pool})), WithStreamPollInterval(5*time.Millisecond), func(o *handlerOptions) { configured = o })
		return h, capture, configured.agentChanges, func() { pool.Close() }
	}
	return result
}

type performanceWriter struct {
	*httptest.ResponseRecorder
	cancel        context.CancelFunc
	flushes       []time.Time
	onFlush       func()
	eventStream   bool
	receiptStream bool
	lastBodyLen   int
	polls         int
	closed        bool
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
	delta := w.Body.String()[w.lastBodyLen:]
	w.lastBodyLen = w.Body.Len()
	header := len(w.flushes) == 0
	w.flushes = append(w.flushes, time.Now())
	// #311 flushes headers, independent comment-only keepalives, and each
	// completed scanner result. An empty result writes no bytes before Flush.
	// Count only the latter; timer activity cannot cancel unfinished reads.
	if header || (w.eventStream && delta != "" && !strings.Contains(delta, "event: ")) || (w.receiptStream && !strings.Contains(delta, "event: notification_receipt\n")) {
		return
	}
	w.polls++
	if w.polls == 1 && w.onFlush != nil {
		w.onFlush()
	}
	if w.polls >= 2 {
		w.closed = true
		w.cancel()
	}
}

func TestPerformanceRoutes(t *testing.T) {
	requirePerformanceTest(t)
	budgets := performanceBudgets(t)
	allBudgets := budgets
	shard, shardCount := performanceShardSelection(t)
	budgets = performanceShardBudgets(t, budgets, shard, shardCount)
	worker, workerCount := performanceWorkerSelection(t, shard)
	budgets = performanceWorkerBudgets(t, allBudgets, budgets, worker, workerCount)
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
			found = found || selector == b.Method+" "+b.Path || selector == performanceCaseKey(b.Method, b.Path, b.Case)
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
	sourceHash, err := performanceRuntimeSourceHash("../../..")
	if err != nil {
		t.Fatal(err)
	}
	stageDir := os.Getenv("ANX_PERFORMANCE_STAGE_BARRIER")
	schedule := performanceStageSchedule{Policy: "independent"}
	ownerPublished := false
	waitOwners := func() {
		deadline := time.Now().Add(20 * time.Minute)
		if testDeadline, ok := t.Deadline(); ok && testDeadline.Before(deadline) {
			deadline = testDeadline
		}
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		defer cancel()
		var err error
		schedule, err = waitPerformanceOwnerStages(ctx, stageDir, sourceHash)
		if err != nil {
			t.Fatal(err)
		}
		for _, stage := range schedule.Owners {
			if !stage.Success {
				t.Errorf("authorized stage failed in worker %d", stage.Worker)
			}
		}
	}
	if stageDir != "" {
		if !filepath.IsAbs(stageDir) || worker < 1 || worker > 3 || selector != "" {
			t.Fatal("stage barrier requires an absolute directory and a complete isolated worker")
		}
		if err := os.MkdirAll(stageDir, 0755); err != nil {
			t.Fatal(err)
		}
		if worker == 3 {
			waitOwners() // Delay fixture setup and its GC as well as measured post reads.
		} else {
			defer func() {
				if !ownerPublished {
					if err := publishPerformanceOwnerStage(stageDir, sourceHash, worker, false); err != nil {
						t.Error(err)
					}
				}
			}()
		}
	}
	allowed := performancePlanExceptions(t)
	baseline := performanceBaselineBudgets(t, allBudgets) // expire pins before the costly corpus
	fixtureStarted := time.Now().UnixNano()
	env := preparePerformanceStreamCases(t, newPerformanceEnv(t), allBudgets) // every shard retains the complete privacy selector set
	for _, selector := range []string{"{board_id}", "{card_id}", "{document_id}", "{artifact_id}", "{event_id}", "{thread_id}", "{topic_id}", "{conversation_id}", "{decision_id}", "{action_id}", "{turn_id}"} {
		if value := env.replace.Replace(selector); value == "" || value == selector {
			t.Fatalf("shard omitted privacy selector %s", selector)
		}
	}
	large, err := perfguard.LargeTables(context.Background(), env.db)
	if err != nil {
		t.Fatal(err)
	}
	functions, err := perfguard.CustomFunctions(context.Background(), env.db)
	if err != nil {
		t.Fatal(err)
	}
	aggregates, err := perfguard.AggregateFunctions(context.Background(), env.db)
	if err != nil {
		t.Fatal(err)
	}
	views, err := perfguard.ViewDefinitions(context.Background(), env.db)
	if err != nil {
		t.Fatal(err)
	}
	checked := map[string]bool{}
	reported := map[string]bool{}
	report := struct {
		SourceHash     string                    `json:"core_source_sha256"`
		Diagnostic     bool                      `json:"diagnostic"`
		Shard          int                       `json:"shard"`
		ShardCount     int                       `json:"shard_count"`
		Worker         int                       `json:"worker"`
		WorkerCount    int                       `json:"worker_count"`
		MaxProcs       int                       `json:"gomaxprocs"`
		Schedule       *performanceStageSchedule `json:"stage_schedule"`
		FixtureStarted int64                     `json:"fixture_started_unix_ns"`
		Completed      []string                  `json:"completed"`
		Plans          map[string]map[string]any `json:"plans"`
		Samples        []map[string]any          `json:"samples"`
	}{SourceHash: sourceHash, Diagnostic: diagnostic, Shard: shard, ShardCount: shardCount, Worker: worker, WorkerCount: workerCount, MaxProcs: runtime.GOMAXPROCS(0), Schedule: &schedule, FixtureStarted: fixtureStarted, Plans: map[string]map[string]any{}}
	persistReport := func() {
		if path := reportPath; path != "" {
			b, err := json.MarshalIndent(report, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path+".new", b, 0600); err != nil {
				t.Fatal(err)
			}
			if err = os.Rename(path+".new", path); err != nil {
				t.Fatal(err)
			}
		}
	}
	defer persistReport()

	ownersSucceeded := true
	for pi, principal := range env.principals {
		label := "authorized"
		if pi == 1 {
			label = "unauthorized"
		}
		for _, b := range budgets {
			if selector != "" && selector != b.Method+" "+b.Path && selector != performanceCaseKey(b.Method, b.Path, b.Case) {
				continue
			}
			routeEnv, routePrincipal := env, principal
			fixturePolicy := "fresh-pool-and-handler-per-case-principal/shared-4096-distinct-thread-corpus"

			passed := t.Run(label+"/"+performanceCaseKey(b.Method, b.Path, b.Case), func(t *testing.T) {
				env, principal := routeEnv, routePrincipal
				defer persistReport()
				env.setupHandler = env.handler
				preparePerformanceVisit(t, env, b, principal, pi)
				h, capture, hub, closePool := env.fresh(t)
				defer closePool()
				env.handler, env.capture, env.hub = h, capture, hub
				original := b
				defer func() {
					if !t.Failed() {
						suffix := ""
						if worker == 3 {
							suffix = " post-invalidation"
						} else if worker != 0 {
							suffix = " first-and-warm"
						}
						report.Completed = append(report.Completed, performanceCaseKey(original.Method, original.Path, original.Case)+" "+label+suffix)
					}
				}()
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
				durations := map[string][]time.Duration{}
				// The first read warms only this subject. Five warm measurements keep
				// the established median policy; cold and invalidated work have their
				// own deterministic ceilings and loose secondary deadlines.
				for sample := 0; sample < 7; sample++ {
					if (worker == 1 || worker == 2) && sample == 6 || worker == 3 && sample > 0 && sample < 6 {
						continue
					}
					phase := "warm"
					if sample == 0 {
						phase = "first_read"
					}
					if sample == 6 {
						phase = "post_invalidation"
					}
					b := original
					if e, ok := baseline[performanceBaselineKey(b.Method, b.Path, b.Case, label, phase)]; ok {
						b.LatencyMS, b.MaxQueries, b.MaxRows, b.MaxVMSteps = e.LatencyMS, e.MaxQueries, e.MaxRows, e.MaxVMSteps
						t.Logf("%s existing baseline exception %s: %s", phase, e.Issue, e.Reason)
					}
					policy := "one-first-read/five-warm/one-post-invalidation"
					if worker != 0 {
						policy = "one-first-read/five-warm/measured-post-preparation/one-post-invalidation"
					}
					deadlineBudget := performanceMaxSample(b.LatencyMS)
					if diagnostic {
						deadlineBudget = 60 * time.Minute // diagnostic evidence only; never an acceptance allowance
					}
					ctx, cancel := context.WithCancel(context.Background())
					req := httptest.NewRequest(b.Method, path, bytes.NewBufferString(env.replace.Replace(string(b.Body)))).WithContext(ctx)
					req.Header.Set("Authorization", "Bearer "+principal.AccessToken)
					if pi == 0 && b.AuthorizedAs == "agent" {
						req.Header.Set("Authorization", "Bearer "+env.agent.AccessToken)
					}
					if pi == 0 && b.Path == "/auth/hosts/enrollments/{enrollment_id}" {
						req.Header.Set("X-ANX-Enrollment-Token", "synthetic-enrollment-poll")
					}
					req.Header.Set("Content-Type", "application/json")
					for name, value := range b.Headers {
						req.Header.Set(name, env.replace.Replace(value))
					}
					w := &performanceWriter{ResponseRecorder: httptest.NewRecorder(), cancel: cancel, eventStream: b.Path == "/stream/events", receiptStream: b.Path == "/stream/agent-notification-receipts"}
					if b.Path == "/stream/agents" {
						w.onFlush = env.hub.publish
					}
					validateStream := configurePerformanceStreamCase(t, env, b, pi, sample, req, w)
					beforeEpoch, afterEpoch := int64(0), int64(0)
					beforeReceiptEpoch, afterReceiptEpoch := int64(0), int64(0)
					if phase == "post_invalidation" {
						if w.receiptStream {
							if err := env.db.QueryRow(`SELECT version FROM receipt_access_epoch WHERE singleton=1`).Scan(&beforeReceiptEpoch); err != nil {
								t.Fatal(err)
							}
						}
						beforeEpoch, afterEpoch = invalidatePerformanceCache(t, env)
						if w.receiptStream {
							if err := env.db.QueryRow(`SELECT version FROM receipt_access_epoch WHERE singleton=1`).Scan(&afterReceiptEpoch); err != nil {
								t.Fatal(err)
							}
							if afterReceiptEpoch <= beforeReceiptEpoch {
								t.Fatal("receipt cache invalidation did not advance its authority epoch")
							}
						}
					}
					cancel()
					ctx, cancel = context.WithTimeout(context.Background(), deadlineBudget)
					req = req.WithContext(ctx)
					w.cancel = cancel
					env.capture.Start()
					start := time.Now()
					env.handler.ServeHTTP(w, req)
					finished := time.Now()
					elapsed := finished.Sub(start)
					statements, queries, rows := env.capture.Stop()
					if w.receiptStream && phase == "warm" && w.Code == http.StatusOK {
						for _, statement := range statements {
							if strings.Contains(statement.SQL, "main.receipt_access_epoch") && strings.Contains(statement.SQL, "json_group_array") && !strings.HasPrefix(strings.TrimSpace(statement.SQL), "WITH RECURSIVE _anx_fresh_denied(") {
								t.Error("warm receipt request rebuilt its authority closure")
							}
						}
					}
					work := env.capture.Work()
					if err := env.capture.WorkError(); err != nil {
						t.Fatalf("SQLite work counters: %v", err)
					}
					deadline := ctx.Err() == context.DeadlineExceeded
					report.Samples = append(report.Samples, map[string]any{"method": b.Method, "path": b.Path, "case": b.Case, "principal": label, "sample": sample, "measured": true, "cache_phase": phase, "invalidation_epoch_before": beforeEpoch, "invalidation_epoch_after": afterEpoch, "sampling_policy": policy, "fixture_policy": fixturePolicy, "request_started_unix_ns": start.UnixNano(), "request_finished_unix_ns": finished.UnixNano(), "elapsed_ms": float64(elapsed) / float64(time.Millisecond), "queries": queries, "rows": rows, "vm_steps": work.VMSteps, "fullscan_steps": work.FullScanSteps, "sorts": work.Sorts, "autoindex_rows": work.AutoIndexRows, "status": w.Code, "deadline_exceeded": deadline, "stream_polls": w.polls})
					cancel()
					lastSample := report.Samples[len(report.Samples)-1]
					lastSample["worker"] = worker
					if worker == 3 && sample == 0 {
						lastSample["cache_phase"] = "post_preparation"
					}
					lastSample["receipt_invalidation_epoch_before"], lastSample["receipt_invalidation_epoch_after"] = beforeReceiptEpoch, afterReceiptEpoch
					durations[phase] = append(durations[phase], elapsed)
					for _, s := range statements {
						// EXPLAIN does not execute SQL. Retain every typed argument
						// variant for STAT4-dependent plans while deduplicating repeats;
						// execution and row counts still include every call.
						key := fixturePolicy + "\x00" + perfguard.PlanMemoKey(s)
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
						analysisSQL := perfguard.AnalysisSQL(s.SQL, views)
						findings := perfguard.Findings(analysisSQL, plan, large, functions, aggregates)
						hash := perfguard.PlanSQLHash(analysisSQL)
						planHash := perfguard.PlanHash(plan)
						reportKey := hash + "/" + planHash
						if len(findings) > 0 {
							entry := report.Plans[reportKey]
							if entry == nil {
								entry = map[string]any{"sql": s.SQL, "analysis_sql": analysisSQL, "sql_sha256": hash, "plan_sha256": planHash, "plan": plan, "method": b.Method, "path": b.Path, "case": b.Case, "principal": label, "findings": map[string]bool{}}
								report.Plans[reportKey] = entry
							}
							for _, f := range findings {
								entry["findings"].(map[string]bool)[f] = true
							}
						}
						for _, finding := range findings {
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
					if validateStream != nil {
						validateStream()
					}
					validatePerformanceVisit(t, b, sample, w.Body.Bytes())
					if pi == 0 && b.MustContain != "" && !strings.Contains(w.Body.String(), env.replace.Replace(b.MustContain)) {
						t.Errorf("successful response omitted positive fixture %q", b.MustContain)
					}
					if pi == 1 && b.UnauthorizedMustContain != "" && !strings.Contains(w.Body.String(), env.replace.Replace(b.UnauthorizedMustContain)) {
						t.Errorf("denied collection omitted expected filtered result %q", b.UnauthorizedMustContain)
					}
					if pi == 1 && len(b.UnauthorizedExactBody) > 0 && !performanceExactDenialBody(w.Body.Bytes(), []byte(env.replace.Replace(string(b.UnauthorizedExactBody)))) {
						t.Errorf("denied reference response must echo only the requested input without metadata: %.500s", w.Body.String())
					}
					if strings.HasPrefix(b.Path, "/stream/") && strings.Contains(w.Body.String(), "event: error") {
						t.Errorf("stream returned an error event: %.500s", w.Body.String())
					}
					if want == 200 && strings.HasPrefix(b.Path, "/stream/") && w.polls != 2 {
						t.Fatal("stream did not exercise a second poll/invalidation")
					}
					if pi == 1 && want == 200 {
						if strings.Contains(w.Body.String(), "Private scale target") {
							t.Error("unauthorized response exposed private target title")
						}
						for _, selector := range []string{"{board_id}", "{card_id}", "{document_id}", "{artifact_id}", "{event_id}", "{thread_id}", "{topic_id}", "{conversation_id}", "{decision_id}", "{action_id}", "{turn_id}"} {
							if len(b.UnauthorizedExactBody) > 0 {
								continue // the complete exact body above permits only the supplied reference
							}
							if strings.Contains(w.Body.String(), env.replace.Replace(selector)) {
								t.Errorf("unauthorized response exposed private target %s", selector)
							}
						}
					}
					if !(pi == 0 && b.AuthorizedAs == "agent") && strings.Contains(w.Body.String(), "PrivatePerformanceSecret") {
						t.Error("unrelated private control leaked")
					}
					if performanceCountsExceeded(b, queries, rows, work.VMSteps) {
						t.Errorf("unbounded read: SQL=%d (budget %d), rows=%d (budget %d), VM steps=%d (budget %d)", queries, b.MaxQueries, rows, b.MaxRows, work.VMSteps, b.MaxVMSteps)
					}
				}
				for _, phase := range []string{"first_read", "warm", "post_invalidation"} {
					if len(durations[phase]) == 0 {
						continue
					}
					b := original
					if e, ok := baseline[performanceBaselineKey(b.Method, b.Path, b.Case, label, phase)]; ok {
						b.LatencyMS = e.LatencyMS
					}
					median, p95, maximum := performanceLatency(durations[phase])
					t.Logf("%s median=%v p95=%v max=%v measured_samples=%d median_budget=%dms secondary_max=%v", phase, median, p95, maximum, len(durations[phase]), b.LatencyMS, performanceMaxSample(b.LatencyMS))
					if (phase == "warm" && median > time.Duration(b.LatencyMS)*time.Millisecond) || maximum > performanceMaxSample(b.LatencyMS) {
						t.Errorf("%s secondary latency bound exceeded: median=%v (budget%dms), max=%v (budget%v)", phase, median, b.LatencyMS, maximum, performanceMaxSample(b.LatencyMS))
					}
				}

			})
			if pi == 0 && !passed {
				ownersSucceeded = false
			}
		}
		if pi == 0 && stageDir != "" && worker != 3 {
			if err := publishPerformanceOwnerStage(stageDir, sourceHash, worker, ownersSucceeded); err != nil {
				t.Fatal(err)
			}
			ownerPublished = true
			waitOwners()
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

// Independent keepalives can fire while the scanner is blocked. None is a
// completed read, and an empty scanner result still counts when it arrives.
func TestPerformanceEventStreamKeepalivesCannotCompleteSample(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	completed := 0
	w := &performanceWriter{ResponseRecorder: httptest.NewRecorder(), cancel: cancel, eventStream: true, onFlush: func() { completed++ }}
	w.Flush()
	for n := 0; n < 8; n++ {
		w.WriteString(": keepalive\n\n")
		w.Flush()
	}
	if w.polls != 0 || completed != 0 || ctx.Err() != nil {
		t.Fatal("timer canceled a pending scanner")
	}
	w.Flush() // completed empty result
	if w.polls != 1 || completed != 1 || ctx.Err() != nil {
		t.Fatal("empty result was not measured")
	}
	for n := 0; n < 8; n++ {
		w.WriteString(": keepalive\n\n")
		w.Flush()
	}
	w.WriteString("id: visible\nevent: event\ndata: {}\n\n")
	w.Flush()
	if w.polls != 2 || ctx.Err() != context.Canceled {
		t.Fatal("second result did not finish sample")
	}
}

func TestPerformanceReceiptKeepalivesCannotCompleteSample(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &performanceWriter{ResponseRecorder: httptest.NewRecorder(), cancel: cancel, receiptStream: true}
	w.Flush()
	for n := 0; n < 8; n++ {
		w.WriteString(": keepalive\n\n")
		w.Flush()
		w.Flush()
	}
	if w.polls != 0 || ctx.Err() != nil {
		t.Fatal("receipt timer/empty flush counted an unfinished read")
	}
	w.WriteString("id: initial\nevent: notification_receipt\ndata: {}\n\n")
	w.Flush()
	if w.polls != 1 || ctx.Err() != nil {
		t.Fatal("first receipt payload not measured")
	}
	w.WriteString(": keepalive\n\n")
	w.Flush()
	w.WriteString("id: fresh\nevent: notification_receipt\ndata: {}\n\n")
	w.Flush()
	if w.polls != 2 || ctx.Err() != context.Canceled {
		t.Fatal("fresh receipt payload did not finish sample")
	}
}

func performanceCaseKey(method, path, caseID string) string {
	key := method + " " + path
	if caseID != "" {
		key += " [" + caseID + "]"
	}
	return key
}

func performanceCountsExceeded(b routeBudget, queries, rows int, steps uint64) bool {
	return queries > b.MaxQueries || rows > b.MaxRows || steps > b.MaxVMSteps
}

// Wall-clock is secondary to deterministic statement/row/work ceilings.
func performanceMaxSample(medianMS int) time.Duration {
	bound := time.Duration(medianMS) * time.Millisecond * 4
	if bound < 5*time.Second {
		bound = 5 * time.Second
	}
	return bound
}
func performanceLatency(durations []time.Duration) (median, p95, maximum time.Duration) {
	values := append([]time.Duration(nil), durations...)
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	n := len(values)
	if n == 0 {
		panic("latency requires completed samples")
	}
	median = values[(n-1)/2]
	p95 = values[(95*n+99)/100-1]
	maximum = values[n-1]
	return
}

// Empty legacy phase keys denote warm allowances only. Cold exceptions never
// flow into the five-sample warm ceiling.
func performanceBaselineKey(method, path, caseID, principal, phase string) string {
	key := performanceCaseKey(method, path, caseID) + " " + principal
	if phase != "" && phase != "warm" {
		key += " " + phase
	}
	return key
}
func invalidatePerformanceCache(t *testing.T, env performanceEnv) (int64, int64) {
	t.Helper()
	var before, after int64
	if err := env.db.QueryRow(`SELECT version FROM resource_access_epoch WHERE singleton=1`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	r, err := env.db.Exec(`UPDATE threads SET updated_by=updated_by WHERE id=?`, env.replace.Replace("{thread_id}"))
	if err != nil {
		t.Fatal(err)
	}
	n, err := r.RowsAffected()
	if err != nil || n != 1 {
		t.Fatalf("invalidation changed %d rows: %v", n, err)
	}
	if err := env.db.QueryRow(`SELECT version FROM resource_access_epoch WHERE singleton=1`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after <= before {
		t.Fatal("canonical mutation did not invalidate authorization epoch")
	}
	return before, after
}
