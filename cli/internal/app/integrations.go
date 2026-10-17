package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/integrations"
)

var integrationsSpec = subcommandSpec{command: "integrations", valid: []string{"list", "status", "enable", "disable", "run", "bind", "map"}, examples: []string{"anx integrations list", "anx integrations enable claude-code-suggested-tasks --board follow-ups"}}

type integrationOptions struct {
	verb, name, scope, installation, board, column string
	dry                                            bool
}

func parseIntegrationOptions(args []string) (integrationOptions, error) {
	o := integrationOptions{}
	if len(args) == 0 {
		return o, nil
	}
	o.verb = args[0]
	if err := preflightSubcommand(args, integrationsSpec); err != nil {
		return o, err
	}
	rest := args[1:]
	if o.verb != "list" {
		if len(rest) == 0 || strings.HasPrefix(rest[0], "-") {
			return o, errnorm.Usage("invalid_args", "integration name is required")
		}
		o.name = rest[0]
		rest = rest[1:]
		if _, err := integrations.Lookup(o.name); err != nil {
			return o, errnorm.Usage("unknown_integration", err.Error())
		}
	}
	fs := newSilentFlagSet("integrations " + o.verb)
	if o.verb != "run" {
		fs.StringVar(&o.scope, "scope", "", "Harness settings scope (adapter default when omitted)")
	}
	if o.verb == "enable" {
		fs.StringVar(&o.board, "board", "", "Parent board ref or handle")
		fs.StringVar(&o.column, "column", "", "Initial column key")
	}
	if o.verb == "enable" || o.verb == "disable" {
		fs.BoolVar(&o.dry, "dry-run", false, "Show exact JSON without writing")
	}
	if o.verb == "run" {
		fs.StringVar(&o.installation, "installation", "", "Managed hook installation id")
	}
	if err := fs.Parse(rest); err != nil {
		return o, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return o, errnorm.Usage("invalid_args", "unexpected positional arguments")
	}
	if o.name != "" {
		r, _ := integrations.Lookup(o.name)
		scopes := integrations.Adapters[r.Harness].Scopes()
		if o.scope == "" {
			o.scope = scopes[0]
		}
		if !slices.Contains(scopes, o.scope) {
			return o, errnorm.Usage("invalid_flags", "scope must be one of: "+strings.Join(scopes, ", "))
		}
	} else if o.scope != "" {
		allowed := false
		for _, adapter := range integrations.Adapters {
			allowed = allowed || slices.Contains(adapter.Scopes(), o.scope)
		}
		if !allowed {
			return o, errnorm.Usage("invalid_flags", "unknown harness settings scope")
		}
	}
	if len(o.board) > 256 {
		return o, errnorm.Usage("invalid_flags", "board ref exceeds 256 bytes")
	}
	if o.board != "" {
		if err := validateID(o.board, "board ref"); err != nil {
			return o, err
		}
	}
	if o.column != "" {
		switch o.column {
		case "backlog", "ready", "in_progress", "blocked", "review", "done":
		default:
			return o, errnorm.Usage("invalid_flags", "unknown column key")
		}
	}
	if o.installation != "" && (len(o.installation) != 24 || strings.Trim(o.installation, "0123456789abcdef") != "") {
		return o, errnorm.Usage("invalid_flags", "invalid installation id")
	}
	return o, nil
}
func integrationsUsageText() string {
	usage := strings.TrimSpace(`Optional harness hooks. Disabled until explicitly enabled.

Usage:
  anx integrations list [--scope <scope>]
  anx integrations status <name> [--scope <scope>]
  anx integrations enable <name> [--scope <scope>] [--board <ref>] [--column <key>] [--workspace <alias>] [--as <name>] [--dry-run]
  anx integrations disable <name> [--scope <scope>] [--dry-run]
  anx integrations run <name> < hook-event.json

Enable preserves unrelated harness settings and creates a one-time .anx-backup.
Dry-run returns the exact settings/config JSON and writes nothing. Each harness
adapter defines its scopes and default; project scopes use the current directory.
Workspace routing follows the hook event cwd unless --workspace pins an alias.
Board choices are stored per workspace; missing boards spool with reason no_board.
Run is a silent, bounded hook entrypoint, always exits 0, and retries a bounded
local spool. Status exposes pending events, dead letters, ledger count, and drops.
Use status/disable with the same scope and project directory used for enable.`)
	scopes := []string{}
	seen := map[string]bool{}
	for _, r := range integrations.Recipes {
		if seen[r.Harness] {
			continue
		}
		seen[r.Harness] = true
		allowed := integrations.Adapters[r.Harness].Scopes()
		scopes = append(scopes, "  "+r.Harness+": "+strings.Join(allowed, "|")+" (default: "+allowed[0]+")")
	}
	return usage + "\n\nHarness scopes:\n" + strings.Join(scopes, "\n")
}
func init() {
	for _, verb := range integrationsSpec.valid {
		localHelperTopics = append(localHelperTopics, localHelperTopic{Path: "integrations " + verb, Summary: "Opt-in harness integration: " + verb + ".", QuickStart: "anx integrations " + verb, Composition: "Local settings and durable bounded spool; no hooks installed by default.", Examples: integrationsSpec.examples})
	}
}
func (a *App) integrationLocation(cfg config.Resolved, r integrations.Recipe, scope string) (string, string, string, string, error) {
	if scope == "" {
		scope = integrations.Adapters[r.Harness].Scopes()[0]
	}
	dir, err := a.configDir(cfg)
	if err != nil {
		return "", "", "", "", err
	}
	home, err := a.UserHomeDir()
	if err != nil {
		return "", "", "", "", err
	}
	cwd, err := a.workspaceCWD()
	if err != nil {
		return "", "", "", "", err
	}
	path, err := integrations.Adapters[r.Harness].ConfigPath(home, cwd, scope)
	return dir, cwd, path, integrations.InstallationID(r.Name, path), err
}
func integrationError(err error) error {
	return errnorm.Wrap(errnorm.KindLocal, "integration_config_invalid", "cannot inspect or update integration settings", err)
}
func (a *App) integrationStatus(cfg config.Resolved, r integrations.Recipe, scope string) (map[string]any, error) {
	if scope == "" {
		scope = integrations.Adapters[r.Harness].Scopes()[0]
	}
	dir, _, path, id, err := a.integrationLocation(cfg, r, scope)
	if err != nil {
		return nil, err
	}
	c, err := integrations.LoadConfig(integrations.ConfigPath(dir, r.Name, id))
	exists := err == nil
	invalidConfig := err != nil && !errors.Is(err, os.ErrNotExist)
	if invalidConfig {
		c = integrations.Config{}
	}
	raw, err := integrations.ReadSettings(path)
	if err != nil {
		return nil, integrationError(err)
	}
	inspectCommand := c.Command
	if !exists {
		if executable, exeErr := os.Executable(); exeErr == nil {
			inspectCommand = integrations.HookCommand(executable, dir, r.Name, id)
		}
	}
	present, matches, err := integrations.Adapters[r.Harness].Inspect(raw, r, inspectCommand)
	if err != nil {
		return nil, integrationError(err)
	}
	state := "disabled"
	if invalidConfig || !exists && present {
		state = "drifted"
	}
	configurationMatches := exists && c.Name == r.Name && c.ID == id && c.Scope == scope && c.SettingsPath == path && c.Command == integrations.HookCommand(c.Executable, dir, r.Name, id)
	executableExists := false
	if exists {
		st, statErr := os.Stat(c.Executable)
		executableExists = statErr == nil && st.Mode().IsRegular() && st.Mode().Perm()&0111 != 0
		state = "drifted"
		if present && matches && executableExists && configurationMatches {
			state = "enabled"
		}
	}
	q, err := integrations.LoadQueue(integrations.QueuePath(dir, r.Name, id))
	if err != nil {
		return nil, integrationError(err)
	}
	reasons := map[string]int{}
	for _, p := range q.Pending {
		reasons[p.Reason]++
	}
	result := map[string]any{"name": r.Name, "harness": r.Harness, "description": r.Description, "scope": scope, "state": state, "settings_path": path, "hook_present": present, "hook_matches": matches, "config_matches": configurationMatches, "executable_exists": executableExists, "config": nil, "spool_depth": len(q.Pending), "dead_letter_count": len(q.DeadLetters), "dead_letter_reasons": integrationReasons(q.DeadLetters), "spool_reasons": reasons, "ledger_count": len(q.Ledger), "dropped": q.Dropped, "last_drop_reason": q.LastDropReason}
	if exists {
		result["config"] = c
	}
	if invalidConfig {
		result["config_error"] = "config_invalid"
	}
	return result, nil
}
func (a *App) runIntegrations(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if newHookIntegration(args) {
		return a.runHookIntegration(ctx, args, cfg)
	}
	o, err := parseIntegrationOptions(args)
	name := "integrations " + o.verb
	if err != nil {
		return nil, name, err
	}
	if o.verb == "" {
		return &commandResult{Text: integrationsUsageText()}, "integrations", nil
	}
	if o.verb == "list" {
		items := []any{}
		for _, r := range integrations.Recipes {
			if o.scope != "" && !slices.Contains(integrations.Adapters[r.Harness].Scopes(), o.scope) {
				continue
			}
			s, err := a.integrationStatus(cfg, r, o.scope)
			if err != nil {
				return nil, name, err
			}
			items = append(items, s)
		}
		dir, err := a.configDir(cfg)
		if err != nil {
			return nil, name, err
		}
		hooks, err := integrations.LoadHooks(dir)
		if err != nil {
			return nil, name, err
		}
		for _, recipe := range []string{"context.inject", "git.capture"} {
			enabled := false
			installed := []integrations.HookInstallation{}
			for _, i := range hooks.Installations {
				if i.Recipe == recipe {
					installed = append(installed, i)
					enabled = enabled || i.Enabled
				}
			}
			items = append(items, map[string]any{"name": recipe, "enabled": enabled, "installations": installed, "harnesses": []string{"generic", "claude-code", "codex", "cursor"}, "status_command": "anx integrations status " + recipe})
		}
		return &commandResult{Data: map[string]any{"integrations": items}}, name, nil
	}
	r, _ := integrations.Lookup(o.name)
	if o.verb == "status" {
		s, err := a.integrationStatus(cfg, r, o.scope)
		return &commandResult{Data: s}, name, err
	}
	if o.verb != "enable" && o.verb != "disable" {
		return nil, name, errnorm.Usage("invalid_args", "hook execution must use the silent entrypoint")
	}
	dir, project, path, id, err := a.integrationLocation(cfg, r, o.scope)
	if err != nil {
		return nil, name, err
	}
	configPath := integrations.ConfigPath(dir, r.Name, id)
	// Refuse an invalid existing file before even creating auxiliary locks.
	// Re-read and validate again under the locks before the actual mutation.
	probe, err := integrations.ReadSettings(path)
	if err != nil {
		return nil, name, integrationError(err)
	}
	if _, _, err = integrations.Adapters[r.Harness].Merge(probe, r, "", "", false); err != nil {
		return nil, name, integrationError(err)
	}
	if !o.dry {
		unlock, err := integrations.Lock(ctx, configPath+".lock")
		if err != nil {
			return nil, name, integrationError(err)
		}
		defer unlock()
		unlockSettings, err := integrations.Lock(ctx, path+".anx-lock")
		if err != nil {
			return nil, name, integrationError(err)
		}
		defer unlockSettings()
	}
	old, loadErr := integrations.LoadConfig(configPath)
	exists := loadErr == nil
	if loadErr != nil && !errors.Is(loadErr, os.ErrNotExist) {
		return nil, name, integrationError(loadErr)
	}
	raw, err := integrations.ReadSettings(path)
	if err != nil {
		return nil, name, integrationError(err)
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, name, err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return nil, name, err
	}
	oldJSON := string(integrations.JSON(old))
	c := old
	if o.verb == "enable" {
		c.Name = r.Name
		c.ID = id
		c.Scope = o.scope
		c.Project = project
		if !integrations.Adapters[r.Harness].ProjectScoped(o.scope) {
			c.Project = ""
		}
		c.SettingsPath = path
		c.Executable = executable
		c.Command = integrations.HookCommand(executable, dir, r.Name, id)
		if c.Agent == "" {
			c.Agent = r.DefaultAgent
		}
		if cfg.As != "" {
			c.Agent = cfg.As
		}
		if !agentNamePattern.MatchString(c.Agent) {
			return nil, name, errnorm.Usage("invalid_flags", "--as must be a lowercase agent name (letters, digits, hyphens; max 32)")
		}
		if alias := cfg.Sources["integration_workspace"]; alias != "" {
			c.Workspace = alias
		}
		if c.Boards == nil {
			c.Boards = map[string]integrations.Target{}
		}
		if o.board != "" {
			routed := cfg
			var alias *string
			if c.Workspace != "" {
				alias = &c.Workspace
			}
			routed, err = a.resolveIntegrationWorkspace(routed, alias)
			if err != nil {
				return nil, name, err
			}
			c.Boards[routed.BaseURL] = integrations.Target{Board: o.board, Column: o.column}
		} else if o.column != "" {
			return nil, name, errnorm.Usage("invalid_flags", "--column requires --board")
		}
	}
	command := c.Command
	if command == "" {
		command = integrations.HookCommand(executable, dir, r.Name, id)
	}
	next, settingsChanged, err := integrations.Adapters[r.Harness].Merge(raw, r, command, old.Command, o.verb == "enable")
	if err != nil {
		return nil, name, integrationError(err)
	}
	configChanged := exists
	if o.verb == "enable" {
		configChanged = !exists || oldJSON != string(integrations.JSON(c))
	}
	changed := settingsChanged || configChanged
	result := map[string]any{"name": r.Name, "changed": changed, "dry_run": o.dry, "scope": o.scope, "settings_path": path, "config_path": configPath, "settings_json": json.RawMessage(next), "config_json": nil}
	if o.verb == "enable" {
		result["config_json"] = c
	}
	if o.dry || !changed {
		return &commandResult{Data: result}, name, nil
	}
	if o.verb == "enable" && configChanged {
		if err = integrations.AtomicWrite(configPath, integrations.JSON(c)); err != nil {
			return nil, name, integrationError(err)
		}
	}
	if settingsChanged {
		next, settingsChanged, err = integrations.UpdateSettings(path, func(latest []byte) ([]byte, bool, error) {
			return integrations.Adapters[r.Harness].Merge(latest, r, command, old.Command, o.verb == "enable")
		})
		if err != nil {
			return nil, name, integrationError(err)
		}
		result["settings_json"] = json.RawMessage(next)
		result["changed"] = settingsChanged || configChanged
	}
	if o.verb == "disable" && exists {
		// Inspect the file actually on disk before deleting setup state. If
		// removal failed or an external writer restored the hook, retain config.
		latest, readErr := integrations.ReadSettings(path)
		if readErr != nil {
			return nil, name, integrationError(readErr)
		}
		present, _, inspectErr := integrations.Adapters[r.Harness].Inspect(latest, r, command)
		if inspectErr != nil {
			return nil, name, integrationError(inspectErr)
		}
		if present {
			return nil, name, integrationError(fmt.Errorf("owned hook remains; integration config retained"))
		}
		if err = os.Remove(configPath); err != nil {
			return nil, name, integrationError(err)
		}
	}
	return &commandResult{Data: result}, name, nil
}

