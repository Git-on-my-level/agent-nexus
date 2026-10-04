package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"agent-nexus-cli/internal/buildinfo"
	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/skills"
)

const skillsSyncConfigName = "skills-sync.json"

// skillsSyncConfig is deliberately separate from workspace selection. It only
// records local delivery preferences and the last automatic refresh attempt.
type skillsSyncConfig struct {
	SchemaVersion  int    `json:"schema_version"`
	ManagedBy      string `json:"managed_by"`
	AutoSync       bool   `json:"auto_sync"`
	PMEnabled      bool   `json:"pm_enabled"`
	LastAutoSyncAt string `json:"last_auto_sync_at,omitempty"`
}

func defaultSkillsSyncConfig() skillsSyncConfig {
	return skillsSyncConfig{SchemaVersion: 1, ManagedBy: "anx", AutoSync: true}
}

type skillHarnessTarget struct {
	Harnesses []string `json:"harnesses"`
	Root      string   `json:"root"`
}

type legacySkillFinding struct {
	Harness       string `json:"harness"`
	Name          string `json:"name"`
	Path          string `json:"path"`
	ContentSHA256 string `json:"content_sha256"`
	AdoptCommand  string `json:"adopt_command"`
}

func (a *App) runSkillsSync(args []string, cfg config.Resolved) (*commandResult, string, error) {
	parsed, err := parseSkillsFlags("sync", args)
	if err != nil {
		return nil, "skills sync", errnorm.Usage("invalid_flags", err.Error())
	}
	if len(parsed.positionals) != 0 {
		return nil, "skills sync", errnorm.Usage("invalid_args", "unexpected positional arguments")
	}
	dryRun := parsed.boolValue("dry-run")
	pm := parsed.boolValue("pm")
	noPM := parsed.boolValue("no-pm")
	autoSync := parsed.boolValue("auto-sync")
	noAutoSync := parsed.boolValue("no-auto-sync")
	scheduled := parsed.boolValue("scheduled")
	homeFlag := parsed.stringValue("home")
	if pm.set && noPM.set || autoSync.set && noAutoSync.set {
		return nil, "skills sync", errnorm.Usage("invalid_request", "choose only one of --pm/--no-pm and --auto-sync/--no-auto-sync")
	}

	home, err := a.skillHome(homeFlag.value)
	if err != nil {
		return nil, "skills sync", err
	}
	configDir, err := resolveSkillsConfigDir(home, cfg.ConfigDir)
	if err != nil {
		return nil, "skills sync", errnorm.Wrap(errnorm.KindLocal, "skill_config_invalid", "resolve ANX skills configuration directory", err)
	}
	if scheduled.value {
		defer os.Remove(filepath.Join(configDir, ".skills-auto-sync.lock"))
	}
	preferences, _, err := readSkillsSyncConfig(configDir)
	if err != nil {
		return nil, "skills sync", errnorm.Wrap(errnorm.KindLocal, "skill_config_read_failed", "read ANX skills preferences", err)
	}
	if pm.set {
		preferences.PMEnabled = true
	}
	if noPM.set {
		preferences.PMEnabled = false
	}
	if autoSync.set {
		preferences.AutoSync = true
	}
	if noAutoSync.set {
		preferences.AutoSync = false
	}
	roles := []string{"participant"}
	if preferences.PMEnabled {
		roles = append(roles, "pm")
	}
	lookPath := a.skillsLookPath()
	targets := detectSkillHarnesses(home, a.Getenv, lookPath)
	states := make([]managedSkillState, 0, len(targets)*len(roles))
	for _, target := range targets {
		for _, role := range roles {
			skill, skillErr := skills.Get(role)
			if skillErr != nil {
				return nil, "skills sync", errnorm.Wrap(errnorm.KindInternal, "skill_bundle_invalid", "validate bundled skill", skillErr)
			}
			path, pathErr := resolveManagedSkillDirectory(filepath.Join(target.Root, skill.Name))
			if pathErr != nil {
				return nil, "skills sync", errnorm.Wrap(errnorm.KindLocal, "skill_read_failed", "resolve harness skill directory", pathErr)
			}
			state, inspectErr := inspectManagedSkill(path, skill)
			if inspectErr != nil {
				return nil, "skills sync", errnorm.Wrap(errnorm.KindLocal, "skill_read_failed", "inspect harness skill directory", inspectErr)
			}
			state.Delivery = "managed"
			if state.State == "conflict" && strings.Contains(state.Reason, "agentctl owns") {
				state.State = "deferred"
				state.Reason = "agentctl owns this copy; left untouched"
			}
			state.Harness = strings.Join(target.Harnesses, ",")
			state.Changed = false
			state.DryRun = dryRun.value
			if state.State == "unmanaged" && managedTargetContainsLegacy(path) {
				state.State = "legacy"
				state.Reason = "known legacy ANX skill; adoption requires digest confirmation"
			}
			if !dryRun.value && (state.State == "missing" || state.State == "outdated") {
				if err := configureManagedSkill(path, skill); err != nil {
					state.State = "conflict"
					state.Reason = err.Error()
					states = appendHarnessSkillStates(states, state, target.Harnesses)
					continue
				}
				verified, verifyErr := inspectManagedSkill(path, skill)
				if verifyErr != nil {
					return nil, "skills sync", errnorm.Wrap(errnorm.KindLocal, "skill_read_failed", "verify synchronized skill", verifyErr)
				}
				state = verified
				state.Harness = strings.Join(target.Harnesses, ",")
				state.Delivery = "managed"
				state.Changed = true
				state.DryRun = false
			} else if dryRun.value && (state.State == "missing" || state.State == "outdated") {
				state.Reason = "would install or refresh clean ANX-owned skill"
			}
			states = appendHarnessSkillStates(states, state, target.Harnesses)
		}
	}
	legacy, legacyErr := scanKnownLegacySkills(home, targets)
	if legacyErr != nil {
		return nil, "skills sync", errnorm.Wrap(errnorm.KindLocal, "skill_read_failed", "scan known legacy ANX skills", legacyErr)
	}
	if !dryRun.value && scheduled.value {
		preferences.LastAutoSyncAt = a.clockNow().UTC().Format(time.RFC3339Nano)
		if err := writeSkillsSyncConfig(configDir, preferences); err != nil {
			return nil, "skills sync", errnorm.Wrap(errnorm.KindLocal, "skill_config_write_failed", "record successful automatic ANX skills refresh", err)
		}
	} else if !dryRun.value && (pm.set || noPM.set || autoSync.set || noAutoSync.set) {
		if err := writeSkillsSyncConfig(configDir, preferences); err != nil {
			return nil, "skills sync", errnorm.Wrap(errnorm.KindLocal, "skill_config_write_failed", "save ANX skills preferences", err)
		}
	}
	data := skillsSyncResult(home, configDir, targets, states, legacy, preferences, dryRun.value)
	return &commandResult{Data: data, Text: anyString(data["text"])}, "skills sync", nil
}

