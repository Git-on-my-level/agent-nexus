package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
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
	store      PrimitiveStore
}

func newProjectionMaintenanceTestServer(t *testing.T) projectionMaintenanceTestHarness {
	t.Helper()
	return newProjectionMaintenanceTestServerWithMode(t, ProjectionModeBackground)
}

func newProjectionMaintenanceTestServerWithMode(t *testing.T, mode string) projectionMaintenanceTestHarness {
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
		Mode:              mode,
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
		store:      primitiveStore,
	}
}

func (h projectionMaintenanceTestHarness) step(t *testing.T, now time.Time) {
	t.Helper()
	if err := h.stepErr(now); err != nil {
		t.Fatalf("projection maintainer step: %v", err)
	}
}

func (h projectionMaintenanceTestHarness) stepErr(now time.Time) error {
	return h.maintainer.Step(context.Background(), now)
}

type blockingProjectionStore struct {
	PrimitiveStore
	threadID string
	blocked  chan struct{}
	release  chan struct{}
	once     sync.Once
}

func (s *blockingProjectionStore) PutDerivedTopicProjection(ctx context.Context, projection primitives.DerivedTopicProjection) error {
	if strings.TrimSpace(projection.ThreadID) == s.threadID {
		shouldBlock := false
		s.once.Do(func() {
			shouldBlock = true
			close(s.blocked)
		})
		if shouldBlock {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-s.release:
			}
		}
	}
	return s.PrimitiveStore.PutDerivedTopicProjection(ctx, projection)
}

func TestProjectionMaintainerEmitsStaleExceptionsAndRefreshesInbox(t *testing.T) {
	t.Parallel()

	h := newProjectionMaintenanceTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated).Body.Close()

	threadID := integrationSeedThreadWithStore(t, h.store, nil, "actor-1", map[string]any{
		"title":            "Worker stale thread",
		"type":             "incident",
		"status":           "active",
		"priority":         "p1",
		"tags":             []any{"ops"},
		"cadence":          "daily",
		"next_check_in_at": "2020-01-01T00:00:00Z",
		"current_summary":  "summary",
		"next_actions":     []any{"follow up"},
		"key_artifacts":    []any{},
		"provenance":       map[string]any{"sources": []any{"inferred"}},
	})

	if count := countStaleThreadExceptions(t, h.baseURL, threadID); count != 0 {
		t.Fatalf("expected no stale exceptions before maintainer step, got %d", count)
	}

	stepNow := time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC)
	h.step(t, stepNow)

	if count := countStaleThreadExceptions(t, h.baseURL, threadID); count != 1 {
		t.Fatalf("expected one stale exception after maintainer step, got %d", count)
	}
	if !threadListedAsStale(t, h.baseURL, threadID) {
		t.Fatalf("expected thread %s to be stale after maintainer step", threadID)
	}
	items := getInboxItems(t, h.baseURL)
	if _, ok := findInboxItem(items, func(item map[string]any) bool {
		return asString(item["category"]) == "stale_topic" && asString(item["thread_id"]) == threadID
	}); !ok {
		t.Fatalf("expected stale exception inbox item after worker step, got %#v", items)
	}

	h.step(t, stepNow.Add(2*time.Second))
	if count := countStaleThreadExceptions(t, h.baseURL, threadID); count != 1 {
		t.Fatalf("expected maintainer step to avoid duplicate stale exceptions, got %d", count)
	}
}

func TestProjectionMaintainerSuppressesStaleInboxAfterNewActivity(t *testing.T) {
	t.Parallel()

	h := newProjectionMaintenanceTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated).Body.Close()

	threadID := integrationSeedThreadWithStore(t, h.store, nil, "actor-1", map[string]any{
		"title":            "Worker stale suppression thread",
		"type":             "incident",
		"status":           "active",
		"priority":         "p1",
		"tags":             []any{"ops"},
		"cadence":          "daily",
		"next_check_in_at": "2020-01-01T00:00:00Z",
		"current_summary":  "summary",
		"next_actions":     []any{"follow up"},
		"key_artifacts":    []any{},
		"provenance":       map[string]any{"sources": []any{"inferred"}},
	})

	staleNow := time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC)
	h.step(t, staleNow)

	postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"actor_statement",
			"thread_id":"`+threadID+`",
			"refs":["topic:`+threadID+`"],
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
		return asString(item["category"]) == "stale_topic" && asString(item["thread_id"]) == threadID
	}); ok {
		t.Fatalf("expected stale inbox item to be suppressed after new activity, got %#v", items)
	}
}

