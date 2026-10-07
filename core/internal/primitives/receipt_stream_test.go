package primitives_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

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
	if small.append.statements > 8 || small.append.rows > 8 || small.update != small.append {
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

func TestReceiptStreamStaleDigestResumesAtReceipt(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	store := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	thread, err := store.CreateThread(ctx, "owner", map[string]any{"title": "stale resume"})
	if err != nil {
		t.Fatal(err)
	}
	threadID := anyString(thread.Thread["id"])
	wakeup := receiptWakeup("wake-stale", threadID, "2026-01-01T00:00:01Z")
	if _, err = store.UpsertAgentWakeup(ctx, wakeup); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().ExecContext(ctx, `UPDATE agent_wakeups SET status=? WHERE wakeup_id=?`, primitives.AgentWakeupStatusClaimed, wakeup.WakeupID); err != nil {
		t.Fatal(err)
	}
	scope := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "stranger"})
	cursor, err := store.ReceiptStreamCursor(scope, threadID, "receipt:wake-stale@olddigest", func(primitives.AgentWakeup) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if !cursor.Snapshot || !cursor.IncludeCurrent || cursor.WakeupID != "wake-stale" {
		t.Fatalf("stale digest skipped to the head: %#v", cursor)
	}
	page, err := store.ListReceiptStreamPage(scope, threadID, cursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Wakeups) != 1 || page.Wakeups[0].Status != primitives.AgentWakeupStatusClaimed {
		t.Fatalf("offline claim was not delivered: %#v", page.Wakeups)
	}
}

func TestReceiptStreamVisibilityChangeReplaysHiddenReceipt(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	store := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	public, err := store.CreateThread(ctx, "owner", map[string]any{"title": "public"})
	if err != nil {
		t.Fatal(err)
	}
	private, err := store.CreateThread(ctx, "owner", map[string]any{"title": "private"})
	if err != nil {
		t.Fatal(err)
	}
	publicID := anyString(public.Thread["id"])
	privateID := anyString(private.Thread["id"])
	if _, err = store.PatchThread(ctx, "owner", privateID, map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	event, err := store.AppendEvent(ctx, "owner", map[string]any{
		"type": "message_posted", "thread_id": privateID, "refs": []string{"thread:" + privateID},
		"payload": map[string]any{"text": "hidden trigger"},
	})
	if err != nil {
		t.Fatal(err)
	}
	hidden := receiptWakeup("wake-hidden", publicID, "2026-01-01T00:00:01Z")
	hidden.TriggerEventID = anyString(event["id"])
	if _, err = store.UpsertAgentWakeup(ctx, hidden); err != nil {
		t.Fatal(err)
	}
	scope := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "stranger"})
	cursor, err := store.ReceiptStreamCursor(scope, publicID, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for {
		page, err := store.ListReceiptStreamPage(scope, publicID, cursor)
		if err != nil {
			t.Fatal(err)
		}
		for _, wakeup := range page.Wakeups {
			if wakeup.WakeupID == hidden.WakeupID {
				t.Fatal("hidden receipt was delivered before the thread became public")
			}
		}
		cursor = page.Cursor
		if !page.HasMore {
			break
		}
	}
	if _, err = store.PatchThread(ctx, "owner", privateID, map[string]any{"pm_actor_id": ""}, nil); err != nil {
		t.Fatal(err)
	}
	probe := receiptWakeup("wake-probe", publicID, "2026-01-02T00:00:00Z")
	if _, err = store.UpsertAgentWakeup(ctx, probe); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	changed := false
	for {
		page, err := store.ListReceiptStreamPage(scope, publicID, cursor)
		if err != nil {
			t.Fatal(err)
		}
		changed = changed || page.AccessChanged
		for _, wakeup := range page.Wakeups {
			seen[wakeup.WakeupID] = true
		}
		cursor = page.Cursor
		if !page.HasMore {
			break
		}
	}
	if !changed || !seen[hidden.WakeupID] || !seen[probe.WakeupID] {
		t.Fatalf("visibility replay changed=%v seen=%v", changed, seen)
	}
}

