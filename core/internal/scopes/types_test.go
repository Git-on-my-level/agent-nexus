package scopes

import (
	"errors"
	"fmt"
	"testing"
)

func TestImmutableSentinelCompatibility(t *testing.T) {
	for err, message := range map[error]string{
		ErrDenied: "scope unavailable", ErrUpdating: "scope updating",
		ErrBudget:     "scope request exceeds budget",
		ErrDerivation: "cross-scope derivation requires publication",
		ErrClosed:     "scope capability expired",
	} {
		if err.Error() != message || !errors.Is(fmt.Errorf("wrapped: %w", err), err) {
			t.Fatalf("sentinel contract changed: %v", err)
		}
	}
}

func TestStreamBudgets(t *testing.T) {
	var all []Stream
	for i := 0; i < 64; i++ {
		for j := 0; j < 4; j++ {
			all = append(all, Stream{Scope: ID(string(rune(i + 100))), Family: string(rune(j + 100)), Audience: "all"})
		}
	}
	if e := ValidateStreams(all); e != nil {
		t.Fatal(e)
	}
	if e := ValidateStreams(append(all, Stream{"extra", "inbox", "all"})); e != ErrBudget {
		t.Fatal(e)
	}
	if e := ValidateStreams(append(all[:4:4], Stream{all[0].Scope, "fifth", "all"})); e != ErrBudget {
		t.Fatal(e)
	}
	if e := ValidateStreams([]Stream{all[0], all[0]}); e != ErrBudget {
		t.Fatal(e)
	}
}

func TestChangeBounds(t *testing.T) {
	c := Change{ScopeID: "private", Kind: "doc", ResourceID: "opaque", CanonicalVersion: 1, Family: "search", Audience: "all", After: &Projection{Title: "title"}}
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
	c.Before = &Projection{Text: string(make([]byte, MaxValueBytes+1))}
	if e := c.Validate(); e != ErrBudget {
		t.Fatal(e)
	}
}
