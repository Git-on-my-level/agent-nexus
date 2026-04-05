package primitives

import (
	"context"
	"database/sql"
	"reflect"
	"sort"
	"strings"
	"testing"

	"organization-autorunner-core/internal/blob"
	"organization-autorunner-core/internal/storage"
)

func TestTypedRefHelpersLifecycleAndConcurrency(t *testing.T) {
	t.Parallel()

	targets := typedRefEdgeTargets(refEdgeTypeRef, []string{
		" customprefix:abc ",
		"thread:thread-1",
		"customprefix:abc",
		"malformed",
	})
	if got, want := len(targets), 2; got != want {
		t.Fatalf("typedRefEdgeTargets length: got %d want %d", got, want)
	}
	if !reflect.DeepEqual(targets[0], refEdgeTarget{TargetType: "customprefix", TargetID: "abc", EdgeType: refEdgeTypeRef}) {
		t.Fatalf("unexpected first typed ref target: %#v", targets[0])
	}
	if !reflect.DeepEqual(targets[1], refEdgeTarget{TargetType: "thread", TargetID: "thread-1", EdgeType: refEdgeTypeRef}) {
		t.Fatalf("unexpected second typed ref target: %#v", targets[1])
	}

	body := map[string]any{
		"archived_at":      "stale",
		"archived_by":      "stale",
		"tombstoned_at":    "stale",
		"tombstoned_by":    "stale",
		"tombstone_reason": "stale",
	}
	applyArchivedLifecycle(body, "2026-04-04T00:00:00Z", "actor-1")
	if got := body["archived_at"]; got != "2026-04-04T00:00:00Z" {
		t.Fatalf("applyArchivedLifecycle archived_at: %#v", got)
	}
	if _, exists := body["tombstoned_at"]; exists {
		t.Fatalf("applyArchivedLifecycle should clear tombstone fields: %#v", body)
	}

	applyTombstonedLifecycle(body, "2026-04-05T00:00:00Z", "actor-2", "cleanup")
	if got := body["tombstoned_at"]; got != "2026-04-05T00:00:00Z" {
		t.Fatalf("applyTombstonedLifecycle tombstoned_at: %#v", got)
	}
	if _, exists := body["archived_at"]; exists {
		t.Fatalf("applyTombstonedLifecycle should clear archived fields: %#v", body)
	}

	clearTombstonedLifecycle(body, "", "")
	for _, key := range []string{"archived_at", "archived_by", "tombstoned_at", "tombstoned_by", "tombstone_reason"} {
		if _, exists := body[key]; exists {
			t.Fatalf("clearTombstonedLifecycle should clear %s: %#v", key, body)
		}
	}

	provenance, provenanceJSON, err := marshalProvenance(map[string]any{
		"sources": []string{"inferred"},
	}, "test marshal")
	if err != nil {
		t.Fatalf("marshalProvenance: %v", err)
	}
	provenance = setProvenanceFieldLabels(provenance, "status", []string{"decision:event-1"})
	if got := provenance["by_field"].(map[string]any)["status"]; !reflect.DeepEqual(got, []string{"decision:event-1"}) {
		t.Fatalf("setProvenanceFieldLabels status: %#v", got)
	}
	if provenanceJSON == "" {
		t.Fatal("marshalProvenance returned empty JSON")
	}

	if err := ensureUpdatedAtMatches("2026-04-04T00:00:00Z", stringPointer("2026-04-04T00:00:00Z")); err != nil {
		t.Fatalf("ensureUpdatedAtMatches match: %v", err)
	}
	if err := ensureUpdatedAtMatches("2026-04-04T00:00:00Z", stringPointer("2026-04-05T00:00:00Z")); err != ErrConflict {
		t.Fatalf("ensureUpdatedAtMatches conflict: got %v want %v", err, ErrConflict)
	}
}

func TestSharedResourceWritesIndexRefEdges(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)

	primaryThreadID := createPrimitiveTestThread(t, ctx, store, "Primary")
	cardThreadID := createPrimitiveTestThread(t, ctx, store, "Card")
	document, revision, err := store.CreateDocument(ctx, "actor-1", map[string]any{
		"id":        "doc-edge-1",
		"thread_id": primaryThreadID,
		"title":     "Edge doc",
	}, "document body", "text", []string{"customprefix:doc-ref"})
	if err != nil {
		t.Fatalf("create document: %v", err)
	}
	documentID := document["id"].(string)
	revisionID := revision["revision_id"].(string)

	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{
		"id":            "board-edge-1",
		"title":         "Edge board",
		"document_refs": []string{"document:" + documentID},
		"pinned_refs":   []string{"customprefix:board-ref"},
	})
	if err != nil {
		t.Fatalf("create board: %v", err)
	}
	boardID := board["id"].(string)
	boardThreadID := board["thread_id"].(string)

	cardResult, err := store.CreateBoardCard(ctx, "actor-2", boardID, AddBoardCardInput{
		CardID:           "card-edge-1",
		Title:            "Edge card",
		ParentThreadID:   cardThreadID,
		PinnedDocumentID: stringPointer(documentID),
		Status:           "todo",
	})
	if err != nil {
		t.Fatalf("create board card: %v", err)
	}
	cardID := cardResult.Card["id"].(string)

	event, err := store.AppendEvent(ctx, "actor-3", map[string]any{
		"id":        "event-edge-1",
		"type":      "note_added",
		"thread_id": primaryThreadID,
		"refs":      []string{"thread:" + primaryThreadID, "customprefix:event-ref"},
		"payload":   map[string]any{"text": "edge"},
	})
	if err != nil {
		t.Fatalf("append event: %v", err)
	}
	eventID := event["id"].(string)

	assertRefEdges(t, workspace.DB(), "document", documentID, []string{
		refEdgeTypeDocumentThread + "|thread|" + primaryThreadID,
		refEdgeTypeRef + "|customprefix|doc-ref",
		refEdgeTypeRef + "|thread|" + primaryThreadID,
	})
	assertRefEdges(t, workspace.DB(), "document_revision", revisionID, []string{
		refEdgeTypeRef + "|customprefix|doc-ref",
		refEdgeTypeRef + "|thread|" + primaryThreadID,
	})
	assertRefEdges(t, workspace.DB(), "board", boardID, []string{
		refEdgeTypeBoardCard + "|card|" + cardID,
		refEdgeTypeRef + "|customprefix|board-ref",
		refEdgeTypeRef + "|document|" + documentID,
	})
	assertRefEdges(t, workspace.DB(), "thread", boardThreadID, []string{
		refEdgeTypeRef + "|board|" + boardID,
	})
	assertRefEdges(t, workspace.DB(), "card", cardID, []string{
		refEdgeTypeCardParentThread + "|thread|" + cardThreadID,
		refEdgeTypeCardPinnedDocument + "|document|" + documentID,
	})
	assertRefEdges(t, workspace.DB(), "event", eventID, []string{
		refEdgeTypeRef + "|customprefix|event-ref",
		refEdgeTypeRef + "|thread|" + primaryThreadID,
	})
}

