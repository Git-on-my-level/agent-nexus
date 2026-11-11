package storage

import (
	"context"
	"database/sql/driver"
	"io"
	"reflect"
	"testing"
)

type metadataRows struct {
	driver.Rows
}

func (r *metadataRows) Columns() []string              { return []string{"c"} }
func (r *metadataRows) Close() error                   { return nil }
func (r *metadataRows) Next(dest []driver.Value) error { return io.EOF }

func (r *metadataRows) ColumnTypeDatabaseTypeName(index int) string { return "TEXT" }
func (r *metadataRows) ColumnTypeLength(index int) (int64, bool)    { return 42, true }
func (r *metadataRows) ColumnTypeNullable(index int) (bool, bool)   { return true, true }
func (r *metadataRows) ColumnTypePrecisionScale(index int) (int64, int64, bool) {
	return 10, 2, true
}
func (r *metadataRows) ColumnTypeScanType(index int) reflect.Type { return reflect.TypeOf(int64(0)) }
func (r *metadataRows) HasNextResultSet() bool                    { return true }
func (r *metadataRows) NextResultSet() error                      { return nil }

func TestScopeRowsForwardsOptionalInterfaces(t *testing.T) {
	rows := &scopeRows{Rows: &metadataRows{}, ctx: context.Background(), conn: &scopeConnection{}}
	if got := rows.ColumnTypeDatabaseTypeName(0); got != "TEXT" {
		t.Fatalf("database type name = %q", got)
	}
	if length, ok := rows.ColumnTypeLength(0); !ok || length != 42 {
		t.Fatalf("length = %d, %v", length, ok)
	}
	if nullable, ok := rows.ColumnTypeNullable(0); !ok || !nullable {
		t.Fatalf("nullable = %v, %v", nullable, ok)
	}
	if precision, scale, ok := rows.ColumnTypePrecisionScale(0); !ok || precision != 10 || scale != 2 {
		t.Fatalf("precision/scale = %d/%d, %v", precision, scale, ok)
	}
	if got := rows.ColumnTypeScanType(0); got != reflect.TypeOf(int64(0)) {
		t.Fatalf("scan type = %v", got)
	}
	if !rows.HasNextResultSet() {
		t.Fatal("next result set was not forwarded")
	}
	if err := rows.NextResultSet(); err != nil {
		t.Fatalf("next result set: %v", err)
	}
}

func TestScopeRowsCloseInvalidatesCanceledConnection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	conn := &scopeConnection{}
	rows := &scopeRows{Rows: &metadataRows{}, ctx: ctx, conn: conn}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if conn.IsValid() {
		t.Fatal("canceled rows left the connection valid")
	}
	if err := conn.ResetSession(context.Background()); err != driver.ErrBadConn {
		t.Fatalf("ResetSession = %v, want driver.ErrBadConn", err)
	}
}

func TestScopeRowsCloseKeepsLiveConnection(t *testing.T) {
	conn := &scopeConnection{}
	rows := &scopeRows{Rows: &metadataRows{}, ctx: context.Background(), conn: conn}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if !conn.IsValid() {
		t.Fatal("live rows invalidated the connection")
	}
}
