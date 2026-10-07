package scopedrepo_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/scopedrepo"
	"agent-nexus-core/internal/scopes"
	"agent-nexus-core/internal/storage"
)

func inboxVerificationFixture(t *testing.T, n int, install bool) (*sql.DB, *scopedrepo.Store, string) {
	t.Helper()
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	must(t, err)
	t.Cleanup(func() { ws.Close() })
	db := ws.DB()
	_, err = pm.NewStore(db)
	must(t, err)
	s := scopedrepo.New(db)
	for _, init := range []func(context.Context) error{s.Initialize, s.InitializeFeedSchema, s.InitializeInboxOrderSchema, s.InitializeInboxVerificationSchema} {
		must(t, init(ctx))
	}
	p := primitives.NewTestStore(db, ws.Layout().ArtifactContentDir)
	thread, err := p.CreateThread(ctx, "owner", map[string]any{"title": "public inbox"})
	must(t, err)
	threadID := thread.Thread["id"].(string)
	must(t, exec(db, `INSERT INTO scope_domains VALUES('public','active',1); INSERT INTO scope_memberships VALUES('owner','public','owner',1); INSERT INTO scope_feed_bindings VALUES('owner','public',1,'inbox','owner',1,1)`))
	for i := 0; i < n; i++ {
		inboxVerificationInsert(t, db, threadID, fmt.Sprintf("row-%04d", i), true, i)
	}
	// Real canonical rows with an unrelated recipient have no scoped registration.
	// The worker must enumerate them and derive their exclusion from old policy.
	inboxVerificationInsert(t, db, threadID, "recipient-hidden", false, 0)
	private, err := p.CreateThread(ctx, "another", map[string]any{"title": "private inbox", "pm_actor_id": "another"})
	must(t, err)
	inboxVerificationInsert(t, db, private.Thread["id"].(string), "ownership-hidden", false, 1)
	must(t, exec(db, `UPDATE derived_inbox_items SET data_json=json_set(data_json,'$.recipient_actor_id','') WHERE id='ownership-hidden'`))
	if install {
		inboxVerificationInstall(t, db)
	}
	return db, s, threadID
}

func inboxVerificationInstall(t *testing.T, db *sql.DB) {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	must(t, err)
	defer tx.Rollback()
	result, err := primitives.InstallScopeInboxInvalidation(context.Background(), tx)
	must(t, err)
	if !result.Complete {
		t.Fatalf("unexpected incomplete source installation: %v", result.MissingSources)
	}
	must(t, tx.Commit())
}

func inboxVerificationInsert(t *testing.T, db *sql.DB, thread, id string, visible bool, i int) {
	t.Helper()
	category := []string{"ask", "review", "escalate"}[i%3]
	data := map[string]any{"kind": category, "title": "canonical payload", "subject_ref": "thread:" + thread, "precise": json.Number("9007199254740993")}
	if !visible {
		data["recipient_actor_id"] = "someone-else"
	}
	item := primitives.DerivedInboxItem{ID: id, ThreadID: thread, Category: category, TriggerAt: fmt.Sprintf("2026-10-07T10:00:00.%03dZ", i%2), GeneratedAt: "2026-10-07T10:00:00Z", Data: data}
	raw, err := json.Marshal(data)
	must(t, err)
	must(t, exec(db, `INSERT INTO derived_inbox_items(id,thread_id,category,trigger_at,generated_at,data_json) VALUES(?,?,?,?,?,?)`, id, thread, category, item.TriggerAt, item.GeneratedAt, string(raw)))
	if !visible {
		return
	}
	must(t, exec(db, `INSERT INTO scope_resources VALUES('public','inbox',?,?,1)`, "opaque-"+id, id))
	var rid int64
	must(t, db.QueryRow(`SELECT rid FROM scope_resource_rids WHERE scope_id='public' AND kind='inbox' AND resource_id=?`, "opaque-"+id).Scan(&rid))
	identity := scopes.ResourceIdentity{ScopeID: "public", Kind: "inbox", ResourceID: "opaque-" + id, CanonicalID: id, RID: rid, CanonicalVersion: 1}
	payload, err := primitives.EncodeScopeInbox(identity, item)
	must(t, err)
	key, err := primitives.ScopeInboxSortKey(item)
	must(t, err)
	must(t, exec(db, `INSERT INTO scope_inbox_order VALUES('public',1,'inbox','owner',?,?,1)`, key, rid))
	must(t, exec(db, `INSERT INTO scope_feed_payloads VALUES('public',1,'inbox','owner',?,1,?)`, rid, string(payload)))
	for _, bucket := range []string{"total", category} {
		must(t, exec(db, `INSERT INTO scope_counters VALUES('public',1,'inbox','owner',?,1) ON CONFLICT(scope_id,generation,family,audience_key,bucket) DO UPDATE SET value=value+1`, bucket))
	}
}

