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
		if _, err := parseSemanticVersion(o.version); err != nil || strings.ContainsAny(o.version, "/\\?# \t\r\n\"'") {
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
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return syncUpdateDirectory(filepath.Dir(path))
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
	tx, txErr := readUpdateTransaction(path)
	var transaction any
	if txErr == nil {
		transaction = tx
		if tx.Phase == "prepared" {
			state.BackupPath = tx.BackupPath
			if state.Rollback != "failed" {
				state.Rollback = "pending"
				state.FailureStage = "transaction_pending"
			}
		}
	}
	if txErr != nil && !errors.Is(txErr, os.ErrNotExist) {
		state.FailureStage = "transaction_invalid"
	}
	return &commandResult{Data: map[string]any{
		"policy": policy, "policy_source": source, "managed": reason == "", "skip_reason": reason, "install_path": path,
		"observed_binary":  map[string]any{"version": httpclient.CLIVersion, "sha256": digest, "bookkeeping_matches": record.SHA256 == digest && digest != "" && normalizeReleaseTag(record.Version) == normalizeReleaseTag(httpclient.CLIVersion)},
		"installer_record": record, "transaction": transaction, "state": state, "state_path": filepath.Join(dir, "state.json"),
	}}, nil
}

// This is deliberately bounded to successful classified reads and writes.
// Local maintenance, update commands, streaming waits, dry runs and help
// cannot trigger binary maintenance.
func updateInvocationEligible(command string, args []string, results ...*commandResult) bool {
	for _, result := range results {
		if result != nil {
			body := asMap(result.Data)
			if asBool(body["dry_run"]) || anyString(body["status"]) == "dry_run" {
				return false
			}
		}
	}
	command = strings.TrimSpace(command)
	if strings.HasPrefix(command, "update") || command == "await" || command == "help" {
		return false
	}
	sideEffect := commandSideEffectClass(command)
	if sideEffect != "read_only" && sideEffect != "remote_coordination_write" {
		return false
	}
	// Use the same bool parser as trackedBool; account for all spellings and
	// repeated flags. Results above override this conservative argv fallback.
	preview := map[string]bool{}
	for _, arg := range args {
		name, value, inline, option := parseLongOptionToken(arg)
		if !option || name != "dry-run" && name != "plan" {
			continue
		}
		if !inline {
			preview[name] = true
			continue
		}
		parsed, err := strconvParseBool(value)
		if err != nil {
			return false
		}
		preview[name] = parsed
	}
	for _, value := range preview {
		if value {
			return false
		}
	}
	return true
}
func (a *App) maybeScheduleUpdate(command string, args []string, cfg config.Resolved, results ...*commandResult) []output.Warning {
	if !a.updateSchedulingInteractive(cfg) || !updateInvocationEligible(command, args, results...) {
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

// An automatic binary replacement is only safe from an attended interactive
// invocation. CI, redirected/piped invocations and PM turns must not have the
// installed binary replaced underneath the work currently using it.
func (a *App) updateSchedulingInteractive(cfg config.Resolved) bool {
	if a.Getenv != nil && strings.TrimSpace(a.Getenv("CI")) != "" {
		return false
	}
	if strings.EqualFold(cfg.As, "pm") || strings.EqualFold(cfg.Agent, "pm") {
		return false
	}
	if a.Getenv != nil {
		pmAgent := strings.TrimSpace(a.Getenv("ANX_PM_AGENT")) != ""
		pmTurn := strings.TrimSpace(a.Getenv("ANX_PM_TURN_ID")) != ""
		if pmAgent || pmTurn {
			return false
		}
	}
	return a.StdinIsTTY != nil && a.StdinIsTTY() && a.StdoutIsTTY != nil && a.StdoutIsTTY()
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
		if skill.State == "conflict" || skill.State == "outdated" || skill.State == "drifted" {
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
	if _, journalErr := os.Stat(updateTransactionPath(path)); reason != "" && errors.Is(journalErr, os.ErrNotExist) {
		return nil, errnorm.WithDetails(errnorm.Local("unmanaged_install", "self-update requires a matching ANX installer receipt; rerun the release installer"), map[string]any{"skip_reason": reason})
	}
	if os.MkdirAll(dir, 0700) != nil {
		return nil, errnorm.Local("update_write_failed", "cannot create update state directory")
	}
	lock, err := lockUpdateInstall(path)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	outcome, backup, recoveryErr := recoverUpdateTransaction(path)
	if outcome != "not_needed" || recoveryErr != nil {
		state := updateState{CheckedOn: a.clockNow().UTC().Format("2006-01-02"), FailureStage: "crash_recovery", Rollback: outcome, BackupPath: backup}
		if recoveryErr != nil {
			state.FailureCode = "update_recovery_failed"
		}
		if err := writeUpdateJSON(filepath.Join(dir, "state.json"), state); err != nil {
			return nil, err
		}
		if recoveryErr != nil {
			return nil, errnorm.Wrap(errnorm.KindLocal, "update_recovery_failed", "interrupted transaction needs recovery", recoveryErr)
		}
		return a.runUpdateStatus(cfg)
	}
	// Reinspect after taking the process lock.
	path, record, _, reason = inspectUpdateInstall()
	if reason != "" {
		return nil, errnorm.Local("unmanaged_install", "installation changed before lock acquisition")
	}
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
