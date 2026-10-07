package perfguard

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"testing"
	"unsafe"

	"modernc.org/libc"
	"modernc.org/sqlite"
	sqlite3lib "modernc.org/sqlite/lib"
)

func workFixture(t *testing.T) (*sql.DB, *Capture) {
	t.Helper()
	db, c, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err = db.Exec(`CREATE TABLE runs(id TEXT PRIMARY KEY); WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<4096) INSERT INTO runs SELECT printf('run-%06d',x) FROM n`); err != nil {
		t.Fatal(err)
	}
	return db, c
}

func TestWorkCountsIndexedRangeAggregateReturningOneRow(t *testing.T) {
	db, c := workFixture(t)
	q := "SELECT SUM(LENGTH(id)) FROM runs WHERE id >= ''"
	plan, err := Explain(context.Background(), db, Statement{SQL: q})
	if err != nil {
		t.Fatal(err)
	}
	c.Start()
	var n int
	if err = db.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	_, queries, rows := c.Stop()
	aggregate := c.Work()
	c.Start()
	if err = db.QueryRow("SELECT SUM(LENGTH(id)) FROM runs WHERE id=?", "run-000001").Scan(&n); err != nil {
		t.Fatal(err)
	}
	c.Stop()
	lookup := c.Work()
	t.Logf("indexed aggregate: plan=%v work=%+v queries=%d rows=%d; indexed lookup work=%+v", plan, aggregate, queries, rows, lookup)
	if queries != 1 || rows != 1 || aggregate.FullScanSteps != 0 || aggregate.VMSteps < 4*4096 || lookup.VMSteps == 0 || lookup.VMSteps > 64 {
		t.Fatalf("lost internal work: aggregate=%+v lookup=%+v", aggregate, lookup)
	}
	if aggregate.VMSteps < 100*lookup.VMSteps {
		t.Fatal("aggregate indistinguishable from bounded lookup")
	}
}

func TestWorkCountsRepeatedPreparedTransactionAndExec(t *testing.T) {
	db, c := workFixture(t)
	q := "SELECT SUM(LENGTH(id)) FROM runs WHERE id >= ''"
	c.Start()
	if _, err := db.Exec(q); err != nil {
		t.Fatal(err)
	}
	c.Stop()
	one := c.Work().VMSteps
	for _, transactional := range []bool{false, true} {
		c.Start()
		var stmt *sql.Stmt
		var tx *sql.Tx
		var err error
		if transactional {
			tx, err = db.Begin()
			if err == nil {
				stmt, err = tx.Prepare(q)
			}
		} else {
			stmt, err = db.Prepare(q)
		}
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ {
			if _, err = stmt.Exec(); err != nil {
				t.Fatal(err)
			}
		}
		for i := 0; i < 2; i++ {
			var n int
			if err = stmt.QueryRow().Scan(&n); err != nil {
				t.Fatal(err)
			}
		}
		stmt.Close()
		if tx != nil {
			if err = tx.Rollback(); err != nil {
				t.Fatal(err)
			}
		}
		_, queries, rows := c.Stop()
		work := c.Work().VMSteps
		if queries != 5 || rows != 2 || work < 5*one || work > 5*one+32 {
			t.Fatalf("transaction=%v one=%d total=%d queries=%d rows=%d", transactional, one, work, queries, rows)
		}
	}
	c.Start()
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	err = conn.Raw(func(raw any) error {
		_, err := raw.(driver.ExecerContext).ExecContext(context.Background(), q, nil)
		return err
	})
	conn.Close()
	if err != nil {
		t.Fatal(err)
	}
	c.Stop()
	if c.Work().VMSteps != one {
		t.Fatalf("raw Exec work=%+v want=%d", c.Work(), one)
	}
}