func TestPublicHealthEndpointsDoNotExposeProjectionMaintenance(t *testing.T) {
	t.Parallel()

	h := newProjectionMaintenanceTestServer(t)

	for _, path := range []string{"/health", "/livez", "/readyz"} {
		resp, err := http.Get(h.baseURL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			t.Fatalf("unexpected %s status: %d", path, resp.StatusCode)
		}

		var payload map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			resp.Body.Close()
			t.Fatalf("decode %s response: %v", path, err)
		}
		resp.Body.Close()

		if payload["ok"] != true {
			t.Fatalf("expected ok=true for %s, got %#v", path, payload)
		}
		if _, ok := payload["projection_maintenance"]; ok {
			t.Fatalf("expected %s to omit projection_maintenance, got %#v", path, payload)
		}
	}
}

func TestOpsHealthEndpointReportsProjectionMaintenanceLag(t *testing.T) {
	t.Parallel()

	h := newProjectionMaintenanceTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated).Body.Close()

	integrationSeedThreadWithStore(t, h.store, nil, "actor-1", map[string]any{
		"id":              "health-thread",
		"title":           "Health projection thread",
		"type":            "incident",
		"status":          "active",
		"priority":        "p1",
		"tags":            []any{"ops"},
		"cadence":         "reactive",
		"current_summary": "summary",
		"next_actions":    []any{"follow up"},
		"key_artifacts":   []any{},
		"provenance":      map[string]any{"sources": []any{"inferred"}},
	})

	before := getProjectionMaintenanceHealth(t, h.baseURL, "/ops/health")
	if before.Mode != ProjectionModeBackground {
		t.Fatalf("expected background projection mode before maintainer step, got %#v", before)
	}
	if before.PendingDirtyCount == 0 {
		t.Fatalf("expected pending dirty count before maintainer step, got %#v", before)
	}
	if before.OldestDirtyAt == "" {
		t.Fatalf("expected oldest dirty timestamp before maintainer step, got %#v", before)
	}

	h.step(t, time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC))

	after := getProjectionMaintenanceHealth(t, h.baseURL, "/ops/health")
	if after.Mode != ProjectionModeBackground {
		t.Fatalf("expected background projection mode after maintainer step, got %#v", after)
	}
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

func TestProjectionMaintainerManualModeRunExitsWithoutProcessingQueue(t *testing.T) {
	t.Parallel()

	h := newProjectionMaintenanceTestServerWithMode(t, ProjectionModeManual)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated).Body.Close()

	integrationSeedThreadWithStore(t, h.store, nil, "actor-1", map[string]any{
		"id":              "manual-mode-thread",
		"title":           "Manual mode projection thread",
		"type":            "incident",
		"status":          "active",
		"priority":        "p1",
		"tags":            []any{"ops"},
		"cadence":         "reactive",
		"current_summary": "summary",
		"next_actions":    []any{"follow up"},
		"key_artifacts":   []any{},
		"provenance":      map[string]any{"sources": []any{"inferred"}},
	})

	before, err := h.store.GetDerivedTopicProjectionQueueStats(context.Background())
	if err != nil {
		t.Fatalf("load projection queue stats before manual run: %v", err)
	}
	if before.PendingCount == 0 {
		t.Fatalf("expected pending dirty count before manual run, got %#v", before)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		h.maintainer.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected manual-mode Run to exit immediately")
	}

	after, err := h.store.GetDerivedTopicProjectionQueueStats(context.Background())
	if err != nil {
		t.Fatalf("load projection queue stats after manual run: %v", err)
	}
	if after.PendingCount == 0 {
		t.Fatalf("expected manual mode to leave queued projection work pending, got %#v", after)
	}
}

