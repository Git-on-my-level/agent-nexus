package perfguard

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	_ "modernc.org/sqlite"
)

// Statement is the SQL actually submitted to SQLite, after policy rewriting.
// Arguments are retained only for EXPLAIN on synthetic data; never log them.
type Statement struct {
	SQL  string
	Args []any
}
type Capture struct {
	mu            sync.Mutex
	statements    []Statement
	queries, rows int
	enabled       bool
}

func (c *Capture) Start() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.statements = nil
	c.queries = 0
	c.rows = 0
	c.enabled = true
}
func (c *Capture) Stop() ([]Statement, int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enabled = false
	return append([]Statement(nil), c.statements...), c.queries, c.rows
}
func (c *Capture) record(q string, args []driver.NamedValue) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.enabled {
		return
	}
	v := make([]any, len(args))
	for i, a := range args {
		if b, ok := a.Value.([]byte); ok {
			v[i] = append([]byte(nil), b...)
		} else {
			v[i] = a.Value
		}
	}
	c.statements = append(c.statements, Statement{q, v})
	c.queries++
}
func (c *Capture) row() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.enabled {
		c.rows++
	}
}

var driverID atomic.Uint64

// Open instruments a separate pool, including raw auth reads, transactions and
// prepared statements. The normal workspace connection remains uninstrumented
// for fixture construction and EXPLAIN (which must not recursively capture).
func Open(dsn string) (*sql.DB, *Capture, error) {
	base, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, nil, err
	}
	d := base.Driver() // includes the canonical driver's registered scalar functions
	_ = base.Close()
	c := &Capture{}
	name := fmt.Sprintf("scale-sqlite-%d", driverID.Add(1))
	sql.Register(name, &captureDriver{base: d, capture: c})
	db, err := sql.Open(name, dsn)
	return db, c, err
}

type captureDriver struct {
	base    driver.Driver
	capture *Capture
}

func (d *captureDriver) Open(name string) (driver.Conn, error) {
	c, e := d.base.Open(name)
	if e != nil {
		return nil, e
	}
	return &captureConn{Conn: c, capture: d.capture}, nil
}

type captureConn struct {
	driver.Conn
	capture *Capture
}

func (c *captureConn) Prepare(q string) (driver.Stmt, error) {
	s, e := c.Conn.Prepare(q)
	if e != nil {
		return nil, e
	}
	return &captureStmt{Stmt: s, q: q, capture: c.capture}, nil
}
func (c *captureConn) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	s, e := c.Conn.(driver.ConnPrepareContext).PrepareContext(ctx, q)
	if e != nil {
		return nil, e
	}
	return &captureStmt{Stmt: s, q: q, capture: c.capture}, nil
}
func (c *captureConn) BeginTx(ctx context.Context, o driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, o)
}
func (c *captureConn) Ping(ctx context.Context) error { return c.Conn.(driver.Pinger).Ping(ctx) }
func (c *captureConn) ExecContext(ctx context.Context, q string, a []driver.NamedValue) (driver.Result, error) {
	c.capture.record(q, a)
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, q, a)
}
func (c *captureConn) QueryContext(ctx context.Context, q string, a []driver.NamedValue) (driver.Rows, error) {
	c.capture.record(q, a)
	r, e := c.Conn.(driver.QueryerContext).QueryContext(ctx, q, a)
	if e != nil {
		return nil, e
	}
	return &captureRows{Rows: r, capture: c.capture}, nil
}

type captureStmt struct {
	driver.Stmt
	q       string
	capture *Capture
}

func named(a []driver.Value) []driver.NamedValue {
	out := make([]driver.NamedValue, len(a))
	for i, v := range a {
		out[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return out
}
func (s *captureStmt) Exec(a []driver.Value) (driver.Result, error) {
	s.capture.record(s.q, named(a))
	return s.Stmt.Exec(a)
}
func (s *captureStmt) Query(a []driver.Value) (driver.Rows, error) {
	s.capture.record(s.q, named(a))
	r, e := s.Stmt.Query(a)
	if e != nil {
		return nil, e
	}
	return &captureRows{Rows: r, capture: s.capture}, nil
}
func (s *captureStmt) ExecContext(ctx context.Context, a []driver.NamedValue) (driver.Result, error) {
	s.capture.record(s.q, a)
	return s.Stmt.(driver.StmtExecContext).ExecContext(ctx, a)
}
func (s *captureStmt) QueryContext(ctx context.Context, a []driver.NamedValue) (driver.Rows, error) {
	s.capture.record(s.q, a)
	r, e := s.Stmt.(driver.StmtQueryContext).QueryContext(ctx, a)
	if e != nil {
		return nil, e
	}
	return &captureRows{Rows: r, capture: s.capture}, nil
}

type captureRows struct {
	driver.Rows
	capture *Capture
}

func (r *captureRows) Next(dest []driver.Value) error {
	e := r.Rows.Next(dest)
	if e == nil {
		r.capture.row()
	} else if e != io.EOF {
		return e
	}
	return e
}
