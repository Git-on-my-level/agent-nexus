package storage

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agent-nexus-core/internal/testutil/perfguard"
)

// This measures the post-contraction schema: the compact ledger is the only
// exact-edge ledger, and the offline reclamation step shrinks the file. The
// legacy exact ledger and admission mirror are already gone after migration 150.
func TestPerformanceCompactAccessShadow(t *testing.T) {
	if testing.Short() || os.Getenv("ANX_PERFORMANCE_TEST") != "1" {
		t.Skip("performance CI tier")
	}
	w := compactFixture(t, 0)
	ctx := context.Background()
	if err := perfguard.Seed(ctx, w.db, w.Layout().ArtifactContentDir, "scale-owner", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	size := func() int64 {
		t.Helper()
		var busy, logPages, checkpointed int
		if err := w.db.QueryRow(`PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logPages, &checkpointed); err != nil || busy != 0 {
			t.Fatalf("checkpoint: %d %v", busy, err)
		}
		info, err := os.Stat(w.Layout().DatabasePath)
		if err != nil {
			t.Fatal(err)
		}
		return info.Size()
	}
	before := size()
	result, err := w.VerifyCompactAccessShadow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var compactBytes int64
	if err := w.db.QueryRow(`SELECT sum(pgsize) FROM dbstat WHERE name IN ('resource_access_compact_edges','idx_access_compact_target')`).Scan(&compactBytes); err != nil {
		t.Fatal(err)
	}
	t.Logf("verified_rows=%d compact_component_bytes=%d db_file_before=%d", result.Rows, compactBytes, before)
	// A new file is prepared during a maintenance window; no serving database is
	// overwritten. The operator must stop writers before replacing the old file.
	destination := filepath.Join(t.TempDir(), "reclaimed.sqlite")
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	if err = ReclaimCompactAccessOffline(ctx, w.Layout().DatabasePath, destination); err != nil {
		t.Fatal(err)
	}
	copyDB, err := sql.Open("sqlite", sqliteDSN(destination))
	if err != nil {
		t.Fatal(err)
	}
	defer copyDB.Close()
	copyWorkspace := &Workspace{db: copyDB}
	if verified, err := copyWorkspace.VerifyCompactAccessCutover(ctx); err != nil || verified.Rows != result.Rows {
		t.Fatalf("reclaimed verification: %+v %v", verified, err)
	}
	if _, err = copyDB.ExecContext(ctx, `ATTACH DATABASE ? AS original`, w.Layout().DatabasePath); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`SELECT EXISTS(SELECT rowid,source_kind,source_id,CAST(target_ref AS BLOB) FROM original.resource_access_edges EXCEPT SELECT rowid,source_kind,source_id,CAST(target_ref AS BLOB) FROM main.resource_access_edges)`,
		`SELECT EXISTS(SELECT rowid,kind,id FROM original.pm_records EXCEPT SELECT rowid,kind,id FROM main.pm_records)`,
	} {
		var mismatch bool
		if err = copyDB.QueryRowContext(ctx, q).Scan(&mismatch); err != nil || mismatch {
			t.Fatalf("reclamation changed identities: %v %v", mismatch, err)
		}
	}
	var integrity string
	if err = copyDB.QueryRow(`PRAGMA quick_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatal(integrity, err)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("db_file_before=%d after_reclamation=%d saved_bytes=%d", before, info.Size(), before-info.Size())
	if info.Size() >= before {
		t.Fatal("no real file saving")
	}
	if result.Rows < 100000 {
		t.Fatal("unexpected dense fixture cardinality")
	}
}
