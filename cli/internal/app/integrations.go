package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/integrations"
)

var integrationsSpec = subcommandSpec{command: "integrations", valid: []string{"list", "status", "enable", "disable", "run"}, examples: []string{"anx integrations list", "anx integrations enable claude-code-suggested-tasks --board follow-ups"}}

type integrationOptions struct {
	verb, name, scope, installation, board, column string
	dry                                            bool
}

func parseIntegrationOptions(args []string) (integrationOptions, error) {
	o := integrationOptions{scope: "user"}
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
		fs.StringVar(&o.scope, "scope", "user", "Settings scope: user, project, local")
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
	if o.scope != "user" && o.scope != "project" && o.scope != "local" {
		return o, errnorm.Usage("invalid_flags", "scope must be user, project, or local")
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
	return strings.TrimSpace(`Optional harness hooks. Disabled until explicitly enabled.

Usage:
  anx integrations list [--scope user|project|local]
  anx integrations status <name> [--scope user|project|local]
  anx integrations enable <name> [--scope user|project|local] [--board <ref>] [--column <key>] [--workspace <alias>] [--as <name>] [--dry-run]
  anx integrations disable <name> [--scope user|project|local] [--dry-run]
  anx integrations run <name>

Enable preserves unrelated harness settings and creates a one-time .anx-backup.
Dry-run returns the exact settings/config JSON and writes nothing. Project/local
scope uses the current directory as project root. User scope is the default.
Workspace routing follows the hook event cwd unless --workspace pins an alias.
Board choices are stored per workspace; missing boards spool with reason no_board.
Run is a silent, bounded hook entrypoint, always exits 0, and retries a bounded
local spool. Status exposes pending events, ledger count, and retention drops.
Use status/disable with the same scope and project directory used for enable.`)
}
func init() {
	for _, verb := range integrationsSpec.valid {
		localHelperTopics = append(localHelperTopics, localHelperTopic{Path: "integrations " + verb, Summary: "Opt-in harness integration: " + verb + ".", QuickStart: "anx integrations " + verb, Composition: "Local settings and durable bounded spool; no hooks installed by default.", Examples: integrationsSpec.examples})
	}
}
func (a *App) integrationLocation(cfg config.Resolved, r integrations.Recipe, scope string) (string, string, string, string, error) {
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
	dir, _, path, id, err := a.integrationLocation(cfg, r, scope)
	if err != nil {
		return nil, err
	}
	c, err := integrations.LoadConfig(integrations.ConfigPath(dir, r.Name, id))
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, integrationError(err)
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
	if !exists && present {
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
	result := map[string]any{"name": r.Name, "harness": r.Harness, "description": r.Description, "scope": scope, "state": state, "settings_path": path, "hook_present": present, "hook_matches": matches, "config_matches": configurationMatches, "executable_exists": executableExists, "config": nil, "spool_depth": len(q.Pending), "spool_reasons": reasons, "ledger_count": len(q.Ledger), "dropped": q.Dropped, "last_drop_reason": q.LastDropReason}
	if exists {
		result["config"] = c
	}
	return result, nil
}
func (a *App) runIntegrations(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
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
			s, err := a.integrationStatus(cfg, r, o.scope)
			if err != nil {
				return nil, name, err
			}
			items = append(items, s)
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
		if o.scope == "user" {
			c.Project = ""
		}
		c.SettingsPath = path
		c.Executable = executable
		c.Command = integrations.HookCommand(executable, dir, r.Name, id)
		if c.Agent == "" {
			c.Agent = "claude-code"
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
			routed, err = a.resolveWorkspace(routed, alias)
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
	if settingsChanged {
		if err = integrations.Backup(path, raw); err != nil {
			return nil, name, integrationError(err)
		}
	}
	if o.verb == "enable" && configChanged {
		if err = integrations.AtomicWrite(configPath, integrations.JSON(c)); err != nil {
			return nil, name, integrationError(err)
		}
	}
	if settingsChanged {
		if err = integrations.AtomicWrite(path, next); err != nil {
			return nil, name, integrationError(err)
		}
	}
	if o.verb == "disable" && exists {
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
	go func() { finished <- a.processIntegrationHook(ctx, overrides, args) }()
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
	dir, _, _, id, err := a.integrationLocation(cfg, r, "user")
	if err != nil {
		return err
	}
	if o.installation != "" {
		id = o.installation
	}
	c, err := integrations.LoadConfig(integrations.ConfigPath(dir, r.Name, id))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
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
	if err = integrations.Enqueue(ctx, path, event); err != nil {
		return err
	}
	return integrations.Drain(ctx, path, func(e integrations.Event) (integrations.Delivery, error) {
		routed := cfg
		routed.Sources = map[string]string{}
		for k, v := range cfg.Sources {
			routed.Sources[k] = v
		}
		local := *a
		local.Getwd = func() (string, error) { return e.CWD, nil }
		var alias *string
		if c.Workspace != "" {
			alias = &c.Workspace
		}
		routed, routeErr := local.resolveWorkspace(routed, alias)
		if routeErr != nil {
			return integrations.Delivery{}, fmt.Errorf("workspace_unresolved")
		}
		target, ok := c.Boards[routed.BaseURL]
		if !ok || target.Board == "" {
			return integrations.Delivery{}, fmt.Errorf("no_board")
		}
		return integrations.Delivery{BaseURL: routed.BaseURL, Agent: c.Agent, CommandID: r.CommandID, Body: r.BuildBody(e, target)}, nil
	}, func(sendCtx context.Context, d integrations.Delivery) error {
		routed := cfg
		routed.BaseURL = d.BaseURL
		routed.As = d.Agent
		routed.AccessToken = ""
		routed.Sources = map[string]string{"base_url": "integration", "agent": "integration"}
		routed.Timeout = time.Second
		_, err := a.invokeTypedJSON(sendCtx, routed, "integrations run", d.CommandID, nil, nil, d.Body)
		return err
	}, event.Key)
}
