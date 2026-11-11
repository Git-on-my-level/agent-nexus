package storage

import (
	"context"
	"database/sql/driver"
	"errors"
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

type singleRowRows struct {
	driver.Rows
	done bool
}

func (r *singleRowRows) Columns() []string { return []string{"c"} }
func (r *singleRowRows) Close() error      { return nil }
func (r *singleRowRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = int64(1)
	return nil
}

type failingCloseRows struct {
	driver.Rows
	err error
}

func (r *failingCloseRows) Columns() []string              { return []string{"c"} }
func (r *failingCloseRows) Close() error                   { return r.err }
func (r *failingCloseRows) Next(dest []driver.Value) error { return io.EOF }

func TestScopeRowsCloseErrorInvalidatesConnection(t *testing.T) {
	conn := &scopeConnection{}
	rows := &scopeRows{
		Rows: &failingCloseRows{err: errors.New("close failed")},
		ctx:  context.Background(),
		conn: conn,
	}
	if err := rows.Close(); err == nil {
		t.Fatal("expected the close error to be returned")
	}
	if conn.IsValid() {
		t.Fatal("failed rows close left the connection valid")
	}
}

func TestScopeRowsNextResultSetResetsEOF(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	conn := &scopeConnection{}
	rows := &scopeRows{Rows: &metadataRows{}, ctx: ctx, conn: conn}
	dest := make([]driver.Value, 1)
	if err := rows.Next(dest); err != io.EOF {
		t.Fatalf("Next = %v, want io.EOF", err)
	}
	if !rows.eof {
		t.Fatal("EOF was not recorded")
	}
	if err := rows.NextResultSet(); err != nil {
		t.Fatalf("NextResultSet = %v", err)
	}
	if rows.eof {
		t.Fatal("NextResultSet did not reset EOF")
	}
	cancel()
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if conn.IsValid() {
		t.Fatal("early close of the next result set left the connection valid")
	}
}

func TestScopeRowsCloseAfterEOFKeepsCanceledConnection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	conn := &scopeConnection{}
	rows := &scopeRows{Rows: &singleRowRows{}, ctx: ctx, conn: conn}
	dest := make([]driver.Value, 1)
	if err := rows.Next(dest); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := rows.Next(dest); err != io.EOF {
		t.Fatalf("second Next = %v, want io.EOF", err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if !conn.IsValid() {
		t.Fatal("fully consumed rows invalidated the connection")
	}
}

type preparedConn struct {
	driver.Conn
	stmt driver.Stmt
}

func (c *preparedConn) PrepareContext(context.Context, string) (driver.Stmt, error) {
	return c.stmt, nil
}

type preparedStmt struct {
	driver.Stmt
	rows driver.Rows
}

func (s *preparedStmt) QueryContext(context.Context, []driver.NamedValue) (driver.Rows, error) {
	return s.rows, nil
}

func TestScopeStmtQueryContextInvalidatesCanceledEarlyClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	conn := &scopeConnection{Conn: &preparedConn{stmt: &preparedStmt{rows: &singleRowRows{}}}}
	stmt, err := conn.PrepareContext(context.Background(), "SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := stmt.(driver.StmtQueryContext).QueryContext(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if conn.IsValid() {
		t.Fatal("canceled prepared-statement read left the connection valid")
	}
}

// legacyStmt implements only the pre-context driver.Stmt methods, so the
// scopeStmt wrapper must fall back to Query/Exec like database/sql does.
type legacyStmt struct {
	driver.Stmt
	queryArgs []driver.Value
	execArgs  []driver.Value
}

func (s *legacyStmt) Close() error  { return nil }
func (s *legacyStmt) NumInput() int { return -1 }
func (s *legacyStmt) Exec(args []driver.Value) (driver.Result, error) {
	s.execArgs = append([]driver.Value(nil), args...)
	return driver.RowsAffected(0), nil
}
func (s *legacyStmt) Query(args []driver.Value) (driver.Rows, error) {
	s.queryArgs = append([]driver.Value(nil), args...)
	return &metadataRows{}, nil
}

func TestScopeStmtLegacyFallback(t *testing.T) {
	t.Run("query named args rejected", func(t *testing.T) {
		stmt := &scopeStmt{Stmt: &legacyStmt{}, conn: &scopeConnection{}}
		if _, err := stmt.QueryContext(context.Background(), []driver.NamedValue{{Name: "id", Value: int64(1)}}); err == nil {
			t.Fatal("expected named arguments to be rejected")
		}
	})

	t.Run("query canceled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		conn := &scopeConnection{}
		stmt := &scopeStmt{Stmt: &legacyStmt{}, conn: conn}
		if _, err := stmt.QueryContext(ctx, nil); !errors.Is(err, context.Canceled) {
			t.Fatalf("QueryContext = %v, want context.Canceled", err)
		}
		if conn.IsValid() {
			t.Fatal("canceled legacy query left the connection valid")
		}
	})

	t.Run("query positional args pass through", func(t *testing.T) {
		legacy := &legacyStmt{}
		stmt := &scopeStmt{Stmt: legacy, conn: &scopeConnection{}}
		args := []driver.NamedValue{{Ordinal: 1, Value: "a"}, {Ordinal: 2, Value: int64(2)}}
		if _, err := stmt.QueryContext(context.Background(), args); err != nil {
			t.Fatal(err)
		}
		want := []driver.Value{"a", int64(2)}
		if !reflect.DeepEqual(legacy.queryArgs, want) {
			t.Fatalf("legacy Query args = %v, want %v", legacy.queryArgs, want)
		}
	})

	t.Run("exec named args rejected", func(t *testing.T) {
		stmt := &scopeStmt{Stmt: &legacyStmt{}, conn: &scopeConnection{}}
		if _, err := stmt.ExecContext(context.Background(), []driver.NamedValue{{Name: "id", Value: int64(1)}}); err == nil {
			t.Fatal("expected named arguments to be rejected")
		}
	})

	t.Run("exec canceled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		stmt := &scopeStmt{Stmt: &legacyStmt{}, conn: &scopeConnection{}}
		if _, err := stmt.ExecContext(ctx, nil); !errors.Is(err, context.Canceled) {
			t.Fatalf("ExecContext = %v, want context.Canceled", err)
		}
	})

	t.Run("exec positional args pass through", func(t *testing.T) {
		legacy := &legacyStmt{}
		stmt := &scopeStmt{Stmt: legacy, conn: &scopeConnection{}}
		args := []driver.NamedValue{{Ordinal: 1, Value: "a"}, {Ordinal: 2, Value: int64(2)}}
		if _, err := stmt.ExecContext(context.Background(), args); err != nil {
			t.Fatal(err)
		}
		want := []driver.Value{"a", int64(2)}
		if !reflect.DeepEqual(legacy.execArgs, want) {
			t.Fatalf("legacy Exec args = %v, want %v", legacy.execArgs, want)
		}
	})
}
