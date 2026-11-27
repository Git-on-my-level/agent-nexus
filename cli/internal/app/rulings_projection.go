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
	projections []map[string]any
	warnings    []output.Warning
}

func (a *App) collectRulings(ctx context.Context, cfg config.Resolved, force bool) (rulingCollection, error) {
	out := rulingCollection{names: map[string]map[string]any{}, namespaces: map[string]string{}}
	catalog, dir, err := a.workspaceCatalog(cfg)
	if err != nil {
		return out, err
	}
	ttl := 15 * time.Minute
	if raw := strings.TrimSpace(a.Getenv("ANX_RULINGS_TTL")); raw != "" {
		ttl, err = time.ParseDuration(raw)
		if err != nil || ttl < 0 {
			return out, errnorm.Usage("invalid_rulings_ttl", "ANX_RULINGS_TTL must be a non-negative duration")
		}
	}
	if cfg.Sources["base_url"] == "default" && cfg.AccessToken == "" {
		if host, err := a.resolvedHost(cfg); err == nil {
			cfg.BaseURL = host.BaseURL
		}
	}
	selected := strings.TrimRight(cfg.BaseURL, "/")
	configs := map[string]config.Resolved{selected: cfg}
	// Only locally enrolled workspaces can be authenticated for this principal.
	// Never send an explicit bearer to a second workspace.
	for _, workspace := range catalog.Workspaces {
		if !workspace.Enrolled {
			continue
		}
		base := strings.TrimRight(workspace.BaseURL, "/")
		if base == selected {
			continue
		}
		other := cfg
		other.BaseURL = base
		other.AccessToken, other.AccessTokenExpiresAt = "", ""
		other.ActorID, other.Agent, other.AgentID, other.Username = "", "", "", ""
		other.HostID, other.HostKeyID, other.HostKeyPath = "", "", ""
		other.Sources = map[string]string{"base_url": "config:rulings-fanout"}
		if _, _, err := a.identityName(other); err == nil {
			configs[base] = other
		}
	}
	var bases []string
	for base := range configs {
		if base != selected {
			bases = append(bases, base)
		}
	}
	sort.Strings(bases)
	for _, base := range append([]string{selected}, bases...) {
		projection, stale, err := a.loadRulingProjection(ctx, configs[base], dir, ttl, force)
		if err != nil {
			return out, err
		}
		if stale {
			out.warnings = append(out.warnings, staleRulingProjectionWarning(base, projection))
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
				// Hidden outside this workspace's selected view, but still
				// contributes to account-wide name collision checks below.
				if err := out.claimRulingNames(ruling, id, base); err != nil {
					return out, err
				}
				continue
			}
			if err := out.claimRulingNames(ruling, id, base); err != nil {
				return out, err
			}
			for _, name := range rulingNames(ruling, id) {
				out.names[name] = ruling
			}
			out.rows = append(out.rows, ruling)
		}
	}
	return out, nil
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
			return errnorm.Local("ruling_name_collision", fmt.Sprintf("ruling name %q occurs in %s and %s", name, old, base))
		}
		c.namespaces[name] = base
	}
	return nil
}

func (a *App) loadRulingProjection(ctx context.Context, cfg config.Resolved, dir string, ttl time.Duration, force bool) (rulingProjection, bool, error) {
	workspace, principal, err := a.rulingCacheIdentity(cfg)
	if err != nil {
		return rulingProjection{}, false, err
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	path := filepath.Join(dir, "rulings", workspace+".json")
	start := a.clockNow().UTC()
	cached, cacheErr := readRulingProjection(path, base, principal)
	hasCache := cacheErr == nil && !cached.FetchedAt.After(start)
	if hasCache && !force && start.Sub(cached.FetchedAt) < ttl {
		return cached, false, nil
	}
	projection, err := a.fetchRulingProjection(ctx, cfg)
	if err != nil {
		failure := errnorm.Normalize(err)
		if hasCache && !force && (failure.Kind == errnorm.KindNetwork || errnorm.ExitCode(err) == 6 || errnorm.ExitCode(err) == 8) {
			return cached, true, nil
		}
		return rulingProjection{}, false, err
	}
	projection.BaseURL, projection.Principal, projection.FetchedAt = base, principal, start
	if err := saveRulingProjection(path, projection); err != nil {
		return rulingProjection{}, false, errnorm.Wrap(errnorm.KindLocal, "rulings_cache_write_failed", "cannot save the ruling projection", err)
	}
	return projection, false, nil
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

func (a *App) fetchRulingProjection(ctx context.Context, cfg config.Resolved) (rulingProjection, error) {
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
		// The legacy history page is capped independently. Fetch its live
		// page too so newer revocations cannot crowd out active answers.
		seen := map[string]bool{}
		for _, path := range []string{"/rulings?include_revoked=true", "/rulings"} {
			legacy, err := a.invokeRawJSON(ctx, authCfg, "rulings list", http.MethodGet, path, nil)
			if err != nil {
				return p, err
			}
			for _, raw := range asSlice(commandResultBody(legacy)["rulings"]) {
				row := asMap(raw)
				ref := firstNonEmpty(anyString(row["ref"]), anyString(row["id"]))
				if !seen[ref] {
					seen[ref] = true
					p.Rulings = append(p.Rulings, row)
				}
			}
		}
		return p, nil
	}
	return rulingProjection{}, errnorm.Network("request_failed", "rulings changed during refresh; retry the read")
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
			if cursor == "" && errnorm.ExitCode(err) == 3 {
				p.Revision = "legacy"
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
