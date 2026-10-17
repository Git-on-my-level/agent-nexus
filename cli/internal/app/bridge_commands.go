package app

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	hostbridge "agent-nexus-cli/internal/bridge"
	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/filetransport"
	"agent-nexus-cli/internal/integrations"
)

var bridgeCommandRun = defaultBridgeCommandRun

func defaultBridgeCommandRun(ctx context.Context, name string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = integrations.GitEnvironment(os.Environ())
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	e := cmd.Run()
	return out.String(), stderr.String(), e
}
func shellSingleQuote(v string) string     { return integrations.ShellQuote(v) }
func (a *App) bridgeHome() (string, error) { return a.UserHomeDir() }
func (a *App) hostBridgeRoot() (string, error) {
	home, e := a.bridgeHome()
	if e != nil {
		return "", e
	}
	st, e := os.Stat(home)
	if e != nil {
		return "", e
	}
	if !st.IsDir() {
		return "", fmt.Errorf("host home is not a directory")
	}
	return hostbridge.Root(home), nil
}
func bridgeUsageText() string {
	return `One optional bridge service per host, across all enrolled workspaces.

Recommended: anx bridge install, then anx bridge add --as <agent> --workspace <alias>
anx bridge add --as <agent> [--answers=true] [--git] [--spool] [--runtime <json-file>]
anx bridge remove --as <agent> [--workspace <alias>]
anx bridge install [--dry-run]
anx bridge run
anx bridge start|stop|status
anx bridge doctor

Install opts into signed presence reporting; add explicitly enables independent jobs.
Without the answer job, unattended resumes wait; Git and spool drain remain manual.
Await/batch and direct commands work without the bridge. Files require explicit anx sync enable with a card allowlist.
Host commands share the canonical ~/.config/anx/bridge registry, service and lock.
Rollback only: anx --as <agent> --workspace <alias> bridge run [--command-timeout 30m] [--max-attempts 5]
The legacy form serves one answer namespace without host heartbeat or other jobs.
`
}
func init() {
	runtimeHelpManualDocTopics = append(runtimeHelpManualDocTopics, runtimeHelpDocTopic{Path: "bridge", Kind: "manual", Summary: "One optional host service for answers, wake routing, Git and spool jobs."})
	for _, topic := range []localHelperTopic{
		{Path: "bridge run", Summary: "Run the host supervisor, or one legacy answers-only namespace for rollback.", JSONShape: "`stopped`", Composition: "Bare bridge run serves the host registry. Explicit --as or ANX_AS selects a single-namespace answers-only rollback executor, preserving its policy and receipts without publishing host presence. New service units clear ANX_AS. See cli/docs/host-bridge.md.", Flags: []localHelperFlag{{Name: "--command-timeout", Description: "Legacy rollback consumer command timeout (default 30m); requires --as."}, {Name: "--max-attempts", Description: "Legacy rollback consumer attempts (1..20, default 5); requires --as."}}, Examples: []string{"anx bridge run"}},
		{Path: "bridge remove", Summary: "Remove a local registration, preserving commands and receipts.", JSONShape: "`removed`, `agent`, `retained_records`", Examples: []string{"anx bridge remove --as worker"}},
		{Path: "bridge add", Summary: "Register an enrolled agent/workspace and enable its selected jobs.", JSONShape: "`workspace_id`, `host_id`, `actor_id`, `registered`", Examples: []string{"anx bridge add --as codex"}},
		{Path: "bridge install", Summary: "Install one host unit and safely migrate legacy consumers.", JSONShape: "`unit_path`, `label`, `replaced`, `imported`, `dry_run`", Examples: []string{"anx bridge install --dry-run"}},
		{Path: "bridge doctor", Summary: "Inspect host singleton, namespace safety, jobs and writer bindings.", JSONShape: "`running`, `registrations`, `checks`", Examples: []string{"anx bridge doctor"}},
	} {
		localHelperTopics = append(localHelperTopics, topic)
	}
}
func (a *App) runBridgeCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 || isHelpToken(args[0]) {
		return &commandResult{Text: bridgeUsageText()}, "bridge", nil
	}
	sub := args[0]
	name := "bridge " + sub
	switch sub {
	case "add":
		r, e := a.addHostBridgeAgent(ctx, args[1:], cfg)
		return r, name, e
	case "remove":
		r, e := a.removeHostBridgeAgent(ctx, args[1:], cfg)
		return r, name, e
	case "run":
		if legacyAnswerBridgeInvocation(args, cfg) {
			r, e := a.runLegacyAnswerBridge(ctx, args[1:], cfg)
			return r, name, e
		}
		r, e := a.runHostSupervisor(ctx)
		return r, name, e
	case "install":
		r, e := a.installHostBridge(ctx, args[1:], cfg)
		return r, name, e
	case "start", "stop":
		if len(args) != 1 {
			return nil, name, errnorm.Usage("invalid_flags", "host service takes no workspace or agent flags")
		}
		r, e := a.controlHostBridge(ctx, sub)
		return r, name, e
	case "status", "doctor":
		if len(args) != 1 {
			return nil, name, errnorm.Usage("invalid_flags", "use bridge status|doctor without flags")
		}
		r, e := a.inspectHostBridge(ctx, sub == "doctor")
		return r, name, e
	default:
		return nil, name, bridgeSubcommandSpec.unknownError(sub)
	}
}
func (a *App) bridgeRegistration(ctx context.Context, cfg config.Resolved) (hostbridge.Workspace, hostbridge.Agent, error) {
	var w hostbridge.Workspace
	var agent hostbridge.Agent
	cfg, e := a.resolveHostAgent(ctx, cfg)
	if e != nil {
		return w, agent, e
	}
	h, e := a.resolvedHost(cfg)
	if e != nil {
		return w, agent, e
	}
	dir, e := a.configDir(cfg)
	if e != nil {
		return w, agent, e
	}
	dir, e = filepath.EvalSymlinks(dir)
	if e != nil {
		return w, agent, e
	}
	workspaceID, e := a.bridgeWorkspaceID(ctx, cfg, h.WorkspaceID)
	if e != nil {
		return w, agent, e
	}
	if cfg.ActorID == "" || workspaceID == "" {
		return w, agent, errnorm.Local("identity_unresolved", "enrolled workspace and actor required")
	}
	w = hostbridge.Workspace{ID: workspaceID, BaseURL: strings.TrimRight(cfg.BaseURL, "/"), HostID: h.ID, ConfigDir: dir}
	registry := filepath.Join(dir, "answer-resumes", hostbridge.Namespace(w.BaseURL, cfg.ActorID))
	agent = hostbridge.Agent{Name: cfg.As, ActorID: cfg.ActorID, Jobs: map[string]bool{"answers": true}, RegistryDirs: []string{registry}, Policy: hostbridge.Policy{MaxAttempts: 5, Timeout: 30 * time.Minute}}
	return w, agent, nil
}
func (a *App) addHostBridgeAgent(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("bridge add")
	as := fs.String("as", cfg.As, "Enrolled agent name")
	answers := fs.Bool("answers", true, "Enable registered answer delivery")
	git := fs.Bool("git", false, "Enable explicitly configured Git capture")
	spool := fs.Bool("spool", false, "Enable configured integration spool drain")
	wake := fs.Bool("wake", false, "Enable exact runtime wake routing (requires runtime configuration)")
	runtime := fs.String("runtime", "", "Owner-only JSON with exact argv/cwd and optional agentctl wiring")
	attempts := fs.Int("max-attempts", 5, "Resume attempts (1..20)")
	timeout := fs.Duration("command-timeout", 30*time.Minute, "Resume timeout")
	if e := fs.Parse(args); e != nil {
		return nil, errnorm.Usage("invalid_flags", e.Error())
	}
	if len(fs.Args()) != 0 || !agentNamePattern.MatchString(*as) || *attempts < 1 || *attempts > 20 || *timeout <= 0 {
		return nil, errnorm.Usage("invalid_args", "bridge add requires --as <agent> and valid execution policy")
	}
	cfg.As = *as
	w, agent, e := a.bridgeRegistration(ctx, cfg)
	if e != nil {
		return nil, e
	}
	agent.Jobs = map[string]bool{"answers": *answers, "git": *git, "spool": *spool}
	agent.Policy = hostbridge.Policy{MaxAttempts: *attempts, Timeout: *timeout}
	if *runtime != "" {
		raw, e := integrations.PrivateRead(*runtime)
		if e != nil {
			return nil, e
		}
		if len(raw) > 64<<10 {
			return nil, fmt.Errorf("runtime config too large")
		}
		var r hostbridge.Runtime
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if e = dec.Decode(&r); e != nil {
			return nil, e
		}
		agent.Runtime = &r
		agent.Jobs["wake"] = true
	}
	for _, namespace := range agent.RegistryDirs {
		if e = os.MkdirAll(namespace, 0700); e != nil {
			return nil, e
		}
	}
	wakeSpecified := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "wake" {
			wakeSpecified = true
		}
	})
	if wakeSpecified {
		agent.Jobs["wake"] = *wake
	}
	root, e := a.hostBridgeRoot()
	if e != nil {
		return nil, e
	}
	e = hostbridge.Update(ctx, root, func(r *hostbridge.Registry) error {
		for _, oldW := range r.Workspaces {
			if oldW.BaseURL != w.BaseURL || oldW.HostID != w.HostID {
				continue
			}
			for _, old := range oldW.Agents {
				if old.ActorID != agent.ActorID {
					continue
				}
				if *runtime == "" {
					agent.Runtime = old.Runtime
					if !wakeSpecified {
						agent.Jobs["wake"] = old.Jobs["wake"]
					}
				}
				specified := map[string]bool{}
				fs.Visit(func(f *flag.Flag) { specified[f.Name] = true })
				for _, kind := range []string{"git", "spool"} {
					if !specified[kind] {
						agent.Jobs[kind] = old.Jobs[kind]
					}
				}
				if !specified["max-attempts"] {
					agent.Policy.MaxAttempts = old.Policy.MaxAttempts
				}
				if !specified["command-timeout"] {
					agent.Policy.Timeout = old.Policy.Timeout
				}
			}
		}
		return hostbridge.Register(r, w, agent)
	})
	if e != nil {
		return nil, e
	}
	return &commandResult{Data: map[string]any{"registered": true, "workspace_id": w.ID, "host_id": w.HostID, "actor_id": agent.ActorID, "jobs": agent.Jobs, "next": "anx bridge install"}}, nil
}

