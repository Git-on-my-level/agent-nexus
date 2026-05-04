package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"agent-nexus-cli/internal/buildinfo"
	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/httpclient"
	"agent-nexus-cli/internal/profile"

	toml "github.com/pelletier/go-toml/v2"
)

const bridgeRepoURL = "https://github.com/Git-on-my-level/agent-nexus.git"

// bridgeDoctorChildTimeout bounds subprocess calls to anx-agent-bridge (doctor + registration status).
const bridgeDoctorChildTimeout = 5 * time.Minute

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
	// Status is pass | warn | fail. Empty means derive from OK (pass when OK else fail).
	Status string `json:"status,omitempty"`
}

func init() {
	runtimeHelpManualDocTopics = append(runtimeHelpManualDocTopics, runtimeHelpDocTopic{
		Path:    "bridge",
		Kind:    "manual",
		Summary: "CLI-managed bridge bootstrap helpers for installing, templating, and checking `anx-agent-bridge`.",
	})
	localHelperTopics = append(localHelperTopics,
		localHelperTopic{
			Path:        "bridge install",
			Summary:     "Install `anx-agent-bridge` into a dedicated Python 3.11+ virtualenv and expose a PATH wrapper.",
			JSONShape:   "`install_dir`, `bin_dir`, `wrapper_path`, `python`, `bridge_binary`, `package_ref`",
			Composition: "Pure local bootstrap helper with network package download. Creates or reuses a venv, installs the bridge package from the GitHub subdirectory at a pinned git ref (defaults to the running CLI release tag), and writes a thin launcher script.",
			Examples: []string{
				"anx bridge install",
				"anx bridge install --ref main --with-dev",
			},
			Flags: []localHelperFlag{
				{Name: "--python <exe>", Description: "Preferred Python executable. Default probes for Python 3.11+."},
				{Name: "--install-dir <dir>", Description: "Root directory for the managed bridge virtualenv."},
				{Name: "--bin-dir <dir>", Description: "Directory where the `anx-agent-bridge` wrapper should be written."},
				{Name: "--ref <git-ref>", Description: "Git ref to install from. Defaults to the running CLI's version tag (e.g. `v0.3.2`) so the bridge matches this binary; use `main` for the latest commit on the default branch."},
				{Name: "--with-dev", Description: "Also install bridge test dependencies."},
			},
		},
		localHelperTopic{
			Path:        "bridge import-auth",
			Summary:     "Copy an existing `anx` profile and key into the bridge agent home auth state.",
			JSONShape:   "`config_path`, `auth_state_path`, `wake_config_path`, `profile_path`, `profile_agent`, `username`, `actor_id`, `agent_id`, `key_id`, `public_key_fingerprint`",
			Composition: "Pure local helper. Reads an existing `anx` profile plus Ed25519 key material, converts it into bridge auth state, stamps agent.toml identity including public key fingerprint, and reconciles wake.toml workspace base URLs.",
			Examples: []string{
				"anx bridge import-auth --config ./bridge.toml --from-profile agent-a",
				"anx --agent agent-a bridge import-auth --config ./bridge.toml",
			},
			Flags: []localHelperFlag{
				{Name: "--config <path>", Description: "Bridge config whose auth state should be populated."},
				{Name: "--from-profile <agent>", Description: "Existing `anx` profile name to import. Defaults to the active CLI profile."},
			},
		},
		localHelperTopic{
			Path:        "bridge init-config",
			Summary:     "Write a bridge runtime config plus an agent home with wake subscriptions.",
			JSONShape:   "`kind`, `output`, `agent_home`, `workspace_ids`, `workspace_id_source`, `handle`, `content`",
			Composition: "Local helper. Renders a bridge runtime config that references an explicit agent home, plus agent.toml and wake.toml when --output is used. If --workspace-id is omitted, discovers the durable workspace id from the active profile or core handshake.",
			Examples: []string{
				"anx bridge init-config --kind hermes --output ./bridge.toml --agent-home ./.anx --handle myagent",
				"anx bridge init-config --kind openclaw --output ./bridge.toml --agent-home ./.anx --handle myagent --openclaw-bin /opt/homebrew/bin/openclaw",
				"anx bridge init-config --kind subprocess --output ./bridge.toml --agent-home ./.anx --handle myagent --adapter-entrypoint ./adapter.py",
				"anx bridge init-config --kind python-plugin --output ./bridge.toml --agent-home ./.anx --workspace-id ws_main --workspace-id ws_ops --handle myagent --plugin-module my_bridge --plugin-factory build_adapter",
			},
			Flags: []localHelperFlag{
				{Name: "--kind <hermes|openclaw|subprocess|python-plugin>", Description: "Template kind to render."},
				{Name: "--output <path>", Description: "Write the rendered TOML to a file. Omit to print it."},
				{Name: "--agent-home <dir>", Description: "Agent home directory for identity, auth, wake config, state, and logs. Default: ./.anx."},
				{Name: "--base-url <url>", Description: "ANX base URL for agent.toml identity and wake.toml workspace entries."},
				{Name: "--workspace-id <id>", Description: "Durable ANX workspace id. Optional when the active profile/core handshake exposes one; repeat for multi-workspace agents; do not use slugs."},
				{Name: "--workspace-name <name>", Description: "Display name for the first wake workspace."},
				{Name: "--workspace-url <url>", Description: "Optional URL for the first wake workspace."},
				{Name: "--handle <name>", Description: "Agent handle (required); must match the principal username for bridge-managed registration."},
				{Name: "--auth-state-path <path>", Description: "Optional agent-home-relative auth state path override."},
				{Name: "--state-dir <path>", Description: "Optional agent-home-relative bridge state directory."},
				{Name: "--openclaw-bin <path>", Description: "OpenClaw template: absolute path for `[adapter].openclaw_bin`; auto-detected when omitted."},
				{Name: "--anx-cli-bin <path>", Description: "OpenClaw template: absolute path for `[adapter].anx_cli_bin`; auto-detected when omitted."},
				{Name: "--adapter-entrypoint <path>", Description: "Subprocess template: script path used as the second element of `[adapter].command` after python3."},
				{Name: "--plugin-module <module>", Description: "python-plugin template: Python module for `[adapter].plugin_module`."},
				{Name: "--plugin-factory <callable>", Description: "python-plugin template: factory name for `[adapter].plugin_factory`."},
				{Name: "--managed-package-auto-update", Description: "Write `[bridge].managed_package_auto_update = true`; opt-in allows pip refreshes toward the CLI release tag during bridge doctor/start when skew is detected. Requires Python 3.11+, git on PATH, network access, and macOS/Linux (same prerequisites as `anx bridge install`)."},
			},
		},
		localHelperTopic{
			Path:        "bridge workspace-id",
			Summary:     "Discover durable workspace ids from an existing agent wake registration.",
			JSONShape:   "`agent_id`, `handle`, `actor_id`, `registration_status`, `workspace_ids`, `workspace_bindings`",
			Composition: "Uses the active `anx` auth/profile to read agent principal registration metadata and extract enabled workspace bindings so bridge bootstrap can reuse the real durable workspace id instead of guessing.",
			Examples: []string{
				"anx --agent agent-a bridge workspace-id --handle myagent",
			},
			Flags: []localHelperFlag{
				{Name: "--handle <name>", Description: "Agent handle whose wake registration should be inspected."},
			},
		},
		localHelperTopic{
			Path:        "bridge doctor",
			Summary:     "Validate bridge install, config presence, and registration readiness without starting the daemon.",
			JSONShape:   "`checks`, `registration`, `bridge_binary`, `python`",
			Composition: "Pure local helper plus optional bridge CLI calls. Probes Python, the managed install, and `registration status` for a supplied config.",
			Examples: []string{
				"anx bridge doctor",
				"anx bridge doctor --config ./bridge.toml",
			},
			Flags: []localHelperFlag{
				{Name: "--config <path>", Description: "Bridge config to validate with `registration status`."},
				{Name: "--python <exe>", Description: "Preferred Python executable. Default probes for Python 3.11+."},
				{Name: "--install-dir <dir>", Description: "Root directory for the managed bridge virtualenv."},
				{Name: "--bin-dir <dir>", Description: "Directory where the managed `anx-agent-bridge` wrapper should exist."},
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
	case "import-auth":
		result, err := a.runBridgeImportAuth(args[1:], cfg)
		return result, "bridge import-auth", err
	case "init-config":
		result, err := a.runBridgeInitConfig(ctx, args[1:], cfg)
		return result, "bridge init-config", err
	case "workspace-id":
		result, err := a.runBridgeWorkspaceID(ctx, args[1:], cfg)
		return result, "bridge workspace-id", err
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

Use `+"`anx bridge`"+` when you only have the main CLI installed and need to bootstrap, manage, or inspect the Python `+"`anx-agent-bridge`"+` runtime for one agent. This is the discoverable install/setup path for agent operators. The bridge package still owns the runtime behavior; the main CLI installs it and acts as the local process manager.

Bootstrap prerequisites

- Python `+"`3.11+`"+`
- `+"`git`"+` on PATH for the current GitHub-subdirectory install path

Lifecycle constraint

- Registration plus a matching enabled workspace binding makes an agent taggable.
- A fresh bridge check-in makes the agent online for immediate delivery.
- Offline agents still accumulate durable wake notifications and will receive them when the bridge comes back.

Subcommands

  bridge install      Install or refresh the managed `+"`anx-agent-bridge`"+` virtualenv and wrapper
  bridge import-auth  Copy an existing `+"`anx`"+` profile into bridge auth state
  bridge init-config  Render a minimal agent bridge TOML config
  bridge start        Start a managed bridge daemon for one config
  bridge stop         Stop a managed bridge daemon for one config
  bridge restart      Restart a managed bridge daemon for one config
  bridge status       Inspect managed process state for one config
  bridge logs         Read recent log lines for one config
  bridge workspace-id Read workspace ids from an existing wake registration
  bridge doctor       Validate install/config/readiness without starting daemons

Recommended order

1. `+"`anx bridge install`"+`
2. `+"`anx bridge init-config --kind hermes --output ./bridge.toml --agent-home ./.anx --handle <handle>`"+`, `+"`--kind openclaw`"+`, or `+"`--kind subprocess --adapter-entrypoint ./adapter.py`"+` (add `+"`--workspace-id <workspace-id>`"+` only if discovery fails or you need an explicit binding)
3. `+"`anx bridge workspace-id --handle <handle>`"+` if a wake registration already exists and you want to reuse its bindings
4. `+"`anx bridge import-auth --config ./bridge.toml --from-profile <agent>`"+` when matching `+"`anx`"+` auth already exists
5. `+"`anx-agent-bridge auth register ...`"+` for the agent principal when auth does not already exist
6. `+"`anx bridge start --config ./bridge.toml`"+`
7. `+"`anx bridge status --config ./bridge.toml`"+` and `+"`anx bridge doctor --config ./bridge.toml`"+` before expecting immediate online delivery
8. `+"`anx notifications list --status unread`"+` or `+"`anx-agent-bridge notifications list --config ./bridge.toml --status unread`"+` when you want to pull pending notifications directly

Workspace-owned wake routing

- `+"`anx bridge`"+` only manages per-agent bridge daemons.
- Tagged wake routing runs inside `+"`anx-core`"+` as an embedded workspace sidecar.
- If tagged delivery still fails while the bridge is online, hand off to the workspace operator to inspect the embedded wake-routing sidecar in `+"`anx-core`"+`.
`) + "\n"
}

func (a *App) runBridgeInstall(ctx context.Context, args []string) (*commandResult, error) {
	if runtime.GOOS == "windows" {
		return nil, errnorm.Usage("unsupported_platform", "`anx bridge install` currently supports macOS and Linux only")
	}
	fs := newSilentFlagSet("bridge install")
	var pythonFlag trackedString
	var installDirFlag trackedString
	var binDirFlag trackedString
	var refFlag trackedString
	var withDev trackedBool
	fs.Var(&pythonFlag, "python", "Preferred Python executable")
	fs.Var(&installDirFlag, "install-dir", "Root directory for the managed bridge virtualenv")
	fs.Var(&binDirFlag, "bin-dir", "Directory where the anx-agent-bridge wrapper should be written")
	fs.Var(&refFlag, "ref", "Git ref to install from")
	fs.Var(&withDev, "with-dev", "Also install bridge development/test dependencies")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx bridge install`")
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
	ref := strings.TrimSpace(refFlag.value)
	if ref == "" {
		ref = defaultBridgeInstallRef()
	}
	result, err := a.performManagedBridgeInstall(ctx, managedBridgeInstallOpts{
		Home:            home,
		PreferredPython: strings.TrimSpace(pythonFlag.value),
		InstallDir:      installDir,
		BinDir:          binDir,
		Ref:             ref,
		WithDev:         withDev.set && withDev.value,
	})
	if err != nil {
		return nil, err
	}
	data := map[string]any{
		"install_dir":    result.InstallDir,
		"bin_dir":        result.BinDir,
		"wrapper_path":   result.WrapperPath,
		"python":         result.PythonRuntime.Command,
		"python_version": result.PythonRuntime.Version,
		"bridge_binary":  result.BridgeBinary,
		"package_ref":    result.PackageRef,
		"version":        result.VersionLine,
	}
	lines := []string{
		"Bridge install complete.",
		"Bridge binary: " + result.BridgeBinary,
		"Wrapper path: " + result.WrapperPath,
		"Python: " + result.PythonRuntime.Command + " (" + result.PythonRuntime.Version + ")",
		"Installed ref: " + result.PackageRef,
		"Version: " + result.VersionLine,
		"Next step: anx bridge init-config --kind hermes --output ./bridge.toml --agent-home ./.anx --handle <handle>",
		"Add --workspace-id <workspace-id> only if discovery fails or you need an explicit binding.",
		"Alternatives: --kind openclaw for OpenClaw, --kind subprocess with --adapter-entrypoint for custom JSON adapters, or --kind python-plugin with --plugin-module and --plugin-factory for in-process Python adapters.",
		"Next step: anx bridge doctor --config ./bridge.toml once the bridge has checked in",
	}
	if !bridgePathContains(a.Getenv, result.BinDir) {
		lines = append(lines, "PATH note: add "+result.BinDir+" to PATH to run `anx-agent-bridge` directly.")
	}
	return &commandResult{Text: strings.Join(lines, "\n"), Data: data}, nil
}

func (a *App) runBridgeImportAuth(args []string, cfg config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("bridge import-auth")
	var configFlag trackedString
	var fromProfileFlag trackedString
	fs.Var(&configFlag, "config", "Bridge config whose auth state should be populated")
	fs.Var(&fromProfileFlag, "from-profile", "Existing anx profile name to import")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx bridge import-auth`")
	}
	configPath := strings.TrimSpace(configFlag.value)
	if configPath == "" {
		return nil, errnorm.Usage("invalid_request", "--config is required")
	}
	configDetails, err := loadBridgeConfigDetails(configPath)
	if err != nil {
		return nil, err
	}

	profileAgent := strings.TrimSpace(fromProfileFlag.value)
	profilePath := ""
	if profileAgent == "" {
		profileAgent = firstNonEmptyString(cfg.Agent, config.DefaultAgent)
	}
	if strings.TrimSpace(cfg.ProfilePath) != "" && profileAgent == strings.TrimSpace(cfg.Agent) {
		profilePath = strings.TrimSpace(cfg.ProfilePath)
	}
	if profilePath == "" {
		home, err := a.bridgeHome()
		if err != nil {
			return nil, err
		}
		profilePath = profile.ProfilePath(home, profileAgent)
	}

	prof, ok, err := profile.Load(profilePath)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "profile_read_failed", "failed to read source profile", err)
	}
	if !ok {
		return nil, errnorm.Local("profile_not_found", "source profile not found at "+profilePath)
	}
	if prof.Revoked {
		return nil, errnorm.Local("agent_revoked", "cannot import auth from a revoked profile")
	}

	username := firstNonEmptyString(prof.Username, configDetails.AgentHandle, prof.Agent)
	if configDetails.AgentHandle != "" && username != "" && username != configDetails.AgentHandle {
		return nil, errnorm.Local(
			"bridge_auth_handle_mismatch",
			fmt.Sprintf("profile username %q does not match bridge agent.handle %q", username, configDetails.AgentHandle),
		)
	}
	if strings.TrimSpace(prof.AgentID) == "" || strings.TrimSpace(prof.ActorID) == "" || strings.TrimSpace(prof.KeyID) == "" {
		return nil, errnorm.Local("profile_incomplete", "source profile is missing required agent_id/actor_id/key_id fields")
	}
	if strings.TrimSpace(prof.PrivateKeyPath) == "" {
		return nil, errnorm.Local("profile_incomplete", "source profile is missing private_key_path")
	}
	privateKey, err := profile.LoadPrivateKey(strings.TrimSpace(prof.PrivateKeyPath))
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "bridge_auth_key_failed", "failed to load source private key", err)
	}
	publicKey, ok := privateKey.Public().(ed25519.PublicKey)
	if !ok || len(publicKey) != ed25519.PublicKeySize {
		return nil, errnorm.Local("bridge_auth_key_failed", "source private key did not yield a valid Ed25519 public key")
	}

	expiresAtEpoch := 0.0
	if expiry, ok := profile.ParseAccessTokenExpiry(strings.TrimSpace(prof.AccessTokenExpiresAt)); ok {
		expiresAtEpoch = float64(expiry.Unix())
	}
	authState := map[string]any{
		"username":         username,
		"agent_id":         strings.TrimSpace(prof.AgentID),
		"actor_id":         strings.TrimSpace(prof.ActorID),
		"key_id":           strings.TrimSpace(prof.KeyID),
		"public_key_b64":   base64.StdEncoding.EncodeToString(publicKey),
		"private_key_b64":  base64.StdEncoding.EncodeToString(privateKey.Seed()),
		"access_token":     strings.TrimSpace(prof.AccessToken),
		"refresh_token":    strings.TrimSpace(prof.RefreshToken),
		"token_type":       firstNonEmptyString(strings.TrimSpace(prof.TokenType), "Bearer"),
		"expires_at_epoch": expiresAtEpoch,
	}
	if err := writeBridgeJSONFile(configDetails.AuthStatePath, authState); err != nil {
		return nil, err
	}

	if err := bridgeUpdateAgentManifestIdentity(configDetails.AgentManifestPath, prof, publicKey); err != nil {
		return nil, err
	}
	wakeUpdated, err := bridgeUpdateWakeConfigBaseURL(configDetails.WakeConfigPath, prof.BaseURL)
	if err != nil {
		return nil, err
	}

	lines := []string{
		"Bridge auth imported.",
		"Config: " + configDetails.ConfigPath,
		"Agent home: " + configDetails.AgentHome,
		"Auth state: " + configDetails.AuthStatePath,
		"Source profile: " + profilePath,
		"Username: " + username,
		"Actor ID: " + strings.TrimSpace(prof.ActorID),
	}
	lines = append(lines, "Agent manifest identity updated: "+configDetails.AgentManifestPath)
	if wakeUpdated {
		lines = append(lines, "Wake config base URLs updated: "+configDetails.WakeConfigPath)
	}
	return &commandResult{
		Text: strings.Join(lines, "\n"),
		Data: map[string]any{
			"config_path":            configDetails.ConfigPath,
			"agent_home":             configDetails.AgentHome,
			"auth_state_path":        configDetails.AuthStatePath,
			"wake_config_path":       configDetails.WakeConfigPath,
			"profile_path":           profilePath,
			"profile_agent":          profileAgent,
			"username":               username,
			"actor_id":               strings.TrimSpace(prof.ActorID),
			"agent_id":               strings.TrimSpace(prof.AgentID),
			"key_id":                 strings.TrimSpace(prof.KeyID),
			"public_key_fingerprint": bridgePublicKeyFingerprint(publicKey),
		},
	}, nil
}

func (a *App) runBridgeInitConfig(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("bridge init-config")
	var kindFlag trackedString
	var outputFlag trackedString
	var agentHomeFlag trackedString
	var baseURLFlag trackedString
	var workspaceIDFlags trackedStrings
	var workspaceNameFlag trackedString
	var workspaceURLFlag trackedString
	var handleFlag trackedString
	var authStateFlag trackedString
	var stateDirFlag trackedString
	var adapterEntryFlag trackedString
	var openclawBinFlag trackedString
	var anxCLIBinFlag trackedString
	var pluginModuleFlag trackedString
	var pluginFactoryFlag trackedString
	var managedBridgeAutoFlag trackedBool
	fs.Var(&kindFlag, "kind", "Template kind: hermes, openclaw, subprocess, or python-plugin")
	fs.Var(&outputFlag, "output", "Write the rendered TOML to a file")
	fs.Var(&agentHomeFlag, "agent-home", "Directory for this local agent identity")
	fs.Var(&baseURLFlag, "base-url", "ANX base URL")
	fs.Var(&workspaceIDFlags, "workspace-id", "Durable ANX workspace id; optional when the active profile/core handshake exposes one; repeat for multi-workspace agents")
	fs.Var(&workspaceNameFlag, "workspace-name", "Display name for the workspace")
	fs.Var(&workspaceURLFlag, "workspace-url", "Workspace URL shown in listings")
	fs.Var(&handleFlag, "handle", "Agent handle for bridge templates")
	fs.Var(&authStateFlag, "auth-state-path", "Auth state path")
	fs.Var(&stateDirFlag, "state-dir", "Agent state dir")
	fs.Var(&openclawBinFlag, "openclaw-bin", "OpenClaw template: absolute path for [adapter].openclaw_bin; auto-detected when omitted")
	fs.Var(&anxCLIBinFlag, "anx-cli-bin", "OpenClaw template: absolute path for [adapter].anx_cli_bin; auto-detected when omitted")
	fs.Var(&adapterEntryFlag, "adapter-entrypoint", "Subprocess template: path for [adapter].command after python3")
	fs.Var(&pluginModuleFlag, "plugin-module", "python-plugin template: plugin_module value")
	fs.Var(&pluginFactoryFlag, "plugin-factory", "python-plugin template: plugin_factory value")
	fs.Var(&managedBridgeAutoFlag, "managed-package-auto-update", "Write [bridge].managed_package_auto_update=true in the template; when enabled, anx may pip refresh anx-agent-bridge toward the CLI-pinned git ref during doctor/start. Requires Python 3.11+, git on PATH, network access, macOS/Linux (same as anx bridge install).")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx bridge init-config`")
	}
	kind := normalizeBridgeInitConfigKind(kindFlag.value)
	baseURL := strings.TrimSpace(baseURLFlag.value)
	if baseURL == "" {
		baseURL = cfg.BaseURL
	}
	handle := strings.TrimSpace(handleFlag.value)
	if handle == "" {
		return nil, errnorm.Usage("invalid_request", "--handle is required")
	}
	if kind == "python_plugin" {
		if strings.TrimSpace(pluginModuleFlag.value) == "" {
			return nil, errnorm.Usage("invalid_request", "--plugin-module is required for python-plugin template")
		}
		if strings.TrimSpace(pluginFactoryFlag.value) == "" {
			return nil, errnorm.Usage("invalid_request", "--plugin-factory is required for python-plugin template")
		}
	}
	discoveryCfg := cfg
	discoveryCfg.BaseURL = baseURL
	if baseURLFlag.set {
		// A caller-provided base URL means the generated bridge config targets
		// that deployment, so a cached workspace id from the active profile may
		// be stale. Discover from the requested core unless the caller supplied
		// --workspace-id explicitly.
		discoveryCfg.WorkspaceID = ""
	}
	workspaceIDs := uniqueNonEmptyStrings(workspaceIDFlags.values)
	if len(workspaceIDs) == 0 {
		discovered, err := a.discoverBridgeWorkspaceIDs(ctx, discoveryCfg)
		if err != nil {
			return nil, err
		}
		workspaceIDs = discovered
	}
	workspaceName := strings.TrimSpace(workspaceNameFlag.value)
	if workspaceName == "" {
		workspaceName = "Main"
	}

	agentHome := strings.TrimSpace(agentHomeFlag.value)
	if agentHome == "" {
		agentHome = ".anx"
	}
	managedAuto := managedBridgeAutoFlag.set && managedBridgeAutoFlag.value
	rendered, handle, err := renderBridgeConfigTemplate(bridgeTemplateParams{
		Kind:                     kind,
		AgentHome:                agentHome,
		BaseURL:                  baseURL,
		WorkspaceIDs:             workspaceIDs,
		WorkspaceName:            workspaceName,
		WorkspaceURL:             strings.TrimSpace(workspaceURLFlag.value),
		Handle:                   handle,
		AuthStatePath:            strings.TrimSpace(authStateFlag.value),
		StateDir:                 strings.TrimSpace(stateDirFlag.value),
		AdapterEntrypoint:        strings.TrimSpace(adapterEntryFlag.value),
		OpenClawBin:              bridgeInitConfigOpenClawBin(strings.TrimSpace(openclawBinFlag.value)),
		ANXCLIBin:                bridgeInitConfigANXCLIBin(strings.TrimSpace(anxCLIBinFlag.value)),
		PluginModule:             strings.TrimSpace(pluginModuleFlag.value),
		PluginFactory:            strings.TrimSpace(pluginFactoryFlag.value),
		ManagedPackageAutoUpdate: managedAuto,
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
		outputAbs, err := filepath.Abs(outputPath)
		if err != nil {
			return nil, errnorm.Wrap(errnorm.KindLocal, "bridge_config_resolve_failed", "failed to resolve bridge config path", err)
		}
		agentHomePath, err := expandBridgePath(filepath.Dir(outputAbs), agentHome)
		if err != nil {
			return nil, err
		}
		agentManifest, wakeConfig := renderAgentHomeFiles(bridgeTemplateParams{
			Kind:                     kind,
			AgentHome:                agentHome,
			BaseURL:                  baseURL,
			WorkspaceIDs:             workspaceIDs,
			WorkspaceName:            workspaceName,
			WorkspaceURL:             strings.TrimSpace(workspaceURLFlag.value),
			Handle:                   handle,
			AuthStatePath:            strings.TrimSpace(authStateFlag.value),
			StateDir:                 strings.TrimSpace(stateDirFlag.value),
			AdapterEntrypoint:        strings.TrimSpace(adapterEntryFlag.value),
			PluginModule:             strings.TrimSpace(pluginModuleFlag.value),
			PluginFactory:            strings.TrimSpace(pluginFactoryFlag.value),
			ManagedPackageAutoUpdate: managedAuto,
		})
		if err := bridgeMkdirAll(agentHomePath, 0o700); err != nil {
			return nil, errnorm.Wrap(errnorm.KindLocal, "agent_home_create_failed", "failed to create agent home", err)
		}
		if err := bridgeWriteFile(filepath.Join(agentHomePath, "agent.toml"), []byte(agentManifest), 0o600); err != nil {
			return nil, errnorm.Wrap(errnorm.KindLocal, "agent_home_write_failed", "failed to write agent.toml", err)
		}
		if err := bridgeWriteFile(filepath.Join(agentHomePath, "wake.toml"), []byte(wakeConfig), 0o600); err != nil {
			return nil, errnorm.Wrap(errnorm.KindLocal, "agent_home_write_failed", "failed to write wake.toml", err)
		}
		textLines := []string{
			"Bridge config written.",
			"Kind: " + kind,
			"Path: " + outputPath,
			"Agent home: " + agentHomePath,
			"Wake config: " + filepath.Join(agentHomePath, "wake.toml"),
			"Lifecycle: registrations stay pending until the bridge checks in.",
			bridgeInitConfigNextStep(kind, outputPath),
		}
		text = strings.Join(textLines, "\n")
	}
	return &commandResult{
		Text: text,
		Data: map[string]any{
			"kind":                kind,
			"output":              outputPath,
			"agent_home":          agentHome,
			"workspace_ids":       workspaceIDs,
			"workspace_id_source": bridgeWorkspaceIDSource(workspaceIDFlags.values, discoveryCfg),
			"handle":              handle,
			"content":             rendered,
		},
	}, nil
}

func (a *App) discoverBridgeWorkspaceIDs(ctx context.Context, cfg config.Resolved) ([]string, error) {
	if workspaceID := strings.TrimSpace(cfg.WorkspaceID); workspaceID != "" {
		return []string{workspaceID}, nil
	}
	client, err := httpclient.New(cfg)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client for workspace discovery", err)
	}
	callCtx, cancel := httpclient.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	resp, err := client.RawCall(callCtx, httpclient.RawRequest{Method: http.MethodGet, Path: "/meta/handshake"})
	if err != nil {
		return nil, errnorm.WithDetails(
			errnorm.Wrap(errnorm.KindNetwork, "bridge_workspace_discovery_failed", "failed to discover workspace id from core handshake; pass --workspace-id explicitly", err),
			map[string]any{"base_url": cfg.BaseURL},
		)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, errnorm.WithDetails(
			errnorm.FromHTTPFailure(resp.StatusCode, resp.Body),
			map[string]any{"hint": "failed to discover workspace id from core handshake; pass --workspace-id explicitly"},
		)
	}
	var payload map[string]any
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		return nil, errnorm.Wrap(errnorm.KindRemote, "invalid_response", "core handshake response is not valid JSON", err)
	}
	workspaceID := strings.TrimSpace(anyString(payload["workspace_id"]))
	if workspaceID == "" {
		return nil, errnorm.WithDetails(
			errnorm.Local("bridge_workspace_discovery_missing", "core handshake did not expose workspace_id; pass --workspace-id explicitly"),
			map[string]any{"base_url": cfg.BaseURL},
		)
	}
	return []string{workspaceID}, nil
}

func bridgeWorkspaceIDSource(explicit []string, cfg config.Resolved) string {
	if len(uniqueNonEmptyStrings(explicit)) > 0 {
		return "flag"
	}
	if strings.TrimSpace(cfg.WorkspaceID) != "" {
		return "profile"
	}
	return "handshake"
}

func (a *App) runBridgeWorkspaceID(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("bridge workspace-id")
	var handleFlag trackedString
	fs.Var(&handleFlag, "handle", "Agent handle whose registration should be inspected")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx bridge workspace-id`")
	}

	handle := strings.TrimSpace(handleFlag.value)
	if handle == "" {
		return nil, errnorm.Usage("invalid_request", "--handle is required")
	}

	principal, err := a.lookupPrincipalByHandle(ctx, cfg, handle)
	if err != nil {
		return nil, err
	}
	if principal == nil {
		return nil, errnorm.WithDetails(
			errnorm.Local("bridge_workspace_id_missing", "no agent principal matched the requested handle"),
			map[string]any{"handle": handle},
		)
	}
	registration, _ := principal["registration"].(map[string]any)
	if registration == nil {
		return nil, errnorm.WithDetails(
			errnorm.Local("bridge_workspace_id_missing", "principal does not contain wake registration metadata"),
			map[string]any{"handle": handle, "agent_id": anyString(principal["agent_id"])},
		)
	}
	registrationStatus := anyString(registration["status"])
	actorID := anyString(principal["actor_id"])
	workspaceBindingsRaw, _ := registration["workspace_bindings"].([]any)
	workspaceBindings := make([]map[string]any, 0, len(workspaceBindingsRaw))
	workspaceIDs := make([]string, 0, len(workspaceBindingsRaw))
	seen := map[string]struct{}{}
	for _, item := range workspaceBindingsRaw {
		binding, ok := item.(map[string]any)
		if !ok {
			continue
		}
		workspaceID := strings.TrimSpace(anyString(binding["workspace_id"]))
		if workspaceID == "" {
			continue
		}
		workspaceBindings = append(workspaceBindings, binding)
		enabled := true
		if _, exists := binding["enabled"]; exists {
			enabled = asBool(binding["enabled"])
		}
		if !enabled {
			continue
		}
		if _, exists := seen[workspaceID]; exists {
			continue
		}
		seen[workspaceID] = struct{}{}
		workspaceIDs = append(workspaceIDs, workspaceID)
	}
	if len(workspaceIDs) == 0 {
		return nil, errnorm.WithDetails(
			errnorm.Local("bridge_workspace_id_missing", "registration does not contain any enabled workspace bindings"),
			map[string]any{
				"agent_id":           anyString(principal["agent_id"]),
				"handle":             handle,
				"workspace_bindings": workspaceBindings,
			},
		)
	}

	lines := []string{
		"Bridge workspace id discovery",
		"Agent ID: " + anyString(principal["agent_id"]),
	}
	if handle != "" {
		lines = append(lines, "Handle: "+handle)
	}
	if actorID != "" {
		lines = append(lines, "Actor ID: "+actorID)
	}
	if registrationStatus != "" {
		lines = append(lines, "Registration status: "+registrationStatus)
	}
	lines = append(lines, "Workspace IDs:")
	for _, workspaceID := range workspaceIDs {
		lines = append(lines, "- "+workspaceID)
	}
	if handle != "" {
		repeated := make([]string, 0, len(workspaceIDs))
		for _, workspaceID := range workspaceIDs {
			repeated = append(repeated, "--workspace-id "+workspaceID)
		}
		lines = append(lines, "Next step: anx bridge init-config --kind hermes --output ./bridge.toml --agent-home ./.anx "+strings.Join(repeated, " ")+" --handle "+handle)
		lines = append(lines, "Or: --kind openclaw for OpenClaw, --kind subprocess with --adapter-entrypoint for a custom JSON adapter, or --kind python-plugin with --plugin-module and --plugin-factory.")
	}
	return &commandResult{
		Text: strings.Join(lines, "\n"),
		Data: map[string]any{
			"agent_id":            anyString(principal["agent_id"]),
			"handle":              handle,
			"actor_id":            actorID,
			"registration_status": registrationStatus,
			"workspace_ids":       workspaceIDs,
			"workspace_bindings":  workspaceBindings,
		},
	}, nil
}

func (a *App) lookupPrincipalByHandle(ctx context.Context, cfg config.Resolved, handle string) (map[string]any, error) {
	cursor := ""
	seenCursors := map[string]struct{}{}
	client, err := httpclient.New(cfg)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}
	for {
		requestPath := "/auth/principals?limit=200"
		if cursor != "" {
			requestPath += "&cursor=" + url.QueryEscape(cursor)
		}
		callCtx, cancel := httpclient.WithTimeout(ctx, cfg.Timeout)
		resp, callErr := client.RawCall(callCtx, httpclient.RawRequest{Method: http.MethodGet, Path: requestPath, Headers: generatedHeaders(cfg)})
		cancel()
		if callErr != nil {
			return nil, errnorm.Wrap(errnorm.KindNetwork, "request_failed", "auth principals list request failed", callErr)
		}
		if resp.StatusCode >= 400 {
			return nil, errnorm.FromHTTPFailure(resp.StatusCode, resp.Body)
		}
		var payload map[string]any
		if err := json.Unmarshal(resp.Body, &payload); err != nil {
			return nil, err
		}
		body := payload
		principalsRaw, _ := body["principals"].([]any)
		for _, item := range principalsRaw {
			candidate, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if strings.TrimSpace(anyString(candidate["username"])) == handle {
				return candidate, nil
			}
		}
		nextCursor := strings.TrimSpace(anyString(body["next_cursor"]))
		if nextCursor == "" {
			return nil, nil
		}
		if _, exists := seenCursors[nextCursor]; exists {
			return nil, errnorm.Local("bridge_workspace_id_invalid_cursor", "auth principals list repeated a pagination cursor")
		}
		seenCursors[nextCursor] = struct{}{}
		cursor = nextCursor
	}
}

func (a *App) runBridgeDoctor(ctx context.Context, args []string) (*commandResult, error) {
	fs := newSilentFlagSet("bridge doctor")
	var pythonFlag trackedString
	var installDirFlag trackedString
	var binDirFlag trackedString
	var configFlag trackedString
	fs.Var(&pythonFlag, "python", "Preferred Python executable")
	fs.Var(&installDirFlag, "install-dir", "Root directory for the managed bridge virtualenv")
	fs.Var(&binDirFlag, "bin-dir", "Directory where the anx-agent-bridge wrapper should exist")
	fs.Var(&configFlag, "config", "Bridge config to validate")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx bridge doctor`")
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
		st := "pass"
		if !ok {
			hasFailure = true
			st = "fail"
		}
		checks = append(checks, bridgeDoctorCheck{Name: name, OK: ok, Message: message, Status: st})
	}
	addWarn := func(name, message string) {
		checks = append(checks, bridgeDoctorCheck{Name: name, OK: true, Message: message, Status: "warn"})
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

	bridgeBinary := filepath.Join(binDir, "anx-agent-bridge")
	if _, err := bridgeStat(bridgeBinary); err != nil {
		lookup, lookupErr := bridgeLookPath("anx-agent-bridge")
		if lookupErr != nil {
			addCheck("bridge_binary", false, "anx-agent-bridge wrapper not found; run `anx bridge install`")
		} else {
			bridgeBinary = lookup
			addCheck("bridge_binary", true, "resolved from PATH at "+lookup)
		}
	} else {
		addCheck("bridge_binary", true, "managed wrapper present at "+bridgeBinary)
	}

	var (
		bridgeVersionProbeOK bool
		bridgeVersionLine    string
	)
	if !hasFailure || (checks[len(checks)-1].Name == "bridge_binary" && checks[len(checks)-1].OK) {
		out, probeErr := probeBridgeBinaryOutput(ctx, bridgeBinary)
		if probeErr != nil {
			addCheck("bridge_version", false, errnorm.Normalize(probeErr).Message)
		} else {
			bridgeVersionLine = strings.TrimSpace(out)
			bridgeVersionProbeOK = true
			addCheck("bridge_version", true, bridgeVersionLine)
		}
	}

	registrationData := map[string]any{}
	configPath := strings.TrimSpace(configFlag.value)
	if configPath != "" {
		if _, err := bridgeStat(configPath); err != nil {
			addCheck("config", false, "config file not found: "+configPath)
		} else {
			addCheck("config", true, "config file present: "+configPath)
			if bridgeVersionProbeOK {
				details, derr := loadBridgeConfigDetails(configPath)
				if derr != nil {
					addCheck("managed_bridge_package", false, "failed loading bridge config for alignment policy: "+errnorm.Normalize(derr).Message)
				} else {
					expectedSem := normalizedBridgeCLIExpectedPackageSemver(defaultBridgeInstallRef())
					installedSem := parsedBridgeInstalledPackageSemver(bridgeVersionLine)
					if strings.TrimSpace(expectedSem) == "" {
						addWarn("managed_bridge_package", "could not derive expected bridge package semver from this CLI release tag")
					} else if strings.TrimSpace(installedSem) == "" {
						addWarn("managed_bridge_package", fmt.Sprintf(`could not parse installed anx-agent-bridge semver from reported version line %q`, bridgeVersionLine))
					} else {
						mism := bridgePackageSemverMismatch(installedSem, expectedSem)
						managedLayout := bridgeResolvedBinaryUsesManagedInstallLayout(bridgeBinary, binDir) && bridgeManagedVenvExists(installDir)
						switch {
						case mism && !managedLayout:
							addWarn("managed_bridge_package", fmt.Sprintf(`installed semver %s is behind CLI pinned release (%s); non-managed anx-agent-bridge resolved (not the wrapper at %s); automatic pip refresh skipped`, installedSem, expectedSem, filepath.Join(binDir, "anx-agent-bridge")))
						case mism && managedLayout && !details.ManagedPackageAutoUpdate:
							addWarn("managed_bridge_package", fmt.Sprintf(`managed anx-agent-bridge semver %s is behind CLI pinned release (%s); run anx bridge install (or set [bridge].managed_package_auto_update = true)`, installedSem, expectedSem))
						case mism && managedLayout && details.ManagedPackageAutoUpdate && runtime.GOOS == "windows":
							addWarn("managed_bridge_package", "managed_package_auto_update is enabled but automatic refresh runs on macOS/Linux only")
						case mism && managedLayout && details.ManagedPackageAutoUpdate:
							if refrErr := a.refreshManagedBridgeDefaultInstall(ctx, strings.TrimSpace(pythonFlag.value), installDir, binDir, defaultBridgeInstallRef()); refrErr != nil {
								addCheck("managed_bridge_package", false, "managed_package_auto_update is enabled but refresh failed: "+errnorm.Normalize(refrErr).Message)
							} else {
								freshLine, rfErr := probeBridgeBinaryOutput(ctx, bridgeBinary)
								if rfErr != nil {
									addCheck("managed_bridge_package", false, "refresh completed but probing version failed: "+errnorm.Normalize(rfErr).Message)
								} else {
									freshSem := parsedBridgeInstalledPackageSemver(freshLine)
									if bridgePackageSemverMismatch(freshSem, expectedSem) {
										addWarn("managed_bridge_package", fmt.Sprintf(`refresh ran but semver still reports %s while CLI pins %s; inspect logs or rerun anx bridge install`, freshSem, expectedSem))
									} else {
										addCheck("managed_bridge_package", true, fmt.Sprintf(`refreshed managed anx-agent-bridge to CLI pinned release (%s)`, expectedSem))
									}
								}
							}
						default:
							if managedLayout {
								addCheck("managed_bridge_package", true, fmt.Sprintf("managed anx-agent-bridge semver matches CLI expectation (%s)", expectedSem))
							} else {
								addCheck("managed_bridge_package", true, fmt.Sprintf("anx-agent-bridge semver %s matches CLI expectation (%s) on PATH", installedSem, expectedSem))
							}
						}
					}
				}
			}
			adapterCtx, cancelAdapter := context.WithTimeout(ctx, bridgeDoctorChildTimeout)
			adapterOut, adapterErr := runBridgeExternalOutput(adapterCtx, bridgeBinary, "bridge", "doctor", "--config", configPath)
			cancelAdapter()
			if adapterErr != nil {
				addCheck("adapter", false, errnorm.Normalize(adapterErr).Message)
			} else {
				var adapterData map[string]any
				if err := json.Unmarshal([]byte(adapterOut), &adapterData); err != nil {
					addCheck("adapter", false, "adapter doctor stdout was not valid JSON: "+err.Error())
				} else {
					addCheck("adapter", true, "adapter readiness probe passed")
					registrationData["adapter"] = adapterData
				}
			}
			statusCtx, cancelStatus := context.WithTimeout(ctx, bridgeDoctorChildTimeout)
			statusOut, statusErr := runBridgeExternalOutput(statusCtx, bridgeBinary, "registration", "status", "--config", configPath)
			cancelStatus()
			if statusErr != nil {
				addCheck("registration", false, errnorm.Normalize(statusErr).Message)
			} else {
				if err := json.Unmarshal([]byte(statusOut), &registrationData); err != nil {
					addCheck("registration", false, "failed to parse registration status output")
				} else if wakeable, _ := registrationData["wakeable"].(bool); wakeable {
					addCheck("registration", true, "bridge is online for immediate tagged delivery")
				} else {
					message := "bridge is offline or not ready for live delivery; tags will queue durable notifications"
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
		code := strings.ToLower(strings.TrimSpace(check.Status))
		if code == "" {
			if check.OK {
				code = "pass"
			} else {
				code = "fail"
			}
		}
		lines = append(lines, "["+strings.ToUpper(code)+"] "+check.Name+": "+check.Message)
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
	Kind                     string
	AgentHome                string
	BaseURL                  string
	WorkspaceIDs             []string
	WorkspaceName            string
	WorkspaceURL             string
	Handle                   string
	AuthStatePath            string
	StateDir                 string
	AdapterEntrypoint        string
	OpenClawBin              string
	ANXCLIBin                string
	PluginModule             string
	PluginFactory            string
	ManagedPackageAutoUpdate bool
}

func bridgeTomlSnippetManagedAutoUpdate(enabled bool) string {
	if !enabled {
		return ""
	}
	return `

` + "# managed_package_auto_update: when true, anx may pip refresh anx-agent-bridge to the CLI-pinned git ref during doctor/start (and optional `anx update --bridge-config`)." + `
` + "# Requires Python 3.11+, git on PATH, outbound network access, macOS/Linux (same prerequisites as anx bridge install)." + `
managed_package_auto_update = true`
}

func normalizeBridgeInitConfigKind(kind string) string {
	k := strings.ToLower(strings.TrimSpace(kind))
	if k == "" {
		return "subprocess"
	}
	switch k {
	case "python-plugin", "python_plugin":
		return "python_plugin"
	case "hermes":
		return "hermes"
	case "openclaw":
		return "openclaw"
	case "subprocess":
		return "subprocess"
	default:
		return k
	}
}

func bridgeInitConfigOpenClawBin(raw string) string {
	if strings.TrimSpace(raw) != "" {
		return raw
	}
	if path, err := exec.LookPath("openclaw"); err == nil {
		return path
	}
	return ""
}

func bridgeInitConfigANXCLIBin(raw string) string {
	if strings.TrimSpace(raw) != "" {
		return raw
	}
	if exe, err := os.Executable(); err == nil && filepath.IsAbs(exe) {
		return exe
	}
	if path, err := exec.LookPath("anx"); err == nil {
		return path
	}
	return ""
}

func renderBridgeConfigTemplate(params bridgeTemplateParams) (string, string, error) {
	agentHome := firstNonEmptyString(params.AgentHome, ".anx")
	kind := normalizeBridgeInitConfigKind(params.Kind)
	switch kind {
	case "hermes":
		handle := firstNonEmptyString(params.Handle, "<handle>")
		stateDir := firstNonEmptyString(params.StateDir, "run/"+handle)
		return strings.TrimSpace(fmt.Sprintf(`
agent_home = %q
wake_config = "wake.toml"

[bridge]
driver_kind = "hermes"
adapter_kind = "hermes"
resume_policy = "resume_or_create"
status = "pending"
checkin_interval_seconds = 60
checkin_ttl_seconds = 300%s

[runtime]
state_dir = %q

[adapter]
kind = "hermes"
timeout_seconds = 900
doctor_timeout_seconds = 60
# Hermes wake handling should use ACP so final answers stay separated from
# internal thought/tool progress before ANX message_posted writeback.
# Optional overrides:
# command = ["python3", "-m", "anx_agent_bridge.adapters.hermes_acp"]
# hermes_bin = "/path/to/hermes"
# hermes_args = ["acp"]
# cwd = "."
# interactive = false
`, agentHome, bridgeTomlSnippetManagedAutoUpdate(params.ManagedPackageAutoUpdate), stateDir)) + "\n", handle, nil
	case "openclaw":
		handle := firstNonEmptyString(params.Handle, "<handle>")
		stateDir := firstNonEmptyString(params.StateDir, "run/"+handle)
		var openclawBinLine string
		if strings.TrimSpace(params.OpenClawBin) != "" {
			openclawBinLine = fmt.Sprintf("openclaw_bin = %q\n", strings.TrimSpace(params.OpenClawBin))
		} else {
			openclawBinLine = "# openclaw_bin = \"/absolute/path/to/openclaw\"\n"
		}
		var anxCLIBinLine string
		if strings.TrimSpace(params.ANXCLIBin) != "" {
			anxCLIBinLine = fmt.Sprintf("anx_cli_bin = %q\n", strings.TrimSpace(params.ANXCLIBin))
		} else {
			anxCLIBinLine = "# anx_cli_bin = \"/absolute/path/to/anx\"\n"
		}
		return strings.TrimSpace(fmt.Sprintf(`
agent_home = %q
wake_config = "wake.toml"

[bridge]
driver_kind = "openclaw"
adapter_kind = "openclaw"
resume_policy = "resume_or_create"
status = "pending"
checkin_interval_seconds = 60
checkin_ttl_seconds = 300%s

[runtime]
state_dir = %q

[adapter]
kind = "openclaw"
timeout_seconds = 900
doctor_timeout_seconds = 60
%s%sopenclaw_timeout_seconds = 300
# OpenClaw wake handling uses a fresh --session-id for each wake so a busy
# gateway/main session cannot deadlock agent delivery.
# Optional override:
# command = ["python3", "-m", "anx_agent_bridge.adapters.openclaw"]
`, agentHome, bridgeTomlSnippetManagedAutoUpdate(params.ManagedPackageAutoUpdate), stateDir, openclawBinLine, anxCLIBinLine)) + "\n", handle, nil
	case "subprocess":
		handle := firstNonEmptyString(params.Handle, "<handle>")
		stateDir := firstNonEmptyString(params.StateDir, "run/"+handle)
		entry := firstNonEmptyString(params.AdapterEntrypoint, "./adapter.py")
		return strings.TrimSpace(fmt.Sprintf(`
agent_home = %q
wake_config = "wake.toml"

[bridge]
driver_kind = "custom"
adapter_kind = "subprocess"
resume_policy = "resume_or_create"
status = "pending"
checkin_interval_seconds = 60
checkin_ttl_seconds = 300%s

[runtime]
state_dir = %q

[adapter]
kind = "subprocess"
command = ["python3", %q]
timeout_seconds = 600
doctor_timeout_seconds = 60
`, agentHome, bridgeTomlSnippetManagedAutoUpdate(params.ManagedPackageAutoUpdate), stateDir, entry)) + "\n", handle, nil
	case "python_plugin":
		handle := firstNonEmptyString(params.Handle, "<handle>")
		stateDir := firstNonEmptyString(params.StateDir, "run/"+handle)
		mod := strings.TrimSpace(params.PluginModule)
		fac := strings.TrimSpace(params.PluginFactory)
		return strings.TrimSpace(fmt.Sprintf(`
agent_home = %q
wake_config = "wake.toml"

[bridge]
driver_kind = "custom"
adapter_kind = "python_plugin"
resume_policy = "resume_or_create"
status = "pending"
checkin_interval_seconds = 60
checkin_ttl_seconds = 300%s

[runtime]
state_dir = %q

[adapter]
kind = "python_plugin"
plugin_module = %q
plugin_factory = %q
`, agentHome, bridgeTomlSnippetManagedAutoUpdate(params.ManagedPackageAutoUpdate), stateDir, mod, fac)) + "\n", handle, nil
	default:
		return "", "", errnorm.Usage("invalid_request", "unknown bridge config kind; use hermes, openclaw, subprocess, or python-plugin")
	}
}

func bridgeInitConfigNextStep(kind string, outputPath string) string {
	switch normalizeBridgeInitConfigKind(kind) {
	case "hermes":
		return "Next: run `anx bridge doctor --config " + outputPath + "` to verify Hermes and ACP readiness."
	case "openclaw":
		return "Next: run `anx bridge doctor --config " + outputPath + "` to verify OpenClaw gateway readiness."
	}
	return "Next: implement your adapter (subprocess JSON or python_plugin), then `anx-agent-bridge adapter contract --config " + outputPath + "`."
}

func renderAgentHomeFiles(params bridgeTemplateParams) (agentManifest string, wakeConfig string) {
	baseURL := firstNonEmptyString(params.BaseURL, "http://127.0.0.1:8000")
	handle := firstNonEmptyString(params.Handle, "<handle>")
	authState := firstNonEmptyString(params.AuthStatePath, "profiles/default.json")
	workspaceName := firstNonEmptyString(params.WorkspaceName, "Main")
	workspaceURL := strings.TrimSpace(params.WorkspaceURL)
	var wake strings.Builder
	wake.WriteString("schema_version = 1\n\n")
	for i, workspaceID := range uniqueNonEmptyStrings(params.WorkspaceIDs) {
		name := workspaceName
		if i > 0 {
			name = workspaceID
		}
		wake.WriteString("[[workspaces]]\n")
		wake.WriteString("id = " + strconv.Quote(workspaceID) + "\n")
		wake.WriteString("name = " + strconv.Quote(name) + "\n")
		wake.WriteString("base_url = " + strconv.Quote(baseURL) + "\n")
		if workspaceURL != "" && i == 0 {
			wake.WriteString("url = " + strconv.Quote(workspaceURL) + "\n")
		}
		wake.WriteString("enabled = true\n\n")
	}
	manifest := strings.TrimSpace(fmt.Sprintf(`
schema_version = 1
agent_home_id = %q
profile = "default"

[identity]
base_url = %q
handle = %q
agent_id = ""
actor_id = ""
key_id = ""
public_key_fingerprint = ""
verify_ssl = true

[auth]
state_path = %q

[wake]
config_path = "wake.toml"
`, "agenthome_"+shortBridgeHash(handle+"|"+baseURL), baseURL, handle, authState)) + "\n"
	return manifest, strings.TrimSpace(wake.String()) + "\n"
}

func uniqueNonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
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
	return filepath.Join(home, ".local", "share", "anx", "agent-bridge")
}

func bridgeDefaultBinDir(home string) string {
	return filepath.Join(home, ".local", "bin")
}

func bridgeInstallPackageSpec(ref string) string {
	return fmt.Sprintf("git+%s@%s#subdirectory=adapters/agent-bridge", bridgeRepoURL, ref)
}

func defaultBridgeInstallRef() string {
	// Pin to the same tag as this CLI build so `anx bridge install` pulls a bridge
	// snapshot that matches the released binary (including adapter surface area).
	// Development: pass `--ref main` (or a feature branch) when iterating ahead of the tag.
	return strings.TrimSpace(buildinfo.Current)
}

type managedBridgeInstallOpts struct {
	Home            string
	PreferredPython string
	InstallDir      string
	BinDir          string
	Ref             string
	WithDev         bool
}

type managedBridgeInstallResult struct {
	InstallDir    string
	BinDir        string
	VenvPython    string
	BridgeBinary  string
	WrapperPath   string
	PackageRef    string
	VersionLine   string
	PythonRuntime bridgePythonRuntime
}

func (a *App) performManagedBridgeInstall(ctx context.Context, opts managedBridgeInstallOpts) (managedBridgeInstallResult, error) {
	installDir := strings.TrimSpace(opts.InstallDir)
	if installDir == "" {
		installDir = bridgeDefaultInstallDir(opts.Home)
	}
	binDir := strings.TrimSpace(opts.BinDir)
	if binDir == "" {
		binDir = bridgeDefaultBinDir(opts.Home)
	}
	pythonRuntime, err := detectBridgePython(ctx, strings.TrimSpace(opts.PreferredPython))
	if err != nil {
		return managedBridgeInstallResult{}, err
	}
	if _, err := bridgeLookPath("git"); err != nil {
		return managedBridgeInstallResult{}, errnorm.Local("git_required", "`anx bridge install` currently requires `git` on PATH because it installs the bridge package from the GitHub repo")
	}
	venvDir := filepath.Join(installDir, ".venv")
	venvPython := filepath.Join(venvDir, "bin", "python")
	pkgBridgeBinary := filepath.Join(venvDir, "bin", "anx-agent-bridge")
	ref := strings.TrimSpace(opts.Ref)
	if ref == "" {
		ref = defaultBridgeInstallRef()
	}
	if err := bridgeMkdirAll(installDir, 0o755); err != nil {
		return managedBridgeInstallResult{}, errnorm.Wrap(errnorm.KindLocal, "bridge_install_dir_failed", "failed to create bridge install directory", err)
	}
	if err := runBridgeExternal(ctx, pythonRuntime.Command, "-m", "venv", venvDir); err != nil {
		return managedBridgeInstallResult{}, err
	}
	if err := runBridgeExternal(ctx, venvPython, "-m", "pip", "install", "--upgrade", "pip"); err != nil {
		return managedBridgeInstallResult{}, err
	}
	if err := runBridgeExternal(ctx, venvPython, "-m", "pip", "install", bridgeInstallPackageSpec(ref)); err != nil {
		return managedBridgeInstallResult{}, err
	}
	if opts.WithDev {
		if err := runBridgeExternal(ctx, venvPython, "-m", "pip", "install", "pytest>=8,<9"); err != nil {
			return managedBridgeInstallResult{}, err
		}
	}
	if err := bridgeMkdirAll(binDir, 0o755); err != nil {
		return managedBridgeInstallResult{}, errnorm.Wrap(errnorm.KindLocal, "bridge_bin_dir_failed", "failed to create bridge bin directory", err)
	}
	wrapperPath := filepath.Join(binDir, "anx-agent-bridge")
	if err := bridgeWriteLauncher(wrapperPath, pkgBridgeBinary); err != nil {
		return managedBridgeInstallResult{}, err
	}
	versionOut, err := probeBridgeBinaryOutput(ctx, pkgBridgeBinary)
	if err != nil {
		return managedBridgeInstallResult{}, err
	}
	return managedBridgeInstallResult{
		InstallDir:    installDir,
		BinDir:        binDir,
		VenvPython:    venvPython,
		BridgeBinary:  pkgBridgeBinary,
		WrapperPath:   wrapperPath,
		PackageRef:    ref,
		VersionLine:   strings.TrimSpace(versionOut),
		PythonRuntime: pythonRuntime,
	}, nil
}

func normalizedBridgeCLIExpectedPackageSemver(gitRefOrTag string) string {
	return strings.TrimPrefix(strings.TrimSpace(strings.ToLower(gitRefOrTag)), "v")
}

func parsedBridgeInstalledPackageSemver(versionLine string) string {
	line := strings.TrimSpace(versionLine)
	if line == "" {
		return ""
	}
	parts := strings.Fields(line)
	if len(parts) >= 2 && strings.EqualFold(parts[0], "anx-agent-bridge") {
		return normalizedBridgeCLIExpectedPackageSemver(parts[1])
	}
	if len(parts) == 1 {
		return normalizedBridgeCLIExpectedPackageSemver(parts[0])
	}
	if len(parts) >= 2 {
		return normalizedBridgeCLIExpectedPackageSemver(parts[len(parts)-1])
	}
	return ""
}

func bridgePackageSemverMismatch(installedComparable, expectedComparable string) bool {
	if strings.TrimSpace(expectedComparable) == "" || strings.TrimSpace(installedComparable) == "" {
		return false
	}
	return installedComparable != expectedComparable
}

func bridgeManagedVenvExists(installDir string) bool {
	venvPython := filepath.Join(installDir, ".venv", "bin", "python")
	if _, err := bridgeStat(venvPython); err != nil {
		return false
	}
	return true
}

func bridgeResolvedBinaryUsesManagedInstallLayout(bridgeBinary, binDir string) bool {
	wrapperPath := filepath.Join(binDir, "anx-agent-bridge")
	bp, err1 := filepath.Abs(bridgeBinary)
	wp, err2 := filepath.Abs(wrapperPath)
	if err1 != nil || err2 != nil {
		return strings.TrimSpace(bridgeBinary) == strings.TrimSpace(wrapperPath)
	}
	return bp == wp
}

func (a *App) refreshManagedBridgeDefaultInstall(ctx context.Context, preferredPython string, installDir string, binDir string, gitRef string) error {
	if runtime.GOOS == "windows" {
		return errnorm.Usage("unsupported_platform", "managed bridge install refresh supports macOS and Linux only")
	}
	home, err := a.bridgeHome()
	if err != nil {
		return err
	}
	_, err = a.performManagedBridgeInstall(ctx, managedBridgeInstallOpts{
		Home:            home,
		PreferredPython: preferredPython,
		InstallDir:      installDir,
		BinDir:          binDir,
		Ref:             gitRef,
		WithDev:         false,
	})
	return err
}

// managedBridgeStartupAlignment runs optionally before bridge start when version skew exists.
func (a *App) managedBridgeStartupAlignment(ctx context.Context, mc bridgeManagedConfig, bridgeBinary string, home string, installDirRaw, binDirRaw, pythonRaw string) ([]string, error) {
	if runtime.GOOS == "windows" {
		return nil, nil
	}
	installDir := strings.TrimSpace(installDirRaw)
	if installDir == "" {
		installDir = bridgeDefaultInstallDir(home)
	}
	binDir := strings.TrimSpace(binDirRaw)
	if binDir == "" {
		binDir = bridgeDefaultBinDir(home)
	}
	expectedSem := normalizedBridgeCLIExpectedPackageSemver(defaultBridgeInstallRef())
	if strings.TrimSpace(expectedSem) == "" {
		return nil, nil
	}
	versionLine, probeErr := probeBridgeBinaryOutput(ctx, bridgeBinary)
	if probeErr != nil {
		return nil, probeErr
	}
	installedSem := parsedBridgeInstalledPackageSemver(versionLine)
	if strings.TrimSpace(installedSem) == "" {
		return nil, nil
	}
	mism := bridgePackageSemverMismatch(installedSem, expectedSem)
	if !mism {
		return nil, nil
	}
	managedLayout := bridgeResolvedBinaryUsesManagedInstallLayout(bridgeBinary, binDir) && bridgeManagedVenvExists(installDir)
	if !managedLayout {
		w := filepath.Join(binDir, "anx-agent-bridge")
		return []string{
			fmt.Sprintf("Warning: anx-agent-bridge reports %s; this CLI pins %s. Non-managed binary (not wrapper at %s); skipped automatic pip refresh. Run anx bridge install to align.", installedSem, expectedSem, w),
		}, nil
	}
	if !mc.ManagedPackageAutoUpdate {
		return []string{
			fmt.Sprintf("Warning: managed anx-agent-bridge %s is behind CLI pinned release (%s); run anx bridge install (or enable [bridge].managed_package_auto_update in bridge.toml)", installedSem, expectedSem),
		}, nil
	}
	ref := strings.TrimSpace(defaultBridgeInstallRef())
	if err := a.refreshManagedBridgeDefaultInstall(ctx, strings.TrimSpace(pythonRaw), installDir, binDir, ref); err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "bridge_managed_refresh_failed", fmt.Sprintf(`managed_package_auto_update is enabled but anx-agent-bridge refresh failed (%s)`, defaultBridgeInstallRef()), err)
	}
	fresh, err := probeBridgeBinaryOutput(ctx, bridgeBinary)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "bridge_refresh_probe_failed", "failed probing anx-agent-bridge after refresh", err)
	}
	freshSem := parsedBridgeInstalledPackageSemver(fresh)
	if bridgePackageSemverMismatch(freshSem, expectedSem) {
		return nil, errnorm.Local("bridge_managed_refresh_failed", fmt.Sprintf(`anx-agent-bridge still reports %s after pip refresh while CLI pins %s; run anx bridge install`, freshSem, expectedSem))
	}
	return []string{"Refreshed managed anx-agent-bridge to CLI pinned release (" + expectedSem + ")."}, nil
}

