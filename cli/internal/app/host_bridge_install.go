package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	hostbridge "agent-nexus-cli/internal/bridge"
	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/hostidentity"
	"agent-nexus-cli/internal/integrations"
	"github.com/pelletier/go-toml/v2"
)

var bridgeProcesses = hostbridge.Processes
var bridgePlatform = runtime.GOOS
var bridgeWaitReady = waitHostBridgeReady
var bridgeRetire = retireBridgeProcess

func serviceCall(ctx context.Context, kind, action string, unit hostbridge.UnitPlan) error {
	var argv []string
	if kind == "launchd" {
		domain := "gui/" + strconv.Itoa(os.Getuid())
		switch action {
		case "start", "load":
			argv = []string{"launchctl", "bootstrap", domain, unit.Path}
		case "stop":
			argv = []string{"launchctl", "bootout", domain + "/" + unit.Label}
		case "enable":
			argv = []string{"launchctl", "enable", domain + "/" + unit.Label}
		case "disable":
			argv = []string{"launchctl", "disable", domain + "/" + unit.Label}
		}
	} else {
		switch action {
		case "start":
			if _, _, e := bridgeCommandRun(ctx, "systemctl", "--user", "daemon-reload"); e != nil {
				return e
			}
			argv = []string{"systemctl", "--user", "enable", "--now", unit.Label}
		case "load":
			argv = []string{"systemctl", "--user", "start", unit.Label}
		case "stop":
			argv = []string{"systemctl", "--user", "stop", unit.Label}
		case "enable":
			argv = []string{"systemctl", "--user", "enable", unit.Label}
		case "disable":
			argv = []string{"systemctl", "--user", "disable", "--now", unit.Label}
		}
	}
	if len(argv) == 0 {
		return fmt.Errorf("unsupported service action")
	}
	out, stderr, e := bridgeCommandRun(ctx, argv[0], argv[1:]...)
	if e != nil && action == "stop" && (strings.Contains(out+stderr, "Could not find service") || strings.Contains(out+stderr, "No such process") || strings.Contains(out+stderr, "not loaded")) {
		return nil
	}
	// A retry may find the new unit already removed by an interrupted rollback.
	// Stop has already quiesced it; a missing unit has no enablement to undo.
	if e != nil && action == "disable" && kind == "systemd-user" && (strings.Contains(out+stderr, "does not exist") || strings.Contains(out+stderr, "not found")) {
		return nil
	}
	if e != nil {
		return fmt.Errorf("service %s failed for %s", action, unit.Label)
	}
	return nil
}
func (a *App) controlHostBridge(ctx context.Context, action string) (*commandResult, error) {
	home, e := a.bridgeHome()
	if e != nil {
		return nil, e
	}
	exe, e := os.Executable()
	if e != nil {
		return nil, e
	}
	unit, e := hostbridge.UnitFor(bridgePlatform, home, exe, os.Getuid())
	if e != nil {
		return nil, e
	}
	if e = serviceCall(ctx, unit.Kind, action, unit); e != nil {
		return nil, e
	}
	return &commandResult{Data: map[string]any{"service": unit.Label, "action": action}}, nil
}
func loadedBridgeUnits(ctx context.Context, units []hostbridge.LegacyUnit) ([]hostbridge.LegacyUnit, error) {
	for i := range units {
		u := &units[i]
		var out, stderr string
		u.PID = 0
		u.Loaded = false
		u.Enabled = true
		if u.Unresolved == "" {
			var err error
			u.Enabled, err = bridgeUnitEnabled(ctx, *u)
			if err != nil {
				return nil, err
			}
		}
		var e error
		if u.Kind == "launchd" {
			out, stderr, e = bridgeCommandRun(ctx, "launchctl", "print", "gui/"+strconv.Itoa(os.Getuid())+"/"+u.Label)
			if e != nil {
				detail := out + stderr
				if strings.Contains(detail, "Could not find service") || strings.Contains(detail, "not loaded") || strings.Contains(detail, "not found") || strings.Contains(detail, "could not be found") {
					continue
				}
				return nil, fmt.Errorf("cannot inspect service %s: %w", u.Label, e)
			}
			u.Loaded = true
			for _, line := range strings.Split(out, "\n") {
				k, v, ok := strings.Cut(strings.TrimSpace(line), " = ")
				if ok && k == "pid" {
					u.PID, _ = strconv.Atoi(strings.TrimSpace(v))
				}
			}

		} else {
			out, stderr, e = bridgeCommandRun(ctx, "systemctl", "--user", "show", u.Label, "--property=MainPID", "--value")
			if e != nil {
				detail := out + stderr
				if strings.Contains(detail, "Could not find service") || strings.Contains(detail, "not loaded") || strings.Contains(detail, "not found") || strings.Contains(detail, "could not be found") {
					continue
				}
				return nil, fmt.Errorf("cannot inspect service %s: %w", u.Label, e)
			}
			u.PID, _ = strconv.Atoi(strings.TrimSpace(out))
			u.Loaded = u.PID > 0

		}
	}
	return units, nil
}
func bridgeUnitEnabled(ctx context.Context, u hostbridge.LegacyUnit) (bool, error) {
	if u.Kind == "launchd" {
		out, _, e := bridgeCommandRun(ctx, "launchctl", "print-disabled", "gui/"+strconv.Itoa(os.Getuid()))
		if e != nil {
			return false, fmt.Errorf("cannot inspect enable state for %s: %w", u.Label, e)
		}
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, strconv.Quote(u.Label)) && strings.Contains(line, "=> true") {
				return false, nil
			}
		}
		return true, nil
	}
	out, stderr, e := bridgeCommandRun(ctx, "systemctl", "--user", "is-enabled", u.Label)
	switch strings.TrimSpace(out) {
	case "enabled", "enabled-runtime":
		return true, nil
	case "disabled", "static", "indirect":
		return false, nil
	}
	return false, fmt.Errorf("cannot inspect enable state for %s: %v %s", u.Label, e, stderr)
}

