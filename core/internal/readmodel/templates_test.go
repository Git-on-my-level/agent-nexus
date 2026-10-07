package readmodel

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"testing"
)

// sqlFixture executes the proposed exact-stream templates. It intentionally
// supplies a test-only authorized snapshot; production HTTP parity/budget proof
// remains a separate gate after A integrates the scoped repository adapter.
type sqlFixture struct {
	fixtureReader
	db *sql.DB
}

func (f *sqlFixture) Candidates(ctx context.Context, stream int, after *Key, limit int) ([]Candidate, error) {
	s := f.s.Streams[stream]
	args := []any{s.Scope, 1, s.Family, s.Audience}
	q := SeekFeedStart
	if after != nil {
		q = SeekFeedAfter
		args = append(args, after.Sort, after.RID)
	}
	args = append(args, limit)
	rows, e := f.db.QueryContext(ctx, q, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []Candidate
	for rows.Next() {
		var c Candidate
		if e = rows.Scan(&c.Key.Sort, &c.Key.RID, &c.Version); e != nil {
			return nil, e
		}
		out = append(out, c)
		f.visits++
	}
	return out, rows.Err()
}

func TestFeedPlansAndWrongAudienceDominatedRanges(t *testing.T) {
	db := openFixture(t, ":memory:")
	defer db.Close()
	if _, e := db.Exec(SchemaProposal); e != nil {
		t.Fatal(e)
	}
	// 64 scopes, each with 1,000 wrong-audience rows before the selected range.
	_, e := db.Exec(`WITH RECURSIVE scopes(s) AS (SELECT 1 UNION ALL SELECT s+1 FROM scopes WHERE s<64),
 rows(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM rows WHERE n<1000)
 INSERT INTO scope_feed SELECT 'scope-'||(s-1),1,'inbox','person:other',n,s*10000+n,1 FROM scopes,rows`)
	if e != nil {
		t.Fatal(e)
	}
	base := fixture(64)
	f := &sqlFixture{fixtureReader: *base, db: db}
	for i := range f.s.Streams {
		f.s.Streams[i].Family = "inbox"
		f.s.Streams[i].Audience = "person:reader"
		if _, e = db.Exec(InsertFeed, f.s.Streams[i].Scope, 1, "inbox", "person:reader", 1, int64(i*10000+2000), 1); e != nil {
			t.Fatal(e)
		}
	}
	p, e := Read(context.Background(), f, codec(t), 1, "")
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Items) != 1 || f.visits != 64 || f.hydrated != 1 {
		t.Fatalf("items=%d visits=%d hydrated=%d", len(p.Items), f.visits, f.hydrated)
	}
	for _, probe := range []struct {
		q    string
		args []any
	}{
		{SeekFeedStart, []any{"scope-0", 1, "inbox", "person:reader", 2}},
		{SeekFeedAfter, []any{"scope-0", 1, "inbox", "person:reader", 0, 0, 2}},
		{ReadCounterBucket, []any{"scope-0", 1, "inbox", "person:reader", "open"}},
	} {
		rows, e := db.Query("EXPLAIN QUERY PLAN "+probe.q, probe.args...)
		if e != nil {
			t.Fatal(e)
		}
		var details []string
		for rows.Next() {
			var a, b, c int
			var d string
			if e = rows.Scan(&a, &b, &c, &d); e != nil {
				t.Fatal(e)
			}
			details = append(details, d)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			t.Fatal(e)
		}
		plan := strings.Join(details, "\n")
		if !strings.Contains(plan, "SEARCH") || strings.Contains(plan, "SCAN") || strings.Contains(plan, "TEMP B-TREE") {
			t.Fatalf("unbounded plan for %s:\n%s", probe.q, plan)
		}
	}
}

func TestCounterAndSourceMutationRollbackTogether(t *testing.T) {
	db := openFixture(t, ":memory:")
	defer db.Close()
	if _, e := db.Exec(SchemaProposal + `CREATE TABLE test_source(id INTEGER PRIMARY KEY,state TEXT); INSERT INTO test_source VALUES(42,'open');`); e != nil {
		t.Fatal(e)
	}
	if _, e := db.Exec(IncrementCounter, 1, 1, "inbox", "person:a", "open", 1); e != nil {
		t.Fatal(e)
	}
	if _, e := db.Exec(InsertFeed, 1, 1, "inbox", "person:a", 1, 42, 1); e != nil {
		t.Fatal(e)
	}
	apply := func(fail bool) error {
		tx, e := db.Begin()
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if _, e = tx.Exec(`UPDATE test_source SET state='answered' WHERE id=42`); e != nil {
			return e
		}
		if _, e = tx.Exec(DecrementCounter, -1, 1, 1, "inbox", "person:a", "open"); e != nil {
			return e
		}
		if _, e = tx.Exec(IncrementCounter, 1, 1, "inbox", "person:a", "answered", 1); e != nil {
			return e
		}
		if fail {
			return fmt.Errorf("injected source transaction failure")
		}
		return tx.Commit()
	}
	if e := apply(true); e == nil {
		t.Fatal("fault missing")
	}
	var state string
	var open int
	if e := db.QueryRow(`SELECT state FROM test_source WHERE id=42`).Scan(&state); e != nil {
		t.Fatal(e)
	}
	if e := db.QueryRow(ReadCounterBucket, 1, 1, "inbox", "person:a", "open").Scan(&open); e != nil {
		t.Fatal(e)
	}
	if state != "open" || open != 1 {
		t.Fatal("source/delta escaped rollback", state, open)
	}
	if e := apply(false); e != nil {
		t.Fatal(e)
	}
	if _, e := db.Exec(DecrementCounter, -1, 1, 1, "inbox", "person:a", "open"); e == nil {
		t.Fatal("counter underflow accepted")
	}
	if _, e := db.Exec(IncrementCounter, 1, 1, "inbox", "person:a", "overflow", int64(math.MaxInt64)); e != nil {
		t.Fatal(e)
	}
	if _, e := db.Exec(IncrementCounter, 1, 1, "inbox", "person:a", "overflow", 1); e == nil {
		t.Fatal("counter integer overflow accepted")
	}
}
