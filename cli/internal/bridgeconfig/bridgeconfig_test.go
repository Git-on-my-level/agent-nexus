package bridgeconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverFindsNestedBridgeConfigs(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	configDir := filepath.Join(RootDir(home), "workspace-a")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	configPath := filepath.Join(configDir, "agent.toml")
	if err := os.WriteFile(configPath, []byte("[anx]\nbase_url = \"https://core.example\"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	configs, err := Discover(home)
	if err != nil {
		t.Fatalf("discover configs: %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("expected one config, got %#v", configs)
	}
	if configs[0].Path != configPath {
		t.Fatalf("unexpected config path: %s", configs[0].Path)
	}
	if configs[0].BaseURL != "https://core.example" {
		t.Fatalf("unexpected base url: %s", configs[0].BaseURL)
	}
}

func TestDiscoverIgnoresConfigsWithoutBaseURL(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	configDir := filepath.Join(RootDir(home), "workspace-a")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "agent.toml"), []byte("[agent]\nhandle = \"bot\"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	configs, err := Discover(home)
	if err != nil {
		t.Fatalf("discover configs: %v", err)
	}
	if len(configs) != 0 {
		t.Fatalf("expected no configs, got %#v", configs)
	}
}
