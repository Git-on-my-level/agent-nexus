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
	if got := anyStr(serverObj["access_token"]); got != "" {
		t.Fatalf("expected empty access_token in whoami data, got %q", got)
	}
	if got := anyStr(serverObj["refresh_token"]); got != "" {
		t.Fatalf("expected empty refresh_token in whoami data, got %q", got)
	}
	if got := anyStr(serverObj["private_key_path"]); got != "" {
		t.Fatalf("expected empty private_key_path in whoami data, got %q", got)
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
	if protectedData == nil {
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

func TestAuthTextOutputIncludesWakeRoutingNextSteps(t *testing.T) {
	t.Parallel()

	core := newFakeAuthCore(t)
	server := httptest.NewServer(http.HandlerFunc(core.handle))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}

	registerOut := runCLIForTest(t, home, env, nil, []string{
		"--base-url", server.URL,
		"--agent", "agent-text",
		"auth", "register",
		"--username", "agent.text",
	})
	if !strings.Contains(registerOut, "Wake registration help: oar help bridge; oar meta doc agent-bridge; oar meta doc wake-routing (principal: @agent.text)") {
		t.Fatalf("expected wake registration hint in register output=%s", registerOut)
	}

	whoamiOut := runCLIForTest(t, home, env, nil, []string{
		"--base-url", server.URL,
		"--agent", "agent-text",
		"auth", "whoami",
	})
	if !strings.Contains(whoamiOut, "Wake registration help: oar help bridge; oar meta doc agent-bridge; oar meta doc wake-routing (principal: @agent.text)") {
		t.Fatalf("expected wake registration hint in whoami output=%s", whoamiOut)
	}
	if !strings.Contains(whoamiOut, "Server actor ID: agent-123") {
		t.Fatalf("expected server actor id in whoami output=%s", whoamiOut)
	}
}

func TestAuthRegisterSupportsExistingActorLinking(t *testing.T) {
	t.Parallel()

	core := newFakeAuthCore(t)
	server := httptest.NewServer(http.HandlerFunc(core.handle))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}

	registerOut := runCLIForTest(t, home, env, nil, []string{
		"--json",
		"--base-url", server.URL,
		"--agent", "agent-linked",
		"auth", "register",
		"--username", "linked.agent",
		"--existing-actor-id", "actor-boss-kid",
	})
	payload := assertEnvelopeOK(t, registerOut)
	data, _ := payload["data"].(map[string]any)
	if data == nil {
		t.Fatalf("unexpected auth register payload: %#v", payload)
	}
	profileMap, _ := data["profile"].(map[string]any)
	if got := strings.TrimSpace(anyStr(profileMap["actor_id"])); got != "actor-boss-kid" {
		t.Fatalf("expected linked actor id in profile, got %#v", payload)
	}

	core.mu.Lock()
	defer core.mu.Unlock()
	if got := strings.TrimSpace(anyStr(core.lastRegisterRequest["existing_actor_id"])); got != "actor-boss-kid" {
		t.Fatalf("expected existing_actor_id in register request, got %#v", core.lastRegisterRequest)
	}
}

func TestAuthWhoAmIHintUsesServerResolvedUsername(t *testing.T) {
	t.Parallel()

	core := newFakeAuthCore(t)
	server := httptest.NewServer(http.HandlerFunc(core.handle))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}

	_ = runCLIForTest(t, home, env, nil, []string{
		"--json", "--base-url", server.URL,
		"--agent", "agent-renamed",
		"auth", "register",
		"--username", "server.name",
	})

	profilePath := filepath.Join(home, ".config", "oar", "profiles", "agent-renamed.json")
	storedProfile, ok, err := profile.Load(profilePath)
	if err != nil || !ok {
		t.Fatalf("load profile after register: ok=%t err=%v", ok, err)
	}
	storedProfile.Username = "local-stale-name"
	jsonDisabled := false
	storedProfile.JSON = &jsonDisabled
	if err := profile.Save(profilePath, storedProfile); err != nil {
		t.Fatalf("save stale profile username: %v", err)
	}

	whoamiOut := runCLIForTest(t, home, env, nil, []string{
		"--base-url", server.URL,
		"--agent", "agent-renamed",
		"auth", "whoami",
	})
	if !strings.Contains(whoamiOut, "Wake registration help: oar help bridge; oar meta doc agent-bridge; oar meta doc wake-routing (principal: @server.name)") {
		t.Fatalf("expected server-resolved wake registration hint output=%s", whoamiOut)
	}
}

