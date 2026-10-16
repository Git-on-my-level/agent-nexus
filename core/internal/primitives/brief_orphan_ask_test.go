package primitives

import (
	"context"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/blob"
)

// The production ask is a human_attention_requested event whose thread_id has
// no threads row. Inbox projection is per thread and writes nothing when
// GetThread misses, so respond 404s. The brief must omit that ask from
// decisions and from the counts Decide and initiative waiting use.
func TestBriefOmitsAskWhoseThreadIsMissing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	workspace, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	store := NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{"title": "Portfolio", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	work := func(title string) map[string]any {
		t.Helper()
		card, err := store.CreateWork(ctx, "actor-1", anyStringValue(board["id"]), map[string]any{"title": title})
		if err != nil {
			t.Fatal(err)
		}
		return card
	}
	subject := work("Battery")
	blocked := work("Report")
	ask := func(threadID, title, requester string, blocks bool) string {
		t.Helper()
		payload := map[string]any{
			"kind": "ask", "title": title, "requester_actor_id": requester,
			"subject_ref": anyStringValue(subject["ref"]), "response_proposals": []any{"Proceed", "Wait"},
			"by": "2026-06-01T00:00:00Z",
		}
		if blocks {
			payload["blocks"] = []any{blocked["ref"]}
		}
		event, err := store.AppendEvent(ctx, "agent-1", map[string]any{
			"type": "human_attention_requested", "thread_id": threadID, "summary": title,
			"refs": []string{"thread:" + threadID}, "payload": payload,
		})
		if err != nil {
			t.Fatal(err)
		}
		return anyStringValue(event["id"])
	}
	answer := func(threadID, askID string) {
		t.Helper()
		if _, _, err := store.AppendHumanAttentionResponse(ctx, "human", askID, "inbox:"+askID, "", "", map[string]any{
			"type": "human_attention_responded", "thread_id": threadID, "refs": []string{"event:" + askID},
			"payload": map[string]any{"outcome": "answered", "response_text": "Proceed", "request_event_ref": "event:" + askID},
		}, map[string]any{"requested": true, "target_actor_id": "agent-1", "thread_id": threadID}); err != nil {
			t.Fatal(err)
		}
	}
	threadID := anyStringValue(subject["thread_id"])
	// The hosted row: a legacy event id whose thread_id never landed in threads.
	const orphanThread = "ac17545a-6113-47bc-b33c-4b0df6b0df6968893"
	ask(threadID, "Merge calibration?", "agent-1", true)
	ask(orphanThread, "Orphan ask", "agent-1", true)
	ask(threadID, "My open ask", "reader", false)
	ask(orphanThread, "Orphan yours", "reader", false)
	answeredID := ask(threadID, "My answered ask", "reader", false)
	orphanAnswered := ask(orphanThread, "Orphan answered", "reader", false)
	answer(threadID, answeredID)
	answer(orphanThread, orphanAnswered)
	for drained := false; !drained; {
		if drained, err = store.RefreshBriefInitiatives(ctx, 200); err != nil {
			t.Fatal(err)
		}
	}

	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	scoped := WithAccessScope(ctx, AccessScope{ActorID: "reader"})
	brief, err := store.Brief(scoped, BriefReader{ActorID: "reader", Kind: "human", Visible: func(string, string) bool { return true }}, now)
	if err != nil {
		t.Fatal(err)
	}
	titles := func(section string) []string {
		t.Helper()
		items, _ := brief[section].([]map[string]any)
		out := make([]string, 0, len(items))
		for _, item := range items {
			out = append(out, anyStringValue(item["title"]))
		}
		return out
	}
	if got := titles("decisions"); len(got) != 1 || got[0] != "Merge calibration?" {
		t.Fatalf("decisions = %#v", brief["decisions"])
	}
	if got := titles("your_asks"); len(got) != 1 || got[0] != "My open ask" {
		t.Fatalf("your_asks = %#v", brief["your_asks"])
	}
	if got := titles("answered_for_you"); len(got) != 1 || got[0] != "My answered ask" {
		t.Fatalf("answered_for_you = %#v", brief["answered_for_you"])
	}
	for _, section := range []string{"decisions", "your_asks", "answered_for_you"} {
		for _, title := range titles(section) {
			if title == "Orphan ask" || title == "Orphan yours" || title == "Orphan answered" {
				t.Fatalf("orphan ask offered in %s: %q", section, title)
			}
		}
	}
	initiatives, _ := brief["initiatives"].([]map[string]any)
	byTitle := map[string]map[string]any{}
	for _, item := range initiatives {
		byTitle[anyStringValue(item["title"])] = item
	}
	battery := byTitle["Battery"]
	// The reader's own open ask is about Battery too, so the subject count is
	// two. The orphan open asks are the same subject and must not add a third.
	if battery["waiting_on_you_count"] != 1 || battery["blocked_count"] != 2 {
		t.Fatalf("battery counts = %#v", battery)
	}
	report := byTitle["Report"]
	if report["blocked_count"] != 1 || report["waiting_on_you_count"] != 0 {
		t.Fatalf("report counts = %#v", report)
	}
}

