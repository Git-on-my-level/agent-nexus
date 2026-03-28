package app

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"organization-autorunner-cli/internal/errnorm"
)

var (
	bridgeReadFile            = os.ReadFile
	bridgeOpenFile            = os.OpenFile
	bridgeStartManagedProcess = defaultBridgeStartManagedProcess
	bridgeStopManagedProcess  = defaultBridgeStopManagedProcess
	bridgeProcessAlive        = defaultBridgeProcessAlive
	bridgeProcessCommandLine  = defaultBridgeProcessCommandLine
)

type bridgeManagedRuntime struct {
	Kind             string   `json:"kind"`
	ConfigPath       string   `json:"config_path"`
	ManagerDir       string   `json:"manager_dir"`
	ProcessStatePath string   `json:"process_state_path"`
	LogPath          string   `json:"log_path"`
	BridgeBinary     string   `json:"bridge_binary"`
	Command          []string `json:"command"`
	PID              int      `json:"pid"`
	PGID             int      `json:"pgid"`
	StartedAt        string   `json:"started_at"`
	StoppedAt        string   `json:"stopped_at"`
	LastSignal       string   `json:"last_signal"`
}

type bridgeManagedConfig struct {
	RuntimeKind      string
	RunCommand       string
	ConfigPath       string
	DisplayName      string
	ManagerDir       string
	ProcessStatePath string
	LogPath          string
}

func init() {
	localHelperTopics = append(localHelperTopics,
		localHelperTopic{
			Path:        "bridge start",
			Summary:     "Start a managed bridge or router daemon for one config file.",
			JSONShape:   "`kind`, `config_path`, `pid`, `log_path`, `process_state_path`, `command`",
			Composition: "Pure local helper. Resolves the installed `oar-agent-bridge` binary, infers the config role, launches the daemon in the background, and records pid/log metadata in a per-config manager directory.",
			Examples: []string{
				"oar bridge start --config ./router.toml",
				"oar bridge start --config ./agent.toml",
			},
			Flags: []localHelperFlag{
				{Name: "--config <path>", Description: "Bridge config to start. The role is inferred from `[router]` vs `[agent]`."},
				{Name: "--install-dir <dir>", Description: "Root directory for the managed bridge virtualenv."},
				{Name: "--bin-dir <dir>", Description: "Directory where the managed `oar-agent-bridge` wrapper should exist."},
			},
		},
		localHelperTopic{
			Path:        "bridge stop",
			Summary:     "Stop a managed bridge or router daemon for one config file.",
			JSONShape:   "`kind`, `config_path`, `pid`, `stopped_at`, `last_signal`",
			Composition: "Pure local helper. Reads the per-config manager state, sends SIGTERM, and records the stopped timestamp once the daemon exits.",
			Examples: []string{
				"oar bridge stop --config ./router.toml",
				"oar bridge stop --config ./agent.toml --force",
			},
			Flags: []localHelperFlag{
				{Name: "--config <path>", Description: "Managed config to stop."},
				{Name: "--force", Description: "Escalate to SIGKILL if SIGTERM does not stop the daemon before the timeout."},
				{Name: "--timeout-seconds <n>", Description: "How long to wait after SIGTERM before failing or force-killing."},
			},
		},
		localHelperTopic{
			Path:        "bridge restart",
			Summary:     "Restart a managed bridge or router daemon for one config file.",
			JSONShape:   "`kind`, `config_path`, `pid`, `log_path`, `process_state_path`",
			Composition: "Pure local helper. Stops the existing managed process if one is present, then launches a fresh daemon and updates the manager state.",
			Examples: []string{
				"oar bridge restart --config ./router.toml",
				"oar bridge restart --config ./agent.toml",
			},
			Flags: []localHelperFlag{
				{Name: "--config <path>", Description: "Managed config to restart."},
				{Name: "--force", Description: "Force-kill during the stop phase if needed."},
			},
		},
		localHelperTopic{
			Path:        "bridge status",
			Summary:     "Inspect managed process state for a bridge or router config.",
			JSONShape:   "`kind`, `managed`, `running`, `pid`, `log_path`, `process_state_path`, `registration`",
			Composition: "Pure local helper plus optional bridge CLI calls. Reports the background process state, log path, and for agent configs also includes registration readiness when available.",
			Examples: []string{
				"oar bridge status --config ./router.toml",
				"oar bridge status --config ./agent.toml",
			},
			Flags: []localHelperFlag{
				{Name: "--config <path>", Description: "Managed config to inspect."},
				{Name: "--install-dir <dir>", Description: "Root directory for the managed bridge virtualenv."},
				{Name: "--bin-dir <dir>", Description: "Directory where the managed `oar-agent-bridge` wrapper should exist."},
			},
		},
		localHelperTopic{
			Path:        "bridge logs",
			Summary:     "Read recent log lines for a managed bridge or router config.",
			JSONShape:   "`kind`, `config_path`, `log_path`, `lines`, `content`",
			Composition: "Pure local helper. Reads the per-config managed log file and returns the last N lines without requiring direct shell access.",
			Examples: []string{
				"oar bridge logs --config ./router.toml",
				"oar bridge logs --config ./agent.toml --lines 200",
			},
			Flags: []localHelperFlag{
				{Name: "--config <path>", Description: "Managed config whose log should be tailed."},
				{Name: "--lines <n>", Description: "How many recent lines to return. Default is 80."},
			},
		},
	)
}

