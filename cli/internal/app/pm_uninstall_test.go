package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-nexus-cli/internal/config"
)

func TestPMUninstallResetKeepRegistrationAndServerFailure(t *testing.T) {
	for _, mode := range []string{"reset", "keep", "server-failure", "auth-failure"} {
		t.Run(mode, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
			oldOS, oldExec, oldAuth := pmServiceOS, pmServiceExec, pmServiceResolveAuth
			t.Cleanup(func() { pmServiceOS, pmServiceExec, pmServiceResolveAuth = oldOS, oldExec, oldAuth })
			pmServiceOS = "linux"
			pmServiceExec = func(context.Context, string, ...string) ([]byte, error) { return []byte("active"), nil }
			pmServiceResolveAuth = func(_ *App, _ context.Context, cfg config.Resolved) (config.Resolved, error) {
				if mode == "auth-failure" {
					return cfg, os.ErrPermission
				}
				cfg.AccessToken = "fake-key"
				return cfg, nil
			}
			calls := 0
			var unit string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != "/pm/disconnect" || r.Header.Get("Authorization") != "Bearer fake-key" {
					t.Errorf("bad request %s %s", r.Method, r.URL.Path)
				}
				if _, err := os.Stat(unit); !os.IsNotExist(err) {
					t.Errorf("network reset before local removal %v", err)
				}
				if mode == "server-failure" {
					w.WriteHeader(503)
					_, _ = w.Write([]byte(`{"error":"unavailable"}`))
					return
				}
				_, _ = w.Write([]byte(`{"state":"not_onboarded","configured":true,"connected":false,"last_seen":null,"runner":null,"host":null}`))
			}))
			defer server.Close()
			a := &App{UserHomeDir: func() (string, error) { return home, nil }}
			cfg := config.Resolved{BaseURL: server.URL, ConfigDir: filepath.Join(home, "credentials"), As: "pm"}
			installed, err := a.runPMService(context.Background(), "install", []string{"--runner", "runner {prompt_file}"}, cfg)
			if err != nil {
				t.Fatal(err)
			}
			status := installed.Data.(pmServiceStatus)
			unit = filepath.Join(home, "config", "systemd", "user", status.Service+".service")
			args := []string{}
			if mode == "keep" {
				args = append(args, "--keep-registration")
			}
			result, err := a.runPMService(context.Background(), "uninstall", args, cfg)
			if err != nil || result.Data.(pmServiceStatus).Installed {
				t.Fatalf("local removal %+v %v", result, err)
			}
			if _, err = os.Stat(unit); !os.IsNotExist(err) {
				t.Fatal("unit remains", err)
			}
			if _, err = os.Stat(status.Logs); !os.IsNotExist(err) {
				t.Fatal("state remains", err)
			}
			if mode == "server-failure" || mode == "auth-failure" {
				if len(result.Warnings) != 1 || result.Warnings[0].Code != "pm_disconnect_failed" || !strings.Contains(result.Warnings[0].Message, "Retry:") || strings.Contains(result.Warnings[0].Message, "fake-key") {
					t.Fatalf("missing safe recovery %+v", result.Warnings)
				}
			} else if len(result.Warnings) != 0 {
				t.Fatal(result.Warnings)
			}
			if mode == "keep" || mode == "auth-failure" {
				if calls != 0 {
					t.Fatal("unexpected server reset")
				}
			} else if calls != 1 {
				t.Fatalf("reset calls %d", calls)
			}
			if mode == "server-failure" {
				retry := result.Warnings[0].Details.(map[string]any)["retry_argv"].([]string)
				mode = "retry-success"
				payload := assertEnvelopeOK(t, runCLIForTest(t, home, map[string]string{"ANX_ACCESS_TOKEN": "fake-key"}, nil, append([]string{"--json"}, retry[1:]...)))
				if !payload["ok"].(bool) || calls != 2 {
					t.Fatalf("retry failed %+v calls=%d", payload, calls)
				}
			}
			if mode == "reset" {
				if _, err = a.runPMService(context.Background(), "uninstall", nil, cfg); err != nil || calls != 2 {
					t.Fatalf("repeat uninstall calls=%d err=%v", calls, err)
				}
			}
		})
	}
}
