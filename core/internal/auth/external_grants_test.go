package auth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkspaceHumanGrantJWKResolverFailureModes(t *testing.T) {
	t.Parallel()

	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}

	var (
		nowMu      sync.Mutex
		nowValue   = time.Date(2026, 4, 16, 3, 0, 0, 0, time.UTC)
		statusCode = http.StatusOK
		hits       atomic.Int64
	)
	jwksPayload := map[string]any{
		"keys": []map[string]any{{
			"kty": "OKP",
			"crv": "Ed25519",
			"kid": "kid-1",
			"x":   base64.RawURLEncoding.EncodeToString(publicKey),
			"use": "sig",
			"alg": "EdDSA",
		}},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if strings.TrimSpace(r.URL.Path) != "/.well-known/jwks.json" {
			http.NotFound(w, r)
			return
		}
		if statusCode != http.StatusOK {
			w.WriteHeader(statusCode)
			return
		}
		writeJSONJWKSTest(w, http.StatusOK, jwksPayload)
	}))
	defer server.Close()

	jwksURL, err := WorkspaceHumanGrantJWKSURL(server.URL)
	if err != nil {
		t.Fatalf("derive jwks url: %v", err)
	}
	resolver, err := NewWorkspaceHumanGrantJWKResolver(WorkspaceHumanGrantJWKResolverConfig{
		JWKSURL: jwksURL,
		Now: func() time.Time {
			nowMu.Lock()
			defer nowMu.Unlock()
			return nowValue
		},
	})
	if err != nil {
		t.Fatalf("new jwks resolver: %v", err)
	}

	statusCode = http.StatusServiceUnavailable
	if _, err := resolver.Resolve(context.Background(), "kid-1"); !errors.Is(err, ErrExternalGrantUnavailable) {
		t.Fatalf("expected unavailable on cold cache fetch failure, got %v", err)
	}

	statusCode = http.StatusOK
	if _, err := resolver.Resolve(context.Background(), "kid-1"); err != nil {
		t.Fatalf("prime cache with known kid: %v", err)
	}

	nowMu.Lock()
	nowValue = nowValue.Add(10 * time.Minute)
	nowMu.Unlock()
	statusCode = http.StatusServiceUnavailable
	if _, err := resolver.Resolve(context.Background(), "kid-unknown"); !errors.Is(err, ErrExternalGrantUnavailable) {
		t.Fatalf("expected unavailable on unknown kid when warm-cache refresh fails, got %v", err)
	}
	// Unknown kid refresh attempts are cooldown-limited even after a refresh failure.
	if _, err := resolver.Resolve(context.Background(), "kid-unknown-2"); !errors.Is(err, ErrExternalGrantInvalid) {
		t.Fatalf("expected invalid on unknown kid while refresh cooldown is active, got %v", err)
	}
	if _, err := resolver.Resolve(context.Background(), "kid-1"); err != nil {
		t.Fatalf("expected warm cache key to remain usable: %v", err)
	}

	nowMu.Lock()
	nowValue = nowValue.Add(time.Hour + time.Minute)
	nowMu.Unlock()
	if _, err := resolver.Resolve(context.Background(), "kid-1"); err != nil {
		t.Fatalf("expected stale cache key to remain usable on refresh failure: %v", err)
	}

	nowMu.Lock()
	nowValue = nowValue.Add(time.Hour + time.Minute)
	nowMu.Unlock()
	if _, err := resolver.Resolve(context.Background(), "kid-1"); !errors.Is(err, ErrExternalGrantUnavailable) {
		t.Fatalf("expected unavailable after cache expiration and refresh failure, got %v", err)
	}
}

func TestWorkspaceHumanGrantJWKResolverUnknownKidRefreshCooldown(t *testing.T) {
	t.Parallel()

	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}

	var (
		nowMu    sync.Mutex
		nowValue = time.Date(2026, 4, 16, 4, 0, 0, 0, time.UTC)
		hits     atomic.Int64
	)
	jwksPayload := map[string]any{
		"keys": []map[string]any{{
			"kty": "OKP",
			"crv": "Ed25519",
			"kid": "kid-primary",
			"x":   base64.RawURLEncoding.EncodeToString(publicKey),
			"use": "sig",
			"alg": "EdDSA",
		}},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if strings.TrimSpace(r.URL.Path) != "/.well-known/jwks.json" {
			http.NotFound(w, r)
			return
		}
		writeJSONJWKSTest(w, http.StatusOK, jwksPayload)
	}))
	defer server.Close()

	jwksURL, err := WorkspaceHumanGrantJWKSURL(server.URL)
	if err != nil {
		t.Fatalf("derive jwks url: %v", err)
	}
	resolver, err := NewWorkspaceHumanGrantJWKResolver(WorkspaceHumanGrantJWKResolverConfig{
		JWKSURL: jwksURL,
		Now: func() time.Time {
			nowMu.Lock()
			defer nowMu.Unlock()
			return nowValue
		},
		UnknownKidRefreshCooldown: time.Minute,
	})
	if err != nil {
		t.Fatalf("new jwks resolver: %v", err)
	}

	if _, err := resolver.Resolve(context.Background(), "kid-primary"); err != nil {
		t.Fatalf("prime known kid: %v", err)
	}

	before := hits.Load()
	if _, err := resolver.Resolve(context.Background(), "kid-missing-a"); !errors.Is(err, ErrExternalGrantInvalid) {
		t.Fatalf("expected invalid for unknown kid: %v", err)
	}
	afterFirst := hits.Load()
	if afterFirst <= before {
		t.Fatalf("expected unknown kid to trigger one refresh, before=%d after=%d", before, afterFirst)
	}

	if _, err := resolver.Resolve(context.Background(), "kid-missing-b"); !errors.Is(err, ErrExternalGrantInvalid) {
		t.Fatalf("expected invalid for unknown kid in cooldown window: %v", err)
	}
	afterSecond := hits.Load()
	if afterSecond != afterFirst {
		t.Fatalf("expected unknown kid refresh to be cooldown-limited, afterFirst=%d afterSecond=%d", afterFirst, afterSecond)
	}

	nowMu.Lock()
	nowValue = nowValue.Add(time.Minute + time.Second)
	nowMu.Unlock()
	if _, err := resolver.Resolve(context.Background(), "kid-missing-c"); !errors.Is(err, ErrExternalGrantInvalid) {
		t.Fatalf("expected invalid for unknown kid after cooldown: %v", err)
	}
	afterThird := hits.Load()
	if afterThird <= afterSecond {
		t.Fatalf("expected unknown kid refresh after cooldown, afterSecond=%d afterThird=%d", afterSecond, afterThird)
	}
}

func writeJSONJWKSTest(w http.ResponseWriter, status int, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(payload)
}
