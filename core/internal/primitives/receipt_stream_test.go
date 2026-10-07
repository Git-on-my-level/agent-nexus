package primitives_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
	"agent-nexus-core/internal/testsql"
)

func TestReceiptStreamResumeAndUpdateReplay(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	store := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	public, err := store.CreateThread(ctx, "owner", map[string]any{"title": "public receipts"})
	if err != nil {
		t.Fatal(err)
	}
	privateThread, err := store.CreateThread(ctx, "owner", map[string]any{"title": "private receipts"})
	if err != nil {
		t.Fatal(err)
	}
	threadID := anyString(public.Thread["id"])
	privateID := anyString(privateThread.Thread["id"])
	if _, err = store.PatchThread(ctx, "owner", privateID, map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	privateEvent, err := store.AppendEvent(ctx, "owner", map[string]any{
		"type": "message_posted", "thread_id": privateID, "refs": []string{"thread:" + privateID},
		"payload": map[string]any{"text": "private"},
	})
	if err != nil {
		t.Fatal(err)
	}
	trashedEvent, err := store.AppendEvent(ctx, "owner", map[string]any{
		"type": "message_posted", "thread_id": threadID, "refs": []string{"thread:" + threadID},
		"payload": map[string]any{"text": "trashed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.TrashEvent(ctx, "owner", anyString(trashedEvent["id"]), "gone"); err != nil {
		t.Fatal(err)
	}
	first := receiptWakeup("wake-first", threadID, "2026-01-01T00:00:01Z")
	second := receiptWakeup("wake-second", threadID, "2026-01-01T00:00:02Z")
	hidden := receiptWakeup("wake-hidden", threadID, "2026-01-01T00:00:00Z")
	hidden.TriggerEventID = anyString(privateEvent["id"])
	trashed := receiptWakeup("wake-trashed", threadID, "2026-01-01T00:00:03Z")
	trashed.TriggerEventID = anyString(trashedEvent["id"])
	for _, wakeup := range []primitives.AgentWakeup{hidden, first, second, trashed} {
		if _, err = store.UpsertAgentWakeup(ctx, wakeup); err != nil {
			t.Fatal(err)
		}
	}
	scope := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "stranger"})
	accept := func(wakeup primitives.AgentWakeup) bool {
		return wakeup.WakeupID == first.WakeupID && wakeup.Status == primitives.AgentWakeupStatusRequested
	}
	head, err := store.ReceiptStreamCursor(scope, threadID, "receipt:missing@abcdef", accept)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"receipt:wake-hidden@abcdef", "receipt:wake-trashed@abcdef", "not-a-receipt"} {
		got, err := store.ReceiptStreamCursor(scope, threadID, id, accept)
		if err != nil {
			t.Fatal(err)
		}
		if got != head || got.Snapshot {
			t.Fatalf("%s cursor %#v differs from unknown head %#v", id, got, head)
		}
	}
	resumed, err := store.ReceiptStreamCursor(scope, threadID, "receipt:wake-first@abcdef", func(wakeup primitives.AgentWakeup) bool {
		return wakeup.WakeupID == "wake-first"
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resumed.Snapshot || resumed.WakeupID != "wake-first" {
		t.Fatalf("visible resume cursor %#v", resumed)
	}
	page, err := store.ListReceiptStreamPage(scope, threadID, resumed)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Wakeups) != 1 || page.Wakeups[0].WakeupID != "wake-second" {
		t.Fatalf("resume page %#v", page.Wakeups)
	}
	fresh, err := store.ReceiptStreamCursor(scope, threadID, "", accept)
	if err != nil {
		t.Fatal(err)
	}
	seen := []string{}
	for fresh.Snapshot || fresh.Seq == 0 {
		page, err = store.ListReceiptStreamPage(scope, threadID, fresh)
		if err != nil {
			t.Fatal(err)
		}
		for _, wakeup := range page.Wakeups {
			seen = append(seen, wakeup.WakeupID)
		}
		fresh = page.Cursor
		if !page.HasMore {
			break
		}
	}
	if strings.Join(seen, ",") != "wake-first,wake-second" {
		t.Fatalf("snapshot leaked or reordered: %#v", seen)
	}
	if _, err = ws.DB().ExecContext(ctx, `UPDATE agent_wakeups SET status=?, updated_at=? WHERE wakeup_id=?`, primitives.AgentWakeupStatusClaimed, "2026-01-01T00:00:09Z", "wake-first"); err != nil {
		t.Fatal(err)
	}
	updated, err := store.ListReceiptStreamPage(scope, threadID, fresh)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Wakeups) != 1 || updated.Wakeups[0].WakeupID != "wake-first" || updated.Wakeups[0].Status != primitives.AgentWakeupStatusClaimed {
		t.Fatalf("update replay %#v", updated.Wakeups)
	}
}

func TestReceiptStreamTickBudgetIndependentOfHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("counts statements across 100k receipt rows")
	}
	budgets := map[int]receiptTickBudget{}
	for _, count := range []int{1000, 100000} {
		budgets[count] = measureReceiptTickBudget(t, count)
		t.Logf("history=%d warm=%+v idle=%+v append=%+v update=%+v", count, budgets[count].warm, budgets[count].idle, budgets[count].append, budgets[count].update)
	}
	small, large := budgets[1000], budgets[100000]
	if small != large {
		t.Fatalf("tick budget depends on history: 1k=%+v 100k=%+v", small, large)
	}
	if small.warm.statements > 2 || small.warm.rows > 1 || small.idle != small.warm {
		t.Fatalf("warm/idle tick scanned history: %+v", small)
	}
	if small.append.statements > 4 || small.append.rows > 3 || small.update != small.append {
		t.Fatalf("after-change tick scanned history: %+v", small)
	}
}

