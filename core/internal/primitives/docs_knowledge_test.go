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
		"handle":      "kb-shared-runbook",
		"title":       "Lane docs knowledge runbook",
		"source":      "https://example.invalid/kb/runbook.md",
		"tags":        []string{"knowledge", "ops"},
		"hosts":       []string{"m4-air"},
		"verified_at": "2026-09-08T12:00:00Z",
	}, "body token alphawhiz lives only in the document body", "text", nil)
	if err != nil {
		t.Fatalf("create knowledge document: %v", err)
	}
	if strings.TrimSpace(anyString(knowledge["source"])) != "https://example.invalid/kb/runbook.md" {
		t.Fatalf("source roundtrip: got %q", knowledge["source"])
	}
	hosts, _ := knowledge["hosts"].([]string)
	if !containsString(hosts, "m4-air") {
		t.Fatalf("expected hosts, got %#v", knowledge["hosts"])
	}
	if strings.TrimSpace(anyString(knowledge["verified_at"])) == "" {
		t.Fatalf("verified_at missing: %#v", knowledge)
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
	if strings.TrimSpace(anyString(reply["reply_to"])) == "" {
		t.Fatalf("reply_to missing: %#v", reply)
	}
	if strings.TrimSpace(anyString(root["ref"])) == "" {
		t.Fatalf("comment missing stable ref: %#v", root)
	}

	edited, err := store.UpdateDocumentComment(ctx, "actor-b", docID, commentID, "edited token gammawhiz")
	if err != nil {
		t.Fatalf("edit own comment: %v", err)
	}
	if strings.TrimSpace(anyString(edited["body"])) != "edited token gammawhiz" {
		t.Fatalf("edited body: %#v", edited)
	}
	if strings.TrimSpace(anyString(edited["ref"])) != strings.TrimSpace(anyString(root["ref"])) {
		t.Fatalf("edit changed comment ref: %#v vs %#v", edited, root)
	}
	if _, err := store.UpdateDocumentComment(ctx, "actor-a", docID, commentID, "should fail"); !errors.Is(err, primitives.ErrForbidden) {
		t.Fatalf("expected forbidden edit, got %v", err)
	}

	comments, _, err := store.ListDocumentComments(ctx, docID, nil, "")
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	if len(comments) < 2 {
		t.Fatalf("expected comment thread, got %#v", comments)
	}

	hostHits, _, err := store.SearchDocuments(ctx, primitives.DocumentSearchFilter{Query: "alphawhiz", Host: "m4-air"})
	if err != nil {
		t.Fatalf("host search: %v", err)
	}
	if !searchContainsHandle(hostHits, "kb-shared-runbook") {
		t.Fatalf("expected host filter hit, got %#v", handlesOf(hostHits))
	}
	missHost, _, err := store.SearchDocuments(ctx, primitives.DocumentSearchFilter{Query: "alphawhiz", Host: "proxmox"})
	if err != nil {
		t.Fatalf("host miss search: %v", err)
	}
	if searchContainsHandle(missHost, "kb-shared-runbook") {
		t.Fatalf("host filter leaked other host: %#v", handlesOf(missHost))
	}

	commentHits, _, err := store.SearchDocuments(ctx, primitives.DocumentSearchFilter{Query: "gammawhiz"})
	if err != nil {
		t.Fatalf("search edited comments: %v", err)
	}
	if !searchContainsHandle(commentHits, "kb-shared-runbook") {
		t.Fatalf("expected edited comment text hit, got %#v", handlesOf(commentHits))
	}

	withLast, _, err := store.ListDocuments(ctx, primitives.DocumentListFilter{})
	if err != nil {
		t.Fatalf("list documents for last_comment: %v", err)
	}
	var lastComment map[string]any
	for _, document := range withLast {
		if strings.TrimSpace(anyString(document["handle"])) == "kb-shared-runbook" {
			lastComment, _ = document["last_comment"].(map[string]any)
		}
	}
	if lastComment == nil {
		t.Fatalf("expected last_comment enrichment on commented document, got %#v", withLast)
	}
	if strings.TrimSpace(anyString(lastComment["body"])) != "ack from host A" {
		t.Fatalf("last_comment.body: %#v", lastComment)
	}
	if strings.TrimSpace(anyString(lastComment["created_by"])) != "actor-a" {
		t.Fatalf("last_comment.created_by: %#v", lastComment)
	}

	searchWithLast, _, err := store.SearchDocuments(ctx, primitives.DocumentSearchFilter{Query: "alphawhiz"})
	if err != nil {
		t.Fatalf("search documents for last_comment: %v", err)
	}
	var searchLastComment map[string]any
	for _, document := range searchWithLast {
		if strings.TrimSpace(anyString(document["handle"])) == "kb-shared-runbook" {
			searchLastComment, _ = document["last_comment"].(map[string]any)
		}
	}
	if searchLastComment == nil {
		t.Fatalf("expected last_comment enrichment on search hit, got %#v", searchWithLast)
	}
	if strings.TrimSpace(anyString(searchLastComment["body"])) != "ack from host A" {
		t.Fatalf("search last_comment.body: %#v", searchLastComment)
	}

	head := strings.TrimSpace(anyString(knowledge["head_revision_id"]))
	updated, nextRev, err := store.UpdateDocument(ctx, "actor-a", docID, map[string]any{
		"title":       "Lane docs knowledge runbook",
		"source":      "https://example.invalid/kb/runbook.md",
		"tags":        []string{"knowledge", "ops"},
		"hosts":       []string{"m4-air"},
		"verified_at": "2026-09-08T12:00:00Z",
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
	afterRev, _, err := store.ListDocumentComments(ctx, docID, nil, "")
	if err != nil {
		t.Fatalf("list comments after revision: %v", err)
	}
	if len(afterRev) < 2 {
		t.Fatalf("comments did not survive document revision: %#v", afterRev)
	}

	deleted, err := store.DeleteDocumentComment(ctx, "actor-b", docID, commentID)
	if err != nil {
		t.Fatalf("delete own comment: %v", err)
	}
	if strings.TrimSpace(anyString(deleted["id"])) != commentID {
		t.Fatalf("deleted comment id: %#v", deleted)
	}
	remaining, _, err := store.ListDocumentComments(ctx, docID, nil, "")
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	for _, row := range remaining {
		if strings.TrimSpace(anyString(row["id"])) == commentID {
			t.Fatalf("deleted comment still listed: %#v", remaining)
		}
	}
	_ = other
}

func TestUpdateDocumentNoopWhenContentAndMetadataMatch(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	ctx := context.Background()
	doc, rev, err := store.CreateDocument(ctx, "actor-a", map[string]any{
		"handle": "kb-noop",
		"title":  "Noop",
		"source": "https://example.invalid/kb/noop.md",
		"tags":   []string{"knowledge"},
	}, "same body", "text", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	head := strings.TrimSpace(anyString(rev["revision_id"]))
	if head == "" {
		head = strings.TrimSpace(anyString(doc["head_revision_id"]))
	}
	again, next, err := store.UpdateDocument(ctx, "actor-a", anyString(doc["id"]), map[string]any{
		"title":  "Noop",
		"source": "https://example.invalid/kb/noop.md",
		"tags":   []string{"knowledge"},
	}, head, "same body", "text", nil, nil)
	if err != nil {
		t.Fatalf("noop update: %v", err)
	}
	if strings.TrimSpace(anyString(next["revision_id"])) != head {
		t.Fatalf("expected unchanged revision, got %q vs %q", next["revision_id"], head)
	}
	if anyInt(next["revision_number"]) != 1 {
		t.Fatalf("expected revision 1 after noop, doc=%#v rev=%#v", again, next)
	}
}

func anyInt(raw any) int {
	switch typed := raw.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
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
