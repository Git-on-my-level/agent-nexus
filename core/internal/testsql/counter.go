// Package testsql instruments real SQLite reads in integration tests. It is not
// used by runtime code and does not replace SQL execution with mocks.
package testsql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

type ReadQuery struct {
	SQL  string
	Args []any
}

func (c *Counter) RowsRead() int64 { return int64(c.ReturnedRows()) }
func (c *Counter) Reads() []ReadQuery {
	out := []ReadQuery{}
	for _, s := range c.Statements() {
		out = append(out, ReadQuery{SQL: s.SQL, Args: s.Args})
	}
	return out
}

type Statement struct {
	SQL     string
	Args    []any
	Rows    int
	Elapsed time.Duration
}
type Counter struct {
	queries    atomic.Int64
	mu         sync.Mutex
	statements []*Statement
}

func (c *Counter) Statements() []Statement {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []Statement{}
	for _, s := range c.statements {
		out = append(out, *s)
	}
	return out
}
func (c *Counter) ReturnedRows() int {
	total := 0
	for _, s := range c.Statements() {
		total += s.Rows
	}
	return total
}
func (c *Counter) start(query string, args []driver.NamedValue) *Statement {
	c.queries.Add(1)
	s := &Statement{SQL: query}
	for _, v := range args {
		s.Args = append(s.Args, v.Value)
	}
	c.mu.Lock()
	c.statements = append(c.statements, s)
	c.mu.Unlock()
	return s
}

type countedRows struct {
	driver.Rows
	counter   *Counter
	statement *Statement
	started   time.Time
}

func (r *countedRows) Next(dest []driver.Value) error {
	err := r.Rows.Next(dest)
	r.counter.mu.Lock()
	r.statement.Elapsed = time.Since(r.started)
	if err == nil {
		r.statement.Rows++
	}
	r.counter.mu.Unlock()
	return err
}

func (c *Counter) Count() int64 { return c.queries.Load() }
func (c *Counter) Reset()       { c.queries.Store(0); c.mu.Lock(); c.statements = nil; c.mu.Unlock() }

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
	statement := c.counter.start(query, args)
	started := time.Now()
	rows, err := c.Conn.(driver.QueryerContext).QueryContext(ctx, query, args)
	if err != nil {
		return nil, err
	}
	return &countedRows{Rows: rows, counter: c.counter, statement: statement, started: started}, nil
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
	return &countedStmt{Stmt: stmt, counter: c.counter, query: query}, nil
}

type countedStmt struct {
	driver.Stmt
	counter *Counter
	query   string
}

func (s *countedStmt) Query(args []driver.Value) (driver.Rows, error) {
	named := []driver.NamedValue{}
	for i, v := range args {
		named = append(named, driver.NamedValue{Ordinal: i + 1, Value: v})
	}
	statement := s.counter.start(s.query, named)
	started := time.Now()
	rows, err := s.Stmt.Query(args)
	if err != nil {
		return nil, err
	}
	return &countedRows{Rows: rows, counter: s.counter, statement: statement, started: started}, nil
}

func (s *countedStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	statement := s.counter.start(s.query, args)
	started := time.Now()
	rows, err := s.Stmt.(driver.StmtQueryContext).QueryContext(ctx, args)
	if err != nil {
		return nil, err
	}
	return &countedRows{Rows: rows, counter: s.counter, statement: statement, started: started}, nil
}

func (s *countedStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	return s.Stmt.(driver.StmtExecContext).ExecContext(ctx, args)
}

func (c *countedConn) ResetSession(ctx context.Context) error {
	if inner, ok := c.Conn.(driver.SessionResetter); ok {
		return inner.ResetSession(ctx)
	}
	return nil
}
func (c *countedConn) IsValid() bool {
	if inner, ok := c.Conn.(driver.Validator); ok {
		return inner.IsValid()
	}
	return true
}
func (c *countedConn) CheckNamedValue(value *driver.NamedValue) error {
	if inner, ok := c.Conn.(driver.NamedValueChecker); ok {
		return inner.CheckNamedValue(value)
	}
	return driver.ErrSkip
}
