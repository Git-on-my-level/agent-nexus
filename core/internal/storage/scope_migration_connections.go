package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"os"
	"path/filepath"
	"sync"
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
	return sql.OpenDB(&scopeConnector{registered: d, dsn: dsn, lease: lease}), nil
}

type scopeConnection struct {
	driver.Conn
	lease     *scopeProcessLease
	closeOnce sync.Once
	closeErr  error
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
	if conn, ok := c.Conn.(driver.ConnPrepareContext); ok {
		return conn.PrepareContext(ctx, query)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c.Conn.Prepare(query)
}
func (c *scopeConnection) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if conn, ok := c.Conn.(driver.ExecerContext); ok {
		return conn.ExecContext(ctx, query, args)
	}
	return nil, driver.ErrSkip
}
func (c *scopeConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if conn, ok := c.Conn.(driver.QueryerContext); ok {
		return conn.QueryContext(ctx, query, args)
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
	if conn, ok := c.Conn.(driver.SessionResetter); ok {
		return conn.ResetSession(ctx)
	}
	return ctx.Err()
}
func (c *scopeConnection) IsValid() bool {
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
