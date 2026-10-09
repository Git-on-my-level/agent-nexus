package primitives_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/pm"
	p "agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/resourceaccess"
)

func ledgerFixture(t *testing.T) (*sql.DB, *p.Store) {
	t.Helper()
	ws, err := initializeTestWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	if _, err := pm.NewStore(ws.DB()); err != nil {
		t.Fatal(err)
	}
	return ws.DB(), p.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
}

func installLedgerFixture(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(p.ScopeInboxEpochGuardProposal); err != nil {
		t.Fatal(err)
	}
	for _, proposal := range p.ScopeInboxInvalidationProposal() {
		if _, err := db.Exec(proposal.SQL); err != nil {
			t.Fatalf("%s/%s: %v", proposal.Source.Table, proposal.Operation, err)
		}
	}
}

func ledgerEpoch(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var result int64
	if err := db.QueryRow(`SELECT version FROM resource_access_epoch WHERE singleton=1`).Scan(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestScopeInboxMutationLedgerCoverageAndDetachedTemplates(t *testing.T) {
	t.Parallel()
	ledger := p.ScopeInboxMutationLedger()
	tables := map[string]bool{}
	for _, source := range ledger {
		if tables[source.Table] || source.Dependency == "" {
			t.Fatalf("bad dependency: %+v", source)
		}
		tables[source.Table] = true
	}
	for _, source := range append(append([]resourceaccess.OwnershipSource{}, resourceaccess.OwnershipSources...), resourceaccess.FilterOwnershipSources()...) {
		if !tables[source.Table] {
			t.Fatalf("legacy source missing from mutation ledger: %s", source.Table)
		}
	}
	for _, name := range []string{"human_attention_answer_reads", "human_attention_request_resolutions", "access_requests", "workspace_dashboard", "topic_projection_refresh_status", "pm_records"} {
		if !tables[name] {
			t.Fatalf("missing enriched/canonical source %s", name)
		}
	}
	ledger[0].Table = "caller_modified"
	proposals := p.ScopeInboxInvalidationProposal()
	if len(proposals) != 3*len(ledger) {
		t.Fatal("incomplete operation coverage")
	}
	proposals[0].SQL = "caller_modified"
	if strings.Contains(p.ScopeInboxInvalidationProposal()[0].SQL, "caller_modified") {
		t.Fatal("mutable schema templates")
	}
	db, _ := ledgerFixture(t)
	installLedgerFixture(t, db)
	for table := range tables {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type='trigger' AND tbl_name=? AND name LIKE 'scope_inbox_invalidate_%'`, table).Scan(&n); err != nil || n != 3 {
			t.Fatalf("%s: n=%d err=%v", table, n, err)
		}
	}
}

// Actual legacy APIs retain their valid write behavior. Invalidation is in the
// source transaction, including state previously outside the ownership epoch.
// This does not project or certify these sources; A's trusted verifier remains
// required before a generation is admitted.
func TestScopeInboxCanonicalWritesInvalidateBeforeCommit(t *testing.T) {
	t.Parallel()
	db, store := ledgerFixture(t)
	installLedgerFixture(t, db)
	ctx := context.Background()
	advance := func(name string, mutate func() error) {
		t.Helper()
		before := ledgerEpoch(t, db)
		if err := mutate(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if after := ledgerEpoch(t, db); after <= before {
			t.Fatalf("%s did not invalidate: %d -> %d", name, before, after)
		}
	}
	var threadID string
	advance("newly eligible discovery", func() error {
		thread, err := store.CreateThread(ctx, "requester", map[string]any{"title": "Canonical inbox"})
		if err == nil {
			threadID = fmt.Sprint(thread.Thread["id"])
		}
		return err
	})
	item := p.DerivedInboxItem{ID: "ledger-item", ThreadID: threadID, Category: "ask", TriggerAt: "2026-10-07T00:00:00Z", Data: map[string]any{"kind": "ask", "subject_ref": "thread:" + threadID}}
	advance("bulk regeneration", func() error { return store.ReplaceDerivedInboxItems(ctx, threadID, []p.DerivedInboxItem{item}) })
	// A previously valid oversized legacy row must invalidate, not be forced
	// through the <=16KiB certified payload codec and refused.
	item.Data["body"] = strings.Repeat("x", p.MaxScopeInboxPayloadBytes+1)
	advance("unsupported projection size", func() error { return store.ReplaceDerivedInboxItems(ctx, threadID, []p.DerivedInboxItem{item}) })
	var size int
	if err := db.QueryRow(`SELECT length(json_extract(data_json,'$.body')) FROM derived_inbox_items WHERE id='ledger-item'`).Scan(&size); err != nil || size <= p.MaxScopeInboxPayloadBytes {
		t.Fatal(size, err)
	}
	advance("freshness requeue", func() error { return store.RequeueTopicProjectionRefresh(ctx, threadID, time.Now().UTC()) })
	var generation int64
	advance("freshness start", func() error {
		var err error
		generation, err = store.MarkTopicProjectionRefreshStarted(ctx, threadID, time.Now().UTC())
		return err
	})
	advance("freshness error", func() error {
		return store.MarkTopicProjectionRefreshFailed(ctx, threadID, generation, time.Now().UTC(), "fixture error")
	})
	var askID, answerID string
	advance("ask creation", func() error {
		event, err := store.AppendEvent(ctx, "requester", map[string]any{"type": "human_attention_requested", "thread_id": threadID, "refs": []string{"thread:" + threadID}, "payload": map[string]any{"kind": "ask", "title": "Choose", "requester_actor_id": "requester", "subject_ref": "thread:" + threadID, "response_proposals": []string{"Proceed"}}})
		if err == nil {
			askID = fmt.Sprint(event["id"])
		}
		return err
	})
	advance("answer and resolution", func() error {
		result, _, err := store.AppendHumanAttentionResponse(ctx, "responder", askID, "inbox:"+askID, "", "", map[string]any{"type": "human_attention_responded", "thread_id": threadID, "refs": []string{"event:" + askID, "thread:" + threadID}, "payload": map[string]any{"request_event_ref": "event:" + askID, "requester_actor_id": "requester", "responding_actor_id": "responder", "outcome": "answered", "response_text": "Proceed", "subject_ref": "thread:" + threadID}}, nil)
		if err == nil {
			answerID = fmt.Sprint(result["event"].(map[string]any)["id"])
		}
		return err
	})
	advance("answer read", func() error { _, err := store.MarkHumanAttentionAnswerRead(ctx, "requester", answerID); return err })
	advance("imported reopen", func() error {
		_, err := db.Exec(`DELETE FROM human_attention_request_resolutions WHERE request_event_id=?`, askID)
		return err
	})
	advance("PM imported source", func() error {
		_, err := db.Exec(`INSERT INTO pm_records(kind,id,workspace_id,actor_id,revision,body) VALUES('goal','ledger-pm','workspace','requester',1,'{"title":"PM source"}')`)
		return err
	})
	advance("PM ownership change", func() error {
		_, err := db.Exec(`UPDATE pm_records SET actor_id='other' WHERE id='ledger-pm'`)
		return err
	})
	advance("PM source deletion", func() error { _, err := db.Exec(`DELETE FROM pm_records WHERE id='ledger-pm'`); return err })
	advance("report pin", func() error {
		_, err := db.Exec(`INSERT INTO workspace_dashboard VALUES(1,NULL,'now','requester') ON CONFLICT(singleton) DO UPDATE SET updated_at='later'`)
		return err
	})
	var documentID, revisionID string
	advance("report source", func() error {
		document, revision, err := store.CreateDocument(ctx, "requester", map[string]any{"title": "Report", "thread_id": threadID}, "Report body", "text", nil)
		if err == nil {
			documentID = fmt.Sprint(document["id"])
			revisionID = fmt.Sprint(revision["revision_id"])
		}
		return err
	})
	advance("pin current report", func() error {
		_, err := db.Exec(`UPDATE workspace_dashboard SET document_id=? WHERE singleton=1`, documentID)
		return err
	})
	advance("report review reminder", func() error {
		return store.AddReportReviewReminder(ctx, documentID, revisionID, "panel", "Review report", "requester", "2026-10-07T00:00:00Z")
	})
	advance("report metadata revision", func() error {
		_, _, err := store.PatchDocument(ctx, "requester", documentID, map[string]any{"title": "Changed report"}, nil)
		return err
	})
	advance("owner and role authority", func() error {
		_, err := store.PatchThread(ctx, "requester", threadID, map[string]any{"pm_actor_id": "other"}, nil)
		return err
	})
	advance("parent lifecycle", func() error { _, err := store.ArchiveThread(ctx, "requester", threadID); return err })
	advance("canonical deletion", func() error { return store.ReplaceDerivedInboxItems(ctx, threadID, nil) })
}

func TestScopeInboxInvalidationFailureAndStickyTransactionGate(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"missing", "overflow", "statement failure"} {
		t.Run(fault, func(t *testing.T) {
			db, store := ledgerFixture(t)
			ctx := context.Background()
			thread, err := store.CreateThread(ctx, "owner", map[string]any{"title": "Rollback"})
			if err != nil {
				t.Fatal(err)
			}
			tid := fmt.Sprint(thread.Thread["id"])
			item := p.DerivedInboxItem{ID: "unchanged", ThreadID: tid, Category: "ask", TriggerAt: "now", Data: map[string]any{"kind": "ask"}}
			if err := store.ReplaceDerivedInboxItems(ctx, tid, []p.DerivedInboxItem{item}); err != nil {
				t.Fatal(err)
			}
			installLedgerFixture(t, db)
			if _, err := db.Exec(`CREATE TABLE prior_source_write(value TEXT)`); err != nil {
				t.Fatal(err)
			}
			switch fault {
			case "missing":
				_, err = db.Exec(`DELETE FROM resource_access_epoch`)
			case "overflow":
				_, err = db.Exec(`UPDATE resource_access_epoch SET version=9223372036854775807`)
			case "statement failure":
				_, err = db.Exec(`CREATE TRIGGER fail_invalidation BEFORE UPDATE ON resource_access_epoch BEGIN SELECT RAISE(ABORT,'fixture failure'); END`)
			}
			if err != nil {
				t.Fatal(err)
			}
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(`INSERT INTO prior_source_write VALUES('must rollback')`); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(`UPDATE derived_inbox_items SET category='review' WHERE id='unchanged'`); err == nil {
				t.Fatal("ignored invalidation failure")
			}
			commitErr := tx.Commit()
			if fault != "statement failure" && commitErr == nil {
				t.Fatal("ignored invalid clock committed source transaction")
			}
			if fault == "statement failure" && commitErr != nil {
				t.Fatal("fixture no longer demonstrates the sticky-transaction gate", commitErr)
			}
			var category string
			var n int
			if err := db.QueryRow(`SELECT category FROM derived_inbox_items WHERE id='unchanged'`).Scan(&category); err != nil || category != "ask" {
				t.Fatal(category, err)
			}
			want := 0
			if fault == "statement failure" {
				// SQL aborts the source statement atomically, but cannot turn every
				// storage error into whole-transaction rollback. A must propagate
				// and stick errors; this proposal alone is not that authority.
				want = 1
			}
			if err := db.QueryRow(`SELECT count(*) FROM prior_source_write`).Scan(&n); err != nil || n != want {
				t.Fatal(n, err)
			}
		})
	}
}

func TestScopeInboxPasskeyPrincipalFallbackInvalidatesNotificationTarget(t *testing.T) {
	t.Parallel()
	db, _ := ledgerFixture(t)
	ctx := context.Background()
	if _, err := db.Exec(`INSERT INTO actors(id,display_name,tags_json,created_at,metadata_json) VALUES('legacy-actor','Legacy','[]','now','{}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO agents(id,actor_id,username,created_at,updated_at,metadata_json) VALUES('legacy-agent','legacy-actor','legacy.agent','now','now','{}')`); err != nil {
		t.Fatal(err)
	}
	store := auth.NewStore(db)
	installLedgerFixture(t, db)
	assertTarget := func(want bool) {
		t.Helper()
		targets, err := store.NotificationTargets(ctx, []string{"legacy-actor"}, []string{"legacy-agent"})
		if err != nil {
			t.Fatal(err)
		}
		_, actorFound := targets["actor:legacy-actor"]
		_, agentFound := targets["agent:legacy-agent"]
		if actorFound != want || agentFound != want {
			t.Fatal("wrong legacy principal fallback", targets)
		}
	}
	assertTarget(true)
	before := ledgerEpoch(t, db)
	if _, err := db.Exec(`INSERT INTO passkey_credentials(credential_id,agent_id,user_handle,public_key,attestation_type,created_at) VALUES('credential','legacy-agent',X'01',X'01','none','now')`); err != nil {
		t.Fatal(err)
	}
	if ledgerEpoch(t, db) <= before {
		t.Fatal("passkey insertion did not invalidate target authority")
	}
	assertTarget(false)
	before = ledgerEpoch(t, db)
	if _, err := db.Exec(`DELETE FROM passkey_credentials WHERE credential_id='credential'`); err != nil {
		t.Fatal(err)
	}
	if ledgerEpoch(t, db) <= before {
		t.Fatal("passkey deletion did not invalidate target authority")
	}
	assertTarget(true)
}
