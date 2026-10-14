package primitives

import (
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/plans"
)

func TestPossiblyMootFromFacts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		refs          []string
		facts         map[string]plans.Fact
		wantFlagged   bool
		wantSources   int
		wantReasonHas string
	}{
		{
			name:        "no linked sources never flags",
			refs:        nil,
			facts:       map[string]plans.Fact{},
			wantFlagged: false,
		},
		{
			name: "merged PRs flag a ready card",
			refs: []string{"https://github.com/example/repo/pull/1", "https://github.com/example/repo/pull/2"},
			facts: map[string]plans.Fact{
				"https://github.com/example/repo/pull/1": {Known: true, Status: "done"},
				"https://github.com/example/repo/pull/2": {Known: true, Status: "done"},
			},
			wantFlagged:   true,
			wantSources:   2,
			wantReasonHas: "all 2 linked sources are done",
		},
		{
			name: "one open source blocks the flag",
			refs: []string{"card:one", "card:two"},
			facts: map[string]plans.Fact{
				"card:one": {Known: true, Status: "done"},
				"card:two": {Known: true, Status: "in_progress"},
			},
			wantFlagged: false,
			wantSources: 2,
		},
		{
			// Rule (b): an unobserved source stays unknown and withholds the
			// flag, even though every source ANX did observe is terminal.
			name: "one unobserved url blocks the flag",
			refs: []string{"https://example.test/pr/1", "https://example.test/pr/2"},
			facts: map[string]plans.Fact{
				"https://example.test/pr/1": {Known: true, Status: "merged"},
			},
			wantFlagged: false,
		},
		{
			name: "an invisible card blocks the flag",
			refs: []string{"card:done", "card:private"},
			facts: map[string]plans.Fact{
				"card:done": {Known: true, Status: "done"},
			},
			wantFlagged: false,
		},
		{
			name:        "only unresolvable sources never flag",
			refs:        []string{"card:private"},
			facts:       map[string]plans.Fact{},
			wantFlagged: false,
		},
		{
			name: "cancelled counts as terminal; mixed statuses read done",
			refs: []string{"card:cancelled", "https://example.test/pr/9"},
			facts: map[string]plans.Fact{
				"card:cancelled":            {Known: true, Status: "cancelled"},
				"https://example.test/pr/9": {Known: true, Status: "merged"},
			},
			wantFlagged:   true,
			wantSources:   2,
			wantReasonHas: "done",
		},
		{
			name: "context refs never participate",
			refs: []string{"doc:spec", "document:notes", "topic:launch", "card:done"},
			facts: map[string]plans.Fact{
				"doc:spec":       {Known: true, Status: "archived"},
				"document:notes": {Known: true, Status: "active"},
				"topic:launch":   {Known: true, Status: "active"},
				"card:done":      {Known: true, Status: "done"},
			},
			wantFlagged:   true,
			wantSources:   1,
			wantReasonHas: "all 1 linked source is done",
		},
		{
			name: "an unknown workflow state blocks the flag",
			refs: []string{"card:mystery", "card:done"},
			facts: map[string]plans.Fact{
				"card:mystery": {Known: true, Status: "unknown"},
				"card:done":    {Known: true, Status: "closed"},
			},
			wantFlagged: false,
		},
		{
			name: "a ref absent from the facts blocks the flag",
			refs: []string{"card:done", "card:budget-dropped"},
			facts: map[string]plans.Fact{
				"card:done": {Known: true, Status: "done"},
			},
			wantFlagged: false,
		},
		{
			name: "a known source with an empty status blocks the flag",
			refs: []string{"card:done", "card:silent"},
			facts: map[string]plans.Fact{
				"card:done":   {Known: true, Status: "done"},
				"card:silent": {Known: true, Status: ""},
			},
			wantFlagged: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := PossiblyMootFromFacts(tc.refs, tc.facts)
			if got.Flagged != tc.wantFlagged || got.Sources != tc.wantSources {
				t.Fatalf("got=%+v want flagged=%v sources=%d", got, tc.wantFlagged, tc.wantSources)
			}
			if tc.wantReasonHas != "" && !strings.Contains(got.Reason, tc.wantReasonHas) {
				t.Fatalf("reason=%q want substring %q", got.Reason, tc.wantReasonHas)
			}
			if !tc.wantFlagged && got.Reason != "" {
				t.Fatalf("unflagged result carries evidence: %+v", got)
			}
		})
	}
}

func TestCardPossiblyMootEligibility(t *testing.T) {
	t.Parallel()
	facts := map[string]plans.Fact{
		"https://example.test/pr/1": {Known: true, Status: "done"},
	}
	p := &plans.Plan{Steps: []plans.Step{{ID: "pr", Title: "Merge", Ref: "https://example.test/pr/1"}}}
	for _, tc := range []struct {
		phase       string
		wantFlagged bool
	}{
		{"backlog", true},
		{"ready", true},
		{"in_progress", false},
		{"review", false},
		{"done", false},
		{"cancelled", false},
		{"", false},
	} {
		moot := cardPossiblyMoot(cardHealthInput{Phase: tc.phase}, p, facts)
		if moot.Flagged != tc.wantFlagged {
			t.Fatalf("phase %q: got %+v", tc.phase, moot)
		}
	}
	// Related refs join step refs without a plan.
	moot := cardPossiblyMoot(cardHealthInput{Phase: "ready", Related: []string{"https://example.test/pr/1"}}, nil, facts)
	if !moot.Flagged || moot.Sources != 1 {
		t.Fatal(moot)
	}
}

func TestParseDarkAfter(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]time.Duration{
		"":         0,
		"   ":      0,
		"168h":     168 * time.Hour,
		"30m":      30 * time.Minute,
		"7d":       7 * 24 * time.Hour,
		"0.5d":     12 * time.Hour,
		"-48h":     0,
		"nonsense": 0,
		"7":        0,
		"3650d":    3650 * 24 * time.Hour,
		"3651d":    0,
		"-3d":      0,
	} {
		if got := parseDarkAfter(raw); got != want {
			t.Fatalf("parseDarkAfter(%q)=%v want %v", raw, got, want)
		}
	}
}

func TestDarkAfterDefaultEnvOverride(t *testing.T) {
	t.Setenv("ANX_PLAN_DARK_AFTER", "")
	if got := DarkAfterDefault(); got != plans.DefaultDarkAfter {
		t.Fatalf("default horizon: got %v want %v", got, plans.DefaultDarkAfter)
	}
	t.Setenv("ANX_PLAN_DARK_AFTER", "48h")
	if got := DarkAfterDefault(); got != 48*time.Hour {
		t.Fatalf("env horizon: got %v want 48h", got)
	}
	t.Setenv("ANX_PLAN_DARK_AFTER", "nonsense")
	if got := DarkAfterDefault(); got != plans.DefaultDarkAfter {
		t.Fatalf("invalid env must fall back: got %v", got)
	}
}
