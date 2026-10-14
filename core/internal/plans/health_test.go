package plans

import (
	"strings"
	"testing"
	"time"
)

func TestHealthForPromiseStates(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	p := Plan{Steps: []Step{{ID: "a", Title: "Ship a", Due: "2026-10-12"}, {ID: "b", Title: "Ship b", Due: "2026-10-20", After: []string{"a"}}}}
	state := Compute(p, nil, now)
	h := HealthFor(HealthInput{Ref: "card:x", Plan: &p, State: state, Phase: "in_progress"}, now)
	if h.State != PromisePast || h.Promise == nil || h.Promise.SourceRef != "card:x#a" || h.PastPromiseForSeconds == nil || *h.PastPromiseForSeconds != int64(3*24*3600+12*3600) || h.Since == nil || *h.Since != "2026-10-12T00:00:00Z" {
		t.Fatalf("past step due: %+v", h)
	}
	// A plan edit within the horizon before the due meets it; the next open due counts.
	h = HealthFor(HealthInput{Ref: "card:x", Plan: &p, State: state, Phase: "in_progress", PlanEditedAt: time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)}, now)
	if h.State != PromiseMoving || h.Promise.Source != "plan_step" || h.Promise.At != "2026-10-20T00:00:00Z" || h.LastEvidenceAt == nil {
		t.Fatalf("met step due: %+v", h)
	}
	// Nothing dated: no_promise, never a threshold state.
	planless := HealthFor(HealthInput{Ref: "card:y", Phase: "in_progress"}, now)
	if planless.State != PromiseNone || planless.Promise != nil || planless.Since != nil {
		t.Fatalf("planless: %+v", planless)
	}
	// Card due and wake are promises.
	if h := HealthFor(HealthInput{Phase: "ready", CardDue: "2026-10-18"}, now); h.State != PromiseMoving || h.Promise.Source != "due_at" {
		t.Fatalf("due: %+v", h)
	}
	if h := HealthFor(HealthInput{Phase: "ready", Wake: "check again 2026-10-14"}, now); h.State != PromisePast || h.Promise.Source != "wake_condition" {
		t.Fatalf("wake: %+v", h)
	}
	// Done is done: a done or cancelled phase wins over a past promise.
	for _, phase := range []string{"done", "cancelled"} {
		if h := HealthFor(HealthInput{Plan: &p, State: state, Phase: phase}, now); h.State != PromiseDone || h.Promise != nil || h.Since != nil {
			t.Fatalf("%s: %+v", phase, h)
		}
	}
	// A note promise counts until evidence lands after it.
	notes := []PromiseNote{{EventID: "e1", At: now.Add(-48 * time.Hour), Author: "actor:a", NextAt: now.Add(-time.Hour), NextWhat: "land it"}}
	if h := HealthFor(HealthInput{Phase: "in_progress", Notes: notes}, now); h.State != PromisePast || h.Promise.Author == nil || h.Promise.Author.ID != "actor:a" {
		t.Fatalf("note: %+v", h)
	}
	notes = append(notes, PromiseNote{EventID: "e2", At: now.Add(-30 * time.Minute), Author: "actor:a", Evidence: true})
	if h := HealthFor(HealthInput{Phase: "in_progress", Notes: notes}, now); h.State != PromiseNone {
		t.Fatalf("evidence clears note promise: %+v", h)
	}
}

func TestHealthForStepMovementResets(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	p := Plan{Steps: []Step{{ID: "a", Title: "a", Ref: "card:a", Due: "2026-10-14"}}}
	facts := map[string]Fact{"card:a": {Known: true, Status: "in_progress", MovementAt: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)}}
	state := Compute(p, facts, now)
	if h := HealthFor(HealthInput{Plan: &p, State: state, Facts: facts, Phase: "in_progress"}, now); h.State != PromiseNone {
		t.Fatalf("linked step moved inside the horizon: %+v", h)
	}
	facts["card:a"] = Fact{Known: true, Status: "in_progress", MovementAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	if h := HealthFor(HealthInput{Plan: &p, State: state, Facts: facts, Phase: "in_progress"}, now); h.State != PromisePast {
		t.Fatalf("old movement must not meet the due: %+v", h)
	}
}

func TestWakeTime(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"", ""},
		{"no dates here", ""},
		{"2026-11-05", "2026-11-05T00:00:00Z"},
		{"wake when 1.0.559 ships, check 2026-11-05T09:00:00Z", "2026-11-05T09:00:00Z"},
		{"date 2026-11-05 then instant 2026-11-06T08:00:00Z", "2026-11-05T00:00:00Z"},
		{"instant first 2026-11-06T08:00:00+02:00 then date", "2026-11-06T08:00:00+02:00"},
		{"truncated 2026-13-45 is not a date", ""},
		{"wake on 2026-10-08T09:00:00.000Z", "2026-10-08T09:00:00Z"},
		{"2026-10-08T09:00:00.123Z", "2026-10-08T09:00:00.123Z"},
	} {
		got := WakeTime(tc.in)
		if tc.want == "" {
			if !got.IsZero() {
				t.Fatalf("%q: got %v want none", tc.in, got)
			}
			continue
		}
		want, _ := time.Parse(time.RFC3339Nano, tc.want)
		if got.IsZero() || !got.Equal(want) {
			t.Fatalf("%q: got %v want %v", tc.in, got, want)
		}
	}
	// The scan is bounded: a date buried past the first 1024 bytes is not read.
	if got := WakeTime(strings.Repeat("x", 1100) + " 2026-11-05"); !got.IsZero() {
		t.Fatalf("date past the scan bound leaked: %v", got)
	}
	if got := WakeTime(strings.Repeat("x", 1000) + " 2026-11-05"); got.IsZero() {
		t.Fatal("date inside the scan bound was missed")
	}
}

func TestNextStepPrefersReadyCriticalPath(t *testing.T) {
	p := Plan{Steps: []Step{{ID: "a", Title: "Short"}, {ID: "b", Title: "Long", Ref: "github:org/repo#1"}, {ID: "c", Title: "After long", After: []string{"b"}}}}
	now := time.Now()
	state := Compute(p, nil, now)
	if got := ReadyStep(p, state, nil); got == nil || got.ID != "b" || got.Ref != p.Steps[1].Ref {
		t.Fatal(got)
	}
	if got := ReadyStep(p, state, func(s Step) bool { return s.ID != "b" }); got == nil || got.ID != "a" {
		t.Fatal(got)
	}
}
