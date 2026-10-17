package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/hostidentity"
)

const workspaceHintLimit = 4
const workspaceHintBudget = 1500 * time.Millisecond
const workspaceHintTimeout = 350 * time.Millisecond

// Only transport path selectors count as missing refs. Prose, evidence and
// unrelated refs elsewhere in argv must never trigger a cross-workspace lookup.
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

// Hints are read-only and best effort: use only unexpired, workspace-local
// cached tokens. Never borrow ANX_ACCESS_TOKEN, exchange a grant, follow a
// redirect or change the workspace of the original operation.
func (a *App) withWorkspaceHint(ctx context.Context, cfg config.Resolved, argv []string, err error) error {
	if !isRemoteNotFound(err) || a.Getenv("ANX_WORKSPACE_HINTS") == "0" || strings.EqualFold(a.Getenv("ANX_WORKSPACE_HINTS"), "false") {
		return err
	}
	normalized := errnorm.Normalize(err)
	details, _ := normalized.Details.(map[string]any)
	ref := anyString(details["requested_ref"])
	path, ok := workspaceRefPath(ref)
	if !ok {
		return err
	}
	catalog, dir, loadErr := a.workspaceCatalog(cfg)
	if loadErr != nil || len(catalog.Workspaces) < 2 {
		return err
	}
	name, _, nameErr := a.identityName(cfg)
	if nameErr != nil {
		return err
	}
	current := cfg.Sources["workspace_alias"]
	if current == "" {
		current = cfg.BaseURL
	}
	for _, ws := range catalog.Workspaces {
		if current == cfg.BaseURL && strings.TrimRight(ws.BaseURL, "/") == strings.TrimRight(cfg.BaseURL, "/") {
			current = ws.Alias
			break
		}
	}
	hosts, hostErr := hostidentity.ListAt(dir)
	if hostErr != nil {
		return err
	}
	byURL := map[string]hostidentity.Host{}
	ambiguous := map[string]bool{}
	for _, host := range hosts {
		key := strings.TrimRight(host.BaseURL, "/")
		if _, exists := byURL[key]; exists {
			ambiguous[key] = true
		}
		byURL[key] = host
	}
	ctx, cancel := context.WithTimeout(ctx, workspaceHintBudget)
	defer cancel()
	client := &http.Client{Timeout: workspaceHintTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	seen := map[string]bool{strings.TrimRight(cfg.BaseURL, "/"): true}
	checked := 0
	for _, ws := range catalog.Workspaces {
		base := strings.TrimRight(ws.BaseURL, "/")
		if seen[base] || !ws.Enrolled {
			continue
		}
		seen[base] = true
		if checked >= workspaceHintLimit || ctx.Err() != nil {
			break
		}
		host, found := byURL[base]
		if !found || ambiguous[base] {
			continue
		}
		cachePath := filepath.Join(hostidentity.DirAt(dir, host.WorkspaceID, base), "token-"+name+".json")
		st, statErr := os.Lstat(cachePath)
		if statErr != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 || st.Size() > 64*1024 {
			continue
		}
		raw, readErr := os.ReadFile(cachePath)
		var cache tokenCache
		if readErr != nil || json.Unmarshal(raw, &cache) != nil || cache.Token == "" || !cache.ExpiresAt.After(a.clockNow()) {
			continue
		}
		checked++
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
		if reqErr != nil {
			continue
		}
		req.Header.Set("Authorization", "Bearer "+cache.Token)
		resp, callErr := client.Do(req)
		if callErr != nil {
			continue
		}
		var record map[string]any
		decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&record)
		resp.Body.Close()
		kind, _, _, _ := parseTypedRef(ref)
		resource := asMap(record[canonicalResourceKind(kind)])
		if resp.StatusCode != http.StatusOK || decodeErr != nil || !workspaceResourceMatches(ref, resource) {
			continue
		}
		normalized.Message = fmt.Sprintf("%s not found in workspace %q; it exists in workspace %q — rerun with --workspace %s", ref, current, ws.Alias, ws.Alias)
		normalized.Hint = "Rerun the original command with --workspace " + ws.Alias + "."
		if details == nil {
			details = map[string]any{}
		}
		details["workspace_hint"] = map[string]any{"ref": ref, "current_workspace": current, "workspace": ws.Alias}
		retry := []string{"anx"}
		if cfg.ConfigDir != "" {
			retry = append(retry, "--config-dir", cfg.ConfigDir)
		}
		retry = append(retry, "--workspace", ws.Alias)
		if cfg.As != "" {
			retry = append(retry, "--as", cfg.As)
		}
		details["workspace_next_argv"] = append(retry, argv...)
		normalized.Details = details
		return normalized
	}
	return err
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
	if alias == "" {
		alias = cfg.BaseURL
	}
	others := []string{}
	for _, ws := range catalog.Workspaces {
		if ws.Alias != alias {
			others = append(others, ws.Alias)
		}
	}
	sort.Strings(others)
	return map[string]any{"alias": alias, "base_url": cfg.BaseURL, "source": cfg.Sources["base_url"], "other_aliases": others}
}

// Require evidence that this is the requested resource, not just a successful endpoint.
func workspaceResourceMatches(requested string, resource map[string]any) bool {
	kind, value, _, err := parseTypedRef(requested)
	if err != nil {
		return false
	}
	if ref, ok := resource["ref"].(string); ok {
		actualKind, actualValue, _, refErr := parseTypedRef(ref)
		if refErr != nil || canonicalResourceKind(actualKind) != canonicalResourceKind(kind) {
			return false
		}
		if actualValue == value {
			return true
		}
	}
	for _, key := range []string{"handle", "id"} {
		if actual, ok := resource[key].(string); ok && actual == value {
			return true
		}
	}
	return false
}

// Argument vectors are lossless. stringList is for normalized identifiers, not replay.
func exactArgv(raw any) []string {
	switch values := raw.(type) {
	case []string:
		return append([]string(nil), values...)
	case []any:
		result := make([]string, 0, len(values))
		for _, value := range values {
			s, ok := value.(string)
			if !ok {
				return nil
			}
			result = append(result, s)
		}
		return result
	}
	return nil
}
