package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/output"
)

var pmServiceOS = runtime.GOOS
var pmServiceExec = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
var pmServiceWaitForConnection = func(a *App, ctx context.Context, cfg config.Resolved, budget time.Duration, stateDir string, since time.Time) error {
	return a.waitPMConnection(ctx, cfg, budget, stateDir, since)
}
var pmServiceStageFile = stagePMPrivateFile
var pmServiceCommitFile = os.Rename

type pmServiceSettings struct {
	Workspace string `json:"workspace"`
	ConfigDir string `json:"config_dir"`
	Agent     string `json:"agent"`
	Runner    string `json:"runner"`
}

type pmServiceStatus struct {
	Installed   bool   `json:"installed"`
	Running     bool   `json:"running"`
	Workspace   string `json:"workspace"`
	Agent       string `json:"agent"`
	Service     string `json:"service"`
	Logs        string `json:"logs"`
	LastClaimAt string `json:"last_claim_at,omitempty"`
}

func init() {
	for _, verb := range []string{"install", "status", "uninstall"} {
		topic := localHelperTopic{Path: "pm " + verb, Summary: map[string]string{"install": "Install or update the PM service on this computer.", "status": "Read the local PM service and last accepted claim.", "uninstall": "Remove the local PM service and reset workspace onboarding."}[verb], JSONShape: "installed, running, workspace, agent, service, logs, last_claim_at", Composition: "Per-user launchd on macOS or systemd --user on Linux. Uses the selected workspace and profile; stores no credentials in the service definition.", Examples: []string{"anx --as pm pm " + verb}}
		if verb == "install" {
			topic.Flags = []localHelperFlag{{Name: "--wait", Description: "Wait for an accepted connection after install."}, {Name: "--wait-timeout <duration>", Description: "Bound the connection wait (default 90s, maximum 5m)."}, {Name: "--runner <argv>", Description: "Runner command; saved locally for subsequent installs. Use {prompt_file} for a private prompt file, or omit a placeholder to use agentctl's file transport."}}
		}
		if verb == "uninstall" {
			topic.Flags = []localHelperFlag{{Name: "--keep-registration", Description: "Keep workspace onboarding when moving the PM to another computer."}}
		}
		localHelperTopics = append(localHelperTopics, topic)
	}
}

