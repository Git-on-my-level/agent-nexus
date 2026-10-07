package scopedrepo_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/readmodel"
	"agent-nexus-core/internal/scopedrepo"
	"agent-nexus-core/internal/scopes"
)

// Fixture-only receipt injection. No production serving receipt writer exists;
// the real base-row verifier never calls this helper.
func dispatchFixture(t *testing.T, certified bool) (*sql.DB, *scopedrepo.Store, *scopedrepo.InboxDispatcher) {
	t.Helper()
	db, s, request, streams := orderedFeedFixture(t)
	must(t, exec(db, `DELETE FROM scope_memberships WHERE principal='owner' AND scope_id='private'`))
	seedOrderedFeed(t, db, "public", "owner", "opaque-one", orderedItem("canonical-one", "ask", "z"))
	seedOrderedFeed(t, db, "public", "role", "opaque-two", orderedItem("canonical-two", "review", "z"))
	must(t, exec(db, `INSERT INTO scope_counters VALUES('public',1,'inbox','owner','total',1),('public',1,'inbox','role','total',1),('public',1,'inbox','owner','ask',1),('public',1,'inbox','role','review',1)`))
	must(t, s.InitializeInboxDispatcherSchema(context.Background()))
	must(t, exec(db, primitives.ScopeInboxInvalidationSchemaProposal))
	must(t, exec(db, `INSERT INTO scope_inbox_source_clock VALUES(1,1,1,1,?,1,(SELECT schema_version FROM pragma_schema_version),1)`, primitives.ScopeInboxInvalidationRegistryHash()))
	installTestOnlyProof(t, db, s, request, streams)
	if certified {
		var binding string
		must(t, s.ReadOrderedBatchFeed(context.Background(), request, streams, func(r scopedrepo.OrderedBatchFeedReader) error { v, e := r.Snapshot(); binding = v.Binding; return e }))
		must(t, exec(db, `INSERT INTO scope_inbox_serving_receipts SELECT ?,source_revision,authority_revision,directory_revision,registry_hash,1,1,1,1,1,1 FROM scope_inbox_source_clock`, binding))
	}
	codec, e := readmodel.NewCursorCodec(make([]byte, 32))
	must(t, e)
	return db, s, scopedrepo.NewInboxDispatcher(s, codec)
}

func TestInboxDispatcherRequiresCanonicalDirectoryEnrichmentProof(t *testing.T) {
	for n, mutation := range []string{
		`DELETE FROM scope_inbox_serving_receipts`,
		`UPDATE scope_inbox_serving_receipts SET enrichment_version=0`,
		`UPDATE scope_inbox_serving_receipts SET discovery_version=0`,
		`UPDATE scope_inbox_serving_receipts SET derivation_version=0`,
		`UPDATE scope_inbox_serving_receipts SET source_revision=source_revision+1`,
		`UPDATE scope_inbox_serving_receipts SET authority_revision=authority_revision+1`,
		`UPDATE scope_inbox_serving_receipts SET directory_revision=directory_revision+1`,
		`UPDATE scope_inbox_serving_receipts SET registry_hash=printf('%064d',0)`,
		`UPDATE scope_inbox_source_clock SET directory_coverage_revision=NULL`,
		`UPDATE scope_inbox_source_clock SET installation_complete=0`,
		`UPDATE scope_inbox_source_clock SET source_revision=source_revision+1`,
		`UPDATE scope_inbox_source_clock SET installed_schema_version=installed_schema_version-1`,
	} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			db, _, dispatcher := dispatchFixture(t, true)
			must(t, exec(db, mutation))
			out, e := dispatcher.Read(context.Background(), "owner", 1, "")
			must(t, e)
			if out.Fallback != scopedrepo.InboxProofUnavailable || len(out.Page.Items) != 0 || out.Counts != nil {
				t.Fatal(out)
			}
			m := dispatcher.Diagnostics()
			if m.Requests != 1 || m.FallbackRequests != 1 || m.ProofUnavailable != 1 || m.ScopeAttempts != 1 || m.FallbackScopes != 1 {
				t.Fatal(m)
			}
			raw, e := json.Marshal(out)
			must(t, e)
			if string(raw) != "{}" {
				t.Fatal("internal fallback diagnosis serialized", string(raw))
			}
		})
	}
}