func appendHarnessSkillStates(dst []managedSkillState, state managedSkillState, harnesses []string) []managedSkillState {
	for _, harness := range harnesses {
		copy := state
		copy.Harness = harness
		dst = append(dst, copy)
	}
	return dst
}

func (a *App) runSkillsStatus(args []string, cfg config.Resolved) (*commandResult, string, error) {
	parsed, err := parseSkillsFlags("status", args)
	if err != nil {
		return nil, "skills status", errnorm.Usage("invalid_flags", err.Error())
	}
	if len(parsed.positionals) != 0 {
		return nil, "skills status", errnorm.Usage("invalid_args", "unexpected positional arguments")
	}
	pathFlag := parsed.stringValue("path")
	roleFlag := parsed.stringValue("role")
	homeFlag := parsed.stringValue("home")
	if pathFlag.set != roleFlag.set {
		return nil, "skills status", errnorm.Usage("invalid_request", "--path and --role must be supplied together")
	}
	if pathFlag.set {
		role := strings.TrimSpace(roleFlag.value)
		if role != "participant" && role != "pm" {
			return nil, "skills status", errnorm.Usage("invalid_request", "unknown skill role; use participant or pm")
		}
		skill, err := skills.Get(role)
		if err != nil {
			return nil, "skills status", errnorm.Wrap(errnorm.KindInternal, "skill_bundle_invalid", "validate bundled skill", err)
		}
		requested, err := filepath.Abs(strings.TrimSpace(pathFlag.value))
		if err != nil {
			return nil, "skills status", errnorm.Usage("invalid_request", "invalid skill path")
		}
		path, err := resolveManagedSkillDirectory(requested)
		if err != nil {
			return nil, "skills status", errnorm.Wrap(errnorm.KindLocal, "skill_read_failed", "resolve skill directory", err)
		}
		state, err := inspectManagedSkill(path, skill)
		if err != nil {
			return nil, "skills status", errnorm.Wrap(errnorm.KindLocal, "skill_read_failed", "inspect skill directory", err)
		}
		state.DryRun = true
		data, _ := json.Marshal(state)
		var result map[string]any
		_ = json.Unmarshal(data, &result)
		result["requested_path"] = requested
		result["text"] = fmt.Sprintf("skill name=%s role=%s state=%s cli_version=%s source_revision=%s path=%q\nverification scope=local_file harness_configuration=unknown session_activation=unknown\n", state.SkillName, state.Role, state.State, state.CLIVersion, state.SourceRevision, state.Path)
		return &commandResult{Data: result, Text: anyString(result["text"])}, "skills status", nil
	}
	home, err := a.skillHome(homeFlag.value)
	if err != nil {
		return nil, "skills status", err
	}
	configDir, err := resolveSkillsConfigDir(home, cfg.ConfigDir)
	if err != nil {
		return nil, "skills status", errnorm.Wrap(errnorm.KindLocal, "skill_config_invalid", "resolve ANX skills configuration directory", err)
	}
	preferences, _, err := readSkillsSyncConfig(configDir)
	if err != nil {
		return nil, "skills status", errnorm.Wrap(errnorm.KindLocal, "skill_config_read_failed", "read ANX skills preferences", err)
	}
	lookPath := a.skillsLookPath()
	targets := detectSkillHarnesses(home, a.Getenv, lookPath)
	roles := []string{"participant"}
	if preferences.PMEnabled {
		roles = append(roles, "pm")
	}
	states, err := inspectDetectedSkillStates(targets, roles)
	if err != nil {
		return nil, "skills status", errnorm.Wrap(errnorm.KindLocal, "skill_read_failed", "inspect detected harness skill state", err)
	}
	legacy, err := scanKnownLegacySkills(home, targets)
	if err != nil {
		return nil, "skills status", errnorm.Wrap(errnorm.KindLocal, "skill_read_failed", "scan known legacy ANX skills", err)
	}
	data := skillsSyncResult(home, configDir, targets, states, legacy, preferences, true)
	return &commandResult{Data: data, Text: anyString(data["text"])}, "skills status", nil
}

