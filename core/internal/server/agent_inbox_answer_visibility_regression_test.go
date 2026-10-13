package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

// Regression for oss#389: an agent that filed five asks with no delivery
// wiring, whose human answered every one of them, must see all five again
// through /agent-inbox/asks with their answers — the source that powers
// `anx inbox list --status answered` and `anx orient` → my_asks_and_answers.
// The asks carry the delivery mode so both surfaces can show it.
func TestAgentInboxReturnsCallersOwnAnsweredAsks(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	agent := seedNotificationTestAgent(t, env, "ask.agent")
	ctx := context.Background()
	human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "answer-human", "answer-human-actor", "answer-human", "answer-human-token")

	threadID := integrationSeedThreadWithStore(t, env.primitiveStore, nil, agent.ActorID, map[string]any{
		"title": "Five ask thread", "type": "incident", "status": "active", "priority": "p2",
		"tags": []any{"asks"}, "cadence": "daily", "next_check_in_at": "2026-03-06T00:00:00Z",
		"current_summary": "summary", "next_actions": []any{"check"}, "key_artifacts": []any{},
		"provenance": map[string]any{"sources": []any{"inferred"}},
	})

	askIDs := map[string]bool{}
	for i := 1; i <= 5; i++ {
		// Shape matches what the CLI publishes: a client-allocated event id and
		// the delivery mode recorded in the ask payload.
		resp := postJSONExpectStatusWithAuth(t, env.server.URL+"/events", map[string]any{
			"event": map[string]any{
				"type":      "human_attention_requested",
				"thread_id": threadID,
				"summary":   fmt.Sprintf("Decision %d: ship now?", i),
				"refs":      []string{"thread:" + threadID},
				"payload": map[string]any{
					"kind": "ask", "title": fmt.Sprintf("Decision %d: ship now?", i),
					"subject_ref":        "thread:" + threadID,
					"requester_actor_id": agent.ActorID,
					"requester_label":    agent.Username,
					"response_proposals": []any{"Accept", "Wait"},
					"delivery":           "batch",
				},
				"provenance": map[string]any{"sources": []string{"event:cli.ask"}},
			},
		}, agent.AccessToken, http.StatusCreated)
		var created struct {
			Event map[string]any `json:"event"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
			t.Fatalf("decode ask %d: %v", i, err)
		}
		resp.Body.Close()
		askIDs[fmt.Sprint(created.Event["id"])] = true
	}

	if err := refreshDerivedTopicProjection(ctx, handlerOptions{primitiveStore: env.primitiveStore}, threadID, time.Now().UTC(), agent.ActorID); err != nil {
		t.Fatalf("refresh projection: %v", err)
	}
	items, err := env.primitiveStore.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{ThreadID: threadID})
	if err != nil {
		t.Fatalf("list inbox items: %v", err)
	}
	if len(items) != 5 {
		t.Fatalf("expected five open inbox items, got %d", len(items))
	}
	for i, item := range items {
		status, result := hostHTTP(t, http.MethodPost, env.server.URL+"/inbox/"+url.PathEscape(item.ID)+"/respond", human.AccessToken, map[string]any{
			"response_text": fmt.Sprintf("Answer %d", i+1), "outcome": "answered", "notify_mode": "original",
		})
		if status != http.StatusCreated {
			t.Fatalf("answer %d (%s): %d %#v", i+1, item.ID, status, result)
		}
	}

	listResp := getJSONExpectStatusWithAuth(t, env.server.URL+"/agent-inbox/asks?limit=200", agent.AccessToken, http.StatusOK)
	var page struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&page); err != nil {
		t.Fatalf("decode agent inbox: %v", err)
	}
	listResp.Body.Close()
	if len(page.Items) != 5 {
		t.Fatalf("expected five asks for the caller, got %d: %#v", len(page.Items), page.Items)
	}
	for _, item := range page.Items {
		askID := trimEventPrefix(fmt.Sprint(item["ask_id"]))
		if !askIDs[askID] {
			t.Fatalf("unexpected ask in caller inbox: %s", askID)
		}
		if item["answer"] == nil || item["status"] != "answered" {
			t.Fatalf("answered ask %s lost its answer: %#v", askID, item)
		}
		if item["answer_unread"] != true {
			t.Fatalf("fresh answer %s must start unread: %#v", askID, item)
		}
		if item["delivery"] != "batch" {
			t.Fatalf("ask %s lost its recorded delivery mode: %#v", askID, item)
		}
	}
}

func trimEventPrefix(ref string) string {
	if len(ref) > 6 && ref[:6] == "event:" {
		return ref[6:]
	}
	return ref
}