func TestManualModeDerivedRebuildClearsPendingProjectionWork(t *testing.T) {
	t.Parallel()

	h := newProjectionMaintenanceTestServerWithMode(t, ProjectionModeManual)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated).Body.Close()

	threadID := integrationSeedThreadWithStore(t, h.store, nil, "actor-1", map[string]any{
		"title":            "Manual rebuild thread",
		"type":             "incident",
		"status":           "active",
		"priority":         "p1",
		"tags":             []any{"ops"},
		"cadence":          "daily",
		"next_check_in_at": "2020-01-01T00:00:00Z",
		"current_summary":  "summary",
		"next_actions":     []any{"follow up"},
		"key_artifacts":    []any{},
		"provenance":       map[string]any{"sources": []any{"inferred"}},
	})

	before := getProjectionMaintenanceHealth(t, h.baseURL, "/ops/health")
	if before.Mode != ProjectionModeManual {
		t.Fatalf("expected manual projection mode before explicit rebuild, got %#v", before)
	}
	if before.PendingDirtyCount == 0 {
		t.Fatalf("expected pending dirty count before explicit rebuild, got %#v", before)
	}

	postJSONExpectStatus(t, h.baseURL+"/derived/rebuild", `{"actor_id":"actor-1"}`, http.StatusOK).Body.Close()

	after := getProjectionMaintenanceHealth(t, h.baseURL, "/ops/health")
	if after.Mode != ProjectionModeManual {
		t.Fatalf("expected manual projection mode after explicit rebuild, got %#v", after)
	}
	if after.PendingDirtyCount != 0 {
		t.Fatalf("expected explicit rebuild to clear queued projection work, got %#v", after)
	}

	if count := countStaleThreadExceptions(t, h.baseURL, threadID); count != 1 {
		t.Fatalf("expected rebuild to emit exactly one stale exception, got %d", count)
	}
	if !threadListedAsStale(t, h.baseURL, threadID) {
		t.Fatalf("expected explicit rebuild to refresh stale projection for thread %s", threadID)
	}
}

