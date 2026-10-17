package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/integrations"
	"agent-nexus-cli/internal/output"
)

type gitOptions struct {
	verb, repo, binding, since, to, skip, commitURL string
	writer, hook, dry, restart                      bool
}

func parseGitOptions(args []string) (gitOptions, error) {
	o := gitOptions{}
	if len(args) == 0 {
		return o, nil
	}
	o.verb = args[0]
	rest := args[1:]
	if o.verb == "binding" {
		if len(rest) == 0 || rest[0] != "export" {
			return o, errnorm.Usage("invalid_args", "use git binding export [repo]")
		}
		o.verb = "binding export"
		rest = rest[1:]
	}
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		o.repo = rest[0]
		rest = rest[1:]
	}
	fs := newSilentFlagSet("git " + o.verb)
	switch o.verb {
	case "enable":
		fs.BoolVar(&o.writer, "writer", false, "Explicitly elect this host/agent")
		fs.StringVar(&o.binding, "binding", "", "Import non-secret capture binding JSON")
		fs.BoolVar(&o.hook, "install-hook", false, "Install unoccupied Git post-commit hook")
		fs.StringVar(&o.commitURL, "commit-url", "", "Credential-free URL containing {sha}")
		fs.BoolVar(&o.dry, "dry-run", false, "Preview without writing")
	case "sync":
		fs.BoolVar(&o.restart, "restart-scan", false, "Restart an incomplete scan from an explicit --since boundary, retaining commit records")
		fs.StringVar(&o.since, "since", "", "Explicit backfill boundary (suppresses claims only below each card checkpoint)")
		fs.StringVar(&o.to, "to", "", "Frozen end revision (default HEAD)")
		fs.StringVar(&o.skip, "skip", "", "Skip an inspected failed or unsent deferred SHA, retaining its bytes")
	case "disable", "status", "binding export":
	default:
		return o, errnorm.Usage("unknown_subcommand", "use git enable|sync|status|disable|binding export")
	}
	if e := fs.Parse(rest); e != nil {
		return o, errnorm.Usage("invalid_flags", e.Error())
	}
	if len(fs.Args()) > 0 {
		return o, errnorm.Usage("invalid_args", "unexpected arguments")
	}
	if o.verb == "enable" && (o.writer == (o.binding != "")) {
		return o, errnorm.Usage("invalid_args", "choose exactly one of --writer or --binding; no automatic election")
	}
	if o.restart && o.since == "" {
		return o, errnorm.Usage("invalid_args", "--restart-scan requires --since")
	}
	if o.skip != "" && !integrations.ObjectID(o.skip) {
		return o, errnorm.Usage("invalid_args", "--skip requires a full object ID")
	}
	if o.commitURL != "" {
		if e := validateCommitURL(o.commitURL); e != nil {
			return o, errnorm.Usage("invalid_args", e.Error())
		}
	}
	for _, ref := range []string{o.since, o.to} {
		if strings.HasPrefix(ref, "-") || strings.ContainsAny(ref, "\x00\r\n") {
			return o, errnorm.Usage("invalid_args", "invalid revision")
		}
	}
	return o, nil
}
func validateCommitURL(template string) error {
	if strings.Count(template, "{sha}") != 1 {
		return fmt.Errorf("commit URL needs exactly one {sha}")
	}
	u, e := url.Parse(strings.ReplaceAll(template, "{sha}", strings.Repeat("a", 40)))
	if e != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("commit URL must be http(s), without credentials, query or fragment")
	}
	return nil
}
func (a *App) runGitCapture(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	o, e := parseGitOptions(args)
	name := strings.TrimSpace("git " + o.verb)
	if e != nil {
		return nil, name, e
	}
	if o.verb == "" {
		return &commandResult{Text: gitCaptureHelp}, "git", nil
	}
	cwd := o.repo
	if cwd == "" {
		cwd, e = a.workspaceCWD()
		if e != nil {
			return nil, name, e
		}
	}
	root, gd, e := integrations.Repo(ctx, cwd)
	if e != nil {
		return nil, name, e
	}
	dir, e := a.configDir(cfg)
	if e != nil {
		return nil, name, e
	}
	path := integrations.CapturePath(dir, gd)
	if o.verb == "sync" {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		data, e := a.syncCapture(ctx, cfg, root, o.since, o.to, o.skip, o.restart)
		warnings, _ := data["warnings"].([]output.Warning)
		delete(data, "warnings")
		return &commandResult{Data: data, Warnings: warnings}, name, e
	}
	if o.verb == "status" || o.verb == "binding export" {
		s, e := integrations.LoadCapture(path)
		if e != nil {
			return nil, name, e
		}
		if o.verb == "binding export" {
			return &commandResult{Data: s.Binding}, name, nil
		}
		if !s.SelectWorktree(root) {
			return nil, name, fmt.Errorf("worktree not registered: enable it with the elected writer")
		}
		data := captureStatus(s)
		if len(s.Pending) > 0 {
			c, err := integrations.LoadCommit(path, s.Pending[0])
			if err != nil {
				return nil, name, err
			}
			data["head"] = map[string]any{"sha": c.SHA, "state": c.State, "reason": c.Reason, "attempts": c.Attempts, "first_attempt": c.FirstAttempt, "retry_at": c.RetryAt}
		}
		local := cfg
		local.BaseURL = s.Binding.Workspace
		local.Sources = map[string]string{"base_url": "capture"}
		if host, err := a.resolvedHost(local); err != nil {
			data["reason"] = "host_not_enrolled"
		} else if host.ID != s.Binding.Host {
			data["reason"] = "not_capture_writer"
		}
		return &commandResult{Data: data}, name, nil
	}
	if !o.dry {
		unlock, err := integrations.Lock(ctx, path+".lock")
		if err != nil {
			return nil, name, err
		}
		defer unlock()
	}
	if o.verb == "disable" {
		s, e := integrations.LoadCapture(path)
		if e != nil {
			return nil, name, e
		}
		s.Enabled = false
		if e = integrations.PrivateWrite(path, integrations.CaptureJSON(s)); e != nil {
			return nil, name, e
		}
		if s.HookPath != "" {
			b, err := integrations.Read(s.HookPath)
			if err == nil && string(b) == s.HookBody {
				if e = os.Remove(s.HookPath); e != nil {
					return nil, name, e
				}
			}
		}
		s.HookPath, s.HookBody = "", ""
		e = integrations.PrivateWrite(path, integrations.CaptureJSON(s))
		return &commandResult{Data: captureStatus(s)}, name, e
	}
	existing, loadErr := integrations.LoadCapture(path)
	registerWorktree := false
	if loadErr == nil && !existing.Enabled {
		registerWorktree = !existing.SelectWorktree(root)
	}
	if loadErr == nil && existing.Enabled {
		if existing.SelectWorktree(root) {
			return nil, name, errnorm.Usage("capture_exists", "capture already configured; disable/reconcile before replacing the binding")
		}
		registerWorktree = true
	}
	if registerWorktree && len(existing.Cursors) >= 100 {
		return nil, name, fmt.Errorf("worktree capacity reached")
	}
	if loadErr != nil && !errors.Is(loadErr, os.ErrNotExist) {
		return nil, name, loadErr
	}
	if loadErr == nil && !(registerWorktree && existing.Enabled) && (len(existing.Pending) > 0 || existing.HasScan()) {
		return nil, name, fmt.Errorf("reconcile pending captures before changing writer")
	}

	routed, e := a.hookWorkspace(cfg, root, "")
	if e != nil {
		return nil, name, e
	}
	head, e := integrations.ResolveCommit(ctx, root, "HEAD")
	if e != nil {
		return nil, name, e
	}
	if o.dry {
		return &commandResult{Data: map[string]any{"dry_run": true, "root": root, "git_dir": gd, "head": head, "workspace": routed.BaseURL, "writer_agent": routed.As, "writer": o.writer, "binding_file": o.binding, "install_hook": o.hook, "commit_url": o.commitURL}}, name, nil
	}
	auth, e := a.resolveHostAgent(ctx, routed)
	if e != nil {
		return nil, name, e
	}
	host, e := a.resolvedHost(auth)
	if e != nil {
		return nil, name, e
	}
	id, e := integrations.RandomID()
	if e != nil {
		return nil, name, e
	}
	binding := integrations.CaptureBinding{RepoID: id, Workspace: auth.BaseURL, Host: auth.HostID, Actor: auth.ActorID, Agent: auth.As}
	if o.binding != "" {
		raw, e := integrations.Read(o.binding)
		if e != nil {
			return nil, name, e
		}
		var envelope struct {
			OK     bool            `json:"ok"`
			Result json.RawMessage `json:"result"`
		}
		_ = json.Unmarshal(raw, &envelope)
		if envelope.OK {
			raw = envelope.Result
		}
		if e = json.Unmarshal(raw, &binding); e != nil {
			return nil, name, e
		}
		if len(binding.RepoID) != 32 || strings.Trim(binding.RepoID, "0123456789abcdef") != "" || binding.Host == "" || binding.Actor == "" || !agentNamePattern.MatchString(binding.Agent) || binding.Workspace != auth.BaseURL {
			return nil, name, fmt.Errorf("invalid binding or workspace mismatch")
		}
	}
	if binding.Host == "" || binding.Actor == "" {
		return nil, name, fmt.Errorf("capture requires enrolled host and derived actor")
	}
	_ = host
	if loadErr == nil {
		if o.binding != "" && binding.RepoID != existing.Binding.RepoID {
			return nil, name, fmt.Errorf("imported repository UUID differs from retained capture history")
		}
		binding.RepoID = existing.Binding.RepoID
	}
	s := integrations.CaptureState{Version: 1, Cursors: map[string]integrations.CaptureCursor{}, Binding: binding, Root: root, GitDir: gd, Enabled: true, Anchor: head, Pending: []string{}, Walk: []string{}, PromiseCheckpoint: map[string]integrations.CaptureCheckpoint{}, CommitURL: o.commitURL}
	if registerWorktree && binding != existing.Binding {
		return nil, name, fmt.Errorf("linked worktrees must use the elected repo writer/binding")
	}
	if loadErr == nil {
		s = existing
		s.Enabled = true
		s.Binding = binding
		if o.commitURL != "" {
			s.CommitURL = o.commitURL
		}
	}
	if registerWorktree {
		// Preserve the previous cursor before registering a new worktree at HEAD.
		s.Cursors[s.Root] = integrations.CaptureCursor{Anchor: s.Anchor, Snapshot: s.Snapshot, Walk: s.Walk, Collected: s.Collected, Backfill: s.Backfill}
		s.Root, s.Anchor, s.Snapshot, s.Walk, s.Collected, s.Backfill = root, head, "", []string{}, false, false
	}
	installedHook := false
	if o.hook && s.HookPath != "" {
		if _, err := os.Lstat(s.HookPath); errors.Is(err, os.ErrNotExist) {
			s.HookPath, s.HookBody = "", ""
		} else if err != nil {
			return nil, name, err
		}
	}
	if o.hook && s.HookPath == "" {
		if e = a.installGitHook(ctx, cfg, &s); e != nil {
			return nil, name, e
		}
		installedHook = true
	}
	if e = integrations.PrivateWrite(path, integrations.CaptureJSON(s)); e != nil {
		if installedHook {
			_ = os.Remove(s.HookPath)
		}
		return nil, name, e
	}
	data := captureStatus(s)
	if binding.Host != auth.HostID || binding.Actor != auth.ActorID {
		data["reason"] = "not_capture_writer"
	}
	return &commandResult{Data: data}, name, nil
}
func captureStatus(s integrations.CaptureState) map[string]any {
	return map[string]any{"root": s.Root, "enabled": s.Enabled, "capacity": integrations.CaptureCapacity, "binding": s.Binding, "pending": len(s.Pending), "confirmed": s.Confirmed, "quarantined": s.Quarantined, "anchor": s.Anchor, "snapshot": s.Snapshot, "walk_remaining": len(s.Walk), "worktrees": len(s.Cursors), "hook_path": s.HookPath}
}
func (a *App) installGitHook(ctx context.Context, cfg config.Resolved, s *integrations.CaptureState) error {
	b, e := integrations.Git(ctx, s.Root, 8192, "rev-parse", "--git-path", "hooks/post-commit")
	if e != nil {
		return e
	}
	path := strings.TrimSpace(string(b))
	if !filepath.IsAbs(path) {
		path = filepath.Join(s.Root, path)
	}
	if _, e = os.Lstat(path); !errors.Is(e, os.ErrNotExist) {
		return fmt.Errorf("post-commit is occupied; chain anx hook git.commit --harness git explicitly")
	}
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	dir, e := a.configDir(cfg)
	if e != nil {
		return e
	}
	body := gitPostCommitScript(exe, dir, s.Binding)
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	// Exclusive publication refuses even a concurrently planted symlink.
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if e != nil {
		return e
	}
	_, e = f.WriteString(body)
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		_ = os.Remove(path)
		return e
	}
	s.HookPath = path
	s.HookBody = body
	return nil
}
func gitPostCommitScript(exe, dir string, binding integrations.CaptureBinding) string {
	return "#!/bin/sh\nunset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_COMMON_DIR\n" + integrations.ShellQuote(exe) + " --config-dir " + integrations.ShellQuote(dir) + " --as " + integrations.ShellQuote(binding.Agent) + " --base-url " + integrations.ShellQuote(binding.Workspace) + " hook git.commit --harness git < /dev/null\nexit 0\n"
}

