package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-nexus-cli/internal/config"
)

type seriesTokenCall struct {
	path  string
	token string
}

type seriesTokenFixture struct {
	mu      sync.Mutex
	mints   map[string]int
	pushes  []seriesTokenCall
	invalid map[string]bool
}

func newSeriesTokenFixture() *seriesTokenFixture {
	return &seriesTokenFixture{mints: map[string]int{}, invalid: map[string]bool{}}
}

func (f *seriesTokenFixture) mintCount(adapter string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mints[adapter]
}

func (f *seriesTokenFixture) pushCalls() []seriesTokenCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]seriesTokenCall(nil), f.pushes...)
}

func (f *seriesTokenFixture) reject(token string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.invalid[token] = true
}

func (f *seriesTokenFixture) handler(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case len(parts) == 3 && parts[0] == "adapters" && parts[2] == "token" && r.Method == http.MethodPost:
		adapter := parts[1]
		f.mu.Lock()
		f.mints[adapter]++
		token := fmt.Sprintf("%s-scoped-%d", adapter, f.mints[adapter])
		f.mu.Unlock()
		fmt.Fprintf(w, `{"tokens":{"access_token":%q,"token_type":"Bearer","expires_in":900}}`, token)
	case len(parts) == 3 && parts[0] == "adapters" && (parts[2] == "revoke" || parts[2] == "delete"):
		fmt.Fprintf(w, `{"adapter":{"name":%q}}`, parts[1])
	case len(parts) == 3 && parts[0] == "series" && parts[2] == "points" && r.Method == http.MethodPost:
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		f.mu.Lock()
		f.pushes = append(f.pushes, seriesTokenCall{path: r.URL.Path, token: token})
		rejected := f.invalid[token]
		f.mu.Unlock()
		if rejected {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"error":{"code":"invalid_token","message":"token expired or revoked"}}`)
			return
		}
		fmt.Fprintf(w, `{"series":%q,"accepted":true}`, parts[1])
	default:
		http.NotFound(w, r)
	}
}

func newSeriesTokenServer(t *testing.T, fixture *seriesTokenFixture) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(fixture.handler))
	t.Cleanup(server.Close)
	return server
}

func runSeriesTokenPush(t *testing.T, a *App, cfg config.Resolved, adapter, series, value string) {
	t.Helper()
	_, _, err := a.runSeriesCommand(context.Background(), []string{"series", "push", series, value, "--adapter", adapter}, cfg)
	if err != nil {
		t.Fatalf("push %s: %v", adapter, err)
	}
}

func seriesTestConfig(baseURL, agent string) config.Resolved {
	return config.Resolved{BaseURL: baseURL, AccessToken: "owner", Agent: agent, Timeout: 5 * time.Second}
}

func TestSeriesPushReusesCachedTokenWithinTTL(t *testing.T) {
	a := newTestApp(t)
	a.Stderr = &bytes.Buffer{}
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }
	fixture := newSeriesTokenFixture()
	server := newSeriesTokenServer(t, fixture)
	cfg := seriesTestConfig(server.URL, "collector")

	runSeriesTokenPush(t, a, cfg, "collector", "builds", "1")
	runSeriesTokenPush(t, a, cfg, "collector", "builds", "2")

	if got := fixture.mintCount("collector"); got != 1 {
		t.Fatalf("minted %d tokens, want 1", got)
	}
	pushes := fixture.pushCalls()
	if len(pushes) != 2 || pushes[0].token != "collector-scoped-1" || pushes[1].token != "collector-scoped-1" {
		t.Fatalf("pushes: %#v", pushes)
	}

	path, _, err := a.seriesTokenCachePath(cfg, "collector")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"token": "collector-scoped-1"`) || strings.Contains(string(raw), `"token": "owner"`) {
		t.Fatalf("cache contents: %s", raw)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("cache mode = %v, err=%v", info, err)
	}
	if info, err := os.Stat(filepath.Dir(path)); err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("cache dir mode = %v, err=%v", info, err)
	}
}

func TestSeriesPushMintsAgainAfterCacheExpires(t *testing.T) {
	a := newTestApp(t)
	a.Stderr = &bytes.Buffer{}
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }
	fixture := newSeriesTokenFixture()
	server := newSeriesTokenServer(t, fixture)
	cfg := seriesTestConfig(server.URL, "collector")

	runSeriesTokenPush(t, a, cfg, "collector", "builds", "1")
	now = now.Add(16 * time.Minute)
	runSeriesTokenPush(t, a, cfg, "collector", "builds", "2")

	if got := fixture.mintCount("collector"); got != 2 {
		t.Fatalf("minted %d tokens, want 2", got)
	}
	pushes := fixture.pushCalls()
	if len(pushes) != 2 || pushes[0].token != "collector-scoped-1" || pushes[1].token != "collector-scoped-2" {
		t.Fatalf("pushes: %#v", pushes)
	}
}

