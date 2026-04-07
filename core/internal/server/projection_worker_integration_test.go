package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"organization-autorunner-core/internal/blob"
	"path/filepath"
	"testing"
	"time"

	"organization-autorunner-core/internal/actors"
	"organization-autorunner-core/internal/primitives"
	"organization-autorunner-core/internal/schema"
	"organization-autorunner-core/internal/storage"
)

type manualProjectionHarness struct {
	primitivesTestHarness
	store      *primitives.Store
	maintainer *ProjectionMaintainer
}

func newManualProjectionTestServer(t *testing.T) manualProjectionHarness {
	t.Helper()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	contractPath := filepath.Join("..", "..", "..", "contracts", "oar-schema.yaml")
	contract, err := schema.Load(contractPath)
	if err != nil {
		_ = workspace.Close()
		t.Fatalf("load schema contract: %v", err)
	}

	registry := actors.NewStore(workspace.DB())
	primitiveStore := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	maintainer := NewProjectionMaintainer(ProjectionMaintainerConfig{
		PrimitiveStore:   primitiveStore,
		Contract:         contract,
		InboxRiskHorizon: defaultInboxRiskHorizon,
		DirtyBatchSize:   20,
		SystemActorID:    "oar-core",
	})
	handler := NewHandler(
		contract.Version,
		WithHealthCheck(workspace.Ping),
		WithActorRegistry(registry),
		WithPrimitiveStore(primitiveStore),
		WithSchemaContract(contract),
		WithAllowUnauthenticatedWrites(true),
		WithEnableDevActorMode(true),
	)
	server := httptest.NewServer(handler)
	t.Cleanup(func() {
		server.Close()
		_ = workspace.Close()
	})

	return manualProjectionHarness{
		primitivesTestHarness: primitivesTestHarness{workspace: workspace, baseURL: server.URL, maintainer: maintainer, primitiveStore: primitiveStore},
		store:                 primitiveStore,
		maintainer:            maintainer,
	}
}