func (a *App) runSkillsAdopt(args []string) (*commandResult, string, error) {
	positionals, flagArgs := splitSkillsAdoptArgs(args)
	parsed, err := parseSkillsFlags("adopt", flagArgs)
	if err != nil {
		return nil, "skills adopt", errnorm.Usage("invalid_flags", err.Error())
	}
	if len(positionals) != 1 {
		return nil, "skills adopt", errnorm.Usage("invalid_args", "usage: anx skills adopt <path> [--expected-digest sha256:<digest>] [--role participant|pm]")
	}
	expectedDigest := parsed.stringValue("expected-digest")
	roleFlag := parsed.stringValue("role")
	role := strings.TrimSpace(roleFlag.value)
	if role == "" {
		role = "participant"
	}
	skill, err := skills.Get(role)
	if err != nil {
		return nil, "skills adopt", errnorm.Usage("invalid_request", "unknown skill role; use participant or pm")
	}
	requested := strings.TrimSpace(positionals[0])
	abs, err := filepath.Abs(requested)
	if err != nil {
		return nil, "skills adopt", errnorm.Usage("invalid_request", "invalid legacy skill path")
	}
	target := abs
	if strings.EqualFold(filepath.Base(abs), "SKILL.md") {
		target = filepath.Dir(abs)
	}
	target, err = resolveManagedSkillDirectory(target)
	if err != nil {
		return nil, "skills adopt", errnorm.Wrap(errnorm.KindLocal, "skill_read_failed", "resolve legacy skill directory", err)
	}
	contentPath := filepath.Join(target, "SKILL.md")
	content, err := readManagedSkillFile(contentPath)
	if err != nil {
		return nil, "skills adopt", errnorm.Wrap(errnorm.KindLocal, "skill_read_failed", "read legacy skill", err)
	}
	digest := skills.Digest(content)
	if !isLegacyANXSkill(target, digest, content) {
		return nil, "skills adopt", errnorm.Local("conflict", "path does not contain a recognized legacy ANX skill")
	}
	home, err := a.skillHome("")
	if err != nil {
		return nil, "skills adopt", err
	}
	managedTarget, harnesses, err := canonicalAdoptionTarget(home, target, a, skill)
	if err != nil {
		return nil, "skills adopt", errnorm.Local("invalid_request", err.Error())
	}
	for _, marker := range []string{managedSkillMarkerName, ".agentctl-skill.json"} {
		if _, markerErr := os.Lstat(filepath.Join(target, marker)); markerErr == nil {
			return nil, "skills adopt", errnorm.Local("conflict", "skill directory already has an ownership marker; adoption refused")
		} else if !errors.Is(markerErr, os.ErrNotExist) {
			return nil, "skills adopt", errnorm.Wrap(errnorm.KindLocal, "skill_read_failed", "inspect skill ownership marker", markerErr)
		}
	}
	name := filepath.Base(target)
	if expectedDigest.value == "" {
		data := map[string]any{"state": "legacy", "path": target, "canonical_path": managedTarget, "harnesses": harnesses, "legacy_name": name, "content_sha256": digest, "requires_confirmation": true, "role": role}
		return &commandResult{Data: data, Text: fmt.Sprintf("Legacy ANX skill found at %q (sha256 %s); adoption will migrate it to %q. Review it, then rerun anx skills adopt with --expected-digest %s.", target, digest, managedTarget, digest)}, "skills adopt", nil
	}
	if !skillDigestPattern.MatchString(strings.TrimSpace(expectedDigest.value)) {
		return nil, "skills adopt", errnorm.Usage("invalid_request", "--expected-digest must be a sha256:<64 lowercase hex> digest")
	}
	if expectedDigest.value != digest {
		return nil, "skills adopt", errnorm.WithDetails(errnorm.Local("conflict", "legacy skill changed after review; inspect it again before adoption"), map[string]any{"expected_digest": expectedDigest.value, "observed_digest": digest, "path": target})
	}
	if err := skillPathWithoutSymlinks(target); err != nil {
		return nil, "skills adopt", errnorm.Local("conflict", err.Error())
	}
	lockPath := filepath.Join(target, ".anx-skill.lock")
	lock, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, "skills adopt", errnorm.Wrap(errnorm.KindLocal, "conflict", "skill adoption is locked; inspect before retrying", err)
	}
	_ = lock.Close()
	defer os.Remove(lockPath)
	latest, err := readManagedSkillFile(contentPath)
	if err != nil {
		return nil, "skills adopt", errnorm.Wrap(errnorm.KindLocal, "skill_read_failed", "recheck legacy skill before adoption", err)
	}
	if skills.Digest(latest) != digest || !bytes.Equal(content, latest) {
		return nil, "skills adopt", errnorm.Local("conflict", "legacy skill changed during adoption; inspect it again")
	}
	backup := contentPath + ".backup-" + a.clockNow().UTC().Format("20060102T150405.000000000Z")
	if _, err := os.Lstat(backup); err == nil {
		return nil, "skills adopt", errnorm.Local("conflict", "timestamped backup already exists; retry adoption")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, "skills adopt", errnorm.Wrap(errnorm.KindLocal, "skill_write_failed", "check legacy backup path", err)
	}
	if err := os.Rename(contentPath, backup); err != nil {
		return nil, "skills adopt", errnorm.Wrap(errnorm.KindLocal, "skill_write_failed", "move legacy skill to timestamped backup", err)
	}
	if managedTarget != target {
		if err := configureManagedSkill(managedTarget, skill); err != nil {
			if restoreErr := os.Rename(backup, contentPath); restoreErr != nil {
				err = fmt.Errorf("%v; restore legacy backup failed: %w", err, restoreErr)
			}
			return nil, "skills adopt", errnorm.Wrap(errnorm.KindLocal, "skill_write_failed", "install managed skill at canonical harness path", err)
		}
		state, err := inspectManagedSkill(managedTarget, skill)
		if err != nil {
			if restoreErr := os.Rename(backup, contentPath); restoreErr != nil {
				err = fmt.Errorf("%v; restore legacy backup failed: %w", err, restoreErr)
			}
			return nil, "skills adopt", errnorm.Wrap(errnorm.KindLocal, "skill_read_failed", "verify migrated skill", err)
		}
		state.Changed = true
		state.Delivery = "managed"
		return &commandResult{Data: map[string]any{"state": "adopted", "path": managedTarget, "legacy_path": target, "canonical_path": managedTarget, "harnesses": harnesses, "legacy_name": name, "content_sha256": digest, "backup_path": backup, "skill": state}}, "skills adopt", nil
	}
	rollback := func(cause error) error {
		_ = os.Remove(contentPath)
		_ = os.Remove(filepath.Join(target, managedSkillMarkerName))
		if restoreErr := os.Rename(backup, contentPath); restoreErr != nil {
			return fmt.Errorf("%v; restore legacy backup failed: %w", cause, restoreErr)
		}
		return cause
	}
	if err := writeManagedSkillFile(contentPath, []byte(skill.Content), true); err != nil {
		return nil, "skills adopt", errnorm.Wrap(errnorm.KindLocal, "skill_write_failed", "install managed skill", rollback(err))
	}
	marker := managedSkillMarker{SchemaVersion: 1, ManagedBy: "anx", Role: skill.Role, SkillName: skill.Name, SkillVersion: skill.Version, ContentSHA256: skill.SHA256, CLIVersion: buildinfo.Current, SourceRevision: currentSkillSourceRevision()}
	markerBytes, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return nil, "skills adopt", errnorm.Wrap(errnorm.KindInternal, "skill_marker_invalid", "encode ANX ownership marker", rollback(err))
	}
	markerBytes = append(markerBytes, '\n')
	if err := writeManagedSkillFile(filepath.Join(target, managedSkillMarkerName), markerBytes, true); err != nil {
		return nil, "skills adopt", errnorm.Wrap(errnorm.KindLocal, "skill_write_failed", "write ANX ownership marker", rollback(err))
	}
	state, err := inspectManagedSkill(target, skill)
	if err != nil {
		return nil, "skills adopt", errnorm.Wrap(errnorm.KindLocal, "skill_read_failed", "verify adopted skill", err)
	}
	state.Changed = true
	state.Delivery = "managed"
	return &commandResult{Data: map[string]any{"state": "adopted", "path": managedTarget, "legacy_path": target, "canonical_path": managedTarget, "harnesses": harnesses, "legacy_name": name, "content_sha256": digest, "backup_path": backup, "skill": state}}, "skills adopt", nil
}

