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
	CheckedOn              string `json:"checked_on,omitempty"`
	LatestVersion          string `json:"latest_version,omitempty"`
	FailureStage           string `json:"failure_stage,omitempty"`
	FailureCode            string `json:"failure_code,omitempty"`
	Rollback               string `json:"rollback"`
	BackupPath             string `json:"backup_path,omitempty"`
	SkillsSynced           bool   `json:"skills_synced"`
	SkillsSyncError        string `json:"skills_sync_error,omitempty"`
	FollowUpNoticePending  bool   `json:"follow_up_notice_pending,omitempty"`
	FollowUpNoticeID       string `json:"follow_up_notice_id,omitempty"`
	LastAutomaticAttemptAt string `json:"last_automatic_attempt_at,omitempty"`
	LastAutomaticTrigger   string `json:"last_automatic_trigger,omitempty"`
	LastAutomaticResult    string `json:"last_automatic_result,omitempty"`
	ServerVersion          string `json:"server_version,omitempty"`
	ServerTriggerCheckedAt string `json:"server_trigger_checked_at,omitempty"`
	ServerTriggerResult    string `json:"server_trigger_result,omitempty"`
}
type updateOptions struct {
	verb, policy, version string
	check, scheduled      bool
	serverTrigger         bool
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
	fs.BoolVar(&o.serverTrigger, "server-trigger", false, "Internal workspace-version worker")
	fs.StringVar(&o.version, "version", "", "Release tag")
	if err := fs.Parse(args); err != nil {
		return o, errnorm.Usage("invalid_update_flags", err.Error())
	}
	if len(fs.Args()) != 0 || o.verb == "status" && len(args) != 0 || o.scheduled && (o.check || o.version != "") || o.serverTrigger && !o.scheduled {
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
func isScheduledUpdateInvocation(command string, args []string) bool {
	if !strings.HasPrefix(strings.TrimSpace(command), "update") || len(args) < 2 || args[0] != "update" {
		return false
	}
	opts, err := parseUpdateOptions(args[1:])
	return err == nil && opts.scheduled
}

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
		"last_automatic_attempt": map[string]any{"at": state.LastAutomaticAttemptAt, "trigger": state.LastAutomaticTrigger, "result": state.LastAutomaticResult},
		"server_version_trigger": map[string]any{"server_version": state.ServerVersion, "checked_at": state.ServerTriggerCheckedAt, "result": state.ServerTriggerResult},
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

type scheduledUpdateWorker struct {
	trigger string
	start   func() error
}

func (a *App) prepareUpdateSchedule(command string, args []string, cfg config.Resolved, results ...*commandResult) ([]output.Warning, func()) {
	interactive := a.updateSchedulingInteractive()
	if !a.updateSchedulingAllowed(cfg, interactive) || !updateInvocationEligible(command, args, results...) {
		return nil, nil
	}
	dir, err := a.updateDirectory(cfg)
	if err != nil {
		return nil, nil
	}
	policy, _, err := a.updatePolicy(dir)
	if err != nil || policy == "off" || policy == "notify" && !interactive {
		return nil, nil
	}
	path, _, _, reason := inspectUpdateInstall(false)
	if reason != "" {
		return nil, nil
	}
	day := a.clockNow().UTC().Format("2006-01-02")
	attemptAt := a.clockNow().UTC().Format(time.RFC3339Nano)
	// A separate notification claim lets a worker's observation become visible
	// on a later foreground invocation on the same day, without extra checks.
	claims := path + ".anx-update-days"
	if os.MkdirAll(claims, 0700) != nil {
		return nil, nil
	}
	state, _ := readUpdateState(dir)
	var warnings []output.Warning
	var workers []scheduledUpdateWorker
	if policy == "notify" && state.LatestVersion != "" {
		if cmp, err := compareSemanticVersions(httpclient.CLIVersion, state.LatestVersion); err == nil && cmp < 0 {
			notice, err := os.OpenFile(filepath.Join(claims, day+".notice"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err == nil {
				_ = notice.Close()
				warnings = append(warnings, output.Warning{Code: "cli_update_available", Message: "ANX " + state.LatestVersion + " is available; run anx update now"})
			}
		}
	}
	if a.startUpdateWorker != nil && claimUpdateDay(claims, day) {
		workers = append(workers, scheduledUpdateWorker{trigger: "daily", start: func() error { return a.startUpdateWorker(path, cfg.ConfigDir) }})
	}
	if policy == "auto" && a.startServerUpdateWorker != nil && claimUpdateDay(claims, day+".server") {
		workers = append(workers, scheduledUpdateWorker{trigger: "server_version", start: func() error { return a.startServerUpdateWorker(path, cfg.ConfigDir, cfg.BaseURL) }})
	}
	if len(workers) == 0 {
		return warnings, nil
	}
	if stateLock, lockErr := lockUpdateInstall(path); lockErr == nil {
		latestState, _ := readUpdateState(dir)
		for _, worker := range workers {
			latestState = markAutomaticAttempt(latestState, day, attemptAt, worker.trigger)
		}
		_ = writeUpdateJSON(filepath.Join(dir, "state.json"), latestState)
		_ = stateLock.Close()
	}
	afterOutput := func() {
		for _, worker := range workers {
			if err := worker.start(); err != nil {
				a.recordUpdateWorkerStartFailure(path, dir, day, attemptAt, worker.trigger)
			}
		}
	}
	return warnings, afterOutput
}

func (a *App) maybeScheduleUpdate(command string, args []string, cfg config.Resolved, results ...*commandResult) []output.Warning {
	warnings, afterOutput := a.prepareUpdateSchedule(command, args, cfg, results...)
	if afterOutput != nil {
		afterOutput()
	}
	return warnings
}

func claimUpdateDay(directory, claim string) bool {
	f, err := os.OpenFile(filepath.Join(directory, claim), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

func markAutomaticAttempt(state updateState, day, at, trigger string) updateState {
	state.CheckedOn = day
	state.FailureStage = ""
	state.FailureCode = ""
	state.LastAutomaticAttemptAt = at
	state.LastAutomaticTrigger = trigger
	state.LastAutomaticResult = "scheduled"
	if trigger == "server_version" {
		state.ServerTriggerCheckedAt = at
		state.ServerTriggerResult = "scheduled"
	}
	return state
}

func (a *App) recordUpdateWorkerStartFailure(path, directory, day, at, trigger string) {
	lock, err := lockUpdateInstall(path)
	if err != nil {
		return
	}
	defer lock.Close()
	state, _ := readUpdateState(directory)
	if state.LastAutomaticAttemptAt != at || state.LastAutomaticTrigger != trigger {
		return
	}
	state.CheckedOn = day
	state.FailureStage = "worker_start"
	state.FailureCode = "update_worker_start_failed"
	state.LastAutomaticResult = "failed"
	if trigger == "server_version" {
		state.ServerTriggerResult = "worker_start_failed"
	}
	_ = writeUpdateJSON(filepath.Join(directory, "state.json"), state)
}

func (a *App) updateSchedulingInteractive() bool {
	return a.StdinIsTTY != nil && a.StdinIsTTY() && a.StdoutIsTTY != nil && a.StdoutIsTTY()
}

func (a *App) updateSchedulingAllowed(cfg config.Resolved, interactive bool) bool {
	if a.Getenv != nil && strings.TrimSpace(a.Getenv("CI")) != "" {
		return false
	}
	return interactive || a.updateAgentInvocation(cfg)
}

func (a *App) updateAgentInvocation(cfg config.Resolved) bool {
	if strings.TrimSpace(cfg.As) != "" {
		return true
	}
	if a.Getenv == nil {
		return false
	}
	for _, key := range []string{"ANX_AS", "AGENTCTL_CALLER_HARNESS"} {
		if strings.TrimSpace(a.Getenv(key)) != "" {
			return true
		}
	}
	if strings.TrimSpace(a.Getenv("AGENTCTL_EXECUTION_ID")) != "" && strings.TrimSpace(a.Getenv("AGENTCTL_ADAPTER")) != "" {
		return true
	}
	if a.Getenv("CLAUDECODE") == "1" || strings.TrimSpace(a.Getenv("CODEX_THREAD_ID")) != "" || strings.TrimSpace(a.Getenv("CURSOR_AGENT_COMPLETED_PATH")) != "" {
		return true
	}
	return a.Getenv("AGENT") == "1" && a.hasOMPAncestor != nil && a.hasOMPAncestor()
}

func (a *App) updateFollowUpWarnings(cfg config.Resolved) []output.Warning {
	dir, err := a.updateDirectory(cfg)
	if err != nil {
		return nil
	}
	state, err := readUpdateState(dir)
	if err != nil || !state.FollowUpNoticePending {
		return nil
	}
	path, _, _, reason := inspectUpdateInstall(false)
	if reason != "" {
		return nil
	}
	lock, err := lockUpdateInstall(path)
	if err != nil {
		return nil
	}
	defer lock.Close()
	state, err = readUpdateState(dir)
	if err != nil || !state.FollowUpNoticePending {
		return nil
	}
	claims := path + ".anx-update-days"
	if os.MkdirAll(claims, 0700) != nil {
		return nil
	}
	identity := state.FollowUpNoticeID
	if identity == "" {
		identity = state.LastAutomaticAttemptAt + state.LatestVersion
	}
	sum := sha256.Sum256([]byte(identity))
	if !claimUpdateDay(claims, "follow-up-"+hex.EncodeToString(sum[:])) {
		return nil
	}
	state.FollowUpNoticePending = false
	_ = writeUpdateJSON(filepath.Join(dir, "state.json"), state)
	message := "CLI updated; reload the anx skill if this session has older instructions."
	if state.SkillsSyncError != "" {
		message += " Managed skill sync needs attention; run anx skills status."
	}
	return []output.Warning{{Code: "cli_update_follow_up", Message: message}}
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

func startDetachedServerUpdateWorker(path, configDir, baseURL string) error {
	args := []string{"update", "now", "--scheduled", "--server-trigger"}
	if configDir != "" {
		args = append([]string{"--config-dir", configDir}, args...)
	}
	cmd := exec.Command(path, args...)
	cmd.Env = replaceEnvironment(os.Environ(), "ANX_BASE_URL", baseURL)
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

func replaceEnvironment(environment []string, key, value string) []string {
	prefix := key + "="
	updated := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			updated = append(updated, entry)
		}
	}
	return append(updated, prefix+value)
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
	if o.serverTrigger && policy != "auto" {
		state, _ := readUpdateState(dir)
		state.LastAutomaticResult = "skipped_policy_changed"
		state.ServerTriggerResult = "skipped_policy_changed"
		_ = writeUpdateJSON(filepath.Join(dir, "state.json"), state)
		return &commandResult{Data: map[string]any{"updated": false, "skip_reason": "policy_changed", "state": state}}, nil
	}
	path, record, _, reason := inspectUpdateInstall()
	if _, journalErr := os.Stat(updateTransactionPath(path)); reason != "" && errors.Is(journalErr, os.ErrNotExist) {
		if o.scheduled {
			state, _ := readUpdateState(dir)
			state.CheckedOn = a.clockNow().UTC().Format("2006-01-02")
			state.LastAutomaticAttemptAt = a.clockNow().UTC().Format(time.RFC3339Nano)
			state.LastAutomaticTrigger = "daily"
			state.LastAutomaticResult = "skipped_unmanaged"
			if o.serverTrigger {
				state.LastAutomaticTrigger = "server_version"
				state.ServerTriggerCheckedAt = state.LastAutomaticAttemptAt
				state.ServerTriggerResult = "skipped_unmanaged"
			}
			state.FailureStage = ""
			state.FailureCode = ""
			_ = writeUpdateJSON(filepath.Join(dir, "state.json"), state)
			return &commandResult{Data: map[string]any{"updated": false, "skip_reason": reason, "state": state}}, nil
		}
		return nil, errnorm.WithDetails(errnorm.Local("unmanaged_install", "self-update requires a matching ANX installer receipt; rerun the release installer"), map[string]any{"skip_reason": reason})
	}
	if os.MkdirAll(dir, 0700) != nil {
		return nil, errnorm.Local("update_write_failed", "cannot create update state directory")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var lock *os.File
	if o.scheduled {
		lock, err = lockScheduledUpdateInstall(ctx, path)
	} else {
		lock, err = lockUpdateInstall(path)
	}
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	outcome, backup, recoveryErr := recoverUpdateTransaction(path)
	if outcome != "not_needed" || recoveryErr != nil {
		state, _ := readUpdateState(dir)
		state.CheckedOn = a.clockNow().UTC().Format("2006-01-02")
		state.FailureStage = "crash_recovery"
		state.Rollback = outcome
		state.BackupPath = backup
		if recoveryErr != nil {
			state.FailureCode = "update_recovery_failed"
			if o.scheduled {
				state.LastAutomaticResult = "failed"
			}
		} else if o.scheduled {
			state.LastAutomaticAttemptAt = a.clockNow().UTC().Format(time.RFC3339Nano)
			state.LastAutomaticTrigger = "daily"
			state.LastAutomaticResult = "recovered"
			if o.serverTrigger {
				state.LastAutomaticTrigger = "server_version"
				state.ServerTriggerCheckedAt = state.LastAutomaticAttemptAt
				state.ServerTriggerResult = "recovered"
			}
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
		if o.scheduled {
			state, _ := readUpdateState(dir)
			state.CheckedOn = a.clockNow().UTC().Format("2006-01-02")
			state.LastAutomaticAttemptAt = a.clockNow().UTC().Format(time.RFC3339Nano)
			state.LastAutomaticTrigger = "daily"
			state.LastAutomaticResult = "failed"
			state.FailureStage = "inspect_install"
			state.FailureCode = "unmanaged_install"
			if o.serverTrigger {
				state.LastAutomaticTrigger = "server_version"
				state.ServerTriggerCheckedAt = state.LastAutomaticAttemptAt
				state.ServerTriggerResult = "failed"
			}
			_ = writeUpdateJSON(filepath.Join(dir, "state.json"), state)
		}
		return nil, errnorm.Local("unmanaged_install", "installation changed before lock acquisition")
	}
	state, _ := readUpdateState(dir)
	state.CheckedOn = a.clockNow().UTC().Format("2006-01-02")
	state.FailureStage = ""
	state.FailureCode = ""
	state.Rollback = "not_needed"
	state.BackupPath = ""
	if o.scheduled {
		state.LastAutomaticAttemptAt = a.clockNow().UTC().Format(time.RFC3339Nano)
		state.LastAutomaticTrigger = "daily"
		state.LastAutomaticResult = "running"
		if o.serverTrigger {
			state.LastAutomaticTrigger = "server_version"
			state.ServerTriggerCheckedAt = state.LastAutomaticAttemptAt
			state.ServerTriggerResult = "checking"
		}
		if err := writeUpdateJSON(filepath.Join(dir, "state.json"), state); err != nil {
			return nil, err
		}
	}
	stage := "resolve_release"
	fail := func(err error) (*commandResult, error) {
		state.FailureStage = stage
		state.FailureCode = errnorm.Normalize(err).Code
		if o.scheduled {
			state.LastAutomaticResult = "failed"
			if o.serverTrigger {
				state.ServerTriggerResult = "failed"
			}
		}
		_ = writeUpdateJSON(filepath.Join(dir, "state.json"), state)
		return nil, err
	}
	// A worker launched by the previous image must not downgrade a binary
	// another updater has already replaced while this process was waiting.
	stage = "observe_binary"
	if err := updateProbeBinary(ctx, path, httpclient.CLIVersion); err != nil {
		if o.scheduled && normalizeReleaseTag(record.Version) != normalizeReleaseTag(httpclient.CLIVersion) {
			state.LastAutomaticResult = "superseded_by_newer_binary"
			if o.serverTrigger {
				state.ServerTriggerResult = "superseded_by_newer_binary"
			}
			if writeErr := writeUpdateJSON(filepath.Join(dir, "state.json"), state); writeErr != nil {
				return nil, writeErr
			}
			return &commandResult{Data: map[string]any{"updated": false, "skip_reason": "newer_binary_already_installed", "state": state}}, nil
		}
		return fail(errnorm.Wrap(errnorm.KindLocal, "update_binary_changed", "installed executable differs from this running CLI; rerun anx update now", err))
	}
	if o.serverTrigger {
		stage = "server_handshake"
		handshake, err := a.hostCall(ctx, cfg, "GET", "/meta/handshake", nil, nil)
		if err != nil {
			return fail(errnorm.Wrap(errnorm.KindNetwork, "update_handshake_failed", "failed to read workspace server version", err))
		}
		serverVersion := strings.TrimSpace(anyString(handshake["core_version"]))
		state.ServerVersion = serverVersion
		if serverVersion == "" {
			state.ServerTriggerResult = "skipped_no_server_version"
			state.LastAutomaticResult = "skipped_no_server_version"
			if err := writeUpdateJSON(filepath.Join(dir, "state.json"), state); err != nil {
				return nil, err
			}
			return &commandResult{Data: map[string]any{"updated": false, "skip_reason": "no_server_version", "state": state}}, nil
		}
		cmp, compareErr := compareSemanticVersions(httpclient.CLIVersion, serverVersion)
		if compareErr != nil {
			state.ServerTriggerResult = "skipped_uncomparable_server_version"
			state.LastAutomaticResult = "skipped_uncomparable_server_version"
			if err := writeUpdateJSON(filepath.Join(dir, "state.json"), state); err != nil {
				return nil, err
			}
			return &commandResult{Data: map[string]any{"updated": false, "skip_reason": "uncomparable_server_version", "state": state}}, nil
		}
		if cmp >= 0 {
			state.ServerTriggerResult = "not_newer"
			state.LastAutomaticResult = "not_newer"
			if err := writeUpdateJSON(filepath.Join(dir, "state.json"), state); err != nil {
				return nil, err
			}
			return &commandResult{Data: map[string]any{"updated": false, "skip_reason": "server_not_newer", "state": state}}, nil
		}
		state.ServerTriggerResult = "newer"
	}
	stage = "resolve_release"
	plan, err := buildUpdatePlan(ctx, cfg, o.version)
	if err != nil {
		return fail(err)
	}
	plan.InstallPath = path
	state.LatestVersion = plan.TargetVersion
	if plan.AlreadyCurrent || o.scheduled && policy == "notify" {
		if o.scheduled {
			state.LastAutomaticResult = "current"
			if o.serverTrigger && state.ServerTriggerResult == "newer" {
				state.ServerTriggerResult = "checked_release"
			}
			if o.scheduled && policy == "notify" && plan.UpdateAvailable {
				state.LastAutomaticResult = "available"
			}
		}
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
		state.SkillsSyncError = err.Error()
		state.FollowUpNoticePending = true
		state.FollowUpNoticeID = a.clockNow().UTC().Format(time.RFC3339Nano) + ":" + plan.TargetVersion
		if o.scheduled {
			state.FailureStage = ""
			state.FailureCode = ""
			state.LastAutomaticResult = "updated_skills_sync_attention"
			if o.serverTrigger {
				state.ServerTriggerResult = "updated_skills_sync_attention"
			}
			if err := writeUpdateJSON(filepath.Join(dir, "state.json"), state); err != nil {
				return nil, err
			}
			result := renderUpdatePlan(plan, true)
			appendUpdateBridgeReminderText(&result.Text)
			return result, nil
		}
		return fail(errnorm.Wrap(errnorm.KindLocal, "update_skills_failed", "binary updated; retry anx skills sync", err))
	}
	state.SkillsSynced = true
	state.SkillsSyncError = ""
	state.FollowUpNoticePending = true
	state.FollowUpNoticeID = a.clockNow().UTC().Format(time.RFC3339Nano) + ":" + plan.TargetVersion
	if o.scheduled {
		state.LastAutomaticResult = "updated"
		if o.serverTrigger {
			state.ServerTriggerResult = "updated"
		}
	}
	if err = writeUpdateJSON(filepath.Join(dir, "state.json"), state); err != nil {
		return fail(err)
	}
	result := renderUpdatePlan(plan, true)
	appendUpdateBridgeReminderText(&result.Text)
	return result, nil
}
