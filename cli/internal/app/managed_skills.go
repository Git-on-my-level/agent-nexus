package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"

	"agent-nexus-cli/internal/buildinfo"
	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/filelock"
	"agent-nexus-cli/skills"
)

const managedSkillMarkerName = ".anx-skill.json"
const maxManagedSkillBytes = 1 << 20

var skillDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type managedSkillMarker struct {
	SchemaVersion  int    `json:"schema_version"`
	ManagedBy      string `json:"managed_by"`
	Role           string `json:"role"`
	SkillName      string `json:"skill_name"`
	SkillVersion   string `json:"skill_version"`
	ContentSHA256  string `json:"content_sha256"`
	CLIVersion     string `json:"cli_version"`
	SourceRevision string `json:"source_revision"`
}

type managedSkillState struct {
	SchemaVersion           int    `json:"schema_version"`
	Harness                 string `json:"harness,omitempty"`
	Path                    string `json:"path"`
	Role                    string `json:"role"`
	SkillName               string `json:"skill_name"`
	ExpectedVersion         string `json:"expected_version"`
	InstalledVersion        string `json:"installed_version,omitempty"`
	CLIVersion              string `json:"cli_version"`
	SourceRevision          string `json:"source_revision"`
	InstalledCLIVersion     string `json:"installed_cli_version,omitempty"`
	InstalledSourceRevision string `json:"installed_source_revision,omitempty"`
	ExpectedSHA256          string `json:"expected_sha256"`
	ObservedSHA256          string `json:"observed_sha256,omitempty"`
	State                   string `json:"state"`
	Reason                  string `json:"reason,omitempty"`
	Delivery                string `json:"delivery"`
	HarnessConfiguration    string `json:"harness_configuration"`
	SessionActivation       string `json:"session_activation"`
	Changed                 bool   `json:"changed"`
	DryRun                  bool   `json:"dry_run"`
}

func (a *App) runSkills(args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 || isHelpToken(args[0]) {
		return &commandResult{Text: skillsUsageText()}, "skills", nil
	}
	sub := args[0]
	name := "skills " + sub
	switch sub {
	case "sync":
		return a.runSkillsSync(args[1:], cfg)
	case "adopt":
		return a.runSkillsAdopt(args[1:])
	case "status":
		return a.runSkillsStatus(args[1:], cfg)
	}
	if sub != "configure" && sub != "verify" {
		return nil, "skills", skillsSubcommandSpec.unknownError(sub)
	}
	parsed, err := parseSkillsFlags(sub, args[1:])
	if err != nil {
		return nil, name, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(parsed.positionals) > 0 {
		return nil, name, errnorm.Usage("invalid_args", "unexpected positional arguments")
	}
	path := parsed.stringValue("path")
	role := parsed.stringValue("role")
	dryRun := parsed.boolValue("dry-run")
	if strings.TrimSpace(path.value) == "" || strings.TrimSpace(role.value) == "" {
		return nil, name, errnorm.Usage("invalid_request", "--path <skill-directory> and --role participant|pm are required")
	}
	roleValue := strings.TrimSpace(role.value)
	if roleValue != "participant" && roleValue != "pm" {
		return nil, name, errnorm.Usage("invalid_request", "unknown skill role; use participant or pm")
	}
	skill, err := skills.Get(roleValue)
	if err != nil {
		return nil, name, errnorm.Wrap(errnorm.KindInternal, "skill_bundle_invalid", "failed to validate bundled skill", err)
	}
	requestedPath, err := filepath.Abs(strings.TrimSpace(path.value))
	if err != nil {
		return nil, name, errnorm.Usage("invalid_request", "invalid skill path")
	}
	target, err := resolveManagedSkillDirectory(requestedPath)
	if err != nil {
		return nil, name, errnorm.Wrap(errnorm.KindLocal, "skill_read_failed", "failed to resolve skill parent directory", err)
	}
	state, err := inspectManagedSkill(target, skill)
	if err != nil {
		return nil, name, errnorm.Wrap(errnorm.KindLocal, "skill_read_failed", "failed to inspect skill destination", err)
	}
	state.DryRun = dryRun.value
	if sub == "configure" && !dryRun.value {
		if state.State != "missing" && state.State != "current" && state.State != "outdated" {
			return nil, name, managedSkillError(state)
		}
		if state.State != "current" {
			if err := configureManagedSkill(target, skill); err != nil {
				return nil, name, err
			}
			state, err = inspectManagedSkill(target, skill)
			if err != nil {
				return nil, name, errnorm.Wrap(errnorm.KindLocal, "skill_read_failed", "failed to verify configured skill", err)
			}
			state.Changed = true
		}
	}
	if sub == "verify" && state.State != "current" {
		return nil, name, managedSkillError(state)
	}
	data, _ := json.Marshal(state)
	var result map[string]any
	_ = json.Unmarshal(data, &result)
	result["requested_path"] = requestedPath
	result["text"] = fmt.Sprintf("skill name=%s role=%s state=%s expected_version=%s installed_version=%s cli_version=%s source_revision=%s path=%q\nverification scope=local_file harness_configuration=unknown session_activation=unknown\n", state.SkillName, state.Role, state.State, state.ExpectedVersion, state.InstalledVersion, state.CLIVersion, state.SourceRevision, state.Path)
	if sub == "configure" && dryRun.value {
		result["would_change"] = state.State == "missing" || state.State == "outdated"
	}
	return &commandResult{Data: result}, name, nil
}

func managedSkillError(state managedSkillState) error {
	code := "conflict"
	if state.State == "missing" {
		code = "skill_not_found"
	}
	if state.State == "outdated" {
		code = "skill_outdated"
	}
	return errnorm.WithDetails(errnorm.Local(code, "skill is "+state.State+"; existing content was preserved"), map[string]any{"skill": state})
}

func currentSkillSourceRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info == nil {
		return "unknown"
	}
	revision := ""
	modified := false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = strings.TrimSpace(setting.Value)
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if revision == "" {
		revision = strings.TrimSpace(buildinfo.SourceRevision)
	}
	if revision == "" || revision == "unknown" {
		return "unknown"
	}
	if modified {
		return "tree:" + revision
	}
	return revision
}

