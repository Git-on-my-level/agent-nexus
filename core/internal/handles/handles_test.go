package handles

import (
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", " Roadmap Alpha ", "roadmap-alpha"},
		{"non ascii", "Café Roadmap", "caf-roadmap"},
		{"reserved", "settings", ""},
		{"uuid looking", "550e8400-e29b-41d4-a716-446655440000", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Normalize(tt.in); got != tt.want {
				t.Fatalf("Normalize(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFallbackIsSpeakableAndDeterministicForSeed(t *testing.T) {
	t.Parallel()
	a := Fallback("thread-1")
	b := Fallback("thread-1")
	if a == "" || a != b {
		t.Fatalf("expected deterministic seeded fallback, got %q and %q", a, b)
	}
	if Normalize("你好") != "" && Candidate("你好", "thread-1") == "" {
		t.Fatal("expected non-ASCII-only input to receive fallback candidate")
	}
}

func TestShortCandidateRevalidatesTruncatedNames(t *testing.T) {
	for name := range reserved {
		title := name + " " + strings.Repeat("x", 40)
		got := ShortCandidate(title, "stable-seed", 40)
		if !ValidSlug(got) || len(got) > 40 || got != ShortCandidate(title, "stable-seed", 40) {
			t.Fatalf("%s: %q", title, got)
		}
	}
	for _, title := range []string{strings.Repeat("z", 80), "ordinary words " + strings.Repeat("word ", 20), "550e8400-e29b-41d4-a716-446655440000 " + strings.Repeat("x", 40)} {
		got := ShortCandidate(title, "stable-seed", 40)
		if !ValidSlug(got) || len(got) > 40 {
			t.Fatalf("%q: %q", title, got)
		}
	}
}
