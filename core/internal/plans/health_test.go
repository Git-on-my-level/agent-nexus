package plans

import (
	"strings"
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
			got := HealthFor(tc.p, state, tc.movement, old, now, 0, tc.due, DarkInput{})
			if got.State != tc.want || got.Reason == "" || got.Since == nil {
				t.Fatalf("got=%+v want=%s", got, tc.want)
			}
			if tc.want == "stale" && *got.Since != now.Format(time.RFC3339Nano) {
				t.Fatal(got)
			}
		})
	}
	// A longer stale threshold keeps the card out of stale, but with no dated
	// signal anywhere it now reads dark rather than on_track.
	p := Plan{Steps: []Step{{ID: "a", Title: "A"}}}
	state := Compute(p, nil, old, now, 0)
	got := HealthFor(&p, state, old, old, now, 96*time.Hour, "", DarkInput{})
	if got.State != "dark" || got.Since != nil {
		t.Fatal(got)
	}
	// The same card with a signal inside the horizon stays on_track.
	near := Plan{Steps: []Step{{ID: "a", Title: "A", Due: "2026-10-09"}}}
	state = Compute(near, nil, old, now, 0)
	if got := HealthFor(&near, state, old, old, now, 96*time.Hour, "", DarkInput{}); got.State != "on_track" {
		t.Fatal(got)
	}
}

func TestDarkHealth(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	movement := now.Add(-time.Hour)
	plan := func(due string) *Plan {
		return &Plan{Steps: []Step{{ID: "a", Title: "A", Due: due}}}
	}
	for _, tc := range []struct {
		name      string
		due       string
		dark      DarkInput
		want      string
		wantSince string
	}{
		{"no-signal", "", DarkInput{}, "dark", ""},
		{"far-step-due", "2026-11-05", DarkInput{}, "dark", "2026-11-05T00:00:00Z"},
		{"near-step-due", "2026-10-09", DarkInput{}, "on_track", ""},
		{"horizon-boundary", "2026-10-12T12:00:00Z", DarkInput{}, "on_track", ""},
		{"just-past-horizon", "2026-10-12T12:00:01Z", DarkInput{}, "dark", "2026-10-12T12:00:01Z"},
		{"earliest-signal-wins", "2026-10-20", DarkInput{Wake: "revisit 2026-11-05 after the launch"}, "dark", "2026-10-20T00:00:00Z"},
		{"wake-earlier-wins", "2026-11-20", DarkInput{Wake: "revisit 2026-11-05 after the launch"}, "dark", "2026-11-05T00:00:00Z"},
		{"wake-near", "", DarkInput{Wake: "wake on 2026-10-08"}, "on_track", ""},
		{"wake-near-fractional", "", DarkInput{Wake: "wake on 2026-10-08T09:00:00.000Z"}, "on_track", ""},
		{"past-wake-not-dark", "", DarkInput{Wake: "was due 2026-09-01"}, "on_track", ""},
		{"per-card-horizon", "2026-10-20", DarkInput{Threshold: 48 * time.Hour}, "dark", "2026-10-20T00:00:00Z"},
		{"per-card-horizon-covers", "2026-10-20", DarkInput{Threshold: 30 * 24 * time.Hour}, "on_track", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := plan(tc.due)
			state := Compute(*p, nil, movement, now, 0)
			got := HealthFor(p, state, movement, now.Add(-48*time.Hour), now, 0, "", tc.dark)
			if got.State != tc.want {
				t.Fatalf("got=%+v want=%s", got, tc.want)
			}
			if tc.want != "dark" {
				return
			}
			if tc.wantSince == "" {
				if got.Since != nil {
					t.Fatalf("want nil since, got %s", *got.Since)
				}
				return
			}
			if got.Since == nil || *got.Since != tc.wantSince {
				t.Fatalf("got since %v want %s", got.Since, tc.wantSince)
			}
		})
	}
}

func TestDarkLosesToAlarms(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	movement := now.Add(-time.Hour)
	for _, tc := range []struct {
		name string
		p    *Plan
		due  string
		want string
	}{
		{"blocked", &Plan{Steps: []Step{{ID: "a", Title: "A", Status: "blocked"}}}, "", "blocked"},
		{"overdue-step", &Plan{Steps: []Step{{ID: "a", Title: "A", Due: "2026-10-01"}}}, "", "at_risk"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := Compute(*tc.p, nil, movement, now, 0)
			if got := HealthFor(tc.p, state, movement, now.Add(-48*time.Hour), now, 0, tc.due, DarkInput{}); got.State != tc.want {
				t.Fatalf("got=%+v want=%s", got, tc.want)
			}
		})
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
	state := Compute(p, nil, now, now, 0)
	if got := ReadyStep(p, state, nil); got == nil || got.ID != "b" || got.Ref != p.Steps[1].Ref {
		t.Fatal(got)
	}
	if got := ReadyStep(p, state, func(s Step) bool { return s.ID != "b" }); got == nil || got.ID != "a" {
		t.Fatal(got)
	}
}

func TestPlanlessCardHealthUsesPhaseAndDue(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		phase, due string
		movement   time.Time
		want       string
	}{
		{"blocked", "", now, "blocked"},
		{"done", "2020-01-01", now.Add(-100 * time.Hour), "done"},
		{"cancelled", "2020-01-01", now.Add(-100 * time.Hour), "cancelled"},
		{"ready", "2020-01-01", now, "at_risk"},
		{"in_progress", "", now.Add(-100 * time.Hour), "stale"},
	} {
		got := HealthFor(nil, State{}, tc.movement, now.Add(-200*time.Hour), now, 0, tc.due, DarkInput{}, tc.phase)
		if got.State != tc.want {
			t.Fatalf("phase %q: got %+v want %s", tc.phase, got, tc.want)
		}
	}
}