type captureWriterMismatch struct{ host, actor string }

func (e *captureWriterMismatch) Error() string {
	return fmt.Sprintf("not_capture_writer: elected host=%q actor=%q", truncateHook(e.host, 256), truncateHook(e.actor, 256))
}

func (a *App) captureAuth(ctx context.Context, cfg config.Resolved, b integrations.CaptureBinding) (config.Resolved, error) {
	if alias := cfg.Sources["integration_workspace"]; alias != "" {
		var e error
		cfg, e = a.hookWorkspace(cfg, "", alias)
		if e != nil {
			return cfg, e
		}
	}
	if (cfg.Sources["base_url"] == "flag:--base-url" || cfg.Sources["base_url"] == "flag:--workspace" || cfg.Sources["base_url"] == "hook") && cfg.BaseURL != b.Workspace {
		return cfg, integrations.ErrIdentityChanged
	}
	if cfg.As != "" && cfg.As != b.Agent {
		return cfg, &captureWriterMismatch{host: b.Host, actor: b.Actor}
	}
	cfg.BaseURL = b.Workspace
	cfg.As = b.Agent
	cfg.AccessToken = ""
	cfg.Sources = map[string]string{"base_url": "capture", "agent": "capture"}
	cfg.Timeout = time.Second
	host, e := a.resolvedHost(cfg)
	if e != nil {
		return cfg, e
	}
	if host.ID != b.Host {
		return cfg, &captureWriterMismatch{host: b.Host, actor: b.Actor}
	}
	auth, e := a.resolveHostAgent(ctx, cfg)
	if e != nil {
		return auth, e
	}
	if auth.HostID != b.Host || auth.ActorID != b.Actor {
		return auth, integrations.ErrIdentityChanged
	}
	return auth, nil
}
func (a *App) syncCapture(ctx context.Context, cfg config.Resolved, repo, since, to, skip string, restart bool) (map[string]any, error) {
	root, gd, e := integrations.Repo(ctx, repo)
	if e != nil {
		return nil, e
	}
	dir, e := a.configDir(cfg)
	if e != nil {
		return nil, e
	}
	path := integrations.CapturePath(dir, gd)
	if _, err := integrations.LoadCapture(path); err != nil {
		return nil, err
	}
	unlock, e := integrations.Lock(ctx, path+".lock")
	if e != nil {
		return nil, e
	}
	defer unlock()
	s, e := integrations.LoadCapture(path)
	if e != nil {
		return nil, e
	}
	if !s.Enabled {
		return captureStatus(s), nil
	}
	if s.GitDir != gd {
		return nil, fmt.Errorf("repository identity changed")
	}
	if !s.SelectWorktree(root) {
		return nil, fmt.Errorf("worktree not registered: enable it with the elected writer")
	}
	auth, e := a.captureAuth(ctx, cfg, s.Binding)
	if e != nil {
		return captureStatus(s), e
	}
	if skip != "" {
		c, e := integrations.LoadCommit(path, skip)
		if e != nil {
			return nil, e
		}
		if len(s.Pending) == 0 || s.Pending[0] != skip || !(c.State == "failed" || c.State == "deferred" && c.Delivery == nil) {
			return nil, fmt.Errorf("only an inspected failed or unsent deferred head entry can be skipped")
		}
		c.State = "skipped"
		if e = integrations.SaveCommit(path, c); e != nil {
			return nil, e
		}
		s.Pending = s.Pending[1:]
	}
	if restart && since != "" {
		if len(s.Pending) > 0 {
			return nil, fmt.Errorf("finish pending queue before restarting scan")
		}
		s.Snapshot = ""
		s.Walk = []string{}
		s.Collected = false
	}
	if since != "" {
		if len(s.Pending) > 0 || s.Snapshot != "" {
			return nil, fmt.Errorf("finish existing scan/queue before backfill")
		}
		anchor, e := integrations.ResolveCommit(ctx, root, since)
		if e != nil {
			return nil, e
		}
		s.Anchor = anchor
		s.Backfill = true
	}
	if to != "" {
		if s.Snapshot != "" {
			return nil, fmt.Errorf("scan already has frozen endpoint")
		}
		s.Snapshot, e = integrations.ResolveCommit(ctx, root, to)
		if e != nil {
			return nil, e
		}
		if _, e = integrations.Git(ctx, root, 256, "merge-base", "--is-ancestor", s.Anchor, s.Snapshot); e != nil {
			return nil, fmt.Errorf("divergent_history: --since must be an ancestor of the selected endpoint")
		}
	}
	if e = integrations.Collect(ctx, &s); e != nil {
		return captureStatus(s), e
	}
	if e = integrations.PrivateWrite(path, integrations.CaptureJSON(s)); e != nil {
		return nil, e
	}
	warnings := []output.Warning{}
	for n := 0; s.Collected && len(s.Walk) > 0 && n < integrations.WalkPage; n++ {
		if len(s.Pending) >= integrations.CaptureCapacity {
			break
		}
		sha := s.Walk[len(s.Walk)-1]
		exists, e := integrations.HasCommit(path, sha)
		if e != nil {
			return nil, e
		}
		if !exists {
			c, parseErr := integrations.ParseCommit(ctx, root, sha)
			if ctx.Err() != nil {
				return captureStatus(s), ctx.Err()
			}
			if parseErr != nil && !integrations.InvalidCaptureClaim(parseErr) {
				return captureStatus(s), parseErr
			}
			s.Sequence++
			c.Sequence = s.Sequence
			c.Backfill = s.Backfill
			if parseErr == nil && c.State == "pending" && c.Next != "" {
				at, ok := parsePromiseWhen(c.Next, c.At)
				if !ok {
					parseErr = fmt.Errorf("invalid_next")
				} else {
					c.Next = at.Format(time.RFC3339Nano)
					c.What = truncateRunes(c.What, maxPromiseWhatRunes)
					if at.Sub(a.clockNow()) > promiseHorizon {
						warnings = append(warnings, noteWarning("note_beyond_horizon", "promise is beyond the two-day horizon", []string{"anx", "git", "status"}))
					}
				}
			}
			if parseErr != nil {
				c.State = "quarantined"
				c.Reason = parseErr.Error()
			}
			if e = integrations.SaveCommit(path, c); e != nil {
				return nil, e
			}
		}
		c, e := integrations.LoadCommit(path, sha)
		if e != nil {
			return nil, e
		}
		if c.Sequence > s.Sequence {
			s.Sequence = c.Sequence
		}
		if c.State == "pending" {
			present := false
			for _, x := range s.Pending {
				if x == sha {
					present = true
				}
			}
			if !present {
				s.Pending = append(s.Pending, sha)
			}
		} else if !exists && c.State == "quarantined" {
			s.Quarantined++
		}
		s.Walk = s.Walk[:len(s.Walk)-1]
		if e = integrations.PrivateWrite(path, integrations.CaptureJSON(s)); e != nil {
			return nil, e
		}
	}
	if s.Collected && len(s.Walk) == 0 {
		s.Anchor = s.Snapshot
		s.Snapshot = ""
		s.Collected = false
		s.Backfill = false
	}
	if e = integrations.PrivateWrite(path, integrations.CaptureJSON(s)); e != nil {
		return nil, e
	}
	// One FIFO lane: failed/uncertain older notes cannot be overtaken by promises.
	for n := 0; len(s.Pending) > 0 && n < integrations.MaxDrain; n++ {
		sha := s.Pending[0]
		c, e := integrations.LoadCommit(path, sha)
		if e != nil {
			return nil, e
		}
		if c.State == "confirmed" || c.State == "skipped" {
			if c.State == "confirmed" {
				applyCaptureReceipt(&s, c)
			}
			s.Pending = s.Pending[1:]
			if e = integrations.PrivateWrite(path, integrations.CaptureJSON(s)); e != nil {
				return nil, e
			}
			continue
		}
		if c.State == "failed" {
			return captureStatus(s), fmt.Errorf("capture blocked: %s (%s); inspect commits/%s.json", sha, c.Reason, sha)
		}
		if !c.FirstAttempt.IsZero() && a.clockNow().Sub(c.FirstAttempt) >= 25*24*time.Hour {
			c.State = "failed"
			c.Reason = "replay_window_expired"
			if e = integrations.SaveCommit(path, c); e != nil {
				return nil, e
			}
			return captureStatus(s), fmt.Errorf("replay_window_expired: reconcile or explicitly skip the inspected head")
		}
		if a.clockNow().Before(c.RetryAt) {
			break
		}
		// Persist the attempt before preparation too: an unsendable card must have
		// bounded retries and an inspectable, skippable recovery path.
		c.Attempts++
		if c.FirstAttempt.IsZero() {
			c.FirstAttempt = a.clockNow().UTC()
		}
		if e = integrations.SaveCommit(path, c); e != nil {
			return nil, e
		}
		if c.Delivery == nil {
			d, w, e := a.prepareCapture(ctx, auth, s, &c)
			warnings = append(warnings, w...)
			if e != nil {
				if saveErr := saveCaptureFailure(path, &c, a.clockNow(), e, true); saveErr != nil {
					return nil, saveErr
				}
				return captureStatus(s), e
			}
			c.Delivery = &d
			c.State, c.Reason = "pending", ""
		}
		d := c.Delivery
		if d.BaseURL != s.Binding.Workspace || d.HostID != s.Binding.Host || d.PrincipalID != s.Binding.Actor || d.Agent != s.Binding.Agent || d.CommandID != "events.create" || d.Key != anyString(d.Body["request_key"]) || anyString(d.Body["actor_id"]) != s.Binding.Actor {
			return captureStatus(s), integrations.ErrIdentityChanged
		}
		if e = integrations.SaveCommit(path, c); e != nil {
			return nil, e
		}
		// Fence enrollment again immediately before the mutation; freeze bearer/body.
		current, e := a.captureAuth(ctx, cfg, s.Binding)
		if e == nil {
			current.HostID = ""
			_, e = a.invokeTypedJSON(ctx, current, "git sync", "events.create", nil, nil, c.Delivery.Body)
		}
		if e != nil {
			if saveErr := saveCaptureFailure(path, &c, a.clockNow(), e, false); saveErr != nil {
				return nil, saveErr
			}
			return captureStatus(s), e
		}
		c.State = "confirmed"
		c.Reason = ""
		if e = integrations.SaveCommit(path, c); e != nil {
			return nil, e
		}
		applyCaptureReceipt(&s, c)
		s.Pending = s.Pending[1:]
		if e = integrations.PrivateWrite(path, integrations.CaptureJSON(s)); e != nil {
			return nil, e
		}
	}
	data := captureStatus(s)
	if len(warnings) > 0 {
		data["warnings"] = warnings
	}
	if len(s.Pending) > 0 || s.Snapshot != "" {
		data["next"] = "anx git sync"
	}
	return data, nil
}

