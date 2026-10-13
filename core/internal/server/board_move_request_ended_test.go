package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

// A move whose request ends while queued behind a held writer lease is
// contention, not an internal fault. The response must be the retryable busy
// classification, never the opaque internal error, the card must be
// unchanged, and a retry after release must succeed.
func TestBoardMoveRequestEndedWhileQueuedReturnsTypedContention(t *testing.T) {
	requireIntegrationTest(t)
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	// The serving pool keeps a busy window far shorter than production but
	// longer than the cancellation delay, so the request ends mid-wait
	// deterministically and the queued move still runs the real production
	// lock path to its busy classification.
	pool, err := sql.Open("sqlite", "file:"+ws.Layout().DatabasePath+"?_pragma=busy_timeout(1500)&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := primitives.NewTestStore(pool, ws.Layout().ArtifactContentDir)
	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{"title": "Queued board"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := store.CreateWork(ctx, "actor-1", anyString(board["id"]), map[string]any{"title": "Queued card", "phase": "blocked"})
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
	handler := NewHandler(contract.Version, WithPrimitiveStore(store), WithSchemaContract(contract), WithActorRegistry(actors.NewStore(ws.DB())), WithAllowUnauthenticatedWrites(true), WithEnableDevActorMode(true))
	server := httptest.NewServer(handler)
	defer server.Close()
	postJSONExpectStatus(t, server.URL+"/actors", `{"actor":{"id":"actor-1","display_name":"Writer","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated).Body.Close()

	// Hold the workspace write lease on the production pool.
	held, err := ws.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback()

	body, _ := json.Marshal(map[string]any{"actor_id": "actor-1", "column_key": "in_progress", "if_board_updated_at": board["updated_at"]})
	requestCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "/cards/"+anyString(card["id"])+"/move", strings.NewReader(string(body))).WithContext(requestCtx)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	complete := make(chan struct{})
	go func() {
		defer close(complete)
		handler.ServeHTTP(rec, req)
	}()
	select {
	case <-complete:
		t.Fatalf("move finished while the writer lease was held: status=%d body=%s", rec.Code, rec.Body.String())
	case <-time.After(300 * time.Millisecond):
	}
	cancel()
	<-complete
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("request-ended queued move: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err = json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	errBody := payload["error"].(map[string]any)
	if errBody["code"] != "busy" || errBody["recoverable"] != true {
		t.Fatalf("missing retryable contention classification: %v", payload)
	}
	if rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("missing Retry-After: %v", rec.Header())
	}
	after, err := store.GetWork(ctx, anyString(card["ref"]))
	if err != nil || after["phase"] != card["phase"] || after["version"] != card["version"] {
		t.Fatalf("ended move changed state: %v %v", after, err)
	}
	if err = held.Rollback(); err != nil {
		t.Fatal(err)
	}
	retry := httptest.NewRequest(http.MethodPost, "/cards/"+anyString(card["id"])+"/move", strings.NewReader(string(body)))
	retry.Header.Set("Content-Type", "application/json")
	retryRec := httptest.NewRecorder()
	handler.ServeHTTP(retryRec, retry)
	if retryRec.Code != http.StatusOK {
		t.Fatalf("retry after release: status=%d body=%s", retryRec.Code, retryRec.Body.String())
	}
}
