package app

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-nexus-cli/internal/buildinfo"
	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/profile"
)

func TestBridgeHelpTopic(t *testing.T) {
	output := runHelpCommand(t, "help", "bridge")
	if !strings.Contains(output, "bridge install") || !strings.Contains(output, "bridge doctor") || !strings.Contains(output, "bridge start") || !strings.Contains(output, "bridge status") {
		t.Fatalf("expected bridge subcommands in help output=%s", output)
	}
	if !strings.Contains(output, "Offline agents still accumulate durable wake notifications") {
		t.Fatalf("expected readiness lifecycle guidance output=%s", output)
	}
	if strings.Contains(output, "router.toml") {
		t.Fatalf("expected router bootstrap guidance to be removed output=%s", output)
	}
}

func TestRenderBridgeSubprocessTemplateUsesPendingLifecycle(t *testing.T) {
	rendered, handle, err := renderBridgeConfigTemplate(bridgeTemplateParams{
		Kind:              "subprocess",
		BaseURL:           "https://anx.example",
		WorkspaceIDs:      []string{"ws_main"},
		WorkspaceName:     "Main",
		Handle:            "myagent",
		AdapterEntrypoint: "./adapter.py",
	})
	if err != nil {
		t.Fatalf("renderBridgeConfigTemplate: %v", err)
	}
	if handle != "myagent" {
		t.Fatalf("expected handle myagent, got %q", handle)
	}
	if !strings.Contains(rendered, `status = "pending"`) || !strings.Contains(rendered, "checkin_ttl_seconds = 300") {
		t.Fatalf("expected pending lifecycle fields output=%s", rendered)
	}
	if !strings.Contains(rendered, `agent_home = ".anx"`) {
		t.Fatalf("expected agent home output=%s", rendered)
	}
	if !strings.Contains(rendered, `adapter_kind = "subprocess"`) || !strings.Contains(rendered, `kind = "subprocess"`) {
		t.Fatalf("expected subprocess adapter kind output=%s", rendered)
	}
	if !strings.Contains(rendered, `command = ["python3", "./adapter.py"]`) {
		t.Fatalf("expected subprocess command output=%s", rendered)
	}
}

func TestRenderBridgePythonPluginTemplate(t *testing.T) {
	rendered, handle, err := renderBridgeConfigTemplate(bridgeTemplateParams{
		Kind:          "python_plugin",
		BaseURL:       "https://anx.example",
		WorkspaceIDs:  []string{"ws_main"},
		WorkspaceName: "Main",
		Handle:        "myagent",
		PluginModule:  "my_bridge_adapter",
		PluginFactory: "build",
	})
	if err != nil {
		t.Fatalf("renderBridgeConfigTemplate: %v", err)
	}
	if handle != "myagent" {
		t.Fatalf("expected handle myagent, got %q", handle)
	}
	if !strings.Contains(rendered, `kind = "python_plugin"`) || !strings.Contains(rendered, `plugin_module = "my_bridge_adapter"`) {
		t.Fatalf("expected python_plugin adapter section output=%s", rendered)
	}
}

func TestRenderBridgeHermesTemplate(t *testing.T) {
	rendered, handle, err := renderBridgeConfigTemplate(bridgeTemplateParams{
		Kind:          "hermes",
		BaseURL:       "https://anx.example",
		WorkspaceIDs:  []string{"ws_main"},
		WorkspaceName: "Main",
		Handle:        "myagent",
	})
	if err != nil {
		t.Fatalf("renderBridgeConfigTemplate: %v", err)
	}
	if handle != "myagent" {
		t.Fatalf("expected handle myagent, got %q", handle)
	}
	if !strings.Contains(rendered, `driver_kind = "hermes"`) || !strings.Contains(rendered, `adapter_kind = "hermes"`) {
		t.Fatalf("expected hermes bridge metadata output=%s", rendered)
	}
	if !strings.Contains(rendered, `kind = "hermes"`) {
		t.Fatalf("expected hermes adapter kind output=%s", rendered)
	}
	if !strings.Contains(rendered, `command = ["python3", "-m", "anx_agent_bridge.adapters.hermes_acp"]`) {
		t.Fatalf("expected optional hermes module command hint output=%s", rendered)
	}
}