func TestReceiptStreamNewPrivateRefStaysHidden(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	store := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	public, err := store.CreateThread(ctx, "owner", map[string]any{"title": "ref public"})
	if err != nil {
		t.Fatal(err)
	}
	private, err := store.CreateThread(ctx, "owner", map[string]any{"title": "ref private"})
	if err != nil {
		t.Fatal(err)
	}
	publicID := anyString(public.Thread["id"])
	privateID := anyString(private.Thread["id"])
	if _, err = store.PatchThread(ctx, "owner", privateID, map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	scope := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "stranger"})
	cursor, err := store.ReceiptStreamCursor(scope, publicID, "receipt:missing@abcdef", func(primitives.AgentWakeup) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	primer := receiptWakeup("wake-primer", publicID, "2026-01-01T00:00:01Z")
	if _, err = store.UpsertAgentWakeup(ctx, primer); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ListReceiptStreamPage(scope, publicID, cursor); err != nil {
		t.Fatal(err)
	}
	secret := receiptWakeup("wake-secret", publicID, "2026-01-01T00:00:02Z")
	secret.Refs = []string{"thread:" + privateID}
	if _, err = store.UpsertAgentWakeup(ctx, secret); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListReceiptStreamPage(scope, publicID, cursor)
	if err != nil {
		t.Fatal(err)
	}
	for _, wakeup := range page.Wakeups {
		if wakeup.WakeupID == secret.WakeupID {
			t.Fatal("wakeup referencing a private thread was delivered")
		}
	}
}

