package storage_test

import (
	"context"
	"testing"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
)

func TestSessionMigrationUpgradeAndRestart(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	ws, err := storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	// Reconstruct a v41 workspace, preserving a preexisting agent and task.
	for _, sql := range []string{
		`DROP TABLE work_participants`, `DROP TABLE agent_sessions`, `DELETE FROM schema_migrations WHERE version=42`,
		`INSERT INTO actors(id,display_name,tags_json,created_at,metadata_json) VALUES('actor-session','Agent','["agent"]','2026-01-01T00:00:00Z','{}')`,
		`INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) VALUES('agent-session','session','actor-session','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','{}')`,
	} {
		if _, err = ws.DB().ExecContext(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	store := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	work, err := store.CreateWork(ctx, "actor-session", "", map[string]any{"title": "Pre-upgrade work"})
	if err != nil {
		t.Fatal(err)
	}
	if err = ws.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err = storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	store = primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	seq := int64(0)
	request := primitives.SessionRegistration{Provider: "custom", HostScope: "local", NativeSessionID: "opaque-native", Capabilities: primitives.SessionCapabilities{Resume: "unsupported", History: "unknown", Logs: "unsupported"}, Activity: "active", Sequence: &seq}
	session, err := store.UpsertSession(ctx, "agent-session", "actor-session", request)
	if err != nil {
		t.Fatal(err)
	}
	participant, err := store.UpsertWorkParticipant(ctx, "agent-session", work["id"].(string), primitives.WorkParticipantRegistration{SessionID: session.SessionID, Activity: "active", Sequence: &seq})
	if err != nil {
		t.Fatal(err)
	}
	if err = ws.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err = storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	store = primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	replay, err := store.UpsertSession(ctx, "agent-session", "actor-session", request)
	if err != nil {
		t.Fatal(err)
	}
	if replay != session {
		t.Fatalf("session changed across restart: %#v %#v", session, replay)
	}
	participants, err := store.ListWorkParticipants(ctx, "agent-session", work["id"].(string), 50, "")
	if err != nil || len(participants.Participants) != 1 || participants.Participants[0] != participant {
		t.Fatalf("participants lost: %#v %v", participants, err)
	}
	got, err := store.GetWork(ctx, work["id"].(string))
	if err != nil || got["title"] != "Pre-upgrade work" {
		t.Fatalf("pre-upgrade work lost: %#v %v", got, err)
	}
	var versions int
	if err = ws.DB().QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=42`).Scan(&versions); err != nil || versions != 1 {
		t.Fatalf("migration applied %d times: %v", versions, err)
	}
}
