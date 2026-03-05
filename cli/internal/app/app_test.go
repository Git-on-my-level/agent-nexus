package app

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
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
	if int(data["status_code"].(float64)) != http.StatusCreated {
		t.Fatalf("unexpected status code payload: %#v", data)
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