func (a *App) runBridgeStart(ctx context.Context, args []string) (*commandResult, error) {
	if runtime.GOOS == "windows" {
		return nil, errnorm.Usage("unsupported_platform", "`oar bridge start` currently supports macOS and Linux only")
	}
	fs := newSilentFlagSet("bridge start")
	var configFlag trackedString
	var installDirFlag trackedString
	var binDirFlag trackedString
	fs.Var(&configFlag, "config", "Bridge config to start")
	fs.Var(&installDirFlag, "install-dir", "Root directory for the managed bridge virtualenv")
	fs.Var(&binDirFlag, "bin-dir", "Directory where the managed oar-agent-bridge wrapper should exist")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `oar bridge start`")
	}
	configPath := strings.TrimSpace(configFlag.value)
	if configPath == "" {
		return nil, errnorm.Usage("invalid_request", "--config is required")
	}
	managedConfig, err := loadBridgeManagedConfig(configPath)
	if err != nil {
		return nil, err
	}
	home, err := a.bridgeHome()
	if err != nil {
		return nil, err
	}
	bridgeBinary, err := resolveBridgeBinary(home, strings.TrimSpace(installDirFlag.value), strings.TrimSpace(binDirFlag.value))
	if err != nil {
		return nil, err
	}
	if existing, ok := loadManagedRuntimeState(managedConfig.ProcessStatePath); ok {
		if running, _ := bridgeManagedRuntimeRunning(existing); running {
			return nil, errnorm.WithDetails(
				errnorm.Local("bridge_already_running", "bridge runtime is already running for this config"),
				map[string]any{
					"config_path": managedConfig.ConfigPath,
					"kind":        managedConfig.RuntimeKind,
					"pid":         existing.PID,
					"log_path":    existing.LogPath,
				},
			)
		}
	}
	runtimeState, err := bridgeStartManagedProcess(managedConfig, bridgeBinary)
	if err != nil {
		return nil, err
	}
	if err := writeManagedRuntimeState(runtimeState); err != nil {
		_, _ = bridgeStopManagedProcess(runtimeState, 2*time.Second, true)
		return nil, err
	}
	lines := []string{
		"Bridge runtime started.",
		"Kind: " + runtimeState.Kind,
		"Config: " + runtimeState.ConfigPath,
		"PID: " + strconv.Itoa(runtimeState.PID),
		"Log: " + runtimeState.LogPath,
		"State: " + runtimeState.ProcessStatePath,
		"Next step: oar bridge status --config " + shellSingleQuote(runtimeState.ConfigPath),
	}
	return &commandResult{
		Text: strings.Join(lines, "\n"),
		Data: map[string]any{
			"kind":               runtimeState.Kind,
			"config_path":        runtimeState.ConfigPath,
			"pid":                runtimeState.PID,
			"log_path":           runtimeState.LogPath,
			"process_state_path": runtimeState.ProcessStatePath,
			"command":            runtimeState.Command,
		},
	}, nil
}