func TestBriefAskReadsSeekThreadByPrimaryKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	workspace, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()

	decision := briefAskSelect + ` WHERE COALESCE(e.trashed_at,'')='' AND COALESCE(json_extract(brief_ask_thread.body_json,'$.pm_actor_id'),'') IN ('', ?) AND a.state='open' AND a.decide_by IS NOT NULL AND (a.recipient IN (?,?) OR a.recipient='') AND a.requester NOT IN (?,?) ORDER BY a.decide_by LIMIT ?`
	subject := `SELECT a.subject_ref,a.recipient,a.requester FROM brief_asks a INDEXED BY idx_brief_asks_subject JOIN events e ON e.id=a.ask_id ` + briefAnswerableAskJoin + `
		WHERE a.subject_ref IN (SELECT value FROM json_each(?)) AND a.state='open' AND COALESCE(e.trashed_at,'')='' AND COALESCE(json_extract(brief_ask_thread.body_json,'$.pm_actor_id'),'') IN ('', ?) LIMIT 1000`
	blocks := `SELECT k.ref,COUNT(*) FROM brief_ask_blocks k INDEXED BY idx_brief_ask_blocks_ref JOIN brief_asks a ON a.ask_id=k.ask_id JOIN events e ON e.id=a.ask_id ` + briefAnswerableAskJoin + `
		WHERE k.ref IN (SELECT value FROM json_each(?)) AND a.state='open' AND a.subject_ref<>k.ref AND COALESCE(e.trashed_at,'')='' AND COALESCE(json_extract(brief_ask_thread.body_json,'$.pm_actor_id'),'') IN ('', ?) GROUP BY k.ref`

	scope := WithAccessScope(ctx, AccessScope{ActorID: "reader"})
	for _, tc := range []struct {
		name, query string
		args        []any
		index       string
	}{
		{"decisions", decision, []any{"reader", "reader", "actor:reader", "reader", "actor:reader", briefAskCandidates}, "idx_brief_asks_open_"},
		{"waiting", subject, []any{`["card:battery"]`, "reader"}, "idx_brief_asks_subject"},
		{"blocks", blocks, []any{`["card:report"]`, "reader"}, "idx_brief_ask_blocks_ref"},
		{"cited", citedAskSelect, []any{`["event-1"]`, `["event-1"]`}, "sqlite_autoindex_brief_asks_1"},
	} {
		raw := explainQueryPlan(t, workspace.DB(), tc.query, tc.args...)
		assertAskPlan(t, tc.name+" raw", raw, tc.index)
		scoped := explainQueryPlan(t, workspace.DB(), scopeRead(scope, tc.query), tc.args...)
		assertAskPlan(t, tc.name+" scoped", scoped, tc.index)
	}
}

