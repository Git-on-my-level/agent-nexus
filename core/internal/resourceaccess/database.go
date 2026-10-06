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
	Read  func(string) string
	Check func(context.Context, QueryRower, any) error
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

type DB struct{ raw *sql.DB }
type Tx struct{ raw *sql.Tx }

func NewDB(raw *sql.DB) *DB {
	if raw == nil {
		return nil
	}
	return &DB{raw: raw}
}
func (d *DB) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return d.raw.QueryContext(ctx, ReadQuery(ctx, q), args...)
}
func (d *DB) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	return d.raw.QueryRowContext(ctx, ReadQuery(ctx, q), args...)
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
		if err := p.Check(ctx, t.raw, args); err != nil {
			return nil, err
		}
	}
	return t.raw.ExecContext(ctx, q, args...)
}
func (t *Tx) Commit() error   { return t.raw.Commit() }
func (t *Tx) Rollback() error { return t.raw.Rollback() }
