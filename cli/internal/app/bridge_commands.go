package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"organization-autorunner-cli/internal/config"
	"organization-autorunner-cli/internal/errnorm"
)

const bridgeRepoURL = "https://github.com/Git-on-my-level/organization-autorunner.git"

var (
	bridgeLookPath    = exec.LookPath
	bridgeMkdirAll    = os.MkdirAll
	bridgeWriteFile   = os.WriteFile
	bridgeStat        = os.Stat
	bridgeCommandRun  = defaultBridgeCommandRun
	bridgeUserHomeDir = os.UserHomeDir
)

type bridgePythonRuntime struct {
	Command string
	Version string
}

type bridgeDoctorCheck struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

func init() {
	runtimeHelpManualDocTopics = append(runtimeHelpManualDocTopics, runtimeHelpDocTopic{
		Path:    "bridge",
		Kind:    "manual",
		Summary: "CLI-managed bridge bootstrap helpers for installing, templating, and checking `oar-agent-bridge`.",
	})
	localHelperTopics = append(localHelperTopics,
		localHelperTopic{
			Path:        "bridge install",
			Summary:     "Install `oar-agent-bridge` into a dedicated Python 3.11+ virtualenv and expose a PATH wrapper.",
			JSONShape:   "`install_dir`, `bin_dir`, `wrapper_path`, `python`, `bridge_binary`, `package_ref`",
			Composition: "Pure local bootstrap helper with network package download. Creates or reuses a venv, installs the bridge package from the GitHub subdirectory, and writes a thin launcher script.",
			Examples: []string{
				"oar bridge install",
				"oar bridge install --ref main --with-dev",
			},
			Flags: []localHelperFlag{
				{Name: "--python <exe>", Description: "Preferred Python executable. Default probes for Python 3.11+."},
				{Name: "--install-dir <dir>", Description: "Root directory for the managed bridge virtualenv."},
				{Name: "--bin-dir <dir>", Description: "Directory where the `oar-agent-bridge` wrapper should be written."},
				{Name: "--ref <git-ref>", Description: "Git ref to install from. Defaults to `main` unless you pin a different branch or tag."},
				{Name: "--with-dev", Description: "Also install bridge test dependencies."},
			},
		},
		localHelperTopic{
			Path:        "bridge init-config",
			Summary:     "Write a minimal router or agent bridge TOML config with the pending-until-check-in lifecycle baked in.",
			JSONShape:   "`kind`, `output`, `workspace_id`, `handle`, `content`",
			Composition: "Pure local helper. Renders one minimal bridge config template with explicit workspace-id and readiness settings; optionally writes it to disk.",
			Examples: []string{
				"oar bridge init-config --kind router --output ./router.toml --workspace-id ws_main",
				"oar bridge init-config --kind hermes --output ./agent.toml --workspace-id ws_main --handle hermes",
			},
			Flags: []localHelperFlag{
				{Name: "--kind <router|hermes|zeroclaw>", Description: "Template kind to render."},
				{Name: "--output <path>", Description: "Write the rendered TOML to a file. Omit to print it."},
				{Name: "--workspace-id <id>", Description: "Durable OAR workspace id. Do not use a slug or UI path segment."},
				{Name: "--handle <name>", Description: "Agent handle for bridge templates."},
			},
		},
		localHelperTopic{
			Path:        "bridge doctor",
			Summary:     "Validate bridge install, config presence, and registration readiness without starting the daemon.",
			JSONShape:   "`checks`, `registration`, `bridge_binary`, `python`",
			Composition: "Pure local helper plus optional bridge CLI calls. Probes Python, the managed install, and `registration status` for a supplied config.",
			Examples: []string{
				"oar bridge doctor",
				"oar bridge doctor --config ./agent.toml",
			},
			Flags: []localHelperFlag{
				{Name: "--config <path>", Description: "Bridge config to validate with `registration status`."},
				{Name: "--python <exe>", Description: "Preferred Python executable. Default probes for Python 3.11+."},
				{Name: "--install-dir <dir>", Description: "Root directory for the managed bridge virtualenv."},
				{Name: "--bin-dir <dir>", Description: "Directory where the managed `oar-agent-bridge` wrapper should exist."},
			},
		},
	)
}