// No harness catalog, environment detection, network reads or history access is
// involved. A local file digest cannot prove configuration or session activation.
func inspectManagedSkill(target string, skill skills.Skill) (managedSkillState, error) {
	state := managedSkillState{SchemaVersion: 1, Path: target, Role: skill.Role, SkillName: skill.Name, ExpectedVersion: skill.Version, ExpectedSHA256: skill.SHA256, CLIVersion: buildinfo.Current, SourceRevision: currentSkillSourceRevision(), Delivery: "manual", HarnessConfiguration: "unknown", SessionActivation: "unknown"}
	if err := skillPathWithoutSymlinks(target); err != nil {
		state.State = "conflict"
		state.Reason = err.Error()
		return state, nil
	}
	if info, err := os.Lstat(target); err == nil && !info.IsDir() {
		state.State = "conflict"
		state.Reason = "destination is not a directory"
		return state, nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return state, err
	}
	if _, err := os.Lstat(filepath.Join(target, ".agentctl-skill.json")); err == nil {
		state.State = "conflict"
		state.Reason = "agentctl owns this directory; use agentctl skills status/update"
		return state, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return state, err
	}
	content, contentErr := readManagedSkillFile(filepath.Join(target, "SKILL.md"))
	if contentErr != nil && !errors.Is(contentErr, os.ErrNotExist) {
		return state, contentErr
	}
	markerBytes, markerErr := readManagedSkillFile(filepath.Join(target, managedSkillMarkerName))
	if markerErr != nil && !errors.Is(markerErr, os.ErrNotExist) {
		return state, markerErr
	}
	if contentErr == nil {
		state.ObservedSHA256 = skills.Digest(content)
	}
	if errors.Is(markerErr, os.ErrNotExist) {
		state.State = "missing"
		if contentErr == nil {
			state.State = "unmanaged"
			state.Reason = "existing SKILL.md has no ANX ownership marker; use a new directory or keep managing it manually"
		}
		return state, nil
	}
	var marker managedSkillMarker
	decoder := json.NewDecoder(bytes.NewReader(markerBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&marker); err != nil || decoder.Decode(&struct{}{}) != io.EOF || marker.SchemaVersion != 1 || marker.ManagedBy != "anx" || marker.Role != skill.Role || marker.SkillName != skill.Name || strings.TrimSpace(marker.SkillVersion) == "" || !skillDigestPattern.MatchString(marker.ContentSHA256) {
		state.State = "conflict"
		state.Reason = "invalid or incompatible ownership marker"
		return state, nil
	}
	state.InstalledVersion = marker.SkillVersion
	state.InstalledCLIVersion = marker.CLIVersion
	state.InstalledSourceRevision = marker.SourceRevision
	if contentErr != nil || marker.ContentSHA256 != state.ObservedSHA256 {
		state.State = "drifted"
		state.Reason = "managed skill was edited or removed; refresh refused"
		return state, nil
	}
	state.State = "current"
	if marker.SkillVersion != skill.Version || marker.ContentSHA256 != skill.SHA256 || marker.CLIVersion != state.CLIVersion || marker.SourceRevision != state.SourceRevision {
		state.State = "outdated"
	}
	return state, nil
}

func configureManagedSkill(target string, skill skills.Skill) error {
	// Only ANX writers share this advisory lock. Reinspect under it, and verify
	// again immediately before replacement. User-authored shared files are never
	// read or rewritten. Interrupted two-file updates fail closed as drift.
	if err := skillPathWithoutSymlinks(target); err != nil {
		return errnorm.Local("conflict", err.Error())
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return errnorm.Wrap(errnorm.KindLocal, "skill_write_failed", "create skill directory", err)
	}
	lockPath := filepath.Join(target, ".anx-skill.lock")
	lock, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errnorm.Wrap(errnorm.KindLocal, "conflict", "skill configuration is locked; inspect an interrupted configure before removing its lock", err)
	}
	defer os.Remove(lockPath)
	if err := lock.Close(); err != nil {
		return err
	}
	state, err := inspectManagedSkill(target, skill)
	if err != nil {
		return errnorm.Wrap(errnorm.KindLocal, "skill_read_failed", "inspect managed skill", err)
	}
	if state.State == "current" {
		return nil
	}
	if state.State != "missing" && state.State != "outdated" {
		return managedSkillError(state)
	}
	marker := managedSkillMarker{SchemaVersion: 1, ManagedBy: "anx", Role: skill.Role, SkillName: skill.Name, SkillVersion: skill.Version, ContentSHA256: skill.SHA256, CLIVersion: buildinfo.Current, SourceRevision: currentSkillSourceRevision()}
	markerBytes, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return err
	}
	markerBytes = append(markerBytes, '\n')
	// Detect user edits after preflight. A concurrent external editor is not a
	// participant in this lock protocol; do not run editors and refresh together.
	latest, err := inspectManagedSkill(target, skill)
	if err != nil {
		return err
	}
	if latest.State != state.State || latest.ObservedSHA256 != state.ObservedSHA256 || latest.InstalledVersion != state.InstalledVersion {
		return errnorm.Local("conflict", "skill changed during configuration; retry after reviewing it")
	}
	if err := writeManagedSkillFile(filepath.Join(target, "SKILL.md"), []byte(skill.Content), state.State == "missing"); err != nil {
		return errnorm.Wrap(errnorm.KindLocal, "skill_write_failed", "write managed skill", err)
	}
	if err := writeManagedSkillFile(filepath.Join(target, managedSkillMarkerName), markerBytes, state.State == "missing"); err != nil {
		return errnorm.Wrap(errnorm.KindLocal, "skill_write_failed", "write ownership marker; verify reports any interrupted update as unmanaged or drifted", err)
	}
	return nil
}

func readManagedSkillFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxManagedSkillBytes {
		return nil, fmt.Errorf("skill file must be a bounded regular non-symlink file")
	}
	file, err := filelock.OpenNoFollow(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, opened) {
		return nil, fmt.Errorf("skill file changed during inspection")
	}
	content, err := io.ReadAll(io.LimitReader(file, maxManagedSkillBytes+1))
	if err != nil {
		return nil, err
	}
	if len(content) > maxManagedSkillBytes {
		return nil, fmt.Errorf("skill file exceeds size limit")
	}
	return content, nil
}

// Resolve existing parent aliases once, including normal macOS /tmp and /var
// prefixes. Never resolve the destination leaf: it remains subject to the
// symlink/ownership checks. All later operations use the canonical path so
// retargeting the requested ancestor alias cannot redirect this operation.
// Missing parent suffixes are reconstructed without creating anything.
func resolveManagedSkillDirectory(target string) (string, error) {
	parent := filepath.Dir(target)
	suffix := []string{filepath.Base(target)}
	for {
		_, err := os.Lstat(parent)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		next := filepath.Dir(parent)
		if next == parent {
			return "", err
		}
		suffix = append([]string{filepath.Base(parent)}, suffix...)
		parent = next
	}
	canonical, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("skill parent must be a directory")
	}
	return filepath.Join(append([]string{canonical}, suffix...)...), nil
}

