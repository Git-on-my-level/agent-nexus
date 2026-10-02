package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/skills"
)

func skillTestDir(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return path
}
func participantFixture(t *testing.T) skills.Skill {
	t.Helper()
	s, err := skills.Get("participant")
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func assertSkillState(t *testing.T, path string, skill skills.Skill, want string) managedSkillState {
	t.Helper()
	state, err := inspectManagedSkill(path, skill)
	if err != nil {
		t.Fatal(err)
	}
	if state.State != want {
		t.Fatalf("state=%+v want=%s", state, want)
	}
	return state
}
func skillCommand(t *testing.T, args ...string) (int, map[string]any) {
	t.Helper()
	a := New()
	out := new(bytes.Buffer)
	a.Stdout = out
	a.Stderr = new(bytes.Buffer)
	a.Stdin = strings.NewReader("")
	a.Getenv = func(string) string { return "" }
	a.UserHomeDir = func() (string, error) { return "", errors.New("no home") }
	a.runtimeIdentity = func() (*runtimeIdentityReport, error) {
		t.Fatal("skill command discovered runtime identity")
		return nil, nil
	}
	a.hasOMPAncestor = func() bool { t.Fatal("skill command inspected processes"); return false }
	a.ReadFile = func(path string) ([]byte, error) {
		t.Fatalf("unexpected config/identity read: %s", path)
		return nil, nil
	}
	code := a.Run(append([]string{"--json"}, args...))
	var payload map[string]any
	decoder := json.NewDecoder(out)
	if err := decoder.Decode(&payload); err != nil {
		t.Fatalf("bad JSON: %v; %s", err, out.String())
	}
	if out.Len() != 1 && strings.TrimSpace(out.String()) != "" {
		t.Fatalf("more than one output envelope: %s", out.String())
	}
	return code, payload
}

func TestManagedSkillsConfigureVerifyIdempotent(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(skillTestDir(t), "custom-provider", "anx-participant")
	skill := participantFixture(t)
	code, payload := skillCommand(t, "skills", "configure", "--path", dir, "--role", "participant")
	if code != 0 {
		t.Fatalf("code=%d payload=%v", code, payload)
	}
	state := assertSkillState(t, dir, skill, "current")
	if state.HarnessConfiguration != "unknown" || state.SessionActivation != "unknown" || state.Delivery != "manual" {
		t.Fatalf("false activation: %+v", state)
	}
	before, err := os.Stat(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	code, payload = skillCommand(t, "skills", "configure", "--path", dir, "--role", "participant")
	if code != 0 || payload["result"].(map[string]any)["changed"] != false {
		t.Fatalf("idempotent configure=%v", payload)
	}
	after, _ := os.Stat(filepath.Join(dir, "SKILL.md"))
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("current skill rewritten")
	}
	code, payload = skillCommand(t, "skills", "verify", "--path", dir, "--role", "participant")
	if code != 0 {
		t.Fatalf("verify: %v", payload)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatalf("unexpected files: %v", entries)
	}
}

func TestManagedSkillsDryRunAndStatusDoNotCreate(t *testing.T) {
	t.Parallel()
	parent := skillTestDir(t)
	dir := filepath.Join(parent, "unseen", "skill")
	for _, sub := range []string{"configure", "status", "verify"} {
		args := []string{"skills", sub, "--path", dir, "--role", "participant"}
		if sub == "configure" {
			args = append(args, "--dry-run")
		}
		code, payload := skillCommand(t, args...)
		want := 0
		if sub == "verify" {
			want = 3
		}
		if code != want {
			t.Fatalf("%s code=%d payload=%v", sub, code, payload)
		}
		if _, err := os.Lstat(filepath.Dir(dir)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s wrote parent: %v", sub, err)
		}
	}
}

func TestManagedSkillsCleanUpgradePreservesUserInstructions(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(skillTestDir(t), "skill")
	current := participantFixture(t)
	old := current
	old.Version = "anx.participant.v0"
	old.Content = "old bundled bytes\n"
	old.SHA256 = skills.Digest([]byte(old.Content))
	if err := configureManagedSkill(dir, old); err != nil {
		t.Fatal(err)
	}
	instructions := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(instructions, []byte("Keep my personal rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	assertSkillState(t, dir, current, "outdated")
	code, payload := skillCommand(t, "skills", "verify", "--path", dir, "--role", "participant")
	if code != 7 {
		t.Fatalf("outdated exit: %d %v", code, payload)
	}
	if err := configureManagedSkill(dir, current); err != nil {
		t.Fatal(err)
	}
	assertSkillState(t, dir, current, "current")
	data, _ := os.ReadFile(instructions)
	if string(data) != "Keep my personal rules\n" {
		t.Fatal("shared instructions changed")
	}
}

func TestManagedSkillsPreserveEditsAndLegacyMigration(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"edited", "removed", "legacy", "agentctl", "wrong-role", "bad-marker", "future-marker", "trailing-marker"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			dir := filepath.Join(skillTestDir(t), "skill")
			skill := participantFixture(t)
			if err := configureManagedSkill(dir, skill); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "SKILL.md")
			marker := filepath.Join(dir, managedSkillMarkerName)
			want := "conflict"
			switch kind {
			case "edited":
				os.WriteFile(path, []byte(skill.Content+"\nMy extra instructions\n"), 0o644)
				want = "drifted"
			case "removed":
				os.Remove(path)
				want = "drifted"
			case "legacy":
				os.Remove(marker)
				os.WriteFile(path, []byte("---\nname: anx-opinionated-onboarding\n---\nMy customized legacy instructions\n"), 0o644)
				want = "unmanaged"
			case "agentctl":
				os.WriteFile(filepath.Join(dir, ".agentctl-skill.json"), []byte("{}"), 0o644)
			case "wrong-role":
				var m managedSkillMarker
				raw, _ := os.ReadFile(marker)
				json.Unmarshal(raw, &m)
				m.Role = "pm"
				raw, _ = json.Marshal(m)
				os.WriteFile(marker, raw, 0o644)
			case "bad-marker":
				os.WriteFile(marker, []byte("{}"), 0o644)
			case "future-marker":
				raw, _ := os.ReadFile(marker)
				os.WriteFile(marker, bytes.Replace(raw, []byte(`"schema_version": 1`), []byte(`"schema_version": 2`), 1), 0o644)
			case "trailing-marker":
				raw, _ := os.ReadFile(marker)
				os.WriteFile(marker, append(raw, []byte("{}")...), 0o644)
			}
			before, _ := os.ReadFile(path)
			beforeMarker, _ := os.ReadFile(marker)
			assertSkillState(t, dir, skill, want)
			code, payload := skillCommand(t, "skills", "configure", "--path", dir, "--role", "participant")
			if code != 4 {
				t.Fatalf("configure exit=%d payload=%v", code, payload)
			}
			after, _ := os.ReadFile(path)
			afterMarker, _ := os.ReadFile(marker)
			if !bytes.Equal(before, after) || !bytes.Equal(beforeMarker, afterMarker) {
				t.Fatal("conflicting content overwritten")
			}
			code, payload = skillCommand(t, "skills", "verify", "--path", dir, "--role", "participant")
			if code != 4 {
				t.Fatalf("verify exit=%d payload=%v", code, payload)
			}
		})
	}
}

