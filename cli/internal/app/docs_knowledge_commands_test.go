package app

import "testing"

func TestDeriveDocsPutHandle(t *testing.T) {
	t.Parallel()
	if got := deriveDocsPutHandle("document:kb-shared", "ignored.md", "Title"); got != "kb-shared" {
		t.Fatalf("explicit handle: %q", got)
	}
	if got := deriveDocsPutHandle("", "/tmp/Lane Docs.md", ""); got != "lane-docs" {
		t.Fatalf("filename stem: %q", got)
	}
	if got := deriveDocsPutHandle("", "-", "Lane Docs"); got != "lane-docs" {
		t.Fatalf("title slug: %q", got)
	}
	if got := deriveDocsPutHandle("", "search.md", "Title"); got != "title" {
		t.Fatalf("reserved search stem should fall back to title: %q", got)
	}
}

func TestSplitDocsTags(t *testing.T) {
	t.Parallel()
	got := splitDocsTags([]string{"knowledge,ops", " knowledge ", "ops"})
	if len(got) != 2 || got[0] != "knowledge" || got[1] != "ops" {
		t.Fatalf("tags: %#v", got)
	}
}
