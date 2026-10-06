// Package testsql instruments real SQLite reads in integration tests. It is not
// used by runtime code and does not replace SQL execution with mocks.
package testsql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"sync"
	"sync/atomic"

	_ "modernc.org/sqlite"
)

type ReadQuery struct {
	SQL  string
	Args []any
}
type Counter struct {
	queries atomic.Int64
	rows    atomic.Int64
	mu      sync.Mutex
	reads   []ReadQuery
}

func (c *Counter) Count() int64    { return c.queries.Load() }
func (c *Counter) RowsRead() int64 { return c.rows.Load() }
func (c *Counter) Reset() {
	c.queries.Store(0)
	c.rows.Store(0)
	c.mu.Lock()
	c.reads = nil
	c.mu.Unlock()
}
func (c *Counter) Reads() []ReadQuery {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]ReadQuery{}, c.reads...)
}
func (c *Counter) record(q string, args []driver.NamedValue) {
	c.queries.Add(1)
	values := make([]any, len(args))
	for i, arg := range args {
		values[i] = arg.Value
	}
	c.mu.Lock()
	c.reads = append(c.reads, ReadQuery{SQL: q, Args: values})
	c.mu.Unlock()
}

// Open wraps each connection and prepared statement, counting QueryContext and
// QueryRowContext alike. Each test gets its own counter and connection pool.
func Open(dsn string) (*sql.DB, *Counter) {
	counter := &Counter{}
	// Wrap the registered runtime driver, including its deterministic functions,
	// instead of creating a bare driver that omits production query behavior.
	runtimeDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		panic(err)
	}
	runtimeDriver := runtimeDB.Driver()
	_ = runtimeDB.Close()
	db := sql.OpenDB(&connector{dsn: dsn, underlying: runtimeDriver, counter: counter})
	db.SetMaxOpenConns(1)
	return db, counter
}

type connector struct {
	dsn        string
	underlying driver.Driver
	counter    *Counter
}

func (c *connector) Driver() driver.Driver { return c.underlying }
func (c *connector) Connect(context.Context) (driver.Conn, error) {
	conn, err := c.underlying.Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return &countedConn{Conn: conn, counter: c.counter}, nil
}

type countedConn struct {
	driver.Conn
	counter *Counter
}

func (c *countedConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.counter.record(query, args)
	rows, err := c.Conn.(driver.QueryerContext).QueryContext(ctx, query, args)
	if err != nil {
		return nil, err
	}
	return &countedRows{Rows: rows, counter: c.counter}, nil
}

type countedRows struct {
	driver.Rows
	counter *Counter
}

func (r *countedRows) Next(values []driver.Value) error {
	err := r.Rows.Next(values)
	if err == nil {
		r.counter.rows.Add(1)
	}
	return err
}

func (c *countedConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, query, args)
}

func (c *countedConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
}

func (c *countedConn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}

func (c *countedConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	stmt, err := c.Conn.(driver.ConnPrepareContext).PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	return &countedStmt{Stmt: stmt, counter: c.counter}, nil
}

type countedStmt struct {
	driver.Stmt
	counter *Counter
}

func (s *countedStmt) Query(args []driver.Value) (driver.Rows, error) {
	s.counter.queries.Add(1)
	return s.Stmt.Query(args)
}

func (s *countedStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	s.counter.queries.Add(1)
	return s.Stmt.(driver.StmtQueryContext).QueryContext(ctx, args)
}

func (s *countedStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	return s.Stmt.(driver.StmtExecContext).ExecContext(ctx, args)
}