func (a *App) migrationRegistration(ctx context.Context, v hostbridge.LegacyInvocation) (hostbridge.Workspace, hostbridge.Agent, error) {
	cfg := config.Resolved{BaseURL: config.DefaultBaseURL, ConfigDir: v.ConfigDir, As: v.Agent, Timeout: 2 * time.Second, Sources: map[string]string{"base_url": "default"}}
	if v.BaseURL != "" {
		cfg.BaseURL = v.BaseURL
		cfg.Sources["base_url"] = "legacy"
	}
	child := *a
	if v.CWD != "" {
		child.Getwd = func() (string, error) { return v.CWD, nil }
	}
	var alias *string
	if v.Workspace != "" {
		alias = &v.Workspace
	}
	cfg, e := child.resolveWorkspace(cfg, alias)
	if e != nil {
		return hostbridge.Workspace{}, hostbridge.Agent{}, e
	}
	w, agent, e := child.bridgeRegistration(ctx, cfg)
	if e != nil {
		return w, agent, e
	}
	agent.Policy = v.Policy
	return w, agent, nil
}
func (a *App) importPythonBridge(ctx context.Context, v hostbridge.LegacyInvocation, r *hostbridge.Registry) error {
	raw, e := integrations.Read(v.PythonConfig)
	if e != nil {
		return e
	}
	if len(raw) > 1<<20 {
		return fmt.Errorf("Python bridge config too large")
	}
	var in struct {
		Host struct {
			BaseURL   string  `toml:"base_url"`
			ID        string  `toml:"id"`
			Slug      string  `toml:"slug"`
			ConfigDir string  `toml:"config_dir"`
			Anx       string  `toml:"anx"`
			Agentctl  string  `toml:"agentctl"`
			Poll      float64 `toml:"poll_seconds"`
			Checkin   float64 `toml:"checkin_seconds"`
		} `toml:"host"`
		Agents map[string]struct {
			Command []string          `toml:"command"`
			Adapter string            `toml:"adapter"`
			CWD     string            `toml:"cwd"`
			Env     map[string]string `toml:"env"`
			Enabled *bool             `toml:"enabled"`
		} `toml:"agents"`
	}
	dec := toml.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if e = dec.Decode(&in); e != nil {
		return fmt.Errorf("unsupported legacy Python config: %w", e)
	}
	for name, entry := range in.Agents {
		if entry.Enabled != nil && !*entry.Enabled {
			continue
		}
		copy := v
		copy.Agent = name
		copy.BaseURL = in.Host.BaseURL
		if in.Host.ConfigDir != "" {
			copy.ConfigDir = in.Host.ConfigDir
		}
		w, agent, e := a.migrationRegistration(ctx, copy)
		if e != nil {
			return e
		}
		if w.HostID != in.Host.ID {
			return integrations.ErrIdentityChanged
		}
		cwd := entry.CWD
		if cwd == "" {
			cwd = filepath.Dir(v.PythonConfig)
		}
		if !filepath.IsAbs(cwd) {
			cwd = filepath.Join(filepath.Dir(v.PythonConfig), cwd)
		}
		if strings.Contains(cwd, "$") || strings.HasPrefix(cwd, "~") {
			return fmt.Errorf("unresolved runtime cwd; use absolute path before migration")
		}
		if entry.Adapter == "" {
			entry.Adapter = "generic"
			switch name {
			case "claude", "codex", "cursor", "omp", "generic":
				entry.Adapter = name
			}
		}
		agent.Jobs = map[string]bool{"wake": true}
		agent.Runtime = &hostbridge.Runtime{Argv: entry.Command, CWD: cwd, Env: entry.Env, Adapter: entry.Adapter, Agentctl: in.Host.Agentctl}
		if agent.Runtime.Agentctl == "" {
			if bin, e := exec.LookPath("agentctl"); e == nil {
				agent.Runtime.Agentctl = bin
			}
		}
		if e = mergeImportedAgent(r, w, agent); e != nil {
			return e
		}
	}
	return nil
}
func mergeImportedAgent(r *hostbridge.Registry, w hostbridge.Workspace, agent hostbridge.Agent) error {
	for _, oldW := range r.Workspaces {
		if oldW.BaseURL != w.BaseURL || oldW.HostID != w.HostID {
			continue
		}
		for _, old := range oldW.Agents {
			if old.ActorID != agent.ActorID {
				continue
			}
			if old.Policy != agent.Policy {
				return fmt.Errorf("conflicting execution policies for imported namespace")
			}
			for kind, enabled := range old.Jobs {
				agent.Jobs[kind] = agent.Jobs[kind] || enabled
			}
			if old.Runtime != nil {
				if agent.Runtime != nil {
					a, _ := json.Marshal(old.Runtime)
					b, _ := json.Marshal(agent.Runtime)
					if string(a) != string(b) {
						return fmt.Errorf("conflicting imported runtime commands")
					}
				} else {
					agent.Runtime = old.Runtime
				}
			}
		}
	}
	return hostbridge.Register(r, w, agent)
}

