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
)

func TestRunVersionJSON(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := New()
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

func TestRunVersionUsesProfileJSONDefault(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	profilesDir := filepath.Join(home, ".config", "oar", "profiles")
	if err := os.MkdirAll(profilesDir, 0o700); err != nil {
		t.Fatalf("mkdir profiles dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "agent-a.json"), []byte(`{"base_url":"http://profile:8000","json":true}`), 0o600); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := New()
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = strings.NewReader("")
	cli.StdinIsTTY = func() bool { return true }
	cli.UserHomeDir = func() (string, error) { return home, nil }
	cli.ReadFile = os.ReadFile

	exitCode := cli.Run([]string{"--agent", "agent-a", "version"})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d stderr=%s", exitCode, stderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode stdout json: %v", err)
	}
	if payload["ok"] != true {
		t.Fatalf("expected ok=true payload=%#v", payload)
	}
	data, _ := payload["data"].(map[string]any)
	if data == nil || data["base_url"] != "http://profile:8000" {
		t.Fatalf("unexpected version payload: %#v", payload)
	}
}

func TestRunVersionAcceptsTrailingJSONFlag(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := New()
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

func TestRunTrailingGlobalBaseURLIsAccepted(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := New()
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
	if !strings.Contains(stdout.String(), "CLI version:") || !strings.Contains(stdout.String(), "Base URL: http://127.0.0.1:8000") {
		t.Fatalf("expected version output with trailing global flag, got %q", stdout.String())
	}
}

func TestRunTrailingGlobalBaseURLPreservesJSONMode(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
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
	cli := New()
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
	cli := New()
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
		"--agent", "agent-late",
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
	if overrides.Agent == nil || *overrides.Agent != "agent-late" {
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
		case "/health":
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
	cli := New()
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
		} `json:"data"`
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
	cli := New()
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
	data, _ := payload["data"].(map[string]any)
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
	cli := New()
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

func TestRunAPICallRaw(t *testing.T) {
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
	cli := New()
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = strings.NewReader("")
	cli.StdinIsTTY = func() bool { return true }
	cli.UserHomeDir = func() (string, error) { return "/home/tester", nil }
	cli.ReadFile = func(path string) ([]byte, error) {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}

	exitCode := cli.Run([]string{"--base-url", server.URL, "api", "call", "--raw", "--path", "/plain"})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d stderr=%s", exitCode, stderr.String())
	}
	if stdout.String() != "raw-response" {
		t.Fatalf("unexpected raw output: %q", stdout.String())
	}
}

func TestRunAPICallUsageFailureExitCode2(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := New()
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
