package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

func TestPMChatHTTPRecoveryRetainsRealPinnedResourceAuthority(t *testing.T) {
	requireIntegrationTest(t)
	env := newPMStoreTestEnv(t)
	ctx := context.Background()
	db := env.workspace.DB()
	human := seedHumanPrincipalForLockoutTest(t, ctx, db, "chat-reader", "chat-reader-actor", "chat-reader", "chat-reader-token")
	agent := seedMachinePrincipalForLockoutTest(t, ctx, db, "chat-pm", "chat-pm-actor", "chat-pm", "chat-pm-token")
	store := env.primitiveStore.(*primitives.Store)
	doc, _, err := store.CreateDocument(ctx, human.ActorID, map[string]any{"title": "Private chat evidence"}, "PRIVATE CHAT EVIDENCE", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	thread := anyString(doc["thread_id"])
	if _, err = store.PatchThread(ctx, human.ActorID, thread, map[string]any{"pm_actor_id": human.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	rt, err := newOnboardedPMRuntime(t, db, store, env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main", AgentActorID: agent.ActorID}})
	if err != nil {
		t.Fatal(err)
	}
	contract, err := schema.Load("../../../contracts/anx-schema.yaml")
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler("", WithSchemaContract(contract), WithAuthStore(env.authStore), WithActorRegistry(env.registry), WithPrimitiveStore(store), WithPMRuntime(rt))
	db.SetMaxOpenConns(1)
	call := func(method, path string, body any, token string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
		req.Header.Set("Authorization", "Bearer "+token)
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		return out
	}
	creation := map[string]any{"request_key": "pinned-chat", "title": "Private question", "context_refs": []string{anyString(doc["ref"])}}
	out := call("POST", "/pm/conversations", creation, human.AccessToken)
	if out.Code != 201 {
		t.Fatal(out.Code, out.Body.String())
	}
	var c pm.Conversation
	if err = json.Unmarshal(out.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	message := map[string]any{"request_key": "pinned-question", "text": "PRIVATE QUESTION TEXT"}
	path := "/pm/conversations/" + c.ID + "/messages"
	out = call("POST", path, message, human.AccessToken)
	if out.Code != 202 {
		t.Fatal(out.Code, out.Body.String())
	}
	var turn pm.Turn
	if err = json.Unmarshal(out.Body.Bytes(), &turn); err != nil {
		t.Fatal(err)
	}
	out = call("POST", "/pm/disconnect", map[string]any{}, agent.AccessToken)
	if out.Code != 200 {
		t.Fatal("disconnect", out.Code, out.Body.String())
	}
	if out = call("POST", path, message, human.AccessToken); out.Code != 202 {
		t.Fatal("confirmed retry stranded", out.Code, out.Body.String())
	}
	if _, err = store.PatchThread(ctx, human.ActorID, thread, map[string]any{"pm_actor_id": "replacement-owner"}, nil); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{"POST", "/pm/conversations", creation},
		{"POST", path, message},
		{"GET", "/pm/conversations/" + c.ID, nil},
		{"GET", "/pm/turns/" + turn.ID, nil},
	} {
		out = call(tc.method, tc.path, tc.body, human.AccessToken)
		denied := out.Code == 403 || out.Code == 404 || (tc.method == "POST" && out.Code == 409 && strings.Contains(out.Body.String(), "pm_not_onboarded"))
		// Scoped records may disappear before the disconnected-route peek;
		// that path retains the setup gate rather than revealing existence.
		if !denied || strings.Contains(out.Body.String(), "PRIVATE") {
			t.Fatal("revoked evidence leaked", out.Code, out.Body.String())
		}
	}
}
