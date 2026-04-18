package primitives

import (
	"context"
	"strings"
	"testing"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/storage"
)

func TestBuildListDocumentsQueryStateFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		filter       DocumentListFilter
		wantFragment string
		wantNot      []string
	}{
		{
			name:         "active",
			filter:       DocumentListFilter{State: "active"},
			wantFragment: "d.archived_at IS NULL AND d.trashed_at IS NULL",
		},
		{
			name:         "archived",
			filter:       DocumentListFilter{State: "archived"},
			wantFragment: "d.archived_at IS NOT NULL AND d.trashed_at IS NULL",
		},
		{
			name:         "trashed",
			filter:       DocumentListFilter{State: "trashed"},
			wantFragment: "d.trashed_at IS NOT NULL",
		},
		{
			name: "state active overrides trashed_only",
			filter: DocumentListFilter{
				State:       "active",
				TrashedOnly: true,
			},
			wantFragment: "d.archived_at IS NULL AND d.trashed_at IS NULL",
			wantNot:      []string{"d.trashed_at IS NOT NULL"},
		},
		{
			name: "legacy trashed_only when state empty",
			filter: DocumentListFilter{
				TrashedOnly: true,
			},
			wantFragment: "d.trashed_at IS NOT NULL",
			wantNot:      []string{"d.archived_at IS NULL AND d.trashed_at IS NULL"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			query, _ := buildListDocumentsQuery(tt.filter)
			if !strings.Contains(query, tt.wantFragment) {
				t.Fatalf("query missing expected fragment %q:\n%s", tt.wantFragment, query)
			}
			for _, frag := range tt.wantNot {
				if strings.Contains(query, frag) {
					t.Fatalf("query unexpectedly contains %q:\n%s", frag, query)
				}
			}
		})
	}
}

func TestBuildListDocumentsQueryLabelsOR(t *testing.T) {
	t.Parallel()

	filter := DocumentListFilter{
		Labels: []string{"  alpha ", "beta", "", "alpha"},
	}
	query, args := buildListDocumentsQuery(filter)

	wantSub := "EXISTS (SELECT 1 FROM json_each(d.labels_json) WHERE value = ?) OR EXISTS (SELECT 1 FROM json_each(d.labels_json) WHERE value = ?)"
	if !strings.Contains(query, wantSub) {
		t.Fatalf("expected label OR clause in query, got:\n%s", query)
	}
	if len(args) != 2 || args[0] != "alpha" || args[1] != "beta" {
		t.Fatalf("expected args [alpha beta], got %#v", args)
	}
}

func TestBuildListDocumentsQueryInvalidStateDefensive(t *testing.T) {
	t.Parallel()

	query, _ := buildListDocumentsQuery(DocumentListFilter{State: "unknown"})
	if !strings.Contains(query, "1=0") {
		t.Fatalf("expected defensive empty match for invalid state, got:\n%s", query)
	}
}

func TestStoreListDocumentsFiltersByLabelJSON(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := NewStore(
		workspace.DB(),
		blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir),
		workspace.Layout().ArtifactContentDir,
	)

	threadResult, err := store.CreateThread(ctx, "actor-1", map[string]any{
		"id":               "thread-doc-label-int",
		"title":            "doc label test thread",
		"type":             "initiative",
		"status":           "active",
		"priority":         "p2",
		"tags":             []string{},
		"cadence":          "reactive",
		"next_check_in_at": "2026-03-20T00:00:00Z",
		"current_summary":  "s",
		"next_actions":     []string{},
		"key_artifacts":    []string{},
		"provenance":       map[string]any{"sources": []string{"inferred"}},
	})
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	threadID, _ := threadResult.Thread["id"].(string)

	if _, _, err := store.CreateDocument(ctx, "actor-1", map[string]any{
		"id":        "doc-label-only-beta",
		"thread_id": threadID,
		"title":     "Beta only",
		"labels":    []string{"beta"},
	}, "b", "text", []string{"thread:" + threadID}); err != nil {
		t.Fatalf("create doc beta: %v", err)
	}
	// Second lineage gets its own backing thread (cannot reuse thread_id from another doc).
	if _, _, err := store.CreateDocument(ctx, "actor-1", map[string]any{
		"id":     "doc-label-alpha-shared",
		"title":  "Alpha doc",
		"labels": []string{"alpha", "beta"},
	}, "ab", "text", nil); err != nil {
		t.Fatalf("create doc alpha: %v", err)
	}

	docs, _, err := store.ListDocuments(ctx, DocumentListFilter{Labels: []string{"alpha"}})
	if err != nil {
		t.Fatalf("list by label alpha: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 document for label alpha, got %d (%#v)", len(docs), docIDs(docs))
	}
	if id, _ := docs[0]["id"].(string); id != "doc-label-alpha-shared" {
		t.Fatalf("expected doc-label-alpha-shared, got %q", id)
	}

	docsOR, _, err := store.ListDocuments(ctx, DocumentListFilter{Labels: []string{"alpha", "gamma"}})
	if err != nil {
		t.Fatalf("list OR labels: %v", err)
	}
	if len(docsOR) != 1 {
		t.Fatalf("expected 1 document for alpha OR gamma, got %d", len(docsOR))
	}
}

func docIDs(docs []map[string]any) []string {
	out := make([]string, 0, len(docs))
	for _, d := range docs {
		if id, ok := d["id"].(string); ok {
			out = append(out, id)
		}
	}
	return out
}
