package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"agent-nexus-core/internal/primitives"
)

func TestPMNativeRoutesHideConversationFromOtherPrincipals(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "pm-owner", "pm-owner-actor", "pm-owner", "pm-owner-token")
	stranger := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "pm-stranger", "pm-stranger-actor", "pm-stranger", "pm-stranger-token")
	store := env.primitiveStore.(*primitives.Store)

	created, err := store.CreateThread(ctx, owner.ActorID, map[string]any{
		"title":              "Project manager",
		"pm_actor_id":        owner.ActorID,
		"pm_conversation_id": "conv-owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	threadID := anyString(created.Thread["id"])
	if threadID == "" {
		t.Fatalf("missing thread id: %#v", created.Thread)
	}
	event, err := store.AppendEvent(ctx, owner.ActorID, map[string]any{
		"type":      "message_posted",
		"thread_id": threadID,
		"summary":   "PM turn",
		"refs":      []any{"thread:" + threadID},
		"payload":   map[string]any{"text": "private instruction", "pm_turn_id": "turn-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := store.CreateArtifact(ctx, owner.ActorID, map[string]any{
		"kind":      "agent_wake",
		"summary":   "PM wake",
		"thread_id": threadID,
		"refs":      []any{"thread:" + threadID},
	}, map[string]any{"pm_execution": map[string]any{"turn_id": "turn-1"}}, "structured")
	if err != nil {
		t.Fatal(err)
	}

	visible := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "pm-public", "pm-public-actor", "pm-public", "pm-public-token")
	publicThread, err := store.CreateThread(ctx, visible.ActorID, map[string]any{"title": "Shared incident"})
	if err != nil {
		t.Fatal(err)
	}
	publicID := anyString(publicThread.Thread["id"])

	ownerGET := func(path string) *http.Response {
		t.Helper()
		return getJSONExpectStatusWithAuth(t, env.server.URL+path, owner.AccessToken, http.StatusOK)
	}
	stranger404 := func(path string) {
		t.Helper()
		resp := getJSONExpectStatusWithAuth(t, env.server.URL+path, stranger.AccessToken, http.StatusNotFound)
		defer resp.Body.Close()
	}

	ownerThread := ownerGET("/threads/" + threadID)
	defer ownerThread.Body.Close()
	stranger404("/threads/" + threadID)
	stranger404("/threads/" + threadID + "/timeline")
	stranger404("/threads/" + threadID + "/context")
	stranger404("/threads/" + threadID + "/workspace")
	stranger404("/events/" + anyString(event["id"]))
	stranger404("/artifacts/" + anyString(artifact["id"]))

	listThreads := getJSONExpectStatusWithAuth(t, env.server.URL+"/threads", stranger.AccessToken, http.StatusOK)
	defer listThreads.Body.Close()
	var listed struct {
		Threads []map[string]any `json:"threads"`
	}
	if err := json.NewDecoder(listThreads.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	for _, thread := range listed.Threads {
		if anyString(thread["id"]) == threadID {
			t.Fatalf("stranger listed PM thread: %#v", thread)
		}
	}
	foundPublic := false
	for _, thread := range listed.Threads {
		if anyString(thread["id"]) == publicID {
			foundPublic = true
		}
	}
	if !foundPublic {
		t.Fatal("stranger lost access to a non-PM thread")
	}

	listEvents := getJSONExpectStatusWithAuth(t, env.server.URL+"/events", stranger.AccessToken, http.StatusOK)
	defer listEvents.Body.Close()
	var events struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.NewDecoder(listEvents.Body).Decode(&events); err != nil {
		t.Fatal(err)
	}
	for _, item := range events.Events {
		if anyString(item["id"]) == anyString(event["id"]) {
			t.Fatalf("stranger listed PM event: %#v", item)
		}
	}

	filtered := getJSONExpectStatusWithAuth(t, env.server.URL+"/events?thread_id="+threadID, stranger.AccessToken, http.StatusNotFound)
	defer filtered.Body.Close()

	listArtifacts := getJSONExpectStatusWithAuth(t, env.server.URL+"/artifacts", stranger.AccessToken, http.StatusOK)
	defer listArtifacts.Body.Close()
	var artifacts struct {
		Artifacts []map[string]any `json:"artifacts"`
	}
	if err := json.NewDecoder(listArtifacts.Body).Decode(&artifacts); err != nil {
		t.Fatal(err)
	}
	for _, item := range artifacts.Artifacts {
		if anyString(item["id"]) == anyString(artifact["id"]) {
			t.Fatalf("stranger listed PM artifact: %#v", item)
		}
	}
}
