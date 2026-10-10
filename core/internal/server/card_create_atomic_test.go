package server

import (
	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/primitives"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type failedReplayPersistence struct{ PrimitiveStore }

func (s failedReplayPersistence) CreateBoardCard(ctx context.Context, actor, board string, input primitives.AddBoardCardInput) (primitives.BoardCardMutationResult, error) {
	result, err := s.PrimitiveStore.CreateBoardCard(ctx, actor, board, input)
	if err != nil {
		return result, err
	}
	return primitives.BoardCardMutationResult{}, errors.New("response path interrupted after commit")
}

func TestCardCreateConcurrentRequestKey(t *testing.T) {
	h := newProjectionMaintenanceTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Writer","created_at":"2026-10-10T00:00:00Z"}}`, 201).Body.Close()
	board, err := h.store.CreateBoard(context.Background(), "actor-1", map[string]any{"title": "Concurrent"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"actor_id": "actor-1", "request_key": "concurrent", "board_id": board["id"], "if_board_updated_at": board["updated_at"], "card": map[string]any{"title": "Same operation", "summary": "Evidence"}})
	type response struct {
		status int
		id     string
		err    error
	}
	results := make(chan response, 8)
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			<-start
			resp, err := http.Post(h.baseURL+"/cards", "application/json", strings.NewReader(string(raw)))
			if err != nil {
				results <- response{err: err}
				return
			}
			defer resp.Body.Close()
			var body map[string]any
			err = json.NewDecoder(resp.Body).Decode(&body)
			var id string
			if card, ok := body["card"].(map[string]any); ok {
				id = fmt.Sprint(card["id"])
			}
			results <- response{resp.StatusCode, id, err}
		}()
	}
	close(start)
	var id string
	for i := 0; i < 8; i++ {
		r := <-results
		if r.err != nil || r.status != 201 || r.id == "" {
			t.Fatalf("concurrent create: %+v", r)
		}
		if id != "" && id != r.id {
			t.Fatal("duplicate identity")
		}
		id = r.id
	}
	changed := strings.Replace(string(raw), "Same operation", "Different operation", 1)
	postJSONExpectStatus(t, h.baseURL+"/cards", changed, 409).Body.Close()
}

func TestCardCreateReplayRespectsRevokedResourceAccess(t *testing.T) {
	h := newProjectionMaintenanceTestServer(t)
	ctx := context.Background()
	board, err := h.store.CreateBoard(ctx, "owner", map[string]any{"title": "Private replay"})
	if err != nil {
		t.Fatal(err)
	}
	store := h.store.(*primitives.Store)
	if _, err := store.PatchThread(ctx, "owner", board["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	scope := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "owner"})
	_, err = h.store.CreateBoardCard(scope, "owner", board["id"].(string), primitives.AddBoardCardInput{Title: "Private card", CreateReplay: &primitives.CardCreateReplay{Scope: "cards.create", Key: "one", Hash: "hash", Extras: map[string]any{"warnings": []any{map[string]any{"message": "Private card normalized"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.GetIdempotencyReplay(scope, "cards.create", "owner", "one"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.GetIdempotencyReplay(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "stranger"}), "cards.create", "stranger", "one"); !errors.Is(err, primitives.ErrNotFound) {
		t.Fatalf("principal key leaked: %v", err)
	}
	if _, err := h.store.GetIdempotencyReplay(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "stranger"}), "cards.create", "owner", "one"); !errors.Is(err, primitives.ErrNotFound) {
		t.Fatalf("private response leaked through known owner key: %v", err)
	}
	if _, err := store.PatchThread(ctx, "owner", board["thread_id"].(string), map[string]any{"pm_actor_id": "new-owner"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.GetIdempotencyReplay(scope, "cards.create", "owner", "one"); !errors.Is(err, primitives.ErrNotFound) {
		t.Fatalf("revoked replay leaked card/board/warnings: %v", err)
	}
}

func TestCardCreateReplaySurvivesPostCommitFailure(t *testing.T) {
	h := newProjectionMaintenanceTestServer(t)
	ctx := context.Background()
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Writer","created_at":"2026-10-10T00:00:00Z"}}`, 201).Body.Close()
	board, err := h.store.CreateBoard(ctx, "actor-1", map[string]any{"title": "Retry"})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler("test", WithPrimitiveStore(failedReplayPersistence{h.store}), WithActorRegistry(actors.NewStore(h.workspace.DB())), WithAllowUnauthenticatedWrites(true), WithEnableDevActorMode(true))
	server := httptest.NewServer(handler)
	defer server.Close()
	payload := map[string]any{"actor_id": "actor-1", "request_key": "one", "board_id": board["id"], "if_board_updated_at": board["updated_at"], "card": map[string]any{"title": "Create once", "summary": "Evidence", "column_key": "ready"}}
	post := func(expected int) map[string]any {
		raw, _ := json.Marshal(payload)
		resp, err := http.Post(server.URL+"/cards", "application/json", strings.NewReader(string(raw)))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var result map[string]any
		json.NewDecoder(resp.Body).Decode(&result)
		if resp.StatusCode != expected {
			t.Fatalf("status=%d want=%d body=%v", resp.StatusCode, expected, result)
		}
		return result
	}
	post(500)           // canonical commit succeeded, but the old response persistence failed
	replay := post(201) // replay precedes the now-stale board precondition
	if replay["board"].(map[string]any)["card_refs_truncated"] != true {
		t.Fatal("unbounded board echo")
	}
	payload["card"].(map[string]any)["title"] = "Changed request"
	post(409)
	var count int
	if err := h.workspace.DB().QueryRow(`SELECT count(*) FROM cards WHERE board_id=?`, board["id"]).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate cards: %d %v", count, err)
	}
}

func TestCardCreateReplayFailureRollsBackCard(t *testing.T) {
	h := newProjectionMaintenanceTestServer(t)
	ctx := context.Background()
	board, err := h.store.CreateBoard(ctx, "actor", map[string]any{"title": "Atomic"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.workspace.DB().Exec(`CREATE TRIGGER fail_replay BEFORE INSERT ON idempotency_replays BEGIN SELECT RAISE(ABORT,'reject replay'); END`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.store.CreateBoardCard(ctx, "actor", board["id"].(string), primitives.AddBoardCardInput{Title: "Must roll back", CreateReplay: &primitives.CardCreateReplay{Scope: "cards.create", Key: "one", Hash: "hash"}})
	if err == nil {
		t.Fatal("expected replay failure")
	}
	var count int
	if err := h.workspace.DB().QueryRow(`SELECT count(*) FROM cards WHERE board_id=?`, board["id"]).Scan(&count); err != nil || count != 0 {
		t.Fatalf("card committed without replay: %d %v", count, err)
	}
}
