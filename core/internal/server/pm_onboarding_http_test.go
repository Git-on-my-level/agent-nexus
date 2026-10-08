package server

import (
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPMOnboardingHTTPGateBootstrapAndIdentitySelection(t *testing.T) {
	env := newPMStoreTestEnv(t)
	ctx := context.Background()
	db := env.workspace.DB()
	human := seedHumanPrincipalForLockoutTest(t, ctx, db, "human", "human-actor", "human", "human-token")
	machine := seedMachinePrincipalForLockoutTest(t, ctx, db, "local-pm", "local-pm-actor", "local-pm", "local-pm-token")
	if _, err := db.Exec(`UPDATE agents SET metadata_json='{"principal_kind":"agent","auth_method":"public_key"}' WHERE id=?`, machine.AgentID); err != nil {
		t.Fatal(err)
	}
	// Canonical, registered derived identity; labels cannot create this mapping.
	_, err := db.Exec(`INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at) VALUES('local-host','computer','Computer','user','computer','[]',?);`, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO host_agents(host_id,name,agent_id,identity_kind) VALUES('local-host','pm',?,'derived')`, machine.AgentID); err != nil {
		t.Fatal(err)
	}
	candidate, e := env.authStore.IsLocalPMCandidate(ctx, machine.AgentID)
	if e != nil || !candidate {
		t.Fatalf("candidate %t %v", candidate, e)
	}
	actual, e := env.authStore.AuthenticateAccessToken(ctx, machine.AccessToken)
	if e != nil {
		t.Fatal(e)
	}
	if actual.PrincipalKind != "agent" {
		t.Fatal("candidate must be an authenticated agent")
	}
	rt, err := NewPMRuntime(db, env.primitiveStore.(*primitives.Store), env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}})
	if err != nil {
		t.Fatal(err)
	}
	probe := httptest.NewRequest("POST", "/pm/connect", nil)
	cacheAuthenticatedPrincipal(probe, &actual)
	attachResourceAccessScope(probe, handlerOptions{primitiveStore: env.primitiveStore, pmRuntime: rt})
	candidate, e = env.authStore.IsLocalPMCandidate(probe.Context(), machine.AgentID)
	if e != nil || !candidate {
		t.Fatalf("scoped candidate %t %v", candidate, e)
	}
	srv := httptest.NewServer(NewHandler("", WithAuthStore(env.authStore), WithActorRegistry(env.registry), WithPrimitiveStore(env.primitiveStore), WithPMRuntime(rt)))
	defer srv.Close()
	call := func(method, path, token, body string, want int) map[string]any {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		var out map[string]any
		if res.StatusCode != 204 {
			if e = json.NewDecoder(res.Body).Decode(&out); e != nil {
				t.Fatal(e)
			}
		}
		if res.StatusCode != want {
			t.Fatalf("%s %s: %d %+v", method, path, res.StatusCode, out)
		}
		return out
	}
	state := call("GET", "/agents/me", human.AccessToken, "", 200)["pm"].(map[string]any)
	if state["state"] != "not_onboarded" || state["last_seen"] != nil {
		t.Fatal(state)
	}
	for _, path := range []string{"/pm/conversations", "/pm/context", "/pm/decisions", "/pm/actions", "/pm/bindings", "/pm/turns/missing", "/pm/turns/missing/context"} {
		out := call("GET", path, human.AccessToken, "", 409)
		if out["error"].(map[string]any)["code"] != "pm_not_onboarded" {
			t.Fatal(out)
		}
	}
	call("POST", "/pm/conversations", human.AccessToken, `{"request_key":"blocked","title":"Question"}`, 409)
	for _, action := range []string{"complete", "activity", "fail"} {
		call("POST", "/pm/turns/missing/"+action, machine.AccessToken, `{}`, 409)
	}
	call("GET", "/work/capabilities", human.AccessToken, "", 200)
	call("GET", "/overview", human.AccessToken, "", 200)
	call("POST", "/pm/connect", human.AccessToken, `{"runner":"custom","host":"computer"}`, 403)
	call("POST", "/pm/connect", machine.AccessToken, `{"runner":"custom","host":"computer"}`, 200)
	if rt.AgentActorID() != machine.ActorID {
		t.Fatal("runtime identity not updated")
	}
	state = call("GET", "/agents/me", human.AccessToken, "", 200)["pm"].(map[string]any)
	if state["state"] != "connected" || state["runner"] != "custom" {
		t.Fatal(state)
	}
	call("POST", "/pm/turns/claim", machine.AccessToken, `{"runner_id":"idle"}`, 204)
	call("POST", "/pm/conversations", human.AccessToken, `{"request_key":"now","title":"Question"}`, 201)
	if _, err = db.Exec(`UPDATE hosts SET revoked_at=? WHERE id='local-host'`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	call("POST", "/pm/connect", machine.AccessToken, `{"runner":"custom","host":"computer"}`, 403)
}
