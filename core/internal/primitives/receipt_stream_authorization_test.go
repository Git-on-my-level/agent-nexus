package primitives_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
	"agent-nexus-core/internal/testsql"
)

func TestReceiptStreamCanonicalReferenceChanges(t *testing.T) {
	for _, change := range []struct{ name, sql string }{
		{"new-event", `INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES('new-trigger','message_posted','now','owner','public','["thread:private"]','{}'); UPDATE agent_wakeups SET trigger_event_id='new-trigger' WHERE wakeup_id='wake'`},
		{"event-refs", `UPDATE events SET refs_json='["thread:private"]' WHERE id='trigger'`},
		{"event-payload", `UPDATE events SET payload_json='{"text":"private"}' WHERE id='trigger'`},
		{"event-parent", `UPDATE events SET thread_id='private' WHERE id='trigger'`},
		{"receipt-bare-ref", `UPDATE agent_wakeups SET refs_json='["private"]' WHERE wakeup_id='wake'`},
		{"receipt-bare-text", `UPDATE agent_wakeups SET trigger_text='private' WHERE wakeup_id='wake'`},
		{"receipt-prose-alias", `UPDATE agent_wakeups SET trigger_text='See **thread:private-alias**.' WHERE wakeup_id='wake'`},
		{"receipt-title", `UPDATE agent_wakeups SET thread_title='private' WHERE wakeup_id='wake'`},
		{"receipt-failure", `UPDATE agent_wakeups SET failure_reason='private' WHERE wakeup_id='wake'`},
		{"thread-body", `UPDATE threads SET body_json='{"title":"private"}' WHERE id='public'`},
		{"navigation-edge", `INSERT INTO ref_edges(source_type,source_id,target_type,target_id,edge_type,created_at) VALUES('event','trigger','thread','private','ref','now')`},
	} {
		t.Run(change.name, func(t *testing.T) {
			ctx := context.Background()
			ws, store := receiptAuthorizationFixture(t)
			scope := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "stranger"})
			cursor, err := store.ReceiptStreamCursor(scope, "public", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			page, err := store.ListReceiptStreamPage(scope, "public", cursor)
			if err != nil || len(page.Wakeups) != 1 {
				t.Fatalf("prime: %+v %v", page, err)
			}
			var before, after int64
			if err = ws.DB().QueryRow(`SELECT version FROM receipt_access_epoch`).Scan(&before); err != nil {
				t.Fatal(err)
			}
			if _, err = ws.DB().Exec(change.sql); err != nil {
				t.Fatal(err)
			}
			if err = ws.DB().QueryRow(`SELECT version FROM receipt_access_epoch`).Scan(&after); err != nil {
				t.Fatal(err)
			}
			if after <= before {
				t.Fatal("changed ownership input did not invalidate the closure")
			}
			if _, err = store.GetAgentWakeup(scope, "wake"); !errors.Is(err, primitives.ErrNotFound) {
				t.Fatalf("canonical read: %v", err)
			}
			// Re-read the same indexed position under the current permissions.
			page, err = store.ListReceiptStreamPage(scope, "public", cursor)
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Wakeups) != 0 {
				t.Fatalf("stream exposed canonical-hidden receipt: %+v", page.Wakeups)
			}
		})
	}
}

func TestReceiptStreamNewLeavesUseCanonicalReferenceIndexes(t *testing.T) {
	ctx := context.Background()
	_, store := receiptAuthorizationFixture(t)
	scope := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "stranger"})
	cursor, err := store.ReceiptStreamCursor(scope, "public", "receipt:unknown@digest", nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, ref := range []string{"private", "thread:private", "thread:private-alias", "See **thread:private-alias**."} {
		wake := receiptWakeup(fmt.Sprintf("leaf-%d", i), "public", "2026-01-01T00:00:02Z")
		wake.TriggerText = ref
		if _, err = store.UpsertAgentWakeup(ctx, wake); err != nil {
			t.Fatal(err)
		}
		if _, err = store.GetAgentWakeup(scope, wake.WakeupID); !errors.Is(err, primitives.ErrNotFound) {
			t.Fatalf("canonical %q: %v", ref, err)
		}
	}
	page, err := store.ListReceiptStreamPage(scope, "public", cursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Wakeups) != 0 {
		t.Fatalf("new restricted leaves escaped: %+v", page.Wakeups)
	}
}

