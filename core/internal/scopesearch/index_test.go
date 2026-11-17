package scopesearch

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestExactTermsAndPhraseOnly(t *testing.T) {
	if matches("needles anchor", "anchor needle", []string{"anchor", "needle"}, false) {
		t.Fatal("prefix search accidentally enabled")
	}
	if matches("anchor intervening needle", "anchor needle", nil, true) {
		t.Fatal("noncontiguous phrase matched")
	}
	if !matches("anchor needle", "anchor needle", nil, true) {
		t.Fatal("phrase rejected")
	}
}

type unopenedRepo struct{ calls int }

func (r *unopenedRepo) ReadSearch(context.Context, string, []string, func(Snapshot) error) error {
	r.calls++
	return ErrUnavailable
}
func TestInvalidSearchBoundsAndCursorDoNotOpenRepository(t *testing.T) {
	tokens, err := NewTokens([]byte(strings.Repeat("a", 32)))
	if err != nil {
		t.Fatal(err)
	}
	base := Request{Principal: "reader", Scopes: []string{"s"}, Query: "needle", Limit: 1}
	for _, mutate := range []func(*Request){
		func(r *Request) { r.Limit = 101 }, func(r *Request) { r.Scopes = []string{"s", "s"} }, func(r *Request) { r.Query = "a b c d e f g h i" }, func(r *Request) { r.VerificationBytes = MaxTextBytes - 1 }, func(r *Request) { r.CandidateBudget = MaxCandidates + 1 }, func(r *Request) { r.Continuation = "bad" },
	} {
		req := base
		mutate(&req)
		repo := &unopenedRepo{}
		_, err := Search(context.Background(), repo, tokens, req)
		if err == nil || repo.calls != 0 {
			t.Fatalf("bad input reached repository: %v calls=%d", err, repo.calls)
		}
	}
	if _, err := NewTokens([]byte("short")); !errors.Is(err, ErrCursor) {
		t.Fatal("weak key length accepted")
	}
}
