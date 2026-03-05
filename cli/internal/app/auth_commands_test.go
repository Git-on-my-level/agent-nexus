package app

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"organization-autorunner-cli/internal/profile"
)

func TestAuthRegisterLifecycleCommands(t *testing.T) {
	t.Parallel()

	core := newFakeAuthCore(t)
	server := httptest.NewServer(http.HandlerFunc(core.handle))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}

	registerOut := runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "--agent", "agent-a", "auth", "register", "--username", "Agent.One"})
	assertEnvelopeOK(t, registerOut)

	profilePath := filepath.Join(home, ".config", "oar", "profiles", "agent-a.json")
	storedProfile, ok, err := profile.Load(profilePath)
	if err != nil {
		t.Fatalf("load profile after register: %v", err)
	}
	if !ok {
		t.Fatal("expected profile file after register")
	}
	if storedProfile.AgentID == "" || storedProfile.KeyID == "" || storedProfile.AccessToken == "" || storedProfile.RefreshToken == "" {
		t.Fatalf("unexpected stored profile: %#v", storedProfile)
	}
	expectedKeyPath := filepath.Join(home, ".config", "oar", "keys", "agent-a.ed25519")
	if storedProfile.PrivateKeyPath != expectedKeyPath {
		t.Fatalf("unexpected private key path: got %s want %s", storedProfile.PrivateKeyPath, expectedKeyPath)
	}
	if _, err := os.Stat(expectedKeyPath); err != nil {
		t.Fatalf("expected private key file at %s: %v", expectedKeyPath, err)
	}

	whoamiOut := runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "--agent", "agent-a", "auth", "whoami"})
	whoamiPayload := assertEnvelopeOK(t, whoamiOut)
	serverObj, _ := whoamiPayload["data"].(map[string]any)
	if serverObj == nil {
		t.Fatalf("unexpected whoami payload: %#v", whoamiPayload)
	}

	updateOut := runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "--agent", "agent-a", "auth", "update-username", "--username", "renamed_agent"})
	assertEnvelopeOK(t, updateOut)
	storedProfile, ok, err = profile.Load(profilePath)
	if err != nil || !ok {
		t.Fatalf("reload profile after update: ok=%t err=%v", ok, err)
	}
	if storedProfile.Username != "renamed_agent" {
		t.Fatalf("expected updated username in profile, got %q", storedProfile.Username)
	}

	oldKeyID := storedProfile.KeyID
	rotateOut := runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "--agent", "agent-a", "auth", "rotate"})
	assertEnvelopeOK(t, rotateOut)
	storedProfile, ok, err = profile.Load(profilePath)
	if err != nil || !ok {
		t.Fatalf("reload profile after rotate: ok=%t err=%v", ok, err)
	}
	if storedProfile.KeyID == "" || storedProfile.KeyID == oldKeyID {
		t.Fatalf("expected rotated key id, old=%s new=%s", oldKeyID, storedProfile.KeyID)
	}

	tokenStatusOut := runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "--agent", "agent-a", "auth", "token-status"})
	tokenPayload := assertEnvelopeOK(t, tokenStatusOut)
	statusData, _ := tokenPayload["data"].(map[string]any)
	if statusData == nil || statusData["has_access_token"] != true {
		t.Fatalf("unexpected token status payload: %#v", tokenPayload)
	}

	protectedOut := runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "--agent", "agent-a", "api", "call", "--path", "/protected"})
	protectedPayload := assertEnvelopeOK(t, protectedOut)
	protectedData, _ := protectedPayload["data"].(map[string]any)
	if protectedData == nil || int(protectedData["status_code"].(float64)) != http.StatusOK {
		t.Fatalf("unexpected protected api payload: %#v", protectedPayload)
	}

	revokeOut := runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "--agent", "agent-a", "auth", "revoke"})
	assertEnvelopeOK(t, revokeOut)
	storedProfile, ok, err = profile.Load(profilePath)
	if err != nil || !ok {
		t.Fatalf("reload profile after revoke: ok=%t err=%v", ok, err)
	}
	if !storedProfile.Revoked {
		t.Fatalf("expected profile revoked flag after revoke: %#v", storedProfile)
	}

	whoamiAfterRevoke := runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "--agent", "agent-a", "auth", "whoami"})
	errorPayload := assertEnvelopeError(t, whoamiAfterRevoke)
	errorObj, _ := errorPayload["error"].(map[string]any)
	if errorObj == nil || errorObj["code"] != "agent_revoked" {
		t.Fatalf("unexpected whoami after revoke payload: %#v", whoamiAfterRevoke)
	}
}

