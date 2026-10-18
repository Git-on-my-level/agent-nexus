package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/integrations"
)

type hookOptions struct {
	verb, recipe, harness, scope, card, session, directory, id string
	dry, stale                                                 bool
}

func newHookIntegration(args []string) bool {
	return len(args) > 0 && (args[0] == "bind" || args[0] == "map" || len(args) > 1 && integrations.HookRecipe(args[1]))
}
func parseHookOptions(args []string) (hookOptions, error) {
	o := hookOptions{harness: "generic", scope: "project"}
	if len(args) == 0 {
		return o, errnorm.Usage("invalid_args", "integration command required")
	}
	o.verb = args[0]
	rest := args[1:]
	if o.verb != "bind" && o.verb != "map" {
		if len(rest) == 0 || !integrations.HookRecipe(rest[0]) {
			return o, errnorm.Usage("unknown_integration", "expected context.inject or git.capture")
		}
		o.recipe = rest[0]
		rest = rest[1:]
	}
	if o.verb == "map" && len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		o.directory = rest[0]
		rest = rest[1:]
	}
	fs := newSilentFlagSet("integrations " + o.verb)
	fs.StringVar(&o.harness, "harness", "generic", "generic, claude-code, codex or cursor")
	fs.StringVar(&o.scope, "scope", "project", "user, project or Claude local")
	if o.verb == "enable" || o.verb == "bind" || o.verb == "map" {
		fs.StringVar(&o.card, "card-id", "", "Explicit card ref")
	}
	if o.verb == "bind" {
		fs.StringVar(&o.session, "session-id", "", "Local harness session ID")
	}
	if o.verb == "enable" {
		fs.BoolVar(&o.stale, "allow-stale", false, "Allow marked cache up to five minutes old while offline")
	}
	if o.verb == "enable" || o.verb == "disable" || o.verb == "bind" || o.verb == "map" {
		fs.BoolVar(&o.dry, "dry-run", false, "Preview without writing")
	}
	if e := fs.Parse(rest); e != nil {
		return o, errnorm.Usage("invalid_flags", e.Error())
	}
	if len(fs.Args()) > 0 {
		return o, errnorm.Usage("invalid_args", "unexpected arguments")
	}
	if o.verb != "enable" && o.verb != "disable" && o.verb != "status" && o.verb != "bind" && o.verb != "map" {
		return o, errnorm.Usage("unknown_subcommand", "expected enable, disable, status, bind or map")
	}
	if _, e := integrations.HookAdapterPath("/home", "/project", o.harness, o.scope); e != nil {
		return o, errnorm.Usage("invalid_flags", e.Error())
	}
	if o.card != "" && !integrations.ValidCard(o.card) || ((o.verb == "bind" || o.verb == "map") && !integrations.ValidCard(o.card)) {
		return o, errnorm.Usage("invalid_card", "use an explicit card: ref")
	}
	if o.verb == "bind" && (o.session == "" || len(o.session) > 1024 || strings.ContainsAny(o.session, "\x00\r\n")) {
		return o, errnorm.Usage("invalid_args", "--session-id is required")
	}
	if o.verb == "map" && o.directory == "" {
		return o, errnorm.Usage("invalid_args", "directory is required")
	}
	return o, nil
}
func (a *App) hookWorkspace(cfg config.Resolved, cwd, alias string) (config.Resolved, error) {
	local := *a
	local.Getwd = func() (string, error) { return cwd, nil }
	if alias == "" {
		alias = cfg.Sources["integration_workspace"]
	}
	var choice *string
	if alias != "" {
		choice = &alias
	}
	return local.resolveWorkspace(cfg, choice)
}
func (a *App) runHookIntegration(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	o, e := parseHookOptions(args)
	name := "integrations " + o.verb
	if e != nil {
		return nil, name, e
	}
	dir, e := a.configDir(cfg)
	if e != nil {
		return nil, name, e
	}
	cwd, e := a.workspaceCWD()
	if e != nil {
		return nil, name, e
	}
	cwd, e = filepath.EvalSymlinks(cwd)
	if e != nil {
		return nil, name, e
	}
	home, e := a.UserHomeDir()
	if e != nil {
		return nil, name, e
	}
	path, e := integrations.HookAdapterPath(home, cwd, o.harness, o.scope)
	if e != nil {
		return nil, name, e
	}
	scopeRoot := cwd
	if o.scope == "user" {
		scopeRoot = home
	}
	id := integrations.InstallationID(o.recipe+"/"+o.harness, firstNonEmpty(path, scopeRoot+"/"+o.scope))
	s, e := integrations.LoadHooks(dir)
	if e != nil {
		return nil, name, e
	}
	var old integrations.HookInstallation
	for _, v := range s.Installations {
		if v.ID == id {
			old = v
		}
	}
	if o.verb == "status" {
		data := map[string]any{"installation": old, "enabled": old.Enabled, "bindings": len(s.Bindings), "rules": len(s.Rules)}
		if old.ID != "" && path != "" {
			raw, err := integrations.ReadSettings(path)
			if err != nil {
				return nil, name, err
			}
			_, drift, err := integrations.MergeHook(raw, old.Harness, old.Recipe, old.Command, "", old.Enabled)
			if err != nil {
				return nil, name, err
			}
			data["hook_matches"] = !drift
		}
		if o.harness == "codex" {
			data["harness_trust"] = "review_in_harness"
		}
		return &commandResult{Data: data}, name, nil
	}
	if o.verb == "disable" && old.ID == "" {
		return &commandResult{Data: map[string]any{"changed": false}}, name, nil
	}
	routed := cfg
	if o.verb != "disable" {
		routed, e = a.hookWorkspace(cfg, cwd, "")
		if e != nil {
			return nil, name, e
		}
	}
	agent := routed.As
	if agent == "" {
		agent, _, e = a.identityName(routed)
		if e != nil && o.verb != "disable" {
			return nil, name, e
		}
	}
	if o.verb == "bind" || o.verb == "map" {
		// Preview performs no grant/cache mutation. Access is validated on enable.
		if o.dry {
			return &commandResult{Data: map[string]any{"dry_run": true, "card_ref": o.card, "session_hash": integrations.SessionKey(o.harness, o.session), "directory": o.directory, "workspace": routed.BaseURL}}, name, nil
		}
		auth, e := a.resolveHostAgent(ctx, routed)
		if e != nil {
			return nil, name, e
		}
		if _, e = a.fetchCardBody(ctx, auth, o.card); e != nil {
			return nil, name, e
		}
		directory := o.directory
		if o.verb == "map" {
			if !filepath.IsAbs(directory) {
				directory = filepath.Join(cwd, directory)
			}
			directory, e = filepath.Abs(directory)
			if e == nil {
				directory, e = filepath.EvalSymlinks(directory)
			}
			if e != nil {
				return nil, name, e
			}
		}
		binding := integrations.HookBinding{Workspace: auth.BaseURL, Actor: auth.ActorID, Session: integrations.SessionKey(o.harness, o.session), Card: o.card}
		rule := integrations.HookRule{Workspace: auth.BaseURL, Actor: auth.ActorID, Directory: directory, Card: o.card}
		if !o.dry {
			e = integrations.UpdateHooks(ctx, dir, func(s *integrations.HookSettings) error {
				if o.verb == "bind" {
					for j, v := range s.Bindings {
						if v.Workspace == binding.Workspace && v.Actor == binding.Actor && v.Session == binding.Session {
							s.Bindings[j] = binding
							return nil
						}
					}
					s.Bindings = append(s.Bindings, binding)
				} else {
					for j, v := range s.Rules {
						if v.Workspace == rule.Workspace && v.Actor == rule.Actor && v.Directory == rule.Directory {
							s.Rules[j] = rule
							return nil
						}
					}
					s.Rules = append(s.Rules, rule)
				}
				return nil
			})
		}
		return &commandResult{Data: map[string]any{"binding": binding, "rule": rule, "dry_run": o.dry}}, name, e
	}
	exe, e := os.Executable()
	if e != nil {
		return nil, name, e
	}
	event := "session.start"
	if o.recipe == "git.capture" {
		event = "tool.after"
	}
	command := integrations.ShellQuote(exe) + " --config-dir " + integrations.ShellQuote(dir) + " hook " + event + " --harness " + o.harness + " --installation " + id
	install := integrations.HookInstallation{ID: id, Recipe: o.recipe, Harness: o.harness, Scope: o.scope, Project: cwd, SettingsPath: path, Command: command, Agent: agent, Workspace: routed.BaseURL, Card: o.card, AllowStale: o.stale, Enabled: o.verb == "enable"}
	if o.verb == "disable" {
		install = old
		install.Enabled = false
	}
	var next []byte
	changed := false
	if path != "" {
		raw, e := integrations.ReadSettings(path)
		if e != nil {
			return nil, name, e
		}
		next, changed, e = integrations.MergeHook(raw, o.harness, o.recipe, command, old.Command, install.Enabled)
		if e != nil {
			return nil, name, e
		}
	}
	if o.dry {
		return &commandResult{Data: map[string]any{"installation": install, "settings": json.RawMessage(firstNonEmpty(string(next), "{}")), "changed": changed, "dry_run": true}}, name, nil
	}
	// Enable config last; disable it first. An interrupted install cannot turn on
	// an unconfigured hook; retry repairs only the exact owned command.
	if !install.Enabled {
		e = saveHookInstallation(ctx, dir, install)
		if e != nil {
			return nil, name, e
		}
	}
	if path != "" {
		unlock, e := integrations.Lock(ctx, path+".anx-lock")
		if e != nil {
			return nil, name, e
		}
		defer unlock()
		_, changed, e = integrations.UpdateSettings(path, func(raw []byte) ([]byte, bool, error) {
			return integrations.MergeHook(raw, o.harness, o.recipe, command, old.Command, install.Enabled)
		})
		if e != nil {
			return nil, name, e
		}
	}
	if install.Enabled {
		e = saveHookInstallation(ctx, dir, install)
	}
	return &commandResult{Data: map[string]any{"installation": install, "changed": changed, "dry_run": false}}, name, e
}
func saveHookInstallation(ctx context.Context, dir string, i integrations.HookInstallation) error {
	return integrations.UpdateHooks(ctx, dir, func(s *integrations.HookSettings) error {
		for j, v := range s.Installations {
			if v.ID == i.ID {
				s.Installations[j] = i
				return nil
			}
		}
		s.Installations = append(s.Installations, i)
		return nil
	})
}