// reconcileBridgePkgAfterCliUpdate runs after upgrading the anx binary using new release tag expectations.
func (a *App) reconcileBridgePkgAfterCliUpdate(ctx context.Context, bridgeConfigPath string, targetReleaseTagWithVPrefix string, pythonPref string) ([]string, error) {
	path := strings.TrimSpace(bridgeConfigPath)
	if path == "" {
		return nil, nil
	}
	details, err := loadBridgeConfigDetails(path)
	if err != nil {
		return nil, err
	}
	targetTag := normalizeReleaseTag(targetReleaseTagWithVPrefix)
	expectedSem := normalizedBridgeCLIExpectedPackageSemver(targetTag)
	expLine := ""
	if strings.TrimSpace(expectedSem) != "" {
		expLine = fmt.Sprintf("New CLI release expects anx-agent-bridge semver %s; run anx bridge install to align.", expectedSem)
	} else if strings.TrimSpace(targetTag) != "" {
		expLine = fmt.Sprintf("New CLI release is tagged %s; run anx bridge install to align anx-agent-bridge.", targetTag)
	}
	if !details.ManagedPackageAutoUpdate {
		if expLine != "" {
			return []string{expLine}, nil
		}
		return []string{"If you rely on anx bridge install/managed layouts, rerun anx bridge install after updating the anx binary."}, nil
	}
	if runtime.GOOS == "windows" {
		msg := ""
		if expLine != "" {
			msg = expLine + " "
		}
		return []string{strings.TrimSpace(msg + "automatic managed bridge refresh skipped on windows")}, nil
	}
	home, err := a.bridgeHome()
	if err != nil {
		return nil, err
	}
	installDir := bridgeDefaultInstallDir(home)
	binDir := bridgeDefaultBinDir(home)
	bridgeBinary, err := resolveBridgeBinary(home, installDir, binDir)
	if err != nil {
		return []string{"Managed bridge refresh after CLI update skipped: " + errnorm.Normalize(err).Message}, nil
	}
	versionLine, probeErr := probeBridgeBinaryOutput(ctx, bridgeBinary)
	if probeErr != nil {
		return []string{"Managed bridge refresh after CLI update skipped: " + errnorm.Normalize(probeErr).Message}, nil
	}
	installedSem := parsedBridgeInstalledPackageSemver(versionLine)
	if strings.TrimSpace(expectedSem) != "" && !bridgePackageSemverMismatch(installedSem, expectedSem) {
		return []string{"Managed anx-agent-bridge already matches CLI release expectation (" + expectedSem + ")."}, nil
	}
	managedLayout := bridgeResolvedBinaryUsesManagedInstallLayout(bridgeBinary, binDir) && bridgeManagedVenvExists(installDir)
	if !managedLayout {
		lines := []string{
			fmt.Sprintf(`Managed auto-update skipped: resolved anx-agent-bridge is not the default managed wrapper at %s`, filepath.Join(binDir, "anx-agent-bridge")),
		}
		if strings.TrimSpace(expLine) != "" {
			lines = append(lines, expLine)
		}
		return lines, nil
	}
	if err := a.refreshManagedBridgeDefaultInstall(ctx, strings.TrimSpace(pythonPref), installDir, binDir, targetTag); err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "bridge_managed_refresh_failed", "managed anx-agent-bridge refresh after anx update failed", err)
	}
	freshLine, ferr := probeBridgeBinaryOutput(ctx, bridgeBinary)
	if ferr != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "bridge_refresh_probe_failed", "failed probing anx-agent-bridge after update refresh", ferr)
	}
	freshSem := parsedBridgeInstalledPackageSemver(freshLine)
	if strings.TrimSpace(expectedSem) != "" && bridgePackageSemverMismatch(freshSem, expectedSem) {
		return nil, errnorm.Local("bridge_managed_refresh_failed", fmt.Sprintf(`anx-agent-bridge still reports %s after refresh while new CLI pins %s; run anx bridge install`, freshSem, expectedSem))
	}
	return []string{fmt.Sprintf(`Refreshed managed anx-agent-bridge for CLI release expectation (%s; installed semver %s).`, expectedSem, freshSem)}, nil
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
	return bridgePythonRuntime{}, errnorm.Local("python_unsupported", "Python 3.11+ is required for `anx-agent-bridge`; pass --python <exe> if needed")
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
		firstLine = "anx-agent-bridge --help"
	}
	return firstLine, nil
}

