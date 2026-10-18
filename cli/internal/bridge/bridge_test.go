package bridge

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMigrationCustomWrapperRetainsThreeWorkspaceInvocations(t *testing.T) {
	home := t.TempDir()
	exe := filepath.Join(home, "anx")
	if e := os.WriteFile(exe, []byte("fixture"), 0700); e != nil {
		t.Fatal(e)
	}
	units := []LegacyUnit{{Path: filepath.Join(home, "custom.plist"), Label: "io.agent-nexus.hermes-answer.anx", Kind: "launchd", PID: 100, Argv: []string{"/bin/sh", filepath.Join(home, "wrapper")}, Environment: map[string]string{}}}
	processes := []Process{{PID: 100, UID: os.Getuid(), Started: "wrapper", Executable: "/bin/sh", Argv: units[0].Argv}}
	for i, alias := range []string{"personal", "anx", "omi"} {
		processes = append(processes, Process{PID: 101 + i, PPID: 100, UID: os.Getuid(), Started: fmt.Sprint(i), Executable: exe, CWD: home, Argv: []string{exe, "--as", "hermes", "--workspace", alias, "bridge", "run", "--command-timeout", "30m"}})
	}
	plan := PlanMigration(home, exe, os.Getuid(), units, processes)
	if len(plan.Blockers) > 0 || len(plan.Units) != 1 || len(plan.Invocations) != 3 || len(plan.Processes) != 3 {
		t.Fatal(plan)
	}
	seen := map[string]bool{}
	for _, v := range plan.Invocations {
		seen[v.Workspace] = true
		if v.Agent != "hermes" || v.Policy.Timeout != 30*time.Minute {
			t.Fatal(v)
		}
	}
	if !seen["personal"] || !seen["anx"] || !seen["omi"] {
		t.Fatal(seen)
	}
	processes[1].UID++
	if len(PlanMigration(home, exe, os.Getuid(), units, processes).Blockers) == 0 {
		t.Fatal("foreign process accepted")
	}
}
func TestUnitParsersAndSingleHostPlans(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		t.Run(goos, func(t *testing.T) {
			home := t.TempDir()
			plan, e := UnitFor(goos, home, "/opt/bin/anx", 501)
			if e != nil {
				t.Fatal(e)
			}
			parsed, e := ParseUnit(plan.Path, plan.Kind, []byte(plan.Content))
			if e != nil || len(parsed.Argv) != 3 || parsed.Argv[1] != "bridge" || parsed.Argv[2] != "run" {
				t.Fatal(parsed, e)
			}
			if value, ok := parsed.Environment["ANX_AS"]; !ok || value != "" {
				t.Fatal("host service can inherit a legacy identity", parsed.Environment)
			}
			if strings.Contains(plan.Content, "--as") || strings.Contains(plan.Content, "--workspace") {
				t.Fatal(plan.Content)
			}
		})
	}
}
func TestSupervisorOfflineJobCannotBlockOthersOrHeartbeat(t *testing.T) {
	root := Root(t.TempDir())
	w := Workspace{ID: "ws", BaseURL: "http://fixture.test", HostID: "host", ConfigDir: filepath.Dir(root)}
	a := Agent{Name: "worker", ActorID: "actor", Jobs: map[string]bool{"answers": true, "git": true, "spool": true}, Policy: Policy{5, time.Minute}}
	if e := Update(context.Background(), root, func(r *Registry) error { return Register(r, w, a) }); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var jobs atomic.Int32
	var reported atomic.Bool
	e := Supervise(ctx, root, func(ctx context.Context, _ Workspace, _ Agent, kind string) error {
		if kind == "answers" {
			<-ctx.Done()
			return ctx.Err()
		}
		jobs.Add(1)
		return nil
	}, func(context.Context, Registry) { reported.Store(true) })
	if e != nil || jobs.Load() < 2 || !reported.Load() {
		t.Fatal(jobs.Load(), reported.Load(), e)
	}
}

