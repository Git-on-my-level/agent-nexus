package primitives

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agent-nexus-core/internal/storage"
)

func TestRound6CanonicalResolutionRefs(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	board, err := s.CreateBoard(ctx, "actor", map[string]any{"title": "Evidence"})
	if err != nil {
		t.Fatal(err)
	}
	boardID := board["id"].(string)
	artifact, err := s.CreateArtifact(ctx, "actor", map[string]any{"kind": "text", "refs": []string{}, "title": "Evidence report"}, "done", "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	artifactID := artifact["id"].(string)
	artifactRef := "artifact:" + artifactID
	event, err := s.AppendEvent(ctx, "actor", map[string]any{"type": "completion_evidence", "refs": []string{}, "summary": "Completion checked"})
	if err != nil {
		t.Fatal(err)
	}
	eventRef := "event:" + event["id"].(string)
	doc, rev, err := s.CreateDocument(ctx, "actor", map[string]any{"title": "Acceptance notes"}, "# Accepted", "text", []string{})
	if err != nil {
		t.Fatal(err)
	}
	revisionRef := "document_revision:" + rev["revision_id"].(string)
	for _, tc := range []struct{ ref, title string }{{artifactRef, "Evidence report"}, {eventRef, "Completion checked"}, {revisionRef, "Acceptance notes"}, {"document:" + doc["id"].(string), "Acceptance notes"}} {
		got, err := s.ResolveResolutionRef(ctx, tc.ref)
		if err != nil || !got.Exists || got.TitleOrSummary != tc.title {
			t.Fatalf("%s: %+v %v", tc.ref, got, err)
		}
	}
	for _, ref := range []string{"artifact:missing", "event:missing", "document_revision:missing", "custom:missing", "not-a-ref"} {
		got, err := s.ResolveResolutionRef(ctx, ref)
		if err != nil || got.Exists {
			t.Fatalf("%s: %+v %v", ref, got, err)
		}
	}
	// Valid creation and movement accept actual event/artifact evidence plus a revision.
	made, err := s.AddBoardCard(ctx, "actor", boardID, AddBoardCardInput{Title: "Already accepted", ColumnKey: "done", ResolutionRefs: []string{eventRef, revisionRef}})
	if err != nil {
		t.Fatal(err)
	}
	work, err := s.CreateWork(ctx, "actor", boardID, map[string]any{"title": "To complete"})
	if err != nil {
		t.Fatal(err)
	}
	id := work["id"].(string)
	refs := []string{artifactRef, revisionRef}
	if _, err := s.MoveBoardCard(ctx, "actor", boardID, id, MoveBoardCardInput{ColumnKey: "done", ResolutionRefs: &refs}); err != nil {
		t.Fatal(err)
	}
	// Missing supplementary refs also fail; one existing artifact does not excuse them.
	bad := []string{artifactRef, "document_revision:missing"}
	if _, err := s.UpdateBoardCard(ctx, "actor", boardID, id, UpdateBoardCardInput{ResolutionRefs: &bad}); !errors.Is(err, ErrInvalidBoardRequest) || !strings.Contains(err.Error(), bad[1]) {
		t.Fatalf("update accepted missing ref: %v", err)
	}
	if _, err := s.AddBoardCard(ctx, "actor", boardID, AddBoardCardInput{Title: "Invalid evidence", ColumnKey: "done", ResolutionRefs: bad}); !errors.Is(err, ErrInvalidBoardRequest) {
		t.Fatalf("insert accepted missing ref: %v", err)
	}
	// Trashing a revision's parent or content invalidates the revision.
	if _, err := ws.DB().Exec(`UPDATE documents SET trashed_at='2026-09-13' WHERE id=?`, doc["id"]); err != nil {
		t.Fatal(err)
	}
	got, err := s.ResolveResolutionRef(ctx, revisionRef)
	if err != nil || got.Exists {
		t.Fatalf("trashed parent: %+v %v", got, err)
	}
	if _, err := ws.DB().Exec(`UPDATE documents SET trashed_at=NULL WHERE id=?`, doc["id"]); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.DB().Exec(`UPDATE artifacts SET trashed_at='2026-09-13' WHERE id=?`, rev["artifact_id"]); err != nil {
		t.Fatal(err)
	}
	got, err = s.ResolveResolutionRef(ctx, revisionRef)
	if err != nil || got.Exists {
		t.Fatalf("trashed revision content: %+v %v", got, err)
	}
	if _, err := s.TrashArtifact(ctx, "actor", artifactID, "obsolete"); err != nil {
		t.Fatal(err)
	}
	got, err = s.ResolveResolutionRef(ctx, artifactRef)
	if err != nil || got.Exists || got.TitleOrSummary != "" {
		t.Fatalf("trashed artifact: %+v %v", got, err)
	}
	if _, err := s.MoveBoardCard(ctx, "actor", boardID, id, MoveBoardCardInput{ColumnKey: "done"}); !errors.Is(err, ErrInvalidBoardRequest) {
		t.Fatalf("retained trashed refs: %v", err)
	}
	if _, err := ws.DB().Exec(`DELETE FROM events WHERE id=?`, event["id"]); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateBoardCard(ctx, "actor", boardID, made.Card["id"].(string), UpdateBoardCardInput{Title: stringPointer("Retitle")}); !errors.Is(err, ErrInvalidBoardRequest) {
		t.Fatalf("update with deleted refs: %v", err)
	}
	// Every rejection leaves durable card fields unchanged.
	after, err := s.GetWork(ctx, id)
	if err != nil || after["title"] != work["title"] || after["phase"] != "done" {
		t.Fatalf("%+v %v", after, err)
	}
}