type bridgeMigrationJournal struct {
	Phase           string                   `json:"phase"`
	Plan            hostbridge.MigrationPlan `json:"plan"`
	Before          hostbridge.Registry      `json:"registry_before"`
	After           hostbridge.Registry      `json:"registry_after"`
	NewUnit         hostbridge.UnitPlan      `json:"new_unit"`
	RegistryBefore  []byte                   `json:"registry_bytes_before,omitempty"`
	RegistryExisted bool                     `json:"registry_existed"`
	UnitBefore      []byte                   `json:"unit_bytes_before,omitempty"`
	UnitExisted     bool                     `json:"unit_existed"`
	NewActivated    bool                     `json:"new_activated"`
	Paused          []string                 `json:"paused_records,omitempty"`
}

func (a *App) installHostBridge(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("bridge install")
	dry := fs.Bool("dry-run", false, "Preview migration without writes or service changes")
	if e := fs.Parse(args); e != nil {
		return nil, errnorm.Usage("invalid_flags", e.Error())
	}
	if len(fs.Args()) != 0 {
		return nil, errnorm.Usage("invalid_args", "bridge install takes no agent")
	}
	home, e := a.bridgeHome()
	if e != nil {
		return nil, e
	}
	root := hostbridge.Root(home)
	exe, e := os.Executable()
	if e != nil {
		return nil, e
	}
	unit, e := hostbridge.UnitFor(bridgePlatform, home, exe, os.Getuid())
	if e != nil {
		return nil, e
	}
	// Recovery and cutover share the same locks: a second installer must never
	// roll back a journal while its owner is still activating the new service.
	if !*dry {
		if e = os.MkdirAll(root, 0700); e != nil {
			return nil, e
		}
		unlock, err := integrations.Lock(ctx, filepath.Join(root, "install.lock"))
		if err != nil {
			return nil, err
		}
		defer unlock()
		registryUnlock, err := integrations.Lock(ctx, filepath.Join(root, "registry.lock"))
		if err != nil {
			return nil, err
		}
		defer registryUnlock()
	}
	var prior bridgeMigrationJournal
	if raw, readErr := integrations.PrivateRead(filepath.Join(root, "migration.json")); readErr == nil {
		if e = json.Unmarshal(raw, &prior); e != nil {
			return nil, e
		}
		if prior.Phase != "complete" && prior.Phase != "rolled_back" {
			if *dry {
				return nil, fmt.Errorf("interrupted migration needs recovery; install without --dry-run restores the saved services before retrying")
			}
			if prior.NewUnit.Path != unit.Path || prior.NewUnit.Label != unit.Label {
				return nil, fmt.Errorf("migration journal does not identify this host unit")
			}
			if e = a.rollbackHostBridge(ctx, root, &prior); e != nil {
				return nil, e
			}
		}
	} else if !os.IsNotExist(readErr) {
		return nil, readErr
	}
	// Even preview performs only read-only inventory and scoped remote preflight.
	units, e := hostbridge.ReadUnits(home, bridgePlatform)
	if e != nil {
		return nil, e
	}
	units, e = loadedBridgeUnits(ctx, units)
	if e != nil {
		return nil, e
	}
	processes, e := bridgeProcesses(ctx)
	if e != nil {
		return nil, e
	}
	plan := hostbridge.PlanMigration(home, exe, os.Getuid(), units, processes)
	for _, u := range plan.Units {
		if u.Environment["UNRESOLVED_ENV_FILE"] != "" {
			plan.Blockers = append(plan.Blockers, "unit uses an environment file; replace with explicit flags before migration: "+u.Label)
		}
	}
	before, e := hostbridge.Load(root)
	if e != nil {
		return nil, e
	}
	afterRaw, _ := json.Marshal(before)
	var after hostbridge.Registry
	_ = json.Unmarshal(afterRaw, &after)
	if len(plan.Blockers) > 0 {
		return nil, errnorm.WithDetails(errnorm.Local("migration_blocked", "legacy bridge ownership or configuration needs repair"), map[string]any{"blockers": plan.Blockers})
	}
	for _, v := range plan.Invocations {
		if v.PythonConfig != "" {
			if e = a.importPythonBridge(ctx, v, &after); e != nil {
				return nil, e
			}
			continue
		}
		w, agent, e := a.migrationRegistration(ctx, v)
		if e != nil {
			return nil, e
		}
		if e = mergeImportedAgent(&after, w, agent); e != nil {
			return nil, e
		}
	}
	// Include enrolled zero-agent workspaces without deriving arbitrary agents.
	roots := map[string]bool{filepath.Join(home, ".config", "anx"): true}
	if cfg.ConfigDir != "" {
		roots[cfg.ConfigDir] = true
	}
	for _, w := range after.Workspaces {
		roots[w.ConfigDir] = true
	}
	for dir := range roots {
		hosts, e := hostidentity.ListAt(dir)
		if e != nil {
			return nil, e
		}
		for _, h := range hosts {
			found := false
			for _, w := range after.Workspaces {
				if w.BaseURL == h.BaseURL && w.HostID == h.ID {
					found = true
				}
			}
			if !found {
				if h.WorkspaceID == "" {
					local := config.Resolved{BaseURL: h.BaseURL, ConfigDir: dir, Timeout: 2 * time.Second, Sources: map[string]string{"base_url": "bridge"}}
					h.WorkspaceID, e = a.bridgeWorkspaceID(ctx, local, "")
					if e != nil {
						return nil, e
					}
				}
				after.Workspaces = append(after.Workspaces, hostbridge.Workspace{ID: h.WorkspaceID, BaseURL: h.BaseURL, HostID: h.ID, ConfigDir: dir, Agents: []hostbridge.Agent{}})
			}
		}
	}
	if e = mergeResumeCoverage(&after, roots); e != nil {
		return nil, e
	}
	if e = after.Validate(); e != nil {
		return nil, e
	}
	for _, w := range after.Workspaces {
		if e = a.preflightBridgeWorkspace(ctx, w); e != nil {
			return nil, e
		}
		for _, agent := range w.Agents {
			if agent.Jobs["wake"] {
				if e = validateBridgeRuntime(agent.Runtime); e != nil {
					return nil, e
				}
			}
		}
		if e = checkBridgeResumeRoots(w, false); e != nil {
			return nil, e
		}
	}
	count := 0
	for _, w := range after.Workspaces {
		count += len(w.Agents)
	}
	replaced := []string{}
	for _, u := range plan.Units {
		replaced = append(replaced, u.Label)
	}
	pids := []int{}
	for _, p := range plan.Processes {
		pids = append(pids, p.PID)
	}
	result := map[string]any{"unit_path": unit.Path, "label": unit.Label, "replaced": replaced, "replaced_pids": pids, "imported": count, "workspaces": len(after.Workspaces), "dry_run": *dry}
	if *dry {
		return &commandResult{Data: result}, nil
	}
	current, e := hostbridge.Load(root)
	if e != nil {
		return nil, e
	}
	if current.Revision != before.Revision {
		return nil, fmt.Errorf("bridge registry changed during preflight; retry install")
	}
	existing, unitReadErr := os.ReadFile(unit.Path)
	if unitReadErr != nil && !os.IsNotExist(unitReadErr) {
		return nil, unitReadErr
	}
	if len(plan.Units) == 0 && string(existing) == unit.Content && askBridgeLockState(filepath.Join(root, "service.lock")) == answerBridgeRunning {
		if !reflect.DeepEqual(before.Workspaces, after.Workspaces) {
			after.Revision = before.Revision + 1
			if e = integrations.PrivateWrite(filepath.Join(root, "registry.json"), integrations.JSON(after)); e != nil {
				return nil, e
			}
		}
		result["already_current"] = true
		return &commandResult{Data: result}, nil
	}
	if askBridgeLockState(filepath.Join(root, "service.lock")) == answerBridgeRunning {
		return nil, fmt.Errorf("host supervisor already running; stop it before a migration or unit replacement")
	}
	after.Revision = before.Revision + 1
	registryBytes, readErr := integrations.PrivateRead(filepath.Join(root, "registry.json"))
	if readErr != nil && !os.IsNotExist(readErr) {
		return nil, readErr
	}
	journal := bridgeMigrationJournal{Phase: "planned", Plan: plan, Before: before, After: after, NewUnit: unit, RegistryBefore: registryBytes, RegistryExisted: readErr == nil, UnitBefore: existing, UnitExisted: len(existing) > 0}
	save := func(phase string) error { journal.Phase = phase; return saveBridgeMigration(root, &journal) }
	if e = save("planned"); e != nil {
		return nil, e
	}
	// Catch an already executing/uncertain namespace before touching ANY unit.
	// The old consumer can finish its current command; healthy namespaces stay up.
	for _, w := range after.Workspaces {
		if e = checkBridgeResumeIdle(w); e != nil {
			_ = save("rolled_back")
			return nil, e
		}
	}
	fail := func(cause error) (*commandResult, error) {
		if restoreErr := a.rollbackHostBridge(ctx, root, &journal); restoreErr != nil {
			return nil, fmt.Errorf("migration failed: %w; restore: %v", cause, restoreErr)
		}
		return nil, fmt.Errorf("migration failed; all legacy services restored to their previous loaded state; retry install after repair: %w", cause)
	}
	if e = save("quiescing"); e != nil {
		return nil, e
	}
	for _, u := range plan.Units {
		if e = validateLegacyUnitSnapshot(ctx, u, plan.Processes); e != nil {
			return fail(e)
		}
		old := hostbridge.UnitPlan{Path: u.Path, Label: u.Label, Kind: u.Kind}
		if e = serviceCall(ctx, u.Kind, "disable", old); e != nil {
			return fail(e)
		}
		if e = serviceCall(ctx, u.Kind, "stop", old); e != nil {
			return fail(e)
		}
	}
	for _, p := range plan.Processes {
		if e = bridgeRetire(ctx, p); e != nil {
			return fail(e)
		}
	}
	for _, w := range after.Workspaces {
		if e = checkBridgeResumeRoots(w, true); e != nil {
			return fail(e)
		}
	}
	if e = mergeResumeCoverage(&after, roots); e != nil {
		return fail(e)
	}
	journal.After = after
	remaining, e := bridgeProcesses(ctx)
	if e != nil {
		return fail(e)
	}
	for _, p := range remaining {
		if hostbridge.IsConsumer(p.Argv) {
			return fail(fmt.Errorf("consumer PID %d remains before activation", p.PID))
		}
	}
	// Keep EVERY legacy unit file until the new unit has passed readiness and
	// singleton/survivor verification. The journal is also sufficient after a crash.
	for _, u := range plan.Units {
		raw, err := os.ReadFile(u.Path)
		if err != nil || string(raw) != string(u.Content) {
			return fail(fmt.Errorf("legacy unit changed during migration: %s", u.Label))
		}
	}
	if e = save("quiesced"); e != nil {
		return fail(e)
	}
	for _, w := range after.Workspaces {
		for _, agent := range w.Agents {
			for _, dir := range agent.RegistryDirs {
				if e = os.MkdirAll(dir, 0700); e != nil {
					return fail(e)
				}
			}
		}
	}
	if e = integrations.PrivateWrite(filepath.Join(root, "registry.json"), integrations.JSON(after)); e != nil {
		return fail(e)
	}
	if e = os.MkdirAll(filepath.Dir(unit.Path), 0700); e != nil {
		return fail(e)
	}
	if st, err := os.Lstat(unit.Path); err == nil && !st.Mode().IsRegular() {
		return fail(fmt.Errorf("unsafe host unit path"))
	}
	if e = integrations.PrivateWrite(unit.Path, []byte(unit.Content)); e != nil {
		return fail(e)
	}
	journal.NewActivated = true
	if e = save("activating"); e != nil {
		return fail(e)
	}
	if e = serviceCall(ctx, unit.Kind, "enable", unit); e == nil {
		e = serviceCall(ctx, unit.Kind, "start", unit)
	}
	if e == nil {
		e = bridgeWaitReady(ctx, root, after)
	}
	if e != nil {
		return fail(e)
	}
	survivors, e := bridgeProcesses(ctx)
	if e != nil {
		return fail(e)
	}
	newCount := 0
	for _, p := range survivors {
		if !hostbridge.IsConsumer(p.Argv) {
			continue
		}
		if len(p.Argv) != 3 || p.Argv[1] != "bridge" || p.Argv[2] != "run" {
			return fail(fmt.Errorf("legacy supervisor PID %d survived cutover", p.PID))
		}
		newCount++
	}
	if newCount != 1 {
		return fail(fmt.Errorf("expected exactly one host supervisor; found %d", newCount))
	}
	if e = save("healthy"); e != nil {
		return fail(e)
	}
	for _, u := range plan.Units {
		raw, err := os.ReadFile(u.Path)
		if err != nil || string(raw) != string(u.Content) {
			return fail(fmt.Errorf("legacy unit changed before cleanup: %s", u.Label))
		}
		if e = os.Remove(u.Path); e != nil {
			return fail(e)
		}
	}
	if e = save("complete"); e != nil {
		return fail(e)
	}
	result["started"] = true
	return &commandResult{Data: result}, nil
}

