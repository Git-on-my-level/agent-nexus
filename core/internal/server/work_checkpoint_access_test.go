package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"agent-nexus-core/internal/primitives"
)

func TestWorkCheckpointInheritsPrivateReferences(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	store := env.primitiveStore.(*primitives.Store)
	owner := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "checkpoint-owner", "checkpoint-owner-actor", "checkpoint-owner", "checkpoint-owner-token")
	stranger := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "checkpoint-stranger", "checkpoint-stranger-actor", "checkpoint-stranger", "checkpoint-stranger-token")
	publicBoard, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Public research"})
	if err != nil {
		t.Fatal(err)
	}
	privateBoard, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Private research"})
	if err != nil {
		t.Fatal(err)
	}
	private, err := store.CreateWork(ctx, owner.ActorID, anyString(privateBoard["id"]), map[string]any{"title": "Secret draft"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PatchThread(ctx, owner.ActorID, anyString(privateBoard["thread_id"]), map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	public, err := store.CreateWork(ctx, owner.ActorID, anyString(publicBoard["id"]), map[string]any{"title": "Public project"})
	if err != nil {
		t.Fatal(err)
	}
	path := env.server.URL + "/work/" + anyString(public["ref"])
	getJSONExpectStatusWithAuth(t, path+"/briefing", stranger.AccessToken, 200).Body.Close()
	data, _ := json.Marshal(map[string]any{"if_version": 0, "checkpoint": map[string]any{"summary": "Read " + anyString(private["ref"]), "next_action": "Review private draft"}})
	req, _ := http.NewRequest("PUT", path+"/checkpoint", strings.NewReader(string(data)))
	req.Header.Set("Authorization", "Bearer "+owner.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatal("checkpoint write", resp.StatusCode)
	}
	resp.Body.Close()
	getJSONExpectStatusWithAuth(t, path+"/briefing", owner.AccessToken, 200).Body.Close()
	getJSONExpectStatusWithAuth(t, path+"/briefing", stranger.AccessToken, 404).Body.Close()
	getJSONExpectStatusWithAuth(t, path, stranger.AccessToken, 404).Body.Close()
	req, _ = http.NewRequest("PUT", path+"/checkpoint", strings.NewReader(string(data)))
	req.Header.Set("Authorization", "Bearer "+stranger.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatal("inaccessible checkpoint write", resp.StatusCode)
	}
}
