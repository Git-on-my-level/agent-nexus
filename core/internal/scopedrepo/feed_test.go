package scopedrepo_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"

	"agent-nexus-core/internal/scopedrepo"
	"agent-nexus-core/internal/scopes"
	"agent-nexus-core/internal/testsql"
)

func feedFixture(t *testing.T) (*sql.DB, *scopedrepo.Store, scopes.RequestSelection, []scopes.Stream) {
	t.Helper()
	db, s := fixture(t)
	must(t, s.InitializeFeedSchema(context.Background()))
	must(t, exec(db, `CREATE TABLE resource_access_epoch(singleton INTEGER PRIMARY KEY,version INTEGER NOT NULL);INSERT INTO resource_access_epoch VALUES(1,7);
 INSERT INTO scope_feed_generations VALUES('public',1,1,1,1,1,7);
 INSERT INTO scope_feed_bindings VALUES('owner','public',1,'documents','owner',1,1);`))
	return db, s, scopes.RequestSelection{Principal: "owner", ScopeIDs: []scopes.ID{"public"}}, []scopes.Stream{{Scope: "public", Family: "documents", Audience: "owner"}}
}

func seedFeed(t *testing.T, db *sql.DB, scope, family, audience, id string, sort int64) int64 {
	t.Helper()
	must(t, exec(db, `INSERT INTO scope_resources VALUES(?,'doc',?,?,1)`, scope, id, "canonical-"+id))
	var rid int64
	must(t, db.QueryRow(`SELECT rid FROM scope_resource_rids WHERE scope_id=? AND resource_id=?`, scope, id).Scan(&rid))
	must(t, exec(db, `INSERT INTO scope_feed VALUES(?,1,?,?,?,?,1)`, scope, family, audience, sort, rid))
	must(t, exec(db, `INSERT INTO scope_feed_payloads VALUES(?,1,?,?,?,1,?)`, scope, family, audience, rid, `{"title":"`+id+`"}`))
	return rid
}

func TestFeedAuthorityAudiencesAndSelection(t *testing.T) {
	db, s, request, streams := feedFixture(t)
	seedFeed(t, db, "public", "documents", "owner", "visible", 1)
	seedFeed(t, db, "public", "documents", "stranger", "hidden", 0)
	for _, test := range []struct {
		name    string
		request scopes.RequestSelection
		streams []scopes.Stream
	}{
		{"stranger", scopes.RequestSelection{Principal: "stranger", ScopeIDs: request.ScopeIDs}, streams},
		{"wrong audience", request, []scopes.Stream{{Scope: "public", Family: "documents", Audience: "stranger"}}},
		{"wrong family", request, []scopes.Stream{{Scope: "public", Family: "inbox", Audience: "owner"}}},
		{"unselected scope", request, []scopes.Stream{{Scope: "private", Family: "documents", Audience: "owner"}}},
		{"unauthorized scope", scopes.RequestSelection{Principal: "reader", ScopeIDs: []scopes.ID{"public", "private"}}, streams},
	} {
		t.Run(test.name, func(t *testing.T) {
			called := false
			err := s.ReadFeed(context.Background(), test.request, test.streams, func(scopedrepo.FeedReader) error { called = true; return nil })
			if !errors.Is(err, scopes.ErrDenied) || called {
				t.Fatal("authority bypass", err, called)
			}
		})
	}
	must(t, s.ReadFeed(context.Background(), request, streams, func(r scopedrepo.FeedReader) error {
		rows, err := r.Candidates(0, nil, 2)
		if err != nil {
			return err
		}
		if len(rows) != 1 {
			t.Fatal(rows)
		}
		items, err := r.Hydrate([]scopedrepo.FeedReference{{Stream: 0, Candidate: rows[0]}})
		if err == nil && (len(items) != 1 || items[0].Ref != "doc:visible" || strings.Contains(string(items[0].Data), "hidden")) {
			t.Fatal(items)
		}
		return err
	}))
}

