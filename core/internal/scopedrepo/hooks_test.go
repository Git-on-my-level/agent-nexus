package scopedrepo_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/scopedrepo"
	"agent-nexus-core/internal/scopes"
)

type canonicalHook func(context.Context, scopedrepo.MutationTx, scopes.CanonicalMutation) error

func (h canonicalHook) ApplyCanonical(ctx context.Context, tx scopedrepo.MutationTx, m scopes.CanonicalMutation) error {
	return h(ctx, tx, m)
}
func mutation() scopes.CanonicalMutation {
	return scopes.CanonicalMutation{Identity: scopes.ResourceIdentity{ScopeID: "public", Kind: "doc", ResourceID: "opaque", RID: 1, CanonicalID: "canonical", CanonicalVersion: 1}, Changes: []scopes.Change{{ScopeID: "public", Kind: "doc", ResourceID: "opaque", CanonicalVersion: 1, Family: "work", Audience: "all", After: &scopes.Projection{Title: "title"}}}}
}
func TestCanonicalHooksAtomicAndExpire(t *testing.T) {
	for _, mode := range []string{"success", "error", "panic", "ignored_sql_error"} {
		t.Run(mode, func(t *testing.T) {
			db, _ := fixture(t)
			must(t, exec(db, `CREATE TABLE hook_projection(value TEXT)`))
			ctx := context.Background()
			tx, e := resourceaccess.NewDB(db).BeginTx(ctx, nil)
			must(t, e)
			_, e = tx.ExecContext(ctx, `INSERT INTO documents VALUES('canonical','title')`)
			must(t, e)
			var saved scopedrepo.MutationTx
			var rows *sql.Rows
			sentinel := errors.New("hook rejected")
			h := canonicalHook(func(ctx context.Context, cap scopedrepo.MutationTx, m scopes.CanonicalMutation) error {
				saved = cap
				if _, ok := cap.(interface{ Commit() error }); ok {
					t.Fatal("commit exposed")
				}
				_, e := cap.ExecContext(ctx, `INSERT INTO hook_projection VALUES(?)`, m.Changes[0].After.Title)
				if e != nil {
					return e
				}
				rows, e = cap.QueryContext(ctx, `SELECT title FROM documents`)
				if e != nil {
					return e
				}
				m.Changes[0].After.Title = "adapter changed its copy"
				return nil
			})
			h2 := canonicalHook(func(ctx context.Context, cap scopedrepo.MutationTx, m scopes.CanonicalMutation) error {
				if m.Changes[0].After.Title != "title" {
					t.Fatal("descriptor shared across hooks")
				}
				switch mode {
				case "error":
					return sentinel
				case "panic":
					panic(sentinel)
				case "ignored_sql_error":
					_, _ = cap.ExecContext(ctx, `INSERT INTO nonexistent VALUES(1)`)
				}
				return nil
			})
			var got error
			panicked := false
			func() {
				defer func() {
					if r := recover(); r != nil {
						if r != sentinel {
							panic(r)
						}
						panicked = true
					}
				}()
				got = scopedrepo.ApplyCanonicalHooks(ctx, tx, mutation(), h, h2)
			}()
			if mode == "success" {
				must(t, got)
				must(t, tx.Commit())
			} else {
				if mode == "panic" && !panicked {
					t.Fatal("missing panic")
				}
				if mode != "panic" && got == nil {
					t.Fatal("missing failure")
				}
				if !errors.Is(tx.Commit(), sql.ErrTxDone) {
					t.Fatal("failed hook left source committable")
				}
			}
			if _, e = saved.ExecContext(ctx, `INSERT INTO hook_projection VALUES('late')`); !errors.Is(e, scopes.ErrClosed) {
				t.Fatal("retained capability", e)
			}
			if rows.Next() {
				t.Fatal("escaped rows remain usable")
			}
			for _, table := range []string{"documents", "hook_projection"} {
				var n int
				must(t, db.QueryRow(`SELECT count(*) FROM `+table).Scan(&n))
				want := 0
				if mode == "success" {
					want = 1
				}
				if n != want {
					t.Fatal(table, n, want)
				}
			}
		})
	}
}
func TestCanonicalHooksPinSourcePolicy(t *testing.T) {
	db, _ := fixture(t)
	denied := errors.New("source policy denied")
	checks := 0
	ctx := resourceaccess.WithPolicy(context.Background(), resourceaccess.Policy{Read: func(q string) string { return q }, Check: func(context.Context, resourceaccess.QueryRower, any) error { checks++; return denied }})
	tx, e := resourceaccess.NewDB(db).BeginTx(ctx, nil)
	must(t, e)
	h := canonicalHook(func(_ context.Context, cap scopedrepo.MutationTx, _ scopes.CanonicalMutation) error {
		_, _ = cap.ExecContext(resourceaccess.WithoutPolicy(ctx), `INSERT INTO documents VALUES('leak','secret')`)
		return nil // Ignoring the policy failure cannot commit either.
	})
	if e = scopedrepo.ApplyCanonicalHooks(ctx, tx, mutation(), h); !errors.Is(e, denied) || checks != 1 {
		t.Fatal(e, checks)
	}
	if !errors.Is(tx.Commit(), sql.ErrTxDone) {
		t.Fatal("policy rejection left transaction open")
	}
	var n int
	must(t, db.QueryRow(`SELECT count(*) FROM documents`).Scan(&n))
	if n != 0 {
		t.Fatal("policy bypass")
	}
}
func TestCanonicalHooksSharedBudget(t *testing.T) {
	db, _ := fixture(t)
	ctx := context.Background()
	tx, e := resourceaccess.NewDB(db).BeginTx(ctx, nil)
	must(t, e)
	h := canonicalHook(func(ctx context.Context, cap scopedrepo.MutationTx, _ scopes.CanonicalMutation) error {
		for i := 0; i < scopes.MaxComputationOps/2+1; i++ {
			_, _ = cap.ExecContext(ctx, `INSERT OR IGNORE INTO documents VALUES('same','title')`)
		}
		return nil
	})
	if e = scopedrepo.ApplyCanonicalHooks(ctx, tx, mutation(), h, h); !errors.Is(e, scopes.ErrBudget) {
		t.Fatal(e)
	}
	if !errors.Is(tx.Commit(), sql.ErrTxDone) {
		t.Fatal("budget rejection left transaction open")
	}
}

func TestCanonicalHookCallerDeadline(t *testing.T) {
	db, _ := fixture(t)
	ctx := context.Background()
	tx, e := resourceaccess.NewDB(db).BeginTx(ctx, nil)
	must(t, e)
	h := canonicalHook(func(ctx context.Context, cap scopedrepo.MutationTx, _ scopes.CanonicalMutation) error {
		short, cancel := context.WithTimeout(ctx, 5*time.Millisecond)
		defer cancel()
		_, err := cap.ExecContext(short, `WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<100000000) INSERT INTO documents SELECT CAST(x AS TEXT),'x' FROM n`)
		if !errors.Is(short.Err(), context.DeadlineExceeded) || err == nil {
			t.Fatalf("caller deadline lost: %v %v", short.Err(), err)
		}
		return err
	})
	if e = scopedrepo.ApplyCanonicalHooks(ctx, tx, mutation(), h); e == nil {
		t.Fatal("deadline ignored")
	}
	if !errors.Is(tx.Commit(), sql.ErrTxDone) {
		t.Fatal("cancelled hook left source committable")
	}
}