// Bypass envelopes, identity preflight and background refresh. Even blocked stdin
// or an existing auth-token lock cannot hold the harness beyond this deadline.
func (a *App) runIntegrationHook(overrides config.Overrides, args []string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		defer func() {
			if recover() != nil {
				finished <- fmt.Errorf("hook_panic")
			}
		}()
		finished <- a.processIntegrationHook(ctx, overrides, args)
	}()
	select {
	case err := <-finished:
		if err != nil {
			fmt.Fprintln(a.Stderr, "anx integration: event queued or unavailable; inspect integrations status")
		}
	case <-ctx.Done():
		fmt.Fprintln(a.Stderr, "anx integration: time budget reached; inspect integrations status")
	}
	return 0
}
func (a *App) processIntegrationHook(ctx context.Context, overrides config.Overrides, args []string) error {
	o, err := parseIntegrationOptions(append([]string{"run"}, args...))
	if err != nil {
		return err
	}
	cfg, err := config.Resolve(overrides, config.Environment{Getenv: a.Getenv, UserHomeDir: a.UserHomeDir})
	if err != nil {
		return err
	}
	r, _ := integrations.Lookup(o.name)
	dir, _, _, id, err := a.integrationLocation(cfg, r, "")
	if err != nil {
		return err
	}
	if o.installation != "" {
		id = o.installation
	}
	raw, err := a.readStdinBytes(integrations.MaxPayloadBytes + 1)
	if err != nil {
		return err
	}
	event, err := r.Parse(raw)
	if err != nil {
		return err
	}
	event.Timestamp = a.clockNow().UTC()
	event.Host, _ = os.Hostname()
	path := integrations.QueuePath(dir, r.Name, id)
	c, loadErr := integrations.LoadConfig(integrations.ConfigPath(dir, r.Name, id))
	reason := "pending"
	if errors.Is(loadErr, os.ErrNotExist) {
		reason = "missing_config"
	} else if loadErr != nil || c.Name != r.Name || c.ID != id || c.Agent == "" {
		reason = "config_invalid"
	}
	if err = integrations.Enqueue(ctx, path, event, reason); err != nil {
		return err
	}
	if reason != "pending" {
		return fmt.Errorf("%s", reason)
	}
	return a.drainIntegrationQueue(ctx, cfg, dir, r, c, path, nil, event.Key)
}
func (a *App) drainIntegrationQueue(ctx context.Context, cfg config.Resolved, dir string, r integrations.Recipe, c integrations.Config, path string, admit func(integrations.Pending) bool, priority ...string) error {
	prepare := func(e integrations.Event) (integrations.Delivery, error) {
		routed := cfg
		local := *a
		local.Getwd = func() (string, error) { return e.CWD, nil }
		var alias *string
		if c.Workspace != "" {
			alias = &c.Workspace
		}
		routed, routeErr := local.resolveIntegrationWorkspace(routed, alias)
		if routeErr != nil {
			return integrations.Delivery{}, fmt.Errorf("workspace_unresolved")
		}
		target, ok := c.Boards[routed.BaseURL]
		if !ok || target.Board == "" {
			return integrations.Delivery{}, fmt.Errorf("no_board")
		}
		return integrations.Delivery{BaseURL: routed.BaseURL, Agent: c.Agent, CommandID: r.CommandID, Body: r.BuildBody(e, target)}, nil
	}
	send := func(sendCtx context.Context, d integrations.Delivery) error {
		routed := cfg
		routed.BaseURL = d.BaseURL
		routed.As = d.Agent
		routed.AccessToken = ""
		routed.Sources = map[string]string{"base_url": "integration", "agent": "integration"}
		routed.Timeout = time.Second
		host, err := a.resolvedHost(routed)
		if err != nil {
			return err
		}
		if d.HostID != "" && d.HostID != host.ID {
			return integrations.ErrIdentityChanged
		}
		key := d.Key
		auth, err := a.resolveHostAgent(sendCtx, routed)
		if err != nil {
			return err
		}
		if auth.HostID != host.ID {
			return integrations.ErrIdentityChanged
		}
		principal := "actor:" + auth.ActorID
		if auth.ActorID == "" {
			if auth.AgentID == "" {
				return errnorm.Local("identity_unresolved", "derived principal is unavailable")
			}
			principal = "agent:" + auth.AgentID
		}
		if err = integrations.BindHost(sendCtx, path, key, host.ID, principal); err != nil {
			return err
		}
		// Use this already-resolved bearer for the bounded call. Do not allow
		// a second auth refresh to switch hosts after the durable binding.
		auth.HostID = ""
		_, err = a.invokeTypedJSON(sendCtx, auth, "integrations run", d.CommandID, nil, nil, d.Body)
		var remote *errnorm.Error
		if errors.As(err, &remote) && remote.Kind == errnorm.KindRemote {
			details, _ := remote.Details.(map[string]any)
			status, _ := details["status"].(int)
			switch status {
			case 400, 422:
				return integrations.RejectedDelivery{Status: status}
			case 401:
				return integrations.DeferredDelivery{Reason: "auth"}
			case 403:
				return integrations.DeferredDelivery{Reason: "forbidden"}
			case 404:
				return integrations.DeferredDelivery{Reason: "board_not_found"}
			}
		}
		return err
	}
	if admit != nil {
		return integrations.DrainSelected(ctx, path, prepare, send, admit)
	}
	return integrations.Drain(ctx, path, prepare, send, priority...)
}

// Integrations route from event cwd, independently of the invoking shell's API
// override. Copy Sources so ordinary command resolution retains its precedence.
func (a *App) resolveIntegrationWorkspace(cfg config.Resolved, alias *string) (config.Resolved, error) {
	sources := make(map[string]string, len(cfg.Sources))
	for k, v := range cfg.Sources {
		sources[k] = v
	}
	cfg.Sources = sources
	if alias == nil {
		cfg.BaseURL = config.DefaultBaseURL
		cfg.Sources["base_url"] = "default"
	}
	return a.resolveWorkspace(cfg, alias)
}

func integrationReasons(entries []integrations.Pending) map[string]int {
	reasons := map[string]int{}
	for _, p := range entries {
		reasons[p.Reason]++
	}
	return reasons
}
