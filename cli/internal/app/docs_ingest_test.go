package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIngestHandleFromRelPath(t *testing.T) {
	t.Parallel()
	got := ingestHandleFromRelPath("NOW.md")
	if !strings.HasPrefix(got, "now-") {
		t.Fatalf("NOW.md handle: %q", got)
	}
	if len(got) > docsIngestHandleMax {
		t.Fatalf("handle longer than %d: %q", docsIngestHandleMax, got)
	}
	long := ingestHandleFromRelPath("projects/core-reliability-regression-confidence/evidence/2026-07-25-desktop-backend-candidate-acceptance-packet.md")
	if len(long) > docsIngestHandleMax {
		t.Fatalf("long handle: %q len=%d", long, len(long))
	}
	if ingestHandleFromRelPath("NOW.md") != got {
		t.Fatal("handle must be stable")
	}
	if ingestHandleFromRelPath("lessons/NOW.md") == got {
		t.Fatal("different relative paths must not share a handle")
	}
}

func TestJoinSourceURL(t *testing.T) {
	t.Parallel()
	got := joinSourceURL("https://example.invalid/kb/", "lessons/NOW.md")
	if got != "https://example.invalid/kb/lessons/NOW.md" {
		t.Fatalf("source: %q", got)
	}
}

func TestDocsTitleFromMarkdown(t *testing.T) {
	t.Parallel()
	if got := docsTitleFromMarkdown("# Omi now\n\nbody\n", "NOW.md"); got != "Omi now" {
		t.Fatalf("heading title: %q", got)
	}
	if got := docsTitleFromMarkdown("no heading\n", "lessons/runbook.md"); got != "runbook" {
		t.Fatalf("filename title: %q", got)
	}
}

func TestIngestContentEqualIgnoresTrailingWhitespace(t *testing.T) {
	t.Parallel()
	if !ingestContentEqual("hello  \n\n\nworld\n", "hello\n\nworld") {
		t.Fatal("expected hygiene-equivalent content to match")
	}
	if ingestContentEqual("hello", "goodbye") {
		t.Fatal("different content must not match")
	}
}

func TestListMarkdownFilesSkipsGitAndNonMarkdown(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "lessons"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "NOW.md"), []byte("# Now\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "lessons", "note.md"), []byte("# Note\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ignore.txt"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "COMMIT.md"), []byte("# git\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := listMarkdownFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("files: %#v", files)
	}
}

func TestDocsIngestHelpHasCopyableExample(t *testing.T) {
	t.Parallel()
	output := runHelpCommand(t, "help", "docs", "ingest")
	if !strings.Contains(output, "anx docs ingest ./kb --source https://example.invalid/kb") {
		t.Fatalf("expected copyable ingest example, got %s", output)
	}
	if !strings.Contains(output, "--source <url-prefix>") {
		t.Fatalf("expected --source flag, got %s", output)
	}
}

func TestDocsIngestRequiresSource(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	payload := assertEnvelopeError(t, runCLIForTest(t, home, nil, nil, []string{"--json", "--base-url", "http://127.0.0.1:9", "docs", "ingest", home}))
	errObj := asMap(payload["error"])
	if got := anyStringValue(errObj["code"]); got != "invalid_request" {
		t.Fatalf("expected invalid_request, got %#v", payload)
	}
	if got := anyStringValue(errObj["message"]); !strings.Contains(got, "--source") {
		t.Fatalf("expected --source error, got %#v", payload)
	}
}