func TestAuthRegisterPersistsProfileDefaults(t *testing.T) {
	t.Parallel()

	core := newFakeAuthCore(t)
	server := httptest.NewServer(http.HandlerFunc(core.handle))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}

	_ = runCLIForTest(t, home, env, nil, []string{
		"--json",
		"--base-url", server.URL,
		"--agent", "agent-profile-defaults",
		"auth", "register",
		"--username", "agent.defaults",
	})

	profilePath := filepath.Join(home, ".config", "oar", "profiles", "agent-profile-defaults.json")
	storedProfile, ok, err := profile.Load(profilePath)
	if err != nil || !ok {
		t.Fatalf("load profile: ok=%t err=%v", ok, err)
	}
	if storedProfile.JSON == nil || !*storedProfile.JSON {
		t.Fatalf("expected profile json=true, profile=%#v", storedProfile)
	}
	if storedProfile.BaseURL != server.URL {
		t.Fatalf("expected profile base_url=%s, got %s", server.URL, storedProfile.BaseURL)
	}

	versionRaw := runCLIForTest(t, home, env, nil, []string{"--agent", "agent-profile-defaults", "version"})
	var payload map[string]any
	if err := json.Unmarshal([]byte(versionRaw), &payload); err != nil {
		t.Fatalf("expected JSON output from profile json default, err=%v raw=%s", err, versionRaw)
	}
	if payload["ok"] != true {
		t.Fatalf("unexpected version payload: %#v", payload)
	}
	data, _ := payload["data"].(map[string]any)
	if data == nil || strings.TrimSpace(anyStr(data["base_url"])) != server.URL {
		t.Fatalf("unexpected version payload data: %#v", payload)
	}
}

func TestAuthDefaultSelectsProfileForAdHocCommands(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := profile.Save(profile.ProfilePath(home, "alpha"), profile.Profile{Agent: "alpha", BaseURL: "http://alpha:8000"}); err != nil {
		t.Fatalf("save alpha profile: %v", err)
	}
	if err := profile.Save(profile.ProfilePath(home, "beta"), profile.Profile{Agent: "beta", BaseURL: "http://beta:8000"}); err != nil {
		t.Fatalf("save beta profile: %v", err)
	}

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "auth", "default", "beta"})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	if strings.TrimSpace(anyStr(data["default_profile"])) != "beta" {
		t.Fatalf("unexpected auth default payload: %#v", payload)
	}

	versionRaw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "version"})
	versionPayload := assertEnvelopeOK(t, versionRaw)
	versionData, _ := versionPayload["data"].(map[string]any)
	if strings.TrimSpace(anyStr(versionData["agent"])) != "beta" {
		t.Fatalf("unexpected version agent: %#v", versionPayload)
	}
	if strings.TrimSpace(anyStr(versionData["base_url"])) != "http://beta:8000" {
		t.Fatalf("unexpected version base url: %#v", versionPayload)
	}
}

func TestAuthListShowsDefaultProfile(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := profile.Save(profile.ProfilePath(home, "alpha"), profile.Profile{Agent: "alpha", BaseURL: "http://alpha:8000"}); err != nil {
		t.Fatalf("save alpha profile: %v", err)
	}
	if err := profile.Save(profile.ProfilePath(home, "beta"), profile.Profile{Agent: "beta", BaseURL: "http://beta:8000"}); err != nil {
		t.Fatalf("save beta profile: %v", err)
	}
	if err := profile.SaveDefaultAgent(home, "beta"); err != nil {
		t.Fatalf("save default profile: %v", err)
	}

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "--agent", "alpha", "auth", "list"})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	if strings.TrimSpace(anyStr(data["default_profile"])) != "beta" {
		t.Fatalf("unexpected auth list payload: %#v", payload)
	}
	profiles, _ := data["profiles"].([]any)
	if len(profiles) != 2 {
		t.Fatalf("expected two profiles, got %#v", payload)
	}
	var beta map[string]any
	for _, item := range profiles {
		profileMap, _ := item.(map[string]any)
		if strings.TrimSpace(anyStr(profileMap["agent"])) == "beta" {
			beta = profileMap
		}
	}
	if beta == nil {
		t.Fatalf("expected beta profile summary, got %#v", payload)
	}
	if isDefault, _ := beta["default"].(bool); !isDefault {
		t.Fatalf("expected beta to be marked default, got %#v", beta)
	}
}

