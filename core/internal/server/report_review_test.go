package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
	reports "agent-nexus-visualreport"
)

func TestReportReviewAuthorPrivacyDedupeAndRetirement(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	store := primitives.NewStore(ws.DB(), blob.NewFilesystemBackend(ws.Layout().ArtifactContentDir), ws.Layout().ArtifactContentDir)
	registry := actors.NewStore(ws.DB())
	for _, id := range []string{"author", "writer", "reader"} {
		if _, err := registry.Register(ctx, actors.Actor{ID: id, DisplayName: id, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
			t.Fatal(err)
		}
	}
	opts := handlerOptions{primitiveStore: store, actorRegistry: registry}
	report := map[string]any{"kind": reports.Kind, "schema_version": 1, "title": "Dashboard", "summary": "Context", "generated_at": "2026-01-01T00:00:00Z", "projects": []any{map[string]any{"id": "workspace", "title": "Workspace", "summary": "Current", "outcome": "Ship"}}, "sources": []any{}, "panels": []any{map[string]any{"id": "note", "project_id": "workspace", "type": "explanation", "title": "Context", "author": "author", "provenance": "reported", "observed_at": nil, "freshness": "unknown", "source_ids": []any{}, "data": map[string]any{"text": "Narrative"}}}}
	raw, _ := json.Marshal(report)
	doc, rev, err := store.CreateDocument(ctx, "writer", map[string]any{"title": "Dashboard"}, string(raw), "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	panels, err := reports.ParseAll(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/", nil)
	cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: "writer"})
	attachResourceAccessScope(req, opts)
	if err := checkReportReviews(req, opts, doc, rev, panels); err != nil {
		t.Fatal(err)
	}
	items, err := store.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{RecipientActorID: "author"})
	if err != nil || len(items) != 0 {
		t.Fatal("unpinned report created reminder")
	}
	if err := store.SetWorkspaceDashboard(ctx, "writer", anyString(doc["id"])); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := checkReportReviews(req, opts, doc, rev, panels); err != nil {
			t.Fatal(err)
		}
	}
	items, err = store.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{RecipientActorID: "author"})
	if err != nil || len(items) != 1 {
		t.Fatalf("reminders: %#v %v", items, err)
	}
	item := items[0]
	if item.Data["recipient_actor_id"] != "author" {
		t.Fatal("writer stole author's reminder")
	}
	privateDoc, _, err := store.CreateDocument(ctx, "reader", map[string]any{"title": "Private evidence"}, "Confidential", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PatchThread(ctx, "reader", anyString(privateDoc["thread_id"]), map[string]any{"pm_actor_id": "reader"}, nil); err != nil {
		t.Fatal(err)
	}
	subjectRead := req.Clone(ctx)
	cacheAuthenticatedPrincipal(subjectRead, &auth.Principal{ActorID: "author"})
	attachResourceAccessScope(subjectRead, opts)
	if inboxItemAccessible(subjectRead, opts, item.ThreadID, map[string]any{"recipient_actor_id": "author", "related_refs": []string{"document:" + anyString(privateDoc["id"])}}) {
		t.Fatal("legacy fallback bypassed canonical private document evidence")
	}
	for _, filter := range []primitives.DerivedInboxListFilter{{}, {ThreadID: anyString(doc["thread_id"])}, {RecipientActorID: "writer"}} {
		rows, err := store.ListDerivedInboxItems(ctx, filter)
		if err != nil || len(rows) != 0 {
			t.Fatalf("composite/other reader leaked reminder: %#v %v", rows, err)
		}
	}
	if inboxItemAccessible(req, opts, item.ThreadID, item.Data) {
		t.Fatal("direct inbox read leaked reminder")
	}
	if err := store.ReplaceDerivedInboxItems(ctx, item.ThreadID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetDerivedInboxItem(ctx, item.ID); err != nil {
		t.Fatalf("projection rebuild removed durable dedupe: %v", err)
	}
	// Target privacy is checked independently of an authorized reader.
	if _, err := store.PatchThread(ctx, "writer", item.ThreadID, map[string]any{"pm_actor_id": "reader"}, nil); err != nil {
		t.Fatal(err)
	}
	authorRead := req.Clone(ctx)
	cacheAuthenticatedPrincipal(authorRead, &auth.Principal{ActorID: "author"})
	attachResourceAccessScope(authorRead, opts)
	if inboxItemAccessible(authorRead, opts, item.ThreadID, item.Data) {
		t.Fatal("recipient retained access after subject became private")
	}
	if _, err := store.PatchThread(ctx, "writer", item.ThreadID, map[string]any{"pm_actor_id": ""}, nil); err != nil {
		t.Fatal(err)
	}
	report["summary"] = "Revised narrative"
	raw, _ = json.Marshal(report)
	_, newRev, err := store.UpdateDocument(ctx, "writer", anyString(doc["id"]), nil, anyString(rev["revision_id"]), string(raw), "text", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetDerivedInboxItem(ctx, item.ID); err != primitives.ErrNotFound {
		t.Fatalf("obsolete revision remained visible: %v", err)
	}
	if err := checkReportReviews(req, opts, doc, newRev, panels); err != nil {
		t.Fatal(err)
	}
	items, err = store.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{RecipientActorID: "author"})
	if err != nil || len(items) != 1 {
		t.Fatalf("new revision reminder: %#v %v", items, err)
	}
	board, err := store.CreateBoard(ctx, "reader", map[string]any{"title": "Private board"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := store.CreateWork(ctx, "reader", anyString(board["id"]), map[string]any{"title": "Legacy document parent"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.DB().Exec(`UPDATE cards SET thread_id='',parent_thread_id=? WHERE id=?`, item.ThreadID, anyString(card["id"])); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PatchThread(ctx, "reader", anyString(board["thread_id"]), map[string]any{"pm_actor_id": "reader"}, nil); err != nil {
		t.Fatal(err)
	}
	if inboxItemAccessible(authorRead, opts, item.ThreadID, items[0].Data) {
		t.Fatal("private containing board leaked through legacy parent")
	}
	var before int
	if err := ws.DB().QueryRow(`SELECT COUNT(*) FROM derived_inbox_items WHERE json_extract(data_json,'$.subtype')='report_review_due'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	report["summary"] = "Another revision"
	raw, _ = json.Marshal(report)
	_, latestRev, err := store.UpdateDocument(ctx, "writer", anyString(doc["id"]), nil, anyString(newRev["revision_id"]), string(raw), "text", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkReportReviews(req, opts, doc, latestRev, panels); err != nil {
		t.Fatal(err)
	}
	var after int
	if err := ws.DB().QueryRow(`SELECT COUNT(*) FROM derived_inbox_items WHERE json_extract(data_json,'$.subtype')='report_review_due'`).Scan(&after); err != nil || before != after {
		t.Fatal("reminder created for author denied by containing board")
	}
	if err := store.SetWorkspaceDashboard(ctx, "writer", ""); err != nil {
		t.Fatal(err)
	}
	items, err = store.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{RecipientActorID: "author"})
	if err != nil || len(items) != 0 {
		t.Fatal("unpinned reminder remained visible")
	}
}

func TestReportMaterializationIncludesAuthoredReviewMetadata(t *testing.T) {
	panels := []reports.Panel{{ID: "note", Type: "explanation", Author: "author", AuthoredAt: "2026-01-01T00:00:00Z", ReviewBy: "2026-01-08T00:00:00Z", ReviewByDefaulted: true, StaticData: map[string]any{"text": "Narrative"}}}
	_, results := materializeReportPanels(httptest.NewRequest("GET", "/", nil), handlerOptions{}, panels)
	if len(results) != 1 || results[0]["provenance_class"] != "authored" || results[0]["review_due"] != true || results[0]["review_by_defaulted"] != true || results[0]["authored_at"] != panels[0].AuthoredAt {
		t.Fatalf("metadata: %#v", results)
	}
}

func TestLiveCardsFiltersStoredLabelsRolesAndStatus(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	store := primitives.NewStore(ws.DB(), blob.NewFilesystemBackend(ws.Layout().ArtifactContentDir), ws.Layout().ArtifactContentDir)
	board, err := store.CreateBoard(ctx, "actor", map[string]any{"title": "Delivery"})
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range []struct{ title, role, phase string }{{"B release", "reviewer", "ready"}, {"A release", "reviewer", "ready"}, {"C unrelated", "executor", "ready"}, {"D cancelled", "reviewer", "cancelled"}} {
		if _, err := store.CreateWork(ctx, "actor", anyString(board["id"]), map[string]any{"title": spec.title, "labels": []string{"release"}, "roles": []string{spec.role}, "phase": spec.phase}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ws.DB().Exec(`UPDATE cards SET column_key='cancelled' WHERE title='D cancelled'`); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.DB().Exec(`UPDATE ref_edges SET metadata_json=json_set(metadata_json,'$.column_key','cancelled') WHERE edge_type='board_card' AND target_id IN (SELECT id FROM cards WHERE title='D cancelled')`); err != nil {
		t.Fatal(err)
	}
	reader := reportReader{r: httptest.NewRequest("GET", "/", nil), opts: handlerOptions{primitiveStore: store}, now: time.Now(), visibility: map[string]bool{}}
	data, truncated, err := reader.materialize(reports.Panel{Type: "live-cards", Query: reports.Query{BoardRefs: []string{anyString(board["ref"])}, Label: "release", Role: "reviewer", Status: "ready", Limit: 1, Sort: "title"}})
	if err != nil {
		t.Fatal(err)
	}
	items := data["items"].([]map[string]any)
	if len(items) != 1 || items[0]["title"] != "A release" || !truncated {
		t.Fatalf("filtered result: %#v %v", items, truncated)
	}
	data, truncated, err = reader.materialize(reports.Panel{Type: "live-cards", Query: reports.Query{Label: "release", Role: "reviewer", Status: "cancelled", Limit: 10, Sort: "title"}})
	if err != nil {
		t.Fatal(err)
	}
	items = data["items"].([]map[string]any)
	if len(items) != 1 || items[0]["title"] != "D cancelled" || truncated {
		t.Fatalf("closed card query: %#v %v", items, truncated)
	}
	if _, err := store.PatchThread(ctx, "actor", anyString(board["thread_id"]), map[string]any{"pm_actor_id": "private-owner"}, nil); err != nil {
		t.Fatal(err)
	}
	reader = reportReader{r: httptest.NewRequest("GET", "/", nil), opts: handlerOptions{primitiveStore: store}, now: time.Now(), visibility: map[string]bool{}}
	data, _, err = reader.materialize(reports.Panel{Type: "live-cards", Query: reports.Query{Limit: 10}})
	if err != nil || len(data["items"].([]map[string]any)) != 0 {
		t.Fatalf("private board leaked: %#v %v", data, err)
	}
}
