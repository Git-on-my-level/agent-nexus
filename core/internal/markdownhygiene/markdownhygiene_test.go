package markdownhygiene

import (
	"strings"
	"testing"
)

func TestNormalizeConservativeFixes(t *testing.T) {
	input := "###Title   \nBody A→B\n\n\n\n|a|b|\n|---|---|\n|1|2|"
	got := Normalize(input)
	if len(got.Errors) != 0 {
		t.Fatalf("unexpected errors: %#v", got.Errors)
	}
	want := "### Title\n\nBody A → B\n\n| a | b |\n| --- | --- |\n| 1 | 2 |"
	if got.Text != want {
		t.Fatalf("text:\ngot  %q\nwant %q", got.Text, want)
	}
	for _, code := range []string{"heading_spacing", "heading_blank_line", "arrow_spacing", "blank_lines", "trailing_whitespace", "table_pipe_padding"} {
		if !hasWarning(got, code) {
			t.Fatalf("missing warning %q in %#v", code, got.Warnings)
		}
	}
}

func TestNormalizeSkipsCodeAndUnsafeGuesses(t *testing.T) {
	input := "```md\n###No space\nA→B\n```\nUse `A→B`, keep 2kanbans, and leave https://example.test/A→B alone."
	got := Normalize(input)
	if len(got.Errors) != 0 {
		t.Fatalf("unexpected errors: %#v", got.Errors)
	}
	if got.Text != input {
		t.Fatalf("expected no semantic rewrite:\ngot  %q\nwant %q", got.Text, input)
	}
}

func TestNormalizePreservesLongFenceUntilMatchingClose(t *testing.T) {
	input := "````md\n```literal fence\n###No space\nA→B\n```\n````\nAfter A→B"
	got := Normalize(input)
	if len(got.Errors) != 0 {
		t.Fatalf("unexpected errors: %#v", got.Errors)
	}
	want := "````md\n```literal fence\n###No space\nA→B\n```\n````\nAfter A → B"
	if got.Text != want {
		t.Fatalf("long fence handling:\ngot  %q\nwant %q", got.Text, want)
	}
}

func TestNormalizePreservesInlineCodeUntilMatchingBacktickRun(t *testing.T) {
	input := "Keep ``A→B and `literal` ticks`` but change A→B."
	got := Normalize(input)
	if len(got.Errors) != 0 {
		t.Fatalf("unexpected errors: %#v", got.Errors)
	}
	want := "Keep ``A→B and `literal` ticks`` but change A → B."
	if got.Text != want {
		t.Fatalf("inline code handling:\ngot  %q\nwant %q", got.Text, want)
	}
}

func TestNormalizeLeavesUnclosedFenceAsWarning(t *testing.T) {
	got := Normalize("```go\nfmt.Println(\"x\")")
	if len(got.Errors) != 0 {
		t.Fatalf("unexpected hard errors: %#v", got.Errors)
	}
	if !hasWarning(got, "unclosed_fence") {
		t.Fatalf("expected unclosed fence warning, got %#v", got.Warnings)
	}
}

func TestNormalizeRejectsHardHazards(t *testing.T) {
	if got := Normalize("ok\x00bad"); len(got.Errors) == 0 || got.Errors[0].Code != "control_character" {
		t.Fatalf("expected control character error, got %#v", got.Errors)
	}
	if got := Normalize(strings.Repeat("a", MaxLineBytes+1)); len(got.Errors) == 0 || got.Errors[0].Code != "line_too_long" {
		t.Fatalf("expected long line error, got %#v", got.Errors)
	}
}

func TestNormalizeIdempotent(t *testing.T) {
	input := "## Title\n\n| a | b |\n| --- | --- |\n| 1 | 2 |"
	first := Normalize(input)
	second := Normalize(first.Text)
	if first.Text != second.Text {
		t.Fatalf("not idempotent: first=%q second=%q", first.Text, second.Text)
	}
	if len(second.Warnings) != 0 {
		t.Fatalf("second pass should be clean, got %#v", second.Warnings)
	}
}

func hasWarning(result Result, code string) bool {
	for _, warning := range result.Warnings {
		if warning.Code == code {
			return true
		}
	}
	return false
}