func (a *App) runBridgeStop(args []string) (*commandResult, error) {
	if runtime.GOOS == "windows" {
		return nil, errnorm.Usage("unsupported_platform", "`oar bridge stop` currently supports macOS and Linux only")
	}
	fs := newSilentFlagSet("bridge stop")
	var configFlag trackedString
	var timeoutFlag trackedString
	var force trackedBool
	fs.Var(&configFlag, "config", "Managed bridge config to stop")
	fs.Var(&timeoutFlag, "timeout-seconds", "How long to wait after SIGTERM before failing")
	fs.Var(&force, "force", "Escalate to SIGKILL if SIGTERM does not stop the daemon")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `oar bridge stop`")
	}
	configPath := strings.TrimSpace(configFlag.value)
	if configPath == "" {
		return nil, errnorm.Usage("invalid_request", "--config is required")
	}
	managedConfig, err := loadBridgeManagedConfig(configPath)
	if err != nil {
		return nil, err
	}
	runtimeState, ok := loadManagedRuntimeState(managedConfig.ProcessStatePath)
	if !ok {
		return nil, errnorm.WithDetails(
			errnorm.Local("bridge_not_managed", "bridge runtime is not currently managed for this config"),
			map[string]any{"config_path": managedConfig.ConfigPath, "kind": managedConfig.RuntimeKind},
		)
	}
	timeout := 10 * time.Second
	if raw := strings.TrimSpace(timeoutFlag.value); raw != "" {
		seconds, convErr := strconv.Atoi(raw)
		if convErr != nil || seconds <= 0 {
			return nil, errnorm.Usage("invalid_request", "--timeout-seconds must be a positive integer")
		}
		timeout = time.Duration(seconds) * time.Second
	}
	if running, reason := bridgeManagedRuntimeRunning(runtimeState); running {
		runtimeState, err = bridgeStopManagedProcess(runtimeState, timeout, force.set && force.value)
		if err != nil {
			return nil, err
		}
	} else {
		runtimeState.StoppedAt = time.Now().UTC().Format(time.RFC3339)
		runtimeState.LastSignal = reason
	}
	if err := writeManagedRuntimeState(runtimeState); err != nil {
		return nil, err
	}
	lines := []string{
		"Bridge runtime stopped.",
		"Kind: " + runtimeState.Kind,
		"Config: " + runtimeState.ConfigPath,
		"Last PID: " + strconv.Itoa(runtimeState.PID),
		"Stopped at: " + runtimeState.StoppedAt,
	}
	return &commandResult{
		Text: strings.Join(lines, "\n"),
		Data: map[string]any{
			"kind":        runtimeState.Kind,
			"config_path": runtimeState.ConfigPath,
			"pid":         runtimeState.PID,
			"stopped_at":  runtimeState.StoppedAt,
			"last_signal": runtimeState.LastSignal,
		},
	}, nil
}

func (a *App) runBridgeRestart(ctx context.Context, args []string) (*commandResult, error) {
	if runtime.GOOS == "windows" {
		return nil, errnorm.Usage("unsupported_platform", "`oar bridge restart` currently supports macOS and Linux only")
	}
	fs := newSilentFlagSet("bridge restart")
	var configFlag trackedString
	var installDirFlag trackedString
	var binDirFlag trackedString
	var timeoutFlag trackedString
	var force trackedBool
	fs.Var(&configFlag, "config", "Managed bridge config to restart")
	fs.Var(&installDirFlag, "install-dir", "Root directory for the managed bridge virtualenv")
	fs.Var(&binDirFlag, "bin-dir", "Directory where the managed oar-agent-bridge wrapper should exist")
	fs.Var(&timeoutFlag, "timeout-seconds", "How long to wait after SIGTERM before failing")
	fs.Var(&force, "force", "Escalate to SIGKILL if SIGTERM does not stop the daemon")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `oar bridge restart`")
	}
	configPath := strings.TrimSpace(configFlag.value)
	if configPath == "" {
		return nil, errnorm.Usage("invalid_request", "--config is required")
	}
	stopArgs := []string{"--config", configPath}
	if strings.TrimSpace(timeoutFlag.value) != "" {
		stopArgs = append(stopArgs, "--timeout-seconds", strings.TrimSpace(timeoutFlag.value))
	}
	if force.set && force.value {
		stopArgs = append(stopArgs, "--force")
	}
	_, stopErr := a.runBridgeStop(stopArgs)
	if stopErr != nil {
		norm := errnorm.Normalize(stopErr)
		if norm.Code != "bridge_not_managed" {
			return nil, stopErr
		}
	}
	startArgs := []string{"--config", configPath}
	if strings.TrimSpace(installDirFlag.value) != "" {
		startArgs = append(startArgs, "--install-dir", strings.TrimSpace(installDirFlag.value))
	}
	if strings.TrimSpace(binDirFlag.value) != "" {
		startArgs = append(startArgs, "--bin-dir", strings.TrimSpace(binDirFlag.value))
	}
	result, err := a.runBridgeStart(ctx, startArgs)
	if err != nil {
		return nil, err
	}
	result.Text = "Bridge runtime restarted.\n" + result.Text
	return result, nil
}

