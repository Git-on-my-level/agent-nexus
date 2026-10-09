package primitives_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"agent-nexus-visualreport"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/primitives"
)

const validVisualReport = `{"kind":"anx.visual-report","schema_version":1,"title":"Demo dashboard","summary":"Seven initiatives","generated_at":"2026-10-04T12:00:00Z","projects":[{"id":"demo","title":"Demo","summary":"Build","outcome":"Launch"}],"sources":[],"panels":[{"id":"note","project_id":"demo","type":"explanation","title":"Progress","author":"Test","provenance":"reported","observed_at":null,"freshness":"unavailable","source_ids":[],"data":{"text":"3 of 7 ready"}}]}`

const invalidVisualReport = `{"kind":"anx.visual-report","schema_version":1}`

func TestDocumentWriteMatchesReportReader(t *testing.T) {
	t.Parallel()

	oversized := validVisualReport[:len(validVisualReport)-1] + strings.Repeat(" ", visualreport.MaxBytes) + "}"
	corpus := []string{
		validVisualReport,
		invalidVisualReport,
		oversized,
		`{"kind":"ANX.VISUAL-REPORT","schema_version":1,"title":"Demo","summary":"x","generated_at":"2026-10-04T12:00:00Z","projects":[],"sources":[],"panels":[]}`,
		`{"kind":" anx.visual-report ","schema_version":1}`,
		"---\nkind: anx.visual-report\n---\n" + validVisualReport + "\n",
		"---\nkind: note\n---\n{\"kind\":\"anx.visual-report\",\"schema_version\":1}\n",
		"plain notes",
		`{"kind":"anx.visual-report",`,
		`{"kind":"ANX.VISUAL-REPORT",`,
		`{"kind":" anx.visual-report ",`,
	}

	workspace, err := initializeTestWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()
	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	ctx := context.Background()

	for _, body := range corpus {
		reader := visualreport.Validate([]byte(body))
		_, readErr := visualreport.Parse(body)
		if reader.Valid != (readErr == nil) {
			t.Fatalf("reader split on %q: valid=%v parse=%v", body, reader.Valid, readErr)
		}
		doc, rev, writeErr := store.CreateDocument(ctx, "actor-1", map[string]any{"title": "Report"}, body, "text", nil)
		if reader.Recognized && !reader.Valid {
			var reportErr *primitives.VisualReportValidationError
			if !errors.As(writeErr, &reportErr) || strings.Join(reportErr.Errors, "\n") != strings.Join(reader.Errors, "\n") {
				t.Fatalf("write %q: got %v, reader %#v", body, writeErr, reader)
			}
			continue
		}
		if writeErr != nil {
			t.Fatalf("reader ignored %q but write refused: %v", body, writeErr)
		}
		_, head, err := store.GetDocument(ctx, anyString(doc["id"]))
		if err != nil {
			t.Fatalf("get %q: %v", body, err)
		}
		stored, _ := head["content"].(string)
		if stored != body {
			t.Fatalf("stored bytes differ for %q", body)
		}
		storedReader := visualreport.Validate([]byte(stored))
		_, storedReadErr := visualreport.Parse(stored)
		if storedReader.Recognized != reader.Recognized || storedReader.Valid != reader.Valid || (storedReadErr == nil) != reader.Valid {
			t.Fatalf("stored read diverged for %q: before=%#v after=%#v parse=%v", body, reader, storedReader, storedReadErr)
		}
		if rev == nil {
			t.Fatal("missing revision")
		}
	}

	doc, rev, err := store.CreateDocument(ctx, "actor-1", map[string]any{"title": "Live"}, validVisualReport, "text", nil)
	if err != nil {
		t.Fatalf("seed valid report: %v", err)
	}
	documentID := anyString(doc["id"])
	revisionID := anyString(rev["revision_id"])
	_, _, err = store.UpdateDocument(ctx, "actor-1", documentID, nil, revisionID, oversized, "text", nil, nil)
	var reportErr *primitives.VisualReportValidationError
	if !errors.As(err, &reportErr) || !strings.Contains(strings.Join(reportErr.Errors, " "), "128 KiB") {
		t.Fatalf("oversized revision: %v", err)
	}
	_, head, err := store.GetDocument(ctx, documentID)
	if err != nil {
		t.Fatalf("get pinned report: %v", err)
	}
	if anyString(head["content"]) != validVisualReport {
		t.Fatal("oversized revision replaced the working report")
	}
	if _, err := visualreport.Parse(anyString(head["content"])); err != nil {
		t.Fatalf("working report no longer reads: %v", err)
	}
	if _, updated, err := store.UpdateDocument(ctx, "actor-1", documentID, nil, revisionID, invalidVisualReport, "text", nil, nil); err == nil || updated != nil {
		t.Fatalf("invalid revision stored: %v", err)
	}
}

