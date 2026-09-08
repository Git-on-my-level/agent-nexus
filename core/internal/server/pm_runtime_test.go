package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
)

func TestPMRuntimeNativeDecisionAuthorizationAndReadback(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	principal := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "pm-human", "pm-human-actor", "pm-human", "pm-human-token")
	store := env.primitiveStore.(*primitives.Store)
	board, err := store.CreateBoard(ctx, principal.ActorID, map[string]any{"title": "Native PM"})
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateWork(ctx, principal.ActorID, asString(board["id"]), map[string]any{"title": "Deliver accepted work"})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewPMRuntime(env.workspace.DB(), store, env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()
	post := func(path string, body map[string]any, status int) map[string]any {
		t.Helper()
		resp := postJSONExpectStatusWithAuth(t, srv.URL+path, body, principal.AccessToken, status)
		defer resp.Body.Close()
		var out map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	d := post("/pm/decisions", map[string]any{"request_key": "decision", "work_ref": work["ref"], "instruction": `{"next_action":"Review acceptance evidence"}`, "scope": "work.annotate", "target_revision": "1"}, 201)
	answer := post("/pm/decisions/"+asString(d["id"])+"/answer", map[string]any{"revision": 1, "approve": true, "text": "Approved exact annotation"}, 200)
	if answer["status"] != "answered" {
		t.Fatal(answer)
	}
	action := post("/pm/decisions/"+asString(d["id"])+"/dispatch", map[string]any{}, 200)
	if action["status"] != "source_reported" {
		t.Fatalf("dispatch incorrectly verified or failed: %v", action)
	}
	receipt := post("/pm/actions/"+asString(action["id"])+"/reconcile", map[string]any{}, 200)
	if receipt["status"] != "verified" {
		t.Fatal(receipt)
	}
	updated, err := store.GetWork(ctx, asString(work["ref"]))
	if err != nil || updated["next_action"] != "Review acceptance evidence" {
		t.Fatalf("action not persisted: %v %v", updated, err)
	}
	// Restart service against same workspace storage: decision and receipt survive.
	restored, err := NewPMRuntime(env.workspace.DB(), store, env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/pm/actions/"+asString(action["id"]), nil)
	req.Header.Set("Authorization", "Bearer "+principal.AccessToken)
	rr := httptest.NewRecorder()
	restored.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	if _, err = env.workspace.DB().ExecContext(ctx, `UPDATE agents SET revoked_at=? WHERE id=?`, time.Now().UTC().Format(time.RFC3339Nano), principal.AgentID); err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	restored.ServeHTTP(rr, req)
	if rr.Code != 403 {
		t.Fatalf("revoked principal retained PM access: %d", rr.Code)
	}
}
func TestPMRuntimeDoesNotTrustBodyIdentityOrConfigureProvider(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	seed := seedHumanPrincipalForLockoutTest(t, context.Background(), env.workspace.DB(), "pm-second", "pm-second-actor", "pm-second", "pm-second-token")
	handler, err := NewPMRuntime(env.workspace.DB(), env.primitiveStore.(*primitives.Store), env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()
	postJSONExpectStatusWithAuth(t, srv.URL+"/pm/conversations", map[string]any{"request_key": "forged", "title": "forged", "actor_id": seed.ActorID}, "", 403)
	resp := postJSONExpectStatusWithAuth(t, srv.URL+"/pm/conversations", map[string]any{"request_key": "conversation", "title": "Context"}, seed.AccessToken, 201)
	defer resp.Body.Close()
	var c map[string]any
	if err = json.NewDecoder(resp.Body).Decode(&c); err != nil {
		t.Fatal(err)
	}
	postJSONExpectStatusWithAuth(t, srv.URL+"/pm/conversations/"+asString(c["id"])+"/messages", map[string]any{"request_key": "turn", "text": "What changed?"}, seed.AccessToken, 503)
}

func TestPMRuntimeBridgeFailsClosedWithoutRuntimeEnvelope(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	_, err := NewPMRuntime(env.workspace.DB(), env.primitiveStore.(*primitives.Store), env.authStore, PMRuntimeConfig{
		PM:            pm.Config{WorkspaceID: "ws_main", AgentActorID: "pm-agent", AgentHandle: "pm"},
		BridgeEnabled: true,
	})
	if err != pm.ErrUnavailable {
		t.Fatalf("bridge without independently enforced envelope started: %v", err)
	}
}

func TestPMRuntimeSourceWriteStaysUnavailable(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	principal := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "pm-source", "pm-source-actor", "pm-source", "pm-source-token")
	store := env.primitiveStore.(*primitives.Store)
	board, err := store.CreateBoard(ctx, principal.ActorID, map[string]any{"title": "Source PM"})
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateWork(ctx, principal.ActorID, asString(board["id"]), map[string]any{
		"title":  "External issue",
		"source": map[string]any{"authority": "github", "connection_id": "fixture", "native_id": "org/repo#1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewPMRuntime(env.workspace.DB(), store, env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()
	post := func(path string, body map[string]any, status int) map[string]any {
		t.Helper()
		resp := postJSONExpectStatusWithAuth(t, srv.URL+path, body, principal.AccessToken, status)
		defer resp.Body.Close()
		var out map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	d := post("/pm/decisions", map[string]any{"request_key": "github-write", "work_ref": work["ref"], "instruction": "close", "scope": "github", "target_revision": "abc"}, 201)
	answer := post("/pm/decisions/"+asString(d["id"])+"/answer", map[string]any{"revision": 1, "approve": true, "text": "Approved exact source write"}, 200)
	if answer["status"] != "answered" {
		t.Fatal(answer)
	}
	post("/pm/decisions/"+asString(d["id"])+"/dispatch", map[string]any{}, http.StatusServiceUnavailable)
}

func TestPMChannelIngressUnavailableUntilConfigured(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	resp := postJSONExpectStatusWithAuth(t, env.server.URL+"/pm/ingress/telegram", map[string]any{"update_id": 1}, "", http.StatusServiceUnavailable)
	defer resp.Body.Close()
	resp = postJSONExpectStatusWithAuth(t, env.server.URL+"/pm/ingress/discord", map[string]any{"type": 1}, "", http.StatusServiceUnavailable)
	defer resp.Body.Close()
}

func TestPMRuntimeReplyUsesConversationAuthorization(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	seed := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "reply-fixture", "reply-fixture-actor", "reply-fixture", "reply-fixture-token")
	handler, err := NewPMRuntime(env.workspace.DB(), env.primitiveStore.(*primitives.Store), env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}})
	if err != nil {
		t.Fatal(err)
	}
	turn := pm.Turn{ID: "fixture-turn", ConversationID: "fixture-conversation", WorkspaceID: "ws_main", ActorID: seed.ActorID, AgentActorID: seed.ActorID, Status: pm.Sending, Revision: 1, Deadline: time.Now().Add(time.Minute)}
	body, _ := json.Marshal(turn)
	if _, err = env.workspace.DB().ExecContext(ctx, `INSERT INTO pm_records(kind,id,workspace_id,actor_id,parent_id,revision,body) VALUES('turn',?,?,?,?,1,?)`, turn.ID, turn.WorkspaceID, turn.ActorID, turn.ConversationID, body); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()
	postJSONExpectStatusWithAuth(t, srv.URL+"/pm/turns/fixture-turn/complete", map[string]any{"text": "Evidence reviewed", "evidence_refs": []string{}}, seed.AccessToken, 200)
}
