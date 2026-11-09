package storage

import (
	"context"
	"os"
	"testing"
	"time"

	"agent-nexus-core/internal/testutil/perfguard"
)

// Measure only the migration introduced here, starting from a genuine populated
// released schema, independently of the legacy schema-58 readiness gate. That
// gate and its existing budget are unchanged.
func TestPerformanceWatcherHealthReleasedUpgrade(t *testing.T) {
	if testing.Short() || os.Getenv("ANX_PERFORMANCE_TEST") != "1" {
		t.Skip("performance CI tier")
	}
	root := t.TempDir()
	db := mainHistoryDatabase(t, root, 58)
	layout := NewLayout(root)
	if err := ensureLayout(layout); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := perfguard.Seed(ctx, db, layout.ArtifactContentDir, "scale-owner", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	// Follow the standard startup fixture's genuine history. Seeding directly
	// into 135 would introduce pre-130-shaped revisions after their content
	// migration and fail its recorded invariant, independently of this change.
	for _, m := range migrations {
		if m.Version > 58 && m.Version <= 135 {
			if err := applyMigration(ctx, db, m); err != nil {
				db.Close()
				t.Fatal(err)
			}
		}
	}
	// Supplement the standard corpus with an annotation and live + archived
	// receipt for every card; the standard fixture contains no run_recorded.
	for _, q := range []string{
		`INSERT INTO work_metadata(card_id,authority,metadata_json,updated_at,updated_by) SELECT id,'nexus','{"cadence":{"every_seconds":60}}','2026-01-01T00:00:00Z','scale-owner' FROM cards WHERE id LIKE 'scale-card-%'`,
		`INSERT INTO events(id,handle,type,ts,actor_id,thread_id,refs_json,payload_json) SELECT 'watcher-live-'||id,'watcher-live-'||id,'run_recorded','2026-01-01T00:00:00Z','scale-owner',thread_id,json_array('card:'||id),json_object('subject_ref','card:'||id,'payload',json_object('run_id','live','outcome','acted')) FROM cards WHERE id LIKE 'scale-card-%'`,
		`INSERT INTO events(id,handle,type,ts,actor_id,thread_id,refs_json,payload_json,archived_at) SELECT 'watcher-archived-'||id,'watcher-archived-'||id,'run_recorded','2026-01-02T00:00:00Z','scale-owner',thread_id,json_array('card:'||id),json_object('subject_ref','card:'||id,'payload',json_object('run_id','archived','outcome','acted')),'2026-01-03T00:00:00Z' FROM cards WHERE id LIKE 'scale-card-%'`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"135-to-136", "warm-open"} {
		budget := 5 * time.Second
		if phase == "135-to-136" {
			budget = performanceUpgradeBudget(t)
		}
		bounded, cancel := context.WithTimeout(ctx, budget)
		start := time.Now()
		ws, err := InitializeWorkspace(bounded, root)
		elapsed := time.Since(start)
		cancel()
		if err != nil {
			t.Fatal(phase, err)
		}
		var candidates, receipts, effects, indexes, version int
		if err = ws.DB().QueryRow(`SELECT (SELECT COUNT(*) FROM watcher_loop_candidates),(SELECT COUNT(*) FROM watcher_run_positions),(SELECT COUNT(*) FROM watcher_effect_positions),(SELECT COUNT(*) FROM sqlite_master WHERE name IN('idx_work_metadata_health','idx_watcher_loop_runs','idx_watcher_loop_effects')),(SELECT MAX(version) FROM schema_migrations)`).Scan(&candidates, &receipts, &effects, &indexes, &version); err != nil || candidates != perfguard.Rows || receipts != perfguard.Rows || effects != perfguard.Rows || indexes != 0 || version != 136 {
			ws.Close()
			t.Fatal(candidates, receipts, effects, indexes, version, err)
		}
		if err = ws.Close(); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s ready=%v budget=%v corpus=%d cards/annotations, %d live + %d archived receipts; no new canonical expression indexes", phase, elapsed, budget, candidates, receipts, perfguard.Rows)
		if elapsed > budget {
			t.Fatal("readiness exceeded", budget)
		}
	}
}