func defaultBridgeCommandRun(ctx context.Context, name string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return stdout.String(), stderr.String(), err
	}
	return stdout.String(), stderr.String(), nil
}

func bridgeWriteLauncher(path string, bridgeBinary string) error {
	content := "#!/bin/sh\nexec " + shellSingleQuote(bridgeBinary) + ` "$@"` + "\n"
	if err := bridgeWriteFile(path, []byte(content), 0o755); err != nil {
		return errnorm.Wrap(errnorm.KindLocal, "bridge_wrapper_write_failed", "failed to write anx-agent-bridge launcher", err)
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

type bridgeConfigDetails struct {
	ConfigPath               string
	AgentHome                string
	AgentManifestPath        string
	WakeConfigPath           string
	AuthStatePath            string
	AgentHandle              string
	BaseURL                  string
	ManagedPackageAutoUpdate bool
}

func loadBridgeConfigDetails(configPath string) (bridgeConfigDetails, error) {
	absPath, err := filepath.Abs(configPath)
	if err != nil {
		return bridgeConfigDetails{}, errnorm.Wrap(errnorm.KindLocal, "bridge_config_resolve_failed", "failed to resolve bridge config path", err)
	}
	content, err := bridgeReadFile(absPath)
	if err != nil {
		return bridgeConfigDetails{}, errnorm.Wrap(errnorm.KindLocal, "bridge_config_read_failed", "failed to read bridge config", err)
	}
	var root map[string]any
	if err := toml.Unmarshal(content, &root); err != nil {
		return bridgeConfigDetails{}, errnorm.Wrap(errnorm.KindLocal, "bridge_config_toml_invalid", "bridge config is not valid TOML", err)
	}
	agentHomeRaw := strings.TrimSpace(bridgeTomlString(root["agent_home"]))
	if agentHomeRaw == "" {
		return bridgeConfigDetails{}, errnorm.Local("bridge_config_invalid", "bridge config requires top-level agent_home")
	}
	agentHome, err := expandBridgePath(filepath.Dir(absPath), agentHomeRaw)
	if err != nil {
		return bridgeConfigDetails{}, err
	}
	agentManifestPath := filepath.Join(agentHome, "agent.toml")
	manifestContent, err := bridgeReadFile(agentManifestPath)
	if err != nil {
		return bridgeConfigDetails{}, errnorm.Wrap(errnorm.KindLocal, "agent_manifest_read_failed", "failed to read agent home agent.toml", err)
	}
	var manifest map[string]any
	if err := toml.Unmarshal(manifestContent, &manifest); err != nil {
		return bridgeConfigDetails{}, errnorm.Wrap(errnorm.KindLocal, "agent_manifest_toml_invalid", "agent.toml is not valid TOML", err)
	}
	authStatePath := ""
	wakeConfigPath := ""
	if authSec := bridgeTomlTable(manifest, "auth"); authSec != nil {
		authStatePath = strings.TrimSpace(bridgeTomlString(authSec["state_path"]))
	}
	if wakeSec := bridgeTomlTable(manifest, "wake"); wakeSec != nil {
		wakeConfigPath = strings.TrimSpace(bridgeTomlString(wakeSec["config_path"]))
	}
	if authStatePath == "" {
		authStatePath = "profiles/default.json"
	}
	authStatePath, err = expandBridgePath(agentHome, authStatePath)
	if err != nil {
		return bridgeConfigDetails{}, err
	}
	if topWakeConfigPath := strings.TrimSpace(bridgeTomlString(root["wake_config"])); topWakeConfigPath != "" {
		wakeConfigPath = topWakeConfigPath
	}
	if wakeConfigPath == "" {
		wakeConfigPath = "wake.toml"
	}
	wakeConfigPath, err = expandBridgePath(agentHome, wakeConfigPath)
	if err != nil {
		return bridgeConfigDetails{}, err
	}
	agentHandle := ""
	baseURL := ""
	if identitySec := bridgeTomlTable(manifest, "identity"); identitySec != nil {
		agentHandle = strings.TrimSpace(bridgeTomlString(identitySec["handle"]))
		baseURL = strings.TrimSpace(bridgeTomlString(identitySec["base_url"]))
	}
	autoManaged := false
	if bridgeRoot := bridgeTomlTable(root, "bridge"); bridgeRoot != nil {
		autoManaged = asBool(bridgeRoot["managed_package_auto_update"])
	}
	return bridgeConfigDetails{
		ConfigPath:               absPath,
		AgentHome:                agentHome,
		AgentManifestPath:        agentManifestPath,
		WakeConfigPath:           wakeConfigPath,
		AuthStatePath:            authStatePath,
		AgentHandle:              agentHandle,
		BaseURL:                  baseURL,
		ManagedPackageAutoUpdate: autoManaged,
	}, nil
}

func bridgeTomlTable(root map[string]any, section string) map[string]any {
	raw, ok := root[section]
	if !ok || raw == nil {
		return nil
	}
	sec, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	return sec
}

func bridgeTomlString(value any) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func bridgeConfigStringValue(content string, section string, key string) string {
	currentSection := ""
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if matches := bridgeSectionHeaderPattern.FindStringSubmatch(line); len(matches) == 2 {
			currentSection = matches[1]
			continue
		}
		if currentSection != section {
			continue
		}
		name, rawValue, ok := parseBridgeConfigAssignment(line)
		if !ok || name != key {
			continue
		}
		return rawValue
	}
	return ""
}

func parseBridgeConfigAssignment(line string) (string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}
	idx := strings.Index(trimmed, "=")
	if idx <= 0 {
		return "", "", false
	}
	name := strings.TrimSpace(trimmed[:idx])
	rawValue := strings.TrimSpace(trimmed[idx+1:])
	if commentIdx := strings.Index(rawValue, "#"); commentIdx >= 0 {
		rawValue = strings.TrimSpace(rawValue[:commentIdx])
	}
	if len(rawValue) >= 2 && strings.HasPrefix(rawValue, "\"") && strings.HasSuffix(rawValue, "\"") {
		if unquoted, err := strconv.Unquote(rawValue); err == nil {
			rawValue = unquoted
		}
	}
	if name == "" {
		return "", "", false
	}
	return name, rawValue, true
}

