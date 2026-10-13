package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

// `anx bridge install` keeps an `anx bridge run` answer bridge alive for one
// derived agent: a launchd agent on macOS, a systemd user unit on Linux. The
// legacy Python wake-routing runtime install keeps its own flags.

const answerBridgeUnitPrefix = "com.agent-nexus.anx-bridge"

type bridgeUnitPlan struct {
	Path      string
	Label     string
	Unit      string
	Content   string
	StartArgs []string
	Env       map[string]string
}

func (a *App) runBridgeUnitInstall(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	if runtime.GOOS == "windows" {
		return nil, errnorm.Usage("unsupported_platform", "`anx bridge install` currently supports macOS and Linux only")
	}
	fs := newSilentFlagSet("bridge install")
	var asFlag trackedString
	var dryRun trackedBool
	fs.Var(&asFlag, "as", "Derived agent the bridge delivers for (default: the host's resolved agent)")
	fs.Var(&dryRun, "dry-run", "Print the unit plan without writing or starting anything")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx bridge install`")
	}
	agent := strings.TrimSpace(asFlag.value)
	if agent == "" {
		resolved, err := a.resolveHostAgent(ctx, cfg)
		if err != nil {
			return nil, errnorm.Usage("identity_unresolved", "cannot resolve this host's agent; pass --as <agent-name>")
		}
		agent = firstNonEmpty(strings.TrimSpace(resolved.As), strings.TrimSpace(resolved.Agent))
	}
	if !validBridgeUnitAgentName(agent) {
		return nil, errnorm.Usage("invalid_request", "agent names for bridge units accept letters, digits, dots, dashes and underscores only; pass --as <agent-name>")
	}
	execPath, err := bridgeExecutablePath()
	if err != nil {
		return nil, err
	}
	configDir, err := a.configDir(cfg)
	if err != nil {
		return nil, err
	}
	logPath := filepath.Join(configDir, "logs", fmt.Sprintf("bridge-%s.log", agent))
	home, err := a.bridgeHome()
	if err != nil {
		return nil, err
	}
	env := map[string]string{}
	if cfg.BaseURL != "" && cfg.Sources["base_url"] != "default" {
		env["ANX_BASE_URL"] = cfg.BaseURL
	}
	if cfg.ConfigDir != "" {
		env["ANX_CONFIG_DIR"] = cfg.ConfigDir
	}
	plan, err := answerBridgeUnitPlan(home, agent, execPath, logPath, env)
	if err != nil {
		return nil, err
	}
	view := map[string]any{
		"agent": agent, "binary": execPath, "log_path": logPath,
		"unit_path": plan.Path, "label": plan.Label, "unit": plan.Unit, "content": plan.Content,
		"start_args": plan.StartArgs, "next": strings.Join(plan.StartArgs, " "),
	}
	if dryRun.value {
		view["dry_run"] = true
		return &commandResult{Data: view}, nil
	}
	if err := bridgeMkdirAll(filepath.Dir(plan.Path), 0o755); err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "bridge_unit_dir_failed", "failed to create unit directory", err)
	}
	if err := bridgeMkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "bridge_log_dir_failed", "failed to create bridge log directory", err)
	}
	existing, readErr := bridgeReadFile(plan.Path)
	alreadyCurrent := readErr == nil && string(existing) == plan.Content
	if !alreadyCurrent {
		if err := bridgeWriteFile(plan.Path, []byte(plan.Content), 0o644); err != nil {
			return nil, errnorm.Wrap(errnorm.KindLocal, "bridge_unit_write_failed", "failed to write bridge unit", err)
		}
	}
	view["already_current"] = alreadyCurrent
	view["wrote"] = plan.Path
	startErr := activateAnswerBridgeUnit(ctx, plan, alreadyCurrent)
	if startErr == nil {
		view["started"] = true
	} else {
		view["started"] = false
		view["start_hint"] = strings.Join(plan.StartArgs, " ") + " (" + startErr.Error() + ")"
	}
	return &commandResult{Data: view}, nil
}

// activateAnswerBridgeUnit loads or reloads the written unit so a first install
// is visible to systemd/launchd and a rewritten unit replaces the running job.
func activateAnswerBridgeUnit(ctx context.Context, plan bridgeUnitPlan, alreadyCurrent bool) error {
	switch plan.Unit {
	case "launchd":
		domain := "gui/" + strconv.Itoa(os.Getuid())
		stdout, stderr, err := bridgeCommandRun(ctx, "/bin/launchctl", "bootout", domain+"/"+plan.Label)
		if err != nil && !launchctlServiceMissing(stdout, stderr) {
			return err
		}
		_, _, err = bridgeCommandRun(ctx, "/bin/launchctl", "bootstrap", domain, plan.Path)
		return err
	case "systemd-user":
		if _, _, err := bridgeCommandRun(ctx, "/usr/bin/systemctl", "--user", "daemon-reload"); err != nil {
			return err
		}
		if _, _, err := bridgeCommandRun(ctx, "/usr/bin/systemctl", "--user", "enable", "--now", plan.Label); err != nil {
			return err
		}
		if alreadyCurrent {
			return nil
		}
		_, _, err := bridgeCommandRun(ctx, "/usr/bin/systemctl", "--user", "restart", plan.Label)
		return err
	default:
		if len(plan.StartArgs) == 0 {
			return nil
		}
		_, _, err := bridgeCommandRun(ctx, plan.StartArgs[0], plan.StartArgs[1:]...)
		return err
	}
}

func launchctlServiceMissing(stdout, stderr string) bool {
	msg := stdout + "\n" + stderr
	return strings.Contains(msg, "Could not find service") || strings.Contains(msg, "No such process")
}

// validBridgeUnitAgentName keeps agent names safe to embed in launchd labels,
// log file names and unquoted systemd ExecStart arguments.
func validBridgeUnitAgentName(agent string) bool {
	if agent == "" || len(agent) > 64 {
		return false
	}
	for _, r := range agent {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

func bridgeExecutablePath() (string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return "", errnorm.Wrap(errnorm.KindLocal, "bridge_unit_binary_failed", "cannot locate the anx binary", err)
	}
	if abs, absErr := filepath.Abs(execPath); absErr == nil {
		execPath = abs
	}
	return execPath, nil
}

// answerBridgeUnitPlan renders the persistent `anx bridge run` unit for the
// current platform and the command that loads it.
func answerBridgeUnitPlan(home, agent, execPath, logPath string, env map[string]string) (bridgeUnitPlan, error) {
	slug := strings.ReplaceAll(strings.ToLower(agent), ".", "-")
	switch runtime.GOOS {
	case "darwin":
		label := answerBridgeUnitPrefix + "." + slug
		plist := launchdAnswerBridgePlist(label, execPath, agent, logPath, env)
		uid := os.Getuid()
		return bridgeUnitPlan{
			Path:      filepath.Join(home, "Library", "LaunchAgents", label+".plist"),
			Label:     label,
			Unit:      "launchd",
			StartArgs: []string{"/bin/launchctl", "bootstrap", "gui/" + strconv.Itoa(uid), filepath.Join(home, "Library", "LaunchAgents", label+".plist")},
			Content:   plist,
			Env:       env,
		}, nil
	case "linux":
		unit := "anx-bridge-" + slug + ".service"
		return bridgeUnitPlan{
			Path:      filepath.Join(home, ".config", "systemd", "user", unit),
			Label:     unit,
			Unit:      "systemd-user",
			StartArgs: []string{"/usr/bin/systemctl", "--user", "enable", "--now", unit},
			Content:   systemdAnswerBridgeUnit("ANX answer bridge ("+agent+")", execPath, agent, logPath, env),
			Env:       env,
		}, nil
	default:
		return bridgeUnitPlan{}, errnorm.Usage("unsupported_platform", "`anx bridge install` currently supports macOS and Linux only")
	}
}

func launchdAnswerBridgePlist(label, execPath, agent, logPath string, env map[string]string) string {
	envXML := ""
	for _, key := range sortedEnvKeys(env) {
		envXML += fmt.Sprintf("\n        <key>%s</key>\n        <string>%s</string>", xmlEscape(key), xmlEscape(env[key]))
	}
	envBlock := ""
	if envXML != "" {
		envBlock = "\n    <key>EnvironmentVariables</key>\n    <dict>" + envXML + "\n    </dict>"
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
  <dict>
    <key>Label</key>
    <string>` + label + `</string>
    <key>ProgramArguments</key>
    <array>
      <string>` + xmlEscape(execPath) + `</string>
      <string>bridge</string>
      <string>run</string>
      <string>--as</string>
      <string>` + xmlEscape(agent) + `</string>
    </array>` + envBlock + `
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>` + xmlEscape(logPath) + `</string>
    <key>StandardErrorPath</key>
    <string>` + xmlEscape(logPath) + `</string>
  </dict>
</plist>
`
}

func systemdAnswerBridgeUnit(description, execPath, agent, logPath string, env map[string]string) string {
	envLines := ""
	for _, key := range sortedEnvKeys(env) {
		envLines += "Environment=\"" + systemdSpecEscape(key) + "=" + systemdSpecEscape(env[key]) + "\"\n"
	}
	return `# Keep the ANX answer bridge running for one derived agent.
[Unit]
Description=` + systemdSpecEscape(description) + `
Wants=network-online.target
After=network-online.target

[Service]
ExecStart=` + systemdSpecEscape(execPath) + ` bridge run --as ` + systemdSpecEscape(agent) + `
` + envLines + `Restart=on-failure
RestartSec=5
StandardOutput=append:` + systemdSpecEscape(logPath) + `
StandardError=append:` + systemdSpecEscape(logPath) + `
WorkingDirectory=%h

[Install]
WantedBy=default.target
`
}

// systemdSpecEscape doubles % so unit specifiers in agent names or paths stay literal.
func systemdSpecEscape(value string) string {
	return strings.ReplaceAll(value, "%", "%%")
}

func sortedEnvKeys(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

func xmlEscape(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return replacer.Replace(value)
}
