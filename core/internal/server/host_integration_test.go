package server

import (
	"agent-nexus-core/internal/auth"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"regexp"
	"testing"
	"time"
)

func hostHTTP(t *testing.T, method, url, bearer string, body any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(method, url, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var p map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, p
}
func hostStatus(t *testing.T, actual, want int, p map[string]any) {
	t.Helper()
	if actual != want {
		t.Fatalf("status %d want %d: %#v", actual, want, p)
	}
}
func hostSign(key ed25519.PrivateKey, message string) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(key, []byte(message)))
}
func hostSignedHTTP(t *testing.T, method, url, hostID, keyID, kind string, priv ed25519.PrivateKey, body any) (int, map[string]any) {
	return hostSignedHTTPAt(t, method, url, hostID, keyID, kind, priv, body, time.Now().UTC().Format(time.RFC3339Nano))
}
func hostSignedHTTPAt(t *testing.T, method, url, hostID, keyID, kind string, priv ed25519.PrivateKey, body any, signed string) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(method, url, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	message := "anx-host-" + kind + "|" + hostID + "|" + signed + "|" + base64.RawURLEncoding.EncodeToString(digest[:])
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ANX-Host-Key-Id", keyID)
	req.Header.Set("X-ANX-Host-Signed-At", signed)
	req.Header.Set("X-ANX-Host-Signature", hostSign(priv, message))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var p map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, p
}

