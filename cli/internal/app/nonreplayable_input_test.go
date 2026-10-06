package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"agent-nexus-cli/internal/config"
)

func TestInputFileMarksNonRegularStreams(t *testing.T) {
	t.Parallel()
	app := newTestApp(t)
	app.ReadFile = func(string) ([]byte, error) { return []byte("document body"), nil }

	info, err := os.Stat("/dev/stdin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.readRawFile("/dev/stdin"); err != nil {
		t.Fatal(err)
	}
	if info.Mode().IsRegular() == app.stdinConsumed {
		t.Fatalf("/dev/stdin regular=%v consumed=%v", info.Mode().IsRegular(), app.stdinConsumed)
	}

	fifoApp := newTestApp(t)
	fifoApp.ReadFile = func(string) ([]byte, error) { return []byte("from fifo"), nil }
	fifo := filepath.Join(t.TempDir(), "input.fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := fifoApp.readRawFile(fifo)
	if err != nil || string(data) != "from fifo" || !fifoApp.stdinConsumed {
		t.Fatalf("data=%q err=%v consumed=%v", data, err, fifoApp.stdinConsumed)
	}
	fromFile := newTestApp(t)
	fromFile.ReadFile = func(string) ([]byte, error) { return []byte(`{"ok":true}`), nil }
	if _, err := fromFile.readBodyInput(fifo); err != nil || !fromFile.stdinConsumed {
		t.Fatalf("from-file fifo err=%v consumed=%v", err, fromFile.stdinConsumed)
	}

	regular := newTestApp(t)
	path := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(path, []byte("notes"), 0o600); err != nil {
		t.Fatal(err)
	}
	regular.ReadFile = os.ReadFile
	got, err := regular.readRawFile(path)
	if err != nil || string(got) != "notes" || regular.stdinConsumed {
		t.Fatalf("data=%q err=%v consumed=%v", got, err, regular.stdinConsumed)
	}
}

func TestDocsCreateBodyFileFIFODoesNotReplayEmpty(t *testing.T) {
	managedUpdateFixture(t)
	fifo := filepath.Join(t.TempDir(), "body.fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	wrote := make(chan error, 1)
	go func() {
		file, err := os.OpenFile(fifo, os.O_WRONLY, 0)
		if err != nil {
			wrote <- err
			return
		}
		_, err = file.WriteString("pinned dashboard\n")
		_ = file.Close()
		wrote <- err
	}()

	var posted []byte
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body, _ := io.ReadAll(r.Body)
		posted = append([]byte(nil), body...)
		hits++
		w.WriteHeader(http.StatusUpgradeRequired)
		_, _ = w.Write([]byte(`{"error":{"code":"cli_outdated","message":"CLI version is below the minimum compatible version"},"upgrade":{"min_cli_version":"v9.0.0","recommended_cli_version":"v9.1.0"}}`))
	}))
	t.Cleanup(server.Close)

	updates := 0
	oldUpdate := runVerifiedSelfUpdate
	runVerifiedSelfUpdate = func(*App, context.Context, config.Resolved, string) (*commandResult, error) {
		updates++
		return &commandResult{Data: map[string]any{"updated": true}}, nil
	}
	t.Cleanup(func() { runVerifiedSelfUpdate = oldUpdate })
	execs := 0
	oldExec := execUpdatedCommand
	execUpdatedCommand = func(string, []string, []string) (int, error) {
		execs++
		return 0, nil
	}
	t.Cleanup(func() { execUpdatedCommand = oldExec })

	home := t.TempDir()
	writeDerivedAgentFixture(t, home, "agent-a", `{"agent":"agent-a","actor_id":"actor_a","base_url":"`+server.URL+`","access_token":"token","access_token_expires_at":"2099-01-01T00:00:00Z"}`)
	raw := runCLIForTest(t, home, map[string]string{"ANX_UPDATE_POLICY": "auto"}, strings.NewReader(""), []string{
		"--json", "--base-url", server.URL, "docs", "create", "--title", "Dashboard", "--body-file", fifo,
	})
	if err := <-wrote; err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	_ = json.Unmarshal(posted, &payload)
	if updates != 0 || execs != 0 || hits != 1 || payload["content"] != "pinned dashboard\n" || !strings.Contains(raw, "anx update --version v9.1.0") {
		t.Fatalf("updates=%d execs=%d hits=%d content=%#v stdout=%s", updates, execs, hits, payload["content"], raw)
	}
}
