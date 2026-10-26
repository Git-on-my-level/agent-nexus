package server

import (
	"context"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

// Decide reads canonical ask facts, while inbox projections are asynchronous.
// A revision and a sibling withdrawal must not make answering depend on that cache.
func TestDecideAnswerOriginalAskIdentity(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	ctx := context.Background()
	store := env.primitiveStore.(*primitives.Store)
	agent := seedNotificationTestAgent(t, env, "answer.requester")
	human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "answer-human", "answer-human-actor", "answer-human", "answer-human-token")
	work, err := store.CreateWork(ctx, agent.ActorID, "", map[string]any{"title": "Answer subject"})
	if err != nil {
		t.Fatal(err)
	}
	thread := anyString(work["thread_id"])
	file := func() map[string]any {
		t.Helper()
		ask, err := store.AppendTaskAttentionEvent(ctx, agent.ActorID, map[string]any{"type": humanAttentionRequestedEventType, "thread_id": thread, "refs": []string{"thread:" + thread, anyString(work["ref"])}, "summary": "Proceed?", "payload": map[string]any{"kind": "ask", "title": "Proceed?", "subject_ref": work["ref"], "requester_actor_id": agent.ActorID, "response_proposals": []string{"Proceed", "Wait"}, "delivery": "none"}})
		if err != nil {
			t.Fatal(err)
		}
		return ask
	}
	for _, tc := range []struct {
		name               string
		revised, projected bool
		closed             string
	}{
		{"unrevised", false, true, ""},
		{"revised-unwrapped-id", true, false, ""},
		{"revised-wrapped-id", true, false, ""},
		{"revised", true, true, ""},
		{"revised-before-projection", true, false, ""},
		{"unrevised-before-projection", false, false, ""},
		{"withdrawn", true, false, "withdrawn"},
		{"retired", true, false, "retired"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ask := file()
			item, _ := deriveHumanAttentionInboxItem(ask)
			if tc.projected {
				if err := refreshDerivedTopicProjection(ctx, handlerOptions{primitiveStore: store}, thread, time.Now().UTC(), agent.ActorID); err != nil {
					t.Fatal(err)
				}
			}
			if tc.revised {
				status, body := hostHTTP(t, "POST", env.server.URL+"/asks/"+url.PathEscape(anyString(ask["ref"]))+"/revise", human.AccessToken, map[string]any{"if_revision": 0, "request_key": "revision-" + tc.name, "presentation": map[string]any{"title": "Clear question?"}})
				hostStatus(t, status, 200, body)
			}
			// A duplicate on the same subject is withdrawn, not this request.
			sibling := file()
			if _, err := store.AppendHumanAttentionWithdrawal(ctx, agent.ActorID, anyString(sibling["id"]), map[string]any{"type": humanAttentionWithdrawnEventType, "thread_id": thread, "refs": []string{"thread:" + thread}, "payload": map[string]any{"request_event_id": sibling["id"], "reason": "Duplicate"}}); err != nil {
				t.Fatal(err)
			}
			switch tc.closed {
			case "withdrawn":
				if _, err := store.AppendHumanAttentionWithdrawal(ctx, agent.ActorID, anyString(ask["id"]), map[string]any{"type": humanAttentionWithdrawnEventType, "thread_id": thread, "refs": []string{"thread:" + thread}, "payload": map[string]any{"request_event_id": ask["id"], "reason": "No longer needed"}}); err != nil {
					t.Fatal(err)
				}
			case "retired":
				status, body := hostHTTP(t, "POST", env.server.URL+"/asks/"+url.PathEscape(anyString(ask["ref"]))+"/retire", human.AccessToken, map[string]any{"request_key": "retire", "reason": "No longer needed", "evidence": []string{anyString(work["ref"])}})
				hostStatus(t, status, 200, body)
			}
			if tc.closed == "" {
				status, brief := hostHTTP(t, "GET", env.server.URL+"/brief", human.AccessToken, nil)
				hostStatus(t, status, 200, brief)
				found := false
				for _, raw := range brief["decisions"].([]any) {
					d := raw.(map[string]any)
					if d["event_ref"] == "event:"+anyString(ask["id"]) {
						found = true
						if d["inbox_item_id"] != item.ID {
							t.Fatalf("identity changed: %#v", d)
						}
					}
				}
				if !found {
					t.Fatal("ask absent from Decide")
				}
			}
			id := item.ID
			if tc.name == "revised-unwrapped-id" {
				id = strings.TrimPrefix(id, "inbox:")
			}
			if tc.name == "revised-wrapped-id" {
				id = "inbox:" + id
			}
			path := env.server.URL + "/inbox/" + url.PathEscape(id) + "/respond"
			request := map[string]any{"response_text": "Proceed with the saved answer", "outcome": "answered", "notify_mode": "none", "idempotency_key": "saved-" + tc.name}
			forged := strings.Replace(item.ID, "inbox:ask:", "inbox:review:", 1)
			status, rejected := hostHTTP(t, "POST", env.server.URL+"/inbox/"+url.PathEscape(forged)+"/respond", human.AccessToken, request)
			hostStatus(t, status, 404, rejected)
			status, answer := hostHTTP(t, "POST", path, human.AccessToken, request)
			if tc.closed != "" {
				hostStatus(t, status, 409, answer)
				return
			}
			hostStatus(t, status, 201, answer)
			var count int
			if err := env.workspace.DB().QueryRowContext(ctx, `SELECT count(*) FROM human_attention_response_claims WHERE request_event_id=?`, ask["id"]).Scan(&count); err != nil || count != 1 {
				t.Fatalf("claims=%d err=%v", count, err)
			}
			payload := answer["event"].(map[string]any)["payload"].(map[string]any)
			resolved, err := store.ResolveResourceRef(ctx, primitives.ResourceRefInput{Type: "event", Ref: anyString(payload["request_event_ref"])})
			if err != nil || resolved.ID != ask["id"] {
				t.Fatalf("answer binding: %#v %v", payload, err)
			}
			// Saved/unsent replay must retain the exact path, body and key after removal.
			if err := refreshDerivedTopicProjection(ctx, handlerOptions{primitiveStore: store}, thread, time.Now().UTC(), agent.ActorID); err != nil {
				t.Fatal(err)
			}
			status, replay := hostHTTP(t, "POST", path, human.AccessToken, request)
			hostStatus(t, status, 201, replay)
			if !reflect.DeepEqual(answer, replay) {
				t.Fatalf("replay changed: %#v", replay)
			}
			request["idempotency_key"] = "second-" + tc.name
			status, closed := hostHTTP(t, "POST", path, human.AccessToken, request)
			hostStatus(t, status, 409, closed)
			if e := closed["error"].(map[string]any); e["code"] != "conflict" || e["message"] != "Ask is no longer open" {
				t.Fatalf("missing explicit resolution: %#v", closed)
			}
		})
	}
	t.Run("private-original-without-projection", func(t *testing.T) {
		ask := file()
		item, _ := deriveHumanAttentionInboxItem(ask)
		card, err := store.GetBoardCard(ctx, "", anyString(work["id"]))
		if err != nil {
			t.Fatal(err)
		}
		board, err := store.GetBoard(ctx, anyString(card["board_id"]))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.PatchThread(ctx, agent.ActorID, anyString(board["thread_id"]), map[string]any{"pm_actor_id": agent.ActorID}, nil); err != nil {
			t.Fatal(err)
		}
		status, body := hostHTTP(t, "POST", env.server.URL+"/inbox/"+url.PathEscape(item.ID)+"/respond", human.AccessToken, map[string]any{"response_text": "Must remain private", "outcome": "answered", "notify_mode": "none", "idempotency_key": "private-original"})
		hostStatus(t, status, 404, body)
		var count int
		if err := env.workspace.DB().QueryRowContext(ctx, `SELECT count(*) FROM human_attention_response_claims WHERE request_event_id=?`, ask["id"]).Scan(&count); err != nil || count != 0 {
			t.Fatalf("private claims=%d err=%v", count, err)
		}
	})
}
