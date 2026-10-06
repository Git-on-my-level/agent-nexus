package readmodel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// This adapter tests the Step transaction contract against durable SQLite. It
// is not a production authority adapter or a replacement for real-route gates.
type durableFixture struct {
	tx             *sql.Tx
	failCheckpoint bool
}

func (f durableFixture) Job(ctx context.Context) (Job, error) {
	j := Job{ScopeID: "scope", Fence: 1}
	e := f.tx.QueryRowContext(ctx, `SELECT generation,cursor FROM test_job`).Scan(&j.Generation, &j.Cursor)
	return j, e
}
func (f durableFixture) Rows(ctx context.Context, limit int) ([]LifecycleRow, error) {
	j, e := f.Job(ctx)
	if e != nil {
		return nil, e
	}
	rows, e := f.tx.QueryContext(ctx, `SELECT rid,parent FROM test_resources WHERE rid>? ORDER BY rid LIMIT ?`, j.Cursor, limit)
	if e != nil {
		return nil, e
	}
	var out []LifecycleRow
	for rows.Next() {
		var rid, parent int64
		if e = rows.Scan(&rid, &parent); e != nil {
			rows.Close()
			return nil, e
		}
		row := LifecycleRow{RID: rid, ScopeID: "scope"}
		if parent > 0 {
			row.Ancestors = []Ancestor{{"scope", parent}}
		}
		out = append(out, row)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	for i := range out {
		row := &out[i]
		var present, hidden int
		if e = f.tx.QueryRowContext(ctx, `SELECT count(*) FROM test_feed WHERE generation=? AND rid=?`, j.Generation, row.RID).Scan(&present); e != nil {
			return nil, e
		}
		if e = f.tx.QueryRowContext(ctx, `SELECT hidden FROM test_resources WHERE rid=?`, row.RID).Scan(&hidden); e != nil {
			return nil, e
		}
		if len(row.Ancestors) > 0 {
			var parentHidden int
			if e = f.tx.QueryRowContext(ctx, `SELECT hidden FROM test_resources WHERE rid=?`, row.Ancestors[0].RID).Scan(&parentHidden); e != nil {
				return nil, e
			}
			hidden |= parentHidden
		}
		p := &Projection{ScopeID: "scope", Generation: j.Generation, RID: row.RID, Version: 1, Entries: []Entry{{Family: "work", Audience: "all", Sort: row.RID, Buckets: []string{"live"}}}}
		if present == 1 {
			row.Before = p
		}
		if hidden == 0 {
			row.After = p
		}
	}
	return out, nil
}
func (f durableFixture) Apply(ctx context.Context, d Delta) error {
	for _, v := range d.Feeds {
		var e error
		if v.Delete {
			_, e = f.tx.ExecContext(ctx, `DELETE FROM test_feed WHERE generation=? AND rid=?`, v.Projection.Generation, v.Projection.RID)
		} else {
			_, e = f.tx.ExecContext(ctx, `INSERT INTO test_feed VALUES(?,?)`, v.Projection.Generation, v.Projection.RID)
		}
		if e != nil {
			return e
		}
	}
	for _, v := range d.Counters {
		if _, e := f.tx.ExecContext(ctx, `UPDATE test_count SET n=n+? WHERE generation=?`, v.Delta, v.Key.Generation); e != nil {
			return e
		}
	}
	return nil
}
func (f durableFixture) Checkpoint(ctx context.Context, _ Job, cursor int64) error {
	if f.failCheckpoint {
		return errors.New("injected durable checkpoint failure")
	}
	_, e := f.tx.ExecContext(ctx, `UPDATE test_job SET cursor=?`, cursor)
	return e
}
func (f durableFixture) Activate(ctx context.Context, j Job) error {
	_, e := f.tx.ExecContext(ctx, `UPDATE test_scope SET state='active',generation=?; DELETE FROM test_job`, j.Generation)
	return e
}
func openFixture(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	db.SetMaxOpenConns(1)
	return db
}

func TestLifecycleTenThousandDurableChunksAndRetry(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "worker.db")
	db := openFixture(t, path)
	defer func() { db.Close() }()
	_, e := db.Exec(`PRAGMA journal_mode=WAL;
CREATE TABLE test_resources(rid INTEGER PRIMARY KEY,parent INTEGER NOT NULL,hidden INTEGER NOT NULL);
CREATE TABLE test_feed(generation INTEGER,rid INTEGER,PRIMARY KEY(generation,rid)) WITHOUT ROWID;
CREATE TABLE test_count(generation INTEGER PRIMARY KEY,n INTEGER CHECK(n>=0));
CREATE TABLE test_job(generation INTEGER,cursor INTEGER NOT NULL);
CREATE TABLE test_scope(state TEXT,generation INTEGER);
INSERT INTO test_resources VALUES(1,0,1),(2,0,0);
WITH RECURSIVE children(n) AS (SELECT 3 UNION ALL SELECT n+1 FROM children WHERE n<10002)
INSERT INTO test_resources SELECT n,1,0 FROM children;
INSERT INTO test_feed SELECT 1,rid FROM test_resources;
INSERT INTO test_count VALUES(1,10002),(2,0);
INSERT INTO test_job VALUES(2,0);
INSERT INTO test_scope VALUES('transitioning',1);`)
	if e != nil {
		t.Fatal(e)
	}
	run := func(fail bool) (StepResult, error) {
		tx, e := db.BeginTx(ctx, nil)
		if e != nil {
			return StepResult{}, e
		}
		defer tx.Rollback()
		r, e := Step(ctx, durableFixture{tx, fail})
		if e != nil {
			return r, e
		}
		return r, tx.Commit()
	}
	if _, e = run(true); e == nil {
		t.Fatal("checkpoint fault lost")
	}
	var count, cursor int
	if e = db.QueryRow(`SELECT n FROM test_count WHERE generation=2`).Scan(&count); e != nil {
		t.Fatal(e)
	}
	if e = db.QueryRow(`SELECT cursor FROM test_job`).Scan(&cursor); e != nil {
		t.Fatal(e)
	}
	if count != 0 || cursor != 0 {
		t.Fatalf("partial chunk committed: count=%d cursor=%d", count, cursor)
	}
	examined, slices := 0, 0
	for {
		r, e := run(false)
		if e != nil {
			t.Fatal(e)
		}
		if r.Examined > 64 {
			t.Fatal(r)
		}
		if r.Complete {
			break
		}
		examined += r.Examined
		slices++
		if slices == 30 {
			if e = db.Close(); e != nil {
				t.Fatal(e)
			}
			db = openFixture(t, path)
		}
		var state string
		if e = db.QueryRow(`SELECT state FROM test_scope`).Scan(&state); e != nil {
			t.Fatal(e)
		}
		if state != "transitioning" {
			t.Fatal("premature activation", state)
		}
		if e = db.QueryRow(`SELECT n FROM test_count WHERE generation=1`).Scan(&count); e != nil {
			t.Fatal(e)
		}
		if count != 10002 {
			t.Fatal("selected generation modified by worker", count)
		}
	}
	if examined != 10002 || slices != 157 {
		t.Fatalf("examined=%d slices=%d", examined, slices)
	}
	if e = db.QueryRow(`SELECT n FROM test_count WHERE generation=(SELECT generation FROM test_scope)`).Scan(&count); e != nil {
		t.Fatal(e)
	}
	var rid int
	if e = db.QueryRow(`SELECT rid FROM test_feed WHERE generation=(SELECT generation FROM test_scope)`).Scan(&rid); e != nil {
		t.Fatal(e)
	}
	if count != 1 || rid != 2 {
		t.Fatal("dead descendants survived", count, rid)
	}
	// Restore the same ancestor into the next staging generation.
	_, e = db.Exec(`UPDATE test_resources SET hidden=0 WHERE rid=1;UPDATE test_scope SET state='transitioning';INSERT INTO test_job VALUES(3,0);INSERT INTO test_count VALUES(3,0)`)
	if e != nil {
		t.Fatal(e)
	}
	for {
		r, e := run(false)
		if e != nil {
			t.Fatal(e)
		}
		if r.Complete {
			break
		}
	}
	if e = db.QueryRow(`SELECT n FROM test_count WHERE generation=(SELECT generation FROM test_scope)`).Scan(&count); e != nil {
		t.Fatal(e)
	}
	if count != 10002 {
		t.Fatal("restore lost rows", count)
	}
}

