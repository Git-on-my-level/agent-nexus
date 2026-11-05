package auth_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/storage"
)

// Auto-unarchive on host assertion runs only after the exclusion and
// revocation guards, and a human archive of a sweep-archived persona makes it
// manual so the name stays reserved.
func TestHostAssertionReuseOfArchivedNames(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db := ws.DB()
	now := time.Now().UTC()
	day := 24 * time.Hour
	seedPersonas(t, db, now, []personaSeed{
		{host: "host-a", name: "excluded-one", kind: "derived", tokenAge: 40 * day},
		{host: "host-a", name: "revoked-one", kind: "derived", tokenAge: 40 * day},
		{host: "host-a", name: "promoted-one", kind: "derived", tokenAge: 40 * day},
		{host: "host-a", name: "plain-one", kind: "derived", tokenAge: 40 * day},
	})
	seedHuman(t, db)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO host_keys(id,host_id,public_key,created_at) VALUES('key-a','host-a','` + base64.StdEncoding.EncodeToString(pub) + `','2026-01-01T00:00:00Z')`,
		`UPDATE host_agents SET archived_at='2026-01-01T00:00:00Z',archived_reason='inactive' WHERE host_id='host-a'`,
		`INSERT INTO host_exclusions(host_id,name) VALUES('host-a','excluded-one')`,
		`UPDATE agents SET revoked_at='2026-01-01T00:00:00Z' WHERE id='host-a-revoked-one'`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	store := auth.NewStore(db)
	grant := func(name string) error {
		signedAt := time.Now().UTC().Format(time.RFC3339)
		msg := "anx-host-agent-token|host-a|key-a|" + name + "|" + signedAt
		_, _, err := store.IssueHostAgentToken(ctx, "host-a", "key-a", name, signedAt, base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(msg))))
		return err
	}
	stillArchived := func(name, reason string) {
		t.Helper()
		if got := archivedReasons(t, db)["host-a/"+name]; got != reason {
			t.Fatalf("%s archive reason = %q, want %q", name, got, reason)
		}
	}

	if err := grant("excluded-one"); !errors.Is(err, auth.ErrAgentExcluded) {
		t.Fatalf("excluded inactive name: %v", err)
	}
	stillArchived("excluded-one", "inactive")
	if err := grant("revoked-one"); !errors.Is(err, auth.ErrAgentRevoked) {
		t.Fatalf("revoked inactive name: %v", err)
	}
	stillArchived("revoked-one", "inactive")

	human := auth.Principal{AgentID: "human", ActorID: "human", PrincipalKind: "human"}
	if _, err := store.ArchiveHostAgent(ctx, "host-a", "promoted-one", human); err != nil {
		t.Fatal(err)
	}
	stillArchived("promoted-one", "manual")
	var promoted int
	if err := db.QueryRow(`SELECT COUNT(*) FROM auth_audit_events WHERE event_type='host_agent_archived' AND actor_agent_id='human' AND json_extract(metadata_json,'$.reason')='manual' AND json_extract(metadata_json,'$.previous_reason')='inactive'`).Scan(&promoted); err != nil || promoted != 1 {
		t.Fatalf("promotion audit: %d %v", promoted, err)
	}
	if err := grant("promoted-one"); !errors.Is(err, auth.ErrAgentArchived) {
		t.Fatalf("human-archived name minted: %v", err)
	}
	stillArchived("promoted-one", "manual")

	if err := grant("plain-one"); err != nil {
		t.Fatalf("inactive name reuse: %v", err)
	}
	stillArchived("plain-one", "")
}
