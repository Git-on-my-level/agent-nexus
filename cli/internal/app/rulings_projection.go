package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	"agent-nexus-cli/internal/filelock"
	"agent-nexus-cli/internal/hostidentity"
	"agent-nexus-cli/internal/output"
)

const rulingProjectionStale = "rulings_projection_stale"
const rulingProjectionSize = 64 << 20
const rulingRevisionLegacy = "legacy"
const rulingLegacyPageTTL = 5 * time.Second

type rulingProjection struct {
	FetchedAt time.Time        `json:"fetched_at"`
	Revision  string           `json:"source_revision"`
	BaseURL   string           `json:"workspace_url"`
	Principal string           `json:"principal_key"`
	Rulings   []map[string]any `json:"rulings"`
}

type rulingCollection struct {
	rows        []map[string]any
	names       map[string]map[string]any
	namespaces  map[string]string
	collisions  map[string][]string
	projections []map[string]any
	warnings    []output.Warning
}

type rulingWorkspaces struct {
	selected string
	dir      string
	configs  map[string]config.Resolved
	aliases  map[string]string
}

func (a *App) resolveRulingWorkspaces(cfg config.Resolved) (rulingWorkspaces, error) {
	catalog, dir, err := a.workspaceCatalog(cfg)
	if err != nil {
		return rulingWorkspaces{}, err
	}
	if cfg.Sources["base_url"] == "default" && cfg.AccessToken == "" {
		if host, err := a.resolvedHost(cfg); err == nil {
			cfg.BaseURL = host.BaseURL
		}
	}
	selected := strings.TrimRight(cfg.BaseURL, "/")
	configs := map[string]config.Resolved{selected: cfg}
	aliases := map[string]string{}
	selectedKey := rulingWorkspaceURLKey(selected)
	byURL := map[string]string{selectedKey: selected}
	byID := map[string]string{}
	for _, workspace := range catalog.Workspaces {
		if workspace.Enrolled && workspace.WorkspaceID != "" && rulingWorkspaceURLKey(workspace.BaseURL) == selectedKey {
			byID[workspace.WorkspaceID] = selected
		}
	}
	// Only locally enrolled workspaces can be authenticated for this principal.
	// Never send an explicit bearer to a second workspace.
	for _, workspace := range catalog.Workspaces {
		base := strings.TrimRight(workspace.BaseURL, "/")
		baseKey := rulingWorkspaceURLKey(base)
		target := byURL[baseKey]
		if target != "" {
			aliases[workspace.Alias] = target
			continue
		}
		if !workspace.Enrolled {
			continue
		}
		if target = byID[workspace.WorkspaceID]; target != "" {
			aliases[workspace.Alias] = target
			continue
		}
		aliases[workspace.Alias] = base
		byURL[baseKey] = base
		if workspace.WorkspaceID != "" {
			byID[workspace.WorkspaceID] = base
		}
		other := cfg
		other.BaseURL = base
		other.AccessToken, other.AccessTokenExpiresAt = "", ""
		other.ActorID, other.Agent, other.AgentID, other.Username = "", "", "", ""
		other.HostID, other.HostKeyID, other.HostKeyPath = "", "", ""
		other.Sources = map[string]string{"base_url": "config:rulings-fanout"}
		configs[base] = other
	}
	return rulingWorkspaces{selected: selected, dir: dir, configs: configs, aliases: aliases}, nil
}

func (a *App) collectRulings(ctx context.Context, cfg config.Resolved, force, allowPartial bool) (rulingCollection, error) {
	workspaces, err := a.resolveRulingWorkspaces(cfg)
	if err != nil {
		return rulingCollection{}, err
	}
	projections, warnings, err := a.readRulingProjections(ctx, workspaces, force, allowPartial)
	if err != nil {
		return rulingCollection{}, err
	}
	out, err := indexRulingProjections(projections, workspaces.selected)
	out.warnings = append(warnings, out.warnings...)
	return out, err
}

func (a *App) readRulingProjections(ctx context.Context, workspaces rulingWorkspaces, force, allowPartial bool) ([]rulingProjection, []output.Warning, error) {
	ttl := 15 * time.Minute
	if raw := strings.TrimSpace(a.Getenv("ANX_RULINGS_TTL")); raw != "" {
		var err error
		ttl, err = time.ParseDuration(raw)
		if err != nil || ttl < 0 {
			return nil, nil, errnorm.Usage("invalid_rulings_ttl", "ANX_RULINGS_TTL must be a non-negative duration")
		}
	}
	var bases []string
	for base := range workspaces.configs {
		if base != workspaces.selected {
			bases = append(bases, base)
		}
	}
	sort.Strings(bases)
	var projections []rulingProjection
	var warnings []output.Warning
	for _, base := range append([]string{workspaces.selected}, bases...) {
		projection, stale, err := a.loadRulingProjection(ctx, workspaces.configs[base], workspaces.dir, ttl, force, allowPartial && base == workspaces.selected)
		if err != nil {
			if base == workspaces.selected || !allowPartial {
				failure := *errnorm.Normalize(err)
				failure.Message = fmt.Sprintf("cannot read rulings for %s: %s", base, failure.Message)
				return nil, nil, &failure
			}
			warnings = append(warnings, unavailableRulingProjectionWarning(base))
			continue
		}
		if stale {
			warnings = append(warnings, staleRulingProjectionWarning(base, projection))
		}
		projections = append(projections, projection)
	}
	return projections, warnings, nil
}

