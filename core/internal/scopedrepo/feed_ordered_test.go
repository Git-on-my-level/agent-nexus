package scopedrepo_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/readmodel"
	"agent-nexus-core/internal/scopedrepo"
	"agent-nexus-core/internal/scopes"
	"agent-nexus-core/internal/testsql"
)

func orderedFeedFixture(t *testing.T) (*sql.DB, *scopedrepo.Store, scopes.RequestSelection, []scopes.Stream) {
	t.Helper()
	db, s, request, _ := feedFixture(t)
	must(t, s.InitializeInboxOrderSchema(context.Background()))
	must(t, exec(db, `INSERT INTO scope_feed_bindings VALUES('owner','public',1,'inbox','owner',1,1),('owner','public',1,'inbox','role',1,1)`))
	return db, s, request, []scopes.Stream{{Scope: "public", Family: "inbox", Audience: "owner"}, {Scope: "public", Family: "inbox", Audience: "role"}}
}

func seedOrderedFeed(t *testing.T, db *sql.DB, scope, audience, opaque string, item primitives.DerivedInboxItem) scopes.ResourceIdentity {
	t.Helper()
	must(t, exec(db, `INSERT INTO scope_resources VALUES(?,'inbox',?,?,1)`, scope, opaque, item.ID))
	i := scopes.ResourceIdentity{ScopeID: scopes.ID(scope), Kind: "inbox", ResourceID: opaque, CanonicalID: item.ID, CanonicalVersion: 1}
	must(t, db.QueryRow(`SELECT rid FROM scope_resource_rids WHERE scope_id=? AND kind='inbox' AND resource_id=?`, scope, opaque).Scan(&i.RID))
	key, err := primitives.ScopeInboxSortKey(item)
	must(t, err)
	data, err := primitives.EncodeScopeInbox(i, item)
	must(t, err)
	must(t, exec(db, `INSERT INTO scope_inbox_order VALUES(?,1,'inbox',?,?,?,1)`, scope, audience, key, i.RID))
	must(t, exec(db, `INSERT INTO scope_feed_payloads VALUES(?,1,'inbox',?,?,1,?)`, scope, audience, i.RID, string(data)))
	return i
}
func orderedItem(id, category, trigger string) primitives.DerivedInboxItem {
	return primitives.DerivedInboxItem{ID: id, ThreadID: "thread", Category: category, TriggerAt: trigger, Data: map[string]any{"opaque_extension": true}}
}