func TestInboxDispatcherSyntheticCompleteSnapshotReadsAndCounts(t *testing.T) {
	_, _, dispatcher := dispatchFixture(t, true)
	first, e := dispatcher.Read(context.Background(), "owner", 1, "")
	must(t, e)
	if first.Fallback != "" || len(first.Page.Items) != 1 || first.Counts["total"] != 2 || first.Counts["ask"] != 1 || first.Page.NextCursor == "" {
		t.Fatal(first)
	}
	second, e := dispatcher.Read(context.Background(), "owner", 1, first.Page.NextCursor)
	must(t, e)
	if len(second.Page.Items) != 1 || second.Page.Items[0].Ref == first.Page.Items[0].Ref || second.Page.NextCursor != "" {
		t.Fatal(second)
	}
	if dispatcher.Diagnostics().FallbackRequests != 0 {
		t.Fatal(dispatcher.Diagnostics())
	}
}

func TestInboxDispatcherRejectsCrossDatabaseContinuationWithSharedCodecKey(t *testing.T) {
	// Both fixtures have identical principals, scopes, RID allocation, generations,
	// clocks, source rows and codec keys. Only the immutable namespace differs.
	_, _, first := dispatchFixture(t, true)
	_, _, second := dispatchFixture(t, true)
	page, err := first.Read(context.Background(), "owner", 1, "")
	must(t, err)
	if page.Page.NextCursor == "" {
		t.Fatal("missing continuation")
	}
	out, err := second.Read(context.Background(), "owner", 1, page.Page.NextCursor)
	if err == nil && out.Fallback == "" {
		t.Fatal("foreign database cursor admitted", out)
	}
	if len(out.Page.Items) != 0 || out.Counts != nil {
		t.Fatal("foreign cursor disclosed data", out)
	}
}

func TestInboxDispatcherRejectsCopiedCrossDatabaseReceipts(t *testing.T) {
	for _, receipts := range []string{"selection", "serving", "both"} {
		t.Run(receipts, func(t *testing.T) {
			source, _, _ := dispatchFixture(t, true)
			target, _, dispatcher := dispatchFixture(t, true)
			var seq int
			var name, path string
			must(t, source.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &path))
			must(t, exec(target, `ATTACH DATABASE ? AS source`, path))
			if receipts != "serving" {
				must(t, exec(target, `DELETE FROM scope_feed_selection_proofs; INSERT INTO scope_feed_selection_proofs SELECT * FROM source.scope_feed_selection_proofs`))
			}
			if receipts != "selection" {
				must(t, exec(target, `DELETE FROM scope_inbox_serving_receipts; INSERT INTO scope_inbox_serving_receipts SELECT * FROM source.scope_inbox_serving_receipts`))
			}
			out, err := dispatcher.Read(context.Background(), "owner", 1, "")
			must(t, err)
			if out.Fallback != scopedrepo.InboxProofUnavailable || len(out.Page.Items) != 0 || out.Counts != nil {
				t.Fatal("foreign receipt served", out)
			}
		})
	}
}

func TestWorkspaceNamespaceIsPersistentAndImmutable(t *testing.T) {
	db, store, dispatcher := dispatchFixture(t, true)
	ctx := context.Background()
	var before, after string
	must(t, db.QueryRow(`SELECT namespace FROM scope_workspace_namespace WHERE singleton=1`).Scan(&before))
	must(t, store.Initialize(ctx))
	must(t, db.QueryRow(`SELECT namespace FROM scope_workspace_namespace WHERE singleton=1`).Scan(&after))
	if before != after || len(before) != 32 {
		t.Fatal(before, after)
	}
	for _, mutation := range []string{
		`UPDATE scope_workspace_namespace SET namespace=printf('%032d',0)`,
		`DELETE FROM scope_workspace_namespace`,
		`INSERT OR REPLACE INTO scope_workspace_namespace VALUES(1,printf('%032d',0))`,
		`INSERT OR IGNORE INTO scope_workspace_namespace VALUES(1,printf('%032d',0))`,
	} {
		if _, err := db.Exec(mutation); err == nil {
			t.Fatal("namespace mutation admitted", mutation)
		}
	}
	// Existing local receipts remain usable after failed mutations/reinitialization.
	out, err := dispatcher.Read(ctx, "owner", 1, "")
	must(t, err)
	if out.Fallback != "" || len(out.Page.Items) != 1 {
		t.Fatal(out)
	}
	// Close and reopen the database: identity is persisted, not a process nonce.
	var seq int
	var name, path string
	must(t, db.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &path))
	must(t, db.Close())
	reopenedDB, err := sql.Open("sqlite", path)
	must(t, err)
	defer reopenedDB.Close()
	reopenedDB.SetMaxOpenConns(1)
	must(t, scopedrepo.New(reopenedDB).Initialize(ctx))
	must(t, reopenedDB.QueryRow(`SELECT namespace FROM scope_workspace_namespace WHERE singleton=1`).Scan(&after))
	if after != before {
		t.Fatal("namespace changed after reopen", before, after)
	}
	codec, err := readmodel.NewCursorCodec(make([]byte, 32))
	must(t, err)
	reopened := scopedrepo.NewInboxDispatcher(scopedrepo.New(reopenedDB), codec)
	next, err := reopened.Read(ctx, "owner", 1, out.Page.NextCursor)
	must(t, err)
	if next.Fallback != "" || len(next.Page.Items) != 1 || next.Page.Items[0].Ref == out.Page.Items[0].Ref {
		t.Fatal(next)
	}
}