func indexRulingProjections(projections []rulingProjection, selected string) (rulingCollection, error) {
	out := rulingCollection{names: map[string]map[string]any{}, namespaces: map[string]string{}, collisions: map[string][]string{}}
	for _, projection := range projections {
		base := projection.BaseURL
		if base != selected && projection.Revision == rulingRevisionLegacy {
			out.warnings = append(out.warnings, unavailableRulingProjectionWarning(base))
		}
		out.projections = append(out.projections, map[string]any{"workspace_url": base, "fetched_at": projection.FetchedAt, "source_revision": projection.Revision})
		for _, ruling := range projection.Rulings {
			id := anyString(ruling["ruling_id"])
			if id == "" {
				if base != selected {
					continue
				}
				id = strings.TrimPrefix(anyString(ruling["ref"]), "ruling:")
			} else if base != selected && ruling["scope"] != "account" {
				continue
			}
			if err := out.claimRulingNames(ruling, id, base); err != nil {
				return out, err
			}
			out.rows = append(out.rows, ruling)
		}
	}
	var ambiguous []string
	for name := range out.collisions {
		ambiguous = append(ambiguous, name)
	}
	sort.Strings(ambiguous)
	for _, name := range ambiguous {
		out.warnings = append(out.warnings, rulingNameCollisionWarning(name, out.collisions[name]))
	}
	return out, nil
}

// Compare equivalent base URLs without treating distinct proxy paths as aliases.
func rulingWorkspaceURLKey(base string) string {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return strings.TrimRight(base, "/")
	}
	scheme, authority := strings.ToLower(u.Scheme), strings.ToLower(u.Host)
	if scheme == "http" && u.Port() == "80" {
		authority = strings.TrimSuffix(authority, ":80")
	}
	if scheme == "https" && u.Port() == "443" {
		authority = strings.TrimSuffix(authority, ":443")
	}
	return scheme + "://" + authority + strings.TrimRight(u.EscapedPath(), "/")
}

func rulingNameCollisionWarning(name string, homes []string) output.Warning {
	message := fmt.Sprintf("ruling name %q occurs in %s", name, strings.Join(homes, ", "))
	if len(homes) == 2 && homes[0] == homes[1] {
		message = fmt.Sprintf("ruling name %q occurs in two records in %s", name, homes[0])
	}
	return output.Warning{
		Code: "ruling_name_collision", Message: message,
		Details: map[string]any{"name": name, "workspace_urls": homes},
	}
}

func unavailableRulingProjectionWarning(base string) output.Warning {
	return output.Warning{
		Code: "rulings_projection_unavailable", Message: "cannot read rulings for " + base,
		Details: map[string]any{"workspace_url": base},
	}
}

func staleRulingProjectionWarning(base string, projection rulingProjection) output.Warning {
	return output.Warning{
		Code: rulingProjectionStale, Message: "using cached rulings for " + base,
		Details: map[string]any{"fetched_at": projection.FetchedAt, "source_revision": projection.Revision},
	}
}

func rulingNames(ruling map[string]any, id string) []string {
	names := []string{id}
	for _, raw := range asSlice(ruling["aliases"]) {
		names = append(names, anyString(raw))
	}
	return names
}

func (c rulingCollection) claimRulingNames(ruling map[string]any, id, base string) error {
	for _, name := range rulingNames(ruling, id) {
		if name == "" {
			return errnorm.Local("rulings_projection_invalid", "ruling has an empty ID or alias")
		}
		if old, exists := c.namespaces[name]; exists {
			homes := c.collisions[name]
			if len(homes) == 0 {
				homes = []string{old}
			}
			if homes[len(homes)-1] != base || len(homes) == 1 {
				homes = append(homes, base)
			}
			c.collisions[name] = homes
			c.names[name] = map[string]any{"kind": "operating", "status": rulingStatusAmbiguous}
			continue
		}
		c.namespaces[name] = base
		c.names[name] = ruling
	}
	return nil
}

