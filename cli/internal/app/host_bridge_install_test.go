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
	"strings"
	"testing"
	"time"

	hostbridge "agent-nexus-cli/internal/bridge"
	"agent-nexus-cli/internal/hostidentity"
	"agent-nexus-cli/internal/integrations"
	"agent-nexus-cli/internal/workspaceconfig"
)

func TestHostBridgeInstallMigratesThreeWorkspaceWrapperAndPreservesResumeBytes(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		t.Run(goos, func(t *testing.T) {
			home := t.TempDir()
			configDir := filepath.Join(home, "custom-config")
			a, _ := workspaceTestApp(t, home, home, map[string]string{"ANX_CONFIG_DIR": configDir})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
				alias := parts[0]
				switch strings.Join(parts[1:], "/") {
				case "meta/handshake":
					fmt.Fprintf(w, `{"workspace_id":%q,"capabilities":["host_bridge_v1"]}`, alias)
				case "auth/token":
					fmt.Fprintf(w, `{"agent":{"id":%q,"actor_id":%q,"handle":"worker.fixture"},"tokens":{"access_token":"fixture","expires_in":3600}}`, alias, "actor-"+alias)
				default:
					t.Errorf("unexpected %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			oldRun, oldProcesses, oldPlatform, oldWait, oldRetire := bridgeCommandRun, bridgeProcesses, bridgePlatform, bridgeWaitReady, bridgeRetire
			t.Cleanup(func() {
				bridgeCommandRun, bridgeProcesses, bridgePlatform, bridgeWaitReady, bridgeRetire = oldRun, oldProcesses, oldPlatform, oldWait, oldRetire
			})
			bridgePlatform = goos
			exe, e := os.Executable()
			if e != nil {
				t.Fatal(e)
			}
			label := "io.agent-nexus.hermes-answer.anx"
			path := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
			kind := "launchd"
			content := `<?xml version="1.0"?><plist><dict><key>Label</key><string>` + label + `</string><key>ProgramArguments</key><array><string>/bin/sh</string><string>/fixture/wrapper</string></array></dict></plist>`
			if goos == "linux" {
				label = "custom-answer-wrapper.service"
				path = filepath.Join(home, ".config", "systemd", "user", label)
				kind = "systemd-user"
				content = "[Service]\nExecStart=/bin/sh /fixture/wrapper\n"
			}
			_ = kind
			if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(path, []byte(content), 0600); e != nil {
				t.Fatal(e)
			}
			// An unrelated malformed service must not block either preview or cutover.
			if goos == "darwin" {
				if e = os.WriteFile(filepath.Join(filepath.Dir(path), "unrelated.plist"), []byte(`<plist version=bad><string>/opt/bin/zeroclaw</string>`), 0600); e != nil {
					t.Fatal(e)
				}
			}
			processes := []hostbridge.Process{{PID: 700, UID: os.Getuid(), Started: "wrapper", Executable: "/bin/sh", Argv: []string{"/bin/sh", "/fixture/wrapper"}}}
			original := map[string][]byte{}
			for i, alias := range []string{"personal", "anx", "omi"} {
				base := server.URL + "/" + alias
				_, key, e := ed25519.GenerateKey(nil)
				if e != nil {
					t.Fatal(e)
				}
				if e = hostidentity.SaveAt(configDir, hostidentity.Host{ID: "host-" + alias, KeyID: "key", Slug: "fixture", WorkspaceID: alias, BaseURL: base}, key); e != nil {
					t.Fatal(e)
				}
				if e = workspaceconfig.Update(configDir, func(c *workspaceconfig.Catalog) error { c.File.AddAlias(alias, base); return nil }); e != nil {
					t.Fatal(e)
				}
				dir := filepath.Join(configDir, "answer-resumes", hostbridge.Namespace(base, "actor-"+alias))
				if e = os.MkdirAll(dir, 0700); e != nil {
					t.Fatal(e)
				}
				for _, state := range []string{"pending", "delivered", "acknowledged"} {
					entry := askResumeRegistration{AskID: "event:" + alias + "-" + state, BaseURL: base, ActorID: "actor-" + alias, Command: "printf 'registered exact bytes'", Dir: home, State: state, Subscription: "subscription-" + alias, Instance: "original-claim", Wakeup: "original-wake"}
					entry.Acknowledged = state == "acknowledged"
					p := askRegistryPath(dir, entry.AskID)
					if e = writeAskRegistration(p, entry); e != nil {
						t.Fatal(e)
					}
					original[p], e = os.ReadFile(p)
					if e != nil {
						t.Fatal(e)
					}
				}
				processes = append(processes, hostbridge.Process{PID: 701 + i, PPID: 700, UID: os.Getuid(), Started: fmt.Sprint(i), Executable: exe, CWD: home, Argv: []string{exe, "--config-dir", configDir, "--as", "hermes", "--workspace", alias, "bridge", "run", "--command-timeout", "30m"}})
			}
			actions := []string{}
			bridgeProcesses = func(context.Context) ([]hostbridge.Process, error) {
				return append([]hostbridge.Process{}, processes...), nil
			}
			bridgeRetire = func(_ context.Context, p hostbridge.Process) error {
				if p.PID < 701 || p.PID > 703 {
					t.Fatal("unverified process signalled", p)
				}
				return nil
			}
			bridgeCommandRun = func(_ context.Context, name string, args ...string) (string, string, error) {
				actions = append(actions, name+" "+strings.Join(args, " "))
				joined := strings.Join(args, " ")
				if name == "systemctl" && len(args) > 1 && args[1] == "is-enabled" {
					return "enabled", "", nil
				}
				if strings.Contains(joined, "print") {
					return "pid = 700\n", "", nil
				}
				if strings.Contains(joined, "show") {
					return "700", "", nil
				}
				if strings.Contains(joined, "bootout") || strings.Contains(joined, "--now "+label) {
					processes = nil
				}
				return "", "", nil
			}
			var unlock func()
			bridgeWaitReady = func(_ context.Context, root string, r hostbridge.Registry) error {
				if len(r.Workspaces) != 3 {
					t.Fatal(r)
				}
				for _, w := range r.Workspaces {
					if len(w.Agents) != 1 || w.Agents[0].Policy.Timeout != 30*time.Minute || !w.Agents[0].Jobs["answers"] {
						t.Fatal(w)
					}
				}
				processes = []hostbridge.Process{{PID: 900, UID: os.Getuid(), Started: "new", Executable: exe, Argv: []string{exe, "bridge", "run"}}}
				var err error
				unlock, err = lockAskBridge(filepath.Join(root, "service.lock"))
				return err
			}
			t.Cleanup(func() {
				if unlock != nil {
					unlock()
				}
			})
			cfg := bridgeCfg(hostbridge.Workspace{ConfigDir: configDir}, hostbridge.Agent{})
			preview, e := a.installHostBridge(context.Background(), []string{"--dry-run"}, cfg)
			if e != nil {
				t.Fatal(e)
			}
			if asMap(preview.Data)["imported"] != 3 {
				t.Fatal(preview.Data)
			}
			for _, cmd := range actions {
				if strings.Contains(cmd, "bootout") || strings.Contains(cmd, " disable ") {
					t.Fatal("preview mutated a service", actions)
				}
			}
			if _, e = os.Stat(filepath.Join(hostbridge.Root(home), "registry.json")); !os.IsNotExist(e) {
				t.Fatal("preview wrote registry", e)
			}
			result, e := a.installHostBridge(context.Background(), nil, cfg)
			if e != nil {
				t.Fatal(e)
			}
			if asMap(result.Data)["started"] != true {
				t.Fatal(result)
			}
			r, e := hostbridge.Load(hostbridge.Root(home))
			if e != nil || len(r.Workspaces) != 3 {
				t.Fatal(r, e)
			}
			if _, e = os.Stat(path); !os.IsNotExist(e) {
				t.Fatal("legacy restart source remains", e)
			}
			unit, e := hostbridge.UnitFor(goos, home, exe, os.Getuid())
			if e != nil {
				t.Fatal(e)
			}
			raw, e := os.ReadFile(unit.Path)
			if e != nil || strings.Contains(string(raw), "--as") {
				t.Fatal(string(raw), e)
			}
			for path, before := range original {
				after, e := os.ReadFile(path)
				if e != nil || !bytes.Equal(before, after) {
					t.Fatal("resume command/state changed", path, e)
				}
			}
			result, e = a.installHostBridge(context.Background(), nil, cfg)
			if e != nil || asMap(result.Data)["already_current"] != true {
				t.Fatal(result, e)
			}
			raw, e = integrations.PrivateRead(filepath.Join(hostbridge.Root(home), "migration.json"))
			var journal bridgeMigrationJournal
			if e != nil || json.Unmarshal(raw, &journal) != nil || journal.Phase != "complete" {
				t.Fatal(journal, e)
			}
		})
	}
}
func TestHostBridgeMigrationPausesInterruptedCommands(t *testing.T) {
	home := t.TempDir()
	w := hostbridge.Workspace{ID: "ws", BaseURL: "http://fixture.test", HostID: "host", ConfigDir: home}
	agent := hostbridge.Agent{Name: "worker", ActorID: "actor", Jobs: map[string]bool{"answers": true}, RegistryDirs: []string{filepath.Join(home, "answer-resumes", hostbridge.Namespace(w.BaseURL, "actor"))}, Policy: hostbridge.Policy{MaxAttempts: 5, Timeout: time.Minute}}
	w.Agents = []hostbridge.Agent{agent}
	dir := agent.RegistryDirs[0]
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	p := askRegistryPath(dir, "ask")
	entry := askResumeRegistration{AskID: "ask", BaseURL: w.BaseURL, ActorID: "actor", Command: "never execute me", State: "pending", Attempts: 1}
	if e := writeAskRegistration(p, entry); e != nil {
		t.Fatal(e)
	}
	if e := checkBridgeResumeRoots(w, true); e == nil {
		t.Fatal("interrupted attempt accepted")
	}
	got, e := readAskRegistration(p)
	if e != nil || got.State != "uncertain" || got.Command != entry.Command {
		t.Fatal(got, e)
	}
}

func TestHostBridgeInstallRequiresCoreCapabilityBeforeServiceChanges(t *testing.T) {
	f := newHookFixture(t)
	f.run(t, "bridge", "add", "--as", "worker")
	f.handshakeUnsupported.Store(true)
	originalRun, originalProcesses := bridgeCommandRun, bridgeProcesses
	t.Cleanup(func() { bridgeCommandRun, bridgeProcesses = originalRun, originalProcesses })
	calls := 0
	bridgeCommandRun = func(context.Context, string, ...string) (string, string, error) { calls++; return "", "", nil }
	bridgeProcesses = func(context.Context) ([]hostbridge.Process, error) { return nil, nil }
	_, e := f.a.installHostBridge(context.Background(), nil, bridgeCfg(hostbridge.Workspace{ConfigDir: f.dir}, hostbridge.Agent{}))
	if e == nil || !strings.Contains(e.Error(), "host_bridge_v1") || calls != 0 {
		t.Fatal(e, calls)
	}
}
func TestHostBridgeInstallActivationFailureRestoresPreviouslyUnloadedSource(t *testing.T) {
	f := newHookFixture(t)
	f.run(t, "bridge", "add", "--as", "worker")
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	label := "old-answer"
	path := filepath.Join(f.home, "Library", "LaunchAgents", label+".plist")
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	content := `<plist><dict><key>Label</key><string>` + label + `</string><key>ProgramArguments</key><array><string>` + exe + `</string><string>--as</string><string>worker</string><string>--base-url</string><string>` + f.server.URL + `</string><string>--config-dir</string><string>` + f.dir + `</string><string>bridge</string><string>run</string></array></dict></plist>`
	if e = os.WriteFile(path, []byte(content), 0600); e != nil {
		t.Fatal(e)
	}
	originalRun, originalProcesses, originalOS := bridgeCommandRun, bridgeProcesses, bridgePlatform
	t.Cleanup(func() { bridgeCommandRun, bridgeProcesses, bridgePlatform = originalRun, originalProcesses, originalOS })
	bridgePlatform = "darwin"
	bridgeProcesses = func(context.Context) ([]hostbridge.Process, error) { return nil, nil }
	actions := []string{}
	bridgeCommandRun = func(_ context.Context, _ string, args ...string) (string, string, error) {
		line := strings.Join(args, " ")
		actions = append(actions, line)
		if strings.HasPrefix(line, "print-disabled") {
			return "{}", "", nil
		}
		if strings.HasPrefix(line, "print ") {
			return "", "Could not find service", fmt.Errorf("missing")
		}
		if strings.HasPrefix(line, "bootstrap") && strings.Contains(line, hostbridge.Label) {
			return "", "fixture activation failure", fmt.Errorf("failed")
		}
		return "", "", nil
	}
	_, e = f.a.installHostBridge(context.Background(), nil, bridgeCfg(hostbridge.Workspace{ConfigDir: f.dir}, hostbridge.Agent{}))
	if e == nil {
		t.Fatal("activation failure ignored")
	}
	for _, line := range actions {
		if strings.HasPrefix(line, "bootstrap") && strings.Contains(line, label+".plist") {
			t.Fatal("old consumer restarted after activation uncertainty", actions)
		}
	}
	raw, e := integrations.PrivateRead(filepath.Join(hostbridge.Root(f.home), "migration.json"))
	var journal bridgeMigrationJournal
	if e != nil || json.Unmarshal(raw, &journal) != nil || journal.Phase != "rolled_back" || len(journal.Plan.Units) != 1 || string(journal.Plan.Units[0].Content) != content {
		t.Fatal(journal, e)
	}
}

func TestHostBridgeMigrationRefusesUncoveredRetainedWork(t *testing.T) {
	root := t.TempDir()
	base := "http://fixture.test"
	dir := filepath.Join(root, "answer-resumes", hostbridge.Namespace(base, "actor"))
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	path := askRegistryPath(dir, "ask")
	entry := askResumeRegistration{AskID: "ask", ActorID: "actor", BaseURL: base, Command: "retained command", State: "pending"}
	if e := writeAskRegistration(path, entry); e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(path)
	r := hostbridge.Registry{Version: 1, Workspaces: []hostbridge.Workspace{}}
	if e := mergeResumeCoverage(&r, map[string]bool{root: true}); e == nil {
		t.Fatal("install could lose an unregistered namespace")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("orphan recovery changed source bytes")
	}
}

func TestHostBridgeDoctorInventoryIsReadOnlyAndRemovalRetainsCommands(t *testing.T) {
	f := newHookFixture(t)
	f.run(t, "bridge", "add", "--as", "worker")
	if bridgePlatform == "darwin" {
		units := filepath.Join(f.home, "Library", "LaunchAgents")
		if err := os.MkdirAll(units, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(units, "unrelated.plist"), []byte(`<plist version=bad><string>/opt/bin/zeroclaw</string>`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	root := hostbridge.Root(f.home)
	r, e := hostbridge.Load(root)
	if e != nil {
		t.Fatal(e)
	}
	agent := r.Workspaces[0].Agents[0]
	dir := agent.RegistryDirs[0]
	path := askRegistryPath(dir, "retained")
	entry := askResumeRegistration{AskID: "retained", BaseURL: f.server.URL, ActorID: agent.ActorID, Command: "exact command", State: "uncertain"}
	if e = writeAskRegistration(path, entry); e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(path)
	oldRun, oldProcesses := bridgeCommandRun, bridgeProcesses
	t.Cleanup(func() { bridgeCommandRun, bridgeProcesses = oldRun, oldProcesses })
	bridgeCommandRun = func(_ context.Context, name string, argv ...string) (string, string, error) {
		if len(argv) == 0 || argv[0] != "print" && argv[0] != "print-disabled" && !(len(argv) > 1 && argv[0] == "--user" && argv[1] == "show") {
			t.Fatalf("doctor mutated service: %s %v", name, argv)
		}
		return "", "Could not find service", fmt.Errorf("missing")
	}
	bridgeProcesses = func(context.Context) ([]hostbridge.Process, error) {
		return []hostbridge.Process{{PID: 99, UID: os.Getuid(), Started: "fixture", Executable: "/unproven", Argv: []string{"anx", "--as", "worker", "bridge", "run"}}}, nil
	}
	result, e := f.a.inspectHostBridge(context.Background(), true)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(result.Data)
	var resume map[string]any
	for _, check := range asMap(result.Data)["checks"].([]map[string]any) {
		if check["check"] == "resume_namespace" {
			resume = check
		}
	}
	if resume["ok"] != false {
		t.Fatal("uncertain resume did not fail doctor", resume)
	}
	if !strings.Contains(string(raw), `"legacy_consumers":0`) || !strings.Contains(string(raw), "unowned bridge consumer PID 99") || !strings.Contains(string(raw), `"check":"resume_namespace"`) || !strings.Contains(string(raw), `"error":"resume outcome uncertain; reconcile retained record before enabling this namespace"`) || strings.Contains(string(raw), entry.Command) {
		t.Fatal(string(raw))
	}
	cfg := bridgeCfg(r.Workspaces[0], agent)
	result, e = f.a.removeHostBridgeAgent(context.Background(), []string{"--as", "worker"}, cfg)
	if e != nil || asMap(result.Data)["removed"] != true {
		t.Fatal(result, e)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("removal deleted or modified retained work")
	}
	r, e = hostbridge.Load(root)
	if e != nil || len(r.Workspaces[0].Agents) != 0 {
		t.Fatal(r, e)
	}
}
func TestHostBridgeInventoryErrorsAreNotTreatedAsStopped(t *testing.T) {
	old := bridgeCommandRun
	t.Cleanup(func() { bridgeCommandRun = old })
	bridgeCommandRun = func(context.Context, string, ...string) (string, string, error) {
		return "", "permission denied", fmt.Errorf("denied")
	}
	if _, e := loadedBridgeUnits(context.Background(), []hostbridge.LegacyUnit{{Kind: "launchd", Label: "fixture"}}); e == nil {
		t.Fatal("permission failure treated as absent")
	}
}

func TestHostBridgeImportsPythonRuntimeWithoutLosingAnswerRegistrations(t *testing.T) {
	f := newHookFixture(t)
	f.run(t, "bridge", "add", "--as", "worker")
	r, e := hostbridge.Load(hostbridge.Root(f.home))
	if e != nil {
		t.Fatal(e)
	}
	w := r.Workspaces[0]
	agent := w.Agents[0]
	path := filepath.Join(t.TempDir(), "bridge.toml")
	raw := fmt.Sprintf("[host]\nbase_url=%q\nid=%q\nslug=\"fixture\"\nconfig_dir=%q\n[agents.worker]\ncommand=[\"/bin/cat\"]\ncwd=%q\nadapter=\"generic\"\n[agents.worker.env]\nRUNTIME_MODE=\"fixture\"\n", w.BaseURL, w.HostID, w.ConfigDir, f.home)
	if e = os.WriteFile(path, []byte(raw), 0600); e != nil {
		t.Fatal(e)
	}
	if e = f.a.importPythonBridge(context.Background(), hostbridge.LegacyInvocation{PythonConfig: path, ConfigDir: w.ConfigDir, Policy: agent.Policy}, &r); e != nil {
		t.Fatal(e)
	}
	got := r.Workspaces[0].Agents[0]
	if !got.Jobs["wake"] || !got.Jobs["answers"] || got.Runtime == nil || got.Runtime.Argv[0] != "/bin/cat" || got.Runtime.Env["RUNTIME_MODE"] != "fixture" || got.RegistryDirs[0] != agent.RegistryDirs[0] {
		t.Fatal(got)
	}
}
