package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-nexus-cli/internal/profile"
)

func TestConfigUseSetsActiveProfile(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := profile.Save(profile.ProfilePath(home, "alpha"), profile.Profile{Agent: "alpha", BaseURL: "http://alpha:8000"}); err != nil {
		t.Fatalf("save alpha profile: %v", err)
	}
	if err := profile.Save(profile.ProfilePath(home, "beta"), profile.Profile{Agent: "beta", BaseURL: "http://beta:8000"}); err != nil {
		t.Fatalf("save beta profile: %v", err)
	}

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "config", "use", "beta"})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	if strings.TrimSpace(anyStr(data["active_profile"])) != "beta" {
		t.Fatalf("unexpected config use payload: %#v", payload)
	}
	if warnings, _ := data["warnings"].([]any); len(warnings) == 0 {
		t.Fatalf("expected multi-profile warning: %#v", payload)
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

func TestConfigUseOmitsMultiProfileWarningForSingleProfile(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := profile.Save(profile.ProfilePath(home, "solo"), profile.Profile{Agent: "solo", BaseURL: "http://solo:8000"}); err != nil {
		t.Fatalf("save solo profile: %v", err)
	}

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "config", "use", "solo"})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	if _, exists := data["warnings"]; exists {
		t.Fatalf("did not expect warning for one profile: %#v", payload)
	}
}

func TestConfigUseNormalizedSubcommandIsConfigLenient(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := profile.Save(profile.ProfilePath(home, "a"), profile.Profile{Agent: "a", BaseURL: "http://a:8000"}); err != nil {
		t.Fatalf("save profile: %v", err)
	}
	if err := profile.Save(profile.ProfilePath(home, "b"), profile.Profile{Agent: "b", BaseURL: "http://b:8000"}); err != nil {
		t.Fatalf("save profile: %v", err)
	}

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "config", "USE", "b"})
	assertEnvelopeOK(t, raw)
	versionRaw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "version"})
	versionPayload := assertEnvelopeOK(t, versionRaw)
	versionData, _ := versionPayload["data"].(map[string]any)
	if strings.TrimSpace(anyStr(versionData["agent"])) != "b" {
		t.Fatalf("unexpected agent: %#v", versionPayload)
	}
}

func TestConfigUseWorksWithMultipleProfilesWithoutPriorDefault(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := profile.Save(profile.ProfilePath(home, "a"), profile.Profile{Agent: "a", BaseURL: "http://a:8000"}); err != nil {
		t.Fatalf("save profile: %v", err)
	}
	if err := profile.Save(profile.ProfilePath(home, "b"), profile.Profile{Agent: "b", BaseURL: "http://b:8000"}); err != nil {
		t.Fatalf("save profile: %v", err)
	}

	// Without default, plain `version` would fail resolve; `config use` is config-lenient.
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "config", "use", "b"})
	assertEnvelopeOK(t, raw)
	versionRaw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "version"})
	versionPayload := assertEnvelopeOK(t, versionRaw)
	versionData, _ := versionPayload["data"].(map[string]any)
	if strings.TrimSpace(anyStr(versionData["agent"])) != "b" {
		t.Fatalf("unexpected agent: %#v", versionPayload)
	}
}

func TestConfigUnsetClearsDefaultMarker(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := profile.Save(profile.ProfilePath(home, "solo"), profile.Profile{Agent: "solo", BaseURL: "http://solo:8000"}); err != nil {
		t.Fatalf("save profile: %v", err)
	}
	if err := profile.SaveDefaultAgent(home, "solo"); err != nil {
		t.Fatalf("default: %v", err)
	}

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "config", "unset"})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	if cleared, _ := data["cleared"].(bool); !cleared {
		t.Fatalf("expected cleared=true: %#v", payload)
	}
	defaultPath := filepath.Join(home, ".config", "anx", "default-profile")
	if strings.TrimSpace(anyStr(data["default_file_path"])) != defaultPath {
		t.Fatalf("unexpected default path in payload: %#v", payload)
	}
	_, ok, err := profile.LoadDefaultAgent(home)
	if err != nil {
		t.Fatalf("LoadDefaultAgent: %v", err)
	}
	if ok {
		t.Fatal("expected default marker removed")
	}
}