func TestBridgeInstallPackageSpecDefaultsToRepoSubdirectory(t *testing.T) {
	spec := bridgeInstallPackageSpec("v0.0.6")
	if !strings.Contains(spec, "agent-nexus.git@v0.0.6#subdirectory=adapters/agent-bridge") {
		t.Fatalf("unexpected bridge install spec=%s", spec)
	}
}

func TestDefaultBridgeInstallRefMatchesCLIVersion(t *testing.T) {
	t.Parallel()
	got := defaultBridgeInstallRef()
	if strings.TrimSpace(got) == "" {
		t.Fatal("defaultBridgeInstallRef returned empty string")
	}
	if got != buildinfo.Current {
		t.Fatalf("defaultBridgeInstallRef()=%q want buildinfo.Current=%q", got, buildinfo.Current)
	}
}

func TestLoadBridgeManagedConfigDetectsAgentConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "bridge.toml")
	if err := os.WriteFile(configPath, []byte("agent_home = \".anx\"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := loadBridgeManagedConfig(configPath)
	if err != nil {
		t.Fatalf("loadBridgeManagedConfig: %v", err)
	}
	if cfg.RuntimeKind != "agent" || cfg.RunCommand != "bridge" {
		t.Fatalf("unexpected managed config: %#v", cfg)
	}
	if !strings.Contains(cfg.ManagerDir, ".anx-bridge") || !strings.HasSuffix(cfg.ProcessStatePath, "process.json") {
		t.Fatalf("unexpected manager paths: %#v", cfg)
	}
}

func TestLoadBridgeManagedConfigDetectsAgentConfigWithHeaderComment(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "bridge.toml")
	if err := os.WriteFile(configPath, []byte("agent_home = \".anx\" # prod\n"), 0o600); err != nil {
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
	configPath := filepath.Join(t.TempDir(), "bridge.toml")
	if err := os.WriteFile(configPath, []byte("agent_home = \".anx\"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin dir: %v", err)
	}
	binaryPath := filepath.Join(binDir, "anx-agent-bridge")
	verLine := normalizedBridgeCLIExpectedPackageSemver(defaultBridgeInstallRef())
	if strings.TrimSpace(verLine) == "" {
		verLine = "0.7.1"
	}
	shim := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo \"anx-agent-bridge %s\"; exit 0; fi\nexit 0\n", verLine)
	if err := os.WriteFile(binaryPath, []byte(shim), 0o755); err != nil {
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
	configPath := filepath.Join(t.TempDir(), "bridge.toml")
	if err := os.WriteFile(configPath, []byte("agent_home = \".anx\"\n"), 0o600); err != nil {
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

func TestLoadBridgeManagedConfigRejectsNonAgentConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "router.toml")
	if err := os.WriteFile(configPath, []byte("[router]\nstate_path = \".state/router-state.json\"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := loadBridgeManagedConfig(configPath); err == nil {
		t.Fatal("expected non-agent config to be rejected")
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
	configDir := t.TempDir()
	configPath := writeBridgeAgentHomeFixture(t, configDir, "hermes")

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	keyPath := filepath.Join(home, ".config", "anx", "keys", "agent-a.ed25519")
	if err := profile.SavePrivateKey(keyPath, privateKey); err != nil {
		t.Fatalf("save private key: %v", err)
	}
	profilePath := filepath.Join(home, ".config", "anx", "profiles", "agent-a.json")
	if err := profile.Save(profilePath, profile.Profile{
		Agent:                "agent-a",
		BaseURL:              "https://anx.example",
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

	statePath := filepath.Join(configDir, ".anx", "profiles", "default.json")
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
		if r.Method != http.MethodGet || r.URL.Path != "/auth/principals" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"principals":[{"agent_id":"agent-hermes","actor_id":"actor_123","username":"hermes","principal_kind":"agent","auth_method":"public_key","created_at":"2026-03-29T00:00:00Z","last_seen_at":"2026-03-29T00:00:00Z","updated_at":"2026-03-29T00:00:00Z","revoked":false,"registration":{"handle":"hermes","actor_id":"actor_123","status":"active","workspace_bindings":[{"workspace_id":"ws_main","enabled":true},{"workspace_id":"ws_backup","enabled":true},{"workspace_id":"ws_disabled","enabled":false}]}}],"active_human_principal_count":0}`))
	}))
	defer server.Close()

	home := t.TempDir()
	profilesDir := filepath.Join(home, ".config", "anx", "profiles")
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
	if got := anyString(data["agent_id"]); got != "agent-hermes" {
		t.Fatalf("unexpected agent id: %#v", data)
	}
	workspaceIDs, _ := data["workspace_ids"].([]any)
	if len(workspaceIDs) != 2 || anyString(workspaceIDs[0]) != "ws_main" || anyString(workspaceIDs[1]) != "ws_backup" {
		t.Fatalf("unexpected workspace ids: %#v", data)
	}
}

func TestBridgeWorkspaceIDPaginatesUntilHandleMatches(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer access-token" {
			t.Fatalf("expected auth header, got %q", got)
		}
		if r.Method != http.MethodGet || r.URL.Path != "/auth/principals" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch strings.TrimSpace(r.URL.Query().Get("cursor")) {
		case "":
			_, _ = w.Write([]byte(`{"principals":[{"agent_id":"agent-other","actor_id":"actor_other","username":"other","principal_kind":"agent","auth_method":"public_key","created_at":"2026-03-29T00:00:00Z","last_seen_at":"2026-03-29T00:00:00Z","updated_at":"2026-03-29T00:00:00Z","revoked":false}],"next_cursor":"cursor-2","active_human_principal_count":0}`))
		case "cursor-2":
			_, _ = w.Write([]byte(`{"principals":[{"agent_id":"agent-hermes","actor_id":"actor_123","username":"hermes","principal_kind":"agent","auth_method":"public_key","created_at":"2026-03-29T00:00:00Z","last_seen_at":"2026-03-29T00:00:00Z","updated_at":"2026-03-29T00:00:00Z","revoked":false,"registration":{"handle":"hermes","actor_id":"actor_123","status":"active","workspace_bindings":[{"workspace_id":"ws_main","enabled":true}]}}],"active_human_principal_count":0}`))
		default:
			t.Fatalf("unexpected cursor %q", r.URL.Query().Get("cursor"))
		}
	}))
	defer server.Close()

	home := t.TempDir()
	profilesDir := filepath.Join(home, ".config", "anx", "profiles")
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
	if got := anyString(data["agent_id"]); got != "agent-hermes" {
		t.Fatalf("unexpected agent id after pagination: %#v", data)
	}
	workspaceIDs, _ := data["workspace_ids"].([]any)
	if len(workspaceIDs) != 1 || anyString(workspaceIDs[0]) != "ws_main" {
		t.Fatalf("unexpected workspace ids after pagination: %#v", data)
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

	configDir := t.TempDir()
	agentHome := filepath.Join(configDir, ".anx")
	if err := os.MkdirAll(agentHome, 0o700); err != nil {
		t.Fatalf("mkdir agent home: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentHome, "agent.toml"), []byte("[identity]\nbase_url = \"https://anx.example\"\nhandle = \"hermes\"\n\n[auth]\nstate_path = \"~/$BRIDGE_AUTH_SUBDIR/bridge-auth.json\"\n"), 0o600); err != nil {
		t.Fatalf("write agent manifest: %v", err)
	}
	configPath := filepath.Join(configDir, "bridge.toml")
	if err := os.WriteFile(configPath, []byte("agent_home = \".anx\"\n"), 0o600); err != nil {
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

func TestBridgeInitConfigSubprocessUsesAdapterEntrypoint(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "agent.toml")
	app := New()
	result, err := app.runBridgeInitConfig([]string{
		"--kind", "subprocess",
		"--output", outputPath,
		"--workspace-id", "ws_main",
		"--handle", "myagent",
		"--adapter-entrypoint", "/custom/adapter.py",
	}, config.Resolved{})
	if err != nil {
		t.Fatalf("runBridgeInitConfig: %v", err)
	}
	if !strings.Contains(result.Text, "adapter contract") {
		t.Fatalf("expected contract next step in output=%s", result.Text)
	}
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output config: %v", err)
	}
	if !strings.Contains(string(content), `command = ["python3", "/custom/adapter.py"]`) {
		t.Fatalf("expected adapter entrypoint in command, content=%s", content)
	}
}

func TestBridgeInitConfigHermesWritesBundledAdapter(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "agent.toml")
	app := New()
	result, err := app.runBridgeInitConfig([]string{
		"--kind", "hermes",
		"--output", outputPath,
		"--workspace-id", "ws_main",
		"--handle", "myagent",
	}, config.Resolved{})
	if err != nil {
		t.Fatalf("runBridgeInitConfig: %v", err)
	}
	if !strings.Contains(result.Text, "bridge doctor") {
		t.Fatalf("expected hermes doctor next step in output=%s", result.Text)
	}
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output config: %v", err)
	}
	body := string(content)
	if !strings.Contains(body, `adapter_kind = "hermes"`) || !strings.Contains(body, `kind = "hermes"`) {
		t.Fatalf("expected hermes adapter config, content=%s", body)
	}
}

func TestBridgeInitConfigPythonPluginRequiresPluginFlags(t *testing.T) {
	app := New()
	_, err := app.runBridgeInitConfig([]string{
		"--kind", "python-plugin",
		"--output", filepath.Join(t.TempDir(), "agent.toml"),
		"--workspace-id", "ws_main",
		"--handle", "myagent",
	}, config.Resolved{})
	if err == nil {
		t.Fatal("expected error when plugin-module missing")
	}
}

func TestBridgeInitConfigRequiresHandle(t *testing.T) {
	app := New()
	_, err := app.runBridgeInitConfig([]string{
		"--kind", "subprocess",
		"--output", filepath.Join(t.TempDir(), "agent.toml"),
		"--workspace-id", "ws_main",
	}, config.Resolved{})
	if err == nil {
		t.Fatal("expected error when --handle missing")
	}
}

func TestBridgeInitConfigPythonPluginWritesModule(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "agent.toml")
	app := New()
	result, err := app.runBridgeInitConfig([]string{
		"--kind", "python-plugin",
		"--output", outputPath,
		"--workspace-id", "ws_main",
		"--handle", "myagent",
		"--plugin-module", "my_mod",
		"--plugin-factory", "build",
	}, config.Resolved{})
	if err != nil {
		t.Fatalf("runBridgeInitConfig: %v", err)
	}
	if !strings.Contains(result.Text, "Bridge config written.") {
		t.Fatalf("expected success output=%s", result.Text)
	}
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output config: %v", err)
	}
	body := string(content)
	if !strings.Contains(body, `plugin_module = "my_mod"`) || !strings.Contains(body, `plugin_factory = "build"`) {
		t.Fatalf("expected plugin fields, content=%s", content)
	}
}

func writeBridgeAgentHomeFixture(t *testing.T, configDir string, handle string) string {
	t.Helper()
	agentHome := filepath.Join(configDir, ".anx")
	if err := os.MkdirAll(agentHome, 0o700); err != nil {
		t.Fatalf("mkdir agent home: %v", err)
	}
	manifest := `schema_version = 1

[identity]
base_url = "http://127.0.0.1:8000"
handle = "` + handle + `"
agent_id = ""
actor_id = ""
key_id = ""

[auth]
state_path = "profiles/default.json"
`
	if err := os.WriteFile(filepath.Join(agentHome, "agent.toml"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write agent.toml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentHome, "wake.toml"), []byte("schema_version = 1\n\n[[workspaces]]\nid = \"ws_main\"\nname = \"Main\"\nenabled = true\n"), 0o600); err != nil {
		t.Fatalf("write wake.toml: %v", err)
	}
	configPath := filepath.Join(configDir, "bridge.toml")
	if err := os.WriteFile(configPath, []byte("agent_home = \".anx\"\nwake_config = \"wake.toml\"\n\n[adapter]\nkind = \"subprocess\"\ncommand = [\"python3\", \"./adapter.py\"]\n"), 0o600); err != nil {
		t.Fatalf("write bridge config: %v", err)
	}
	return configPath
}

func saveBridgeImportProfile(t *testing.T, home string, agentName string, privateKey ed25519.PrivateKey) string {
	t.Helper()
	keyPath := filepath.Join(home, ".config", "anx", "keys", agentName+".ed25519")
	if err := profile.SavePrivateKey(keyPath, privateKey); err != nil {
		t.Fatalf("save private key: %v", err)
	}
	profilePath := filepath.Join(home, ".config", "anx", "profiles", agentName+".json")
	if err := profile.Save(profilePath, profile.Profile{
		Agent:                agentName,
		BaseURL:              "http://127.0.0.1:8002",
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
	return profilePath
}

func TestBridgeImportAuthUpdatesAgentHomeIdentityFromProfile(t *testing.T) {
	home := t.TempDir()
	configDir := t.TempDir()
	configPath := writeBridgeAgentHomeFixture(t, configDir, "hermes")
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	profilePath := saveBridgeImportProfile(t, home, "agent-a", privateKey)

	app := New()
	app.UserHomeDir = func() (string, error) { return home, nil }
	result, err := app.runBridgeImportAuth([]string{"--config", configPath, "--from-profile", "agent-a"}, config.Resolved{Agent: "agent-a", ProfilePath: profilePath})
	if err != nil {
		t.Fatalf("runBridgeImportAuth: %v", err)
	}
	if !strings.Contains(result.Text, "Agent manifest identity updated") {
		t.Fatalf("expected agent manifest update message, output=%s", result.Text)
	}

	updated, err := os.ReadFile(filepath.Join(configDir, ".anx", "agent.toml"))
	if err != nil {
		t.Fatalf("read updated manifest: %v", err)
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	expectedFingerprint := bridgePublicKeyFingerprint(publicKey)
	if !strings.Contains(string(updated), `agent_id = "agent_123"`) ||
		!strings.Contains(string(updated), `actor_id = "actor_123"`) ||
		!strings.Contains(string(updated), `public_key_fingerprint = "`+expectedFingerprint+`"`) {
		t.Fatalf("expected updated identity in manifest, content=%s", updated)
	}
	wakeConfig, err := os.ReadFile(filepath.Join(configDir, ".anx", "wake.toml"))
	if err != nil {
		t.Fatalf("read wake config: %v", err)
	}
	if !strings.Contains(string(wakeConfig), `base_url = "http://127.0.0.1:8002"`) {
		t.Fatalf("expected imported profile base_url in wake config, content=%s", wakeConfig)
	}
	authState, err := os.ReadFile(filepath.Join(configDir, ".anx", "profiles", "default.json"))
	if err != nil {
		t.Fatalf("read auth state: %v", err)
	}
	if !strings.Contains(string(authState), `"agent_id": "agent_123"`) {
		t.Fatalf("expected bridge auth state written, content=%s", authState)
	}
}

func TestBridgeImportAuthRejectsHandleMismatch(t *testing.T) {
	home := t.TempDir()
	configDir := t.TempDir()
	configPath := writeBridgeAgentHomeFixture(t, configDir, "other")
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	profilePath := saveBridgeImportProfile(t, home, "agent-b", privateKey)
	app := New()
	app.UserHomeDir = func() (string, error) { return home, nil }
	_, err = app.runBridgeImportAuth([]string{"--config", configPath, "--from-profile", "agent-b"}, config.Resolved{Agent: "agent-b", ProfilePath: profilePath})
	if err == nil {
		t.Fatal("expected handle mismatch error")
	}
}

func TestBridgeReplaceConfigValue(t *testing.T) {
	input := `[anx]
base_url = "http://127.0.0.1:8000"
workspace_id = "ws_main"

[adapter]
cwd_default = "/path"
`
	result, changed := bridgeReplaceConfigValue(input, "anx", "base_url", "http://127.0.0.1:8002")
	if !changed {
		t.Fatal("expected replacement to report a change")
	}
	if !strings.Contains(result, `base_url = "http://127.0.0.1:8002"`) {
		t.Fatalf("expected updated base_url, got=%s", result)
	}
	if !strings.Contains(result, `workspace_id = "ws_main"`) {
		t.Fatalf("expected other fields preserved, got=%s", result)
	}
	if !strings.Contains(result, `cwd_default = "/path"`) {
		t.Fatalf("expected adapter section preserved, got=%s", result)
	}
}

func TestBridgeReplaceConfigValueInsertsMissingKeyInSection(t *testing.T) {
	input := `[anx]
workspace_id = "ws_main"
workspace_name = "Main"

[adapter]
cwd_default = "/path"
`
	result, changed := bridgeReplaceConfigValue(input, "anx", "base_url", "http://127.0.0.1:8002")
	if !changed {
		t.Fatal("expected insert to report a change")
	}
	if !strings.Contains(result, "[anx]\nworkspace_id = \"ws_main\"\nworkspace_name = \"Main\"") ||
		!strings.Contains(result, "\nbase_url = \"http://127.0.0.1:8002\"\n[adapter]") &&
			!strings.Contains(result, "\n\nbase_url = \"http://127.0.0.1:8002\"\n[adapter]") {
		t.Fatalf("expected missing key inserted before next section, got=%s", result)
	}
}

func TestBridgeReplaceConfigValueReplacesEmptyString(t *testing.T) {
	input := `[identity]
base_url = "https://anx.example"
agent_id = ""
actor_id = ""

[auth]
state_path = "profiles/default.json"
`
	result, changed := bridgeReplaceConfigValue(input, "identity", "agent_id", "agent_123")
	if !changed {
		t.Fatal("expected empty value replacement to report a change")
	}
	if !strings.Contains(result, `agent_id = "agent_123"`) {
		t.Fatalf("expected empty agent_id to be replaced, got=%s", result)
	}
	if strings.Count(result, "agent_id") != 1 {
		t.Fatalf("expected one agent_id assignment, got=%s", result)
	}
}

func TestBridgeReplaceConfigValueDoesNotGrowTrailingBlankLines(t *testing.T) {
	input := "[identity]\nagent_id = \"\"\nactor_id = \"\"\n"
	once, changed := bridgeReplaceConfigValue(input, "identity", "agent_id", "agent_123")
	if !changed {
		t.Fatal("expected first replacement")
	}
	twice, changed := bridgeReplaceConfigValue(once, "identity", "actor_id", "actor_123")
	if !changed {
		t.Fatal("expected second replacement")
	}
	if strings.HasSuffix(twice, "\n\n") {
		t.Fatalf("expected single trailing newline, got=%q", twice)
	}
}

func TestBridgeReplaceWorkspaceBaseURLUpdatesEveryWorkspace(t *testing.T) {
	input := `schema_version = 1

[[workspaces]]
id = "ws_main"
name = "Main"
base_url = "http://127.0.0.1:8000"
enabled = true

[[workspaces]]
id = "ws_ops"
enabled = true
`
	result, changed := bridgeReplaceWorkspaceBaseURL(input, "https://anx.example")
	if !changed {
		t.Fatal("expected workspace base_url replacement to report a change")
	}
	if strings.Count(result, `base_url = "https://anx.example"`) != 2 {
		t.Fatalf("expected each workspace to have updated base_url, got=%s", result)
	}
	if !strings.Contains(result, `id = "ws_ops"`) {
		t.Fatalf("expected missing base_url inserted in second workspace, got=%s", result)
	}
}

func TestLoadBridgeConfigDetailsRejectsInvalidTOML(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bad.toml")
	if err := os.WriteFile(configPath, []byte("[agent\nhandle = \"x\"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := loadBridgeConfigDetails(configPath); err == nil {
		t.Fatal("expected invalid TOML error")
	}
}

func doctorHarnessBridgeVersionStdoutLine() string {
	v := normalizedBridgeCLIExpectedPackageSemver(defaultBridgeInstallRef())
	if strings.TrimSpace(v) == "" {
		v = "0.0.1"
	}
	return fmt.Sprintf("anx-agent-bridge %s", v)
}

func TestRunBridgeDoctorRejectsExtraArgs(t *testing.T) {
	app := New()
	_, err := app.runBridgeDoctor(context.Background(), []string{"surprise"})
	if err == nil {
		t.Fatal("expected usage error for unexpected positional args")
	}
}

func TestRunBridgeDoctorWithoutConfigAllChecksPass(t *testing.T) {
	home := t.TempDir()
	installDir := filepath.Join(home, ".local", "share", "anx", "agent-bridge")
	binDir := filepath.Join(home, ".local", "bin")
	venvPy := filepath.Join(installDir, ".venv", "bin", "python")
	bridgeBin := filepath.Join(binDir, "anx-agent-bridge")
	fakePy := filepath.Join(home, "fakepython")

	if err := os.MkdirAll(filepath.Dir(venvPy), 0o755); err != nil {
		t.Fatalf("mkdir venv: %v", err)
	}
	if err := os.WriteFile(venvPy, []byte(""), 0o644); err != nil {
		t.Fatalf("write venv python: %v", err)
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	if err := os.WriteFile(bridgeBin, []byte(""), 0o755); err != nil {
		t.Fatalf("write bridge bin: %v", err)
	}
	if err := os.WriteFile(fakePy, []byte(""), 0o755); err != nil {
		t.Fatalf("write fake python: %v", err)
	}

	origRun := bridgeCommandRun
	t.Cleanup(func() { bridgeCommandRun = origRun })
	bridgeCommandRun = func(ctx context.Context, name string, args ...string) (string, string, error) {
		if len(args) >= 2 && args[0] == "-c" && strings.Contains(args[1], "sys.version_info") {
			return "3.12.0", "", nil
		}
		if len(args) == 1 && args[0] == "--version" {
			return "anx-agent-bridge 1.0.0", "", nil
		}
		t.Fatalf("unexpected command name=%q args=%v", name, args)
		return "", "", nil
	}

	app := New()
	app.UserHomeDir = func() (string, error) { return home, nil }
	res, err := app.runBridgeDoctor(context.Background(), []string{"--python", fakePy})
	if err != nil {
		t.Fatalf("runBridgeDoctor: %v", err)
	}
	data, ok := res.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", res.Data)
	}
	checks, ok := data["checks"].([]bridgeDoctorCheck)
	if !ok {
		t.Fatalf("checks type %T", data["checks"])
	}
	for _, c := range checks {
		if !c.OK {
			t.Fatalf("check %s: %s", c.Name, c.Message)
		}
	}
}

func TestRunBridgeDoctorWithConfigAdapterProbeInvalidJSON(t *testing.T) {
	home := t.TempDir()
	installDir := filepath.Join(home, ".local", "share", "anx", "agent-bridge")
	binDir := filepath.Join(home, ".local", "bin")
	venvPy := filepath.Join(installDir, ".venv", "bin", "python")
	bridgeBin := filepath.Join(binDir, "anx-agent-bridge")
	fakePy := filepath.Join(home, "fakepython")

	if err := os.MkdirAll(filepath.Dir(venvPy), 0o755); err != nil {
		t.Fatalf("mkdir venv: %v", err)
	}
	if err := os.WriteFile(venvPy, []byte(""), 0o644); err != nil {
		t.Fatalf("write venv python: %v", err)
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	if err := os.WriteFile(bridgeBin, []byte(""), 0o755); err != nil {
		t.Fatalf("write bridge bin: %v", err)
	}
	if err := os.WriteFile(fakePy, []byte(""), 0o755); err != nil {
		t.Fatalf("write fake python: %v", err)
	}
	configPath := writeBridgeAgentHomeFixture(t, home, "hermes")

	origRun := bridgeCommandRun
	t.Cleanup(func() { bridgeCommandRun = origRun })
	bridgeCommandRun = func(ctx context.Context, name string, args ...string) (string, string, error) {
		if len(args) >= 2 && args[0] == "-c" && strings.Contains(args[1], "sys.version_info") {
			return "3.12.0", "", nil
		}
		if len(args) == 1 && args[0] == "--version" {
			return doctorHarnessBridgeVersionStdoutLine(), "", nil
		}
		if len(args) >= 4 && args[0] == "bridge" && args[1] == "doctor" {
			return "NOT JSON", "", nil
		}
		if len(args) >= 3 && args[0] == "registration" && args[1] == "status" {
			return `{"wakeable":true}`, "", nil
		}
		t.Fatalf("unexpected command name=%q args=%v", name, args)
		return "", "", nil
	}

	app := New()
	app.UserHomeDir = func() (string, error) { return home, nil }
	_, err := app.runBridgeDoctor(context.Background(), []string{"--python", fakePy, "--config", configPath})
	if err == nil {
		t.Fatal("expected bridge_doctor_failed when adapter stdout is not JSON")
	}
}

func TestRunBridgeDoctorWithConfigAllProbesPass(t *testing.T) {
	home := t.TempDir()
	installDir := filepath.Join(home, ".local", "share", "anx", "agent-bridge")
	binDir := filepath.Join(home, ".local", "bin")
	venvPy := filepath.Join(installDir, ".venv", "bin", "python")
	bridgeBin := filepath.Join(binDir, "anx-agent-bridge")
	fakePy := filepath.Join(home, "fakepython")

	if err := os.MkdirAll(filepath.Dir(venvPy), 0o755); err != nil {
		t.Fatalf("mkdir venv: %v", err)
	}
	if err := os.WriteFile(venvPy, []byte(""), 0o644); err != nil {
		t.Fatalf("write venv python: %v", err)
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	if err := os.WriteFile(bridgeBin, []byte(""), 0o755); err != nil {
		t.Fatalf("write bridge bin: %v", err)
	}
	if err := os.WriteFile(fakePy, []byte(""), 0o755); err != nil {
		t.Fatalf("write fake python: %v", err)
	}
	configPath := writeBridgeAgentHomeFixture(t, home, "hermes")

	origRun := bridgeCommandRun
	t.Cleanup(func() { bridgeCommandRun = origRun })
	bridgeCommandRun = func(ctx context.Context, name string, args ...string) (string, string, error) {
		if len(args) >= 2 && args[0] == "-c" && strings.Contains(args[1], "sys.version_info") {
			return "3.12.0", "", nil
		}
		if len(args) == 1 && args[0] == "--version" {
			return doctorHarnessBridgeVersionStdoutLine(), "", nil
		}
		if len(args) >= 4 && args[0] == "bridge" && args[1] == "doctor" {
			return `{"ok":true,"adapter_kind":"stub"}`, "", nil
		}
		if len(args) >= 3 && args[0] == "registration" && args[1] == "status" {
			return `{"wakeable":true}`, "", nil
		}
		t.Fatalf("unexpected command name=%q args=%v", name, args)
		return "", "", nil
	}

	app := New()
	app.UserHomeDir = func() (string, error) { return home, nil }
	res, err := app.runBridgeDoctor(context.Background(), []string{"--python", fakePy, "--config", configPath})
	if err != nil {
		t.Fatalf("runBridgeDoctor: %v", err)
	}
	data, ok := res.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", res.Data)
	}
	checks, ok := data["checks"].([]bridgeDoctorCheck)
	if !ok {
		t.Fatalf("checks type %T", data["checks"])
	}
	byName := map[string]bridgeDoctorCheck{}
	for _, c := range checks {
		byName[c.Name] = c
	}
	for _, name := range []string{"python", "managed_venv", "bridge_binary", "bridge_version", "config", "managed_bridge_package", "adapter", "registration"} {
		c, ok := byName[name]
		if !ok {
			t.Fatalf("missing check %q in %#v", name, checks)
		}
		if !c.OK {
			t.Fatalf("check %s: %s", name, c.Message)
		}
	}
}

func TestRenderBridgeIncludesManagedAutoWhenOptedIn(t *testing.T) {
	rendered, _, err := renderBridgeConfigTemplate(bridgeTemplateParams{
		Kind:                     "subprocess",
		BaseURL:                  "https://anx.example",
		WorkspaceIDs:             []string{"ws_main"},
		WorkspaceName:            "Main",
		Handle:                   "h",
		AdapterEntrypoint:        "./adapter.py",
		ManagedPackageAutoUpdate: true,
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(rendered, "managed_package_auto_update = true") {
		t.Fatalf("expected managed auto update stanza output=%s", rendered)
	}
}

func TestLoadBridgeManagedParsesManagedPackageAutoFlag(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "b.toml")
	if err := os.WriteFile(p, []byte(`
agent_home = ".anx"

[bridge]
managed_package_auto_update = true
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadBridgeManagedConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ManagedPackageAutoUpdate {
		t.Fatalf("managed auto flag parsing failed: %#v", cfg)
	}
}
