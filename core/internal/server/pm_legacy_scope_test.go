package server

import (
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// This uses only pre-PR APIs so it can also be run against origin/main.
// Direct selected-PM reads have PM authority; legacy turn context has reader authority.
func TestPMLegacyAgentAndTurnReaderScopes(t *testing.T) {
	ctx := context.Background()
	env := newPMStoreTestEnv(t)
	human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "scope-human", "scope-human-actor", "scope-human", "scope-human-token")
	machine := seedMachinePrincipalForLockoutTest(t, ctx, env.workspace.DB(), "scope-pm", "scope-pm-actor", "scope-pm", "scope-pm-token")
	store := env.primitiveStore.(*primitives.Store)
	secret, err := store.CreateWork(ctx, "another-owner", "", map[string]any{"title": "OtherPrincipalPrivateSecret"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PatchThread(ctx, "another-owner", anyString(secret["thread_id"]), map[string]any{"pm_actor_id": "another-owner"}, nil); err != nil {
		t.Fatal(err)
	}
	rt, err := newOnboardedPMRuntime(t, env.workspace.DB(), store, env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main", AgentActorID: machine.ActorID}})
	if err != nil {
		t.Fatal(err)
	}
	hp := pm.Principal{WorkspaceID: "ws_main", ActorID: human.ActorID, Human: true}
	agent := pm.Principal{WorkspaceID: "ws_main", ActorID: machine.ActorID}
	c, err := rt.Service.CreateConversation(ctx, hp, pm.CreateConversation{RequestKey: "scope", Title: "No pins"})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := rt.Service.PostMessage(ctx, hp, c.ID, pm.MessageInput{RequestKey: "scope", Text: "What needs me?"})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := rt.Service.ClaimTurn(ctx, agent, pm.ClaimInput{RunnerID: "scope"})
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path string, body any) string {
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
		req.Header.Set("Authorization", "Bearer "+machine.AccessToken)
		rr := httptest.NewRecorder()
		rt.ServeHTTP(rr, req)
		if rr.Code != 200 {
			t.Fatalf("%s: %d %s", path, rr.Code, rr.Body)
		}
		return rr.Body.String()
	}
	if !strings.Contains(request("GET", "/pm/context?limit=8", nil), "OtherPrincipalPrivateSecret") {
		t.Fatal("expected selected PM authority on direct legacy context")
	}
	if strings.Contains(request("POST", "/pm/turns/"+turn.ID+"/context", map[string]any{"lease_token": claim.LeaseToken, "limit": 8}), "OtherPrincipalPrivateSecret") {
		t.Fatal("legacy turn context escaped reader authority")
	}
}
