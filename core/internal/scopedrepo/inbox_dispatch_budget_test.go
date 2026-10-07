package scopedrepo_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/readmodel"
	"agent-nexus-core/internal/scopedrepo"
	"agent-nexus-core/internal/scopes"
	"agent-nexus-core/internal/testsql"
)

func TestInboxDispatcherMaximumRepositoryBudgetAndPlans(t *testing.T) {
	db, s, _, _ := orderedFeedFixture(t)
	request := scopes.RequestSelection{Principal: "many"}
	var streams []scopes.Stream
	for scope := 0; scope < 64; scope++ {
		id := fmt.Sprintf("ordered-scope-%02d", scope)
		request.ScopeIDs = append(request.ScopeIDs, scopes.ID(id))
		must(t, exec(db, `INSERT INTO scope_domains VALUES(?,'active',1);INSERT INTO scope_memberships VALUES('many',?,'reader',1);INSERT INTO scope_feed_generations VALUES(?,1,1,1,1,1,7)`, id, id, id))
		for audience := 0; audience < 4; audience++ {
			key := fmt.Sprint(audience)
			streams = append(streams, scopes.Stream{Scope: scopes.ID(id), Family: "inbox", Audience: key})
			must(t, exec(db, `INSERT INTO scope_feed_bindings VALUES('many',?,1,'inbox',?,1,1)`, id, key))
			canonical := fmt.Sprintf("canonical-%03d-%d", scope, audience)
			seedOrderedFeed(t, db, id, key, canonical, orderedItem(canonical, "ask", "z"))
			must(t, exec(db, `INSERT INTO scope_counters VALUES(?,1,'inbox',?,'total',1)`, id, key))
		}
	}
	// Large irrelevant binding history must be skipped by exact current family /
	// generation prefixes, rather than filtered until enough eligible rows appear.
	must(t, exec(db, `WITH RECURSIVE n(x) AS(VALUES(2) UNION ALL SELECT x+1 FROM n WHERE x<10001) INSERT INTO scope_feed_bindings SELECT 'many','ordered-scope-00',x,'other','irrelevant',1,1 FROM n`))
	must(t, s.InitializeInboxDispatcherSchema(context.Background()))
	must(t, exec(db, primitives.ScopeInboxInvalidationSchemaProposal))
	must(t, exec(db, `INSERT INTO scope_inbox_source_clock VALUES(1,1,1,1,?,1,(SELECT schema_version FROM pragma_schema_version),1)`, primitives.ScopeInboxInvalidationRegistryHash()))
	installTestOnlyProof(t, db, s, request, streams)
	var binding string
	must(t, s.ReadOrderedBatchFeed(context.Background(), request, streams, func(r scopedrepo.OrderedBatchFeedReader) error { v, e := r.Snapshot(); binding = v.Binding; return e }))
	must(t, exec(db, `INSERT INTO scope_inbox_serving_receipts SELECT ?,source_revision,authority_revision,directory_revision,registry_hash,1,1,1,1,1,1 FROM scope_inbox_source_clock`, binding))
	var seq int
	var name, path string
	must(t, db.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &path))
	counted, counter := testsql.Open(path)
	defer counted.Close()
	codec, e := readmodel.NewCursorCodec(make([]byte, 32))
	must(t, e)
	dispatcher := scopedrepo.NewInboxDispatcher(scopedrepo.New(counted), codec)
	counter.Reset()
	start := time.Now()
	out, e := dispatcher.Read(context.Background(), "many", 100, "")
	must(t, e)
	elapsed := time.Since(start)
	if out.Fallback != "" || len(out.Page.Items) != 100 || out.Counts["total"] != 256 || out.Page.NextCursor == "" {
		t.Fatal(out.Fallback, len(out.Page.Items), out.Counts)
	}
	// This is repository evidence with fixture certificates, not mounted HTTP
	// certification or proof of full-request authentication/enrichment cost.
	t.Logf("repository dispatcher: %d SQL / %d returned rows / %s", counter.Count(), counter.ReturnedRows(), elapsed)
	if counter.Count() > 100 || counter.ReturnedRows() > 1024 {
		t.Fatal(counter.Count(), counter.ReturnedRows())
	}
	var directoryPlan bool
	for _, statement := range counter.Statements() {
		if strings.Contains(statement.SQL, "WITH RECURSIVE") || strings.Contains(statement.SQL, "resource_access_edges") {
			t.Fatal("legacy graph on certified path")
		}
		if strings.Contains(statement.SQL, "LIMIT 5") {
			directoryPlan = true
			rows, e := db.Query(`EXPLAIN QUERY PLAN `+statement.SQL, statement.Args...)
			must(t, e)
			branches := 0
			for rows.Next() {
				var id, parent, unused int
				var detail string
				must(t, rows.Scan(&id, &parent, &unused, &detail))
				if strings.Contains(detail, "SEARCH b USING") {
					branches++
					for _, prefix := range []string{"principal=?", "scope_id=?", "generation=?", "family=?"} {
						if !strings.Contains(detail, prefix) {
							t.Fatal("missing exact directory prefix", detail)
						}
					}
				}
				if strings.HasPrefix(detail, "SCAN b") {
					t.Fatal("unbounded directory scan", detail)
				}
			}
			must(t, rows.Err())
			must(t, rows.Close())
			if branches != 64 {
				t.Fatal("missing indexed branches", branches)
			}
		}
	}
	if !directoryPlan {
		t.Fatal("directory query not exercised")
	}
}
