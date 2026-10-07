package readmodel_test

import (
	"agent-nexus-core/internal/readmodel"
	"context"
	"database/sql"
	_ "modernc.org/sqlite"
	"testing"
)

func openFixture(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	return db
}
func codec(t *testing.T) *readmodel.CursorCodec {
	t.Helper()
	c, err := readmodel.NewCursorCodec(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// The integration assertion exercises affected-row rejection without accessing
// the kernel's private helper.
func exact(ctx context.Context, tx readmodel.Executor, q string, args ...any) error {
	n, err := tx.Exec(ctx, q, args...)
	if err != nil {
		return err
	}
	if n != 1 {
		return readmodel.ErrProjection
	}
	return nil
}
func before(a, b readmodel.Key) bool { return a.Sort < b.Sort || a.Sort == b.Sort && a.RID < b.RID }