type badLifecycle struct {
	durableFixture
	rows   []LifecycleRow
	writes int
	plans  []Delta
}

func (f *badLifecycle) Job(context.Context) (Job, error)                  { return Job{"scope", 1, 1, 0}, nil }
func (f *badLifecycle) Rows(context.Context, int) ([]LifecycleRow, error) { return f.rows, nil }
func (f *badLifecycle) Apply(_ context.Context, d Delta) error {
	f.writes++
	f.plans = append(f.plans, d)
	return nil
}
func (f *badLifecycle) Checkpoint(context.Context, Job, int64) error { return nil }
func (f *badLifecycle) Activate(context.Context, Job) error          { return nil }
func TestLifecycleRejectsWholeInvalidChunkBeforeWrites(t *testing.T) {
	for _, bad := range []LifecycleRow{{RID: 2, ScopeID: "other"}, {RID: 2, ScopeID: "scope", Ancestors: []Ancestor{{"scope", 2}}}, {RID: 2, ScopeID: "scope", Ancestors: []Ancestor{{"other", 1}}}} {
		f := &badLifecycle{rows: []LifecycleRow{{RID: 1, ScopeID: "scope"}, bad}}
		if _, e := Step(context.Background(), f); !errors.Is(e, ErrProjection) || f.writes != 0 {
			t.Fatal(fmt.Sprintf("err=%v writes=%d", e, f.writes))
		}
	}
}

func TestLifecycleEqualVersionStagedProjection(t *testing.T) {
	p := projection(1, "all", "live")
	f := &badLifecycle{rows: []LifecycleRow{{RID: 42, ScopeID: "scope", Before: p, After: p}}}
	r, e := Step(context.Background(), f)
	if e != nil || r.Examined != 1 || f.writes != 1 || len(f.plans[0].Feeds) != 2 || len(f.plans[0].Counters) != 0 {
		t.Fatalf("result=%+v err=%v plans=%+v", r, e, f.plans)
	}
}

func TestLifecycleRejectsOversizedChunkAndDepth(t *testing.T) {
	f := &badLifecycle{rows: make([]LifecycleRow, 65)}
	if _, e := Step(context.Background(), f); !errors.Is(e, ErrBudget) || f.writes != 0 {
		t.Fatal(e, f.writes)
	}
	f.rows = []LifecycleRow{{RID: 1, ScopeID: "scope", Ancestors: make([]Ancestor, 9)}}
	if _, e := Step(context.Background(), f); !errors.Is(e, ErrProjection) || f.writes != 0 {
		t.Fatal(e, f.writes)
	}
}
