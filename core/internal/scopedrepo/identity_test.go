package scopedrepo_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"agent-nexus-core/internal/scopedrepo"
	"agent-nexus-core/internal/scopes"
)

func TestRIDMappingAuthorityAndReservation(t *testing.T) {
	db, s := fixture(t)
	ctx := context.Background()
	id, e := s.RegisterForMigration(ctx, "owner", "private", "doc", "secret-canonical", "alias", "replay")
	must(t, e)
	other, e := s.RegisterForMigration(ctx, "owner", "public", "doc", "other-canonical", "alias", "other")
	must(t, e)
	var identity scopes.ResourceIdentity
	var saved scopedrepo.Reader
	must(t, s.Read(ctx, scopes.RequestSelection{Principal: "owner", ScopeIDs: []scopes.ID{"private", "public"}}, func(r scopedrepo.Reader) error {
		saved = r
		var e error
		identity, e = r.ResourceIdentity("private", "doc", id)
		if e != nil {
			return e
		}
		p, e := r.ResourceIdentity("public", "doc", other)
		if e != nil {
			return e
		}
		if identity.RID < 1 || p.RID == identity.RID || identity.CanonicalID != "secret-canonical" {
			t.Fatal(identity, p)
		}
		return nil
	}))
	encoded, e := json.Marshal(identity)
	must(t, e)
	if strings.Contains(string(encoded), "RID") || strings.Contains(string(encoded), "secret-canonical") {
		t.Fatal("internal identity serialized", string(encoded))
	}
	again, e := s.RegisterForMigration(ctx, "owner", "private", "doc", "secret-canonical", "alias", "replay")
	must(t, e)
	if again != id {
		t.Fatal("unstable opaque ID")
	}
	must(t, s.Read(ctx, scopes.RequestSelection{Principal: "owner", ScopeIDs: []scopes.ID{"private"}}, func(r scopedrepo.Reader) error {
		v, e := r.ResourceIdentity("private", "doc", id)
		if v != identity {
			t.Fatal("unstable RID", v, identity)
		}
		return e
	}))
	must(t, s.Read(ctx, scopes.RequestSelection{Principal: "reader", ScopeIDs: []scopes.ID{"public"}}, func(r scopedrepo.Reader) error {
		for _, candidate := range []struct {
			scope scopes.ID
			id    string
		}{{"private", id}, {"public", id}, {"public", "missing"}} {
			if _, e := r.ResourceIdentity(candidate.scope, "doc", candidate.id); !errors.Is(e, scopes.ErrDenied) {
				t.Fatal("identity oracle", candidate, e)
			}
		}
		return nil
	}))
	if _, e = saved.ResourceIdentity("private", "doc", id); !errors.Is(e, scopes.ErrClosed) {
		t.Fatal(e)
	}
	for _, q := range []string{
		`UPDATE scope_resource_rids SET rid=rid+100 WHERE rid=?`,
		`DELETE FROM scope_resource_rids WHERE rid=?`,
		`INSERT OR REPLACE INTO scope_resource_rids SELECT rid,scope_id,kind,resource_id FROM scope_resource_rids WHERE rid=?`,
		`INSERT OR REPLACE INTO scope_resource_rids(scope_id,kind,resource_id) SELECT scope_id,kind,resource_id FROM scope_resource_rids WHERE rid=?`,
	} {
		if e = exec(db, q, identity.RID); e == nil {
			t.Fatal("RID rebinding allowed", q)
		}
	}
	must(t, exec(db, `DELETE FROM scope_memberships WHERE principal='owner' AND scope_id='private'`))
	if e = s.Read(ctx, scopes.RequestSelection{Principal: "owner", ScopeIDs: []scopes.ID{"private"}}, func(scopedrepo.Reader) error { t.Fatal("revoked reader admitted"); return nil }); !errors.Is(e, scopes.ErrDenied) {
		t.Fatal(e)
	}
}
func TestRIDAllocationRollsBackWithRegistry(t *testing.T) {
	db, _ := fixture(t)
	tx, e := db.Begin()
	must(t, e)
	_, e = tx.Exec(`INSERT INTO scope_resources VALUES('public','doc','rolled-back','canonical',1)`)
	must(t, e)
	var n int
	must(t, tx.QueryRow(`SELECT count(*) FROM scope_resource_rids`).Scan(&n))
	if n != 1 {
		t.Fatal("mapping not atomic", n)
	}
	must(t, tx.Rollback())
	must(t, db.QueryRow(`SELECT count(*) FROM scope_resource_rids`).Scan(&n))
	if n != 0 {
		t.Fatal("orphan RID", n)
	}
}

func TestRIDLookupUsesBothExactKeys(t *testing.T) {
	db, _ := fixture(t)
	query, e := os.ReadFile("queries/identity.sql")
	must(t, e)
	rows, e := db.Query("EXPLAIN QUERY PLAN "+string(query), "public", "doc", "opaque")
	must(t, e)
	defer rows.Close()
	probes := 0
	for rows.Next() {
		var id, parent, unused int
		var detail string
		must(t, rows.Scan(&id, &parent, &unused, &detail))
		if strings.Contains(detail, "SCAN") {
			t.Fatal("identity lookup scans", detail)
		}
		if strings.Contains(detail, "SEARCH") {
			probes++
		}
	}
	must(t, rows.Err())
	if probes != 2 {
		t.Fatal("expected two indexed identity probes", probes)
	}
}