func TestAuthWhoAmIAutoRefreshesExpiredAccessToken(t *testing.T) {
	t.Parallel()

	core := newFakeAuthCore(t)
	server := httptest.NewServer(http.HandlerFunc(core.handle))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}

	_ = runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "--agent", "agent-refresh", "auth", "register", "--username", "agent.refresh"})

	profilePath := filepath.Join(home, ".config", "oar", "profiles", "agent-refresh.json")
	storedProfile, ok, err := profile.Load(profilePath)
	if err != nil || !ok {
		t.Fatalf("load profile after register: ok=%t err=%v", ok, err)
	}
	storedProfile.AccessToken = "expired-access"
	storedProfile.AccessTokenExpiresAt = time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	if err := profile.Save(profilePath, storedProfile); err != nil {
		t.Fatalf("save expired profile: %v", err)
	}

	whoamiOut := runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "--agent", "agent-refresh", "auth", "whoami"})
	assertEnvelopeOK(t, whoamiOut)

	updatedProfile, ok, err := profile.Load(profilePath)
	if err != nil || !ok {
		t.Fatalf("reload profile after whoami refresh: ok=%t err=%v", ok, err)
	}
	if updatedProfile.AccessToken == "expired-access" {
		t.Fatalf("expected refreshed access token, profile=%#v", updatedProfile)
	}
	if core.refreshCallCount() < 1 {
		t.Fatalf("expected refresh endpoint to be called at least once")
	}
}

func runCLIForTest(t *testing.T, home string, env map[string]string, stdin io.Reader, args []string) string {
	t.Helper()
	if stdin == nil {
		stdin = strings.NewReader("")
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := New()
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = stdin
	cli.StdinIsTTY = func() bool { return stdin == nil }
	cli.UserHomeDir = func() (string, error) { return home, nil }
	cli.ReadFile = os.ReadFile
	cli.Getenv = func(key string) string { return env[key] }

	exitCode := cli.Run(args)
	if exitCode != 0 {
		if stdout.Len() == 0 {
			t.Fatalf("cli run failed: exit=%d stderr=%s", exitCode, stderr.String())
		}
	}
	return stdout.String()
}

func assertEnvelopeOK(t *testing.T, raw string) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode envelope json: %v raw=%s", err, raw)
	}
	if payload["ok"] != true {
		t.Fatalf("expected ok=true payload=%#v", payload)
	}
	return payload
}

func assertEnvelopeError(t *testing.T, raw string) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode envelope json: %v raw=%s", err, raw)
	}
	if payload["ok"] != false {
		t.Fatalf("expected ok=false payload=%#v", payload)
	}
	return payload
}

type fakeAuthCore struct {
	t *testing.T

	mu           sync.Mutex
	agentID      string
	actorID      string
	username     string
	keyID        string
	publicKeyB64 string
	accessToken  string
	refreshToken string
	revoked      bool
	counter      int
	refreshCalls int
}

func newFakeAuthCore(t *testing.T) *fakeAuthCore {
	return &fakeAuthCore{t: t}
}

func (f *fakeAuthCore) refreshCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refreshCalls
}

func (f *fakeAuthCore) issueTokensLocked() (access string, refresh string, expiresIn int64) {
	f.counter++
	f.accessToken = "access-" + fmt.Sprint(f.counter)
	f.refreshToken = "refresh-" + fmt.Sprint(f.counter)
	return f.accessToken, f.refreshToken, 300
}

