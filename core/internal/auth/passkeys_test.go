package auth

import (
	"context"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"organization-autorunner-core/internal/storage"
)

func TestPasskeySessionStoreConsumeIsOneTimeAndExpires(t *testing.T) {
	t.Parallel()

	store := NewPasskeySessionStore(20 * time.Millisecond)
	defer store.Close()

	sessionID := store.Save(PasskeySession{
		Kind:        PasskeySessionKindRegistration,
		DisplayName: "Casey",
		UserHandle:  []byte("user-handle"),
	})
	if sessionID == "" {
		t.Fatal("expected session id")
	}

	session, ok := store.Consume(sessionID)
	if !ok {
		t.Fatal("expected session to be consumed")
	}
	if session.Kind != PasskeySessionKindRegistration {
		t.Fatalf("unexpected session kind: %q", session.Kind)
	}

	if _, ok := store.Consume(sessionID); ok {
		t.Fatal("expected session consume to be one-time")
	}

	expiredID := store.Save(PasskeySession{
		Kind:      PasskeySessionKindLoginDiscoverable,
		ExpiresAt: time.Now().UTC().Add(-time.Second),
	})
	if _, ok := store.Consume(expiredID); ok {
		t.Fatal("expected expired session to be rejected")
	}
}

func TestNormalizePasskeyDisplayName(t *testing.T) {
	t.Parallel()

	displayName, err := NormalizePasskeyDisplayName("  Casey Example  ")
	if err != nil {
		t.Fatalf("normalize display name: %v", err)
	}
	if displayName != "Casey Example" {
		t.Fatalf("unexpected normalized display name: %q", displayName)
	}

	if _, err := NormalizePasskeyDisplayName("   "); err == nil {
		t.Fatal("expected empty display name to be rejected")
	}

	if _, err := NormalizePasskeyDisplayName(strings.Repeat("a", 121)); err == nil {
		t.Fatal("expected overly long display name to be rejected")
	}
}

func TestGeneratePasskeyUsername(t *testing.T) {
	t.Parallel()

	username, err := generatePasskeyUsername("  Casey Example!  ")
	if err != nil {
		t.Fatalf("generate passkey username: %v", err)
	}
	if !strings.HasPrefix(username, "passkey.casey.example.") {
		t.Fatalf("unexpected username prefix: %q", username)
	}
	if strings.Contains(username, "_") {
		t.Fatalf("expected normalized username without underscores: %q", username)
	}
}

func TestRegisterPasskeyAgentWithDevSyntheticCredential(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	const bootstrapToken = "bootstrap-for-synthetic-test"
	store := NewStore(workspace.DB(), WithBootstrapToken(bootstrapToken))

	claim, err := store.ResolveOnboardingClaim(ctx, bootstrapToken, "", PrincipalKindHuman)
	if err != nil {
		t.Fatalf("resolve claim: %v", err)
	}
	cred, err := DevSyntheticPasskeyCredential()
	if err != nil {
		t.Fatalf("synthetic credential: %v", err)
	}
	userHandle := make([]byte, 32)
	if _, err := rand.Read(userHandle); err != nil {
		t.Fatalf("user handle: %v", err)
	}

	_, _, err = store.RegisterPasskeyAgent(ctx, RegisterPasskeyAgentInput{
		DisplayName: "Synthetic Test",
		UserHandle:  userHandle,
		Credential:  &cred,
	}, claim)
	if err != nil {
		t.Fatalf("RegisterPasskeyAgent: %v", err)
	}
}

func TestRegisterPasskeyAgentWithLinkedActorSyntheticCredential(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	const bootstrapToken = "bootstrap-for-linked-passkey-test"
	store := NewStore(
		workspace.DB(),
		WithBootstrapToken(bootstrapToken),
		WithAllowDevRegisterLinkedActor(true),
	)

	_, err = workspace.DB().ExecContext(ctx, `
		INSERT INTO actors(id, display_name, tags_json, created_at, metadata_json)
		VALUES (?, ?, ?, ?, ?)`,
		"actor-linked-human",
		"Linked Human",
		`["human","operator"]`,
		"2026-01-01T00:00:00.000Z",
		`{}`,
	)
	if err != nil {
		t.Fatalf("seed actor: %v", err)
	}

	claim, err := store.ResolveOnboardingClaim(ctx, bootstrapToken, "", PrincipalKindHuman)
	if err != nil {
		t.Fatalf("resolve claim: %v", err)
	}
	cred, err := DevSyntheticPasskeyCredential()
	if err != nil {
		t.Fatalf("synthetic credential: %v", err)
	}
	userHandle := make([]byte, 32)
	if _, err := rand.Read(userHandle); err != nil {
		t.Fatalf("user handle: %v", err)
	}

	agent, _, err := store.RegisterPasskeyAgent(ctx, RegisterPasskeyAgentInput{
		DisplayName:     "Linked Human",
		UserHandle:      userHandle,
		Credential:      &cred,
		ExistingActorID: "actor-linked-human",
	}, claim)
	if err != nil {
		t.Fatalf("RegisterPasskeyAgent: %v", err)
	}
	if agent.ActorID != "actor-linked-human" {
		t.Fatalf("expected linked actor id, got %q", agent.ActorID)
	}
	if agent.PrincipalKind == nil || *agent.PrincipalKind != "human" {
		t.Fatalf("expected human principal: %#v", agent)
	}
}
