package perfguard

import (
	"context"
	"database/sql/driver"
	"fmt"
	"math"
	"reflect"
	"sync"
	"sync/atomic"
	"unsafe"

	"modernc.org/libc"
	sqlite3 "modernc.org/sqlite/lib"
)

// WorkStats measures SQLite VM work, independently of rows returned and query
// plans. VMSteps catches indexed range aggregates that return only one row.
// These are instruction counts, not elapsed time or bytes processed by a UDF.
type WorkStats struct {
	VMSteps       uint64
	FullScanSteps uint64
	Sorts         uint64
	AutoIndexRows uint64
}

// Work returns work observed in the current (or most recently stopped) capture.
// Stop keeps its existing three-result API. Callers must close their rows before
// Stop, just as they must finish the request before reading execution counts.
func (c *Capture) Work() WorkStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.work
}

// WorkError reports instrumentation failures. Budget consumers must check it:
// native counters are 32 bits, so large counts fail closed rather than wrapping.
func (c *Capture) WorkError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.workErr
}

func (c *Capture) failWork(epoch uint64, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.enabled && epoch != 0 && epoch == c.epoch && c.workErr == nil {
		c.workErr = err
	}
}

const workProgressInterval = 1 << 20

var statementWorkCounters = [...]int32{
	sqlite3.SQLITE_STMTSTATUS_VM_STEP,
	sqlite3.SQLITE_STMTSTATUS_FULLSCAN_STEP,
	sqlite3.SQLITE_STMTSTATUS_SORT,
	sqlite3.SQLITE_STMTSTATUS_AUTOINDEX,
}

func (c *Capture) workEpoch() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.enabled {
		return 0
	}
	return c.epoch
}

func (c *Capture) addWork(epoch uint64, delta WorkStats) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.enabled || epoch == 0 || epoch != c.epoch {
		return
	}
	totals := [...]*uint64{&c.work.VMSteps, &c.work.FullScanSteps, &c.work.Sorts, &c.work.AutoIndexRows}
	additions := [...]uint64{delta.VMSteps, delta.FullScanSteps, delta.Sorts, delta.AutoIndexRows}
	for i, total := range totals {
		if math.MaxUint64-*total < additions[i] {
			*total = math.MaxUint64
			if c.workErr == nil {
				c.workErr = fmt.Errorf("perfguard: SQLite work total overflow")
			}
		} else {
			*total += additions[i]
		}
	}
}

type statementWork struct {
	epoch    uint64
	last     [4]uint32
	progress uint64
	parent   uintptr
}

type workTrace struct {
	capture   *Capture
	tls       *libc.TLS
	db        uintptr
	id        uintptr
	mu        sync.Mutex
	active    map[uintptr]statementWork
	running   uintptr
	ctx       context.Context
	callEpoch uint64
}

var workTraceID atomic.Uint64
var workTraces sync.Map

// modernc v1.38.2 exposes sqlite3_stmt_status through its generated public lib,
// but not connection/rows handles through database/sql. This test-only adapter
// reads the named fields via reflection, avoiding a copied architecture-specific
// struct layout. Fail closed if their package/type/field shape changes. The only
// unsafe read is the *libc.TLS field; database/sql retains the owning connection.
// Keep this adapter paired with the pinned modernc version and its regressions.
func sqliteHandle(v any, typeName string, field string, fieldType reflect.Type) (reflect.Value, error) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() || rv.Type().Elem().PkgPath() != "modernc.org/sqlite" || rv.Type().Elem().Name() != typeName {
		return reflect.Value{}, fmt.Errorf("perfguard: unsupported SQLite %s handle %T", typeName, v)
	}
	f := rv.Elem().FieldByName(field)
	if !f.IsValid() || f.Type() != fieldType || !f.CanAddr() {
		return reflect.Value{}, fmt.Errorf("perfguard: unsupported modernc SQLite %s.%s layout", typeName, field)
	}
	return f, nil
}

