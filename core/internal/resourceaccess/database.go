// Package resourceaccess applies the request's record policy at database reads
// and writes without coupling auxiliary stores to the canonical primitive store.
package resourceaccess

import (
	"context"
	"database/sql"
)

type QueryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type Policy struct {
	Read     func(string) string
	ReadOnDB func(context.Context, QueryRower, string, []any) (string, []any)
	Check    func(context.Context, QueryRower, any) error
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

type DB struct{ raw *sql.DB }
type Tx struct{ raw *sql.Tx }

type pinnedReadKey struct{}
type pinnedRead struct {
	db *sql.DB
	tx *sql.Tx
}

// PinReads routes reads of this database through one read-only snapshot. The
// policy owner admits that snapshot before it is exposed. Business mutations
// continue using ordinary transactions and their current canonical policy.
func (d *DB) PinReads(ctx context.Context, admit func(context.Context, QueryRower) (context.Context, error)) (context.Context, func(), error) {
	_, _ = readOnDB(ctx, d.raw, "SELECT id FROM cards", nil)
	tx, err := d.raw.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ctx, nil, err
	}
	next, err := admit(ctx, tx)
	if err != nil {
		_ = tx.Rollback()
		return ctx, nil, err
	}
	return context.WithValue(next, pinnedReadKey{}, pinnedRead{d.raw, tx}), func() { _ = tx.Rollback() }, nil
}

func (d *DB) readHandle(ctx context.Context) interface {
	QueryRower
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
} {
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
func (d *DB) CheckValues(ctx context.Context, values any) error {
	if err := ValidateText(values); err != nil {
		return err
	}
	if p, ok := PolicyFrom(ctx); ok {
		return p.Check(ctx, d.raw, values)
	}
	return nil
}
func (d *DB) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	if err := ValidateSQLValues(args); err != nil {
		return nil, err
	}
	if _, ok := PolicyFrom(ctx); !ok {
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
	tx, e := d.raw.BeginTx(ctx, o)
	if e != nil {
		return nil, e
	}
	return &Tx{raw: tx}, nil
}
func (t *Tx) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return t.raw.QueryContext(ctx, ReadQuery(ctx, q), args...)
}
func (t *Tx) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	return t.raw.QueryRowContext(ctx, ReadQuery(ctx, q), args...)
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
	return t.raw.ExecContext(ctx, q, args...)
}
func (t *Tx) Commit() error   { return t.raw.Commit() }
func (t *Tx) Rollback() error { return t.raw.Rollback() }

// ReadSnapshot keeps one read-only SQLite snapshot while retaining the request's
// epoch-validated denial cache. It exposes no mutation or raw transaction handle.
// A cache captured outside this transaction is safe: each consuming statement
// compares its epoch inside the transaction and falls back to its canonical graph.
// Business write transactions continue to use BeginTx and canonical checks.
type ReadSnapshot struct {
	raw *sql.Tx
}

func (d *DB) BeginReadSnapshot(ctx context.Context, admissionQuery string) (*ReadSnapshot, error) {
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
func (t *ReadSnapshot) Commit() error   { return t.raw.Commit() }
func (t *ReadSnapshot) Rollback() error { return t.raw.Rollback() }
