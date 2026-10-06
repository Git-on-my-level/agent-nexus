package scopes

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSearchProjectionSourceCoverage(t *testing.T) {
	source := Projection{Title: "title", ContainerID: "parent", Text: strings.Repeat("a", MaxValueBytes) + " late phrase", Timestamp: 42}
	bounded, e := BoundSearchProjection(source)
	if e != nil {
		t.Fatal(e)
	}
	if !bounded.SourceTruncated || len(bounded.Title)+len(bounded.Text)+len(bounded.ContainerID) != MaxValueBytes || bounded.Timestamp != 42 || strings.Contains(bounded.Text, "late phrase") {
		t.Fatal("incorrect source coverage", len(bounded.Text), bounded.SourceTruncated)
	}
	c := Change{ScopeID: "private", Kind: "document", ResourceID: "opaque", CanonicalVersion: 1, Family: "search", Audience: "all", After: &bounded}
	if e = c.Validate(); e != nil {
		t.Fatal("bounded delta rejected", e)
	}
	again, e := BoundSearchProjection(bounded)
	if e != nil || again != bounded {
		t.Fatal("coverage lost on already truncated delta", e)
	}
	exact := Projection{Text: strings.Repeat("b", MaxValueBytes)}
	out, e := BoundSearchProjection(exact)
	if e != nil || out.SourceTruncated || out.Text != exact.Text {
		t.Fatal("exact boundary mislabeled", e)
	}
}
func TestSearchProjectionUTF8BoundaryAndMetadata(t *testing.T) {
	for _, runeText := range []string{"é", "界", "🙂"} {
		for remaining := 1; remaining < len(runeText); remaining++ {
			prefix := strings.Repeat("a", MaxValueBytes-remaining)
			p, e := BoundSearchProjection(Projection{Text: prefix + runeText + "suffix"})
			if e != nil || p.Text != prefix || !p.SourceTruncated || !utf8.ValidString(p.Text) {
				t.Fatalf("partial rune %q/%d: %v", runeText, remaining, e)
			}
		}
	}
	for _, p := range []Projection{{Title: strings.Repeat("x", MaxValueBytes+1)}, {Title: "bad\xff"}, {Text: "bad\xff"}} {
		if _, e := BoundSearchProjection(p); !errors.Is(e, ErrBudget) {
			t.Fatal("invalid source accepted", e)
		}
	}
	p, e := BoundSearchProjection(Projection{Title: strings.Repeat("x", MaxValueBytes), Text: "more"})
	if e != nil || p.Text != "" || !p.SourceTruncated {
		t.Fatal("zero body capacity", e)
	}
}
