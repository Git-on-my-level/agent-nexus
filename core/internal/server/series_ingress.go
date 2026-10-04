package server

import (
	"crypto/sha256"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/series"
)

// This cache remembers only which limiter owns a recognized credential. It is
// never an authorization cache: admitted requests still authenticate and read
// their current capability, and point writes re-read it in their transaction.
type seriesCredentialLimiter struct {
	adapter string
	expires time.Time
}

type seriesAdmissionState struct {
	minute   int64
	requests int
	active   int
}

type seriesIngress struct {
	mu          sync.Mutex
	credentials map[[32]byte]seriesCredentialLimiter
	adapters    map[string]*seriesAdmissionState
	workspace   seriesAdmissionState
	fresh       seriesAdmissionState
	clock       func() time.Time
}

func newSeriesIngress() *seriesIngress {
	return &seriesIngress{credentials: map[[32]byte]seriesCredentialLimiter{}, adapters: map[string]*seriesAdmissionState{}, clock: time.Now}
}

func (g *seriesIngress) lookup(token string, now time.Time) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	key := sha256.Sum256([]byte(token))
	entry := g.credentials[key]
	if !now.Before(entry.expires) {
		delete(g.credentials, key)
		return ""
	}
	return entry.adapter
}

func (g *seriesIngress) remember(token, adapter string, now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	const maxCredentials = 4096
	if len(g.credentials) >= maxCredentials {
		for key, entry := range g.credentials {
			if !now.Before(entry.expires) {
				delete(g.credentials, key)
			}
		}
	}
	if len(g.credentials) < maxCredentials {
		g.credentials[sha256.Sum256([]byte(token))] = seriesCredentialLimiter{adapter: adapter, expires: now.Add(10 * time.Minute)}
	}
}

// Reject, never queue, excess scoped requests. The lease covers authentication,
// body decoding and the point transaction, bounding pressure on human traffic.
func (g *seriesIngress) admit(adapter string, now time.Time) (release func(), retryAfter int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	minute := now.Unix() / 60
	if g.workspace.minute != minute {
		g.workspace.minute, g.workspace.requests = minute, 0
	}
	state := g.adapters[adapter]
	if adapter == "" {
		// Unrecognized scoped-token hints get a bounded authentication lane. They
		// share the four in-flight slots, but cannot consume a specific adapter's
		// budget until the database authenticates their real scope.
		state = &g.fresh
	} else if state == nil {
		for name, old := range g.adapters {
			if old.minute != minute && old.active == 0 {
				delete(g.adapters, name)
			}
		}
		if len(g.adapters) >= series.MaxSeries {
			return nil, 60
		}
		state = &seriesAdmissionState{}
		g.adapters[adapter] = state
	}
	if state.minute != minute {
		state.minute, state.requests = minute, 0
	}
	limit := series.MaxAdapterRequestsPerMinute
	if adapter == "" {
		limit = series.MaxRequestsPerMinute
	}
	if state.requests >= limit || g.workspace.requests >= series.MaxRequestsPerMinute {
		return nil, 60 - int(now.Unix()%60)
	}
	state.requests++
	if adapter != "" {
		g.workspace.requests++
	}
	if state.active >= series.MaxAdapterConcurrentRequests || g.workspace.active >= series.MaxConcurrentRequests {
		return nil, 1
	}
	state.active++
	g.workspace.active++
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			defer g.mu.Unlock()
			state.active--
			g.workspace.active--
		})
	}, 0
}

func requireSeriesAdmission(w http.ResponseWriter, g *seriesIngress, adapter string, now time.Time) (func(), bool) {
	release, retry := g.admit(adapter, now)
	if retry != 0 {
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		writeError(w, 429, "series_rate_limited", "series request safety budget exceeded; retry after the indicated delay")
		return nil, false
	}
	return release, true
}

func beginScopedSeriesRequest(w http.ResponseWriter, r *http.Request, opts handlerOptions, g *seriesIngress) (func(), bool) {
	if opts.authStore == nil {
		return nil, true
	}
	token, err := parseBearerToken(strings.TrimSpace(r.Header.Get("Authorization")))
	if err != nil {
		return nil, true
	}
	now := g.clock().UTC()
	var release func()
	var authenticationLease func()
	if adapter := g.lookup(token, now); adapter != "" {
		var allowed bool
		release, allowed = requireSeriesAdmission(w, g, adapter, now)
		if !allowed {
			return nil, false // No body read or database access on a rejected retry.
		}
	} else if strings.HasPrefix(token, auth.SeriesTokenPrefix) {
		var allowed bool
		authenticationLease, allowed = requireSeriesAdmission(w, g, "", now)
		if !allowed {
			return nil, false
		}
	}
	principal, err := opts.authStore.AuthenticateAccessToken(r.Context(), token)
	if authenticationLease != nil {
		authenticationLease()
	}
	if err != nil || principal.SeriesAdapter == "" {
		return release, true // Normal route authentication handles ordinary/invalid tokens.
	}
	g.remember(token, principal.SeriesAdapter, now)
	if release == nil {
		var allowed bool
		release, allowed = requireSeriesAdmission(w, g, principal.SeriesAdapter, now)
		if !allowed {
			return nil, false
		}
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	capability := auth.SeriesCapability{}
	if r.Method == http.MethodPost && len(parts) == 3 && parts[0] == "series" && parts[2] == "points" {
		capability = auth.SeriesCapability{Operation: auth.SeriesPointsPushOperation, Resource: parts[1]}
	}
	if err := opts.authStore.RequireSeriesCapability(r.Context(), principal, capability); err != nil {
		seriesError(w, err)
		release()
		return nil, false
	}
	cacheAuthenticatedPrincipal(r, &principal)
	return release, true
}