func TestReceiptStreamCrossReceiptReferencesInvalidateBase(t *testing.T) {
	ctx := context.Background()
	ws, store := receiptAuthorizationFixture(t)
	scope := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "stranger"})
	cursor, err := store.ReceiptStreamCursor(scope, "public", "receipt:unknown@digest", nil)
	if err != nil {
		t.Fatal(err)
	}
	secret := receiptWakeup("secret-leaf", "public", "2026-01-02T00:00:01Z")
	secret.TriggerText = "private"
	if _, err = store.UpsertAgentWakeup(ctx, secret); err != nil {
		t.Fatal(err)
	}
	dependent := receiptWakeup("dependent", "public", "2026-01-02T00:00:02Z")
	dependent.TriggerText = "secret-leaf"
	if _, err = store.UpsertAgentWakeup(ctx, dependent); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`UPDATE events SET payload_json='{"text":"See **wakeup:late-leaf**."}' WHERE id='trigger'`); err != nil {
		t.Fatal(err)
	}
	// Prime before a referenced identity arrives, then ensure its creation
	// invalidates the closure even for materialized prose references.
	if _, err = store.ListReceiptStreamPage(scope, "public", cursor); err != nil {
		t.Fatal(err)
	}
	late := receiptWakeup("late-leaf", "public", "2026-01-02T00:00:03Z")
	late.TriggerText = "private"
	if _, err = store.UpsertAgentWakeup(ctx, late); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListReceiptStreamPage(scope, "public", cursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Wakeups) != 0 {
		t.Fatalf("transitive receipt references leaked: %+v", page.Wakeups)
	}
	for _, id := range []string{"wake", "dependent", "late-leaf"} {
		if _, err = store.GetAgentWakeup(scope, id); !errors.Is(err, primitives.ErrNotFound) {
			t.Fatalf("canonical %s: %v", id, err)
		}
	}
}

func TestReceiptStreamLateWakeupNavigationReference(t *testing.T) {
	ctx := context.Background()
	ws, store := receiptAuthorizationFixture(t)
	if _, err := ws.DB().Exec(`INSERT INTO ref_edges(id,source_type,source_id,target_type,target_id,edge_type,created_at) VALUES('late-link','event','trigger','wakeup','late','ref','now')`); err != nil {
		t.Fatal(err)
	}
	scope := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "stranger"})
	cursor, err := store.ReceiptStreamCursor(scope, "public", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if page, err := store.ListReceiptStreamPage(scope, "public", cursor); err != nil || len(page.Wakeups) != 1 {
		t.Fatalf("prime: %+v %v", page, err)
	}
	late := receiptWakeup("late", "public", "2026-01-02T00:00:01Z")
	late.TriggerText = "private"
	if _, err = store.UpsertAgentWakeup(ctx, late); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListReceiptStreamPage(scope, "public", cursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Wakeups) != 0 {
		t.Fatalf("late navigation target leaked: %+v", page.Wakeups)
	}
	if _, err = store.GetAgentWakeup(scope, "wake"); !errors.Is(err, primitives.ErrNotFound) {
		t.Fatalf("canonical read: %v", err)
	}
}

func TestReceiptStreamPayloadChecksConcurrentTriggerTrash(t *testing.T) {
	ctx := context.Background()
	ws, _ := receiptAuthorizationFixture(t)
	db, counter := testsql.Open("file:" + ws.Layout().DatabasePath)
	defer db.Close()
	store := primitives.NewTestStore(db, "")
	scope := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "stranger"})
	cursor, err := store.ReceiptStreamCursor(scope, "public", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	trashed := false
	counter.BeforeQuery(func(query string) {
		if strings.Contains(query, "CROSS JOIN agent_wakeups ON") {
			once.Do(func() {
				_, err := ws.DB().Exec(`UPDATE events SET trashed_at='now' WHERE id='trigger'`)
				if err != nil {
					t.Fatal(err)
				}
				trashed = true
			})
		}
	})
	page, err := store.ListReceiptStreamPage(scope, "public", cursor)
	if err != nil {
		t.Fatal(err)
	}
	if !trashed || len(page.Wakeups) != 0 {
		t.Fatalf("concurrent trash escaped lifecycle filter: trashed=%v receipts=%+v", trashed, page.Wakeups)
	}
}