func finishInboxVerification(t *testing.T, s *scopedrepo.Store, id string) (scopedrepo.InboxVerificationProgress, error) {
	t.Helper()
	var result scopedrepo.InboxVerificationProgress
	for i := 0; i < 1000; i++ {
		var err error
		result, err = s.RunInboxVerificationSlice(context.Background(), id)
		if err != nil {
			return result, err
		}
		if result.Examined > 64 {
			t.Fatalf("slice exceeded bound: %d", result.Examined)
		}
		if result.ServingProof {
			t.Fatal("base comparison minted serving proof")
		}
		if result.Complete {
			return result, nil
		}
	}
	t.Fatal("worker failed to terminate")
	return result, nil
}

func TestInboxVerificationCanonicalResumeAndScale(t *testing.T) {
	for _, n := range []int{16, 160} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			db, s, _ := inboxVerificationFixture(t, n, true)
			id, err := s.StartInboxVerification(context.Background(), "owner", "")
			must(t, err)
			first, err := s.RunInboxVerificationSlice(context.Background(), id)
			must(t, err)
			if first.Complete {
				t.Fatal("premature completion")
			}
			var before string
			must(t, db.QueryRow(`SELECT checkpoint FROM scope_inbox_verification_jobs WHERE id=?`, id).Scan(&before))
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err = s.RunInboxVerificationSlice(ctx, id)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel result: %v", err)
			}
			var after string
			must(t, db.QueryRow(`SELECT checkpoint FROM scope_inbox_verification_jobs WHERE id=?`, id).Scan(&after))
			if before != after {
				t.Fatal("cancelled slice advanced checkpoint")
			}
			// No process-local cursor or success callback is needed to resume.
			result, err := finishInboxVerification(t, scopedrepo.New(db), id)
			must(t, err)
			if result.Eligible != int64(n) {
				t.Fatalf("eligible=%d want %d", result.Eligible, n)
			}
			var canonical, eligible, serving int
			var kind string
			must(t, db.QueryRow(`SELECT canonical_rows,eligible_rows,comparison_kind,serving_eligible FROM scope_inbox_comparison_receipts WHERE job_id=?`, id).Scan(&canonical, &eligible, &kind, &serving))
			if canonical != n+2 || eligible != n || kind != "canonical_base_v1" || serving != 0 {
				t.Fatalf("bad receipt: %d %d %s %d", canonical, eligible, kind, serving)
			}
			var proofs int
			must(t, db.QueryRow(`SELECT count(*) FROM scope_feed_selection_proofs`).Scan(&proofs))
			if proofs != 0 {
				t.Fatal("serving proof minted")
			}
			var coverage sql.NullInt64
			must(t, db.QueryRow(`SELECT directory_coverage_revision FROM scope_inbox_source_clock`).Scan(&coverage))
			if coverage.Valid {
				t.Fatal("base receipt asserted serving directory coverage")
			}
			again, err := s.RunInboxVerificationSlice(context.Background(), id)
			must(t, err)
			if !again.Complete || again.Examined != 0 {
				t.Fatal("completed worker replayed work")
			}
		})
	}
}

