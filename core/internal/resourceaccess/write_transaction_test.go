package resourceaccess

import (
	"context"
	"database/sql"
	"errors"
	_ "modernc.org/sqlite"
	"testing"
	"time"
)

func TestWriteTransactionPreservesNestedRollbackAndScopedReads(t *testing.T) {
	raw, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	raw.SetMaxOpenConns(1)
	if _, err = raw.Exec(`CREATE TABLE records(id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	db := NewDB(raw)
	denied := errors.New("denied")
	abort := errors.New("receipt failed")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ctx = WithPolicy(ctx, Policy{Read: func(q string) string { return q }, Check: func(_ context.Context, _ QueryRower, value any) error {
		for _, v := range value.(SQLValues).Args {
			if v == "forbidden" {
				return denied
			}
		}
		return nil
	}})
	err = db.WriteTransaction(ctx, func(ctx context.Context, outer *Tx) error {
		inner, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err = inner.ExecContext(ctx, `INSERT INTO records VALUES(?)`, "discarded"); err != nil {
			return err
		}
		if err = inner.Rollback(); err != nil {
			return err
		}
		if _, err = db.ExecContext(ctx, `INSERT INTO records VALUES(?)`, "forbidden"); !errors.Is(err, denied) {
			t.Fatalf("policy skipped %v", err)
		}
		if _, err = db.ExecContext(ctx, `INSERT INTO records VALUES(?)`, "kept until outer rollback"); err != nil {
			return err
		}
		var count int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM records`).Scan(&count); err != nil || count != 1 {
			t.Fatalf("nested read count %d %v", count, err)
		}
		snapshot, err := db.BeginReadSnapshot(ctx, "SELECT id FROM records")
		if err != nil {
			return err
		}
		defer snapshot.Rollback()
		return abort
	})
	if !errors.Is(err, abort) {
		t.Fatalf("unexpected result %v", err)
	}
	var count int
	if err = raw.QueryRow(`SELECT count(*) FROM records`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial outer commit %d %v", count, err)
	}
}
