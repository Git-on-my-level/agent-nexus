package app

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-nexus-cli/internal/config"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	return newTestAppWithHome(t.TempDir())
}

func newTestAppWithHome(home string) *App {
	app := New()
	env := map[string]string{"HOME": home}
	app.Getenv = func(key string) string { return env[key] }
	app.UserHomeDir = func() (string, error) { return home, nil }
	app.runtimeIdentity = func() (*runtimeIdentityReport, error) { return nil, nil }
	return app
}

func TestDefaultTestAppIsIsolatedFromAgentEnvironment(t *testing.T) {
	for key, value := range map[string]string{
		"CLAUDECODE":            "1",
		"CODEX_THREAD_ID":       "x",
		"AGENTCTL_EXECUTION_ID": "y",
	} {
		t.Setenv(key, value)
	}

	home := t.TempDir()
	app := newTestAppWithHome(home)
	for _, key := range []string{"CLAUDECODE", "CODEX_THREAD_ID", "AGENTCTL_EXECUTION_ID"} {
		if got := app.Getenv(key); got != "" {
			t.Fatalf("test app inherited %s=%q", key, got)
		}
	}
	if got := app.Getenv("HOME"); got != home {
		t.Fatalf("test app HOME = %q, want isolated home %q", got, home)
	}
	if got, err := app.UserHomeDir(); err != nil || got != home {
		t.Fatalf("test app home = %q, err=%v; want %q", got, err, home)
	}
	if got, err := app.configDir(config.Resolved{}); err != nil || got != filepath.Join(home, ".config", "anx") {
		t.Fatalf("test app config/cache dir = %q, err=%v; want isolated home cache", got, err)
	}

	runTypedCommandUsageFailures(t, home)
	runDraftCreateHelpWithCommand(t, home)
}

func TestRunVersionJSON(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := newTestApp(t)
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = strings.NewReader("")
	cli.StdinIsTTY = func() bool { return true }
	cli.UserHomeDir = func() (string, error) { return "/home/tester", nil }
	cli.ReadFile = func(path string) ([]byte, error) {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}

	exitCode := cli.Run([]string{"--json", "version"})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d stderr=%s", exitCode, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode stdout json: %v", err)
	}
	if payload["ok"] != true {
		t.Fatalf("expected ok=true, payload=%#v", payload)
	}
	if payload["command"] != "version" {
		t.Fatalf("unexpected command: %#v", payload["command"])
	}
}

