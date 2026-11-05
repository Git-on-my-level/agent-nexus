package auth

import (
	"context"
	"testing"
	"time"

	"agent-nexus-core/internal/storage"
)

// The in-transaction re-check sees activity that appeared after the sweep
// page was read: a bridge job, fresh presence or a restore.
func TestArchiveInactivePersonaRechecksActivityInTransaction(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db := ws.DB()
	now := time.Now().UTC()
	old := now.Add(-40 * 24 * time.Hour).Format(time.RFC3339Nano)
	fresh := now.Format(time.RFC3339Nano)
	cutoff := now.Add(-30 * 24 * time.Hour).Format(time.RFC3339Nano)
	for _, q := range []string{
		`INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at) VALUES('h','h','h','u','m','[]','` + old + `')`,
		`INSERT INTO actors VALUES('p','p','["agent"]','` + old + `','{}')`,
		`INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) VALUES('p','p.h','p','` + old + `','` + old + `','{}')`,
		`INSERT INTO host_agents(host_id,name,agent_id,identity_kind) VALUES('h','p','p','derived')`,
		`INSERT INTO auth_access_tokens(id,agent_id,token_hash,created_at,expires_at) VALUES('t','p','x','` + old + `','` + old + `')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	store := NewStore(db)
	candidate := personaSweepCandidate{hostID: "h", name: "p", agentID: "p", actorID: "p"}
	for _, step := range []struct{ name, apply, undo string }{
		{"bridge job", `INSERT INTO host_bridge_jobs VALUES('h','p','wake',1,1)`, `DELETE FROM host_bridge_jobs`},
		{"presence", `INSERT INTO agent_presence(agent_id,observed_at) VALUES('p','` + fresh + `')`, `DELETE FROM agent_presence`},
		{"restore", `UPDATE host_agents SET restored_at='` + fresh + `'`, `UPDATE host_agents SET restored_at=NULL`},
	} {
		if _, err := db.Exec(step.apply); err != nil {
			t.Fatal(step.name, err)
		}
		archived, err := store.archiveInactivePersona(ctx, candidate, cutoff)
		if err != nil || archived {
			t.Fatalf("%s: archived=%v err=%v", step.name, archived, err)
		}
		if _, err := db.Exec(step.undo); err != nil {
			t.Fatal(step.name, err)
		}
	}
	if archived, err := store.archiveInactivePersona(ctx, candidate, cutoff); err != nil || !archived {
		t.Fatalf("idle persona: archived=%v err=%v", archived, err)
	}
}
