package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

func TestBoardMoveWriterContentionIsRetryableAndAtomic(t *testing.T) {
	requireIntegrationTest(t)
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	// Only the fixture's serving pool has a short busy timeout, so exhausting
	// the same production lock path is deterministic without a 20-second sleep.
	pool, err := sql.Open("sqlite", "file:"+ws.Layout().DatabasePath+"?_pragma=busy_timeout(25)&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := primitives.NewTestStore(pool, ws.Layout().ArtifactContentDir)
	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{"title": "Contended board"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := store.CreateWork(ctx, "actor-1", anyString(board["id"]), map[string]any{"title": "Contended card", "phase": "blocked"})
	if err != nil {
		t.Fatal(err)
	}
	board, err = store.GetBoard(ctx, anyString(board["id"]))
	if err != nil {
		t.Fatal(err)
	}
	contract, err := schema.Load(filepath.Join("..", "..", "..", "contracts", "anx-schema.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewHandler(contract.Version, WithPrimitiveStore(store), WithSchemaContract(contract), WithActorRegistry(actors.NewStore(ws.DB())), WithAllowUnauthenticatedWrites(true), WithEnableDevActorMode(true)))
	defer server.Close()
	postJSONExpectStatus(t, server.URL+"/actors", `{"actor":{"id":"actor-1","display_name":"Writer","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated).Body.Close()
	held, err := ws.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback()
	body, _ := json.Marshal(map[string]any{"actor_id": "actor-1", "column_key": "in_progress", "if_board_updated_at": board["updated_at"]})
	endpoint := server.URL + "/cards/" + url.PathEscape(anyString(card["id"])) + "/move"
	resp := postJSONExpectStatus(t, endpoint, string(body), http.StatusServiceUnavailable)
	var payload map[string]any
	if err = json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.Header.Get("Retry-After") != "1" || payload["error"].(map[string]any)["code"] != "busy" || payload["error"].(map[string]any)["recoverable"] != true {
		t.Fatalf("missing actionable busy response: %v %v", resp.Header, payload)
	}
	after, err := store.GetWork(ctx, anyString(card["ref"]))
	if err != nil || after["phase"] != card["phase"] || after["version"] != card["version"] {
		t.Fatalf("failed move changed state: %v %v", after, err)
	}
	if err = held.Rollback(); err != nil {
		t.Fatal(err)
	}
	postJSONExpectStatus(t, endpoint, string(body), http.StatusOK).Body.Close()
	after, err = store.GetWork(ctx, anyString(card["ref"]))
	if err != nil || after["phase"] != "in_progress" {
		t.Fatalf("retry failed: %v %v", after, err)
	}
}
