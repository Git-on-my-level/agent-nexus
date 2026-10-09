package storage

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func scopeMust(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func scopeFixture(t *testing.T) *Workspace {
	t.Helper()
	w, err := InitializeWorkspace(context.Background(), t.TempDir())
	scopeMust(t, err)
	t.Cleanup(func() { w.Close() })
	tx, err := w.DB().Begin()
	scopeMust(t, err)
	scopeMust(t, InstallScopeMigration(context.Background(), tx))
	scopeMust(t, tx.Commit())
	scopeMust(t, w.EnableScopeServingLease(context.Background()))
	return w
}

func TestScopeFenceOldLoaderRefusesAndCompatibleReopens(t *testing.T) {
	t.Parallel()
	w := scopeFixture(t)
	ctx := context.Background()
	root := w.Layout().RootDir
	scopeMust(t, w.InstallScopeFormatFence(ctx))
	scopeMust(t, w.Close())
	// applyMigrations is the unchanged released loader, including its initial
	// CREATE TABLE IF NOT EXISTS and SELECT against schema_migrations.
	old, err := sql.Open("sqlite", sqliteDSN(w.Layout().DatabasePath))
	scopeMust(t, err)
	for i := 0; i < 2; i++ {
		if err := applyMigrations(ctx, old); err == nil {
			t.Fatal("released loader accepted fenced ledger")
		}
	}
	var count int
	scopeMust(t, old.QueryRow(`SELECT count(*) FROM scope_schema_migrations`).Scan(&count))
	if count != len(migrations) {
		t.Fatal("fence lost released ledger entries")
	}
	scopeMust(t, old.Close())
	w2, err := InitializeWorkspace(ctx, root)
	scopeMust(t, err)
	scopeMust(t, w2.Ping(ctx))
	scopeMust(t, w2.Close())
	// Actual fresh compatible process, not just another connection.
	childCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(childCtx, os.Args[0], "-test.run=^TestScopeCompatibleReopenChild$", "-test.v")
	cmd.Env = append(os.Environ(), "ANX_SCOPE_REOPEN_ROOT="+root)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("compatible child: %v %s", err, out)
	}
	t.Logf("compatible subprocess: %s", out)
}

// Release gate: point this at an independently built released schema-63 core.
// The unchanged-loader regression above always runs; this full executable
// check additionally proves refusal through bootstrap, before listen.
func TestScopeHistoricalBinaryRefusesFence(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("historical executable release gate")
	}
	binary := os.Getenv("ANX_SCOPE_OLD_BINARY")
	if binary == "" {
		t.Skip("set ANX_SCOPE_OLD_BINARY to a schema-63 core executable")
	}
	w := scopeFixture(t)
	scopeMust(t, w.InstallScopeFormatFence(context.Background()))
	root := w.Layout().RootDir
	scopeMust(t, w.Close())
	schema, err := filepath.Abs("../../../contracts/anx-schema.yaml")
	scopeMust(t, err)
	for attempt := 0; attempt < 2; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cmd := exec.CommandContext(ctx, binary, "--workspace-root", root, "--schema-path", schema, "--host", "127.0.0.1", "--port", "0")
		out, err := cmd.CombinedOutput()
		cancel()
		if err == nil || !strings.Contains(string(out), "no such function: anx_requires_scope_format_v1") {
			t.Fatalf("historical binary didn't refuse at storage initialization: %v %s", err, out)
		}
	}
	compatible, err := InitializeWorkspace(context.Background(), root)
	scopeMust(t, err)
	scopeMust(t, compatible.Close())
}

func TestScopeCompatibleReopenChild(t *testing.T) {
	t.Parallel()
	root := os.Getenv("ANX_SCOPE_REOPEN_ROOT")
	if root == "" {
		t.Skip("subprocess only")
	}
	w, err := InitializeWorkspace(context.Background(), root)
	scopeMust(t, err)
	defer w.Close()
	var format int
	var reader string
	scopeMust(t, w.DB().QueryRow(`SELECT format,reader FROM scope_format_state`).Scan(&format, &reader))
	if format != 1 || reader != "legacy" {
		t.Fatal(format, reader)
	}
	if _, err = w.DB().Query(`SELECT version FROM schema_migrations`); err == nil {
		t.Fatal("reopen neutralized fence")
	}
	scopeMust(t, w.Ping(context.Background()))
}