func TestConfigShowRedactsTokens(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := profile.Save(profile.ProfilePath(home, "sec"), profile.Profile{
		Agent:       "sec",
		BaseURL:     "http://sec:8000",
		AccessToken: "super-secret-access",
	}); err != nil {
		t.Fatalf("save profile: %v", err)
	}

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "--agent", "sec", "config", "show"})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	if strings.TrimSpace(anyStr(data["access_token"])) != "(redacted)" {
		t.Fatalf("expected redacted access_token, got %#v", data["access_token"])
	}
	rawJSON, _ := json.Marshal(data)
	if strings.Contains(string(rawJSON), "super-secret") {
		t.Fatalf("secret leaked in JSON: %s", rawJSON)
	}
	envVars, _ := data["environment_overrides_available"].([]any)
	if len(envVars) == 0 {
		t.Fatalf("expected env var discovery data: %#v", data)
	}
	if !strings.Contains(string(rawJSON), "ANX_AGENT") || !strings.Contains(string(rawJSON), "ANX_JSON") {
		t.Fatalf("expected supported env vars in config show data: %s", rawJSON)
	}
}

func TestConfigShowTextIncludesPrecedenceAndEnvHints(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := profile.Save(profile.ProfilePath(home, "sec"), profile.Profile{
		Agent:   "sec",
		BaseURL: "http://sec:8000",
	}); err != nil {
		t.Fatalf("save profile: %v", err)
	}

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--agent", "sec", "config", "show"})
	if !strings.Contains(raw, "command flags > environment variables > profile/default marker/autodiscovery > built-in defaults") {
		t.Fatalf("expected precedence in text output: %s", raw)
	}
	if !strings.Contains(raw, "ANX_AGENT") || !strings.Contains(raw, "anx meta doc profiles") {
		t.Fatalf("expected env hints and docs links in text output: %s", raw)
	}
}

func TestConfigUseMissingProfile(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := profile.Save(profile.ProfilePath(home, "only"), profile.Profile{Agent: "only", BaseURL: "http://only:8000"}); err != nil {
		t.Fatalf("save profile: %v", err)
	}

	stdout := runCLIForTestJSONError(t, home, map[string]string{}, []string{"--json", "config", "use", "nope"})
	payload := assertEnvelopeError(t, stdout)
	full, _ := json.Marshal(payload)
	if !strings.Contains(string(full), "profile_not_found") {
		t.Fatalf("expected profile_not_found in envelope: %s", full)
	}
}

func TestConfigShowFailsWhenProfilesAmbiguous(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := profile.Save(profile.ProfilePath(home, "x"), profile.Profile{Agent: "x", BaseURL: "http://x:8000"}); err != nil {
		t.Fatalf("save profile: %v", err)
	}
	if err := profile.Save(profile.ProfilePath(home, "y"), profile.Profile{Agent: "y", BaseURL: "http://y:8000"}); err != nil {
		t.Fatalf("save profile: %v", err)
	}

	stdout := runCLIForTestJSONError(t, home, map[string]string{}, []string{"--json", "config", "show"})
	payload := assertEnvelopeError(t, stdout)
	full, _ := json.Marshal(payload)
	if !strings.Contains(string(full), "multiple local profiles") {
		t.Fatalf("expected ambiguous profile error in envelope: %s", full)
	}
}

func runCLIForTestJSONError(t *testing.T, home string, env map[string]string, args []string) string {
	t.Helper()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := New()
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = strings.NewReader("")
	cli.StdinIsTTY = func() bool { return false }
	cli.UserHomeDir = func() (string, error) { return home, nil }
	cli.ReadFile = os.ReadFile
	cli.Getenv = func(key string) string { return env[key] }
	exit := cli.Run(args)
	if exit == 0 {
		t.Fatalf("expected non-zero exit, got 0 stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	return stdout.String()
}
