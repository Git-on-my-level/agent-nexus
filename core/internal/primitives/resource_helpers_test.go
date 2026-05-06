package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/storage"
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
		"archived_at":  "stale",
		"archived_by":  "stale",
		"trashed_at":   "stale",
		"trashed_by":   "stale",
		"trash_reason": "stale",
	}
	applyArchivedLifecycle(body, "2026-04-04T00:00:00Z", "actor-1")
	if got := body["archived_at"]; got != "2026-04-04T00:00:00Z" {
		t.Fatalf("applyArchivedLifecycle archived_at: %#v", got)
	}
	if _, exists := body["trashed_at"]; exists {
		t.Fatalf("applyArchivedLifecycle should clear trash fields: %#v", body)
	}

	applyTrashedLifecycle(body, "2026-04-05T00:00:00Z", "actor-2", "cleanup")
	if got := body["trashed_at"]; got != "2026-04-05T00:00:00Z" {
		t.Fatalf("applyTrashedLifecycle trashed_at: %#v", got)
	}
	if _, exists := body["archived_at"]; exists {
		t.Fatalf("applyTrashedLifecycle should clear archived fields: %#v", body)
	}

	clearTrashedLifecycle(body, "", "")
	for _, key := range []string{"archived_at", "archived_by", "trashed_at", "trashed_by", "trash_reason"} {
		if _, exists := body[key]; exists {
			t.Fatalf("clearTrashedLifecycle should clear %s: %#v", key, body)
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

func TestPublicRefsInValueOnlyRewritesRefFields(t *testing.T) {
	t.Parallel()

	value := map[string]any{
		"text": "note: follow up",
		"url":  "https://example.test/path",
		"payload": map[string]any{
			"subject_ref": " topic:topic-1 ",
			"notes": []any{
				map[string]any{"target_ref": " document:doc-1 "},
				"event: ordinary prose",
			},
			"related_refs": []any{" thread:thread-1 ", "topic:topic-2"},
		},
		"provenance": map[string]any{
			"sources": []any{"event: event-1", "https://example.test/source"},
		},
	}

	got := publicRefsInValue(context.Background(), nil, value).(map[string]any)
	if got["text"] != "note: follow up" {
		t.Fatalf("non-ref text field was rewritten: %#v", got["text"])
	}
	if got["url"] != "https://example.test/path" {
		t.Fatalf("non-ref url field was rewritten: %#v", got["url"])
	}

	payload := got["payload"].(map[string]any)
	if got := payload["subject_ref"]; got != "topic:topic-1" {
		t.Fatalf("subject_ref should be normalized: %#v", got)
	}
	notes := payload["notes"].([]any)
	if got := notes[0].(map[string]any)["target_ref"]; got != "document:doc-1" {
		t.Fatalf("nested target_ref should be normalized: %#v", got)
	}
	if got := notes[1]; got != "event: ordinary prose" {
		t.Fatalf("non-ref array string was rewritten: %#v", got)
	}
	if refs := payload["related_refs"]; !reflect.DeepEqual(refs, []any{"thread:thread-1", "topic:topic-2"}) {
		t.Fatalf("related_refs should be normalized: %#v", refs)
	}
	provenance := got["provenance"].(map[string]any)
	if sources := provenance["sources"]; !reflect.DeepEqual(sources, []any{"event: event-1", "https://example.test/source"}) {
		t.Fatalf("provenance.sources should not be treated as refs: %#v", sources)
	}
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
		"title":      title,
		"type":       "incident",
		"provenance": map[string]any{"sources": []string{"inferred"}},
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

func TestRefEdgesStoreTypedRefRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)

	threadID := createPrimitiveTestThread(t, ctx, store, "RoundTrip")

	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{
		"id":            "board-rt-1",
		"title":         "RoundTrip Board",
		"document_refs": []string{},
		"pinned_refs":   []string{},
	})
	if err != nil {
		t.Fatalf("create board: %v", err)
	}
	boardID := board["id"].(string)

	cardResult, err := store.CreateBoardCard(ctx, "actor-1", boardID, AddBoardCardInput{
		CardID:         "card-rt-1",
		Title:          "RoundTrip Card",
		ParentThreadID: threadID,
	})
	if err != nil {
		t.Fatalf("create card: %v", err)
	}
	cardID := cardResult.Card["id"].(string)

	t.Run("forward lookup by source_ref", func(t *testing.T) {
		edges, err := store.ListRefEdgesBySource(ctx, anyStringValue(board["ref"]), "")
		if err != nil {
			t.Fatalf("ListRefEdgesBySource: %v", err)
		}
		if len(edges) == 0 {
			t.Fatal("expected ref edges for board")
		}
		for _, e := range edges {
			if e.SourceRef != anyStringValue(board["ref"]) {
				t.Fatalf("expected source_ref board:%s, got %s", boardID, e.SourceRef)
			}
			if !strings.HasPrefix(e.TargetRef, "card:") && e.Relation != "ref" {
				t.Fatalf("unexpected edge: source_ref=%s target_ref=%s relation=%s", e.SourceRef, e.TargetRef, e.Relation)
			}
			if e.Relation == "" {
				t.Fatal("relation must not be empty")
			}
			if e.DiscoveredAt == "" {
				t.Fatal("discovered_at must not be empty")
			}
		}
	})

	t.Run("reverse lookup by target_ref", func(t *testing.T) {
		edges, err := store.ListRefEdgesByTarget(ctx, anyStringValue(cardResult.Card["ref"]), "")
		if err != nil {
			t.Fatalf("ListRefEdgesByTarget: %v", err)
		}
		if len(edges) == 0 {
			t.Fatal("expected ref edges pointing at card")
		}
		for _, e := range edges {
			if e.TargetRef != anyStringValue(cardResult.Card["ref"]) {
				t.Fatalf("expected target_ref card:%s, got %s", cardID, e.TargetRef)
			}
		}
	})

	t.Run("relation filter", func(t *testing.T) {
		edges, err := store.ListRefEdgesBySource(ctx, anyStringValue(board["ref"]), "board_card")
		if err != nil {
			t.Fatalf("ListRefEdgesBySource with relation filter: %v", err)
		}
		if len(edges) != 1 {
			t.Fatalf("expected exactly 1 board_card edge, got %d", len(edges))
		}
		if edges[0].Relation != "board_card" {
			t.Fatalf("expected relation board_card, got %s", edges[0].Relation)
		}
		if edges[0].TargetRef != anyStringValue(cardResult.Card["ref"]) {
			t.Fatalf("expected target_ref card:%s, got %s", cardID, edges[0].TargetRef)
		}
	})

	t.Run("empty source_ref returns empty", func(t *testing.T) {
		edges, err := store.ListRefEdgesBySource(ctx, "", "")
		if err != nil {
			t.Fatalf("ListRefEdgesBySource empty: %v", err)
		}
		if len(edges) != 0 {
			t.Fatalf("expected 0 edges for empty source_ref, got %d", len(edges))
		}
	})

	t.Run("invalid typed_ref returns error", func(t *testing.T) {
		_, err := store.ListRefEdgesBySource(ctx, "not-a-typed-ref", "")
		if err == nil {
			t.Fatal("expected error for invalid typed ref")
		}
	})

	t.Run("unknown ref returns empty", func(t *testing.T) {
		edges, err := store.ListRefEdgesBySource(ctx, "board:nonexistent-id", "")
		if err != nil {
			t.Fatalf("ListRefEdgesBySource nonexistent: %v", err)
		}
		if len(edges) != 0 {
			t.Fatalf("expected 0 edges for nonexistent source, got %d", len(edges))
		}
	})
}

