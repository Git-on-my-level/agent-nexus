package textdiff

import (
	"fmt"
	"strings"
	"testing"
)

// apply replays a unified diff onto before, proving the rendered hunks are a
// correct edit script.
func apply(t *testing.T, before, unified string) string {
	t.Helper()
	a := splitLines(before)
	var out []string
	pos := 0
	for _, l := range strings.Split(strings.TrimSuffix(unified, "\n"), "\n") {
		switch {
		case strings.HasPrefix(l, "---"), strings.HasPrefix(l, "+++"):
		case strings.HasPrefix(l, "@@"):
			var as, ac, bs, bc int
			if _, err := fmt.Sscanf(strings.ReplaceAll(strings.ReplaceAll(l, ",", " "), "@@", ""), " -%d %d +%d %d", &as, &ac, &bs, &bc); err != nil {
				// single-line ranges omit the count
				fields := strings.Fields(strings.Trim(l, "@ "))
				fmt.Sscanf(strings.SplitN(strings.TrimPrefix(fields[0], "-"), ",", 2)[0], "%d", &as)
				if !strings.Contains(fields[0], ",") {
					ac = 1
				} else {
					fmt.Sscanf(strings.SplitN(fields[0], ",", 2)[1], "%d", &ac)
				}
			}
			start := as - 1
			if ac == 0 {
				start = as
			}
			for pos < start {
				out = append(out, a[pos])
				pos++
			}
		case strings.HasPrefix(l, " "):
			out = append(out, a[pos])
			pos++
		case strings.HasPrefix(l, "-"):
			pos++
		case strings.HasPrefix(l, "+"):
			out = append(out, l[1:])
		}
	}
	out = append(out, a[pos:]...)
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n") + "\n"
}

func TestLinesRoundTrip(t *testing.T) {
	cases := []struct{ before, after string }{
		{"", "a\nb\n"},
		{"a\nb\n", ""},
		{"a\nb\nc\n", "a\nB\nc\n"},
		{"1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n", "1\n2\nX\n4\n5\n6\n7\n8\n9\n10\n11\nY\n13\n14\n15\n"},
		{"x\ny\nz\n", "q\nx\nz\nw\n"},
	}
	for _, c := range cases {
		res := Lines("a", c.before, "b", c.after)
		if !res.Minimal || res.Truncated {
			t.Fatalf("small diff must be minimal and complete: %+v", res)
		}
		if got := apply(t, c.before, res.Unified); got != c.after {
			t.Fatalf("diff does not apply:\n%s\ngot %q want %q", res.Unified, got, c.after)
		}
	}
	if res := Lines("a", "x\nlast", "b", "x\nlast\n"); res.Unified != "" {
		t.Fatalf("a missing final newline is not a change: %+v", res)
	}
	if res := Lines("a", "same\n", "b", "same\n"); res.Unified != "" || res.AddedLines+res.RemovedLines != 0 {
		t.Fatalf("identical input must have no diff: %+v", res)
	}
	res := Lines("a", "a\nb\nc\n", "b", "a\nB\nc\n")
	want := "--- a\n+++ b\n@@ -1,3 +1,3 @@\n a\n-b\n+B\n c\n"
	if res.Unified != want || res.AddedLines != 1 || res.RemovedLines != 1 {
		t.Fatalf("unexpected unified diff:\n%s", res.Unified)
	}
}

func TestLinesIsBoundedOnLargeInputs(t *testing.T) {
	var a, b strings.Builder
	for i := 0; i < 20000; i++ {
		fmt.Fprintf(&a, "line %d\n", i)
		fmt.Fprintf(&b, "other %d\n", i)
	}
	res := Lines("a", a.String(), "b", b.String())
	if res.Minimal || !res.Truncated || len(res.Unified) > MaxOutputBytes {
		t.Fatalf("large disjoint diff must fall back and truncate: minimal=%v truncated=%v len=%d", res.Minimal, res.Truncated, len(res.Unified))
	}
	if res.AddedLines != 20000 || res.RemovedLines != 20000 {
		t.Fatalf("counts must cover the full change: %+v", res.AddedLines)
	}
}
