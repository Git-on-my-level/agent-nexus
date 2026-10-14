package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"agent-nexus-core/internal/primitives"
)

const durableEventBody = `{"actor_id":"actor-1","request_key":"durable-comment","event":{"type":"message_posted","thread_id":"thread-comment","refs":["thread:thread-comment"],"summary":"Original comment","payload":{"text":"Keep this exact comment"},"provenance":{"sources":["inferred"]}}}`

func TestEventCreateConcurrentAtomicReplay(t *testing.T) {
	h := newProjectionMaintenanceTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Writer","created_at":"2026-10-10T00:00:00Z"}}`, 201).Body.Close()
	start := make(chan struct{})
	results := make(chan string, 8)
	for i := 0; i < 8; i++ {
		go func() {
			<-start
			resp, err := http.Post(h.baseURL+"/events", "application/json", strings.NewReader(durableEventBody))
			if err != nil {
				results <- err.Error()
				return
			}
			defer resp.Body.Close()
			raw, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != 201 {
				results <- fmt.Sprintf("status %d: %s", resp.StatusCode, raw)
				return
			}
			results <- string(raw)
		}()
	}
	close(start)
	first := <-results
	var body map[string]any
	if err := json.Unmarshal([]byte(first), &body); err != nil || body["event"] == nil {
		t.Fatalf("first response: %s", first)
	}
	for i := 1; i < 8; i++ {
		if next := <-results; next != first {
			t.Fatalf("replay differs: %s / %s", first, next)
		}
	}
	postJSONExpectStatus(t, h.baseURL+"/events", strings.Replace(durableEventBody, "Keep this exact comment", "Changed body", 1), 409).Body.Close()
	// An explicit existing event ID is not proof that a different key/body was
	// committed. Fail closed instead of claiming that event as a replay result.
	event := body["event"].(map[string]any)
	collision := strings.Replace(durableEventBody, `"type":"message_posted"`, fmt.Sprintf(`"id":%q,"type":"message_posted"`, event["id"]), 1)
	collision = strings.Replace(collision, "durable-comment", "different-key", 1)
	postJSONExpectStatus(t, h.baseURL+"/events", collision, 409).Body.Close()
	var replays int
	if err := h.workspace.DB().QueryRow(`SELECT count(*) FROM idempotency_replays WHERE scope='events.create' AND request_key='different-key'`).Scan(&replays); err != nil || replays != 0 {
		t.Fatalf("claimed unrelated event: %d %v", replays, err)
	}
	var count int
	if err := h.workspace.DB().QueryRow(`SELECT count(*) FROM events WHERE type='message_posted'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("event count %d: %v", count, err)
	}
}

func TestEventCreateAtomicReplayRespectsRevokedAccess(t *testing.T) {
	h := newProjectionMaintenanceTestServer(t)
	ctx := context.Background()
	board, err := h.store.CreateBoard(ctx, "owner", map[string]any{"title": "Private discussion"})
	if err != nil {
		t.Fatal(err)
	}
	store := h.store.(*primitives.Store)
	thread := board["thread_id"].(string)
	if _, err = store.PatchThread(ctx, "owner", thread, map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	owner := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "owner"})
	event := map[string]any{"id": "private-comment", "type": "message_posted", "thread_id": thread, "refs": []string{fmt.Sprint(board["ref"])}, "payload": map[string]any{"text": "Private comment"}}
	if _, _, err = store.AppendEventWithReplay(owner, "owner", "private-key", "body-hash", event, nil); err != nil {
		t.Fatal(err)
	}
	stranger := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "stranger"})
	if _, err = store.GetIdempotencyReplay(stranger, "events.create", "owner", "private-key"); !errors.Is(err, primitives.ErrNotFound) {
		t.Fatalf("private replay leaked: %v", err)
	}
	if _, err = store.PatchThread(ctx, "owner", thread, map[string]any{"pm_actor_id": "new-owner"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = store.GetIdempotencyReplay(owner, "events.create", "owner", "private-key"); !errors.Is(err, primitives.ErrNotFound) {
		t.Fatalf("revoked replay leaked: %v", err)
	}
	if replay, _, err := store.AppendEventWithReplay(owner, "owner", "private-key", "body-hash", event, nil); err == nil || replay.Response != nil {
		t.Fatalf("atomic retry bypassed revoked ownership: %+v %v", replay, err)
	}
}

func TestEventCreateReplayInsertFailureRollsBackEvent(t *testing.T) {
	h := newProjectionMaintenanceTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Writer","created_at":"2026-10-10T00:00:00Z"}}`, 201).Body.Close()
	// Fault exactly between event insertion and replay insertion. A retry after
	// repairing storage must create one event, never adopt an unrelated ID.
	if _, err := h.workspace.DB().Exec(`CREATE TRIGGER fail_event_replay BEFORE INSERT ON idempotency_replays WHEN NEW.scope='events.create' BEGIN SELECT RAISE(ABORT,'injected replay failure'); END`); err != nil {
		t.Fatal(err)
	}
	postJSONExpectStatus(t, h.baseURL+"/events", durableEventBody, 500).Body.Close()
	var count int
	if err := h.workspace.DB().QueryRow(`SELECT count(*) FROM events WHERE type='message_posted'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partially committed event: %d %v", count, err)
	}
	if _, err := h.workspace.DB().Exec(`DROP TRIGGER fail_event_replay`); err != nil {
		t.Fatal(err)
	}
	a := postJSONExpectStatus(t, h.baseURL+"/events", durableEventBody, 201)
	defer a.Body.Close()
	b := postJSONExpectStatus(t, h.baseURL+"/events", durableEventBody, 201)
	defer b.Body.Close()
	left, _ := io.ReadAll(a.Body)
	right, _ := io.ReadAll(b.Body)
	if string(left) != string(right) {
		t.Fatal("original result was not replayed")
	}
}
