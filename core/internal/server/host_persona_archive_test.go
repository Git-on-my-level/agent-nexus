package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"testing"
	"time"
)

// Archive cuts every access path of one persona while keeping its principal,
// actor and name; restore lets the same actor back in.
func TestHostPersonaArchiveCutsAccessAndRestore(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken, allowPasskeyDevBypass: true})
	url := env.server.URL
	db := env.workspace.DB()
	status, p := hostHTTP(t, "POST", url+"/auth/passkey/dev/register", "", map[string]any{"display_name": "Admin", "bootstrap_token": testBootstrapToken})
	hostStatus(t, status, 201, p)
	admin := p["tokens"].(map[string]any)["access_token"].(string)
	status, p = hostHTTP(t, "POST", url+"/auth/hosts/enrollment-tokens", admin, map[string]any{"label": "archive test", "expires_at": time.Now().Add(20 * time.Minute).UTC().Format(time.RFC3339)})
	hostStatus(t, status, 201, p)
	secret := p["token"].(string)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public := base64.StdEncoding.EncodeToString(pub)
	nonce := base64.RawURLEncoding.EncodeToString(pub[:16])
	slug := "archive-host"
	status, p = hostHTTP(t, "POST", url+"/auth/hosts/enrollments/headless", "", map[string]any{"public_key": public, "requested_slug": slug, "os_user": "test", "hostname": "test", "discovered_adapters": []string{"codex"}, "request_nonce": nonce, "adoptions": []any{}, "enrollment_token": secret, "signature": hostSign(priv, "anx-host-headless-enroll|"+nonce+"|"+slug+"|"+public)})
	hostStatus(t, status, 201, p)
	host := p["host"].(map[string]any)
	hostID, keyID := host["id"].(string), host["key_id"].(string)
	grant := func(name string) (int, map[string]any) {
		signed := time.Now().UTC().Format(time.RFC3339Nano)
		return hostHTTP(t, "POST", url+"/auth/token", "", map[string]any{"grant_type": "host_assertion", "host_id": hostID, "key_id": keyID, "agent_name": name, "signed_at": signed, "signature": hostSign(priv, "anx-host-agent-token|"+hostID+"|"+keyID+"|"+name+"|"+signed)})
	}

	// Registration reports near-duplicates on the same host.
	status, p = grant("codex-spkfinal")
	hostStatus(t, status, 200, p)
	if p["registered"] != true || len(p["similar_names"].([]any)) != 0 {
		t.Fatalf("first persona registration: %#v", p)
	}
	unusedID := p["agent"].(map[string]any)["id"].(string)
	status, p = grant("codex-spkfinal-r2")
	hostStatus(t, status, 200, p)
	if p["registered"] != true || len(p["similar_names"].([]any)) != 1 || p["similar_names"].([]any)[0] != "codex-spkfinal" {
		t.Fatalf("near-duplicate registration: %#v", p)
	}
	agentID := p["agent"].(map[string]any)["id"].(string)
	actorID := p["agent"].(map[string]any)["actor_id"].(string)
	access := p["tokens"].(map[string]any)["access_token"].(string)
	status, p = grant("codex-spkfinal-r2")
	hostStatus(t, status, 200, p)
	if _, ok := p["registered"]; ok {
		t.Fatalf("existing persona reported as registered: %#v", p)
	}
	access2 := p["tokens"].(map[string]any)["access_token"].(string)

	// Activity is per persona: last_used_at comes from its own token use.
	status, p = hostHTTP(t, "GET", url+"/hosts/"+hostID, admin, nil)
	hostStatus(t, status, 200, p)
	for _, item := range p["host"].(map[string]any)["agents"].([]any) {
		row := item.(map[string]any)
		if row["last_used_at"] == nil || row["archived_at"] != nil {
			t.Fatalf("persona activity: %#v", row)
		}
	}

	// A live host bridge is not activity for personas it does not serve.
	status, p = hostSignedHTTP(t, "POST", url+"/hosts/"+hostID+"/bridge/check-in", hostID, keyID, "bridge-check-in", priv,
		map[string]any{"instance_id": "bridge-0", "sequence": 1, "config_revision": 1, "version": "test", "agents": []any{map[string]any{"actor_id": actorID, "jobs": []any{map[string]any{"kind": "wake", "enabled": true, "ready": true}}}}})
	hostStatus(t, status, 200, p)
	status, p = hostHTTP(t, "GET", url+"/agents", admin, nil)
	hostStatus(t, status, 200, p)
	for _, item := range p["agents"].([]any) {
		row := item.(map[string]any)
		switch row["id"] {
		case unusedID:
			if row["last_signal_at"] != nil || row["state"] == "idle" || row["bridge_online"] != false {
				t.Fatalf("unserved persona inherited host bridge presence: %#v", row)
			}
		case agentID:
			if row["state"] != "idle" || row["bridge_online"] != true {
				t.Fatalf("served persona: %#v", row)
			}
		}
	}

	// Agents, even with their own valid bearer, cannot archive.
	archiveURL := url + "/hosts/" + hostID + "/agents/codex-spkfinal-r2/archive"
	status, p = hostHTTP(t, "POST", archiveURL, access, nil)
	hostStatus(t, status, 403, p)
	status, p = hostHTTP(t, "POST", archiveURL, "", nil)
	hostStatus(t, status, 401, p)
	status, p = hostHTTP(t, "POST", url+"/hosts/"+hostID+"/agents/nobody/archive", admin, nil)
	hostStatus(t, status, 404, p)

	status, p = hostHTTP(t, "POST", archiveURL, admin, nil)
	hostStatus(t, status, 200, p)
	archivedRow := map[string]any(nil)
	for _, item := range p["host"].(map[string]any)["agents"].([]any) {
		if row := item.(map[string]any); row["name"] == "codex-spkfinal-r2" {
			archivedRow = row
		}
	}
	if archivedRow == nil || archivedRow["archived_at"] == nil || archivedRow["archived_reason"] != "manual" || archivedRow["revoked_at"] != nil {
		t.Fatalf("archived persona row: %#v", archivedRow)
	}

	// Tokens and sessions issued before the archive stop working.
	for _, token := range []string{access, access2} {
		status, p = hostHTTP(t, "GET", url+"/agents/me", token, nil)
		hostStatus(t, status, 401, p)
	}
	var live int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM auth_access_tokens WHERE agent_id=? AND revoked_at IS NULL)+(SELECT COUNT(*) FROM auth_refresh_sessions WHERE agent_id=? AND revoked_at IS NULL)`, agentID, agentID).Scan(&live); err != nil || live != 0 {
		t.Fatalf("live credentials after archive: %d %v", live, err)
	}

	// The name stays reserved: no token, no new principal, a clear error.
	tokensBefore := countTableRows(t, db, "auth_access_tokens")
	agentsBefore := countTableRows(t, db, "agents")
	status, p = grant("codex-spkfinal-r2")
	hostStatus(t, status, 403, p)
	if p["error"].(map[string]any)["code"] != "agent_archived" || p["error"].(map[string]any)["message"] != agentArchivedMessage {
		t.Fatalf("archived name grant: %#v", p)
	}
	if countTableRows(t, db, "auth_access_tokens") != tokensBefore || countTableRows(t, db, "agents") != agentsBefore {
		t.Fatal("archived name minted a token or a principal")
	}

	// The host bridge cannot serve the archived actor.
	status, p = hostSignedHTTP(t, "POST", url+"/hosts/"+hostID+"/bridge/check-in", hostID, keyID, "bridge-check-in", priv,
		map[string]any{"instance_id": "bridge-0", "sequence": 2, "config_revision": 1, "version": "test", "agents": []any{map[string]any{"actor_id": actorID, "jobs": []any{map[string]any{"kind": "wake", "enabled": true, "ready": true}}}}})
	hostStatus(t, status, 403, p)

	// Defense in depth: a token that somehow stayed live is still refused.
	if _, err := db.Exec(`UPDATE auth_access_tokens SET revoked_at=NULL WHERE agent_id=?`, agentID); err != nil {
		t.Fatal(err)
	}
	status, p = hostHTTP(t, "GET", url+"/agents/me", access2, nil)
	hostStatus(t, status, 403, p)
	if p["error"].(map[string]any)["code"] != "agent_archived" {
		t.Fatalf("archived principal authenticated: %#v", p)
	}
	if _, err := db.Exec(`UPDATE auth_access_tokens SET revoked_at=? WHERE agent_id=?`, time.Now().UTC().Format(time.RFC3339Nano), agentID); err != nil {
		t.Fatal(err)
	}

	// Archive is idempotent; restore lets the same actor back in.
	status, p = hostHTTP(t, "POST", archiveURL, admin, nil)
	hostStatus(t, status, 200, p)
	restoreURL := url + "/hosts/" + slug + "/agents/codex-spkfinal-r2/restore"
	status, p = grant("codex")
	hostStatus(t, status, 200, p)
	status, p = hostHTTP(t, "POST", restoreURL, p["tokens"].(map[string]any)["access_token"].(string), nil)
	hostStatus(t, status, 403, p)
	status, p = hostHTTP(t, "POST", restoreURL, admin, nil)
	hostStatus(t, status, 200, p)
	status, p = grant("codex-spkfinal-r2")
	hostStatus(t, status, 200, p)
	if p["agent"].(map[string]any)["id"] != agentID || p["agent"].(map[string]any)["actor_id"] != actorID {
		t.Fatalf("restore minted a new principal: %#v", p)
	}
	restoredAccess := p["tokens"].(map[string]any)["access_token"].(string)
	status, p = hostHTTP(t, "GET", url+"/agents/me", restoredAccess, nil)
	hostStatus(t, status, 200, p)

	// last_used_at is for human and auth-admin readers only.
	status, p = hostHTTP(t, "GET", url+"/hosts/"+hostID, restoredAccess, nil)
	hostStatus(t, status, 200, p)
	for _, item := range p["host"].(map[string]any)["agents"].([]any) {
		if _, ok := item.(map[string]any)["last_used_at"]; ok && item.(map[string]any)["last_used_at"] != nil {
			t.Fatalf("agent reader saw last_used_at: %#v", item)
		}
	}
	status, p = hostHTTP(t, "GET", url+"/agents", restoredAccess, nil)
	hostStatus(t, status, 200, p)
	for _, item := range p["agents"].([]any) {
		if item.(map[string]any)["last_used_at"] != nil {
			t.Fatalf("agent reader saw last_used_at on /agents: %#v", item)
		}
	}

	// An auto (inactive) archive only hides the persona: the next valid host
	// assertion for the name unarchives it and mints for the same principal.
	if _, err := db.Exec(`UPDATE host_agents SET archived_at=?,archived_reason='inactive' WHERE host_id=? AND name='codex-spkfinal'`, time.Now().UTC().Format(time.RFC3339Nano), hostID); err != nil {
		t.Fatal(err)
	}
	status, p = grant("codex-spkfinal")
	hostStatus(t, status, 200, p)
	if p["agent"].(map[string]any)["id"] != unusedID || p["registered"] != nil {
		t.Fatalf("auto-archived name reuse: %#v", p)
	}
	var archivedAt, restoredAt sql.NullString
	if err := db.QueryRow(`SELECT archived_at,restored_at FROM host_agents WHERE host_id=? AND name='codex-spkfinal'`, hostID).Scan(&archivedAt, &restoredAt); err != nil || archivedAt.Valid || !restoredAt.Valid {
		t.Fatalf("auto unarchive state: archived=%v restored=%v err=%v", archivedAt, restoredAt, err)
	}
	var unarchived int
	if err := db.QueryRow(`SELECT COUNT(*) FROM auth_audit_events WHERE event_type='host_agent_unarchived' AND json_extract(metadata_json,'$.reason')='reused'`).Scan(&unarchived); err != nil || unarchived != 1 {
		t.Fatalf("unarchive audit: %d %v", unarchived, err)
	}
}
