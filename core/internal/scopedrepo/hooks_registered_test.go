package scopedrepo_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"agent-nexus-core/internal/readmodel"
	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/scopedrepo"
	"agent-nexus-core/internal/scopes"
)

func TestRegisteredHooksRejectUnknownBeforeCallback(t *testing.T) {
	db, _ := fixture(t)
	ctx := context.Background()
	tx, err := resourceaccess.NewDB(db).BeginTx(ctx, nil)
	must(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO documents VALUES('canonical','source')`)
	must(t, err)
	called := false
	hook := canonicalHook(func(context.Context, scopedrepo.MutationTx, scopes.CanonicalMutation) error {
		called = true
		return nil
	})
	err = scopedrepo.ApplyRegisteredCanonicalHooks(ctx, tx, mutation(), hook)
	if !errors.Is(err, scopes.ErrDenied) || called {
		t.Fatal(err, called)
	}
	if !errors.Is(tx.Commit(), sql.ErrTxDone) {
		t.Fatal("unregistered hook left source committable")
	}
	var count int
	must(t, db.QueryRow(`SELECT count(*) FROM documents`).Scan(&count))
	if count != 0 {
		t.Fatal("unregistered hook committed source")
	}
}

func TestRegisteredHookCommitsReviewedTemplates(t *testing.T) {
	db, _ := fixture(t)
	s := scopedrepo.New(db)
	must(t, s.InitializeFeedSchema(context.Background()))
	must(t, exec(db, `INSERT INTO scope_resources VALUES('public','doc','opaque','canonical',1)`))
	m := mutation()
	must(t, db.QueryRow(`SELECT rid FROM scope_resource_rids WHERE resource_id='opaque'`).Scan(&m.Identity.RID))
	ctx := context.Background()
	tx, err := resourceaccess.NewDB(db).BeginTx(ctx, nil)
	must(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO documents VALUES('canonical','source')`)
	must(t, err)
	hook := scopedrepo.ReadModelCanonicalHook{Capture: func(c scopes.Change, p scopes.Projection) (readmodel.Entry, json.RawMessage, error) {
		return readmodel.Entry{Family: c.Family, Audience: c.Audience, Sort: 1, Buckets: []string{"total"}}, json.RawMessage(`{"title":"source"}`), nil
	}}
	must(t, scopedrepo.ApplyRegisteredCanonicalHooks(ctx, tx, m, hook))
	must(t, tx.Commit())
	var count int
	must(t, db.QueryRow(`SELECT count(*) FROM scope_feed`).Scan(&count))
	if count != 1 {
		t.Fatal(count)
	}
}
