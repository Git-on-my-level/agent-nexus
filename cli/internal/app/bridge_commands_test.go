package app

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"organization-autorunner-cli/internal/config"
	"organization-autorunner-cli/internal/profile"
)

func TestBridgeHelpTopic(t *testing.T) {
	output := runHelpCommand(t, "help", "bridge")
	if !strings.Contains(output, "bridge install") || !strings.Contains(output, "bridge doctor") || !strings.Contains(output, "bridge start") || !strings.Contains(output, "bridge status") {
		t.Fatalf("expected bridge subcommands in help output=%s", output)
	}
	if !strings.Contains(output, "Bridge-managed registrations stay `pending`") {
		t.Fatalf("expected readiness lifecycle guidance output=%s", output)
	}
}

func TestRenderBridgeHermesTemplateUsesPendingLifecycle(t *testing.T) {
	rendered, handle, err := renderBridgeConfigTemplate(bridgeTemplateParams{
		Kind:          "hermes",
		BaseURL:       "https://oar.example",
		WorkspaceID:   "ws_main",
		WorkspaceName: "Main",
		Handle:        "hermes",
	})
	if err != nil {
		t.Fatalf("renderBridgeConfigTemplate: %v", err)
	}
	if handle != "hermes" {
		t.Fatalf("expected handle hermes, got %q", handle)
	}
	if !strings.Contains(rendered, `status = "pending"`) || !strings.Contains(rendered, "checkin_ttl_seconds = 300") {
		t.Fatalf("expected pending lifecycle fields output=%s", rendered)
	}
	if !strings.Contains(rendered, `workspace_bindings = ["ws_main"]`) {
		t.Fatalf("expected workspace binding output=%s", rendered)
	}
}

func TestBridgeInstallPackageSpecDefaultsToRepoSubdirectory(t *testing.T) {
	spec := bridgeInstallPackageSpec("v0.0.6")
	if !strings.Contains(spec, "organization-autorunner.git@v0.0.6#subdirectory=adapters/agent-bridge") {
		t.Fatalf("unexpected bridge install spec=%s", spec)
	}
}

