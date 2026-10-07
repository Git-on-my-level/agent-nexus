package scopedrepo

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"agent-nexus-core/internal/scopes"
	"modernc.org/sqlite"
)

func TestBatchReviewedPlansAndExaminedCandidateBound(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	s := New(db)
	if err = s.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = s.InitializeFeedSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	var visited atomic.Int64
	name := fmt.Sprintf("batch_visit_%d", batchVisitID.Add(1))
	if err = sqlite.RegisterScalarFunction(name, 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		visited.Add(1)
		return args[0], nil
	}); err != nil {
		t.Fatal(err)
	}
	// Function registration applies to new connections, so use a file-backed pool.
	db.Close()
	db, err = sql.Open("sqlite", t.TempDir()+"/plans.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	s = New(db)
	if err = s.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = s.InitializeFeedSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO scope_domains VALUES('s','active',1);
 INSERT INTO scope_memberships VALUES('p','s','reader',1);
 INSERT INTO scope_feed_generations VALUES('s',1,1,1,1,1,7);
 INSERT INTO scope_feed_bindings VALUES('p','s',1,'inbox','a',1,1),('p','s',1,'inbox','b',1,1);
 INSERT INTO scope_feed WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<10000)
 SELECT 's',1,'inbox',a.key,x,x+a.offset,1 FROM n CROSS JOIN (SELECT 'a' key,0 offset UNION ALL SELECT 'b',10000 UNION ALL SELECT 'hidden',20000) a`)
	if err != nil {
		t.Fatal(err)
	}
	streams := []feedStreamAuthority{{Stream: scopes.Stream{Scope: "s", Family: "inbox", Audience: "a"}, Generation: 1}, {Stream: scopes.Stream{Scope: "s", Family: "inbox", Audience: "b"}, Generation: 1}}
	for _, after := range [][]*FeedKey{{nil, nil}, {{Sort: 9000, RID: 9000}, {Sort: 9000, RID: 19000}}} {
		q, args := batchCandidateQuery(streams, after, 100)
		plan := batchPlan(t, db, q, args...)
		if strings.Count(plan, "SEARCH scope_feed USING PRIMARY KEY") != 2 || strings.Contains(plan, "SCAN scope_feed") || strings.Contains(plan, "AUTOMATIC") {
			t.Fatal(plan)
		}
		// Instrument only projection evaluation inside each bounded seek. This is
		// the same query/plan with a non-deterministic visit function on version.
		instrumented := strings.ReplaceAll(q, "SELECT sort_key,rid,version FROM scope_feed", "SELECT sort_key,rid,"+name+"(version) AS version FROM scope_feed")
		visited.Store(0)
		rows, e := db.Query(instrumented, args...)
		if e != nil {
			t.Fatal(e)
		}
		n := 0
		for rows.Next() {
			n++
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			t.Fatal(e)
		}
		if n != 101 || visited.Load() > 202 || visited.Load() < 101 {
			t.Fatalf("returned=%d examined=%d cap=202", n, visited.Load())
		}
	}
	cases := []struct {
		q      string
		args   []any
		probes []string
	}{
		{`WITH requested(ord,scope_id) AS (VALUES (?,?)` + query_feed_batch_authority_suffix, []any{0, "s", "p"}, []string{"SEARCH d USING PRIMARY KEY", "SEARCH m USING PRIMARY KEY", "SEARCH g USING PRIMARY KEY"}},
		{`WITH requested(ord,scope_id,generation,family,audience_key) AS (VALUES (?,?,?,?,?)` + query_feed_batch_binding_suffix, []any{0, "s", 1, "inbox", "a", "p"}, []string{"SEARCH b USING PRIMARY KEY"}},
		{query_feed_batch_proof, []any{strings.Repeat("0", 64), 1, 1}, []string{"SEARCH p USING PRIMARY KEY", "SEARCH c USING INTEGER PRIMARY KEY"}},
		{`WITH requested(scope_id,generation,family,audience_key,bucket) AS (VALUES (?,?,?,?,?)` + query_feed_batch_buckets_suffix, []any{"s", 1, "inbox", "a", "total"}, []string{"SEARCH c USING PRIMARY KEY"}},
	}
	for _, tc := range cases {
		plan := batchPlan(t, db, tc.q, tc.args...)
		for _, p := range tc.probes {
			if !strings.Contains(plan, p) {
				t.Fatalf("missing %s: %s", p, plan)
			}
		}
		if strings.Contains(plan, "AUTOMATIC") {
			t.Fatal(plan)
		}
	}
}

var batchVisitID atomic.Int64

func batchPlan(t *testing.T, db *sql.DB, q string, args ...any) string {
	t.Helper()
	rows, err := db.Query("EXPLAIN QUERY PLAN "+q, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out strings.Builder
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		out.WriteString(detail)
		out.WriteByte('\n')
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}