func TestAuthRegisterInternalErrorIsActionable(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/auth/agents/register" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":    "internal_error",
				"message": "failed to register agent",
			},
		})
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"--agent", "agent-register-failure",
		"auth", "register",
		"--username", "agent.failure",
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || strings.TrimSpace(anyStr(errObj["code"])) != "auth_registration_unavailable" {
		t.Fatalf("unexpected auth register error payload: %#v", payload)
	}
	if message := strings.TrimSpace(anyStr(errObj["message"])); !strings.Contains(message, "temporarily unavailable") {
		t.Fatalf("expected actionable register error message, got %q payload=%#v", message, payload)
	}
	if hint := strings.TrimSpace(anyStr(errObj["hint"])); !strings.Contains(hint, "oar api call --path /readyz") {
		t.Fatalf("expected readiness hint, got %q payload=%#v", hint, payload)
	}
	if recoverable, _ := errObj["recoverable"].(bool); !recoverable {
		t.Fatalf("expected recoverable=true, payload=%#v", payload)
	}
}

func TestAuthInvitesCreateRequiresKind(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"auth", "invites", "create",
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || strings.TrimSpace(anyStr(errObj["code"])) != "invite_kind_required" {
		t.Fatalf("unexpected auth invites create error payload: %#v", payload)
	}
	if message := strings.TrimSpace(anyStr(errObj["message"])); !strings.Contains(message, "kind is required") {
		t.Fatalf("expected kind-required guidance, got %q payload=%#v", message, payload)
	}
}

func TestAuthInvitesListShowsConsumedInvites(t *testing.T) {
	t.Parallel()

	core := newFakeAuthCore(t)
	core.invites = []map[string]any{{
		"id":                  "invite-1",
		"kind":                "human",
		"note":                "operator",
		"created_at":          "2026-03-19T00:00:00Z",
		"created_by_agent_id": "agent-123",
		"created_by_actor_id": "actor-123",
		"consumed_at":         "2026-03-19T01:00:00Z",
	}}
	server := httptest.NewServer(http.HandlerFunc(core.handle))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}

	_ = runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "--agent", "agent-a", "auth", "register", "--username", "Agent.One"})
	profilePath := filepath.Join(home, ".config", "oar", "profiles", "agent-a.json")
	storedProfile, ok, err := profile.Load(profilePath)
	if err != nil || !ok {
		t.Fatalf("load profile after register: ok=%t err=%v", ok, err)
	}
	jsonOutput := false
	storedProfile.JSON = &jsonOutput
	if err := profile.Save(profilePath, storedProfile); err != nil {
		t.Fatalf("persist profile with text output default: %v", err)
	}

	listOut := runCLIForTest(t, home, env, nil, []string{"--base-url", server.URL, "--agent", "agent-a", "auth", "invites", "list"})
	if !strings.Contains(listOut, "status=consumed") {
		t.Fatalf("expected consumed invite status, got %q", listOut)
	}

	jsonOut := runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "--agent", "agent-a", "auth", "invites", "list"})
	payload := assertEnvelopeOK(t, jsonOut)
	data, _ := payload["data"].(map[string]any)
	if data == nil {
		t.Fatalf("unexpected auth invites list payload: %#v", payload)
	}
	invites, _ := data["invites"].([]any)
	if len(invites) != 1 {
		t.Fatalf("expected one invite in payload, got %#v", payload)
	}
	invite, _ := invites[0].(map[string]any)
	if invite == nil || strings.TrimSpace(anyStr(invite["consumed_at"])) == "" {
		t.Fatalf("expected consumed_at in JSON payload, got %#v", payload)
	}
}