func TestFeedMaximumPageAndCounters(t *testing.T) {
	db, s, _, _ := feedFixture(t)
	request := scopes.RequestSelection{Principal: "many"}
	var streams []scopes.Stream
	tx, err := db.Begin()
	must(t, err)
	for i := 0; i < 64; i++ {
		id := fmt.Sprintf("feed-scope-%02d", i)
		request.ScopeIDs = append(request.ScopeIDs, scopes.ID(id))
		_, err = tx.Exec(`INSERT INTO scope_domains VALUES(?,'active',1)`, id)
		must(t, err)
		_, err = tx.Exec(`INSERT INTO scope_memberships VALUES('many',?,'reader',1)`, id)
		must(t, err)
		_, err = tx.Exec(`INSERT INTO scope_feed_generations VALUES(?,1,1,1,1,1,7)`, id)
		must(t, err)
		for j := 0; j < 4; j++ {
			family := fmt.Sprintf("family-%d", j)
			streams = append(streams, scopes.Stream{Scope: scopes.ID(id), Family: family, Audience: "many"})
			_, err = tx.Exec(`INSERT INTO scope_feed_bindings VALUES('many',?,1,?,'many',1,1)`, id, family)
			must(t, err)
			resource := fmt.Sprintf("item-%d-%d", i, j)
			_, err = tx.Exec(`INSERT INTO scope_resources VALUES(?,'doc',?,?,1)`, id, resource, resource)
			must(t, err)
			var rid int64
			must(t, tx.QueryRow(`SELECT rid FROM scope_resource_rids WHERE scope_id=? AND resource_id=?`, id, resource).Scan(&rid))
			_, err = tx.Exec(`INSERT INTO scope_feed VALUES(?,1,?,'many',?,?,1)`, id, family, i*4+j, rid)
			must(t, err)
			_, err = tx.Exec(`INSERT INTO scope_feed_payloads VALUES(?,1,?,'many',?,1,'{}')`, id, family, rid)
			must(t, err)
			for _, b := range []string{"one", "two", "three", "four"} {
				_, err = tx.Exec(`INSERT INTO scope_counters VALUES(?,1,?,'many',?,1)`, id, family, b)
				must(t, err)
			}
		}
	}
	must(t, tx.Commit())
	must(t, s.ReadFeed(context.Background(), request, streams, func(r scopedrepo.FeedReader) error {
		snap, err := r.Snapshot()
		if err != nil {
			return err
		}
		if len(snap.Scopes) != 64 || len(snap.Streams) != 256 || snap.Binding == "" || snap.AsOf.IsZero() {
			t.Fatal(snap)
		}
		snap.Streams[0].Audience = "wrong"
		snap.Scopes[0].Ready = false
		var refs []scopedrepo.FeedReference
		for i := range streams {
			batch, e := r.Candidates(i, nil, 101)
			if e != nil {
				return e
			}
			if len(batch) != 1 {
				t.Fatal(i, batch)
			}
			if len(refs) < 100 {
				refs = append(refs, scopedrepo.FeedReference{Stream: i, Candidate: batch[0]})
			}
		}
		items, err := r.Hydrate(refs)
		if err != nil {
			return err
		}
		if len(items) != 100 {
			t.Fatal(len(items))
		}
		counts, err := r.Buckets([]string{"one", "two", "three", "four"})
		if err != nil {
			return err
		}
		for _, v := range counts {
			if v != 256 {
				t.Fatal(counts)
			}
		}
		_, err = r.Snapshot()
		return err
	}))
	// Exercise and count the new path at the same 64/256 maximum.
	var seq int
	var name, path string
	must(t, db.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &path))
	counted, counter := testsql.Open(path)
	defer counted.Close()
	batchStore := scopedrepo.New(counted)
	installTestOnlyProof(t, db, s, request, streams)
	counter.Reset()
	must(t, batchStore.ReadBatchFeed(context.Background(), request, streams, func(r scopedrepo.BatchFeedReader) error {
		refs, err := r.Candidates(make([]*scopedrepo.FeedKey, len(streams)), 100)
		if err != nil {
			return err
		}
		if len(refs) != 101 {
			t.Fatal(len(refs))
		}
		items, err := r.Hydrate(refs[:100])
		if err != nil {
			return err
		}
		if len(items) != 100 {
			t.Fatal(len(items))
		}
		counts, err := r.Buckets([]string{"one", "two", "three", "four"})
		if err == nil {
			for _, v := range counts {
				if v != 256 {
					t.Fatal(counts)
				}
			}
		}
		return err
	}))

	if counter.Count() != 7 || counter.ReturnedRows() != 527 {
		t.Fatalf("batch repository subtotal: SQL=%d rows=%d, want 7/527", counter.Count(), counter.ReturnedRows())
	}

}

