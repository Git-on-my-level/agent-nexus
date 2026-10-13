package storage

import (
	"context"
	"os"
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
	if result.Rows < 100000 || compactBytes >= oldBytes || after < before {
		t.Fatal("unexpected dense fixture size/cardinality")
	}
}
