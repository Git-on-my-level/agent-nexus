package scopedrepo_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"agent-nexus-core/internal/scopedrepo"
	"agent-nexus-core/internal/scopes"
	"agent-nexus-core/internal/storage"
)

type integrityConnection interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func seedIntegrity(t *testing.T, db *sql.DB) {
	t.Helper()
	must(t, exec(db, `
INSERT INTO scope_domains VALUES
 ('ig-scope','active',1),('ig-other','active',1),
 ('ig-membership','active',1),('ig-projection','active',1),
 ('ig-generation','active',1),('ig-resource','active',1);
INSERT INTO scope_memberships VALUES
 ('ig-owner','ig-scope','owner',1),('ig-unbound','ig-scope','reader',1),
 ('ig-owner','ig-membership','reader',1);
INSERT INTO scope_resources VALUES
 ('ig-scope','doc','ig-opaque','ig-canonical',1),
 ('ig-resource','doc','ig-other-opaque','ig-other-canonical',1);
INSERT INTO scope_aliases VALUES('ig-scope','doc','ig-alias','ig-opaque',0);
INSERT INTO scope_projection_values VALUES('ig-projection','ig-key','value');
INSERT INTO scope_feed_generations(scope_id,generation) VALUES('ig-generation',1);
INSERT INTO scope_feed_bindings VALUES('ig-owner','ig-scope',1,'documents','owner',1,1);
`))
}

func integrityIdentity(t *testing.T, s *scopedrepo.Store) scopes.ResourceIdentity {
	t.Helper()
	var identity scopes.ResourceIdentity
	must(t, s.Read(context.Background(), scopes.RequestSelection{Principal: "ig-owner", ScopeIDs: []scopes.ID{"ig-scope"}}, func(r scopedrepo.Reader) error {
		var err error
		identity, err = r.ResourceIdentity("ig-scope", "doc", "ig-opaque")
		return err
	}))
	return identity
}

