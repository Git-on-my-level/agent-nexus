package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestCommandCenterRoutesAndAttribution(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	agent := seedMachinePrincipalForLockoutTest(t, ctx, db, "cc-agent", "cc-actor", "codex.host", "cc-token")
	_, e := db.ExecContext(ctx, `INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at) VALUES('host-1','host','Host','test','host','["codex"]',?)`, time.Now().UTC().Format(time.RFC3339Nano))
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.ExecContext(ctx, `INSERT INTO host_agents(host_id,name,agent_id,identity_kind) VALUES('host-1','codex',?,'derived')`, agent.AgentID)
	if e != nil {
		t.Fatal(e)
	}
	human := seedHumanPrincipalForLockoutTest(t, ctx, db, "cc-human", "cc-human-actor", "human.host", "human-token")
	noAuth := getJSONExpectStatusWithAuth(t, env.server.URL+"/runs", "", http.StatusUnauthorized)
	noAuth.Body.Close()
	denied := postJSONExpectStatusWithAuth(t, env.server.URL+"/runs", map[string]any{}, human.AccessToken, http.StatusForbidden)
	denied.Body.Close()
	denied = patchJSONExpectStatusWithAuth(t, env.server.URL+"/agents/me/presence", map[string]any{"note": "hello"}, human.AccessToken, http.StatusForbidden)
	denied.Body.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	upsert := map[string]any{"launcher": "agentctl", "external_id": "exec-cc", "host_id": "host-1", "agent_id": agent.AgentID, "adapter": "codex", "state": "running", "liveness": "alive", "result_collected": false, "labels": []string{}, "last_observed_at": now}
	created := postJSONExpectStatusWithAuth(t, env.server.URL+"/runs", upsert, agent.AccessToken, http.StatusCreated)
	var body map[string]any
	if e = json.NewDecoder(created.Body).Decode(&body); e != nil {
		t.Fatal(e)
	}
	created.Body.Close()
	run := body["run"].(map[string]any)
	runID := run["id"].(string)
	replay := postJSONExpectStatusWithAuth(t, env.server.URL+"/runs", upsert, agent.AccessToken, http.StatusOK)
	replay.Body.Close()
	got := getJSONExpectStatusWithAuth(t, env.server.URL+"/runs/"+runID, human.AccessToken, http.StatusOK)
	got.Body.Close()
	listed := getJSONExpectStatusWithAuth(t, env.server.URL+"/runs?active=true&agent_id="+agent.AgentID, human.AccessToken, http.StatusOK)
	listed.Body.Close()
	presence := patchJSONExpectStatusWithAuth(t, env.server.URL+"/agents/me/presence", map[string]any{"note": "Implementing route"}, agent.AccessToken, http.StatusOK)
	presence.Body.Close()
	roster := getJSONExpectStatusWithAuth(t, env.server.URL+"/agents", human.AccessToken, http.StatusOK)
	roster.Body.Close()
	detail := getJSONExpectStatusWithAuth(t, env.server.URL+"/agents/"+agent.AgentID, human.AccessToken, http.StatusOK)
	detail.Body.Close()
	threadID := integrationSeedThreadWithStore(t, env.primitiveStore, nil, agent.ActorID, map[string]any{"title": "Attribution thread", "type": "incident", "status": "active", "priority": "p2", "tags": []any{}, "cadence": "daily", "next_check_in_at": "2026-03-06T00:00:00Z", "provenance": map[string]any{"sources": []any{"inferred"}}})
	event := map[string]any{"event": map[string]any{"type": "message_posted", "thread_id": threadID, "summary": "Working", "refs": []string{"thread:" + threadID}, "payload": map[string]any{"text": "Working"}, "provenance": map[string]any{"sources": []string{"inferred"}}}}
	written := postJSONExpectStatusWithHeaders(t, env.server.URL+"/events", event, map[string]string{"Authorization": "Bearer " + agent.AccessToken, "X-ANX-Run-Id": "agentctl/exec-cc"}, http.StatusCreated)
	var eventBody map[string]any
	if e = json.NewDecoder(written.Body).Decode(&eventBody); e != nil {
		t.Fatal(e)
	}
	written.Body.Close()
	ev := eventBody["event"].(map[string]any)
	attr := ev["run_attribution"].(map[string]any)
	if attr["run_id"] != runID || attr["host_id"] != "host-1" || attr["agent_id"] != agent.AgentID || attr["adapter"] != "codex" {
		t.Fatalf("attribution: %#v", attr)
	}
	read := getJSONExpectStatusWithAuth(t, env.server.URL+"/events/"+ev["id"].(string), human.AccessToken, http.StatusOK)
	var readBody map[string]any
	if e = json.NewDecoder(read.Body).Decode(&readBody); e != nil {
		t.Fatal(e)
	}
	read.Body.Close()
	if readBody["event"].(map[string]any)["run_attribution"] == nil {
		t.Fatal("event read lost attribution")
	}
	provisional := postJSONExpectStatusWithHeaders(t, env.server.URL+"/events", event, map[string]string{"Authorization": "Bearer " + agent.AccessToken, "X-ANX-Run-Id": "agentctl/exec-provisional"}, http.StatusCreated)
	provisional.Body.Close()
	provisionalList := getJSONExpectStatusWithAuth(t, env.server.URL+"/runs?agent_id="+agent.AgentID, human.AccessToken, http.StatusOK)
	var provisionalBody map[string]any
	if e = json.NewDecoder(provisionalList.Body).Decode(&provisionalBody); e != nil {
		t.Fatal(e)
	}
	provisionalList.Body.Close()
	foundUnknown := false
	for _, item := range provisionalBody["runs"].([]any) {
		run := item.(map[string]any)
		if run["external_id"] == "exec-provisional" && run["state"] == "unknown" {
			foundUnknown = true
		}
	}
	if !foundUnknown {
		t.Fatal("attributed write did not create provisional run")
	}
	invalid := postJSONExpectStatusWithHeaders(t, env.server.URL+"/events", event, map[string]string{"Authorization": "Bearer " + agent.AccessToken, "X-ANX-Run-Id": "invalid"}, http.StatusBadRequest)
	invalid.Body.Close()
}
