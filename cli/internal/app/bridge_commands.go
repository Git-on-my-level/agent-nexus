package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"agent-nexus-cli/internal/buildinfo"
	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

const bridgeRepoURL = "https://github.com/Git-on-my-level/agent-nexus.git"

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

func init() {
	runtimeHelpManualDocTopics = append(runtimeHelpManualDocTopics, runtimeHelpDocTopic{Path: "bridge", Kind: "manual", Summary: "One bridge per enrolled host for derived-agent wake routing."})
	localHelperTopics = append(localHelperTopics,
		localHelperTopic{Path: "bridge run", Summary: "Run registered answer commands for the selected agent on this host.", JSONShape: "`stopped`", Composition: "Long-lived signed wake consumer; command registry stays local.", Examples: []string{"anx --as worker bridge run"}},
		localHelperTopic{Path: "bridge install", Summary: "Install the host bridge runtime.", JSONShape: "`install_dir`, `bin_dir`, `wrapper_path`, `python`, `bridge_binary`, `package_ref`", Composition: "Install a managed Python virtualenv and wrapper.", Examples: []string{"anx bridge install"}},
		localHelperTopic{Path: "bridge doctor", Summary: "Check one enrolled-host bridge and its configured runtimes.", JSONShape: "`host`, `agents`, `agentctl`", Composition: "Invoke the bridge's host roster validation.", Examples: []string{"anx bridge doctor --config ./bridge.toml"}, Flags: []localHelperFlag{{Name: "--config <path>", Description: "One host bridge config."}}},
	)
}

func (a *App) runBridgeCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 || isHelpToken(args[0]) {
		return &commandResult{Text: bridgeUsageText()}, "bridge", nil
	}
	sub := bridgeSubcommandSpec.normalize(args[0])
	switch sub {
	case "run":
		result, err := a.runAskBridge(ctx, args[1:], cfg)
		return result, "bridge run", err
	case "install":
		result, err := a.runBridgeInstall(ctx, args[1:])
		return result, "bridge install", err
	case "doctor":
		result, err := a.runBridgeDoctor(ctx, args[1:])
		return result, "bridge doctor", err
	case "start":
		result, err := a.runBridgeStart(ctx, args[1:])
		return result, "bridge start", err
	case "stop":
		result, err := a.runBridgeStop(args[1:])
		return result, "bridge stop", err
	case "status":
		result, err := a.runBridgeStatus(ctx, args[1:])
		return result, "bridge status", err
	default:
		return nil, "bridge", bridgeSubcommandSpec.unknownError(args[0])
	}
}

func bridgeUsageText() string {
	return "Bridge: one process per enrolled host\n\n" +
		"anx --as <agent> bridge run [--max-attempts 5] [--command-timeout 30m]\n" +
		"anx host enroll\n" +
		"anx bridge install\n" +
		"anx bridge start --config ./bridge.toml\n" +
		"anx bridge status --config ./bridge.toml\n" +
		"anx bridge doctor --config ./bridge.toml\n" +
		"anx bridge stop --config ./bridge.toml\n\n" +
		"Config: [host] base_url, id, slug; one [agents.<name>] command argv per enabled derived agent.\n"
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
		"Next: enroll the host, write one host bridge config, then run anx bridge start --config ./bridge.toml",
	}
	if !bridgePathContains(a.Getenv, result.BinDir) {
		lines = append(lines, "PATH note: add "+result.BinDir+" to PATH to run `anx-agent-bridge` directly.")
	}
	return &commandResult{Text: strings.Join(lines, "\n"), Data: data}, nil
}

func (a *App) runBridgeDoctor(ctx context.Context, args []string) (*commandResult, error) {
	fs := newSilentFlagSet("bridge doctor")
	var configFlag trackedString
	fs.Var(&configFlag, "config", "Host bridge config")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 || configFlag.value == "" {
		return nil, errnorm.Usage("invalid_args", "--config is required")
	}
	home, err := a.bridgeHome()
	if err != nil {
		return nil, err
	}
	binary, err := resolveBridgeBinary(home, "", "")
	if err != nil {
		return nil, err
	}
	raw, err := runBridgeExternalOutput(ctx, binary, "doctor", "--config", configFlag.value)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "bridge_doctor_failed", "host bridge doctor failed", err)
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "bridge_doctor_invalid", "bridge doctor returned invalid JSON", err)
	}
	return &commandResult{Text: raw, Data: data}, nil
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
		if err := runBridgeExternal(ctx, venvPython, "-m", "pip", "install", "pytest>=8,<9", "cryptography>=42,<47"); err != nil {
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