func TestInboxVerificationRejectsWholeGenerationMismatch(t *testing.T) {
	cases := map[string]string{
		"missing payload":           `DELETE FROM scope_feed_payloads WHERE rid=(SELECT MIN(rid) FROM scope_inbox_order)`,
		"wrong payload":             `UPDATE scope_feed_payloads SET data=json_set(data,'$.item.Data.title','changed') WHERE rid=(SELECT MIN(rid) FROM scope_inbox_order)`,
		"wrong order":               `UPDATE scope_inbox_order SET order_key=X'00' WHERE rid=(SELECT MIN(rid) FROM scope_inbox_order)`,
		"text comparator":           `PRAGMA ignore_check_constraints=ON; UPDATE scope_inbox_order SET order_key=CAST(order_key AS TEXT) WHERE rid=(SELECT MIN(rid) FROM scope_inbox_order)`,
		"blob payload":              `UPDATE scope_feed_payloads SET data=CAST(data AS BLOB) WHERE rid=(SELECT MIN(rid) FROM scope_inbox_order)`,
		"wrong version":             `UPDATE scope_inbox_order SET version=2 WHERE rid=(SELECT MIN(rid) FROM scope_inbox_order)`,
		"omitted binding":           `DELETE FROM scope_feed_bindings`,
		"omitted visible canonical": `INSERT INTO derived_inbox_items(id,thread_id,category,trigger_at,generated_at,data_json) SELECT 'new-unregistered',thread_id,category,trigger_at,generated_at,data_json FROM derived_inbox_items WHERE id='row-0000'`,
		"wrong counts":              `UPDATE scope_counters SET value=value+1 WHERE bucket='total'`,
		"missing count":             `DELETE FROM scope_counters WHERE bucket='total'`,
		"unsupported bucket":        `INSERT INTO scope_counters VALUES('public',1,'inbox','owner','unsupported',0)`,
		"empty counter bucket":      `INSERT INTO scope_counters VALUES('public',1,'inbox','owner','',0)`,
		"blob counter bucket":       `UPDATE scope_counters SET bucket=CAST(bucket AS BLOB) WHERE bucket='total'`,
		"empty directory scope":     `INSERT INTO scope_domains VALUES('','active',1); INSERT INTO scope_memberships VALUES('owner','','owner',1)`,
		"extra payload version":     `INSERT INTO scope_feed_payloads SELECT scope_id,generation,family,audience_key,rid,2,data FROM scope_feed_payloads WHERE rid=(SELECT MIN(rid) FROM scope_inbox_order)`,
		"duplicate cross stream":    `INSERT INTO scope_feed_bindings VALUES('owner','public',1,'inbox','duplicate',1,1); INSERT INTO scope_inbox_order SELECT scope_id,generation,family,'duplicate',order_key,rid,version FROM scope_inbox_order; INSERT INTO scope_feed_payloads SELECT scope_id,generation,family,'duplicate',rid,version,data FROM scope_feed_payloads`,
		"unauthorized staged row":   `UPDATE derived_inbox_items SET data_json=json_set(data_json,'$.recipient_actor_id','someone-else') WHERE id='row-0000'`,
	}
	for name, q := range cases {
		t.Run(name, func(t *testing.T) {
			db, s, _ := inboxVerificationFixture(t, 3, true)
			must(t, exec(db, q))
			id, err := s.StartInboxVerification(context.Background(), "owner", "")
			must(t, err)
			_, err = finishInboxVerification(t, s, id)
			if !errors.Is(err, scopedrepo.ErrInboxVerificationMismatch) {
				t.Fatalf("mismatch accepted: %v", err)
			}
			var n int
			must(t, db.QueryRow(`SELECT count(*) FROM scope_inbox_comparison_receipts`).Scan(&n))
			if n != 0 {
				t.Fatal("mismatch minted receipt")
			}
		})
	}
}

func TestInboxVerificationEpochChurnInvalidatesCheckpoint(t *testing.T) {
	for name, q := range map[string]string{
		"canonical":               `UPDATE derived_inbox_items SET trigger_at=trigger_at WHERE id='row-0000'`,
		"authority ABA":           `UPDATE scope_memberships SET role='reader' WHERE principal='owner'; UPDATE scope_memberships SET role='owner' WHERE principal='owner'`,
		"new scope":               `INSERT INTO scope_domains VALUES('new','active',1); INSERT INTO scope_memberships VALUES('owner','new','owner',1)`,
		"payload ABA":             `UPDATE scope_feed_payloads SET data=data WHERE rid=(SELECT MIN(rid) FROM scope_inbox_order)`,
		"late eligible canonical": `INSERT INTO derived_inbox_items(id,thread_id,category,trigger_at,generated_at,data_json) SELECT 'new-source',thread_id,category,trigger_at,generated_at,data_json FROM derived_inbox_items WHERE id='row-0000'`,
	} {
		t.Run(name, func(t *testing.T) {
			db, s, _ := inboxVerificationFixture(t, 3, true)
			id, err := s.StartInboxVerification(context.Background(), "owner", "")
			must(t, err)
			_, err = s.RunInboxVerificationSlice(context.Background(), id)
			must(t, err)
			must(t, exec(db, q))
			_, err = s.RunInboxVerificationSlice(context.Background(), id)
			if !errors.Is(err, scopedrepo.ErrInboxVerificationStale) {
				t.Fatalf("stale checkpoint admitted: %v", err)
			}
		})
	}
}

