package auth

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agent-nexus-core/internal/storage"
	"agent-nexus-core/internal/testsql"
)

func TestPrincipalForActorUsesBoundedIdentityAndSelectedHost(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	for _, q := range []string{
		`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<4095) INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) SELECT 'unrelated-'||i,'user-'||i,'actor-'||i,'2026-01-01','2026-01-01','{}' FROM n`,
		`INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) VALUES('target-z','codex.mini','target','2025-01-01','2025-01-01','{"principal_kind":"human"}')`,
		`INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json,revoked_at) VALUES('target-revoked','revoked','revoked-target','2027-01-01','2027-01-01','{}','now')`,
		`INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at,bridge_expires_at) VALUES('selected-host','mini','Host','user','machine','[]','now','2999-01-01T00:00:00Z')`,
		`INSERT INTO host_agents(host_id,name,agent_id,identity_kind) VALUES('selected-host','codex','target-z','derived')`,
		`INSERT INTO host_exclusions(host_id,name) VALUES('selected-host','codex')`,
	} {
		if _, err := ws.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db, counter := testsql.Open("file:" + ws.Layout().DatabasePath)
	defer db.Close()
	s := NewStore(db)
	p, err := s.PrincipalForActor(ctx, "target")
	if err != nil || p.AgentID != "target-z" || p.PrincipalKind != "human" || p.HostID != "selected-host" || !p.HostBridgeOnline || !p.HostExcluded {
		t.Fatalf("identity/host changed: %+v %v", p, err)
	}
	if counter.Count() != 3 || counter.ReturnedRows() != 3 {
		t.Fatalf("directory materialized: %d statements/%d rows", counter.Count(), counter.ReturnedRows())
	}
	selector := counter.Statements()[0]
	rows, err := ws.DB().Query("EXPLAIN QUERY PLAN "+selector.SQL, selector.Args...)
	if err != nil {
		t.Fatal(err)
	}
	var plan string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		plan += detail + ";"
	}
	err = rows.Err()
	rows.Close()
	if err != nil || !strings.Contains(plan, "SEARCH agents USING INDEX") || !strings.Contains(plan, "actor_id=?") {
		t.Fatalf("identity selector is not indexed: %s %v", plan, err)
	}
	if _, err := ws.DB().Exec(`UPDATE agents SET revoked_at='now' WHERE id='target-z'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PrincipalForActor(ctx, "target"); !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("revoked identity reused: %v", err)
	}
	if _, err := s.PrincipalForActor(ctx, "missing"); !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("missing identity: %v", err)
	}
}