func assertAskPlan(t *testing.T, name, plan, index string) {
	t.Helper()
	if !strings.Contains(plan, index) {
		t.Fatalf("%s plan did not use %s:\n%s", name, index, plan)
	}
	if !strings.Contains(plan, "sqlite_autoindex_threads_1 (id=?)") {
		t.Fatalf("%s plan did not seek threads by primary key:\n%s", name, plan)
	}
	for _, line := range strings.Split(plan, "\n") {
		// The ask's event is one primary-key seek (SEARCH e / SEARCH _row
		// USING sqlite_autoindex_events_1). A scoped read inlines the events
		// shadow, so neither the raw nor the scoped plan materializes it.
		// Naming threads does materialize the ownership graph (_anx_denied);
		// that scan is the access policy, not the ask lookup.
		if strings.Contains(line, "SCAN events") || strings.Contains(line, "SCAN main.events") || strings.Contains(line, "MATERIALIZE events") {
			t.Fatalf("%s plan scanned or materialized events:\n%s", name, plan)
		}
		if !strings.Contains(strings.ToUpper(line), "SCAN") {
			continue
		}
		// Naming threads pulls in the ownership graph, which lists private
		// threads once per statement through its covering index.
		if strings.Contains(line, "idx_access_private_threads") || strings.Contains(line, "resource_access_tombstones") || strings.Contains(line, "SCAN d") {
			continue
		}
		if strings.Contains(line, "brief_asks") || strings.Contains(line, "brief_ask_thread") || strings.Contains(line, "SCAN a ") || strings.Contains(line, "SCAN main.threads") {
			t.Fatalf("%s plan scanned asks or threads:\n%s", name, plan)
		}
	}
}

