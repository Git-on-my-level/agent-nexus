// Package resourceaccess applies the request's record policy at database reads
// and writes without coupling auxiliary stores to the canonical primitive store.
package resourceaccess

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"sync/atomic"
)

type QueryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type Policy struct {
	// ReadableValues checks arbitrary cached projection provenance in the same
	// read snapshot, without interpreting denial as a database failure.
	ReadableValues func(context.Context, QueryRower, any) (bool, error)
	// Optional transaction-local admission. Implementations must refresh their
	// canonical proof after every authority epoch change and fail closed on a
	// consuming-statement mismatch. Never share a proof with another transaction.
	TransactionRead func(context.Context, QueryRower, string, []any) (string, []any)
	Read            func(string) string
	ReadOnDB        func(context.Context, QueryRower, string, []any) (string, []any)
	Check           func(context.Context, QueryRower, any) error
}
type policyKey struct{}

func WithPolicy(ctx context.Context, p Policy) context.Context {
	return context.WithValue(ctx, policyKey{}, p)
}
func WithoutPolicy(ctx context.Context) context.Context {
	return context.WithValue(ctx, policyKey{}, struct{}{})
}
func PolicyFrom(ctx context.Context) (Policy, bool) {
	p, ok := ctx.Value(policyKey{}).(Policy)
	return p, ok
}
func ReadQuery(ctx context.Context, q string) string {
	if p, ok := PolicyFrom(ctx); ok {
		return p.Read(q)
	}
	return q
}

func readOnDB(ctx context.Context, q QueryRower, query string, args []any) (string, []any) {
	if p, ok := PolicyFrom(ctx); ok && p.ReadOnDB != nil {
		return p.ReadOnDB(ctx, q, query, args)
	}
	return ReadQuery(ctx, query), args
}

type WriteHook func(context.Context, *sql.Tx, string, []any) error

type DB struct {
	raw        *sql.DB
	afterWrite WriteHook
	savepoints atomic.Uint64
}
type Tx struct {
	raw        *sql.Tx
	afterWrite WriteHook
	savepoint  string
	closed     bool
	ctx        context.Context
}
type writeTransactionKey struct{}
type writeTransaction struct {
	db    *sql.DB
	tx    *sql.Tx
	hooks *completionHooks
}

// completionHooks run once the outermost WriteTransaction has finished.
type completionHooks struct {
	mu  sync.Mutex
	fns []func(ctx context.Context, committed bool)
}