func TestDocumentReviewDeadlineRejectedAtCreateAndRevise(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	workspace, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	now := time.Now().UTC()
	past := strings.Replace(validVisualReport, `"author":"Test"`, fmt.Sprintf(`"author":"Test","authored_at":%q,"review_by":%q`, now.Add(-48*time.Hour).Format(time.RFC3339Nano), now.Add(-24*time.Hour).Format(time.RFC3339Nano)), 1)
	if !visualreport.Validate([]byte(past)).Valid {
		t.Fatal("expired stored report did not validate for reading")
	}
	if _, _, err := store.CreateDocument(ctx, "actor", map[string]any{"title": "Report"}, past, "text", nil); err == nil {
		t.Fatal("expired deadline accepted on create")
	}
	doc, rev, err := store.CreateDocument(ctx, "actor", map[string]any{"title": "Report"}, validVisualReport, "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.UpdateDocument(ctx, "actor", anyString(doc["id"]), nil, anyString(rev["revision_id"]), past, "text", nil, nil); err == nil {
		t.Fatal("expired deadline accepted on revise")
	}
	_, current, err := store.GetDocument(ctx, anyString(doc["id"]))
	if err != nil || current["revision_id"] != rev["revision_id"] {
		t.Fatal("failed deadline write changed head")
	}
}

func TestDocumentRevisionRetainsExpiredPanelDeadline(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	workspace, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	due := time.Now().UTC().Add(2 * time.Second)
	content := strings.Replace(validVisualReport, `"author":"Test"`, fmt.Sprintf(`"author":"Test","authored_at":%q,"review_by":%q`, due.Add(-24*time.Hour).Format(time.RFC3339Nano), due.Format(time.RFC3339Nano)), 1)
	doc, revision, err := store.CreateDocument(ctx, "actor", map[string]any{"title": "Report"}, content, "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the actual create->expire->revise lifecycle without changing stored
	// bytes or bypassing validation to construct an already expired revision.
	time.Sleep(time.Until(due) + time.Millisecond)
	doc, revision, err = store.UpdateDocument(ctx, "actor", anyString(doc["id"]), map[string]any{"summary": "Unrelated metadata"}, anyString(revision["revision_id"]), content, "text", nil, nil)
	if err != nil {
		t.Fatalf("unchanged expired panel blocked metadata revision: %v", err)
	}
	content = strings.Replace(content, `"summary":"Seven initiatives"`, `"summary":"Other work changed"`, 1)
	_, revision, err = store.UpdateDocument(ctx, "actor", anyString(doc["id"]), nil, anyString(revision["revision_id"]), content, "text", nil, nil)
	if err != nil {
		t.Fatalf("unchanged expired panel blocked report summary revision: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(content), &root); err != nil {
		t.Fatal(err)
	}
	panel := root["panels"].([]any)[0].(map[string]any)
	panel["review_by"] = due.Add(-time.Second).Format(time.RFC3339Nano)
	changedDeadline, _ := json.Marshal(root)
	if _, _, err := store.UpdateDocument(ctx, "actor", anyString(doc["id"]), nil, anyString(revision["revision_id"]), string(changedDeadline), "text", nil, nil); err == nil {
		t.Fatal("changed past deadline accepted")
	}
	panel["review_by"] = due.Format(time.RFC3339Nano)
	added := make(map[string]any, len(panel))
	for key, value := range panel {
		added[key] = value
	}
	added["id"] = "new-note"
	root["panels"] = append(root["panels"].([]any), added)
	newExpiredPanel, _ := json.Marshal(root)
	if _, _, err := store.UpdateDocument(ctx, "actor", anyString(doc["id"]), nil, anyString(revision["revision_id"]), string(newExpiredPanel), "text", nil, nil); err == nil {
		t.Fatal("new panel inherited another panel's expired deadline exemption")
	}
	_, current, err := store.GetDocument(ctx, anyString(doc["id"]))
	if err != nil || current["revision_id"] != revision["revision_id"] {
		t.Fatal("rejected deadline revision changed the head")
	}
}
