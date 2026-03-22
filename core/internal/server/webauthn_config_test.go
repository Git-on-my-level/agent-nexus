package server

import (
	"net/http/httptest"
	"testing"
)

func TestWebAuthnConfigBuildForRequestDerivesOriginAndRPIDFromBrowserOrigin(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("POST", "http://127.0.0.1:8000/auth/passkey/register/options", nil)
	req.Header.Set("Origin", "http://localhost:5173")

	webAuthn, err := (WebAuthnConfig{RPDisplayName: "OAR"}).buildForRequest(req)
	if err != nil {
		t.Fatalf("build WebAuthn config: %v", err)
	}
	if got := webAuthn.Config.RPID; got != "localhost" {
		t.Fatalf("expected localhost RP ID, got %q", got)
	}
	if got := webAuthn.Config.RPOrigins; len(got) != 1 || got[0] != "http://localhost:5173" {
		t.Fatalf("unexpected RP origins: %#v", got)
	}
}

func TestWebAuthnConfigBuildForRequestUsesForwardedHostWhenOriginMissing(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("POST", "http://127.0.0.1:8000/auth/passkey/login/options", nil)
	req.RemoteAddr = "127.0.0.1:9000"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "login.example.test")

	webAuthn, err := (WebAuthnConfig{RPDisplayName: "OAR"}).buildForRequest(req)
	if err != nil {
		t.Fatalf("build WebAuthn config: %v", err)
	}
	if got := webAuthn.Config.RPID; got != "login.example.test" {
		t.Fatalf("expected forwarded host RP ID, got %q", got)
	}
	if got := webAuthn.Config.RPOrigins; len(got) != 1 || got[0] != "https://login.example.test" {
		t.Fatalf("unexpected RP origins: %#v", got)
	}
}

func TestWebAuthnConfigBuildForRequestIgnoresForwardedHostFromUntrustedPeer(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("POST", "http://127.0.0.1:8000/auth/passkey/login/options", nil)
	req.RemoteAddr = "198.51.100.20:9000"
	req.Host = "workspace.example.test"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "login.example.test")

	webAuthn, err := (WebAuthnConfig{RPDisplayName: "OAR"}).buildForRequest(req)
	if err != nil {
		t.Fatalf("build WebAuthn config: %v", err)
	}
	if got := webAuthn.Config.RPID; got != "workspace.example.test" {
		t.Fatalf("expected direct host RP ID, got %q", got)
	}
	if got := webAuthn.Config.RPOrigins; len(got) != 1 || got[0] != "http://workspace.example.test" {
		t.Fatalf("unexpected RP origins: %#v", got)
	}
}

func TestWebAuthnConfigBuildForRequestRejectsConfiguredOriginMismatch(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("POST", "http://127.0.0.1:8000/auth/passkey/register/options", nil)
	req.Header.Set("Origin", "http://localhost:5173")

	_, err := (WebAuthnConfig{
		RPDisplayName: "OAR",
		RPOrigin:      "http://127.0.0.1:5173",
		RPID:          "127.0.0.1",
	}).buildForRequest(req)
	if err == nil {
		t.Fatal("expected mismatch error")
	}
	if got := err.Error(); got != `configured WebAuthn origin "http://127.0.0.1:5173" does not match browser origin "http://localhost:5173"` {
		t.Fatalf("unexpected error: %q", got)
	}
}

func TestValidateRPIDAgainstHost(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		rpID    string
		host    string
		wantErr bool
	}{
		{name: "exact localhost", rpID: "localhost", host: "localhost"},
		{name: "exact ip", rpID: "127.0.0.1", host: "127.0.0.1"},
		{name: "domain suffix", rpID: "example.com", host: "app.example.com"},
		{name: "localhost mismatch", rpID: "127.0.0.1", host: "localhost", wantErr: true},
		{name: "ip mismatch", rpID: "127.0.0.1", host: "10.0.0.10", wantErr: true},
		{name: "unrelated domain", rpID: "example.com", host: "example.org", wantErr: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateRPIDAgainstHost(tc.rpID, tc.host)
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
