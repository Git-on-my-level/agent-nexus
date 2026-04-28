package app

import (
	"slices"
	"testing"
)

func TestAppendLifecycleStatesForHTTPList(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                                                         string
		explicit                                                     string
		includeArchived, archivedOnly, includeTrashed, trashedOnly bool
		wantStates                                                   []string
		wantErr                                                      bool
	}{
		{
			name:            "explicit active merges include archived",
			explicit:        "active",
			includeArchived: true,
			wantStates:      []string{"active", "archived"},
		},
		{
			name:           "explicit active merges include trashed",
			explicit:       "active",
			includeTrashed: true,
			wantStates:     []string{"active", "trashed"},
		},
		{
			name:       "explicit only",
			explicit:   "archived",
			wantStates: []string{"archived"},
		},
		{
			name:       "default omits query",
			wantStates: nil,
		},
		{
			name:           "include trashed without explicit state",
			includeTrashed: true,
			wantStates:     []string{"active", "trashed"},
		},
		{
			name:         "archived only",
			archivedOnly: true,
			wantStates:   []string{"archived"},
		},
		{
			name:         "state active conflicts with archived-only",
			explicit:     "active",
			archivedOnly: true,
			wantErr:      true,
		},
		{
			name:        "state active conflicts with trashed-only",
			explicit:    "active",
			trashedOnly: true,
			wantErr:     true,
		},
		{
			name:     "bad state token",
			explicit: "pending",
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var q []queryParam
			err := appendLifecycleStatesForHTTPList(&q, tt.explicit, tt.includeArchived, tt.archivedOnly, tt.includeTrashed, tt.trashedOnly)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := queryValuesFromParams(q)["state"]
			if tt.wantStates == nil {
				if len(got) > 0 {
					t.Fatalf("want no state query, got %#v", got)
				}
				return
			}
			if !slices.Equal(got, tt.wantStates) {
				t.Fatalf("want state %#v, got %#v", tt.wantStates, got)
			}
		})
	}
}
