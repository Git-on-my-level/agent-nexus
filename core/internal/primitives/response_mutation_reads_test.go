package primitives

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"agent-nexus-core/internal/resourceaccess"
)

func TestResponseMutationProofConsumesCurrentEpoch(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	// These two shadows are constructed separately from ordinary relation shadows.
	if _, err := ws.DB().Exec(`CREATE TABLE IF NOT EXISTS pm_records(kind TEXT,id TEXT,workspace_id TEXT,actor_id TEXT,parent_id TEXT,revision INTEGER,body BLOB,PRIMARY KEY(kind,id))`); err != nil {
		t.Fatal(err)
	}
	scoped := WithAccessScope(ctx, AccessScope{ActorID: "reader"})
	admitted, close := withResponseMutationReads(scoped)
	defer close()
	tx, err := ws.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	checks := []struct {
		query string
		args  []any
		value bool
	}{
		{exactValueCheckSQL(), []any{`["public"]`, `["public"]`, `["public"]`, `["public"]`, `["public"]`, `["public"]`}, true},
		{`SELECT EXISTS(SELECT 1 FROM _anx_denied_atoms WHERE ref=?)`, []any{"public"}, true},
		{`SELECT count(*) FROM cards`, nil, false},
		{`SELECT count(*) FROM pm_records`, nil, false},
		{`SELECT count(*) FROM series_adapters`, nil, false},
	}
	for _, test := range checks {
		prepared, args, ok := responseMutationRead(admitted, tx, test.query, test.args)
		if !ok {
			t.Fatalf("not admitted: %s", test.query)
		}
		// Prepare using the old authority, then change it before consumption.
		if _, err := tx.Exec(`UPDATE resource_access_epoch SET version=version+1 WHERE singleton=1`); err != nil {
			t.Fatal(err)
		}
		if test.value {
			var denied bool
			if err := tx.QueryRowContext(admitted, prepared, args...).Scan(&denied); err != nil || !denied {
				t.Fatalf("stale value check granted access: denied=%v err=%v", denied, err)
			}
		} else {
			var count int
			if err := tx.QueryRowContext(admitted, prepared, args...).Scan(&count); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("stale relation exposed result: count=%d err=%v", count, err)
			}
		}
		// The next operation must rebuild rather than permanently denying reads.
		prepared, args, ok = responseMutationRead(admitted, tx, test.query, test.args)
		if !ok {
			t.Fatal("refresh failed")
		}
		var count int
		if err := tx.QueryRowContext(admitted, prepared, args...).Scan(&count); err != nil || count != 0 {
			t.Fatalf("fresh statement count=%d err=%v", count, err)
		}
	}
	for _, query := range []string{`SELECT count(*) FROM cards WHERE id=?1`, `SELECT count(*) FROM cards WHERE id=:id`} {
		if _, _, ok := responseMutationRead(admitted, tx, query, []any{"public"}); ok {
			t.Fatal("explicit parameters must use canonical fallback")
		}
	}
	if _, _, ok := responseMutationRead(scoped, tx, `SELECT count(*) FROM cards`, nil); ok {
		t.Fatal("ordinary mutation gained admission")
	}
	// A changed principal cannot inherit the old denial closure.
	rebound := WithAccessScope(admitted, AccessScope{ActorID: "owner"})
	if _, _, ok := responseMutationRead(rebound, tx, `SELECT count(*) FROM cards`, nil); !ok {
		t.Fatal("scope refresh failed")
	}
	proof := admitted.Value(responseMutationReadKey{}).(*responseMutationProof)
	if proof.scope.ActorID != "owner" {
		t.Fatal("scope was not rebound")
	}
	if _, _, ok := responseMutationRead(admitted, ws.DB(), `SELECT count(*) FROM cards`, nil); ok {
		t.Fatal("non-transaction read admitted")
	}
	close()
	if _, _, ok := responseMutationRead(admitted, tx, `SELECT count(*) FROM cards`, nil); ok {
		t.Fatal("closed proof reused")
	}
}

func TestResponseMutationProofRefreshesAuthorityAndTransaction(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	doc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "response proof control"}, "body", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	admitted, close := withResponseMutationReads(WithAccessScope(ctx, AccessScope{ActorID: "reader"}))
	defer close()
	policy, _ := resourceaccess.PolicyFrom(admitted)
	proof := admitted.Value(responseMutationReadKey{}).(*responseMutationProof)
	query := `SELECT id FROM documents WHERE id=?`
	args := []any{doc["id"]}
	check := func(tx *sql.Tx, wantHidden bool) {
		t.Helper()
		q, bound := policy.TransactionRead(admitted, tx, query, args)
		var id string
		err := tx.QueryRowContext(admitted, q, bound...).Scan(&id)
		if wantHidden {
			if !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("private document exposed: id=%q err=%v", id, err)
			}
		} else if err != nil || id != doc["id"] {
			t.Fatalf("public document unavailable: id=%q err=%v", id, err)
		}
	}
	first, err := ws.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	check(first, false)
	old := proof.snapshot
	if err := first.Rollback(); err != nil {
		t.Fatal(err)
	}
	second, err := ws.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Rollback()
	check(second, false)
	if proof.snapshot == old || proof.snapshot.epoch != old.epoch || proof.tx != second {
		t.Fatal("equal-epoch transactions shared admission")
	}
	// A real ownership change must invalidate the empty closure, including
	// structural descendants, rather than merely passing an epoch-only test.
	if _, err := second.ExecContext(ctx, `UPDATE threads SET body_json=json_set(body_json,'$.pm_actor_id','owner') WHERE id=?`, doc["thread_id"]); err != nil {
		t.Fatal(err)
	}
	check(second, true)
	// Losing epoch metadata disables admission and preserves the full policy.
	if _, err := second.ExecContext(ctx, `DELETE FROM resource_access_epoch WHERE singleton=1`); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := responseMutationRead(admitted, second, query, args); ok {
		t.Fatal("missing epoch admitted a cached proof")
	}
	check(second, true)
}
