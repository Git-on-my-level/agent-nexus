// Deciding when a managed self-update may run: invocation eligibility (CI,
// harness, principal), attempt accounting, follow-up warnings, and launching
// the detached update worker.

package app

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/httpclient"
	"agent-nexus-cli/internal/output"
)

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
	skipServerTriggerNoWorkspace := policy == "auto" && !hasResolvedUpdateWorkspace(cfg)
	if policy == "auto" && hasResolvedUpdateWorkspace(cfg) && a.startServerUpdateWorker != nil && claimUpdateDay(claims, day+".server") {
		workers = append(workers, scheduledUpdateWorker{trigger: "server_version", start: func() error { return a.startServerUpdateWorker(path, cfg.ConfigDir, cfg.BaseURL) }})
	}
	if len(workers) == 0 && !skipServerTriggerNoWorkspace {
		return warnings, nil
	}
	if stateLock, lockErr := lockUpdateInstall(path); lockErr == nil {
		latestState, _ := readUpdateState(dir)
		for _, worker := range workers {
			latestState = markAutomaticAttempt(latestState, day, attemptAt, worker.trigger)
		}
		if skipServerTriggerNoWorkspace {
			markServerTriggerNoWorkspace(&latestState, attemptAt)
		}
		_ = writeUpdateJSON(filepath.Join(dir, "state.json"), latestState)
		_ = stateLock.Close()
	}
	if len(workers) == 0 {
		return warnings, nil
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
	state.FailureReason = ""
	state.FailureCategory = ""
	state.LastAutomaticAttemptAt = at
	state.LastAutomaticTrigger = trigger
	state.LastAutomaticResult = "scheduled"
	if trigger == "server_version" {
		state.ServerTriggerCheckedAt = at
		state.ServerTriggerResult = "scheduled"
		state.ServerTriggerReason = ""
		state.ServerTriggerFailureCategory = ""
	}
	return state
}

func hasResolvedUpdateWorkspace(cfg config.Resolved) bool {
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		return false
	}
	source := strings.TrimSpace(cfg.Sources["base_url"])
	if source == "default" || strings.HasPrefix(source, "unresolved:") {
		return false
	}
	return source != "" || strings.TrimRight(baseURL, "/") != config.DefaultBaseURL
}

func markServerTriggerNoWorkspace(state *updateState, at string) {
	state.ServerTriggerCheckedAt = at
	state.ServerTriggerResult = "skipped_no_workspace"
	state.ServerTriggerReason = "no workspace was resolved for the triggering command"
	state.ServerTriggerFailureCategory = ""
	state.ServerVersion = ""
}

func updateFailureReason(stage string) string {
	switch stage {
	case "transaction_pending":
		return "an interrupted update needs recovery"
	case "transaction_invalid":
		return "the update transaction record is invalid"
	case "worker_start":
		return "could not start the automatic update worker"
	case "crash_recovery":
		return "the interrupted update could not be recovered"
	case "inspect_install":
		return "the installed executable is no longer managed by anx"
	case "server_handshake":
		return "workspace handshake failed; the daily release check continued"
	case "observe_binary":
		return "could not verify the installed executable"
	case "resolve_release":
		return "could not resolve a verified release"
	case "download_verify":
		return "release download or checksum verification failed"
	case "replace":
		return "could not safely replace the installed executable"
	case "skills_sync":
		return "the binary updated, but managed skill sync needs attention"
	default:
		return "automatic update failed"
	}
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
	state.FailureReason = updateFailureReason(state.FailureStage)
	state.FailureCategory = ""
	state.LastAutomaticResult = "failed"
	if trigger == "server_version" {
		state.ServerTriggerResult = "worker_start_failed"
		state.ServerTriggerReason = state.FailureReason
		state.ServerTriggerFailureCategory = ""
	}
	_ = writeUpdateJSON(filepath.Join(directory, "state.json"), state)
}

func (a *App) updateSchedulingInteractive() bool {
	return a.StdinIsTTY != nil && a.StdinIsTTY() && a.StdoutIsTTY != nil && a.StdoutIsTTY()
}

func (a *App) updateSchedulingAllowed(cfg config.Resolved, interactive bool) bool {
	if a.updateKnownCIInvocation() {
		return false
	}
	if a.updateHarnessInvocation() {
		return true
	}
	if a.Getenv != nil && strings.TrimSpace(a.Getenv("CI")) != "" {
		return false
	}
	return interactive || a.updatePrincipalInvocation(cfg)
}

func (a *App) updateKnownCIInvocation() bool {
	if a.Getenv == nil {
		return false
	}
	for _, key := range []string{
		"JENKINS_URL", "GITHUB_ACTIONS", "GITLAB_CI", "BUILDKITE", "CIRCLECI", "TF_BUILD", "TEAMCITY_VERSION",
	} {
		if strings.TrimSpace(a.Getenv(key)) != "" {
			return true
		}
	}
	return strings.TrimSpace(a.Getenv("BUILD_ID")) != "" && strings.TrimSpace(a.Getenv("JENKINS_HOME")) != ""
}

func (a *App) updatePrincipalInvocation(cfg config.Resolved) bool {
	if strings.TrimSpace(cfg.As) != "" {
		return true
	}
	return a.Getenv != nil && strings.TrimSpace(a.Getenv("ANX_AS")) != ""
}

func (a *App) updateHarnessInvocation() bool {
	if a.Getenv == nil {
		return false
	}
	for _, key := range []string{"AGENTCTL_CALLER_HARNESS", "CLAUDECODE", "CODEX_THREAD_ID", "CURSOR_AGENT_COMPLETED_PATH", "HERMES_SESSION_ID"} {
		if strings.TrimSpace(a.Getenv(key)) != "" {
			return true
		}
	}
	if a.Getenv("CURSOR_AGENT") == "1" || strings.EqualFold(strings.TrimSpace(a.Getenv("HERMES_AGENT")), "true") || strings.EqualFold(strings.TrimSpace(a.Getenv("AI_AGENT")), "hermes-agent") {
		return true
	}
	if strings.TrimSpace(a.Getenv("AGENTCTL_EXECUTION_ID")) != "" && strings.TrimSpace(a.Getenv("AGENTCTL_ADAPTER")) != "" {
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
	cmd := exec.Command(path, serverUpdateWorkerArgs(configDir)...)
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

func serverUpdateWorkerArgs(configDir string) []string {
	args := []string{"update", "now", "--scheduled", "--server-trigger"}
	if configDir != "" {
		args = append([]string{"--config-dir", configDir}, args...)
	}
	return args
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
