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
		{"blocked", false},
		{"in_review", false},
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

func TestNextAskOpenPhaseFollowsBriefPlacement(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"backlog", "ready", "in_progress", "review", "blocked", "in_review"} {
		if !nextAskOpenPhase(phase) {
			t.Fatalf("%s is an open brief placement", phase)
		}
	}
	for _, phase := range []string{"done", "cancelled"} {
		if nextAskOpenPhase(phase) {
			t.Fatalf("%s is closed", phase)
		}
	}
	if briefPlacement("blocked") != "in_progress" || briefPlacement("in_review") != "review" {
		t.Fatal("placement aliases moved")
	}
}

func TestMergeNextAskKeepsBothRules(t *testing.T) {
	t.Parallel()
	base := PossiblyMoot{Flagged: true, Reason: "all 1 linked source is done", Sources: 1}
	ask := PossiblyMoot{Flagged: true, Reason: nextAskDecidedCode, Code: nextAskDecidedCode, AskRef: "event:ask-1", DecidedAt: "2026-10-14T08:00:00Z"}
	got := mergeNextAsk(base, ask)
	if !got.Flagged || got.Reason != base.Reason || got.Code != nextAskDecidedCode || got.AskRef != "event:ask-1" || got.DecidedAt == "" || got.Sources != 1 {
		t.Fatalf("both rules = %+v", got)
	}
}

func TestNextAskDecidedMoot(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	decided := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	asks := func(state, outcome, reason string, visible bool) map[string]citedAsk {
		return map[string]citedAsk{
			"ask-1": {ID: "ask-1", Kind: "ask", State: state, Outcome: outcome, Reason: reason, AnsweredAt: decided, Visible: visible},
		}
	}
	for _, tc := range []struct {
		name        string
		text        string
		asks        map[string]citedAsk
		wantFlagged bool
		wantRef     string
	}{
		{
			name:        "answered ask flags",
			text:        "next step waits on event:ask-1",
			asks:        asks("answered", "answered", "", true),
			wantFlagged: true,
			wantRef:     "event:ask-1",
		},
		{
			name:        "resolved outcome flags",
			text:        "blocked on event:ask-1",
			asks:        asks("answered", "resolved", "", true),
			wantFlagged: true,
			wantRef:     "event:ask-1",
		},
		{
			name:        "withdrawn ask flags",
			text:        "still pointing at event:ask-1",
			asks:        asks("withdrawn", "", "", true),
			wantFlagged: true,
			wantRef:     "event:ask-1",
		},
		{
			name:        "expired open ask flags",
			text:        "waiting on event:ask-1",
			asks:        map[string]citedAsk{"ask-1": {ID: "ask-1", Kind: "ask", State: "open", ExpiresAt: "2026-10-01T00:00:00Z", Visible: true}},
			wantFlagged: true,
			wantRef:     "event:ask-1",
		},
		{
			name: "review that was answered flags",
			text: "review event:rev-1",
			asks: map[string]citedAsk{
				"rev-1": {ID: "rev-1", Kind: "review", State: "answered", Outcome: "approved", AnsweredAt: decided, Visible: true},
			},
			wantFlagged: true,
			wantRef:     "event:rev-1",
		},
		{
			name:        "open ask does not flag",
			text:        "next step waits on event:ask-1",
			asks:        asks("open", "", "", true),
			wantFlagged: false,
		},
		{
			name:        "needs_context is still waiting",
			text:        "event:ask-1",
			asks:        asks("answered", "needs_context", "", true),
			wantFlagged: false,
		},
		{
			name: "expired needs_context flags",
			text: "event:ask-1",
			asks: map[string]citedAsk{"ask-1": {
				ID: "ask-1", Kind: "ask", State: "answered", Outcome: "needs_context",
				AnsweredAt: decided, ExpiresAt: "2026-10-01T00:00:00Z", Visible: true,
			}},
			wantFlagged: true,
			wantRef:     "event:ask-1",
		},
		{
			name:        "prose without a ref does not flag",
			text:        "waiting on the budget ask David already answered",
			asks:        asks("answered", "answered", "", true),
			wantFlagged: false,
		},
		{
			name:        "invisible ask does not flag and is not named",
			text:        "next step waits on event:ask-1",
			asks:        asks("answered", "answered", "", false),
			wantFlagged: false,
		},
		{
			name:        "a ref that is not an ask does not flag",
			text:        "see event:note-9",
			asks:        map[string]citedAsk{},
			wantFlagged: false,
		},
		{
			name: "newer note text without the ref does not flag",
			text: "shipped the draft",
			asks: asks("answered", "answered", "", true),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := nextAskDecidedMoot(tc.text, tc.asks, now)
			if got.Flagged != tc.wantFlagged {
				t.Fatalf("flagged=%v want %v (%+v)", got.Flagged, tc.wantFlagged, got)
			}
			if !tc.wantFlagged {
				if got.Reason != "" || got.Code != "" || got.AskRef != "" || got.DecidedAt != "" {
					t.Fatalf("unflagged result carries evidence: %+v", got)
				}
				return
			}
			if got.Code != nextAskDecidedCode || got.Reason != nextAskDecidedCode || got.AskRef != tc.wantRef || got.DecidedAt == "" {
				t.Fatalf("got %+v want code %s ref %s", got, nextAskDecidedCode, tc.wantRef)
			}
			if _, err := time.Parse(time.RFC3339, got.DecidedAt); err != nil {
				t.Fatal(err)
			}
		})
	}
}