func (a *App) runPMService(ctx context.Context, verb string, args []string, cfg config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("pm " + verb)
	var runner trackedString
	var wait bool
	var keepRegistration bool
	var warnings []output.Warning
	var waitTimeout = 90 * time.Second
	if verb == "install" {
		fs.Var(&runner, "runner", "Runner command")
		fs.BoolVar(&wait, "wait", false, "Wait for the first accepted PM connection")
		fs.DurationVar(&waitTimeout, "wait-timeout", 90*time.Second, "Connection wait budget")
	}
	if verb == "uninstall" {
		fs.BoolVar(&keepRegistration, "keep-registration", false, "Keep workspace PM onboarding")
	}
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) != 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected arguments for pm "+verb)
	}
	if verb == "install" && cfg.Sources["pm_install_mode"] == "interactive" {
		selected, command, err := a.pmInstallWizard(ctx, cfg)
		if err != nil {
			return nil, err
		}
		cfg = selected
		runner.value = command
		wait = true
	}
	if waitTimeout < time.Second || waitTimeout > 5*time.Minute {
		return nil, errnorm.Usage("invalid_flags", "--wait-timeout must be between 1s and 5m")
	}
	if pmServiceOS != "darwin" && pmServiceOS != "linux" {
		return nil, errnorm.New(errnorm.KindLocal, "unsupported_platform", "PM services support macOS and Linux")
	}
	home, err := a.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir, err := a.configDir(cfg)
	if err != nil {
		return nil, err
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	agent := firstNonEmpty(cfg.As, "pm")
	// Resolve the default human-less install profile explicitly to pm, retaining an
	// explicit --as profile. The service resolves fresh host grants on each start.
	if cfg.As == "" {
		agent = "pm"
	}
	id := pmServiceID(cfg.BaseURL, dir, agent)
	stateRoot := os.Getenv("XDG_STATE_HOME")
	if stateRoot == "" {
		stateRoot = filepath.Join(home, ".local", "state")
	}
	if !filepath.IsAbs(stateRoot) {
		return nil, fmt.Errorf("XDG_STATE_HOME must be absolute")
	}
	state := filepath.Join(stateRoot, "anx", "pm", id)
	unitDir := filepath.Join(home, "Library", "LaunchAgents")
	unitSuffix := ".plist"
	if pmServiceOS == "linux" {
		configRoot := os.Getenv("XDG_CONFIG_HOME")
		if configRoot == "" {
			configRoot = filepath.Join(home, ".config")
		}
		if !filepath.IsAbs(configRoot) {
			return nil, fmt.Errorf("XDG_CONFIG_HOME must be absolute")
		}
		unitDir = filepath.Join(configRoot, "systemd", "user")
		unitSuffix = ".service"
	}
	unit := filepath.Join(unitDir, id+unitSuffix)
	settingsPath := filepath.Join(state, "settings.json")
	settings := pmServiceSettings{Workspace: cfg.BaseURL, ConfigDir: dir, Agent: agent}
	var saved pmServiceSettings
	if b, e := os.ReadFile(settingsPath); e == nil {
		if e = json.Unmarshal(b, &saved); e != nil {
			return nil, e
		}
		settings.Runner = saved.Runner
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	status := pmServiceStatus{Workspace: cfg.BaseURL, Agent: agent, Service: id, Logs: state}
	installStartedAt := time.Time{}
	_, err = os.Stat(unit)
	status.Installed = err == nil
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	domain := "gui/" + strconv.Itoa(os.Getuid())
	command := func(name string, args ...string) error {
		_, err := pmServiceExec(ctx, name, args...)
		if err != nil {
			return fmt.Errorf("%s failed: %w", name, err)
		}
		return nil
	}
	switch verb {
	case "install":
		settings.Runner = firstNonEmpty(runner.value, settings.Runner)
		argv, e := splitRunnerArgv(settings.Runner)
		if e != nil || len(argv) == 0 {
			return nil, errnorm.Usage("runner_required", "set --runner once, or run anx --as pm pm install in an interactive terminal for the setup wizard; subsequent non-interactive installs reuse settings.json")
		}
		binary, e := os.Executable()
		if e != nil {
			return nil, e
		}
		binary, e = filepath.EvalSymlinks(binary)
		if e != nil {
			return nil, e
		}
		for _, path := range []string{state, filepath.Dir(unit)} {
			if e = os.MkdirAll(path, 0700); e != nil {
				return nil, e
			}
		}
		for _, name := range []string{"stdout.log", "stderr.log"} {
			file, e := os.OpenFile(filepath.Join(state, name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if e != nil {
				return nil, e
			}
			e = file.Chmod(0600)
			file.Close()
			if e != nil {
				return nil, e
			}
		}
		b, _ := json.MarshalIndent(settings, "", "  ")
		settingsStage, stageErr := pmServiceStageFile(settingsPath, b)
		if stageErr != nil {
			return nil, stageErr
		}
		defer os.Remove(settingsStage)
		serviceArgs := []string{binary, "--base-url", cfg.BaseURL, "--config-dir", dir, "--as", agent, "pm", "serve", "--runner", settings.Runner, "--work-dir", state, "--poll-interval", "5s", "--max-concurrent", "1"}
		definition := pmServiceDefinition(pmServiceOS, id, serviceArgs, state, home, os.Getenv("PATH"))
		unitStage, stageErr := pmServiceStageFile(unit, []byte(definition))
		if stageErr != nil {
			return nil, stageErr
		}
		defer os.Remove(unitStage)
		validateDestination := func(path string) error {
			info, err := os.Lstat(path)
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("refusing to replace non-regular PM service file %s", path)
			}
			return nil
		}
		if e = validateDestination(settingsPath); e != nil {
			return nil, e
		}
		if e = validateDestination(unit); e != nil {
			return nil, e
		}
		stageBackup := func(path string) (string, bool, error) {
			data, err := os.ReadFile(path)
			if os.IsNotExist(err) {
				return "", false, nil
			}
			if err != nil {
				return "", false, err
			}
			staged, err := pmServiceStageFile(path, data)
			return staged, err == nil, err
		}
		oldSettingsStage, hadOldSettings, err := stageBackup(settingsPath)
		if err != nil {
			return nil, err
		}
		if hadOldSettings {
			defer os.Remove(oldSettingsStage)
		}
		oldUnitStage, hadOldUnit, err := stageBackup(unit)
		if err != nil {
			return nil, err
		}
		if hadOldUnit {
			defer os.Remove(oldUnitStage)
		}
		linuxServiceWasEnabled := false
		if pmServiceOS == "linux" {
			out, enabledErr := pmServiceExec(ctx, "systemctl", "--user", "is-enabled", id+".service")
			linuxServiceWasEnabled = enabledErr == nil && strings.TrimSpace(string(out)) == "enabled"
		}
		linuxServiceEnableAttempted := false
		activationAttempted := false
		// Stage and validate the complete replacement before stopping the current
		// process. A preparation failure therefore leaves a working service alone.
		if status.Installed {
			if pmServiceOS == "darwin" {
				out, stopErr := pmServiceExec(ctx, "launchctl", "bootout", domain+"/"+id)
				if stopErr != nil && !strings.Contains(string(out), "Could not find service") && !strings.Contains(string(out), "No such process") {
					return nil, fmt.Errorf("launchctl bootout failed: %w", stopErr)
				}
			} else {
				if e = command("systemctl", "--user", "daemon-reload"); e != nil {
					return nil, e
				}
				if e = command("systemctl", "--user", "stop", id+".service"); e != nil {
					return nil, e
				}
			}
		}
		settingsCommitted := false
		unitCommitted := false
		rollbackFilesAndService := func(commitErr error) error {
			var rollbackErrors []string
			managerReady := true
			if pmServiceOS == "darwin" && activationAttempted {
				out, err := pmServiceExec(ctx, "launchctl", "bootout", domain+"/"+id)
				if err != nil && !strings.Contains(string(out), "Could not find service") && !strings.Contains(string(out), "No such process") {
					rollbackErrors = append(rollbackErrors, fmt.Sprintf("stop failed replacement service: %v", err))
					managerReady = false
				}
			} else if pmServiceOS == "linux" && linuxServiceEnableAttempted && !linuxServiceWasEnabled {
				if _, err := pmServiceExec(ctx, "systemctl", "--user", "disable", "--now", id+".service"); err != nil {
					rollbackErrors = append(rollbackErrors, fmt.Sprintf("disable failed replacement service: %v", err))
					managerReady = false
				}
			} else if pmServiceOS == "linux" && activationAttempted {
				if _, err := pmServiceExec(ctx, "systemctl", "--user", "stop", id+".service"); err != nil {
					rollbackErrors = append(rollbackErrors, fmt.Sprintf("stop failed replacement service: %v", err))
					managerReady = false
				}
			}
			filesRestored := true
			restore := func(path, backup string, existed bool, committed bool) {
				if !committed {
					return
				}
				if existed {
					if err := pmServiceCommitFile(backup, path); err != nil {
						rollbackErrors = append(rollbackErrors, fmt.Sprintf("restore %s: %v", path, err))
						filesRestored = false
					}
				} else if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					rollbackErrors = append(rollbackErrors, fmt.Sprintf("remove incomplete %s: %v", path, err))
					filesRestored = false
				}
			}
			restore(unit, oldUnitStage, hadOldUnit, unitCommitted)
			restore(settingsPath, oldSettingsStage, hadOldSettings, settingsCommitted)
			if pmServiceOS == "linux" {
				if _, err := pmServiceExec(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
					rollbackErrors = append(rollbackErrors, fmt.Sprintf("reload prior service state: %v", err))
					managerReady = false
				}
			}
			if status.Installed && filesRestored && managerReady {
				if pmServiceOS == "darwin" {
					if _, err := pmServiceExec(ctx, "launchctl", "bootstrap", domain, unit); err != nil {
						rollbackErrors = append(rollbackErrors, fmt.Sprintf("restart prior service: %v", err))
					}
				} else if _, err := pmServiceExec(ctx, "systemctl", "--user", "start", id+".service"); err != nil {
					rollbackErrors = append(rollbackErrors, fmt.Sprintf("restart prior service: %v", err))
				}
			}
			if len(rollbackErrors) != 0 {
				return fmt.Errorf("PM service update failed: %w; rollback also failed: %s", commitErr, strings.Join(rollbackErrors, "; "))
			}
			if status.Installed {
				return fmt.Errorf("PM service update failed: %w; the prior service was restored", commitErr)
			}
			return fmt.Errorf("PM service install failed: %w; incomplete installation state was removed", commitErr)
		}
		if e = pmServiceCommitFile(settingsStage, settingsPath); e != nil {
			return nil, rollbackFilesAndService(e)
		}
		settingsCommitted = true
		if e = pmServiceCommitFile(unitStage, unit); e != nil {
			return nil, rollbackFilesAndService(e)
		}
		unitCommitted = true
		if pmServiceOS == "darwin" {
			// The old job was booted out before replacing its definition. This is
			// the freshness boundary for claims written by the new process.
			installStartedAt = time.Now()
			activationAttempted = true
			if e = command("launchctl", "bootstrap", domain, unit); e != nil {
				return nil, rollbackFilesAndService(e)
			}
		} else {
			if e = command("systemctl", "--user", "daemon-reload"); e != nil {
				return nil, rollbackFilesAndService(e)
			}
			linuxServiceEnableAttempted = true
			if e = command("systemctl", "--user", "enable", id+".service"); e != nil {
				return nil, rollbackFilesAndService(e)
			}
			// Any prior instance was explicitly stopped before its files changed;
			// only claims after this point belong to the replacement process.
			installStartedAt = time.Now()
			activationAttempted = true
			if e = command("systemctl", "--user", "start", id+".service"); e != nil {
				return nil, rollbackFilesAndService(e)
			}
		}
		status.Installed = true
	case "uninstall":
		if status.Installed {
			if pmServiceOS == "darwin" {
				out, e := pmServiceExec(ctx, "launchctl", "bootout", domain+"/"+id)
				if e != nil && !strings.Contains(string(out), "Could not find service") && !strings.Contains(string(out), "No such process") {
					return nil, fmt.Errorf("launchctl bootout failed: %w", e)
				}

			} else {
				if e := command("systemctl", "--user", "disable", "--now", id+".service"); e != nil {
					return nil, e
				}
			}
			if e := os.Remove(unit); e != nil && !os.IsNotExist(e) {
				return nil, e
			}
			if pmServiceOS == "linux" {
				if e := command("systemctl", "--user", "daemon-reload"); e != nil {
					return nil, e
				}
			}
		}
		if e := os.RemoveAll(state); e != nil {
			return nil, e
		}
		status.Installed = false
		if !keepRegistration {
			// Local removal is complete before any authentication or network work.
			resetCfg := cfg
			resetCfg.As = agent
			resetCfg, resetErr := pmServiceResolveAuth(a, ctx, resetCfg)
			if resetErr == nil {
				_, resetErr = a.invokeRawJSON(ctx, resetCfg, "pm disconnect", "POST", "/pm/disconnect", nil)
			}
			if resetErr != nil {
				retry := []string{"anx", "--base-url", cfg.BaseURL, "--config-dir", dir, "--as", agent, "pm", "disconnect"}
				warnings = append(warnings, output.Warning{Code: "pm_disconnect_failed", Message: "Local PM service removed, but workspace onboarding could not be reset. PM features may remain visible and asks may still queue. Retry: " + pmRetryCommand(retry), Details: map[string]any{"retry_argv": retry}})
			}
		}
	}
	refreshStatus := func() {
		if !status.Installed {
			status.Running = false
			status.LastClaimAt = ""
			return
		}
		if pmServiceOS == "linux" {
			out, e := pmServiceExec(ctx, "systemctl", "--user", "is-active", id+".service")
			status.Running = e == nil && strings.TrimSpace(string(out)) == "active"
		} else {
			out, e := pmServiceExec(ctx, "launchctl", "print", domain+"/"+id)
			status.Running = e == nil && strings.Contains(string(out), "state = running")
		}
		if b, e := os.ReadFile(filepath.Join(state, "last-claim")); e == nil {
			status.LastClaimAt = strings.TrimSpace(string(b))
		}
	}
	refreshStatus()
	if verb == "install" && wait {
		if err := pmServiceWaitForConnection(a, ctx, cfg, waitTimeout, state, installStartedAt); err != nil {
			return nil, err
		}
		if agent != "pm" {
			if err := stopPriorDefaultPMService(ctx, pmServiceOS, domain, stateRoot, unitDir, unitSuffix, cfg.BaseURL, dir); err != nil {
				warnings = append(warnings, priorDefaultPMCleanupWarning(err))
			}
		}
		// --wait has confirmed the first accepted connection. Re-read both
		// process state and the local claim timestamp so the returned status is
		// about that post-claim state, not the instant just after launch.
		refreshStatus()
		return &commandResult{Data: status, Warnings: warnings, Text: fmt.Sprintf("PM connected\nWorkspace: %s\nProfile: %s\nLast accepted claim: %s\nService manager reports running=%t\nLogs: %s", cfg.BaseURL, status.Agent, firstNonEmpty(status.LastClaimAt, "unknown"), status.Running, state)}, nil
	}
	if verb == "install" && agent != "pm" {
		// Without --wait, the manager may have accepted the launch request while
		// the replacement has not claimed work yet. Keep the old profile until
		// the replacement has proved its connection.
		legacyUnit := priorDefaultPMServiceUnit(unitDir, unitSuffix, cfg.BaseURL, dir)
		if claimConfirmedSince(status.LastClaimAt, installStartedAt) {
			if err := stopPriorDefaultPMService(ctx, pmServiceOS, domain, stateRoot, unitDir, unitSuffix, cfg.BaseURL, dir); err != nil {
				warnings = append(warnings, priorDefaultPMCleanupWarning(err))
			}
		} else if _, err := os.Stat(legacyUnit); err == nil {
			retry := []string{"anx", "--base-url", cfg.BaseURL, "--config-dir", dir, "--as", agent, "pm", "install", "--wait"}
			warnings = append(warnings, output.Warning{Code: "prior_pm_service_migration_pending", Message: "The selected PM service is installed, but its first accepted connection is not confirmed. The prior default-profile service remains active; rerun `" + pmRetryCommand(retry) + "` to confirm the replacement and retire the prior service.", Details: map[string]any{"retry_argv": retry}})
		} else if !os.IsNotExist(err) {
			warnings = append(warnings, priorDefaultPMCleanupWarning(err))
		}
	}
	message := fmt.Sprintf("PM %s: installed=%t running=%t\nWorkspace: %s\nProfile: %s\nLast accepted claim: %s\nLogs: %s", verb, status.Installed, status.Running, status.Workspace, status.Agent, firstNonEmpty(status.LastClaimAt, "none"), state)
	if verb == "install" && !wait {
		connection := "starting, not yet claimed"
		if claimConfirmedSince(status.LastClaimAt, installStartedAt) {
			connection = "connection accepted during this install"
		}
		message = fmt.Sprintf("PM installed; %s\nWorkspace: %s\nProfile: %s\nService manager reports running=%t\nLogs: %s", connection, status.Workspace, status.Agent, status.Running, state)
	}
	return &commandResult{Data: status, Warnings: warnings, Text: message}, nil
}

func priorDefaultPMCleanupWarning(err error) output.Warning {
	return output.Warning{Code: "prior_pm_service_cleanup_failed", Message: "The selected PM service was installed, but the prior default-profile service could not be retired: " + err.Error()}
}

func writePMPrivateFile(path string, data []byte) error {
	temp, err := stagePMPrivateFile(path, data)
	if err != nil {
		return err
	}
	defer os.Remove(temp)
	return os.Rename(temp, path)
}

func stagePMPrivateFile(path string, data []byte) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(path), ".pm-*")
	if err != nil {
		return "", err
	}
	name := f.Name()
	cleanup := func(err error) (string, error) {
		_ = f.Close()
		_ = os.Remove(name)
		return "", err
	}
	if _, err = f.Write(data); err != nil {
		return cleanup(err)
	}
	if err = f.Chmod(0600); err != nil {
		return cleanup(err)
	}
	if err = f.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

