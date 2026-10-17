//go:build darwin || linux

package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	hostbridge "agent-nexus-cli/internal/bridge"
	"agent-nexus-cli/internal/filetransport"
	"agent-nexus-cli/internal/integrations"
)

func TestFileSyncDispatchRegistrationFrozenNotesAndOfflineDisable(t *testing.T) {
	f := newHookFixture(t)
	f.filePlan.Store(true)
	project, e := filepath.EvalSymlinks(f.root)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		_ = filepath.WalkDir(project, func(path string, d os.DirEntry, e error) error {
			if e == nil && d.IsDir() {
				return os.Chmod(path, 0700)
			}
			return e
		})
	})
	preview := f.run(t, "sync", "enable", project, "--card-id", "card:work", "--dry-run")
	if preview["enabled"] != false {
		t.Fatal(preview)
	}
	if _, e = os.Stat(filepath.Join(project, ".anx")); !os.IsNotExist(e) {
		t.Fatal(e)
	}
	f.run(t, "sync", "enable", project, "--card-id", "card:work")
	reg, e := hostbridge.Load(hostbridge.Root(f.home))
	if e != nil || len(reg.Workspaces) != 1 {
		t.Fatal(reg, e)
	}
	w := reg.Workspaces[0]
	agent := w.Agents[0]
	if !agent.Jobs["files"] || agent.Jobs["answers"] || agent.Jobs["wake"] || len(agent.FileDirs) != 1 {
		t.Fatal(agent)
	}
	entry := filetransport.Entry{Version: 1, ID: filetransport.UUID(), Card: "card:work", Text: "File writer note", Evidence: []string{" https://example.test/evidence "}, Next: &filetransport.Next{At: "2d", What: "  " + strings.Repeat("r", 170)}}
	raw := integrations.JSON(entry)
	path := filepath.Join(project, ".anx/outbox", entry.ID+".json")
	if e = os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	if _, e = filetransport.Tick(context.Background(), agent.FileDirs[0], now, f.a.fileCallbacks(w, agent)); e != nil {
		t.Fatal(e)
	}
	f.mu.Lock()
	bodies := append([]string{}, f.bodies...)
	f.mu.Unlock()
	if len(bodies) != 1 {
		t.Fatal(bodies)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(bodies[0]), &body)
	payload := asMap(asMap(body["event"])["payload"])
	if anyString(asSlice(payload["evidence_refs"])[0]) != "https://example.test/evidence" || !strings.HasPrefix(anyString(asMap(payload["next"])["what"]), "r") {
		t.Fatal("file note differs from note normalization", payload)
	}
	if payload["text"] != entry.Text || len(asSlice(payload["evidence_refs"])) != 1 || len(anyString(asMap(payload["next"])["what"])) != 160 {
		t.Fatal(body)
	}
	nextAt, _ := time.Parse(time.RFC3339Nano, anyString(asMap(payload["next"])["at"]))
	if !nextAt.Equal(now.Add(48 * time.Hour)) {
		t.Fatal(nextAt, now)
	}
	mirror, e := os.ReadFile(filepath.Join(project, ".anx/cards/card-id.md"))
	if e != nil || !strings.Contains(string(mirror), "Read scoped note [active]") {
		t.Fatal("mirror omitted computed plan state", string(mirror), e)
	}
	// Ordinary bridge add must retain the independently opted-in file roots.
	f.run(t, "bridge", "add", "--as", "worker")
	reg, e = hostbridge.Load(hostbridge.Root(f.home))
	if e != nil || len(reg.Workspaces[0].Agents[0].FileDirs) != 1 || !reg.Workspaces[0].Agents[0].Jobs["files"] {
		t.Fatal(reg, e)
	}
	f.server.Close()
	status := f.run(t, "sync", "status", project)
	if status["enabled"] != true {
		t.Fatal(status)
	}
	disabled := f.run(t, "sync", "disable", project)
	if disabled["stale"] != true {
		t.Fatal("disabled mirror reported fresh", disabled)
	}
	reg, e = hostbridge.Load(hostbridge.Root(f.home))
	if e != nil || reg.Workspaces[0].Agents[0].Jobs["files"] || !reg.Workspaces[0].Agents[0].Jobs["answers"] {
		t.Fatal(reg, e)
	}
}
func TestSyncUsageAndMetadataBeforeEnrollment(t *testing.T) {
	a, out, root := integrationTestApp(t)
	if code := a.Run([]string{"--json", "sync", "status", root}); code != 0 || !strings.Contains(out.String(), `"configured": false`) {
		t.Fatal("default-off status requires enrollment", code, out.String())
	}

	for _, args := range [][]string{{"sync", "enable"}, {"sync", "enable", ".", "--card-id", "topic:wrong"}, {"sync", "status", ".", "--card-id", "card:work"}, {"sync", "unknown"}} {
		a, out, _ := integrationTestApp(t)
		a.Getenv = func(string) string { return "" }
		if code := a.Run(append([]string{"--json"}, args...)); code != 2 {
			t.Fatal(args, code, out.String())
		}
	}
	for _, args := range [][]string{{"sync", "--help"}, {"sync", "enable", "--help"}, {"help", "sync"}} {
		a, out, _ := integrationTestApp(t)
		if code := a.Run(args); code != 0 || !strings.Contains(out.String(), "file") {
			t.Fatal(args, code, out.String())
		}
	}
	for cmd, want := range map[string]string{"sync status": "read_only", "sync enable": "local_operational_write", "sync disable": "local_operational_write"} {
		if got := commandSideEffectClass(cmd); got != want {
			t.Fatal(cmd, got)
		}
	}
}

