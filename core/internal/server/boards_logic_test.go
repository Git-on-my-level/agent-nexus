package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
			name:           "accepts resolution refs without explicit resolution in done column",
			resolution:     nil,
			resolutionRefs: []string{"event:done-1"},
			columnKey:      "done",
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
			name:           "rejects canceled resolution",
			resolution:     ptr("canceled"),
			resolutionRefs: []string{"event:canceled-1"},
			columnKey:      "done",
			wantErr:        "resolution must be done",
		},
		{
			name:           "rejects legacy completed alias",
			resolution:     ptr("completed"),
			resolutionRefs: []string{"event:done-1"},
			columnKey:      "done",
			wantErr:        "resolution must be done",
		},
		{
			name:           "rejects legacy superseded alias",
			resolution:     ptr("superseded"),
			resolutionRefs: []string{"event:done-1"},
			columnKey:      "done",
			wantErr:        "resolution must be done",
		},
		{
			name:           "rejects legacy unresolved alias",
			resolution:     ptr("unresolved"),
			resolutionRefs: []string{"event:done-1"},
			columnKey:      "done",
			wantErr:        "resolution must be done",
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
		"summary":            "Card summary",
		"thread_id":          "card-thread-1",
		"column_key":         "ready",
		"assignee_refs":      []string{"actor:actor-1"},
		"document_ref":       "document:doc-1",
		"due_at":             "2026-04-06T00:00:00Z",
		"definition_of_done": []string{"receipt", "sign-off"},
		"resolution":         "",
		"resolution_refs":    []string{},
		"related_refs":       []string{"thread:thread-1"},
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
			ptr("actor-1"),
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
			ptr("actor-1"),
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
			ptr("actor-1"),
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

func TestValidateBoardCardCreateRejectsLegacyThreadFields(t *testing.T) {
	t.Parallel()
	if err := validateBoardCardCreateRequest("", "thr-1", "", "ready", "", "", "", "", nil); err == nil || !strings.Contains(err.Error(), "parent_thread must not") {
		t.Fatalf("expected parent_thread rejection, got %v", err)
	}
	if err := validateBoardCardCreateRequest("", "", "thr-1", "ready", "", "", "", "", nil); err == nil || !strings.Contains(err.Error(), "thread_id must not") {
		t.Fatalf("expected thread_id rejection, got %v", err)
	}
	if err := validateBoardCardCreateRequest("", "", "", "ready", "", "", "thr-1", "", nil); err == nil || !strings.Contains(err.Error(), "before_thread_id") {
		t.Fatalf("expected before_thread_id rejection, got %v", err)
	}
}

func TestBuildBoardUpdatedEventOmitsBoardStatusFields(t *testing.T) {
	t.Parallel()

	event := buildBoardUpdatedEvent(
		map[string]any{"id": "board-1", "title": "Old"},
		map[string]any{"id": "board-1", "title": "New"},
		map[string]any{"title": "New"},
	)

	payload, ok := event["payload"].(map[string]any)
	if !ok {
		t.Fatalf("missing payload: %#v", event["payload"])
	}
	if _, exists := payload["previous_status"]; exists {
		t.Fatalf("expected previous_status to be omitted, got %#v", payload)
	}
	if _, exists := payload["status"]; exists {
		t.Fatalf("expected status to be omitted, got %#v", payload)
	}
	if got := anyString(payload["board_id"]); got != "board-1" {
		t.Fatalf("unexpected board_id: %q", got)
	}
}

func TestValidateBoardCardMoveRejectsThreadAnchors(t *testing.T) {
	t.Parallel()
	if err := validateBoardCardMoveRequest("ready", "", "", "thr-1", ""); err == nil || !strings.Contains(err.Error(), "before_thread_id") {
		t.Fatalf("expected thread anchor rejection, got %v", err)
	}
}

func TestBoardCardMatchesCreateReplayDerivesParentFromRefs(t *testing.T) {
	t.Parallel()
	card := map[string]any{
		"id":                 "card-1",
		"title":              "Card title",
		"thread_id":          "card-thread-1",
		"column_key":         "ready",
		"definition_of_done": []any{},
		"resolution_refs":    []any{},
		"related_refs":       []any{"thread:thread-1"},
		"refs":               []any{"topic:topic-1", "thread:thread-1"},
	}
	if !boardCardMatchesCreateReplay(
		card,
		"",
		"Card title",
		"",
		"",
		"",
		"ready",
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		[]string{"thread:thread-1", "topic:topic-1"},
		nil,
	) {
		t.Fatal("expected replay match when parent is derived from refs only")
	}
}

func TestParseBoardCardPatchInputRejectsMixedAliases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		patch map[string]any
	}{
		{
			name: "assignee refs with assignee",
			patch: map[string]any{
				"assignee_refs": []any{"actor:alice"},
				"assignee":      "alice",
			},
		},
		{
			name: "document ref with pinned document id",
			patch: map[string]any{
				"document_ref":       "document:doc-1",
				"pinned_document_id": "doc-1",
			},
		},
		{
			name: "pinned document id alias",
			patch: map[string]any{
				"pinned_document_id": "doc-1",
			},
		},
		{
			name: "related refs with refs",
			patch: map[string]any{
				"related_refs": []any{"thread:thr-1"},
				"refs":         []any{"topic:top-1"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			_, _, ok := parseBoardCardPatchInput(rec, tt.patch)
			if ok {
				t.Fatal("expected patch parse failure")
			}
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("unexpected status: got %d want %d", rec.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestParseBoardCardPatchInputAcceptsCanonicalFields(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	input, changedFields, ok := parseBoardCardPatchInput(rec, map[string]any{
		"summary":       "Canonical summary",
		"assignee_refs": []any{"actor:alice"},
		"document_ref":  "document:doc-1",
		"related_refs":  []any{"thread:thr-1"},
		"risk":          "high",
	})
	if !ok {
		t.Fatalf("expected patch parse success, got status=%d body=%s", rec.Code, rec.Body.String())
	}
	if input.Body == nil || *input.Body != "Canonical summary" {
		t.Fatalf("unexpected body: %#v", input.Body)
	}
	if input.Assignee == nil || *input.Assignee != "alice" {
		t.Fatalf("unexpected assignee: %#v", input.Assignee)
	}
	if input.PinnedDocumentID == nil || *input.PinnedDocumentID != "doc-1" {
		t.Fatalf("unexpected pinned document id: %#v", input.PinnedDocumentID)
	}
	if input.Refs == nil || len(*input.Refs) != 1 || (*input.Refs)[0] != "thread:thr-1" {
		t.Fatalf("unexpected refs: %#v", input.Refs)
	}
	if len(changedFields) != 5 {
		t.Fatalf("unexpected changed fields: %#v", changedFields)
	}
}

func TestPrepareBoardWriteMapFoldsLegacyPrimaryDocumentID(t *testing.T) {
	t.Parallel()

	t.Run("merges into refs and strips alias", func(t *testing.T) {
		t.Parallel()
		out := prepareBoardWriteMap(map[string]any{
			"title":               "B",
			"primary_document_id": "doc-7",
			"refs":                []any{"thread:t1"},
		})
		if _, ok := out["primary_document_id"]; ok {
			t.Fatal("expected primary_document_id removed")
		}
		refs, err := extractStringSlice(out["refs"])
		if err != nil {
			t.Fatalf("refs: %v", err)
		}
		refs = uniqueSortedStrings(refs)
		want := []string{"document:doc-7", "thread:t1"}
		if len(refs) != len(want) {
			t.Fatalf("refs: got %#v want %#v", refs, want)
		}
		for i := range want {
			if refs[i] != want[i] {
				t.Fatalf("refs: got %#v want %#v", refs, want)
			}
		}
	})

	t.Run("shallow copy does not mutate source map keys for merge", func(t *testing.T) {
		t.Parallel()
		src := map[string]any{"refs": []any{"thread:t1"}, "primary_document_id": "doc-2"}
		_ = prepareBoardWriteMap(src)
		if _, gone := src["primary_document_id"]; !gone {
			t.Fatal("expected source map to still have primary_document_id (prepare copies first)")
		}
	})
}
