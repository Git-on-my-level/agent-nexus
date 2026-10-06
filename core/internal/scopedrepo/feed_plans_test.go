package scopedrepo

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestFeedExactBatchQueryPlans(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	store := New(db)
	if err = store.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = store.InitializeFeedSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, query string
		args        []any
		probes      []string
	}{
		{"start", query_feed_start, []any{"scope", 1, "docs", "reader", 101}, []string{"SEARCH scope_feed USING PRIMARY KEY"}},
		{"after", query_feed_after, []any{"scope", 1, "docs", "reader", 1, 1, 101}, []string{"SEARCH scope_feed USING PRIMARY KEY"}},
		{"hydrate", `WITH requested(ord,scope_id,generation,family,audience_key,sort_key,rid,version) AS (VALUES (?,?,?,?,?,?,?,?),(?,?,?,?,?,?,?,?)` + query_feed_hydrate_suffix,
			[]any{0, "scope", 1, "docs", "reader", 1, 1, 1, 1, "scope", 1, "docs", "reader", 2, 2, 1},
			[]string{"SEARCH f USING", "SEARCH p USING PRIMARY KEY", "SEARCH k USING INTEGER PRIMARY KEY", "SEARCH r USING PRIMARY KEY"}},
		{"buckets", `WITH requested(scope_id,generation,family,audience_key,bucket) AS (VALUES (?,?,?,?,?),(?,?,?,?,?)` + query_feed_buckets_suffix,
			[]any{"scope", 1, "docs", "reader", "one", "scope", 1, "docs", "reader", "two"}, []string{"SEARCH c USING PRIMARY KEY"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, e := db.Query("EXPLAIN QUERY PLAN "+tc.query, tc.args...)
			if e != nil {
				t.Fatal(e)
			}
			defer rows.Close()
			var plan strings.Builder
			for rows.Next() {
				var id, parent, unused int
				var detail string
				if e = rows.Scan(&id, &parent, &unused, &detail); e != nil {
					t.Fatal(e)
				}
				plan.WriteString(detail)
				plan.WriteByte('\n')
			}
			if e = rows.Err(); e != nil {
				t.Fatal(e)
			}
			text := plan.String()
			for _, probe := range tc.probes {
				if !strings.Contains(text, probe) {
					t.Fatalf("missing exact probe %s:\n%s", probe, text)
				}
			}
			for _, bad := range []string{"TEMP B-TREE", "SCAN scope_feed", "SCAN f ", "SCAN p ", "SCAN k ", "SCAN r ", "SCAN c "} {
				if strings.Contains(text, bad) {
					t.Fatalf("unbounded query plan: %s", text)
				}
			}
		})
	}
}
