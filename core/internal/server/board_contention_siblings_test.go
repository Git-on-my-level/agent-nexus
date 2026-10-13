package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

// contendedStore fails only the card/work mutation methods with the
// configured error; every other call passes through to the real store.
type contendedStore struct {
	PrimitiveStore
	WorkStore
	err error
}

func (s *contendedStore) CreateBoardCard(ctx context.Context, actorID, boardID string, input primitives.AddBoardCardInput) (primitives.BoardCardMutationResult, error) {
	return primitives.BoardCardMutationResult{}, s.err
}

func (s *contendedStore) CreateBoardCardsBatch(ctx context.Context, actorID, boardID string, ifBoard *string, inputs []primitives.AddBoardCardInput) ([]primitives.BoardCardMutationResult, error) {
	return nil, s.err
}

func (s *contendedStore) UpdateBoardCard(ctx context.Context, actorID, boardID, identifier string, input primitives.UpdateBoardCardInput) (primitives.BoardCardMutationResult, error) {
	return primitives.BoardCardMutationResult{}, s.err
}

func (s *contendedStore) ArchiveBoardCard(ctx context.Context, actorID, boardID, identifier string, input primitives.RemoveBoardCardInput) (primitives.BoardCardMutationResult, error) {
	return primitives.BoardCardMutationResult{}, s.err
}

func (s *contendedStore) TrashBoardCard(ctx context.Context, actorID, boardID, identifier string, reason string, input primitives.RemoveBoardCardInput) (primitives.BoardCardMutationResult, error) {
	return primitives.BoardCardMutationResult{}, s.err
}

func (s *contendedStore) CreateWork(ctx context.Context, actorID, boardID string, input map[string]any) (map[string]any, error) {
	return nil, s.err
}

func (s *contendedStore) GetWork(ctx context.Context, id string) (map[string]any, error) {
	return s.WorkStore.GetWork(ctx, id)
}

func (s *contendedStore) ListWork(ctx context.Context, filter primitives.WorkListFilter) (primitives.WorkPage, error) {
	return s.WorkStore.ListWork(ctx, filter)
}

func (s *contendedStore) PatchWork(ctx context.Context, actor, identifier string, version int64, patch map[string]any) (map[string]any, error) {
	return s.WorkStore.PatchWork(ctx, actor, identifier, version, patch)
}

func (s *contendedStore) SubmitWorkObservation(ctx context.Context, actor, identifier string, input map[string]any) (map[string]any, error) {
	return s.WorkStore.SubmitWorkObservation(ctx, actor, identifier, input)
}

func (s *contendedStore) ListWorkObservations(ctx context.Context, identifier string, limit int, cursor string) ([]map[string]any, string, error) {
	return s.WorkStore.ListWorkObservations(ctx, identifier, limit, cursor)
}

func (s *contendedStore) RequestWorkRefresh(ctx context.Context, actor, identifier string) (map[string]any, error) {
	return s.WorkStore.RequestWorkRefresh(ctx, actor, identifier)
}

// Every sibling card/work mutation classifies a request that ends before the
// store acquires the workspace writer as retryable contention — never as an
// opaque internal error.
func TestSiblingMutationsClassifyRequestEndedAsBusy(t *testing.T) {
	requireIntegrationTest(t)
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	realStore := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	board, err := realStore.CreateBoard(ctx, "actor-1", map[string]any{"title": "Sibling board"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := realStore.CreateWork(ctx, "actor-1", anyString(board["id"]), map[string]any{"title": "Sibling card", "phase": "ready"})
	if err != nil {
		t.Fatal(err)
	}
	board, err = realStore.GetBoard(ctx, anyString(board["id"]))
	if err != nil {
		t.Fatal(err)
	}
	contract, err := schema.Load(filepath.Join("..", "..", "..", "contracts", "anx-schema.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	stub := &contendedStore{PrimitiveStore: realStore, err: fmt.Errorf("store call ended: %w", context.Canceled)}
	handler := NewHandler(contract.Version, WithPrimitiveStore(stub), WithSchemaContract(contract), WithActorRegistry(actors.NewStore(ws.DB())), WithAllowUnauthenticatedWrites(true), WithEnableDevActorMode(true))
	server := httptest.NewServer(handler)
	defer server.Close()
	postJSONExpectStatus(t, server.URL+"/actors", `{"actor":{"id":"actor-1","display_name":"Writer","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated).Body.Close()

	token := anyString(board["updated_at"])
	cardID := anyString(card["id"])
	boardID := anyString(board["id"])
	for name, tc := range map[string]struct {
		method string
		path   string
		body   string
	}{
		"card create":     {http.MethodPost, "/boards/" + boardID + "/cards", `{"title":"Created card","actor_id":"actor-1"}`},
		"cards batch add": {http.MethodPost, "/boards/" + boardID + "/cards/batch", `{"items":[{"title":"Batched card"}],"actor_id":"actor-1"}`},
		"card update":     {http.MethodPatch, "/cards/" + cardID, `{"patch":{"title":"Renamed"},"if_updated_at":"` + token + `","actor_id":"actor-1"}`},
		"card archive":    {http.MethodPost, "/cards/" + cardID + "/archive", `{"if_board_updated_at":"` + token + `","actor_id":"actor-1"}`},
		"card trash":      {http.MethodPost, "/cards/" + cardID + "/trash", `{"reason":"cleanup","if_board_updated_at":"` + token + `","actor_id":"actor-1"}`},
		"work create":     {http.MethodPost, "/work", `{"title":"Created work","actor_id":"actor-1"}`},
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			var payload map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			errBody := payload["error"].(map[string]any)
			if errBody["code"] != "busy" || errBody["recoverable"] != true {
				t.Fatalf("missing retryable contention classification: %v", payload)
			}
			if rec.Header().Get("Retry-After") != "1" {
				t.Fatalf("missing Retry-After: %v", rec.Header())
			}
			if bytes.Contains(rec.Body.Bytes(), []byte("canceled-by-store")) || bytes.Contains(rec.Body.Bytes(), []byte("store call ended")) {
				t.Fatalf("response leaked the cause: %s", rec.Body.String())
			}
		})
	}
}
