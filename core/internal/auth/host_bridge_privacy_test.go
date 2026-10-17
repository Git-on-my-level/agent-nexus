package auth_test

import (
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
	"context"
	"testing"
)

func TestHostBridgeStatusRespectsHostAndAgentProfileVisibility(t *testing.T) {
	ctx := context.Background()
	ws, e := storage.InitializeWorkspace(ctx, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer ws.Close()
	store := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	card, e := store.CreateWork(ctx, "owner", "", map[string]any{"title": "Private"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = store.PatchThread(ctx, "owner", card["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); e != nil {
		t.Fatal(e)
	}
	for _, q := range []string{`INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at) VALUES('host','machine','Host','user','machine','[]','now')`, `INSERT INTO actors VALUES('agent-actor','Agent','[]','now','{}')`, `INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) VALUES('agent','agent','agent-actor','now','now','{}')`, `INSERT INTO host_agents VALUES('host','agent','agent','derived')`, `INSERT INTO host_bridge_presence VALUES('host','instance',1,1,'test','now','now','2999-01-01T00:00:00Z')`, `INSERT INTO host_bridge_jobs VALUES('host','agent-actor','answers',1,1)`} {
		if _, e = ws.DB().Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	s := auth.NewStore(ws.DB())
	reader := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "stranger"})
	out, e := s.HostBridgeStatus(reader, []string{"host"})
	if e != nil || len(out) != 1 || len(out[0].Agents) != 1 {
		t.Fatal(out, e)
	}
	if _, e = ws.DB().Exec(`UPDATE hosts SET display_name=? WHERE id='host'`, card["ref"]); e != nil {
		t.Fatal(e)
	}
	out, e = s.HostBridgeStatus(reader, []string{"host"})
	if e != nil || len(out) != 0 {
		t.Fatal("private host disclosed", out, e)
	}
	if _, e = ws.DB().Exec(`UPDATE hosts SET display_name='Host' WHERE id='host'; UPDATE agents SET metadata_json=? WHERE id='agent'`, "{\"private\":\""+card["ref"].(string)+"\"}"); e != nil {
		t.Fatal(e)
	}
	out, e = s.HostBridgeStatus(reader, []string{"host"})
	if e != nil || len(out) != 1 || len(out[0].Agents) != 0 {
		t.Fatal("private agent jobs disclosed", out, e)
	}
}
