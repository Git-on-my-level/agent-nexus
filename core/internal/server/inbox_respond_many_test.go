package server

import (
	"context"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

type respondManyFixture struct {
	env       authIntegrationEnv
	human     lockoutPrincipalSeed
	fileAsk   func(title string, payload map[string]any) string
	requester string
}

func newRespondManyFixture(t *testing.T, name string, options authIntegrationOptions) respondManyFixture {
	t.Helper()
	options.bootstrapToken = testBootstrapToken
	env := newAuthIntegrationEnv(t, options)
	ctx := context.Background()
	store := env.primitiveStore.(*primitives.Store)
	requester := seedNotificationTestAgent(t, env, name+".requester")
	human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), name+"-human", name+"-human-actor", name+"-human", name+"-human-token")
	work, err := store.CreateWork(ctx, requester.ActorID, "", map[string]any{"title": "Bulk skip subject", "phase": "in_progress"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := store.GetBoardCard(ctx, "", anyString(work["id"]))
	if err != nil {
		t.Fatal(err)
	}
	fileAsk := func(title string, extra map[string]any) string {
		t.Helper()
		payload := map[string]any{"kind": "ask", "title": title, "requester_actor_id": requester.ActorID, "subject_ref": card["ref"], "response_proposals": []any{"Yes", "No"}, "recommended_response_index": 0, "revision": 1, "by": "2099-01-01T00:00:00Z", "delivery": "batch"}
		for k, v := range extra {
			payload[k] = v
		}
		ask, e := store.AppendTaskAttentionEvent(ctx, requester.ActorID, map[string]any{"type": "human_attention_requested", "thread_id": card["thread_id"], "refs": []string{anyString(card["ref"]), "thread:" + anyString(card["thread_id"])}, "summary": title, "payload": payload})
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
	return respondManyFixture{env: env, human: human, fileAsk: fileAsk, requester: requester.AccessToken}
}

func skipItem(id, key string) map[string]any {
	return map[string]any{"inbox_item_id": id, "response_text": "Skipped without a decision.", "outcome": "skipped", "notify_mode": "original", "idempotency_key": key, "quiet_idempotency_key": key + "-quiet"}
}

func respondManyStatuses(t *testing.T, p map[string]any) []string {
	t.Helper()
	results, _ := p["results"].([]any)
	out := make([]string, 0, len(results))
	for _, raw := range results {
		result, _ := raw.(map[string]any)
		out = append(out, anyString(result["status"]))
	}
	return out
}

func TestRespondInboxItemsSkipsManyAndReportsEachItem(t *testing.T) {
	requireIntegrationTest(t)
	f := newRespondManyFixture(t, "many", authIntegrationOptions{})
	env, human := f.env, f.human
	first, second, answered := f.fileAsk("First?", nil), f.fileAsk("Second?", nil), f.fileAsk("Answered elsewhere?", nil)
	status, p := hostHTTP(t, "POST", env.server.URL+"/inbox/"+answered+"/respond", human.AccessToken, map[string]any{"response_text": "Yes", "outcome": "answered", "idempotency_key": "elsewhere"})
	hostStatus(t, status, 201, p)

	skip := skipItem
	body := map[string]any{"items": []any{skip(first, "k1"), skip(second, "k2"), skip(answered, "k3"), skip("inbox:ask:missing:ev_x:ev_x", "k4")}}
	path := env.server.URL + "/inbox/responses"

	status, p = hostHTTP(t, "POST", path, f.requester, body)
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

func TestRespondInboxItemsRecordsQuietlyWhenTheRequesterCannotBeWoken(t *testing.T) {
	requireIntegrationTest(t)
	f := newRespondManyFixture(t, "quiet", authIntegrationOptions{})
	orphan := f.fileAsk("Orphaned?", map[string]any{"requester_actor_id": "actor-that-does-not-exist"})
	path := f.env.server.URL + "/inbox/responses"

	// Without a quiet key the item fails as the single route would.
	noQuiet := skipItem(orphan, "loud")
	delete(noQuiet, "quiet_idempotency_key")
	status, p := hostHTTP(t, "POST", path, f.human.AccessToken, map[string]any{"items": []any{noQuiet}})
	hostStatus(t, status, 200, p)
	result := p["results"].([]any)[0].(map[string]any)
	if result["status"] != "failed" || anyString(result["error"].(map[string]any)["code"]) != "notification_target_required" {
		t.Fatalf("without quiet key: %#v", result)
	}

	// With one it is recorded without a wake, under the quiet key.
	status, p = hostHTTP(t, "POST", path, f.human.AccessToken, map[string]any{"items": []any{skipItem(orphan, "loud")}})
	hostStatus(t, status, 200, p)
	result = p["results"].([]any)[0].(map[string]any)
	if result["status"] != "responded" {
		t.Fatalf("with quiet key: %#v", result)
	}
	notify, _ := result["response"].(map[string]any)["notify"].(map[string]any)
	if notify["mode"] != "none" {
		t.Fatalf("quiet response woke someone: %#v", notify)
	}
	// The quiet key holds the response: the single route, sent as the web UI
	// sends it (naming the actor), replays it rather than conflicting.
	status, p = hostHTTP(t, "POST", f.env.server.URL+"/inbox/"+orphan+"/respond", f.human.AccessToken, map[string]any{"actor_id": f.human.ActorID, "response_text": "Skipped without a decision.", "outcome": "skipped", "notify_mode": "none", "idempotency_key": "loud-quiet"})
	if status != 200 && status != 201 {
		t.Fatalf("quiet replay %d %#v", status, p)
	}
}

func TestRespondInboxItemsDefersPastItsBudgets(t *testing.T) {
	requireIntegrationTest(t)
	t.Run("time", func(t *testing.T) {
		f := newRespondManyFixture(t, "budget", authIntegrationOptions{})
		a, b := f.fileAsk("A?", nil), f.fileAsk("B?", nil)
		saved := respondManyBudget
		respondManyBudget = 0
		t.Cleanup(func() { respondManyBudget = saved })
		status, p := hostHTTP(t, "POST", f.env.server.URL+"/inbox/responses", f.human.AccessToken, map[string]any{"items": []any{skipItem(a, "a"), skipItem(b, "b")}})
		hostStatus(t, status, 200, p)
		if got := respondManyStatuses(t, p); got[0] != "deferred" || got[1] != "deferred" {
			t.Fatalf("want every item deferred, got %v", got)
		}
	})
	t.Run("write rate", func(t *testing.T) {
		// A burst of 2 covers the request and one more item; the third waits.
		f := newRespondManyFixture(t, "rate", authIntegrationOptions{routeRateLimits: RouteRateLimits{WriteRequestsPerMinute: 1, WriteBurst: 2}})
		a, b, c := f.fileAsk("A?", nil), f.fileAsk("B?", nil), f.fileAsk("C?", nil)
		status, p := hostHTTP(t, "POST", f.env.server.URL+"/inbox/responses", f.human.AccessToken, map[string]any{"items": []any{skipItem(a, "a"), skipItem(b, "b"), skipItem(c, "c")}})
		hostStatus(t, status, 200, p)
		got := respondManyStatuses(t, p)
		if got[0] != "responded" || got[1] != "responded" || got[2] != "deferred" {
			t.Fatalf("want responded, responded, deferred; got %v", got)
		}
	})
}
