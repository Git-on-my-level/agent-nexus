package auth

import (
	"agent-nexus-core/internal/storage"
	"agent-nexus-core/internal/testsql"
	"context"
	"fmt"
	"testing"
)

func TestAuthAdminPagesBatchHostsAndPreserveExhaustiveWrapper(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	for i := 0; i < 205; i++ {
		id := fmt.Sprintf("agent-%03d", i)
		if _, err := ws.DB().ExecContext(ctx, `INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) VALUES(?,?,?,'now','now','{"principal_kind":"agent","auth_admin":true}')`, id, id, "actor-"+id); err != nil {
			t.Fatal(err)
		}
	}
	counted, counter := testsql.Open("file:" + ws.Layout().DatabasePath)
	defer counted.Close()
	s := NewStore(counted)
	page, err := s.AuthAdminPage(ctx, 200, "")
	if err != nil || len(page.Admins) != 200 || !page.HasMore || page.NextCursor == "" {
		t.Fatalf("first: %+v %v", page, err)
	}
	if counter.Count() != 1 || counter.ReturnedRows() != 201 {
		t.Fatalf("unbounded admin page: %d statements/%d rows", counter.Count(), counter.ReturnedRows())
	}
	second, err := s.AuthAdminPage(ctx, 200, page.NextCursor)
	if err != nil || len(second.Admins) != 5 || second.HasMore || second.Admins[0].PrincipalID == page.Admins[199].PrincipalID {
		t.Fatalf("second: %+v %v", second, err)
	}
	all, err := s.ListAuthAdmins(ctx)
	if err != nil || len(all) != 205 {
		t.Fatalf("exhaustive wrapper: %d %v", len(all), err)
	}
	for _, cursor := range []string{"!", "e30"} {
		if _, err := s.AuthAdminPage(ctx, 50, cursor); err != ErrInvalidRequest {
			t.Errorf("accepted cursor %q: %v", cursor, err)
		}
	}
}

func TestHostInventoryBatchesSelectedHostsAndBoundsAgents(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("host-%d", i)
		if _, err := ws.DB().ExecContext(ctx, `INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at,bridge_expires_at) VALUES(?,?,'Host','user','machine','[]','now','2999-01-01T00:00:00Z')`, id, id); err != nil {
			t.Fatal(err)
		}
		for j := 0; j < 4; j++ {
			agent := fmt.Sprintf("%s-agent-%d", id, j)
			if _, err := ws.DB().ExecContext(ctx, `INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) VALUES(?,?,?,'now','now','{}')`, agent, agent, "actor-"+agent); err != nil {
				t.Fatal(err)
			}
			if _, err := ws.DB().ExecContext(ctx, `INSERT INTO host_agents(host_id,name,agent_id,identity_kind) VALUES(?,?,?,'derived')`, id, agent, agent); err != nil {
				t.Fatal(err)
			}
		}
	}
	db, counter := testsql.Open("file:" + ws.Layout().DatabasePath)
	defer db.Close()
	entries, total, active, err := NewStore(db).ListHostInventory(ctx, 2, 3)
	if err != nil || total != 3 || active != 3 || len(entries) != 2 {
		t.Fatalf("inventory: %+v total=%d active=%d err=%v", entries, total, active, err)
	}
	for _, entry := range entries {
		if entry.AgentCount != 4 || len(entry.Host.Agents) != 3 || !entry.Host.Agents[0].BridgeOnline || *entry.Host.Agents[0].HostID != entry.Host.ID {
			t.Fatalf("inventory changed: %+v", entry)
		}
	}
	if counter.Count() != 4 || counter.ReturnedRows() != 11 {
		t.Fatalf("unbounded inventory: statements=%d rows=%d", counter.Count(), counter.ReturnedRows())
	}
}