func TestScopeFenceRollbackAndLedgerTampering(t *testing.T) {
	t.Parallel()
	w := scopeFixture(t)
	ctx := context.Background()
	_, err := w.DB().Exec(`CREATE TRIGGER fail_scope_fence BEFORE INSERT ON scope_format_state BEGIN SELECT RAISE(ABORT,'injected fence failure'); END`)
	scopeMust(t, err)
	if err := w.InstallScopeFormatFence(ctx); err == nil {
		t.Fatal("fence unexpectedly succeeded")
	}
	var count int
	scopeMust(t, w.DB().QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&count))
	if count != len(migrations) {
		t.Fatal(count)
	}
	_, err = w.DB().Exec(`DROP TRIGGER fail_scope_fence`)
	scopeMust(t, err)
	scopeMust(t, w.InstallScopeFormatFence(ctx))
	_, err = w.DB().Exec(`UPDATE scope_schema_migrations SET applied_at='tampered' WHERE version=1`)
	scopeMust(t, err)
	root := w.Layout().RootDir
	scopeMust(t, w.Close())
	if reopened, err := InitializeWorkspace(ctx, root); err == nil {
		reopened.Close()
		t.Fatal("tampered ledger accepted")
	}
}

func TestScopeUnsupportedReaderAndMalformedFenceRefuse(t *testing.T) {
	t.Parallel()
	for _, statement := range []string{
		`DROP VIEW schema_migrations;DROP TABLE scope_schema_migrations;CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,applied_at TEXT NOT NULL)`,
		`DROP VIEW schema_migrations;CREATE VIEW schema_migrations AS SELECT * FROM scope_schema_migrations`,
		`DROP TABLE scope_format_state`,
		`PRAGMA ignore_check_constraints=ON;UPDATE scope_format_state SET reader='scopes'`,
	} {
		t.Run(statement, func(t *testing.T) {
			w := scopeFixture(t)
			ctx := context.Background()
			scopeMust(t, w.InstallScopeFormatFence(ctx))
			_, err := w.DB().Exec(statement)
			scopeMust(t, err)
			root := w.Layout().RootDir
			scopeMust(t, w.Close())
			if reopened, err := InitializeWorkspace(ctx, root); err == nil {
				reopened.Close()
				t.Fatal("unsupported state reopened")
			}
		})
	}
}

func TestScopeServingLockAndCrashRelease(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("process crash integration")
	}
	w := scopeFixture(t)
	root := w.Layout().RootDir
	if other, err := InitializeWorkspace(context.Background(), root); err == nil {
		other.Close()
		t.Fatal("two servers admitted")
	}
	scopeMust(t, w.Close())
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestScopeServingCrashChild$")
	cmd.Env = append(os.Environ(), "ANX_SCOPE_LOCK_ROOT="+root)
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 24 {
		t.Fatalf("crash child %v %s", err, out)
	}
	w2, err := InitializeWorkspace(context.Background(), root)
	scopeMust(t, err)
	defer w2.Close()
}
func TestScopeServingCrashChild(t *testing.T) {
	t.Parallel()
	root := os.Getenv("ANX_SCOPE_LOCK_ROOT")
	if root == "" {
		t.Skip("subprocess only")
	}
	_, err := InitializeWorkspace(context.Background(), root)
	scopeMust(t, err)
	os.Exit(24)
}

func TestScopeServingLeaseRetainedByOutstandingConnections(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"transaction", "connection"} {
		t.Run(kind, func(t *testing.T) {
			w := scopeFixture(t)
			root := w.Layout().RootDir
			ctx := context.Background()
			var finish func() error
			if kind == "transaction" {
				tx, err := w.DB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
				scopeMust(t, err)
				finish = func() error {
					_, err := tx.Exec(`INSERT INTO actors(id,display_name,created_at) VALUES('old-transaction','Old connection','now')`)
					if err != nil {
						return err
					}
					return tx.Commit()
				}
			} else {
				conn, err := w.DB().Conn(ctx)
				scopeMust(t, err)
				finish = func() error {
					_, err := conn.ExecContext(ctx, `INSERT INTO actors(id,display_name,created_at) VALUES('old-connection','Old connection','now')`)
					if err != nil {
						return err
					}
					return conn.Close()
				}
			}
			scopeMust(t, w.Close())
			if admitted, err := InitializeWorkspace(ctx, root); err == nil {
				admitted.Close()
				t.Fatal("serving admitted while an old connection could still write")
			}
			scopeMust(t, finish())
			admitted, err := InitializeWorkspace(ctx, root)
			scopeMust(t, err)
			scopeMust(t, admitted.Close())
		})
	}
}
