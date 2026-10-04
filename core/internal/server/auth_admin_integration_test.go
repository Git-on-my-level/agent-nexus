package server

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAgentAuthAdminFleetLifecycle(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken, allowPasskeyDevBypass: true})
	base := env.server.URL
	status, p := hostHTTP(t, "POST", base+"/auth/passkey/dev/register", "", map[string]any{"display_name": "Admin", "bootstrap_token": testBootstrapToken})
	hostStatus(t, status, 201, p)
	human := p["tokens"].(map[string]any)["access_token"].(string)
	humanID := p["agent"].(map[string]any)["agent_id"].(string)
	createToken := func(bearer string) (string, string) {
		t.Helper()
		status, p := hostHTTP(t, "POST", base+"/auth/hosts/enrollment-tokens", bearer, map[string]any{"label": "fleet", "expires_in_seconds": 600})
		hostStatus(t, status, 201, p)
		return p["token"].(string), p["enrollment_token"].(map[string]any)["id"].(string)
	}
	enroll := func(secret, slug string) (string, string, ed25519.PrivateKey, map[string]any) {
		t.Helper()
		pub, key, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		public := base64.StdEncoding.EncodeToString(pub)
		nonce := base64.RawURLEncoding.EncodeToString(pub[:16])
		request := map[string]any{"public_key": public, "requested_slug": slug, "os_user": "test", "hostname": slug, "discovered_adapters": []string{}, "request_nonce": nonce, "adoptions": []any{}, "enrollment_token": secret, "signature": hostSign(key, "anx-host-headless-enroll|"+nonce+"|"+slug+"|"+public)}
		status, p := hostHTTP(t, "POST", base+"/auth/hosts/enrollments/headless", "", request)
		hostStatus(t, status, 201, p)
		host := p["host"].(map[string]any)
		return host["id"].(string), host["key_id"].(string), key, request
	}
	secret, _ := createToken(human)
	hostA, keyID, key, _ := enroll(secret, "host-a")
	signed := time.Now().UTC().Format(time.RFC3339Nano)
	status, p = hostHTTP(t, "POST", base+"/auth/token", "", map[string]any{"grant_type": "host_assertion", "host_id": hostA, "key_id": keyID, "agent_name": "fleet", "signed_at": signed, "signature": hostSign(key, "anx-host-agent-token|"+hostA+"|"+keyID+"|fleet|"+signed)})
	hostStatus(t, status, 200, p)
	agentID := p["agent"].(map[string]any)["id"].(string)
	agentActorID := p["agent"].(map[string]any)["actor_id"].(string)
	agent := p["tokens"].(map[string]any)["access_token"].(string)
	change := func(target, verb, bearer string, want int) map[string]any {
		t.Helper()
		status, p := hostHTTP(t, "POST", base+"/auth/admins/"+target+"/"+verb, bearer, nil)
		hostStatus(t, status, want, p)
		return p
	}
	deniedReads := func() {
		t.Helper()
		for _, path := range []string{"/auth/admins", "/auth/hosts/enrollment-tokens", "/auth/hosts/enrollments/pending"} {
			status, p := hostHTTP(t, "GET", base+path, agent, nil)
			hostStatus(t, status, 403, p)
		}
		status, p := hostHTTP(t, "POST", base+"/auth/hosts/enrollment-tokens", agent, map[string]any{"label": "denied", "expires_in_seconds": 3600})
		hostStatus(t, status, 403, p)
	}
	deniedReads()
	change(agentID, "grant", agent, 403)
	change(humanID, "grant", human, 400)
	change("missing", "grant", human, 404)
	granted := change("fleet.host-a", "grant", human, 200)
	if granted["admin"].(map[string]any)["auth_admin"] != true {
		t.Fatal(granted)
	}
	if granted["admin"].(map[string]any)["host_id"] != hostA || granted["admin"].(map[string]any)["agent_name"] != "fleet" {
		t.Fatal("grant omitted host scope", granted)
	}
	change(agentID, "grant", human, 200)
	status, p = hostHTTP(t, "GET", base+"/auth/admins", agent, nil)
	hostStatus(t, status, 200, p)
	if len(p["admins"].([]any)) != 1 {
		t.Fatal(p)
	}
	// The existing agent token sees its new grant immediately.
	secret, tokenID := createToken(agent)
	hostB, hostBKeyID, _, request := enroll(secret, "host-b")
	status, p = hostHTTP(t, "POST", base+"/auth/hosts/enrollments/headless", "", request)
	hostStatus(t, status, 401, p)
	for _, target := range []string{hostA, "host-a"} {
		status, p = hostHTTP(t, "DELETE", base+"/hosts/"+target, agent, nil)
		hostStatus(t, status, 403, p)
		if p["error"].(map[string]any)["code"] != "host_self_revoke" {
			t.Fatal(p)
		}
	}
	status, p = hostHTTP(t, "PATCH", base+"/hosts/"+hostB, agent, map[string]any{"display_name": "denied"})
	hostStatus(t, status, 403, p)
	change(agentID, "grant", agent, 403)
	change(agentID, "revoke", agent, 403)
	status, p = hostHTTP(t, "DELETE", base+"/hosts/host-b", agent, nil)
	hostStatus(t, status, 200, p)
	_, unused := createToken(agent)
	status, p = hostHTTP(t, "POST", base+"/auth/hosts/enrollment-tokens/"+unused+"/revoke", agent, nil)
	hostStatus(t, status, 200, p)
	for _, verb := range []string{"approve", "deny"} {
		pub, _, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		status, p = hostHTTP(t, "POST", base+"/auth/hosts/enrollments", "", map[string]any{"public_key": base64.StdEncoding.EncodeToString(pub), "requested_slug": "pending-" + verb, "os_user": "test", "hostname": "pending", "discovered_adapters": []string{}, "request_nonce": base64.RawURLEncoding.EncodeToString(pub[:16]), "adoptions": []any{}})
		hostStatus(t, status, 201, p)
		id := p["enrollment_id"].(string)
		status, p = hostHTTP(t, "GET", base+"/auth/hosts/enrollments/pending", agent, nil)
		hostStatus(t, status, 200, p)
		raw, _ := json.Marshal(p)
		if strings.Contains(string(raw), "poll_token") {
			t.Fatal("pending list exposed poll token")
		}
		status, p = hostHTTP(t, "POST", base+"/auth/hosts/enrollments/"+id+"/"+verb, agent, nil)
		hostStatus(t, status, 200, p)
	}
	change(agentID, "revoke", human, 200)
	deniedReads()
	status, p = hostHTTP(t, "DELETE", base+"/hosts/"+hostB, agent, nil)
	hostStatus(t, status, 403, p)
	status, p = hostHTTP(t, "GET", base+"/hosts", agent, nil)
	hostStatus(t, status, 200, p)
	status, p = hostHTTP(t, "GET", base+"/auth/admins", human, nil)
	hostStatus(t, status, 200, p)
	if len(p["admins"].([]any)) != 0 {
		t.Fatal(p)
	}
	status, p = hostHTTP(t, "GET", base+"/auth/audit?limit=200", human, nil)
	hostStatus(t, status, 200, p)
	wanted := map[string]bool{"auth_admin_granted": false, "auth_admin_revoked": false, "host_enrollment_token_created": false, "host_enrollment_token_revoked": false, "host_enrollment_token_consumed": false, "host_revoked": false, "host_enroll_approved": false, "host_enroll_denied": false}
	grants := 0
	for _, raw := range p["events"].([]any) {
		event := raw.(map[string]any)
		kind := event["event_type"].(string)
		if kind == "auth_admin_granted" {
			grants++
		}
		if kind == "auth_admin_granted" || kind == "auth_admin_revoked" {
			meta := event["metadata"].(map[string]any)
			if meta["host_id"] != hostA || meta["host_slug"] != "host-a" || meta["agent_name"] != "fleet" {
				t.Fatal("audit omitted host scope", event)
			}
			if event["actor_agent_id"] != humanID || event["subject_agent_id"] != agentID {
				t.Fatal(event)
			}
			wanted[kind] = true
		} else if kind == "host_enrollment_token_consumed" {
			meta := event["metadata"].(map[string]any)
			if meta["token_id"] == tokenID {
				if event["actor_agent_id"] != nil || event["actor_actor_id"] != nil || meta["issuer_principal_id"] != agentID || meta["host_id"] != hostB || meta["host_key_id"] != hostBKeyID || meta["issuer_actor_id"] != agentActorID {
					t.Fatal("token consumption falsely attributed or missing proof destination", event)
				}
				wanted[kind] = true
			}
		} else if event["actor_agent_id"] == agentID {
			if _, ok := wanted[kind]; ok {
				wanted[kind] = true
			}
			meta := event["metadata"].(map[string]any)
			if kind == "host_enrollment_token_consumed" && (meta["host_id"] != hostB || meta["token_id"] != tokenID) {
				t.Fatal(event)
			}
			if kind == "host_revoked" && meta["host_id"] != hostB {
				t.Fatal(event)
			}
		}
	}
	for kind, found := range wanted {
		if !found {
			t.Errorf("missing audit event %s", kind)
		}
	}
	if grants != 1 {
		t.Fatalf("grant event count=%d", grants)
	}
	raw, _ := json.Marshal(p)
	if strings.Contains(string(raw), secret) {
		t.Fatal("secret appeared in audit")
	}
	status, p = hostHTTP(t, "GET", base+"/auth/hosts/enrollment-tokens", human, nil)
	hostStatus(t, status, 200, p)
	raw, _ = json.Marshal(p)
	if strings.Contains(string(raw), secret) || p["token"] != nil {
		t.Fatal("secret appeared in token list")
	}
}
