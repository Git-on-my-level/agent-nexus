package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"organization-autorunner-core/internal/actors"
	"organization-autorunner-core/internal/blob"
	"organization-autorunner-core/internal/primitives"
	"organization-autorunner-core/internal/schema"
	"organization-autorunner-core/internal/storage"
)

type projectionMaintenanceTestHarness struct {
	workspace  *storage.Workspace
	baseURL    string
	maintainer *ProjectionMaintainer
}

func newProjectionMaintenanceTestServer(t *testing.T) projectionMaintenanceTestHarness {
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
		PrimitiveStore:    primitiveStore,
		Contract:          contract,
		StaleScanInterval: time.Second,
		DirtyBatchSize:    20,
		SystemActorID:     "oar-core",
	})
	handler := NewHandler(
		contract.Version,
		WithHealthCheck(workspace.Ping),
		WithActorRegistry(registry),
		WithPrimitiveStore(primitiveStore),
		WithSchemaContract(contract),
		WithEnableDevActorMode(true),
		WithAllowUnauthenticatedWrites(true),
		WithProjectionMaintainer(maintainer),
	)
	server := httptest.NewServer(handler)
	t.Cleanup(func() {
		server.Close()
		_ = workspace.Close()
	})

	return projectionMaintenanceTestHarness{
		workspace:  workspace,
		baseURL:    server.URL,
		maintainer: maintainer,
	}
}

func (h projectionMaintenanceTestHarness) step(t *testing.T, now time.Time) {
	t.Helper()
	if err := h.maintainer.Step(context.Background(), now); err != nil {
		t.Fatalf("projection maintainer step: %v", err)
	}
}

func TestProjectionMaintainerEmitsStaleExceptionsAndRefreshesInbox(t *testing.T) {
	t.Parallel()

	h := newProjectionMaintenanceTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated).Body.Close()

	createResp := postJSONExpectStatus(t, h.baseURL+"/threads", `{
		"actor_id":"actor-1",
		"thread":{
			"title":"Worker stale thread",
			"type":"incident",
			"status":"active",
			"priority":"p1",
			"tags":["ops"],
			"cadence":"daily",
			"next_check_in_at":"2020-01-01T00:00:00Z",
			"current_summary":"summary",
			"next_actions":["follow up"],
			"key_artifacts":[],
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer createResp.Body.Close()

	var created struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode thread response: %v", err)
	}
	threadID := asString(created.Thread["id"])
	if threadID == "" {
		t.Fatal("expected thread id")
	}

	if count := countStaleThreadExceptions(t, h.baseURL, threadID); count != 0 {
		t.Fatalf("expected no stale exceptions before worker step, got %d", count)
	}

	stepNow := time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC)
	h.step(t, stepNow)

	if count := countStaleThreadExceptions(t, h.baseURL, threadID); count != 1 {
		t.Fatalf("expected one stale exception after worker step, got %d", count)
	}
	if !threadListedAsStale(t, h.baseURL, threadID) {
		t.Fatalf("expected thread %s to be stale after worker step", threadID)
	}
	items := getInboxItems(t, h.baseURL)
	if _, ok := findInboxItem(items, func(item map[string]any) bool {
		return asString(item["category"]) == "exception" && asString(item["thread_id"]) == threadID
	}); !ok {
		t.Fatalf("expected stale exception inbox item after worker step, got %#v", items)
	}

	h.step(t, stepNow.Add(2*time.Second))
	if count := countStaleThreadExceptions(t, h.baseURL, threadID); count != 1 {
		t.Fatalf("expected worker step to avoid duplicate stale exceptions, got %d", count)
	}
}

func TestProjectionMaintainerSuppressesStaleInboxAfterNewActivity(t *testing.T) {
	t.Parallel()

	h := newProjectionMaintenanceTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated).Body.Close()

	createResp := postJSONExpectStatus(t, h.baseURL+"/threads", `{
		"actor_id":"actor-1",
		"thread":{
			"title":"Worker stale suppression thread",
			"type":"incident",
			"status":"active",
			"priority":"p1",
			"tags":["ops"],
			"cadence":"daily",
			"next_check_in_at":"2020-01-01T00:00:00Z",
			"current_summary":"summary",
			"next_actions":["follow up"],
			"key_artifacts":[],
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer createResp.Body.Close()

	var created struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode thread response: %v", err)
	}
	threadID := asString(created.Thread["id"])
	if threadID == "" {
		t.Fatal("expected thread id")
	}

	staleNow := time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC)
	h.step(t, staleNow)

	postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"actor_statement",
			"thread_id":"`+threadID+`",
			"refs":["thread:`+threadID+`"],
			"summary":"progress update",
			"payload":{"statement":"on it"},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated).Body.Close()

	h.step(t, staleNow.Add(30*time.Second))

	if threadListedAsStale(t, h.baseURL, threadID) {
		t.Fatalf("expected new activity to clear stale thread %s", threadID)
	}
	items := getInboxItems(t, h.baseURL)
	if _, ok := findInboxItem(items, func(item map[string]any) bool {
		return asString(item["category"]) == "exception" && asString(item["thread_id"]) == threadID
	}); ok {
		t.Fatalf("expected stale inbox item to be suppressed after new activity, got %#v", items)
	}
}

func TestHealthEndpointReportsProjectionMaintenanceLag(t *testing.T) {
	t.Parallel()

	h := newProjectionMaintenanceTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated).Body.Close()

	postJSONExpectStatus(t, h.baseURL+"/threads", `{
		"actor_id":"actor-1",
		"thread":{
			"id":"health-thread",
			"title":"Health projection thread",
			"type":"incident",
			"status":"active",
			"priority":"p1",
			"tags":["ops"],
			"cadence":"reactive",
			"current_summary":"summary",
			"next_actions":["follow up"],
			"key_artifacts":[],
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated).Body.Close()

	before := getProjectionMaintenanceHealth(t, h.baseURL)
	if before.PendingDirtyCount == 0 {
		t.Fatalf("expected pending dirty count before maintainer step, got %#v", before)
	}
	if before.OldestDirtyAt == "" {
		t.Fatalf("expected oldest dirty timestamp before maintainer step, got %#v", before)
	}

	h.step(t, time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC))

	after := getProjectionMaintenanceHealth(t, h.baseURL)
	if after.PendingDirtyCount != 0 {
		t.Fatalf("expected pending dirty count to clear after maintainer step, got %#v", after)
	}
	if after.LastSuccessfulStaleScanAt == "" {
		t.Fatalf("expected last successful stale scan after maintainer step, got %#v", after)
	}
	if after.LastError != nil {
		t.Fatalf("did not expect maintenance error after successful step, got %#v", after)
	}
}

func getProjectionMaintenanceHealth(t *testing.T, baseURL string) ProjectionMaintenanceSnapshot {
	t.Helper()

	resp, err := http.Get(baseURL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected /health status: %d", resp.StatusCode)
	}

	var payload struct {
		ProjectionMaintenance ProjectionMaintenanceSnapshot `json:"projection_maintenance"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode /health response: %v", err)
	}
	return payload.ProjectionMaintenance
}