func TestWorkCountsEarlyCloseAndCaptureLifetime(t *testing.T) {
	db, c := workFixture(t)
	c.Start()
	rows, err := db.Query("SELECT id FROM runs")
	if err != nil {
		t.Fatal(err)
	}
	initial := c.Work().VMSteps
	if initial == 0 {
		t.Fatal("first driver step was not captured before Next/Close")
	}
	if !rows.Next() {
		t.Fatal(rows.Err())
	}
	before := c.Work().VMSteps
	rows.Close()
	c.Stop()
	if c.Work().VMSteps != before {
		t.Fatalf("finalize double-counted first step: before=%d after=%d", before, c.Work().VMSteps)
	}

	c.Start()
	rows, err = db.Query("SELECT id FROM runs")
	if err != nil {
		t.Fatal(err)
	}
	c.Stop()
	stopped := c.Work()
	c.Start() // old rows must not accrue either rows or work to this request
	if !rows.Next() || !rows.Next() {
		t.Fatal(rows.Err())
	}
	rows.Close()
	_, queries, count := c.Stop()
	if c.Work().VMSteps != 0 || count != 0 || queries != 0 {
		t.Fatalf("old execution leaked into next capture: work=%+v queries=%d rows=%d", c.Work(), queries, count)
	}
	if stopped.VMSteps == 0 {
		t.Fatal("stopped capture lost its completed initial step")
	}
	// A query begun while disabled must also stay outside the next capture.
	rows, err = db.Query("SELECT id FROM runs")
	if err != nil {
		t.Fatal(err)
	}
	c.Start()
	if !rows.Next() || !rows.Next() {
		t.Fatal(rows.Err())
	}
	rows.Close()
	c.Stop()
	if c.Work().VMSteps != 0 {
		t.Fatal("disabled query leaked into capture")
	}
}

func TestWorkCountsErrorsCancellationAndPoolReset(t *testing.T) {
	var cancelQuery context.CancelFunc
	function := fmt.Sprintf("perfguard_cancel_%d", driverID.Add(1))
	if err := sqlite.RegisterScalarFunction(function, 0, func(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) {
		cancelQuery()
		return int64(0), nil
	}); err != nil {
		t.Fatal(err)
	}
	db, c := workFixture(t)
	c.Start()
	if _, err := db.Exec("INSERT INTO runs VALUES('run-000001')"); err == nil {
		t.Fatal("constraint error expected")
	}
	c.Stop()
	if c.Work().VMSteps == 0 {
		t.Fatal("error finalized without counting executed instructions")
	}

	for _, exec := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		cancelQuery = cancel
		c.Start()
		// Cancel synchronously at the first input, proving that work began and
		// avoiding a deadline that can expire before preparation on a busy host.
		q := "WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<1000000000) SELECT SUM(x+CASE WHEN x=1 THEN " + function + "() ELSE 0 END) FROM n"
		var err error
		if exec {
			_, err = db.ExecContext(ctx, q)
		} else {
			var n int64
			err = db.QueryRowContext(ctx, q).Scan(&n)
		}
		cancel()
		c.Stop()
		if !errors.Is(err, context.Canceled) || c.Work().VMSteps == 0 {
			t.Fatalf("exec=%v canceled work=%+v err=%v", exec, c.Work(), err)
		}
		t.Logf("canceled exec=%v work=%+v", exec, c.Work())
		c.Start()
		var n int
		if err = db.QueryRow("SELECT 1").Scan(&n); err != nil {
			t.Fatal(err)
		}
		c.Stop()
		if n != 1 || c.Work().VMSteps == 0 || c.Work().VMSteps > 16 {
			t.Fatalf("reset inherited old work/interruption: n=%d work=%+v", n, c.Work())
		}
	}
}

func TestWorkTraceRemovedWhenConnectionCloses(t *testing.T) {
	db, c := workFixture(t)
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var id uintptr
	var trace *workTrace
	if err = conn.Raw(func(raw any) error { trace = raw.(*captureConn).work; id = trace.id; return nil }); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	db.Close()
	if _, exists := workTraces.Load(id); exists {
		t.Fatal("closed connection retained callback capture")
	}
	trace.mu.Lock()
	if len(trace.active) != 0 || trace.running != 0 {
		t.Errorf("closed connection retained VM ancestry: active=%v running=%d", trace.active, trace.running)
	}
	trace.mu.Unlock()
	c.Start()
	if c.Work().VMSteps != 0 {
		t.Fatal(fmt.Sprint(c.Work()))
	}
}

