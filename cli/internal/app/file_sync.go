package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	hostbridge "agent-nexus-cli/internal/bridge"
	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/filetransport"
	"agent-nexus-cli/internal/integrations"
)

type syncOptions struct {
	verb, directory string
	cards           []string
	dry             bool
}

func parseSyncOptions(args []string) (syncOptions, error) {
	var o syncOptions
	if len(args) == 0 {
		return o, nil
	}
	o.verb = args[0]
	rest := args[1:]
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		o.directory = rest[0]
		rest = rest[1:]
	}
	fs := newSilentFlagSet("sync " + o.verb)
	var cards trackedStrings
	switch o.verb {
	case "enable":
		fs.Var(&cards, "card-id", "Explicit allowlisted card (repeatable)")
		fs.BoolVar(&o.dry, "dry-run", false, "Preview without local changes")
	case "disable", "status":
	default:
		return o, errnorm.Usage("unknown_subcommand", "use sync enable|disable|status")
	}
	if e := fs.Parse(rest); e != nil {
		return o, errnorm.Usage("invalid_flags", e.Error())
	}
	if len(fs.Args()) > 0 || o.directory == "" {
		return o, errnorm.Usage("invalid_args", "sync requires a directory")
	}
	o.cards = cards.values
	if o.verb == "enable" && (len(o.cards) == 0 || len(o.cards) > filetransport.MaxCards) {
		return o, errnorm.Usage("invalid_args", "allowlist 1..100 explicit --card-id refs")
	}
	for _, ref := range o.cards {
		if !integrations.ValidCard(ref) {
			return o, errnorm.Usage("invalid_args", "--card-id requires a card: ref")
		}
	}
	return o, nil
}

const syncHelp = `Optional scoped file transport, off until explicitly enabled.
anx sync enable <directory> --card-id <ref> [--card-id <ref> ...] [--dry-run]
anx sync disable <directory>
anx sync status <directory>

Enable pins the current workspace/host/agent and explicit card allowlist, and
registers a files job with the optional host bridge; it never installs a service.
Mount ONLY .anx/outbox writable into the producer sandbox. No token/network needed.
Producer UUIDs, notes and optional next promises only. Mirror is read-only.
See cli/docs/file-transport.md for intake receipts, stale_after and reconciliation.
`

