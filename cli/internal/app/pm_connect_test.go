package app

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func stubPMInstallConnect(t *testing.T) {
	t.Helper()
	old := pmServicePreflightConnect
	t.Cleanup(func() { pmServicePreflightConnect = old })
	pmServicePreflightConnect = func(*App, context.Context, config.Resolved, []string) error { return nil }
}

const mismatchBody = `{"error":{"code":"pm_actor_mismatch","message":"configured old; acting new","hint":"Rebind ANX_PM_AGENT_ACTOR_ID to new and restart anx-core.","details":{"configured_actor_id":"old","acting_actor_id":"new"},"recoverable":false}}`

func TestPMInstallPreflightsBeforeServiceMutation(t *testing.T) {
	for _, platform := range []string{"darwin", "linux"} {
		t.Run(platform, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
			oldOS, oldExec, oldAuth := pmServiceOS, pmServiceExec, pmServiceResolveAuth
			t.Cleanup(func() { pmServiceOS, pmServiceExec, pmServiceResolveAuth = oldOS, oldExec, oldAuth })
			pmServiceOS = platform
			pmServiceExec = func(context.Context, string, ...string) ([]byte, error) {
				t.Fatal("service manager called after failed preflight")
				return nil, nil
			}
			resolvedAs := ""
			pmServiceResolveAuth = func(_ *App, _ context.Context, cfg config.Resolved) (config.Resolved, error) {
				resolvedAs = cfg.As
				cfg.AccessToken = "test"
				return cfg, nil
			}
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/pm/connect" || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer test" {
					t.Errorf("unexpected preflight: %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(403)
				w.Write([]byte(mismatchBody))
			}))
			defer srv.Close()
			a := &App{UserHomeDir: func() (string, error) { return home, nil }}
			result, err := a.runPMService(context.Background(), "install", []string{"--runner", "echo {prompt}"}, config.Resolved{BaseURL: srv.URL, ConfigDir: filepath.Join(home, "credentials")})
			if err == nil || result != nil || calls != 1 || resolvedAs != "pm" {
				t.Fatalf("preflight: result=%+v err=%v calls=%d as=%q", result, err, calls, resolvedAs)
			}
			if errnorm.Normalize(err).Code != "pm_actor_mismatch" {
				t.Fatal(err)
			}
			result, err = a.runPMService(context.Background(), "status", nil, config.Resolved{BaseURL: srv.URL, ConfigDir: filepath.Join(home, "credentials")})
			if err != nil {
				t.Fatal(err)
			}
			status := result.Data.(pmServiceStatus)
			if status.Installed || status.Running || status.LastConnectFailure == nil || status.LastConnectFailure.Code != "pm_actor_mismatch" {
				t.Fatalf("failed install status: %+v", status)
			}
		})
	}
}

