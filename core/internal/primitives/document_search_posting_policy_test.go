package primitives

import (
	"context"
	"math"
	"strings"
	"testing"

	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/scopesearch"
)

func TestDocumentSearchParentANDCanonicalPostingPlan(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	doc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "plan"}, "anchor bravo charlie delta echo foxtrot golf hotel", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	var n int64
	if err = ws.DB().QueryRow(`SELECT n FROM document_search_heads WHERE document_id=?`, doc["id"]).Scan(&n); err != nil {
		t.Fatal(err)
	}
	measured := s
	key := scopesearch.Candidate{Parent: doc["id"].(string), Key: scopesearch.Key{Recency: math.MaxInt64 - n}}
	for _, mode := range []string{"canonical", "pinned-denial", "point"} {
		request := WithAccessScope(ctx, AccessScope{ActorID: "reader"})
		closeRead := func() {}
		if mode != "canonical" {
			request, closeRead, err = measured.BeginResourceRead(WithRequestAccessScope(ctx, AccessScope{ActorID: "reader"}))
			if err != nil {
				t.Fatal(err)
			}
			if mode == "pinned-denial" {
				request = context.WithValue(request, summaryPointReadKey{}, false)
			}
		}
		type statement struct {
			SQL  string
			Args []any
		}
		var statements []statement
		policy, _ := resourceaccess.PolicyFrom(request)
		original := policy
		policy.ReadOnDB = func(c context.Context, db resourceaccess.QueryRower, q string, args []any) (string, []any) {
			rewritten, bound := q, args
			if original.ReadOnDB != nil {
				rewritten, bound = original.ReadOnDB(c, db, q, args)
			} else {
				rewritten = original.Read(q)
			}
			statements = append(statements, statement{rewritten, bound})
			return rewritten, bound
		}
		request = resourceaccess.WithPolicy(request, policy)
		for _, size := range []int{2, 8} {
			terms := strings.Fields("anchor bravo charlie delta echo foxtrot golf hotel")[:size]
			statements = nil
			matches, err := (documentSearchRepository{s: measured, ctx: request}).ParentMatches(ctx, []scopesearch.Candidate{key}, terms)
			if err != nil || len(matches) != 1 || !matches[0] {
				closeRead()
				t.Fatal(matches, err)
			}
			if len(statements) != 1 {
				closeRead()
				t.Fatal("unexpected parent match statements", len(statements))
			}
			stmt := statements[0]
			if !strings.Contains(stmt.SQL, "document_search_postings AS NOT MATERIALIZED (") {
				t.Error(mode, "posting shadow is not explicitly inlined")
			}
			rows, err := ws.DB().QueryContext(ctx, "EXPLAIN QUERY PLAN "+stmt.SQL, stmt.Args...)
			if err != nil {
				closeRead()
				t.Fatal(err)
			}
			var plan []string
			for rows.Next() {
				var id, parent, unused int
				var detail string
				if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
					t.Fatal(err)
				}
				plan = append(plan, detail)
			}
			if err = rows.Err(); err != nil {
				t.Fatal(err)
			}
			rows.Close()
			t.Logf("authority=%s terms=%d plan:\n%s", mode, size, strings.Join(plan, "\n"))
			seeks := 0
			for _, line := range plan {
				if strings.Contains(line, "MATERIALIZE document_search_postings") || strings.Contains(line, "SCAN document_search_postings") || strings.HasPrefix(line, "SCAN p") || strings.HasPrefix(line, "SCAN _row") {
					t.Errorf("%s terms=%d: posting scan/materialization: %s", mode, size, line)
				}
				if strings.HasPrefix(line, "SEARCH _row USING") && strings.Contains(line, "term=? AND doc_n=?") {
					seeks++
				}
			}
			if seeks != size {
				t.Errorf("%s terms=%d: got %d term/parent index seeks", mode, size, seeks)
			}
		}
		closeRead()
	}
}