func (a *App) runBridgeCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 || isHelpToken(args[0]) {
		return &commandResult{Text: bridgeUsageText()}, "bridge", nil
	}
	sub := bridgeSubcommandSpec.normalize(args[0])
	switch sub {
	case "install":
		result, err := a.runBridgeInstall(ctx, args[1:])
		return result, "bridge install", err
	case "init-config":
		result, err := a.runBridgeInitConfig(args[1:], cfg)
		return result, "bridge init-config", err
	case "doctor":
		result, err := a.runBridgeDoctor(ctx, args[1:])
		return result, "bridge doctor", err
	case "start":
		result, err := a.runBridgeStart(ctx, args[1:])
		return result, "bridge start", err
	case "stop":
		result, err := a.runBridgeStop(args[1:])
		return result, "bridge stop", err
	case "restart":
		result, err := a.runBridgeRestart(ctx, args[1:])
		return result, "bridge restart", err
	case "status":
		result, err := a.runBridgeStatus(ctx, args[1:])
		return result, "bridge status", err
	case "logs":
		result, err := a.runBridgeLogs(args[1:])
		return result, "bridge logs", err
	default:
		return nil, "bridge", bridgeSubcommandSpec.unknownError(args[0])
	}
}

func bridgeUsageText() string {
	return strings.TrimSpace(`Bridge bootstrap

Use `+"`oar bridge`"+` when you only have the main CLI installed and need to bootstrap, manage, or inspect the Python `+"`oar-agent-bridge`"+` runtime. This is the discoverable install/setup path for agents and operators. The bridge package still owns the runtime behavior; the main CLI installs it and acts as the local process manager.

Bootstrap prerequisites

- Python `+"`3.11+`"+`
- `+"`git`"+` on PATH for the current GitHub-subdirectory install path

Lifecycle constraint

- A registration document alone is not enough to make an agent taggable.
- Bridge-managed registrations stay `+"`pending`"+` until the bridge has checked in.
- Humans should only expect `+"`@handle`"+` wakeups to work after `+"`oar bridge doctor --config <agent.toml>`"+` reports the registration as wakeable.

Subcommands

  bridge install      Install or refresh the managed `+"`oar-agent-bridge`"+` virtualenv and wrapper
  bridge init-config  Render a minimal router or bridge TOML config
  bridge start        Start a managed router or bridge daemon for one config
  bridge stop         Stop a managed router or bridge daemon for one config
  bridge restart      Restart a managed router or bridge daemon for one config
  bridge status       Inspect managed process state for one config
  bridge logs         Read recent log lines for one config
  bridge doctor       Validate install/config/readiness without starting daemons

Recommended order

1. `+"`oar bridge install`"+`
2. `+"`oar bridge init-config --kind router --output ./router.toml --workspace-id <workspace-id>`"+`
3. `+"`oar bridge init-config --kind hermes --output ./agent.toml --workspace-id <workspace-id> --handle <handle>`"+`
4. `+"`oar-agent-bridge auth register ...`"+` for the router and agent principal
5. `+"`oar bridge start --config ./router.toml`"+` and `+"`oar bridge start --config ./agent.toml`"+`
6. `+"`oar bridge status --config ./agent.toml`"+` and `+"`oar bridge doctor --config ./agent.toml`"+` before telling humans to tag `+"`@handle`"+`
`) + "\n"
}

