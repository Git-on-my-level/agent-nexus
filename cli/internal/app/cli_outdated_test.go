package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func TestCLIOutdatedRecoveryPolicies(t *testing.T) {
	body := []byte(`{"error":{"code":"cli_outdated","message":"CLI version is below the minimum compatible version for this core instance","recoverable":true},"upgrade":{"min_cli_version":"v9.0.0","recommended_cli_version":"v9.1.0"}}`)
	cases := []struct {
		name    string
		managed bool
		policy  string
		retry   bool
		wantRun bool
	}{
		{name: "managed auto", managed: true, policy: "auto", wantRun: true},
		{name: "managed notify", managed: true, policy: "notify"},
		{name: "managed off", managed: true, policy: "off"},
		{name: "unmanaged auto", managed: false, policy: "auto"},
		{name: "managed auto already retried", managed: true, policy: "auto", retry: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var app *App
			var cfg config.Resolved
			if tc.managed {
				app, cfg, _ = managedUpdateFixture(t)
			} else {
				app = newTestApp(t)
				home := t.TempDir()
				app.UserHomeDir = func() (string, error) { return home, nil }
				app.Getenv = func(string) string { return "" }
				cfg = config.Resolved{ConfigDir: t.TempDir(), Timeout: 1}
			}
			env := map[string]string{"ANX_UPDATE_POLICY": tc.policy}
			if tc.retry {
				env[cliOutdatedRetryEnv] = "1"
			}
			app.Getenv = func(key string) string { return env[key] }
			updates := 0
			gotVersion := ""
			oldUpdate := runVerifiedSelfUpdate
			runVerifiedSelfUpdate = func(_ *App, _ context.Context, _ config.Resolved, version string) (*commandResult, error) {
				updates++
				gotVersion = version
				return &commandResult{Data: map[string]any{"updated": true, "target_version": "v9.1.0"}}, nil
			}
			t.Cleanup(func() { runVerifiedSelfUpdate = oldUpdate })
			execs := 0
			oldExec := execUpdatedCommand
			execUpdatedCommand = func(path string, args []string, env []string) (int, error) {
				execs++
				if path == "" || !strings.Contains(strings.Join(env, "\n"), cliOutdatedRetryEnv+"=1") {
					t.Fatalf("retry path=%q env=%v", path, env)
				}
				if strings.Join(args, " ") != "--json work list" {
					t.Fatalf("retry args=%v", args)
				}
				return 0, nil
			}
			t.Cleanup(func() { execUpdatedCommand = oldExec })

			code, nextErr, retried := app.recoverCLIOutdated(context.Background(), []string{"--json", "work", "list"}, cfg, "work list", errnorm.FromHTTPFailure(426, body))
			if tc.wantRun {
				if !retried || code != 0 || nextErr != nil || updates != 1 || execs != 1 || gotVersion != "v9.1.0" {
					t.Fatalf("retried=%v code=%d err=%v updates=%d execs=%d version=%s", retried, code, nextErr, updates, execs, gotVersion)
				}
				return
			}
			if retried || updates != 0 || execs != 0 {
				t.Fatalf("updated unmanaged or non-auto install: retried=%v updates=%d execs=%d", retried, updates, execs)
			}
			if nextErr == nil || !strings.Contains(nextErr.Error(), "anx update --version v9.1.0") {
				t.Fatalf("error=%v", nextErr)
			}
			typed := errnorm.Normalize(nextErr)
			if anyString(asMap(typed.Details)["recommended_cli_version"]) != "v9.1.0" {
				t.Fatalf("details=%#v", typed.Details)
			}
		})
	}
}