func TestInfrastructureRefsUsePublicRefsWithInternalJoinMetadata(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	topicResult, err := store.CreateTopic(ctx, "actor-1", map[string]any{
		"id":            "topic-internal-public-ref",
		"title":         "Public Topic Ref",
		"summary":       "summary",
		"owner_refs":    []string{},
		"document_refs": []string{},
		"board_refs":    []string{},
		"related_refs":  []string{},
		"provenance":    map[string]any{"sources": []string{"inferred"}},
	})
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	topicID := topicResult.Topic["id"].(string)
	topicRef := topicResult.Topic["ref"].(string)
	if topicRef == "topic:"+topicID || !strings.HasPrefix(topicRef, "topic:") {
		t.Fatalf("expected topic public ref, got topicRef=%q topicID=%q", topicRef, topicID)
	}

	threadID := topicResult.Topic["thread_id"].(string)
	thread, err := store.GetThread(ctx, threadID)
	if err != nil {
		t.Fatalf("get topic backing thread: %v", err)
	}
	if got := anyStringValue(thread["subject_ref"]); got != topicRef {
		t.Fatalf("thread subject_ref should be public: got %q want %q", got, topicRef)
	}
	if got := anyStringValue(thread["ref"]); got == "" || got == "thread:"+threadID {
		t.Fatalf("thread ref should be public handle-backed ref, got %q for id %q", got, threadID)
	}

	event, err := store.AppendEvent(ctx, "actor-1", map[string]any{
		"id":        "event-public-ref-normalize",
		"type":      "message_posted",
		"thread_id": threadID,
		"refs":      []string{"thread:" + threadID, "topic:" + topicID},
		"payload": map[string]any{
			"subject_ref": "topic:" + topicID,
			"related_refs": []any{
				"topic:" + topicID,
			},
		},
		"provenance": map[string]any{"sources": []string{"topic:" + topicID}},
	})
	if err != nil {
		t.Fatalf("append event: %v", err)
	}
	if refs := stringListFromAny(event["refs"]); !containsStringForResourceTest(refs, topicRef) || containsStringForResourceTest(refs, "topic:"+topicID) {
		t.Fatalf("event refs should expose public topic ref: got %#v", refs)
	}
	payload := event["payload"].(map[string]any)
	if got := anyStringValue(payload["subject_ref"]); got != topicRef {
		t.Fatalf("event payload subject_ref should be public: got %q want %q", got, topicRef)
	}
	provenance := event["provenance"].(map[string]any)
	if sources := stringListFromAny(provenance["sources"]); !reflect.DeepEqual(sources, []string{"topic:" + topicID}) {
		t.Fatalf("event provenance sources should preserve exact user strings: got %#v want %#v", sources, []string{"topic:" + topicID})
	}

	var metadataJSON string
	if err := workspace.DB().QueryRowContext(ctx, `
		SELECT metadata_json
		  FROM ref_edges
		 WHERE source_type = 'event'
		   AND source_id = ?
		   AND target_type = 'topic'
		   AND target_id = ?
		   AND edge_type = 'ref'`,
		event["id"], topicID,
	).Scan(&metadataJSON); err != nil {
		t.Fatalf("query event topic ref edge: %v", err)
	}
	metadata := map[string]any{}
	if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
		t.Fatalf("decode ref edge metadata: %v", err)
	}
	if got := anyStringValue(metadata["target_ref"]); got != topicRef {
		t.Fatalf("ref edge target_ref metadata should be public: got %q want %q", got, topicRef)
	}
	if got := anyStringValue(metadata["resolved_target_id"]); got != topicID {
		t.Fatalf("ref edge resolved_target_id should preserve internal join id: got %q want %q", got, topicID)
	}
}

func stringPointer(value string) *string {
	value = strings.TrimSpace(value)
	return &value
}

func stringListFromAny(raw any) []string {
	var out []string
	switch values := raw.(type) {
	case []string:
		out = append(out, values...)
	case []any:
		for _, value := range values {
			if text, ok := value.(string); ok {
				out = append(out, text)
			}
		}
	}
	sort.Strings(out)
	return out
}

func containsStringForResourceTest(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
