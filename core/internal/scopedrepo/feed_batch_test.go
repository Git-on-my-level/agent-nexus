package scopedrepo_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"testing"

	"agent-nexus-core/internal/scopedrepo"
	"agent-nexus-core/internal/scopes"
)

// Fixture injection ONLY. There is no production comparator or minting API.
// A raw DB owner is trusted; neither request nor render code has that handle.
func installTestOnlyProof(t *testing.T, db *sql.DB, s *scopedrepo.Store, request scopes.RequestSelection, streams []scopes.Stream) {
	t.Helper()
	var binding string
	must(t, s.ReadFeed(context.Background(), request, streams, func(r scopedrepo.FeedReader) error { v, e := r.Snapshot(); binding = v.Binding; return e }))
	must(t, exec(db, `INSERT OR REPLACE INTO scope_feed_selection_proofs SELECT ?,revision,1,1,1,1,1,1,1,1 FROM scope_feed_proof_clock WHERE singleton=1`, binding))
}

func TestBatchProofSelectionRejectsBeforeCallback(t *testing.T) {
	for i, mutation := range []string{
		`DELETE FROM scope_feed_selection_proofs`,
		`UPDATE scope_feed_selection_proofs SET authority_binding=printf('%064d',0)`,
		`UPDATE scope_feed_selection_proofs SET source_revision=source_revision+1`,
		`UPDATE scope_feed_selection_proofs SET format_version=2`,
		`UPDATE scope_feed_selection_proofs SET policy_version=2`,
		`UPDATE scope_feed_selection_proofs SET projector_version=2`,
		`UPDATE scope_feed_selection_proofs SET audience_compared=0`,
		`UPDATE scope_feed_selection_proofs SET lifecycle_compared=0`,
		`UPDATE scope_feed_selection_proofs SET payload_compared=0`,
		`UPDATE scope_feed_selection_proofs SET counters_compared=0`,
		`UPDATE scope_feed_selection_proofs SET disjoint=0`,
		`PRAGMA ignore_check_constraints=ON;UPDATE scope_feed_selection_proofs SET source_revision='bad'`,
		`UPDATE resource_access_epoch SET version=8`,
		`UPDATE scope_domains SET state='transitioning' WHERE id='public'`,
		`UPDATE scope_domains SET generation=2 WHERE id='public';UPDATE scope_feed_bindings SET generation=2`,
		`UPDATE scope_memberships SET role='reader' WHERE principal='owner' AND scope_id='public'`,
		`UPDATE scope_memberships SET generation=2 WHERE principal='owner' AND scope_id='public';UPDATE scope_feed_bindings SET membership_generation=2`,
		`UPDATE scope_feed_bindings SET binding_generation=2`,
		`UPDATE scope_feed_generations SET projection_version=2`,
		`UPDATE scope_resources SET version=2`,
		`UPDATE scope_feed_payloads SET data='{"changed":true}'`,
		`UPDATE scope_feed SET sort_key=sort_key+1`,
		`INSERT INTO scope_counters VALUES('public',1,'documents','owner','total',1)`,
		// ABA must still invalidate; matching authority tags again proves nothing.
		`UPDATE scope_memberships SET role='reader' WHERE principal='owner' AND scope_id='public';UPDATE scope_memberships SET role='writer' WHERE principal='owner' AND scope_id='public'`,
	} {
		t.Run(fmt.Sprintf("mutation-%02d", i), func(t *testing.T) {
			t.Log(mutation)
			db, s, request, streams := feedFixture(t)
			seedFeed(t, db, "public", "documents", "owner", "one", 1)
			installTestOnlyProof(t, db, s, request, streams)
			must(t, exec(db, mutation))
			// If admission accidentally attempts a candidate, this becomes a SQL error.
			must(t, exec(db, `DROP TABLE scope_feed`))
			err := s.ReadBatchFeed(context.Background(), request, streams, func(scopedrepo.BatchFeedReader) error { t.Fatal("uncertified callback"); return nil })
			if !errors.Is(err, scopes.ErrUpdating) {
				t.Fatal(err)
			}
		})
	}
}