func TestOrderedBatchBINARYComparatorAndCiphertextContinuation(t *testing.T) {
	db, s, request, streams := orderedFeedFixture(t)
	must(t, exec(db, `CREATE TABLE legacy_items(id TEXT,category TEXT,trigger_at TEXT)`))
	items := []primitives.DerivedInboxItem{
		orderedItem("z", "ask", "2026-10-01T00:00:00Z"), orderedItem("a", "ask", "2026-10-01T00:00:00Z"),
		orderedItem("fraction-z", "ask", "2026-10-01T00:00:00.999Z"), orderedItem("fraction-a", "ask", "2026-10-01T00:00:00.001Z"),
		orderedItem("é", " ask ", "prefix"), orderedItem("Z", "ask", "prefix"), orderedItem("a-long", "ask", "prefix-long"),
		orderedItem("urgent", "escalate", "a"), orderedItem("review", "review", "z"), orderedItem("unknown", "unknown", "z"),
	}
	opaques := map[string]string{}
	for n, item := range items {
		opaque := fmt.Sprintf("private-opaque-%d", n)
		opaques[item.ID] = "inbox:" + opaque
		seedOrderedFeed(t, db, "public", streams[n%2].Audience, opaque, item)
		must(t, exec(db, `INSERT INTO legacy_items VALUES(?,?,?)`, item.ID, item.Category, item.TriggerAt))
	}
	installTestOnlyProof(t, db, s, request, streams)
	rows, err := db.Query(`SELECT id FROM legacy_items ORDER BY CASE trim(category) WHEN 'escalate' THEN 0 WHEN 'ask' THEN 1 WHEN 'review' THEN 2 ELSE 99 END,trigger_at COLLATE BINARY DESC,id COLLATE BINARY`)
	must(t, err)
	var expected []string
	for rows.Next() {
		var id string
		must(t, rows.Scan(&id))
		expected = append(expected, opaques[id])
	}
	must(t, rows.Err())
	must(t, rows.Close())
	codec, err := readmodel.NewCursorCodec(make([]byte, 32))
	must(t, err)
	var actual []string
	var token string
	var saved scopedrepo.OrderedReadModelReader
	for page := 0; page < 10; page++ {
		var p readmodel.Page
		must(t, s.ReadOrderedBatchFeed(context.Background(), request, streams, func(r scopedrepo.OrderedBatchFeedReader) error {
			saved = scopedrepo.AdaptOrderedReadModel(r, scopes.DirectoryPage{}, "")
			var err error
			p, err = readmodel.ReadOrdered(context.Background(), saved, codec, 3, token)
			return err
		}))
		for _, item := range p.Items {
			actual = append(actual, item.Ref)
			if strings.Contains(string(item.Data), `"identity"`) {
				t.Fatal("private envelope leaked", string(item.Data))
			}
		}
		token = p.NextCursor
		if len(token) > 2048 {
			t.Fatal("cursor budget", len(token))
		}
		if token == "" {
			break
		}
	}
	if strings.Join(actual, ",") != strings.Join(expected, ",") {
		t.Fatalf("got %v want %v", actual, expected)
	}
	if _, err = saved.OrderedCandidates(context.Background(), 1, nil, 4); !errors.Is(err, scopes.ErrClosed) {
		t.Fatal("cached rows survived transaction", err)
	}
	// Reproof after an ABA preserves authority but changes the encrypted binding.
	var old string
	must(t, s.ReadOrderedBatchFeed(context.Background(), request, streams, func(r scopedrepo.OrderedBatchFeedReader) error {
		p, e := readmodel.ReadOrdered(context.Background(), scopedrepo.AdaptOrderedReadModel(r, scopes.DirectoryPage{}, ""), codec, 1, "")
		old = p.NextCursor
		return e
	}))
	must(t, exec(db, `UPDATE scope_inbox_order SET version=version`))
	installTestOnlyProof(t, db, s, request, streams)
	err = s.ReadOrderedBatchFeed(context.Background(), request, streams, func(r scopedrepo.OrderedBatchFeedReader) error {
		_, e := readmodel.ReadOrdered(context.Background(), scopedrepo.AdaptOrderedReadModel(r, scopes.DirectoryPage{}, ""), codec, 1, old)
		return e
	})
	if !errors.Is(err, readmodel.ErrCursor) {
		t.Fatal(err)
	}
}

func TestOrderedBatchFarDuplicateAndStaleProof(t *testing.T) {
	for _, mutation := range []string{`DELETE FROM scope_feed_selection_proofs`, `UPDATE scope_inbox_order SET order_key=x'ffff'`, `UPDATE scope_inbox_order SET version=version`} {
		db, s, request, streams := orderedFeedFixture(t)
		seedOrderedFeed(t, db, "public", "owner", "opaque", orderedItem("canonical", "ask", "z"))
		installTestOnlyProof(t, db, s, request, streams)
		must(t, exec(db, mutation))
		err := s.ReadOrderedBatchFeed(context.Background(), request, streams, func(scopedrepo.OrderedBatchFeedReader) error { t.Fatal("stale callback"); return nil })
		if !errors.Is(err, scopes.ErrUpdating) {
			t.Fatal(err)
		}
	}
	db, s, request, streams := orderedFeedFixture(t)
	i := seedOrderedFeed(t, db, "public", "owner", "opaque", orderedItem("canonical", "ask", "z"))
	must(t, exec(db, `INSERT INTO scope_inbox_order VALUES('public',1,'inbox','role',x'ffff',?,1)`, i.RID))
	err := s.ReadOrderedBatchFeed(context.Background(), request, streams, func(scopedrepo.OrderedBatchFeedReader) error {
		t.Fatal("far duplicate admitted without full proof")
		return nil
	})
	if !errors.Is(err, scopes.ErrUpdating) {
		t.Fatal(err)
	}
}

