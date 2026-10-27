package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

func TestCardMoveAtomicReplayOriginalTokensEventsAndEvidence(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	owner := seedNotificationTestAgent(t, env, "owner.movehost")
	store := env.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	work, err := store.CreateWork(ctx, owner.ActorID, "", map[string]any{"title": "Durable move"})
	if err != nil {
		t.Fatal(err)
	}
	peer, err := store.CreateWork(ctx, owner.ActorID, "", map[string]any{"title": "Placement peer", "phase": "ready"})
	if err != nil {
		t.Fatal(err)
	}
	proof, err := store.AppendEvent(ctx, owner.ActorID, map[string]any{"type": "message_posted", "thread_id": work["thread_id"], "refs": []string{anyString(work["ref"]), "thread:" + anyString(work["thread_id"])}, "payload": map[string]any{"text": "Completion evidence"}})
	if err != nil {
		t.Fatal(err)
	}
	contract, err := schema.Load("../../../contracts/anx-schema.yaml")
	if err != nil {
		t.Fatal(err)
	}
	card, err := store.GetBoardCard(ctx, "", anyString(work["id"]))
	if err != nil {
		t.Fatal(err)
	}
	boardID := anyString(card["board_id"])
	board, err := store.GetBoard(ctx, boardID)
	if err != nil {
		t.Fatal(err)
	}
	originalVersion := work["version"].(int64)
	primitives.WithWorkspaceQuota(primitives.WorkspaceQuota{MaxArtifacts: 1000, MaxBlobBytes: 1 << 30})(store)
	env.workspace.DB().SetMaxOpenConns(1)
	scope := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: owner.ActorID})
	opts := handlerOptions{primitiveStore: store, authStore: env.authStore, actorRegistry: env.registry, contract: contract, workspaceID: "ws_main"}
	body := map[string]any{"actor_id": owner.ActorID, "request_key": "original-move", "column_key": "ready", "before_card_id": peer["ref"], "if_board_updated_at": board["updated_at"], "if_version": originalVersion}
	write := func(body map[string]any, card string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/cards/"+card+"/move", strings.NewReader(string(raw))).WithContext(scope)
		cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: owner.ActorID})
		out := httptest.NewRecorder()
		handleMoveCardMutation(out, req, opts, "", card, "card not found")
		return out
	}
	snapshot := func() string {
		card, err := store.GetBoardCard(ctx, "", anyString(work["id"]))
		if err != nil {
			t.Fatal(err)
		}
		b, err := store.GetBoard(ctx, boardID)
		if err != nil {
			t.Fatal(err)
		}
		w, err := store.GetWork(ctx, anyString(work["ref"]))
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal([]any{card, b, w})
		out := string(raw)
		for _, table := range []string{"events", "ref_edges", "derived_topic_dirty_queue", "idempotency_replays"} {
			var count int
			if err := env.workspace.DB().QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil {
				t.Fatal(err)
			}
			out += fmt.Sprintf("%s=%d;", table, count)
		}
		var dirty string
		if err := env.workspace.DB().QueryRow(`SELECT COALESCE(group_concat(thread_id || ':' || dirty_at),'') FROM (SELECT thread_id,dirty_at FROM derived_topic_dirty_queue ORDER BY thread_id)`).Scan(&dirty); err != nil {
			t.Fatal(err)
		}
		out += dirty
		return out
	}
	assertRollback := func(trigger string, request map[string]any) {
		before := snapshot()
		if _, err := env.workspace.DB().Exec(trigger); err != nil {
			t.Fatal(err)
		}
		out := write(request, anyString(work["ref"]))
		if out.Code == 200 {
			t.Fatal("failed mutation confirmed", out.Body.String())
		}
		if after := snapshot(); before != after {
			t.Fatalf("partial move survived: before %s after %s response %s", before, after, out.Body.String())
		}
		if _, err := env.workspace.DB().Exec("DROP TRIGGER reject_move_write"); err != nil {
			t.Fatal(err)
		}
	}
	for _, trigger := range []string{
		`CREATE TRIGGER reject_move_write BEFORE INSERT ON idempotency_replays WHEN NEW.scope='cards.move' BEGIN SELECT RAISE(ABORT,'receipt failed'); END`,
		`CREATE TRIGGER reject_move_write BEFORE INSERT ON events WHEN NEW.type='card_moved' BEGIN SELECT RAISE(ABORT,'move event failed'); END`,
	} {
		assertRollback(trigger, body)
	}
	// The optional task token fences an annotation change even when the board
	// timestamp is unchanged; the original board alone is not a sufficient base.
	annotated, err := store.PatchWork(ctx, owner.ActorID, anyString(work["ref"]), originalVersion, map[string]any{"next_action": "A newer annotation"})
	if err != nil {
		t.Fatal(err)
	}
	unchangedBoard, err := store.GetBoard(ctx, boardID)
	if err != nil || unchangedBoard["updated_at"] != board["updated_at"] {
		t.Fatal("annotation changed board token", err)
	}
	beforeStale := snapshot()
	if out := write(body, anyString(work["ref"])); out.Code != 409 {
		t.Fatal("stale task token accepted", out.Code, out.Body.String())
	}
	if snapshot() != beforeStale {
		t.Fatal("stale move changed canonical state")
	}
	body["if_version"] = annotated["version"]
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 8)
	for i := 0; i < 8; i++ {
		go func() { <-start; results <- write(body, anyString(work["ref"])) }()
	}
	close(start)
	first := <-results
	if first.Code != 200 {
		t.Fatal(first.Code, first.Body.String())
	}
	for i := 1; i < 8; i++ {
		if next := <-results; next.Code != 200 || next.Body.String() != first.Body.String() {
			t.Fatal("replay changed", next.Code, next.Body.String())
		}
	}
	var eventCount int
	if err := env.workspace.DB().QueryRow(`SELECT count(*) FROM events WHERE type='card_moved'`).Scan(&eventCount); err != nil || eventCount != 1 {
		t.Fatalf("duplicate move event %d %v", eventCount, err)
	}
	for field, value := range map[string]any{"column_key": "review", "before_card_id": "", "if_board_updated_at": "2026-10-01T00:00:00Z", "if_version": -1} {
		changed := copyMutationBody(body)
		changed[field] = value
		if out := write(changed, anyString(work["ref"])); out.Code != 409 {
			t.Fatal("changed request accepted", field, out.Code, out.Body.String())
		}
	}
	if out := write(body, anyString(peer["ref"])); out.Code != 409 {
		t.Fatal("changed target accepted", out.Code, out.Body.String())
	}
	var original map[string]any
	if err := json.Unmarshal(first.Body.Bytes(), &original); err != nil {
		t.Fatal(err)
	}
	current, err := store.GetWork(ctx, anyString(work["ref"]))
	if err != nil {
		t.Fatal(err)
	}
	completion := map[string]any{"actor_id": owner.ActorID, "request_key": "completion", "column_key": "done", "resolution": "done", "resolution_refs": []string{anyString(proof["ref"])}, "if_board_updated_at": original["board"].(map[string]any)["updated_at"], "if_version": current["version"]}
	var updatedBeforeCompletion int
	if err := env.workspace.DB().QueryRow(`SELECT count(*) FROM events WHERE type='card_updated'`).Scan(&updatedBeforeCompletion); err != nil {
		t.Fatal(err)
	}
	assertRollback(`CREATE TRIGGER reject_move_write BEFORE INSERT ON events WHEN NEW.type='card_updated' BEGIN SELECT RAISE(ABORT,'resolution event failed'); END`, completion)
	done := write(completion, anyString(work["ref"]))
	if done.Code != 200 {
		t.Fatal(done.Code, done.Body.String())
	}
	if replay := write(completion, anyString(work["ref"])); replay.Code != 200 || replay.Body.String() != done.Body.String() {
		t.Fatal("completion replay changed", replay.Code, replay.Body.String())
	}
	var moved, updated int
	if err := env.workspace.DB().QueryRow(`SELECT count(*) FROM events WHERE type='card_moved'`).Scan(&moved); err != nil {
		t.Fatal(err)
	}
	if err := env.workspace.DB().QueryRow(`SELECT count(*) FROM events WHERE type='card_updated'`).Scan(&updated); err != nil {
		t.Fatal(err)
	}
	if moved != 2 || updated != updatedBeforeCompletion+1 {
		t.Fatalf("duplicate or missing completion events: %d moved, %d updated (before completion %d)", moved, updated, updatedBeforeCompletion)
	}
	changedEvidence := copyMutationBody(completion)
	changedEvidence["resolution_refs"] = []string{"event:other"}
	if out := write(changedEvidence, anyString(work["ref"])); out.Code != 409 {
		t.Fatal("changed evidence accepted", out.Code, out.Body.String())
	}
	if out := write(body, anyString(work["ref"])); out.Code != 200 || out.Body.String() != first.Body.String() {
		t.Fatal("original replay returned newer move", out.Code, out.Body.String())
	}
	for _, key := range []string{strings.Repeat("x", 257), "invalid\x00key"} {
		invalid := copyMutationBody(body)
		invalid["request_key"] = key
		if out := write(invalid, anyString(work["ref"])); out.Code != 400 {
			t.Fatal("invalid key accepted", out.Code, out.Body.String())
		}
	}
	if _, err := store.PatchThread(ctx, owner.ActorID, anyString(work["thread_id"]), map[string]any{"pm_actor_id": "replacement"}, nil); err != nil {
		t.Fatal(err)
	}
	if out := write(body, anyString(work["ref"])); out.Code != 404 {
		t.Fatal("revoked replay accepted", out.Code, out.Body.String())
	}
}

func copyMutationBody(body map[string]any) map[string]any {
	copy := map[string]any{}
	for key, value := range body {
		copy[key] = value
	}
	return copy
}
