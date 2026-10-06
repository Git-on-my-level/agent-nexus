package scopes

import (
	"errors"
	"math"
	"testing"
)

func validMutation() CanonicalMutation {
	return CanonicalMutation{Identity: ResourceIdentity{ScopeID: "s", Kind: "doc", ResourceID: "opaque", RID: 1, CanonicalID: "source", CanonicalVersion: 2}, PreviousVersion: 1, Changes: []Change{{ScopeID: "s", Kind: "doc", ResourceID: "opaque", CanonicalVersion: 2, Family: "work", Audience: "all", After: &Projection{Title: "x"}}}}
}
func TestCanonicalMutationBoundsAndIdentity(t *testing.T) {
	if e := validMutation().Validate(); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name string
		edit func(*CanonicalMutation)
		want error
	}{
		{"wrong_scope", func(m *CanonicalMutation) { m.Changes[0].ScopeID = "private" }, ErrDerivation},
		{"wrong_version", func(m *CanonicalMutation) { m.Changes[0].CanonicalVersion = 1 }, ErrDerivation},
		{"missing_rid", func(m *CanonicalMutation) { m.Identity.RID = 0 }, ErrBudget},
		{"skipped_version", func(m *CanonicalMutation) { m.PreviousVersion = 0 }, ErrBudget},
		{"overflow", func(m *CanonicalMutation) {
			m.PreviousVersion = math.MaxInt64
			m.Identity.CanonicalVersion = math.MinInt64
		}, ErrBudget},
		{"duplicate_stream", func(m *CanonicalMutation) { m.Changes = append(m.Changes, m.Changes[0]) }, ErrBudget},
		{"too_many_streams", func(m *CanonicalMutation) {
			for i := 0; i < MaxStreamsPerScope; i++ {
				m.Changes = append(m.Changes, m.Changes[0])
			}
		}, ErrBudget},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := validMutation()
			tc.edit(&m)
			if e := m.Validate(); !errors.Is(e, tc.want) {
				t.Fatal(e)
			}
		})
	}
}