func TestRunVersionFlag(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := newTestApp(t)
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = strings.NewReader("")
	cli.StdinIsTTY = func() bool { return true }
	cli.UserHomeDir = func() (string, error) { return "/home/tester", nil }
	cli.ReadFile = func(path string) ([]byte, error) {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}

	exitCode := cli.Run([]string{"--version"})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "fact result.cli_version=") {
		t.Fatalf("expected version output, got stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunVersionFlagFalseShowsRootHelp(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := newTestApp(t)
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = strings.NewReader("")
	cli.StdinIsTTY = func() bool { return true }
	cli.UserHomeDir = func() (string, error) { return "/home/tester", nil }
	cli.ReadFile = func(path string) ([]byte, error) {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}

	exitCode := cli.Run([]string{"--version=false"})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Usage:") || !strings.Contains(stdout.String(), "anx [global flags] <command>") {
		t.Fatalf("expected root help output, got stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "CLI version:") {
		t.Fatalf("did not expect version output, got stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunVersionAcceptsTrailingJSONFlag(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := newTestApp(t)
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = strings.NewReader("")
	cli.StdinIsTTY = func() bool { return true }
	cli.UserHomeDir = func() (string, error) { return "/home/tester", nil }
	cli.ReadFile = func(path string) ([]byte, error) {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}

	exitCode := cli.Run([]string{"version", "--json"})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d stderr=%s stdout=%s", exitCode, stderr.String(), stdout.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode stdout json: %v", err)
	}
	if payload["ok"] != true || payload["command"] != "version" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestRunMetaDocsIsConfigLenient(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	profilesDir := filepath.Join(home, ".config", "anx", "profiles")
	if err := os.MkdirAll(profilesDir, 0o700); err != nil {
		t.Fatalf("mkdir profiles dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "agent-a.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write profile a: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "agent-b.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write profile b: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := newTestApp(t)
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = strings.NewReader("")
	cli.StdinIsTTY = func() bool { return true }
	cli.UserHomeDir = func() (string, error) { return home, nil }
	cli.ReadFile = os.ReadFile

	exitCode := cli.Run([]string{"debug", "meta", "docs"})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d stderr=%s stdout=%s", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "# ANX Runtime Help Reference") {
		t.Fatalf("expected runtime docs output=%s", stdout.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected no stderr, got %q", stderr.String())
	}
}

func TestRunSubcommandTrailingHelpIsConfigLenientWithMultipleProfiles(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	profilesDir := filepath.Join(home, ".config", "anx", "profiles")
	if err := os.MkdirAll(profilesDir, 0o700); err != nil {
		t.Fatalf("mkdir profiles dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "agent-a.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write profile a: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "agent-b.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write profile b: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := newTestApp(t)
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = strings.NewReader("")
	cli.StdinIsTTY = func() bool { return true }
	cli.UserHomeDir = func() (string, error) { return home, nil }
	cli.ReadFile = os.ReadFile

	exitCode := cli.Run([]string{"--json", "--base-url", "http://127.0.0.1:9", "debug", "inbox", "respond", "--help"})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d stderr=%s stdout=%s", exitCode, stderr.String(), stdout.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode stdout json: %v", err)
	}
	if payload["ok"] != true {
		t.Fatalf("expected ok=true payload=%#v", payload)
	}
	data, _ := payload["result"].(map[string]any)
	helpText, _ := data["help_text"].(string)
	if !strings.Contains(helpText, "inbox respond") {
		t.Fatalf("expected inbox respond help in output, got %q", helpText)
	}
}

func TestRunMetaUtilityCommandsDispatchGeneratedEndpoints(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/livez":
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/readyz":
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/ops/health":
			if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer access-token-1" {
				t.Fatalf("expected auth header for /ops/health, got %q", got)
			}
			_, _ = w.Write([]byte(`{"ok":true,"projection_maintenance":{"pending_dirty_count":1}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	writeDerivedAgentFixture(t, home, "agent-a", `{"base_url":"`+server.URL+`","access_token":"access-token-1","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

	livezRaw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "--as", "agent-a", "debug", "meta", "livez"})
	livezPayload := assertEnvelopeOK(t, livezRaw)
	if got := anyStringValue(livezPayload["command"]); got != "debug meta livez" {
		t.Fatalf("unexpected livez envelope: %#v", livezPayload)
	}

	opsRaw := runCLIForTest(t, home, map[string]string{"ANX_ACCESS_TOKEN": "access-token-1"}, nil, []string{"--json", "--as", "agent-a", "debug", "meta", "ops", "health"})
	opsPayload := assertEnvelopeOK(t, opsRaw)
	if got := anyStringValue(opsPayload["command"]); got != "debug meta ops health" {
		t.Fatalf("unexpected ops health envelope: %#v", opsPayload)
	}
}

func TestRunTrailingGlobalBaseURLIsAccepted(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := newTestApp(t)
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = strings.NewReader("")
	cli.StdinIsTTY = func() bool { return true }
	cli.UserHomeDir = func() (string, error) { return "/home/tester", nil }
	cli.ReadFile = func(path string) ([]byte, error) {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}

	exitCode := cli.Run([]string{"version", "--base-url", "http://127.0.0.1:8000"})
	if exitCode != 0 {
		t.Fatalf("expected trailing global flag to work, got exit %d stderr=%s", exitCode, stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected no stderr for trailing global flag, got %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "fact result.cli_version=") || !strings.Contains(stdout.String(), "fact result.base_url=http://127.0.0.1:8000") {
		t.Fatalf("expected version output with trailing global flag, got %q", stdout.String())
	}
}

func TestRunTrailingGlobalBaseURLPreservesJSONMode(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/readyz":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/meta/handshake":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"min_cli_version":"0.1.0"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := newTestApp(t)
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = strings.NewReader("")
	cli.StdinIsTTY = func() bool { return true }
	cli.UserHomeDir = func() (string, error) { return "/home/tester", nil }
	cli.ReadFile = func(path string) ([]byte, error) {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}

	exitCode := cli.Run([]string{"doctor", "--base-url", server.URL, "--json"})
	if exitCode != 0 {
		t.Fatalf("expected trailing global flag to work in json mode, got %d stderr=%s stdout=%s", exitCode, stderr.String(), stdout.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected stderr to stay empty in --json mode, got %q", stderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode stdout json: %v raw=%s", err, stdout.String())
	}
	if payload["ok"] != true {
		t.Fatalf("expected ok=true payload=%#v", payload)
	}
}

func TestRunTrailingFlagParseErrorsPreserveJSONMode(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := newTestApp(t)
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = strings.NewReader("")
	cli.StdinIsTTY = func() bool { return true }
	cli.UserHomeDir = func() (string, error) { return "/home/tester", nil }
	cli.ReadFile = func(path string) ([]byte, error) {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}

	exitCode := cli.Run([]string{"doctor", "--timeout", "bad", "--json"})
	if exitCode != 2 {
		t.Fatalf("expected usage exit code 2, got %d stderr=%s stdout=%s", exitCode, stderr.String(), stdout.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected stderr to stay empty in trailing json mode, got %q", stderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode stdout json: %v raw=%s", err, stdout.String())
	}
	if payload["ok"] != false {
		t.Fatalf("expected ok=false payload=%#v", payload)
	}
	errorObj, _ := payload["error"].(map[string]any)
	if anyStringValue(errorObj["code"]) != "invalid_flags" {
		t.Fatalf("expected invalid_flags payload=%#v", payload)
	}
	if !strings.Contains(anyStringValue(errorObj["message"]), "invalid value for --timeout") {
		t.Fatalf("expected timeout parse failure in JSON payload=%#v", payload)
	}
}

func TestParseGlobalFlagsSupportsTrailingValueAndBoolFlags(t *testing.T) {
	t.Parallel()

	overrides, remaining, helpRequested, err := parseGlobalFlags([]string{
		"doctor",
		"--base-url", "http://127.0.0.1:8000",
		"--as", "agent-late",
		"--headers",
		"--timeout", "2s",
	})
	if err != nil {
		t.Fatalf("parseGlobalFlags: %v", err)
	}
	if helpRequested {
		t.Fatalf("did not expect helpRequested")
	}
	if len(remaining) != 1 || remaining[0] != "doctor" {
		t.Fatalf("expected remaining command to stay intact, got %#v", remaining)
	}
	if overrides.BaseURL == nil || *overrides.BaseURL != "http://127.0.0.1:8000" {
		t.Fatalf("expected trailing base-url override, got %#v", overrides)
	}
	if overrides.As == nil || *overrides.As != "agent-late" {
		t.Fatalf("expected trailing agent override, got %#v", overrides)
	}
	if overrides.Headers == nil || !*overrides.Headers {
		t.Fatalf("expected trailing headers bool override, got %#v", overrides)
	}
	if overrides.Timeout == nil || *overrides.Timeout != 2*time.Second {
		t.Fatalf("expected trailing timeout override, got %#v", overrides)
	}
}

func TestRunDoctorJSON(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/readyz":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/meta/handshake":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"min_cli_version":"0.1.0"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := newTestApp(t)
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = strings.NewReader("")
	cli.StdinIsTTY = func() bool { return true }
	cli.UserHomeDir = func() (string, error) { return "/home/tester", nil }
	cli.ReadFile = func(path string) ([]byte, error) {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}

	exitCode := cli.Run([]string{"--json", "--base-url", server.URL, "doctor"})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d stderr=%s stdout=%s", exitCode, stderr.String(), stdout.String())
	}

	var payload struct {
		OK   bool `json:"ok"`
		Data struct {
			Checks []map[string]any `json:"checks"`
		} `json:"result"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode doctor json: %v", err)
	}
	if !payload.OK {
		t.Fatalf("expected doctor ok=true, got %#v", payload)
	}
	if len(payload.Data.Checks) < 4 {
		t.Fatalf("expected 4 checks, got %d", len(payload.Data.Checks))
	}
}

func doctorCheckOK(checks []any, name string) bool {
	for _, raw := range checks {
		check, _ := raw.(map[string]any)
		if anyStringValue(check["name"]) == name {
			ok, _ := check["ok"].(bool)
			return ok
		}
	}
	return false
}

func doctorCheckMessageContains(checks []any, name string, needle string) bool {
	for _, raw := range checks {
		check, _ := raw.(map[string]any)
		if anyStringValue(check["name"]) == name {
			return strings.Contains(anyStringValue(check["message"]), needle)
		}
	}
	return false
}

func TestRunAPICallJSONWithStdinBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/echo" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"received":` + string(body) + `}`))
	}))
	defer server.Close()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := newTestApp(t)
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = strings.NewReader(`{"hello":"world"}`)
	cli.StdinIsTTY = func() bool { return false }
	cli.UserHomeDir = func() (string, error) { return "/home/tester", nil }
	cli.ReadFile = func(path string) ([]byte, error) {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}

	exitCode := cli.Run([]string{"--json", "--base-url", server.URL, "api", "call", "--method", "POST", "--path", "/echo"})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d stderr=%s stdout=%s", exitCode, stderr.String(), stdout.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode api call json: %v", err)
	}
	if payload["ok"] != true {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	data, _ := payload["result"].(map[string]any)
	if data == nil {
		t.Fatalf("unexpected nil data payload: %#v", payload)
	}
}

func TestRunAPICallJSONWithFromFileBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/echo" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"received":` + string(body) + `}`))
	}))
	defer server.Close()

	requestFile := filepath.Join(t.TempDir(), "body.json")
	if err := os.WriteFile(requestFile, []byte(`{"hello":"file"}`), 0o600); err != nil {
		t.Fatalf("write request file: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := newTestApp(t)
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = strings.NewReader("")
	cli.StdinIsTTY = func() bool { return true }
	cli.UserHomeDir = func() (string, error) { return "/home/tester", nil }
	cli.ReadFile = os.ReadFile

	exitCode := cli.Run([]string{"--json", "--base-url", server.URL, "api", "call", "--method", "POST", "--path", "/echo", "--from-file", requestFile})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d stderr=%s stdout=%s", exitCode, stderr.String(), stdout.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode api call json: %v", err)
	}
	if payload["ok"] != true {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestRunAPICallTextProjection(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/plain" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("raw-response"))
	}))
	defer server.Close()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := newTestApp(t)
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = strings.NewReader("")
	cli.StdinIsTTY = func() bool { return true }
	cli.UserHomeDir = func() (string, error) { return "/home/tester", nil }
	cli.ReadFile = func(path string) ([]byte, error) {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}

	exitCode := cli.Run([]string{"--base-url", server.URL, "api", "call", "--path", "/plain"})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "fact result.body=raw-response") {
		t.Fatalf("unexpected text projection: %q", stdout.String())
	}
}

func TestRunAPICallUsageFailureExitCode2(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := newTestApp(t)
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = strings.NewReader("")
	cli.StdinIsTTY = func() bool { return true }
	cli.UserHomeDir = func() (string, error) { return "/home/tester", nil }
	cli.ReadFile = func(path string) ([]byte, error) {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}

	exitCode := cli.Run([]string{"--json", "api", "call", "--method", "POST"})
	if exitCode != 2 {
		t.Fatalf("expected usage exit code 2, got %d", exitCode)
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode usage error payload: %v", err)
	}
	if payload["ok"] != false {
		t.Fatalf("expected ok=false payload=%#v", payload)
	}
}

func TestAPICallHelpRunsWithoutResolvableConfig(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := newTestApp(t)
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = strings.NewReader("")
	cli.StdinIsTTY = func() bool { return true }
	env := map[string]string{}
	cli.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	cli.ReadFile = func(path string) ([]byte, error) {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}
	cli.Getenv = func(key string) string { return env[key] }

	exitCode := cli.Run([]string{"api", "call", "--help"})
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d stderr=%s", exitCode, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "Local Help: api call") || !strings.Contains(out, "--path") {
		t.Fatalf("expected api call help text, got:\n%s", out)
	}
}