func TestOrderedBatchCorruptionFailsClosedAndSticky(t *testing.T) {
	mutations := []string{
		`PRAGMA ignore_check_constraints=ON;UPDATE scope_inbox_order SET order_key='text'`,
		`PRAGMA ignore_check_constraints=ON;UPDATE scope_inbox_order SET order_key=zeroblob(771)`,
		`PRAGMA ignore_check_constraints=ON;UPDATE scope_inbox_order SET version=1.5`,
		`DELETE FROM scope_feed_payloads`,
		`UPDATE scope_resources SET version=2`,
		`UPDATE scope_feed_payloads SET data='invalid-json'`,
		`PRAGMA ignore_check_constraints=ON;UPDATE scope_feed_payloads SET data=printf('%20000s','x')`,
		`UPDATE scope_feed_payloads SET data=json_set(data,'$.identity.rid',99999)`,
		`UPDATE scope_feed_payloads SET data=json_set(data,'$.identity.scope','private')`,
		`UPDATE scope_feed_payloads SET data=json_set(data,'$.item.ID','wrong')`,
	}
	for n, mutation := range mutations {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			db, s, request, streams := orderedFeedFixture(t)
			seedOrderedFeed(t, db, "public", "owner", "opaque", orderedItem("canonical", "ask", "z"))
			must(t, exec(db, mutation))
			installTestOnlyProof(t, db, s, request, streams)
			codec, err := readmodel.NewCursorCodec(make([]byte, 32))
			must(t, err)
			var observed error
			err = s.ReadOrderedBatchFeed(context.Background(), request, streams, func(r scopedrepo.OrderedBatchFeedReader) error {
				a := scopedrepo.AdaptOrderedReadModel(r, scopes.DirectoryPage{}, "")
				_, observed = readmodel.ReadOrdered(context.Background(), a, codec, 1, "")
				if observed == nil {
					t.Fatal("corrupt data accepted", mutation)
				}
				if _, e := a.Snapshot(context.Background()); !errors.Is(e, observed) {
					t.Fatal("adapter failure not sticky", e, observed)
				}
				return nil // Ignoring the failure must still abort the callback transaction.
			})
			if err == nil {
				t.Fatal("ignored failure committed")
			}
		})
	}
}

