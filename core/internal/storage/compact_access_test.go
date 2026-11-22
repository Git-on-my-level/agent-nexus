package storage

import (
	"context"
	"errors"
	"fmt"
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

// compactFixtureAtVersion builds a workspace on a historical schema so tests
// can exercise the pre-150 cutover and rollback paths that migration 150
// retires.
func compactFixtureAtVersion(t *testing.T, n, version int) *Workspace {
	t.Helper()
	root := t.TempDir()
	db := mainHistoryDatabase(t, root, version)
	w := &Workspace{layout: NewLayout(root), db: db, processLock: &scopeProcessLease{refs: 1}}
	if err := w.initializeCompactAccessMetadata(context.Background()); err != nil {
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

// The compact ledger is written directly by the mention triggers after
// migration 150. There is no legacy ledger to copy or resume.
func TestCompactLedgerWritesAndVerification(t *testing.T) {
	w := compactFixture(t, 450)
	ctx := context.Background()
	var n int
	if err := w.db.QueryRow(`SELECT count(*) FROM resource_access_compact_edges`).Scan(&n); err != nil || n != 450 {
		t.Fatalf("compact ledger not written by canonical edges: %d %v", n, err)
	}
	if done, err := w.MaintainCompactAccessBatch(ctx); err != nil || !done {
		t.Fatal(done, err)
	}
	result := compactFinish(t, w)
	if result.Rows != 450 {
		t.Fatalf("unexpected count: %+v", result)
	}
	// Canonical writes remain atomic after the worker is retired.
	for _, q := range []string{
		`UPDATE resource_access_edges SET target_ref='document:edited' WHERE rowid=1`,
		`UPDATE resource_access_edges SET source_id='moved-source' WHERE rowid=449`,
		`UPDATE resource_access_edges SET rowid=-9 WHERE rowid=2`,
		`DELETE FROM resource_access_edges WHERE rowid IN (3,450)`,
		`INSERT INTO resource_access_edges(rowid,source_kind,source_id,target_ref) VALUES(3,'card','reused','document:reused'),(0,'card','zero','document:zero'),(-1,'card','negative','document:negative'),(9223372036854775807,'card','maximum','document:maximum')`,
	} {
		compactExec(t, w, q)
	}
	if _, err := w.VerifyCompactAccessShadow(ctx); err != nil {
		t.Fatal(err)
	}
	compactExec(t, w, `DELETE FROM resource_access_edges WHERE rowid=9223372036854775807`)
	compactExec(t, w, `INSERT INTO resource_access_edges(rowid,source_kind,source_id,target_ref) VALUES(-100,'card','late','DOCUMENT:MiXeD')`)
	if _, err := w.VerifyCompactAccessShadow(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestCompactLedgerCorruptionAndRollback(t *testing.T) {
	w := compactFixture(t, 3)
	ctx := context.Background()
	compactFinish(t, w)
	for _, q := range []string{
		`DELETE FROM resource_access_compact_edges WHERE edge_id=1`,
		`UPDATE resource_access_compact_edges SET target_key=zeroblob(32) WHERE edge_id=1`,
		`INSERT INTO resource_access_compact_edges VALUES(999,zeroblob(32))`,
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
		compactExec(t, w, `INSERT INTO resource_access_compact_edges SELECT rowid,unhex(anx_resource_atom_key(CAST(target_ref AS BLOB))) FROM resource_access_edges`)
		if _, err = w.VerifyCompactAccessShadow(ctx); err != nil {
			t.Fatal(err)
		}
	}
	// Malformed imported digests cannot partially mutate the compact ledger.
	if _, err := w.db.Exec(`UPDATE resource_access_compact_edges SET target_key=zeroblob(31) WHERE edge_id=1`); err == nil {
		t.Fatal("malformed digest admitted")
	}
	if _, err := w.VerifyCompactAccessShadow(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestCompactLedgerYieldsAndUsesKeyset(t *testing.T) {
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
		`SELECT edge_id FROM resource_access_compact_edges WHERE edge_id>1000000 ORDER BY edge_id LIMIT 200`,
		`SELECT edge_id,target_key FROM resource_access_compact_edges WHERE edge_id BETWEEN 1000000 AND 1000200`,
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

func TestCompactLedgerSignedInitialKeysAndConcurrentWrites(t *testing.T) {
	w := compactFixture(t, 450)
	compactExec(t, w, `INSERT INTO resource_access_edges(rowid,source_kind,source_id,target_ref) VALUES(-9223372036854775808,'card','minimum','document:min'),(0,'card','zero','document:zero')`)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for i := 0; i < 80; i++ {
		time.Sleep(5 * time.Millisecond)
		if _, err := w.db.ExecContext(ctx, `UPDATE resource_access_edges SET target_ref=? WHERE rowid=?`, fmt.Sprintf("document:concurrent-%d", i), 1+i%450); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := w.db.QueryRowContext(ctx, `SELECT count(*) FROM resource_access_compact_edges`).Scan(&n); err != nil || n != 452 {
			t.Fatalf("serving ledger unavailable: %d %v", n, err)
		}
	}
	if result, err := w.VerifyCompactAccessShadow(ctx); err != nil || result.Rows != 452 {
		t.Fatalf("final verification: %+v %v", result, err)
	}
}
