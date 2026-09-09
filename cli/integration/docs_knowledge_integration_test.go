//go:build integration

package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Two CLI profiles on one core stand in for two hosts that cannot see each
// other's disks. All document bodies and comments are synthetic fixtures.
func TestDocsKnowledgeTwoProfileSearchAndComment(t *testing.T) {
	h := newLiveCoreHarness(t)
	token := runToken()
	h.registerAgentBootstrap(t, "host-a", "host-a."+token)
	invite := h.createInviteToken(t, "host-a")
	h.registerAgentInvite(t, "host-b", "host-b."+token, invite)

	bodyPath := filepath.Join(t.TempDir(), "kb-shared-runbook.md")
	if err := os.WriteFile(bodyPath, []byte("Synthetic runbook body token alphawhiz-"+token), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	put := h.runCLIExpectOK(t, "host-a", nil,
		"docs", "put", bodyPath,
		"--title", "Lane docs knowledge runbook",
		"--source", "https://example.invalid/kb/runbook.md",
		"--tags", "knowledge",
		"--hosts", "host-a",
		"--verified-at", "2026-09-08T12:00:00Z",
		"--handle", "kb-shared-runbook-"+token,
	)
	handle := firstStringPath(t, put.Payload, "data.body.document.handle", "data.document.handle")
	if !strings.Contains(handle, "kb-shared-runbook") {
		t.Fatalf("put handle: %s stdout=%s", handle, put.Stdout)
	}
	source := firstStringPath(t, put.Payload, "data.body.document.source", "data.document.source")
	if source != "https://example.invalid/kb/runbook.md" {
		t.Fatalf("source pointer missing: %s", put.Stdout)
	}

	again := h.runCLIExpectOK(t, "host-a", nil,
		"docs", "put", bodyPath,
		"--title", "Lane docs knowledge runbook",
		"--source", "https://example.invalid/kb/runbook.md",
		"--tags", "knowledge",
		"--hosts", "host-a",
		"--verified-at", "2026-09-08T12:00:00Z",
		"--handle", "kb-shared-runbook-"+token,
	)
	if firstStringPath(t, again.Payload, "data.body.document.handle", "data.document.handle") != handle {
		t.Fatalf("put was not idempotent by handle: %s vs %s", put.Stdout, again.Stdout)
	}

	search := h.runCLIExpectOK(t, "host-b", nil, "docs", "search", "alphawhiz-"+token, "--knowledge", "--host", "host-a", "--limit", "20")
	docs := firstSlicePath(t, search.Payload, "data.body.documents", "data.documents")
	if !searchHasHandle(docs, handle) {
		t.Fatalf("host-b search missed host-a doc: %s", search.Stdout)
	}

	comment := h.runCLIExpectOK(t, "host-b", nil, "docs", "comment", handle, "comment token betawhiz-"+token+" from host B")
	commentID := firstStringPath(t, comment.Payload, "data.body.comment.id", "data.comment.id")

	listed := h.runCLIExpectOK(t, "host-a", nil, "docs", "comments", handle)
	comments := firstSlicePath(t, listed.Payload, "data.body.comments", "data.comments")
	found := false
	for _, raw := range comments {
		row, _ := raw.(map[string]any)
		if strings.TrimSpace(fmt.Sprint(row["id"])) == commentID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("host-a did not read host-b comment %s: %s", commentID, listed.Stdout)
	}
}

func firstStringPath(t *testing.T, payload map[string]any, paths ...string) string {
	t.Helper()
	for _, path := range paths {
		if value, ok := getPathValue(payload, path); ok {
			text := strings.TrimSpace(fmt.Sprint(value))
			if text != "" && text != "<nil>" {
				return text
			}
		}
	}
	t.Fatalf("none of %v found in payload %#v", paths, payload)
	return ""
}

func firstSlicePath(t *testing.T, payload map[string]any, paths ...string) []any {
	t.Helper()
	for _, path := range paths {
		value, ok := getPathValue(payload, path)
		if !ok {
			continue
		}
		if rows, ok := value.([]any); ok {
			return rows
		}
	}
	t.Fatalf("none of %v found as a list in payload %#v", paths, payload)
	return nil
}

func TestDocsIngestIdempotentByRelativePath(t *testing.T) {
	h := newLiveCoreHarness(t)
	token := runToken()
	h.registerAgentBootstrap(t, "host-a", "host-a."+token)

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "lessons"), 0o755); err != nil {
		t.Fatal(err)
	}
	nowBody := "# Omi now\n\nSynthetic ingest token nowwhiz-" + token + "\n"
	noteBody := "# Lesson note\n\nSynthetic ingest token lessonwhiz-" + token + "\n"
	if err := os.WriteFile(filepath.Join(root, "NOW.md"), []byte(nowBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "lessons", "note.md"), []byte(noteBody), 0o644); err != nil {
		t.Fatal(err)
	}

	first := h.runCLIExpectOK(t, "host-a", nil,
		"docs", "ingest", root,
		"--source", "https://example.invalid/kb",
	)
	if firstIntPath(t, first.Payload, "data.created") != 2 {
		t.Fatalf("first ingest created: %s", first.Stdout)
	}
	if firstIntPath(t, first.Payload, "data.updated") != 0 || firstIntPath(t, first.Payload, "data.unchanged") != 0 {
		t.Fatalf("first ingest should only create: %s", first.Stdout)
	}

	second := h.runCLIExpectOK(t, "host-a", nil,
		"docs", "ingest", root,
		"--source", "https://example.invalid/kb",
	)
	if firstIntPath(t, second.Payload, "data.created") != 0 || firstIntPath(t, second.Payload, "data.updated") != 0 {
		t.Fatalf("second ingest made new revisions: %s", second.Stdout)
	}
	if firstIntPath(t, second.Payload, "data.unchanged") != 2 {
		t.Fatalf("second ingest unchanged: %s", second.Stdout)
	}

	search := h.runCLIExpectOK(t, "host-a", nil, "docs", "search", "NOW.md", "--knowledge", "--limit", "20")
	docs := firstSlicePath(t, search.Payload, "data.body.documents", "data.documents")
	if len(docs) == 0 {
		t.Fatalf("search missed NOW.md: %s", search.Stdout)
	}
	handle := ingestHandleFromSearch(docs, "NOW.md")
	if handle == "" {
		t.Fatalf("search rows lacked NOW.md source: %s", search.Stdout)
	}
	history := h.runCLIExpectOK(t, "host-a", nil, "docs", "history", handle)
	revisions := firstSlicePath(t, history.Payload, "data.body.revisions", "data.revisions")
	if len(revisions) != 1 {
		t.Fatalf("expected 1 revision after second ingest, got %d: %s", len(revisions), history.Stdout)
	}
}

func firstIntPath(t *testing.T, payload map[string]any, path string) int {
	t.Helper()
	value, ok := getPathValue(payload, path)
	if !ok {
		t.Fatalf("missing %s in %#v", path, payload)
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		t.Fatalf("%s is %T %#v", path, value, value)
		return 0
	}
}

func ingestHandleFromSearch(documents []any, filename string) string {
	for _, raw := range documents {
		row, _ := raw.(map[string]any)
		source := strings.TrimSpace(fmt.Sprint(row["source"]))
		if strings.Contains(source, filename) {
			return strings.TrimSpace(fmt.Sprint(row["handle"]))
		}
	}
	return ""
}

func searchHasHandle(documents []any, handle string) bool {
	for _, raw := range documents {
		row, _ := raw.(map[string]any)
		if strings.TrimSpace(fmt.Sprint(row["handle"])) == handle {
			return true
		}
	}
	return false
}