func TestManagedSkillsUnsafePathsAndLock(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"directory-symlink", "file-symlink", "marker-symlink", "oversized", "file-destination", "lock"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			root := skillTestDir(t)
			dir := filepath.Join(root, "skill")
			outside := filepath.Join(root, "outside")
			os.MkdirAll(dir, 0o755)
			os.WriteFile(outside, []byte("private unrelated bytes"), 0o644)
			skill := participantFixture(t)
			switch kind {
			case "directory-symlink":
				os.Remove(dir)
				if err := os.Symlink(root, dir); err != nil {
					t.Skip(err)
				}
			case "file-symlink":
				if err := os.Symlink(outside, filepath.Join(dir, "SKILL.md")); err != nil {
					t.Skip(err)
				}
			case "marker-symlink":
				if err := os.Symlink(outside, filepath.Join(dir, managedSkillMarkerName)); err != nil {
					t.Skip(err)
				}
			case "oversized":
				os.WriteFile(filepath.Join(dir, "SKILL.md"), bytes.Repeat([]byte("x"), maxManagedSkillBytes+1), 0o644)
			case "file-destination":
				os.Remove(dir)
				os.WriteFile(dir, []byte("custom"), 0o644)
			case "lock":
				os.WriteFile(filepath.Join(dir, ".anx-skill.lock"), []byte(""), 0o600)
			}
			if err := configureManagedSkill(dir, skill); err == nil {
				t.Fatal("unsafe destination accepted")
			}
			raw, _ := os.ReadFile(outside)
			if string(raw) != "private unrelated bytes" {
				t.Fatal("symlink target modified")
			}
		})
	}
}