func skillPathWithoutSymlinks(path string) error {
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("skill path must not contain symlinks")
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return nil
}

func writeManagedSkillFile(path string, content []byte, exclusive bool) error {
	if err := skillPathWithoutSymlinks(path); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".anx-skill-tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(content); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if exclusive {
		return os.Link(file.Name(), path)
	}
	return os.Rename(file.Name(), path)
}

var skillsSubcommandSpec = subcommandSpec{command: "skills", valid: []string{"configure", "status", "verify", "sync", "adopt"}, examples: []string{"anx skills sync --dry-run", "anx skills configure --path ./anx-participant --role participant --dry-run", "anx skills adopt ~/.codex/skills/anx"}}

func skillsUsageText() string {
	return strings.TrimSpace(`Install and maintain ANX skills across detected harnesses, or inspect one explicit copy.

Usage:
  anx skills sync [--dry-run] [--pm|--no-pm] [--auto-sync|--no-auto-sync] [--home <dir>]
  anx skills status [--home <dir>]
  anx skills adopt <path> [--expected-digest sha256:<digest>] [--role participant|pm]
  anx skills configure --path <skill-directory> --role participant|pm [--dry-run]
  anx skills status --path <skill-directory> --role participant|pm
  anx skills verify --path <skill-directory> --role participant|pm

Sync detects installed harnesses and installs or refreshes only clean ANX-owned
copies. It preserves unmanaged or edited files. --pm opts into the additional PM
skill and remembers that choice. Automatic refresh is enabled by default and can
be disabled with --no-auto-sync or ANX_SKILLS_AUTO_SYNC=0. --dry-run performs no
writes. Agentctl-owned copies are reported and left for agentctl to manage.

Legacy skills are reported with their digest. Adoption is read-only until the
printed digest is passed back with --expected-digest; adoption migrates the
skill to the canonical harness path and keeps the old SKILL.md as a timestamped
backup, leaving the legacy directory without a loadable skill. Status reports state per harness. Explicit
configure/verify remain available for arbitrary providers and paths. Harness
activation remains unknown even when a managed copy is current.

Managed copies record CLI version, source revision, skill version and content
digest. Custom files, unrelated instructions and credentials are preserved.`)
}

func init() {
	for _, sub := range []string{"sync", "adopt", "configure", "status", "verify"} {
		var examples []string
		switch sub {
		case "sync":
			examples = []string{"anx skills sync --dry-run", "anx skills sync --pm"}
		case "adopt":
			examples = []string{"anx skills adopt ~/.codex/skills/anx"}
		case "configure":
			examples = []string{"anx skills configure --path ./anx-participant --role participant --dry-run"}
		case "status":
			examples = []string{"anx skills status", "anx skills status --path ./anx-participant --role participant"}
		case "verify":
			examples = []string{"anx skills verify --path ./anx-participant --role participant"}
		}
		localHelperTopics = append(localHelperTopics, localHelperTopic{Path: "skills " + sub, Summary: "Inspect or maintain versioned local ANX skill files.", JSONShape: "`schema_version`, `state`, `cli_version`, `source_revision`, `expected_sha256`, `observed_sha256`, `harness_configuration`, `session_activation`", Composition: "Local managed files preserve edited and agentctl-owned copies; session activation remains unknown.", Flags: skillsLocalHelperFlags(sub), Examples: examples})
	}
}
