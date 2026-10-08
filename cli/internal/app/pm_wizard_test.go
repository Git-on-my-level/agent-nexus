package app

import (
	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPMWizardChoicePrivatePromptAndCancellation(t *testing.T) {
	previousAuth := pmServiceResolveAuth
	defer func() { pmServiceResolveAuth = previousAuth }()
	pmServiceResolveAuth = func(_ *App, _ context.Context, cfg config.Resolved) (config.Resolved, error) { return cfg, nil }
	home := t.TempDir()
	dir := filepath.Join(home, "config")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "workspaces.json"), []byte(`{"aliases":{"one":"https://one.test","two":"https://two.test"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	a := newTestAppWithHome(home)
	a.Stdin = strings.NewReader("2\n2\n")
	var diagnostics bytes.Buffer
	a.Stderr = &diagnostics
	cfg := config.Resolved{ConfigDir: dir, Sources: map[string]string{}}
	previous := runCmd
	defer func() { runCmd = previous }()
	count := 0
	var promptPath string
	runCmd = func(ctx context.Context, name string, args []string, work string, env []string) ([]byte, []byte, error) {
		count++
		promptPath = args[len(args)-1]
		f, err := os.Stat(promptPath)
		if err != nil {
			t.Fatal(err)
		}
		if f.Mode().Perm() != 0600 {
			t.Fatal("prompt permissions")
		}
		prompt, err := os.ReadFile(promptPath)
		if err != nil {
			t.Fatal(err)
		}
		for _, arg := range args {
			if strings.Contains(arg, string(prompt)) {
				t.Fatal("prompt in argv")
			}
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("unbounded test")
		}
		return []byte("PM runner ready"), []byte("fake-secret-not-for-diagnostics"), nil
	}
	selected, runner, err := a.pmInstallWizard(context.Background(), cfg)
	if err != nil || selected.BaseURL != "https://two.test" || selected.As != "pm" || !strings.Contains(runner, "claude") || count != 1 {
		t.Fatalf("wizard %s %s %d %v", selected.BaseURL, runner, count, err)
	}
	if strings.Contains(diagnostics.String(), "fake-secret") {
		t.Fatal("runner output leaked")
	}
	if _, err = os.Stat(promptPath); !os.IsNotExist(err) {
		t.Fatal("test prompt retained")
	}
	a.Stdin = strings.NewReader("1\n")
	if _, _, err = a.pmInstallWizard(context.Background(), cfg); err == nil || count != 1 {
		t.Fatal("cancelled wizard ran a runner", err)
	}
}

func TestPMWizardRunnerFailureDoesNotPrintOutput(t *testing.T) {
	a := newTestApp(t)
	var stderr bytes.Buffer
	a.Stderr = &stderr
	a.Stdout = io.Discard
	previous := runCmd
	defer func() { runCmd = previous }()
	runCmd = func(context.Context, string, []string, string, []string) ([]byte, []byte, error) {
		return []byte("fake-secret"), []byte("fake-secret"), errors.New("fake-secret")
	}
	err := a.testPMRunner(context.Background(), config.Resolved{}, []string{"runner", "{prompt_file}"})
	if err == nil || strings.Contains(err.Error(), "fake-secret") || strings.Contains(stderr.String(), "fake-secret") {
		t.Fatal("unsafe failure", err)
	}
}

func TestPMConnectionWaitSuccessAndTimeout(t *testing.T) {
	previousAuth := pmServiceResolveAuth
	defer func() { pmServiceResolveAuth = previousAuth }()
	pmServiceResolveAuth = func(_ *App, _ context.Context, cfg config.Resolved) (config.Resolved, error) { return cfg, nil }
	state := t.TempDir()
	since := time.Now()
	connected := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if connected {
			fmt.Fprintf(w, `{"state":"connected","last_seen":%q}`, time.Now().UTC().Format(time.RFC3339Nano))
		} else {
			io.WriteString(w, `{"state":"not_onboarded"}`)
		}
	}))
	defer server.Close()
	a := newTestApp(t)
	cfg := config.Resolved{BaseURL: server.URL, AccessToken: "fixture", Timeout: time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := a.waitPMConnection(ctx, cfg, time.Second, state, since); err == nil || errnorm.ExitCode(err) != 8 {
		t.Fatalf("unconfirmed connection %v", err)
	}
	connected = true
	// Another computer's fresh connection must not complete this local install.
	staleCtx, staleCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	if err := a.waitPMConnection(staleCtx, cfg, time.Second, state, since); err == nil {
		t.Fatal("remote PM satisfied a local install")
	}
	staleCancel()
	if err := os.WriteFile(filepath.Join(state, "last-claim"), []byte(since.Add(-time.Minute).UTC().Format(time.RFC3339Nano)), 0600); err != nil {
		t.Fatal(err)
	}
	staleCtx, staleCancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
	if err := a.waitPMConnection(staleCtx, cfg, time.Second, state, since); err == nil {
		t.Fatal("old local claim satisfied a new install")
	}
	staleCancel()
	if err := os.WriteFile(filepath.Join(state, "last-claim"), []byte(time.Now().UTC().Format(time.RFC3339Nano)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.waitPMConnection(context.Background(), cfg, time.Second, state, since); err != nil {
		t.Fatal(err)
	}
}

func TestPMInstallNoFlagsWizardPrecedesAmbiguousWorkspaceResolution(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "anx")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "workspaces.json"), []byte(`{"aliases":{"one":"https://one.test","two":"https://two.test"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	a := newTestAppWithHome(home)
	a.StdinIsTTY = func() bool { return true }
	a.Stdin = strings.NewReader("")
	var stderr bytes.Buffer
	a.Stderr = &stderr
	if code := a.Run([]string{"pm", "install"}); code == 0 || !strings.Contains(stderr.String(), "Workspace number:") || strings.Contains(stderr.String(), "workspace_ambiguous") {
		t.Fatalf("wizard blocked by ambiguity: %d %s", code, stderr.String())
	}
}
