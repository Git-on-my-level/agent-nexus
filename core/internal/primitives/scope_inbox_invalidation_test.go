package primitives

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/storage"
)

func inboxInvalidationFixture(t *testing.T, withPM bool) (*sql.DB, *Store) {
	t.Helper()
	ws, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	for _, path := range []string{"../scopedrepo/schema.sql", "../scopedrepo/feed_schema.sql"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = ws.DB().Exec(string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	if withPM {
		inboxInvalidationInstallPM(t, ws.DB())
	}
	inboxInvalidationInstall(t, ws.DB(), withPM)
	return ws.DB(), NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
}

func inboxInvalidationInstallPM(t *testing.T, db *sql.DB) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	// The production PM store creates this canonical table in its schema tx,
	// then installs the legacy ownership index in that same transaction.
	if _, err = tx.Exec(`CREATE TABLE pm_records(kind TEXT NOT NULL,id TEXT NOT NULL,workspace_id TEXT NOT NULL,actor_id TEXT NOT NULL,parent_id TEXT NOT NULL DEFAULT '',revision INTEGER NOT NULL,body BLOB NOT NULL,PRIMARY KEY(kind,id))`); err != nil {
		t.Fatal(err)
	}
	if err = resourceaccess.InstallPMAccess(context.Background(), tx, false); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func inboxInvalidationInstall(t *testing.T, db *sql.DB, wantComplete bool) ScopeInboxInvalidationInstallation {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	got, err := InstallScopeInboxInvalidation(context.Background(), tx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Complete != wantComplete {
		t.Fatalf("installation %+v", got)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return got
}

func inboxInvalidationClocks(t *testing.T, db interface{ QueryRow(string, ...any) *sql.Row }) [4]int64 {
	t.Helper()
	var v [4]int64
	if err := db.QueryRow(`SELECT source_revision,authority_revision,directory_revision,(SELECT revision FROM scope_feed_proof_clock WHERE singleton=1) FROM scope_inbox_source_clock WHERE singleton=1`).Scan(&v[0], &v[1], &v[2], &v[3]); err != nil {
		t.Fatal(err)
	}
	return v
}

func inboxInvalidationCertifyFixture(t *testing.T, db *sql.DB) {
	t.Helper()
	// Synthetic receipts are test-only; installation itself never mints them.
	if _, err := db.Exec(`UPDATE scope_inbox_source_clock SET directory_coverage_revision=directory_revision; INSERT OR REPLACE INTO scope_feed_selection_proofs SELECT ?,revision,1,1,1,1,1,1,1,1 FROM scope_feed_proof_clock WHERE singleton=1`, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
}

func inboxInvalidationAssertAdvance(t *testing.T, db *sql.DB, write func() error) {
	t.Helper()
	inboxInvalidationCertifyFixture(t, db)
	before := inboxInvalidationClocks(t, db)
	if err := write(); err != nil {
		t.Fatal(err)
	}
	after := inboxInvalidationClocks(t, db)
	t.Logf("revision advances source/authority/directory/selection: %v", [4]int64{after[0] - before[0], after[1] - before[1], after[2] - before[2], after[3] - before[3]})
	for i := range before {
		if after[i] <= before[i] {
			t.Fatalf("clock %d did not advance: %v -> %v", i, before, after)
		}
	}
	var covered, proof bool
	if err := db.QueryRow(`SELECT directory_coverage_revision IS NOT NULL,EXISTS(SELECT 1 FROM scope_feed_selection_proofs p JOIN scope_feed_proof_clock c ON p.source_revision=c.revision) FROM scope_inbox_source_clock`).Scan(&covered, &proof); err != nil {
		t.Fatal(err)
	}
	if covered || proof {
		t.Fatal("source mutation retained directory/selection certification")
	}
}

func TestScopeInboxInvalidationRegistryComplete(t *testing.T) {
	db, _ := inboxInvalidationFixture(t, true)
	sources, hash, err := scopeInboxInvalidationSources()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("invalidation registry: %d tables, %d persistent triggers, hash %s", len(sources), len(sources)*3, hash)
	byTable := map[string]scopeInboxInvalidationSource{}
	for _, s := range sources {
		byTable[s.Table] = s
	}
	for _, s := range ScopeInboxMutationLedger() {
		got, ok := byTable[s.Table]
		if !ok || got.Late != s.Optional {
			t.Fatalf("canonical ledger not reconciled: %+v", s)
		}
	}
	for _, s := range append(append([]resourceaccess.OwnershipSource{}, resourceaccess.OwnershipSources...), resourceaccess.FilterOwnershipSources()...) {
		got, ok := byTable[s.Table]
		if !ok {
			t.Fatalf("uncovered source %s", s.Table)
		}
		for _, col := range append([]string{s.ID}, s.Columns...) {
			found := false
			for _, candidate := range got.Columns {
				found = found || candidate == col
			}
			if !found {
				t.Fatalf("unvalidated authoritative column %s.%s", s.Table, col)
			}
		}
	}
	for _, s := range sources {
		for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
			var ddl string
			if err = db.QueryRow(`SELECT sql FROM sqlite_schema WHERE type='trigger' AND tbl_name=? AND name=?`, s.Table, "scope_inbox_invalidate_"+s.Table+"_"+strings.ToLower(op)).Scan(&ddl); err != nil {
				t.Fatal(s.Table, op, err)
			}
			if ddl != scopeInboxInvalidationTrigger(s.Table, op) {
				t.Fatalf("incorrect invalidation %s %s", s.Table, op)
			}
		}
	}
	snapshot, err := ReadScopeInboxSourceSnapshot(context.Background(), db)
	if err != nil || snapshot.RegistryHash != hash || snapshot.DirectoryCovered() {
		t.Fatalf("snapshot %+v %v", snapshot, err)
	}
	// Independently pin dependencies beyond OwnershipSources, including directory
	// creation/grants, legacy graph materializations and every inbox enrichment.
	for _, table := range []string{"pm_records", "resource_access_pm_refs", "resource_access_epoch", "resource_handle_aliases", "resource_access_edges", "resource_access_external_edges", "resource_access_exact_edges", "resource_access_tombstones", "resource_access_identities", "resource_access_mentions", "resource_access_mention_buckets", "resource_access_series_refs", "resource_access_series_unknown", "ref_edges", "scope_domains", "scope_memberships", "scope_resources", "scope_resource_rids", "scope_aliases", "scope_feed_bindings", "human_attention_request_resolutions", "human_attention_answer_reads", "human_attention_response_claims", "human_attention_answer_wake_batches", "access_requests", "derived_topic_views", "derived_topic_dirty_queue", "topic_projection_refresh_status"} {
		if _, ok := byTable[table]; !ok {
			t.Fatal("uncovered dependency", table)
		}
	}
}

func TestScopeInboxInvalidationRegistryPinsSourceMeaning(t *testing.T) {
	_, before, err := scopeInboxInvalidationSources()
	if err != nil {
		t.Fatal(err)
	}
	if before != ScopeInboxInvalidationRegistryHash() {
		t.Fatal("compiled registry drift")
	}
	// No parallel tests modify this executable registry. Restore each mutation
	// before returning so later storage fixtures retain the canonical inventory.
	old := resourceaccess.OwnershipSources[0]
	defer func() { resourceaccess.OwnershipSources[0] = old }()
	resourceaccess.OwnershipSources[0].Kind = "different-source-kind"
	_, after, err := scopeInboxInvalidationSources()
	if err != nil || before == after {
		t.Fatal("source kind reinterpretation retained hash", err)
	}
	resourceaccess.OwnershipSources[0] = old
	resourceaccess.OwnershipSources[0].ID = old.Columns[0]
	_, after, err = scopeInboxInvalidationSources()
	if err != nil || before == after {
		t.Fatal("source identity reinterpretation retained hash", err)
	}
}

func TestScopeInboxInvalidationCanonicalAndBulk(t *testing.T) {
	db, s := inboxInvalidationFixture(t, true)
	ctx := context.Background()
	thread, err := s.CreateThread(ctx, "owner", map[string]any{"title": "source"})
	if err != nil {
		t.Fatal(err)
	}
	threadID := anyStringValue(thread.Thread["id"])
	item := DerivedInboxItem{ID: "canonical-ask", ThreadID: threadID, Category: "ask", TriggerAt: "now", GeneratedAt: "now", Data: map[string]any{"title": "first", "kind": "decision_requested"}}
	for _, name := range []string{"create", "change", "noop", "ABA", "delete", "empty-replace", "bulk-replace", "import-replace"} {
		t.Run(name, func(t *testing.T) {
			inboxInvalidationAssertAdvance(t, db, func() error {
				switch name {
				case "create":
					return s.ReplaceDerivedInboxItems(ctx, threadID, []DerivedInboxItem{item})
				case "change":
					_, err := db.Exec(`UPDATE derived_inbox_items SET data_json='{"title":"second"}' WHERE id=?`, item.ID)
					return err
				case "noop":
					_, err := db.Exec(`UPDATE derived_inbox_items SET data_json=data_json WHERE id=?`, item.ID)
					return err
				case "ABA":
					_, err := db.Exec(`UPDATE derived_inbox_items SET data_json='{"title":"first"}' WHERE id=?`, item.ID)
					return err
				case "delete":
					_, err := db.Exec(`DELETE FROM derived_inbox_items WHERE id=?`, item.ID)
					return err
				case "empty-replace":
					if err := s.ReplaceDerivedInboxItems(ctx, threadID, []DerivedInboxItem{item}); err != nil {
						return err
					}
					return s.ReplaceDerivedInboxItems(ctx, threadID, nil)
				case "bulk-replace":
					items := []DerivedInboxItem{}
					for _, id := range []string{"a", "b", "c"} {
						i := item
						i.ID = id
						items = append(items, i)
					}
					return s.ReplaceDerivedInboxItems(ctx, threadID, items)
				default:
					_, err := db.Exec(`INSERT OR REPLACE INTO derived_inbox_items(id,thread_id,category,trigger_at,generated_at,data_json) VALUES('a',?,'ask','now','now','{"title":"import"}')`, threadID)
					return err
				}
			})
		})
	}
}

func TestScopeInboxInvalidationOutsideInboxAuthorityAndEnrichment(t *testing.T) {
	db, s := inboxInvalidationFixture(t, true)
	ctx := context.Background()
	work, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "outside inbox"})
	if err != nil {
		t.Fatal(err)
	}
	threadID := anyStringValue(work["thread_id"])
	for _, name := range []string{"owner", "mention", "alias", "lifecycle", "authority-ABA", "answer", "read", "response-claim", "answer-batch", "refresh", "directory-create", "membership"} {
		t.Run(name, func(t *testing.T) {
			inboxInvalidationAssertAdvance(t, db, func() error {
				switch name {
				case "owner":
					_, err := s.PatchThread(ctx, "owner", threadID, map[string]any{"pm_actor_id": "private-owner"}, nil)
					return err
				case "mention":
					_, err := db.Exec(`UPDATE cards SET summary='See thread:'||? WHERE id=?`, threadID, work["id"])
					return err
				case "alias":
					_, err := db.Exec(`INSERT INTO resource_handle_aliases(id,resource_type,resource_id,alias_handle,canonical_handle,created_at) VALUES('alias-id','card',?,'alias-outside',?,'now')`, work["id"], work["id"])
					return err
				case "lifecycle":
					_, err := db.Exec(`UPDATE cards SET archived_at='now' WHERE id=?`, work["id"])
					return err
				case "authority-ABA":
					_, err := s.PatchThread(ctx, "owner", threadID, map[string]any{"pm_actor_id": ""}, nil)
					return err
				case "answer":
					_, err := db.Exec(`INSERT INTO human_attention_request_resolutions VALUES('ask','answer','answered','owner','now')`)
					return err
				case "read":
					_, err := db.Exec(`INSERT INTO human_attention_answer_reads VALUES('answer','owner','now')`)
					return err
				case "response-claim":
					_, err := db.Exec(`INSERT INTO human_attention_response_claims VALUES('ask','inbox-ask','owner','request-key','request-hash','answer','{}','now')`)
					return err
				case "answer-batch":
					_, err := db.Exec(`INSERT INTO human_attention_answer_wake_batches(target_actor_id,target_handle,workspace_id,batch_id,first_answered_at,last_answered_at,thread_id,trigger_event_id,trigger_created_at,answer_count,ask_event_ids_json,answer_event_ids_json,refs_json,updated_at) VALUES('owner','owner','ws','batch','now','now',?,'answer','now',1,'["ask"]','["answer"]','[]','now')`, threadID)
					return err
				case "refresh":
					_, err := db.Exec(`INSERT INTO topic_projection_refresh_status(thread_id,desired_generation,updated_at) VALUES(?,1,'now') ON CONFLICT(thread_id) DO UPDATE SET desired_generation=desired_generation+1`, threadID)
					return err
				case "directory-create":
					_, err := db.Exec(`INSERT INTO scope_domains VALUES('newly-eligible','active',1)`)
					return err
				default:
					_, err := db.Exec(`INSERT INTO scope_memberships VALUES('reader','newly-eligible','reader',1)`)
					return err
				}
			})
		})
	}
}

func TestScopeInboxInvalidationCanonicalLedgerEnrichment(t *testing.T) {
	db, _ := inboxInvalidationFixture(t, true)
	for _, q := range []string{
		`INSERT INTO actors(id,display_name,tags_json,created_at,metadata_json) VALUES('ledger-actor','Actor','[]','now','{}')`,
		`INSERT INTO agents(id,actor_id,username,created_at,updated_at,metadata_json) VALUES('ledger-agent','ledger-actor','ledger.agent','now','now','{}')`,
		`INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at) VALUES('ledger-host','ledger-host','Host','user','host','[]','now')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	// These dependencies do not appear in OwnershipSources: a report pin and
	// changes to legacy principal/notification identity still revoke all receipts.
	for _, source := range []struct {
		name   string
		writes []string
	}{
		{"report-pin", []string{
			`INSERT OR REPLACE INTO workspace_dashboard VALUES(1,NULL,'now','ledger-actor')`,
			`UPDATE workspace_dashboard SET updated_at='later' WHERE singleton=1`,
			`DELETE FROM workspace_dashboard WHERE singleton=1`,
		}},
		{"principal-kind", []string{
			`INSERT INTO passkey_credentials(credential_id,agent_id,user_handle,public_key,attestation_type,created_at) VALUES('ledger-key','ledger-agent',X'01',X'01','none','now')`,
			`UPDATE passkey_credentials SET agent_id=agent_id WHERE credential_id='ledger-key'`,
			`DELETE FROM passkey_credentials WHERE credential_id='ledger-key'`,
		}},
		{"host-binding", []string{
			`INSERT INTO host_agents VALUES('ledger-host','agent','ledger-agent','agent')`,
			`UPDATE host_agents SET identity_kind=identity_kind WHERE host_id='ledger-host'`,
			`DELETE FROM host_agents WHERE host_id='ledger-host'`,
		}},
	} {
		t.Run(source.name, func(t *testing.T) {
			for _, q := range source.writes {
				inboxInvalidationAssertAdvance(t, db, func() error { _, err := db.Exec(q); return err })
			}
		})
	}
}

func TestScopeInboxInvalidationPMOrderAndChanges(t *testing.T) {
	db, _ := inboxInvalidationFixture(t, false)
	if _, err := ReadScopeInboxSourceSnapshot(context.Background(), db); !errors.Is(err, ErrScopeInboxInvalidationIncomplete) {
		t.Fatal("absent PM certified", err)
	}
	var complete bool
	if err := db.QueryRow(`SELECT installation_complete FROM scope_inbox_source_clock`).Scan(&complete); err != nil || complete {
		t.Fatal("missing PM installation marked complete", err)
	}
	inboxInvalidationInstallPM(t, db)
	// Merely creating PM after initial installation cannot create provenance.
	if _, err := ReadScopeInboxSourceSnapshot(context.Background(), db); !errors.Is(err, ErrScopeInboxInvalidationIncomplete) {
		t.Fatal("PM installation-order gap certified", err)
	}
	installed := inboxInvalidationInstall(t, db, true)
	if len(installed.MissingSources) != 0 {
		t.Fatal(installed)
	}
	for _, q := range []string{
		`INSERT INTO pm_records VALUES('decision','pm-source','ws','owner','',1,CAST('{"title":"before"}' AS BLOB))`,
		`UPDATE pm_records SET body=CAST('{"title":"after"}' AS BLOB),revision=revision+1 WHERE id='pm-source'`,
		`UPDATE pm_records SET body=body WHERE id='pm-source'`,
		`UPDATE pm_records SET actor_id='other',parent_id='new-parent' WHERE id='pm-source'`,
		`UPDATE pm_records SET body=CAST('{"title":"before"}' AS BLOB),actor_id='owner',parent_id='' WHERE id='pm-source'`,
		`INSERT OR REPLACE INTO pm_records VALUES('decision','pm-source','ws','owner','',1,CAST('{}' AS BLOB))`,
		`DELETE FROM pm_records WHERE id='pm-source'`,
	} {
		inboxInvalidationAssertAdvance(t, db, func() error { _, err := db.Exec(q); return err })
	}
}

func TestScopeInboxInvalidationRollbackAndSchemaFailure(t *testing.T) {
	for _, damage := range []string{"source-missing", "selection-missing", "source-overflow", "authority-overflow", "directory-overflow", "selection-overflow"} {
		t.Run(damage, func(t *testing.T) {
			db, _ := inboxInvalidationFixture(t, true)
			before := inboxInvalidationClocks(t, db)
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if _, err = tx.Exec(`INSERT INTO human_attention_answer_reads VALUES('prior','owner','now')`); err != nil {
				t.Fatal(err)
			}
			q := map[string]string{"source-missing": `DELETE FROM scope_inbox_source_clock`, "selection-missing": `DELETE FROM scope_feed_proof_clock`, "source-overflow": `UPDATE scope_inbox_source_clock SET source_revision=9223372036854775807`, "authority-overflow": `UPDATE scope_inbox_source_clock SET authority_revision=9223372036854775807`, "directory-overflow": `UPDATE scope_inbox_source_clock SET directory_revision=9223372036854775807`, "selection-overflow": `UPDATE scope_feed_proof_clock SET revision=9223372036854775807`}[damage]
			if _, err = tx.Exec(q); err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(`INSERT INTO human_attention_answer_reads VALUES('failing','owner','now')`); err == nil {
				t.Fatal("failed invalidation allowed write")
			}
			_ = tx.Commit() // Deliberately ignore the mutation error.
			var n int
			if err = db.QueryRow(`SELECT count(*) FROM human_attention_answer_reads`).Scan(&n); err != nil || n != 0 {
				t.Fatal("ignored invalidation error committed source", n, err)
			}
			if after := inboxInvalidationClocks(t, db); after != before {
				t.Fatal("rollback clock mismatch", before, after)
			}
		})
	}
	t.Run("removed trigger fails snapshot", func(t *testing.T) {
		db, _ := inboxInvalidationFixture(t, true)
		if _, err := db.Exec(`DROP TRIGGER scope_inbox_invalidate_cards_update`); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadScopeInboxSourceSnapshot(context.Background(), db); !errors.Is(err, ErrScopeInboxInvalidationIncomplete) {
			t.Fatal("changed schema remained admitted", err)
		}
	})
	t.Run("unsupported source schema rolls installer back", func(t *testing.T) {
		db, _ := inboxInvalidationFixture(t, true)
		if _, err := db.Exec(`ALTER TABLE cards RENAME COLUMN summary TO unsupported_summary`); err != nil {
			t.Fatal(err)
		}
		before := inboxInvalidationClocks(t, db)
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = InstallScopeInboxInvalidation(context.Background(), tx); !errors.Is(err, ErrScopeInboxInvalidationIncomplete) {
			t.Fatal("unsupported source accepted", err)
		}
		if err = tx.Commit(); !errors.Is(err, sql.ErrTxDone) {
			t.Fatal("failed installer transaction remained committable", err)
		}
		if after := inboxInvalidationClocks(t, db); !reflect.DeepEqual(before, after) {
			t.Fatal("installer failed nonatomically", before, after)
		}
	})
}