func TestBatchWholeSelectionAuthority(t *testing.T) {
	for _, missing := range []bool{false, true} {
		db, s, request, streams := feedFixture(t)
		installTestOnlyProof(t, db, s, request, streams)
		request.ScopeIDs = append(request.ScopeIDs, "private")
		if missing {
			request.ScopeIDs[1] = "absent"
		} else {
			must(t, exec(db, `DELETE FROM scope_memberships WHERE principal='owner' AND scope_id='private'`))
		}
		err := s.ReadBatchFeed(context.Background(), request, streams, func(scopedrepo.BatchFeedReader) error { t.Fatal("partial admission"); return nil })
		if !errors.Is(err, scopes.ErrDenied) {
			t.Fatal(err)
		}
	}
	for i, mutation := range []string{
		`DELETE FROM scope_feed_bindings WHERE audience_key='role'`,
		`UPDATE scope_feed_bindings SET membership_generation=2 WHERE audience_key='role'`,
		`UPDATE scope_feed_bindings SET audience_key='wrong' WHERE audience_key='role'`,
	} {
		t.Run(fmt.Sprintf("mutation-%02d", i), func(t *testing.T) {
			t.Log(mutation)
			db, s, request, streams := feedFixture(t)
			must(t, exec(db, `INSERT INTO scope_feed_bindings VALUES('owner','public',1,'documents','role',1,1)`))
			streams = append(streams, scopes.Stream{Scope: "public", Family: "documents", Audience: "role"})
			installTestOnlyProof(t, db, s, request, streams)
			must(t, exec(db, mutation))
			err := s.ReadBatchFeed(context.Background(), request, streams, func(scopedrepo.BatchFeedReader) error { t.Fatal("partial audience admission"); return nil })
			if !errors.Is(err, scopes.ErrDenied) {
				t.Fatal(err)
			}
		})
	}
}

func TestBatchGlobalPageHydrationAndContinuation(t *testing.T) {
	db, s, request, streams := feedFixture(t)
	must(t, exec(db, `INSERT INTO scope_feed_bindings VALUES('owner','public',1,'documents','role',1,1)`))
	streams = append(streams, scopes.Stream{Scope: "public", Family: "documents", Audience: "role"})
	seedFeed(t, db, "public", "documents", "owner", "one", 1)
	seedFeed(t, db, "public", "documents", "role", "two", 2)
	seedFeed(t, db, "public", "documents", "owner", "three", 3)
	seedFeed(t, db, "public", "documents", "role", "four", 4)
	must(t, exec(db, `INSERT INTO scope_counters VALUES('public',1,'documents','owner','total',2),('public',1,'documents','role','total',2)`))
	installTestOnlyProof(t, db, s, request, streams)
	after := make([]*scopedrepo.FeedKey, 2)
	seen := map[string]bool{}
	var saved scopedrepo.BatchFeedReader
	var binding string
	for page := 0; page < 2; page++ {
		must(t, s.ReadBatchFeed(context.Background(), request, streams, func(r scopedrepo.BatchFeedReader) error {
			saved = r
			snap, e := r.Snapshot()
			if e != nil {
				return e
			}
			if page > 0 && binding != snap.Binding {
				t.Fatal("unstable snapshot")
			}
			binding = snap.Binding
			refs, e := r.Candidates(after, 2)
			if e != nil {
				return e
			}
			want := 3
			if page == 1 {
				want = 2
			}
			if len(refs) != want {
				t.Fatal(refs)
			}
			items, e := r.Hydrate(refs[:2])
			if e != nil {
				return e
			}
			for _, item := range items {
				if seen[item.Ref] {
					t.Fatal("duplicate", item.Ref)
				}
				seen[item.Ref] = true
			}
			for _, ref := range refs[:2] {
				key := ref.Candidate.Key
				after[ref.Stream] = &key
			}
			counts, e := r.Buckets([]string{"total", "absent"})
			if e == nil && (counts["total"] != 4 || counts["absent"] != 0) {
				t.Fatal(counts)
			}
			return e
		}))
	}
	if len(seen) != 4 {
		t.Fatal(seen)
	}
	if _, e := saved.Candidates(after, 2); !errors.Is(e, scopes.ErrClosed) {
		t.Fatal(e)
	}
	if _, e := saved.Hydrate(nil); !errors.Is(e, scopes.ErrClosed) {
		t.Fatal(e)
	}
	if _, e := saved.Buckets([]string{"total"}); !errors.Is(e, scopes.ErrClosed) {
		t.Fatal(e)
	}
	if _, e := saved.Snapshot(); !errors.Is(e, scopes.ErrClosed) {
		t.Fatal(e)
	}
	must(t, exec(db, `UPDATE scope_feed_payloads SET data=data`))
	installTestOnlyProof(t, db, s, request, streams)
	must(t, s.ReadBatchFeed(context.Background(), request, streams, func(r scopedrepo.BatchFeedReader) error {
		snap, e := r.Snapshot()
		if snap.Binding == binding {
			t.Fatal("source revision missing from cursor binding")
		}
		return e
	}))
}

func TestBatchFarDuplicateCannotUseGenerationFlags(t *testing.T) {
	db, s, request, streams := feedFixture(t)
	rid := seedFeed(t, db, "public", "documents", "owner", "one", 1)
	must(t, exec(db, `INSERT INTO scope_feed_bindings VALUES('owner','public',1,'documents','role',1,1);INSERT INTO scope_feed VALUES('public',1,'documents','role',100000,?,1)`, rid))
	streams = append(streams, scopes.Stream{Scope: "public", Family: "documents", Audience: "role"})
	// All old generation certificates match, but they do not prove disjointness.
	err := s.ReadBatchFeed(context.Background(), request, streams, func(scopedrepo.BatchFeedReader) error {
		t.Fatal("far duplicate admitted without disjointness proof")
		return nil
	})
	if !errors.Is(err, scopes.ErrUpdating) {
		t.Fatal(err)
	}
}