func (a *App) runBridgeStatus(ctx context.Context, args []string) (*commandResult, error) {
	fs := newSilentFlagSet("bridge status")
	var configFlag trackedString
	var installDirFlag trackedString
	var binDirFlag trackedString
	fs.Var(&configFlag, "config", "Managed bridge config to inspect")
	fs.Var(&installDirFlag, "install-dir", "Root directory for the managed bridge virtualenv")
	fs.Var(&binDirFlag, "bin-dir", "Directory where the managed oar-agent-bridge wrapper should exist")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `oar bridge status`")
	}
	configPath := strings.TrimSpace(configFlag.value)
	if configPath == "" {
		return nil, errnorm.Usage("invalid_request", "--config is required")
	}
	managedConfig, err := loadBridgeManagedConfig(configPath)
	if err != nil {
		return nil, err
	}
	home, err := a.bridgeHome()
	if err != nil {
		return nil, err
	}
	bridgeBinary, _ := resolveBridgeBinary(home, strings.TrimSpace(installDirFlag.value), strings.TrimSpace(binDirFlag.value))

	registrationData := map[string]any{}
	if managedConfig.RuntimeKind == "agent" && bridgeBinary != "" {
		statusOut, statusErr := runBridgeExternalOutput(ctx, bridgeBinary, "registration", "status", "--config", managedConfig.ConfigPath)
		if statusErr == nil {
			_ = json.Unmarshal([]byte(statusOut), &registrationData)
		}
	}

	runtimeState, ok := loadManagedRuntimeState(managedConfig.ProcessStatePath)
	if !ok {
		lines := []string{
			"Bridge runtime status",
			"Kind: " + managedConfig.RuntimeKind,
			"Config: " + managedConfig.ConfigPath,
			"Process: not managed",
			"Log: " + managedConfig.LogPath,
			"State: " + managedConfig.ProcessStatePath,
			"Start with: oar bridge start --config " + shellSingleQuote(managedConfig.ConfigPath),
		}
		return &commandResult{
			Text: strings.Join(lines, "\n"),
			Data: map[string]any{
				"kind":               managedConfig.RuntimeKind,
				"config_path":        managedConfig.ConfigPath,
				"managed":            false,
				"running":            false,
				"log_path":           managedConfig.LogPath,
				"process_state_path": managedConfig.ProcessStatePath,
				"registration":       registrationData,
			},
		}, nil
	}
	running, mismatchReason := bridgeManagedRuntimeRunning(runtimeState)
	processState := "exited"
	if running {
		processState = "running"
	} else if mismatchReason == "pid_reused" {
		processState = "stale"
	} else if runtimeState.StoppedAt != "" {
		processState = "stopped"
	}
	lines := []string{
		"Bridge runtime status",
		"Kind: " + runtimeState.Kind,
		"Config: " + runtimeState.ConfigPath,
		"Process: " + processState,
		"PID: " + strconv.Itoa(runtimeState.PID),
		"Log: " + runtimeState.LogPath,
		"State: " + runtimeState.ProcessStatePath,
	}
	if runtimeState.StartedAt != "" {
		lines = append(lines, "Started at: "+runtimeState.StartedAt)
	}
	if runtimeState.StoppedAt != "" {
		lines = append(lines, "Stopped at: "+runtimeState.StoppedAt)
	}
	if managedConfig.RuntimeKind == "agent" {
		if wakeable, _ := registrationData["wakeable"].(bool); wakeable {
			lines = append(lines, "Registration: wakeable")
		} else if len(registrationData) > 0 {
			lines = append(lines, "Registration: not wakeable yet")
		} else if bridgeBinary == "" {
			lines = append(lines, "Registration: unavailable because oar-agent-bridge is not installed or not on PATH")
		}
	}
	return &commandResult{
		Text: strings.Join(lines, "\n"),
		Data: map[string]any{
			"kind":               runtimeState.Kind,
			"config_path":        runtimeState.ConfigPath,
			"managed":            true,
			"running":            running,
			"pid":                runtimeState.PID,
			"log_path":           runtimeState.LogPath,
			"process_state_path": runtimeState.ProcessStatePath,
			"registration":       registrationData,
		},
	}, nil
}