func pmServiceID(workspace, configDir, agent string) string {
	sum := sha256.Sum256([]byte(workspace + "\x00" + configDir + "\x00" + agent))
	return "io.agent-nexus.pm." + hex.EncodeToString(sum[:8])
}

func priorDefaultPMServiceUnit(unitDir, unitSuffix, workspace, configDir string) string {
	legacyID := pmServiceID(workspace, configDir, "pm")
	return filepath.Join(unitDir, legacyID+unitSuffix)
}

func claimConfirmedSince(lastClaim string, since time.Time) bool {
	claimedAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(lastClaim))
	return err == nil && !claimedAt.Before(since)
}

// stopPriorDefaultPMService migrates an installed default-profile service when
// an explicitly selected non-pm profile is installed. The service id includes
// the profile, so without this one-time cleanup the former `pm` process would
// keep running and claiming work alongside the new service. Its state directory
// is kept so the old logs and last claim remain available.
func stopPriorDefaultPMService(ctx context.Context, platform, domain, stateRoot, unitDir, unitSuffix, workspace, configDir string) error {
	legacyID := pmServiceID(workspace, configDir, "pm")
	legacyUnit := priorDefaultPMServiceUnit(unitDir, unitSuffix, workspace, configDir)
	if _, err := os.Stat(legacyUnit); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	legacyState := filepath.Join(stateRoot, "anx", "pm", legacyID)
	settingsBytes, err := os.ReadFile(filepath.Join(legacyState, "settings.json"))
	if err != nil {
		return fmt.Errorf("found the prior default PM service but could not verify its settings; inspect it and run `anx --as pm pm uninstall --keep-registration` before installing another identity: %w", err)
	}
	var settings pmServiceSettings
	if err := json.Unmarshal(settingsBytes, &settings); err != nil || settings.Agent != "pm" || settings.Workspace != workspace || settings.ConfigDir != configDir {
		return fmt.Errorf("found a prior PM service whose profile could not be verified; inspect it and run `anx --as pm pm uninstall --keep-registration` before installing another identity")
	}
	if platform == "darwin" {
		out, err := pmServiceExec(ctx, "launchctl", "bootout", domain+"/"+legacyID)
		if err != nil && !strings.Contains(string(out), "Could not find service") && !strings.Contains(string(out), "No such process") {
			return fmt.Errorf("could not stop prior default PM service: %w", err)
		}
	} else {
		if _, err := pmServiceExec(ctx, "systemctl", "--user", "disable", "--now", legacyID+".service"); err != nil {
			return fmt.Errorf("could not stop prior default PM service: %w", err)
		}
	}
	if err := os.Remove(legacyUnit); err != nil && !os.IsNotExist(err) {
		return err
	}
	if platform == "linux" {
		if _, err := pmServiceExec(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
			return fmt.Errorf("could not reload the user service manager after retiring the prior PM service: %w", err)
		}
	}
	return nil
}

