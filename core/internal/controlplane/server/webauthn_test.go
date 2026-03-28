package server

import (
	"net/http/httptest"
	"testing"
)

func TestControlPlaneWebAuthnConfigResolveForRequestAllowsConfiguredAllowedOrigin(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("POST", "http://127.0.0.1:8100/account/passkeys/registrations/start", nil)
	req.Header.Set("Origin", "https://m2-internal.scalingforever.com")

	rpID, origin, err := (WebAuthnConfig{
		RPID:           "scalingforever.com",
		RPOrigin:       "https://ignored.example.test",
		AllowedOrigins: []string{"https://host.tail76ea03.ts.net", "https://m2-internal.scalingforever.com"},
	}).resolveForRequest(req)
	if err != nil {
		t.Fatalf("resolve WebAuthn config: %v", err)
	}
	if rpID != "scalingforever.com" {
		t.Fatalf("expected configured RP ID, got %q", rpID)
	}
	if origin != "https://m2-internal.scalingforever.com" {
		t.Fatalf("expected request origin, got %q", origin)
	}
}

func TestControlPlaneWebAuthnConfigResolveForRequestRejectsOriginOutsideAllowedOrigins(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("POST", "http://127.0.0.1:8100/account/passkeys/registrations/start", nil)
	req.Header.Set("Origin", "https://untrusted.example.test")

	_, _, err := (WebAuthnConfig{
		AllowedOrigins: []string{"https://m2-internal.scalingforever.com"},
	}).resolveForRequest(req)
	if err == nil {
		t.Fatal("expected allowlist mismatch error")
	}
	if got := err.Error(); got != `browser origin "https://untrusted.example.test" is not in configured WebAuthn allowed origins` {
		t.Fatalf("unexpected error: %q", got)
	}
}