func checkIntegrity(t *testing.T, conn integrityConnection, s *scopedrepo.Store, foreignKeys int) {
	t.Helper()
	ctx := context.Background()
	var enabled int
	must(t, conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&enabled))
	if enabled != foreignKeys {
		t.Fatalf("foreign_keys = %d, want %d", enabled, foreignKeys)
	}
	_, err := conn.ExecContext(ctx, `PRAGMA recursive_triggers=OFF`)
	must(t, err)
	before := integrityIdentity(t, s)
	for _, tc := range []struct{ name, sql string }{
		{"source", `UPDATE scope_resources SET canonical_id='rebound' WHERE id='ig-opaque'`},
		{"scope", `UPDATE scope_resources SET scope_id='ig-other' WHERE id='ig-opaque'`},
		{"kind", `UPDATE scope_resources SET kind='board' WHERE id='ig-opaque'`},
		{"opaque", `UPDATE scope_resources SET id='rebound' WHERE id='ig-opaque'`},
		{"delete", `DELETE FROM scope_resources WHERE id='ig-opaque'`},
		{"replace-primary", `INSERT OR REPLACE INTO scope_resources VALUES('ig-scope','doc','ig-opaque','replacement',1)`},
		{"replace-canonical", `INSERT OR REPLACE INTO scope_resources VALUES('ig-other','doc','replacement','ig-canonical',1)`},
		{"replace-same", `INSERT OR REPLACE INTO scope_resources VALUES('ig-scope','doc','ig-opaque','ig-canonical',2)`},
		{"update-replace", `UPDATE OR REPLACE scope_resources SET canonical_id='ig-other-canonical' WHERE id='ig-opaque'`},
	} {
		t.Run("identity-"+tc.name, func(t *testing.T) {
			if _, err := conn.ExecContext(ctx, tc.sql); err == nil {
				t.Fatal("canonical source mutation accepted")
			}
			if after := integrityIdentity(t, s); after != before {
				t.Fatal("Reader.ResourceIdentity changed", before, after)
			}
		})
	}
	_, err = conn.ExecContext(ctx, `UPDATE scope_resources SET version=version+1 WHERE id='ig-opaque'`)
	must(t, err)
	before.CanonicalVersion++
	if after := integrityIdentity(t, s); after != before {
		t.Fatal("version advance changed binding", before, after)
	}
	for _, tc := range []struct{ name, sql string }{
		{"membership-insert", `INSERT INTO scope_memberships VALUES('orphan','missing','reader',1)`},
		{"membership-update", `UPDATE scope_memberships SET scope_id='missing' WHERE principal='ig-unbound'`},
		{"resource-insert", `INSERT INTO scope_resources VALUES('missing','doc','orphan','orphan',1)`},
		{"resource-update", `UPDATE scope_resources SET scope_id='missing' WHERE id='ig-opaque'`},
		{"alias-insert", `INSERT INTO scope_aliases VALUES('ig-scope','doc','orphan','missing',0)`},
		{"alias-update-id", `UPDATE scope_aliases SET resource_id='missing' WHERE alias='ig-alias'`},
		{"alias-update-kind", `UPDATE scope_aliases SET kind='missing' WHERE alias='ig-alias'`},
		{"alias-update-scope", `UPDATE scope_aliases SET scope_id='missing' WHERE alias='ig-alias'`},
		{"rid-insert", `INSERT INTO scope_resource_rids(scope_id,kind,resource_id) VALUES('ig-scope','doc','missing')`},
		{"rid-update", `UPDATE scope_resource_rids SET resource_id='missing' WHERE resource_id='ig-opaque'`},
		{"rid-delete", `DELETE FROM scope_resource_rids WHERE resource_id='ig-opaque'`},
		{"rid-replace", `INSERT OR REPLACE INTO scope_resource_rids SELECT * FROM scope_resource_rids WHERE resource_id='ig-opaque'`},
		{"projection-insert", `INSERT INTO scope_projection_values VALUES('missing','orphan','value')`},
		{"projection-update", `UPDATE scope_projection_values SET scope_id='missing' WHERE scope_id='ig-projection'`},
		{"receipt-insert", `INSERT INTO scope_feed_generations(scope_id,generation) VALUES('missing',1)`},
		{"receipt-update", `UPDATE scope_feed_generations SET scope_id='missing' WHERE scope_id='ig-generation'`},
		{"binding-insert", `INSERT INTO scope_feed_bindings VALUES('missing','ig-scope',1,'documents','owner',1,1)`},
		{"binding-update-principal", `UPDATE scope_feed_bindings SET principal='missing' WHERE principal='ig-owner'`},
		{"binding-update-scope", `UPDATE scope_feed_bindings SET scope_id='ig-other' WHERE principal='ig-owner'`},
		{"membership-rekey", `UPDATE scope_memberships SET principal='rekeyed' WHERE principal='ig-owner' AND scope_id='ig-scope'`},
		{"membership-replace", `INSERT OR REPLACE INTO scope_memberships VALUES('ig-owner','ig-scope','reader',2)`},
		{"membership-update-replace", `UPDATE OR REPLACE scope_memberships SET principal='ig-owner' WHERE principal='ig-unbound' AND scope_id='ig-scope'`},
		{"domain-membership-delete", `DELETE FROM scope_domains WHERE id='ig-membership'`},
		{"domain-resource-delete", `DELETE FROM scope_domains WHERE id='ig-resource'`},
		{"domain-projection-delete", `DELETE FROM scope_domains WHERE id='ig-projection'`},
		{"domain-receipt-delete", `DELETE FROM scope_domains WHERE id='ig-generation'`},
		{"domain-rekey", `UPDATE scope_domains SET id='rekeyed' WHERE id='ig-projection'`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := conn.ExecContext(ctx, tc.sql); err == nil {
				t.Fatal("referential integrity violation accepted")
			}
		})
	}
	// Deleting an ordinary, unreferenced scope is still allowed.
	_, err = conn.ExecContext(ctx, `INSERT INTO scope_domains VALUES('ig-empty','active',1); DELETE FROM scope_domains WHERE id='ig-empty'`)
	must(t, err)
	// Historical binding generations are all removed, including when the driver
	// does not enforce the declared ON DELETE CASCADE itself.
	_, err = conn.ExecContext(ctx, `INSERT INTO scope_memberships VALUES('ig-revoke','ig-scope','reader',1);
INSERT INTO scope_feed_bindings VALUES
 ('ig-revoke','ig-scope',1,'documents','reader',1,1),
 ('ig-revoke','ig-scope',2,'documents','reader',1,1),
 ('ig-revoke','ig-scope',3,'documents','reader',1,1);
DELETE FROM scope_memberships WHERE principal='ig-revoke' AND scope_id='ig-scope';
INSERT INTO scope_memberships VALUES('ig-revoke','ig-scope','reader',1);`)
	must(t, err)
	var n int
	must(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM scope_feed_bindings WHERE principal='ig-revoke' AND scope_id='ig-scope'`).Scan(&n))
	if n != 0 {
		t.Fatal("deleted membership bindings revived on regrant", n)
	}
	_, err = conn.ExecContext(ctx, `DELETE FROM scope_memberships WHERE principal='ig-revoke' AND scope_id='ig-scope'`)
	must(t, err)
	must(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM scope_feed_bindings WHERE principal='ig-owner' AND scope_id='ig-scope'`).Scan(&n))
	if n != 1 {
		t.Fatal("unrelated bindings changed", n)
	}
}