func TestSeriesPushRetriesOnceAfterCachedTokenIsRejected(t *testing.T) {
	a := newTestApp(t)
	a.Stderr = &bytes.Buffer{}
	a.now = func() time.Time { return time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC) }
	fixture := newSeriesTokenFixture()
	server := newSeriesTokenServer(t, fixture)
	cfg := seriesTestConfig(server.URL, "collector")

	runSeriesTokenPush(t, a, cfg, "collector", "builds", "1")
	fixture.reject("collector-scoped-1")
	runSeriesTokenPush(t, a, cfg, "collector", "builds", "2")

	if got := fixture.mintCount("collector"); got != 2 {
		t.Fatalf("minted %d tokens, want 2", got)
	}
	pushes := fixture.pushCalls()
	if len(pushes) != 3 {
		t.Fatalf("pushes: %#v", pushes)
	}
	if pushes[1].token != "collector-scoped-1" || pushes[2].token != "collector-scoped-2" {
		t.Fatalf("cached-token retry: %#v", pushes)
	}
	path, _, err := a.seriesTokenCachePath(cfg, "collector")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "collector-scoped-2") {
		t.Fatalf("cache was not refreshed: %s", raw)
	}
}

func TestSeriesTokenCacheIsolation(t *testing.T) {
	a := newTestApp(t)
	a.Stderr = &bytes.Buffer{}
	a.now = func() time.Time { return time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC) }

	t.Run("adapters", func(t *testing.T) {
		fixture := newSeriesTokenFixture()
		server := newSeriesTokenServer(t, fixture)
		cfg := seriesTestConfig(server.URL, "collector")
		runSeriesTokenPush(t, a, cfg, "alpha", "alpha-series", "1")
		runSeriesTokenPush(t, a, cfg, "beta", "beta-series", "2")
		if fixture.mintCount("alpha") != 1 || fixture.mintCount("beta") != 1 {
			t.Fatalf("mints: %#v", fixture.mints)
		}
		pushes := fixture.pushCalls()
		if pushes[0].token != "alpha-scoped-1" || pushes[1].token != "beta-scoped-1" {
			t.Fatalf("pushes: %#v", pushes)
		}
	})

	t.Run("agents", func(t *testing.T) {
		fixture := newSeriesTokenFixture()
		server := newSeriesTokenServer(t, fixture)
		runSeriesTokenPush(t, a, seriesTestConfig(server.URL, "agent-a"), "collector", "builds", "1")
		runSeriesTokenPush(t, a, seriesTestConfig(server.URL, "agent-b"), "collector", "builds", "2")
		if got := fixture.mintCount("collector"); got != 2 {
			t.Fatalf("minted %d tokens, want 2", got)
		}
	})

	t.Run("base URLs", func(t *testing.T) {
		firstFixture := newSeriesTokenFixture()
		first := newSeriesTokenServer(t, firstFixture)
		secondFixture := newSeriesTokenFixture()
		second := newSeriesTokenServer(t, secondFixture)
		runSeriesTokenPush(t, a, seriesTestConfig(first.URL, "collector"), "collector", "builds", "1")
		runSeriesTokenPush(t, a, seriesTestConfig(second.URL, "collector"), "collector", "builds", "2")
		if firstFixture.mintCount("collector") != 1 || secondFixture.mintCount("collector") != 1 {
			t.Fatalf("mints: first=%#v second=%#v", firstFixture.mints, secondFixture.mints)
		}
		if got := secondFixture.pushCalls(); len(got) != 1 || got[0].token != "collector-scoped-1" {
			t.Fatalf("second workspace push: %#v", got)
		}
	})
}

func TestAdaptersRevokeClearsCachedSeriesToken(t *testing.T) {
	a := newTestApp(t)
	a.Stderr = &bytes.Buffer{}
	a.now = func() time.Time { return time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC) }
	fixture := newSeriesTokenFixture()
	server := newSeriesTokenServer(t, fixture)
	cfg := seriesTestConfig(server.URL, "collector")

	runSeriesTokenPush(t, a, cfg, "collector", "builds", "1")
	path, _, err := a.seriesTokenCachePath(cfg, "collector")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("cache missing before revoke: %v", err)
	}
	if _, _, err := a.runSeriesCommand(context.Background(), []string{"adapters", "revoke", "collector"}, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cache survived revoke: %v", err)
	}
	runSeriesTokenPush(t, a, cfg, "collector", "builds", "2")
	if got := fixture.mintCount("collector"); got != 2 {
		t.Fatalf("minted %d tokens, want 2", got)
	}
}
