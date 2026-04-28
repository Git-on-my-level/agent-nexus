package server

import (
	"strings"
	"testing"
)

func TestNormalizeHumanAttentionResponseProposalsDedupeAndOrder(t *testing.T) {
	t.Parallel()

	raw := []any{"  B ", "A", "B", ""}
	got, err := NormalizeHumanAttentionResponseProposals(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "B" || got[1] != "A" {
		t.Fatalf("got %#v", got)
	}
}

func TestNormalizeHumanAttentionResponseProposalsRejectsTooMany(t *testing.T) {
	t.Parallel()

	raw := []any{"a", "b", "c", "d", "e", "f", "g"}
	_, err := NormalizeHumanAttentionResponseProposals(raw)
	if err == nil || !strings.Contains(err.Error(), "at most") {
		t.Fatalf("expected too many error, got %v", err)
	}
}

func TestNormalizeHumanAttentionResponseProposalsRejectsTooLong(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", 241)
	_, err := NormalizeHumanAttentionResponseProposals([]any{long})
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected length error, got %v", err)
	}
}

func TestNormalizeHumanAttentionResponseProposalsRejectsNonString(t *testing.T) {
	t.Parallel()

	_, err := NormalizeHumanAttentionResponseProposals([]any{"ok", 3})
	if err == nil || !strings.Contains(err.Error(), "string") {
		t.Fatalf("expected type error, got %v", err)
	}
}
