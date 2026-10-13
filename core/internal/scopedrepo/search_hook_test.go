package scopedrepo_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/scopedrepo"
	"agent-nexus-core/internal/scopes"
	"agent-nexus-core/internal/scopesearch"
	"agent-nexus-core/internal/testutil/perfguard"
)

func searchMutation(t *testing.T, db *sql.DB, kind, id, parent, text string) scopes.CanonicalMutation {
	t.Helper()
	must(t, exec(db, `INSERT INTO scope_resources VALUES('public',?,?,?,1)`, kind, id, "source-"+id))
	i := scopes.ResourceIdentity{ScopeID: "public", Kind: kind, ResourceID: id, CanonicalID: "source-" + id, CanonicalVersion: 1}
	must(t, db.QueryRow(`SELECT rid FROM scope_resource_rids WHERE scope_id='public' AND kind=? AND resource_id=?`, kind, id).Scan(&i.RID))
	return scopes.CanonicalMutation{Identity: i, Changes: []scopes.Change{{ScopeID: i.ScopeID, Kind: kind, ResourceID: id, CanonicalVersion: 1, Family: "events", Audience: "all", After: &scopes.Projection{Text: text, ContainerID: parent, Timestamp: 10}}}}
}

func applySearch(t *testing.T, db *sql.DB, m scopes.CanonicalMutation) {
	t.Helper()
	ctx := context.Background()
	tx, err := resourceaccess.NewDB(db).BeginTx(ctx, nil)
	must(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `UPDATE scope_resources SET version=? WHERE scope_id=? AND kind=? AND id=?`, m.Identity.CanonicalVersion, m.Identity.ScopeID, m.Identity.Kind, m.Identity.ResourceID)
	must(t, err)
	must(t, scopedrepo.ApplyRegisteredCanonicalHooks(ctx, tx, m, scopedrepo.SearchCanonicalHook{}))
	must(t, tx.Commit())
}

func TestSearchCanonicalHookIndependentLifecycle(t *testing.T) {
	db, s := fixture(t)
	must(t, s.InitializeSearchSchema(context.Background()))
	doc := searchMutation(t, db, "document", "doc", "", "body untouched")
	applySearch(t, db, doc)
	comment := searchMutation(t, db, "comment", "comment", "doc", "first secret")
	comment.Changes[0].After.SourceTruncated = true
	applySearch(t, db, comment)
	var text string
	var truncated bool
	must(t, db.QueryRow(`SELECT text,truncated FROM scope_search_resources WHERE kind='comment'`).Scan(&text, &truncated))
	if text != "first secret" || !truncated {
		t.Fatal(text, truncated)
	}
	for version := int64(2); version <= 4; version++ {
		comment.PreviousVersion = version - 1
		comment.Identity.CanonicalVersion = version
		c := &comment.Changes[0]
		c.CanonicalVersion = version
		c.Before = c.After
		c.After = &scopes.Projection{Text: "edited token", ContainerID: "doc", Timestamp: 20}
		if version == 3 {
			c.After.Deleted = true
		}
		applySearch(t, db, comment)
		var count int
		must(t, db.QueryRow(`SELECT count(*) FROM scope_search_postings WHERE kind='comment'`).Scan(&count))
		want := 2
		if version == 3 {
			want = 0
		}
		if count != want {
			t.Fatal(version, count)
		}
		must(t, db.QueryRow(`SELECT text FROM scope_search_resources WHERE kind='document'`).Scan(&text))
		if text != "body untouched" {
			t.Fatal("comment rewrote document", text)
		}
	}
	var generation int
	must(t, db.QueryRow(`SELECT generation FROM scope_search_generations WHERE scope_id='public'`).Scan(&generation))
	if generation != 5 {
		t.Fatal(generation)
	}
	var old int
	must(t, db.QueryRow(`SELECT count(*) FROM scope_search_postings WHERE term IN ('first','secret')`).Scan(&old))
	if old != 0 {
		t.Fatal("stale postings survived edit", old)
	}
}

