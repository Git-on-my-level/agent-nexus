package app

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/hostidentity"
)

func TestDoctorCLIVersionAgainstHandshake(t *testing.T) {
	cases := []struct {
		name        string
		min         string
		recommended string
		wantExit    int
		wantStatus  string
		wantCode    string
		wantNext    string
	}{
		{name: "below minimum fails", min: "v99.0.0", recommended: "v99.1.0", wantExit: 7, wantStatus: "fail", wantCode: "cli_outdated", wantNext: "anx update --version v99.1.0"},
		{name: "below recommended warns", min: "v0.1.0", recommended: "v99.1.0", wantExit: 0, wantStatus: "warn", wantNext: "anx update --version v99.1.0"},
		{name: "current meets handshake", min: "v0.1.0", recommended: "v0.1.0", wantExit: 0, wantStatus: "pass"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/readyz":
					_, _ = w.Write([]byte(`{"ok":true}`))
				case "/meta/handshake":
					body, _ := json.Marshal(map[string]string{"min_cli_version": tc.min, "recommended_cli_version": tc.recommended})
					_, _ = w.Write(body)
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)

			stdout := &strings.Builder{}
			cli := newTestApp(t)
			cli.Stdout = stdout
			cli.Stderr = &strings.Builder{}
			cli.Getenv = func(string) string { return "" }
			cli.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
			exitCode := cli.Run([]string{"--json", "--base-url", server.URL, "doctor"})
			if exitCode != tc.wantExit {
				t.Fatalf("exit %d want %d stdout=%s", exitCode, tc.wantExit, stdout.String())
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(stdout.String()), &payload); err != nil {
				t.Fatalf("decode: %v raw=%s", err, stdout.String())
			}
			var checks []any
			var actions []any
			if tc.wantExit == 0 {
				result := asMap(payload["result"])
				checks = asSlice(result["checks"])
				if result["source"] != "flag:--base-url" || result["base_url"] != server.URL || !doctorStatus(checks, "workspace_resolution", "pass") {
					t.Fatalf("workspace diagnostics=%#v", result)
				}
				actions = asSlice(payload["next_actions"])
				if tc.wantStatus == "warn" {
					warnings := asSlice(payload["warnings"])
					if len(warnings) != 1 || anyString(asMap(warnings[0])["code"]) != "cli_version" {
						t.Fatalf("warnings=%#v", warnings)
					}
				}
			} else {
				errObj := asMap(payload["error"])
				if anyString(errObj["code"]) != tc.wantCode {
					t.Fatalf("error=%#v", errObj)
				}
				if !strings.Contains(anyString(errObj["message"]), "anx update --version") {
					t.Fatalf("message=%s", anyString(errObj["message"]))
				}
				checks = asSlice(asMap(errObj["details"])["checks"])
				actions = asSlice(errObj["next_actions"])
			}
			if !doctorStatus(checks, "cli_version", tc.wantStatus) {
				t.Fatalf("cli_version status want %s checks=%#v", tc.wantStatus, checks)
			}
			if tc.wantNext != "" {
				found := false
				for _, raw := range actions {
					if strings.Join(stringList(asMap(raw)["argv"]), " ") == tc.wantNext {
						found = true
					}
				}
				if !found {
					t.Fatalf("next actions=%#v want %s", actions, tc.wantNext)
				}
			}
		})
	}
}

func doctorStatus(checks []any, name, status string) bool {
	for _, raw := range checks {
		check := asMap(raw)
		if anyString(check["name"]) == name && anyString(check["status"]) == status {
			return true
		}
	}
	return false
}