func splitSkillsAdoptArgs(args []string) (positionals, flags []string) {
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if !strings.HasPrefix(arg, "-") {
			positionals = append(positionals, arg)
			continue
		}
		flags = append(flags, arg)
		if strings.Contains(arg, "=") {
			continue
		}
		if definition, ok := skillsFlagDefinitionFor("adopt", strings.TrimPrefix(arg, "--")); ok && definition.kind == preflightFlagString {
			if index+1 < len(args) {
				index++
				flags = append(flags, args[index])
			}
		}
	}
	return positionals, flags
}

func canonicalAdoptionTarget(home, legacyPath string, a *App, skill skills.Skill) (string, []string, error) {
	targets := detectSkillHarnesses(home, a.Getenv, a.skillsLookPath())
	legacyRoot := filepath.Dir(filepath.Clean(legacyPath))
	for _, target := range targets {
		if sameSkillsPath(legacyRoot, target.Root) {
			canonical, err := resolveManagedSkillDirectory(filepath.Join(target.Root, skill.Name))
			return canonical, target.Harnesses, err
		}
	}
	if sameSkillsPath(legacyRoot, filepath.Join(home, ".codex", "skills")) {
		for _, target := range targets {
			for _, harness := range target.Harnesses {
				if harness == "codex" {
					canonical, err := resolveManagedSkillDirectory(filepath.Join(target.Root, skill.Name))
					return canonical, target.Harnesses, err
				}
			}
		}
	}
	return "", nil, fmt.Errorf("legacy skill path must be a direct child of a supported harness skills directory")
}

