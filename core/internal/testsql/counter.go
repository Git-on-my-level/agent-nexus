// Package testsql instruments real SQLite reads in integration tests. It is not
// used by runtime code and does not replace SQL execution with mocks.
package testsql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"sync/atomic"

	"modernc.org/sqlite"
)

type Counter struct{ queries atomic.Int64 }

func (c *Counter) Count() int64 { return c.queries.Load() }
func (c *Counter) Reset()       { c.queries.Store(0) }

// Open wraps each connection and prepared statement, counting QueryContext and
// QueryRowContext alike. Each test gets its own counter and connection pool.
func Open(dsn string) (*sql.DB, *Counter) {
	counter := &Counter{}
	db := sql.OpenDB(&connector{dsn: dsn, underlying: &sqlite.Driver{}, counter: counter})
	db.SetMaxOpenConns(1)
	return db, counter
}

type connector struct {
	dsn        string
	underlying *sqlite.Driver
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
	c.counter.queries.Add(1)
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, query, args)
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