// One atomic state save applies the FIFO receipt, counters and promise checkpoint.
// A crash after the per-commit confirmation is repaired without another send.
func applyCaptureReceipt(s *integrations.CaptureState, c integrations.CommitCapture) {
	if c.Sequence > s.ConfirmedSequence {
		s.Confirmed++
		s.ConfirmedSequence = c.Sequence
	}
	card := firstNonEmpty(c.CardID, c.Card)
	if c.Next != "" && c.Sequence > s.PromiseCheckpoint[card].Sequence {
		s.PromiseCheckpoint[card] = integrations.CaptureCheckpoint{SHA: c.SHA, Sequence: c.Sequence}
	}
}

// Deferred preparation has never sent a mutation, so explicit skip is safe.
// Uncertain sends stay pending and cannot be skipped until the replay cutoff.
func saveCaptureFailure(path string, c *integrations.CommitCapture, now time.Time, err error, preparing bool) error {
	c.Reason = captureFailureReason(err)
	if preparing {
		c.State = "deferred"
		var n *errnorm.Error
		if errors.As(err, &n) && (n.Kind == errnorm.KindLocal || n.Kind == errnorm.KindUsage || n.Kind == errnorm.KindInternal) {
			c.State = "failed"
			c.Reason = n.Code
		}
	}
	if c.Reason == "rejected" || c.Reason == "conflict" {
		c.State = "failed"
	}
	if preparing {
		c.Reason = "prepare_" + c.Reason
	}
	delay := time.Second << min(c.Attempts, 5)
	c.RetryAt = now.Add(delay + time.Duration(c.SHA[0]%10)*time.Millisecond*50)
	return integrations.SaveCommit(path, *c)
}