func (a *App) runBridgeInstall(ctx context.Context, args []string) (*commandResult, error) {
	if runtime.GOOS == "windows" {
		return nil, errnorm.Usage("unsupported_platform", "`oar bridge install` currently supports macOS and Linux only")
	}
	fs := newSilentFlagSet("bridge install")
	var pythonFlag trackedString
	var installDirFlag trackedString
	var binDirFlag trackedString
	var refFlag trackedString
	var withDev trackedBool
	fs.Var(&pythonFlag, "python", "Preferred Python executable")
	fs.Var(&installDirFlag, "install-dir", "Root directory for the managed bridge virtualenv")
	fs.Var(&binDirFlag, "bin-dir", "Directory where the oar-agent-bridge wrapper should be written")
	fs.Var(&refFlag, "ref", "Git ref to install from")
	fs.Var(&withDev, "with-dev", "Also install bridge development/test dependencies")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `oar bridge install`")
	}

	home, err := a.bridgeHome()
	if err != nil {
		return nil, err
	}
	installDir := strings.TrimSpace(installDirFlag.value)
	if installDir == "" {
		installDir = bridgeDefaultInstallDir(home)
	}
	binDir := strings.TrimSpace(binDirFlag.value)
	if binDir == "" {
		binDir = bridgeDefaultBinDir(home)
	}
	pythonRuntime, err := detectBridgePython(ctx, strings.TrimSpace(pythonFlag.value))
	if err != nil {
		return nil, err
	}
	if _, err := bridgeLookPath("git"); err != nil {
		return nil, errnorm.Local("git_required", "`oar bridge install` currently requires `git` on PATH because it installs the bridge package from the GitHub repo")
	}
	venvDir := filepath.Join(installDir, ".venv")
	venvPython := filepath.Join(venvDir, "bin", "python")
	bridgeBinary := filepath.Join(venvDir, "bin", "oar-agent-bridge")
	ref := strings.TrimSpace(refFlag.value)
	if ref == "" {
		ref = defaultBridgeInstallRef()
	}
	if err := bridgeMkdirAll(installDir, 0o755); err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "bridge_install_dir_failed", "failed to create bridge install directory", err)
	}
	if err := runBridgeExternal(ctx, pythonRuntime.Command, "-m", "venv", venvDir); err != nil {
		return nil, err
	}
	if err := runBridgeExternal(ctx, venvPython, "-m", "pip", "install", "--upgrade", "pip"); err != nil {
		return nil, err
	}
	if err := runBridgeExternal(ctx, venvPython, "-m", "pip", "install", bridgeInstallPackageSpec(ref)); err != nil {
		return nil, err
	}
	if withDev.set && withDev.value {
		if err := runBridgeExternal(ctx, venvPython, "-m", "pip", "install", "pytest>=8,<9"); err != nil {
			return nil, err
		}
	}
	if err := bridgeMkdirAll(binDir, 0o755); err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "bridge_bin_dir_failed", "failed to create bridge bin directory", err)
	}
	wrapperPath := filepath.Join(binDir, "oar-agent-bridge")
	if err := bridgeWriteLauncher(wrapperPath, bridgeBinary); err != nil {
		return nil, err
	}
	versionOut, err := probeBridgeBinaryOutput(ctx, bridgeBinary)
	if err != nil {
		return nil, err
	}
	data := map[string]any{
		"install_dir":    installDir,
		"bin_dir":        binDir,
		"wrapper_path":   wrapperPath,
		"python":         pythonRuntime.Command,
		"python_version": pythonRuntime.Version,
		"bridge_binary":  bridgeBinary,
		"package_ref":    ref,
		"version":        strings.TrimSpace(versionOut),
	}
	lines := []string{
		"Bridge install complete.",
		"Bridge binary: " + bridgeBinary,
		"Wrapper path: " + wrapperPath,
		"Python: " + pythonRuntime.Command + " (" + pythonRuntime.Version + ")",
		"Installed ref: " + ref,
		"Version: " + strings.TrimSpace(versionOut),
		"Next step: oar bridge init-config --kind router --output ./router.toml --workspace-id <workspace-id>",
		"Next step: oar bridge doctor --config ./agent.toml once the bridge has checked in",
	}
	if !bridgePathContains(a.Getenv, binDir) {
		lines = append(lines, "PATH note: add "+binDir+" to PATH to run `oar-agent-bridge` directly.")
	}
	return &commandResult{Text: strings.Join(lines, "\n"), Data: data}, nil
}