func TestManagedSkillsOfflineUsageAndClassifications(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(skillTestDir(t), "manual")
	for _, args := range [][]string{
		{"skills", "not-real"}, {"skills", "configure"}, {"skills", "status", "--path", dir, "--role", "unknown"},
		{"skills", "configure", "--path", dir, "--role", "participant", "--force"},
		{"skills", "configure", "--path", dir, "--role", "participant", "--harness", "arbitrary-provider"},
		{"skills", "status", "--path", dir, "--role", "participant", "--dry-run"},
	} {
		code, payload := skillCommand(t, args...)
		if code != 2 {
			t.Fatalf("args=%v code=%d payload=%v", args, code, payload)
		}
	}
	for command, want := range map[string]string{"skills configure": "local_operational_write", "skills configure --path ./x --role participant": "local_operational_write", "skills configure --dry-run": "local_operational_write", "skills configure --dry-run=false": "local_operational_write", "skills status": "read_only", "skills verify": "read_only"} {
		if got := commandSideEffectClass(command); got != want {
			t.Fatalf("%s=%s want %s", command, got, want)
		}
	}
	if errnorm.ExitCode(errnorm.Local("skill_outdated", "")) != 7 {
		t.Fatal("wrong outdated exit")
	}
}

func TestManagedSkillsPMAndExports(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(skillTestDir(t), "pm")
	code, payload := skillCommand(t, "skills", "configure", "--path", dir, "--role", "pm")
	if code != 0 {
		t.Fatalf("pm configure=%v", payload)
	}
	pm, err := skills.Get("pm")
	if err != nil {
		t.Fatal(err)
	}
	assertSkillState(t, dir, pm, "current")
	code, payload = skillCommand(t, "debug", "meta", "skill", "pm")
	if code != 0 {
		t.Fatalf("pm export=%v", payload)
	}
	result := payload["result"].(map[string]any)
	if result["content"] != pm.Content || result["content_sha256"] != pm.SHA256 || result["skill_version"] != pm.Version {
		t.Fatalf("export mismatch: %v", result)
	}
	participant := participantFixture(t)
	if renderOpinionatedANXSkillMarkdown() != participant.Content || agentGuideSkillName != participant.Name || agentGuideSkillVersion != participant.Version {
		t.Fatal("legacy export and catalog differ")
	}
}

func TestManagedSkillsDeniedFileReadDoesNotBecomeMissing(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission denial requires an unprivileged process")
	}
	dir := filepath.Join(skillTestDir(t), "skill")
	skill := participantFixture(t)
	if err := configureManagedSkill(dir, skill); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "SKILL.md")
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0o600)
	if _, err := inspectManagedSkill(dir, skill); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("denied read must fail, got %v", err)
	}
	code, payload := skillCommand(t, "skills", "configure", "--path", dir, "--role", "participant")
	if code != 1 {
		t.Fatalf("denied configure exit=%d payload=%v", code, payload)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0 {
		t.Fatalf("denied file altered: %v %v", info, err)
	}
}

func TestManagedSkillsRepeatedDryRunAndConservativeClassification(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		flags  []string
		writes bool
	}{
		{[]string{"--dry-run", "--dry-run=false"}, true},
		{[]string{"--dry-run=false", "--dry-run"}, false},
	} {
		dir := filepath.Join(skillTestDir(t), "custom --dry-run provider", "skill")
		args := append([]string{"skills", "configure", "--path", dir, "--role", "participant"}, test.flags...)
		code, payload := skillCommand(t, args...)
		if code != 0 {
			t.Fatalf("%v: %v", args, payload)
		}
		_, err := os.Stat(filepath.Join(dir, "SKILL.md"))
		if test.writes != (err == nil) {
			t.Fatalf("writes=%v stat=%v", test.writes, err)
		}
		if got := commandSideEffectClass(strings.Join(args, " ")); got != "local_operational_write" {
			t.Fatalf("unsafe classification: %s", got)
		}
	}
}
