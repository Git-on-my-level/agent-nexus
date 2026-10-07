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

func TestOrderedBatchIndexedPlansAndExaminedBound(t *testing.T) {
	var visited atomic.Int64
	name := fmt.Sprintf("ordered_visit_%d", batchVisitID.Add(1))
	if err := sqlite.RegisterScalarFunction(name, 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		visited.Add(1)
		return args[0], nil
	}); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", t.TempDir()+"/ordered.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	s := New(db)
	for _, init := range []func(context.Context) error{s.Initialize, s.InitializeFeedSchema, s.InitializeInboxOrderSchema} {
		if err = init(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	_, err = db.Exec(`INSERT INTO scope_domains VALUES('s','active',1);
 INSERT INTO scope_inbox_order WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<10000)
 SELECT 's',1,'inbox',a.key,CAST(printf('%05d',x) AS BLOB),x+a.offset,1
 FROM n CROSS JOIN (SELECT 'a' key,0 offset UNION ALL SELECT 'b',10000 UNION ALL SELECT 'hidden',20000) a`)
	if err != nil {
		t.Fatal(err)
	}
	streams := []feedStreamAuthority{{Stream: scopes.Stream{Scope: "s", Family: "inbox", Audience: "a"}, Generation: 1}, {Stream: scopes.Stream{Scope: "s", Family: "inbox", Audience: "b"}, Generation: 1}}
	for _, after := range []*OrderedFeedKey{nil, {Order: []byte("09000"), RID: 9000}} {
		q, args := orderedBatchCandidateQuery(streams, after, 100)
		plan := batchPlan(t, db, q, args...)
		if strings.Count(plan, "SEARCH scope_inbox_order USING PRIMARY KEY") != 2 || strings.Contains(plan, "SCAN scope_inbox_order") || strings.Contains(plan, "AUTOMATIC") {
			t.Fatal(plan)
		}
		if after != nil && strings.Count(plan, "order_key,rid") != 2 {
			t.Fatal("full comparator missing from indexed seek", plan)
		}
		instrumented := strings.ReplaceAll(q, "SELECT order_key,rid,version FROM scope_inbox_order", "SELECT order_key,rid,"+name+"(version) AS version FROM scope_inbox_order")
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
	q := `WITH requested(ord,scope_id,generation,family,audience_key,order_key,rid,version) AS (VALUES (?,?,?,?,?,?,?,?),(?,?,?,?,?,?,?,?)` + orderedFeedHydrateSuffix
	plan := batchPlan(t, db, q, 0, "s", 1, "inbox", "a", []byte("00001"), 1, 1, 1, "s", 1, "inbox", "b", []byte("00001"), 10001, 1)
	for _, probe := range []string{"SEARCH f USING", "SEARCH p USING PRIMARY KEY", "SEARCH k USING INTEGER PRIMARY KEY", "SEARCH r USING PRIMARY KEY"} {
		if !strings.Contains(plan, probe) {
			t.Fatalf("missing %s: %s", probe, plan)
		}
	}
	for _, bad := range []string{"SCAN f ", "SCAN p ", "SCAN k ", "SCAN r ", "AUTOMATIC"} {
		if strings.Contains(plan, bad) {
			t.Fatal(plan)
		}
	}
}