// WriteTransaction binds nested scoped reads/writes to one transaction. Nested
// BeginTx calls use savepoints, preserving local rollback and outer atomicity.
// A WriteTransaction opened inside another one joins it.
func (d *DB) WriteTransaction(ctx context.Context, run func(context.Context, *Tx) error) error {
	if bound, ok := ctx.Value(writeTransactionKey{}).(writeTransaction); ok && bound.db == d.raw {
		tx, err := d.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err := run(ctx, tx); err != nil {
			return err
		}
		return tx.Commit()
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	hooks := &completionHooks{}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
		hooks.run(ctx, committed)
	}()
	bound := context.WithValue(ctx, writeTransactionKey{}, writeTransaction{d.raw, tx.raw, hooks})
	if err := run(bound, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// AfterWriteTransaction defers fn until the outermost WriteTransaction bound to
// ctx finishes, with a context outside that transaction and whether it
// committed. Work inside a nested transaction is durable only then. It reports
// false, without calling fn, when ctx is not inside a WriteTransaction.
func AfterWriteTransaction(ctx context.Context, fn func(ctx context.Context, committed bool)) bool {
	bound, ok := ctx.Value(writeTransactionKey{}).(writeTransaction)
	if !ok || bound.hooks == nil {
		return false
	}
	bound.hooks.mu.Lock()
	bound.hooks.fns = append(bound.hooks.fns, fn)
	bound.hooks.mu.Unlock()
	return true
}

func (h *completionHooks) run(ctx context.Context, committed bool) {
	h.mu.Lock()
	fns := h.fns
	h.fns = nil
	h.mu.Unlock()
	for _, fn := range fns {
		fn(ctx, committed)
	}
}

// SetWriteHook is construction-only. Hooks maintain derived state in the source
// transaction after its ordinary authorization check; they confer no read grant.
func (d *DB) SetWriteHook(h WriteHook) { d.afterWrite = h }

type pinnedReadKey struct{}
type pinnedRead struct {
	db *sql.DB
	tx *sql.Tx
}

// InWriteTransaction identifies a mutable borrowed snapshot for this handle.
// Read optimizations must not treat it as an immutable page transaction.
func (d *DB) InWriteTransaction(ctx context.Context) bool {
	bound, ok := ctx.Value(writeTransactionKey{}).(writeTransaction)
	return ok && bound.db == d.raw
}

// PinReads routes reads of this database through one read-only snapshot. The
// policy owner admits that snapshot before it is exposed. Business mutations
// continue using ordinary transactions and their current canonical policy.
func (d *DB) PinReads(ctx context.Context, admit func(context.Context, QueryRower) (context.Context, error)) (context.Context, func(), error) {
	// Projection admission must see the caller's uncommitted ownership writes
	// too. Borrow an existing write/read snapshot instead of opening a second
	// connection with older authority (or stalling a one-connection pool).
	snapshot, err := d.BeginReadSnapshot(ctx, "SELECT id FROM cards")
	if err != nil {
		return ctx, nil, err
	}
	next, err := admit(ctx, snapshot.raw)
	if err != nil {
		_ = snapshot.Rollback()
		return ctx, nil, err
	}
	return context.WithValue(next, pinnedReadKey{}, pinnedRead{d.raw, snapshot.raw}), func() { _ = snapshot.Rollback() }, nil
}

func (d *DB) readHandle(ctx context.Context) interface {
	QueryRower
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
} {
	if bound, ok := ctx.Value(writeTransactionKey{}).(writeTransaction); ok && bound.db == d.raw {
		return bound.tx
	}
	if pin, ok := ctx.Value(pinnedReadKey{}).(pinnedRead); ok && pin.db == d.raw {
		return pin.tx
	}
	return d.raw
}

func NewDB(raw *sql.DB) *DB {
	if raw == nil {
		return nil
	}
	return &DB{raw: raw}
}
func (d *DB) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	handle := d.readHandle(ctx)
	q, args = readOnDB(ctx, handle, q, args)
	return handle.QueryContext(ctx, q, args...)
}
func (d *DB) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	handle := d.readHandle(ctx)
	q, args = readOnDB(ctx, handle, q, args)
	return handle.QueryRowContext(ctx, q, args...)
}

// ReadableValues authorizes a cached projection on the underlying connection.
// A missing recipient policy fails closed; scoped query wrappers must not be
// applied a second time to the policy's own authorization statements.
func (d *DB) ReadableValues(ctx context.Context, values any) (bool, error) {
	return readableValues(ctx, d.readHandle(ctx), values)
}
func (t *Tx) ReadableValues(ctx context.Context, values any) (bool, error) {
	return readableValues(ctx, t.raw, values)
}
func readableValues(ctx context.Context, q QueryRower, values any) (bool, error) {
	p, ok := PolicyFrom(ctx)
	if !ok || p.ReadableValues == nil {
		return false, nil
	}
	return p.ReadableValues(ctx, q, values)
}

func (d *DB) CheckValues(ctx context.Context, values any) error {
	if err := ValidateText(values); err != nil {
		return err
	}
	if p, ok := PolicyFrom(ctx); ok {
		return p.Check(ctx, d.readHandle(ctx), values)
	}
	return nil
}
func (d *DB) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	if err := ValidateSQLValues(args); err != nil {
		return nil, err
	}
	bound, nested := ctx.Value(writeTransactionKey{}).(writeTransaction)
	if _, ok := PolicyFrom(ctx); !ok && d.afterWrite == nil && (!nested || bound.db != d.raw) {
		return d.raw.ExecContext(ctx, q, args...)
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
func (d *DB) BeginTx(ctx context.Context, o *sql.TxOptions) (*Tx, error) {
	if bound, ok := ctx.Value(writeTransactionKey{}).(writeTransaction); ok && bound.db == d.raw {
		name := fmt.Sprintf("anx_nested_%d", d.savepoints.Add(1))
		if _, err := bound.tx.ExecContext(ctx, "SAVEPOINT "+name); err != nil {
			return nil, err
		}
		return &Tx{raw: bound.tx, afterWrite: d.afterWrite, savepoint: name, ctx: ctx}, nil
	}
	tx, e := d.raw.BeginTx(ctx, o)
	if e != nil {
		return nil, e
	}
	return &Tx{raw: tx, afterWrite: d.afterWrite}, nil
}
func (t *Tx) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	if p, ok := PolicyFrom(ctx); ok && p.TransactionRead != nil {
		q, args = p.TransactionRead(ctx, t.raw, q, args)
	} else {
		q = ReadQuery(ctx, q)
	}
	return t.raw.QueryContext(ctx, q, args...)
}
func (t *Tx) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	if p, ok := PolicyFrom(ctx); ok && p.TransactionRead != nil {
		q, args = p.TransactionRead(ctx, t.raw, q, args)
	} else {
		q = ReadQuery(ctx, q)
	}
	return t.raw.QueryRowContext(ctx, q, args...)
}

