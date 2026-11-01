// The managed self-update: syncing managed skills and the
// performManagedUpdate install flow.

package app

import (
	"context"
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
		state.FailureCategory = ""
		state.ServerTriggerFailureCategory = ""
		_ = writeUpdateJSON(filepath.Join(dir, "state.json"), state)
		return &commandResult{Data: map[string]any{"updated": false, "skip_reason": "policy_changed", "state": state}}, nil
	}
	path, record, _, reason := inspectUpdateInstall()
	if o.serverTrigger && !hasResolvedUpdateWorkspace(cfg) {
		if path == "" {
			return nil, errnorm.Local("install_path_unavailable", "cannot lock update state without the installed executable path")
		}
		lock, lockErr := lockUpdateInstall(path)
		if lockErr != nil {
			return nil, lockErr
		}
		defer lock.Close()
		state, _ := readUpdateState(dir)
		at := a.clockNow().UTC().Format(time.RFC3339Nano)
		if state.LastAutomaticTrigger == "server_version" && state.LastAutomaticAttemptAt != "" {
			at = state.LastAutomaticAttemptAt
		} else {
			state.LastAutomaticAttemptAt = at
			state.LastAutomaticTrigger = "server_version"
		}
		state.CheckedOn = a.clockNow().UTC().Format("2006-01-02")
		markServerTriggerNoWorkspace(&state, at)
		state.FailureStage = ""
		state.FailureCode = ""
		state.FailureReason = ""
		state.FailureCategory = ""
		state.LastAutomaticResult = "skipped_no_workspace"
		if err := writeUpdateJSON(filepath.Join(dir, "state.json"), state); err != nil {
			return nil, err
		}
		return &commandResult{Data: map[string]any{"updated": false, "skip_reason": "no_workspace", "state": state}}, nil
	}
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
			state.FailureReason = ""
			state.FailureCategory = ""
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
			state.FailureReason = updateFailureReason(state.FailureStage)
			if o.scheduled {
				state.LastAutomaticResult = "failed"
			}
		} else {
			state.FailureReason = ""
			if o.scheduled {
				state.LastAutomaticAttemptAt = a.clockNow().UTC().Format(time.RFC3339Nano)
				state.LastAutomaticTrigger = "daily"
				state.LastAutomaticResult = "recovered"
				if o.serverTrigger {
					state.LastAutomaticTrigger = "server_version"
					state.ServerTriggerCheckedAt = state.LastAutomaticAttemptAt
					state.ServerTriggerResult = "recovered"
				}
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
			state.FailureReason = updateFailureReason(state.FailureStage)
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
	state.FailureReason = ""
	state.FailureCategory = ""
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
			state.ServerTriggerReason = ""
			state.ServerTriggerFailureCategory = ""
			state.ServerVersion = ""
		}
		if err := writeUpdateJSON(filepath.Join(dir, "state.json"), state); err != nil {
			return nil, err
		}
	}
	stage := "resolve_release"
	fail := func(err error) (*commandResult, error) {
		state.FailureStage = stage
		state.FailureCode = errnorm.Normalize(err).Code
		state.FailureReason = updateFailureReason(stage)
		if stage == "resolve_release" {
			state.FailureCategory = updateFailureCategory(err)
		}
		if o.scheduled {
			state.LastAutomaticResult = "failed"
			if o.serverTrigger {
				if state.ServerTriggerResult == "handshake_failed_fallback_daily" {
					state.ServerTriggerResult = "handshake_failed_daily_check_failed"
				} else {
					state.ServerTriggerResult = "failed"
				}
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
			state.FailureStage = stage
			state.FailureCode = "update_handshake_failed"
			state.FailureReason = updateFailureReason(stage)
			state.FailureCategory = updateFailureCategory(err)
			state.ServerTriggerReason = state.FailureReason
			state.ServerTriggerFailureCategory = state.FailureCategory
			state.ServerTriggerResult = "handshake_failed_fallback_daily"
		} else {
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
			state.FailureReason = ""
			state.LastAutomaticResult = "updated_skills_sync_attention"
			if o.serverTrigger && state.ServerTriggerResult != "handshake_failed_fallback_daily" {
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
		if o.serverTrigger && state.ServerTriggerResult != "handshake_failed_fallback_daily" {
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
