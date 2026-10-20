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
		pinned, closeRead, err := db.PinReads(ctx, func(ctx context.Context, handle QueryRower) (context.Context, error) {
			var count int
			err := handle.QueryRowContext(ctx, `SELECT count(*) FROM records`).Scan(&count)
			if err != nil || count != 1 {
				t.Fatalf("admission saw stale write snapshot: count %d error %v", count, err)
			}
			return ctx, err
		})
		if err != nil {
			return err
		}
		if err := db.QueryRowContext(pinned, `SELECT count(*) FROM records`).Scan(&count); err != nil || count != 1 {
			t.Fatalf("pinned write count %d %v", count, err)
		}
		closeRead()
		// Closing a borrowed projection must leave its parent transaction open.
		if _, err := outer.ExecContext(ctx, `INSERT INTO records VALUES(?)`, "after projection close"); err != nil {
			return err
		}
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

func TestWriteTransactionBindsUnscopedExecAcrossWrappers(t *testing.T) {
	raw, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	raw.SetMaxOpenConns(1)
	if _, err := raw.Exec(`CREATE TABLE records(id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	abort := errors.New("receipt failed")
	err = NewDB(raw).WriteTransaction(ctx, func(ctx context.Context, _ *Tx) error {
		// PM Store.database creates a wrapper per call. It must borrow the
		// writer even when no access policy or write hook is installed.
		if _, err := NewDB(raw).ExecContext(ctx, `INSERT INTO records VALUES('pending')`); err != nil {
			return err
		}
		var count int
		if err := NewDB(raw).QueryRowContext(ctx, `SELECT count(*) FROM records`).Scan(&count); err != nil || count != 1 {
			t.Fatalf("write invisible inside transaction: %d %v", count, err)
		}
		return abort
	})
	if !errors.Is(err, abort) {
		t.Fatal(err)
	}
	var count int
	if err := raw.QueryRow(`SELECT count(*) FROM records`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("unscoped write escaped rollback: %d %v", count, err)
	}
}

func TestAfterWriteTransactionRunsWhenOutermostFinishes(t *testing.T) {
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
	ctx := context.Background()
	if AfterWriteTransaction(ctx, func(context.Context, bool) { t.Fatal("hook ran outside a transaction") }) {
		t.Fatal("hook registered outside a write transaction")
	}
	for _, tc := range []struct {
		name      string
		fail      error
		committed bool
	}{{"commit", nil, true}, {"rollback", errors.New("outer failed"), false}} {
		var calls []bool
		err := db.WriteTransaction(ctx, func(ctx context.Context, _ *Tx) error {
			// A nested WriteTransaction and a nested savepoint both defer to
			// the outermost transaction.
			if err := db.WriteTransaction(ctx, func(ctx context.Context, _ *Tx) error {
				inner, err := db.BeginTx(ctx, nil)
				if err != nil {
					return err
				}
				if _, err = inner.ExecContext(ctx, `INSERT INTO records VALUES(?)`, tc.name); err != nil {
					return err
				}
				if err = inner.Commit(); err != nil {
					return err
				}
				AfterWriteTransaction(ctx, func(hookCtx context.Context, committed bool) {
					if _, bound := hookCtx.Value(writeTransactionKey{}).(writeTransaction); bound {
						t.Error("hook context is still bound to the finished transaction")
					}
					calls = append(calls, committed)
				})
				return nil
			}); err != nil {
				return err
			}
			if len(calls) != 0 {
				t.Errorf("%s: hook ran before the outermost transaction finished", tc.name)
			}
			return tc.fail
		})
		if !errors.Is(err, tc.fail) {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if len(calls) != 1 || calls[0] != tc.committed {
			t.Fatalf("%s: hook calls = %v, want [%v]", tc.name, calls, tc.committed)
		}
	}
}