func init() {
	runtimeHelpManualDocTopics = append(runtimeHelpManualDocTopics, runtimeHelpDocTopic{Path: "sync", Kind: "manual", Summary: "Opt-in scoped file outbox and read-only card mirror."})
	for _, verb := range []string{"enable", "disable", "status"} {
		topic := localHelperTopic{Path: "sync " + verb, Summary: map[string]string{"enable": "Enable a directory-scoped file job with an explicit card allowlist.", "disable": "Disable file delivery and clear generated caches, retaining original work.", "status": "Inspect file binding, pending work, failures and mirror freshness."}[verb], JSONShape: "`directory_uuid`, `enabled`, `pending`, `failed`, `last_error_code`", Examples: []string{"anx sync " + verb + " <directory>"}}
		if verb == "enable" {
			topic.Flags = []localHelperFlag{{Name: "--card-id", Description: "Explicit card ref (repeatable, max 100)."}, {Name: "--dry-run", Description: "Preview binding and allowlist without writing."}}
			topic.Examples = []string{"anx --as worker sync enable <directory> --card-id card:<handle> --dry-run"}
		}
		localHelperTopics = append(localHelperTopics, topic)
	}
}
func (a *App) runFileSync(ctx context.Context, args []string, cfg config.Resolved) (result *commandResult, command string, retErr error) {
	defer func() {
		switch {
		case errors.Is(retErr, integrations.ErrIdentityChanged):
			retErr = errnorm.Local("identity_changed", "directory binding belongs to another enrollment; preserve retained work and reconcile it before changing identity")
		case errors.Is(retErr, filetransport.ErrUnsafe):
			retErr = errnorm.Local("unsafe_file_transport_path", "file transport requires a safe, owner-only directory and matching binding")
		case errors.Is(retErr, filetransport.ErrCapacity):
			retErr = errnorm.Local("file_transport_capacity", "file transport is full; retain and reconcile existing work before admitting more")
		}
	}()

	o, e := parseSyncOptions(args)
	name := strings.TrimSpace("sync " + o.verb)
	if e != nil {
		return nil, name, e
	}
	if o.verb == "" {
		return &commandResult{Text: syncHelp}, "sync", nil
	}
	directory := o.directory
	if !filepath.IsAbs(directory) {
		cwd, err := a.workspaceCWD()
		if err != nil {
			return nil, name, err
		}
		directory = filepath.Join(cwd, directory)
	}
	directory = filepath.Clean(directory)
	canonical, err := filepath.EvalSymlinks(directory)
	if err != nil && os.IsNotExist(err) && o.verb != "enable" {
		if parent, pe := filepath.EvalSymlinks(filepath.Dir(directory)); pe == nil {
			canonical = filepath.Join(parent, filepath.Base(directory))
			err = nil
		}
	}
	if err != nil {
		return nil, name, err
	}
	directory = canonical
	root, e := a.configDir(cfg)
	if e != nil {
		return nil, name, e
	}
	root, e = filepath.Abs(root)
	if e != nil {
		return nil, name, e
	}
	cfg.ConfigDir = root
	stateDir := filetransport.StateDir(root, directory)
	if o.verb == "status" {
		s, e := filetransport.Load(stateDir)
		if os.IsNotExist(e) {
			return &commandResult{Data: map[string]any{"directory": directory, "configured": false, "enabled": false, "pending": 0, "failed": 0, "stale": true}}, name, nil
		}
		if e != nil {
			return nil, name, e
		}
		return &commandResult{Data: fileSyncStatus(stateDir, s, a.clockNow())}, name, nil
	}
	if o.verb == "disable" {
		s, e := filetransport.Disable(ctx, stateDir, a.clockNow())
		if s.Version != 1 {
			return nil, name, e
		}
		registryErr := a.registerFilesJob(ctx, s.Binding, stateDir, false)
		if e == nil {
			e = registryErr
		}
		return &commandResult{Data: fileSyncStatus(stateDir, s, a.clockNow())}, name, e
	}
	cfg, e = a.hookWorkspace(cfg, directory, cfg.Sources["sync_workspace"])
	if e != nil {
		return nil, name, e
	}
	w, agent, e := a.bridgeRegistration(ctx, cfg)
	if e != nil {
		return nil, name, e
	}
	cfg, e = a.validateBridgeAgent(ctx, w, agent)
	if e != nil {
		return nil, name, e
	}
	binding := filetransport.Binding{ID: filetransport.UUID(), Directory: directory, Workspace: w.ID, BaseURL: w.BaseURL, Host: w.HostID, Actor: agent.ActorID, Agent: agent.Name}
	if old, err := filetransport.Load(stateDir); err == nil {
		binding.ID = old.Binding.ID
	} else if !os.IsNotExist(err) {
		return nil, name, err
	}
	seen := map[string]bool{}
	for _, ref := range o.cards {
		card, err := a.fileCard(ctx, cfg, ref)
		if err != nil {
			return nil, name, err
		}
		id := anyString(card["id"])
		canonical := firstNonEmpty(anyString(card["ref"]), "card:"+id)
		if seen[id] {
			continue
		}
		seen[id] = true
		binding.Cards = append(binding.Cards, filetransport.Card{ID: id, Ref: canonical})
	}
	sort.Slice(binding.Cards, func(i, j int) bool { return binding.Cards[i].ID < binding.Cards[j].ID })
	if e = binding.Validate(); e != nil {
		return nil, name, e
	}
	if e = excludeFileTransport(ctx, directory, !o.dry); e != nil {
		return nil, name, e
	}
	if o.dry {
		return &commandResult{Data: map[string]any{"binding": binding, "dry_run": true, "enabled": false}}, name, nil
	}
	s, e := filetransport.Enable(ctx, stateDir, binding, a.clockNow())
	if e != nil {
		return nil, name, e
	}
	e = a.registerFilesJob(ctx, binding, stateDir, true)
	return &commandResult{Data: fileSyncStatus(stateDir, s, a.clockNow())}, name, e
}
func fileSyncStatus(dir string, s filetransport.State, now time.Time) map[string]any {
	detail, inspectErr := filetransport.Inspect(dir, now)
	return map[string]any{"inspection": detail, "inspection_ok": inspectErr == nil, "directory_uuid": s.Binding.ID, "binding": s.Binding, "enabled": s.Enabled, "pending": len(s.Pending), "failed": s.Failed, "records": s.Records, "last_server_contact": s.LastContact, "stale_after": s.LastContact.Add(filetransport.StaleAfter), "stale": !s.Enabled || s.LastContact.IsZero() || now.Sub(s.LastContact) >= filetransport.StaleAfter, "last_error_code": s.LastCode, "state_path": dir}
}
func excludeFileTransport(ctx context.Context, directory string, write bool) error {
	root, common, e := integrations.Repo(ctx, directory)
	if e != nil {
		return nil
	} // Non-Git directories are valid.
	rel, e := filepath.Rel(root, filepath.Join(directory, ".anx"))
	if e != nil || strings.HasPrefix(rel, "..") {
		return filetransport.ErrUnsafe
	}
	tracked, e := integrations.Git(ctx, root, 4096, "ls-files", "-z", "--", rel)
	if e != nil {
		return e
	}
	if len(tracked) > 0 {
		return fmt.Errorf("file transport destination is tracked; remove it from Git before enabling")
	}
	if !write {
		return nil
	}
	escaped := rel
	for _, ch := range []string{"\\", " ", "*", "?", "[", "]", "#", "!"} {
		escaped = strings.ReplaceAll(escaped, ch, "\\"+ch)
	}
	line := "/" + filepath.ToSlash(escaped) + "/"
	path := filepath.Join(common, "info", "exclude")
	raw, e := integrations.Read(path)
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	for _, old := range strings.Split(string(raw), "\n") {
		if old == line {
			return nil
		}
	}
	return integrations.AtomicWrite(path, []byte(strings.TrimRight(string(raw), "\n")+"\n"+line+"\n"))
}
func (a *App) registerFilesJob(ctx context.Context, b filetransport.Binding, dir string, enable bool) error {
	root, e := a.hostBridgeRoot()
	if e != nil {
		return e
	}
	return hostbridge.Update(ctx, root, func(r *hostbridge.Registry) error {
		for wi := range r.Workspaces {
			w := &r.Workspaces[wi]
			if w.BaseURL != b.BaseURL || w.HostID != b.Host {
				continue
			}
			if w.ID != b.Workspace {
				return integrations.ErrIdentityChanged
			}
			for ai := range w.Agents {
				agent := &w.Agents[ai]
				if agent.ActorID != b.Actor {
					continue
				}
				dirs := []string{}
				for _, d := range agent.FileDirs {
					if d != dir {
						dirs = append(dirs, d)
					}
				}
				if enable {
					dirs = append(dirs, dir)
				}
				agent.FileDirs = dirs
				if agent.Jobs == nil {
					agent.Jobs = map[string]bool{}
				}
				agent.Jobs["files"] = len(dirs) > 0
				return nil
			}
		}
		if !enable {
			return nil
		}
		w := hostbridge.Workspace{ID: b.Workspace, BaseURL: b.BaseURL, HostID: b.Host, ConfigDir: filepath.Dir(filepath.Dir(dir))}
		agent := hostbridge.Agent{Name: b.Agent, ActorID: b.Actor, Jobs: map[string]bool{"files": true}, RegistryDirs: []string{}, FileDirs: []string{dir}, Policy: hostbridge.Policy{MaxAttempts: 5, Timeout: 30 * time.Minute}}
		return hostbridge.Register(r, w, agent)
	})
}
func (a *App) fileCard(ctx context.Context, cfg config.Resolved, ref string) (map[string]any, error) {
	r, e := a.invokeRawJSON(ctx, cfg, "file card", "GET", "/cards/"+url.PathEscape(ref), nil)
	if e != nil {
		return nil, e
	}
	card := asMap(commandResultBody(r)["card"])
	if anyString(card["id"]) == "" {
		return nil, fmt.Errorf("invalid_card")
	}
	return card, nil
}
func fileErrorClass(e error) string {
	if errors.Is(e, integrations.ErrIdentityChanged) {
		return "identity"
	}
	var typed *errnorm.Error
	if errors.As(e, &typed) {
		switch typed.Code {
		case "host_not_enrolled", "host_revoked", "agent_revoked", "identity_changed", "key_load_failed":
			return "identity"
		}
		status := fileHTTPStatus(typed)
		if status == 401 || status == 403 || status == 404 {
			return "denied"
		}
		if status == 400 || status == 409 || status == 422 || typed.Kind == errnorm.KindUsage {
			return "invalid"
		}
	}
	return "retry"
}
func fileHTTPStatus(e *errnorm.Error) int {
	if details, ok := e.Details.(map[string]any); ok {
		switch v := details["status"].(type) {
		case int:
			return v
		case float64:
			return int(v)
		}
	}
	return 0
}