func TestAuthPrincipalsAndAuditList(t *testing.T) {
	t.Parallel()

	core := newFakeAuthCore(t)
	core.principals = []map[string]any{{
		"agent_id":       "agent-999",
		"actor_id":       "actor-999",
		"username":       "revoked.agent",
		"principal_kind": "agent",
		"auth_method":    "public_key",
		"created_at":     "2026-03-19T00:00:00Z",
		"updated_at":     "2026-03-19T01:00:00Z",
		"revoked":        true,
		"revoked_at":     "2026-03-19T02:00:00Z",
	}}
	core.principalsNextCursor = "cursor-principals"
	core.auditEvents = []map[string]any{{
		"event_id":         "authevt-1",
		"event_type":       "invite_revoked",
		"occurred_at":      "2026-03-19T03:00:00Z",
		"actor_agent_id":   "agent-123",
		"subject_agent_id": "agent-999",
		"invite_id":        "invite-1",
		"metadata":         map[string]any{"kind": "agent"},
	}}
	core.auditNextCursor = "cursor-audit"

	server := httptest.NewServer(http.HandlerFunc(core.handle))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}

	_ = runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "--agent", "agent-a", "auth", "register", "--username", "Agent.One"})
	profilePath := filepath.Join(home, ".config", "oar", "profiles", "agent-a.json")
	storedProfile, ok, err := profile.Load(profilePath)
	if err != nil || !ok {
		t.Fatalf("load profile after register: ok=%t err=%v", ok, err)
	}
	jsonOutput := false
	storedProfile.JSON = &jsonOutput
	if err := profile.Save(profilePath, storedProfile); err != nil {
		t.Fatalf("persist profile with text output default: %v", err)
	}

	principalsOut := runCLIForTest(t, home, env, nil, []string{"--base-url", server.URL, "--agent", "agent-a", "auth", "principals", "list", "--limit", "1"})
	if !strings.Contains(principalsOut, "status=revoked") {
		t.Fatalf("expected revoked principal status in output, got %q", principalsOut)
	}

	principalsJSON := runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "--agent", "agent-a", "auth", "principals", "list", "--limit", "1"})
	principalsPayload := assertEnvelopeOK(t, principalsJSON)
	principalsData, _ := principalsPayload["data"].(map[string]any)
	if principalsData == nil || strings.TrimSpace(anyStr(principalsData["next_cursor"])) != "cursor-principals" {
		t.Fatalf("unexpected principals payload: %#v", principalsPayload)
	}

	auditOut := runCLIForTest(t, home, env, nil, []string{"--base-url", server.URL, "--agent", "agent-a", "auth", "audit", "list", "--limit", "1"})
	if !strings.Contains(auditOut, "invite_revoked") || !strings.Contains(auditOut, "invite=invite-1") {
		t.Fatalf("expected auth audit details in output, got %q", auditOut)
	}

	auditJSON := runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "--agent", "agent-a", "auth", "audit", "list", "--limit", "1"})
	auditPayload := assertEnvelopeOK(t, auditJSON)
	auditData, _ := auditPayload["data"].(map[string]any)
	if auditData == nil || strings.TrimSpace(anyStr(auditData["next_cursor"])) != "cursor-audit" {
		t.Fatalf("unexpected auth audit payload: %#v", auditPayload)
	}
}

func TestAuthPrincipalsRevoke(t *testing.T) {
	t.Parallel()

	core := newFakeAuthCore(t)
	core.principals = []map[string]any{{
		"agent_id":       "agent-999",
		"actor_id":       "actor-999",
		"username":       "member.agent",
		"principal_kind": "agent",
		"auth_method":    "public_key",
		"created_at":     "2026-03-19T00:00:00Z",
		"updated_at":     "2026-03-19T01:00:00Z",
		"revoked":        false,
	}}
	server := httptest.NewServer(http.HandlerFunc(core.handle))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}

	_ = runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "--agent", "agent-a", "auth", "register", "--username", "Agent.One"})

	raw := runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "--agent", "agent-a", "auth", "principals", "revoke", "--agent-id", "agent-999", "--allow-human-lockout", "--human-lockout-reason", "incident recovery"})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	if data == nil {
		t.Fatalf("unexpected auth principals revoke payload: %#v", payload)
	}
	revocation, _ := data["revocation"].(map[string]any)
	if revocation == nil || strings.TrimSpace(anyStr(revocation["mode"])) != "admin" {
		t.Fatalf("expected admin revocation payload, got %#v", payload)
	}
	if revocation["allow_human_lockout"] != true {
		t.Fatalf("expected allow_human_lockout=true payload, got %#v", payload)
	}
}

