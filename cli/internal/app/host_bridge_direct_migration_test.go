package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	hostbridge "agent-nexus-cli/internal/bridge"
	"agent-nexus-cli/internal/hostidentity"
	"agent-nexus-cli/internal/integrations"
	"agent-nexus-cli/internal/workspaceconfig"
)

type directBridgeFixture struct {
	a                                          *App
	home, configDir, goos, fail                string
	units                                      map[string]hostbridge.LegacyUnit
	loaded, enabled, priorLoaded, priorEnabled map[string]bool
	pids                                       map[string]int
	originals                                  map[string][]byte
	entries                                    map[string]string
	actions                                    []string
	ready, lateBusy                            bool
	nextPID                                    int
	exe                                        string
	t                                          *testing.T
}

func newDirectBridgeFixture(t *testing.T, goos string) *directBridgeFixture {
	t.Helper()
	f := &directBridgeFixture{t: t, home: t.TempDir(), goos: goos, units: map[string]hostbridge.LegacyUnit{}, loaded: map[string]bool{}, enabled: map[string]bool{}, priorLoaded: map[string]bool{}, priorEnabled: map[string]bool{}, pids: map[string]int{}, originals: map[string][]byte{}, entries: map[string]string{}, nextPID: 700}
	f.configDir = filepath.Join(f.home, ".config", "anx")
	f.a, _ = workspaceTestApp(t, f.home, f.home, map[string]string{"ANX_CONFIG_DIR": f.configDir})
	f.exe, _ = os.Executable()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		alias := parts[0]
		switch strings.Join(parts[1:], "/") {
		case "meta/handshake":
			fmt.Fprintf(w, `{"workspace_id":%q,"capabilities":["host_bridge_v1"]}`, alias)
		case "auth/token":
			fmt.Fprintf(w, `{"agent":{"id":%q,"actor_id":%q,"handle":"hermes.fixture"},"tokens":{"access_token":"fixture","expires_in":3600}}`, alias, "actor-"+alias)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	for _, alias := range []string{"personal", "anx", "omi"} {
		base := server.URL + "/" + alias
		_, key, e := ed25519.GenerateKey(nil)
		if e != nil {
			t.Fatal(e)
		}
		if e = hostidentity.SaveAt(f.configDir, hostidentity.Host{ID: "host-" + alias, KeyID: "key", Slug: "fixture", WorkspaceID: alias, BaseURL: base}, key); e != nil {
			t.Fatal(e)
		}
		if e = workspaceconfig.Update(f.configDir, func(c *workspaceconfig.Catalog) error { c.File.AddAlias(alias, base); return nil }); e != nil {
			t.Fatal(e)
		}
		label := "io.agent-nexus.hermes-answer." + alias
		path := filepath.Join(f.home, "Library", "LaunchAgents", label+".plist")
		kind := "launchd"
		argv := []string{f.exe, "--as", "hermes", "--workspace", alias, "bridge", "run", "--command-timeout", "30m"}
		content := `<plist><dict><key>Label</key><string>` + label + `</string><key>ProgramArguments</key><array>`
		for _, arg := range argv {
			content += `<string>` + arg + `</string>`
		}
		content += `</array><key>EnvironmentVariables</key><dict><key>ANX_CONFIG_DIR</key><string>` + f.configDir + `</string></dict></dict></plist>`
		if goos == "linux" {
			label += ".service"
			path = filepath.Join(f.home, ".config", "systemd", "user", label)
			kind = "systemd-user"
			quoted := []string{}
			for _, arg := range argv {
				quoted = append(quoted, strconv.Quote(arg))
			}
			content = "[Service]\nExecStart=" + strings.Join(quoted, " ") + "\nEnvironment=" + strconv.Quote("ANX_CONFIG_DIR="+f.configDir) + "\nWorkingDirectory=%h\n"
		}
		if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(path, []byte(content), 0644); e != nil {
			t.Fatal(e)
		}
		u, e := hostbridge.ParseUnit(path, kind, []byte(content))
		if e != nil {
			t.Fatal(e)
		}
		f.units[label] = u
		f.loaded[label] = true
		f.enabled[label] = true
		f.priorLoaded[label] = true
		f.priorEnabled[label] = true
		f.nextPID++
		f.pids[label] = f.nextPID
		dir := filepath.Join(f.configDir, "answer-resumes", hostbridge.Namespace(base, "actor-"+alias))
		if e = os.MkdirAll(dir, 0700); e != nil {
			t.Fatal(e)
		}
		path = askRegistryPath(dir, "ask-"+alias)
		entry := askResumeRegistration{AskID: "ask-" + alias, BaseURL: base, ActorID: "actor-" + alias, Command: "printf 'exact retained command'", Dir: f.home, State: "pending", Subscription: "sub-" + alias, Instance: "original-instance", Wakeup: "original-wake"}
		if e = writeAskRegistration(path, entry); e != nil {
			t.Fatal(e)
		}
		f.entries[alias] = path
		f.originals[path], _ = os.ReadFile(path)
	}
	// An unrelated symlink is not a bridge-ownership failure and is never read.
	suffix := ".plist"
	dir := filepath.Join(f.home, "Library", "LaunchAgents")
	if goos == "linux" {
		suffix = ".service"
		dir = filepath.Join(f.home, ".config", "systemd", "user")
	}
	if e := os.Symlink("/fixture/unrelated-unit", filepath.Join(dir, "unrelated"+suffix)); e != nil {
		t.Fatal(e)
	}
	oldRun, oldProcesses, oldPlatform, oldWait, oldRetire := bridgeCommandRun, bridgeProcesses, bridgePlatform, bridgeWaitReady, bridgeRetire
	t.Cleanup(func() {
		bridgeCommandRun, bridgeProcesses, bridgePlatform, bridgeWaitReady, bridgeRetire = oldRun, oldProcesses, oldPlatform, oldWait, oldRetire
	})
	bridgePlatform = goos
	bridgeCommandRun = f.command
	bridgeProcesses = func(context.Context) ([]hostbridge.Process, error) { return f.processes(), nil }
	bridgeRetire = func(_ context.Context, p hostbridge.Process) error {
		for _, actual := range f.processes() {
			if actual.PID == p.PID {
				t.Fatal("service did not quiesce its direct process", p.PID)
			}
		}
		return nil
	}
	bridgeWaitReady = func(_ context.Context, _ string, r hostbridge.Registry) error {
		for _, u := range f.units {
			raw, e := os.ReadFile(u.Path)
			if e != nil || !bytes.Equal(raw, u.Content) {
				t.Fatal("legacy file removed before readiness", u.Label, e)
			}
		}
		if len(r.Workspaces) != 3 {
			t.Fatal("lost registration", r)
		}
		for _, w := range r.Workspaces {
			if len(w.Agents) != 1 || w.Agents[0].Policy.Timeout != 30*time.Minute || !w.Agents[0].Jobs["answers"] {
				t.Fatal(w)
			}
		}
		if f.fail == "new-executing" {
			entry, _ := readAskRegistration(f.entries["personal"])
			entry.State = "executing"
			entry.Attempts = 1
			if e := writeAskRegistration(f.entries["personal"], entry); e != nil {
				t.Fatal(e)
			}
			f.fail = ""
			return context.DeadlineExceeded
		}
		if f.fail == "readiness" {
			f.fail = ""
			return context.DeadlineExceeded
		}
		f.ready = true
		return nil
	}
	return f
}
func (f *directBridgeFixture) processes() []hostbridge.Process {
	out := []hostbridge.Process{}
	for label, loaded := range f.loaded {
		if !loaded {
			continue
		}
		argv := []string{f.exe, "bridge", "run"}
		if u, ok := f.units[label]; ok {
			argv = u.Argv
		}
		out = append(out, hostbridge.Process{PID: f.pids[label], UID: os.Getuid(), Started: fmt.Sprint(f.pids[label]), Executable: f.exe, Argv: argv, CWD: f.home})
	}
	return out
}
func (f *directBridgeFixture) command(_ context.Context, name string, args ...string) (string, string, error) {
	f.actions = append(f.actions, name+" "+strings.Join(args, " "))
	action, label := "", ""
	if name == "launchctl" {
		action = args[0]
		if action == "print-disabled" {
			out := ""
			for label, enabled := range f.enabled {
				if !enabled {
					out += strconv.Quote(label) + " => true\n"
				}
			}
			return out, "", nil
		}
		label = filepath.Base(args[len(args)-1])
		if action == "bootstrap" {
			label = strings.TrimSuffix(label, ".plist")
			action = "load"
		}
		if action == "print" {
			if !f.loaded[label] {
				return "", "Could not find service", fmt.Errorf("not loaded")
			}
			return fmt.Sprintf("pid = %d\n", f.pids[label]), "", nil
		}
		if action == "bootout" {
			action = "stop"
		}
	} else if name == "systemctl" {
		action = args[1]
		if action == "daemon-reload" {
			return "", "", nil
		}
		label = args[2]
		if label == "--now" {
			label = args[3]
		}
		if action == "show" {
			if f.loaded[label] {
				return fmt.Sprint(f.pids[label]), "", nil
			}
			return "0", "", nil
		}
		if action == "is-enabled" {
			if f.enabled[label] {
				return "enabled", "", nil
			}
			return "disabled", "", fmt.Errorf("disabled")
		}
		if action == "start" {
			action = "load"
		}
	} else {
		f.t.Fatalf("unexpected executable %s", name)
	}
	isNew := label == hostbridge.Label || label == hostbridge.Unit
	if action == "load" || action == "enable" && strings.Contains(strings.Join(args, " "), "--now") {
		if isNew && f.fail == "bootstrap" {
			f.fail = ""
			return "", "injected bootstrap failure", fmt.Errorf("bootstrap failed")
		}
		if action == "enable" {
			f.enabled[label] = true
		}
		f.loaded[label] = true
		f.nextPID++
		f.pids[label] = f.nextPID
		return "", "", nil
	}
	if action == "enable" {
		f.enabled[label] = true
		return "", "", nil
	}
	if action == "disable" {
		f.enabled[label] = false
		if name == "systemctl" {
			f.loaded[label] = false
		}
		return "", "", nil
	}
	if action == "stop" {
		if !isNew && f.fail == "quiesce" && strings.Contains(label, "omi") {
			f.fail = ""
			return "", "injected stop failure", fmt.Errorf("stop failed")
		}
		f.loaded[label] = false
		if !isNew && f.lateBusy {
			f.lateBusy = false
			entry, _ := readAskRegistration(f.entries["personal"])
			entry.State = "executing"
			entry.Attempts = 1
			if e := writeAskRegistration(f.entries["personal"], entry); e != nil {
				f.t.Fatal(e)
			}
		}
		return "", "", nil
	}
	f.t.Fatalf("unexpected fake service action %s %v", name, args)
	return "", "", nil
}
func (f *directBridgeFixture) install() error {
	_, e := f.a.installHostBridge(context.Background(), nil, bridgeCfg(hostbridge.Workspace{ConfigDir: f.configDir}, hostbridge.Agent{}))
	return e
}
func (f *directBridgeFixture) assertRestored() {
	f.t.Helper()
	for label, u := range f.units {
		raw, e := os.ReadFile(u.Path)
		if e != nil || !bytes.Equal(raw, u.Content) {
			f.t.Fatal("legacy file not restored", label, e)
		}
		st, e := os.Stat(u.Path)
		if e != nil || st.Mode().Perm() != 0644 {
			f.t.Fatal("legacy file mode changed", label, e)
		}
		if f.loaded[label] != f.priorLoaded[label] || f.enabled[label] != f.priorEnabled[label] {
			f.t.Fatalf("state %s loaded=%v enabled=%v; want %v %v", label, f.loaded[label], f.enabled[label], f.priorLoaded[label], f.priorEnabled[label])
		}
	}
	if f.loaded[hostbridge.Label] || f.loaded[hostbridge.Unit] {
		f.t.Fatal("new generation survived rollback")
	}
}
func TestHostBridgeDirectThreeUnitCutoverAndRecovery(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		for _, failure := range []string{"success", "bootstrap", "readiness", "quiesce", "busy-before", "busy-during", "previously-unloaded", "new-executing"} {
			t.Run(goos+"/"+failure, func(t *testing.T) {
				f := newDirectBridgeFixture(t, goos)
				if failure == "previously-unloaded" {
					for label := range f.units {
						if strings.Contains(label, "personal") {
							f.loaded[label] = false
							f.enabled[label] = false
							f.priorLoaded[label] = false
							f.priorEnabled[label] = false
						}
					}
					f.fail = "bootstrap"
				} else {
					f.fail = failure
				}
				if failure == "busy-before" {
					entry, _ := readAskRegistration(f.entries["personal"])
					entry.State = "executing"
					entry.Attempts = 1
					if e := writeAskRegistration(f.entries["personal"], entry); e != nil {
						t.Fatal(e)
					}
				}
				f.lateBusy = failure == "busy-during"
				e := f.install()
				if failure != "success" {
					if e == nil {
						t.Fatal("failure did not abort cutover")
					}
					f.assertRestored()
					var j bridgeMigrationJournal
					raw, err := integrations.PrivateRead(filepath.Join(hostbridge.Root(f.home), "migration.json"))
					if err != nil || json.Unmarshal(raw, &j) != nil || j.Phase != "rolled_back" {
						t.Fatal("journal not retryable", j.Phase, err)
					}
					if failure == "busy-during" || failure == "new-executing" {
						path := f.entries["personal"] + ".paused"
						entry, err := readAskRegistration(path)
						if err != nil || entry.State != "uncertain" || entry.Command != "printf 'exact retained command'" || entry.Instance != "original-instance" {
							t.Fatal("uncertain command lost or replayable", entry, err)
						}
						if err = writeAskRegistration(f.entries["personal"], entry); err == nil {
							t.Fatal("could overwrite paused outcome")
						}
						diagnosis, err := f.a.inspectHostBridge(context.Background(), true)
						if err != nil {
							t.Fatal(err)
						}
						found := false
						for _, check := range diagnosis.Data.(map[string]any)["checks"].([]map[string]any) {
							if check["check"] == "migration_paused_resumes" {
								found = check["ok"] == false && len(check["paths"].([]string)) == 1
							}
						}
						if !found {
							t.Fatal("doctor missed the quarantined namespace")
						}
						// Owner read-back establishes success before an explicit restore.
						entry.State = "delivered"
						if err = writeAskRegistration(path, entry); err != nil {
							t.Fatal(err)
						}
						if err = os.Rename(path, f.entries["personal"]); err != nil {
							t.Fatal(err)
						}
					}
					if failure == "busy-before" {
						entry, _ := readAskRegistration(f.entries["personal"])
						entry.State = "delivered"
						if err = writeAskRegistration(f.entries["personal"], entry); err != nil {
							t.Fatal(err)
						}
					}
					f.fail = ""
					if e = f.install(); e != nil {
						t.Fatal("journal retry failed", e)
					}
				} else if e != nil {
					t.Fatal(e)
				}
				if !f.ready {
					t.Fatal("new unit was not verified")
				}
				for _, u := range f.units {
					if _, err := os.Stat(u.Path); !os.IsNotExist(err) {
						t.Fatal("legacy file survives successful verified cutover", u.Label, err)
					}
				}
				procs := f.processes()
				if len(procs) != 1 || len(procs[0].Argv) != 3 {
					t.Fatal("not one host process", procs)
				}
				aliases := []string{}
				r, err := hostbridge.Load(hostbridge.Root(f.home))
				if err != nil {
					t.Fatal(err)
				}
				for _, w := range r.Workspaces {
					aliases = append(aliases, w.ID)
				}
				sort.Strings(aliases)
				if strings.Join(aliases, ",") != "anx,omi,personal" {
					t.Fatal(aliases)
				}
				for alias, path := range f.entries {
					if alias == "personal" && (failure == "busy-before" || failure == "busy-during" || failure == "new-executing") {
						continue
					}
					raw, err := os.ReadFile(path)
					if err != nil || !bytes.Equal(raw, f.originals[path]) {
						t.Fatal("idle namespace source bytes changed", alias, err)
					}
				}
			})
		}
	}
}

