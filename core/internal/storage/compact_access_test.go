package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"modernc.org/sqlite"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func compactFixture(t *testing.T, n int) *Workspace {
	t.Helper()
	w, err := InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	compactSeed(t, w, n)
	return w
}

func compactSeed(t *testing.T, w *Workspace, n int) {
	t.Helper()
	tx, err := w.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 1; i <= n; i++ {
		if _, err = tx.Exec(`INSERT INTO resource_access_edges(rowid,source_kind,source_id,target_ref) VALUES(?,'card',?,?)`, i, fmt.Sprintf("source-%09d", i), fmt.Sprintf("document:target-%09d", i%13)); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func compactExec(t *testing.T, w *Workspace, q string) {
	t.Helper()
	if _, err := w.db.Exec(q); err != nil {
		t.Fatal(err)
	}
}

func compactFinish(t *testing.T, w *Workspace) CompactAccessVerification {
	t.Helper()
	for i := 0; i < 10000; i++ {
		done, err := w.MaintainCompactAccessBatch(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if done {
			result, err := w.VerifyCompactAccessShadow(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			return result
		}
	}
	t.Fatal("backfill did not finish")
	return CompactAccessVerification{}
}

func TestCompactAccessDefaultOffResumeAndImports(t *testing.T) {
	w := compactFixture(t, 450)
	ctx := context.Background()
	var n int
	if err := w.db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type='trigger' AND name LIKE 'access_compact%'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("default installed shadow: %d %v", n, err)
	}
	if err := w.InitializeCompactAccessShadow(ctx); err != nil {
		t.Fatal(err)
	}
	if err := w.db.QueryRow(`SELECT count(*) FROM resource_access_compact_edges`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("install copied history: %d %v", n, err)
	}
	if _, err := w.VerifyCompactAccessShadow(ctx); err == nil {
		t.Fatal("incomplete backfill verified")
	}
	if done, err := w.MaintainCompactAccessBatch(ctx); err != nil || done {
		t.Fatal(done, err)
	}
	if err := w.db.QueryRow(`SELECT count(*) FROM resource_access_compact_edges`).Scan(&n); err != nil || n != 200 {
		t.Fatalf("unbounded batch: %d %v", n, err)
	}
	root := w.Layout().RootDir
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	w, err = InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err = w.InitializeCompactAccessShadow(ctx); err != nil {
		t.Fatal(err)
	}
	// Canonical writes and imports cover both sides of the committed cursor,
	// signed keys, changed rowids, deletes and key reuse.
	for _, q := range []string{
		`UPDATE resource_access_edges SET target_ref='document:edited' WHERE rowid=1`,
		`UPDATE resource_access_edges SET source_id='moved-source' WHERE rowid=449`,
		`UPDATE resource_access_edges SET rowid=-9 WHERE rowid=2`,
		`DELETE FROM resource_access_edges WHERE rowid IN (3,450)`,
		`INSERT INTO resource_access_edges(rowid,source_kind,source_id,target_ref) VALUES(3,'card','reused','document:reused'),(0,'card','zero','document:zero'),(-1,'card','negative','document:negative'),(9223372036854775807,'card','maximum','document:maximum')`,
	} {
		compactExec(t, w, q)
	}
	result := compactFinish(t, w)
	if result.Rows != 452 {
		t.Fatalf("unexpected count: %+v", result)
	}
	// Subsequent writes remain atomic after the worker finishes or is disabled.
	compactExec(t, w, `DELETE FROM resource_access_edges WHERE rowid=9223372036854775807`)
	compactExec(t, w, `INSERT INTO resource_access_edges(rowid,source_kind,source_id,target_ref) VALUES(-100,'card','late','DOCUMENT:MiXeD')`)
	if _, err = w.VerifyCompactAccessShadow(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestCompactAccessRollbackAndCorruption(t *testing.T) {
	w := compactFixture(t, 3)
	ctx := context.Background()
	if err := w.InitializeCompactAccessShadow(ctx); err != nil {
		t.Fatal(err)
	}
	compactExec(t, w, `CREATE TRIGGER fail_compact BEFORE UPDATE ON resource_access_compact_job BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if _, err := w.MaintainCompactAccessBatch(ctx); err == nil {
		t.Fatal("injected checkpoint failure did not abort")
	}
	var n int
	if err := w.db.QueryRow(`SELECT count(*) FROM resource_access_compact_edges`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("failed checkpoint committed rows: %d %v", n, err)
	}
	compactExec(t, w, `DROP TRIGGER fail_compact`)
	compactFinish(t, w)
	for _, q := range []string{
		`DELETE FROM resource_access_compact_edges WHERE edge_id=1`,
		`UPDATE resource_access_compact_edges SET target_key=zeroblob(32) WHERE edge_id=1`,
		`INSERT INTO resource_access_compact_edges VALUES(999,zeroblob(32))`,
		`UPDATE resource_access_exact_edges SET source_id='corrupt' WHERE edge_id=1`,
		// The mirror agrees, but both differ from the independently hashed raw row.
		`UPDATE resource_access_exact_edges SET target_key=lower(hex(zeroblob(32))) WHERE edge_id=1`,
		`DELETE FROM resource_access_exact_edges WHERE edge_id=1`,
		// Legacy raw REPLACE can retain a stale exact row (recursive_triggers=OFF).
		// It must be rejected, never silently certified by a matching mirror.
		`INSERT OR REPLACE INTO resource_access_edges(rowid,source_kind,source_id,target_ref) VALUES(1,'card','replacement','document:replacement')`,
	} {
		tx, err := w.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(q); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if _, err = w.VerifyCompactAccessShadow(ctx); !errors.Is(err, ErrCompactAccessMismatch) {
			t.Fatalf("corruption accepted: %s: %v", q, err)
		}
		// Repair the disposable fixture from its raw ledger, preserving triggers.
		compactExec(t, w, `DELETE FROM resource_access_compact_edges`)
		compactExec(t, w, `DELETE FROM resource_access_exact_edges`)
		compactExec(t, w, `INSERT INTO resource_access_exact_edges SELECT rowid,source_kind,source_id,anx_resource_atom_key(CAST(target_ref AS BLOB)) FROM resource_access_edges`)
		if _, err = w.VerifyCompactAccessShadow(ctx); err != nil {
			t.Fatal(err)
		}
	}
	// Malformed imported digests cannot partially mutate the legacy ledger.
	if _, err := w.db.Exec(`UPDATE resource_access_exact_edges SET target_key='bad' WHERE edge_id=1`); err == nil {
		t.Fatal("malformed digest admitted")
	}
	if _, err := w.VerifyCompactAccessShadow(ctx); err != nil {
		t.Fatal(err)
	}
	// Operator rollback removes only opt-in artifacts; old rows remain exact.
	compactExec(t, w, "BEGIN IMMEDIATE;"+strings.Join(compactAccessRollbackSQL, ";")+";COMMIT;")
	compactExec(t, w, `UPDATE resource_access_edges SET target_ref='document:after-rollback' WHERE rowid=1`)
	if err := w.InitializeCompactAccessShadow(ctx); err != nil {
		t.Fatal(err)
	}
	compactFinish(t, w)
}

// Kept in the test: this is the documented operator rollback, not an HTTP API.
var compactAccessRollbackSQL = []string{
	`DROP TRIGGER access_compact_insert`, `DROP TRIGGER access_compact_update`, `DROP TRIGGER access_compact_delete`,
	`DROP VIEW resource_access_compact_reconstructed`, `DROP TABLE resource_access_compact_edges`, `DROP TABLE resource_access_compact_job`,
	`DROP TRIGGER access_stable_insert`, `DROP TRIGGER access_stable_update`, `DROP TRIGGER access_stable_delete`, `DROP TABLE resource_access_stable_edges`,
}

func TestCompactAccessYieldsAndUsesKeyset(t *testing.T) {
	w := compactFixture(t, 1)
	ctx := context.Background()
	if err := w.InitializeCompactAccessShadow(ctx); err != nil {
		t.Fatal(err)
	}
	held, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback()
	started := time.Now()
	if _, err = w.MaintainCompactAccessBatch(ctx); err == nil || time.Since(started) > 500*time.Millisecond {
		t.Fatalf("worker failed to yield: %s %v", time.Since(started), err)
	}
	held.Rollback()
	var timeout int
	if err = w.db.QueryRow(`PRAGMA busy_timeout`).Scan(&timeout); err != nil || timeout != 20000 {
		t.Fatalf("serving timeout changed: %d %v", timeout, err)
	}
	for _, q := range []string{
		`SELECT edge_id FROM resource_access_exact_edges WHERE edge_id>1000000 ORDER BY edge_id LIMIT 200`,
		`SELECT edge_id,unhex(target_key) FROM resource_access_exact_edges WHERE edge_id BETWEEN 1000000 AND 1000200`,
		`SELECT e.source_kind,e.source_id FROM resource_access_compact_edges c JOIN resource_access_edges e ON e.rowid=c.edge_id WHERE c.target_key=zeroblob(32)`,
	} {
		rows, err := w.db.Query(`EXPLAIN QUERY PLAN ` + q)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			t.Log(detail)
			if !strings.HasPrefix(detail, "SEARCH ") || strings.Contains(detail, "AUTOMATIC") {
				t.Fatalf("unbounded lookup: %s: %s", q, detail)
			}
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
	}
}

func TestCompactAccessSignedInitialKeysAndConcurrentWrites(t *testing.T) {
	w := compactFixture(t, 450)
	compactExec(t, w, `INSERT INTO resource_access_edges(rowid,source_kind,source_id,target_ref) VALUES(-9223372036854775808,'card','minimum','document:min'),(0,'card','zero','document:zero')`)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// The real worker installs metadata, backfills, verifies, and exits while
	// serving writes and readers continue through independent WAL connections.
	finished := make(chan struct{})
	var verification CompactAccessVerification
	var verifyErr error
	go func() {
		defer close(finished)
		w.RunCompactAccessMigration(ctx, func(result CompactAccessVerification, err error) {
			verification, verifyErr = result, err
		})
	}()
	defer func() { cancel(); <-finished }()
	for i := 0; i < 80; i++ {
		time.Sleep(5 * time.Millisecond)
		if _, err := w.db.ExecContext(ctx, `UPDATE resource_access_edges SET target_ref=? WHERE rowid=?`, fmt.Sprintf("document:concurrent-%d", i), 1+i%450); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := w.db.QueryRowContext(ctx, `SELECT count(*) FROM resource_access_exact_edges`).Scan(&n); err != nil || n != 452 {
			t.Fatalf("serving ledger unavailable: %d %v", n, err)
		}
	}
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal("worker failed to finish alongside serving writes")
	}
	if verifyErr != nil || verification.Rows != 452 {
		t.Fatalf("worker verification: %+v %v", verification, verifyErr)
	}
	if result, err := w.VerifyCompactAccessShadow(ctx); err != nil || result.Rows != 452 {
		t.Fatalf("final verification: %+v %v", result, err)
	}
}

func TestCompactAccessProcessCrash(t *testing.T) {
	if root := os.Getenv("ANX_COMPACT_CRASH_ROOT"); root != "" {
		w, err := InitializeWorkspace(context.Background(), root)
		if err != nil {
			t.Fatal(err)
		}
		if err = w.InitializeCompactAccessShadow(context.Background()); err != nil {
			t.Fatal(err)
		}
		switch os.Getenv("ANX_COMPACT_CRASH_PHASE") {
		case "uncommitted":
			tx, err := w.db.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(`INSERT INTO resource_access_compact_edges SELECT edge_id,unhex(target_key) FROM resource_access_exact_edges; UPDATE resource_access_compact_job SET cursor=3`); err != nil {
				t.Fatal(err)
			}
		case "cutover_uncommitted", "cutover_committed":
			proof := compactCertify(t, w)
			if os.Getenv("ANX_COMPACT_CRASH_PHASE") == "cutover_uncommitted" {
				if err = sqlite.RegisterScalarFunction("anx_test_crash_cutover", 0, func(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) { os.Exit(23); return nil, nil }); err != nil {
					t.Fatal(err)
				}
				// Reopen the pool so the newly registered function is available.
				root := w.Layout().RootDir
				w.Close()
				w, err = InitializeWorkspace(context.Background(), root)
				if err != nil {
					t.Fatal(err)
				}
				proof, err = w.VerifyCompactAccessCutover(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				compactExec(t, w, `CREATE TRIGGER crash_cutover AFTER UPDATE OF reader ON resource_access_compact_control WHEN NEW.reader=1 BEGIN SELECT anx_test_crash_cutover(); END`)
			}
			if err = w.CutoverCompactAccess(context.Background(), proof); err != nil {
				t.Fatal(err)
			}
		case "committed":
			if _, err = w.MaintainCompactAccessBatch(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
		os.Exit(23) // No DB.Close, rollback, or Go cleanup; SQLite must recover WAL.
	}
	for _, phase := range []string{"installed", "uncommitted", "committed", "cutover_uncommitted", "cutover_committed"} {
		t.Run(phase, func(t *testing.T) {
			w := compactFixture(t, 3)
			root := w.Layout().RootDir
			w.Close()
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(binary, "-test.run=^TestCompactAccessProcessCrash$")
			cmd.Env = append(os.Environ(), "ANX_COMPACT_CRASH_ROOT="+root, "ANX_COMPACT_CRASH_PHASE="+phase)
			output, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 23 {
				t.Fatalf("crash helper: %v %s", err, output)
			}
			w, err = InitializeWorkspace(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			defer w.Close()
			var cursor sql.NullInt64
			if err = w.db.QueryRow(`SELECT cursor FROM resource_access_compact_job`).Scan(&cursor); err != nil {
				t.Fatal(err)
			}
			if cursor.Valid != (phase == "committed" || strings.HasPrefix(phase, "cutover_")) || (cursor.Valid && cursor.Int64 != 3) {
				t.Fatalf("crash lost checkpoint atomicity: %+v", cursor)
			}
			if strings.HasPrefix(phase, "cutover_") {
				var stable int
				if err = w.db.QueryRow(`SELECT stable FROM resource_access_compact_control`).Scan(&stable); err != nil {
					t.Fatal(err)
				}
				if (stable == 1) != (phase == "cutover_committed") {
					t.Fatalf("partial cutover published: stable=%d", stable)
				}
				compactExec(t, w, `DROP TRIGGER IF EXISTS crash_cutover`)
			}
			if result := compactFinish(t, w); result.Rows != 3 {
				t.Fatal(result)
			}
		})
	}
}
