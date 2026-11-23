package server

import (
	"context"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

func TestRespondInboxItemsSkipsManyAndReportsEachItem(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	ctx := context.Background()
	store := env.primitiveStore.(*primitives.Store)
	requester := seedNotificationTestAgent(t, env, "many.requester")
	human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "many-human", "many-human-actor", "many-human", "many-human-token")
	work, err := store.CreateWork(ctx, requester.ActorID, "", map[string]any{"title": "Bulk skip subject", "phase": "in_progress"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := store.GetBoardCard(ctx, "", anyString(work["id"]))
	if err != nil {
		t.Fatal(err)
	}
	fileAsk := func(title string) string {
		t.Helper()
		ask, e := store.AppendTaskAttentionEvent(ctx, requester.ActorID, map[string]any{"type": "human_attention_requested", "thread_id": card["thread_id"], "refs": []string{anyString(card["ref"]), "thread:" + anyString(card["thread_id"])}, "summary": title, "payload": map[string]any{"kind": "ask", "title": title, "requester_actor_id": requester.ActorID, "subject_ref": card["ref"], "response_proposals": []any{"Yes", "No"}, "recommended_response_index": 0, "revision": 1, "by": "2099-01-01T00:00:00Z", "delivery": "batch"}})
		if e != nil {
			t.Fatal(e)
		}
		if e := refreshDerivedTopicProjection(ctx, handlerOptions{primitiveStore: store}, anyString(card["thread_id"]), time.Now().UTC(), requester.ActorID); e != nil {
			t.Fatal(e)
		}
		item, ok := deriveHumanAttentionInboxItem(ask)
		if !ok {
			t.Fatal("missing inbox item")
		}
		return item.ID
	}
	first, second, answered := fileAsk("First?"), fileAsk("Second?"), fileAsk("Answered elsewhere?")
	status, p := hostHTTP(t, "POST", env.server.URL+"/inbox/"+answered+"/respond", human.AccessToken, map[string]any{"response_text": "Yes", "outcome": "answered", "idempotency_key": "elsewhere"})
	hostStatus(t, status, 201, p)

	skip := func(id, key string) map[string]any {
		return map[string]any{"inbox_item_id": id, "response_text": "Skipped without a decision.", "outcome": "skipped", "notify_mode": "original", "idempotency_key": key, "quiet_idempotency_key": key + "-quiet"}
	}
	body := map[string]any{"items": []any{skip(first, "k1"), skip(second, "k2"), skip(answered, "k3"), skip("inbox:ask:missing:ev_x:ev_x", "k4")}}
	path := env.server.URL + "/inbox/responses"

	status, p = hostHTTP(t, "POST", path, requester.AccessToken, body)
	hostStatus(t, status, 403, p)

	status, p = hostHTTP(t, "POST", path, human.AccessToken, body)
	hostStatus(t, status, 200, p)
	want := []string{"responded", "responded", "closed", "failed"}
	results, _ := p["results"].([]any)
	if len(results) != len(want) {
		t.Fatalf("results %#v", p)
	}
	for i, raw := range results {
		result, _ := raw.(map[string]any)
		if result["status"] != want[i] {
			t.Fatalf("item %d: want %s, got %#v", i, want[i], result)
		}
	}

	// Sending the same chunk again replays each response by its key.
	status, p = hostHTTP(t, "POST", path, human.AccessToken, body)
	hostStatus(t, status, 200, p)
	results, _ = p["results"].([]any)
	for i := 0; i < 2; i++ {
		if result, _ := results[i].(map[string]any); result["status"] != "responded" {
			t.Fatalf("replay %d: %#v", i, result)
		}
	}

	for _, bad := range []map[string]any{
		{"items": []any{}},
		{"items": []any{skip(first, "a"), skip(first, "b")}},
		{"items": []any{map[string]any{"outcome": "skipped"}}},
	} {
		status, p = hostHTTP(t, "POST", path, human.AccessToken, bad)
		hostStatus(t, status, 400, p)
	}
}
