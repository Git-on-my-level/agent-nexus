// Update policy state and inspection: update state and options parsing, the
// install-record and binary digest checks, policy resolution, `anx update
// status`, and the shared binary probe step used by the transaction installer.
// Scheduling lives in update_schedule.go, the managed update itself in
// update_managed.go.

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
	CheckedOn                    string `json:"checked_on,omitempty"`
	LatestVersion                string `json:"latest_version,omitempty"`
	FailureStage                 string `json:"failure_stage,omitempty"`
	FailureCode                  string `json:"failure_code,omitempty"`
	FailureReason                string `json:"failure_reason,omitempty"`
	FailureCategory              string `json:"failure_category,omitempty"`
	Rollback                     string `json:"rollback"`
	BackupPath                   string `json:"backup_path,omitempty"`
	SkillsSynced                 bool   `json:"skills_synced"`
	SkillsSyncError              string `json:"skills_sync_error,omitempty"`
	FollowUpNoticePending        bool   `json:"follow_up_notice_pending,omitempty"`
	FollowUpNoticeID             string `json:"follow_up_notice_id,omitempty"`
	LastAutomaticAttemptAt       string `json:"last_automatic_attempt_at,omitempty"`
	LastAutomaticTrigger         string `json:"last_automatic_trigger,omitempty"`
	LastAutomaticResult          string `json:"last_automatic_result,omitempty"`
	ServerVersion                string `json:"server_version,omitempty"`
	ServerTriggerCheckedAt       string `json:"server_trigger_checked_at,omitempty"`
	ServerTriggerResult          string `json:"server_trigger_result,omitempty"`
	ServerTriggerReason          string `json:"server_trigger_reason,omitempty"`
	ServerTriggerFailureCategory string `json:"server_trigger_failure_category,omitempty"`
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
	if !validUpdateFailureCategory(s.FailureCategory) {
		s.FailureCategory = ""
	}
	if !validUpdateFailureCategory(s.ServerTriggerFailureCategory) {
		s.ServerTriggerFailureCategory = ""
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
				state.FailureReason = updateFailureReason(state.FailureStage)
			}
		}
	}
	if txErr != nil && !errors.Is(txErr, os.ErrNotExist) {
		state.FailureStage = "transaction_invalid"
		state.FailureReason = updateFailureReason(state.FailureStage)
	}
	return &commandResult{Data: map[string]any{
		"policy": policy, "policy_source": source, "managed": reason == "", "skip_reason": reason, "install_path": path,
		"observed_binary":  map[string]any{"version": httpclient.CLIVersion, "sha256": digest, "bookkeeping_matches": record.SHA256 == digest && digest != "" && normalizeReleaseTag(record.Version) == normalizeReleaseTag(httpclient.CLIVersion)},
		"installer_record": record, "transaction": transaction, "state": state, "state_path": filepath.Join(dir, "state.json"),
		"last_automatic_attempt": map[string]any{"at": state.LastAutomaticAttemptAt, "trigger": state.LastAutomaticTrigger, "result": state.LastAutomaticResult, "failure_category": state.FailureCategory},
		"server_version_trigger": map[string]any{"server_version": state.ServerVersion, "checked_at": state.ServerTriggerCheckedAt, "result": state.ServerTriggerResult, "reason": state.ServerTriggerReason, "failure_category": state.ServerTriggerFailureCategory},
	}}, nil
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
