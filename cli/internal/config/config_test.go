package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResolvePrecedence(t *testing.T) {
	t.Parallel()

	jsonFlag := true
	baseURLFlag := "http://from-flag:9000"
	agentFlag := "flag-agent"
	noColorFlag := true
	timeoutFlag := 42 * time.Second

	profileJSON := []byte(`{
		"base_url": "http://from-profile:7000",
		"timeout": "21s",
		"no_color": false,
		"json": false,
		"access_token": "profile-token"
	}`)
	envMap := map[string]string{
		"ANX_BASE_URL":     "http://from-env:8000",
		"ANX_AGENT":        "env-agent",
		"ANX_NO_COLOR":     "false",
		"ANX_JSON":         "false",
		"ANX_TIMEOUT":      "33s",
		"ANX_ACCESS_TOKEN": "env-token",
	}

	resolved, err := Resolve(Overrides{
		JSON:    &jsonFlag,
		BaseURL: &baseURLFlag,
		Agent:   &agentFlag,
		NoColor: &noColorFlag,
		Timeout: &timeoutFlag,
	}, Environment{
		Getenv: func(key string) string {
			return envMap[key]
		},
		UserHomeDir: func() (string, error) {
			return "/home/tester", nil
		},
		ReadFile: func(path string) ([]byte, error) {
			expected := filepath.Join("/home/tester", ".config", "anx", "profiles", "flag-agent.json")
			if path != expected {
				t.Fatalf("unexpected profile path: got %s want %s", path, expected)
			}
			return profileJSON, nil
		},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if resolved.BaseURL != "http://from-flag:9000" {
		t.Fatalf("unexpected base url: %s", resolved.BaseURL)
	}
	if resolved.Agent != "flag-agent" {
		t.Fatalf("unexpected agent: %s", resolved.Agent)
	}
	if resolved.Timeout != 42*time.Second {
		t.Fatalf("unexpected timeout: %s", resolved.Timeout)
	}
	if !resolved.NoColor {
		t.Fatal("expected no_color true from flag")
	}
	if !resolved.JSON {
		t.Fatal("expected json true from flag")
	}
	if resolved.AccessToken != "env-token" {
		t.Fatalf("unexpected access token: %s", resolved.AccessToken)
	}

	if resolved.Sources["base_url"] != "flag:--base-url" {
		t.Fatalf("unexpected base_url source: %s", resolved.Sources["base_url"])
	}
	if resolved.Sources["agent"] != "flag:--agent" {
		t.Fatalf("unexpected agent source: %s", resolved.Sources["agent"])
	}
	if resolved.Sources["timeout"] != "flag:--timeout" {
		t.Fatalf("unexpected timeout source: %s", resolved.Sources["timeout"])
	}
}

func TestResolveDefaultsWithoutProfile(t *testing.T) {
	t.Parallel()

	resolved, err := Resolve(Overrides{}, Environment{
		Getenv: func(string) string { return "" },
		UserHomeDir: func() (string, error) {
			return "/home/tester", nil
		},
		ReadFile: func(path string) ([]byte, error) {
			return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
		},
	})
	if err != nil {
		t.Fatalf("resolve defaults: %v", err)
	}

	if resolved.BaseURL != DefaultBaseURL {
		t.Fatalf("unexpected default base url: %s", resolved.BaseURL)
	}
	if resolved.Agent != DefaultAgent {
		t.Fatalf("unexpected default agent: %s", resolved.Agent)
	}
	if resolved.Timeout != DefaultTimeout {
		t.Fatalf("unexpected default timeout: %s", resolved.Timeout)
	}
	if resolved.ProfilePath != filepath.Join("/home/tester", ".config", "anx", "profiles", "default.json") {
		t.Fatalf("unexpected default profile path: %s", resolved.ProfilePath)
	}
}

func TestResolveAutoSelectSingleProfileAgent(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	profilesDir := filepath.Join(home, ".config", "anx", "profiles")
	if err := os.MkdirAll(profilesDir, 0o700); err != nil {
		t.Fatalf("mkdir profiles dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "solo.json"), []byte(`{"base_url":"http://solo:8000"}`), 0o600); err != nil {
		t.Fatalf("write profile file: %v", err)
	}

	resolved, err := Resolve(Overrides{}, Environment{
		Getenv:      func(string) string { return "" },
		UserHomeDir: func() (string, error) { return home, nil },
		ReadFile:    os.ReadFile,
	})
	if err != nil {
		t.Fatalf("resolve with single profile: %v", err)
	}
	if resolved.Agent != "solo" {
		t.Fatalf("unexpected selected agent: %s", resolved.Agent)
	}
	if resolved.Sources["agent"] != "profile:auto-single" {
		t.Fatalf("unexpected agent source: %s", resolved.Sources["agent"])
	}
}

func TestResolveFailsWithMultipleProfilesWithoutAgentSelection(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	profilesDir := filepath.Join(home, ".config", "anx", "profiles")
	if err := os.MkdirAll(profilesDir, 0o700); err != nil {
		t.Fatalf("mkdir profiles dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "alpha.json"), []byte(`{"base_url":"http://alpha:8000"}`), 0o600); err != nil {
		t.Fatalf("write alpha profile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "beta.json"), []byte(`{"base_url":"http://beta:8000"}`), 0o600); err != nil {
		t.Fatalf("write beta profile: %v", err)
	}

	_, err := Resolve(Overrides{}, Environment{
		Getenv:      func(string) string { return "" },
		UserHomeDir: func() (string, error) { return home, nil },
		ReadFile:    os.ReadFile,
	})
	if err == nil {
		t.Fatal("expected resolve error with multiple profiles and no explicit agent")
	}
	if !strings.Contains(err.Error(), "multiple local profiles found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveUsesDefaultProfileSelection(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	profilesDir := filepath.Join(home, ".config", "anx", "profiles")
	if err := os.MkdirAll(profilesDir, 0o700); err != nil {
		t.Fatalf("mkdir profiles dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "alpha.json"), []byte(`{"base_url":"http://alpha:8000"}`), 0o600); err != nil {
		t.Fatalf("write alpha profile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "beta.json"), []byte(`{"base_url":"http://beta:8000"}`), 0o600); err != nil {
		t.Fatalf("write beta profile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "anx", "default-profile"), []byte("beta\n"), 0o600); err != nil {
		t.Fatalf("write default profile: %v", err)
	}

	resolved, err := Resolve(Overrides{}, Environment{
		Getenv:      func(string) string { return "" },
		UserHomeDir: func() (string, error) { return home, nil },
		ReadFile:    os.ReadFile,
	})
	if err != nil {
		t.Fatalf("resolve with default profile: %v", err)
	}
	if resolved.Agent != "beta" {
		t.Fatalf("unexpected selected agent: %s", resolved.Agent)
	}
	if resolved.BaseURL != "http://beta:8000" {
		t.Fatalf("unexpected base url: %s", resolved.BaseURL)
	}
	if resolved.Sources["agent"] != "profile:default" {
		t.Fatalf("unexpected agent source: %s", resolved.Sources["agent"])
	}
}

func TestResolveUsesSingleBridgeConfigBaseURLFallback(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	bridgeDir := filepath.Join(home, ".config", "anx-bridge", "workspace-a")
	if err := os.MkdirAll(bridgeDir, 0o700); err != nil {
		t.Fatalf("mkdir bridge dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bridgeDir, "agent.toml"), []byte("[anx]\nbase_url = \"https://bridge.example\"\n"), 0o600); err != nil {
		t.Fatalf("write bridge config: %v", err)
	}

	resolved, err := Resolve(Overrides{}, Environment{
		Getenv:      func(string) string { return "" },
		UserHomeDir: func() (string, error) { return home, nil },
		ReadFile:    os.ReadFile,
	})
	if err != nil {
		t.Fatalf("resolve with bridge fallback: %v", err)
	}
	if resolved.BaseURL != "https://bridge.example" {
		t.Fatalf("unexpected base url: %s", resolved.BaseURL)
	}
	if resolved.Sources["base_url"] != "bridge:auto-single" {
		t.Fatalf("unexpected base url source: %s", resolved.Sources["base_url"])
	}
}

func TestResolveFailsWithMultipleBridgeConfigsWithoutExplicitBaseURL(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	for _, workspace := range []string{"workspace-a", "workspace-b"} {
		bridgeDir := filepath.Join(home, ".config", "anx-bridge", workspace)
		if err := os.MkdirAll(bridgeDir, 0o700); err != nil {
			t.Fatalf("mkdir bridge dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(bridgeDir, "agent.toml"), []byte("[anx]\nbase_url = \"https://"+workspace+".example\"\n"), 0o600); err != nil {
			t.Fatalf("write bridge config: %v", err)
		}
	}

	_, err := Resolve(Overrides{}, Environment{
		Getenv:      func(string) string { return "" },
		UserHomeDir: func() (string, error) { return home, nil },
		ReadFile:    os.ReadFile,
	})
	if err == nil {
		t.Fatal("expected resolve error with multiple bridge configs")
	}
	if !strings.Contains(err.Error(), "multiple bridge configs found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveExplicitBaseURLOverridesBridgeAmbiguity(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	for _, workspace := range []string{"workspace-a", "workspace-b"} {
		bridgeDir := filepath.Join(home, ".config", "anx-bridge", workspace)
		if err := os.MkdirAll(bridgeDir, 0o700); err != nil {
			t.Fatalf("mkdir bridge dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(bridgeDir, "agent.toml"), []byte("[anx]\nbase_url = \"https://"+workspace+".example\"\n"), 0o600); err != nil {
			t.Fatalf("write bridge config: %v", err)
		}
	}

	baseURL := "https://flag.example"
	resolved, err := Resolve(Overrides{BaseURL: &baseURL}, Environment{
		Getenv:      func(string) string { return "" },
		UserHomeDir: func() (string, error) { return home, nil },
		ReadFile:    os.ReadFile,
	})
	if err != nil {
		t.Fatalf("resolve with explicit base url: %v", err)
	}
	if resolved.BaseURL != baseURL {
		t.Fatalf("unexpected base url: %s", resolved.BaseURL)
	}
}

func TestResolveIgnoresStaleDefaultProfileSelection(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	profilesDir := filepath.Join(home, ".config", "anx", "profiles")
	if err := os.MkdirAll(profilesDir, 0o700); err != nil {
		t.Fatalf("mkdir profiles dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "alpha.json"), []byte(`{"base_url":"http://alpha:8000"}`), 0o600); err != nil {
		t.Fatalf("write alpha profile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "beta.json"), []byte(`{"base_url":"http://beta:8000"}`), 0o600); err != nil {
		t.Fatalf("write beta profile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "anx", "default-profile"), []byte("missing\n"), 0o600); err != nil {
		t.Fatalf("write default profile: %v", err)
	}

	_, err := Resolve(Overrides{}, Environment{
		Getenv:      func(string) string { return "" },
		UserHomeDir: func() (string, error) { return home, nil },
		ReadFile:    os.ReadFile,
	})
	if err == nil {
		t.Fatal("expected resolve error with stale default profile and multiple local profiles")
	}
	if !strings.Contains(err.Error(), "multiple local profiles found") {
		t.Fatalf("unexpected error: %v", err)
	}
}
