package primitives_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testsql"
)

func TestReceiptStreamResumeAndUpdateReplay(t *testing.T) {
	t.Parallel()
	testReceiptStreamResumeAndUpdateReplay(t, false)
}

func TestReceiptStreamHistoricalResumeWithoutBackfill(t *testing.T) {
	t.Parallel()
	testReceiptStreamResumeAndUpdateReplay(t, true)
}

func testReceiptStreamResumeAndUpdateReplay(t *testing.T, historical bool) {
	t.Helper()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
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
	if historical {
		// Model receipts committed before the log was installed. The storage
		// upgrade test uses the real released schema; here exercise resume
		// privacy and offline updates with no historical log entries.
		if _, err = ws.DB().Exec(`DELETE FROM agent_wakeup_stream`); err != nil {
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
	changed, err := store.ReceiptStreamCursor(scope, threadID, "receipt:wake-first@stale", func(primitives.AgentWakeup) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	changedPage, err := store.ListReceiptStreamPage(scope, threadID, changed)
	if err != nil || len(changedPage.Wakeups) != 2 || changedPage.Wakeups[0].WakeupID != "wake-first" {
		t.Fatalf("offline historical update: %+v %v", changedPage.Wakeups, err)
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

func TestPerformanceReceiptStreamTickBudgetIndependentOfHistory(t *testing.T) {
	if testing.Short() || os.Getenv("ANX_PERFORMANCE_TEST") != "1" {
		t.Skip("advisory performance tier: make -C core test-perf")
	}
	// Serial: performance samples must not compete with parallel fixtures.
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
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
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
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
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
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
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
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
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
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
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
	sawMetadataSeek := false
	for _, statement := range counter.Statements() {
		metadata := strings.Contains(statement.SQL, "JOIN agent_wakeup_snapshot_positions")
		if !strings.Contains(statement.SQL, "JOIN agent_wakeups") && !metadata {
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
		// The inactive canonical fallback may propagate thread denials through
		// its existing index (alias r). Only the selected payload/metadata lookup
		// must seek wakeup_id, rather than scan that thread index.
		if strings.Contains(detail, "SEARCH _row USING INDEX idx_agent_wakeups_thread_trigger_created") || strings.Contains(detail, "SEARCH agent_wakeups USING INDEX idx_agent_wakeups_thread_trigger_created") {
			t.Fatalf("payload scan used the thread index:\n%s\nSQL=%s", detail, statement.SQL)
		}
		if strings.Contains(detail, "sqlite_autoindex_agent_wakeups_1") {
			sawSeek = true
			if metadata {
				sawMetadataSeek = true
				if !strings.Contains(detail, "sqlite_autoindex_resource_access_edges_1") || !strings.Contains(detail, "SEARCH x USING INTEGER PRIMARY KEY") || !strings.Contains(detail, "sqlite_autoindex_resource_access_mentions_1") {
					t.Fatalf("reference metadata did not seek the selected source keys:\n%s", detail)
				}
			}
		} else {
			t.Fatalf("payload read did not seek wakeup_id:\n%s\nSQL=%s", detail, statement.SQL)
		}
	}
	if !sawSeek {
		t.Fatal("payload query was not explained")
	}
	if !sawMetadataSeek {
		t.Fatal("receipt/reference metadata query was not explained")
	}
}

func TestPerformanceReceiptStreamHiddenHistoryTickTiming(t *testing.T) {
	// Serial: performance samples must not compete with parallel fixtures.
	if testing.Short() || os.Getenv("ANX_PERFORMANCE_TEST") != "1" {
		t.Skip("advisory performance tier: make -C core test-perf")
	}
	if testing.Short() {
		t.Skip("times idle and append ticks across 100k hidden receipts")
	}
	small := measureHiddenReceiptTicks(t, 1000, true)
	large := measureHiddenReceiptTicks(t, 100000, true)
	t.Logf("hidden 1k idle=%s append=%s; 100k idle=%s append=%s", small.idle, small.append, large.idle, large.append)
	t.Logf("during ten unrelated private changes: 1k max=%s budget=%+v; 100k max=%s budget=%+v", small.replay, small.replayBudget, large.replay, large.replayBudget)
	if small.replayBudget != large.replayBudget || large.replayBudget.statements > 10 || large.replayBudget.rows > 10 {
		t.Fatalf("append during replay depends on history: 1k=%+v 100k=%+v", small.replayBudget, large.replayBudget)
	}
	if large.replay > time.Second && large.replay > small.replay*20 {
		t.Fatalf("private changes delayed public delivery: 1k=%s 100k=%s", small.replay, large.replay)
	}
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

type hiddenReceiptTicks struct {
	idle, append, replay time.Duration
	replayBudget         receiptReadBudget
}

func TestReceiptStreamPrivateEpochReplayExcludesHiddenHistory(t *testing.T) {
	t.Parallel()
	measureHiddenReceiptTicks(t, 32, false)
}

func measureHiddenReceiptTicks(t *testing.T, count int, measure bool) hiddenReceiptTicks {
	t.Helper()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	counted, counter := testsql.Open("file:" + ws.Layout().DatabasePath)
	defer counted.Close()
	store := primitives.NewTestStore(counted, "")
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
	unrelated, err := store.AppendEvent(ctx, "owner", map[string]any{"type": "message_posted", "thread_id": privateID, "refs": []string{}, "payload": map[string]any{"text": "unrelated"}})
	if err != nil {
		t.Fatal(err)
	}
	unrelatedID := anyString(unrelated["id"])
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
	counter.Reset()
	started = time.Now()
	page, err = store.ListReceiptStreamPage(scope, publicID, cursor)
	appended := time.Since(started)
	for _, statement := range counter.Statements() {
		if statement.Elapsed > 10*time.Millisecond {
			t.Logf("slow append statement %s: %.160s", statement.Elapsed, statement.SQL)
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Wakeups) != 1 || page.Wakeups[0].WakeupID != next.WakeupID {
		t.Fatalf("append tick %#v", page.Wakeups)
	}
	result := hiddenReceiptTicks{idle: idle, append: appended}
	// An authorized resume establishes an idle stream that may replay visibility.
	cursor, err = store.ReceiptStreamCursor(scope, publicID, "receipt:wake-next@digest", func(primitives.AgentWakeup) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	page, err = store.ListReceiptStreamPage(scope, publicID, cursor)
	if err != nil {
		t.Fatal(err)
	}
	cursor = page.Cursor
	publicIDs := map[string]bool{prime.WakeupID: true, next.WakeupID: true}
	for i := 0; i < 10; i++ {
		trash := "now"
		if i%2 == 1 {
			trash = ""
		}
		if _, err = ws.DB().Exec(`UPDATE events SET trashed_at=? WHERE id=?`, trash, unrelatedID); err != nil {
			t.Fatal(err)
		}
		probe := receiptWakeup(fmt.Sprintf("wake-replay-%d", i), publicID, "2026-04-01T00:00:00Z")
		if _, err = store.UpsertAgentWakeup(ctx, probe); err != nil {
			t.Fatal(err)
		}
		counter.Reset()
		started = time.Now()
		page, err = store.ListReceiptStreamPage(scope, publicID, cursor)
		elapsed := time.Since(started)
		if elapsed > result.replay {
			result.replay = elapsed
		}
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Wakeups) != 1 || page.Wakeups[0].WakeupID != probe.WakeupID {
			t.Fatalf("private epoch change starved append %d: %+v", i, page.Wakeups)
		}
		budget := receiptReadBudget{statements: int(counter.Count()), rows: counter.ReturnedRows()}
		if measure && i > 0 && budget != result.replayBudget {
			t.Fatalf("replay work grew across epochs: prior=%+v current=%+v", result.replayBudget, budget)
		}
		result.replayBudget = budget
		cursor = page.Cursor
		if !measure {
			// The first page prioritizes the new tail receipt. Drain the snapshot
			// too: only historical replay can expose the private-trigger receipts.
			publicIDs[probe.WakeupID] = true
			if !page.AccessChanged || !cursor.Replay || !cursor.Snapshot || !page.HasMore {
				t.Fatalf("epoch %d did not schedule historical replay: %+v", i, page)
			}
			seen := map[string]bool{}
			maxPages := (count+12)/primitives.ReceiptStreamPageSize + 3
			for pages := 0; page.HasMore; pages++ {
				if pages >= maxPages {
					t.Fatalf("epoch %d replay did not finish within %d pages", i, maxPages)
				}
				page, err = store.ListReceiptStreamPage(scope, publicID, cursor)
				if err != nil {
					t.Fatal(err)
				}
				for _, wakeup := range page.Wakeups {
					if !publicIDs[wakeup.WakeupID] {
						t.Fatalf("epoch %d historical replay disclosed hidden receipt %q", i, wakeup.WakeupID)
					}
					seen[wakeup.WakeupID] = true
				}
				cursor = page.Cursor
			}
			if cursor.Snapshot || cursor.Replay || cursor.ReplayAgain {
				t.Fatalf("epoch %d replay cursor remained active: %+v", i, cursor)
			}
			if !seen[prime.WakeupID] || !seen[next.WakeupID] {
				t.Fatalf("epoch %d replay did not visit public historical receipts: %v", i, seen)
			}
		}
	}
	return result
}

func receiptWakeup(id, threadID, createdAt string) primitives.AgentWakeup {
	return primitives.AgentWakeup{
		WakeupID: id, Status: primitives.AgentWakeupStatusRequested, TargetHandle: "target.agent",
		TargetActorID: "actor-target", ThreadID: threadID, CreatedAt: createdAt, UpdatedAt: createdAt,
		Refs: []string{"thread:" + threadID},
	}
}
