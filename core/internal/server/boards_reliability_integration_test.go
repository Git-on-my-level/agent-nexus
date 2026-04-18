package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"agent-nexus-core/internal/blob"
	"path/filepath"
	"strings"
	"testing"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
	"agent-nexus-core/internal/storage"
)

type boardLifecycleFailureStore struct {
	PrimitiveStore
	appendBoardEventErr     error
	failProjectionAfterEmit bool
	boardEventAppended      bool
	projectionFailed        bool
}

func (s *boardLifecycleFailureStore) AppendEvent(ctx context.Context, actorID string, event map[string]any) (map[string]any, error) {
	if s.appendBoardEventErr != nil && strings.HasPrefix(anyString(event["type"]), "board_") {
		return nil, s.appendBoardEventErr
	}
	stored, err := s.PrimitiveStore.AppendEvent(ctx, actorID, event)
	if err == nil && s.failProjectionAfterEmit && strings.HasPrefix(anyString(event["type"]), "board_") {
		s.boardEventAppended = true
	}
	return stored, err
}

func (s *boardLifecycleFailureStore) PutDerivedTopicProjection(ctx context.Context, projection primitives.DerivedTopicProjection) error {
	if s.failProjectionAfterEmit && s.boardEventAppended && !s.projectionFailed {
		s.projectionFailed = true
		return errors.New("derived projection refresh failed")
	}
	return s.PrimitiveStore.PutDerivedTopicProjection(ctx, projection)
}

func newPrimitivesTestServerWithStore(t *testing.T, workspace *storage.Workspace, primitiveStore PrimitiveStore) primitivesTestHarness {
	t.Helper()

	contractPath := filepath.Join("..", "..", "..", "contracts", "anx-schema.yaml")
	contract, err := schema.Load(contractPath)
	if err != nil {
		t.Fatalf("load schema contract: %v", err)
	}

	registry := actors.NewStore(workspace.DB())
	maintainer := NewProjectionMaintainer(ProjectionMaintainerConfig{
		PrimitiveStore:   primitiveStore,
		Contract:         contract,
		InboxRiskHorizon: defaultInboxRiskHorizon,
		DirtyBatchSize:   100,
		SystemActorID:    "anx-core",
	})
	handler := NewHandler(
		contract.Version,
		WithHealthCheck(workspace.Ping),
		WithActorRegistry(registry),
		WithPrimitiveStore(primitiveStore),
		WithSchemaContract(contract),
		WithProjectionMaintainer(maintainer),
		WithAllowUnauthenticatedWrites(true),
		WithEnableDevActorMode(true),
	)
	server := httptest.NewServer(newProjectionMaintainerAutoStepHandler(handler, maintainer))
	t.Cleanup(func() {
		server.Close()
	})

	return primitivesTestHarness{workspace: workspace, baseURL: server.URL, maintainer: maintainer, primitiveStore: primitiveStore}
}

func TestBoardCreateSucceedsWhenLifecycleEventAppendFails(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	baseStore := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	h := newPrimitivesTestServerWithStore(t, workspace, &boardLifecycleFailureStore{
		PrimitiveStore:      baseStore,
		appendBoardEventErr: errors.New("board event append failed"),
	})

	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated).Body.Close()
	primaryThreadID := createBoardThreadViaHTTP(t, h, "Board primary thread")

	createBoardResp := postJSONExpectStatus(t, h.baseURL+"/boards", `{
		"actor_id":"actor-1",
		"board":{
			"title":"Ops Board",
			"refs":["thread:`+primaryThreadID+`"]
		}
	}`, http.StatusCreated)
	defer createBoardResp.Body.Close()

	var payload struct {
		Board map[string]any `json:"board"`
	}
	if err := json.NewDecoder(createBoardResp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode create board response: %v", err)
	}
	if asString(payload.Board["id"]) == "" {
		t.Fatalf("expected created board payload, got %#v", payload)
	}

	resp, err := http.Get(h.baseURL + "/boards/" + asString(payload.Board["id"]))
	if err != nil {
		t.Fatalf("GET /boards/{id}: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected board to be persisted despite event failure, got %d", resp.StatusCode)
	}
}

func TestBoardAddCardSucceedsWhenLifecycleProjectionRefreshFails(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	baseStore := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	store := &boardLifecycleFailureStore{
		PrimitiveStore: baseStore,
	}
	h := newPrimitivesTestServerWithStore(t, workspace, store)

	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated).Body.Close()
	primaryThreadID := createBoardThreadViaHTTP(t, h, "Board primary thread")
	memberThreadID := createBoardThreadViaHTTP(t, h, "Board member thread")

	createBoardResp := postJSONExpectStatus(t, h.baseURL+"/boards", `{
		"actor_id":"actor-1",
		"board":{
			"title":"Ops Board",
			"refs":["thread:`+primaryThreadID+`"]
		}
	}`, http.StatusCreated)
	defer createBoardResp.Body.Close()

	var createBoardPayload struct {
		Board map[string]any `json:"board"`
	}
	if err := json.NewDecoder(createBoardResp.Body).Decode(&createBoardPayload); err != nil {
		t.Fatalf("decode create board response: %v", err)
	}
	boardID := asString(createBoardPayload.Board["id"])
	boardUpdatedAt := asString(createBoardPayload.Board["updated_at"])
	store.failProjectionAfterEmit = true

	addCardResp := postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards", `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+boardUpdatedAt+`",
		"title":"Reliability card",
		"related_refs":["thread:`+memberThreadID+`"],
		"column_key":"ready"
	}`, http.StatusCreated)
	defer addCardResp.Body.Close()

	var addCardPayload struct {
		Board map[string]any `json:"board"`
		Card  map[string]any `json:"card"`
	}
	if err := json.NewDecoder(addCardResp.Body).Decode(&addCardPayload); err != nil {
		t.Fatalf("decode add board card response: %v", err)
	}
	if !cardRelatedRefsContainThread(addCardPayload.Card, memberThreadID) {
		t.Fatalf("expected created board card payload, got %#v", addCardPayload)
	}

	listCardsResp, err := http.Get(h.baseURL + "/boards/" + boardID + "/cards")
	if err != nil {
		t.Fatalf("GET /boards/{id}/cards: %v", err)
	}
	defer listCardsResp.Body.Close()
	if listCardsResp.StatusCode != http.StatusOK {
		t.Fatalf("expected board cards to be persisted despite projection refresh failure, got %d", listCardsResp.StatusCode)
	}
	var listCardsPayload struct {
		Cards []map[string]any `json:"cards"`
	}
	if err := json.NewDecoder(listCardsResp.Body).Decode(&listCardsPayload); err != nil {
		t.Fatalf("decode board cards response: %v", err)
	}
	if len(listCardsPayload.Cards) != 1 || !cardRelatedRefsContainThread(listCardsPayload.Cards[0], memberThreadID) {
		t.Fatalf("expected persisted board card after non-fatal projection refresh error, got %#v", listCardsPayload.Cards)
	}
}