func TestStoppedSystemdUnitPreservesHomeWorkingDirectory(t *testing.T) {
	home := t.TempDir()
	exe := filepath.Join(home, "anx")
	if e := os.WriteFile(exe, []byte("fixture"), 0700); e != nil {
		t.Fatal(e)
	}
	u, e := ParseUnit(filepath.Join(home, "legacy.service"), "systemd-user", []byte("[Service]\nExecStart="+exe+" --as worker bridge run\nWorkingDirectory=%h\n"))
	if e != nil {
		t.Fatal(e)
	}
	plan := PlanMigration(home, exe, os.Getuid(), []LegacyUnit{u}, nil)
	if len(plan.Blockers) != 0 || len(plan.Invocations) != 1 || plan.Invocations[0].CWD != home {
		t.Fatal(plan)
	}
}

func TestMigrationSkipsUnrelatedSymlinkButBlocksItsBridgeConsumer(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		t.Run(goos, func(t *testing.T) {
			home := t.TempDir()
			unit, err := UnitFor(goos, home, filepath.Join(home, "anx"), os.Getuid())
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(filepath.Dir(unit.Path), "unrelated.plist")
			if goos == "linux" {
				path = filepath.Join(filepath.Dir(unit.Path), "unrelated.service")
			}
			if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.Symlink("/fixture/do-not-read", path); err != nil {
				t.Fatal(err)
			}
			units, err := ReadUnits(home, goos)
			if err != nil || len(units) != 1 || units[0].Unresolved == "" {
				t.Fatal(units, err)
			}
			if p := PlanMigration(home, filepath.Join(home, "anx"), os.Getuid(), units, nil); len(p.Blockers) != 0 || len(p.Units) != 0 {
				t.Fatal(p)
			}
			units[0].PID = 100
			proc := Process{PID: 100, UID: os.Getuid(), Argv: []string{filepath.Join(home, "anx"), "--as", "worker", "bridge", "run"}, Executable: filepath.Join(home, "anx"), CWD: home}
			if p := PlanMigration(home, filepath.Join(home, "anx"), os.Getuid(), units, []Process{proc}); len(p.Blockers) == 0 {
				t.Fatal("unverified unit accepted", p)
			}
		})
	}
}

func TestMigrationMalformedUnitsAreScopedToPossibleBridgeConsumers(t *testing.T) {
	const zeroclaw = `<?xml version=\"1.0\" encoding=\"UTF-8\"?>
<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">
<plist version=\"1.0\"><dict>
<key>Label</key><string>com.zeroclaw.daemon</string>
<key>ProgramArguments</key><array><string>/opt/bin/zeroclaw</string><string>daemon</string><string>preserve \"literal quotes\"</string></array>
<key>Comment</key><string>preserve \"literal quotes\"</string>
</dict></plist>`
	u, err := ParseUnit("com.zeroclaw.daemon.plist", "launchd", []byte(zeroclaw))
	if err != nil || len(u.Argv) != 3 || u.Argv[0] != "/opt/bin/zeroclaw" || u.Argv[2] != `preserve \"literal quotes\"` || string(u.Content) != zeroclaw {
		t.Fatal(u, err)
	}
	for _, tc := range []struct {
		name, raw string
		blocked   bool
	}{
		{"com.zeroclaw.daemon.plist", zeroclaw, false},
		{"unrelated.plist", `<plist version=bad><dict><string>/opt/bin/zeroclaw</string>`, false},
		{"custom.plist", `<plist version=bad><dict><string>/opt/bin/anx bridge run</string>`, true},
		{"split.plist", `<plist version=bad><array><string>/custom/tool</string><string>bridge</string><string>run</string>`, true},
		{"wrapper.plist", `<plist version=bad><string>/opt/bin/anx-hermes</string>`, true},
		{"label.plist", `<plist version=bad><string>io.agent-nexus.hermes-answer</string>`, true},
		{"io.agent-nexus.bridge.plist", `<plist version=bad>`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			path := filepath.Join(home, "Library", "LaunchAgents", tc.name)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tc.raw), 0600); err != nil {
				t.Fatal(err)
			}
			units, err := ReadUnits(home, "darwin")
			if err != nil {
				t.Fatal(err)
			}
			plan := PlanMigration(home, filepath.Join(home, "anx"), os.Getuid(), units, nil)
			if (len(plan.Blockers) > 0) != tc.blocked {
				t.Fatal(plan)
			}
			if tc.blocked && (len(units) != 1 || units[0].Unresolved == "") {
				t.Fatal(units)
			}
		})
	}
}