func TestInboxDispatcherMissingProofDoesNotReadCandidates(t *testing.T) {
	db, _, dispatcher := dispatchFixture(t, false)
	must(t, exec(db, `DROP TABLE scope_inbox_order`))
	out, e := dispatcher.Read(context.Background(), "owner", 1, "")
	must(t, e)
	if out.Fallback != scopedrepo.InboxProofUnavailable {
		t.Fatal(out)
	}
}

func TestInboxDispatcherDirectoryCannotSilentlyTruncate(t *testing.T) {
	db, _, dispatcher := dispatchFixture(t, true)
	for n := 0; n < 64; n++ {
		id := fmt.Sprintf("more-%03d", n)
		must(t, exec(db, `INSERT INTO scope_domains VALUES(?,'active',1)`, id))
		must(t, exec(db, `INSERT INTO scope_memberships VALUES('owner',?,'reader',1)`, id))
	}
	out, e := dispatcher.Read(context.Background(), "owner", 1, "")
	must(t, e)
	if out.Fallback != scopedrepo.InboxDirectoryBudget || len(out.Page.Items) != 0 || out.Counts != nil {
		t.Fatal(out)
	}
	out, e = dispatcher.Read(context.Background(), "unknown", 1, "")
	must(t, e)
	if out.Fallback != scopedrepo.InboxNoDirectory {
		t.Fatal(out)
	}
}

func TestInboxDispatcherErrorsNeverBecomeLegacyFallback(t *testing.T) {
	for _, mutation := range []string{`PRAGMA ignore_check_constraints=ON;UPDATE scope_memberships SET role='invalid-role' WHERE scope_id='public'`, `UPDATE scope_feed_payloads SET data='bad'`} {
		db, s, dispatcher := dispatchFixture(t, true)
		must(t, exec(db, mutation))
		if mutation == `UPDATE scope_feed_payloads SET data='bad'` {
			request := scopes.RequestSelection{Principal: "owner", ScopeIDs: []scopes.ID{"public"}}
			streams := []scopes.Stream{{Scope: "public", Family: "inbox", Audience: "owner"}, {Scope: "public", Family: "inbox", Audience: "role"}}
			installTestOnlyProof(t, db, s, request, streams)
			var binding string
			must(t, s.ReadOrderedBatchFeed(context.Background(), request, streams, func(r scopedrepo.OrderedBatchFeedReader) error { v, e := r.Snapshot(); binding = v.Binding; return e }))
			must(t, exec(db, `UPDATE scope_inbox_serving_receipts SET snapshot_binding=?`, binding))
		}
		out, e := dispatcher.Read(context.Background(), "owner", 1, "")
		if e == nil || out.Fallback != "" || len(out.Page.Items) != 0 || out.Counts != nil {
			t.Fatal(out, e)
		}
	}
	_, _, dispatcher := dispatchFixture(t, true)
	out, e := dispatcher.Read(context.Background(), "owner", 1, "invalid-cursor")
	if !errors.Is(e, readmodel.ErrCursor) || out.Fallback != "" {
		t.Fatal(out, e)
	}
}