func TestPMInstallSuccessClearsStaleConnectFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	oldOS, oldExec, oldAuth := pmServiceOS, pmServiceExec, pmServiceResolveAuth
	t.Cleanup(func() { pmServiceOS, pmServiceExec, pmServiceResolveAuth = oldOS, oldExec, oldAuth })
	pmServiceOS = "linux"
	pmServiceResolveAuth = func(_ *App, _ context.Context, cfg config.Resolved) (config.Resolved, error) {
		cfg.AccessToken = "test"
		return cfg, nil
	}
	repaired := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pm/connect" || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer test" {
			t.Errorf("unexpected preflight: %s %s", r.Method, r.URL.Path)
		}
		if !repaired {
			w.WriteHeader(403)
			w.Write([]byte(mismatchBody))
			return
		}
		w.Write([]byte(`{"state":"connected"}`))
	}))
	defer srv.Close()
	pmServiceExec = func(context.Context, string, ...string) ([]byte, error) {
		if !repaired {
			t.Fatal("service manager called after failed preflight")
		}
		return []byte("active"), nil
	}
	a := &App{UserHomeDir: func() (string, error) { return home, nil }}
	cfg := config.Resolved{BaseURL: srv.URL, ConfigDir: filepath.Join(home, "credentials")}
	result, err := a.runPMService(context.Background(), "install", []string{"--runner", "echo {prompt}"}, cfg)
	if err == nil || result != nil || errnorm.Normalize(err).Code != "pm_actor_mismatch" {
		t.Fatalf("expected mismatch: %+v %v", result, err)
	}
	repaired = true
	result, err = a.runPMService(context.Background(), "install", []string{"--runner", "echo {prompt}"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	status := result.Data.(pmServiceStatus)
	if !status.Installed || status.LastConnectFailure != nil {
		t.Fatalf("successful install kept stale connect failure: %+v", status)
	}
	state := filepath.Join(home, "state", "anx", "pm", pmServiceID(cfg.BaseURL, cfg.ConfigDir, "pm"))
	raw, err := os.ReadFile(filepath.Join(state, "last-connect.json"))
	if err != nil || string(raw) != "null" {
		t.Fatalf("connect diagnostic = %q, %v", raw, err)
	}
	result, err = a.runPMService(context.Background(), "status", nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	status = result.Data.(pmServiceStatus)
	if status.LastConnectFailure != nil || strings.Contains(result.Text, "pm_actor_mismatch") || strings.Contains(result.Text, "Last connect failure") {
		t.Fatalf("status kept stale connect failure: %+v %s", status, result.Text)
	}
}

func TestPMConnectAuthorizationBackoffRecoveryAndStatus(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	oldOS, oldExec, oldSleep := pmServiceOS, pmServiceExec, sleepFn
	t.Cleanup(func() { pmServiceOS, pmServiceExec, sleepFn = oldOS, oldExec, oldSleep })
	pmServiceOS = "linux"
	pmServiceExec = func(context.Context, string, ...string) ([]byte, error) { return []byte("active"), nil }
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls <= 7 {
			w.WriteHeader(403)
			w.Write([]byte(mismatchBody))
			return
		}
		w.Write([]byte(`{"state":"connected"}`))
	}))
	defer srv.Close()
	cfg := config.Resolved{BaseURL: srv.URL, ConfigDir: filepath.Join(home, "credentials"), As: "pm", AccessToken: "test"}
	state := filepath.Join(home, "state", "anx", "pm", pmServiceID(cfg.BaseURL, cfg.ConfigDir, "pm"))
	if err := os.MkdirAll(state, 0700); err != nil {
		t.Fatal(err)
	}
	unit := filepath.Join(home, "config", "systemd", "user", filepath.Base(state)+".service")
	os.MkdirAll(filepath.Dir(unit), 0700)
	os.WriteFile(unit, []byte("test"), 0600)
	var logs bytes.Buffer
	a := &App{Stderr: &logs, UserHomeDir: func() (string, error) { return home, nil }}
	var delays []time.Duration
	sleepFn = func(ctx context.Context, d time.Duration) error {
		delays = append(delays, d)
		result, err := a.runPMService(ctx, "status", nil, cfg)
		if err != nil {
			t.Fatal(err)
		}
		status := result.Data.(pmServiceStatus)
		if status.LastConnectFailure == nil || status.LastConnectFailure.Code != "pm_actor_mismatch" || status.LastConnectFailure.Retryable || !strings.Contains(result.Text, "Last connect failure: pm_actor_mismatch") {
			t.Fatalf("status hides connect failure: %+v %s", status, result.Text)
		}
		return nil
	}
	if err := a.waitPMConnect(context.Background(), cfg, []string{"echo"}, state); err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 15 * time.Minute, 15 * time.Minute, 15 * time.Minute}
	if len(delays) != len(want) {
		t.Fatal(delays)
	}
	for i, d := range delays {
		if d != want[i] {
			t.Fatal(delays)
		}
	}
	failure, err := readPMConnectFailure(state)
	if err != nil || failure != nil {
		t.Fatalf("stale failure after recovery: %+v %v", failure, err)
	}
	if !strings.Contains(logs.String(), "ANX_PM_AGENT_ACTOR_ID") {
		t.Fatal(logs.String())
	}
}

func TestPMConnectCancellationAndOldCore(t *testing.T) {
	for _, status := range []int{401, 403, 404, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(status) }))
			defer srv.Close()
			old := sleepFn
			t.Cleanup(func() { sleepFn = old })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sleepFn = func(ctx context.Context, _ time.Duration) error { cancel(); return ctx.Err() }
			a := &App{Stderr: &bytes.Buffer{}}
			err := a.waitPMConnect(ctx, config.Resolved{BaseURL: srv.URL, AccessToken: "test"}, nil, t.TempDir())
			if status == 404 && err != nil {
				t.Fatal(err)
			}
			if status != 404 && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatal(calls)
			}
		})
	}
}