func expandBridgePath(baseDir string, raw string) (string, error) {
	pathValue := strings.TrimSpace(raw)
	if pathValue == "" {
		pathValue = "."
	}
	if pathValue == "~" || strings.HasPrefix(pathValue, "~/") {
		home, err := bridgeUserHomeDir()
		if err != nil {
			return "", errnorm.Wrap(errnorm.KindLocal, "resolve_home_failed", "failed to resolve home directory", err)
		}
		if pathValue == "~" {
			pathValue = home
		} else {
			pathValue = filepath.Join(home, strings.TrimPrefix(pathValue, "~/"))
		}
	}
	pathValue = os.ExpandEnv(pathValue)
	if !filepath.IsAbs(pathValue) {
		pathValue = filepath.Join(baseDir, pathValue)
	}
	return filepath.Clean(pathValue), nil
}

func writeBridgeJSONFile(path string, payload map[string]any) error {
	if err := bridgeMkdirAll(filepath.Dir(path), 0o700); err != nil {
		return errnorm.Wrap(errnorm.KindLocal, "bridge_auth_dir_failed", "failed to create bridge auth directory", err)
	}
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return errnorm.Wrap(errnorm.KindLocal, "bridge_auth_encode_failed", "failed to encode bridge auth state", err)
	}
	if err := bridgeWriteFile(path, append(encoded, '\n'), 0o600); err != nil {
		return errnorm.Wrap(errnorm.KindLocal, "bridge_auth_write_failed", "failed to write bridge auth state", err)
	}
	return nil
}