func sameSkillsPath(left, right string) bool {
	canonical := func(path string) string {
		abs, err := filepath.Abs(path)
		if err != nil {
			return filepath.Clean(path)
		}
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			return filepath.Clean(resolved)
		}
		return filepath.Clean(abs)
	}
	return canonical(left) == canonical(right)
}

func inspectDetectedSkillStates(targets []skillHarnessTarget, roles []string) ([]managedSkillState, error) {
	states := []managedSkillState{}
	for _, target := range targets {
		for _, role := range roles {
			skill, err := skills.Get(role)
			if err != nil {
				return nil, err
			}
			path, err := resolveManagedSkillDirectory(filepath.Join(target.Root, skill.Name))
			if err != nil {
				return nil, err
			}
			state, err := inspectManagedSkill(path, skill)
			if err != nil {
				return nil, err
			}
			state.Delivery = "managed"
			if state.State == "conflict" && strings.Contains(state.Reason, "agentctl owns") {
				state.State = "deferred"
				state.Reason = "agentctl owns this copy; left untouched"
			}
			if state.State == "unmanaged" && managedTargetContainsLegacy(path) {
				state.State = "legacy"
				state.Reason = "known legacy ANX skill; adoption requires digest confirmation"
			}
			states = appendHarnessSkillStates(states, state, target.Harnesses)
		}
	}
	return states, nil
}

