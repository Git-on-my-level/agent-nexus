package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func compactCertify(t *testing.T, w *Workspace) CompactAccessVerification {
	t.Helper()
	ctx := context.Background()
	if err := w.InitializeCompactAccessShadow(ctx); err != nil {
		t.Fatal(err)
	}
	compactFinish(t, w)
	for done := false; !done; {
		var err error
		done, err = w.MaintainStableAccessBatch(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	proof, err := w.VerifyCompactAccessCutover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return proof
}

func TestCompactReclamationRequiresStoppedSource(t *testing.T) {
	w := compactFixture(t, 20)
	ctx := context.Background()
	proof := compactCertify(t, w)
	if err := w.CutoverCompactAccess(ctx, proof); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "reclaimed.sqlite")
	if err := ReclaimCompactAccessOffline(ctx, w.Layout().DatabasePath, path); err == nil {
		t.Fatal("accepted live source")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed copy retained: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ReclaimCompactAccessOffline(ctx, w.Layout().DatabasePath, path); err != nil {
		t.Fatal(err)
	}
	if err := ReclaimCompactAccessOffline(ctx, w.Layout().DatabasePath, path); err == nil {
		t.Fatal("overwrote an existing copy")
	}
}

func TestCompactAccessCutoverStableIdentityAndRollback(t *testing.T) {
	w := compactFixture(t, 450)
	ctx := context.Background()
	proof := compactCertify(t, w)
	shadowOnly, err := w.VerifyCompactAccessShadow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = w.CutoverCompactAccess(ctx, shadowOnly); err == nil {
		t.Fatal("accepted partial certification")
	}
	compactExec(t, w, `UPDATE resource_access_edges SET target_ref='document:after-proof' WHERE rowid=1`)
	if err := w.CutoverCompactAccess(ctx, proof); !errors.Is(err, ErrCompactAccessChanged) {
		t.Fatalf("stale proof accepted: %v", err)
	}
	proof = compactCertify(t, w)
	if err := w.CutoverCompactAccess(ctx, proof); err != nil {
		t.Fatal(err)
	}
	var mode, stable, pk, idx int
	if err := w.db.QueryRow(`SELECT reader,stable FROM resource_access_compact_control`).Scan(&mode, &stable); err != nil || mode != 1 || stable != 1 {
		t.Fatalf("mode: %d %d %v", mode, stable, err)
	}
	if err := w.db.QueryRow(`SELECT pk FROM pragma_table_info('resource_access_edges') WHERE name='edge_id'`).Scan(&pk); err != nil || pk != 1 {
		t.Fatalf("unstable key: %d %v", pk, err)
	}
	if err := w.db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name='idx_resource_access_edges_target'`).Scan(&idx); err != nil || idx != 0 {
		t.Fatalf("redundant index retained: %d %v", idx, err)
	}
	for _, q := range []string{
		`UPDATE resource_access_edges SET target_ref='document:post-cutover' WHERE edge_id=1`,
		`DELETE FROM resource_access_edges WHERE edge_id=2`,
		`INSERT OR REPLACE INTO resource_access_edges(edge_id,source_kind,source_id,target_ref) VALUES(1,'card','replacement','document:replace')`,
		`INSERT OR REPLACE INTO resource_access_edges(source_kind,source_id,target_ref) VALUES('card','replacement','document:replace')`,
		`UPDATE OR REPLACE resource_access_edges SET source_id='replacement',target_ref='document:replace' WHERE edge_id=3`,
		`INSERT INTO resource_access_edges(source_kind,source_id,target_ref) VALUES('card','new-after-cutover','document:new')`,
		`INSERT INTO resource_access_series_refs(series,labels,target_ref) VALUES('test','{}','document:series')`,
	} {
		compactExec(t, w, q)
	}
	if _, err := w.VerifyCompactAccessCutover(ctx); err != nil {
		t.Fatal(err)
	}
	if err := w.RollbackCompactAccess(ctx); err != nil {
		t.Fatal(err)
	}
	if err := w.db.QueryRow(`SELECT reader FROM resource_access_compact_control`).Scan(&mode); err != nil || mode != 2 {
		t.Fatal(mode, err)
	}
	if err := w.CutoverCompactAccess(ctx, proof); err == nil {
		t.Fatal("overrode operator rollback")
	}
	if _, err := w.VerifyCompactAccessCutover(ctx); err != nil {
		t.Fatal(err)
	}
}
