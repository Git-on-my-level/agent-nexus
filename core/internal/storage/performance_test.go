package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

func TestMigrationProgressSignalsBeforeAndDuringWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	messages := make(chan string, 10)
	finish := migrationProgress(ctx, 60, time.Millisecond, func(f string, a ...any) { messages <- fmt.Sprintf(f, a...) })
	if got := <-messages; !strings.HasPrefix(got, "migration_started") {
		t.Fatal(got)
	}
	select {
	case got := <-messages:
		if !strings.HasPrefix(got, "migration_progress") {
			t.Fatal(got)
		}
	case <-time.After(time.Second):
		t.Fatal("no progress while migration runs")
	}
	finish()
	for {
		select {
		case got := <-messages:
			if strings.HasPrefix(got, "migration_finished") {
				return
			}
		default:
			t.Fatal("missing finish signal")
		}
	}
}

func TestPerformanceStartupAndMigrations(t *testing.T) {
	if testing.Short() || os.Getenv("ANX_PERFORMANCE_TEST") != "1" {
		t.Skip("performance CI tier")
	}
	root := t.TempDir()
	layout := NewLayout(root)
	if err := ensureLayout(layout); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", sqliteDSN(layout.DatabasePath))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, createMigrationsTableSQL); err != nil {
		t.Fatal(err)
	}
	// Construct a genuine schema-58 database, not a current schema with ledger
	// rows deleted. This exercises both reconciliation and blob backfills on
	// upgrade. New migrations automatically join the measured upgrade path.
	for _, m := range migrations {
		if m.Version > 58 {
			break
		}
		if err := applyMigration(ctx, db, m); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	if err := perfguard.Seed(ctx, db, layout.ArtifactContentDir, "scale-owner", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"upgrade", "warm-open"} {
		if !t.Run(name, func(t *testing.T) {
			budget := 5 * time.Second
			if name == "upgrade" {
				budget = 90 * time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), budget)
			defer cancel()
			start := time.Now()
			w, err := InitializeWorkspace(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			defer w.Close()
			store := primitives.NewStore(w.DB(), blob.NewFilesystemBackend(layout.ArtifactContentDir), layout.ArtifactContentDir)
			if err := store.BackfillArtifactAccess(ctx); err != nil {
				t.Fatal(err)
			}
			if err := w.Ping(ctx); err != nil {
				t.Fatal(err)
			}
			elapsed := time.Since(start)
			t.Logf("database and blob initialization ready in %v (budget %v)", elapsed, budget)
			if elapsed > budget {
				t.Errorf("readiness exceeded %v", budget)
			}
		}) {
			t.Fatal("upgrade must succeed before measuring warm readiness")
		}
	}
}