func bridgeUpdateAgentManifestIdentity(agentManifestPath string, prof profile.Profile, publicKey ed25519.PublicKey) error {
	content, err := bridgeReadFile(agentManifestPath)
	if err != nil {
		return errnorm.Wrap(errnorm.KindLocal, "agent_manifest_read_failed", "failed to read agent home agent.toml", err)
	}
	updated := string(content)
	for _, item := range []struct {
		Key   string
		Value string
	}{
		{Key: "base_url", Value: strings.TrimSpace(prof.BaseURL)},
		{Key: "agent_id", Value: strings.TrimSpace(prof.AgentID)},
		{Key: "actor_id", Value: strings.TrimSpace(prof.ActorID)},
		{Key: "key_id", Value: strings.TrimSpace(prof.KeyID)},
		{Key: "public_key_fingerprint", Value: bridgePublicKeyFingerprint(publicKey)},
	} {
		if item.Value == "" {
			continue
		}
		var changed bool
		updated, changed = bridgeReplaceConfigValue(updated, "identity", item.Key, item.Value)
		if !changed {
			return errnorm.Local("agent_manifest_update_failed", "agent.toml is missing [identity]."+item.Key)
		}
	}
	if err := bridgeWriteFile(agentManifestPath, []byte(updated), 0o600); err != nil {
		return errnorm.Wrap(errnorm.KindLocal, "agent_manifest_write_failed", "failed to update agent home identity", err)
	}
	return nil
}

