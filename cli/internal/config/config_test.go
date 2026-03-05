package config

import (
	"os"
	"path/filepath"
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
		"OAR_BASE_URL":     "http://from-env:8000",
		"OAR_AGENT":        "env-agent",
		"OAR_NO_COLOR":     "false",
		"OAR_JSON":         "false",
		"OAR_TIMEOUT":      "33s",
		"OAR_ACCESS_TOKEN": "env-token",
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
			expected := filepath.Join("/home/tester", ".config", "oar", "profiles", "flag-agent.json")
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
	if resolved.ProfilePath != filepath.Join("/home/tester", ".config", "oar", "profiles", "default.json") {
		t.Fatalf("unexpected default profile path: %s", resolved.ProfilePath)
	}
}
