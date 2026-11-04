package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"agent-nexus-core/internal/testsql"
	"agent-nexus-core/internal/testutil/perfguard"
)

func TestAdmissionMigrationCommitsAndResumesRootChunks(t *testing.T) {
	for _, fenced := range []bool{false, true} {
		t.Run(fmt.Sprint("fenced=", fenced), func(t *testing.T) { admissionMigrationCommitsAndResumesRootChunks(t, fenced) })
	}
}

func admissionMigrationCommitsAndResumesRootChunks(t *testing.T, fenced bool) {
	// This test interrupts after exactly one durable root, so it pins one root
	// per transaction. Batched admission is covered by the test below.
	withAdmissionRootBatch(t, 1)
	root := t.TempDir()
	db := mainHistoryDatabase(t, root, 115)
	for _, q := range []string{
		`INSERT INTO threads(id,updated_at,updated_by,body_json) WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<3) SELECT 'root-'||i,'now','owner','{"pm_actor_id":"owner"}' FROM n`,
		`INSERT INTO cards(id,handle,title,summary,column_key,refs_json,created_at,created_by,updated_at,updated_by) WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<63) SELECT 'shared-'||i,'shared-'||i,'Public','','ready','["thread:root-0","thread:root-1","thread:root-2","thread:root-3"]','now','owner','now','owner' FROM n`,
		`INSERT INTO resource_access_tombstones(kind,id,ref,owner) VALUES('thread','root-0','retained-root','second-owner')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	ledger := "schema_migrations"
	if fenced {
		ledger = "scope_schema_migrations"
		fenceAdmissionHistory(t, root, db)
	} else {
		db.Close()
	}
	read, err := sql.Open("sqlite", sqliteDSN(NewLayout(root).DatabasePath))
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	write, counter := testsql.Open(sqliteDSN(NewLayout(root).DatabasePath))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	interrupted, durableRoots := false, 0
	var inspectErr error
	counter.BeforeQuery(func(q string) {
		if interrupted {
			return
		}
		var committed bool
		inspectErr = read.QueryRow(`SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name='resource_access_admission_reach')`).Scan(&committed)
		if inspectErr != nil || !committed {
			return
		}
		inspectErr = read.QueryRow(`SELECT count(*) FROM (SELECT DISTINCT root_kind,root_id FROM resource_access_admission_reach)`).Scan(&durableRoots)
		if inspectErr == nil && durableRoots > 0 {
			interrupted = true
			cancel() // Interrupt after observing the first independently committed root.
		}
	})
	var admission migration
	for _, m := range migrations {
		if m.Version == 116 {
			admission = m
		}
	}
	if fenced {
		err = applyWorkspaceMigrations(ctx, write)
	} else {
		err = applyMigration(ctx, write, admission)
	}
	write.Close()
	if !errors.Is(err, context.Canceled) || !interrupted || inspectErr != nil || durableRoots != 1 {
		t.Fatalf("interrupted=%v durable roots=%d inspection=%v migration=%v; want one committed root before cancellation", interrupted, durableRoots, inspectErr, err)
	}
	var recorded int
	if err := read.QueryRow(`SELECT count(*) FROM ` + ledger + ` WHERE version=116`).Scan(&recorded); err != nil || recorded != 0 {
		t.Fatalf("incomplete migration recorded: count=%d err=%v", recorded, err)
	}
	if _, err := read.Exec(`CREATE TRIGGER test_completed_root_not_replayed BEFORE INSERT ON resource_access_admission_reach WHEN NEW.root_id='root-0' BEGIN SELECT RAISE(ABORT,'completed root replayed'); END`); err != nil {
		t.Fatal(err)
	}
	read.Close()
	ws, err := InitializeWorkspace(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	checkAdmissionSecurityParity(t, ws.DB(), "resumed root chunks")
	if err := ws.DB().QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name='test_completed_root_not_replayed'`).Scan(&recorded); err != nil || recorded != 1 {
		t.Fatalf("resumption rebuilt committed state: count=%d err=%v", recorded, err)
	}
	if err := ws.DB().QueryRow(`SELECT count(*) FROM ` + ledger + ` WHERE version=116`).Scan(&recorded); err != nil || recorded != 1 {
		t.Fatalf("completed migration not recorded: count=%d err=%v", recorded, err)
	}
}

