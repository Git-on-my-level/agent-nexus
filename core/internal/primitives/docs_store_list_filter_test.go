package primitives

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"agent-nexus-core/internal/schema"
)

func TestBuildListDocumentsQueryStateFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		filter       DocumentListFilter
		wantFragment string
		wantNot      []string
	}{
		{
			name:         "active",
			filter:       DocumentListFilter{States: []string{"active"}},
			wantFragment: "d.archived_at IS NULL AND d.trashed_at IS NULL",
		},
		{
			name:         "archived",
			filter:       DocumentListFilter{States: []string{"archived"}},
			wantFragment: "d.archived_at IS NOT NULL AND d.trashed_at IS NULL",
		},
		{
			name:         "trashed",
			filter:       DocumentListFilter{States: []string{"trashed"}},
			wantFragment: "d.trashed_at IS NOT NULL",
		},
		{
			name: "active and archived union",
			filter: DocumentListFilter{
				States: []string{"active", "archived"},
			},
			wantFragment: "d.archived_at IS NOT NULL AND d.trashed_at IS NULL",
			wantNot:      []string{}, // union contains both predicates
		},
		{
			name: "trashed only via union",
			filter: DocumentListFilter{
				States: []string{"trashed"},
			},
			wantFragment: "d.trashed_at IS NOT NULL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ff := tt.filter
			ff.States = NormalizeListLifecycleStates(ff.States)
			query, _ := buildListDocumentsQuery(ff)
			if !strings.Contains(query, tt.wantFragment) {
				t.Fatalf("query missing expected fragment %q:\n%s", tt.wantFragment, query)
			}
			for _, frag := range tt.wantNot {
				if strings.Contains(query, frag) {
					t.Fatalf("query unexpectedly contains %q:\n%s", frag, query)
				}
			}
		})
	}
}

func TestLifecycleStatePredicateUnknown(t *testing.T) {
	t.Parallel()
	sql := LifecycleStatePredicate("a", "t", "nope")
	if sql != "1=0" {
		t.Fatalf("got %s", sql)
	}
}

func TestLifecycleStatesMatchContractEnum(t *testing.T) {
	t.Parallel()
	contract, err := schema.Load(filepath.Join("..", "..", "..", "contracts", "anx-schema.yaml"))
	if err != nil {
		t.Fatalf("load contract: %v", err)
	}
	enum, ok := contract.Enums["lifecycle_state"]
	if !ok {
		t.Fatal("contract missing lifecycle_state enum")
	}
	if enum.Policy != schema.EnumPolicyStrict {
		t.Fatalf("lifecycle_state policy = %q, want %q", enum.Policy, schema.EnumPolicyStrict)
	}
	if got, want := CanonicalLifecycleStates(), enum.OrderedValue; !reflect.DeepEqual(got, want) {
		t.Fatalf("canonical lifecycle states = %#v, want contract order %#v", got, want)
	}
}
