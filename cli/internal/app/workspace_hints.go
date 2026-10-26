package app

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

// Preserve the innermost typed ref from the transport path for error context.
func annotateMissingRef(err error, path string) {
	if !isRemoteNotFound(err) {
		return
	}
	path, _, _ = strings.Cut(path, "?")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// Standalone selectors are used when preserving a ref through read/prefix recovery.
	if len(parts) == 1 {
		ref, _ := url.PathUnescape(parts[0])
		if _, ok := workspaceRefPath(ref); ok {
			errnorm.AnnotateDetail(err, "requested_ref", ref)
		}
		return
	}
	collections := map[string]string{"work": "card", "cards": "card", "boards": "board", "topics": "topic", "docs": "document", "artifacts": "artifact", "threads": "thread", "events": "event", "comments": "event", "revisions": "", "participants": "", "observations": "", "steps": ""}
	for i := len(parts) - 2; i >= 0; i-- {
		kind, collection := collections[parts[i]]
		if !collection {
			continue
		}
		ref, _ := url.PathUnescape(parts[i+1])
		actual, _, _, parseErr := parseTypedRef(ref)
		if parseErr == nil && canonicalResourceKind(actual) == kind {
			if _, ok := workspaceRefPath(ref); ok {
				errnorm.AnnotateDetail(err, "requested_ref", ref)
			}
		}
		// An untyped leaf must not fall back to a typed ancestor resource.
		return
	}
}

func workspaceRefPath(ref string) (string, bool) {
	kind, value, _, err := parseTypedRef(ref)
	if err != nil || value == "" {
		return "", false
	}
	plural := map[string]string{"card": "cards", "board": "boards", "topic": "topics", "document": "docs", "doc": "docs", "artifact": "artifacts", "thread": "threads", "event": "events"}[kind]
	if plural == "" {
		return "", false
	}
	return "/" + plural + "/" + url.PathEscape(ref), true
}

// Workspace diagnostics use local configuration only. Never look for a missing
// resource in another workspace, even if credentials for it are cached.
func (a *App) withWorkspaceHint(cfg config.Resolved, err error) error {
	if !isRemoteNotFound(err) {
		return err
	}
	workspace := a.orientWorkspace(cfg)
	normalized := errnorm.WithWorkspace(err, anyString(workspace["alias"]), anyString(workspace["host"]), anyString(workspace["source"]))
	if normalized.Kind == errnorm.KindRemote && cfg.ConfigDir != "" {
		normalized.Details.(map[string]any)["workspace_config_dir"] = cfg.ConfigDir
	}
	return normalized
}

func (a *App) orientWorkspace(cfg config.Resolved) map[string]any {
	alias := cfg.Sources["workspace_alias"]
	catalog, _, _ := a.workspaceCatalog(cfg)
	if alias == "" {
		for _, ws := range catalog.Workspaces {
			if strings.TrimRight(ws.BaseURL, "/") == strings.TrimRight(cfg.BaseURL, "/") {
				alias = ws.Alias
				break
			}
		}
	}
	// Keep credentials, query parameters and fragments out of diagnostics.
	baseURL, host := "", "unknown"
	if parsed, err := url.Parse(cfg.BaseURL); err == nil && parsed.Host != "" {
		host = parsed.Host
		parsed.User = nil
		parsed.RawQuery = ""
		parsed.Fragment = ""
		baseURL = parsed.String()
	}
	if alias == "" {
		alias = host
	}
	source := cfg.Sources["base_url"]
	if source == "" {
		source = "default"
	}
	others := []string{}
	for _, ws := range catalog.Workspaces {
		if ws.Alias != alias {
			others = append(others, ws.Alias)
		}
	}
	sort.Strings(others)
	return map[string]any{"alias": alias, "base_url": baseURL, "host": host, "source": source, "other_aliases": others}
}

func workspaceText(workspace map[string]any) string {
	return fmt.Sprintf("workspace: %s host=%s source=%s other_aliases=%s\n", anyString(workspace["alias"]), anyString(workspace["host"]), anyString(workspace["source"]), strings.Join(stringList(workspace["other_aliases"]), ","))
}
