package primitives_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
)

const validVisualReport = `{"kind":"anx.visual-report","schema_version":1,"title":"Demo dashboard","summary":"Seven initiatives","generated_at":"2026-10-04T12:00:00Z","projects":[{"id":"demo","title":"Demo","summary":"Build","outcome":"Launch"}],"sources":[],"panels":[{"id":"note","project_id":"demo","type":"explanation","title":"Progress","author":"Test","provenance":"reported","observed_at":null,"freshness":"unavailable","source_ids":[],"data":{"text":"3 of 7 ready"}}]}`

const invalidVisualReport = `{"kind":"anx.visual-report","schema_version":1}`

func TestDocumentWriteRejectsInvalidVisualReport(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()
	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	ctx := context.Background()

	for _, body := range []string{
		invalidVisualReport,
		`{"kind":"ANX.VISUAL-REPORT","schema_version":1}`,
		`{"kind":" anx.visual-report ","schema_version":1}`,
		"---\nkind: anx.visual-report\n---\n{\"kind\":\"anx.visual-report\",\"schema_version\":1}\n",
	} {
		_, _, err = store.CreateDocument(ctx, "actor-1", map[string]any{"title": "Broken"}, body, "text", nil)
		var reportErr *primitives.VisualReportValidationError
		if !errors.As(err, &reportErr) || len(reportErr.Errors) == 0 || !strings.Contains(err.Error(), "visual report validation failed") {
			t.Fatalf("create %s: %v", body, err)
		}
	}
	if _, _, err = store.CreateDocument(ctx, "actor-1", map[string]any{"title": "Notes"}, "---\nkind: note\n---\nhello\n", "text", nil); err != nil {
		t.Fatalf("non-report front matter was refused: %v", err)
	}
	doc, rev, err := store.CreateDocument(ctx, "actor-1", map[string]any{"title": "Notes"}, "plain notes", "text", nil)
	if err != nil {
		t.Fatalf("create notes: %v", err)
	}
	documentID := anyString(doc["id"])
	revisionID := anyString(rev["revision_id"])
	_, _, err = store.UpdateDocument(ctx, "actor-1", documentID, nil, revisionID, invalidVisualReport, "text", nil, nil)
	var reportErr *primitives.VisualReportValidationError
	if !errors.As(err, &reportErr) || len(reportErr.Errors) == 0 || !strings.Contains(err.Error(), "visual report validation failed") {
		t.Fatalf("revise invalid report: %v", err)
	}
	_, head, err := store.GetDocument(ctx, documentID)
	if err != nil {
		t.Fatalf("get document: %v", err)
	}
	if anyString(head["content"]) != "plain notes" {
		t.Fatalf("invalid revision was stored: %#v", head["content"])
	}
	if _, updated, err := store.UpdateDocument(ctx, "actor-1", documentID, nil, revisionID, validVisualReport, "text", nil, nil); err != nil || anyString(updated["content"]) != validVisualReport {
		t.Fatalf("revise valid report: %v content=%#v", err, updated["content"])
	}
}
