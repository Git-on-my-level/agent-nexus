package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/httpclient"
	"agent-nexus-cli/internal/output"
)

// A digest-bound receipt is written only by the release installer/updater. A
// filename or version alone is insufficient evidence of installer ownership.
type updateInstallRecord struct {
	ManagedBy   string `json:"managed_by"`
	Version     string `json:"version"`
	SHA256      string `json:"sha256"`
	InstalledAt string `json:"installed_at"`
}
type updateState struct {
	CheckedOn     string `json:"checked_on,omitempty"`
	LatestVersion string `json:"latest_version,omitempty"`
	FailureStage  string `json:"failure_stage,omitempty"`
	FailureCode   string `json:"failure_code,omitempty"`
	Rollback      string `json:"rollback"`
	BackupPath    string `json:"backup_path,omitempty"`
	SkillsSynced  bool   `json:"skills_synced"`
}
type updateOptions struct {
	verb, policy, version string
	check, scheduled      bool
}

func parseUpdateOptions(args []string) (updateOptions, error) {
	o := updateOptions{verb: "now"}
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		o.verb = args[0]
		args = args[1:]
		if o.verb == "policy" {
			if len(args) != 1 || !validUpdatePolicy(args[0]) {
				return o, errnorm.Usage("invalid_update_policy", "usage: anx update policy auto|notify|off")
			}
			o.policy = args[0]
			return o, nil
		}
		if o.verb != "now" && o.verb != "status" {
			return o, errnorm.Usage("unknown_subcommand", "usage: anx update status|now|policy auto|notify|off")
		}
	}
	fs := newSilentFlagSet("update")
	fs.BoolVar(&o.check, "check", false, "Check release without installing")
	fs.BoolVar(&o.scheduled, "scheduled", false, "Internal daily worker")
	fs.StringVar(&o.version, "version", "", "Release tag")
	if err := fs.Parse(args); err != nil {
		return o, errnorm.Usage("invalid_update_flags", err.Error())
	}
	if len(fs.Args()) != 0 || o.verb == "status" && len(args) != 0 || o.scheduled && (o.check || o.version != "") {
		return o, errnorm.Usage("invalid_update_args", "invalid update arguments")
	}
	if o.version != "" {
		if _, err := parseSemanticVersion(o.version); err != nil || strings.ContainsAny(o.version, "/\\?#") {
			return o, errnorm.Usage("invalid_update_version", "expected a release tag such as v0.12.11")
		}
	}
	return o, nil
}
func validUpdatePolicy(p string) bool      { return p == "auto" || p == "notify" || p == "off" }
func installRecordPath(path string) string { return path + ".anx-install.json" }
func binaryDigest(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
func inspectUpdateInstall(verifyDigest ...bool) (string, updateInstallRecord, string, string) {
	path, err := updateExecutablePath()
	if err != nil {
		return "", updateInstallRecord{}, "", "executable_unavailable"
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return path, updateInstallRecord{}, "", "executable_unavailable"
	}
	lower := strings.ToLower(filepath.ToSlash(path))
	for _, part := range []string{"/cellar/", "/homebrew/", "/nix/store/", "/scoop/", "/chocolatey/", "/go/bin/"} {
		if strings.Contains(lower, part) {
			return path, updateInstallRecord{}, "", "package_manager_install"
		}
	}
	// The foreground scheduler only checks receipt shape/path eligibility.
	// Hashing and full ownership verification run in status or the worker.
	verify := len(verifyDigest) == 0 || verifyDigest[0]
	digest := ""
	if verify {
		digest, err = binaryDigest(path)
		if err != nil {
			return path, updateInstallRecord{}, "", "executable_unreadable"
		}
	}
	var record updateInstallRecord
	receiptInfo, receiptErr := os.Lstat(installRecordPath(path))
	if receiptErr == nil && (!receiptInfo.Mode().IsRegular() || receiptInfo.Mode()&os.ModeSymlink != 0) {
		return path, record, digest, "invalid_installer_receipt"
	}
	if err := readUpdateJSON(installRecordPath(path), &record); err != nil || record.ManagedBy != "anx" || record.SHA256 == "" {
		return path, record, digest, "no_installer_receipt"
	}
	if verify && record.SHA256 != digest {
		return path, record, digest, "binary_differs_from_installer_receipt"
	}
	version := httpclient.CLIVersion
	if _, err := parseSemanticVersion(version); err != nil || strings.Contains(version, "dev") {
		return path, record, digest, "development_build"
	}
	if updateGOOS == "windows" {
		return path, record, digest, "windows_replacement_unsupported"
	}
	return path, record, digest, ""
}
func readUpdateJSON(path string, value any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, value)
}
func writeUpdateJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".anx-record-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(append(b, '\n')); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
func (a *App) updateDirectory(cfg config.Resolved) (string, error) {
	home := ""
	if cfg.ConfigDir == "" {
		var err error
		home, err = a.UserHomeDir()
		if err != nil {
			return "", err
		}
	}
	dir, err := resolveSkillsConfigDir(home, cfg.ConfigDir)
	return filepath.Join(dir, "update"), err
}
func (a *App) updatePolicy(dir string) (string, string, error) {
	var saved struct {
		Policy string `json:"policy"`
	}
	err := readUpdateJSON(filepath.Join(dir, "policy.json"), &saved)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", "", err
	}
	if saved.Policy == "" {
		saved.Policy = "auto"
	}
	source := "default"
	if err == nil {
		source = "saved"
	}
	if override := strings.TrimSpace(a.Getenv("ANX_UPDATE_POLICY")); override != "" {
		saved.Policy = override
		source = "environment"
	}
	if !validUpdatePolicy(saved.Policy) {
		return "", source, errnorm.Usage("invalid_update_policy", "ANX_UPDATE_POLICY must be auto, notify, or off")
	}
	return saved.Policy, source, nil
}
func readUpdateState(dir string) (updateState, error) {
	s := updateState{Rollback: "not_needed"}
	err := readUpdateJSON(filepath.Join(dir, "state.json"), &s)
	if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	return s, err
}
func (a *App) runUpdateStatus(cfg config.Resolved) (*commandResult, error) {
	dir, err := a.updateDirectory(cfg)
	if err != nil {
		return nil, err
	}
	policy, source, err := a.updatePolicy(dir)
	if err != nil {
		return nil, err
	}
	state, err := readUpdateState(dir)
	if err != nil {
		return nil, err
	}
	path, record, digest, reason := inspectUpdateInstall()
	return &commandResult{Data: map[string]any{
		"policy": policy, "policy_source": source, "managed": reason == "", "skip_reason": reason, "install_path": path,
		"observed_binary":  map[string]any{"version": httpclient.CLIVersion, "sha256": digest, "bookkeeping_matches": record.SHA256 == digest && digest != "" && normalizeReleaseTag(record.Version) == normalizeReleaseTag(httpclient.CLIVersion)},
		"installer_record": record, "state": state, "state_path": filepath.Join(dir, "state.json"),
	}}, nil
}