func TestFileDoctorIsLocalReadOnlyAndCoversStaleness(t *testing.T) {
	f := newHookFixture(t)
	project, e := filepath.EvalSymlinks(f.root)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		_ = filepath.WalkDir(project, func(path string, d os.DirEntry, e error) error {
			if e == nil && d.IsDir() {
				return os.Chmod(path, 0700)
			}
			return e
		})
	})
	f.run(t, "sync", "enable", project, "--card-id", "card:work")
	oldRun, oldProcesses := bridgeCommandRun, bridgeProcesses
	t.Cleanup(func() { bridgeCommandRun, bridgeProcesses = oldRun, oldProcesses })
	bridgeCommandRun = func(context.Context, string, ...string) (string, string, error) {
		t.Fatal("doctor unexpectedly called a service manager for empty fixture HOME")
		return "", "", nil
	}
	bridgeProcesses = func(context.Context) ([]hostbridge.Process, error) { return nil, nil }
	result, e := f.a.inspectHostBridge(context.Background(), true)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, raw := range asMap(result.Data)["checks"].([]map[string]any) {
		check := asMap(raw)
		if check["check"] == "file_transport" {
			found = true
			if check["ok"] != false || asMap(check["status"])["stale"] != true {
				t.Fatal(check)
			}
		}
	}
	if !found {
		t.Fatal("doctor omitted file job", result.Data)
	}
	f.run(t, "sync", "disable", project)
	// Removed projects can still stop their private binding and registration offline.
	_ = filepath.WalkDir(project, func(path string, d os.DirEntry, e error) error {
		if e == nil && d.IsDir() {
			return os.Chmod(path, 0700)
		}
		return e
	})
	if e = os.RemoveAll(filepath.Join(project, ".anx")); e != nil {
		t.Fatal(e)
	}
	if e = os.Remove(project); e != nil {
		t.Fatal(e)
	}
	f.server.Close()
	f.run(t, "sync", "disable", project)
}

func TestFileJobRevocationClearsEveryRegisteredDirectory(t *testing.T) {
	f := newHookFixture(t)
	projects := []string{}
	for i := 0; i < 2; i++ {
		project, e := filepath.EvalSymlinks(t.TempDir())
		if e != nil {
			t.Fatal(e)
		}
		projects = append(projects, project)
		t.Cleanup(func() {
			_ = filepath.WalkDir(project, func(path string, d os.DirEntry, e error) error {
				if e == nil && d.IsDir() {
					return os.Chmod(path, 0700)
				}
				return e
			})
		})
		f.run(t, "sync", "enable", project, "--card-id", "card:work")
	}
	r, e := hostbridge.Load(hostbridge.Root(f.home))
	if e != nil {
		t.Fatal(e)
	}
	w := r.Workspaces[0]
	agent := w.Agents[0]
	for _, dir := range agent.FileDirs {
		if e = f.a.bridgeFilesTick(context.Background(), w, agent, dir); e != nil {
			t.Fatal(e)
		}
	}
	for _, project := range projects {
		if _, e = os.Stat(filepath.Join(project, ".anx/cards/card-id.md")); e != nil {
			t.Fatal(e)
		}
	}
	f.identityDenied.Store(true)
	if e = f.a.bridgeFilesTick(context.Background(), w, agent, agent.FileDirs[0]); e == nil {
		t.Fatal("revoked identity accepted")
	}
	for _, project := range projects {
		if _, e = os.Stat(filepath.Join(project, ".anx/cards/card-id.md")); !os.IsNotExist(e) {
			t.Fatal("revoked directory mirror retained", project, e)
		}
	}
}