// PrepareReadContext binds visibility at preparation for request-local repeated
// reads. Never share the statement across principals or use it for mutations.
func (t *Tx) PrepareReadContext(ctx context.Context, q string) (*sql.Stmt, error) {
	return t.raw.PrepareContext(ctx, ReadQuery(ctx, q))
}
func (t *Tx) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	if err := ValidateSQLValues(args); err != nil {
		return nil, err
	}
	if p, ok := PolicyFrom(ctx); ok {
		if err := p.Check(ctx, t.raw, SQLValues{Query: q, Args: args}); err != nil {
			return nil, err
		}
	}
	result, err := t.raw.ExecContext(ctx, q, args...)
	if err == nil && t.afterWrite != nil {
		err = t.afterWrite(ctx, t.raw, q, args)
	}
	return result, err
}
func (t *Tx) Commit() error {
	if t.savepoint == "" {
		return t.raw.Commit()
	}
	if t.closed {
		return sql.ErrTxDone
	}
	t.closed = true
	_, err := t.raw.ExecContext(t.ctx, "RELEASE SAVEPOINT "+t.savepoint)
	return err
}
func (t *Tx) Rollback() error {
	if t.savepoint == "" {
		return t.raw.Rollback()
	}
	if t.closed {
		return sql.ErrTxDone
	}
	t.closed = true
	if _, err := t.raw.ExecContext(context.WithoutCancel(t.ctx), "ROLLBACK TO SAVEPOINT "+t.savepoint); err != nil {
		return err
	}
	_, err := t.raw.ExecContext(context.WithoutCancel(t.ctx), "RELEASE SAVEPOINT "+t.savepoint)
	return err
}

// ReadSnapshot keeps one read-only SQLite snapshot while retaining the request's
// epoch-validated denial cache. It exposes no mutation or raw transaction handle.
// A cache captured outside this transaction is safe: each consuming statement
// compares its epoch inside the transaction and falls back to its canonical graph.
// Business write transactions continue to use BeginTx and canonical checks.
type ReadSnapshot struct {
	raw      *sql.Tx
	borrowed bool
}

func (d *DB) BeginReadSnapshot(ctx context.Context, admissionQuery string) (*ReadSnapshot, error) {
	if bound, ok := ctx.Value(writeTransactionKey{}).(writeTransaction); ok && bound.db == d.raw {
		return &ReadSnapshot{raw: bound.tx, borrowed: true}, nil
	}
	// A page may already own an admitted read transaction. Reuse it so nested
	// readers see the same authority and work with a one-connection pool.
	// Only the outer owner may finish this transaction.
	if pin, ok := ctx.Value(pinnedReadKey{}).(pinnedRead); ok && pin.db == d.raw {
		return &ReadSnapshot{raw: pin.tx, borrowed: true}, nil
	}
	// Capture/cache authority before holding a transaction connection, including
	// pools limited to one connection. No business query is executed here.
	_, _ = readOnDB(ctx, d.raw, admissionQuery, nil)
	tx, err := d.raw.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	return &ReadSnapshot{raw: tx}, nil
}
func (t *ReadSnapshot) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	q, args = readOnDB(ctx, t.raw, q, args)
	return t.raw.QueryContext(ctx, q, args...)
}
func (t *ReadSnapshot) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	q, args = readOnDB(ctx, t.raw, q, args)
	return t.raw.QueryRowContext(ctx, q, args...)
}
func (t *ReadSnapshot) Commit() error {
	if t.borrowed {
		return nil
	}
	return t.raw.Commit()
}
func (t *ReadSnapshot) Rollback() error {
	if t.borrowed {
		return nil
	}
	return t.raw.Rollback()
}
