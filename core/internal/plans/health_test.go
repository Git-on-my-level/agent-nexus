package plans

import (
	"testing"
	"time"
)

func TestInitiativeHealthRules(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	old := now.Add(-72 * time.Hour)
	for _, tc := range []struct {
		name     string
		p        *Plan
		movement time.Time
		due      string
		want     string
	}{
		{"absent", nil, now, "", "no_plan"},
		{"old-planless", nil, old, "", "stale"},
		{"empty", &Plan{Steps: []Step{}}, now, "", "no_plan"},
		{"old-empty", &Plan{Steps: []Step{}}, old, "", "stale"},
		{"complete", &Plan{Steps: []Step{{ID: "a", Title: "A", Status: "done", Due: "2026-10-01"}}}, old, "2026-10-01", "done"},
		{"noncritical-block", &Plan{Steps: []Step{{ID: "a", Title: "A"}, {ID: "b", Title: "B", After: []string{"a"}}, {ID: "c", Title: "C", Status: "blocked"}}}, now, "", "blocked"},
		{"blocked-dependency", &Plan{Steps: []Step{{ID: "a", Title: "A", Status: "blocked"}, {ID: "b", Title: "B", After: []string{"a"}}}}, old, "2026-10-01", "blocked"},
		{"overdue", &Plan{Steps: []Step{{ID: "a", Title: "A", Due: "2026-10-01"}}}, old, "", "at_risk"},
		{"near-card-due", &Plan{Steps: []Step{{ID: "a", Title: "A"}}}, now, now.Add(24 * time.Hour).Format(time.RFC3339), "at_risk"},
		{"stale-boundary", &Plan{Steps: []Step{{ID: "a", Title: "A"}}}, old, "", "stale"},
		{"recent", &Plan{Steps: []Step{{ID: "a", Title: "A", Due: "2026-10-10"}}}, old.Add(time.Second), "", "on_track"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := State{}
			if tc.p != nil {
				state = Compute(*tc.p, nil, tc.movement, now, 0)
			}
			got := HealthFor(tc.p, state, tc.movement, old, now, 0, tc.due)
			if got.State != tc.want || got.Reason == "" || got.Since == nil {
				t.Fatalf("got=%+v want=%s", got, tc.want)
			}
			if tc.want == "stale" && *got.Since != now.Format(time.RFC3339Nano) {
				t.Fatal(got)
			}
		})
	}
	p := Plan{Steps: []Step{{ID: "a", Title: "A"}}}
	state := Compute(p, nil, old, now, 0)
	if got := HealthFor(&p, state, old, old, now, 96*time.Hour, ""); got.State != "on_track" {
		t.Fatal(got)
	}
}

func TestNextStepPrefersReadyCriticalPath(t *testing.T) {
	p := Plan{Steps: []Step{{ID: "a", Title: "Short"}, {ID: "b", Title: "Long", Ref: "github:org/repo#1"}, {ID: "c", Title: "After long", After: []string{"b"}}}}
	now := time.Now()
	state := Compute(p, nil, now, now, 0)
	if got := ReadyStep(p, state, nil); got == nil || got.ID != "b" || got.Ref != p.Steps[1].Ref {
		t.Fatal(got)
	}
	if got := ReadyStep(p, state, func(s Step) bool { return s.ID != "b" }); got == nil || got.ID != "a" {
		t.Fatal(got)
	}
}