// A signed-in read shadows threads. The inner join must still return the
// reader's own PM thread and must not turn a scope-hidden thread into a
// public ask.
func TestBriefAskJoinUnderAccessScope(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	workspace, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	store := NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{"title": "Portfolio", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	card := func(title, owner string) map[string]any {
		t.Helper()
		created, err := store.CreateWork(ctx, "actor-1", anyStringValue(board["id"]), map[string]any{"title": title})
		if err != nil {
			t.Fatal(err)
		}
		if owner != "" {
			if _, err = store.PatchThread(ctx, "actor-1", anyStringValue(created["thread_id"]), map[string]any{"pm_actor_id": owner}, nil); err != nil {
				t.Fatal(err)
			}
		}
		return created
	}
	ask := func(work map[string]any, title string) {
		t.Helper()
		threadID := anyStringValue(work["thread_id"])
		if _, err := store.AppendEvent(ctx, "agent-1", map[string]any{
			"type": "human_attention_requested", "thread_id": threadID, "summary": title,
			"refs": []string{"thread:" + threadID},
			"payload": map[string]any{
				"kind": "ask", "title": title, "requester_actor_id": "agent-1",
				"subject_ref": anyStringValue(work["ref"]), "response_proposals": []any{"Proceed"},
				"by": "2026-06-01T00:00:00Z",
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	own := card("Own PM", "reader")
	hidden := card("Hidden PM", "other")
	ask(own, "Ask on my thread")
	ask(hidden, "Ask on a hidden thread")

	scoped := WithAccessScope(ctx, AccessScope{ActorID: "reader"})
	brief, err := store.Brief(scoped, BriefReader{ActorID: "reader", Kind: "human"}, time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	decisions, _ := brief["decisions"].([]map[string]any)
	seen := map[string]bool{}
	for _, item := range decisions {
		seen[anyStringValue(item["title"])] = true
	}
	if !seen["Ask on my thread"] || seen["Ask on a hidden thread"] {
		t.Fatalf("decisions = %#v", decisions)
	}
}

// A decided ask cited from a live note does not flag when its thread row is
// missing. The same rule still flags a decided ask whose thread joins.
func TestCitedDecidedAskWithMissingThreadDoesNotFlag(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	workspace, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	store := NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{"title": "Portfolio", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	makeCard := func(title string) map[string]any {
		t.Helper()
		card, err := store.CreateWork(ctx, "actor-1", anyStringValue(board["id"]), map[string]any{"title": title})
		if err != nil {
			t.Fatal(err)
		}
		return card
	}
	post := func(threadID, subject string) string {
		t.Helper()
		event, err := store.AppendEvent(ctx, "agent-1", map[string]any{
			"type": "human_attention_requested", "thread_id": threadID, "summary": "Which way?",
			"refs": []string{"thread:" + threadID},
			"payload": map[string]any{
				"kind": "ask", "title": "Which way?", "requester_actor_id": "agent-1",
				"subject_ref": subject, "response_proposals": []any{"Proceed"},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return anyStringValue(event["id"])
	}
	answer := func(threadID, askID string) {
		t.Helper()
		if _, _, err := store.AppendHumanAttentionResponse(ctx, "human", askID, "inbox:"+askID, "", "", map[string]any{
			"type": "human_attention_responded", "thread_id": threadID, "refs": []string{"event:" + askID},
			"payload": map[string]any{"outcome": "answered", "response_text": "Proceed", "request_event_ref": "event:" + askID},
		}, map[string]any{"requested": true, "target_actor_id": "agent-1", "thread_id": threadID}); err != nil {
			t.Fatal(err)
		}
	}
	note := func(card map[string]any, askID string) {
		t.Helper()
		threadID := anyStringValue(card["thread_id"])
		if _, err := store.AppendEvent(ctx, "agent-1", map[string]any{
			"type": "message_posted", "thread_id": threadID,
			"refs":    []string{anyStringValue(card["ref"]), "thread:" + threadID},
			"payload": map[string]any{"text": "next step waits on event:" + askID, "next": map[string]any{"at": "2d", "what": "hear back"}},
		}); err != nil {
			t.Fatal(err)
		}
	}

	const orphanThread = "ac17545a-6113-47bc-b33c-4b0df6b0df6968893"
	citing := makeCard("Cites a missing thread")
	orphanAsk := post(orphanThread, anyStringValue(citing["ref"]))
	answer(orphanThread, orphanAsk)
	note(citing, orphanAsk)

	visible := makeCard("Cites a real thread")
	realAsk := post(anyStringValue(visible["thread_id"]), anyStringValue(visible["ref"]))
	answer(anyStringValue(visible["thread_id"]), realAsk)
	note(visible, realAsk)

	if drained, err := store.RefreshBriefInitiatives(ctx, 200); err != nil || !drained {
		t.Fatalf("refresh drained=%v err=%v", drained, err)
	}
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	allow := func(string, string) bool { return true }
	scoped := WithAccessScope(ctx, AccessScope{ActorID: "reader"})
	brief, err := store.Brief(scoped, BriefReader{ActorID: "reader", Kind: "human", Visible: allow}, now)
	if err != nil {
		t.Fatal(err)
	}
	missing := initiativeMoot(t, brief, "Cites a missing thread")
	if missing["flagged"] != false || missing["ask_ref"] != nil || strings.Contains(anyStringValue(missing["reason"]), orphanAsk) {
		t.Fatalf("missing-thread moot = %#v", missing)
	}
	shown := initiativeMoot(t, brief, "Cites a real thread")
	if shown["flagged"] != true || shown["ask_ref"] != "event:"+realAsk {
		t.Fatalf("real-thread moot = %#v", shown)
	}

	item, err := store.GetWork(ctx, anyStringValue(citing["id"]))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.EnrichCardPlans(scoped, []map[string]any{item}, allow, now); err != nil {
		t.Fatal(err)
	}
	moot, ok := item["possibly_moot"].(PossiblyMoot)
	if !ok || moot.Flagged || moot.AskRef != "" || strings.Contains(moot.Reason, orphanAsk) {
		t.Fatalf("card possibly_moot = %+v", item["possibly_moot"])
	}
}

func TestOpenCurationBatchOmitsMemberWhoseThreadIsMissing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	workspace, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	store := NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{"title": "Portfolio", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := store.CreateWork(ctx, "actor-1", anyStringValue(board["id"]), map[string]any{"title": "Battery"})
	if err != nil {
		t.Fatal(err)
	}
	ask := func(threadID, title string) string {
		t.Helper()
		event, err := store.AppendEvent(ctx, "agent-1", map[string]any{
			"type": "human_attention_requested", "thread_id": threadID, "summary": title,
			"refs": []string{"thread:" + threadID},
			"payload": map[string]any{
				"kind": "ask", "title": title, "requester_actor_id": "agent-1",
				"subject_ref": anyStringValue(card["ref"]), "response_proposals": []any{"Proceed"},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return anyStringValue(event["id"])
	}
	live := ask(anyStringValue(card["thread_id"]), "Live member")
	orphan := ask("ac17545a-6113-47bc-b33c-4b0df6b0df6968893", "Orphan member")
	if err = store.GroupCurationBatch(ctx, CurationProposalRecord{
		Key: "batch-orphan", ProposalEventID: "prop-1", Targets: []string{"event:" + live, "event:" + orphan},
	}); err != nil {
		t.Fatal(err)
	}
	members, hidden, err := store.OpenCurationBatchMembers(WithAccessScope(ctx, AccessScope{ActorID: "reader"}), "batch-orphan")
	if err != nil {
		t.Fatal(err)
	}
	if hidden || len(members) != 1 || members[0].AskID != live || members[0].Title != "Live member" {
		t.Fatalf("members hidden=%v %#v", hidden, members)
	}
}
