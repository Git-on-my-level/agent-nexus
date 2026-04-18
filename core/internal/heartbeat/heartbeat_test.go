package heartbeat

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestPublisherPublishOnceSignsExpectedClaims(t *testing.T) {
	t.Parallel()

	identity := generateIdentity(t, "svc_ws_test")
	var gotAuthHeader string
	var gotSnapshot Snapshot

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthHeader = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotSnapshot); err != nil {
			t.Fatalf("decode snapshot: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	now := time.Date(2026, 4, 19, 8, 0, 0, 0, time.UTC)
	publisher := &Publisher{
		URL:         server.URL,
		Audience:    "anx-control-plane",
		WorkspaceID: "ws_test",
		Identity:    identity,
		Snapshot: func(context.Context) Snapshot {
			return Snapshot{
				Version: "hosted-instance/v1",
				Build:   "build-123",
				HealthSummary: map[string]any{
					"ok": true,
				},
				ProjectionMaintenanceSummary: map[string]any{
					"mode": "background",
				},
				UsageSummary: map[string]any{
					"usage": map[string]any{"artifact_count": 3},
				},
			}
		},
		Now: func() time.Time { return now },
	}

	if err := publisher.publishOnce(context.Background()); err != nil {
		t.Fatalf("publish once: %v", err)
	}

	if gotSnapshot.Version != "hosted-instance/v1" {
		t.Fatalf("expected version hosted-instance/v1, got %q", gotSnapshot.Version)
	}
	if gotSnapshot.Build != "build-123" {
		t.Fatalf("expected build build-123, got %q", gotSnapshot.Build)
	}
	if gotAuthHeader == "" {
		t.Fatal("expected authorization header")
	}

	const bearerPrefix = "Bearer "
	if len(gotAuthHeader) <= len(bearerPrefix) || gotAuthHeader[:len(bearerPrefix)] != bearerPrefix {
		t.Fatalf("expected bearer authorization header, got %q", gotAuthHeader)
	}
	tokenString := gotAuthHeader[len(bearerPrefix):]

	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		return identity.PrivateKey.Public().(ed25519.PublicKey), nil
	}, jwt.WithoutClaimsValidation())
	if err != nil {
		t.Fatalf("parse assertion: %v", err)
	}
	if token == nil || !token.Valid {
		t.Fatal("expected valid signed assertion")
	}

	if got := claims["iss"]; got != identity.ID {
		t.Fatalf("expected iss %q, got %#v", identity.ID, got)
	}
	if got := claims["sub"]; got != identity.ID {
		t.Fatalf("expected sub %q, got %#v", identity.ID, got)
	}
	if got := claims["aud"]; got != "anx-control-plane" {
		t.Fatalf("expected aud anx-control-plane, got %#v", got)
	}
	if got := claims["workspace_id"]; got != "ws_test" {
		t.Fatalf("expected workspace_id ws_test, got %#v", got)
	}
	if got := claims["purpose"]; got != "heartbeat" {
		t.Fatalf("expected purpose heartbeat, got %#v", got)
	}

	iat := int64(claims["iat"].(float64))
	exp := int64(claims["exp"].(float64))
	if got := exp - iat; got != int64(defaultAssertionTTL.Seconds()) {
		t.Fatalf("expected assertion ttl %d seconds, got %d", int64(defaultAssertionTTL.Seconds()), got)
	}
}

func TestPublisherRetriesOnServerErrors(t *testing.T) {
	t.Parallel()

	identity := generateIdentity(t, "svc_ws_retry")
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r
		n := atomic.AddInt32(&requestCount, 1)
		if n < 3 {
			http.Error(w, "temporary failure", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	publisher := &Publisher{
		URL:         server.URL,
		WorkspaceID: "ws_retry",
		Identity:    identity,
		Snapshot: func(context.Context) Snapshot {
			return Snapshot{Version: "v1", Build: "b1"}
		},
		RetryBackoff: 5 * time.Millisecond,
		MaxAttempts:  3,
	}

	if err := publisher.publishOnce(context.Background()); err != nil {
		t.Fatalf("publish once: %v", err)
	}
	if got := atomic.LoadInt32(&requestCount); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}
}

func TestPublisherRunNoopWhenContextCancelled(t *testing.T) {
	t.Parallel()

	identity := generateIdentity(t, "svc_ws_cancel")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var snapshotCalls int32
	publisher := &Publisher{
		URL:         "http://127.0.0.1:0",
		WorkspaceID: "ws_cancel",
		Identity:    identity,
		Snapshot: func(context.Context) Snapshot {
			atomic.AddInt32(&snapshotCalls, 1)
			return Snapshot{Version: "v1", Build: "b1"}
		},
	}

	done := make(chan struct{})
	go func() {
		publisher.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected Run to return immediately for cancelled context")
	}
	if got := atomic.LoadInt32(&snapshotCalls); got != 0 {
		t.Fatalf("expected no snapshot calls, got %d", got)
	}
}

func generateIdentity(t *testing.T, id string) Identity {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}
	return Identity{
		ID:         id,
		PrivateKey: privateKey,
	}
}
