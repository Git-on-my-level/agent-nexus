package primitives_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
	"agent-nexus-core/internal/testsql"
)

func TestOverviewAndReportBudgetsIgnoreInaccessibleWork(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db, counter := testsql.Open("file:" + ws.Layout().DatabasePath)
	defer db.Close()
	store := primitives.NewTestStore(db, ws.Layout().ArtifactContentDir)
	public, err := store.CreateBoard(ctx, "owner", map[string]any{"title": "Public board"})
	if err != nil {
		t.Fatal(err)
	}
	visible, err := store.CreateWork(ctx, "owner", public["id"].(string), map[string]any{"title": "Visible work"})
	if err != nil {
		t.Fatal(err)
	}
	private, err := store.CreateBoard(ctx, "owner", map[string]any{"title": "Private board", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PatchThread(ctx, "owner", private["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	readCtx := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "reader"})
	allowed := func(_ string, owner string) bool { return owner == "" || owner == "reader" }
	assert := func() {
		t.Helper()
		counter.Reset()
		body, err := store.OverviewVisible(readCtx, nil, nil, allowed, time.Now().UTC(), 0)
		if err != nil {
			t.Fatal(err)
		}
		work := body["work"].(map[string]any)
		if work["total"] != 1 || work["truncated"] != false || body["initiatives"].(map[string]any)["count"] != 1 {
			t.Fatalf("hidden rows affect Overview: %+v", body)
		}
		if counter.RowsRead() > 12 {
			t.Fatalf("hydrated private candidates: %d", counter.RowsRead())
		}
		counter.Reset()
		page, err := store.ListReportWork(readCtx, primitives.ReportWorkFilter{Limit: 1})
		if err != nil || page.Truncated || len(page.Work) != 1 || page.Work[0]["id"] != visible["id"] {
			t.Fatalf("report candidates changed: %+v %v", page, err)
		}
		if counter.RowsRead() != 1 {
			t.Fatalf("report hydrated inaccessible rows: %d", counter.RowsRead())
		}
	}
	assert()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	newer := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	for i := 0; i < 2001; i++ {
		if _, err = tx.Exec(`INSERT INTO cards(id,board_id,title,created_at,created_by,updated_at,updated_by) VALUES(?,?,'Private candidate',?,'fixture',?,'fixture')`, fmt.Sprintf("private-candidate-%04d", i), private["id"], newer, newer); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// Also deny a private card on a public board; both policies precede LIMIT.
	hidden, err := store.CreateWork(ctx, "owner", public["id"].(string), map[string]any{"title": "Private card"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PatchThread(ctx, "owner", hidden["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	assert()
	ownerPage, err := store.ListReportWork(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "owner"}), primitives.ReportWorkFilter{Limit: 1})
	if err != nil || !ownerPage.Truncated {
		t.Fatalf("visible overload not reported: %+v %v", ownerPage, err)
	}
}

func TestDigestSubjectBudgetTruncatesWithoutFailingOrInferringAnswers(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db, counter := testsql.Open("file:" + ws.Layout().DatabasePath)
	defer db.Close()
	store := primitives.NewTestStore(db, ws.Layout().ArtifactContentDir)
	board, err := store.CreateBoard(ctx, "owner", map[string]any{"title": "Subject budget"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err = store.RecordOverviewVisit(ctx, "reader", nil, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	refs := []string{}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < 4001; i++ {
		id := fmt.Sprintf("budget-subject-%04d", i)
		if _, err = tx.Exec(`INSERT INTO cards(id,board_id,title,created_at,created_by,updated_at,updated_by) VALUES(?,?,'Subject',?,'fixture',?,'fixture')`, id, board["id"], now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
		refs = append(refs, "card:"+id)
	}
	raw, _ := json.Marshal(refs)
	if _, err = tx.Exec(`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json) VALUES('over-budget-answer','human_attention_responded',?,'fixture',?,?)`, now.Format(time.RFC3339Nano), board["thread_id"], string(raw)); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	counter.Reset()
	readCtx := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "reader"})
	digest, err := store.OverviewChanges(readCtx, "reader", nil, false, func(_ string, owner string) bool { return owner == "" }, now.Add(time.Second))
	if err != nil || !digest.Truncated || len(digest.Items) != 0 {
		t.Fatalf("aggregate budget not explicit/safe: %+v %v", digest, err)
	}
	// Twenty 200-ref batches need at most two native-card page reads each,
	// plus the visit/event reads; hidden/omitted subjects cannot expand this.
	if counter.RowsRead() > 4005 || counter.Count() > 42 {
		t.Fatalf("unbounded subjects: rows=%d queries=%d", counter.RowsRead(), counter.Count())
	}
}

func TestHiddenAnswersDoNotConsumeDigestCandidateBudget(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	store := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	public, err := store.CreateBoard(ctx, "owner", map[string]any{"title": "Visible answers"})
	if err != nil {
		t.Fatal(err)
	}
	private, err := store.CreateBoard(ctx, "owner", map[string]any{"title": "Private answers"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := store.CreateWork(ctx, "owner", private["id"].(string), map[string]any{"title": "Private subject"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PatchThread(ctx, "owner", private["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err = store.RecordOverviewVisit(ctx, "reader", nil, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`INSERT INTO events(id,type,ts,actor_id,thread_id) VALUES('visible-answer','human_attention_responded',?,'fixture',?)`, now.Format(time.RFC3339Nano), public["thread_id"]); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal([]string{card["ref"].(string)})
	tx, err := ws.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < 201; i++ {
		// Public event thread but private subject: thread-only filtering is insufficient.
		if _, err = tx.Exec(`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json) VALUES(?,'human_attention_responded',?,'fixture',?,?)`, fmt.Sprintf("hidden-answer-%03d", i), now.Add(time.Second).Format(time.RFC3339Nano), public["thread_id"], string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	digest, err := store.OverviewChanges(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "reader"}), "reader", nil, false, func(_ string, owner string) bool { return owner == "" }, now.Add(2*time.Second))
	if err != nil || digest.Truncated || len(digest.Items) != 1 || digest.Items[0].Ref != "event:visible-answer" {
		t.Fatalf("hidden answers consume public budget: %+v %v", digest, err)
	}
}

func TestDashboardFiltersPrivatePinsAndNewerReportsBeforeBlobReads(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	backend := &countingReportBackend{Backend: blob.NewFilesystemBackend(ws.Layout().ArtifactContentDir)}
	store := primitives.NewStore(ws.DB(), backend, ws.Layout().ArtifactContentDir)
	report := `{"kind":"anx.visual-report","schema_version":1,"title":"Dashboard","summary":"Launch","generated_at":"2026-10-04T12:00:00Z","projects":[{"id":"launch","title":"Launch","summary":"Ship","outcome":"Delivery"}],"sources":[],"panels":[{"id":"progress","project_id":"launch","type":"live-initiatives","title":"Progress","author":"Test","provenance":"reported","observed_at":null,"freshness":"unknown","source_ids":[],"data":{}}]}`
	public, _, err := store.CreateDocument(ctx, "owner", map[string]any{"title": "Public report"}, report, "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	board, err := store.CreateBoard(ctx, "owner", map[string]any{"title": "Private report board"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := store.CreateWork(ctx, "owner", board["id"].(string), map[string]any{"title": "Private report card"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PatchThread(ctx, "owner", board["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	private, _, err := store.CreateDocument(ctx, "owner", map[string]any{"title": "Confidential report"}, report, "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Stored legacy bindings can share a card backing thread.
	if _, err = ws.DB().Exec(`UPDATE documents SET thread_id=? WHERE id=?`, card["thread_id"], private["id"]); err != nil {
		t.Fatal(err)
	}
	if err = store.SetWorkspaceDashboard(ctx, "owner", private["id"].(string)); err != nil {
		t.Fatal(err)
	}
	readerCtx := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "reader"})
	for _, all := range []bool{false, true} {
		backend.reads = 0
		var dashboard map[string]any
		if all {
			dashboard, err = store.DashboardReports(readerCtx)
		} else {
			var overview map[string]any
			overview, err = store.Overview(readerCtx, nil, nil)
			if err == nil {
				dashboard = overview["dashboard"].(map[string]any)
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		entries := dashboard["reports"].([]map[string]any)
		if len(entries) != 1 || entries[0]["id"] != public["id"] || dashboard["pinned_ref"] != nil || dashboard["has_more"] != false || backend.reads != 1 {
			t.Fatalf("hidden pin changed selection or blob reads: reads=%d %+v", backend.reads, dashboard)
		}
		raw, _ := json.Marshal(dashboard)
		if strings.Contains(string(raw), "Confidential") {
			t.Fatal(string(raw))
		}
	}
	ownerDashboard, err := store.DashboardReports(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "owner"}))
	if err != nil {
		t.Fatal(err)
	}
	entries := ownerDashboard["reports"].([]map[string]any)
	if len(entries) != 2 || entries[0]["id"] != private["id"] || ownerDashboard["pinned_ref"] == nil {
		t.Fatalf("owner lost pin: %+v", ownerDashboard)
	}
}