func TestProjectionMaintainerKeepsProjectionPendingForConcurrentWrites(t *testing.T) {
	t.Parallel()

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
	baseStore := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	store := &blockingProjectionStore{
		PrimitiveStore: baseStore,
		blocked:        make(chan struct{}),
		release:        make(chan struct{}),
	}
	maintainer := NewProjectionMaintainer(ProjectionMaintainerConfig{
		PrimitiveStore:    store,
		Contract:          contract,
		StaleScanInterval: time.Hour,
		DirtyBatchSize:    20,
		SystemActorID:     "oar-core",
	})
	handler := NewHandler(
		contract.Version,
		WithHealthCheck(workspace.Ping),
		WithActorRegistry(registry),
		WithPrimitiveStore(store),
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

	postJSONExpectStatus(t, server.URL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated).Body.Close()
	threadID := integrationSeedThreadWithStore(t, store, nil, "actor-1", map[string]any{
		"title":           "Concurrent projection thread",
		"type":            "incident",
		"status":          "active",
		"priority":        "p1",
		"tags":            []any{"ops"},
		"cadence":         "reactive",
		"current_summary": "summary",
		"next_actions":    []any{"follow up"},
		"key_artifacts":   []any{},
		"provenance":      map[string]any{"sources": []any{"inferred"}},
	})

	if err := maintainer.Step(context.Background(), time.Now().UTC()); err != nil {
		t.Fatalf("initial step: %v", err)
	}
	store.threadID = threadID

	postJSONExpectStatus(t, server.URL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"decision_needed",
			"thread_id":"`+threadID+`",
			"refs":["topic:`+threadID+`"],
			"summary":"Need a first decision",
			"payload":{},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated).Body.Close()

	statuses, err := baseStore.GetTopicProjectionRefreshStatuses(context.Background(), []string{threadID})
	if err != nil {
		t.Fatalf("load refresh statuses after first write: %v", err)
	}
	if got := statuses[threadID].DesiredGeneration; got != 2 {
		t.Fatalf("expected desired_generation=2 after first write, got %#v", statuses[threadID])
	}

	stepErrCh := make(chan error, 1)
	go func() {
		stepErrCh <- maintainer.Step(context.Background(), time.Now().UTC())
	}()

	select {
	case <-store.blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for blocked projection refresh")
	}

	postJSONExpectStatus(t, server.URL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"decision_needed",
			"thread_id":"`+threadID+`",
			"refs":["topic:`+threadID+`"],
			"summary":"Need a second decision",
			"payload":{},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated).Body.Close()

	statuses, err = baseStore.GetTopicProjectionRefreshStatuses(context.Background(), []string{threadID})
	if err != nil {
		t.Fatalf("load refresh statuses during blocked refresh: %v", err)
	}
	if got := statuses[threadID].DesiredGeneration; got != 3 {
		t.Fatalf("expected desired_generation=3 after concurrent write, got %#v", statuses[threadID])
	}
	if got := statuses[threadID].MaterializedGeneration; got != 1 {
		t.Fatalf("expected materialized_generation to stay at 1 during blocked refresh, got %#v", statuses[threadID])
	}
	if statuses[threadID].InProgressGeneration == nil || *statuses[threadID].InProgressGeneration != 2 {
		t.Fatalf("expected in_progress_generation=2 during blocked refresh, got %#v", statuses[threadID])
	}

	close(store.release)
	if err := <-stepErrCh; err != nil {
		t.Fatalf("blocked step: %v", err)
	}

	state, err := loadTopicProjectionState(context.Background(), handlerOptions{primitiveStore: baseStore}, threadID)
	if err != nil {
		t.Fatalf("load state after first refresh: %v", err)
	}
	if state.Status != "pending" {
		t.Fatalf("expected projection to remain pending after concurrent write, got %#v", state.Freshness)
	}

	statuses, err = baseStore.GetTopicProjectionRefreshStatuses(context.Background(), []string{threadID})
	if err != nil {
		t.Fatalf("load refresh statuses after first refresh: %v", err)
	}
	if got := statuses[threadID].MaterializedGeneration; got != 2 {
		t.Fatalf("expected first refresh to materialize generation 2, got %#v", statuses[threadID])
	}
	if !statuses[threadID].IsDirty() {
		t.Fatalf("expected refresh status to stay dirty after concurrent write, got %#v", statuses[threadID])
	}

	if err := maintainer.Step(context.Background(), time.Now().UTC()); err != nil {
		t.Fatalf("follow-up step: %v", err)
	}

	state, err = loadTopicProjectionState(context.Background(), handlerOptions{primitiveStore: baseStore}, threadID)
	if err != nil {
		t.Fatalf("load state after follow-up refresh: %v", err)
	}
	if state.Status != "current" {
		t.Fatalf("expected follow-up refresh to clear pending state, got %#v", state.Freshness)
	}
	if state.Projection.InboxCount != 2 {
		t.Fatalf("expected follow-up refresh to materialize both inbox items, got %#v", state.Projection)
	}

	statuses, err = baseStore.GetTopicProjectionRefreshStatuses(context.Background(), []string{threadID})
	if err != nil {
		t.Fatalf("load refresh statuses after follow-up refresh: %v", err)
	}
	if got := statuses[threadID].MaterializedGeneration; got != 3 {
		t.Fatalf("expected materialized_generation=3 after follow-up refresh, got %#v", statuses[threadID])
	}
	if statuses[threadID].IsDirty() || statuses[threadID].InProgress() {
		t.Fatalf("expected clean refresh status after follow-up refresh, got %#v", statuses[threadID])
	}
}

func TestProjectionMaintainerNotifyWakesRunLoopPromptly(t *testing.T) {
	t.Parallel()

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
	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	maintainer := NewProjectionMaintainer(ProjectionMaintainerConfig{
		PrimitiveStore:    store,
		Contract:          contract,
		PollInterval:      time.Minute,
		StaleScanInterval: time.Hour,
		DirtyBatchSize:    20,
		SystemActorID:     "oar-core",
	})
	handler := NewHandler(
		contract.Version,
		WithHealthCheck(workspace.Ping),
		WithActorRegistry(registry),
		WithPrimitiveStore(store),
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

	postJSONExpectStatus(t, server.URL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated).Body.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go maintainer.Run(ctx)

	threadID := integrationSeedThreadWithStore(t, store, nil, "actor-1", map[string]any{
		"title":           "Wakeup projection thread",
		"type":            "incident",
		"status":          "active",
		"priority":        "p1",
		"tags":            []any{"ops"},
		"cadence":         "reactive",
		"current_summary": "summary",
		"next_actions":    []any{"follow up"},
		"key_artifacts":   []any{},
		"provenance":      map[string]any{"sources": []any{"inferred"}},
	})

	deadline := time.Now().Add(2 * time.Second)
	for {
		state, err := loadTopicProjectionState(context.Background(), handlerOptions{primitiveStore: store}, threadID)
		if err != nil {
			t.Fatalf("load thread projection state: %v", err)
		}
		if state.Status == "current" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("projection maintainer did not wake promptly; latest freshness=%#v", state.Freshness)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

type projectionMaintenanceFailureStore struct {
	PrimitiveStore
	failErr error
	failed  bool
}

func (s *projectionMaintenanceFailureStore) PutDerivedTopicProjection(ctx context.Context, projection primitives.DerivedTopicProjection) error {
	if !s.failed {
		s.failed = true
		return s.failErr
	}
	return s.PrimitiveStore.PutDerivedTopicProjection(ctx, projection)
}

func TestOpsHealthEndpointReportsProjectionMaintenanceErrors(t *testing.T) {
	t.Parallel()

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
	baseStore := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	primitiveStore := &projectionMaintenanceFailureStore{
		PrimitiveStore: baseStore,
		failErr:        errors.New("synthetic projection failure"),
	}
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

	postJSONExpectStatus(t, server.URL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated).Body.Close()
	integrationSeedThreadWithStore(t, primitiveStore, nil, "actor-1", map[string]any{
		"id":              "failing-thread",
		"title":           "Failing projection thread",
		"type":            "incident",
		"status":          "active",
		"priority":        "p1",
		"tags":            []any{"ops"},
		"cadence":         "reactive",
		"current_summary": "summary",
		"next_actions":    []any{"follow up"},
		"key_artifacts":   []any{},
		"provenance":      map[string]any{"sources": []any{"inferred"}},
	})

	if err := maintainer.Step(context.Background(), time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("expected projection maintainer step to fail")
	}

	health := getProjectionMaintenanceHealth(t, server.URL, "/ops/health")
	if health.PendingDirtyCount == 0 {
		t.Fatalf("expected pending dirty work to remain after failure, got %#v", health)
	}
	if health.LastError == nil {
		t.Fatalf("expected maintenance last_error after failure, got %#v", health)
	}
	if strings.TrimSpace(health.LastError.Message) == "" {
		t.Fatalf("expected non-empty maintenance error message, got %#v", health.LastError)
	}
	if !strings.Contains(health.LastError.Message, "synthetic projection failure") {
		t.Fatalf("expected underlying failure text in last_error.message, got %#v", health.LastError)
	}
}

func TestOpsHealthEndpointKeepsDiagnosticsWhenReadinessFails(t *testing.T) {
	t.Parallel()

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
		WithHealthCheck(func(context.Context) error { return errors.New("database unavailable") }),
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

	resp, err := http.Get(server.URL + "/ops/health")
	if err != nil {
		t.Fatalf("GET /ops/health: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unexpected /ops/health status: got %d want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode /ops/health response: %v", err)
	}
	if ok, _ := payload["ok"].(bool); ok {
		t.Fatalf("expected readiness failure payload, got %#v", payload)
	}
	errPayload, _ := payload["error"].(map[string]any)
	if errPayload == nil || errPayload["code"] != "storage_unavailable" {
		t.Fatalf("expected storage_unavailable error code, got %#v", payload["error"])
	}
	if _, ok := payload["projection_maintenance"]; !ok {
		t.Fatalf("expected projection_maintenance payload even on readiness failure, got %#v", payload)
	}
}

func getProjectionMaintenanceHealth(t *testing.T, baseURL string, path string) ProjectionMaintenanceSnapshot {
	t.Helper()
	status, payload := getProjectionMaintenanceHealthPayload(t, baseURL, path)
	if status != http.StatusOK {
		t.Fatalf("unexpected %s status: %d", path, status)
	}
	return payload.ProjectionMaintenance
}

func getProjectionMaintenanceHealthPayload(t *testing.T, baseURL string, path string) (int, struct {
	OK                    bool                          `json:"ok"`
	Error                 map[string]any                `json:"error"`
	ProjectionMaintenance ProjectionMaintenanceSnapshot `json:"projection_maintenance"`
}) {
	t.Helper()
	resp, err := http.Get(baseURL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()

	var payload struct {
		OK                    bool                          `json:"ok"`
		Error                 map[string]any                `json:"error"`
		ProjectionMaintenance ProjectionMaintenanceSnapshot `json:"projection_maintenance"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode %s response: %v", path, err)
	}
	return resp.StatusCode, payload
}