func TestThreadWorkspaceReadDoesNotMutateDerivedState(t *testing.T) {
	t.Parallel()

	h := newManualProjectionTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated).Body.Close()

	threadID := createBoardThreadViaHTTP(t, h.primitivesTestHarness, "Workspace projection thread")
	postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"decision_needed",
			"thread_id":"`+threadID+`",
			"refs":["thread:`+threadID+`"],
			"summary":"Need a decision",
			"payload":{},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated).Body.Close()

	eventsBefore := countTableRows(t, h.workspace.DB(), "events")
	projectionsBefore := countTableRows(t, h.workspace.DB(), "derived_topic_views")
	inboxBefore := countTableRows(t, h.workspace.DB(), "derived_inbox_items")

	resp, err := http.Get(h.baseURL + "/threads/" + threadID + "/workspace")
	if err != nil {
		t.Fatalf("GET /threads/{id}/workspace: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected workspace status: %d", resp.StatusCode)
	}

	var payload struct {
		Inbox struct {
			Count int `json:"count"`
		} `json:"inbox"`
		ProjectionFreshness map[string]any `json:"projection_freshness"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode workspace response: %v", err)
	}
	if got := asString(payload.ProjectionFreshness["status"]); got != "pending" {
		t.Fatalf("expected pending projection freshness, got %#v", payload.ProjectionFreshness)
	}
	if payload.Inbox.Count != 0 {
		t.Fatalf("expected no materialized inbox items before worker runs, got %#v", payload.Inbox)
	}

	if eventsAfter := countTableRows(t, h.workspace.DB(), "events"); eventsAfter != eventsBefore {
		t.Fatalf("expected workspace read not to append events, got before=%d after=%d", eventsBefore, eventsAfter)
	}
	if projectionsAfter := countTableRows(t, h.workspace.DB(), "derived_topic_views"); projectionsAfter != projectionsBefore {
		t.Fatalf("expected workspace read not to update derived thread views, got before=%d after=%d", projectionsBefore, projectionsAfter)
	}
	if inboxAfter := countTableRows(t, h.workspace.DB(), "derived_inbox_items"); inboxAfter != inboxBefore {
		t.Fatalf("expected workspace read not to update derived inbox rows, got before=%d after=%d", inboxBefore, inboxAfter)
	}
}

func TestInboxReadDoesNotEmitStaleThreadExceptions(t *testing.T) {
	t.Parallel()

	h := newManualProjectionTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated).Body.Close()

	threadID := integrationSeedThread(t, h.primitivesTestHarness, "actor-1", map[string]any{
		"title":            "Pending stale thread",
		"type":             "incident",
		"status":           "active",
		"priority":         "p1",
		"tags":             []any{"ops"},
		"cadence":          "daily",
		"next_check_in_at": "2020-01-01T00:00:00Z",
		"current_summary":  "summary",
		"next_actions":     []any{"check"},
		"key_artifacts":    []any{},
		"provenance":       map[string]any{"sources": []any{"inferred"}},
	})

	before := countStaleThreadExceptions(t, h.baseURL, threadID)
	inboxResp, err := http.Get(h.baseURL + "/inbox")
	if err != nil {
		t.Fatalf("GET /inbox: %v", err)
	}
	defer inboxResp.Body.Close()
	if inboxResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected inbox status: %d", inboxResp.StatusCode)
	}
	after := countStaleThreadExceptions(t, h.baseURL, threadID)
	if after != before {
		t.Fatalf("expected GET /inbox not to emit stale-thread exceptions, got before=%d after=%d", before, after)
	}
}

func TestProjectionMaintainerStepClearsPendingStatus(t *testing.T) {
	t.Parallel()

	h := newManualProjectionTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated).Body.Close()

	threadID := createBoardThreadViaHTTP(t, h.primitivesTestHarness, "Manual worker thread")
	postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"decision_needed",
			"thread_id":"`+threadID+`",
			"refs":["thread:`+threadID+`"],
			"summary":"Need a decision",
			"payload":{},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated).Body.Close()

	statuses, err := h.store.GetTopicProjectionRefreshStatuses(context.Background(), []string{threadID})
	if err != nil {
		t.Fatalf("GetTopicProjectionRefreshStatuses: %v", err)
	}
	if !statuses[threadID].IsDirty() {
		t.Fatalf("expected thread %s to be marked dirty before worker runs, got %#v", threadID, statuses[threadID])
	}

	if err := h.maintainer.Step(context.Background(), time.Now().UTC()); err != nil {
		t.Fatalf("Step: %v", err)
	}

	state, err := loadTopicProjectionState(context.Background(), handlerOptions{primitiveStore: h.store}, threadID)
	if err != nil {
		t.Fatalf("loadTopicProjectionState: %v", err)
	}
	if state.Status != "current" {
		t.Fatalf("expected current projection status after worker run, got %#v", state.Freshness)
	}
	if state.Projection.InboxCount != 1 {
		t.Fatalf("expected materialized inbox_count=1 after worker run, got %#v", state.Projection)
	}

	statuses, err = h.store.GetTopicProjectionRefreshStatuses(context.Background(), []string{threadID})
	if err != nil {
		t.Fatalf("GetTopicProjectionRefreshStatuses after worker: %v", err)
	}
	if statuses[threadID].IsDirty() || statuses[threadID].InProgress() || statuses[threadID].LastErrorMessage != "" {
		t.Fatalf("expected clean refresh status after worker run, got %#v", statuses[threadID])
	}
}

func TestDisabledWorkerLeavesProjectionPendingButReadable(t *testing.T) {
	t.Parallel()

	h := newManualProjectionTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated).Body.Close()

	threadID := createBoardThreadViaHTTP(t, h.primitivesTestHarness, "Pending-only thread")
	postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"decision_needed",
			"thread_id":"`+threadID+`",
			"refs":["thread:`+threadID+`"],
			"summary":"Need a decision",
			"payload":{},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated).Body.Close()

	inboxResp, err := http.Get(h.baseURL + "/inbox")
	if err != nil {
		t.Fatalf("GET /inbox: %v", err)
	}
	defer inboxResp.Body.Close()
	if inboxResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected inbox status: %d", inboxResp.StatusCode)
	}

	var inboxPayload struct {
		Items               []map[string]any `json:"items"`
		ProjectionFreshness map[string]any   `json:"projection_freshness"`
	}
	if err := json.NewDecoder(inboxResp.Body).Decode(&inboxPayload); err != nil {
		t.Fatalf("decode inbox payload: %v", err)
	}
	if got := asString(inboxPayload.ProjectionFreshness["status"]); got != "pending" {
		t.Fatalf("expected pending inbox freshness, got %#v", inboxPayload.ProjectionFreshness)
	}
	if len(inboxPayload.Items) != 0 {
		t.Fatalf("expected empty materialized inbox while worker is disabled, got %#v", inboxPayload.Items)
	}

	workspaceResp, err := http.Get(h.baseURL + "/threads/" + threadID + "/workspace")
	if err != nil {
		t.Fatalf("GET /threads/{id}/workspace: %v", err)
	}
	defer workspaceResp.Body.Close()
	if workspaceResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected workspace status: %d", workspaceResp.StatusCode)
	}

	var workspacePayload struct {
		ProjectionFreshness map[string]any `json:"projection_freshness"`
	}
	if err := json.NewDecoder(workspaceResp.Body).Decode(&workspacePayload); err != nil {
		t.Fatalf("decode workspace payload: %v", err)
	}
	if got := asString(workspacePayload.ProjectionFreshness["status"]); got != "pending" {
		t.Fatalf("expected pending workspace freshness, got %#v", workspacePayload.ProjectionFreshness)
	}
}

func countTableRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()

	var count int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
		t.Fatalf("count rows in %s: %v", table, err)
	}
	return count
}
