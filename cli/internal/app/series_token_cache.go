package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/filelock"
	"agent-nexus-cli/internal/integrations"
)

// Reuse a scoped series token while it has enough lifetime left for the push
// and a small clock-skew allowance. The server keeps its 15-minute TTL and
// validates every push, so a revoked adapter still fails closed.
const seriesTokenExpiryMargin = 60 * time.Second

type seriesTokenCacheEntry struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	BaseURL   string    `json:"base_url"`
	Agent     string    `json:"agent"`
	Adapter   string    `json:"adapter"`
	ConfigDir string    `json:"config_dir"`
}

func seriesTokenBaseURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

func seriesTokenAgent(cfg config.Resolved) string {
	if agent := strings.TrimSpace(cfg.As); agent != "" {
		return agent
	}
	return strings.TrimSpace(cfg.Agent)
}

func (a *App) seriesTokenCacheDir(cfg config.Resolved) (string, error) {
	dir, err := a.configDir(cfg)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "series-tokens"), nil
}

func (a *App) seriesTokenCachePath(cfg config.Resolved, adapter string) (string, seriesTokenCacheEntry, error) {
	dir, err := a.configDir(cfg)
	if err != nil {
		return "", seriesTokenCacheEntry{}, err
	}
	entry := seriesTokenCacheEntry{
		BaseURL:   seriesTokenBaseURL(cfg.BaseURL),
		Agent:     seriesTokenAgent(cfg),
		Adapter:   strings.TrimSpace(adapter),
		ConfigDir: dir,
	}
	key := sha256.Sum256([]byte(strings.Join([]string{entry.BaseURL, entry.Agent, entry.Adapter, entry.ConfigDir}, "\x00")))
	return filepath.Join(dir, "series-tokens", fmt.Sprintf("%x.json", key)), entry, nil
}

func (a *App) loadCachedSeriesToken(path string, want seriesTokenCacheEntry) (string, bool) {
	raw, err := integrations.PrivateRead(path)
	if err != nil {
		return "", false
	}
	var got seriesTokenCacheEntry
	if json.Unmarshal(raw, &got) != nil {
		return "", false
	}
	if got.Token == "" || got.Adapter != want.Adapter || got.Agent != want.Agent || got.BaseURL != want.BaseURL || got.ConfigDir != want.ConfigDir {
		return "", false
	}
	if got.ExpiresAt.Sub(a.clockNow()) < seriesTokenExpiryMargin {
		return "", false
	}
	return got.Token, true
}

func (a *App) saveCachedSeriesToken(path string, want seriesTokenCacheEntry, token string, expiresAt time.Time) error {
	if token == "" || expiresAt.IsZero() {
		return nil
	}
	want.Token = token
	want.ExpiresAt = expiresAt
	return integrations.PrivateWrite(path, integrations.JSON(want))
}

func (a *App) warnSeriesTokenCache(err error) {
	if err == nil || a == nil || a.Stderr == nil {
		return
	}
	fmt.Fprintf(a.Stderr, "warning code=series_token_cache_write_failed message=%q\n", truncateHook(err.Error(), 256))
}

func (a *App) withSeriesTokenLock(ctx context.Context, lockPath string, fn func() error) error {
	dir := filepath.Dir(lockPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}
	lock, err := filelock.OpenNoFollow(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	for {
		if err := filelock.TryLock(lock); err == nil {
			break
		} else if !filelock.Contended(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	defer filelock.Unlock(lock)
	return fn()
}

func seriesTokenExpiresIn(raw any) int {
	switch value := raw.(type) {
	case float64:
		return int(value)
	case int:
		return value
	case int64:
		return int(value)
	case json.Number:
		seconds, _ := value.Int64()
		return int(seconds)
	case string:
		seconds, _ := strconv.Atoi(strings.TrimSpace(value))
		return seconds
	}
	return 0
}

func (a *App) mintSeriesToken(ctx context.Context, owner config.Resolved, adapter string) (string, time.Time, error) {
	response, err := a.hostCall(ctx, owner, "POST", "/adapters/"+url.PathEscape(adapter)+"/token", nil, generatedHeaders(owner))
	if err != nil {
		return "", time.Time{}, err
	}
	bundle := asMap(response["tokens"])
	token := anyString(bundle["access_token"])
	if token == "" {
		return "", time.Time{}, errnorm.Usage("invalid_response", "missing scoped token")
	}
	issuedAt := a.clockNow()
	expiresIn := seriesTokenExpiresIn(bundle["expires_in"])
	if expiresIn <= 0 {
		return token, time.Time{}, nil
	}
	return token, issuedAt.Add(time.Duration(expiresIn) * time.Second), nil
}

func (a *App) mintAndStoreSeriesToken(ctx context.Context, owner config.Resolved, adapter, path string, want seriesTokenCacheEntry, token *string) error {
	issuedToken, expiresAt, err := a.mintSeriesToken(ctx, owner, adapter)
	if err != nil {
		return err
	}
	*token = issuedToken
	if err := a.saveCachedSeriesToken(path, want, issuedToken, expiresAt); err != nil {
		a.warnSeriesTokenCache(err)
	}
	return nil
}

func (a *App) seriesTokenForAdapter(ctx context.Context, owner config.Resolved, adapter string) (string, error) {
	path, want, err := a.seriesTokenCachePath(owner, adapter)
	if err != nil {
		return "", err
	}
	token := ""
	err = a.withSeriesTokenLock(ctx, path+".lock", func() error {
		if cached, ok := a.loadCachedSeriesToken(path, want); ok {
			token = cached
			return nil
		}
		return a.mintAndStoreSeriesToken(ctx, owner, adapter, path, want, &token)
	})
	return token, err
}

func (a *App) refreshSeriesToken(ctx context.Context, owner config.Resolved, adapter string) (string, error) {
	path, want, err := a.seriesTokenCachePath(owner, adapter)
	if err != nil {
		return "", err
	}
	token := ""
	err = a.withSeriesTokenLock(ctx, path+".lock", func() error {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return a.mintAndStoreSeriesToken(ctx, owner, adapter, path, want, &token)
	})
	return token, err
}

func seriesTokenUnauthorized(err error) bool {
	var typed *errnorm.Error
	if !errors.As(err, &typed) {
		return false
	}
	details, _ := typed.Details.(map[string]any)
	switch status := details["status"].(type) {
	case int:
		return status == 401
	case float64:
		return status == 401
	}
	return false
}

// discardCachedSeriesTokensForAdapter clears local cached credentials for one
// adapter after a successful revoke/delete. It ignores the acting agent so a
// revoke by a different local admin also clears the owner's cache.
func (a *App) discardCachedSeriesTokensForAdapter(ctx context.Context, cfg config.Resolved, adapter string) error {
	dir, err := a.seriesTokenCacheDir(cfg)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	baseURL := seriesTokenBaseURL(cfg.BaseURL)
	adapter = strings.TrimSpace(adapter)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		lockErr := a.withSeriesTokenLock(ctx, path+".lock", func() error {
			raw, err := integrations.PrivateRead(path)
			if err != nil {
				return nil
			}
			var cached seriesTokenCacheEntry
			if json.Unmarshal(raw, &cached) != nil || cached.Adapter != adapter || cached.BaseURL != baseURL {
				return nil
			}
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			return nil
		})
		if lockErr != nil {
			return lockErr
		}
	}
	return nil
}