func TestAuthPrincipalsRevokeRejectsCurrentProfile(t *testing.T) {
	t.Parallel()

	core := newFakeAuthCore(t)
	server := httptest.NewServer(http.HandlerFunc(core.handle))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}

	_ = runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "--agent", "agent-a", "auth", "register", "--username", "Agent.One"})

	profilePath := filepath.Join(home, ".config", "oar", "profiles", "agent-a.json")
	storedProfile, ok, err := profile.Load(profilePath)
	if err != nil || !ok {
		t.Fatalf("load profile after register: ok=%t err=%v", ok, err)
	}

	raw := runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "--agent", "agent-a", "auth", "principals", "revoke", "--agent-id", storedProfile.AgentID})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil {
		t.Fatalf("expected error payload, got %#v", payload)
	}
	if got := strings.TrimSpace(anyStr(errObj["code"])); got != "invalid_request" {
		t.Fatalf("expected invalid_request code, got %#v", payload)
	}
	if msg := strings.TrimSpace(anyStr(errObj["message"])); !strings.Contains(msg, "oar auth revoke") {
		t.Fatalf("expected self-revoke guidance, got %#v", payload)
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

	mu                   sync.Mutex
	agentID              string
	actorID              string
	username             string
	lastRegisterRequest  map[string]any
	keyID                string
	publicKeyB64         string
	accessToken          string
	refreshToken         string
	revoked              bool
	invites              []map[string]any
	principals           []map[string]any
	principalsNextCursor string
	auditEvents          []map[string]any
	auditNextCursor      string
	counter              int
	refreshCalls         int
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
		f.lastRegisterRequest = req
		f.agentID = "agent-123"
		f.actorID = strings.TrimSpace(anyStr(req["existing_actor_id"]))
		if f.actorID == "" {
			f.actorID = "agent-123"
		}
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
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.revoked = true
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"principal": map[string]any{
				"agent_id":       f.agentID,
				"actor_id":       f.actorID,
				"username":       f.username,
				"principal_kind": "agent",
				"auth_method":    "public_key",
				"created_at":     "2026-03-19T00:00:00Z",
				"updated_at":     "2026-03-19T01:00:00Z",
				"revoked":        true,
				"revoked_at":     "2026-03-19T02:00:00Z",
			},
			"revocation": map[string]any{
				"mode":                "self",
				"already_revoked":     false,
				"allow_human_lockout": req["allow_human_lockout"] == true,
			},
		})
		return
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/auth/principals/") && strings.HasSuffix(r.URL.Path, "/revoke"):
		if !f.requireAuth(w, r) {
			return
		}
		agentID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/auth/principals/"), "/revoke")
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		principal := map[string]any{
			"agent_id":       agentID,
			"actor_id":       "actor-" + agentID,
			"username":       "principal." + agentID,
			"principal_kind": "agent",
			"auth_method":    "public_key",
			"created_at":     "2026-03-19T00:00:00Z",
			"updated_at":     "2026-03-19T01:00:00Z",
			"revoked":        true,
			"revoked_at":     "2026-03-19T02:00:00Z",
		}
		if len(f.principals) > 0 {
			for _, candidate := range f.principals {
				if strings.TrimSpace(anyStr(candidate["agent_id"])) == agentID {
					principal = candidate
					principal["revoked"] = true
					principal["revoked_at"] = "2026-03-19T02:00:00Z"
				}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":        true,
			"principal": principal,
			"revocation": map[string]any{
				"mode":                "admin",
				"already_revoked":     false,
				"allow_human_lockout": req["allow_human_lockout"] == true,
			},
		})
		return
	case r.Method == http.MethodGet && r.URL.Path == "/auth/invites":
		if !f.requireAuth(w, r) {
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"invites": f.invites})
		return
	case r.Method == http.MethodGet && r.URL.Path == "/auth/principals":
		if !f.requireAuth(w, r) {
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"principals": f.principals, "next_cursor": f.principalsNextCursor})
		return
	case r.Method == http.MethodGet && r.URL.Path == "/auth/audit":
		if !f.requireAuth(w, r) {
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"events": f.auditEvents, "next_cursor": f.auditNextCursor})
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