func skillsSyncResult(home, configDir string, targets []skillHarnessTarget, states []managedSkillState, legacy []legacySkillFinding, preferences skillsSyncConfig, dryRun bool) map[string]any {
	harnesses := []string{}
	for _, target := range targets {
		harnesses = append(harnesses, target.Harnesses...)
	}
	sort.Strings(harnesses)
	data := map[string]any{
		"schema_version":     1,
		"home":               home,
		"config_dir":         configDir,
		"detected_harnesses": harnesses,
		"skills":             states,
		"legacy_skills":      legacy,
		"pm_enabled":         preferences.PMEnabled,
		"auto_sync":          preferences.AutoSync,
		"dry_run":            dryRun,
		"cli_version":        buildinfo.Current,
		"source_revision":    currentSkillSourceRevision(),
	}
	lines := []string{fmt.Sprintf("skills detected_harnesses=%s pm_enabled=%t auto_sync=%t dry_run=%t", strings.Join(harnesses, ","), preferences.PMEnabled, preferences.AutoSync, dryRun)}
	for _, state := range states {
		line := fmt.Sprintf("skill harness=%s role=%s state=%s path=%q", state.Harness, state.Role, state.State, state.Path)
		if state.Changed {
			line += " changed=true"
		} else if dryRun && (state.State == "missing" || state.State == "outdated") {
			line += " would_change=true"
		}
		if state.Reason != "" {
			line += " reason=" + strconvQuote(state.Reason)
		}
		lines = append(lines, line)
	}
	for _, item := range legacy {
		lines = append(lines, fmt.Sprintf("legacy harness=%s name=%s path=%q digest=%s next=anx-skills-adopt", item.Harness, item.Name, item.Path, item.ContentSHA256))
	}
	if len(states) == 0 {
		lines = append(lines, "skill_state no_supported_harnesses_detected=true")
	}
	data["text"] = strings.Join(lines, "\n") + "\n"
	return data
}

func strconvQuote(value string) string {
	return fmt.Sprintf("%q", value)
}

func (a *App) skillHome(override string) (string, error) {
	if strings.TrimSpace(override) != "" {
		abs, err := filepath.Abs(strings.TrimSpace(override))
		if err != nil {
			return "", errnorm.Usage("invalid_request", "invalid home directory")
		}
		return filepath.Clean(abs), nil
	}
	if a != nil && a.UserHomeDir != nil {
		home, err := a.UserHomeDir()
		if err == nil && strings.TrimSpace(home) != "" {
			return filepath.Clean(home), nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "", errnorm.Wrap(errnorm.KindLocal, "home_unavailable", "could not resolve user home directory", err)
	}
	return filepath.Clean(home), nil
}

func (a *App) skillsLookPath() func(string) (string, error) {
	if a != nil && a.skillLookPath != nil {
		return a.skillLookPath
	}
	return exec.LookPath
}

func resolveSkillsConfigDir(home, configured string) (string, error) {
	configured = strings.TrimSpace(configured)
	path := configured
	if configured != "" {
		if !filepath.IsAbs(configured) || filepath.Clean(configured) != configured {
			return "", fmt.Errorf("ANX_CONFIG_DIR must be an absolute clean path")
		}
	} else if strings.TrimSpace(home) == "" {
		return "", fmt.Errorf("home directory is unavailable")
	} else {
		path = filepath.Join(home, ".config", "anx")
	}
	resolved, err := resolveManagedSkillDirectory(path)
	if err != nil {
		return "", err
	}
	if err := skillPathWithoutSymlinks(resolved); err != nil {
		return "", err
	}
	return resolved, nil
}

func readSkillsSyncConfig(dir string) (skillsSyncConfig, bool, error) {
	cfg := defaultSkillsSyncConfig()
	path := filepath.Join(dir, skillsSyncConfigName)
	data, err := readManagedSkillFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, false, nil
	}
	if err != nil {
		return cfg, false, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return skillsSyncConfig{}, true, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return skillsSyncConfig{}, true, fmt.Errorf("trailing data in %s", path)
	}
	if cfg.SchemaVersion != 1 || cfg.ManagedBy != "anx" {
		return skillsSyncConfig{}, true, fmt.Errorf("unsupported or foreign ANX skills preferences")
	}
	return cfg, true, nil
}

