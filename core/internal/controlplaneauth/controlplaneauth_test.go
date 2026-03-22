package controlplaneauth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"
)

func TestWorkspaceHumanGrantSignAndVerify(t *testing.T) {
	t.Parallel()

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Date(2026, time.March, 21, 10, 0, 0, 0, time.UTC)
	signer, err := NewWorkspaceHumanGrantSigner(WorkspaceHumanGrantSignerConfig{
		Issuer:     "https://control.example.test",
		Audience:   "oar-core",
		PrivateKey: privateKey,
		Now:        func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	verifier, err := NewWorkspaceHumanVerifier(WorkspaceHumanVerifierConfig{
		Issuer:      "https://control.example.test",
		Audience:    "oar-core",
		WorkspaceID: "ws_123",
		PublicKey:   publicKey,
		Now:         func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	token, expiresAt, err := signer.Sign(WorkspaceHumanGrantInput{
		AccountID:      "acct_123",
		WorkspaceID:    "ws_123",
		OrganizationID: "org_123",
		Email:          "person@example.com",
		DisplayName:    "Person Example",
		LaunchID:       "launch_123",
		TTL:            10 * time.Minute,
	})
	if err != nil {
		t.Fatalf("sign grant: %v", err)
	}

	identity, err := verifier.Verify(token)
	if err != nil {
		t.Fatalf("verify grant: %v", err)
	}
	if identity.Subject != "acct_123" {
		t.Fatalf("expected subject acct_123, got %q", identity.Subject)
	}
	if identity.WorkspaceID != "ws_123" {
		t.Fatalf("expected workspace ws_123, got %q", identity.WorkspaceID)
	}
	if identity.Email != "person@example.com" {
		t.Fatalf("expected email person@example.com, got %q", identity.Email)
	}
	if identity.ExpiresAt != expiresAt {
		t.Fatalf("expected expires_at %q, got %q", expiresAt, identity.ExpiresAt)
	}
}

func TestWorkspaceHumanVerifierRejectsWrongWorkspace(t *testing.T) {
	t.Parallel()

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Date(2026, time.March, 21, 10, 0, 0, 0, time.UTC)
	signer, err := NewWorkspaceHumanGrantSigner(WorkspaceHumanGrantSignerConfig{
		Issuer:     "https://control.example.test",
		Audience:   "oar-core",
		PrivateKey: privateKey,
		Now:        func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	verifier, err := NewWorkspaceHumanVerifier(WorkspaceHumanVerifierConfig{
		Issuer:      "https://control.example.test",
		Audience:    "oar-core",
		WorkspaceID: "ws_other",
		PublicKey:   publicKey,
		Now:         func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	token, _, err := signer.Sign(WorkspaceHumanGrantInput{
		AccountID:   "acct_123",
		WorkspaceID: "ws_123",
		TTL:         10 * time.Minute,
	})
	if err != nil {
		t.Fatalf("sign grant: %v", err)
	}

	if _, err := verifier.Verify(token); err == nil {
		t.Fatal("expected verifier to reject wrong workspace")
	}
}

func TestWorkspaceServiceIdentityAssertionAndPublicKeyExport(t *testing.T) {
	t.Parallel()

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	identity, err := NewWorkspaceServiceIdentity(WorkspaceServiceIdentityConfig{
		ID:         "svc_workspace_123",
		PrivateKey: privateKey,
		Now:        func() time.Time { return time.Date(2026, time.March, 21, 10, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("new service identity: %v", err)
	}

	token, _, err := identity.SignClientAssertion("https://control.example.test", time.Minute, map[string]any{
		"workspace_id": "ws_123",
	})
	if err != nil {
		t.Fatalf("sign client assertion: %v", err)
	}
	if token == "" {
		t.Fatal("expected signed client assertion")
	}

	exported := identity.PublicKeyBase64()
	if exported == "" {
		t.Fatal("expected exported public key")
	}
	if exported != base64.StdEncoding.EncodeToString(publicKey) {
		t.Fatalf("unexpected exported public key: got %q", exported)
	}
}