func TestOrderedBatchAdmissionCopiesAndBudgets(t *testing.T) {
	db, s, request, streams := orderedFeedFixture(t)
	seedOrderedFeed(t, db, "public", "owner", "opaque", orderedItem("canonical", "ask", "z"))
	installTestOnlyProof(t, db, s, request, streams)
	must(t, s.ReadOrderedBatchFeed(context.Background(), request, streams, func(r scopedrepo.OrderedBatchFeedReader) error {
		refs, e := r.Candidates(nil, 1)
		if e != nil {
			return e
		}
		saved := refs[0]
		saved.Candidate.Key.Order = append([]byte(nil), saved.Candidate.Key.Order...)
		refs[0].Candidate.Key.Order[0] ^= 255
		items, e := r.Hydrate([]scopedrepo.OrderedFeedReference{saved})
		if e != nil {
			return e
		}
		if items[0].Identity.CanonicalID != "canonical" {
			t.Fatal(items)
		}
		items[0].Data[0] = 'x'
		return nil
	}))
	for _, call := range []func(scopedrepo.OrderedBatchFeedReader) error{
		func(r scopedrepo.OrderedBatchFeedReader) error { _, e := r.Candidates(nil, 101); return e },
		func(r scopedrepo.OrderedBatchFeedReader) error {
			_, e := r.Candidates(&scopedrepo.OrderedFeedKey{Order: []byte{1}}, 1)
			return e
		},
		func(r scopedrepo.OrderedBatchFeedReader) error {
			_, e := r.Hydrate([]scopedrepo.OrderedFeedReference{{Stream: 256}})
			return e
		},
		func(r scopedrepo.OrderedBatchFeedReader) error {
			_, e := r.Candidates(nil, 1)
			if e != nil {
				return e
			}
			_, e = r.Candidates(nil, 1)
			return e
		},
	} {
		err := s.ReadOrderedBatchFeed(context.Background(), request, streams, func(r scopedrepo.OrderedBatchFeedReader) error {
			if e := call(r); e == nil {
				t.Fatal("missing budget/admission error")
			}
			return nil
		})
		if err == nil {
			t.Fatal("ignored error committed")
		}
	}
	// A corrupted directory overlay also poisons the underlying transaction.
	err := s.ReadOrderedBatchFeed(context.Background(), request, streams, func(r scopedrepo.OrderedBatchFeedReader) error {
		a := scopedrepo.AdaptOrderedReadModel(r, scopes.DirectoryPage{Bindings: []scopes.Binding{{ID: "private", Available: true}}}, "")
		_, e := a.Snapshot(context.Background())
		if e == nil {
			t.Fatal("wrong directory")
		}
		return nil
	})
	if err == nil {
		t.Fatal("ignored directory error committed")
	}
}

func TestOrderedBatchMaximumRepositorySubtotal(t *testing.T) {
	db, s, _, _ := orderedFeedFixture(t)
	request := scopes.RequestSelection{Principal: "many"}
	var streams []scopes.Stream
	for scope := 0; scope < 64; scope++ {
		id := fmt.Sprintf("ordered-scope-%d", scope)
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
	installTestOnlyProof(t, db, s, request, streams)
	var seq int
	var name, path string
	must(t, db.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &path))
	counted, counter := testsql.Open(path)
	defer counted.Close()
	counter.Reset()
	codec, e := readmodel.NewCursorCodec(make([]byte, 32))
	must(t, e)
	must(t, scopedrepo.New(counted).ReadOrderedBatchFeed(context.Background(), request, streams, func(r scopedrepo.OrderedBatchFeedReader) error {
		a := scopedrepo.AdaptOrderedReadModel(r, scopes.DirectoryPage{}, "")
		p, e := readmodel.ReadOrdered(context.Background(), a, codec, 100, "")
		if e != nil {
			return e
		}
		if len(p.Items) != 100 || p.NextCursor == "" {
			t.Fatal(len(p.Items), p.NextCursor)
		}
		counts, e := a.Buckets(context.Background(), []string{"total", "absent", "other", "last"})
		if counts["total"] != 256 {
			t.Fatal(counts)
		}
		return e
	}))
	if counter.Count() != 7 || counter.ReturnedRows() != 527 {
		t.Fatalf("repository subtotal queries=%d rows=%d", counter.Count(), counter.ReturnedRows())
	}
	for _, statement := range counter.Statements() {
		if strings.Contains(statement.SQL, "WITH RECURSIVE") || strings.Contains(statement.SQL, "resource_access_edges") {
			t.Fatal("legacy graph read", statement.SQL)
		}
	}
}

