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

	"github.com/golang-jwt/jwt/v5"
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
	// Unknown kid refresh attempts are cooldown-limited even after a refresh
	// failure, and should preserve unavailable semantics during that cooldown.
	if _, err := resolver.Resolve(context.Background(), "kid-unknown-2"); !errors.Is(err, ErrExternalGrantUnavailable) {
		t.Fatalf("expected unavailable on unknown kid while refresh cooldown is active after failure, got %v", err)
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

func TestWorkspaceHumanGrantJWKResolverUnknownKidRefreshInFlightReturnsUnavailable(t *testing.T) {
	t.Parallel()

	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}

	var (
		hits             atomic.Int64
		blockRefreshCall atomic.Bool
		statusCode       atomic.Int64
	)
	statusCode.Store(http.StatusOK)
	refreshStarted := make(chan struct{}, 1)
	releaseRefresh := make(chan struct{})

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
		hit := hits.Add(1)
		if strings.TrimSpace(r.URL.Path) != "/.well-known/jwks.json" {
			http.NotFound(w, r)
			return
		}
		if blockRefreshCall.Load() && hit >= 2 {
			select {
			case refreshStarted <- struct{}{}:
			default:
			}
			<-releaseRefresh
		}
		if int(statusCode.Load()) != http.StatusOK {
			w.WriteHeader(int(statusCode.Load()))
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
		JWKSURL:                   jwksURL,
		UnknownKidRefreshCooldown: time.Minute,
	})
	if err != nil {
		t.Fatalf("new jwks resolver: %v", err)
	}

	if _, err := resolver.Resolve(context.Background(), "kid-primary"); err != nil {
		t.Fatalf("prime known kid: %v", err)
	}

	blockRefreshCall.Store(true)
	statusCode.Store(http.StatusServiceUnavailable)
	firstErrCh := make(chan error, 1)
	go func() {
		_, resolveErr := resolver.Resolve(context.Background(), "kid-missing-a")
		firstErrCh <- resolveErr
	}()

	select {
	case <-refreshStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for unknown-kid refresh attempt to start")
	}

	if _, err := resolver.Resolve(context.Background(), "kid-missing-b"); !errors.Is(err, ErrExternalGrantUnavailable) {
		t.Fatalf("expected in-flight unknown-kid request to return unavailable, got %v", err)
	}

	close(releaseRefresh)
	firstErr := <-firstErrCh
	if !errors.Is(firstErr, ErrExternalGrantUnavailable) {
		t.Fatalf("expected first unknown-kid request to return unavailable, got %v", firstErr)
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("expected second unknown-kid call to skip duplicate refresh while in-flight, got hits=%d", got)
	}
}

func TestWorkspaceManagedAgentGrantVerifierValidatesClaims(t *testing.T) {
	t.Parallel()

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}
	jwksServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSONForExternalGrantTest(w, map[string]any{
			"keys": []map[string]any{{
				"kty": "OKP",
				"crv": "Ed25519",
				"kid": "kid-managed",
				"x":   base64.RawURLEncoding.EncodeToString(publicKey),
				"use": "sig",
				"alg": "EdDSA",
			}},
		})
	}))
	t.Cleanup(jwksServer.Close)

	jwksURL, err := WorkspaceHumanGrantJWKSURL(jwksServer.URL)
	if err != nil {
		t.Fatalf("derive jwks url: %v", err)
	}
	resolver, err := NewWorkspaceHumanGrantJWKResolver(WorkspaceHumanGrantJWKResolverConfig{JWKSURL: jwksURL})
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	verifier, err := NewWorkspaceManagedAgentGrantVerifier(WorkspaceManagedAgentGrantVerifierConfig{
		Issuer:      jwksServer.URL,
		Audience:    "anx-core",
		WorkspaceID: "ws_unit",
		Resolver:    resolver,
	})
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	sign := func(mutator func(*WorkspaceManagedAgentGrantClaims)) string {
		now := time.Now().UTC()
		claims := WorkspaceManagedAgentGrantClaims{
			WorkspaceID:          "ws_unit",
			OrganizationID:       "org_unit",
			SlotID:               "slot_unit",
			SlotName:             "Unit Agent",
			Provider:             "provider-alpha",
			ProviderConnectionID: "conn_unit",
			OwnerAccountID:       "acct_unit",
			ExternalSubject:      "provider-subject",
			Scope:                "workspace:ws_unit",
			GrantType:            GrantTypeWorkspaceManagedAgent,
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    jwksServer.URL,
				Subject:   "managed-subject",
				Audience:  jwt.ClaimStrings{"anx-core"},
				ID:        "jti-unit-" + time.Now().UTC().Format(time.RFC3339Nano),
				IssuedAt:  jwt.NewNumericDate(now),
				NotBefore: jwt.NewNumericDate(now.Add(-time.Minute)),
				ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute)),
			},
		}
		if mutator != nil {
			mutator(&claims)
		}
		token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
		token.Header["kid"] = "kid-managed"
		signed, err := token.SignedString(privateKey)
		if err != nil {
			t.Fatalf("sign managed grant: %v", err)
		}
		return signed
	}

	identity, err := verifier.Verify(context.Background(), sign(nil))
	if err != nil {
		t.Fatalf("verify valid managed grant: %v", err)
	}
	if identity.GrantType != GrantTypeWorkspaceManagedAgent || identity.SlotID != "slot_unit" || identity.Provider != "provider-alpha" {
		t.Fatalf("unexpected managed grant identity: %#v", identity)
	}

	cases := []struct {
		name    string
		mutator func(*WorkspaceManagedAgentGrantClaims)
	}{
		{name: "invalid issuer", mutator: func(c *WorkspaceManagedAgentGrantClaims) { c.Issuer = "https://issuer.invalid" }},
		{name: "invalid audience", mutator: func(c *WorkspaceManagedAgentGrantClaims) { c.Audience = jwt.ClaimStrings{"wrong"} }},
		{name: "invalid workspace", mutator: func(c *WorkspaceManagedAgentGrantClaims) { c.WorkspaceID = "ws_other"; c.Scope = "workspace:ws_other" }},
		{name: "invalid scope", mutator: func(c *WorkspaceManagedAgentGrantClaims) { c.Scope = "workspace:other" }},
		{name: "invalid grant type", mutator: func(c *WorkspaceManagedAgentGrantClaims) { c.GrantType = GrantTypeWorkspaceHuman }},
		{name: "expired", mutator: func(c *WorkspaceManagedAgentGrantClaims) {
			c.ExpiresAt = jwt.NewNumericDate(time.Now().UTC().Add(-10 * time.Minute))
		}},
		{name: "missing organization", mutator: func(c *WorkspaceManagedAgentGrantClaims) { c.OrganizationID = "" }},
		{name: "missing slot", mutator: func(c *WorkspaceManagedAgentGrantClaims) { c.SlotID = "" }},
		{name: "missing provider", mutator: func(c *WorkspaceManagedAgentGrantClaims) { c.Provider = "" }},
		{name: "missing jti", mutator: func(c *WorkspaceManagedAgentGrantClaims) { c.ID = "" }},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := verifier.Verify(context.Background(), sign(tt.mutator)); !errors.Is(err, ErrExternalGrantInvalid) {
				t.Fatalf("expected ErrExternalGrantInvalid, got %v", err)
			}
		})
	}
}

func writeJSONForExternalGrantTest(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		panic(err)
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