func (a *App) preflightBridgeWorkspace(ctx context.Context, w hostbridge.Workspace) error {
	cfg := config.Resolved{BaseURL: w.BaseURL, ConfigDir: w.ConfigDir, Timeout: 2 * time.Second, Sources: map[string]string{"base_url": "bridge"}}
	h, e := a.resolvedHost(cfg)
	if e != nil {
		return e
	}
	workspaceID, e := a.bridgeWorkspaceID(ctx, cfg, h.WorkspaceID)
	if e != nil {
		return e
	}
	if h.ID != w.HostID || workspaceID != w.ID {
		return integrations.ErrIdentityChanged
	}
	if _, e = hostidentity.Key(h); e != nil {
		return e
	}
	result, e := a.hostCall(ctx, cfg, "GET", "/meta/handshake", nil, nil)
	if e != nil {
		return e
	}
	if anyString(result["workspace_id"]) != w.ID {
		return integrations.ErrIdentityChanged
	}
	for _, c := range asSlice(result["capabilities"]) {
		if anyString(c) == "host_bridge_v1" {
			return nil
		}
	}
	return fmt.Errorf("workspace %s needs host_bridge_v1 before migration", w.ID)
}
func checkBridgeResumeRoots(w hostbridge.Workspace, quiesced bool) error {
	seen := map[string]string{}
	for _, a := range w.Agents {
		for _, dir := range a.RegistryDirs {
			if filepath.Base(dir) != hostbridge.Namespace(w.BaseURL, a.ActorID) {
				return fmt.Errorf("resume namespace identity mismatch")
			}
			info, statErr := os.Lstat(dir)
			if statErr == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
				return fmt.Errorf("unsafe resume namespace")
			}
			files, e := os.ReadDir(dir)
			if os.IsNotExist(e) {
				continue
			}
			if e != nil {
				return e
			}
			for _, f := range files {
				if !isResumeRegistrationFile(f.Name()) {
					continue
				}
				path := filepath.Join(dir, f.Name())
				entry, e := readAskRegistration(path)
				if e != nil {
					return e
				}
				if entry.BaseURL != w.BaseURL || entry.ActorID != a.ActorID {
					return integrations.ErrIdentityChanged
				}
				if old, ok := seen[entry.AskID]; ok && old != path {
					return fmt.Errorf("duplicate resume ask across roots; reconcile without deleting source records")
				}
				seen[entry.AskID] = path
				if quiesced && !entry.Acknowledged && (entry.State == "executing" || entry.State == "pending" && entry.Attempts > 0) {
					entry.State = "uncertain"
					if e = writeAskRegistration(path, entry); e != nil {
						return e
					}
				}
				if quiesced && entry.State == "uncertain" {
					return fmt.Errorf("interrupted resume outcome is uncertain; reconcile retained record before cutover")
				}
			}
		}
	}
	return nil
}
func waitHostBridgeReady(ctx context.Context, root string, r hostbridge.Registry) error {
	deadline, done := context.WithTimeout(ctx, 30*time.Second)
	defer done()
	for deadline.Err() == nil {
		if askBridgeLockState(filepath.Join(root, "service.lock")) == answerBridgeRunning {
			raw, e := integrations.PrivateRead(filepath.Join(root, "status.json"))
			var state localBridgeState
			if e == nil && json.Unmarshal(raw, &state) == nil && time.Since(state.At) < 10*time.Second {
				all := true
				for _, w := range r.Workspaces {
					report := state.Reports[w.BaseURL+"\x00"+w.HostID]
					if report.Revision != r.Revision || report.LastSuccess.IsZero() || time.Since(report.LastSuccess) > 300*time.Second || report.Code != "" {
						all = false
					}
					for _, a := range w.Agents {
						for kind, enabled := range a.Jobs {
							if enabled && !state.Ready[hostbridge.AgentKey(w, a)+"\x00"+kind] {
								all = false
							}
						}
					}
				}
				if all {
					return nil
				}
			}
		}
		select {
		case <-deadline.Done():
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf("host readiness not confirmed for every imported namespace")
}

// Inspect every retained resume root, including stopped/offline consumers. An
// unresolved actor must block install, rather than producing a successful but
// incomplete cutover. Known actor roots are kept in place and deduplicated by
// their physical directory, never moved or pruned.
func mergeResumeCoverage(r *hostbridge.Registry, roots map[string]bool) error {
	for root := range roots {
		dirs, e := os.ReadDir(filepath.Join(root, "answer-resumes"))
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		if len(dirs) > 4096 {
			return fmt.Errorf("too many resume namespaces; reconcile before migration")
		}
		for _, d := range dirs {
			path := filepath.Join(root, "answer-resumes", d.Name())
			st, e := os.Lstat(path)
			if e != nil {
				return e
			}
			if st.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("symlinked resume namespace requires repair")
			}
			if !st.IsDir() {
				continue
			}
			physical, e := filepath.EvalSymlinks(path)
			if e != nil {
				return e
			}
			files, e := os.ReadDir(path)
			if e != nil {
				return e
			}
			if len(files) > 10000 {
				return fmt.Errorf("resume namespace capacity exceeded")
			}
			for _, f := range files {
				if !isResumeRegistrationFile(f.Name()) {
					continue
				}
				entry, e := readAskRegistration(filepath.Join(path, f.Name()))
				if e != nil {
					return e
				}
				if entry.Acknowledged || entry.State == "registration_failed" {
					continue
				}
				found := false
				for wi := range r.Workspaces {
					w := &r.Workspaces[wi]
					if w.BaseURL != entry.BaseURL {
						continue
					}
					for ai := range w.Agents {
						agent := &w.Agents[ai]
						if agent.ActorID != entry.ActorID {
							continue
						}
						if filepath.Base(physical) != hostbridge.Namespace(w.BaseURL, agent.ActorID) {
							return fmt.Errorf("retained resume namespace identity mismatch")
						}
						known := false
						for _, old := range agent.RegistryDirs {
							resolved, e := filepath.EvalSymlinks(old)
							if e == nil && resolved == physical {
								known = true
							}
						}
						if !known {
							agent.RegistryDirs = append(agent.RegistryDirs, physical)
						}
						found = true
					}
				}
				if !found {
					return errnorm.WithDetails(errnorm.Local("resume_namespace_unregistered", "retained unacknowledged work has no host registration; register its original agent/workspace with bridge add before migration"), map[string]any{"namespace": physical, "base_url": entry.BaseURL, "actor_id": entry.ActorID})
				}
			}
		}
	}
	return nil
}
func validateLegacyUnitSnapshot(ctx context.Context, u hostbridge.LegacyUnit, original []hostbridge.Process) error {
	raw, e := os.ReadFile(u.Path)
	if e == nil && string(raw) != string(u.Content) {
		return fmt.Errorf("legacy unit changed during preflight")
	}
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	current, e := loadedBridgeUnits(ctx, []hostbridge.LegacyUnit{u})
	if e != nil {
		return e
	}
	if len(current) != 1 {
		return fmt.Errorf("legacy launcher cannot be revalidated")
	}
	if current[0].PID != 0 && current[0].PID != u.PID {
		return fmt.Errorf("legacy launcher PID changed; rerun migration preview")
	}
	processes, e := bridgeProcesses(ctx)
	if e != nil {
		return e
	}
	for _, expected := range original {
		for _, actual := range processes {
			if expected.PID == actual.PID && (expected.Started != actual.Started || expected.Executable != actual.Executable || expected.UID != actual.UID || !slices.Equal(expected.Argv, actual.Argv)) {
				return fmt.Errorf("legacy PID identity changed; rerun migration preview")
			}
		}
	}
	known := map[int]bool{}
	for _, expected := range original {
		known[expected.PID] = true
	}
	byPID := map[int]hostbridge.Process{}
	for _, actual := range processes {
		byPID[actual.PID] = actual
	}
	for _, actual := range processes {
		if !hostbridge.IsConsumer(actual.Argv) || known[actual.PID] {
			continue
		}
		pid := actual.PID
		seen := map[int]bool{}
		for pid > 1 && !seen[pid] {
			if pid == u.PID {
				return fmt.Errorf("new legacy child appeared during preflight; retry preview")
			}
			seen[pid] = true
			parent, ok := byPID[pid]
			if !ok {
				break
			}
			pid = parent.PPID
		}
	}

	return nil
}