func TestDoctorManagedAutoRetriesOnce(t *testing.T) {
	app, _, _ := managedUpdateFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/readyz":
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/meta/handshake":
			_, _ = w.Write([]byte(`{"min_cli_version":"v99.0.0","recommended_cli_version":"v99.1.0"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	updates := 0
	oldUpdate := runVerifiedSelfUpdate
	runVerifiedSelfUpdate = func(_ *App, _ context.Context, _ config.Resolved, version string) (*commandResult, error) {
		updates++
		if version != "v99.1.0" {
			t.Fatalf("update version=%s", version)
		}
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

	stdout := &strings.Builder{}
	app.Stdout = stdout
	app.Stderr = &strings.Builder{}
	app.Getenv = func(key string) string {
		if key == "ANX_UPDATE_POLICY" {
			return "auto"
		}
		return ""
	}
	code := app.Run([]string{"--json", "--base-url", server.URL, "doctor"})
	if code != 0 || updates != 1 || execs != 1 {
		t.Fatalf("code=%d updates=%d execs=%d stdout=%s", code, updates, execs, stdout.String())
	}
}

func TestCLIOutdatedAutoDoesNotDowngrade(t *testing.T) {
	app, cfg, _ := managedUpdateFixture(t)
	app.Getenv = func(key string) string {
		if key == "ANX_UPDATE_POLICY" {
			return "auto"
		}
		return ""
	}
	gotVersion := "unset"
	oldUpdate := runVerifiedSelfUpdate
	runVerifiedSelfUpdate = func(_ *App, _ context.Context, _ config.Resolved, version string) (*commandResult, error) {
		gotVersion = version
		return &commandResult{Data: map[string]any{"already_current": true}}, nil
	}
	t.Cleanup(func() { runVerifiedSelfUpdate = oldUpdate })
	oldExec := execUpdatedCommand
	execUpdatedCommand = func(string, []string, []string) (int, error) {
		t.Fatal("already-current install retried the outdated command")
		return 0, nil
	}
	t.Cleanup(func() { execUpdatedCommand = oldExec })
	body := []byte(`{"error":{"code":"cli_outdated","message":"CLI version is below the minimum compatible version"},"upgrade":{"recommended_cli_version":"v0.0.1"}}`)
	_, nextErr, retried := app.recoverCLIOutdated(context.Background(), []string{"version"}, cfg, "version", errnorm.FromHTTPFailure(426, body))
	if retried || gotVersion != "" || nextErr == nil || !strings.Contains(nextErr.Error(), "anx update --version v0.0.1") {
		t.Fatalf("retried=%v version=%q err=%v", retried, gotVersion, nextErr)
	}
}

func TestCLIOutdatedNotifyUsesMinimumWhenRecommendedMissing(t *testing.T) {
	app, cfg, _ := managedUpdateFixture(t)
	app.Getenv = func(key string) string {
		if key == "ANX_UPDATE_POLICY" {
			return "notify"
		}
		return ""
	}
	body := []byte(`{"error":{"code":"cli_outdated","message":"CLI version is below the minimum compatible version"},"upgrade":{"min_cli_version":"v9.0.0"}}`)
	_, nextErr, retried := app.recoverCLIOutdated(context.Background(), []string{"work", "list"}, cfg, "work list", errnorm.FromHTTPFailure(426, body))
	if retried || nextErr == nil || !strings.Contains(nextErr.Error(), "anx update --version v9.0.0") {
		t.Fatalf("retried=%v err=%v", retried, nextErr)
	}
}

func TestAPICallCLIOutdatedUpdatesAndRetriesOnce(t *testing.T) {
	app, _, _ := managedUpdateFixture(t)
	docsHits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/docs" {
			http.NotFound(w, r)
			return
		}
		docsHits++
		if docsHits == 1 {
			w.WriteHeader(http.StatusUpgradeRequired)
			_, _ = w.Write([]byte(`{"error":{"code":"cli_outdated","message":"CLI version is below the minimum compatible version"},"upgrade":{"min_cli_version":"v9.0.0","recommended_cli_version":"v9.1.0"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"documents":[]}`))
	}))
	t.Cleanup(server.Close)

	updates := 0
	oldUpdate := runVerifiedSelfUpdate
	runVerifiedSelfUpdate = func(_ *App, _ context.Context, _ config.Resolved, version string) (*commandResult, error) {
		updates++
		if version != "v9.1.0" {
			t.Fatalf("update version=%s", version)
		}
		return &commandResult{Data: map[string]any{"updated": true}}, nil
	}
	t.Cleanup(func() { runVerifiedSelfUpdate = oldUpdate })
	execs := 0
	oldExec := execUpdatedCommand
	execUpdatedCommand = func(_ string, args []string, env []string) (int, error) {
		execs++
		if execs > 1 {
			t.Fatal("retried more than once")
		}
		if !strings.Contains(strings.Join(env, "\n"), cliOutdatedRetryEnv+"=1") {
			t.Fatalf("retry env=%v", env)
		}
		child := *app
		child.Getenv = func(key string) string {
			for _, item := range env {
				name, value, ok := strings.Cut(item, "=")
				if ok && name == key {
					return value
				}
			}
			return ""
		}
		return child.Run(args), nil
	}
	t.Cleanup(func() { execUpdatedCommand = oldExec })

	app.Stdout = &strings.Builder{}
	app.Stderr = &strings.Builder{}
	app.Getenv = func(key string) string {
		if key == "ANX_UPDATE_POLICY" {
			return "auto"
		}
		return ""
	}
	code := app.Run([]string{"--json", "--base-url", server.URL, "api", "call", "--path", "/docs"})
	if code != 0 || updates != 1 || execs != 1 || docsHits != 2 {
		t.Fatalf("code=%d updates=%d execs=%d hits=%d", code, updates, execs, docsHits)
	}
}

func TestSecretCreateFromStdinDoesNotReplayEmpty(t *testing.T) {
	managedUpdateFixture(t)
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
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
	raw := runCLIForTest(t, home, map[string]string{"ANX_UPDATE_POLICY": "auto"}, strings.NewReader("super-secret\n"), []string{
		"--json", "--base-url", server.URL, "secret", "create", "--from-stdin", "OPENAI_API_KEY",
	})
	if updates != 0 || execs != 0 || hits != 1 || !strings.Contains(raw, "anx update --version v9.1.0") || strings.Contains(raw, "secret value must not be empty") {
		t.Fatalf("updates=%d execs=%d hits=%d stdout=%s", updates, execs, hits, raw)
	}
}

func TestReadStdinBodyMarksConsumed(t *testing.T) {
	app := newTestApp(t)
	app.Stdin = strings.NewReader("important document body")
	app.StdinIsTTY = func() bool { return false }
	data, err := app.readStdinBody()
	if err != nil || string(data) != "important document body" || !app.stdinConsumed {
		t.Fatalf("data=%q err=%v consumed=%v", data, err, app.stdinConsumed)
	}
}

func TestCLIOutdatedSkipsUnsafeReplay(t *testing.T) {
	body := []byte(`{"error":{"code":"cli_outdated","message":"CLI version is below the minimum compatible version"},"upgrade":{"recommended_cli_version":"v9.1.0"}}`)
	cases := []struct {
		name   string
		status int
		local  bool
		stdin  bool
		output bool
		update bool
	}{
		{name: "http 426", status: 426, update: true},
		{name: "http 500", status: 500},
		{name: "stdin already read", status: 426, stdin: true},
		{name: "output already started", status: 426, output: true},
		{name: "local doctor", local: true, update: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app, cfg, _ := managedUpdateFixture(t)
			app.Getenv = func(key string) string {
				if key == "ANX_UPDATE_POLICY" {
					return "auto"
				}
				return ""
			}
			app.stdinConsumed = tc.stdin
			app.outputStarted = tc.output
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
			var runErr error
			if tc.local {
				runErr = errnorm.Local("cli_outdated", "CLI is below minimum")
			} else {
				runErr = errnorm.FromHTTPFailure(tc.status, body)
			}
			_, nextErr, retried := app.recoverCLIOutdated(context.Background(), []string{"work", "list"}, cfg, "work list", runErr)
			if tc.update {
				if !retried || updates != 1 || execs != 1 {
					t.Fatalf("retried=%v updates=%d execs=%d err=%v", retried, updates, execs, nextErr)
				}
				return
			}
			if retried || updates != 0 || execs != 0 || nextErr == nil || !strings.Contains(nextErr.Error(), "anx update --version v9.1.0") {
				t.Fatalf("retried=%v updates=%d execs=%d err=%v", retried, updates, execs, nextErr)
			}
		})
	}
}
