package app

import (
	"context"
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
			oldOS, oldExec := pmServiceOS, pmServiceExec
			t.Cleanup(func() { pmServiceOS, pmServiceExec = oldOS, oldExec })
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
