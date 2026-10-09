package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

func TestMigrationProgressSignalsBeforeAndDuringWork(t *testing.T) {
	// Serial: this file mutates process-wide environment, logging or counters.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var messages []string
	progress := make(chan struct{}, 1)
	finish := migrationProgress(ctx, 60, time.Millisecond, func(f string, a ...any) {
		messages = append(messages, fmt.Sprintf(f, a...))
		if strings.HasPrefix(f, "migration_progress") {
			select {
			case progress <- struct{}{}:
			default:
			}
		}
	})
	select {
	case <-progress:
	case <-time.After(time.Second):
		finish()
		t.Fatal("no progress while migration runs")
	}
	// finish joins the ticker before messages is read. The callback must never
	// block when a busy test runner lets several progress ticks accumulate.
	finish()
	if len(messages) < 3 || !strings.HasPrefix(messages[0], "migration_started") || !strings.HasPrefix(messages[len(messages)-1], "migration_finished") {
		t.Fatalf("unexpected progress signals: %v", messages)
	}
}

type migrationLogWriter func([]byte) (int, error)

func (w migrationLogWriter) Write(p []byte) (int, error) { return w(p) }

func TestFencedMigrationSignalsProgressBeforeReadiness(t *testing.T) {
	// Serial: this file mutates process-wide environment, logging or counters.
	w := scopeFixture(t)
	root := w.Layout().RootDir
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	scopeMust(t, w.InstallScopeFormatFence(ctx))
	scopeMust(t, w.Close())
	progress := make(chan struct{}, 1)
	var mu sync.Mutex
	var messages strings.Builder
	originalOutput := log.Writer()
	log.SetOutput(migrationLogWriter(func(p []byte) (int, error) {
		mu.Lock()
		messages.Write(p)
		mu.Unlock()
		if strings.Contains(string(p), "migration_progress version=10000") {
			select {
			case progress <- struct{}{}:
			default:
			}
		}
		return len(p), nil
	}))
	defer log.SetOutput(originalOutput)
	originalMigrations := migrations
	defer func() { migrations = originalMigrations }()
	migrations = append(append([]migration(nil), migrations...), migration{
		Version:    10000,
		Statements: []string{`CREATE TABLE performance_fenced_progress(id INTEGER PRIMARY KEY)`},
		AfterApply: func(ctx context.Context, tx *sql.Tx) error {
			select {
			case <-progress:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	})
	reopened, err := InitializeWorkspace(ctx, root)
	scopeMust(t, err)
	defer reopened.Close()
	mu.Lock()
	captured := messages.String()
	mu.Unlock()
	for _, signal := range []string{"migration_started", "migration_progress", "migration_finished"} {
		if !strings.Contains(captured, signal+" version=10000") {
			t.Fatalf("readiness returned without %s: %s", signal, captured)
		}
	}
	var recorded int
	scopeMust(t, reopened.DB().QueryRowContext(ctx, `SELECT count(*) FROM scope_schema_migrations WHERE version=10000`).Scan(&recorded))
	if recorded != 1 {
		t.Fatal("fenced migration did not commit its ledger entry")
	}
}

type startupBaseline struct {
	ThroughVersion int               `json:"through_version"`
	UpgradeMS      int               `json:"upgrade_ms"`
	SourceHashes   map[string]string `json:"source_sha256"`
	Issue          string            `json:"issue"`
	IssueURL       string            `json:"issue_url"`
	Reason         string            `json:"reason"`
}

// Pin this legacy-upgrade exception to all code that installs/reconciles its
// reference indexes and blob manifests. New migrations or changes to that work
// expire it instead of inheriting the relaxed deadline.
func startupSourceHashes() (map[string]string, error) {
	paths := []string{"../primitives/access_blobs.go", "../primitives/store.go", "../testutil/perfguard/fixture.go"}
	for _, dir := range []string{".", "../resourceaccess", "../blob"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go") {
				paths = append(paths, filepath.Join(dir, e.Name()))
			}
		}
	}
	hashes := map[string]string{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		hashes[p] = fmt.Sprintf("%x", sha256.Sum256(b))
	}
	return hashes, nil
}

func performanceUpgradeBudget(t *testing.T) time.Duration {
	t.Helper()
	raw, err := os.ReadFile("testdata/performance_startup_allowlist.json")
	if err != nil {
		t.Fatal(err)
	}
	var e *startupBaseline
	if err = json.Unmarshal(raw, &e); err != nil {
		t.Fatal(err)
	}
	if e == nil {
		return 90 * time.Second
	}
	if e.ThroughVersion != migrations[len(migrations)-1].Version || e.UpgradeMS <= 90000 || e.UpgradeMS > 900000 || e.Issue == "" || e.IssueURL == "" || len(e.Reason) < 40 {
		t.Fatal("invalid or expired legacy-upgrade baseline; use the 90-second default or review its linked P1")
	}
	actual, err := startupSourceHashes()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, e.SourceHashes) {
		t.Fatal("startup code changed: legacy-upgrade baseline expired; re-review the P1 instead of silently inheriting its budget")
	}
	t.Logf("legacy upgrade exception %s: %s", e.Issue, e.Reason)
	return time.Duration(e.UpgradeMS) * time.Millisecond
}

func TestPerformanceStartupBudgetInventory(t *testing.T) {
	// Serial: this file mutates process-wide environment, logging or counters.
	performanceUpgradeBudget(t)
}

func TestPerformanceStartupAndMigrations(t *testing.T) {
	// Serial: this file mutates process-wide environment, logging or counters.
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
	// rows deleted. This exercises production reconciliation on upgrade. New
	// migrations automatically join the measured upgrade path. Historical blob
	// maintenance is explicit and is not part of production readiness.
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
				budget = performanceUpgradeBudget(t)
			}
			ctx, cancel := context.WithTimeout(context.Background(), budget)
			defer cancel()
			start := time.Now()
			w, err := InitializeWorkspace(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			defer w.Close()
			_ = primitives.NewStore(w.DB(), blob.NewFilesystemBackend(layout.ArtifactContentDir), layout.ArtifactContentDir)
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