func TestSearchCanonicalHookFailureRollsBackSource(t *testing.T) {
	for _, mode := range []string{"identity", "version", "rid", "audience", "duplicate-delta", "missing-parent", "cross-scope-parent", "parent-move", "inactive", "posting-failure", "generation-failure"} {
		t.Run(mode, func(t *testing.T) {
			db, s := fixture(t)
			ctx := context.Background()
			must(t, s.InitializeSearchSchema(ctx))
			searchMutation(t, db, "document", "doc", "", "")
			m := searchMutation(t, db, "comment", "comment", "doc", "secret")
			switch mode {
			case "identity":
				m.Identity.CanonicalID = "wrong"
			case "version":
				m.Identity.CanonicalVersion = 2
				m.PreviousVersion = 1
				m.Changes[0].CanonicalVersion = 2
			case "rid":
				m.Identity.RID++
			case "audience":
				m.Changes[0].Audience = "person:owner"
			case "duplicate-delta":
				c := m.Changes[0]
				c.Family = "documents"
				m.Changes = append(m.Changes, c)
			case "missing-parent":
				m.Changes[0].After.ContainerID = "absent"
			case "cross-scope-parent":
				must(t, exec(db, `INSERT INTO scope_resources VALUES('private','document','other','source-other',1)`))
				m.Changes[0].After.ContainerID = "other"
			case "parent-move":
				searchMutation(t, db, "document", "other", "", "")
				must(t, exec(db, `INSERT INTO scope_search_bindings VALUES('public','comment','comment','other')`))
			case "inactive":
				must(t, exec(db, `UPDATE scope_domains SET state='transitioning' WHERE id='public'`))
			case "posting-failure":
				must(t, exec(db, `CREATE TRIGGER reject_search BEFORE INSERT ON scope_search_postings BEGIN SELECT RAISE(ABORT,'reject'); END`))
			case "generation-failure":
				must(t, exec(db, `CREATE TRIGGER reject_search BEFORE INSERT ON scope_search_generations BEGIN SELECT RAISE(ABORT,'reject'); END`))
			}
			tx, err := resourceaccess.NewDB(db).BeginTx(ctx, nil)
			must(t, err)
			_, err = tx.ExecContext(ctx, `INSERT INTO documents VALUES('canonical','uncommitted')`)
			must(t, err)
			if err = scopedrepo.ApplyRegisteredCanonicalHooks(ctx, tx, m, scopedrepo.SearchCanonicalHook{}); err == nil {
				t.Fatal("invalid mutation accepted")
			}
			if !errors.Is(tx.Commit(), sql.ErrTxDone) {
				t.Fatal("source remained committable")
			}
			for _, table := range []string{"documents", "scope_search_resources", "scope_search_postings", "scope_search_generations"} {
				var n int
				must(t, db.QueryRow(`SELECT count(*) FROM `+table).Scan(&n))
				if n != 0 {
					t.Fatal(table, n)
				}
			}
		})
	}
}

func TestSearchSchemaEnforcesPostingCapWithoutForeignKeys(t *testing.T) {
	db, s := fixture(t)
	must(t, exec(db, `PRAGMA foreign_keys=OFF`))
	must(t, s.InitializeSearchSchema(context.Background()))
	words := make([]string, scopesearch.MaxTerms)
	for i := range words {
		words[i] = fmt.Sprintf("term%d", i)
	}
	m := searchMutation(t, db, "document", "doc", "", strings.Join(words, " "))
	applySearch(t, db, m)
	var n int
	must(t, db.QueryRow(`SELECT count(*) FROM scope_search_postings`).Scan(&n))
	if n != scopesearch.MaxTerms {
		t.Fatal(n)
	}
	for _, q := range []string{
		`INSERT INTO scope_search_postings VALUES('public','overflow',-10,'doc','document',4096)`,
		`INSERT INTO scope_search_postings VALUES('public','duplicate',-10,'doc','document',0)`,
		`INSERT INTO scope_search_postings VALUES('public','orphan',-10,'missing','document',0)`,
		`INSERT INTO scope_search_postings VALUES('public','recency',-11,'doc','document',0)`,
		`UPDATE scope_search_postings SET slot=4096 WHERE slot=0`,
		`DELETE FROM scope_search_resources`,
		`UPDATE scope_search_bindings SET parent='elsewhere'`,
		`DELETE FROM scope_search_bindings`,
		`DELETE FROM scope_search_generations`,
		`UPDATE scope_search_generations SET generation=0`,
		`INSERT OR REPLACE INTO scope_search_generations VALUES('public',1)`,
	} {
		if err := exec(db, q); err == nil {
			t.Fatal("schema accepted", q)
		}
	}
	// Replacing a maximal old head deletes exactly its capped postings.
	m.PreviousVersion = 1
	m.Identity.CanonicalVersion = 2
	m.Changes[0].CanonicalVersion = 2
	m.Changes[0].Before = m.Changes[0].After
	m.Changes[0].After = &scopes.Projection{Text: "replacement", Timestamp: 11}
	applySearch(t, db, m)
	must(t, db.QueryRow(`SELECT count(*) FROM scope_search_postings`).Scan(&n))
	if n != 1 {
		t.Fatal(n)
	}
}