func (a *App) runBridgeLogs(args []string) (*commandResult, error) {
	fs := newSilentFlagSet("bridge logs")
	var configFlag trackedString
	var linesFlag trackedString
	fs.Var(&configFlag, "config", "Managed bridge config whose logs should be read")
	fs.Var(&linesFlag, "lines", "How many recent lines to return")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `oar bridge logs`")
	}
	configPath := strings.TrimSpace(configFlag.value)
	if configPath == "" {
		return nil, errnorm.Usage("invalid_request", "--config is required")
	}
	managedConfig, err := loadBridgeManagedConfig(configPath)
	if err != nil {
		return nil, err
	}
	lineLimit := 80
	if raw := strings.TrimSpace(linesFlag.value); raw != "" {
		value, convErr := strconv.Atoi(raw)
		if convErr != nil || value <= 0 {
			return nil, errnorm.Usage("invalid_request", "--lines must be a positive integer")
		}
		lineLimit = value
	}
	logPath := managedConfig.LogPath
	if runtimeState, ok := loadManagedRuntimeState(managedConfig.ProcessStatePath); ok && strings.TrimSpace(runtimeState.LogPath) != "" {
		logPath = runtimeState.LogPath
	}
	content, err := bridgeReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			lines := []string{
				"Bridge logs",
				"Kind: " + managedConfig.RuntimeKind,
				"Config: " + managedConfig.ConfigPath,
				"Log: " + logPath,
				"No managed log file exists yet. Start the runtime with: oar bridge start --config " + shellSingleQuote(managedConfig.ConfigPath),
			}
			return &commandResult{
				Text: strings.Join(lines, "\n"),
				Data: map[string]any{
					"kind":        managedConfig.RuntimeKind,
					"config_path": managedConfig.ConfigPath,
					"log_path":    logPath,
					"lines":       lineLimit,
					"content":     "",
				},
			}, nil
		}
		return nil, errnorm.Wrap(errnorm.KindLocal, "bridge_log_read_failed", "failed to read bridge log file", err)
	}
	text := tailLines(string(content), lineLimit)
	lines := []string{
		"Bridge logs",
		"Kind: " + managedConfig.RuntimeKind,
		"Config: " + managedConfig.ConfigPath,
		"Log: " + logPath,
	}
	if text != "" {
		lines = append(lines, "", text)
	}
	return &commandResult{
		Text: strings.Join(lines, "\n"),
		Data: map[string]any{
			"kind":        managedConfig.RuntimeKind,
			"config_path": managedConfig.ConfigPath,
			"log_path":    logPath,
			"lines":       lineLimit,
			"content":     text,
		},
	}, nil
}

func loadBridgeManagedConfig(configPath string) (bridgeManagedConfig, error) {
	absPath, err := filepath.Abs(configPath)
	if err != nil {
		return bridgeManagedConfig{}, errnorm.Wrap(errnorm.KindLocal, "bridge_config_resolve_failed", "failed to resolve bridge config path", err)
	}
	content, err := bridgeReadFile(absPath)
	if err != nil {
		return bridgeManagedConfig{}, errnorm.Wrap(errnorm.KindLocal, "bridge_config_read_failed", "failed to read bridge config", err)
	}
	runtimeKind, runCommand, displayName, err := inferBridgeRuntimeKind(string(content), absPath)
	if err != nil {
		return bridgeManagedConfig{}, err
	}
	managerDir := bridgeManagerDir(absPath)
	return bridgeManagedConfig{
		RuntimeKind:      runtimeKind,
		RunCommand:       runCommand,
		ConfigPath:       absPath,
		DisplayName:      displayName,
		ManagerDir:       managerDir,
		ProcessStatePath: filepath.Join(managerDir, "process.json"),
		LogPath:          filepath.Join(managerDir, "current.log"),
	}, nil
}

func inferBridgeRuntimeKind(content string, configPath string) (runtimeKind string, runCommand string, displayName string, err error) {
	hasRouter := bridgeConfigHasSection(content, "router")
	hasAgent := bridgeConfigHasSection(content, "agent")
	switch {
	case hasRouter && hasAgent:
		return "", "", "", errnorm.Usage("invalid_request", "bridge config must contain either [router] or [agent], not both")
	case hasRouter:
		return "router", "router", filepath.Base(configPath), nil
	case hasAgent:
		return "agent", "bridge", filepath.Base(configPath), nil
	default:
		return "", "", "", errnorm.Usage("invalid_request", "bridge config must contain either [router] or [agent]")
	}
}