func TestReceiptPayloadPlanSeeksWakeupID(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	threadID := "plan-thread"
	if _, err = ws.DB().ExecContext(ctx, `INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES(?,?,?,?)`, threadID, "2026-01-01T00:00:00Z", "owner", `{}`); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().ExecContext(ctx, `INSERT INTO agent_wakeups(
		wakeup_id,status,notification_status,target_handle,target_actor_id,thread_id,refs_json,created_at,updated_at)
		VALUES('wake-plan','requested','unread','target.agent','actor-target',?,'[]','2026-01-01T00:00:01Z','2026-01-01T00:00:01Z')`, threadID); err != nil {
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
	if _, err = ws.DB().ExecContext(ctx, `INSERT INTO agent_wakeups(
		wakeup_id,status,notification_status,target_handle,target_actor_id,thread_id,refs_json,created_at,updated_at)
		VALUES('wake-seek','requested','unread','target.agent','actor-target',?,'[]','2026-02-01T00:00:00Z','2026-02-01T00:00:00Z')`, threadID); err != nil {
		t.Fatal(err)
	}
	counter.Reset()
	page, err := store.ListReceiptStreamPage(scope, threadID, cursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Wakeups) != 1 || page.Wakeups[0].WakeupID != "wake-seek" {
		t.Fatalf("page %#v", page.Wakeups)
	}
	sawSeek := false
	for _, statement := range counter.Statements() {
		if !strings.Contains(statement.SQL, "JOIN agent_wakeups") {
			continue
		}
		rows, err := counted.Query("EXPLAIN QUERY PLAN "+statement.SQL, statement.Args...)
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
		detail := plan.String()
		if strings.Contains(detail, "idx_agent_wakeups_thread_trigger_created") {
			t.Fatalf("payload scan used the thread index:\n%s\nSQL=%s", detail, statement.SQL)
		}
		if strings.Contains(detail, "sqlite_autoindex_agent_wakeups_1") || strings.Contains(detail, "idx_agent_wakeups_thread_wakeup") {
			sawSeek = true
		} else {
			t.Fatalf("payload read did not seek wakeup_id:\n%s\nSQL=%s", detail, statement.SQL)
		}
	}
	if !sawSeek {
		t.Fatal("payload query was not explained")
	}
}

func TestReceiptStreamHiddenHistoryTickTiming(t *testing.T) {
	if testing.Short() {
		t.Skip("times idle and append ticks across 100k hidden receipts")
	}
	small := measureHiddenReceiptTicks(t, 1000)
	large := measureHiddenReceiptTicks(t, 100000)
	t.Logf("hidden 1k idle=%s append=%s; 100k idle=%s append=%s", small.idle, small.append, large.idle, large.append)
	const limit = 8 * time.Second
	if large.idle > limit || large.append > limit {
		t.Fatalf("hidden-history ticks depend on history size: 1k idle=%s append=%s; 100k idle=%s append=%s", small.idle, small.append, large.idle, large.append)
	}
	if large.idle > small.idle*20 && large.idle > time.Second {
		t.Fatalf("idle tick grew with hidden history: 1k=%s 100k=%s", small.idle, large.idle)
	}
	if large.append > small.append*20 && large.append > time.Second {
		t.Fatalf("append tick grew with hidden history: 1k=%s 100k=%s", small.append, large.append)
	}
}

type hiddenReceiptTicks struct{ idle, append time.Duration }

func measureHiddenReceiptTicks(t *testing.T, count int) hiddenReceiptTicks {
	t.Helper()
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	store := primitives.NewTestStore(ws.DB(), "")
	public, err := store.CreateThread(ctx, "owner", map[string]any{"title": "timed public"})
	if err != nil {
		t.Fatal(err)
	}
	private, err := store.CreateThread(ctx, "owner", map[string]any{"title": "timed private"})
	if err != nil {
		t.Fatal(err)
	}
	publicID := anyString(public.Thread["id"])
	privateID := anyString(private.Thread["id"])
	if _, err = store.PatchThread(ctx, "owner", privateID, map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	event, err := store.AppendEvent(ctx, "owner", map[string]any{
		"type": "message_posted", "thread_id": privateID, "refs": []string{"thread:" + privateID},
		"payload": map[string]any{"text": "timed hidden"},
	})
	if err != nil {
		t.Fatal(err)
	}
	triggerID := anyString(event["id"])
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
				wakeup_id,status,notification_status,target_handle,target_actor_id,thread_id,trigger_event_id,
				refs_json,created_at,updated_at)
			SELECT printf('hidden-%06d',i),'requested','unread','target.agent','actor-target',?,?,'[]',
				printf('2026-01-01T%02d:%02d:%02dZ', i/3600, (i/60)%60, i%60), printf('2026-01-01T%02d:%02d:%02dZ', i/3600, (i/60)%60, i%60)
			FROM n`, start, end, publicID, triggerID); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	scope := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "stranger"})
	cursor, err := store.ReceiptStreamCursor(scope, publicID, "receipt:missing@abcdef", func(primitives.AgentWakeup) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	prime := receiptWakeup("wake-prime", publicID, "2026-03-01T00:00:00Z")
	if _, err = store.UpsertAgentWakeup(ctx, prime); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListReceiptStreamPage(scope, publicID, cursor)
	if err != nil {
		t.Fatal(err)
	}
	cursor = page.Cursor
	started := time.Now()
	page, err = store.ListReceiptStreamPage(scope, publicID, cursor)
	idle := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	if page.HasMore || len(page.Wakeups) != 0 {
		t.Fatalf("idle tick was not idle: hasMore=%v wakeups=%d", page.HasMore, len(page.Wakeups))
	}
	cursor = page.Cursor
	next := receiptWakeup("wake-next", publicID, "2026-03-02T00:00:00Z")
	if _, err = store.UpsertAgentWakeup(ctx, next); err != nil {
		t.Fatal(err)
	}
	started = time.Now()
	page, err = store.ListReceiptStreamPage(scope, publicID, cursor)
	appended := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Wakeups) != 1 || page.Wakeups[0].WakeupID != next.WakeupID {
		t.Fatalf("append tick %#v", page.Wakeups)
	}
	return hiddenReceiptTicks{idle: idle, append: appended}
}

func receiptWakeup(id, threadID, createdAt string) primitives.AgentWakeup {
	return primitives.AgentWakeup{
		WakeupID: id, Status: primitives.AgentWakeupStatusRequested, TargetHandle: "target.agent",
		TargetActorID: "actor-target", ThreadID: threadID, CreatedAt: createdAt, UpdatedAt: createdAt,
		Refs: []string{"thread:" + threadID},
	}
}
