package auth

import (
	"strings"
	"testing"
	"time"
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