type agentHookOptions struct{ event, harness, id, card string }

func parseAgentHook(args []string) (agentHookOptions, error) {
	o := agentHookOptions{harness: "generic"}
	if len(args) == 0 {
		return o, fmt.Errorf("event required")
	}
	o.event = args[0]
	switch o.event {
	case "session.start", "context.compacted", "tool.after", "git.commit":
	default:
		return o, fmt.Errorf("unknown event")
	}
	fs := newSilentFlagSet("hook")
	fs.StringVar(&o.harness, "harness", "generic", "")
	fs.StringVar(&o.id, "installation", "", "")
	fs.StringVar(&o.card, "card-id", "", "")
	if e := fs.Parse(args[1:]); e != nil {
		return o, e
	}
	if len(fs.Args()) > 0 {
		return o, fmt.Errorf("unexpected arguments")
	}
	if o.harness != "generic" && o.harness != "claude-code" && o.harness != "codex" && o.harness != "cursor" && o.harness != "git" {
		return o, fmt.Errorf("unsupported harness")
	}
	if o.card != "" && !integrations.ValidCard(o.card) {
		return o, fmt.Errorf("invalid card")
	}
	if o.id != "" && (len(o.id) != 24 || strings.Trim(o.id, "0123456789abcdef") != "") {
		return o, fmt.Errorf("invalid installation")
	}
	return o, nil
}
func (a *App) runAgentHook(overrides config.Overrides, args []string) int {
	o, e := parseAgentHook(args)
	if e != nil {
		fmt.Fprintln(a.Stderr, "anx hook: invalid invocation")
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	type done struct {
		b integrations.HookBrief
		e error
	}
	ch := make(chan done, 1)
	go func() {
		defer func() {
			if recover() != nil {
				ch <- done{integrations.EmptyBrief(), fmt.Errorf("hook panic")}
			}
		}()
		b, e := a.processAgentHook(ctx, overrides, o)
		ch <- done{b, e}
	}()
	result := done{b: integrations.EmptyBrief()}
	select {
	case result = <-ch:
	case <-ctx.Done():
		if closer, ok := a.Stdin.(io.Closer); ok && o.harness != "git" {
			_ = closer.Close()
		}
		result.e = ctx.Err()
	}
	if result.e != nil {
		var mismatch *captureWriterMismatch
		if errors.As(result.e, &mismatch) {
			fmt.Fprintf(a.Stderr, "anx hook: %s; inspect anx git status\n", mismatch)
		} else {
			fmt.Fprintf(a.Stderr, "anx hook: unavailable code=%s; inspect integrations status or git status\n", hookFailureCode(result.e))
		}
	}
	a.writeOutput(a.Stdout, string(integrations.RenderHook(o.harness, result.b)))
	return 0
}
func hookFailureCode(e error) string {
	if errors.Is(e, context.DeadlineExceeded) {
		return "timeout"
	}
	var n *errnorm.Error
	if errors.As(e, &n) {
		code := n.Code
		if len(code) <= 64 && code != "" && strings.Trim(code, "abcdefghijklmnopqrstuvwxyz0123456789_") == "" {
			return code
		}
	}
	return "local_failure"
}
func (a *App) processAgentHook(ctx context.Context, overrides config.Overrides, o agentHookOptions) (integrations.HookBrief, error) {
	empty := integrations.EmptyBrief()
	cfg, e := config.Resolve(overrides, config.Environment{Getenv: a.Getenv, UserHomeDir: a.UserHomeDir})
	if e != nil {
		return empty, e
	}
	dir, e := a.configDir(cfg)
	if e != nil {
		return empty, e
	}
	if o.event == "git.commit" && o.harness == "git" {
		cfg.Timeout = time.Second
		cwd, e := a.workspaceCWD()
		if e != nil {
			return empty, e
		}
		_, e = a.syncCapture(ctx, cfg, cwd, "", "", "", false)
		if errors.Is(e, os.ErrNotExist) {
			return empty, nil
		}
		return empty, e
	}
	settings, err := integrations.LoadHooks(dir)
	if err != nil {
		return empty, err
	}
	available := false
	for _, i := range settings.Installations {
		if i.Enabled && i.Harness == o.harness && (o.id == "" || o.id == i.ID) {
			available = true
		}
	}
	if !available {
		return empty, nil
	}
	raw, e := a.readStdinBytes(integrations.MaxPayloadBytes + 1)
	if e != nil {
		return empty, e
	}
	event, e := integrations.ParseHook(o.harness, o.event, raw)
	if e != nil {
		return empty, e
	}
	if o.card != "" {
		event.Card = o.card
	}
	event.CWD, e = filepath.EvalSymlinks(event.CWD)
	if e != nil {
		return empty, e
	}
	s, e := integrations.LoadHooks(dir)
	if e != nil {
		return empty, e
	}
	var install integrations.HookInstallation
	matches := 0
	recipe := "context.inject"
	if o.event == "tool.after" || o.event == "git.commit" {
		recipe = "git.capture"
	}
	for _, i := range s.Installations {
		if !i.Enabled || i.Harness != o.harness || i.Recipe != recipe {
			continue
		}
		if o.id != "" && i.ID != o.id {
			continue
		}
		if i.Scope != "user" {
			rel, e := filepath.Rel(i.Project, event.CWD)
			if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
		}
		install = i
		matches++
	}
	if matches == 0 {
		return empty, nil
	}
	if matches > 1 {
		return empty, fmt.Errorf("ambiguous installation; pass --installation")
	}
	if recipe == "git.capture" {
		cfg.Timeout = time.Second
		cfg.As, cfg.BaseURL = install.Agent, install.Workspace
		cfg.Sources = map[string]string{"base_url": "hook"}
		_, e = a.syncCapture(ctx, cfg, event.CWD, "", "", "", false)
		if errors.Is(e, os.ErrNotExist) {
			return empty, nil
		}
		return empty, e
	}
	cfg.As = install.Agent
	cfg.AccessToken = ""
	cfg.BaseURL = install.Workspace
	cfg.Sources = map[string]string{"base_url": "hook", "agent": "hook"}
	cfg.Timeout = time.Second
	// Do not run an optional 2s discovery subprocess on the 1.5s path. Installed
	// hooks have an explicit agent; no inherited harness can replace it.
	local := *a
	local.runtimeIdentity = nil
	auth, e := local.resolveHostAgent(ctx, cfg)
	if e != nil {
		return empty, e
	}
	card, e := integrations.ResolveHookCard(s, install, event, auth.BaseURL, auth.ActorID)
	if e != nil {
		return empty, e
	}
	if card == "" {
		auth.HostID = "" // The installed principal is already authenticated.
		readCtx, cancel := context.WithTimeout(ctx, 1100*time.Millisecond)
		defer cancel()
		r, err := local.invokeRawJSON(readCtx, auth, "hook brief", "GET", "/brief", nil)
		if err != nil {
			return empty, err
		}
		body := commandResultBody(r)
		if anyString(body["schema"]) != "brief.v1" {
			return empty, errnorm.Local("invalid_response", "hook brief returned no brief.v1")
		}
		at := a.clockNow().UTC().Format(time.RFC3339Nano)
		return integrations.HookBrief{Version: 1, Context: renderHookOrient(body), AsOf: &at}, nil
	}
	key := integrations.Digest(auth.BaseURL + "\x00" + auth.HostID + "\x00" + auth.ActorID + "\x00" + card)
	cache := filepath.Join(dir, "hooks", "cache", key+".json")
	auth.HostID = "" // Use the already resolved bearer, without a second identity refresh.
	readCtx, cancel := context.WithTimeout(ctx, 1100*time.Millisecond)
	defer cancel()
	body, e := local.readHookContext(readCtx, auth, card)
	if e != nil {
		// Authorization failures never disclose a cached body; only uncertain network
		// and timeout failures with an unexpired local credential permit opt-in reuse.
		if install.AllowStale && uncertainHookError(e) && credentialCurrent(auth, a.clockNow()) {
			var b integrations.HookBrief
			if raw, err := integrations.PrivateRead(cache); err == nil && json.Unmarshal(raw, &b) == nil && b.AsOf != nil {
				at, err := time.Parse(time.RFC3339Nano, *b.AsOf)
				age := a.clockNow().Sub(at)
				if err == nil && age >= 0 && age <= 5*time.Minute {
					b.Stale = true
					if !strings.Contains(b.Context, hookSkillReminder) {
						b.Context = hookSkillReminder + "\n" + b.Context
					}
					b.Context = truncateHook("STALE as_of="+*b.AsOf+"\n"+b.Context, 6<<10)
					return b, nil
				}
			}
		}
		_ = os.Remove(cache)
		return empty, e
	}
	text := renderHookContext(card, body)
	at := a.clockNow().UTC().Format(time.RFC3339Nano)
	b := integrations.HookBrief{Version: 1, Context: text, Card: &card, AsOf: &at}
	if ctx.Err() == nil {
		if e = integrations.PrivateWrite(cache, integrations.JSON(b)); e != nil {
			return empty, e
		}
	}
	return b, nil
}

// These existing scoped GETs are independent. Read only the three components
// used in the brief, in parallel under one budget; refresh status is not injected.
// An access denial wins over a concurrent timeout, so stale fallback cannot mask it.
func (a *App) readHookContext(ctx context.Context, cfg config.Resolved, card string) (map[string]any, error) {
	type read struct {
		slot int
		body map[string]any
		err  error
	}
	paths := []string{"/work/" + url.PathEscape(card), "/work/" + url.PathEscape(card) + "/observations?limit=10", "/cards/" + url.PathEscape(card) + "/plan"}
	ch := make(chan read, len(paths))
	for slot, path := range paths {
		go func(slot int, path string) {
			local := *a
			r, e := local.invokeRawJSON(ctx, cfg, "hook context", "GET", path, nil)
			var body map[string]any
			if e == nil {
				body = commandResultBody(r)
				if body == nil {
					e = errnorm.Local("invalid_response", "hook read returned no object")
				}
			}
			ch <- read{slot, body, e}
		}(slot, path)
	}
	parts := make([]map[string]any, len(paths))
	var failure error
	for range paths {
		r := <-ch
		parts[r.slot] = r.body
		if r.err != nil && (failure == nil || uncertainHookError(failure) && !uncertainHookError(r.err)) {
			failure = r.err
		}
	}
	if failure != nil {
		return nil, failure
	}
	return map[string]any{"work": parts[0]["work"], "observations": parts[1], "coordination": parts[2]}, nil
}

func credentialCurrent(c config.Resolved, now time.Time) bool {
	at, e := time.Parse(time.RFC3339Nano, c.AccessTokenExpiresAt)
	return e == nil && now.Before(at)
}
func uncertainHookError(e error) bool {
	if errors.Is(e, context.DeadlineExceeded) {
		return true
	}
	var n *errnorm.Error
	if errors.As(e, &n) {
		if n.Kind == errnorm.KindNetwork {
			return true
		}
		if details, ok := n.Details.(map[string]any); ok {
			status, _ := details["status"].(int)
			return status >= 500 || status == 429
		}
		return false
	}
	return false
}
func renderHookContext(card string, body map[string]any) string {
	w := asMap(body["work"])
	lines := []string{hookSkillReminder, "ANX context " + card}
	for _, k := range []string{"title", "phase", "summary", "definition_of_done", "promise", "next", "plan"} {
		if v, ok := w[k]; ok {
			b, _ := json.Marshal(v)
			lines = append(lines, k+"="+truncateHook(string(b), 2000))
		}
	}
	coordination := asMap(body["coordination"])
	for _, k := range []string{"plan_health", "next_step", "plan"} {
		if v, ok := coordination[k]; ok {
			b, _ := json.Marshal(v)
			lines = append(lines, k+"="+truncateHook(string(b), 1500))
		}
	}
	// Observation preview is already a bounded page; do not dump source bodies.
	observations := asMap(body["observations"])
	for _, v := range asSlice(observations["items"]) {
		o := asMap(v)
		lines = append(lines, "evidence="+truncateHook(firstNonEmpty(anyString(o["summary"]), anyString(o["status"])), 160))
	}
	lines = append(lines, "next anx context "+card)
	return truncateHook(strings.Join(lines, "\n"), 6<<10)
}
func truncateHook(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// Sessions do not report a reliable skill contract version. Remind them on
// every context restoration, without reading harness-private skill files.
const hookSkillReminder = "Reload the anx-participant skill before continuing; its installed contract may have changed."

func renderHookOrient(brief map[string]any) string {
	compact := map[string]any{"principal": brief["principal"]}
	principal := asMap(brief["principal"])
	for _, section := range []string{"answered_for_you", "your_asks", "initiatives"} {
		items := []any{}
		for _, raw := range asSlice(brief[section]) {
			item := asMap(raw)
			if section == "initiatives" && !orientInitiativeIsYours(item, principal) {
				continue
			}
			items = append(items, item)
			if len(items) == 5 {
				break
			}
		}
		compact[section] = items
	}
	text := renderOrientText(compact, "")
	// Keep every section useful even when an answer or title is large.
	lines := strings.Split(strings.TrimSpace(text), "\n")
	next := lines[len(lines)-1]
	for i := range lines {
		lines[i] = truncateHook(lines[i], 320)
	}
	text = strings.Join(lines[:len(lines)-1], "\n")
	prefix := "ANX brief (up to 5 per section; anx orient for more)\n" + hookSkillReminder + "\n"
	suffix := "\n" + truncateHook(next, 512)
	return prefix + truncateHook(text, (6<<10)-len(prefix)-len(suffix)) + suffix
}
