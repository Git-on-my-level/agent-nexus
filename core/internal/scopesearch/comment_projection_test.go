package scopesearch

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCommentProjectionRawCoverage(t *testing.T) {
	// Punctuation must not let an arbitrarily late source token enter a small
	// normalized prefix. Raw coverage is capped before normalization.
	got := PrepareComment(strings.Repeat("!", MaxTextBytes) + "late")
	if !got.Truncated || got.Text != "" || len(got.Terms) != 0 {
		t.Fatal(got)
	}
	got = PrepareComment(strings.Repeat("!", MaxTextBytes-1) + "éclair")
	if !got.Truncated || !utf8.ValidString(got.Text) || got.Text != "" {
		t.Fatal("split UTF-8 rune entered projection", got)
	}
	got = PrepareComment(strings.Repeat("!", MaxTextBytes-2) + "é")
	if got.Truncated || got.Text != "e" || len(got.Terms) != 1 {
		t.Fatal("exact-boundary source lost", got)
	}
}
