package scopes

import "testing"

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
