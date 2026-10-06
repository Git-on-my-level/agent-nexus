package server

import (
	"context"
	"encoding/json"
	"testing"

	"agent-nexus-core/internal/primitives"
)

func TestResourceAccessNULJSONHTTP(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	s := env.primitiveStore.(*primitives.Store)
	owner := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "nul-owner", "nul-owner-actor", "nul-owner", "nul-owner-token")
	stranger := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "nul-stranger", "nul-stranger-actor", "nul-stranger", "nul-stranger-token")
	agent := seedMachinePrincipalForLockoutTest(t, ctx, env.workspace.DB(), "nul-agent", "nul-agent-actor", "nul.agent", "nul-agent-token")
	private, _, err := s.CreateDocument(ctx, owner.ActorID, map[string]any{"id": "[]", "title": "private"}, "source", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, owner.ActorID, anyString(private["thread_id"]), map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{owner.AccessToken, stranger.AccessToken, agent.AccessToken} {
		for _, content := range []any{map[string]any{"text": "prose\x00document:[]"}, map[string]any{"key\x00document:[]": "value"}, map[string]any{"nested": `{"text":"prose\u0000document:[]"}`}} {
			resp := postJSONExpectStatusWithAuth(t, env.server.URL+"/docs", map[string]any{"document": map[string]any{"title": "NULRejected"}, "content_type": "structured", "content": content, "refs": []string{}}, token, 400)
			var body map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if body["error"].(map[string]any)["code"] != "invalid_request" {
				t.Fatalf("wrong error shape: %#v", body)
			}
		}
	}
	// Generic event payloads use the same rejection boundary, including owners.
	postJSONExpectStatusWithAuth(t, env.server.URL+"/events", map[string]any{"event": map[string]any{"type": "message_posted", "refs": []string{}, "payload": map[string]any{"text": "prose\x00document:[]"}}}, owner.AccessToken, 400).Body.Close()
	// Ordinary structured content remains readable; a valid private reference
	// still inherits ownership rather than being rejected as invalid text.
	for _, text := range []string{"ordinary public text", "prose document:[]"} {
		resp := postJSONExpectStatusWithAuth(t, env.server.URL+"/docs", map[string]any{"document": map[string]any{"title": "control"}, "content_type": "structured", "content": map[string]any{"text": text}, "refs": []string{}}, owner.AccessToken, 201)
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		id := body["document"].(map[string]any)["id"].(string)
		for _, token := range []string{stranger.AccessToken, agent.AccessToken} {
			want := 200
			if text == "prose document:[]" {
				want = 404
			}
			getJSONExpectStatusWithAuth(t, env.server.URL+"/docs/"+id, token, want).Body.Close()
		}
	}
}
