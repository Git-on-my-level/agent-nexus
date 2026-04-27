package primitives

import (
	"strings"
	"testing"
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
			filter:       DocumentListFilter{State: "active"},
			wantFragment: "d.archived_at IS NULL AND d.trashed_at IS NULL",
		},
		{
			name:         "archived",
			filter:       DocumentListFilter{State: "archived"},
			wantFragment: "d.archived_at IS NOT NULL AND d.trashed_at IS NULL",
		},
		{
			name:         "trashed",
			filter:       DocumentListFilter{State: "trashed"},
			wantFragment: "d.trashed_at IS NOT NULL",
		},
		{
			name: "state active overrides trashed_only",
			filter: DocumentListFilter{
				State:       "active",
				TrashedOnly: true,
			},
			wantFragment: "d.archived_at IS NULL AND d.trashed_at IS NULL",
			wantNot:      []string{"d.trashed_at IS NOT NULL"},
		},
		{
			name: "legacy trashed_only when state empty",
			filter: DocumentListFilter{
				TrashedOnly: true,
			},
			wantFragment: "d.trashed_at IS NOT NULL",
			wantNot:      []string{"d.archived_at IS NULL AND d.trashed_at IS NULL"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			query, _ := buildListDocumentsQuery(tt.filter)
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

func TestBuildListDocumentsQueryInvalidStateDefensive(t *testing.T) {
	t.Parallel()

	query, _ := buildListDocumentsQuery(DocumentListFilter{State: "unknown"})
	if !strings.Contains(query, "1=0") {
		t.Fatalf("expected defensive empty match for invalid state, got:\n%s", query)
	}
}