func writeSkillsSyncConfig(dir string, cfg skillsSyncConfig) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := skillPathWithoutSymlinks(dir); err != nil {
		return err
	}
	path := filepath.Join(dir, skillsSyncConfigName)
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("skills preferences must be a regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	file, err := os.CreateTemp(dir, ".anx-skills-config-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := skillPathWithoutSymlinks(path); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func detectSkillHarnesses(home string, getenv func(string) string, lookPath func(string) (string, error)) []skillHarnessTarget {
	if getenv == nil {
		getenv = os.Getenv
	}
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	hasPath := func(rel string) bool {
		_, err := os.Stat(filepath.Join(home, filepath.FromSlash(rel)))
		return err == nil
	}
	hasExecutable := func(names ...string) bool {
		for _, name := range names {
			if _, err := lookPath(name); err == nil {
				return true
			}
		}
		return false
	}
	targets := []skillHarnessTarget{}
	agentsRoot := hasPath(".agents") || hasPath(".codex") || hasExecutable("codex", "omp")
	if agentsRoot {
		harnesses := []string{}
		if hasPath(".agents") || hasExecutable("codex") {
			harnesses = append(harnesses, "codex")
		}
		if hasPath(".agents") || hasExecutable("omp") {
			harnesses = append(harnesses, "omp")
		}
		if len(harnesses) == 0 {
			harnesses = append(harnesses, "codex")
		}
		targets = append(targets, skillHarnessTarget{Harnesses: harnesses, Root: filepath.Join(home, ".agents", "skills")})
	}
	for _, spec := range []struct {
		name string
		dir  string
		bin  []string
	}{{"claude", ".claude", []string{"claude"}}, {"cursor", ".cursor", []string{"cursor-agent"}}, {"hermes", ".hermes", []string{"hermes"}}, {"gemini", ".gemini", []string{"gemini"}}, {"pi", ".pi", []string{"pi"}}, {"continue", ".continue", []string{"cn"}}} {
		if hasPath(spec.dir) || hasExecutable(spec.bin...) {
			root := filepath.Join(home, spec.dir, "skills")
			if spec.name == "pi" {
				root = filepath.Join(home, ".pi", "agent", "skills")
			}
			targets = append(targets, skillHarnessTarget{Harnesses: []string{spec.name}, Root: root})
		}
	}
	configHome := strings.TrimSpace(getenv("XDG_CONFIG_HOME"))
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	opencodeRoot := filepath.Join(configHome, "opencode", "skills")
	if _, err := os.Stat(filepath.Dir(opencodeRoot)); err == nil || hasExecutable("opencode") {
		targets = append(targets, skillHarnessTarget{Harnesses: []string{"opencode"}, Root: opencodeRoot})
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Harnesses[0] < targets[j].Harnesses[0] })
	return targets
}

func managedTargetContainsLegacy(path string) bool {
	content, err := readManagedSkillFile(filepath.Join(path, "SKILL.md"))
	if err != nil {
		return false
	}
	return isLegacyANXSkill(path, skills.Digest(content), content)
}

func isLegacyANXSkill(path, digest string, content []byte) bool {
	name := strings.ToLower(filepath.Base(filepath.Clean(path)))
	switch name {
	case "anx", "anx-cli-agent", "anx-cli-onboard", "anx-opinionated-onboarding":
		return true
	}
	if len(content) == 0 {
		return false
	}
	text := strings.ToLower(string(content))
	return strings.Contains(text, "config use") || strings.Contains(text, "auth register") || strings.Contains(string(content), "ANX_AGENT")
}

func scanKnownLegacySkills(home string, targets []skillHarnessTarget) ([]legacySkillFinding, error) {
	roots := []struct{ harness, root string }{
		{"codex", filepath.Join(home, ".codex", "skills")},
		{"codex", filepath.Join(home, ".agents", "skills")},
		{"cursor", filepath.Join(home, ".cursor", "skills")},
		{"claude", filepath.Join(home, ".claude", "skills")},
		{"hermes", filepath.Join(home, ".hermes", "skills")},
		{"gemini", filepath.Join(home, ".gemini", "skills")},
		{"pi", filepath.Join(home, ".pi", "agent", "skills")},
	}
	for _, target := range targets {
		for _, harness := range target.Harnesses {
			roots = append(roots, struct{ harness, root string }{harness, target.Root})
		}
	}
	legacyNames := map[string]bool{"anx": true, "anx-cli-agent": true, "anx-cli-onboard": true, "anx-opinionated-onboarding": true}
	seen := map[string]int{}
	findings := []legacySkillFinding{}
	for _, entry := range roots {
		entries, err := os.ReadDir(entry.root)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for index, dirEntry := range entries {
			if index >= 512 {
				break
			}
			if !dirEntry.IsDir() {
				continue
			}
			name := dirEntry.Name()
			path := filepath.Join(entry.root, name)
			if findingIndex, ok := seen[path]; ok {
				harnesses := strings.Split(findings[findingIndex].Harness, ",")
				found := false
				for _, harness := range harnesses {
					if harness == entry.harness {
						found = true
						break
					}
				}
				if !found {
					harnesses = append(harnesses, entry.harness)
					sort.Strings(harnesses)
					findings[findingIndex].Harness = strings.Join(harnesses, ",")
				}
				continue
			}
			managed := false
			for _, marker := range []string{managedSkillMarkerName, ".agentctl-skill.json"} {
				if _, markerErr := os.Lstat(filepath.Join(path, marker)); markerErr == nil {
					managed = true
					break
				} else if !errors.Is(markerErr, os.ErrNotExist) {
					managed = true
					break
				}
			}
			if managed {
				continue
			}
			content, err := readManagedSkillFile(filepath.Join(path, "SKILL.md"))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				if legacyNames[strings.ToLower(name)] {
					return nil, err
				}
				continue
			}
			digest := skills.Digest(content)
			if !legacyNames[strings.ToLower(name)] && !isLegacyANXSkill(path, digest, content) {
				continue
			}
			command := fmt.Sprintf("anx skills adopt %q --expected-digest %s", path, digest)
			seen[path] = len(findings)
			findings = append(findings, legacySkillFinding{Harness: entry.harness, Name: name, Path: path, ContentSHA256: digest, AdoptCommand: command})
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Harness != findings[j].Harness {
			return findings[i].Harness < findings[j].Harness
		}
		return findings[i].Path < findings[j].Path
	})
	return findings, nil
}