func TestWorkOverflowGuardFailsClosedAndResets(t *testing.T) {
	c := &Capture{}
	c.Start()
	epoch := c.workEpoch()
	id := uintptr(workTraceID.Add(1))
	w := &workTrace{capture: c, id: id, running: 1, active: map[uintptr]statementWork{
		1: {epoch: epoch, progress: math.MaxInt32 - workProgressInterval + 1},
	}}
	workTraces.Store(id, w)
	defer workTraces.Delete(id)
	if sqliteWorkProgress(nil, id) != 1 || c.WorkError() == nil {
		t.Fatal("statement progress overflow did not fail closed")
	}
	c.Start()
	if c.WorkError() != nil || sqliteWorkProgress(nil, id) != 0 {
		t.Fatal("old statement overflow leaked into next capture")
	}
	epoch = c.workEpoch()
	// Two separate statements may exceed the native limit in total without
	// either counter wrapping. Keep the guard local to each executing VM.
	w.active[1] = statementWork{epoch: epoch, progress: math.MaxInt32 - 2*workProgressInterval}
	w.active[2] = statementWork{epoch: epoch, progress: math.MaxInt32 - 2*workProgressInterval}
	if sqliteWorkProgress(nil, id) != 0 {
		t.Fatal("first safe statement interrupted")
	}
	w.running = 2
	if sqliteWorkProgress(nil, id) != 0 || c.WorkError() != nil {
		t.Fatal("request progress incorrectly treated as statement overflow")
	}
	c.addWork(epoch, WorkStats{VMSteps: math.MaxInt32})
	c.addWork(epoch, WorkStats{VMSteps: math.MaxInt32})
	if c.WorkError() != nil || c.Work().VMSteps != 2*uint64(math.MaxInt32) {
		t.Fatal("valid request work lost 64-bit accumulation")
	}
	c.work.VMSteps = math.MaxUint64
	c.addWork(epoch, WorkStats{VMSteps: 1})
	if c.WorkError() == nil || c.Work().VMSteps != math.MaxUint64 {
		t.Fatal("total work overflow did not saturate and fail closed")
	}
	c.Stop()
	c.Start()
	if c.WorkError() != nil || c.Work().VMSteps != 0 {
		t.Fatal("Start did not reset error/count")
	}
}

