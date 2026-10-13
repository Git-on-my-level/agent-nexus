package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

// enteredMoveStore signals when a move request reaches the store call; with a
// writer lease held, the request is then blocked inside BEGIN IMMEDIATE for
// the rest of the serving pool's busy window.
type enteredMoveStore struct {
	PrimitiveStore
	entered chan struct{}
	once    sync.Once
}

func (s *enteredMoveStore) MoveBoardCard(ctx context.Context, actorID, boardID, identifier string, input primitives.MoveBoardCardInput) (primitives.BoardCardMutationResult, error) {
	s.once.Do(func() { close(s.entered) })
	return s.PrimitiveStore.MoveBoardCard(ctx, actorID, boardID, identifier, input)
}

// A move whose request ends while queued behind a held writer lease is
// contention, not an internal fault: the cancellation only ever lands before
// BEGIN IMMEDIATE acquired the writer, so nothing can have committed and the
// retryable busy classification is safe. The signal — not a blind sleep —
// proves the request reached the blocked store operation first; after the
// signal the request cannot finish before the lease releases, so the outcome
// is deterministic whichever side of BEGIN the cancel lands on.
func TestBoardMoveRequestEndedWhileQueuedReturnsTypedContention(t *testing.T) {
	requireIntegrationTest(t)
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	// The serving pool keeps a busy window far shorter than production but
	// longer than the settle margin, so the queued move really waits on the
	// lease and ends through the real production lock path.
	pool, err := sql.Open("sqlite", "file:"+ws.Layout().DatabasePath+"?_pragma=busy_timeout(1500)&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	realStore := primitives.NewTestStore(pool, ws.Layout().ArtifactContentDir)
	signaled := &enteredMoveStore{PrimitiveStore: realStore, entered: make(chan struct{})}
	board, err := realStore.CreateBoard(ctx, "actor-1", map[string]any{"title": "Queued board"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := realStore.CreateWork(ctx, "actor-1", anyString(board["id"]), map[string]any{"title": "Queued card", "phase": "blocked"})
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
	handler := NewHandler(contract.Version, WithPrimitiveStore(signaled), WithSchemaContract(contract), WithActorRegistry(actors.NewStore(ws.DB())), WithAllowUnauthenticatedWrites(true), WithEnableDevActorMode(true))
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
	case <-signaled.entered:
	case <-complete:
		t.Fatal("move finished while the writer lease was held")
	case <-time.After(5 * time.Second):
		t.Fatal("move request never reached the store")
	}
	// The store call is now blocked on the lease; give the in-tx reads their
	// settled margin so the cancel lands inside the BEGIN IMMEDIATE wait.
	time.Sleep(100 * time.Millisecond)
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
	after, err := realStore.GetWork(ctx, anyString(card["ref"]))
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

// A move that already applied must never apply twice: replaying the same
// request with the original concurrency token conflicts instead of moving
// the card again.
func TestBoardMoveReplayWithAppliedTokenConflictsWithoutSecondMove(t *testing.T) {
	requireIntegrationTest(t)
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	store := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{"title": "Replay board"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := store.CreateWork(ctx, "actor-1", anyString(board["id"]), map[string]any{"title": "Replay card", "phase": "ready"})
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

	body, _ := json.Marshal(map[string]any{"actor_id": "actor-1", "column_key": "in_progress", "if_board_updated_at": board["updated_at"]})
	first := httptest.NewRequest(http.MethodPost, "/cards/"+anyString(card["id"])+"/move", strings.NewReader(string(body)))
	first.Header.Set("Content-Type", "application/json")
	firstRec := httptest.NewRecorder()
	handler.ServeHTTP(firstRec, first)
	if firstRec.Code != http.StatusOK {
		t.Fatalf("first move: status=%d body=%s", firstRec.Code, firstRec.Body.String())
	}
	applied, err := store.GetWork(ctx, anyString(card["ref"]))
	if err != nil {
		t.Fatal(err)
	}

	replay := httptest.NewRequest(http.MethodPost, "/cards/"+anyString(card["id"])+"/move", strings.NewReader(string(body)))
	replay.Header.Set("Content-Type", "application/json")
	replayRec := httptest.NewRecorder()
	handler.ServeHTTP(replayRec, replay)
	if replayRec.Code != http.StatusConflict {
		t.Fatalf("replayed move with applied token: status=%d body=%s", replayRec.Code, replayRec.Body.String())
	}
	after, err := store.GetWork(ctx, anyString(card["ref"]))
	if err != nil || after["phase"] != applied["phase"] || after["version"] != applied["version"] || after["updated_at"] != applied["updated_at"] {
		t.Fatalf("conflicted replay moved the card again: before=%v after=%v err=%v", applied, after, err)
	}
}