func TestLoadBridgeManagedConfigDetectsAgentConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.toml")
	if err := os.WriteFile(configPath, []byte("[agent]\nhandle = \"hermes\"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := loadBridgeManagedConfig(configPath)
	if err != nil {
		t.Fatalf("loadBridgeManagedConfig: %v", err)
	}
	if cfg.RuntimeKind != "agent" || cfg.RunCommand != "bridge" {
		t.Fatalf("unexpected managed config: %#v", cfg)
	}
	if !strings.Contains(cfg.ManagerDir, ".oar-bridge") || !strings.HasSuffix(cfg.ProcessStatePath, "process.json") {
		t.Fatalf("unexpected manager paths: %#v", cfg)
	}
}

func TestLoadBridgeManagedConfigDetectsAgentConfigWithHeaderComment(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.toml")
	if err := os.WriteFile(configPath, []byte("[agent] # prod\nhandle = \"hermes\"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := loadBridgeManagedConfig(configPath)
	if err != nil {
		t.Fatalf("loadBridgeManagedConfig: %v", err)
	}
	if cfg.RuntimeKind != "agent" || cfg.RunCommand != "bridge" {
		t.Fatalf("unexpected managed config: %#v", cfg)
	}
}

func TestBridgeStartPersistsManagedRuntimeState(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "agent.toml")
	if err := os.WriteFile(configPath, []byte("[agent]\nhandle = \"hermes\"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin dir: %v", err)
	}
	binaryPath := filepath.Join(binDir, "oar-agent-bridge")
	if err := os.WriteFile(binaryPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write bridge binary: %v", err)
	}

	originalStart := bridgeStartManagedProcess
	t.Cleanup(func() { bridgeStartManagedProcess = originalStart })
	bridgeStartManagedProcess = func(managedConfig bridgeManagedConfig, bridgeBinary string) (bridgeManagedRuntime, error) {
		return bridgeManagedRuntime{
			Kind:             managedConfig.RuntimeKind,
			ConfigPath:       managedConfig.ConfigPath,
			ManagerDir:       managedConfig.ManagerDir,
			ProcessStatePath: managedConfig.ProcessStatePath,
			LogPath:          managedConfig.LogPath,
			BridgeBinary:     bridgeBinary,
			Command:          []string{bridgeBinary, managedConfig.RunCommand, "run", "--config", managedConfig.ConfigPath},
			PID:              4242,
			PGID:             4242,
			StartedAt:        "2026-03-29T00:00:00Z",
		}, nil
	}

	app := New()
	app.UserHomeDir = func() (string, error) { return home, nil }
	result, err := app.runBridgeStart(context.Background(), []string{"--config", configPath})
	if err != nil {
		t.Fatalf("runBridgeStart: %v", err)
	}
	if !strings.Contains(result.Text, "PID: 4242") {
		t.Fatalf("expected pid in output=%s", result.Text)
	}
	state, ok := loadManagedRuntimeState(bridgeManagerDir(configPath) + "/process.json")
	if !ok {
		t.Fatalf("expected process state to be written")
	}
	if state.PID != 4242 || state.Kind != "agent" {
		t.Fatalf("unexpected persisted state: %#v", state)
	}
}

func TestBridgeStatusReportsNotManaged(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "router.toml")
	if err := os.WriteFile(configPath, []byte("[router]\nstate_path = \".state/router.json\"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	app := New()
	app.UserHomeDir = func() (string, error) { return home, nil }
	result, err := app.runBridgeStatus(context.Background(), []string{"--config", configPath})
	if err != nil {
		t.Fatalf("runBridgeStatus: %v", err)
	}
	if !strings.Contains(result.Text, "Process: not managed") {
		t.Fatalf("expected not managed output=%s", result.Text)
	}
}

func TestBridgeStatusReportsRouterHealthFromStateFile(t *testing.T) {
	originalAlive := bridgeProcessAlive
	originalCmdline := bridgeProcessCommandLine
	t.Cleanup(func() {
		bridgeProcessAlive = originalAlive
		bridgeProcessCommandLine = originalCmdline
	})
	bridgeProcessAlive = func(pid int) bool { return pid == 4242 }
	bridgeProcessCommandLine = func(pid int) (string, error) {
		return "/usr/bin/python oar-agent-bridge router run --config /tmp/router.toml", nil
	}

	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "router.toml")
	if err := os.WriteFile(configPath, []byte("[router]\nstate_path = \".state/router-state.json\"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(configDir, ".state"), 0o755); err != nil {
		t.Fatalf("mkdir router state dir: %v", err)
	}
	routerStatePath := filepath.Join(configDir, ".state", "router-state.json")
	if err := os.WriteFile(routerStatePath, []byte(`{
  "last_event_id": "evt-9",
  "router_last_tagged_message_event_id": "evt-10",
  "router_last_tagged_message_seen_at": "2026-03-29T07:16:28Z",
  "router_last_tagged_handles": ["m4-hermes"],
  "router_last_tagged_message_preview": "@m4-hermes can you respond to this?",
  "router_last_routed_event_id": "evt-10",
  "router_last_routed_at": "2026-03-29T07:16:29Z",
  "router_last_routed_handles": ["m4-hermes"],
  "router_last_stream_error_at": "2026-03-29T07:15:00Z",
  "router_last_stream_error": "RemoteProtocolError: incomplete chunked read"
}`), 0o600); err != nil {
		t.Fatalf("write router state: %v", err)
	}

	runtimeState := bridgeManagedRuntime{
		Kind:             "router",
		ConfigPath:       configPath,
		ManagerDir:       bridgeManagerDir(configPath),
		ProcessStatePath: filepath.Join(bridgeManagerDir(configPath), "process.json"),
		LogPath:          filepath.Join(bridgeManagerDir(configPath), "current.log"),
		PID:              4242,
		StartedAt:        "2026-03-29T07:15:00Z",
	}
	if err := writeManagedRuntimeState(runtimeState); err != nil {
		t.Fatalf("write runtime state: %v", err)
	}

	app := New()
	result, err := app.runBridgeStatus(context.Background(), []string{"--config", configPath})
	if err != nil {
		t.Fatalf("runBridgeStatus: %v", err)
	}
	if !strings.Contains(result.Text, "Cursor: evt-9") {
		t.Fatalf("expected cursor in output=%s", result.Text)
	}
	if !strings.Contains(result.Text, "Last routed mention: evt-10 at 2026-03-29T07:16:29Z for @m4-hermes") {
		t.Fatalf("expected routed mention in output=%s", result.Text)
	}
	if !strings.Contains(result.Text, "Last stream error: 2026-03-29T07:15:00Z") {
		t.Fatalf("expected stream error timestamp in output=%s", result.Text)
	}
	if !strings.Contains(result.Text, "Error detail: RemoteProtocolError: incomplete chunked read") {
		t.Fatalf("expected stream error detail in output=%s", result.Text)
	}
}

func TestBridgeStatusUsesDefaultRouterStatePathWhenConfigOmitsIt(t *testing.T) {
	originalAlive := bridgeProcessAlive
	originalCmdline := bridgeProcessCommandLine
	t.Cleanup(func() {
		bridgeProcessAlive = originalAlive
		bridgeProcessCommandLine = originalCmdline
	})
	bridgeProcessAlive = func(pid int) bool { return pid == 4242 }
	bridgeProcessCommandLine = func(pid int) (string, error) {
		return "/usr/bin/python oar-agent-bridge router run --config /tmp/router.toml", nil
	}

	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "router.toml")
	if err := os.WriteFile(configPath, []byte("[router]\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(configDir, ".state"), 0o755); err != nil {
		t.Fatalf("mkdir router state dir: %v", err)
	}
	routerStatePath := filepath.Join(configDir, ".state", "router.json")
	if err := os.WriteFile(routerStatePath, []byte(`{"last_event_id":"evt-42"}`), 0o600); err != nil {
		t.Fatalf("write router state: %v", err)
	}

	runtimeState := bridgeManagedRuntime{
		Kind:             "router",
		ConfigPath:       configPath,
		ManagerDir:       bridgeManagerDir(configPath),
		ProcessStatePath: filepath.Join(bridgeManagerDir(configPath), "process.json"),
		LogPath:          filepath.Join(bridgeManagerDir(configPath), "current.log"),
		PID:              4242,
		StartedAt:        "2026-03-29T07:15:00Z",
	}
	if err := writeManagedRuntimeState(runtimeState); err != nil {
		t.Fatalf("write runtime state: %v", err)
	}

	app := New()
	result, err := app.runBridgeStatus(context.Background(), []string{"--config", configPath})
	if err != nil {
		t.Fatalf("runBridgeStatus: %v", err)
	}
	if !strings.Contains(result.Text, "Cursor: evt-42") {
		t.Fatalf("expected default router state path to be used output=%s", result.Text)
	}
}

func TestBridgeManagedRuntimeRunningRejectsPIDReuse(t *testing.T) {
	originalAlive := bridgeProcessAlive
	originalCmdline := bridgeProcessCommandLine
	t.Cleanup(func() {
		bridgeProcessAlive = originalAlive
		bridgeProcessCommandLine = originalCmdline
	})
	bridgeProcessAlive = func(pid int) bool { return pid == 4242 }
	bridgeProcessCommandLine = func(pid int) (string, error) {
		return "/usr/bin/python unrelated-process --config /tmp/elsewhere.toml", nil
	}
	running, reason := bridgeManagedRuntimeRunning(bridgeManagedRuntime{
		Kind:       "agent",
		ConfigPath: "/tmp/agent.toml",
		PID:        4242,
	})
	if running || reason != "pid_reused" {
		t.Fatalf("expected pid_reused, got running=%v reason=%q", running, reason)
	}
}

func TestBridgeImportAuthCopiesExistingProfileIntoBridgeState(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "agent.toml")
	if err := os.WriteFile(configPath, []byte("[auth]\nstate_path = \".state/bridge-auth.json\"\n\n[agent]\nhandle = \"hermes\"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	keyPath := filepath.Join(home, ".config", "oar", "keys", "agent-a.ed25519")
	if err := profile.SavePrivateKey(keyPath, privateKey); err != nil {
		t.Fatalf("save private key: %v", err)
	}
	profilePath := filepath.Join(home, ".config", "oar", "profiles", "agent-a.json")
	if err := profile.Save(profilePath, profile.Profile{
		Agent:                "agent-a",
		BaseURL:              "https://oar.example",
		Username:             "hermes",
		AgentID:              "agent_123",
		ActorID:              "actor_123",
		KeyID:                "key_123",
		PrivateKeyPath:       keyPath,
		AccessToken:          "access-token",
		RefreshToken:         "refresh-token",
		TokenType:            "Bearer",
		AccessTokenExpiresAt: "2099-01-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("save profile: %v", err)
	}

	app := New()
	app.UserHomeDir = func() (string, error) { return home, nil }
	result, err := app.runBridgeImportAuth([]string{"--config", configPath, "--from-profile", "agent-a"}, config.Resolved{Agent: "agent-a", ProfilePath: profilePath})
	if err != nil {
		t.Fatalf("runBridgeImportAuth: %v", err)
	}
	if !strings.Contains(result.Text, "Bridge auth imported.") {
		t.Fatalf("unexpected output: %s", result.Text)
	}

	statePath := filepath.Join(filepath.Dir(configPath), ".state", "bridge-auth.json")
	content, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read bridge auth state: %v", err)
	}
	var state map[string]any
	if err := json.Unmarshal(content, &state); err != nil {
		t.Fatalf("decode bridge auth state: %v", err)
	}
	if got := anyString(state["username"]); got != "hermes" {
		t.Fatalf("unexpected username: %#v", state)
	}
	if got := anyString(state["public_key_b64"]); got == "" {
		t.Fatalf("expected public key in state: %#v", state)
	}
	if got := anyString(state["private_key_b64"]); got == "" {
		t.Fatalf("expected private key in state: %#v", state)
	}
	if got := anyString(state["access_token"]); got != "access-token" {
		t.Fatalf("expected imported access token, got %#v", state)
	}
	if got := anyString(state["agent_id"]); got != "agent_123" {
		t.Fatalf("expected imported agent id, got %#v", state)
	}
	if got := anyString(state["key_id"]); got != "key_123" {
		t.Fatalf("expected imported key id, got %#v", state)
	}
	if got := anyString(state["public_key_b64"]); got != base64.StdEncoding.EncodeToString(publicKey) {
		t.Fatalf("unexpected public key material: %#v", state)
	}
	privateSeed, err := base64.StdEncoding.DecodeString(anyString(state["private_key_b64"]))
	if err != nil {
		t.Fatalf("decode private key seed: %v", err)
	}
	if len(privateSeed) != ed25519.SeedSize {
		t.Fatalf("expected %d-byte private key seed, got %d", ed25519.SeedSize, len(privateSeed))
	}
	if got := base64.StdEncoding.EncodeToString(privateSeed); got != base64.StdEncoding.EncodeToString(privateKey.Seed()) {
		t.Fatalf("unexpected private key seed material")
	}
}

func TestBridgeWorkspaceIDReadsRegistrationBindings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer access-token" {
			t.Fatalf("expected auth header, got %q", got)
		}
		if r.Method != http.MethodGet || r.URL.Path != "/docs/agentreg.hermes" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"document":{"id":"agentreg.hermes","status":"active"},"revision":{"content":{"handle":"hermes","actor_id":"actor_123","status":"active","workspace_bindings":[{"workspace_id":"ws_main","enabled":true},{"workspace_id":"ws_backup","enabled":true},{"workspace_id":"ws_disabled","enabled":false}]}}}`))
	}))
	defer server.Close()

	home := t.TempDir()
	profilesDir := filepath.Join(home, ".config", "oar", "profiles")
	if err := os.MkdirAll(profilesDir, 0o700); err != nil {
		t.Fatalf("mkdir profiles dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "agent-a.json"), []byte(`{"base_url":"`+server.URL+`","access_token":"access-token","access_token_expires_at":"2099-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "--agent", "agent-a", "bridge", "workspace-id", "--handle", "hermes"})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	if data == nil {
		t.Fatalf("expected data payload: %#v", payload)
	}
	if got := anyString(data["document_id"]); got != "agentreg.hermes" {
		t.Fatalf("unexpected document id: %#v", data)
	}
	workspaceIDs, _ := data["workspace_ids"].([]any)
	if len(workspaceIDs) != 2 || anyString(workspaceIDs[0]) != "ws_main" || anyString(workspaceIDs[1]) != "ws_backup" {
		t.Fatalf("unexpected workspace ids: %#v", data)
	}
}

func TestDefaultBridgeCommandRunKeepsStderrOutOfStdout(t *testing.T) {
	scriptPath := filepath.Join(t.TempDir(), "bridge.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nprintf '{\"wakeable\":true}\\n'\nprintf 'log noise\\n' >&2\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	stdout, stderr, err := defaultBridgeCommandRun(context.Background(), scriptPath)
	if err != nil {
		t.Fatalf("defaultBridgeCommandRun: %v", err)
	}
	if strings.TrimSpace(stdout) != `{"wakeable":true}` {
		t.Fatalf("expected stdout json only, got %q", stdout)
	}
	if strings.TrimSpace(stderr) != "log noise" {
		t.Fatalf("expected stderr to contain log output, got %q", stderr)
	}
}

func TestLoadBridgeConfigDetailsExpandsAuthStatePath(t *testing.T) {
	home := t.TempDir()
	originalHomeDir := bridgeUserHomeDir
	t.Cleanup(func() { bridgeUserHomeDir = originalHomeDir })
	bridgeUserHomeDir = func() (string, error) { return home, nil }
	t.Setenv("BRIDGE_AUTH_SUBDIR", "custom-auth")

	configPath := filepath.Join(t.TempDir(), "agent.toml")
	if err := os.WriteFile(configPath, []byte("[auth]\nstate_path = \"~/$BRIDGE_AUTH_SUBDIR/bridge-auth.json\"\n\n[agent]\nhandle = \"hermes\"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	details, err := loadBridgeConfigDetails(configPath)
	if err != nil {
		t.Fatalf("loadBridgeConfigDetails: %v", err)
	}
	want := filepath.Join(home, "custom-auth", "bridge-auth.json")
	if details.AuthStatePath != want {
		t.Fatalf("unexpected auth state path: got %q want %q", details.AuthStatePath, want)
	}
}