// This is deliberately bounded to classified work writes. Local maintenance,
// reads, streaming waits, dry runs and help cannot trigger binary maintenance.
func updateInvocationEligible(command string, args []string) bool {
	if commandSideEffectClass(command) != "remote_coordination_write" {
		return false
	}
	for _, arg := range args {
		if arg == "--dry-run" || arg == "--dry-run=true" || arg == "--plan" || arg == "--plan=true" {
			return false
		}
	}
	return true
}
func (a *App) maybeScheduleUpdate(command string, args []string, cfg config.Resolved) []output.Warning {
	if !updateInvocationEligible(command, args) {
		return nil
	}
	dir, err := a.updateDirectory(cfg)
	if err != nil {
		return nil
	}
	policy, _, err := a.updatePolicy(dir)
	if err != nil || policy == "off" {
		return nil
	}
	path, _, _, reason := inspectUpdateInstall(false)
	if reason != "" {
		return nil
	}
	day := a.clockNow().UTC().Format("2006-01-02")
	// A separate notification claim lets a worker's observation become visible
	// on a later foreground invocation on the same day, without extra checks.
	claims := path + ".anx-update-days"
	if os.MkdirAll(claims, 0700) != nil {
		return nil
	}
	state, _ := readUpdateState(dir)
	var warnings []output.Warning
	if policy == "notify" && state.LatestVersion != "" {
		if cmp, err := compareSemanticVersions(httpclient.CLIVersion, state.LatestVersion); err == nil && cmp < 0 {
			notice, err := os.OpenFile(filepath.Join(claims, day+".notice"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err == nil {
				_ = notice.Close()
				warnings = append(warnings, output.Warning{Code: "cli_update_available", Message: "ANX " + state.LatestVersion + " is available; run anx update now"})
			}
		}
	}
	claim, err := os.OpenFile(filepath.Join(claims, day), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return warnings
	}
	_ = claim.Close()
	if a.startUpdateWorker == nil {
		return warnings
	}
	if err := a.startUpdateWorker(path, cfg.ConfigDir); err != nil {
		state.CheckedOn = day
		state.FailureStage = "worker_start"
		state.FailureCode = "update_worker_start_failed"
		_ = writeUpdateJSON(filepath.Join(dir, "state.json"), state)
	}
	return warnings
}
func startDetachedUpdateWorker(path, configDir string) error {
	args := []string{"update", "now", "--scheduled"}
	if configDir != "" {
		args = append([]string{"--config-dir", configDir}, args...)
	}
	cmd := exec.Command(path, args...)
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer null.Close()
	cmd.Stdin = null
	cmd.Stdout = null
	cmd.Stderr = null
	attachDetachedSkillsProcess(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

var updateProbeBinary = func(ctx context.Context, path, version string) error {
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	b, err := exec.CommandContext(probeCtx, path, "--json", "version").Output()
	if err != nil {
		return err
	}
	var envelope struct {
		OK     bool `json:"ok"`
		Result struct {
			Version string `json:"cli_version"`
		} `json:"result"`
	}
	if json.Unmarshal(b, &envelope) != nil || !envelope.OK || normalizeReleaseTag(envelope.Result.Version) != normalizeReleaseTag(version) {
		return fmt.Errorf("installed binary version does not match release")
	}
	return nil
}
var updateSyncSkills = func(ctx context.Context, path, configDir string) error {
	args := []string{"--json", "skills", "sync"}
	if configDir != "" {
		args = append([]string{"--config-dir", configDir}, args...)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = append(os.Environ(), "ANX_UPDATE_POLICY=off")
	b, err := cmd.Output()
	if err != nil {
		return err
	}
	var envelope struct {
		OK     bool `json:"ok"`
		Result struct {
			Skills []struct {
				State string `json:"state"`
			} `json:"skills"`
		} `json:"result"`
	}
	if err := json.Unmarshal(b, &envelope); err != nil {
		return err
	}
	if !envelope.OK {
		return fmt.Errorf("skills sync failed")
	}
	for _, skill := range envelope.Result.Skills {
		if skill.State == "conflict" || skill.State == "outdated" {
			return fmt.Errorf("managed skills need attention; run anx skills status")
		}
	}
	return nil
}

func (a *App) performManagedUpdate(ctx context.Context, cfg config.Resolved, o updateOptions) (*commandResult, error) {
	// Legacy --check remains a pure release read, including unmanaged installs.
	if o.check {
		plan, err := buildUpdatePlan(ctx, cfg, o.version)
		if err != nil {
			return nil, err
		}
		return renderUpdatePlan(plan, false), nil
	}
	dir, err := a.updateDirectory(cfg)
	if err != nil {
		return nil, err
	}
	policy, _, err := a.updatePolicy(dir)
	if err != nil {
		return nil, err
	}
	if o.scheduled && policy == "off" {
		return a.runUpdateStatus(cfg)
	}
	path, record, _, reason := inspectUpdateInstall()
	if reason != "" {
		return nil, errnorm.WithDetails(errnorm.Local("unmanaged_install", "self-update requires a matching ANX installer receipt; rerun the release installer"), map[string]any{"skip_reason": reason})
	}
	if os.MkdirAll(dir, 0700) != nil {
		return nil, errnorm.Local("update_write_failed", "cannot create update state directory")
	}
	// Lock beside the executable serializes all workspaces/config directories.
	lockPath := path + ".anx-update.lock"
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		// A killed short-lived worker must not disable updates permanently.
		if info, e := os.Stat(lockPath); e == nil && time.Since(info.ModTime()) > 10*time.Minute {
			_ = os.Remove(lockPath)
			lock, err = os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		}
	}
	if err != nil {
		return nil, errnorm.Local("update_locked", "another update is active; inspect anx update status")
	}
	_ = lock.Close()
	defer os.Remove(lockPath)
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	state := updateState{CheckedOn: a.clockNow().UTC().Format("2006-01-02"), Rollback: "not_needed"}
	stage := "resolve_release"
	fail := func(err error) (*commandResult, error) {
		state.FailureStage = stage
		state.FailureCode = errnorm.Normalize(err).Code
		_ = writeUpdateJSON(filepath.Join(dir, "state.json"), state)
		return nil, err
	}
	// A worker launched by the previous image must not downgrade a binary
	// another updater has already replaced while this process was waiting.
	stage = "observe_binary"
	if err := updateProbeBinary(ctx, path, httpclient.CLIVersion); err != nil {
		return fail(errnorm.Wrap(errnorm.KindLocal, "update_binary_changed", "installed executable differs from this running CLI; rerun anx update now", err))
	}
	stage = "resolve_release"
	plan, err := buildUpdatePlan(ctx, cfg, o.version)
	if err != nil {
		return fail(err)
	}
	plan.InstallPath = path
	state.LatestVersion = plan.TargetVersion
	if plan.AlreadyCurrent || o.scheduled && policy == "notify" {
		prior, _ := readUpdateState(dir)
		state.SkillsSynced = prior.SkillsSynced
		if plan.AlreadyCurrent && record.ManagedBy == "anx" && normalizeReleaseTag(record.Version) != plan.CurrentVersion {
			record.Version = plan.CurrentVersion
			if err := writeUpdateJSON(installRecordPath(path), record); err != nil {
				return fail(err)
			}
		}
		if err := writeUpdateJSON(filepath.Join(dir, "state.json"), state); err != nil {
			return fail(err)
		}
		return renderUpdatePlan(plan, false), nil
	}
	// Ownership may have changed while the release lookup was in flight.
	_, latestRecord, _, reason := inspectUpdateInstall()
	if reason != "" || latestRecord != record {
		return fail(errnorm.Local("unmanaged_install", "installation changed during release check"))
	}
	stage = "download_verify"
	binary, mode, err := downloadUpdateBinary(ctx, cfg.Timeout, plan.TargetVersion, plan.ArchiveName)
	if err != nil {
		return fail(err)
	}
	stage = "replace"
	rollback, backup, err := replaceManagedExecutable(ctx, path, binary, mode, plan.TargetVersion, record)
	state.Rollback = rollback
	state.BackupPath = backup
	if err != nil {
		return fail(err)
	}
	state.SkillsSynced = false
	stage = "skills_sync"
	if err = updateSyncSkills(ctx, path, cfg.ConfigDir); err != nil {
		return fail(errnorm.Wrap(errnorm.KindLocal, "update_skills_failed", "binary updated; retry anx skills sync", err))
	}
	state.SkillsSynced = true
	if err = writeUpdateJSON(filepath.Join(dir, "state.json"), state); err != nil {
		return fail(err)
	}
	result := renderUpdatePlan(plan, true)
	appendUpdateBridgeReminderText(&result.Text)
	return result, nil
}

// The original stays at its path until the candidate is verified. Keep a
// separate backup through post-replace verification and receipt commit.
func replaceManagedExecutable(ctx context.Context, path string, binary []byte, mode os.FileMode, version string, oldRecord updateInstallRecord) (string, string, error) {
	old, err := os.ReadFile(path)
	if err != nil {
		return "not_needed", "", err
	}
	originalSum := sha256.Sum256(old)
	if hex.EncodeToString(originalSum[:]) != oldRecord.SHA256 {
		return "not_needed", "", errnorm.Local("unmanaged_install", "binary changed before replacement")
	}
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if mode == 0 {
		mode = 0755
	}
	tmp, err := os.MkdirTemp(filepath.Dir(path), ".anx-update-")
	if err != nil {
		return "not_needed", "", err
	}
	defer os.RemoveAll(tmp)
	candidate := filepath.Join(tmp, filepath.Base(path))
	if err := updateWriteFile(candidate, binary, mode); err != nil {
		return "not_needed", "", err
	}
	if err := updateChmod(candidate, mode); err != nil {
		return "not_needed", "", err
	}
	if err := updateProbeBinary(ctx, candidate, version); err != nil {
		return "not_needed", "", errnorm.Wrap(errnorm.KindLocal, "update_probe_failed", "release binary failed verification", err)
	}
	backupFile, err := os.CreateTemp(filepath.Dir(path), ".anx-rollback-")
	if err != nil {
		return "not_needed", "", err
	}
	backup := backupFile.Name()
	if _, err = backupFile.Write(old); err == nil {
		err = backupFile.Chmod(mode)
	}
	if err == nil {
		err = backupFile.Sync()
	}
	closeErr := backupFile.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(backup)
		return "not_needed", "", err
	}
	rollback := func(cause error) (string, string, error) {
		if err := updateRename(backup, path); err != nil {
			return "failed", backup, errnorm.Wrap(errnorm.KindLocal, "update_rollback_failed", "rollback failed; original binary retained at backup path", err)
		}
		if err := writeUpdateJSON(installRecordPath(path), oldRecord); err != nil {
			return "failed", "", errnorm.Wrap(errnorm.KindLocal, "update_rollback_record_failed", "binary restored but installer receipt could not be restored", err)
		}
		return "succeeded", "", cause
	}
	if err := updateRename(candidate, path); err != nil {
		_ = os.Remove(backup)
		return "not_needed", "", errnorm.Wrap(errnorm.KindLocal, "update_replace_failed", "atomic binary replacement failed", err)
	}
	if err := updateProbeBinary(ctx, path, version); err != nil {
		return rollback(errnorm.Wrap(errnorm.KindLocal, "update_probe_failed", "installed binary failed verification", err))
	}
	sum := sha256.Sum256(binary)
	record := updateInstallRecord{ManagedBy: "anx", Version: version, SHA256: hex.EncodeToString(sum[:]), InstalledAt: time.Now().UTC().Format(time.RFC3339)}
	if err := writeUpdateJSON(installRecordPath(path), record); err != nil {
		return rollback(err)
	}
	_ = os.Remove(backup)
	return "not_needed", "", nil
}