func installWorkTrace(conn driver.Conn, c *Capture) (*workTrace, error) {
	db, err := sqliteHandle(conn, "conn", "db", reflect.TypeOf(uintptr(0)))
	if err != nil {
		return nil, err
	}
	tls, err := sqliteHandle(conn, "conn", "tls", reflect.TypeOf((*libc.TLS)(nil)))
	if err != nil {
		return nil, err
	}
	w := &workTrace{capture: c, db: uintptr(db.Uint()), tls: *(**libc.TLS)(unsafe.Pointer(tls.UnsafeAddr())), id: uintptr(workTraceID.Add(1)), active: make(map[uintptr]statementWork)}
	if w.db == 0 || w.tls == nil {
		return nil, fmt.Errorf("perfguard: closed modernc SQLite connection")
	}
	workTraces.Store(w.id, w)
	// Same declared-function pointer representation used by modernc's
	// cFuncPointer; closures cannot be passed to generated C callback APIs.
	callback := *(*uintptr)(unsafe.Pointer(&struct {
		f func(*libc.TLS, uint32, uintptr, uintptr, uintptr) int32
	}{sqliteWorkCallback}))
	if rc := sqlite3.Xsqlite3_trace_v2(w.tls, w.db, sqlite3.SQLITE_TRACE_STMT|sqlite3.SQLITE_TRACE_PROFILE, callback, w.id); rc != sqlite3.SQLITE_OK {
		workTraces.Delete(w.id)
		return nil, fmt.Errorf("perfguard: sqlite3_trace_v2: %d", rc)
	}
	progressCallback := *(*uintptr)(unsafe.Pointer(&struct {
		f func(*libc.TLS, uintptr) int32
	}{sqliteWorkProgress}))
	sqlite3.Xsqlite3_progress_handler(w.tls, w.db, workProgressInterval, progressCallback, w.id)
	return w, nil
}

func sqliteWorkCallback(tls *libc.TLS, event uint32, id, stmt, unused uintptr) int32 {
	value, ok := workTraces.Load(id)
	if !ok {
		return 0
	}
	w := value.(*workTrace)
	w.mu.Lock()
	defer w.mu.Unlock()
	switch event {
	case sqlite3.SQLITE_TRACE_STMT:
		parent := w.running
		w.running = stmt
		// Trigger subprograms can emit STMT again for the same VM. Retain its
		// original capture lifetime and accumulated work.
		if _, exists := w.active[stmt]; !exists {
			// sqlite3_reset does not reset stmt_status. FTS and other virtual tables
			// reuse internal prepared VMs, so each native execution must start a new
			// counter interval. A repeated STMT for an active trigger VM stays intact.
			// In pinned SQLite, STMT fires at OP_Init; the current step's local VM
			// count is added on return, preserving its initial instructions here.
			for _, op := range statementWorkCounters {
				sqlite3.Xsqlite3_stmt_status(tls, stmt, op, 1)
			}
			sqlite3.Xsqlite3_stmt_status(tls, stmt, sqlite3.SQLITE_STMTSTATUS_REPREPARE, 1)
			epoch := w.capture.workEpoch()
			if ancestor, exists := w.active[parent]; exists {
				epoch = ancestor.epoch
			}
			w.active[stmt] = statementWork{epoch: epoch, parent: parent}
		}
	case sqlite3.SQLITE_TRACE_PROFILE:
		w.sampleLocked(tls, stmt)
		w.finishStatement(stmt)
	}
	return 0
}

// finishStatement restores the native parent after nested SQL (for example,
// FTS content lookups) completes. Trigger STMT repeats retain the existing
// parent link rather than creating duplicate ancestors. Inactive/finalized
// parents cannot become the running VM again.
func (w *workTrace) finishStatement(stmt uintptr) {
	parent := w.active[stmt].parent
	delete(w.active, stmt)
	if w.running == stmt {
		w.running = 0
		if _, exists := w.active[parent]; exists && parent != stmt {
			w.running = parent
		}
	}
}