func TestInboxVerificationRefusesIncompleteInstallationAndMoreThan64Scopes(t *testing.T) {
	db, s, _ := inboxVerificationFixture(t, 0, false)
	if _, err := s.StartInboxVerification(context.Background(), "owner", ""); !errors.Is(err, scopedrepo.ErrInboxVerificationCoverage) {
		t.Fatalf("missing integration admitted: %v", err)
	}
	inboxVerificationInstall(t, db)
	for i := 0; i < 64; i++ {
		scope := fmt.Sprintf("extra-%02d", i)
		must(t, exec(db, `INSERT INTO scope_domains VALUES(?,'active',1)`, scope))
		must(t, exec(db, `INSERT INTO scope_memberships VALUES('owner',?,'owner',1)`, scope))
	}
	id, err := s.StartInboxVerification(context.Background(), "owner", "")
	must(t, err)
	_, err = finishInboxVerification(t, s, id)
	if !errors.Is(err, scopes.ErrBudget) {
		t.Fatalf("65 scopes accepted: %v", err)
	}
}

func TestInboxVerificationLifecycleBaselineAndSourceCoverage(t *testing.T) {
	db, s, thread := inboxVerificationFixture(t, 1, true)
	// Requests retain independent lifecycle on archived context.
	must(t, exec(db, `UPDATE threads SET archived_at='2026-10-07T10:00:00Z' WHERE id=?`, thread))
	id, err := s.StartInboxVerification(context.Background(), "owner", "")
	must(t, err)
	result, err := finishInboxVerification(t, s, id)
	must(t, err)
	if result.Eligible != 1 {
		t.Fatal("archived independent ask disappeared")
	}
	// A canonical newly visible notification in an undiscovered scope refuses
	// completeness even when every discovered stream itself remains unchanged.
	must(t, exec(db, `UPDATE threads SET archived_at=NULL WHERE id=?`, thread))
	must(t, exec(db, `INSERT INTO derived_inbox_items(id,thread_id,category,trigger_at,generated_at,data_json) VALUES('notification',?,'fyi','2026-10-07T10:00:00Z','2026-10-07T10:00:00Z','{"kind":"notification"}')`, thread))
	id, err = s.StartInboxVerification(context.Background(), "owner", "")
	must(t, err)
	_, err = finishInboxVerification(t, s, id)
	if !errors.Is(err, scopedrepo.ErrInboxVerificationMismatch) {
		t.Fatalf("missing notification scope accepted: %v", err)
	}
}

func TestInboxVerificationRefusesCompletedCursorWithoutReceipt(t *testing.T) {
	db, s, _ := inboxVerificationFixture(t, 1, true)
	id, err := s.StartInboxVerification(context.Background(), "owner", "")
	must(t, err)
	// Simulate damaged durable worker state. Completion must have an exact
	// internally written base receipt; a phase/cursor alone is never evidence.
	must(t, exec(db, `UPDATE scope_inbox_verification_jobs SET phase='done' WHERE id=?`, id))
	result, err := s.RunInboxVerificationSlice(context.Background(), id)
	if result.Complete || !errors.Is(err, scopedrepo.ErrInboxVerificationMismatch) {
		t.Fatalf("completed cursor trusted: %+v %v", result, err)
	}
}

func TestInboxVerificationRejectsMismatchBeyondFirstPage(t *testing.T) {
	db, s, _ := inboxVerificationFixture(t, 160, true)
	must(t, exec(db, `UPDATE scope_feed_payloads SET data=json_set(data,'$.item.Data.title','late mismatch') WHERE rid=(SELECT rid FROM scope_resource_rids WHERE kind='inbox' AND resource_id='opaque-row-0159')`))
	// Synthetic generation flags cannot substitute for complete comparison.
	must(t, exec(db, `INSERT INTO scope_feed_generations VALUES('public',1,1,1,1,1,(SELECT version FROM resource_access_epoch WHERE singleton=1))`))
	id, err := s.StartInboxVerification(context.Background(), "owner", "")
	must(t, err)
	_, err = finishInboxVerification(t, s, id)
	if !errors.Is(err, scopedrepo.ErrInboxVerificationMismatch) {
		t.Fatalf("late mismatch certified: %v", err)
	}
	var receipts int
	must(t, db.QueryRow(`SELECT count(*) FROM scope_inbox_comparison_receipts`).Scan(&receipts))
	if receipts != 0 {
		t.Fatal("partial generation emitted receipt")
	}
}