func TestSearchCanonicalHookUpdateFailurePreservesPriorIndex(t *testing.T) {
	db, s := fixture(t)
	ctx := context.Background()
	must(t, s.InitializeSearchSchema(ctx))
	m := searchMutation(t, db, "document", "doc", "", "original token")
	applySearch(t, db, m)
	must(t, exec(db, `CREATE TRIGGER reject_new_postings BEFORE INSERT ON scope_search_postings BEGIN SELECT RAISE(ABORT,'reject'); END`))
	m.PreviousVersion = 1
	m.Identity.CanonicalVersion = 2
	m.Changes[0].CanonicalVersion = 2
	m.Changes[0].Before = m.Changes[0].After
	m.Changes[0].After = &scopes.Projection{Text: "replacement"}
	tx, err := resourceaccess.NewDB(db).BeginTx(ctx, nil)
	must(t, err)
	_, err = tx.ExecContext(ctx, `UPDATE scope_resources SET version=2 WHERE id='doc'`)
	must(t, err)
	if scopedrepo.ApplyRegisteredCanonicalHooks(ctx, tx, m, scopedrepo.SearchCanonicalHook{}) == nil {
		t.Fatal("injected failure succeeded")
	}
	if !errors.Is(tx.Commit(), sql.ErrTxDone) {
		t.Fatal("failed update committable")
	}
	var version, generation, count int
	var text string
	must(t, db.QueryRow(`SELECT version FROM scope_resources WHERE id='doc'`).Scan(&version))
	must(t, db.QueryRow(`SELECT text FROM scope_search_resources`).Scan(&text))
	must(t, db.QueryRow(`SELECT generation FROM scope_search_generations`).Scan(&generation))
	must(t, db.QueryRow(`SELECT count(*) FROM scope_search_postings`).Scan(&count))
	if version != 1 || generation != 1 || text != "original token" || count != 2 {
		t.Fatal(version, generation, text, count)
	}
}

func TestPerformanceSearchWriterHistoryIndependence(t *testing.T) {
	db, s := fixture(t)
	ctx := context.Background()
	must(t, s.InitializeSearchSchema(ctx))
	doc := searchMutation(t, db, "document", "doc", "", "unchanged body")
	applySearch(t, db, doc)
	// The adapter must never hydrate this source while changing a comment.
	must(t, exec(db, `INSERT INTO documents VALUES('source-doc',?)`, strings.Repeat("large body ", 100000)))
	target := searchMutation(t, db, "comment", "target", "doc", "old comment")
	applySearch(t, db, target)
	var seq int
	var name, path string
	must(t, db.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &path))
	measured, capture, err := perfguard.Open(path)
	must(t, err)
	defer measured.Close()
	measured.SetMaxOpenConns(1)
	previous := 0
	var firstWork uint64
	for _, peers := range []int{100, 1000} {
		for i := previous; i < peers; i++ {
			r := searchMutation(t, db, "comment", fmt.Sprintf("peer-%d", i), "doc", "peer history")
			applySearch(t, db, r)
		}
		previous = peers
		target.PreviousVersion = target.Identity.CanonicalVersion
		target.Identity.CanonicalVersion++
		target.Changes[0].CanonicalVersion = target.Identity.CanonicalVersion
		target.Changes[0].Before = target.Changes[0].After
		target.Changes[0].After = &scopes.Projection{Text: "new comment", ContainerID: "doc", Timestamp: 11}
		tx, err := resourceaccess.NewDB(measured).BeginTx(ctx, nil)
		must(t, err)
		_, err = tx.ExecContext(ctx, `UPDATE scope_resources SET version=? WHERE scope_id='public' AND kind='comment' AND id='target'`, target.Identity.CanonicalVersion)
		must(t, err)
		capture.Start()
		err = scopedrepo.ApplyRegisteredCanonicalHooks(ctx, tx, target, scopedrepo.SearchCanonicalHook{})
		statements, queries, rows := capture.Stop()
		must(t, err)
		must(t, capture.WorkError())
		must(t, tx.Commit())
		work := capture.Work()
		for _, statement := range statements {
			plan, err := db.Query("EXPLAIN QUERY PLAN "+statement.SQL, statement.Args...)
			must(t, err)
			for plan.Next() {
				var id, parent, unused int
				var detail string
				must(t, plan.Scan(&id, &parent, &unused, &detail))
				t.Log(statement.SQL, detail)
			}
			must(t, plan.Close())
		}
		t.Logf("peers=%d source_bytes=%d hook_sql=%d rows=%d vm_steps=%d full_scan_steps=%d", peers, 1100000, queries, rows, work.VMSteps, work.FullScanSteps)
		if queries != 7 || rows != 1 || work.FullScanSteps != 0 || work.Sorts != 0 || work.AutoIndexRows != 0 || work.VMSteps > 5000 {
			t.Fatal("unbounded hook work", queries, rows, work)
		}
		if firstWork == 0 {
			firstWork = work.VMSteps
		} else if work.VMSteps > firstWork+100 {
			t.Fatal("work grew with peer history", firstWork, work)
		}
		for _, statement := range statements {
			if strings.Contains(statement.SQL, "FROM documents") || strings.Contains(statement.SQL, "FROM events") {
				t.Fatal("history/body hydration", statement.SQL)
			}
			if !strings.HasPrefix(statement.SQL, "DELETE FROM scope_search_") {
				continue
			}
			plan, err := db.Query("EXPLAIN QUERY PLAN "+statement.SQL, statement.Args...)
			must(t, err)
			for plan.Next() {
				var id, parent, unused int
				var detail string
				must(t, plan.Scan(&id, &parent, &unused, &detail))
				if strings.Contains(detail, "SCAN ") || !strings.Contains(detail, "(scope_id=? AND kind=? AND rid=?)") {
					t.Fatal("unindexed resource deletion", detail)
				}
			}
			must(t, plan.Err())
			must(t, plan.Close())
		}
	}
}
