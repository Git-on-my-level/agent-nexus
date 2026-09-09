package primitives_test

import (
	"context"
	"strings"
	"testing"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
)

func TestDocumentKnowledgeSearchCommentsAndPut(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	ctx := context.Background()

	knowledge, _, err := store.CreateDocument(ctx, "actor-a", map[string]any{
		"handle": "kb-shared-runbook",
		"title":  "Lane docs knowledge runbook",
		"source": "https://example.invalid/kb/runbook.md",
		"tags":   []string{"knowledge", "ops"},
	}, "body token alphawhiz lives only in the document body", "text", nil)
	if err != nil {
		t.Fatalf("create knowledge document: %v", err)
	}
	if got := strings.TrimSpace(anyString(knowledge["source"])); got != "https://example.invalid/kb/runbook.md" {
		t.Fatalf("source roundtrip: got %q", got)
	}
	tags, _ := knowledge["tags"].([]string)
	if !containsString(tags, "knowledge") {
		t.Fatalf("expected knowledge tag, got %#v", knowledge["tags"])
	}

	other, _, err := store.CreateDocument(ctx, "actor-a", map[string]any{
		"handle": "private-note",
		"title":  "Private note",
		"tags":   []string{"internal"},
	}, "unrelated content", "text", nil)
	if err != nil {
		t.Fatalf("create other document: %v", err)
	}

	titleHits, _, err := store.SearchDocuments(ctx, primitives.DocumentSearchFilter{Query: "alphawhiz"})
	if err != nil {
		t.Fatalf("search body token: %v", err)
	}
	if !searchContainsHandle(titleHits, "kb-shared-runbook") {
		t.Fatalf("expected body token hit, got %#v", handlesOf(titleHits))
	}
	if searchContainsHandle(titleHits, "private-note") {
		t.Fatalf("did not expect private note in body token search: %#v", handlesOf(titleHits))
	}

	knowledgeOnly, _, err := store.SearchDocuments(ctx, primitives.DocumentSearchFilter{Query: "runbook", Knowledge: true})
	if err != nil {
		t.Fatalf("knowledge search: %v", err)
	}
	if !searchContainsHandle(knowledgeOnly, "kb-shared-runbook") {
		t.Fatalf("expected knowledge doc, got %#v", handlesOf(knowledgeOnly))
	}
	if searchContainsHandle(knowledgeOnly, "private-note") {
		t.Fatalf("knowledge filter leaked untagged doc: %#v", handlesOf(knowledgeOnly))
	}

	listed, _, err := store.ListDocuments(ctx, primitives.DocumentListFilter{Knowledge: true})
	if err != nil {
		t.Fatalf("list knowledge: %v", err)
	}
	if !searchContainsHandle(listed, "kb-shared-runbook") || searchContainsHandle(listed, "private-note") {
		t.Fatalf("list knowledge filter: %#v", handlesOf(listed))
	}

	docID := strings.TrimSpace(anyString(knowledge["id"]))
	root, err := store.CreateDocumentComment(ctx, "actor-b", docID, "comment token betawhiz from host B", "")
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}
	commentID := strings.TrimSpace(anyString(root["id"]))
	if commentID == "" {
		t.Fatalf("comment missing stable id: %#v", root)
	}
	reply, err := store.CreateDocumentComment(ctx, "actor-a", docID, "ack from host A", commentID)
	if err != nil {
		t.Fatalf("reply comment: %v", err)
	}
	if strings.TrimSpace(anyString(reply["parent_id"])) != commentID {
		t.Fatalf("reply parent_id: %#v", reply)
	}

	comments, _, err := store.ListDocumentComments(ctx, docID, nil, "")
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	if len(comments) < 2 {
		t.Fatalf("expected comment thread, got %#v", comments)
	}

	commentHits, _, err := store.SearchDocuments(ctx, primitives.DocumentSearchFilter{Query: "betawhiz"})
	if err != nil {
		t.Fatalf("search comments: %v", err)
	}
	if !searchContainsHandle(commentHits, "kb-shared-runbook") {
		t.Fatalf("expected comment text hit, got %#v", handlesOf(commentHits))
	}

	head := strings.TrimSpace(anyString(knowledge["head_revision_id"]))
	updated, nextRev, err := store.UpdateDocument(ctx, "actor-a", docID, map[string]any{
		"title":  "Lane docs knowledge runbook",
		"source": "https://example.invalid/kb/runbook.md",
		"tags":   []string{"knowledge", "ops"},
	}, head, "updated body still has alphawhiz", "text", nil, nil)
	if err != nil {
		t.Fatalf("put-style update: %v", err)
	}
	if strings.TrimSpace(anyString(updated["handle"])) != "kb-shared-runbook" {
		t.Fatalf("handle changed on update: %#v", updated)
	}
	if strings.TrimSpace(anyString(nextRev["revision_id"])) == head {
		t.Fatalf("expected new revision after put-style update")
	}
	_ = other
}

func containsString(values []string, want string) bool {
	for _, item := range values {
		if item == want {
			return true
		}
	}
	return false
}

func handlesOf(documents []map[string]any) []string {
	out := make([]string, 0, len(documents))
	for _, document := range documents {
		out = append(out, strings.TrimSpace(anyString(document["handle"])))
	}
	return out
}

func searchContainsHandle(documents []map[string]any, handle string) bool {
	return containsString(handlesOf(documents), handle)
}
