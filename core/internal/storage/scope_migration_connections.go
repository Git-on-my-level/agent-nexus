package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
)

// sql.DB.Close does NOT drain outstanding transactions or sql.Conn handles.
// Tie the OS lease to driver connection lifetimes as well as the DB owner, so
// a still-usable old transaction prevents a second server from being admitted.
type scopeProcessLease struct {
	mu     sync.Mutex
	file   *os.File
	refs   int
	closed bool
}

func (l *scopeProcessLease) retain() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return errors.New("workspace connection owner closed")
	}
	l.refs++
	return nil
}
func (l *scopeProcessLease) release(owner bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if owner {
		if l.closed {
			return nil
		}
		l.closed = true
	}
	l.refs--
	if l.refs == 0 && l.file != nil {
		return l.file.Close()
	}
	return nil
}

func (l *scopeProcessLease) activate(path string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return errors.New("workspace connection owner closed")
	}
	if l.file != nil {
		return nil
	}
	f, err := acquireScopeProcessLock(path)
	if err != nil {
		return err
	}
	l.file = f
	return nil
}

// Unexpanded legacy storage callers retain their historical reopen behavior.
// Once A registers expansion, every compatible open acquires the serving lease.
func enableExpandedScopeLease(ctx context.Context, db *sql.DB, lease *scopeProcessLease, root string) error {
	var expanded int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE name='scope_format_state'`).Scan(&expanded); err != nil {
		return err
	}
	if expanded == 0 {
		return nil
	}
	return lease.activate(filepath.Join(root, ".scope-serving.lock"))
}

// EnableScopeServingLease is for an already-open workspace on which integration
// has just installed expansion. Pre-bridge connections must be stopped before
// enabling this protocol, as documented for format transition.
func (w *Workspace) EnableScopeServingLease(ctx context.Context) error {
	if w == nil || w.db == nil || w.processLock == nil {
		return errors.New("workspace unavailable")
	}
	return enableExpandedScopeLease(ctx, w.db, w.processLock, w.layout.RootDir)
}

type scopeConnector struct {
	registered driver.Driver
	dsn        string
	lease      *scopeProcessLease
}

func (c *scopeConnector) Connect(ctx context.Context) (driver.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := c.lease.retain(); err != nil {
		return nil, err
	}
	conn, err := c.registered.Open(c.dsn)
	if err != nil {
		c.lease.release(false)
		return nil, err
	}
	wrapped := &scopeConnection{Conn: conn, lease: c.lease}
	if err := ctx.Err(); err != nil {
		wrapped.Close()
		return nil, err
	}
	return wrapped, nil
}
func (c *scopeConnector) Driver() driver.Driver { return scopeDriver{connector: c} }

type scopeDriver struct{ connector *scopeConnector }

func (d scopeDriver) Open(dsn string) (driver.Conn, error) {
	if dsn != d.connector.dsn {
		return nil, errors.New("workspace driver cannot open another database")
	}
	return d.connector.Connect(context.Background())
}

// Get the registered driver's instance: using a new sqlite.Driver would lose
// registered reference functions and connection hooks owned by resourceaccess.
func openScopeDatabase(dsn string, lease *scopeProcessLease) (*sql.DB, error) {
	registered, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	d := registered.Driver()
	if err := registered.Close(); err != nil {
		return nil, err
	}
	db := sql.OpenDB(&scopeConnector{registered: d, dsn: dsn, lease: lease})
	// A physical connection reparses the complete trigger schema. Bound the
	// idle cache across ticks; database/sql's default two idle connections
	// caused repeated schema churn. InitializeWorkspace then applies the
	// configured MaxOpenConns / lifetime limits. The open cap is generous
	// (default 64) because some readers hydrate related records while holding
	// their outer result cursor; a cap of 1 would deadlock those nested reads.
	db.SetMaxIdleConns(8)
	return db, nil
}

type scopeConnection struct {
	driver.Conn
	lease     *scopeProcessLease
	closeOnce sync.Once
	closeErr  error
	invalid   atomic.Bool
}

func (c *scopeConnection) invalidate() {
	c.invalid.Store(true)
}

func (c *scopeConnection) Close() error {
	c.closeOnce.Do(func() {
		c.closeErr = c.Conn.Close()
		// A failed physical close cannot safely release serving ownership.
		if c.closeErr == nil {
			c.closeErr = c.lease.release(false)
		}
	})
	return c.closeErr
}
func (c *scopeConnection) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	if conn, ok := c.Conn.(driver.ConnBeginTx); ok {
		return conn.BeginTx(ctx, options)
	}
	if options.ReadOnly || options.Isolation != 0 {
		return nil, errors.New("unsupported transaction options")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c.Conn.Begin()
}
func (c *scopeConnection) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	var (
		stmt driver.Stmt
		err  error
	)
	if conn, ok := c.Conn.(driver.ConnPrepareContext); ok {
		stmt, err = conn.PrepareContext(ctx, query)
	} else if err = ctx.Err(); err == nil {
		stmt, err = c.Conn.Prepare(query)
	}
	if err != nil {
		return nil, err
	}
	return &scopeStmt{Stmt: stmt, conn: c}, nil
}
func (c *scopeConnection) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if conn, ok := c.Conn.(driver.ExecerContext); ok {
		return conn.ExecContext(ctx, query, args)
	}
	return nil, driver.ErrSkip
}
func (c *scopeConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if conn, ok := c.Conn.(driver.QueryerContext); ok {
		rows, err := conn.QueryContext(ctx, query, args)
		if err != nil {
			if ctx.Err() != nil {
				c.invalidate()
			}
			return nil, err
		}
		return &scopeRows{Rows: rows, ctx: ctx, conn: c}, nil
	}
	return nil, driver.ErrSkip
}
func (c *scopeConnection) CheckNamedValue(value *driver.NamedValue) error {
	if conn, ok := c.Conn.(driver.NamedValueChecker); ok {
		return conn.CheckNamedValue(value)
	}
	return driver.ErrSkip
}
func (c *scopeConnection) ResetSession(ctx context.Context) error {
	if c.invalid.Load() {
		return driver.ErrBadConn
	}
	if conn, ok := c.Conn.(driver.SessionResetter); ok {
		return conn.ResetSession(ctx)
	}
	return ctx.Err()
}
func (c *scopeConnection) IsValid() bool {
	if c.invalid.Load() {
		return false
	}
	if conn, ok := c.Conn.(driver.Validator); ok {
		return conn.IsValid()
	}
	return true
}
func (c *scopeConnection) Ping(ctx context.Context) error {
	if conn, ok := c.Conn.(driver.Pinger); ok {
		return conn.Ping(ctx)
	}
	return ctx.Err()
}

// scopeStmt keeps prepared-statement reads under the same cancellation
// invalidation as direct queries. modernc implements database/sql's context
// interfaces, but the wrapper still mirrors database/sql's legacy Query/Exec
// fallback for any driver that does not, so a prepared statement cannot bypass
// cancellation handling.
type scopeStmt struct {
	driver.Stmt
	conn *scopeConnection
}

func (s *scopeStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	var (
		rows driver.Rows
		err  error
	)
	if stmt, ok := s.Stmt.(driver.StmtQueryContext); ok {
		rows, err = stmt.QueryContext(ctx, args)
	} else if err = ctx.Err(); err == nil {
		var values []driver.Value
		if values, err = namedValues(args); err == nil {
			rows, err = s.Stmt.Query(values)
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			s.conn.invalidate()
		}
		return nil, err
	}
	return &scopeRows{Rows: rows, ctx: ctx, conn: s.conn}, nil
}

func (s *scopeStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	if stmt, ok := s.Stmt.(driver.StmtExecContext); ok {
		return stmt.ExecContext(ctx, args)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	values, err := namedValues(args)
	if err != nil {
		return nil, err
	}
	return s.Stmt.Exec(values)
}

// namedValues mirrors database/sql's legacy-driver fallback, which rejects
// named parameters instead of silently binding them by position.
func namedValues(args []driver.NamedValue) ([]driver.Value, error) {
	values := make([]driver.Value, len(args))
	for i, arg := range args {
		if arg.Name != "" {
			return nil, errors.New("sql: driver does not support the use of Named Parameters")
		}
		values[i] = arg.Value
	}
	return values, nil
}

// scopeRows owns the query context so a canceled or expired read can mark its
// physical connection invalid before database/sql returns it to the pool. The
// native connection is still closed only by scopeConnection.Close, which keeps
// the process lease release idempotent.
type scopeRows struct {
	driver.Rows
	ctx       context.Context
	conn      *scopeConnection
	eof       bool
	closeOnce sync.Once
	closeErr  error
}

func (r *scopeRows) Next(dest []driver.Value) error {
	err := r.Rows.Next(dest)
	if err == io.EOF {
		r.eof = true
	}
	return err
}

func (r *scopeRows) Close() error {
	r.closeOnce.Do(func() {
		r.closeErr = r.Rows.Close()
		// A failed close leaves the native cursor in an unknown state, so the
		// connection cannot safely return to the pool. A fully consumed cursor
		// is otherwise safe even if the request context was canceled after the
		// last row; only an early close with a canceled context is not.
		if r.closeErr != nil || (!r.eof && r.ctx.Err() != nil) {
			r.conn.invalidate()
		}
	})
	return r.closeErr
}

func (r *scopeRows) ColumnTypeDatabaseTypeName(index int) string {
	if rows, ok := r.Rows.(driver.RowsColumnTypeDatabaseTypeName); ok {
		return rows.ColumnTypeDatabaseTypeName(index)
	}
	return ""
}

func (r *scopeRows) ColumnTypeLength(index int) (int64, bool) {
	if rows, ok := r.Rows.(driver.RowsColumnTypeLength); ok {
		return rows.ColumnTypeLength(index)
	}
	return 0, false
}

func (r *scopeRows) ColumnTypeNullable(index int) (bool, bool) {
	if rows, ok := r.Rows.(driver.RowsColumnTypeNullable); ok {
		return rows.ColumnTypeNullable(index)
	}
	return false, false
}

func (r *scopeRows) ColumnTypePrecisionScale(index int) (int64, int64, bool) {
	if rows, ok := r.Rows.(driver.RowsColumnTypePrecisionScale); ok {
		return rows.ColumnTypePrecisionScale(index)
	}
	return 0, 0, false
}

func (r *scopeRows) ColumnTypeScanType(index int) reflect.Type {
	if rows, ok := r.Rows.(driver.RowsColumnTypeScanType); ok {
		return rows.ColumnTypeScanType(index)
	}
	return reflect.TypeOf("")
}

func (r *scopeRows) HasNextResultSet() bool {
	if rows, ok := r.Rows.(driver.RowsNextResultSet); ok {
		return rows.HasNextResultSet()
	}
	return false
}

func (r *scopeRows) NextResultSet() error {
	if rows, ok := r.Rows.(driver.RowsNextResultSet); ok {
		err := rows.NextResultSet()
		if err == nil {
			// A new result set has not been consumed yet, so an earlier EOF
			// must not make an early close look like a completed read.
			r.eof = false
		}
		return err
	}
	return io.EOF
}