func TestAdmissionMigrationRootChunksScale(t *testing.T) {
	if testing.Short() {
		t.Skip("released-schema 4096-card dense migration fixture")
	}
	// The per-root write bound below is defined for one root per transaction.
	withAdmissionRootBatch(t, 1)
	const roots, shared = 512, 512
	root := admissionDenseFixture(t, roots)
	db, capture, err := perfguard.Open(sqliteDSN(NewLayout(root).DatabasePath))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var changes []int64
	var times []time.Time
	var observerErr error
	capture.BeforeBeginConn(func(ctx context.Context, conn driver.Conn, _ driver.TxOptions) {
		rows, err := conn.(driver.QueryerContext).QueryContext(ctx, `SELECT total_changes()`, nil)
		if err != nil {
			observerErr = err
			return
		}
		defer rows.Close()
		value := make([]driver.Value, 1)
		if err := rows.Next(value); err != nil {
			observerErr = err
			return
		}
		changes = append(changes, value[0].(int64))
		times = append(times, time.Now())
	})
	var admission migration
	for _, m := range migrations {
		if m.Version == 116 {
			admission = m
		}
	}
	start := time.Now()
	if err := applyMigration(context.Background(), db, admission); err != nil {
		t.Fatal(err)
	}
	if observerErr != nil {
		t.Fatal(observerErr)
	}
	if len(changes) != roots+2 {
		t.Fatalf("transactions=%d want setup + %d roots + completion", len(changes), roots)
	}
	var maxChanges int64
	var maxElapsed time.Duration
	for i := 2; i < len(changes); i++ {
		delta := changes[i] - changes[i-1]
		maxChanges = max(maxChanges, delta)
		maxElapsed = max(maxElapsed, times[i].Sub(times[i-1]))
		// At most one reach, owner and lifecycle write for each vertex in
		// this root's three ledgers, plus its durable cursor update.
		if delta > 3*3*(shared+2)+1 {
			t.Fatalf("one root rewrote unrelated memberships: changes=%d", delta)
		}
	}
	t.Logf("cards=4096 roots=%d shared=%d transactions=%d setup_writes=%d max_root_writes=%d max_root_elapsed=%s elapsed=%s", roots, shared, len(changes), changes[1]-changes[0], maxChanges, maxElapsed, time.Since(start))
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err := InitializeWorkspace(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	checkAdmissionSecurityParity(t, ws.DB(), "dense chunked migration")
}

// Batched roots must leave exactly the ledgers that one root per transaction
// leaves, and an interrupted batch must resume from its durable cursor.
func TestAdmissionMigrationBatchedRootChunksMatchSingleRootChunks(t *testing.T) {
	if testing.Short() {
		t.Skip("released-schema 4096-card dense migration fixture")
	}
	const roots = 200
	reference, referenceTx := admissionChunkedMigrationRun(t, roots, 1, false)
	if referenceTx != roots+2 {
		t.Fatalf("single-root transactions=%d want %d", referenceTx, roots+2)
	}
	batched, batchedTx := admissionChunkedMigrationRun(t, roots, 64, false)
	// Setup, three full batches of 64, one partial batch of 8, and completion.
	if want := 4 + 2; batchedTx != want {
		t.Fatalf("batched transactions=%d want %d", batchedTx, want)
	}
	if !reflect.DeepEqual(reference, batched) {
		t.Fatalf("batched admission diverged from single-root admission:\nwant %v\ngot  %v", reference, batched)
	}
	resumed, _ := admissionChunkedMigrationRun(t, roots, 64, true)
	if !reflect.DeepEqual(reference, resumed) {
		t.Fatalf("resumed batched admission diverged:\nwant %v\ngot  %v", reference, resumed)
	}
}

// withAdmissionRootBatch sets the roots per migration transaction for one test.
func withAdmissionRootBatch(t *testing.T, n int) {
	t.Helper()
	previous := accessAdmissionRootBatch
	accessAdmissionRootBatch = n
	t.Cleanup(func() { accessAdmissionRootBatch = previous })
}

// admissionChunkedMigrationRun applies migration 116 to a fresh dense fixture
// with the given batch size and returns the admission ledger digest and the
// number of transactions used. With interrupt, it cancels before the third
// transaction, after one batch is durable, and resumes the same migration.
func admissionChunkedMigrationRun(t *testing.T, roots, batch int, interrupt bool) (map[string]string, int) {
	t.Helper()
	withAdmissionRootBatch(t, batch)
	root := admissionDenseFixture(t, roots)
	db, capture, err := perfguard.Open(sqliteDSN(NewLayout(root).DatabasePath))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	txs := 0
	capture.BeforeBeginConn(func(context.Context, driver.Conn, driver.TxOptions) {
		txs++
		if interrupt && txs == 3 {
			cancel()
		}
	})
	var admission migration
	for _, m := range migrations {
		if m.Version == 116 {
			admission = m
		}
	}
	err = applyMigration(ctx, db, admission)
	if interrupt {
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("interrupted migration err=%v; want context canceled", err)
		}
		var recorded int
		if err := db.QueryRow(`SELECT count(*) FROM schema_migrations WHERE version=116`).Scan(&recorded); err != nil || recorded != 0 {
			t.Fatalf("interrupted migration recorded: count=%d err=%v", recorded, err)
		}
		var cursor int
		if err := db.QueryRow(`SELECT count(*) FROM resource_access_admission_build_state WHERE last_kind IS NOT NULL`).Scan(&cursor); err != nil || cursor != 1 {
			t.Fatalf("interrupted batch left no durable cursor: count=%d err=%v", cursor, err)
		}
		// Resume on the same database. The completed batch is not replayed.
		err = applyMigration(context.Background(), db, admission)
	}
	if err != nil {
		t.Fatal(err)
	}
	digest := admissionLedgerDigest(t, db)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err := InitializeWorkspace(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	checkAdmissionSecurityParity(t, ws.DB(), fmt.Sprintf("batch=%d interrupt=%v", batch, interrupt))
	return digest, txs
}

// admissionLedgerDigest hashes the admission schema and every admission table
// row, so two migration runs can be compared without listing their contents.
func admissionLedgerDigest(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	ctx := context.Background()
	out := map[string]string{}
	schema, err := db.QueryContext(ctx, `SELECT type,name,COALESCE(sql,'') FROM sqlite_schema WHERE name LIKE 'resource_access_admission%' OR name LIKE 'admission_%' OR name LIKE 'idx_access_admission%' OR name LIKE 'idx_admission%' ORDER BY type,name`)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	for schema.Next() {
		var typ, name, sqlText string
		if err := schema.Scan(&typ, &name, &sqlText); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(h, "%s|%s|%s\n", typ, name, sqlText)
	}
	if err := schema.Err(); err != nil {
		t.Fatal(err)
	}
	schema.Close()
	out["schema"] = fmt.Sprintf("%x", h.Sum(nil))
	tables, err := db.QueryContext(ctx, `SELECT name FROM sqlite_schema WHERE type='table' AND name LIKE 'resource_access_admission%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for tables.Next() {
		var name string
		if err := tables.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	tables.Close()
	for _, name := range names {
		rows, err := db.QueryContext(ctx, `SELECT * FROM "`+name+`"`)
		if err != nil {
			t.Fatal(err)
		}
		cols, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		var lines []string
		for rows.Next() {
			values := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			lines = append(lines, fmt.Sprintf("%#v", values))
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		sort.Strings(lines)
		rh := sha256.New()
		for _, line := range lines {
			fmt.Fprintln(rh, line)
		}
		out[name] = fmt.Sprintf("rows=%d sha=%x", len(lines), rh.Sum(nil))
	}
	return out
}

// admissionDenseFixture is the released-schema 4096-card dense migration
// fixture with the given number of thread roots, at schema 115.
func admissionDenseFixture(t *testing.T, roots int) string {
	t.Helper()
	root := t.TempDir()
	fixture := mainHistoryDatabase(t, root, 115)
	for _, q := range []string{
		fmt.Sprintf(`INSERT INTO threads(id,updated_at,updated_by,body_json) WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<%d) SELECT printf('root-%%03d',i),'now','owner',json_object('pm_actor_id',CASE WHEN i%%2=0 THEN 'owner' ELSE 'second-owner' END) FROM n`, roots-1),
		`INSERT INTO cards(id,handle,title,summary,column_key,refs_json,archived_at,created_at,created_by,updated_at,updated_by) WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<4095) SELECT CASE WHEN i=0 THEN 'root-hub' ELSE printf('shared-%04d',i) END,CASE WHEN i=0 THEN 'root-hub' ELSE printf('shared-%04d',i) END,'Public','','ready',CASE WHEN i=0 THEN (SELECT json_group_array('thread:'||id) FROM threads) WHEN i<=512 THEN '["card:root-hub"]' ELSE '[]' END,CASE WHEN i<=512 THEN 'now' END,'now','owner','now','owner' FROM n`,
	} {
		if _, err := fixture.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := fixture.Close(); err != nil {
		t.Fatal(err)
	}
	return root
}
