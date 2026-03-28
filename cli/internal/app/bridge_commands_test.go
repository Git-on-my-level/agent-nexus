package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