func TestReceiptStreamPayloadValidatesEpochDuringRevocation(t *testing.T) {
	ctx := context.Background()
	ws, _ := receiptAuthorizationFixture(t)
	db, counter := testsql.Open("file:" + ws.Layout().DatabasePath)
	defer db.Close()
	store := primitives.NewTestStore(db, "")
	scope := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "stranger"})
	cursor, err := store.ReceiptStreamCursor(scope, "public", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	revoked := false
	counter.BeforeQuery(func(query string) {
		if !strings.Contains(query, "JOIN agent_wakeups ON agent_wakeups.wakeup_id=wanted.value") {
			return
		}
		once.Do(func() {
			if _, err := ws.DB().Exec(`UPDATE threads SET body_json='{"pm_actor_id":"owner"}' WHERE id='public'`); err != nil {
				t.Fatal(err)
			}
			revoked = true
		})
	})
	page, err := store.ListReceiptStreamPage(scope, "public", cursor)
	if err != nil {
		t.Fatal(err)
	}
	if !revoked {
		t.Fatal("did not intercept the final real SQLite payload read")
	}
	if len(page.Wakeups) != 0 {
		t.Fatalf("revoked payload was returned: %+v", page.Wakeups)
	}
	if _, err = store.GetAgentWakeup(scope, "wake"); !errors.Is(err, primitives.ErrNotFound) {
		t.Fatalf("canonical read after revocation: %v", err)
	}
}

func TestReceiptStreamReplayRetainsProgressAndPrioritizesAppends(t *testing.T) {
	ctx := context.Background()
	ws, store := receiptAuthorizationFixture(t)
	if _, err := ws.DB().Exec(`WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<2401)
	 INSERT INTO agent_wakeups(wakeup_id,status,notification_status,target_handle,target_actor_id,thread_id,refs_json,created_at,updated_at)
	 SELECT printf('history-%06d',i),'requested','unread','agent','target','public','[]','2026-01-01T00:00:01Z','now' FROM n`); err != nil {
		t.Fatal(err)
	}
	scope := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "stranger"})
	cursor, err := store.ReceiptStreamCursor(scope, "public", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for {
		page, err := store.ListReceiptStreamPage(scope, "public", cursor)
		if err != nil {
			t.Fatal(err)
		}
		cursor = page.Cursor
		if !page.HasMore {
			break
		}
	}
	last := ""
	for i := 0; i < 10; i++ {
		// This event is private and has no relation to the selected thread.
		if _, err = ws.DB().Exec(`UPDATE events SET trashed_at=? WHERE id='unrelated'`, fmt.Sprintf("trash-%d", i)); err != nil {
			t.Fatal(err)
		}
		page, err := store.ListReceiptStreamPage(scope, "public", cursor)
		if err != nil {
			t.Fatal(err)
		}
		if page.Cursor.WakeupID <= last {
			t.Fatalf("replay restarted: previous=%q current=%q", last, page.Cursor.WakeupID)
		}
		last = page.Cursor.WakeupID
		cursor = page.Cursor
		probe := receiptWakeup(fmt.Sprintf("probe-%d", i), "public", "2026-02-01T00:00:01Z")
		if _, err = store.UpsertAgentWakeup(ctx, probe); err != nil {
			t.Fatal(err)
		}
		page, err = store.ListReceiptStreamPage(scope, "public", cursor)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Wakeups) != 1 || page.Wakeups[0].WakeupID != probe.WakeupID {
			t.Fatalf("append was starved by replay: %+v", page.Wakeups)
		}
		if page.Cursor.WakeupID != last {
			t.Fatal("tail delivery discarded replay progress")
		}
		cursor = page.Cursor
	}
}

func receiptAuthorizationFixture(t *testing.T) (*storage.Workspace, *primitives.Store) {
	t.Helper()
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	for _, q := range []string{
		`INSERT INTO threads(id,handle,updated_at,updated_by,body_json) VALUES('public','public','now','owner','{}'),('private','private-alias','now','owner','{"pm_actor_id":"owner"}')`,
		`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES('trigger','message_posted','now','owner','public','[]','{}'),('unrelated','message_posted','now','owner','private','[]','{}')`,
	} {
		if _, err = ws.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	store := primitives.NewTestStore(ws.DB(), "")
	wake := receiptWakeup("wake", "public", "2026-01-01T00:00:01Z")
	wake.TriggerEventID = "trigger"
	if _, err = store.UpsertAgentWakeup(ctx, wake); err != nil {
		t.Fatal(err)
	}
	return ws, store
}