func (a *App) runBridgeInitConfig(args []string, cfg config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("bridge init-config")
	var kindFlag trackedString
	var outputFlag trackedString
	var baseURLFlag trackedString
	var workspaceIDFlag trackedString
	var workspaceNameFlag trackedString
	var workspaceURLFlag trackedString
	var handleFlag trackedString
	var authStateFlag trackedString
	var stateDirFlag trackedString
	var hermesCwdFlag trackedString
	var zeroclawURLFlag trackedString
	var zeroclawTokenFlag trackedString
	fs.Var(&kindFlag, "kind", "Template kind: router, hermes, or zeroclaw")
	fs.Var(&outputFlag, "output", "Write the rendered TOML to a file")
	fs.Var(&baseURLFlag, "base-url", "OAR base URL")
	fs.Var(&workspaceIDFlag, "workspace-id", "Durable OAR workspace id")
	fs.Var(&workspaceNameFlag, "workspace-name", "Human-readable workspace name")
	fs.Var(&workspaceURLFlag, "workspace-url", "Human workspace URL")
	fs.Var(&handleFlag, "handle", "Agent handle for bridge templates")
	fs.Var(&authStateFlag, "auth-state-path", "Auth state path")
	fs.Var(&stateDirFlag, "state-dir", "Agent state dir")
	fs.Var(&hermesCwdFlag, "adapter-cwd", "Default Hermes working directory")
	fs.Var(&zeroclawURLFlag, "gateway-url", "ZeroClaw gateway base URL")
	fs.Var(&zeroclawTokenFlag, "bearer-token", "ZeroClaw bearer token")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `oar bridge init-config`")
	}
	kind := strings.TrimSpace(kindFlag.value)
	if kind == "" {
		kind = "hermes"
	}
	baseURL := strings.TrimSpace(baseURLFlag.value)
	if baseURL == "" {
		baseURL = cfg.BaseURL
	}
	workspaceID := strings.TrimSpace(workspaceIDFlag.value)
	if workspaceID == "" {
		return nil, errnorm.Usage("invalid_request", "--workspace-id is required; use the durable workspace id, not a slug")
	}
	workspaceName := strings.TrimSpace(workspaceNameFlag.value)
	if workspaceName == "" {
		workspaceName = "Main"
	}
	rendered, handle, err := renderBridgeConfigTemplate(bridgeTemplateParams{
		Kind:          kind,
		BaseURL:       baseURL,
		WorkspaceID:   workspaceID,
		WorkspaceName: workspaceName,
		WorkspaceURL:  strings.TrimSpace(workspaceURLFlag.value),
		Handle:        strings.TrimSpace(handleFlag.value),
		AuthStatePath: strings.TrimSpace(authStateFlag.value),
		StateDir:      strings.TrimSpace(stateDirFlag.value),
		HermesCWD:     strings.TrimSpace(hermesCwdFlag.value),
		ZeroClawURL:   strings.TrimSpace(zeroclawURLFlag.value),
		ZeroClawToken: strings.TrimSpace(zeroclawTokenFlag.value),
	})
	if err != nil {
		return nil, err
	}
	outputPath := strings.TrimSpace(outputFlag.value)
	text := rendered
	if outputPath != "" {
		if err := bridgeWriteConfig(outputPath, rendered); err != nil {
			return nil, err
		}
		text = strings.Join([]string{
			"Bridge config written.",
			"Kind: " + kind,
			"Path: " + outputPath,
			"Lifecycle: registrations stay pending until the bridge checks in.",
		}, "\n")
	}
	return &commandResult{
		Text: text,
		Data: map[string]any{
			"kind":         kind,
			"output":       outputPath,
			"workspace_id": workspaceID,
			"handle":       handle,
			"content":      rendered,
		},
	}, nil
}

