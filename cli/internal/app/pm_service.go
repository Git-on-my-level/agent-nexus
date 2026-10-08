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
)

var pmServiceOS = runtime.GOOS
var pmServiceExec = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

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
		topic := localHelperTopic{Path: "pm " + verb, Summary: map[string]string{"install": "Install or update the PM service on this computer.", "status": "Read the local PM service and last accepted claim.", "uninstall": "Stop and remove the local PM service."}[verb], JSONShape: "installed, running, workspace, agent, service, logs, last_claim_at", Composition: "Per-user launchd on macOS or systemd --user on Linux. Uses the selected workspace and profile; stores no credentials in the service definition.", Examples: []string{"anx pm " + verb}}
		if verb == "install" {
			topic.Flags = []localHelperFlag{{Name: "--wait", Description: "Wait for an accepted connection after install."}, {Name: "--wait-timeout <duration>", Description: "Bound the connection wait (default 90s, maximum 5m)."}, {Name: "--runner <argv>", Description: "Runner command; saved locally for subsequent installs. Use {prompt_file} for a private prompt file, or omit a placeholder to use agentctl's file transport."}}
		}
		localHelperTopics = append(localHelperTopics, topic)
	}
}

func (a *App) runPMService(ctx context.Context, verb string, args []string, cfg config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("pm " + verb)
	var runner trackedString
	var wait bool
	var waitTimeout = 90 * time.Second
	if verb == "install" {
		fs.Var(&runner, "runner", "Runner command")
		fs.BoolVar(&wait, "wait", false, "Wait for the first accepted PM connection")
		fs.DurationVar(&waitTimeout, "wait-timeout", 90*time.Second, "Connection wait budget")
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
	sum := sha256.Sum256([]byte(cfg.BaseURL + "\x00" + dir + "\x00" + agent))
	id := "io.agent-nexus.pm." + hex.EncodeToString(sum[:8])
	stateRoot := os.Getenv("XDG_STATE_HOME")
	if stateRoot == "" {
		stateRoot = filepath.Join(home, ".local", "state")
	}
	if !filepath.IsAbs(stateRoot) {
		return nil, fmt.Errorf("XDG_STATE_HOME must be absolute")
	}
	state := filepath.Join(stateRoot, "anx", "pm", id)
	unit := filepath.Join(home, "Library", "LaunchAgents", id+".plist")
	if pmServiceOS == "linux" {
		configRoot := os.Getenv("XDG_CONFIG_HOME")
		if configRoot == "" {
			configRoot = filepath.Join(home, ".config")
		}
		if !filepath.IsAbs(configRoot) {
			return nil, fmt.Errorf("XDG_CONFIG_HOME must be absolute")
		}
		unit = filepath.Join(configRoot, "systemd", "user", id+".service")
	}
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
			return nil, errnorm.Usage("runner_required", "set --runner once; subsequent installs reuse settings.json")
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
		if e = writePMPrivateFile(settingsPath, b); e != nil {
			return nil, e
		}
		serviceArgs := []string{binary, "--base-url", cfg.BaseURL, "--config-dir", dir, "--as", agent, "pm", "serve", "--runner", settings.Runner, "--work-dir", state, "--poll-interval", "5s", "--max-concurrent", "1"}
		definition := pmServiceDefinition(pmServiceOS, id, serviceArgs, state, home, os.Getenv("PATH"))
		if e = writePMPrivateFile(unit, []byte(definition)); e != nil {
			return nil, e
		}
		if pmServiceOS == "darwin" {
			// bootout may report an absent job on an idempotent repair; bootstrap is the
			// authoritative start and any failure is returned to the caller.
			_, _ = pmServiceExec(ctx, "launchctl", "bootout", domain+"/"+id)
			if e = command("launchctl", "bootstrap", domain, unit); e != nil {
				return nil, e
			}
		} else {
			if e = command("systemctl", "--user", "daemon-reload"); e != nil {
				return nil, e
			}
			if e = command("systemctl", "--user", "enable", id+".service"); e != nil {
				return nil, e
			}
			if e = command("systemctl", "--user", "restart", id+".service"); e != nil {
				return nil, e
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
	}
	if status.Installed {
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
	if verb == "install" && wait {
		if err := a.waitPMConnection(ctx, cfg, waitTimeout, state, time.Now()); err != nil {
			return nil, err
		}
		return &commandResult{Data: status, Text: "PM connected\nWorkspace: " + cfg.BaseURL + "\nLogs: " + state}, nil
	}
	return &commandResult{Data: status, Text: fmt.Sprintf("PM %s: installed=%t running=%t\nWorkspace: %s\nProfile: %s\nLast accepted claim: %s\nLogs: %s", verb, status.Installed, status.Running, status.Workspace, status.Agent, firstNonEmpty(status.LastClaimAt, "none"), state)}, nil
}

func writePMPrivateFile(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".pm-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
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