type receiptReadBudget struct {
	statements int
	rows       int
}

type receiptTickBudget struct {
	warm, idle, append, update receiptReadBudget
}

func measureReceiptTickBudget(t *testing.T, count int) receiptTickBudget {
	t.Helper()
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	threadID := "budget-thread"
	if _, err = ws.DB().ExecContext(ctx, `INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES(?,?,?,?)`, threadID, "2026-01-01T00:00:00Z", "owner", `{}`); err != nil {
		t.Fatal(err)
	}
	tx, err := ws.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for start := 1; start <= count; start += 1000 {
		end := start + 999
		if end > count {
			end = count
		}
		if _, err = tx.ExecContext(ctx, `WITH RECURSIVE n(i) AS (VALUES(?) UNION ALL SELECT i+1 FROM n WHERE i<?)
			INSERT INTO agent_wakeups(
				wakeup_id,status,notification_status,target_handle,target_actor_id,workspace_id,workspace_name,
				thread_id,thread_title,trigger_event_id,trigger_created_at,trigger_text,refs_json,bridge_instance_id,
				failure_reason,created_at,updated_at)
			SELECT printf('wake-%06d',i),'requested','unread','target.agent','actor-target','ws','Workspace',
				?,'', '', '', '', '[]','', '', printf('2026-01-01T%02d:%02d:%02dZ', i/3600, (i/60)%60, i%60), printf('2026-01-01T%02d:%02d:%02dZ', i/3600, (i/60)%60, i%60)
			FROM n`, start, end, threadID); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	counted, counter := testsql.Open("file:" + ws.Layout().DatabasePath)
	defer counted.Close()
	store := primitives.NewTestStore(counted, "")
	scope := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "stranger"})
	cursor, err := store.ReceiptStreamCursor(scope, threadID, "receipt:missing@abcdef", func(primitives.AgentWakeup) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if cursor.Snapshot {
		t.Fatal("unknown resume replayed the snapshot")
	}
	counter.Reset()
	warm := readReceiptBudget(t, store, scope, threadID, &cursor, counter)
	assertReceiptPlans(t, counted, counter)
	counter.Reset()
	idle := readReceiptBudget(t, store, scope, threadID, &cursor, counter)
	counter.Reset()
	if _, err = ws.DB().ExecContext(ctx, `INSERT INTO agent_wakeups(
		wakeup_id,status,notification_status,target_handle,target_actor_id,thread_id,refs_json,created_at,updated_at)
		VALUES('wake-appended','requested','unread','target.agent','actor-target',?,'[]','2026-02-01T00:00:00Z','2026-02-01T00:00:00Z')`, threadID); err != nil {
		t.Fatal(err)
	}
	appended := readReceiptBudget(t, store, scope, threadID, &cursor, counter)
	counter.Reset()
	if _, err = ws.DB().ExecContext(ctx, `UPDATE agent_wakeups SET status='claimed', updated_at='2026-02-01T00:00:01Z' WHERE wakeup_id='wake-000001'`); err != nil {
		t.Fatal(err)
	}
	updated := readReceiptBudget(t, store, scope, threadID, &cursor, counter)
	return receiptTickBudget{warm: warm, idle: idle, append: appended, update: updated}
}

func readReceiptBudget(t *testing.T, store *primitives.Store, ctx context.Context, threadID string, cursor *primitives.ReceiptStreamCursor, counter *testsql.Counter) receiptReadBudget {
	t.Helper()
	page, err := store.ListReceiptStreamPage(ctx, threadID, *cursor)
	if err != nil {
		t.Fatal(err)
	}
	if page.HasMore {
		t.Fatalf("tick was not at the head: %#v", page.Cursor)
	}
	if len(page.Wakeups) > 1 {
		t.Fatalf("tick returned %d receipts", len(page.Wakeups))
	}
	*cursor = page.Cursor
	return receiptReadBudget{statements: int(counter.Count()), rows: counter.ReturnedRows()}
}

func assertReceiptPlans(t *testing.T, db *sql.DB, counter *testsql.Counter) {
	t.Helper()
	sawStream := false
	for _, statement := range counter.Statements() {
		if strings.Contains(statement.SQL, "FROM agent_wakeups") {
			t.Fatalf("idle tick read wakeup payloads: %s", statement.SQL)
		}
		if !strings.Contains(statement.SQL, "FROM agent_wakeup_stream") {
			continue
		}
		sawStream = true
		rows, err := db.Query("EXPLAIN QUERY PLAN "+statement.SQL, statement.Args...)
		if err != nil {
			t.Fatal(err)
		}
		var plan strings.Builder
		for rows.Next() {
			var id, parent, notUsed int
			var detail string
			if err = rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			plan.WriteString(detail)
			plan.WriteByte('\n')
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(plan.String(), "idx_agent_wakeup_stream_thread_seq") {
			t.Fatalf("update log was not an indexed seek:\n%s\nSQL=%s", plan.String(), statement.SQL)
		}
	}
	if !sawStream {
		t.Fatal("idle tick did not probe the update log")
	}
}

func receiptWakeup(id, threadID, createdAt string) primitives.AgentWakeup {
	return primitives.AgentWakeup{
		WakeupID: id, Status: primitives.AgentWakeupStatusRequested, TargetHandle: "target.agent",
		TargetActorID: "actor-target", ThreadID: threadID, CreatedAt: createdAt, UpdatedAt: createdAt,
		Refs: []string{"thread:" + threadID},
	}
}
