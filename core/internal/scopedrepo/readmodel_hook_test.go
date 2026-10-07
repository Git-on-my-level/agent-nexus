package scopedrepo_test

import (
	"agent-nexus-core/internal/readmodel"
	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/scopedrepo"
	"agent-nexus-core/internal/scopes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
)

func TestReadModelCanonicalHookTransaction(t *testing.T) {
	for _, mode := range []string{"success", "rid", "version", "provenance", "semantic", "sql"} {
		t.Run(mode, func(t *testing.T) {
			db, s, request, streams := feedFixture(t)
			must(t, exec(db, `INSERT INTO scope_resources VALUES('public','doc','opaque','canonical',1)`))
			var rid int64
			must(t, db.QueryRow(`SELECT rid FROM scope_resource_rids WHERE resource_id='opaque'`).Scan(&rid))
			m := mutation()
			m.Identity.RID = rid
			m.Changes[0].Family = "documents"
			m.Changes[0].Audience = "owner"
			switch mode {
			case "rid":
				m.Identity.RID++
			case "version":
				m.Identity.CanonicalVersion = 2
				m.Changes[0].CanonicalVersion = 2
			case "provenance":
				m.Changes[0].ResourceID = "wrong"
			case "semantic":
				m.PreviousVersion = 1
				m.Identity.CanonicalVersion = 2
				m.Changes[0].CanonicalVersion = 2
				m.Changes[0].Before = &scopes.Projection{Title: "old"}
				must(t, exec(db, `UPDATE scope_resources SET version=2`))
			case "sql":
				must(t, exec(db, `CREATE TRIGGER reject_payload BEFORE INSERT ON scope_feed_payloads BEGIN SELECT RAISE(ABORT,'reject'); END;`))
			}
			ctx := context.Background()
			tx, err := resourceaccess.NewDB(db).BeginTx(ctx, nil)
			must(t, err)
			_, err = tx.ExecContext(ctx, `INSERT INTO documents VALUES('canonical','source')`)
			must(t, err)
			hook := scopedrepo.ReadModelCanonicalHook{Capture: func(c scopes.Change, p scopes.Projection) (readmodel.Entry, json.RawMessage, error) {
				return readmodel.Entry{Family: c.Family, Audience: c.Audience, Sort: 1, Buckets: []string{"unread"}}, json.RawMessage(`{"title":"source"}`), nil
			}}
			err = scopedrepo.ApplyCanonicalHooks(ctx, tx, m, hook)
			if mode == "success" {
				must(t, err)
				must(t, tx.Commit())
				must(t, s.ReadFeed(ctx, request, streams, func(r scopedrepo.FeedReader) error {
					a := scopedrepo.AdaptReadModel(r, scopes.DirectoryPage{}, "")
					_, e := a.Snapshot(ctx)
					if e != nil {
						return e
					}
					candidates, e := a.Candidates(ctx, 0, nil, 2)
					if e != nil {
						return e
					}
					if len(candidates) != 1 {
						t.Fatal(candidates)
					}
					items, e := a.Hydrate(ctx, []readmodel.Reference{{Stream: 0, Candidate: candidates[0]}})
					if e != nil {
						return e
					}
					if len(items) != 1 || items[0].Ref != "doc:opaque" {
						t.Fatal(items)
					}
					counts, e := a.Buckets(ctx, []string{"unread"})
					if e == nil && counts["unread"] != 1 {
						t.Fatal(counts)
					}
					return e
				}))
			} else {
				if err == nil {
					t.Fatal("accepted invalid projection")
				}
				if !errors.Is(tx.Commit(), sql.ErrTxDone) {
					t.Fatal("source transaction remains open")
				}
				for _, table := range []string{"documents", "scope_feed", "scope_feed_payloads", "scope_counters"} {
					var n int
					must(t, db.QueryRow(`SELECT count(*) FROM `+table).Scan(&n))
					if n != 0 {
						t.Fatal(table, n)
					}
				}
			}
		})
	}
}

func TestReadModelCanonicalHookUpdateDelete(t *testing.T) {
	db, _, _, _ := feedFixture(t)
	rid := seedFeed(t, db, "public", "documents", "owner", "opaque", 1)
	must(t, exec(db, `INSERT INTO scope_counters VALUES('public',1,'documents','owner','unread',1); INSERT INTO documents VALUES('canonical-opaque','old')`))
	capture := func(c scopes.Change, p scopes.Projection) (readmodel.Entry, json.RawMessage, error) {
		return readmodel.Entry{Family: c.Family, Audience: c.Audience, Sort: p.Timestamp, Buckets: []string{p.Status}}, json.RawMessage(`{"title":"updated"}`), nil
	}
	for version := int64(2); version <= 3; version++ {
		ctx := context.Background()
		tx, err := resourceaccess.NewDB(db).BeginTx(ctx, nil)
		must(t, err)
		_, err = tx.ExecContext(ctx, `UPDATE scope_resources SET version=? WHERE scope_id=? AND kind=? AND id=?`, version, "public", "doc", "opaque")
		must(t, err)
		m := mutation()
		m.Identity.RID = rid
		m.Identity.CanonicalID = "canonical-opaque"
		m.Identity.CanonicalVersion = version
		m.PreviousVersion = version - 1
		before := &scopes.Projection{Timestamp: 1, Status: "unread"}
		after := &scopes.Projection{Timestamp: 2, Status: "done"}
		if version == 3 {
			before = after
			after = &scopes.Projection{Deleted: true}
			_, err = tx.ExecContext(ctx, `DELETE FROM documents WHERE id=?`, "canonical-opaque")
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE documents SET title=? WHERE id=?`, "updated", "canonical-opaque")
		}
		must(t, err)
		m.Changes = []scopes.Change{{ScopeID: "public", Kind: "doc", ResourceID: "opaque", CanonicalVersion: version, Family: "documents", Audience: "owner", Before: before, After: after}}
		must(t, scopedrepo.ApplyCanonicalHooks(ctx, tx, m, scopedrepo.ReadModelCanonicalHook{Capture: capture}))
		must(t, tx.Commit())
		want := 1
		if version == 3 {
			want = 0
		}
		for _, table := range []string{"scope_feed", "scope_feed_payloads", "documents"} {
			var n int
			must(t, db.QueryRow(`SELECT count(*) FROM `+table).Scan(&n))
			if n != want {
				t.Fatal(version, table, n)
			}
		}
		var unread, done int
		must(t, db.QueryRow(`SELECT value FROM scope_counters WHERE bucket='unread'`).Scan(&unread))
		must(t, db.QueryRow(`SELECT value FROM scope_counters WHERE bucket='done'`).Scan(&done))
		if unread != 0 || done != want {
			t.Fatal(version, unread, done)
		}
		if version == 2 {
			var sort, v int
			must(t, db.QueryRow(`SELECT sort_key,version FROM scope_feed`).Scan(&sort, &v))
			if sort != 2 || v != 2 {
				t.Fatal(sort, v)
			}
		}
	}
}