func TestFeedUnreadyNeverReturnsAvailableZero(t *testing.T) {
	for _, mutation := range []string{
		`UPDATE scope_domains SET state='transitioning' WHERE id='public'`,
		`DELETE FROM scope_feed_generations`,
		`UPDATE scope_feed_generations SET projection_version=0`,
		`UPDATE scope_feed_generations SET audience_version=2`,
		`UPDATE scope_feed_generations SET lifecycle_version=0`,
		`UPDATE scope_feed_generations SET legacy_auth_version=0`,
		`UPDATE resource_access_epoch SET version=version+1`,
	} {
		t.Run(mutation, func(t *testing.T) {
			db, s, request, streams := feedFixture(t)
			must(t, exec(db, mutation))
			for _, method := range []string{"candidates", "hydrate", "buckets"} {
				err := s.ReadFeed(context.Background(), request, streams, func(r scopedrepo.FeedReader) error {
					snap, e := r.Snapshot()
					if e != nil {
						return e
					}
					if snap.Scopes[0].Ready {
						t.Fatal("uncertified ready")
					}
					if strings.Contains(mutation, "transitioning") && snap.Scopes[0].Availability != scopedrepo.FeedUpdating {
						t.Fatal(snap)
					}
					switch method {
					case "candidates":
						_, e = r.Candidates(0, nil, 1)
					case "hydrate":
						_, e = r.Hydrate(nil)
					case "buckets":
						v, err := r.Buckets([]string{"total"})
						e = err
						if v != nil {
							t.Fatal("synthetic zero", v)
						}
					}
					if !errors.Is(e, scopes.ErrUpdating) {
						t.Fatal(e)
					}
					return nil // Ignored failure must still abort the outer operation.
				})
				if !errors.Is(err, scopes.ErrUpdating) {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestFeedHydrationRejectsCorruptionAndUnadmittedKeys(t *testing.T) {
	for _, mutation := range []string{
		`DELETE FROM scope_feed_payloads`,
		`UPDATE scope_feed_payloads SET audience_key='wrong'`,
		`UPDATE scope_feed_payloads SET version=2`,
		`UPDATE scope_feed_payloads SET data='not JSON'`,
		`UPDATE scope_resources SET version=2`,
		`UPDATE scope_resources SET id='changed'`,
		`PRAGMA ignore_check_constraints=ON; UPDATE scope_feed_payloads SET data=printf('%20000s','x')`,
	} {
		t.Run(mutation, func(t *testing.T) {
			db, s, request, streams := feedFixture(t)
			seedFeed(t, db, "public", "documents", "owner", "row", 1)
			// Bypass integrity guards only to model persisted corruption. Ordinary
			// writers cannot redirect this source, even with foreign keys disabled.
			must(t, exec(db, `PRAGMA foreign_keys=OFF`))
			if mutation == `UPDATE scope_resources SET id='changed'` {
				must(t, exec(db, `DROP TRIGGER scope_resource_source_immutable`))
			}
			must(t, exec(db, mutation))
			err := s.ReadFeed(context.Background(), request, streams, func(r scopedrepo.FeedReader) error {
				rows, e := r.Candidates(0, nil, 1)
				if e != nil {
					return e
				}
				items, e := r.Hydrate([]scopedrepo.FeedReference{{Stream: 0, Candidate: rows[0]}})
				if items != nil || !errors.Is(e, scopedrepo.ErrFeedProjection) {
					t.Fatal(items, e)
				}
				return nil
			})
			if !errors.Is(err, scopedrepo.ErrFeedProjection) {
				t.Fatal(err)
			}
		})
	}
	db, s, request, streams := feedFixture(t)
	rid := seedFeed(t, db, "public", "documents", "owner", "row", 1)
	for _, ref := range []scopedrepo.FeedReference{
		{Stream: 0, Candidate: scopedrepo.FeedCandidate{Key: scopedrepo.FeedKey{Sort: 1, RID: rid}, Version: 2}},
		{Stream: 1, Candidate: scopedrepo.FeedCandidate{Key: scopedrepo.FeedKey{Sort: 1, RID: rid}, Version: 1}},
	} {
		err := s.ReadFeed(context.Background(), request, streams, func(r scopedrepo.FeedReader) error {
			_, e := r.Candidates(0, nil, 1)
			if e != nil {
				return e
			}
			_, e = r.Hydrate([]scopedrepo.FeedReference{ref})
			return e
		})
		if !errors.Is(err, scopedrepo.ErrFeedProjection) {
			t.Fatal(err)
		}
	}
}

func TestFeedCountersAndOverflow(t *testing.T) {
	db, s, request, streams := feedFixture(t)
	must(t, exec(db, `INSERT INTO scope_counters VALUES('public',1,'documents','owner','total',3),('public',1,'documents','stranger','total',99),('public',2,'documents','owner','total',88)`))
	must(t, s.ReadFeed(context.Background(), request, streams, func(r scopedrepo.FeedReader) error {
		v, e := r.Buckets([]string{"total", "absent"})
		if e == nil && (v["total"] != 3 || v["absent"] != 0) {
			t.Fatal(v)
		}
		return e
	}))
	must(t, exec(db, `INSERT INTO scope_feed_bindings VALUES('owner','public',1,'other','owner',1,1);INSERT INTO scope_counters VALUES('public',1,'other','owner','total',1)`))
	must(t, exec(db, `UPDATE scope_counters SET value=? WHERE family='documents' AND audience_key='owner' AND generation=1`, int64(math.MaxInt64)))
	streams = append(streams, scopes.Stream{Scope: "public", Family: "other", Audience: "owner"})
	err := s.ReadFeed(context.Background(), request, streams, func(r scopedrepo.FeedReader) error {
		v, e := r.Buckets([]string{"total"})
		if v != nil {
			t.Fatal(v)
		}
		return e
	})
	if !errors.Is(err, scopedrepo.ErrFeedProjection) {
		t.Fatal(err)
	}
}

func TestFeedCapabilityExpiryStickyErrorsAndRevocation(t *testing.T) {
	db, s, request, streams := feedFixture(t)
	var saved scopedrepo.FeedReader
	sentinel := errors.New("callback failed")
	for _, mode := range []string{"return", "error", "panic"} {
		func() {
			defer func() {
				v := recover()
				if mode == "panic" && v != sentinel {
					t.Fatal(v)
				}
				if mode != "panic" && v != nil {
					panic(v)
				}
			}()
			err := s.ReadFeed(context.Background(), request, streams, func(r scopedrepo.FeedReader) error {
				saved = r
				switch mode {
				case "panic":
					panic(sentinel)
				case "error":
					return sentinel
				}
				return nil
			})
			if mode == "error" && !errors.Is(err, sentinel) {
				t.Fatal(err)
			}
			if mode == "return" {
				must(t, err)
			}
		}()
		if _, e := saved.Snapshot(); !errors.Is(e, scopes.ErrClosed) {
			t.Fatal(e)
		}
		if _, e := saved.Candidates(0, nil, 1); !errors.Is(e, scopes.ErrClosed) {
			t.Fatal(e)
		}
		if _, e := saved.Hydrate(nil); !errors.Is(e, scopes.ErrClosed) {
			t.Fatal(e)
		}
		if _, e := saved.Buckets([]string{"x"}); !errors.Is(e, scopes.ErrClosed) {
			t.Fatal(e)
		}
	}
	must(t, exec(db, `DROP TABLE scope_feed`))
	err := s.ReadFeed(context.Background(), request, streams, func(r scopedrepo.FeedReader) error {
		_, e := r.Candidates(0, nil, 1)
		if e == nil {
			t.Fatal("query succeeded")
		}
		return nil
	})
	if err == nil {
		t.Fatal("ignored SQL error accepted")
	}
	must(t, exec(db, `DELETE FROM scope_memberships WHERE principal='owner' AND scope_id='public'`))
	err = s.ReadFeed(context.Background(), request, streams, func(scopedrepo.FeedReader) error { t.Fatal("revoked callback"); return nil })
	if !errors.Is(err, scopes.ErrDenied) {
		t.Fatal(err)
	}
}

func TestFeedBindingChangesWithAuthorityAndGeneration(t *testing.T) {
	db, s, request, streams := feedFixture(t)
	binding := func() string {
		t.Helper()
		var b string
		must(t, s.ReadFeed(context.Background(), request, streams, func(r scopedrepo.FeedReader) error { v, e := r.Snapshot(); b = v.Binding; return e }))
		return b
	}
	old := binding()
	for _, mutation := range []string{
		`UPDATE scope_feed_bindings SET binding_generation=2`,
		`UPDATE scope_memberships SET role='reader' WHERE principal='owner' AND scope_id='public'`,
		`UPDATE resource_access_epoch SET version=8`,
		`UPDATE scope_feed_generations SET legacy_auth_epoch=8`,
		`UPDATE scope_memberships SET generation=2 WHERE principal='owner' AND scope_id='public';UPDATE scope_feed_bindings SET membership_generation=2`,
		`UPDATE scope_domains SET generation=2 WHERE id='public';UPDATE scope_feed_bindings SET generation=2;UPDATE scope_feed_generations SET generation=2`,
	} {
		must(t, exec(db, mutation))
		next := binding()
		if next == old {
			t.Fatal("unchanged binding", mutation)
		}
		old = next
	}
	must(t, exec(db, `UPDATE scope_memberships SET generation=3 WHERE principal='owner' AND scope_id='public'`))
	err := s.ReadFeed(context.Background(), request, streams, func(scopedrepo.FeedReader) error { return nil })
	if !errors.Is(err, scopes.ErrDenied) {
		t.Fatal("stale membership binding", err)
	}
}

func TestFeedIndexedSeekWithIrrelevantRows(t *testing.T) {
	db, s, request, streams := feedFixture(t)
	rid := seedFeed(t, db, "public", "documents", "owner", "only-visible", 5)
	tx, err := db.Begin()
	must(t, err)
	for i := 0; i < 10000; i++ {
		_, err = tx.Exec(`INSERT INTO scope_feed VALUES('public',1,'documents','wrong',?,?,1)`, i, i+1000)
		must(t, err)
	}
	must(t, tx.Commit())
	query := `SELECT sort_key,rid,version FROM scope_feed WHERE scope_id=? AND generation=? AND family=? AND audience_key=? AND (sort_key,rid)>(?,?) ORDER BY sort_key,rid LIMIT ?`
	rows, err := db.Query("EXPLAIN QUERY PLAN "+query, "public", 1, "documents", "owner", 4, 1, 2)
	must(t, err)
	var plan string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		must(t, rows.Scan(&id, &parent, &unused, &detail))
		plan += detail
	}
	must(t, rows.Err())
	rows.Close()
	if !strings.Contains(plan, "SEARCH scope_feed USING PRIMARY KEY") || strings.Contains(plan, "TEMP B-TREE") || strings.Contains(plan, "SCAN scope_feed") {
		t.Fatal(plan)
	}
	must(t, s.ReadFeed(context.Background(), request, streams, func(r scopedrepo.FeedReader) error {
		batch, e := r.Candidates(0, &scopedrepo.FeedKey{Sort: 4, RID: 1}, 2)
		if e == nil && (len(batch) != 1 || batch[0].Key.RID != rid) {
			t.Fatal(batch)
		}
		return e
	}))
}

func TestFeedSchemaDoesNotUpgradeOrCertify(t *testing.T) {
	db, s, _, _ := feedFixture(t)
	if e := s.InitializeFeedSchema(context.Background()); e == nil {
		t.Fatal("silently accepted existing schema")
	}
	must(t, exec(db, `INSERT INTO scope_feed_generations(scope_id,generation) VALUES('private',1)`))
	var version, epoch int
	must(t, db.QueryRow(`SELECT legacy_auth_version,legacy_auth_epoch FROM scope_feed_generations WHERE scope_id='private'`).Scan(&version, &epoch))
	if version != 0 || epoch != -1 {
		t.Fatal("default certification", version, epoch)
	}
}

func TestFeedPinnedSnapshotAndNextTransactionRevocation(t *testing.T) {
	db, s, request, streams := feedFixture(t)
	must(t, exec(db, `PRAGMA journal_mode=WAL`))
	db.SetMaxOpenConns(2)
	seedFeed(t, db, "public", "documents", "owner", "old-snapshot", 1)
	must(t, s.ReadFeed(context.Background(), request, streams, func(r scopedrepo.FeedReader) error {
		before, e := r.Snapshot()
		if e != nil {
			return e
		}
		// A writer commits after authority was read. Every subsequent capability
		// query must retain the old transaction snapshot, including its payload.
		must(t, exec(db, `DELETE FROM scope_memberships WHERE principal='owner' AND scope_id='public';DELETE FROM scope_feed_payloads;UPDATE resource_access_epoch SET version=8`))
		after, e := r.Snapshot()
		if e != nil {
			return e
		}
		if before.Binding != after.Binding || before.AsOf != after.AsOf {
			t.Fatal("snapshot changed")
		}
		batch, e := r.Candidates(0, nil, 1)
		if e != nil {
			return e
		}
		items, e := r.Hydrate([]scopedrepo.FeedReference{{Stream: 0, Candidate: batch[0]}})
		if e == nil && items[0].Ref != "doc:old-snapshot" {
			t.Fatal(items)
		}
		return e
	}))
	err := s.ReadFeed(context.Background(), request, streams, func(scopedrepo.FeedReader) error { t.Fatal("revoked next transaction"); return nil })
	if !errors.Is(err, scopes.ErrDenied) {
		t.Fatal(err)
	}
}

func TestFeedMethodBudgetsAreSticky(t *testing.T) {
	db, s, request, streams := feedFixture(t)
	seedFeed(t, db, "public", "documents", "owner", "item", 1)
	cases := []struct {
		name string
		call func(scopedrepo.FeedReader) error
	}{
		{"candidate limit", func(r scopedrepo.FeedReader) error { _, e := r.Candidates(0, nil, 102); return e }},
		{"candidate repeated", func(r scopedrepo.FeedReader) error {
			_, e := r.Candidates(0, nil, 1)
			if e != nil {
				return e
			}
			_, e = r.Candidates(0, nil, 1)
			return e
		}},
		{"candidate negative stream", func(r scopedrepo.FeedReader) error { _, e := r.Candidates(-1, nil, 1); return e }},
		{"hydration length", func(r scopedrepo.FeedReader) error {
			_, e := r.Hydrate(make([]scopedrepo.FeedReference, 101))
			return e
		}},
		{"hydration repeated", func(r scopedrepo.FeedReader) error {
			_, e := r.Hydrate(nil)
			if e != nil {
				return e
			}
			_, e = r.Hydrate(nil)
			return e
		}},
		{"bucket length", func(r scopedrepo.FeedReader) error { _, e := r.Buckets([]string{"a", "b", "c", "d", "e"}); return e }},
		{"bucket duplicate", func(r scopedrepo.FeedReader) error { _, e := r.Buckets([]string{"a", "a"}); return e }},
		{"bucket text", func(r scopedrepo.FeedReader) error { _, e := r.Buckets([]string{strings.Repeat("a", 129)}); return e }},
		{"bucket repeated", func(r scopedrepo.FeedReader) error {
			_, e := r.Buckets([]string{"a"})
			if e != nil {
				return e
			}
			_, e = r.Buckets([]string{"b"})
			return e
		}},
		{"snapshot count", func(r scopedrepo.FeedReader) error {
			for i := 0; i < 8; i++ {
				_, e := r.Snapshot()
				if e != nil {
					return e
				}
			}
			_, e := r.Snapshot()
			return e
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := s.ReadFeed(context.Background(), request, streams, func(r scopedrepo.FeedReader) error {
				if e := c.call(r); !errors.Is(e, scopes.ErrBudget) {
					t.Fatal(e)
				}
				return nil
			})
			if !errors.Is(err, scopes.ErrBudget) {
				t.Fatal("ignored budget error", err)
			}
		})
	}
}

func TestFeedSerializedCapabilitiesAndCopiedResults(t *testing.T) {
	db, s, request, streams := feedFixture(t)
	seedFeed(t, db, "public", "documents", "owner", "item", 1)
	must(t, s.ReadFeed(context.Background(), request, streams, func(r scopedrepo.FeedReader) error {
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				snapshot, e := r.Snapshot()
				if e != nil {
					t.Error(e)
					return
				}
				snapshot.Scopes[0].ID = "tampered"
				snapshot.Streams[0].Audience = "tampered"
			}()
		}
		wg.Wait()
		rows, e := r.Candidates(0, nil, 1)
		if e != nil {
			return e
		}
		original := rows[0]
		rows[0].Version = 200
		items, e := r.Hydrate([]scopedrepo.FeedReference{{Stream: 0, Candidate: original}})
		if e == nil && items[0].Ref != "doc:item" {
			t.Fatal(items)
		}
		return e
	}))
}
