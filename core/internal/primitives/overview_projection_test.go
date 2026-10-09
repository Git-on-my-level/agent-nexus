package primitives_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/primitives"
)

type countingReportBackend struct {
	blob.Backend
	reads int
}

func (b *countingReportBackend) Read(ctx context.Context, hash string) ([]byte, error) {
	b.reads++
	return b.Backend.Read(ctx, hash)
}

func TestOverviewDefersReportSelectorBlobReads(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	backend := &countingReportBackend{Backend: blob.NewFilesystemBackend(ws.Layout().ArtifactContentDir)}
	store := primitives.NewStore(ws.DB(), backend, ws.Layout().ArtifactContentDir)
	report := `{"kind":"anx.visual-report","schema_version":1,"title":"Dashboard","summary":"Launch","generated_at":"2026-10-04T12:00:00Z","projects":[{"id":"launch","title":"Launch","summary":"Ship","outcome":"Delivery"}],"sources":[],"panels":[{"id":"progress","project_id":"launch","type":"live-initiatives","title":"Progress","author":"Test","provenance":"reported","observed_at":null,"freshness":"unknown","source_ids":[],"data":{}}]}`
	old, _, err := store.CreateDocument(ctx, "actor", map[string]any{"title": "Old report"}, report, "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 109; i++ {
		if _, _, err = store.CreateDocument(ctx, "actor", map[string]any{"title": fmt.Sprintf("Note %d", i)}, "Plain notes", "text", nil); err != nil {
			t.Fatal(err)
		}
	}
	newest, _, err := store.CreateDocument(ctx, "actor", map[string]any{"title": "New report"}, report, "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	backend.reads = 0
	overview, err := store.Overview(ctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	dashboard := overview["dashboard"].(map[string]any)
	entries := dashboard["reports"].([]map[string]any)
	if backend.reads != 1 || len(entries) != 1 || entries[0]["id"] != newest["id"] || dashboard["has_more"] != true {
		t.Fatalf("selector read eagerly: reads=%d dashboard=%v", backend.reads, dashboard)
	}
	backend.reads = 0
	candidates, err := store.DashboardReports(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates["reports"].([]map[string]any)) != 1 || backend.reads != 100 || candidates["has_more"] != true {
		t.Fatalf("selector page: reads=%d %v", backend.reads, candidates)
	}
	cursor, _ := candidates["next_cursor"].(string)
	backend.reads = 0
	next, err := store.DashboardReportsPage(ctx, cursor)
	if err != nil || len(next["reports"].([]map[string]any)) != 1 || backend.reads != 11 || next["has_more"] != false {
		t.Fatalf("selector continuation: reads=%d %v %v", backend.reads, next, err)
	}
	if err = store.SetWorkspaceDashboard(ctx, "actor", old["id"].(string)); err != nil {
		t.Fatal(err)
	}
	backend.reads = 0
	overview, err = store.Overview(ctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	entries = overview["dashboard"].(map[string]any)["reports"].([]map[string]any)
	if backend.reads != 1 || entries[0]["id"] != old["id"] {
		t.Fatalf("pin read other reports: reads=%d %v", backend.reads, entries)
	}
}

func TestBulkWorkMatchesIndividualProjectionAndPagination(t *testing.T) {
	t.Parallel()
	store, board := newWorkTestStore(t)
	ctx := context.Background()
	if _, err := store.CreateWork(ctx, "actor-1", board, map[string]any{"title": "Native work", "next_actor": "human", "priority": "p1"}); err != nil {
		t.Fatal(err)
	}
	mirrored := registerWork(t, store, board)
	id := mirrored["id"].(string)
	for _, observation := range []map[string]any{
		{"idempotency_key": "good", "reader_id": "reader", "reader_revision": "v1", "observed_at": time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano), "status": "reported", "facts": map[string]any{"title": "Observed title", "phase": "review"}},
		{"idempotency_key": "failure", "reader_id": "reader", "reader_revision": "v1", "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "status": "error", "error": map[string]any{"code": "unreachable", "message": "Fixture"}},
	} {
		if _, err := store.SubmitWorkObservation(ctx, "actor-1", id, observation); err != nil {
			t.Fatal(err)
		}
	}
	all, err := store.ListAllWork(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatal(all)
	}
	for _, item := range all {
		single, err := store.GetWork(ctx, item["ref"].(string))
		if err != nil {
			t.Fatal(err)
		}
		a, _ := json.Marshal(item)
		b, _ := json.Marshal(single)
		if string(a) != string(b) {
			t.Fatalf("bulk diverged:\n%s\n%s", a, b)
		}
	}
	cursor := ""
	count := 0
	for {
		page, err := store.ListWork(ctx, primitives.WorkListFilter{Limit: 1, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Work) != 1 || page.Work[0]["ref"] != all[count]["ref"] {
			t.Fatal(page)
		}
		paged, _ := json.Marshal(page.Work[0])
		expected, _ := json.Marshal(all[count])
		if string(paged) != string(expected) {
			t.Fatalf("paged observation projection diverged:\n%s\n%s", paged, expected)
		}
		count++
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if count != len(all) {
		t.Fatalf("paged %d of %d", count, len(all))
	}
}