func (f *fakeAuthCore) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/meta/handshake":
		_, _ = w.Write([]byte(`{"core_instance_id":"fake-core","min_cli_version":"0.1.0"}`))
		return
	case r.Method == http.MethodPost && r.URL.Path == "/auth/agents/register":
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.agentID = "agent-123"
		f.actorID = "agent-123"
		f.username = strings.ToLower(strings.TrimSpace(anyStr(req["username"])))
		f.keyID = "key-1"
		f.publicKeyB64 = strings.TrimSpace(anyStr(req["public_key"]))
		access, refresh, expiresIn := f.issueTokensLocked()
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"agent":  map[string]any{"agent_id": f.agentID, "actor_id": f.actorID, "username": f.username},
			"key":    map[string]any{"key_id": f.keyID, "public_key": f.publicKeyB64},
			"tokens": map[string]any{"access_token": access, "refresh_token": refresh, "token_type": "Bearer", "expires_in": expiresIn},
		})
		return
	case r.Method == http.MethodPost && r.URL.Path == "/auth/token":
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		grant := strings.TrimSpace(anyStr(req["grant_type"]))
		if f.revoked {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "agent_revoked", "message": "revoked"}})
			return
		}
		switch grant {
		case "refresh_token":
			f.refreshCalls++
			if strings.TrimSpace(anyStr(req["refresh_token"])) != f.refreshToken {
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "invalid_token", "message": "invalid refresh"}})
				return
			}
			access, refresh, expiresIn := f.issueTokensLocked()
			_ = json.NewEncoder(w).Encode(map[string]any{"tokens": map[string]any{"access_token": access, "refresh_token": refresh, "token_type": "Bearer", "expires_in": expiresIn}})
			return
		case "assertion":
			if strings.TrimSpace(anyStr(req["agent_id"])) != f.agentID || strings.TrimSpace(anyStr(req["key_id"])) != f.keyID {
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "key_mismatch", "message": "bad assertion"}})
				return
			}
			signedAt := strings.TrimSpace(anyStr(req["signed_at"]))
			signatureB64 := strings.TrimSpace(anyStr(req["signature"]))
			message := "oar-auth-token|" + f.agentID + "|" + f.keyID + "|" + signedAt
			publicKey, err := base64.StdEncoding.DecodeString(f.publicKeyB64)
			if err != nil || len(publicKey) != ed25519.PublicKeySize {
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "key_mismatch", "message": "bad key"}})
				return
			}
			signature, err := base64.StdEncoding.DecodeString(signatureB64)
			if err != nil || !ed25519.Verify(ed25519.PublicKey(publicKey), []byte(message), signature) {
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "key_mismatch", "message": "bad signature"}})
				return
			}
			access, refresh, expiresIn := f.issueTokensLocked()
			_ = json.NewEncoder(w).Encode(map[string]any{"tokens": map[string]any{"access_token": access, "refresh_token": refresh, "token_type": "Bearer", "expires_in": expiresIn}})
			return
		default:
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "invalid_request", "message": "bad grant"}})
			return
		}
	case r.Method == http.MethodGet && r.URL.Path == "/agents/me":
		if !f.requireAuth(w, r) {
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"agent": map[string]any{"agent_id": f.agentID, "username": f.username, "actor_id": f.actorID},
			"keys":  []map[string]any{{"key_id": f.keyID, "public_key": f.publicKeyB64}},
		})
		return
	case r.Method == http.MethodPatch && r.URL.Path == "/agents/me":
		if !f.requireAuth(w, r) {
			return
		}
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		username := strings.TrimSpace(anyStr(req["username"]))
		if username != "" {
			f.username = username
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"agent": map[string]any{"agent_id": f.agentID, "username": f.username, "actor_id": f.actorID}})
		return
	case r.Method == http.MethodPost && r.URL.Path == "/agents/me/keys/rotate":
		if !f.requireAuth(w, r) {
			return
		}
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.publicKeyB64 = strings.TrimSpace(anyStr(req["public_key"]))
		f.keyID = "key-" + fmt.Sprint(f.counter+1)
		_ = json.NewEncoder(w).Encode(map[string]any{"key": map[string]any{"key_id": f.keyID, "public_key": f.publicKeyB64}})
		return
	case r.Method == http.MethodPost && r.URL.Path == "/agents/me/revoke":
		if !f.requireAuth(w, r) {
			return
		}
		f.revoked = true
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		return
	case r.Method == http.MethodGet && r.URL.Path == "/protected":
		if !f.requireAuth(w, r) {
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		return
	default:
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "not_found", "message": "not found"}})
	}
}

func (f *fakeAuthCore) requireAuth(w http.ResponseWriter, r *http.Request) bool {
	if f.revoked {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "agent_revoked", "message": "revoked"}})
		return false
	}
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" || header != "Bearer "+f.accessToken {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "invalid_token", "message": "invalid"}})
		return false
	}
	return true
}

func anyStr(raw any) string {
	text, _ := raw.(string)
	return text
}