func (a *App) runBridgeDoctor(ctx context.Context, args []string) (*commandResult, error) {
	fs := newSilentFlagSet("bridge doctor")
	var pythonFlag trackedString
	var installDirFlag trackedString
	var binDirFlag trackedString
	var configFlag trackedString
	fs.Var(&pythonFlag, "python", "Preferred Python executable")
	fs.Var(&installDirFlag, "install-dir", "Root directory for the managed bridge virtualenv")
	fs.Var(&binDirFlag, "bin-dir", "Directory where the oar-agent-bridge wrapper should exist")
	fs.Var(&configFlag, "config", "Bridge config to validate")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `oar bridge doctor`")
	}
	home, err := a.bridgeHome()
	if err != nil {
		return nil, err
	}
	installDir := strings.TrimSpace(installDirFlag.value)
	if installDir == "" {
		installDir = bridgeDefaultInstallDir(home)
	}
	binDir := strings.TrimSpace(binDirFlag.value)
	if binDir == "" {
		binDir = bridgeDefaultBinDir(home)
	}
	checks := make([]bridgeDoctorCheck, 0, 5)
	hasFailure := false
	addCheck := func(name string, ok bool, message string) {
		if !ok {
			hasFailure = true
		}
		checks = append(checks, bridgeDoctorCheck{Name: name, OK: ok, Message: message})
	}

	pythonRuntime, pyErr := detectBridgePython(ctx, strings.TrimSpace(pythonFlag.value))
	if pyErr != nil {
		addCheck("python", false, errnorm.Normalize(pyErr).Message)
	} else {
		addCheck("python", true, pythonRuntime.Command+" ("+pythonRuntime.Version+")")
	}

	venvPython := filepath.Join(installDir, ".venv", "bin", "python")
	if _, err := bridgeStat(venvPython); err != nil {
		addCheck("managed_venv", false, "managed bridge venv not found at "+filepath.Join(installDir, ".venv"))
	} else {
		addCheck("managed_venv", true, "managed bridge venv present at "+filepath.Join(installDir, ".venv"))
	}

	bridgeBinary := filepath.Join(binDir, "oar-agent-bridge")
	if _, err := bridgeStat(bridgeBinary); err != nil {
		lookup, lookupErr := bridgeLookPath("oar-agent-bridge")
		if lookupErr != nil {
			addCheck("bridge_binary", false, "oar-agent-bridge wrapper not found; run `oar bridge install`")
		} else {
			bridgeBinary = lookup
			addCheck("bridge_binary", true, "resolved from PATH at "+lookup)
		}
	} else {
		addCheck("bridge_binary", true, "managed wrapper present at "+bridgeBinary)
	}

	var versionOut string
	if !hasFailure || checks[len(checks)-1].Name == "bridge_binary" && checks[len(checks)-1].OK {
		versionOut, err = probeBridgeBinaryOutput(ctx, bridgeBinary)
		if err != nil {
			addCheck("bridge_version", false, errnorm.Normalize(err).Message)
		} else {
			addCheck("bridge_version", true, strings.TrimSpace(versionOut))
		}
	}

	registrationData := map[string]any{}
	configPath := strings.TrimSpace(configFlag.value)
	if configPath != "" {
		if _, err := bridgeStat(configPath); err != nil {
			addCheck("config", false, "config file not found: "+configPath)
		} else {
			addCheck("config", true, "config file present: "+configPath)
			adapterOut, adapterErr := runBridgeExternalOutput(ctx, bridgeBinary, "bridge", "doctor", "--config", configPath)
			if adapterErr != nil {
				addCheck("adapter", false, errnorm.Normalize(adapterErr).Message)
			} else {
				addCheck("adapter", true, "adapter readiness probe passed")
				var adapterData map[string]any
				if err := json.Unmarshal([]byte(adapterOut), &adapterData); err == nil {
					registrationData["adapter"] = adapterData
				}
			}
			statusOut, statusErr := runBridgeExternalOutput(ctx, bridgeBinary, "registration", "status", "--config", configPath)
			if statusErr != nil {
				addCheck("registration", false, errnorm.Normalize(statusErr).Message)
			} else {
				if err := json.Unmarshal([]byte(statusOut), &registrationData); err != nil {
					addCheck("registration", false, "failed to parse registration status output")
				} else if wakeable, _ := registrationData["wakeable"].(bool); wakeable {
					addCheck("registration", true, "registration is wakeable")
				} else {
					message := "registration is not wakeable yet"
					if blockers, ok := registrationData["blockers"].([]any); ok && len(blockers) > 0 {
						parts := make([]string, 0, len(blockers))
						for _, blocker := range blockers {
							parts = append(parts, fmt.Sprint(blocker))
						}
						message = strings.Join(parts, "; ")
					}
					addCheck("registration", false, message)
				}
			}
		}
	}

	lines := []string{"Bridge doctor"}
	for _, check := range checks {
		state := "PASS"
		if !check.OK {
			state = "FAIL"
		}
		lines = append(lines, "["+state+"] "+check.Name+": "+check.Message)
	}
	result := &commandResult{
		Text: strings.Join(lines, "\n"),
		Data: map[string]any{
			"checks":        checks,
			"bridge_binary": bridgeBinary,
			"python":        pythonRuntime,
			"registration":  registrationData,
		},
	}
	if hasFailure {
		return result, errnorm.WithDetails(errnorm.Local("bridge_doctor_failed", "bridge doctor found failing checks"), result.Data)
	}
	return result, nil
}

type bridgeTemplateParams struct {
	Kind          string
	BaseURL       string
	WorkspaceID   string
	WorkspaceName string
	WorkspaceURL  string
	Handle        string
	AuthStatePath string
	StateDir      string
	HermesCWD     string
	ZeroClawURL   string
	ZeroClawToken string
}