func TestOrderedAdapterGlobalSeekCopiesAndCancellation(t *testing.T) {
	db, s, request, streams := orderedFeedFixture(t)
	seedOrderedFeed(t, db, "public", "owner", "opaque", orderedItem("canonical", "ask", "z"))
	seedOrderedFeed(t, db, "public", "role", "opaque-role", orderedItem("role-canonical", "ask", "z"))
	installTestOnlyProof(t, db, s, request, streams)
	directory := scopes.DirectoryPage{Bindings: []scopes.Binding{{ID: "public", Available: true}}}
	must(t, s.ReadOrderedBatchFeed(context.Background(), request, streams, func(r scopedrepo.OrderedBatchFeedReader) error {
		a := scopedrepo.AdaptOrderedReadModel(r, directory, "")
		directory.Bindings[0].ID = "private"
		snap, err := a.Snapshot(context.Background())
		if err != nil {
			return err
		}
		snap.Scopes[0].ID = "mutated"
		snap.Streams[0].Audience = "mutated"
		snap, err = a.Snapshot(context.Background())
		if err != nil {
			return err
		}
		if snap.Scopes[0].ID != "public" || snap.Streams[0].Audience != "owner" {
			t.Fatal("snapshot aliases internal state", snap)
		}
		first, err := a.OrderedCandidates(context.Background(), 0, nil, 3)
		if err != nil {
			return err
		}
		first[0].Key.Order[0] ^= 255
		second, err := a.OrderedCandidates(context.Background(), 1, nil, 3)
		if err != nil {
			return err
		}
		if len(second) != 1 {
			t.Fatal(second)
		}
		_, err = a.HydrateOrdered(context.Background(), []readmodel.OrderedReference{{Stream: 1, Candidate: second[0]}})
		return err
	}))
	for _, mode := range []string{"different-key", "different-limit", "cancelled"} {
		var saved scopedrepo.OrderedReadModelReader
		err := s.ReadOrderedBatchFeed(context.Background(), request, streams, func(r scopedrepo.OrderedBatchFeedReader) error {
			a := scopedrepo.AdaptOrderedReadModel(r, scopes.DirectoryPage{}, "")
			saved = a
			if _, e := a.Snapshot(context.Background()); e != nil {
				return e
			}
			if _, e := a.OrderedCandidates(context.Background(), 0, nil, 3); e != nil {
				return e
			}
			var key *readmodel.OrderedKey
			limit := 3
			ctx := context.Background()
			switch mode {
			case "different-key":
				key = &readmodel.OrderedKey{Order: []byte{1}, RID: 1}
			case "different-limit":
				limit = 2
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if _, e := a.OrderedCandidates(ctx, 1, key, limit); e == nil {
				t.Fatal("inconsistent/cancelled branch accepted", mode)
			}
			return nil
		})
		if err == nil {
			t.Fatal("ignored adapter error committed", mode)
		}
		if _, err := saved.Snapshot(context.Background()); !errors.Is(err, scopes.ErrClosed) {
			t.Fatal("failed adapter survived expiry", err)
		}
	}
}

func TestOrderedAdapterRejectsInvalidTypedInboxPayload(t *testing.T) {
	for _, patch := range []string{
		`json_set(data,'$.item.ThreadID',42)`,
		`json_set(data,'$.item.Data',42)`,
		`json_remove(data,'$.item.Category')`,
		`json_remove(data,'$.item.TriggerAt')`,
		`json_set(data,'$.item.ThreadID',char(0))`,
	} {
		t.Run(patch, func(t *testing.T) {
			db, s, request, streams := orderedFeedFixture(t)
			seedOrderedFeed(t, db, "public", "owner", "opaque", orderedItem("canonical", "ask", "z"))
			must(t, exec(db, `UPDATE scope_feed_payloads SET data=`+patch))
			// Impossible fixture certificate exercises corruption defense in depth.
			installTestOnlyProof(t, db, s, request, streams)
			codec, e := readmodel.NewCursorCodec(make([]byte, 32))
			must(t, e)
			e = s.ReadOrderedBatchFeed(context.Background(), request, streams, func(r scopedrepo.OrderedBatchFeedReader) error {
				a := scopedrepo.AdaptOrderedReadModel(r, scopes.DirectoryPage{}, "")
				_, err := readmodel.ReadOrdered(context.Background(), a, codec, 1, "")
				if !errors.Is(err, scopedrepo.ErrFeedProjection) {
					t.Fatal("invalid typed payload admitted", err)
				}
				return nil // Sticky rejection must survive an ignored error.
			})
			if !errors.Is(e, scopedrepo.ErrFeedProjection) {
				t.Fatal(e)
			}
		})
	}
}