func TestRefEdgesBackArtifactAndEventReverseLookups(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	threadID := createPrimitiveTestThread(t, ctx, store, "Lookup")

	artifact, err := store.CreateArtifact(ctx, "actor-1", map[string]any{
		"id":   "artifact-edge-1",
		"kind": "blob",
		"refs": []string{"thread:" + threadID, "thread:secondary-thread"},
	}, "artifact body", "text")
	if err != nil {
		t.Fatalf("create artifact: %v", err)
	}
	artifactID := artifact["id"].(string)
	if _, err := workspace.DB().ExecContext(ctx, `UPDATE artifacts SET refs_json = '[]' WHERE id = ?`, artifactID); err != nil {
		t.Fatalf("clear artifact refs_json: %v", err)
	}

	artifacts, err := store.ListArtifacts(ctx, ArtifactListFilter{ThreadID: "secondary-thread"})
	if err != nil {
		t.Fatalf("list artifacts by secondary thread: %v", err)
	}
	if len(artifacts) != 1 || artifacts[0]["id"] != artifactID {
		t.Fatalf("artifact thread reverse lookup should use ref_edges, got %#v", artifacts)
	}

	parent, err := store.AppendEvent(ctx, "actor-1", map[string]any{
		"id":        "event-parent-edge",
		"type":      "message_posted",
		"thread_id": threadID,
		"refs":      []string{"thread:" + threadID},
		"payload":   map[string]any{"text": "root"},
	})
	if err != nil {
		t.Fatalf("append parent: %v", err)
	}
	parentID := parent["id"].(string)

	child, err := store.AppendEvent(ctx, "actor-1", map[string]any{
		"id":        "event-child-edge",
		"type":      "message_posted",
		"thread_id": threadID,
		"refs":      []string{"thread:" + threadID, "event:" + parentID},
		"payload":   map[string]any{"text": "child"},
	})
	if err != nil {
		t.Fatalf("append child: %v", err)
	}
	childID := child["id"].(string)
	if _, err := workspace.DB().ExecContext(ctx, `UPDATE events SET refs_json = '[]' WHERE id = ?`, childID); err != nil {
		t.Fatalf("clear child refs_json: %v", err)
	}

	if _, err := store.ArchiveEvent(ctx, "actor-2", parentID); err != nil {
		t.Fatalf("archive parent: %v", err)
	}
	childEvent, err := store.GetEvent(ctx, childID)
	if err != nil {
		t.Fatalf("get child after archive: %v", err)
	}
	if childEvent["archived_at"] == nil {
		t.Fatalf("event descendant archive should use ref_edges, got %#v", childEvent)
	}
}

func createPrimitiveTestThread(t *testing.T, ctx context.Context, store *Store, title string) string {
	t.Helper()

	result, err := store.CreateThread(ctx, "actor-1", map[string]any{
		"title":           title,
		"type":            "incident",
		"status":          "active",
		"priority":        "p1",
		"tags":            []string{},
		"cadence":         "reactive",
		"current_summary": title,
		"next_actions":    []string{},
		"key_artifacts":   []string{},
		"provenance":      map[string]any{"sources": []string{"inferred"}},
	})
	if err != nil {
		t.Fatalf("create thread %q: %v", title, err)
	}
	return result.Thread["id"].(string)
}

func assertRefEdges(t *testing.T, db *sql.DB, sourceType, sourceID string, expected []string) {
	t.Helper()

	rows, err := db.Query(`SELECT edge_type, target_type, target_id FROM ref_edges WHERE source_type = ? AND source_id = ?`, sourceType, sourceID)
	if err != nil {
		t.Fatalf("query ref edges for %s %s: %v", sourceType, sourceID, err)
	}
	defer rows.Close()

	got := make([]string, 0)
	for rows.Next() {
		var edgeType string
		var targetType string
		var targetID string
		if err := rows.Scan(&edgeType, &targetType, &targetID); err != nil {
			t.Fatalf("scan ref edge row: %v", err)
		}
		got = append(got, edgeType+"|"+targetType+"|"+targetID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate ref edge rows: %v", err)
	}

	sort.Strings(got)
	sort.Strings(expected)
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("unexpected ref edges for %s %s: got %#v want %#v", sourceType, sourceID, got, expected)
	}
}

func stringPointer(value string) *string {
	value = strings.TrimSpace(value)
	return &value
}