func renderBridgeConfigTemplate(params bridgeTemplateParams) (string, string, error) {
	baseURL := firstNonEmptyString(params.BaseURL, "http://127.0.0.1:8000")
	workspaceName := firstNonEmptyString(params.WorkspaceName, "Main")
	switch strings.TrimSpace(params.Kind) {
	case "router":
		authState := firstNonEmptyString(params.AuthStatePath, ".state/router-auth.json")
		return strings.TrimSpace(fmt.Sprintf(`
[oar]
base_url = %q
workspace_id = %q
workspace_name = %q
workspace_url = %q
verify_ssl = true

[auth]
state_path = %q

[router]
state_path = ".state/router-state.json"
principal_cache_ttl_seconds = 60
reconnect_delay_seconds = 3

[adapter]
kind = "none"
`, baseURL, params.WorkspaceID, workspaceName, params.WorkspaceURL, authState)) + "\n", "", nil
	case "hermes":
		handle := firstNonEmptyString(params.Handle, "<handle>")
		authState := firstNonEmptyString(params.AuthStatePath, ".state/"+handle+"-auth.json")
		stateDir := firstNonEmptyString(params.StateDir, ".state/"+handle)
		workspacePath := firstNonEmptyString(params.HermesCWD, "/absolute/path/to/your/hermes/workspace")
		return strings.TrimSpace(fmt.Sprintf(`
[oar]
base_url = %q
workspace_id = %q
workspace_name = %q
workspace_url = %q
verify_ssl = true

[auth]
state_path = %q

[agent]
handle = %q
driver_kind = "acp"
adapter_kind = "hermes_acp"
state_dir = %q
workspace_bindings = [%q]
resume_policy = "resume_or_create"
status = "pending"
checkin_interval_seconds = 60
checkin_ttl_seconds = 300

[adapter]
kind = "hermes_acp"
command = ["hermes", "acp"]
cwd_default = %q
auto_select_permission = true

[adapter.workspace_map]
%q = %q
`, baseURL, params.WorkspaceID, workspaceName, params.WorkspaceURL, authState, handle, stateDir, params.WorkspaceID, workspacePath, params.WorkspaceID, workspacePath)) + "\n", handle, nil
	case "zeroclaw":
		handle := firstNonEmptyString(params.Handle, "<handle>")
		authState := firstNonEmptyString(params.AuthStatePath, ".state/"+handle+"-auth.json")
		stateDir := firstNonEmptyString(params.StateDir, ".state/"+handle)
		return strings.TrimSpace(fmt.Sprintf(`
[oar]
base_url = %q
workspace_id = %q
workspace_name = %q
workspace_url = %q
verify_ssl = true

[auth]
state_path = %q

[agent]
handle = %q
driver_kind = "http"
adapter_kind = "zeroclaw_gateway"
state_dir = %q
workspace_bindings = [%q]
resume_policy = "resume_or_create"
status = "pending"
checkin_interval_seconds = 60
checkin_ttl_seconds = 300

[adapter]
kind = "zeroclaw_gateway"
base_url = %q
bearer_token = %q
webhook_secret = ""
request_timeout_seconds = 600
session_header_name = "X-Session-Id"
`, baseURL, params.WorkspaceID, workspaceName, params.WorkspaceURL, authState, handle, stateDir, params.WorkspaceID, firstNonEmptyString(params.ZeroClawURL, "http://127.0.0.1:42617"), firstNonEmptyString(params.ZeroClawToken, "REPLACE_WITH_ZEROCLAW_BEARER_TOKEN"))) + "\n", handle, nil
	default:
		return "", "", errnorm.Usage("invalid_request", "unknown bridge config kind; use router, hermes, or zeroclaw")
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (a *App) bridgeHome() (string, error) {
	if a.UserHomeDir != nil {
		home, err := a.UserHomeDir()
		if err == nil {
			return home, nil
		}
	}
	home, err := bridgeUserHomeDir()
	if err != nil {
		return "", errnorm.Wrap(errnorm.KindLocal, "resolve_home_failed", "failed to resolve home directory", err)
	}
	return home, nil
}

func bridgeDefaultInstallDir(home string) string {
	return filepath.Join(home, ".local", "share", "oar", "agent-bridge")
}

func bridgeDefaultBinDir(home string) string {
	return filepath.Join(home, ".local", "bin")
}

func bridgeInstallPackageSpec(ref string) string {
	return fmt.Sprintf("git+%s@%s#subdirectory=adapters/agent-bridge", bridgeRepoURL, ref)
}

func defaultBridgeInstallRef() string {
	return "main"
}

func detectBridgePython(ctx context.Context, preferred string) (bridgePythonRuntime, error) {
	candidates := make([]string, 0, 4)
	if preferred != "" {
		candidates = append(candidates, preferred)
	}
	candidates = append(candidates, "python3.12", "python3.11", "python3")
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		runtimeInfo, ok := probeBridgePython(ctx, candidate)
		if ok {
			return runtimeInfo, nil
		}
	}
	return bridgePythonRuntime{}, errnorm.Local("python_unsupported", "Python 3.11+ is required for `oar-agent-bridge`; pass --python <exe> if needed")
}

func probeBridgePython(ctx context.Context, candidate string) (bridgePythonRuntime, bool) {
	name := candidate
	if !strings.Contains(candidate, string(os.PathSeparator)) {
		if _, err := bridgeLookPath(candidate); err != nil {
			return bridgePythonRuntime{}, false
		}
	}
	out, err := runBridgeExternalOutput(ctx, name, "-c", "import sys; print(f'{sys.version_info[0]}.{sys.version_info[1]}')")
	if err != nil {
		return bridgePythonRuntime{}, false
	}
	version := strings.TrimSpace(out)
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return bridgePythonRuntime{}, false
	}
	major, majorErr := strconv.Atoi(parts[0])
	minor, minorErr := strconv.Atoi(parts[1])
	if majorErr != nil || minorErr != nil || major < 3 || (major == 3 && minor < 11) {
		return bridgePythonRuntime{}, false
	}
	return bridgePythonRuntime{Command: name, Version: version}, true
}