func (a *App) loadRulingProjection(ctx context.Context, cfg config.Resolved, dir string, ttl time.Duration, force, liveLegacy bool) (rulingProjection, bool, error) {
	workspace, principal, err := a.rulingCacheIdentity(cfg)
	if err != nil {
		return rulingProjection{}, false, err
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	path := rulingProjectionPath(dir, workspace, principal)
	start := a.clockNow().UTC()
	cached, cacheErr := readRulingProjection(path, base, principal)
	if os.IsNotExist(cacheErr) {
		cached, cacheErr = readRulingProjection(filepath.Join(dir, "rulings", workspace+".json"), base, principal)
	}
	hasCache := cacheErr == nil && !cached.FetchedAt.After(start)
	if hasCache && cached.Revision != rulingRevisionLegacy && !force && start.Sub(cached.FetchedAt) < ttl {
		if !liveLegacy {
			return cached, false, nil
		}
		authCfg, err := a.cfgWithResolvedAuthToken(ctx, cfg)
		if err == nil {
			var legacy []map[string]any
			legacy, err = a.fetchLegacyRulings(ctx, authCfg)
			if err == nil {
				rows := make([]map[string]any, 0, len(cached.Rulings)+len(legacy))
				for _, row := range cached.Rulings {
					if row["kind"] == "operating" {
						rows = append(rows, row)
					}
				}
				cached.Rulings = append(rows, legacy...)
				return cached, false, nil
			}
		}
		if canUseStaleRulings(err) {
			return cached, true, nil
		}
		return rulingProjection{}, false, err
	}
	var legacyCache *rulingProjection
	if hasCache && cached.Revision == rulingRevisionLegacy && !force && start.Sub(cached.FetchedAt) < min(ttl, rulingLegacyPageTTL) {
		legacyCache = &cached
	}
	projection, err := a.fetchRulingProjection(ctx, cfg, legacyCache)
	if err != nil {
		if hasCache && cached.Revision != rulingRevisionLegacy && !force && canUseStaleRulings(err) {
			return cached, true, nil
		}
		return rulingProjection{}, false, err
	}
	if !projection.FetchedAt.IsZero() {
		// A fresh 404 reused the legacy rows. Keep their original expiry.
		return projection, false, nil
	}
	projection.BaseURL, projection.Principal, projection.FetchedAt = base, principal, start
	if skipReadCacheWrites(ctx) {
		return projection, false, nil
	}
	if projection.Revision == rulingRevisionLegacy && hasCache && cached.Revision != rulingRevisionLegacy {
		return projection, false, nil
	}
	if err := saveRulingProjection(path, projection); err != nil {
		return rulingProjection{}, false, errnorm.Wrap(errnorm.KindLocal, "rulings_cache_write_failed", "cannot save the ruling projection", err)
	}
	return projection, false, nil
}

func canUseStaleRulings(err error) bool {
	return errnorm.Normalize(err).Kind == errnorm.KindNetwork || errnorm.ExitCode(err) == 6 || errnorm.ExitCode(err) == 8
}

func rulingProjectionPath(dir, workspace, principal string) string {
	key := sha256.Sum256([]byte(principal))
	return filepath.Join(dir, "rulings", fmt.Sprintf("%s-%x.json", workspace, key[:16]))
}

func (a *App) rulingCacheIdentity(cfg config.Resolved) (string, string, error) {
	workspace := hostidentity.WorkspaceKey("", cfg.BaseURL)
	if cfg.AccessToken != "" {
		sum := sha256.Sum256([]byte(cfg.AccessToken))
		return workspace, "bearer:" + hex.EncodeToString(sum[:]), nil
	}
	host, err := a.resolvedHost(cfg)
	if err != nil {
		return "", "", err
	}
	name, _, err := a.identityName(cfg)
	if err != nil {
		return "", "", err
	}
	return hostidentity.WorkspaceKey(host.WorkspaceID, cfg.BaseURL), "host:" + host.ID + ":" + host.KeyID + ":" + name, nil
}

func readRulingProjection(path, base, principal string) (rulingProjection, error) {
	var p rulingProjection
	file, err := filelock.OpenNoFollow(path, os.O_RDONLY, 0)
	if err != nil {
		return p, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return p, err
	}
	if !stat.Mode().IsRegular() || stat.Mode().Perm()&0077 != 0 || stat.Size() > rulingProjectionSize {
		return p, errors.New("unsafe ruling projection file")
	}
	raw, err := io.ReadAll(io.LimitReader(file, rulingProjectionSize+1))
	if err != nil {
		return p, err
	}
	if len(raw) > rulingProjectionSize || json.Unmarshal(raw, &p) != nil || p.BaseURL != base || p.Principal != principal || p.Revision == "" || p.FetchedAt.IsZero() || p.Rulings == nil {
		return p, errors.New("invalid ruling projection")
	}
	return p, nil
}

func saveRulingProjection(path string, fresh rulingProjection) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("unsafe ruling projection directory")
	}
	lock, err := filelock.OpenNoFollow(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := filelock.Lock(lock); err != nil {
		return err
	}
	defer filelock.Unlock(lock)
	if prev, err := readRulingProjection(path, fresh.BaseURL, fresh.Principal); err == nil && prev.FetchedAt.After(fresh.FetchedAt) {
		return nil
	}
	raw, err := json.Marshal(fresh)
	if err != nil {
		return err
	}
	if len(raw) > rulingProjectionSize {
		return errors.New("ruling projection exceeds the local size limit")
	}
	file, err := os.CreateTemp(dir, ".projection-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(raw)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func (a *App) fetchRulingProjection(ctx context.Context, cfg config.Resolved, legacyCache *rulingProjection) (rulingProjection, error) {
	authCfg, err := a.cfgWithResolvedAuthToken(ctx, cfg)
	if err != nil {
		return rulingProjection{}, err
	}
	// Retry once if a mutation or access change crossed the pagination window.
	for attempt := 0; attempt < 2; attempt++ {
		p, changed, err := a.fetchOperatingRulingPages(ctx, authCfg)
		if err != nil {
			return p, err
		}
		if changed {
			continue
		}
		if p.Revision == rulingRevisionLegacy && legacyCache != nil {
			return *legacyCache, nil
		}
		legacy, err := a.fetchLegacyRulings(ctx, authCfg)
		p.Rulings = append(p.Rulings, legacy...)
		return p, err
	}
	return rulingProjection{}, errnorm.Network("request_failed", "rulings changed during refresh; retry the read")
}

func (a *App) fetchLegacyRulings(ctx context.Context, cfg config.Resolved) ([]map[string]any, error) {
	// The history page is capped independently. Fetch the live page too so
	// newer revocations cannot crowd out active answers.
	seen := map[string]bool{}
	var rows []map[string]any
	for _, path := range []string{"/rulings?include_revoked=true", "/rulings"} {
		legacy, err := a.invokeRawJSON(ctx, cfg, "rulings list", http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
		for _, raw := range asSlice(commandResultBody(legacy)["rulings"]) {
			row := asMap(raw)
			ref := firstNonEmpty(anyString(row["ref"]), anyString(row["id"]))
			if !seen[ref] {
				seen[ref] = true
				rows = append(rows, row)
			}
		}
	}
	return rows, nil
}

func (a *App) fetchOperatingRulingPages(ctx context.Context, cfg config.Resolved) (rulingProjection, bool, error) {
	p := rulingProjection{Rulings: []map[string]any{}}
	cursor, last := "", ""
	var bytesRead int
	seen := map[string]bool{}
	for page := 0; page < 10000; page++ {
		path := "/rulings/projection"
		if cursor != "" {
			path += "?after=" + url.QueryEscape(cursor)
		}
		res, err := a.invokeRawJSON(ctx, cfg, "rulings pull", http.MethodGet, path, nil)
		if err != nil {
			if cursor == "" && intValue(asMap(errnorm.Normalize(err).Details)["status"]) == http.StatusNotFound {
				p.Revision = rulingRevisionLegacy
				return p, false, nil
			}
			return p, false, err
		}
		body := commandResultBody(res)
		revision := anyString(body["source_revision"])
		if revision == "" {
			return p, false, errnorm.Local("rulings_projection_invalid", "projection page is missing source_revision")
		}
		if p.Revision != "" && p.Revision != revision {
			return p, true, nil
		}
		p.Revision = revision
		rawRows, ok := body["rulings"].([]any)
		if !ok || len(rawRows) > 100 {
			return p, false, errnorm.Local("rulings_projection_invalid", "invalid projection page size")
		}
		encoded, err := json.Marshal(rawRows)
		if err != nil {
			return p, false, err
		}
		bytesRead += len(encoded)
		if bytesRead > rulingProjectionSize {
			return p, false, errnorm.Local("rulings_projection_invalid", "projection exceeds the local size limit")
		}
		for _, raw := range rawRows {
			ruling := asMap(raw)
			id := anyString(ruling["ruling_id"])
			if !validRulingDateID(id) || id <= last || ruling["kind"] != "operating" {
				return p, false, errnorm.Local("rulings_projection_invalid", "invalid projection page order or ruling ID")
			}
			last = id
			p.Rulings = append(p.Rulings, ruling)
		}
		next := anyString(body["next_cursor"])
		if next == "" {
			return p, false, nil
		}
		if seen[next] || len(next) > 256 {
			return p, false, errnorm.Local("rulings_projection_invalid", "projection cursor did not advance")
		}
		seen[next] = true
		cursor = next
	}
	return p, false, errnorm.Local("rulings_projection_invalid", "projection exceeds the page limit")
}
