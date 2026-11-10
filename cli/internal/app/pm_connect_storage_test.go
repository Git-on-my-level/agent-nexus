package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func TestPMConnectDiagnosticHintsAreLocal(t *testing.T) {
	for _, code := range []string{"pm_actor_mismatch", "forbidden", "network_error", "unrecognized_remote_code"} {
		t.Run(code, func(t *testing.T) {
			dir := t.TempDir()
			remote := map[string]any{"error": map[string]any{"code": code, "hint": "https://user:password@example.test/?token=secret", "message": "Bearer secret", "details": map[string]any{"configured_actor_id": "agent_old", "acting_actor_id": "agent_new"}}}
			body, _ := json.Marshal(remote)
			if err := recordPMConnect(dir, errnorm.FromHTTPFailure(403, body)); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(filepath.Join(dir, "last-connect.json"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "password") || strings.Contains(string(raw), "example.test") {
				t.Fatalf("remote secret persisted: %s", raw)
			}
			failure, err := readPMConnectFailure(dir)
			if err != nil || failure == nil || failure.Hint == "" {
				t.Fatalf("missing local hint: %+v %v", failure, err)
			}
			if code == "pm_actor_mismatch" && (failure.ConfiguredActorID != "agent_old" || failure.ActingActorID != "agent_new" || !strings.Contains(failure.Hint, "ANX_PM_AGENT_ACTOR_ID to agent_new")) {
				t.Fatalf("missing binding repair: %+v", failure)
			}
			if code != "pm_actor_mismatch" && (failure.ConfiguredActorID != "" || failure.ActingActorID != "") {
				t.Fatalf("unexpected actor details: %+v", failure)
			}
			// Old records also cannot display a formerly persisted remote hint.
			failure.Hint = "https://user:password@example.test/?token=secret"
			failure.ActingActorID = "https://user:password@example.test/?token=secret"
			raw, _ = json.Marshal(failure)
			if err := os.WriteFile(filepath.Join(dir, "last-connect.json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			failure, _ = readPMConnectFailure(dir)
			if strings.Contains(failure.Hint, "secret") || failure.ActingActorID != "" {
				t.Fatalf("unsafe old record: %+v", failure)
			}
		})
	}
}

func TestPMServiceUnreadableConnectDiagnosticIsUnknown(t *testing.T) {
	for _, contents := range []string{"{corrupt", "{}", strings.Repeat("x", 16*1024+1), "unreadable"} {
		t.Run(contents[:min(len(contents), 10)], func(t *testing.T) {
			stubPMInstallConnect(t)
			home := t.TempDir()
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
			oldOS, oldExec := pmServiceOS, pmServiceExec
			t.Cleanup(func() { pmServiceOS, pmServiceExec = oldOS, oldExec })
			pmServiceOS = "linux"
			pmServiceExec = func(context.Context, string, ...string) ([]byte, error) { return []byte("active"), nil }
			cfg := config.Resolved{BaseURL: "http://localhost:9999", ConfigDir: filepath.Join(home, "credentials")}
			state := filepath.Join(home, "state", "anx", "pm", pmServiceID(cfg.BaseURL, cfg.ConfigDir, "pm"))
			if err := os.MkdirAll(state, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(state, "last-connect.json")
			data := []byte(contents)
			if contents == "unreadable" {
				if os.Geteuid() == 0 {
					t.Skip("root bypasses file read permissions")
				}
				// Valid history ensures this case exercises permission denial,
				// rather than falling through the corrupt-JSON path.
				data = []byte(`{"code":"forbidden","at":"2026-11-10T00:00:00Z","status":403}`)
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if contents == "unreadable" {
				if err := os.Chmod(path, 0000); err != nil {
					t.Fatal(err)
				}
				defer os.Chmod(path, 0600)
			}
			a := &App{UserHomeDir: func() (string, error) { return home, nil }}
			result, err := a.runPMService(context.Background(), "status", nil, cfg)
			if err != nil || result == nil || result.Data.(pmServiceStatus).LastConnectFailure != nil {
				t.Fatalf("bad history broke status: %+v %v", result, err)
			}
			result, err = a.runPMService(context.Background(), "install", []string{"--runner", "echo {prompt}"}, cfg)
			if err != nil || result == nil || !result.Data.(pmServiceStatus).Installed || result.Data.(pmServiceStatus).LastConnectFailure != nil {
				t.Fatalf("bad history broke install: %+v %v", result, err)
			}
		})
	}
}