func automaticSkillsSyncEnabled(a *App, cfg skillsSyncConfig) bool {
	if a != nil && a.Getenv != nil {
		switch strings.ToLower(strings.TrimSpace(a.Getenv("ANX_SKILLS_AUTO_SYNC"))) {
		case "0", "false", "no", "off":
			return false
		case "1", "true", "yes", "on":
			return true
		}
	}
	return cfg.AutoSync
}

func (a *App) maybeScheduleSkillsRefresh(command string, result *commandResult, configDir string) {
	if strings.HasPrefix(command, "skills") || result == nil {
		return
	}
	updated := command == "update" && asBool(asMap(result.Data)["updated"])
	if commandSideEffectClass(command) == "read_only" && !updated {
		return
	}
	home, err := a.skillHome("")
	if err != nil {
		return
	}
	configDir, err = resolveSkillsConfigDir(home, configDir)
	if err != nil {
		return
	}
	preferences, _, err := readSkillsSyncConfig(configDir)
	if err != nil || !automaticSkillsSyncEnabled(a, preferences) {
		return
	}
	now := a.clockNow().UTC()
	if !updated && preferences.LastAutoSyncAt != "" {
		last, parseErr := time.Parse(time.RFC3339Nano, preferences.LastAutoSyncAt)
		if parseErr == nil && last.UTC().Format("2006-01-02") == now.Format("2006-01-02") {
			return
		}
	}
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return
	}
	lockPath := filepath.Join(configDir, ".skills-auto-sync.lock")
	lock, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if !errors.Is(err, os.ErrExist) {
			return
		}
		info, statErr := os.Stat(lockPath)
		if statErr != nil || time.Since(info.ModTime()) < 15*time.Minute {
			return
		}
		if os.Remove(lockPath) != nil {
			return
		}
		lock, err = os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return
		}
	}
	if err := lock.Close(); err != nil {
		_ = os.Remove(lockPath)
		return
	}
	preferences, _, err = readSkillsSyncConfig(configDir)
	if err != nil || !automaticSkillsSyncEnabled(a, preferences) {
		_ = os.Remove(lockPath)
		return
	}
	if !updated && preferences.LastAutoSyncAt != "" {
		last, parseErr := time.Parse(time.RFC3339Nano, preferences.LastAutoSyncAt)
		if parseErr == nil && last.UTC().Format("2006-01-02") == now.Format("2006-01-02") {
			_ = os.Remove(lockPath)
			return
		}
	}
	executable, err := os.Executable()
	if err == nil && a.startSkillsRefresh != nil {
		err = a.startSkillsRefresh(executable, configDir, home)
	} else if err == nil {
		err = fmt.Errorf("detached skills refresh is unavailable")
	}
	if err != nil {
		_ = os.Remove(lockPath)
	}
}

func isGoTestBinary() bool {
	return strings.HasSuffix(filepath.Base(os.Args[0]), ".test")
}

var startDetachedSkillsRefresh = func(executable, configDir, home string) error {
	args := []string{"--config-dir", configDir, "skills", "sync", "--scheduled", "--home", home}
	cmd := exec.Command(executable, args...)
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return err
	}
	defer devNull.Close()
	cmd.Stdin = devNull
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	attachDetachedSkillsProcess(cmd)
	return cmd.Start()
}
