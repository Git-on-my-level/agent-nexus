package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

type eventReplayContentionStore struct {
	PrimitiveStore
	beforePut func()
}

func (s *eventReplayContentionStore) PutIdempotencyReplay(ctx context.Context, scope, actor, key, hash string, status int, response map[string]any) error {
	if s.beforePut != nil {
		s.beforePut()
	}
	return s.PrimitiveStore.PutIdempotencyReplay(ctx, scope, actor, key, hash, status, response)
}

func TestEventAppendWriterContentionIsRetryableAndAtomic(t *testing.T) {
	requireIntegrationTest(t)
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	// Use the production immediate transaction mode, with a shorter wait so a
	// held writer deterministically exhausts admission without a 20-second wait.
	pool, err := sql.Open("sqlite", "file:"+ws.Layout().DatabasePath+"?_pragma=busy_timeout(25)&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := primitives.NewTestStore(pool, ws.Layout().ArtifactContentDir)
	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{"title": "Append contention"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := store.CreateWork(ctx, "actor-1", anyString(board["id"]), map[string]any{"title": "Append subject"})
	if err != nil {
		t.Fatal(err)
	}
	contract, err := schema.Load(filepath.Join("..", "..", "..", "contracts", "anx-schema.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	opts := handlerOptions{primitiveStore: store, contract: contract, actorRegistry: actors.NewStore(ws.DB()), allowUnauthenticatedWrites: true, enableDevActorMode: true}
	if _, err := opts.actorRegistry.Register(ctx, actors.Actor{ID: "actor-1", DisplayName: "Writer", CreatedAt: "2026-03-04T10:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"exception_raised", "message_posted", "human_attention_requested"} {
		t.Run(kind, func(t *testing.T) {
			id := "append-contention-" + kind
			event := map[string]any{"id": id, "type": kind, "thread_id": card["thread_id"], "refs": []string{"thread:" + anyString(card["thread_id"])}, "summary": "Append under contention", "provenance": map[string]any{"sources": []string{"inferred"}}}
			if kind == "exception_raised" {
				event["payload"] = map[string]any{"subtype": "stale_topic"}
			}
			if kind == "message_posted" {
				event["payload"] = map[string]any{"text": "Message under contention"}
			}
			if kind == "human_attention_requested" {
				event["payload"] = map[string]any{"kind": "ask", "title": "Ship now?", "subject_ref": card["ref"], "requester_actor_id": "actor-1", "response_proposals": []string{"Yes", "Wait"}}
			}
			raw, err := json.Marshal(map[string]any{"actor_id": "actor-1", "request_key": id, "event": event})
			if err != nil {
				t.Fatal(err)
			}
			call := func() *httptest.ResponseRecorder {
				out := httptest.NewRecorder()
				handleAppendEvent(out, httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(string(raw))), opts)
				return out
			}
			held, err := ws.DB().BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer held.Rollback()
			out := call()
			if out.Code != 503 || out.Header().Get("Retry-After") != "1" || !strings.Contains(out.Body.String(), `"recoverable":true`) || !strings.Contains(out.Body.String(), `"code":"busy"`) {
				t.Fatalf("lock exhaustion must be retryable: %d %s headers=%v", out.Code, out.Body.String(), out.Header())
			}
			var count int
			if err := ws.DB().QueryRowContext(ctx, `SELECT count(*) FROM events WHERE id=?`, id).Scan(&count); err != nil || count != 0 {
				t.Fatalf("failed append committed event: count=%d err=%v", count, err)
			}
			if err := held.Rollback(); err != nil {
				t.Fatal(err)
			}
			first := call()
			if first.Code != http.StatusCreated {
				t.Fatalf("retry: %d %s", first.Code, first.Body.String())
			}
			if replay := call(); replay.Code != first.Code || replay.Body.String() != first.Body.String() {
				t.Fatalf("replay changed result: %d %s", replay.Code, replay.Body.String())
			}
			if err := ws.DB().QueryRowContext(ctx, `SELECT count(*) FROM events WHERE id=?`, id).Scan(&count); err != nil || count != 1 {
				t.Fatalf("retry lost or duplicated event: count=%d err=%v", count, err)
			}
		})
	}

	t.Run("replay_persistence_after_commit", func(t *testing.T) {
		// An ordinary event commits before its replay row. Hold the writer at
		// that exact seam, then again during conflict recovery on the retry.
		wrapper := &eventReplayContentionStore{PrimitiveStore: store}
		opts.primitiveStore = wrapper
		defer func() { opts.primitiveStore = store }()
		var held *sql.Tx
		defer func() {
			if held != nil {
				_ = held.Rollback()
			}
		}()
		wrapper.beforePut = func() {
			var err error
			held, err = ws.DB().BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
		}
		raw, _ := json.Marshal(map[string]any{"actor_id": "actor-1", "request_key": "late-replay", "event": map[string]any{
			"id": "late-replay-event", "type": "exception_raised", "summary": "Late replay contention", "thread_id": card["thread_id"],
			"refs": []string{"thread:" + anyString(card["thread_id"])}, "payload": map[string]any{"subtype": "stale_topic"}, "provenance": map[string]any{"sources": []string{"inferred"}},
		}})
		call := func() *httptest.ResponseRecorder {
			out := httptest.NewRecorder()
			handleAppendEvent(out, httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(string(raw))), opts)
			return out
		}
		for i := 0; i < 2; i++ {
			out := call()
			if out.Code != 503 || out.Header().Get("Retry-After") != "1" || !strings.Contains(out.Body.String(), `"recoverable":true`) {
				t.Fatalf("replay contention attempt %d: %d %s", i, out.Code, out.Body.String())
			}
			if err := held.Rollback(); err != nil {
				t.Fatal(err)
			}
			held = nil
		}
		wrapper.beforePut = nil
		first := call()
		if first.Code != 201 {
			t.Fatalf("replay recovery: %d %s", first.Code, first.Body.String())
		}
		if replay := call(); replay.Code != first.Code || replay.Body.String() != first.Body.String() {
			t.Fatalf("replay recovery changed result: %d %s", replay.Code, replay.Body.String())
		}
		var count int
		if err := ws.DB().QueryRowContext(ctx, `SELECT count(*) FROM events WHERE id='late-replay-event'`).Scan(&count); err != nil || count != 1 {
			t.Fatalf("late replay lost or duplicated event: count=%d err=%v", count, err)
		}
	})

	// Exercise concurrent asks and ordinary events through the same handler.
	// Production's busy wait must queue these writers without losing writes.
	opts.primitiveStore = primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	const writers = 16
	results := make(chan *httptest.ResponseRecorder, writers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			event := map[string]any{"id": fmt.Sprintf("concurrent-append-%d", i), "type": "message_posted", "thread_id": card["thread_id"], "refs": []string{"thread:" + anyString(card["thread_id"])}, "summary": "Concurrent append", "payload": map[string]any{"text": "Concurrent message"}, "provenance": map[string]any{"sources": []string{"inferred"}}}
			if i%2 == 0 {
				event["type"] = "human_attention_requested"
				event["payload"] = map[string]any{"kind": "ask", "title": "Ship now?", "subject_ref": card["ref"], "requester_actor_id": "actor-1", "response_proposals": []string{"Yes", "Wait"}}
			}
			raw, _ := json.Marshal(map[string]any{"actor_id": "actor-1", "request_key": event["id"], "event": event})
			<-start
			out := httptest.NewRecorder()
			handleAppendEvent(out, httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(string(raw))), opts)
			results <- out
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	for out := range results {
		if out.Code != http.StatusCreated {
			t.Errorf("concurrent append: %d %s", out.Code, out.Body.String())
		}
	}
	var count int
	if err := ws.DB().QueryRowContext(ctx, `SELECT count(*) FROM events WHERE id LIKE 'concurrent-append-%'`).Scan(&count); err != nil || count != writers {
		t.Fatalf("lost concurrent appends: count=%d err=%v", count, err)
	}
}