func pmServiceDefinition(platform, id string, args []string, state, home, path string) string {
	if platform == "darwin" {
		escape := func(s string) string { var b strings.Builder; _ = xml.EscapeText(&b, []byte(s)); return b.String() }
		var a strings.Builder
		for _, arg := range args {
			fmt.Fprintf(&a, "<string>%s</string>", escape(arg))
		}
		return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>Label</key><string>%s</string><key>ProgramArguments</key><array>%s</array><key>RunAtLoad</key><true/><key>KeepAlive</key><true/><key>WorkingDirectory</key><string>%s</string><key>EnvironmentVariables</key><dict><key>HOME</key><string>%s</string><key>PATH</key><string>%s</string></dict><key>StandardOutPath</key><string>%s</string><key>StandardErrorPath</key><string>%s</string></dict></plist>`, escape(id), a.String(), escape(state), escape(home), escape(path), escape(filepath.Join(state, "stdout.log")), escape(filepath.Join(state, "stderr.log")))
	}
	quote := func(s string) string {
		s = strings.ReplaceAll(s, "%", "%%")
		return strconv.Quote(s)
	}
	var quoted []string
	for _, arg := range args {
		quoted = append(quoted, quote(strings.ReplaceAll(arg, "$", "$$")))
	}
	return fmt.Sprintf("[Unit]\nDescription=Agent Nexus local PM\n[Service]\nExecStart=%s\nWorkingDirectory=%s\nEnvironment=%s\nEnvironment=%s\nRestart=always\nRestartSec=5\nStandardOutput=append:%s\nStandardError=append:%s\n[Install]\nWantedBy=default.target\n", strings.Join(quoted, " "), strings.ReplaceAll(state, "%", "%%"), quote("HOME="+home), quote("PATH="+path), strings.ReplaceAll(filepath.Join(state, "stdout.log"), "%", "%%"), strings.ReplaceAll(filepath.Join(state, "stderr.log"), "%", "%%"))
}

func pmRetryCommand(argv []string) string {
	quoted := make([]string, len(argv))
	for i, value := range argv {
		quoted[i] = shellSingleQuote(value)
	}
	return strings.Join(quoted, " ")
}
