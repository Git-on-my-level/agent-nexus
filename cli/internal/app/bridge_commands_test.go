package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBridgeHostOnlyHelp(t *testing.T) {
	help := bridgeUsageText()
	for _, expected := range []string{"anx host enroll", "anx bridge install", "anx bridge start", "anx bridge stop", "anx bridge status", "anx bridge doctor"} {
		if !strings.Contains(help, expected) {
			t.Fatalf("missing %s: %s", expected, help)
		}
	}
	for _, obsolete := range []string{"import-auth", "init-config", "agent_home", "workspace-id"} {
		if strings.Contains(help, obsolete) {
			t.Fatalf("obsolete command %s in help", obsolete)
		}
	}
}

func TestBridgeHostConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bridge.toml")
	content := "[host]\nbase_url = \"http://127.0.0.1:8093\"\nid = \"host-1\"\nslug = \"test-host\"\n[agents.codex]\ncommand = [\"stub\"]\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadBridgeManagedConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RuntimeKind != "host" || cfg.ConfigPath != path {
		t.Fatalf("%+v", cfg)
	}
	if _, _, _, err := inferBridgeRuntimeKind(map[string]any{"agent_home": "old"}, path); err == nil {
		t.Fatal("old agent home accepted")
	}
}

func TestBridgeSideEffectClasses(t *testing.T) {
	for command, want := range map[string]string{
		"bridge install": "local_operational_write",
		"bridge start":   "external_side_effect",
		"bridge stop":    "local_operational_write",
		"bridge status":  "read_only",
		"bridge doctor":  "remote_coordination_write",
	} {
		if got := commandSideEffectClass(command); got != want {
			t.Fatalf("%s: got %s want %s", command, got, want)
		}
	}
}
