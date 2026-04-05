package server

import "testing"

func TestValidateBoardCardCreateResolutionInput(t *testing.T) {
	t.Parallel()

	ptr := func(s string) *string { return &s }

	tests := []struct {
		name           string
		resolution     *string
		resolutionRefs []string
		columnKey      string
		wantErr        string
	}{
		{
			name:           "rejects resolution refs without resolution",
			resolution:     nil,
			resolutionRefs: []string{"event:done-1"},
			columnKey:      "done",
			wantErr:        "resolution_refs require resolution",
		},
		{
			name:           "rejects resolution outside done column",
			resolution:     ptr("done"),
			resolutionRefs: []string{"event:done-1"},
			columnKey:      "review",
			wantErr:        "resolution requires column_key done",
		},
		{
			name:           "rejects resolution without refs",
			resolution:     ptr("done"),
			resolutionRefs: nil,
			columnKey:      "done",
			wantErr:        "resolution_refs are required when resolution is set",
		},
		{
			name:           "rejects invalid done refs",
			resolution:     ptr("done"),
			resolutionRefs: []string{"thread:thread-1"},
			columnKey:      "done",
			wantErr:        "resolution_refs must include at least one artifact: or event: ref for resolution done",
		},
		{
			name:           "accepts valid done resolution",
			resolution:     ptr("done"),
			resolutionRefs: []string{"event:done-1"},
			columnKey:      "done",
		},
		{
			name:           "accepts valid canceled resolution",
			resolution:     ptr("canceled"),
			resolutionRefs: []string{"event:canceled-1"},
			columnKey:      "done",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateBoardCardCreateResolutionInput(tt.resolution, tt.resolutionRefs, tt.columnKey)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected success, got %v", err)
				}
				return
			}
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("expected error %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestBoardCardMatchesCreateReplayRejectsCanonicalFieldMismatches(t *testing.T) {
	t.Parallel()

	ptr := func(s string) *string { return &s }
	baseCard := map[string]any{
		"id":                 "card-1",
		"title":              "Card title",
		"body":               "Card summary",
		"parent_thread":      "thread-1",
		"column_key":         "ready",
		"status":             "todo",
		"assignee":           "actor-1",
		"priority":           "high",
		"pinned_document_id": "doc-1",
		"due_at":             "2026-04-06T00:00:00Z",
		"definition_of_done": []string{"receipt", "sign-off"},
		"resolution":         "",
		"resolution_refs":    []string{},
		"refs":               []string{"topic:topic-1", "artifact:artifact-1"},
		"risk":               "low",
	}

	matches := func(dueAt, resolution *string, definitionOfDone, resolutionRefs, refs []string) bool {
		return boardCardMatchesCreateReplay(
			baseCard,
			"card-1",
			"Card title",
			"Card summary",
			"thread-1",
			"",
			"ready",
			"todo",
			ptr("actor-1"),
			ptr("high"),
			ptr("doc-1"),
			dueAt,
			resolution,
			definitionOfDone,
			resolutionRefs,
			refs,
			nil,
		)
	}

	if !matches(ptr("2026-04-06T00:00:00Z"), nil, []string{"sign-off", "receipt"}, nil, []string{"artifact:artifact-1", "topic:topic-1"}) {
		t.Fatal("expected replay matcher to accept equivalent canonical fields")
	}

	tests := []struct {
		name             string
		dueAt            *string
		resolution       *string
		definitionOfDone []string
		resolutionRefs   []string
		refs             []string
	}{
		{name: "due_at", dueAt: ptr("2026-04-07T00:00:00Z"), definitionOfDone: []string{"receipt", "sign-off"}, refs: []string{"topic:topic-1", "artifact:artifact-1"}},
		{name: "definition_of_done", dueAt: ptr("2026-04-06T00:00:00Z"), definitionOfDone: []string{"receipt"}, refs: []string{"topic:topic-1", "artifact:artifact-1"}},
		{name: "resolution", dueAt: ptr("2026-04-06T00:00:00Z"), resolution: ptr("done"), definitionOfDone: []string{"receipt", "sign-off"}, refs: []string{"topic:topic-1", "artifact:artifact-1"}},
		{name: "resolution_refs", dueAt: ptr("2026-04-06T00:00:00Z"), resolution: nil, definitionOfDone: []string{"receipt", "sign-off"}, resolutionRefs: []string{"event:done-1"}, refs: []string{"topic:topic-1", "artifact:artifact-1"}},
		{name: "refs", dueAt: ptr("2026-04-06T00:00:00Z"), resolution: nil, definitionOfDone: []string{"receipt", "sign-off"}, refs: []string{"topic:topic-1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if matches(tt.dueAt, tt.resolution, tt.definitionOfDone, tt.resolutionRefs, tt.refs) {
				t.Fatalf("expected replay mismatch for %s", tt.name)
			}
		})
	}

	t.Run("risk_mismatch_when_omitted", func(t *testing.T) {
		t.Parallel()
		cardHigh := map[string]any{}
		for k, v := range baseCard {
			cardHigh[k] = v
		}
		cardHigh["risk"] = "high"
		if boardCardMatchesCreateReplay(
			cardHigh,
			"card-1",
			"Card title",
			"Card summary",
			"thread-1",
			"",
			"ready",
			"todo",
			ptr("actor-1"),
			ptr("high"),
			ptr("doc-1"),
			ptr("2026-04-06T00:00:00Z"),
			nil,
			[]string{"receipt", "sign-off"},
			nil,
			[]string{"artifact:artifact-1", "topic:topic-1"},
			nil,
		) {
			t.Fatal("expected replay mismatch when stored risk differs from defaulted low")
		}
	})

	t.Run("risk_match_when_explicit", func(t *testing.T) {
		t.Parallel()
		cardHigh := map[string]any{}
		for k, v := range baseCard {
			cardHigh[k] = v
		}
		cardHigh["risk"] = "high"
		rh := "high"
		if !boardCardMatchesCreateReplay(
			cardHigh,
			"card-1",
			"Card title",
			"Card summary",
			"thread-1",
			"",
			"ready",
			"todo",
			ptr("actor-1"),
			ptr("high"),
			ptr("doc-1"),
			ptr("2026-04-06T00:00:00Z"),
			nil,
			[]string{"receipt", "sign-off"},
			nil,
			[]string{"artifact:artifact-1", "topic:topic-1"},
			&rh,
		) {
			t.Fatal("expected replay match when request risk matches stored card")
		}
	})
}
