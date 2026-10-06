package scopedrepo_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"agent-nexus-core/internal/scopedrepo"
	"agent-nexus-core/internal/scopes"
	_ "modernc.org/sqlite"
)

func fixture(t *testing.T) (*sql.DB, *scopedrepo.Store) {
	t.Helper()
	db, e := sql.Open("sqlite", filepath.Join(t.TempDir(), "scopes.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	must(t, exec(db, `PRAGMA foreign_keys=ON;CREATE TABLE documents(id TEXT PRIMARY KEY,title TEXT);`))
	s := scopedrepo.New(db)
	must(t, s.Initialize(context.Background()))
	must(t, exec(db, `INSERT INTO scope_domains VALUES('public','active',1),('private','active',1);INSERT INTO scope_memberships VALUES('owner','public','writer',1),('owner','private','owner',1),('reader','public','reader',1);`))
	return db, s
}
func exec(db *sql.DB, q string, args ...any) error { _, e := db.Exec(q, args...); return e }
func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func TestIdentityIsolationAndReplay(t *testing.T) {
	for _, hidden := range []bool{false, true} {
		t.Run(fmt.Sprint(hidden), func(t *testing.T) {
			db, s := fixture(t)
			ctx := context.Background()
			if hidden {
				id, e := s.RegisterForMigration(ctx, "owner", "private", "doc", "private-canonical", "report", "hidden")
				must(t, e)
				must(t, exec(db, `UPDATE scope_aliases SET retired=1 WHERE resource_id=?`, id))
			}
			id, e := s.RegisterForMigration(ctx, "owner", "public", "doc", "public-canonical", "report", "request")
			must(t, e)
			if len(id) != 32 || strings.Trim(id, "0123456789abcdef") != "" {
				t.Fatal("identity shape", id)
			}
			again, e := s.RegisterForMigration(ctx, "owner", "public", "doc", "public-canonical", "report", "request")
			must(t, e)
			if again != id {
				t.Fatal("replay changed identity")
			}
			if _, e = s.RegisterForMigration(ctx, "owner", "public", "doc", "different", "report", "request"); e == nil {
				t.Fatal("changed replay accepted")
			}
			must(t, exec(db, `DELETE FROM scope_memberships WHERE principal='owner' AND scope_id='public'`))
			if _, e = s.RegisterForMigration(ctx, "owner", "public", "doc", "public-canonical", "report", "request"); !errors.Is(e, scopes.ErrDenied) {
				t.Fatal("replay bypassed revocation", e)
			}
		})
	}
}
func TestDirectoryAnd64ScopeSelection(t *testing.T) {
	db, s := fixture(t)
	ctx := context.Background()
	tx, e := db.Begin()
	must(t, e)
	ids := make([]scopes.ID, 64)
	for i := 0; i < 10065; i++ {
		id := fmt.Sprintf("scope-%05d", i)
		state := "transitioning"
		if i < 64 {
			state = "active"
			ids[i] = scopes.ID(id)
		}
		_, e = tx.Exec(`INSERT INTO scope_domains VALUES(?,?,1)`, id, state)
		must(t, e)
		_, e = tx.Exec(`INSERT INTO scope_memberships VALUES('many',?,'reader',1)`, id)
		must(t, e)
	}
	must(t, tx.Commit())
	must(t, s.ValidateSelection(ctx, "many", ids))
	if e = s.ValidateSelection(ctx, "many", append(ids, "scope-00064")); !errors.Is(e, scopes.ErrBudget) {
		t.Fatal(e)
	}
	p, e := s.Directory(ctx, "many", "", 64)
	must(t, e)
	if len(p.Bindings) != 64 || !p.MoreScopes || p.After != "scope-00063" {
		t.Fatal(p)
	}
	p, e = s.Directory(ctx, "many", p.After, 64)
	must(t, e)
	if len(p.Bindings) != 64 || !p.MoreScopes || p.Bindings[0].Available {
		t.Fatal("filtered unavailable slots", p)
	}
	stranger, e := s.Directory(ctx, "stranger", "", 64)
	must(t, e)
	if len(stranger.Bindings) != 0 || stranger.MoreScopes {
		t.Fatal("directory leaked", stranger)
	}
	if e = s.ValidateSelection(ctx, "many", []scopes.ID{"scope-00064"}); !errors.Is(e, scopes.ErrUpdating) {
		t.Fatal(e)
	}
	if e = s.ValidateSelection(ctx, "stranger", []scopes.ID{"scope-00064"}); !errors.Is(e, scopes.ErrDenied) {
		t.Fatal("state disclosed before authority", e)
	}
}
func TestDerivedBoundaryAndExpiry(t *testing.T) {
	db, s := fixture(t)
	ctx := context.Background()
	must(t, exec(db, `INSERT INTO documents VALUES('source','PRIVATE SECRET')`))
	id, e := s.RegisterForMigration(ctx, "owner", "private", "doc", "source", "report", "source")
	must(t, e)
	var saved scopedrepo.Derived
	var cap *scopedrepo.Computation
	must(t, s.Compute(ctx, "owner", "private", func(c *scopedrepo.Computation) error {
		cap = c
		v, e := c.DocumentTitle(id)
		if e != nil {
			return e
		}
		saved = v
		if strings.Contains(fmt.Sprintf("%#v %#v", c, v), "PRIVATE SECRET") {
			t.Fatal("reflectable plaintext")
		}
		if e = c.Persist("public", "leak", v); !errors.Is(e, scopes.ErrDerivation) {
			t.Fatal(e)
		}
		constant, e := c.Constant("private result exists")
		if e != nil {
			return e
		}
		if e = c.Persist("public", "branch", constant); !errors.Is(e, scopes.ErrDerivation) {
			t.Fatal(e)
		}
		return c.Persist("private", "allowed", v)
	}))
	if _, e = cap.DocumentTitle(id); !errors.Is(e, scopes.ErrClosed) {
		t.Fatal("retained capability", e)
	}
	must(t, s.Compute(ctx, "owner", "public", func(c *scopedrepo.Computation) error {
		if e := c.Persist("public", "rebound", saved); !errors.Is(e, scopes.ErrDerivation) {
			t.Fatal(e)
		}
		if _, e := c.DocumentTitle(id); !errors.Is(e, scopes.ErrDenied) {
			t.Fatal("cross scope read", e)
		}
		return nil
	}))
	var n int
	must(t, db.QueryRow(`SELECT count(*) FROM scope_projection_values WHERE scope_id='public'`).Scan(&n))
	if n != 0 {
		t.Fatal("public leak")
	}
	var value string
	must(t, db.QueryRow(`SELECT value FROM scope_projection_values WHERE scope_id='private'`).Scan(&value))
	if value != "PRIVATE SECRET" {
		t.Fatal(value)
	}
	must(t, exec(db, `DELETE FROM scope_memberships WHERE principal='owner' AND scope_id='private'`))
	if e = s.Compute(ctx, "owner", "private", func(*scopedrepo.Computation) error { return nil }); !errors.Is(e, scopes.ErrDenied) {
		t.Fatal("revoked compute", e)
	}
}
func TestComputationRollbackAndBudgets(t *testing.T) {
	db, s := fixture(t)
	ctx := context.Background()
	sentinel := errors.New("abort")
	e := s.Compute(ctx, "owner", "private", func(c *scopedrepo.Computation) error {
		v, e := c.Constant("x")
		if e != nil {
			return e
		}
		if e = c.Persist("private", "aborted", v); e != nil {
			return e
		}
		return sentinel
	})
	if !errors.Is(e, sentinel) {
		t.Fatal(e)
	}
	var n int
	must(t, db.QueryRow(`SELECT count(*) FROM scope_projection_values`).Scan(&n))
	if n != 0 {
		t.Fatal("callback not atomic")
	}
	must(t, s.Compute(ctx, "owner", "private", func(c *scopedrepo.Computation) error {
		if _, e := c.Constant(strings.Repeat("x", scopes.MaxValueBytes+1)); !errors.Is(e, scopes.ErrBudget) {
			t.Fatal(e)
		}
		for i := 0; i < scopes.MaxDerivedValues; i++ {
			if _, e := c.Constant("v"); e != nil {
				return e
			}
		}
		if _, e := c.Constant("overflow"); !errors.Is(e, scopes.ErrBudget) {
			t.Fatal(e)
		}
		return nil
	}))
}
func TestPublishSourceOwnerOrAdmin(t *testing.T) {
	db, s := fixture(t)
	ctx := context.Background()
	for _, r := range []string{"reader", "writer", "owner", "admin"} {
		must(t, exec(db, `UPDATE scope_memberships SET role=? WHERE principal='owner' AND scope_id='private'`, r))
		e := s.CanPublish(ctx, "owner", "private", "public")
		if r == "owner" || r == "admin" {
			must(t, e)
		} else if !errors.Is(e, scopes.ErrDenied) {
			t.Fatal(r, e)
		}
	}
	must(t, exec(db, `UPDATE scope_memberships SET role='reader' WHERE principal='owner' AND scope_id='public'`))
	if e := s.CanPublish(ctx, "owner", "private", "public"); !errors.Is(e, scopes.ErrDenied) {
		t.Fatal("destination write not required", e)
	}
}

// Until registration in the live workspace inventory, keep the complete shadow
// schema inventoried here. Adding a column/table requires an explicit ownership
// review and updated negative tests, even when it is not text-shaped.
func TestShadowSchemaInventory(t *testing.T) {
	db, _ := fixture(t)
	expected := map[string]string{
		"scope_domains":           "generation,id,state",
		"scope_memberships":       "generation,principal,role,scope_id",
		"scope_resources":         "canonical_id,id,kind,scope_id,version",
		"scope_aliases":           "alias,kind,resource_id,retired,scope_id",
		"scope_replays":           "kind,principal,replay_key,request_hash,resource_id,scope_id",
		"scope_projection_values": "projection_key,scope_id,value",
	}
	rows, e := db.Query(`SELECT name FROM sqlite_schema WHERE type='table' AND name GLOB 'scope_*' ORDER BY name`)
	must(t, e)
	var names []string
	for rows.Next() {
		var n string
		must(t, rows.Scan(&n))
		names = append(names, n)
	}
	must(t, rows.Err())
	rows.Close()
	if len(names) != len(expected) {
		t.Fatal("unreviewed shadow table", names)
	}
	for _, name := range names {
		rows, e = db.Query(`SELECT name FROM pragma_table_xinfo(?) ORDER BY name`, name)
		must(t, e)
		var columns []string
		for rows.Next() {
			var c string
			must(t, rows.Scan(&c))
			columns = append(columns, c)
		}
		must(t, rows.Err())
		rows.Close()
		if strings.Join(columns, ",") != expected[name] {
			t.Fatal("unreviewed columns", name, columns)
		}
	}
}

func TestReaderWriterSelectionAndExpiry(t *testing.T) {
	db, s := fixture(t)
	ctx := context.Background()
	must(t, exec(db, `INSERT INTO documents VALUES('public-doc','public title'),('private-doc','secret')`))
	public, e := s.RegisterForMigration(ctx, "owner", "public", "doc", "public-doc", "public", "pub")
	must(t, e)
	private, e := s.RegisterForMigration(ctx, "owner", "private", "doc", "private-doc", "private", "priv")
	must(t, e)
	var saved scopedrepo.Reader
	request := scopes.RequestSelection{Principal: "owner", ScopeIDs: []scopes.ID{"public"}}
	must(t, s.Read(ctx, request, func(r scopedrepo.Reader) error {
		saved = r
		title, e := r.DocumentTitle("public", public)
		if e != nil {
			return e
		}
		if title != "public title" {
			t.Fatal(title)
		}
		if _, e = r.DocumentTitle("private", private); !errors.Is(e, scopes.ErrDenied) {
			t.Fatal("read outside selection", e)
		}
		ids, e := r.CoveredScopes()
		if e != nil {
			return e
		}
		ids[0] = "private"
		if _, e = r.DocumentTitle("private", private); !errors.Is(e, scopes.ErrDenied) {
			t.Fatal("selection mutation", e)
		}
		return nil
	}))
	if _, e = saved.DocumentTitle("public", public); !errors.Is(e, scopes.ErrClosed) {
		t.Fatal(e)
	}
	must(t, s.Write(ctx, request, func(w scopedrepo.Writer) error {
		v, e := w.Constant("scoped")
		if e != nil {
			return e
		}
		return w.Persist("public", "typed", v)
	}))
	request.ScopeIDs = append(request.ScopeIDs, "private")
	if e = s.Write(ctx, request, func(scopedrepo.Writer) error { return nil }); !errors.Is(e, scopes.ErrBudget) {
		t.Fatal("multi-scope writer", e)
	}
}

func TestNoGrantsScopeCannotAcquireAudience(t *testing.T) {
	db, s := fixture(t)
	ctx := context.Background()
	must(t, exec(db, `INSERT INTO scope_domains VALUES('lost','inaccessible',1)`))
	if e := exec(db, `INSERT INTO scope_memberships VALUES('admin','lost','admin',1)`); e == nil {
		t.Fatal("grant to inaccessible domain")
	}
	if e := exec(db, `UPDATE scope_memberships SET scope_id='lost' WHERE principal='owner' AND scope_id='private'`); e == nil {
		t.Fatal("moved grant to inaccessible domain")
	}
	if e := exec(db, `UPDATE scope_domains SET state='inaccessible' WHERE id='private'`); e == nil {
		t.Fatal("inaccessible domain retained grants")
	}
	for _, query := range []string{
		`INSERT OR REPLACE INTO scope_domains VALUES('private','inaccessible',2)`,
		`UPDATE scope_domains SET state='active' WHERE id='lost'`,
		`INSERT OR REPLACE INTO scope_domains VALUES('lost','active',2)`,
		`UPDATE scope_domains SET id='reused' WHERE id='lost'`,
		`DELETE FROM scope_domains WHERE id='lost'`,
	} {
		if e := exec(db, query); e == nil {
			t.Fatal("permanent no-grants invariant bypass", query)
		}
	}
	if e := s.ValidateSelection(ctx, "admin", []scopes.ID{"lost"}); !errors.Is(e, scopes.ErrDenied) {
		t.Fatal(e)
	}
}
