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
	ask := func(threadID, title string) {
		t.Helper()
		if _, err := store.AppendEvent(ctx, "agent-1", map[string]any{
			"type": "human_attention_requested", "thread_id": threadID, "summary": title,
			"refs": []string{"thread:" + threadID},
			"payload": map[string]any{
				"kind": "ask", "title": title, "requester_actor_id": "agent-1",
				"subject_ref": anyStringValue(subject["ref"]), "response_proposals": []any{"Proceed", "Wait"},
				"by": "2026-06-01T00:00:00Z", "blocks": []any{blocked["ref"]},
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	ask(anyStringValue(subject["thread_id"]), "Merge calibration?")
	// The hosted row: a legacy event id whose thread_id never landed in threads.
	ask("ac17545a-6113-47bc-b33c-4b0df6b0df6968893", "Orphan ask")
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
	decisions, _ := brief["decisions"].([]map[string]any)
	if len(decisions) != 1 || decisions[0]["title"] != "Merge calibration?" {
		t.Fatalf("decisions = %#v", decisions)
	}
	for _, section := range []string{"decisions", "your_asks", "answered_for_you"} {
		for _, item := range brief[section].([]map[string]any) {
			if item["title"] == "Orphan ask" {
				t.Fatalf("orphan ask offered in %s: %#v", section, item)
			}
		}
	}
	initiatives, _ := brief["initiatives"].([]map[string]any)
	byTitle := map[string]map[string]any{}
	for _, item := range initiatives {
		byTitle[anyStringValue(item["title"])] = item
	}
	battery := byTitle["Battery"]
	if battery["waiting_on_you_count"] != 1 || battery["blocked_count"] != 1 {
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
