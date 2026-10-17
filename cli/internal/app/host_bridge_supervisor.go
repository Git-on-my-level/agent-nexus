package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	hostbridge "agent-nexus-cli/internal/bridge"
	"agent-nexus-cli/internal/buildinfo"
	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/integrations"
)

type bridgeReportState struct {
	Revision    int64     `json:"revision"`
	LastSuccess time.Time `json:"last_success"`
	Code        string    `json:"error_code,omitempty"`
}
type localBridgeState struct {
	Reports map[string]bridgeReportState `json:"reports"`
	At      time.Time                    `json:"as_of"`
	Ready   map[string]bool              `json:"ready"`
}
type bridgeHealth struct {
	sync.Mutex
	ready   map[string]bool
	reports map[string]bridgeReportState
}

func (h *bridgeHealth) set(w hostbridge.Workspace, a hostbridge.Agent, job string, ready bool) {
	h.Lock()
	defer h.Unlock()
	h.ready[hostbridge.AgentKey(w, a)+"\x00"+job] = ready
}
func (h *bridgeHealth) snapshot() localBridgeState {
	h.Lock()
	defer h.Unlock()
	v := localBridgeState{At: time.Now().UTC(), Ready: map[string]bool{}, Reports: map[string]bridgeReportState{}}
	for k, r := range h.reports {
		v.Reports[k] = r
	}
	for k, r := range h.ready {
		v.Ready[k] = r
	}
	return v
}
func (a *App) validateBridgeAgent(ctx context.Context, w hostbridge.Workspace, agent hostbridge.Agent) (config.Resolved, error) {
	cfg, e := a.resolveHostAgent(ctx, bridgeCfg(w, agent))
	if e != nil {
		return cfg, e
	}
	h, e := a.resolvedHost(cfg)
	if e != nil {
		return cfg, e
	}
	workspaceID, e := a.bridgeWorkspaceID(ctx, cfg, h.WorkspaceID)
	if e != nil {
		return cfg, e
	}
	if h.ID != w.HostID || workspaceID != w.ID || cfg.ActorID != agent.ActorID {
		return cfg, integrations.ErrIdentityChanged
	}
	if agent.Jobs["wake"] {
		if e = validateBridgeRuntime(agent.Runtime); e != nil {
			return cfg, e
		}
	}
	return cfg, nil
}
func (a *App) runHostSupervisor(ctx context.Context) (*commandResult, error) {
	ctx, stopSignals := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	root, e := a.hostBridgeRoot()
	if e != nil {
		return nil, e
	}
	if e = os.MkdirAll(root, 0700); e != nil {
		return nil, e
	}
	unlock, e := lockAskBridgeReady(filepath.Join(root, "service.lock"))
	if e != nil {
		return nil, e
	}
	defer unlock()
	health := &bridgeHealth{ready: map[string]bool{}, reports: map[string]bridgeReportState{}}
	incarnation, e := integrations.RandomID()
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(3 * time.Second)
		defer t.Stop()
		for {
			if e := integrations.PrivateWrite(filepath.Join(root, "status.json"), integrations.JSON(health.snapshot())); e != nil {
				cancel()
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
	// The long-lived SSE readers are bounded by the 256-agent registry. Child
	// executions and short Git/spool work have independent fixed worker budgets.
	executors := make(chan struct{}, 4)
	shortJobs := make(chan struct{}, 4)
	sequences := map[string]int64{}
	var seqMu sync.Mutex
	report := func(ctx context.Context, r hostbridge.Registry) {
		var reports sync.WaitGroup
		for _, w := range r.Workspaces {
			w := w
			reports.Add(1)
			go func() {
				defer reports.Done()
				child := *a
				child.writes = writeTracker{}
				cfg := config.Resolved{BaseURL: w.BaseURL, ConfigDir: w.ConfigDir, Timeout: 2 * time.Second, Sources: map[string]string{"base_url": "bridge"}}
				snap := health.snapshot()
				agents := []map[string]any{}
				for _, agent := range w.Agents {
					jobs := []map[string]any{}
					for _, kind := range []string{"answers", "wake", "git", "files", "spool"} {
						enabled := agent.Jobs[kind]
						jobs = append(jobs, map[string]any{"kind": kind, "enabled": enabled, "ready": enabled && snap.Ready[hostbridge.AgentKey(w, agent)+"\x00"+kind]})
					}
					agents = append(agents, map[string]any{"actor_id": agent.ActorID, "jobs": jobs})
				}
				key := w.BaseURL + "\x00" + w.HostID
				sum := sha256.Sum256([]byte(incarnation + "\x00" + key))
				instance := hex.EncodeToString(sum[:16]) // workspace-only incarnation
				// Each workspace is unique in the registry and only this reporter owns its sequence.
				seqMu.Lock()
				seq := sequences[key] + 1
				sequences[key] = seq
				seqMu.Unlock()
				body := map[string]any{"instance_id": instance, "sequence": seq, "config_revision": r.Revision, "version": buildinfo.Current, "agents": agents}
				deadline, done := context.WithTimeout(ctx, 2*time.Second)
				defer done()
				_, reportErr := child.hostBridgeCall(deadline, cfg, w.HostID, "bridge-check-in", "/hosts/"+w.HostID+"/bridge/check-in", body)
				health.Lock()
				last := health.reports[key]
				last.Revision = r.Revision
				last.Code = ""
				if reportErr == nil {
					last.LastSuccess = time.Now().UTC()
				} else {
					last.Code = errnorm.Normalize(reportErr).Code
				}
				health.reports[key] = last
				health.Unlock()
			}()
		}
		reports.Wait()
	}
	var fileMu sync.Mutex
	fileCursor := map[string]int{}
	fileReady := map[string]bool{}
	var logMu sync.Mutex
	run := func(ctx context.Context, w hostbridge.Workspace, agent hostbridge.Agent, job string) (runErr error) {
		defer func() {
			if runErr != nil && ctx.Err() == nil {
				logMu.Lock()
				defer logMu.Unlock()
				fmt.Fprintf(a.Stderr, "anx bridge: job=%s workspace=%q actor=%q code=%s detail=%q\n", job, w.ID, agent.ActorID, errnorm.Normalize(runErr).Code, truncateHook(runErr.Error(), 256))
			}
		}()

		if job == "wake" && agent.Jobs["answers"] {
			return nil
		}
		child := *a
		inheritedGetenv := a.Getenv
		child.Getenv = func(key string) string {
			if strings.HasPrefix(key, "AGENTCTL_") {
				return ""
			}
			return inheritedGetenv(key)
		}
		child.writes = writeTracker{}
		if job == "files" {
			select {
			case shortJobs <- struct{}{}:
				defer func() { <-shortJobs }()
			case <-ctx.Done():
				return ctx.Err()
			}
			tick, done := context.WithTimeout(ctx, 5*time.Second)
			defer done()
			fileMu.Lock()
			key := hostbridge.AgentKey(w, agent)
			i := fileCursor[key] % len(agent.FileDirs)
			fileCursor[key] = i + 1
			fileMu.Unlock()
			e := child.bridgeFilesTick(tick, w, agent, agent.FileDirs[i])
			fileMu.Lock()
			fileReady[key+"\x00"+agent.FileDirs[i]] = e == nil
			ready := true
			for _, dir := range agent.FileDirs {
				ready = ready && fileReady[key+"\x00"+dir]
			}
			fileMu.Unlock()
			health.set(w, agent, job, ready)
			return e
		}
		cfg, e := child.validateBridgeAgent(ctx, w, agent)
		if e != nil {
			health.set(w, agent, job, false)
			return e
		}
		if job == "answers" || job == "wake" {
			for _, dir := range agent.RegistryDirs {
				if e = checkResumeNamespace(dir, w, agent); e == nil && agent.Jobs["wake"] {
					e = checkRuntimeReceipts(dir)
				}
				if e != nil {
					health.set(w, agent, job, false)
					return e
				}
			}
			health.set(w, agent, "answers", agent.Jobs["answers"])
			health.set(w, agent, "wake", agent.Jobs["wake"])
			// Multiple preserved roots share a single dispatcher's cursor. Duplicates
			// are checked before admission; acknowledged outcomes never execute again.
			e = child.consumeHostWakes(ctx, cfg, agent, executors)
			if e != nil && !errors.Is(e, context.DeadlineExceeded) {
				health.set(w, agent, "answers", false)
				health.set(w, agent, "wake", false)
			}
			return e
		}
		select {
		case shortJobs <- struct{}{}:
			defer func() { <-shortJobs }()
		case <-ctx.Done():
			return ctx.Err()
		}
		tick, done := context.WithTimeout(ctx, 5*time.Second)
		defer done()
		roots := map[string]bool{w.ConfigDir: true}
		for _, dir := range agent.RegistryDirs {
			roots[filepath.Dir(filepath.Dir(dir))] = true
		}
		for dir := range roots {
			local := cfg
			local.ConfigDir = dir
			h, err := child.resolvedHost(local)
			if err != nil || h.ID != w.HostID {
				e = integrations.ErrIdentityChanged
				break
			}
			switch job {
			case "git":
				_, e = child.bridgeGitTick(tick, local, false)
			case "spool":
				e = child.bridgeSpoolTick(tick, local)
			}
			if e != nil {
				break
			}
		}

		health.set(w, agent, job, e == nil)
		return e
	}
	e = hostbridge.Supervise(ctx, root, run, report)
	cancel()
	wg.Wait()
	return &commandResult{Data: map[string]any{"stopped": true}}, e
}
func checkResumeNamespace(dir string, w hostbridge.Workspace, agent hostbridge.Agent) error {
	if filepath.Base(dir) != hostbridge.Namespace(w.BaseURL, agent.ActorID) {
		return fmt.Errorf("resume namespace identity mismatch")
	}
	info, e := os.Lstat(dir)
	if e != nil {
		return e
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("unsafe resume namespace")
	}
	entries, e := os.ReadDir(dir)
	if e != nil {
		return e
	}
	if len(entries) > 10000 {
		return fmt.Errorf("resume registry capacity exceeded")
	}
	for _, file := range entries {
		if !isResumeRegistrationFile(file.Name()) {
			continue
		}
		if strings.HasSuffix(file.Name(), ".paused") {
			return fmt.Errorf("resume is paused for migration reconciliation")
		}
		entry, e := readAskRegistration(filepath.Join(dir, file.Name()))
		if e != nil {
			return e
		}
		if entry.BaseURL != w.BaseURL || entry.ActorID != agent.ActorID {
			return integrations.ErrIdentityChanged
		}
		if entry.State == "executing" || entry.State == "uncertain" {
			return fmt.Errorf("resume outcome uncertain; reconcile retained record before enabling this namespace")
		}
	}
	return nil
}
func (a *App) bridgeGitTick(ctx context.Context, cfg config.Resolved, doctor bool) (int, error) {
	files, e := filepath.Glob(filepath.Join(cfg.ConfigDir, "git", "*", "state.json"))
	if e != nil {
		return 0, e
	}
	if len(files) > 100 {
		return 0, fmt.Errorf("too many capture repositories")
	}
	count := 0
	for _, path := range files {
		s, e := integrations.LoadCapture(path)
		if e != nil {
			return count, e
		}
		if !s.Enabled || s.Binding.Workspace != cfg.BaseURL || s.Binding.Actor != cfg.ActorID {
			continue
		}
		if _, e = a.captureAuth(ctx, cfg, s.Binding); e != nil {
			return count, e
		}
		for root := range s.Cursors {
			count++
			if !doctor {
				if _, e = a.syncCapture(ctx, cfg, root, "", "", "", false); e != nil {
					return count, e
				}
			}
		}
	}
	return count, nil
}
func (a *App) bridgeSpoolTick(ctx context.Context, cfg config.Resolved) error {
	paths, e := filepath.Glob(filepath.Join(cfg.ConfigDir, "integrations", "*", "*.spool.json"))
	if e != nil {
		return e
	}
	if len(paths) > 100 {
		return fmt.Errorf("too many integration spools")
	}
	for _, path := range paths {
		name := filepath.Base(filepath.Dir(path))
		r, e := integrations.Lookup(name)
		if e != nil {
			return e
		}
		id := strings.TrimSuffix(filepath.Base(path), ".spool.json")
		c, e := integrations.LoadConfig(integrations.ConfigPath(cfg.ConfigDir, name, id))
		if e != nil {
			return e
		}
		if c.Agent != cfg.As {
			continue
		}
		admit := func(p integrations.Pending) bool {
			if p.Delivery != nil {
				return p.Delivery.BaseURL == cfg.BaseURL && p.Delivery.Agent == cfg.As
			}
			local := *a
			local.Getwd = func() (string, error) { return p.Event.CWD, nil }
			var alias *string
			if c.Workspace != "" {
				alias = &c.Workspace
			}
			routed, e := local.resolveIntegrationWorkspace(cfg, alias)
			return e == nil && routed.BaseURL == cfg.BaseURL
		}
		if e = a.drainIntegrationQueue(ctx, cfg, cfg.ConfigDir, r, c, path, admit); e != nil {
			return e
		}
	}
	return nil
}

func validateBridgeRuntime(r *hostbridge.Runtime) error {
	if r == nil || len(r.Argv) == 0 {
		return fmt.Errorf("missing runtime argv")
	}
	if _, e := exec.LookPath(r.Argv[0]); e != nil {
		return fmt.Errorf("runtime executable unavailable: %w", e)
	}
	st, e := os.Stat(r.CWD)
	if e != nil || !st.IsDir() {
		return fmt.Errorf("runtime working directory unavailable")
	}
	if r.Agentctl != "" {
		if _, e = exec.LookPath(r.Agentctl); e != nil {
			return fmt.Errorf("configured agentctl unavailable: %w", e)
		}
	}
	for _, arg := range r.Argv {
		if strings.ContainsRune(arg, 0) {
			return fmt.Errorf("invalid runtime argv")
		}
	}
	for k, v := range r.Env {
		if k == "" || strings.ContainsAny(k, "=\x00") || strings.ContainsRune(v, 0) || strings.HasPrefix(k, "ANX_") || strings.HasPrefix(k, "AGENTCTL_") {
			return fmt.Errorf("runtime environment cannot override identity or contain invalid keys")
		}
	}
	return nil
}