func (w *workTrace) sampleLocked(tls *libc.TLS, stmt uintptr) {
	s, ok := w.active[stmt]
	if stmt == 0 {
		return
	}
	epoch := s.epoch
	if !ok {
		epoch = w.callEpoch
	}
	// Automatic schema reprepare can emit a failed PROFILE and suppress the
	// retry's STMT in pinned SQLite. Its counter lifecycle is then unreliable;
	// fail closed rather than silently recording an untracked retry as zero.
	if sqlite3.Xsqlite3_stmt_status(tls, stmt, sqlite3.SQLITE_STMTSTATUS_REPREPARE, 0) != 0 {
		w.capture.failWork(epoch, fmt.Errorf("perfguard: SQLite automatic reprepare has unsupported work-counter lifecycle"))
	}
	if !ok {
		return
	}
	var delta [4]uint64
	for i, op := range statementWorkCounters {
		// SQLite exposes unsigned 32-bit counters through a signed C int.
		// Subtract before widening so an observed wrap preserves its delta.
		n := uint32(sqlite3.Xsqlite3_stmt_status(tls, stmt, op, 0))
		if n > math.MaxInt32 || n < s.last[i] {
			w.capture.failWork(s.epoch, fmt.Errorf("perfguard: SQLite statement work counter overflow"))
		}
		delta[i] = uint64(n - s.last[i])
		s.last[i] = n
	}
	w.active[stmt] = s
	w.capture.addWork(s.epoch, WorkStats{delta[0], delta[1], delta[2], delta[3]})
}

func (w *workTrace) sampleRows(rows driver.Rows) error {
	stmt, err := sqliteHandle(rows, "rows", "pstmt", reflect.TypeOf(uintptr(0)))
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sampleLocked(w.tls, uintptr(stmt.Uint()))
	return nil
}

// A progress callback is only an overflow guard; normal measurements still use
// exact stmt_status counts. Sampling every million instructions avoids per-step
// Go callback overhead, and interrupts before a single sqlite3_step can silently
// wrap its native counter. Capture epochs also isolate continued old row cursors.
func sqliteWorkProgress(tls *libc.TLS, id uintptr) int32 {
	value, ok := workTraces.Load(id)
	if !ok {
		return 0
	}
	w := value.(*workTrace)
	w.mu.Lock()
	defer w.mu.Unlock()
	// modernc can receive sqlite3_interrupt during preparation, before SQLite
	// resets the flag at its first step. Retain the active context as well so
	// the test pool honors cancellation even in that race.
	if w.ctx != nil && w.ctx.Err() != nil {
		return 1
	}
	s, exists := w.active[w.running]
	if !exists || s.epoch == 0 || s.epoch != w.capture.workEpoch() {
		return 0
	}
	// The native limit applies to each statement, not the whole request. A
	// request may legitimately accumulate billions of steps across many VMs.
	s.progress += workProgressInterval
	w.active[w.running] = s
	if s.progress >= math.MaxInt32 {
		w.capture.failWork(s.epoch, fmt.Errorf("perfguard: SQLite work exceeds safe 32-bit statement counters"))
		return 1
	}
	return 0
}

func (w *workTrace) beginRows(rows driver.Rows, ctx context.Context, epoch uint64) error {
	stmt, err := sqliteHandle(rows, "rows", "pstmt", reflect.TypeOf(uintptr(0)))
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.running = uintptr(stmt.Uint())
	w.ctx = ctx
	w.callEpoch = epoch
	w.mu.Unlock()
	return nil
}

func (w *workTrace) beginCall(ctx context.Context) {
	w.mu.Lock()
	w.running = 0
	w.ctx = ctx
	w.callEpoch = w.capture.workEpoch()
	w.mu.Unlock()
}

func (w *workTrace) endCall() {
	w.mu.Lock()
	w.ctx = nil
	w.callEpoch = 0
	w.running = 0
	w.mu.Unlock()
}