func captureFailureReason(e error) string {
	var n *errnorm.Error
	if errors.As(e, &n) {
		if details, ok := n.Details.(map[string]any); ok {
			status, _ := details["status"].(int)
			switch status {
			case 400, 422:
				return "rejected"
			case 409:
				return "conflict"
			case 401, 403, 404:
				return "repair_required"
			}
		}
	}
	return "uncertain"
}
func (a *App) prepareCapture(ctx context.Context, auth config.Resolved, s integrations.CaptureState, c *integrations.CommitCapture) (integrations.Delivery, []output.Warning, error) {
	var warnings []output.Warning
	// Native scoped refs need no legacy list-based ID resolution on a 404.
	result, e := a.invokeRawJSON(ctx, auth, "git prepare", "GET", "/cards/"+url.PathEscape(c.Card), nil)
	if e != nil {
		return integrations.Delivery{}, nil, e
	}
	card := asMap(commandResultBody(result)["card"])
	target, e := cardMessageTarget(card, c.Card)
	if e != nil {
		return integrations.Delivery{}, nil, e
	}
	ref := firstNonEmpty(anyString(card["id"]), anyString(card["ref"]), c.Card)
	c.CardID = ref
	checkpoint := s.PromiseCheckpoint[ref]
	if c.Backfill && checkpoint.SHA == "" && s.LegacyCheckpoint {
		return integrations.Delivery{}, nil, errnorm.Local("checkpoint_reconciliation_required", "legacy checkpoint has no commit SHA; confirm a fresh Git promise for this card or repair its checkpoint before backfill")
	}
	if c.Backfill && checkpoint.SHA != "" && c.SHA != checkpoint.SHA {
		if !integrations.ObjectID(checkpoint.SHA) {
			return integrations.Delivery{}, nil, errnorm.Local("invalid_checkpoint", "invalid promise checkpoint")
		}
		_, err := integrations.Git(ctx, s.Root, 256, "merge-base", "--is-ancestor", c.SHA, checkpoint.SHA)
		if err == nil {
			c.Historical = true
			c.Next, c.What = "", ""
			c.Reason = "below_card_promise_checkpoint"
		} else {
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 {
				return integrations.Delivery{}, nil, err
			}
		}
	}
	note := c.Note + "\n\nCommit: " + c.SHA
	evidence := []string{}
	if s.CommitURL != "" {
		link := strings.ReplaceAll(s.CommitURL, "{sha}", c.SHA)
		note += "\n" + link
		if !c.Historical {
			evidence = append(evidence, link)
		}
	}
	body, e := buildMessagePostedBody(auth, "", target, note, "", nil, "")
	if e != nil {
		return integrations.Delivery{}, nil, e
	}
	payload := asMap(asMap(body["event"])["payload"])
	if c.Next != "" {
		payload["next"] = map[string]any{"at": c.Next, "what": c.What}
		if len(evidence) == 0 {
			if prev, ok := a.previousOwnNext(ctx, auth, target.ThreadID); ok && prev.Format(time.RFC3339Nano) != c.Next {
				warnings = append(warnings, noteWarning("note_moved_without_evidence", "promise moved without evidence", []string{"anx", "git", "status"}))
			}
		}
	}
	if len(evidence) > 0 {
		payload["evidence_refs"] = evidence
	}
	key := "anx-git-v1-" + integrations.Digest(s.Binding.RepoID+"\x00"+c.SHA+"\x00"+ref)
	body["request_key"] = key
	if e = validateEventsCreateInput(body, "git sync"); e != nil {
		return integrations.Delivery{}, nil, e
	}
	return integrations.Delivery{Key: key, PrincipalID: auth.ActorID, HostID: auth.HostID, BaseURL: auth.BaseURL, Agent: auth.As, CommandID: "events.create", Body: body}, warnings, nil
}