func TestHostIdentityLifecycle(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken, allowPasskeyDevBypass: true})
	url := env.server.URL
	status, p := hostHTTP(t, "POST", url+"/auth/passkey/dev/register", "", map[string]any{"display_name": "Host Admin", "bootstrap_token": testBootstrapToken})
	hostStatus(t, status, 201, p)
	admin := p["tokens"].(map[string]any)["access_token"].(string)
	status, p = hostHTTP(t, "GET", url+"/agents/me", admin, nil)
	hostStatus(t, status, 200, p)
	self := p["agent"].(map[string]any)
	if self["principal_kind"] != "human" || self["actor_id"] == "" || self["agent_id"] == "" {
		t.Fatalf("human web self-identification: %#v", p)
	}
	status, p = hostHTTP(t, "POST", url+"/auth/passkey/dev/login", "", map[string]any{})
	hostStatus(t, status, 200, p)
	loginAccess := p["tokens"].(map[string]any)["access_token"].(string)
	status, p = hostHTTP(t, "GET", url+"/agents/me", loginAccess, nil)
	hostStatus(t, status, 200, p)
	if loggedIn := p["agent"].(map[string]any); loggedIn["principal_kind"] != "human" || loggedIn["actor_id"] != self["actor_id"] {
		t.Fatalf("human web sign-in lookup: %#v", p)
	}
	status, p = hostHTTP(t, "GET", url+"/agents", admin, nil)
	hostStatus(t, status, 200, p)
	status, p = hostHTTP(t, "POST", url+"/auth/hosts/enrollment-tokens", admin, map[string]any{"label": "test", "expires_at": time.Now().Add(20 * time.Minute).UTC().Format(time.RFC3339)})
	hostStatus(t, status, 201, p)
	secret := p["token"].(string)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public := base64.StdEncoding.EncodeToString(pub)
	nonce := base64.RawURLEncoding.EncodeToString(pub[:16])
	slug := "dev-host"
	msg := "anx-host-headless-enroll|" + nonce + "|" + slug + "|" + public
	status, p = hostHTTP(t, "POST", url+"/auth/hosts/enrollments/headless", "", map[string]any{"public_key": public, "requested_slug": slug, "os_user": "test", "hostname": "test", "discovered_adapters": []string{}, "request_nonce": nonce, "adoptions": []any{}, "enrollment_token": secret, "signature": hostSign(priv, msg)})
	hostStatus(t, status, 201, p)
	host := p["host"].(map[string]any)
	id, keyID := host["id"].(string), host["key_id"].(string)
	standalone := seedMachinePrincipalForLockoutTest(t, context.Background(), env.workspace.DB(), "agent-standalone", "actor-standalone", "standalone", "standalone-access")
	status, p = hostHTTP(t, "GET", url+"/agents/me", standalone.AccessToken, nil)
	hostStatus(t, status, 200, p)
	if p["agent"].(map[string]any)["identity_kind"] != "standalone" {
		t.Fatalf("standalone self-identification: %#v", p)
	}
	status, p = hostHTTP(t, "GET", url+"/hosts", standalone.AccessToken, nil)
	hostStatus(t, status, 200, p)
	status, p = hostHTTP(t, "GET", url+"/hosts/"+id, standalone.AccessToken, nil)
	hostStatus(t, status, 200, p)
	status, p = hostHTTP(t, "GET", url+"/auth/hosts/enrollment-tokens", standalone.AccessToken, nil)
	hostStatus(t, status, 403, p)
	status, p = hostHTTP(t, "GET", url+"/auth/hosts/enrollments/pending", standalone.AccessToken, nil)
	hostStatus(t, status, 403, p)
	status, p = hostHTTP(t, "PATCH", url+"/hosts/"+id, standalone.AccessToken, map[string]any{"display_name": "Forbidden"})
	hostStatus(t, status, 403, p)
	status, p = hostHTTP(t, "DELETE", url+"/hosts/"+id, standalone.AccessToken, nil)
	hostStatus(t, status, 403, p)
	status, p = hostHTTP(t, "POST", url+"/auth/hosts/enrollments/headless", "", map[string]any{"public_key": public, "requested_slug": "other", "os_user": "test", "hostname": "test", "discovered_adapters": []string{}, "request_nonce": nonce, "adoptions": []any{}, "enrollment_token": secret, "signature": hostSign(priv, "anx-host-headless-enroll|"+nonce+"|other|"+public)})
	hostStatus(t, status, 401, p)
	grant := func(name, signed string) (int, map[string]any) {
		message := "anx-host-agent-token|" + id + "|" + keyID + "|" + name + "|" + signed
		return hostHTTP(t, "POST", url+"/auth/token", "", map[string]any{"grant_type": "host_assertion", "host_id": id, "key_id": keyID, "agent_name": name, "signed_at": signed, "signature": hostSign(priv, message)})
	}
	signed := time.Now().UTC().Format(time.RFC3339)
	status, p = grant("codex", signed)
	hostStatus(t, status, 200, p)
	agent := p["agent"].(map[string]any)
	if agent["handle"] != "codex.dev-host" {
		t.Fatal(agent)
	}
	tokens := p["tokens"].(map[string]any)
	if tokens["refresh_token"] != nil {
		t.Fatal(tokens)
	}
	access := tokens["access_token"].(string)
	status, p = hostHTTP(t, "GET", url+"/agents/me", access, nil)
	hostStatus(t, status, 200, p)
	if p["agent"].(map[string]any)["handle"] != "codex.dev-host" {
		t.Fatal(p)
	}
	if p["agent"].(map[string]any)["principal_kind"] != "agent" || p["agent"].(map[string]any)["adapter"] != "codex" {
		t.Fatal(p)
	}
	status, p = hostHTTP(t, "GET", url+"/auth/hosts/enrollment-tokens", access, nil)
	hostStatus(t, status, 403, p)
	status, p = hostHTTP(t, "POST", url+"/auth/hosts/enrollment-tokens", access, map[string]any{"label": "denied", "expires_at": time.Now().Add(20 * time.Minute).UTC().Format(time.RFC3339)})
	hostStatus(t, status, 403, p)
	status, p = hostHTTP(t, "GET", url+"/auth/hosts/enrollments/pending", access, nil)
	hostStatus(t, status, 403, p)
	status, p = hostHTTP(t, "DELETE", url+"/hosts/"+id, access, nil)
	hostStatus(t, status, 403, p)
	status, p = hostHTTP(t, "POST", url+"/hosts/"+id+"/bridge/check-in", access, map[string]any{"bridge_instance_id": "bad", "checked_in_at": time.Now().UTC().Format(time.RFC3339), "expires_at": time.Now().Add(time.Minute).UTC().Format(time.RFC3339)})
	hostStatus(t, status, 403, p)
	status, p = hostHTTP(t, "GET", url+"/auth/hosts/enrollment-tokens", admin, nil)
	hostStatus(t, status, 200, p)
	for _, item := range p["enrollment_tokens"].([]any) {
		if item.(map[string]any)["token"] != nil {
			t.Fatal("listed token secret")
		}
	}
	status, p = hostHTTP(t, "POST", url+"/auth/hosts/enrollment-tokens", admin, map[string]any{"label": "revoke-me", "expires_at": time.Now().Add(20 * time.Minute).UTC().Format(time.RFC3339)})
	hostStatus(t, status, 201, p)
	revokeID := p["enrollment_token"].(map[string]any)["id"].(string)
	status, p = hostHTTP(t, "POST", url+"/auth/hosts/enrollment-tokens/"+revokeID+"/revoke", access, nil)
	hostStatus(t, status, 403, p)
	status, p = hostHTTP(t, "POST", url+"/auth/hosts/enrollment-tokens/"+revokeID+"/revoke", admin, nil)
	hostStatus(t, status, 200, p)
	if p["enrollment_token"].(map[string]any)["revoked_at"] == nil {
		t.Fatal(p)
	}
	status, p = grant("codex", signed)
	hostStatus(t, status, 401, p)
	status, p = grant("codex", time.Now().Add(-6*time.Minute).UTC().Format(time.RFC3339))
	hostStatus(t, status, 401, p)
	status, p = hostHTTP(t, "GET", url+"/hosts", "", nil)
	hostStatus(t, status, 401, p)
	status, p = hostHTTP(t, "GET", url+"/hosts", access, nil)
	hostStatus(t, status, 200, p)
	status, p = hostHTTP(t, "GET", url+"/hosts/"+id, access, nil)
	hostStatus(t, status, 200, p)
	status, p = hostSignedHTTP(t, "POST", url+"/hosts/"+id+"/bridge/check-in", id, keyID, "bridge-check-in", priv, map[string]any{"bridge_instance_id": "bridge-1", "checked_in_at": time.Now().UTC().Format(time.RFC3339), "expires_at": time.Now().Add(time.Minute).UTC().Format(time.RFC3339)})
	hostStatus(t, status, 200, p)
	principals, _, err := env.authStore.ListPrincipals(context.Background(), auth.AuthPrincipalListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, principal := range principals {
		if principal.AgentID == agent["id"] {
			routing := auth.DescribeWakeRouting(principal, "ws_main", time.Now().UTC())
			if !routing.Taggable || !routing.Online {
				t.Fatalf("derived wake routing: %#v", routing)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("derived agent missing from principal inventory")
	}
	status, p = hostSignedHTTP(t, "PATCH", url+"/hosts/"+id, id, keyID, "patch", priv, map[string]any{"display_name": "Signed Host"})
	hostStatus(t, status, 200, p)
	patchBody := map[string]any{"display_name": "Replayed Host"}
	patchSigned := time.Now().UTC().Format(time.RFC3339Nano)
	status, p = hostSignedHTTPAt(t, "PATCH", url+"/hosts/"+id, id, keyID, "patch", priv, patchBody, patchSigned)
	hostStatus(t, status, 200, p)
	status, p = hostSignedHTTPAt(t, "PATCH", url+"/hosts/"+id, id, keyID, "patch", priv, patchBody, patchSigned)
	hostStatus(t, status, 401, p)
	status, p = hostSignedHTTPAt(t, "PATCH", url+"/hosts/"+id, id, keyID, "patch", priv, map[string]any{"display_name": "Old Host"}, time.Now().Add(-6*time.Minute).UTC().Format(time.RFC3339Nano))
	hostStatus(t, status, 401, p)
	status, p = hostHTTP(t, "PATCH", url+"/hosts/"+id, access, map[string]any{"excluded_names": []string{"codex"}})
	hostStatus(t, status, 403, p)
	status, p = hostHTTP(t, "PATCH", url+"/hosts/"+id, admin, map[string]any{"display_name": "Dev Host", "excluded_names": []string{"codex"}})
	hostStatus(t, status, 200, p)
	status, p = grant("codex", time.Now().UTC().Format(time.RFC3339))
	hostStatus(t, status, 403, p)
	if p["error"].(map[string]any)["code"] != "agent_excluded" {
		t.Fatalf("excluded grant: %#v", p)
	}
	status, p = hostHTTP(t, "GET", url+"/hosts", access, nil)
	hostStatus(t, status, 401, p)
	status, p = grant("generic", time.Now().UTC().Format(time.RFC3339))
	hostStatus(t, status, 200, p)
	otherAccess := p["tokens"].(map[string]any)["access_token"].(string)
	status, p = hostHTTP(t, "DELETE", url+"/hosts/"+id, admin, nil)
	hostStatus(t, status, 200, p)
	status, p = hostHTTP(t, "GET", url+"/hosts", otherAccess, nil)
	hostStatus(t, status, 401, p)
	status, p = hostHTTP(t, "DELETE", url+"/hosts/"+id, admin, nil)
	hostStatus(t, status, 200, p)
	status, p = grant("generic", time.Now().UTC().Format(time.RFC3339))
	hostStatus(t, status, 403, p)
	if p["error"].(map[string]any)["code"] != "host_revoked" {
		t.Fatalf("revoked host grant: %#v", p)
	}
	events, _, err := env.authStore.ListAuditEvents(context.Background(), auth.AuthAuditListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	revokes := 0
	for _, event := range events {
		if event.EventType == "host_revoked" {
			revokes++
		}
	}
	if revokes != 1 {
		t.Fatalf("host revoke audit count = %d, want 1", revokes)
	}
}

func TestHostInteractiveApproveDenyAndPoll(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken, allowPasskeyDevBypass: true})
	url := env.server.URL
	status, p := hostHTTP(t, "POST", url+"/auth/passkey/dev/register", "", map[string]any{"display_name": "Admin", "bootstrap_token": testBootstrapToken})
	hostStatus(t, status, 201, p)
	admin := p["tokens"].(map[string]any)["access_token"].(string)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public := base64.StdEncoding.EncodeToString(pub)
	start := func(slug string) (string, string) {
		nonce := base64.RawURLEncoding.EncodeToString(pub[:16])
		status, p := hostHTTP(t, "POST", url+"/auth/hosts/enrollments", "", map[string]any{"public_key": public, "requested_slug": slug, "os_user": "operator", "hostname": "laptop", "discovered_adapters": []string{}, "request_nonce": nonce, "adoptions": []any{}})
		hostStatus(t, status, 201, p)
		code := p["user_code"].(string)
		if !regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{4}-[0-9A-HJKMNP-TV-Z]{4}$`).MatchString(code) {
			t.Fatalf("ambiguous enrollment user code: %q", code)
		}
		if code == p["poll_token"] {
			t.Fatal("user code reused poll secret")
		}
		return p["enrollment_id"].(string), p["poll_token"].(string)
	}
	id, poll := start("interactive")
	status, p = hostHTTP(t, "GET", url+"/auth/hosts/enrollments/pending", "", nil)
	hostStatus(t, status, 401, p)
	status, p = hostHTTP(t, "GET", url+"/auth/hosts/enrollments/pending", admin, nil)
	hostStatus(t, status, 200, p)
	if len(p["enrollments"].([]any)) != 1 {
		t.Fatal(p)
	}
	req, err := http.NewRequest("GET", url+"/auth/hosts/enrollments/"+id, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-ANX-Enrollment-Token", poll)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("poll %d", resp.StatusCode)
	}
	resp.Body.Close()
	complete := func(id, poll string) (int, map[string]any) {
		return hostHTTP(t, "POST", url+"/auth/hosts/enrollments/"+id+"/complete", "", map[string]any{"poll_token": poll, "signature": hostSign(priv, "anx-host-enroll-complete|"+id+"|"+poll)})
	}
	status, p = complete(id, poll)
	hostStatus(t, status, 409, p)
	status, p = hostHTTP(t, "POST", url+"/auth/hosts/enrollments/"+id+"/approve", admin, nil)
	hostStatus(t, status, 200, p)
	status, p = complete(id, poll)
	hostStatus(t, status, 201, p)
	host := p["host"].(map[string]any)
	hostID, hostKeyID := host["id"].(string), host["key_id"].(string)
	signed := time.Now().UTC().Format(time.RFC3339)
	status, p = hostHTTP(t, "POST", url+"/auth/token", "", map[string]any{"grant_type": "host_assertion", "host_id": hostID, "key_id": hostKeyID, "agent_name": "worker", "signed_at": signed, "signature": hostSign(priv, "anx-host-agent-token|"+hostID+"|"+hostKeyID+"|worker|"+signed)})
	hostStatus(t, status, 200, p)
	workerAccess := p["tokens"].(map[string]any)["access_token"].(string)
	status, p = complete(id, poll)
	hostStatus(t, status, 409, p)
	denied, denyPoll := start("denied")
	status, p = hostHTTP(t, "POST", url+"/auth/hosts/enrollments/"+denied+"/approve", workerAccess, nil)
	hostStatus(t, status, 403, p)
	status, p = hostHTTP(t, "POST", url+"/auth/hosts/enrollments/"+denied+"/deny", workerAccess, nil)
	hostStatus(t, status, 403, p)
	status, p = hostHTTP(t, "POST", url+"/auth/hosts/enrollments/"+denied+"/deny", admin, nil)
	hostStatus(t, status, 200, p)
	status, p = complete(denied, denyPoll)
	hostStatus(t, status, 403, p)
}

func TestHostAdoptionProofPreservesActor(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken, allowPasskeyDevBypass: true})
	url := env.server.URL
	status, p := hostHTTP(t, "POST", url+"/auth/passkey/dev/register", "", map[string]any{"display_name": "Admin", "bootstrap_token": testBootstrapToken})
	hostStatus(t, status, 201, p)
	admin := p["tokens"].(map[string]any)["access_token"].(string)
	oldPub, oldPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	db := env.workspace.DB()
	if _, err = db.Exec(`INSERT INTO actors(id,display_name,tags_json,created_at,metadata_json) VALUES('actor-old','Old','["agent"]',?,'{}')`, now); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) VALUES('agent-old','old','actor-old',?,?,'{"principal_kind":"agent","auth_method":"public_key"}')`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO agent_keys(id,agent_id,public_key,algorithm,created_at) VALUES('key-old','agent-old',?,'ed25519',?)`, base64.StdEncoding.EncodeToString(oldPub), now); err != nil {
		t.Fatal(err)
	}
	hostPub, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public := base64.StdEncoding.EncodeToString(hostPub)
	nonce := base64.RawURLEncoding.EncodeToString(hostPub[:16])
	proof := map[string]any{"agent_id": "agent-old", "key_id": "key-old", "agent_name": "reviewer", "signature": hostSign(oldPriv, "anx-host-adopt|"+nonce+"|"+public+"|agent-old|reviewer")}
	body := map[string]any{"public_key": public, "requested_slug": "adopt-host", "os_user": "test", "hostname": "test", "discovered_adapters": []string{}, "request_nonce": nonce, "adoptions": []any{proof}}
	badProof := map[string]any{"agent_id": "agent-old", "key_id": "key-old", "agent_name": "reviewer", "signature": hostSign(hostPriv, "anx-host-adopt|"+nonce+"|"+public+"|agent-old|reviewer")}
	body["adoptions"] = []any{badProof}
	status, p = hostHTTP(t, "POST", url+"/auth/hosts/enrollments", "", body)
	hostStatus(t, status, 401, p)
	body["adoptions"] = []any{proof}
	status, p = hostHTTP(t, "POST", url+"/auth/hosts/enrollments", "", body)
	hostStatus(t, status, 201, p)
	id, poll := p["enrollment_id"].(string), p["poll_token"].(string)
	status, p = hostHTTP(t, "POST", url+"/auth/hosts/enrollments/"+id+"/approve", admin, nil)
	hostStatus(t, status, 200, p)
	status, p = hostHTTP(t, "POST", url+"/auth/hosts/enrollments/"+id+"/complete", "", map[string]any{"poll_token": poll, "signature": hostSign(hostPriv, "anx-host-enroll-complete|"+id+"|"+poll)})
	hostStatus(t, status, 201, p)
	agents := p["host"].(map[string]any)["agents"].([]any)
	if len(agents) != 1 {
		t.Fatal(agents)
	}
	a := agents[0].(map[string]any)
	if a["actor_id"] != "actor-old" || a["handle"] != "reviewer.adopt-host" || a["identity_kind"] != "adopted" {
		t.Fatal(a)
	}
	hostID, keyID := p["host"].(map[string]any)["id"].(string), p["host"].(map[string]any)["key_id"].(string)
	grantAt := time.Now().UTC().Format(time.RFC3339Nano)
	status, p = hostHTTP(t, "POST", url+"/auth/token", "", map[string]any{"grant_type": "host_assertion", "host_id": hostID, "key_id": keyID, "agent_name": "reviewer", "signed_at": grantAt, "signature": hostSign(hostPriv, "anx-host-agent-token|"+hostID+"|"+keyID+"|reviewer|"+grantAt)})
	hostStatus(t, status, 200, p)
	adoptedAccess := p["tokens"].(map[string]any)["access_token"].(string)
	status, p = hostHTTP(t, "GET", url+"/agents/me", adoptedAccess, nil)
	hostStatus(t, status, 200, p)
	if self := p["agent"].(map[string]any); self["actor_id"] != "actor-old" || self["identity_kind"] != "adopted" || self["persona"] != true {
		t.Fatalf("adopted persona self-identification: %#v", p)
	}
	signed := time.Now().UTC().Format(time.RFC3339)
	status, p = hostHTTP(t, "POST", url+"/auth/token", "", map[string]any{"grant_type": "assertion", "agent_id": "agent-old", "key_id": "key-old", "signed_at": signed, "signature": hostSign(oldPriv, "anx-auth-token|agent-old|key-old|"+signed)})
	hostStatus(t, status, 401, p)
}

func TestHostPersonaRunRosterAndBridge(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken, allowPasskeyDevBypass: true})
	url := env.server.URL
	status, p := hostHTTP(t, "POST", url+"/auth/passkey/dev/register", "", map[string]any{"display_name": "Admin", "bootstrap_token": testBootstrapToken})
	hostStatus(t, status, 201, p)
	admin := p["tokens"].(map[string]any)["access_token"].(string)
	status, p = hostHTTP(t, "POST", url+"/auth/hosts/enrollment-tokens", admin, map[string]any{"label": "run test", "expires_at": time.Now().Add(20 * time.Minute).UTC().Format(time.RFC3339)})
	hostStatus(t, status, 201, p)
	secret := p["token"].(string)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public := base64.StdEncoding.EncodeToString(pub)
	nonce := base64.RawURLEncoding.EncodeToString(pub[:16])
	slug := "m5-mbp"
	status, p = hostHTTP(t, "POST", url+"/auth/hosts/enrollments/headless", "", map[string]any{
		"public_key": public, "requested_slug": slug, "os_user": "operator", "hostname": slug,
		"discovered_adapters": []string{"codex"}, "request_nonce": nonce, "adoptions": []any{},
		"enrollment_token": secret, "signature": hostSign(priv, "anx-host-headless-enroll|"+nonce+"|"+slug+"|"+public),
	})
	hostStatus(t, status, 201, p)
	host := p["host"].(map[string]any)
	hostID, keyID := host["id"].(string), host["key_id"].(string)
	signed := time.Now().UTC().Format(time.RFC3339Nano)
	name := "reviewer"
	status, p = hostHTTP(t, "POST", url+"/auth/token", "", map[string]any{
		"grant_type": "host_assertion", "host_id": hostID, "key_id": keyID, "agent_name": name,
		"signed_at": signed, "signature": hostSign(priv, "anx-host-agent-token|"+hostID+"|"+keyID+"|"+name+"|"+signed),
	})
	hostStatus(t, status, 200, p)
	agent := p["agent"].(map[string]any)
	access := p["tokens"].(map[string]any)["access_token"].(string)
	status, p = hostHTTP(t, "GET", url+"/stream/agents", "", nil)
	hostStatus(t, status, 401, p)
	streamReq, err := http.NewRequest("GET", url+"/stream/agents", nil)
	if err != nil {
		t.Fatal(err)
	}
	streamReq.Header.Set("Authorization", "Bearer "+admin)
	streamClient := &http.Client{Timeout: 5 * time.Second}
	streamResp, err := streamClient.Do(streamReq)
	if err != nil {
		t.Fatal(err)
	}
	if streamResp.StatusCode != 200 || streamResp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("agent stream response: %d %s", streamResp.StatusCode, streamResp.Header.Get("Content-Type"))
	}
	changes, stop := startSSEReader(streamResp.Body)
	defer stop()
	initial := awaitSSEEvent(t, changes, 2*time.Second)
	if initial.Event != "agents_changed" {
		t.Fatalf("initial agent signal: %#v", initial)
	}
	eventsBefore := countTableRows(t, env.workspace.DB(), "events")
	observed := time.Now().UTC().Format(time.RFC3339Nano)
	runBody := map[string]any{"launcher": "agentctl", "external_id": "exec-persona", "host_id": hostID,
		"agent_id": agent["id"], "adapter": "codex", "state": "running", "liveness": "alive",
		"result_collected": false, "labels": []string{}, "last_observed_at": observed}
	resp := postJSONExpectStatusWithHeaders(t, url+"/runs", runBody,
		map[string]string{"Authorization": "Bearer " + access, "X-ANX-Run-Id": "agentctl/exec-persona"}, http.StatusOK)
	var runResponse map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&runResponse); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if runResponse["run"].(map[string]any)["adapter"] != "codex" {
		t.Fatal(runResponse)
	}
	change := awaitSSEEvent(t, changes, 2*time.Second)
	if change.Event != "agents_changed" || change.Data["revision"].(float64) <= initial.Data["revision"].(float64) {
		t.Fatalf("run change signal: %#v after %#v", change, initial)
	}
	if got := countTableRows(t, env.workspace.DB(), "events"); got != eventsBefore {
		t.Fatalf("run telemetry wrote canonical event: %d to %d", eventsBefore, got)
	}
	status, p = hostHTTP(t, "PATCH", url+"/agents/me/presence", access, map[string]any{"note": "Reviewing the capture build"})
	hostStatus(t, status, 200, p)
	change = awaitSSEEvent(t, changes, 2*time.Second)
	if change.Event != "agents_changed" {
		t.Fatalf("presence change signal: %#v", change)
	}
	if got := countTableRows(t, env.workspace.DB(), "events"); got != eventsBefore {
		t.Fatalf("presence telemetry wrote canonical event: %d to %d", eventsBefore, got)
	}
	checkRoster := func(wantOnline bool) {
		t.Helper()
		status, p := hostHTTP(t, "GET", url+"/agents", admin, nil)
		hostStatus(t, status, 200, p)
		for _, item := range p["agents"].([]any) {
			row := item.(map[string]any)
			if row["id"] != agent["id"] {
				continue
			}
			if row["handle"] != "reviewer.m5-mbp" || row["display_name"] != "reviewer on m5-mbp" ||
				row["name"] != "reviewer" || row["identity_kind"] != "derived" || row["state"] != "working" ||
				row["bridge_online"] != wantOnline || row["active_run"].(map[string]any)["adapter"] != "codex" {
				t.Fatalf("persona roster: %#v", row)
			}
			status, detail := hostHTTP(t, "GET", url+"/hosts/"+hostID, admin, nil)
			hostStatus(t, status, 200, detail)
			hostAgents := detail["host"].(map[string]any)["agents"].([]any)
			if len(hostAgents) != 1 || hostAgents[0].(map[string]any)["state"] != row["state"] || hostAgents[0].(map[string]any)["bridge_online"] != row["bridge_online"] {
				t.Fatalf("host detail diverged from roster: %#v vs %#v", hostAgents, row)
			}
			status, listed := hostHTTP(t, "GET", url+"/hosts", admin, nil)
			hostStatus(t, status, 200, listed)
			listAgents := listed["hosts"].([]any)[0].(map[string]any)["agents"].([]any)
			if len(listAgents) != 1 || listAgents[0].(map[string]any)["state"] != row["state"] {
				t.Fatalf("host list diverged from roster: %#v vs %#v", listAgents, row)
			}
			return
		}
		t.Fatal("derived persona missing from roster")
	}
	checkRoster(false)
	status, p = hostSignedHTTP(t, "POST", url+"/hosts/"+hostID+"/bridge/check-in", hostID, keyID, "bridge-check-in", priv,
		map[string]any{"bridge_instance_id": "bridge-1", "checked_in_at": time.Now().UTC().Format(time.RFC3339), "expires_at": time.Now().Add(2 * time.Minute).UTC().Format(time.RFC3339)})
	hostStatus(t, status, 200, p)
	checkRoster(true)
	change = awaitSSEEvent(t, changes, 2*time.Second)
	if change.Event != "agents_changed" {
		t.Fatalf("bridge change signal: %#v", change)
	}
	status, p = hostHTTP(t, "PATCH", url+"/hosts/"+hostID, admin, map[string]any{"excluded_names": []string{name}})
	hostStatus(t, status, 200, p)
	checkRoster(false)
}

func TestRevokedDerivedAgentGrantCreatesNoToken(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken, allowPasskeyDevBypass: true})
	url := env.server.URL
	status, p := hostHTTP(t, "POST", url+"/auth/passkey/dev/register", "", map[string]any{"display_name": "Admin", "bootstrap_token": testBootstrapToken})
	hostStatus(t, status, 201, p)
	admin := p["tokens"].(map[string]any)["access_token"].(string)
	status, p = hostHTTP(t, "POST", url+"/auth/hosts/enrollment-tokens", admin, map[string]any{"label": "revoke test", "expires_at": time.Now().Add(20 * time.Minute).UTC().Format(time.RFC3339)})
	hostStatus(t, status, 201, p)
	secret := p["token"].(string)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public := base64.StdEncoding.EncodeToString(pub)
	nonce := base64.RawURLEncoding.EncodeToString(pub[:16])
	slug := "revoke-host"
	status, p = hostHTTP(t, "POST", url+"/auth/hosts/enrollments/headless", "", map[string]any{"public_key": public, "requested_slug": slug, "os_user": "test", "hostname": "test", "discovered_adapters": []string{}, "request_nonce": nonce, "adoptions": []any{}, "enrollment_token": secret, "signature": hostSign(priv, "anx-host-headless-enroll|"+nonce+"|"+slug+"|"+public)})
	hostStatus(t, status, 201, p)
	host := p["host"].(map[string]any)
	hostID, keyID := host["id"].(string), host["key_id"].(string)
	grant := func(name string) (int, map[string]any) {
		signed := time.Now().UTC().Format(time.RFC3339Nano)
		return hostHTTP(t, "POST", url+"/auth/token", "", map[string]any{"grant_type": "host_assertion", "host_id": hostID, "key_id": keyID, "agent_name": name, "signed_at": signed, "signature": hostSign(priv, "anx-host-agent-token|"+hostID+"|"+keyID+"|"+name+"|"+signed)})
	}
	status, p = grant("codex")
	hostStatus(t, status, 200, p)
	agentID := p["agent"].(map[string]any)["id"].(string)
	if _, err := env.workspace.DB().Exec(`UPDATE agents SET revoked_at=? WHERE id=?`, time.Now().UTC().Format(time.RFC3339Nano), agentID); err != nil {
		t.Fatal(err)
	}
	before := countTableRows(t, env.workspace.DB(), "auth_access_tokens")
	sessionsBefore := countTableRows(t, env.workspace.DB(), "auth_refresh_sessions")
	status, p = grant("codex")
	hostStatus(t, status, 403, p)
	if p["error"].(map[string]any)["code"] != "agent_revoked" {
		t.Fatalf("revoked agent grant: %#v", p)
	}
	if after := countTableRows(t, env.workspace.DB(), "auth_access_tokens"); after != before {
		t.Fatalf("revoked grant created token: %d to %d", before, after)
	}
	if after := countTableRows(t, env.workspace.DB(), "auth_refresh_sessions"); after != sessionsBefore {
		t.Fatalf("revoked grant created session: %d to %d", sessionsBefore, after)
	}
	status, p = hostHTTP(t, "PATCH", url+"/hosts/"+hostID, admin, map[string]any{"excluded_names": []string{"cursor"}})
	hostStatus(t, status, 200, p)
	before = countTableRows(t, env.workspace.DB(), "auth_access_tokens")
	status, p = grant("cursor")
	hostStatus(t, status, 403, p)
	if p["error"].(map[string]any)["code"] != "agent_excluded" {
		t.Fatalf("excluded grant: %#v", p)
	}
	if after := countTableRows(t, env.workspace.DB(), "auth_access_tokens"); after != before {
		t.Fatalf("excluded grant created token: %d to %d", before, after)
	}
	status, p = hostHTTP(t, "DELETE", url+"/hosts/"+hostID, admin, nil)
	hostStatus(t, status, 200, p)
	before = countTableRows(t, env.workspace.DB(), "auth_access_tokens")
	status, p = grant("generic")
	hostStatus(t, status, 403, p)
	if p["error"].(map[string]any)["code"] != "host_revoked" {
		t.Fatalf("revoked host grant: %#v", p)
	}
	if after := countTableRows(t, env.workspace.DB(), "auth_access_tokens"); after != before {
		t.Fatalf("revoked host grant created token: %d to %d", before, after)
	}
}

func TestInteractiveEnrollmentBoundsAndRetention(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken, allowPasskeyDevBypass: true})
	ctx := context.Background()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	base := auth.HostEnrollmentInput{PublicKey: base64.StdEncoding.EncodeToString(pub), RequestedSlug: "host-0", OSUser: "tester", Hostname: "laptop", DiscoveredAdapters: []string{}, RequestNonce: base64.RawURLEncoding.EncodeToString(pub[:16]), Adoptions: []auth.AdoptionProof{}}
	oversized := base
	oversized.OSUser = string(bytes.Repeat([]byte("x"), 129))
	if _, err := env.authStore.StartHostEnrollment(ctx, oversized, "192.0.2.1"); err != auth.ErrInvalidRequest {
		t.Fatalf("oversized user accepted: %v", err)
	}
	oversized = base
	oversized.DiscoveredAdapters = make([]string, 17)
	for i := range oversized.DiscoveredAdapters {
		oversized.DiscoveredAdapters[i] = "name-" + string(rune('a'+i))
	}
	if _, err := env.authStore.StartHostEnrollment(ctx, oversized, "192.0.2.1"); err != auth.ErrInvalidRequest {
		t.Fatalf("oversized adapters accepted: %v", err)
	}
	oversized = base
	oversized.Adoptions = make([]auth.AdoptionProof, 17)
	if _, err := env.authStore.StartHostEnrollment(ctx, oversized, "192.0.2.1"); err != auth.ErrInvalidRequest {
		t.Fatalf("oversized adoptions accepted: %v", err)
	}
	startedIDs := []string{}
	for i := 0; i < 4; i++ {
		in := base
		in.RequestedSlug = "host-" + string(rune('a'+i))
		started, err := env.authStore.StartHostEnrollment(ctx, in, "192.0.2.1")
		if err != nil {
			t.Fatal(err)
		}
		startedIDs = append(startedIDs, started.EnrollmentID)
	}
	base.RequestedSlug = "host-over-cap"
	if _, err := env.authStore.StartHostEnrollment(ctx, base, "192.0.2.1"); err != auth.ErrEnrollmentCapacity {
		t.Fatalf("source cap: %v", err)
	}
	var auditCount int
	if err := env.workspace.DB().QueryRow(`SELECT COUNT(*) FROM auth_audit_events WHERE event_type='host_enroll_started'`).Scan(&auditCount); err != nil || auditCount != 0 {
		t.Fatalf("unauthenticated start audit grew: %d %v", auditCount, err)
	}
	old := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339Nano)
	for i, status := range []string{"denied", "expired", "completed"} {
		if _, err := env.workspace.DB().Exec(`UPDATE host_enrollments SET status=?,created_at=?,expires_at=? WHERE id=?`, status, old, old, startedIDs[i]); err != nil {
			t.Fatal(err)
		}
	}
	base.RequestedSlug = "host-after-purge"
	if _, err := env.authStore.StartHostEnrollment(ctx, base, "192.0.2.1"); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := env.workspace.DB().QueryRow(`SELECT COUNT(*) FROM host_enrollments WHERE id IN (?,?,?)`, startedIDs[0], startedIDs[1], startedIDs[2]).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("old terminal enrollments retained: %d %v", remaining, err)
	}
	if err := env.workspace.DB().QueryRow(`SELECT COUNT(*) FROM host_enrollments WHERE status='pending'`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	for i := remaining; i < 64; i++ {
		in := base
		in.RequestedSlug = "workspace-cap-" + string(rune('a'+i/26)) + string(rune('a'+i%26))
		if _, err := env.authStore.StartHostEnrollment(ctx, in, "198.51.100."+string(rune('A'+i))); err != nil {
			t.Fatalf("fill workspace cap %d: %v", i, err)
		}
	}
	base.RequestedSlug = "workspace-over-cap"
	if _, err := env.authStore.StartHostEnrollment(ctx, base, "203.0.113.1"); err != auth.ErrEnrollmentCapacity {
		t.Fatalf("workspace cap: %v", err)
	}
}
