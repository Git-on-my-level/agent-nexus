package app

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-nexus-cli/internal/config"
)

func TestPMServiceLifecycleAndIsolation(t *testing.T) {
	for _, platform := range []string{"linux", "darwin"} {
		t.Run(platform, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
			oldOS, oldExec, oldWait := pmServiceOS, pmServiceExec, pmServiceWaitForConnection
			t.Cleanup(func() { pmServiceOS, pmServiceExec, pmServiceWaitForConnection = oldOS, oldExec, oldWait })
			pmServiceOS = platform
			var calls []string
			pmServiceExec = func(_ context.Context, name string, args ...string) ([]byte, error) {
				calls = append(calls, name+" "+strings.Join(args, " "))
				if len(args) > 1 && args[1] == "is-active" {
					return []byte("active\n"), nil
				}
				return []byte("state = running"), nil
			}
			a := &App{UserHomeDir: func() (string, error) { return home, nil }}
			cfg := config.Resolved{BaseURL: "https://workspace.example", ConfigDir: filepath.Join(home, "credentials"), AccessToken: "fake-secret-must-not-store"}
			run := func(verb string, args ...string) pmServiceStatus {
				t.Helper()
				result, err := a.runPMService(context.Background(), verb, args, cfg)
				if err != nil {
					t.Fatal(err)
				}
				return result.Data.(pmServiceStatus)
			}
			if s := run("status"); s.Installed || s.Running {
				t.Fatalf("initial %+v", s)
			}
			installed := run("install", "--runner", "sh -c 'exec claude -p < \"$1\"' sh {prompt_file}")
			if !installed.Installed || !installed.Running || installed.Agent != "pm" {
				t.Fatalf("installed %+v", installed)
			}
			run("install") // saved config; no runner flag required on repair
			if err := filepath.Walk(home, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if info.IsDir() {
					return nil
				}
				b, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				if strings.Contains(string(b), cfg.AccessToken) {
					t.Errorf("credential persisted in %s", path)
				}
				if info.Mode().Perm() != 0600 {
					t.Errorf("unsafe mode %s: %o", path, info.Mode().Perm())
				}
				if strings.HasSuffix(path, ".plist") {
					var v any
					decoder := xml.NewDecoder(strings.NewReader(string(b)))
					for {
						_, e := decoder.Token()
						if e != nil {
							if e.Error() != "EOF" {
								t.Error(e)
							}
							break
						}
					}
					_ = v
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			cfg.BaseURL = "https://other.example"
			if s := run("status"); s.Installed {
				t.Fatal("workspace services collide")
			}
			cfg.BaseURL = installed.Workspace
			if s := run("uninstall"); s.Installed || s.Running {
				t.Fatalf("uninstall %+v", s)
			}
			run("uninstall")
			if len(calls) == 0 {
				t.Fatal("no native service operations")
			}
		})
	}
}

func TestPMServiceWaitReportsPostClaimState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	oldOS, oldExec, oldWait := pmServiceOS, pmServiceExec, pmServiceWaitForConnection
	t.Cleanup(func() { pmServiceOS, pmServiceExec, pmServiceWaitForConnection = oldOS, oldExec, oldWait })
	pmServiceOS = "darwin"
	claimAt := ""
	var printCount int
	pmServiceExec = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "launchctl" && len(args) > 0 && args[0] == "print" {
			printCount++
			if printCount == 1 {
				return []byte("state = waiting"), nil
			}
			return []byte("state = running"), nil
		}
		return nil, nil
	}
	pmServiceWaitForConnection = func(_ *App, _ context.Context, _ config.Resolved, _ time.Duration, stateDir string, since time.Time) error {
		claimAt = since.Add(time.Second).UTC().Format(time.RFC3339Nano)
		return os.WriteFile(filepath.Join(stateDir, "last-claim"), []byte(claimAt), 0600)
	}
	a := &App{UserHomeDir: func() (string, error) { return home, nil }}
	cfg := config.Resolved{BaseURL: "https://workspace.example", ConfigDir: filepath.Join(home, "credentials"), As: "hermes"}
	result, err := a.runPMService(context.Background(), "install", []string{"--runner", "hermes {prompt_file}", "--wait"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	status := result.Data.(pmServiceStatus)
	if !status.Installed || !status.Running || status.Agent != "hermes" || status.LastClaimAt != claimAt {
		t.Fatalf("wait returned pre-claim status: %+v", status)
	}
	if !strings.Contains(result.Text, "PM connected") || strings.Contains(result.Text, "running=false") {
		t.Fatalf("wait text misreported the accepted connection: %s", result.Text)
	}
}

func TestPMServiceInstallWithoutWaitReportsStartingAndKeepsPriorProfile(t *testing.T) {
	for _, platform := range []string{"darwin", "linux"} {
		t.Run(platform, func(t *testing.T) {
			home := t.TempDir()
			stateRoot := filepath.Join(home, "state")
			configRoot := filepath.Join(home, "config")
			t.Setenv("XDG_STATE_HOME", stateRoot)
			t.Setenv("XDG_CONFIG_HOME", configRoot)
			oldOS, oldExec, oldWait := pmServiceOS, pmServiceExec, pmServiceWaitForConnection
			t.Cleanup(func() { pmServiceOS, pmServiceExec, pmServiceWaitForConnection = oldOS, oldExec, oldWait })
			pmServiceOS = platform
			cfg := config.Resolved{BaseURL: "https://workspace.example", ConfigDir: filepath.Join(home, "credentials"), As: "hermes"}
			legacyID := pmServiceID(cfg.BaseURL, cfg.ConfigDir, "pm")
			legacyState := filepath.Join(stateRoot, "anx", "pm", legacyID)
			legacyUnitDir, suffix := filepath.Join(home, "Library", "LaunchAgents"), ".plist"
			if platform == "linux" {
				legacyUnitDir, suffix = filepath.Join(configRoot, "systemd", "user"), ".service"
			}
			legacyUnit := priorDefaultPMServiceUnit(legacyUnitDir, suffix, cfg.BaseURL, cfg.ConfigDir)
			selectedID := pmServiceID(cfg.BaseURL, cfg.ConfigDir, "hermes")
			selectedState := filepath.Join(stateRoot, "anx", "pm", selectedID)
			selectedUnit := filepath.Join(legacyUnitDir, selectedID+suffix)
			if err := os.MkdirAll(legacyUnitDir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(legacyState, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(selectedState, 0700); err != nil {
				t.Fatal(err)
			}
			settings, _ := json.Marshal(pmServiceSettings{Workspace: cfg.BaseURL, ConfigDir: cfg.ConfigDir, Agent: "pm", Runner: "old runner"})
			if err := os.WriteFile(filepath.Join(legacyState, "settings.json"), settings, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(legacyUnit, []byte("legacy service"), 0600); err != nil {
				t.Fatal(err)
			}
			selectedSettings, _ := json.Marshal(pmServiceSettings{Workspace: cfg.BaseURL, ConfigDir: cfg.ConfigDir, Agent: "hermes", Runner: "older runner"})
			if err := os.WriteFile(filepath.Join(selectedState, "settings.json"), selectedSettings, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(selectedUnit, []byte("selected service"), 0600); err != nil {
				t.Fatal(err)
			}
			var calls []string
			pmServiceExec = func(_ context.Context, name string, args ...string) ([]byte, error) {
				calls = append(calls, name+" "+strings.Join(args, " "))
				stoppingSelected := name == "launchctl" && len(args) > 1 && args[0] == "bootout" && strings.HasSuffix(args[1], "/"+selectedID)
				stoppingSelected = stoppingSelected || (name == "systemctl" && len(args) > 2 && args[1] == "stop" && args[2] == selectedID+".service")
				if stoppingSelected {
					time.Sleep(10 * time.Millisecond)
					claimAt := time.Now().UTC().Format(time.RFC3339Nano)
					if err := os.WriteFile(filepath.Join(selectedState, "last-claim"), []byte(claimAt), 0600); err != nil {
						return nil, err
					}
				}
				if name == "launchctl" && len(args) > 0 && args[0] == "print" {
					return []byte("state = waiting"), nil
				}
				if name == "systemctl" && len(args) > 1 && args[1] == "is-active" {
					return []byte("inactive\n"), nil
				}
				return nil, nil
			}
			a := &App{UserHomeDir: func() (string, error) { return home, nil }}
			result, err := a.runPMService(context.Background(), "install", []string{"--runner", "hermes {prompt_file}"}, cfg)
			if err != nil {
				t.Fatal(err)
			}
			status := result.Data.(pmServiceStatus)
			if !status.Installed || status.Running || !strings.Contains(result.Text, "starting, not yet claimed") || strings.Contains(result.Text, "installed=true running=false") {
				t.Fatalf("install did not explain pending startup: status=%+v text=%s", status, result.Text)
			}
			if len(result.Warnings) != 1 || result.Warnings[0].Code != "prior_pm_service_migration_pending" {
				t.Fatalf("prior migration warning missing: %+v", result.Warnings)
			}
			if !strings.Contains(result.Warnings[0].Message, "'--as' 'hermes'") || !strings.Contains(result.Warnings[0].Message, "'pm' 'install' '--wait'") {
				t.Fatalf("migration retry does not preserve the selected identity: %+v", result.Warnings[0])
			}
			if _, err := os.Stat(legacyUnit); err != nil {
				t.Fatalf("prior service was stopped before a claim: %v", err)
			}
			for _, call := range calls {
				if strings.Contains(call, legacyID) {
					t.Fatalf("prior service touched before a claim: %s", call)
				}
			}
		})
	}
}

func TestPMServiceInstallPreparationFailureKeepsSelectedServiceRunning(t *testing.T) {
	for _, platform := range []string{"darwin", "linux"} {
		t.Run(platform, func(t *testing.T) {
			home := t.TempDir()
			stateRoot := filepath.Join(home, "state")
			configRoot := filepath.Join(home, "config")
			t.Setenv("XDG_STATE_HOME", stateRoot)
			t.Setenv("XDG_CONFIG_HOME", configRoot)
			oldOS, oldExec, oldStage := pmServiceOS, pmServiceExec, pmServiceStageFile
			t.Cleanup(func() { pmServiceOS, pmServiceExec, pmServiceStageFile = oldOS, oldExec, oldStage })
			pmServiceOS = platform
			cfg := config.Resolved{BaseURL: "https://workspace.example", ConfigDir: filepath.Join(home, "credentials"), As: "hermes"}
			id := pmServiceID(cfg.BaseURL, cfg.ConfigDir, "hermes")
			state := filepath.Join(stateRoot, "anx", "pm", id)
			unitDir, suffix := filepath.Join(home, "Library", "LaunchAgents"), ".plist"
			if platform == "linux" {
				unitDir, suffix = filepath.Join(configRoot, "systemd", "user"), ".service"
			}
			unit := filepath.Join(unitDir, id+suffix)
			settingsPath := filepath.Join(state, "settings.json")
			if err := os.MkdirAll(unitDir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(state, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(unit, []byte("existing service definition"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(settingsPath, []byte(`{"runner":"existing"}`), 0600); err != nil {
				t.Fatal(err)
			}
			var calls []string
			pmServiceExec = func(_ context.Context, name string, args ...string) ([]byte, error) {
				calls = append(calls, name+" "+strings.Join(args, " "))
				if name == "launchctl" && len(args) > 0 && args[0] == "print" {
					return []byte("state = running"), nil
				}
				if name == "systemctl" && len(args) > 1 && args[1] == "is-active" {
					return []byte("active\n"), nil
				}
				return nil, nil
			}
			pmServiceStageFile = func(path string, data []byte) (string, error) {
				if path == settingsPath {
					return "", errors.New("simulated settings preparation failure")
				}
				return stagePMPrivateFile(path, data)
			}
			a := &App{UserHomeDir: func() (string, error) { return home, nil }}
			if _, err := a.runPMService(context.Background(), "install", []string{"--runner", "hermes {prompt_file}"}, cfg); err == nil || !strings.Contains(err.Error(), "simulated settings preparation failure") {
				t.Fatalf("install error = %v, want preparation failure", err)
			}
			for _, call := range calls {
				if strings.Contains(call, "bootout") || (strings.Contains(call, "systemctl") && strings.Contains(call, " stop ")) {
					t.Fatalf("preparation failure stopped the selected service: %v", calls)
				}
			}
			if got, err := os.ReadFile(unit); err != nil || string(got) != "existing service definition" {
				t.Fatalf("existing service definition changed: %q, %v", got, err)
			}
			if got, err := os.ReadFile(settingsPath); err != nil || string(got) != `{"runner":"existing"}` {
				t.Fatalf("existing settings changed: %q, %v", got, err)
			}
		})
	}
}

func TestPMServiceInstallCommitFailureRestoresSelectedService(t *testing.T) {
	for _, platform := range []string{"darwin", "linux"} {
		t.Run(platform, func(t *testing.T) {
			home := t.TempDir()
			stateRoot := filepath.Join(home, "state")
			configRoot := filepath.Join(home, "config")
			t.Setenv("XDG_STATE_HOME", stateRoot)
			t.Setenv("XDG_CONFIG_HOME", configRoot)
			oldOS, oldExec, oldStage, oldCommit := pmServiceOS, pmServiceExec, pmServiceStageFile, pmServiceCommitFile
			t.Cleanup(func() {
				pmServiceOS, pmServiceExec, pmServiceStageFile, pmServiceCommitFile = oldOS, oldExec, oldStage, oldCommit
			})
			pmServiceOS = platform
			cfg := config.Resolved{BaseURL: "https://workspace.example", ConfigDir: filepath.Join(home, "credentials"), As: "hermes"}
			id := pmServiceID(cfg.BaseURL, cfg.ConfigDir, "hermes")
			state := filepath.Join(stateRoot, "anx", "pm", id)
			unitDir, suffix := filepath.Join(home, "Library", "LaunchAgents"), ".plist"
			if platform == "linux" {
				unitDir, suffix = filepath.Join(configRoot, "systemd", "user"), ".service"
			}
			unit := filepath.Join(unitDir, id+suffix)
			settingsPath := filepath.Join(state, "settings.json")
			if err := os.MkdirAll(unitDir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(state, 0700); err != nil {
				t.Fatal(err)
			}
			oldUnit := []byte("existing service definition")
			oldSettings := []byte(`{"runner":"existing"}`)
			if err := os.WriteFile(unit, oldUnit, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(settingsPath, oldSettings, 0600); err != nil {
				t.Fatal(err)
			}
			var calls []string
			pmServiceExec = func(_ context.Context, name string, args ...string) ([]byte, error) {
				calls = append(calls, name+" "+strings.Join(args, " "))
				if name == "launchctl" && len(args) > 0 && args[0] == "print" {
					return []byte("state = running"), nil
				}
				if name == "systemctl" && len(args) > 1 && args[1] == "is-active" {
					return []byte("active\n"), nil
				}
				return nil, nil
			}
			var replacementUnitStage string
			pmServiceStageFile = func(path string, data []byte) (string, error) {
				staged, err := stagePMPrivateFile(path, data)
				if err == nil && path == unit && string(data) != string(oldUnit) {
					replacementUnitStage = staged
				}
				return staged, err
			}
			pmServiceCommitFile = func(source, destination string) error {
				if source == replacementUnitStage && destination == unit {
					return errors.New("simulated unit commit failure")
				}
				return os.Rename(source, destination)
			}
			a := &App{UserHomeDir: func() (string, error) { return home, nil }}
			if _, err := a.runPMService(context.Background(), "install", []string{"--runner", "hermes {prompt_file}"}, cfg); err == nil || !strings.Contains(err.Error(), "prior service was restored") {
				t.Fatalf("install error = %v, want rolled-back commit failure", err)
			}
			if got, err := os.ReadFile(unit); err != nil || string(got) != string(oldUnit) {
				t.Fatalf("service definition was not restored: %q, %v", got, err)
			}
			if got, err := os.ReadFile(settingsPath); err != nil || string(got) != string(oldSettings) {
				t.Fatalf("settings were not restored: %q, %v", got, err)
			}
			joined := strings.Join(calls, "\n")
			if !strings.Contains(joined, "bootout") && !strings.Contains(joined, " stop ") {
				t.Fatalf("test did not stop the prior service before the commit: %v", calls)
			}
			if platform == "darwin" && !strings.Contains(joined, "launchctl bootstrap") {
				t.Fatalf("prior launchd service was not restarted: %v", calls)
			}
			if platform == "linux" && !strings.Contains(joined, "systemctl --user start "+id+".service") {
				t.Fatalf("prior systemd service was not restarted: %v", calls)
			}
		})
	}
}

func TestPMServiceInstallActivationFailureRestoresSelectedService(t *testing.T) {
	for _, platform := range []string{"darwin", "linux"} {
		for _, mode := range []string{"restart-old", "restore-fails"} {
			t.Run(platform+"/"+mode, func(t *testing.T) {
				home := t.TempDir()
				stateRoot := filepath.Join(home, "state")
				configRoot := filepath.Join(home, "config")
				t.Setenv("XDG_STATE_HOME", stateRoot)
				t.Setenv("XDG_CONFIG_HOME", configRoot)
				oldOS, oldExec, oldStage, oldCommit := pmServiceOS, pmServiceExec, pmServiceStageFile, pmServiceCommitFile
				t.Cleanup(func() {
					pmServiceOS, pmServiceExec, pmServiceStageFile, pmServiceCommitFile = oldOS, oldExec, oldStage, oldCommit
				})
				pmServiceOS = platform
				cfg := config.Resolved{BaseURL: "https://workspace.example", ConfigDir: filepath.Join(home, "credentials"), As: "hermes"}
				id := pmServiceID(cfg.BaseURL, cfg.ConfigDir, "hermes")
				state := filepath.Join(stateRoot, "anx", "pm", id)
				unitDir, suffix := filepath.Join(home, "Library", "LaunchAgents"), ".plist"
				if platform == "linux" {
					unitDir, suffix = filepath.Join(configRoot, "systemd", "user"), ".service"
				}
				unit := filepath.Join(unitDir, id+suffix)
				settingsPath := filepath.Join(state, "settings.json")
				if err := os.MkdirAll(unitDir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(state, 0700); err != nil {
					t.Fatal(err)
				}
				oldUnit := []byte("existing service definition")
				oldSettings := []byte(`{"runner":"existing"}`)
				if err := os.WriteFile(unit, oldUnit, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(settingsPath, oldSettings, 0600); err != nil {
					t.Fatal(err)
				}
				var oldUnitStage string
				pmServiceStageFile = func(path string, data []byte) (string, error) {
					staged, err := stagePMPrivateFile(path, data)
					if err == nil && path == unit && string(data) == string(oldUnit) {
						oldUnitStage = staged
					}
					return staged, err
				}
				pmServiceCommitFile = func(source, destination string) error {
					if mode == "restore-fails" && source == oldUnitStage && destination == unit {
						return errors.New("simulated old-unit restore failure")
					}
					return os.Rename(source, destination)
				}
				failedReplacementStart := false
				activationAttempts := 0
				var calls []string
				pmServiceExec = func(_ context.Context, name string, args ...string) ([]byte, error) {
					calls = append(calls, name+" "+strings.Join(args, " "))
					if name == "launchctl" && len(args) > 0 && args[0] == "print" {
						return []byte("state = running"), nil
					}
					if name == "systemctl" && len(args) > 1 && args[1] == "is-active" {
						return []byte("active\n"), nil
					}
					activation := (platform == "darwin" && name == "launchctl" && len(args) > 0 && args[0] == "bootstrap") || (platform == "linux" && name == "systemctl" && len(args) > 1 && args[1] == "start")
					if activation {
						activationAttempts++
						if !failedReplacementStart {
							failedReplacementStart = true
							return nil, errors.New("simulated replacement activation failure")
						}
					}
					return nil, nil
				}
				a := &App{UserHomeDir: func() (string, error) { return home, nil }}
				_, installErr := a.runPMService(context.Background(), "install", []string{"--runner", "hermes {prompt_file}"}, cfg)
				if installErr == nil {
					t.Fatal("expected replacement activation failure")
				}
				if mode == "restart-old" {
					if !strings.Contains(installErr.Error(), "prior service was restored") {
						t.Fatalf("install error = %v, want successful rollback", installErr)
					}
					if got, err := os.ReadFile(unit); err != nil || string(got) != string(oldUnit) {
						t.Fatalf("service definition was not restored: %q, %v", got, err)
					}
				} else {
					if !strings.Contains(installErr.Error(), "rollback also failed") {
						t.Fatalf("install error = %v, want rollback failure", installErr)
					}
					if got, err := os.ReadFile(unit); err != nil || string(got) == string(oldUnit) {
						t.Fatalf("failed restore unexpectedly removed replacement definition: %q, %v", got, err)
					}
				}
				if got, err := os.ReadFile(settingsPath); err != nil || string(got) != string(oldSettings) {
					t.Fatalf("settings were not restored before returning: %q, %v", got, err)
				}
				if mode == "restart-old" && activationAttempts < 2 {
					t.Fatalf("prior service activation was not retried after rollback: calls=%v", calls)
				}
				if mode == "restore-fails" && activationAttempts != 1 {
					t.Fatalf("service was started despite a failed unit restore: calls=%v", calls)
				}
			})
		}
	}
}

func TestPMServiceInstallStopsPriorDefaultProfileService(t *testing.T) {
	for _, platform := range []string{"darwin", "linux"} {
		t.Run(platform, func(t *testing.T) {
			home := t.TempDir()
			stateRoot := filepath.Join(home, "state")
			configRoot := filepath.Join(home, "config")
			t.Setenv("XDG_STATE_HOME", stateRoot)
			t.Setenv("XDG_CONFIG_HOME", configRoot)
			oldOS, oldExec, oldWait := pmServiceOS, pmServiceExec, pmServiceWaitForConnection
			t.Cleanup(func() { pmServiceOS, pmServiceExec, pmServiceWaitForConnection = oldOS, oldExec, oldWait })
			pmServiceOS = platform
			cfg := config.Resolved{BaseURL: "https://workspace.example", ConfigDir: filepath.Join(home, "credentials"), As: "hermes"}
			legacyID := pmServiceID(cfg.BaseURL, cfg.ConfigDir, "pm")
			legacyState := filepath.Join(stateRoot, "anx", "pm", legacyID)
			legacyUnitDir, suffix := filepath.Join(home, "Library", "LaunchAgents"), ".plist"
			if platform == "linux" {
				legacyUnitDir, suffix = filepath.Join(configRoot, "systemd", "user"), ".service"
			}
			legacyUnit := filepath.Join(legacyUnitDir, legacyID+suffix)
			if err := os.MkdirAll(legacyUnitDir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(legacyState, 0700); err != nil {
				t.Fatal(err)
			}
			settings, _ := json.Marshal(pmServiceSettings{Workspace: cfg.BaseURL, ConfigDir: cfg.ConfigDir, Agent: "pm", Runner: "old runner"})
			if err := os.WriteFile(filepath.Join(legacyState, "settings.json"), settings, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(legacyUnit, []byte("legacy service"), 0600); err != nil {
				t.Fatal(err)
			}
			var calls []string
			pmServiceExec = func(_ context.Context, name string, args ...string) ([]byte, error) {
				calls = append(calls, name+" "+strings.Join(args, " "))
				if name == "launchctl" && len(args) > 0 && args[0] == "print" {
					return []byte("state = running"), nil
				}
				if name == "systemctl" && len(args) > 1 && args[1] == "is-active" {
					return []byte("active\n"), nil
				}
				return nil, nil
			}
			claimAt := ""
			pmServiceWaitForConnection = func(_ *App, _ context.Context, _ config.Resolved, _ time.Duration, stateDir string, since time.Time) error {
				claimAt = since.Add(time.Second).UTC().Format(time.RFC3339Nano)
				return os.WriteFile(filepath.Join(stateDir, "last-claim"), []byte(claimAt), 0600)
			}
			a := &App{UserHomeDir: func() (string, error) { return home, nil }}
			result, err := a.runPMService(context.Background(), "install", []string{"--runner", "hermes {prompt_file}", "--wait"}, cfg)
			if err != nil {
				t.Fatal(err)
			}
			status := result.Data.(pmServiceStatus)
			if status.Agent != "hermes" || !status.Installed || !status.Running || status.LastClaimAt != claimAt {
				t.Fatalf("new PM service status %+v", status)
			}
			if _, err := os.Stat(legacyUnit); !os.IsNotExist(err) {
				t.Fatalf("prior default-profile service unit remains: %v", err)
			}
			if _, err := os.Stat(filepath.Join(legacyState, "settings.json")); err != nil {
				t.Fatalf("prior service state should be retained for its logs: %v", err)
			}
			stopCall := "launchctl bootout gui/"
			if platform == "linux" {
				stopCall = "systemctl --user disable --now " + legacyID + ".service"
			}
			joined := strings.Join(calls, "\n")
			if !strings.Contains(joined, stopCall) || !strings.Contains(joined, legacyID) {
				t.Fatalf("prior service was not stopped: %v", calls)
			}
		})
	}
}

func TestPMServiceInstallFailureKeepsPriorDefaultProfileService(t *testing.T) {
	for _, platform := range []string{"darwin", "linux"} {
		for _, failure := range []string{"start", "wait"} {
			t.Run(platform+"/"+failure, func(t *testing.T) {
				home := t.TempDir()
				stateRoot := filepath.Join(home, "state")
				configRoot := filepath.Join(home, "config")
				t.Setenv("XDG_STATE_HOME", stateRoot)
				t.Setenv("XDG_CONFIG_HOME", configRoot)
				oldOS, oldExec, oldWait := pmServiceOS, pmServiceExec, pmServiceWaitForConnection
				t.Cleanup(func() { pmServiceOS, pmServiceExec, pmServiceWaitForConnection = oldOS, oldExec, oldWait })
				pmServiceOS = platform
				cfg := config.Resolved{BaseURL: "https://workspace.example", ConfigDir: filepath.Join(home, "credentials"), As: "hermes"}
				legacyID := pmServiceID(cfg.BaseURL, cfg.ConfigDir, "pm")
				legacyState := filepath.Join(stateRoot, "anx", "pm", legacyID)
				legacyUnitDir, suffix := filepath.Join(home, "Library", "LaunchAgents"), ".plist"
				if platform == "linux" {
					legacyUnitDir, suffix = filepath.Join(configRoot, "systemd", "user"), ".service"
				}
				legacyUnit := filepath.Join(legacyUnitDir, legacyID+suffix)
				selectedID := pmServiceID(cfg.BaseURL, cfg.ConfigDir, "hermes")
				selectedEnableLink := filepath.Join(legacyUnitDir, "default.target.wants", selectedID+".service")
				if err := os.MkdirAll(legacyUnitDir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(legacyState, 0700); err != nil {
					t.Fatal(err)
				}
				settings, _ := json.Marshal(pmServiceSettings{Workspace: cfg.BaseURL, ConfigDir: cfg.ConfigDir, Agent: "pm", Runner: "old runner"})
				if err := os.WriteFile(filepath.Join(legacyState, "settings.json"), settings, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(legacyUnit, []byte("legacy service"), 0600); err != nil {
					t.Fatal(err)
				}

				var calls []string
				pmServiceExec = func(_ context.Context, name string, args ...string) ([]byte, error) {
					calls = append(calls, name+" "+strings.Join(args, " "))
					if platform == "linux" && name == "systemctl" && len(args) > 1 {
						switch args[1] {
						case "is-enabled":
							if _, err := os.Lstat(selectedEnableLink); err == nil {
								return []byte("enabled\n"), nil
							}
							return []byte("disabled\n"), errors.New("disabled")
						case "enable":
							if err := os.MkdirAll(filepath.Dir(selectedEnableLink), 0700); err != nil {
								return nil, err
							}
							if err := os.Symlink(selectedID+".service", selectedEnableLink); err != nil {
								return nil, err
							}
						case "disable":
							if err := os.Remove(selectedEnableLink); err != nil && !os.IsNotExist(err) {
								return nil, err
							}
						}
					}
					if failure == "start" && ((platform == "darwin" && name == "launchctl" && len(args) > 0 && args[0] == "bootstrap") || (platform == "linux" && name == "systemctl" && len(args) > 1 && args[1] == "start")) {
						return nil, errors.New("new service failed to start")
					}
					if name == "launchctl" && len(args) > 0 && args[0] == "print" {
						return []byte("state = running"), nil
					}
					if name == "systemctl" && len(args) > 1 && args[1] == "is-active" {
						return []byte("active\n"), nil
					}
					return nil, nil
				}
				pmServiceWaitForConnection = func(_ *App, _ context.Context, _ config.Resolved, _ time.Duration, _ string, _ time.Time) error {
					if failure == "wait" {
						return errors.New("connection timed out")
					}
					return nil
				}
				a := &App{UserHomeDir: func() (string, error) { return home, nil }}
				args := []string{"--runner", "hermes {prompt_file}"}
				if failure == "wait" {
					args = append(args, "--wait")
				}
				if _, err := a.runPMService(context.Background(), "install", args, cfg); err == nil {
					t.Fatal("expected replacement service setup to fail")
				}
				if _, err := os.Stat(legacyUnit); err != nil {
					t.Fatalf("old service unit removed before replacement succeeded: %v", err)
				}
				if _, err := os.Stat(filepath.Join(legacyState, "settings.json")); err != nil {
					t.Fatalf("old service state removed before replacement succeeded: %v", err)
				}
				if platform == "linux" && failure == "start" {
					if _, err := os.Lstat(selectedEnableLink); !os.IsNotExist(err) {
						t.Fatalf("failed first install left an enabled-unit symlink: %v", err)
					}
				}
				for _, call := range calls {
					if strings.Contains(call, legacyID) {
						t.Fatalf("old service touched before replacement succeeded: %s", call)
					}
				}
			})
		}
	}
}

func TestPMServiceStopFailureRetainsFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	oldOS, oldExec := pmServiceOS, pmServiceExec
	defer func() { pmServiceOS, pmServiceExec = oldOS, oldExec }()
	pmServiceOS = "linux"
	pmServiceExec = func(context.Context, string, ...string) ([]byte, error) { return []byte("active"), nil }
	a := &App{UserHomeDir: func() (string, error) { return home, nil }}
	cfg := config.Resolved{BaseURL: "http://localhost:8000", ConfigDir: filepath.Join(home, "credentials")}
	result, err := a.runPMService(context.Background(), "install", []string{"--runner", "tool {prompt_file}"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	s := result.Data.(pmServiceStatus)
	pmServiceExec = func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("stop failed") }
	if _, err = a.runPMService(context.Background(), "uninstall", nil, cfg); err == nil {
		t.Fatal("stop failure ignored")
	}
	if _, err = os.Stat(s.Logs); err != nil {
		t.Fatal("state removed while service could still run")
	}
}

func TestPMPrivatePromptFileReplacesSymlinkAndRestrictsMode(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	path := filepath.Join(dir, "prompt")
	if err := os.WriteFile(target, []byte("untouched"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if err := writePMPrivateFile(path, []byte("private prompt")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(target)
	if string(b) != "untouched" {
		t.Fatal("followed prompt symlink")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("public prompt file")
	}
	argv := expandPromptPlaceholder([]string{"tool", "{prompt_file}", "{prompt}"}, path)
	if argv[1] != path || argv[2] != path || strings.Contains(strings.Join(argv, " "), "private prompt") {
		t.Fatal("prompt content in argv")
	}
}

func TestPMMacStopFailureRetainsFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	oldOS, oldExec := pmServiceOS, pmServiceExec
	defer func() { pmServiceOS, pmServiceExec = oldOS, oldExec }()
	pmServiceOS = "darwin"
	pmServiceExec = func(context.Context, string, ...string) ([]byte, error) { return []byte("state = running"), nil }
	a := &App{UserHomeDir: func() (string, error) { return home, nil }}
	cfg := config.Resolved{BaseURL: "http://localhost:8000", ConfigDir: filepath.Join(home, "credentials")}
	result, err := a.runPMService(context.Background(), "install", []string{"--runner", "tool {prompt_file}"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	s := result.Data.(pmServiceStatus)
	pmServiceExec = func(context.Context, string, ...string) ([]byte, error) { return nil, context.DeadlineExceeded }
	if _, err = a.runPMService(context.Background(), "uninstall", nil, cfg); err == nil {
		t.Fatal("timeout ignored")
	}
	if _, err = os.Stat(s.Logs); err != nil {
		t.Fatal("state removed while service may run")
	}
}

func TestPMSystemdFieldAndArgvEscapes(t *testing.T) {
	definition := pmServiceDefinition("linux", "example", []string{"/fixture/$name/bin/anx", "runner", "echo $HOME 100%"}, "/fixture/$name/state", "/fixture/$name", "/fixture/$name/bin")
	for _, literal := range []string{`WorkingDirectory=/fixture/$name/state`, `Environment="HOME=/fixture/$name"`, `Environment="PATH=/fixture/$name/bin"`, `StandardOutput=append:/fixture/$name/state/stdout.log`, `ExecStart="/fixture/$$name/bin/anx" "runner" "echo $$HOME 100%%"`} {
		if !strings.Contains(definition, literal) {
			t.Fatalf("missing %s in %s", literal, definition)
		}
	}
}

func TestPMSystemdNativeUnitValidation(t *testing.T) {
	tool, err := exec.LookPath("systemd-analyze")
	if err != nil {
		t.Skip("native systemd unit verifier is not installed")
	}
	dir := filepath.Join(t.TempDir(), "state with $ and % spaces")
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	unit := filepath.Join(t.TempDir(), "anx-local-pm-test.service")
	definition := pmServiceDefinition("linux", "anx-local-pm-test", []string{"/bin/echo", "sh -c 'cat < \"$1\"' {prompt_file}"}, dir, dir, "/usr/bin:/bin")
	if err = os.WriteFile(unit, []byte(definition), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, tool, "verify", "--man=no", unit).CombinedOutput()
	if err != nil {
		t.Fatalf("invalid systemd unit: %v %s", err, out)
	}
}