func bridgePublicKeyFingerprint(publicKey ed25519.PublicKey) string {
	sum := sha256.Sum256(publicKey)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func bridgeUpdateWakeConfigBaseURL(wakeConfigPath string, newBaseURL string) (bool, error) {
	newBaseURL = strings.TrimSpace(newBaseURL)
	if newBaseURL == "" {
		return false, nil
	}
	content, err := bridgeReadFile(wakeConfigPath)
	if err != nil {
		return false, errnorm.Wrap(errnorm.KindLocal, "wake_config_read_failed", "failed to read wake.toml", err)
	}
	updated, changed := bridgeReplaceWorkspaceBaseURL(string(content), newBaseURL)
	if changed {
		if err := bridgeWriteFile(wakeConfigPath, []byte(updated), 0o600); err != nil {
			return false, errnorm.Wrap(errnorm.KindLocal, "wake_config_write_failed", "failed to update wake.toml base URLs", err)
		}
	}
	return changed, nil
}

func bridgeReplaceWorkspaceBaseURL(content string, newBaseURL string) (string, bool) {
	var buf strings.Builder
	inWorkspace := false
	wroteBaseURL := false
	changed := false
	flushWorkspaceBaseURL := func() {
		if inWorkspace && !wroteBaseURL {
			buf.WriteString("base_url = " + strconv.Quote(newBaseURL) + "\n")
			changed = true
		}
	}
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			flushWorkspaceBaseURL()
			inWorkspace = trimmed == "[[workspaces]]"
			wroteBaseURL = false
			buf.WriteString(line + "\n")
			continue
		}
		if inWorkspace {
			name, _, ok := parseBridgeConfigAssignment(line)
			if ok && name == "base_url" {
				replacement := "base_url = " + strconv.Quote(newBaseURL)
				buf.WriteString(replacement + "\n")
				wroteBaseURL = true
				if strings.TrimSpace(line) != replacement {
					changed = true
				}
				continue
			}
		}
		buf.WriteString(line + "\n")
	}
	flushWorkspaceBaseURL()
	return strings.TrimRight(buf.String(), "\n") + "\n", changed
}

func bridgeReplaceConfigValue(content string, section string, key string, newValue string) (string, bool) {
	var buf strings.Builder
	currentSection := ""
	sectionSeen := false
	written := false
	for _, line := range strings.Split(content, "\n") {
		if matches := bridgeSectionHeaderPattern.FindStringSubmatch(line); len(matches) == 2 {
			if currentSection == section && !written {
				buf.WriteString(key + " = " + strconv.Quote(newValue) + "\n")
				written = true
			}
			currentSection = matches[1]
			if currentSection == section {
				sectionSeen = true
			}
			buf.WriteString(line + "\n")
			continue
		}
		if currentSection == section && !written {
			name, _, ok := parseBridgeConfigAssignment(line)
			if ok && name == key {
				buf.WriteString(key + " = " + strconv.Quote(newValue) + "\n")
				written = true
				continue
			}
		}
		buf.WriteString(line + "\n")
	}
	if !written {
		if sectionSeen {
			buf.WriteString(key + " = " + strconv.Quote(newValue) + "\n")
		} else {
			buf.WriteString("[" + section + "]\n")
			buf.WriteString(key + " = " + strconv.Quote(newValue) + "\n")
		}
		written = true
	}
	return strings.TrimRight(buf.String(), "\n") + "\n", written
}
