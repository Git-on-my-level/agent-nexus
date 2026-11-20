package server

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

func TestKnownAskVisibilityAcrossOwnerSessions(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	s := env.primitiveStore.(*primitives.Store)
	requester := seedMachinePrincipalForLockoutTest(t, ctx, env.workspace.DB(), "ask-requester", "ask-requester-actor", "ask.requester", "ask-requester-token")
	observer := seedMachinePrincipalForLockoutTest(t, ctx, env.workspace.DB(), "ask-observer", "ask-observer-actor", "ask.observer", "ask-observer-token")
	human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "ask-human", "ask-human-actor", "ask-human", "ask-human-token")
	work, err := s.CreateWork(ctx, requester.ActorID, "", map[string]any{"title": "Shared decision"})
	if err != nil {
		t.Fatal(err)
	}
	// Another private root exercises scoped reads even while this ask is shared.
	seedStreamPrivacyThread(t, s, human.ActorID, true)
	ask, err := s.AppendEvent(ctx, requester.ActorID, map[string]any{
		"id": "ev_11111111111111111111111111111111", "type": "human_attention_requested", "thread_id": work["thread_id"], "refs": []string{anyString(work["ref"])},
		"payload": map[string]any{"subject_ref": work["ref"], "requester_actor_id": requester.ActorID, "kind": "ask", "title": "Proceed?", "response_proposals": []string{"Proceed"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	id := anyString(ask["id"])
	readStream := func(t *testing.T, token, ref, want string) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, env.server.URL+"/stream/asks/"+ref, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("stream status %d", resp.StatusCode)
		}
		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var out map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &out); err != nil || out["status"] != want {
				t.Fatalf("stream outcome %#v: %v; want %s", out, err, want)
			}
			return
		}
	}
	for _, state := range []string{"open", "answered"} {
		if state == "answered" {
			_, _, err = s.AppendHumanAttentionResponse(ctx, human.ActorID, id, "event:"+id, "", "", map[string]any{
				"type": "human_attention_responded", "thread_id": ask["thread_id"], "refs": []string{"event:" + id},
				"payload": map[string]any{"outcome": "answered", "response_text": "Recorded answer", "request_event_ref": "event:" + id},
			}, map[string]any{"requested": false})
			if err != nil {
				t.Fatal(err)
			}
		}
		for _, reader := range []struct{ name, token string }{
			{"same_agent_first_session", requester.AccessToken},
			{"same_agent_second_session", requester.AccessToken},
			{"different_authorized_agent", observer.AccessToken},
		} {
			t.Run(state+"/"+reader.name, func(t *testing.T) {
				for _, ref := range []string{id, "event:" + id} {
					status, body := hostHTTP(t, http.MethodGet, env.server.URL+"/asks/"+ref, reader.token, nil)
					if status != 200 || body["status"] != state || body["ask_id"] != "event:"+id {
						t.Fatalf("durable read: %d %#v", status, body)
					}
					readStream(t, reader.token, ref, state)
				}
			})
		}
	}
	t.Run("subscription_stays_requester_only", func(t *testing.T) {
		status, _ := hostHTTP(t, http.MethodPost, env.server.URL+"/asks/"+id+"/subscriptions", observer.AccessToken, map[string]any{"kind": "await", "label": "observer"})
		if status != http.StatusNotFound {
			t.Fatalf("observer subscription status %d", status)
		}
	})
	t.Run("unknown_id", func(t *testing.T) {
		for _, prefix := range []string{"/asks/", "/stream/asks/"} {
			status, _ := hostHTTP(t, http.MethodGet, env.server.URL+prefix+"event:ev_missing", observer.AccessToken, nil)
			if status != http.StatusNotFound {
				t.Fatalf("missing ask status %d", status)
			}
		}
	})
	t.Run("unauthorized_identity", func(t *testing.T) {
		if _, err := s.PatchThread(ctx, requester.ActorID, anyString(work["thread_id"]), map[string]any{"pm_actor_id": requester.ActorID}, nil); err != nil {
			t.Fatal(err)
		}
		for _, prefix := range []string{"/asks/", "/stream/asks/"} {
			status, _ := hostHTTP(t, http.MethodGet, env.server.URL+prefix+"event:"+id, observer.AccessToken, nil)
			if status != http.StatusNotFound {
				t.Fatalf("private ask status %d", status)
			}
		}
		status, body := hostHTTP(t, http.MethodGet, env.server.URL+"/asks/"+id, requester.AccessToken, nil)
		if status != http.StatusOK || body["status"] != "answered" {
			t.Fatalf("private owner lost access: %d %#v", status, body)
		}
	})
}
