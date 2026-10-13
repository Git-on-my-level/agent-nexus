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

// This measures the actual expand-only implementation: total file size MUST
// grow while both ledgers are retained. Component bytes are not reclaimed bytes.
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
	var rawTargetBytes int64
	if err := w.db.QueryRow(`SELECT sum(pgsize) FROM dbstat WHERE name='idx_resource_access_edges_target'`).Scan(&rawTargetBytes); err != nil {
		t.Fatal(err)
	}
	t.Logf("raw_target_index_bytes=%d", rawTargetBytes)
	before := size()
	start := time.Now()
	if err := w.InitializeCompactAccessShadow(ctx); err != nil {
		t.Fatal(err)
	}
	t.Logf("metadata_install=%s", time.Since(start))
	var maxBatch time.Duration
	batches := 0
	start = time.Now()
	for {
		batchStart := time.Now()
		done, err := w.MaintainCompactAccessBatch(ctx)
		elapsed := time.Since(batchStart)
		maxBatch = max(maxBatch, elapsed)
		if err != nil {
			t.Fatal(err)
		}
		batches++
		if done {
			break
		}
	}
	t.Logf("backfill=%s batches=%d max_batch=%s", time.Since(start), batches, maxBatch)
	start = time.Now()
	result, err := w.VerifyCompactAccessShadow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	after := size()
	var oldBytes, compactBytes int64
	if err := w.db.QueryRow(`SELECT sum(pgsize) FROM dbstat WHERE name IN ('resource_access_exact_edges','idx_access_exact_edges_target')`).Scan(&oldBytes); err != nil {
		t.Fatal(err)
	}
	if err := w.db.QueryRow(`SELECT sum(pgsize) FROM dbstat WHERE name IN ('resource_access_compact_edges','idx_access_compact_target')`).Scan(&compactBytes); err != nil {
		t.Fatal(err)
	}
	t.Logf("verified_rows=%d verification=%s epoch=%d old_component_bytes=%d compact_component_bytes=%d db_file_before=%d db_file_after=%d reclaimed_bytes=0", result.Rows, time.Since(start), result.Epoch, oldBytes, compactBytes, before, after)
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
	cutoverStarted := time.Now()
	if err = w.CutoverCompactAccess(ctx, proof); err != nil {
		t.Fatal(err)
	}
	cutoverElapsed := time.Since(cutoverStarted)
	afterCutover := size()
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
	t.Logf("db_file_before=%d after_cutover=%d after_reclamation=%d saved_bytes=%d cutover=%s legacy_rollback_retained=true", before, afterCutover, info.Size(), before-info.Size(), cutoverElapsed)
	if info.Size() >= before {
		t.Fatal("no real file saving")
	}
	if result.Rows < 100000 || compactBytes >= oldBytes || after < before {
		t.Fatal("unexpected dense fixture size/cardinality")
	}
}