func TestHostBridgeInstallRecoversInterruptedActivationJournal(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		t.Run(goos, func(t *testing.T) {
			f := newDirectBridgeFixture(t, goos)
			f.fail = "bootstrap"
			if err := f.install(); err == nil {
				t.Fatal("expected failed activation")
			}
			root := hostbridge.Root(f.home)
			var j bridgeMigrationJournal
			raw, err := integrations.PrivateRead(filepath.Join(root, "migration.json"))
			if err != nil || json.Unmarshal(raw, &j) != nil {
				t.Fatal(err)
			}
			// Simulate a process dying during activation, before rollback, with
			// every source unloaded and one source file already missing.
			j.Phase = "activating"
			j.NewActivated = true
			if err = saveBridgeMigration(root, &j); err != nil {
				t.Fatal(err)
			}
			for label := range f.units {
				f.loaded[label] = false
				f.enabled[label] = false
			}
			if err = os.Remove(j.Plan.Units[0].Path); err != nil {
				t.Fatal(err)
			}
			f.fail = ""
			if err = f.install(); err != nil {
				t.Fatal("interrupted journal could not recover and retry", err)
			}
			if !f.ready || len(f.processes()) != 1 {
				t.Fatal("recovery did not complete cutover")
			}
			for _, path := range f.entries {
				b, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(b, f.originals[path]) {
					t.Fatal("resume lost during recovery", err)
				}
			}
		})
	}
}