// Removal uses the pinned local name, not a fresh agent grant, so a revoked
// registration can be repaired. Retained commands and outcome receipts stay put.
func (a *App) removeHostBridgeAgent(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("bridge remove")
	as := fs.String("as", cfg.As, "Registered agent name")
	if e := fs.Parse(args); e != nil {
		return nil, errnorm.Usage("invalid_flags", e.Error())
	}
	if len(fs.Args()) != 0 || !agentNamePattern.MatchString(*as) {
		return nil, errnorm.Usage("invalid_args", "bridge remove requires --as <agent>")
	}
	root, e := a.hostBridgeRoot()
	if e != nil {
		return nil, e
	}
	removed := false
	e = hostbridge.Update(ctx, root, func(r *hostbridge.Registry) error {
		for i := range r.Workspaces {
			w := &r.Workspaces[i]
			if w.BaseURL != strings.TrimRight(cfg.BaseURL, "/") {
				continue
			}
			kept := []hostbridge.Agent{}
			for _, agent := range w.Agents {
				if agent.Name == *as {
					removed = true
				} else {
					kept = append(kept, agent)
				}
			}
			w.Agents = kept
		}
		return nil
	})
	if e != nil {
		return nil, e
	}
	return &commandResult{Data: map[string]any{"removed": removed, "agent": *as, "retained_records": true}}, nil
}
func bridgeCfg(w hostbridge.Workspace, a hostbridge.Agent) config.Resolved {
	return config.Resolved{BaseURL: w.BaseURL, As: a.Name, ActorID: a.ActorID, ConfigDir: w.ConfigDir, Timeout: 2 * time.Second, Sources: map[string]string{"base_url": "bridge", "agent": "bridge"}}
}
func (a *App) inspectHostBridge(ctx context.Context, doctor bool) (*commandResult, error) {
	root, e := a.hostBridgeRoot()
	if e != nil {
		return nil, e
	}
	r, e := hostbridge.Load(root)
	if e != nil {
		return nil, e
	}
	running := askBridgeLockState(filepath.Join(root, "service.lock")) == answerBridgeRunning
	checks := []map[string]any{}
	if doctor {
		checks = append(checks, a.inspectBridgeInventory(ctx, root)...)
	}
	if doctor {
		if raw, readErr := integrations.PrivateRead(filepath.Join(root, "migration.json")); readErr == nil {
			var journal bridgeMigrationJournal
			if e := json.Unmarshal(raw, &journal); e != nil {
				return nil, e
			}
			checks = append(checks, map[string]any{"check": "migration_complete", "ok": journal.Phase == "complete", "phase": journal.Phase})
			paused := []string{}
			for _, path := range journal.Paused {
				if _, err := os.Lstat(path); err == nil || !os.IsNotExist(err) {
					paused = append(paused, path)
				}
			}
			checks = append(checks, map[string]any{"check": "migration_paused_resumes", "ok": len(paused) == 0, "paths": paused, "repair": "verify the external outcome, reconcile the retained state and restore its original .json path; never reset attempts"})
		}
		raw, readErr := integrations.PrivateRead(filepath.Join(root, "status.json"))
		var state localBridgeState
		if readErr == nil && json.Unmarshal(raw, &state) == nil {
			checks = append(checks, map[string]any{"check": "fresh_local_status", "ok": time.Since(state.At) < 10*time.Second, "as_of": state.At, "ready": state.Ready, "heartbeat_reports": state.Reports})
		}
	}

	count := 0
	for _, w := range r.Workspaces {
		for _, agent := range w.Agents {
			count++
			cfg := bridgeCfg(w, agent)
			if doctor {
				_, err := a.validateBridgeAgent(ctx, w, agent)
				checks = append(checks, map[string]any{"workspace_id": w.ID, "actor_id": agent.ActorID, "check": "enrollment", "ok": err == nil, "repair": "repair enrollment or use bridge remove --as " + agent.Name + " in this workspace; retained records survive"})
				for _, dir := range agent.RegistryDirs {
					err = checkResumeNamespace(dir, w, agent)
					if err == nil && agent.Jobs["wake"] {
						err = checkRuntimeReceipts(dir)
					}
					checks = append(checks, map[string]any{"workspace_id": w.ID, "actor_id": agent.ActorID, "check": "resume_namespace", "ok": err == nil, "repair": "reconcile retained command/runtime receipts against their external outcome"})
				}
				for _, dir := range agent.FileDirs {
					s, err := filetransport.Inspect(dir, a.clockNow())
					checks = append(checks, map[string]any{"workspace_id": w.ID, "actor_id": agent.ActorID, "check": "file_transport", "ok": err == nil, "status": s, "repair": "inspect sync status and protected originals; reconcile uncertain work before changing identity or UUID"})
				}
				if agent.Jobs["git"] {
					_, err = a.bridgeGitTick(ctx, cfg, true)
					checks = append(checks, map[string]any{"workspace_id": w.ID, "actor_id": agent.ActorID, "check": "capture_writer", "ok": err == nil})
				}
			}
		}
	}
	return &commandResult{Data: map[string]any{"running": running, "registrations": count, "revision": r.Revision, "checks": checks, "registry_path": filepath.Join(root, "registry.json")}}, nil
}

func (a *App) bridgeWorkspaceID(ctx context.Context, cfg config.Resolved, enrolled string) (string, error) {
	if enrolled != "" {
		return enrolled, nil
	}
	reply, e := a.hostCall(ctx, cfg, "GET", "/meta/handshake", nil, nil)
	if e != nil {
		return "", e
	}
	id := anyString(reply["workspace_id"])
	if id == "" {
		return "", fmt.Errorf("workspace identity is unavailable")
	}
	return id, nil
}
