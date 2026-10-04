package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/skills"
)

func isolatedSkillsApp(home string) *App {
	a := New()
	a.UserHomeDir = func() (string, error) { return home, nil }
	a.Getenv = func(string) string { return "" }
	a.skillLookPath = func(string) (string, error) { return "", errors.New("not installed") }
	return a
}

func TestSkillsHarnessDetection(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	for _, rel := range []string{".agents", ".claude", ".cursor", ".hermes", ".gemini", ".pi/agent", ".continue", ".config/opencode"} {
		if err := os.MkdirAll(filepath.Join(home, rel), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	targets := detectSkillHarnesses(home, func(string) string { return filepath.Join(home, ".config") }, func(string) (string, error) {
		return "", errors.New("not installed")
	})
	got := map[string]string{}
	for _, target := range targets {
		for _, harness := range target.Harnesses {
			got[harness] = target.Root
		}
	}
	for _, harness := range []string{"codex", "omp", "claude", "cursor", "hermes", "gemini", "pi", "continue", "opencode"} {
		if got[harness] == "" {
			t.Errorf("harness %q was not detected: %#v", harness, got)
		}
	}
	if got["codex"] != got["omp"] || got["codex"] != filepath.Join(home, ".agents", "skills") {
		t.Fatalf("Codex/OMP did not share agentctl's canonical root: %#v", got)
	}
	if got["opencode"] != filepath.Join(home, ".config", "opencode", "skills") {
		t.Fatalf("unexpected OpenCode root: %q", got["opencode"])
	}
}

func TestSkillsSyncDryRunDoesNotWrite(t *testing.T) {
	t.Parallel()
	home := skillTestDir(t)
	if err := os.Mkdir(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	legacyDir := filepath.Join(home, ".claude", "skills", "anx-cli-onboard")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyContent := []byte("Use config use <profile>, auth register, and ANX_AGENT.\n")
	if err := os.WriteFile(filepath.Join(legacyDir, "SKILL.md"), legacyContent, 0o644); err != nil {
		t.Fatal(err)
	}
	a := isolatedSkillsApp(home)
	result, _, err := a.runSkillsSync([]string{"--dry-run"}, config.Resolved{})
	if err != nil {
		t.Fatal(err)
	}
	data := asMap(result.Data)
	states := data["skills"].([]managedSkillState)
	if len(states) != 1 || states[0].Harness != "claude" || states[0].State != "missing" {
		t.Fatalf("unexpected dry-run states: %#v", states)
	}
	legacy := data["legacy_skills"].([]legacySkillFinding)
	if len(legacy) != 1 || legacy[0].Name != "anx-cli-onboard" || legacy[0].ContentSHA256 != skills.Digest(legacyContent) {
		t.Fatalf("legacy copy was not reported for explicit adoption: %#v", legacy)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "anx-participant")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run created participant copy: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "anx", skillsSyncConfigName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run wrote preferences: %v", err)
	}
}

func TestSkillsAppRunAcceptsEverySyncFlagForm(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		flag string
		want string
	}{
		{name: "pm", flag: "--pm", want: "pm_enabled=true"},
		{name: "no pm", flag: "--no-pm", want: "pm_enabled=false"},
		{name: "auto sync", flag: "--auto-sync", want: "auto_sync=true"},
		{name: "no auto sync", flag: "--no-auto-sync", want: "auto_sync=false"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			a := isolatedSkillsApp(home)
			a.Stdout = &stdout
			a.Stderr = &stderr
			code := a.Run([]string{"skills", "sync", "--home", home, "--dry-run", tc.flag})
			if code != 0 || !strings.Contains(stdout.String(), tc.want) {
				t.Fatalf("App.Run rejected %s: exit=%d stdout=%s stderr=%s", tc.flag, code, stdout.String(), stderr.String())
			}
		})
	}
	var stdout, stderr bytes.Buffer
	a := isolatedSkillsApp(home)
	a.Stdout = &stdout
	a.Stderr = &stderr
	if code := a.Run([]string{"skills", "sync", "--home", home, "--scheduled"}); code != 0 {
		t.Fatalf("App.Run rejected the detached --scheduled form: exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	preferences, exists, err := readSkillsSyncConfig(filepath.Join(home, ".config", "anx"))
	if err != nil || !exists || preferences.LastAutoSyncAt == "" {
		t.Fatalf("successful scheduled run did not record its completion: %+v exists=%t err=%v", preferences, exists, err)
	}
}

func TestSkillsSyncReportsSharedLegacyRootOnce(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	legacyDir := filepath.Join(home, ".agents", "skills", "anx")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("Use config use <profile> and auth register.\n")
	if err := os.WriteFile(filepath.Join(legacyDir, "SKILL.md"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	targets := detectSkillHarnesses(home, func(string) string { return "" }, func(string) (string, error) {
		return "", errors.New("not installed")
	})
	findings, err := scanKnownLegacySkills(home, targets)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Harness != "codex,omp" || findings[0].ContentSHA256 != skills.Digest(content) {
		t.Fatalf("shared Codex/OMP legacy copy was duplicated or misattributed: %#v", findings)
	}
}

func TestSkillsSyncRefreshesCleanCopiesAndPreservesEdits(t *testing.T) {
	t.Parallel()
	home := skillTestDir(t)
	if err := os.Mkdir(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	current := participantFixture(t)
	old := current
	old.Version = "anx.participant.legacy"
	old.Content = "old managed bytes\n"
	old.SHA256 = skills.Digest([]byte(old.Content))
	dir := filepath.Join(home, ".claude", "skills", current.Name)
	if err := configureManagedSkill(dir, old); err != nil {
		t.Fatal(err)
	}
	a := isolatedSkillsApp(home)
	result, _, err := a.runSkillsSync(nil, config.Resolved{})
	if err != nil {
		t.Fatal(err)
	}
	states := asMap(result.Data)["skills"].([]managedSkillState)
	if len(states) != 1 || states[0].State != "current" || !states[0].Changed {
		t.Fatalf("clean copy was not refreshed: %#v", states)
	}
	markerBytes, err := os.ReadFile(filepath.Join(dir, managedSkillMarkerName))
	if err != nil {
		t.Fatal(err)
	}
	markerState := assertSkillState(t, dir, current, "current")
	if markerState.InstalledCLIVersion == "" || markerState.InstalledSourceRevision == "" || !bytes.Contains(markerBytes, []byte(`"cli_version"`)) || !bytes.Contains(markerBytes, []byte(`"source_revision"`)) {
		t.Fatalf("provenance missing from managed marker: %#v", markerState)
	}
	custom := []byte(current.Content + "\nMy local instructions\n")
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), custom, 0o644); err != nil {
		t.Fatal(err)
	}
	result, _, err = a.runSkillsSync(nil, config.Resolved{})
	if err != nil {
		t.Fatal(err)
	}
	states = asMap(result.Data)["skills"].([]managedSkillState)
	if len(states) != 1 || states[0].State != "drifted" || states[0].Changed {
		t.Fatalf("edited copy was not preserved: %#v", states)
	}
	actual, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil || !bytes.Equal(actual, custom) {
		t.Fatalf("edited content changed: %v", err)
	}
}

func TestSkillsAdoptRequiresDigestAndKeepsBackup(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".cursor", "skills", "anx-cli-agent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := []byte("---\nname: anx-cli-agent\n---\nUse config use <profile>, auth register, and ANX_AGENT.\n")
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), old, 0o644); err != nil {
		t.Fatal(err)
	}
	a := isolatedSkillsApp(home)
	a.now = func() time.Time { return time.Date(2026, 10, 5, 9, 8, 7, 0, time.UTC) }
	plan, _, err := a.runSkillsAdopt([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	planData := asMap(plan.Data)
	digest := anyString(planData["content_sha256"])
	if planData["requires_confirmation"] != true || digest != skills.Digest(old) {
		t.Fatalf("unexpected adoption plan: %#v", planData)
	}
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md.backup-20261005T090807.000000000Z")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("plan created a backup: %v", err)
	}
	if _, _, err := a.runSkillsAdopt([]string{dir, "--expected-digest", "sha256:" + strings.Repeat("0", 64)}); err == nil {
		t.Fatal("digest mismatch was accepted")
	}
	adopted, _, err := a.runSkillsAdopt([]string{dir, "--expected-digest", digest})
	if err != nil {
		t.Fatal(err)
	}
	adoptedData := asMap(adopted.Data)
	legacyDir := anyString(adoptedData["legacy_path"])
	dir = anyString(adoptedData["path"])
	canonical, err := resolveManagedSkillDirectory(filepath.Join(home, ".cursor", "skills", "anx-participant"))
	if err != nil {
		t.Fatal(err)
	}
	if dir != canonical || anyString(adoptedData["canonical_path"]) != canonical {
		t.Fatalf("legacy adoption did not migrate to the canonical skill path: %#v", adoptedData)
	}
	backup := anyString(adoptedData["backup_path"])
	backupBytes, err := os.ReadFile(backup)
	if err != nil || !bytes.Equal(backupBytes, old) {
		t.Fatalf("legacy backup was not preserved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(legacyDir, "SKILL.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy skill remains loadable after migration: %v", err)
	}
	assertSkillState(t, dir, participantFixture(t), "current")
	installed, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil || !bytes.Equal(installed, []byte(participantFixture(t).Content)) {
		t.Fatalf("managed skill was not installed: %v", err)
	}
	status, _, err := a.runSkillsStatus(nil, config.Resolved{})
	if err != nil {
		t.Fatal(err)
	}
	states := asMap(status.Data)["skills"].([]managedSkillState)
	if len(states) != 1 || states[0].Path != canonical || states[0].State != "current" {
		t.Fatalf("status omitted the migrated managed copy: %#v", states)
	}
	markerPath := filepath.Join(canonical, managedSkillMarkerName)
	markerBytes, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatal(err)
	}
	var marker managedSkillMarker
	if err := json.Unmarshal(markerBytes, &marker); err != nil {
		t.Fatal(err)
	}
	marker.CLIVersion = "older-cli"
	markerBytes, err = json.Marshal(marker)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(markerPath, markerBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	status, _, err = a.runSkillsStatus(nil, config.Resolved{})
	if err != nil {
		t.Fatal(err)
	}
	states = asMap(status.Data)["skills"].([]managedSkillState)
	if len(states) != 1 || states[0].State != "outdated" {
		t.Fatalf("status did not report stale migrated provenance: %#v", states)
	}
	refreshed, _, err := a.runSkillsSync(nil, config.Resolved{})
	if err != nil {
		t.Fatal(err)
	}
	states = asMap(refreshed.Data)["skills"].([]managedSkillState)
	if len(states) != 1 || states[0].State != "current" || !states[0].Changed {
		t.Fatalf("sync did not refresh migrated copy: %#v", states)
	}
}

func TestSkillsAdoptMigratesCodexLegacyRootToSharedCanonicalRoot(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	legacyDir := filepath.Join(home, ".codex", "skills", "anx")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := []byte("Use config use <profile> and auth register.\n")
	if err := os.WriteFile(filepath.Join(legacyDir, "SKILL.md"), legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	a := isolatedSkillsApp(home)
	plan, _, err := a.runSkillsAdopt([]string{legacyDir})
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := resolveManagedSkillDirectory(filepath.Join(home, ".agents", "skills", "anx-participant"))
	if err != nil {
		t.Fatal(err)
	}
	if got := anyString(asMap(plan.Data)["canonical_path"]); got != canonical {
		t.Fatalf("Codex legacy plan selected %q, want shared root %q", got, canonical)
	}
	digest := skills.Digest(legacy)
	if _, _, err := a.runSkillsAdopt([]string{legacyDir, "--expected-digest", digest}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(legacyDir, "SKILL.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Codex legacy skill remains loadable after migration: %v", err)
	}
	status, _, err := a.runSkillsStatus(nil, config.Resolved{})
	if err != nil {
		t.Fatal(err)
	}
	states := asMap(status.Data)["skills"].([]managedSkillState)
	if len(states) != 2 {
		t.Fatalf("shared Codex/OMP status omitted migrated skill: %#v", states)
	}
	for _, state := range states {
		if state.State != "current" || state.Path != canonical {
			t.Fatalf("migrated shared copy is not visible to %s status: %#v", state.Harness, state)
		}
	}
}

func TestSkillsSyncDefersAgentctlOwnedCopies(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".agents", "skills", "anx-participant")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("agentctl-owned user content\n")
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	marker := []byte(`{"managed_by":"agentctl"}`)
	if err := os.WriteFile(filepath.Join(dir, ".agentctl-skill.json"), marker, 0o644); err != nil {
		t.Fatal(err)
	}
	a := isolatedSkillsApp(home)
	result, _, err := a.runSkillsSync(nil, config.Resolved{})
	if err != nil {
		t.Fatal(err)
	}
	states := asMap(result.Data)["skills"].([]managedSkillState)
	if len(states) != 2 {
		t.Fatalf("shared target should report Codex and OMP: %#v", states)
	}
	for _, state := range states {
		if state.State != "deferred" {
			t.Fatalf("agentctl ownership not deferred: %#v", state)
		}
	}
	actual, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil || !bytes.Equal(actual, content) {
		t.Fatalf("ANX changed agentctl-owned bytes: %v", err)
	}
}

func TestAutomaticSkillsRefreshLaunchIsDetachedAndDaily(t *testing.T) {
	home := skillTestDir(t)
	a := isolatedSkillsApp(home)
	now := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }
	launched := make(chan struct{}, 3)
	workerRelease := make(chan struct{})
	workerDone := make(chan int, 2)
	a.startSkillsRefresh = func(_, configDir, workerHome string) error {
		launched <- struct{}{}
		go func() {
			<-workerRelease
			worker := isolatedSkillsApp(workerHome)
			worker.now = func() time.Time { return now }
			var stdout, stderr bytes.Buffer
			worker.Stdout = &stdout
			worker.Stderr = &stderr
			workerDone <- worker.Run([]string{"--config-dir", configDir, "skills", "sync", "--scheduled", "--home", workerHome})
		}()
		return nil
	}
	complete := make(chan struct{})
	go func() {
		a.maybeScheduleSkillsRefresh("cards create", &commandResult{Data: map[string]any{"created": true}}, "")
		close(complete)
	}()
	select {
	case <-complete:
	case <-time.After(time.Second):
		t.Fatal("mutating command waited for the refresh worker")
	}
	select {
	case <-launched:
	case <-time.After(time.Second):
		t.Fatal("daily refresh did not launch")
	}
	configDir := filepath.Join(home, ".config", "anx")
	if stored, exists, err := readSkillsSyncConfig(configDir); err != nil || exists && stored.LastAutoSyncAt != "" {
		t.Fatalf("parent recorded a refresh before the worker succeeded: %+v exists=%t err=%v", stored, exists, err)
	}
	a.maybeScheduleSkillsRefresh("cards create", &commandResult{Data: map[string]any{"created": true}}, "")
	select {
	case <-launched:
		t.Fatal("daily mutation launched a second refresh")
	default:
	}
	close(workerRelease)
	if code := <-workerDone; code != 0 {
		t.Fatalf("scheduled worker failed: %d", code)
	}
	stored, exists, err := readSkillsSyncConfig(configDir)
	if err != nil || !exists || stored.LastAutoSyncAt == "" {
		t.Fatalf("successful worker did not record its completion: %+v exists=%t err=%v", stored, exists, err)
	}
	a.maybeScheduleSkillsRefresh("cards create", &commandResult{Data: map[string]any{"created": true}}, "")
	select {
	case <-launched:
		t.Fatal("same-day mutation launched after successful refresh")
	default:
	}
	a.maybeScheduleSkillsRefresh("update", &commandResult{Data: map[string]any{"updated": true}}, "")
	select {
	case <-launched:
	case <-time.After(time.Second):
		t.Fatal("successful CLI upgrade did not trigger an immediate refresh")
	}
	if code := <-workerDone; code != 0 {
		t.Fatalf("upgrade-triggered worker failed: %d", code)
	}
}

func TestFailedScheduledSkillsRefreshDoesNotRecordCompletion(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	configDir := filepath.Join(home, ".config", "anx")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, skillsSyncConfigName), []byte("{invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := isolatedSkillsApp(home)
	var stdout, stderr bytes.Buffer
	a.Stdout = &stdout
	a.Stderr = &stderr
	if code := a.Run([]string{"--config-dir", configDir, "skills", "sync", "--scheduled", "--home", home}); code == 0 {
		t.Fatalf("invalid preferences unexpectedly allowed a scheduled refresh: stdout=%s", stdout.String())
	}
	content, err := os.ReadFile(filepath.Join(configDir, skillsSyncConfigName))
	if err != nil || string(content) != "{invalid\n" {
		t.Fatalf("failed refresh changed preferences or recorded completion: %q err=%v", content, err)
	}
}

func TestAutomaticSkillsRefreshOptOutAndSkillsCommandsDoNotRecurse(t *testing.T) {
	home := t.TempDir()
	a := isolatedSkillsApp(home)
	a.Getenv = func(key string) string {
		if key == "ANX_SKILLS_AUTO_SYNC" {
			return "0"
		}
		return ""
	}
	calls := 0
	a.startSkillsRefresh = func(_, _, _ string) error { calls++; return nil }
	a.maybeScheduleSkillsRefresh("cards create", &commandResult{Data: map[string]any{}}, "")
	a.maybeScheduleSkillsRefresh("skills sync", &commandResult{Data: map[string]any{}}, "")
	if calls != 0 {
		t.Fatalf("opt-out or recursion skip ignored: %d launches", calls)
	}
}