func TestShadowIntegrityWithForeignKeysEnabled(t *testing.T) {
	db, s := fixture(t)
	must(t, s.InitializeFeedSchema(context.Background()))
	seedIntegrity(t, db)
	checkIntegrity(t, db, s, 1)
}

func TestShadowIntegrityWorkspacePoolForeignKeysDisabled(t *testing.T) {
	if testing.Short() {
		t.Skip("initializes the full workspace storage stack")
	}
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	must(t, err)
	t.Cleanup(func() { _ = ws.Close() })
	db := ws.DB()
	db.SetMaxOpenConns(4)
	s := scopedrepo.New(db)
	must(t, s.Initialize(ctx))
	must(t, s.InitializeFeedSchema(ctx))
	seedIntegrity(t, db)
	// Hold three distinct physical connections at once; use the remaining pool
	// slot for the real scoped Reader checks after every rejected source change.
	var held []*sql.Conn
	for i := 0; i < 3; i++ {
		conn, err := db.Conn(ctx)
		must(t, err)
		t.Cleanup(func() { _ = conn.Close() })
		held = append(held, conn)
	}
	for i, conn := range held {
		t.Run(fmt.Sprintf("connection-%d", i), func(t *testing.T) {
			checkIntegrity(t, conn, s, 0)
		})
	}
}

func TestIntegrityGuardLookupsUseIndexes(t *testing.T) {
	db, s := fixture(t)
	must(t, s.InitializeFeedSchema(context.Background()))
	for i, query := range []string{
		`SELECT 1 FROM scope_domains WHERE id=?`,
		`SELECT 1 FROM scope_memberships WHERE scope_id=?`,
		`SELECT 1 FROM scope_memberships WHERE principal=? AND scope_id=?`,
		`SELECT 1 FROM scope_resources WHERE scope_id=?`,
		`SELECT 1 FROM scope_resources WHERE scope_id=? AND kind=? AND id=?`,
		`SELECT 1 FROM scope_resources WHERE kind=? AND canonical_id=?`,
		`SELECT 1 FROM scope_projection_values WHERE scope_id=?`,
		`SELECT 1 FROM scope_feed_generations WHERE scope_id=?`,
		`SELECT 1 FROM scope_feed_bindings WHERE principal=? AND scope_id=?`,
		`DELETE FROM scope_feed_bindings WHERE principal=? AND scope_id=?`,
	} {
		t.Run(fmt.Sprintf("lookup-%d", i), func(t *testing.T) {
			args := make([]any, strings.Count(query, "?"))
			for j := range args {
				args[j] = "identity"
			}
			rows, err := db.Query("EXPLAIN QUERY PLAN "+query, args...)
			must(t, err)
			defer rows.Close()
			searches := 0
			for rows.Next() {
				var id, parent, unused int
				var detail string
				must(t, rows.Scan(&id, &parent, &unused, &detail))
				if strings.Contains(detail, "SCAN") {
					t.Fatal("integrity guard scans", detail)
				}
				if strings.Contains(detail, "SEARCH") {
					searches++
				}
			}
			must(t, rows.Err())
			if searches == 0 {
				t.Fatal("no indexed guard probe")
			}
		})
	}
}