func runBridgeExternal(ctx context.Context, name string, args ...string) error {
	_, err := runBridgeExternalOutput(ctx, name, args...)
	return err
}

func runBridgeExternalOutput(ctx context.Context, name string, args ...string) (string, error) {
	stdout, stderr, err := bridgeCommandRun(ctx, name, args...)
	if err != nil {
		message := strings.TrimSpace(stderr)
		if message == "" {
			message = strings.TrimSpace(stdout)
		}
		if message == "" {
			message = err.Error()
		}
		return stdout, errnorm.Wrap(errnorm.KindLocal, "bridge_command_failed", fmt.Sprintf("failed running %s", strings.Join(append([]string{name}, args...), " ")), fmt.Errorf("%s", message))
	}
	return stdout, nil
}

func probeBridgeBinaryOutput(ctx context.Context, bridgeBinary string) (string, error) {
	versionOut, err := runBridgeExternalOutput(ctx, bridgeBinary, "--version")
	if err == nil {
		return versionOut, nil
	}
	helpOut, helpErr := runBridgeExternalOutput(ctx, bridgeBinary, "--help")
	if helpErr != nil {
		return "", err
	}
	firstLine := strings.TrimSpace(strings.SplitN(helpOut, "\n", 2)[0])
	if firstLine == "" {
		firstLine = "oar-agent-bridge --help"
	}
	return firstLine, nil
}

func defaultBridgeCommandRun(ctx context.Context, name string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", string(output), err
	}
	return string(output), "", nil
}

func bridgeWriteLauncher(path string, bridgeBinary string) error {
	content := "#!/bin/sh\nexec " + shellSingleQuote(bridgeBinary) + ` "$@"` + "\n"
	if err := bridgeWriteFile(path, []byte(content), 0o755); err != nil {
		return errnorm.Wrap(errnorm.KindLocal, "bridge_wrapper_write_failed", "failed to write oar-agent-bridge launcher", err)
	}
	return nil
}

func bridgeWriteConfig(path string, content string) error {
	if err := bridgeMkdirAll(filepath.Dir(path), 0o755); err != nil {
		return errnorm.Wrap(errnorm.KindLocal, "bridge_config_dir_failed", "failed to create config directory", err)
	}
	if err := bridgeWriteFile(path, []byte(content), 0o600); err != nil {
		return errnorm.Wrap(errnorm.KindLocal, "bridge_config_write_failed", "failed to write bridge config", err)
	}
	return nil
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func bridgePathContains(getenv func(string) string, dir string) bool {
	if getenv == nil {
		getenv = os.Getenv
	}
	for _, item := range filepath.SplitList(getenv("PATH")) {
		if item == dir {
			return true
		}
	}
	return false
}