func TestBatchStickyFailureAndCounters(t *testing.T) {
	db, s, request, streams := feedFixture(t)
	seedFeed(t, db, "public", "documents", "owner", "one", 1)
	installTestOnlyProof(t, db, s, request, streams)
	for _, call := range []func(scopedrepo.BatchFeedReader) error{
		func(r scopedrepo.BatchFeedReader) error { _, e := r.Candidates(nil, 1); return e },
		func(r scopedrepo.BatchFeedReader) error {
			_, e := r.Candidates([]*scopedrepo.FeedKey{nil}, 101)
			return e
		},
		func(r scopedrepo.BatchFeedReader) error {
			_, e := r.Candidates([]*scopedrepo.FeedKey{nil}, 1)
			if e != nil {
				return e
			}
			_, e = r.Candidates([]*scopedrepo.FeedKey{nil}, 1)
			return e
		},
		func(r scopedrepo.BatchFeedReader) error {
			_, e := r.Hydrate([]scopedrepo.FeedReference{{Stream: 0, Candidate: scopedrepo.FeedCandidate{Key: scopedrepo.FeedKey{RID: 999}, Version: 1}}})
			return e
		},
		func(r scopedrepo.BatchFeedReader) error { _, e := r.Buckets([]string{"total", "total"}); return e },
	} {
		var failure error
		err := s.ReadBatchFeed(context.Background(), request, streams, func(r scopedrepo.BatchFeedReader) error {
			failure = call(r)
			if failure == nil {
				t.Fatal("expected error")
			}
			_, e := r.Snapshot()
			if !errors.Is(e, failure) {
				t.Fatal(e)
			}
			return nil
		})
		if !errors.Is(err, failure) {
			t.Fatal(err, failure)
		}
	}
	must(t, exec(db, `INSERT INTO scope_feed_bindings VALUES('owner','public',1,'documents','role',1,1)`))
	streams = append(streams, scopes.Stream{Scope: "public", Family: "documents", Audience: "role"})
	must(t, exec(db, `INSERT INTO scope_counters VALUES('public',1,'documents','owner','total',?),('public',1,'documents','role','total',1)`, int64(math.MaxInt64)))
	installTestOnlyProof(t, db, s, request, streams)
	err := s.ReadBatchFeed(context.Background(), request, streams, func(r scopedrepo.BatchFeedReader) error { _, e := r.Buckets([]string{"total"}); return e })
	if !errors.Is(err, scopedrepo.ErrFeedProjection) {
		t.Fatal("overflow", err)
	}
}

func TestBatchCanonicalHydrationIntegrity(t *testing.T) {
	for i, mutation := range []string{
		`UPDATE scope_resources SET version=2`,
		`DELETE FROM scope_feed_payloads`,
		`UPDATE scope_feed_payloads SET version=2`,
		`UPDATE scope_feed_payloads SET audience_key='wrong'`,
		`UPDATE scope_feed_payloads SET data='malformed'`,
		`PRAGMA foreign_keys=OFF;DROP TRIGGER scope_resource_source_immutable;UPDATE scope_resources SET id='changed'`,
	} {
		t.Run(fmt.Sprintf("mutation-%02d", i), func(t *testing.T) {
			t.Log(mutation)
			db, s, request, streams := feedFixture(t)
			seedFeed(t, db, "public", "documents", "owner", "one", 1)
			must(t, exec(db, mutation))
			// Deliberately inject an impossible proof to check defense in depth: an
			// actual trusted comparator must reject these before proof publication.
			installTestOnlyProof(t, db, s, request, streams)
			err := s.ReadBatchFeed(context.Background(), request, streams, func(r scopedrepo.BatchFeedReader) error {
				refs, e := r.Candidates([]*scopedrepo.FeedKey{nil}, 1)
				if e != nil {
					return e
				}
				_, e = r.Hydrate(refs)
				if !errors.Is(e, scopedrepo.ErrFeedProjection) {
					t.Fatal(e)
				}
				return nil
			})
			if !errors.Is(err, scopedrepo.ErrFeedProjection) {
				t.Fatal(err)
			}
		})
	}
}

func TestBatchEmptySelectionStillOneShot(t *testing.T) {
	db, s, request, _ := feedFixture(t)
	installTestOnlyProof(t, db, s, request, nil)
	err := s.ReadBatchFeed(context.Background(), request, nil, func(r scopedrepo.BatchFeedReader) error {
		refs, e := r.Candidates(nil, 1)
		if e != nil || len(refs) != 0 {
			t.Fatal(refs, e)
		}
		_, e = r.Candidates(nil, 1)
		if !errors.Is(e, scopes.ErrBudget) {
			t.Fatal(e)
		}
		return nil
	})
	if !errors.Is(err, scopes.ErrBudget) {
		t.Fatal(err)
	}
}