func bridgeConfigHasSection(content string, section string) bool {
	target := "[" + section + "]"
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == target {
			return true
		}
	}
	return false
}

func bridgeManagerDir(configPath string) string {
	base := strings.TrimSuffix(filepath.Base(configPath), filepath.Ext(configPath))
	base = sanitizeBridgeManagerName(base)
	if base == "" {
		base = "bridge"
	}
	return filepath.Join(filepath.Dir(configPath), ".oar-bridge", base+"-"+shortBridgeHash(configPath))
}

func sanitizeBridgeManagerName(value string) string {
	replacer := strings.NewReplacer(" ", "-", "/", "-", "\\", "-", ":", "-", "@", "-", ".", "-")
	value = replacer.Replace(strings.ToLower(strings.TrimSpace(value)))
	value = strings.Trim(value, "-")
	return value
}

func shortBridgeHash(value string) string {
	hash := value
	if len(hash) == 0 {
		hash = defaultBridgeInstallRef()
	}
	sum := md5.Sum([]byte(hash))
	return strings.ToLower(shortHex(fmt.Sprintf("%x", sum), 10))
}

func shortHex(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func loadManagedRuntimeState(path string) (bridgeManagedRuntime, bool) {
	content, err := bridgeReadFile(path)
	if err != nil {
		return bridgeManagedRuntime{}, false
	}
	var runtimeState bridgeManagedRuntime
	if err := json.Unmarshal(content, &runtimeState); err != nil {
		return bridgeManagedRuntime{}, false
	}
	return runtimeState, true
}

func writeManagedRuntimeState(runtimeState bridgeManagedRuntime) error {
	if err := bridgeMkdirAll(filepath.Dir(runtimeState.ProcessStatePath), 0o755); err != nil {
		return errnorm.Wrap(errnorm.KindLocal, "bridge_manager_dir_failed", "failed to create bridge manager directory", err)
	}
	content, err := json.MarshalIndent(runtimeState, "", "  ")
	if err != nil {
		return errnorm.Wrap(errnorm.KindLocal, "bridge_manager_state_failed", "failed to encode bridge manager state", err)
	}
	if err := bridgeWriteFile(runtimeState.ProcessStatePath, append(content, '\n'), 0o600); err != nil {
		return errnorm.Wrap(errnorm.KindLocal, "bridge_manager_state_failed", "failed to write bridge manager state", err)
	}
	return nil
}

func defaultBridgeStartManagedProcess(managedConfig bridgeManagedConfig, bridgeBinary string) (bridgeManagedRuntime, error) {
	if err := bridgeMkdirAll(managedConfig.ManagerDir, 0o755); err != nil {
		return bridgeManagedRuntime{}, errnorm.Wrap(errnorm.KindLocal, "bridge_manager_dir_failed", "failed to create bridge manager directory", err)
	}
	logHandle, err := bridgeOpenFile(managedConfig.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return bridgeManagedRuntime{}, errnorm.Wrap(errnorm.KindLocal, "bridge_log_open_failed", "failed to open bridge log file", err)
	}
	defer logHandle.Close()

	cmd := exec.Command(bridgeBinary, managedConfig.RunCommand, "run", "--config", managedConfig.ConfigPath)
	cmd.Stdout = logHandle
	cmd.Stderr = logHandle
	cmd.Stdin = nil
	cmd.Dir = filepath.Dir(managedConfig.ConfigPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return bridgeManagedRuntime{}, errnorm.Wrap(errnorm.KindLocal, "bridge_start_failed", "failed to start managed bridge runtime", err)
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	runtimeState := bridgeManagedRuntime{
		Kind:             managedConfig.RuntimeKind,
		ConfigPath:       managedConfig.ConfigPath,
		ManagerDir:       managedConfig.ManagerDir,
		ProcessStatePath: managedConfig.ProcessStatePath,
		LogPath:          managedConfig.LogPath,
		BridgeBinary:     bridgeBinary,
		Command:          []string{bridgeBinary, managedConfig.RunCommand, "run", "--config", managedConfig.ConfigPath},
		PID:              pid,
		PGID:             pid,
		StartedAt:        time.Now().UTC().Format(time.RFC3339),
	}
	time.Sleep(150 * time.Millisecond)
	if !bridgeProcessAlive(pid) {
		tailText := ""
		if content, readErr := bridgeReadFile(managedConfig.LogPath); readErr == nil {
			tailText = tailLines(string(content), 20)
		}
		details := map[string]any{"config_path": managedConfig.ConfigPath, "log_path": managedConfig.LogPath}
		if strings.TrimSpace(tailText) != "" {
			details["log_tail"] = tailText
		}
		return bridgeManagedRuntime{}, errnorm.WithDetails(
			errnorm.Local("bridge_start_failed", "bridge runtime exited immediately; inspect `oar bridge logs --config ...`"),
			details,
		)
	}
	return runtimeState, nil
}

func defaultBridgeStopManagedProcess(runtimeState bridgeManagedRuntime, timeout time.Duration, force bool) (bridgeManagedRuntime, error) {
	if runtimeState.PID <= 0 {
		return runtimeState, errnorm.Local("bridge_not_running", "bridge runtime has no recorded pid")
	}
	if err := signalManagedRuntime(runtimeState, syscall.SIGTERM); err != nil {
		return runtimeState, errnorm.Wrap(errnorm.KindLocal, "bridge_stop_failed", "failed to signal bridge runtime", err)
	}
	runtimeState.LastSignal = "SIGTERM"
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !bridgeProcessAlive(runtimeState.PID) {
			runtimeState.StoppedAt = time.Now().UTC().Format(time.RFC3339)
			return runtimeState, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !force {
		return runtimeState, errnorm.WithDetails(
			errnorm.Local("bridge_stop_timeout", "bridge runtime did not stop before the timeout"),
			map[string]any{"pid": runtimeState.PID, "config_path": runtimeState.ConfigPath},
		)
	}
	if err := signalManagedRuntime(runtimeState, syscall.SIGKILL); err != nil {
		return runtimeState, errnorm.Wrap(errnorm.KindLocal, "bridge_kill_failed", "failed to force-stop bridge runtime", err)
	}
	runtimeState.LastSignal = "SIGKILL"
	time.Sleep(150 * time.Millisecond)
	runtimeState.StoppedAt = time.Now().UTC().Format(time.RFC3339)
	return runtimeState, nil
}

func signalManagedRuntime(runtimeState bridgeManagedRuntime, sig syscall.Signal) error {
	target := runtimeState.PID
	if runtimeState.PGID > 0 {
		target = -runtimeState.PGID
	}
	err := syscall.Kill(target, sig)
	if err == syscall.ESRCH {
		return nil
	}
	return err
}

func defaultBridgeProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func bridgeManagedRuntimeRunning(runtimeState bridgeManagedRuntime) (bool, string) {
	if !bridgeProcessAlive(runtimeState.PID) {
		return false, "already_exited"
	}
	cmdline, err := bridgeProcessCommandLine(runtimeState.PID)
	if err != nil {
		return false, "pid_reused"
	}
	if !strings.Contains(cmdline, "oar-agent-bridge") || !strings.Contains(cmdline, runtimeState.ConfigPath) {
		return false, "pid_reused"
	}
	if runtimeState.Kind == "router" && !strings.Contains(cmdline, "router") {
		return false, "pid_reused"
	}
	if runtimeState.Kind == "agent" && !strings.Contains(cmdline, "bridge") {
		return false, "pid_reused"
	}
	return true, ""
}

func defaultBridgeProcessCommandLine(pid int) (string, error) {
	cmd := exec.Command("ps", "-o", "command=", "-p", strconv.Itoa(pid))
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func tailLines(content string, limit int) string {
	if limit <= 0 {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return strings.Join(lines, "\n")
}

func resolveBridgeBinary(home string, installDir string, binDir string) (string, error) {
	if strings.TrimSpace(installDir) == "" {
		installDir = bridgeDefaultInstallDir(home)
	}
	if strings.TrimSpace(binDir) == "" {
		binDir = bridgeDefaultBinDir(home)
	}
	bridgeBinary := filepath.Join(binDir, "oar-agent-bridge")
	if _, err := bridgeStat(bridgeBinary); err == nil {
		return bridgeBinary, nil
	}
	lookup, err := bridgeLookPath("oar-agent-bridge")
	if err != nil {
		return "", errnorm.Local("bridge_binary_missing", "oar-agent-bridge wrapper not found; run `oar bridge install`")
	}
	return lookup, nil
}