func TestWorkAdapterRejectsUnexpectedHandleLayout(t *testing.T) {
	if _, err := sqliteHandle(&captureConn{}, "conn", "db", reflect.TypeOf(uintptr(0))); err == nil {
		t.Fatal("wrapper mistaken for canonical SQLite connection")
	}
	db, _ := workFixture(t)
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.Raw(func(raw any) error {
		_, err := sqliteHandle(raw.(*captureConn).Conn, "conn", "tls", reflect.TypeOf(uintptr(0)))
		if err == nil {
			t.Fatal("unexpected TLS layout silently accepted")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestWorkCountsScanSortAndAutoIndex(t *testing.T) {
	db, c := workFixture(t)
	c.Start()
	var id string
	if err := db.QueryRow("SELECT id FROM runs ORDER BY LENGTH(id) LIMIT 1").Scan(&id); err != nil {
		t.Fatal(err)
	}
	c.Stop()
	if got := c.Work(); got.VMSteps == 0 || got.FullScanSteps != 4095 || got.Sorts != 1 {
		t.Fatalf("scan/sort counts=%+v", got)
	}
	if err := c.WorkError(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE other(id TEXT); INSERT INTO other SELECT id FROM runs`); err != nil {
		t.Fatal(err)
	}
	c.Start()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM other a JOIN other b ON a.id=b.id`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	c.Stop()
	if got := c.Work(); n != 4096 || got.AutoIndexRows != 4095 || got.FullScanSteps != 4095 {
		t.Fatalf("automatic-index counts=%+v n=%d", got, n)
	}
	if err := c.WorkError(); err != nil {
		t.Fatal(err)
	}
}

type registryTestAggregate struct{ n int64 }

func (a *registryTestAggregate) Step(_ *sqlite.FunctionContext, _ []driver.Value) error {
	a.n++
	return nil
}
func (a *registryTestAggregate) WindowInverse(_ *sqlite.FunctionContext, _ []driver.Value) error {
	a.n--
	return nil
}
func (a *registryTestAggregate) WindowValue(_ *sqlite.FunctionContext) (driver.Value, error) {
	return a.n, nil
}
func (a *registryTestAggregate) Final(_ *sqlite.FunctionContext) {}

func TestAggregateRegistryCoversCustomFunctionWithoutNameConvention(t *testing.T) {
	name := fmt.Sprintf("perfguard_count_%d", driverID.Add(1))
	if err := sqlite.RegisterFunction(name, &sqlite.FunctionImpl{NArgs: 1, MakeAggregate: func(sqlite.FunctionContext) (sqlite.AggregateFunction, error) { return &registryTestAggregate{}, nil }}); err != nil {
		t.Fatal(err)
	}
	db, c := workFixture(t)
	aggregates, err := AggregateFunctions(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if !aggregates[name] {
		t.Fatalf("custom aggregate not discovered: %s", name)
	}
	q := "SELECT " + name + "(id) FROM runs WHERE id >= '' LIMIT 1"
	plan, err := Explain(context.Background(), db, Statement{SQL: q})
	if err != nil {
		t.Fatal(err)
	}
	if len(Findings(q, plan, map[string]bool{"runs": true}, nil, aggregates)) == 0 {
		t.Fatalf("registry aggregate escaped: %s %v", q, plan)
	}
	c.Start()
	var n int
	if err := db.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	c.Stop()
	if n != 4096 || c.Work().VMSteps < 4096 {
		t.Fatalf("custom aggregate did not visit corpus: n=%d work=%+v", n, c.Work())
	}
	if err := c.WorkError(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkProgressHonorsCancellationBeforeFirstStep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	id := uintptr(workTraceID.Add(1))
	w := &workTrace{capture: &Capture{}, id: id, active: map[uintptr]statementWork{}}
	workTraces.Store(id, w)
	defer workTraces.Delete(id)
	w.beginCall(ctx)
	if sqliteWorkProgress(nil, id) != 0 {
		t.Fatal("live context interrupted")
	}
	cancel() // model cancellation during prepare, before a STMT event
	if sqliteWorkProgress(nil, id) != 1 {
		t.Fatal("canceled prepare/step context lost")
	}
	w.endCall()
	w.beginCall(context.Background())
	if sqliteWorkProgress(nil, id) != 0 {
		t.Fatal("next call inherited old cancellation")
	}
	w.endCall()
}

func TestWorkRepeatedFTSQueriesCountEachExecutionOnly(t *testing.T) {
	db, c := workFixture(t)
	if _, err := db.Exec(`
CREATE TABLE documents(id INTEGER PRIMARY KEY,body TEXT);
CREATE VIRTUAL TABLE document_search USING fts5(body,content='documents',content_rowid='id');
WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<4096)
INSERT INTO documents SELECT x,printf('common synthetic body %06d',x) FROM n;
INSERT INTO document_search(document_search) VALUES('rebuild');
`); err != nil {
		t.Fatal(err)
	}
	q := `SELECT SUM(LENGTH(body)) FROM document_search WHERE document_search MATCH 'common'`
	// Warm the module/cache before comparing samples. FTS executes a cached
	// content lookup VM for each matching document, resetting/reusing it rather
	// than finalizing it. Its native stmt_status counter persists across reset.
	var n int
	if err := db.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	var want WorkStats
	for i := 0; i < 8; i++ {
		c.Start()
		if err := db.QueryRow(q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		_, queries, rows := c.Stop()
		got := c.Work()
		t.Logf("FTS sample=%d work=%+v queries=%d rows=%d", i, got, queries, rows)
		if err := c.WorkError(); err != nil {
			t.Fatal(err)
		}
		if queries != 1 || rows != 1 || n != 28*4096 || got.VMSteps == 0 {
			t.Fatalf("FTS work/result lost: sum=%d work=%+v queries=%d rows=%d", n, got, queries, rows)
		}
		if i == 0 {
			want = got
		} else if got != want {
			t.Fatalf("reused FTS counters accumulated across capture: sample=%d got=%+v want=%+v", i, got, want)
		}
		assertWorkTraceIdle(t, db)
	}
}

func TestWorkDuplicateStatementTraceRetainsActiveCounters(t *testing.T) {
	db, c := workFixture(t)
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	c.Start()
	err = conn.Raw(func(raw any) error {
		cc := raw.(*captureConn)
		rows, err := cc.QueryContext(context.Background(), "SELECT id FROM runs WHERE id='run-000001'", nil)
		if err != nil {
			return err
		}
		r := rows.(*captureRows)
		stmt, err := sqliteHandle(r.Rows, "rows", "pstmt", reflect.TypeOf(uintptr(0)))
		if err != nil {
			r.Close()
			return err
		}
		before := c.Work()
		if before.VMSteps == 0 {
			t.Fatal("initial step missing")
		}
		// SQLite emits STMT again for trigger subprograms on an active VM.
		// Model that event after a completed step; resetting here would erase
		// its native count and cause a false overflow on the final profile.
		sqliteWorkCallback(cc.work.tls, sqlite3lib.SQLITE_TRACE_STMT, cc.work.id, uintptr(stmt.Uint()), 0)
		if err := r.Close(); err != nil {
			return err
		}
		if c.Work() != before || c.WorkError() != nil {
			t.Fatalf("duplicate active STMT reset accumulated work: before=%+v after=%+v err=%v", before, c.Work(), c.WorkError())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	c.Stop()
}

func TestWorkTriggerExecutionsRetainStableCounters(t *testing.T) {
	db, c := workFixture(t)
	if _, err := db.Exec(`CREATE TABLE trigger_results(id TEXT PRIMARY KEY,n INTEGER); CREATE TRIGGER runs_insert AFTER INSERT ON runs BEGIN INSERT INTO trigger_results VALUES(NEW.id,1); UPDATE trigger_results SET n=n+1 WHERE id=NEW.id; END`); err != nil {
		t.Fatal(err)
	}
	var want WorkStats
	for i := 0; i < 8; i++ {
		c.Start()
		if _, err := db.Exec("INSERT INTO runs VALUES(?)", fmt.Sprintf("new-%d", i)); err != nil {
			t.Fatal(err)
		}
		_, queries, rows := c.Stop()
		got := c.Work()
		if i == 0 {
			want = got
		} else if got != want {
			t.Fatalf("trigger work changed across fresh executions: got=%+v want=%+v", got, want)
		}
		if queries != 1 || rows != 0 || got.VMSteps < 32 {
			t.Fatalf("trigger execution work missing: queries=%d rows=%d work=%+v", queries, rows, got)
		}
		if err := c.WorkError(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWorkNestedProfileRestoresParentOverflowGuard(t *testing.T) {
	c := &Capture{}
	c.Start()
	epoch := c.workEpoch()
	id := uintptr(workTraceID.Add(1))
	w := &workTrace{capture: c, id: id, running: 2, active: map[uintptr]statementWork{
		1: {epoch: epoch, progress: math.MaxInt32 - workProgressInterval + 1},
		2: {epoch: epoch, parent: 1},
	}}
	workTraces.Store(id, w)
	defer workTraces.Delete(id)
	w.finishStatement(2) // nested cached VM finishes while its parent continues
	if w.running != 1 || len(w.active) != 1 {
		t.Fatalf("nested completion lost running parent: running=%d active=%v", w.running, w.active)
	}
	if sqliteWorkProgress(nil, id) != 1 || c.WorkError() == nil {
		t.Fatal("parent native overflow no longer guarded after nested PROFILE")
	}
	w.finishStatement(1)
	if w.running != 0 || len(w.active) != 0 {
		t.Fatalf("finished ancestors retained: running=%d active=%v", w.running, w.active)
	}
	// An early-closed/finalized parent must not be resurrected by a child's
	// later PROFILE event, including when a cursor resumes through beginRows.
	w.active[2] = statementWork{epoch: epoch, parent: 1}
	w.running = 2
	w.finishStatement(2)
	if w.running != 0 || len(w.active) != 0 {
		t.Fatal("finalized parent resurrected")
	}
}

func TestWorkOldFTSCursorDoesNotLeakNestedWorkIntoNextCapture(t *testing.T) {
	db, c := workFixture(t)
	if _, err := db.Exec(`CREATE TABLE documents(id INTEGER PRIMARY KEY,body TEXT); CREATE VIRTUAL TABLE document_search USING fts5(body,content='documents',content_rowid='id'); INSERT INTO documents SELECT rowid,'common body' FROM runs; INSERT INTO document_search(document_search) VALUES('rebuild')`); err != nil {
		t.Fatal(err)
	}
	c.Start()
	rows, err := db.Query(`SELECT body FROM document_search WHERE document_search MATCH 'common'`)
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatal(rows.Err())
	}
	c.Stop()
	old := c.Work()
	c.Start()
	for i := 0; i < 16; i++ {
		if !rows.Next() {
			t.Fatal(rows.Err())
		}
	}
	rows.Close()
	_, queries, count := c.Stop()
	if old.VMSteps == 0 || c.Work().VMSteps != 0 || queries != 0 || count != 0 || c.WorkError() != nil {
		t.Fatalf("old nested FTS cursor leaked: old=%+v new=%+v queries=%d rows=%d err=%v", old, c.Work(), queries, count, c.WorkError())
	}
	assertWorkTraceIdle(t, db)
}

func TestWorkNativePreparedSchemaChangeFailsClosed(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "work.db")
	db, c, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	peer, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	if _, err := db.Exec(`CREATE TABLE runs(id TEXT PRIMARY KEY); WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<4096) INSERT INTO runs SELECT printf('run-%06d',x) FROM n`); err != nil {
		t.Fatal(err)
	}
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var stmt uintptr
	var trace *workTrace
	err = conn.Raw(func(raw any) error {
		trace = raw.(*captureConn).work
		q, err := libc.CString("SELECT SUM(LENGTH(id)) FROM runs WHERE id >= ''")
		if err != nil {
			return err
		}
		defer libc.Xfree(trace.tls, q)
		bytes := int(unsafe.Sizeof(uintptr(0)))
		slot := trace.tls.Alloc(bytes)
		defer trace.tls.Free(bytes)
		if rc := sqlite3lib.Xsqlite3_prepare_v2(trace.tls, trace.db, q, -1, slot, 0); rc != sqlite3lib.SQLITE_OK {
			return fmt.Errorf("prepare: %d", rc)
		}
		native := libc.GoBytes(slot, bytes)
		if bytes == 8 {
			stmt = uintptr(binary.NativeEndian.Uint64(native))
		} else if bytes == 4 {
			stmt = uintptr(binary.NativeEndian.Uint32(native))
		} else {
			return fmt.Errorf("unsupported native pointer width: %d", bytes)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Unlike modernc's public Stmt, which reparses SQL at execution, this is a
	// real native prepared VM. A second connection changes its schema cookie.
	defer func() {
		if stmt != 0 {
			conn.Raw(func(any) error { sqlite3lib.Xsqlite3_finalize(trace.tls, stmt); return nil })
		}
	}()
	if _, err := peer.Exec("ALTER TABLE runs ADD COLUMN marker INTEGER"); err != nil {
		t.Fatal(err)
	}
	c.Start()
	var value int64
	var nativeSteps uint64
	err = conn.Raw(func(raw any) error {
		trace.beginCall(context.Background())
		defer trace.endCall()
		if rc := sqlite3lib.Xsqlite3_step(trace.tls, stmt); rc != sqlite3lib.SQLITE_ROW {
			return fmt.Errorf("first step: %d", rc)
		}
		value = sqlite3lib.Xsqlite3_column_int64(trace.tls, stmt, 0)
		if rc := sqlite3lib.Xsqlite3_step(trace.tls, stmt); rc != sqlite3lib.SQLITE_DONE {
			return fmt.Errorf("completion step: %d", rc)
		}
		nativeSteps = uint64(uint32(sqlite3lib.Xsqlite3_stmt_status(trace.tls, stmt, sqlite3lib.SQLITE_STMTSTATUS_VM_STEP, 0)))
		sqlite3lib.Xsqlite3_finalize(trace.tls, stmt)
		stmt = 0
		return nil
	})
	c.Stop()
	if err != nil {
		t.Fatal(err)
	}
	if value != 40960 {
		t.Fatal(value)
	}
	t.Logf("native reprepare: measured=%+v native=%d error=%v", c.Work(), nativeSteps, c.WorkError())
	if c.WorkError() == nil && (c.Work().VMSteps != nativeSteps || nativeSteps < 4*4096) {
		t.Fatalf("reprepare silently lost work: measured=%+v native=%d", c.Work(), nativeSteps)
	}
}

func assertWorkTraceIdle(t *testing.T, db *sql.DB) {
	t.Helper()
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.Raw(func(raw any) error {
		w := raw.(*captureConn).work
		w.mu.Lock()
		defer w.mu.Unlock()
		if len(w.active) != 0 || w.running != 0 || w.callEpoch != 0 || w.ctx != nil {
			t.Errorf("completed work retained native frame: active=%v running=%d epoch=%d ctx=%v", w.active, w.running, w.callEpoch, w.ctx)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