func TestDoctorDoesNotGuessFromOneInstalledAdapter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/readyz":
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/meta/handshake":
			_, _ = w.Write([]byte(`{"min_cli_version":"0.1.0"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cli := newTestApp(t)
	stdout := &strings.Builder{}
	cli.Stdout = stdout
	cli.Stderr = &strings.Builder{}
	cli.Getenv = func(string) string { return "" }
	cli.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	cli.runtimeIdentity = func() (*runtimeIdentityReport, error) {
		executionID := "exec-123"
		return &runtimeIdentityReport{
			Execution: runtimeIdentityEvidence{ID: &executionID, Confidence: "observed"},
			Harnesses: []runtimeHarness{{ProviderID: "hermes", Availability: "available"}},
		}, nil
	}
	if code := cli.Run([]string{"--json", "--base-url", server.URL, "doctor"}); code != 0 {
		t.Fatalf("doctor failed: %d %s", code, stdout.String())
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout.String()), &payload); err != nil {
		t.Fatal(err)
	}
	checks := asSlice(asMap(payload["result"])["checks"])
	var identityCheck map[string]any
	for _, raw := range checks {
		check := asMap(raw)
		if anyString(check["name"]) == "identity_resolution" {
			identityCheck = check
		}
	}
	message := anyString(identityCheck["message"])
	if anyString(identityCheck["status"]) != "fail" || asBool(identityCheck["ok"]) {
		t.Fatalf("installed adapter must not resolve caller identity: %#v", identityCheck)
	}
	if !strings.Contains(message, "--as <agent-name>") || !strings.Contains(message, "ANX_AS=<agent-name>") || !strings.Contains(message, "lowercase name of the agent tool you are running in") || !strings.Contains(message, "first authenticated call registers that name on this host if it is new") || !strings.Contains(message, "Stop only if you cannot tell which agent tool you are running in") {
		t.Fatalf("identity check lacks a safe repair: %s", message)
	}
	if strings.Contains(message, "hermes") {
		t.Fatalf("doctor guessed the installed adapter as the caller: %s", message)
	}
	for _, raw := range checks {
		check := asMap(raw)
		if anyString(check["name"]) == "agentctl_presence" && !asBool(check["ok"]) {
			t.Fatalf("agentctl absence must not be a failed check: %#v", check)
		}
	}
}

func TestDoctorIdentityFailureNamesTheRequiredSelection(t *testing.T) {
	check := identityResolutionDoctorCheck("", "", errnorm.Usage("identity_unresolved", "pass --as <agent-name> or set ANX_AS"))
	if check.OK || check.Status != "fail" || !strings.Contains(check.Message, "--as <agent-name>") || !strings.Contains(check.Message, "ANX_AS=<agent-name>") || !strings.Contains(check.Message, "first authenticated call registers that name on this host if it is new") || !strings.Contains(check.Message, "Stop only if you cannot tell which agent tool you are running in") {
		t.Fatalf("identity failure should name the exact repair shape: %+v", check)
	}
}

func TestDoctorAgentctlAbsenceIsAnOptionalWarning(t *testing.T) {
	check := agentctlPresenceDoctorCheck(errors.New("agentctl not found"))
	if !check.OK || check.Status != "warn" || !strings.Contains(check.Message, "optional") {
		t.Fatalf("agentctl absence should be an optional warning: %+v", check)
	}
}

func TestBareInvocationUsesSingleEnrolledHostBaseURL(t *testing.T) {
	home := t.TempDir()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	const enrolled = "https://core.example:8443"
	if err := hostidentity.SaveAt(filepath.Join(home, ".config", "anx"), hostidentity.Host{
		ID: "host-1", KeyID: "key-1", Slug: "mac", WorkspaceID: "ws_only", BaseURL: enrolled,
	}, key); err != nil {
		t.Fatal(err)
	}
	stdout := &strings.Builder{}
	cli := newTestApp(t)
	cli.Stdout = stdout
	cli.Stderr = &strings.Builder{}
	cli.Getenv = func(key string) string {
		if key == "HOME" {
			return home
		}
		return ""
	}
	cli.UserHomeDir = func() (string, error) { return home, nil }
	if exitCode := cli.Run([]string{"--json", "config", "show"}); exitCode != 0 {
		t.Fatalf("exit %d stdout=%s", exitCode, stdout.String())
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout.String()), &payload); err != nil {
		t.Fatal(err)
	}
	result := asMap(payload["result"])
	sources := asMap(result["sources"])
	if result["base_url"] != enrolled || sources["base_url"] != "bridge:auto-single" {
		t.Fatalf("bare config fell back: %#v", result)
	}

	stdout.Reset()
	cli.Getenv = func(key string) string {
		switch key {
		case "HOME":
			return home
		case "ANX_BASE_URL":
			return "https://explicit.example"
		default:
			return ""
		}
	}
	if exitCode := cli.Run([]string{"--json", "config", "show"}); exitCode != 0 {
		t.Fatalf("exit %d stdout=%s", exitCode, stdout.String())
	}
	if err := json.Unmarshal([]byte(stdout.String()), &payload); err != nil {
		t.Fatal(err)
	}
	result = asMap(payload["result"])
	if result["base_url"] != "https://explicit.example" || asMap(result["sources"])["base_url"] != "env:ANX_BASE_URL" {
		t.Fatalf("explicit base URL lost: %#v", result)
	}
}

func TestBareInvocationFailsWhenSeveralHostsAreEnrolled(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".config", "anx")
	for _, host := range []hostidentity.Host{
		{ID: "host-a", KeyID: "key-a", Slug: "a", WorkspaceID: "ws_a", BaseURL: "https://a.example"},
		{ID: "host-b", KeyID: "key-b", Slug: "b", WorkspaceID: "ws_b", BaseURL: "https://b.example"},
	} {
		_, key, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if err := hostidentity.SaveAt(configDir, host, key); err != nil {
			t.Fatal(err)
		}
	}
	stdout := &strings.Builder{}
	cli := newTestApp(t)
	cli.Stdout = stdout
	cli.Stderr = &strings.Builder{}
	cli.Getenv = func(key string) string {
		if key == "HOME" {
			return home
		}
		return ""
	}
	cli.UserHomeDir = func() (string, error) { return home, nil }
	for _, args := range [][]string{
		{"--json", "config", "show"}, {"--json", "doctor"}, {"--json", "topics", "list"},
		{"--json", "api", "call", "--path", "/readyz"}, {"--json", "host", "status"},
		{"--json", "debug", "meta", "handshake"}, {"--json", "debug", "meta", "health"},
		{"--json", "debug", "meta", "readyz"}, {"--json", "debug", "meta", "livez"},
		{"--json", "debug", "meta", "version"}, {"--json", "debug", "meta", "ops", "health"},
	} {
		stdout.Reset()
		if exitCode := cli.Run(args); exitCode != 2 {
			t.Fatalf("exit %d stdout=%s", exitCode, stdout.String())
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(stdout.String()), &payload); err != nil {
			t.Fatal(err)
		}
		errorData := asMap(payload["error"])
		if errorData["code"] != "workspace_ambiguous" {
			t.Fatalf("error=%#v", errorData)
		}
		message := anyString(errorData["message"])
		for _, repair := range []string{"a (https://a.example)", "b (https://b.example)", "anx config use a", "anx config use b"} {
			if !strings.Contains(message, repair) {
				t.Fatalf("message=%s missing %s", message, repair)
			}
		}
		if len(asSlice(errorData["next_actions"])) != 3 {
			t.Fatalf("repairs=%#v", errorData)
		}
	}
	for _, args := range [][]string{
		{"--json", "config", "workspaces"},
		{"--json", "debug", "meta", "doc", "env"},
		{"--json", "debug", "meta", "commands"},
		{"--json", "doctor", "--help"},
	} {
		stdout.Reset()
		if exitCode := cli.Run(args); exitCode != 0 {
			t.Fatalf("offline command %v: exit=%d stdout=%s", args, exitCode, stdout.String())
		}
	}

}