func TestPMServeDeniedConnectStaysAliveUntilCancellation(t *testing.T) {
	old := sleepFn
	t.Cleanup(func() { sleepFn = old })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	slept := false
	sleepFn = func(ctx context.Context, d time.Duration) error {
		if d != time.Minute {
			t.Fatalf("unexpected authorization backoff: %s", d)
		}
		slept = true
		cancel()
		return ctx.Err()
	}
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/pm/connect" {
			t.Errorf("claimed work before authorized: %s", r.URL.Path)
		}
		w.WriteHeader(403)
		w.Write([]byte(mismatchBody))
	}))
	defer srv.Close()
	a := &App{Stderr: &bytes.Buffer{}}
	result, err := a.runPMServe(ctx, []string{"--runner", "echo {prompt_file}", "--work-dir", t.TempDir()}, config.Resolved{BaseURL: srv.URL, AccessToken: "test", Agent: "pm"})
	if err != nil || !slept || calls != 1 || result == nil || !asBool(asMap(result.Data)["stopped"]) {
		t.Fatalf("serve did not wait for cancellation: %+v %v slept=%t calls=%d", result, err, slept, calls)
	}
}

func TestPMInstallConnectPreflightSelectedProfileAndAvailability(t *testing.T) {
	for _, statusCode := range []int{200, 404, 503} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
			oldOS, oldExec, oldAuth := pmServiceOS, pmServiceExec, pmServiceResolveAuth
			t.Cleanup(func() { pmServiceOS, pmServiceExec, pmServiceResolveAuth = oldOS, oldExec, oldAuth })
			pmServiceOS = "linux"
			connected := false
			managerCalls := 0
			pmServiceResolveAuth = func(_ *App, _ context.Context, cfg config.Resolved) (config.Resolved, error) {
				if cfg.As != "selected-pm" {
					t.Fatalf("wrong install identity: %q", cfg.As)
				}
				cfg.AccessToken = "selected-token"
				return cfg, nil
			}
			pmServiceExec = func(context.Context, string, ...string) ([]byte, error) {
				managerCalls++
				if !connected || statusCode == 503 {
					t.Error("manager mutation before accepted preflight")
				}
				return []byte("active"), nil
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/pm/connect" || r.Header.Get("Authorization") != "Bearer selected-token" {
					t.Errorf("unexpected preflight: %s", r.URL.Path)
				}
				connected = true
				w.WriteHeader(statusCode)
				if statusCode == 200 {
					w.Write([]byte(`{"state":"connected"}`))
				}
			}))
			defer srv.Close()
			a := &App{UserHomeDir: func() (string, error) { return home, nil }}
			result, err := a.runPMService(context.Background(), "install", []string{"--runner", "echo {prompt}"}, config.Resolved{BaseURL: srv.URL, ConfigDir: filepath.Join(home, "credentials"), As: "selected-pm"})
			if statusCode == 503 {
				if err == nil || result != nil || managerCalls != 0 {
					t.Fatalf("unavailable install: %+v %v calls=%d", result, err, managerCalls)
				}
			} else if err != nil || !result.Data.(pmServiceStatus).Installed || managerCalls == 0 || strings.Contains(result.Text, "PM connected") {
				t.Fatalf("preflight is not runtime health: %+v %v calls=%d", result, err, managerCalls)
			}
		})
	}
}

func TestPMConnectDiagnosticIsPrivateBoundedAndExcludesBody(t *testing.T) {
	dir := t.TempDir()
	err := errnorm.FromHTTPFailure(403, []byte(`{"error":{"code":"pm_actor_mismatch","message":"private response content","hint":"rebind ANX_PM_AGENT_ACTOR_ID"}}`))
	err.Hint += strings.Repeat("x", 20000)
	if e := recordPMConnect(dir, err); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "last-connect.json")
	info, e := os.Stat(path)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if info.Mode().Perm() != 0600 || len(raw) > 16*1024 || strings.Contains(string(raw), "private response content") {
		t.Fatalf("unsafe diagnostic: mode=%o size=%d", info.Mode().Perm(), len(raw))
	}
	if e = os.WriteFile(path, []byte(strings.Repeat("x", 16*1024+1)), 0600); e != nil {
		t.Fatal(e)
	}
	if failure, e := readPMConnectFailure(dir); e != nil || failure != nil {
		t.Fatalf("oversized diagnostic is not unknown: %+v %v", failure, e)
	}
}
