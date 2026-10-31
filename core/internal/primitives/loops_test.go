package primitives

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestLoopMetadataValidation(t *testing.T) {
	valid := []map[string]any{{"cadence": nil, "goal": nil}, {"cadence": map[string]any{"every_seconds": 60, "progress_budget_seconds": 120, "stop_rule": "Enough"}}, {"goal": map[string]any{"metric": "avo.score", "direction": "down", "baseline": 0., "series_ref": "series:score?project=x"}}}
	for _, m := range valid {
		if err := ValidateWorkAnnotations(m); err != nil {
			t.Fatal(m, err)
		}
	}
	bad := []map[string]any{{"cadence": map[string]any{"every_seconds": 59}}, {"cadence": map[string]any{"every_seconds": 60.5}}, {"cadence": map[string]any{"every_seconds": 60, "progress_budget_seconds": 59}}, {"cadence": map[string]any{"every_seconds": 60, "extra": true}}, {"goal": map[string]any{"metric": "X", "direction": "up"}}, {"goal": map[string]any{"metric": "x", "direction": "up", "target": math.Inf(1)}}, {"goal": map[string]any{"metric": "x", "direction": "up", "series_ref": "https://example.com"}}}
	for _, m := range bad {
		if err := ValidateWorkAnnotations(m); err == nil {
			t.Fatal("accepted", m)
		}
	}
}
func TestRunRecordedValidation(t *testing.T) {
	event := func(p map[string]any) map[string]any {
		return map[string]any{"summary": "Measured score", "refs": []string{"card:loop"}, "payload": p}
	}
	if err := ValidateRunRecorded(event(map[string]any{"run_id": "a", "outcome": "measured", "metrics": map[string]any{"x": 0.}, "experiment": map[string]any{"candidate": "a", "decision": "rejected", "score_after": 0.}, "effect_refs": []string{"https://example.com", "card:x"}})); err != nil {
		t.Fatal(err)
	}
	for _, p := range []map[string]any{{"run_id": "a", "outcome": "failed"}, {"run_id": "a", "outcome": "acted", "x": 1}, {"run_id": "a", "outcome": "acted", "metrics": map[string]any{"x": math.NaN()}}, {"run_id": "a", "outcome": "acted", "experiment": map[string]any{"candidate": "a", "decision": "kept", "extra": 0}}, {"run_id": "a", "outcome": "acted", "effect_refs": []string{"url:javascript:alert(1)"}}} {
		if err := ValidateRunRecorded(event(p)); err == nil {
			t.Fatal("accepted", p)
		}
	}
}
func TestLoopHealthPrecedenceAndGoals(t *testing.T) {
	now := time.Now().UTC()
	at := loopTS(now.Add(-time.Hour))
	old := loopTS(now.Add(-10 * 24 * time.Hour))
	last := loopTS(now.Add(-time.Minute))
	card := loopCard{phase: "in_progress", created: old, metadata: map[string]any{"cadence": map[string]any{"every_seconds": 3600}, "goal": map[string]any{"metric": "x", "direction": "down", "baseline": 0.}}}
	for _, tc := range []struct {
		name   string
		f      loopFacts
		health string
	}{{"failing first", loopFacts{LastRun: &old, LastEffect: &at, Runs: []map[string]any{{"outcome": "infra_error"}, {"outcome": "infra_error"}}}, "failing"}, {"overdue before stalled", loopFacts{LastRun: &old}, "overdue"}, {"stalled", loopFacts{LastRun: &last}, "stalled"}, {"healthy", loopFacts{LastRun: &last, LastEffect: &at}, "healthy"}, {"no runs overdue", loopFacts{}, "overdue"}} {
		t.Run(tc.name, func(t *testing.T) {
			got := computeLoop(card, tc.f, now)
			if got["health"] != tc.health {
				t.Fatal(got)
			}
		})
	}
	monitor := loopCard{phase: "in_progress", created: old, metadata: map[string]any{"cadence": map[string]any{"every_seconds": 3600}}}
	if got := computeLoop(monitor, loopFacts{LastRun: &last}, now); got["health"] != "healthy" || got["health_reason"] != "running on cadence" {
		t.Fatal(got)
	}
	if got := computeLoop(monitor, loopFacts{LastRun: &old}, now); got["health"] != "overdue" {
		t.Fatal(got)
	}
	if got := computeLoop(loopCard{phase: "in_progress", created: old, metadata: map[string]any{}}, loopFacts{LastRun: &last}, now); got["health"] != "unknown" {
		t.Fatal(got)
	}
	g := loopGoal(workMap(card.metadata["goal"]), []map[string]any{{"at": old, "value": 3.}, {"at": at, "value": 1.}, {"at": last, "value": 2.}})
	if g["latest"] != 2. || g["best"] != 1. || g["baseline"] != 0. {
		t.Fatal(g)
	}
	card.phase = "done"
	got := computeLoop(card, loopFacts{LastRun: &old}, now)
	if got["health"] != "unknown" || got["health_reason"] != "done" {
		t.Fatal(got)
	}
	if computeLoop(loopCard{metadata: map[string]any{}}, loopFacts{LastRun: &old}, now) == nil {
		t.Fatal("recent receipt must show loop")
	}
}

func TestLoopGoalIncumbentsKeepCandidatePoints(t *testing.T) {
	for _, direction := range []string{"up", "down"} {
		t.Run(direction, func(t *testing.T) {
			points := []map[string]any{{"at": "1", "value": 5., "decision": "rejected"}, {"at": "2", "value": 3., "decision": nil}, {"at": "3", "value": 4., "decision": "kept"}, {"at": "4", "value": 100., "decision": "rejected"}, {"at": "5", "value": -100., "decision": "reverted"}}
			got := loopGoal(map[string]any{"metric": "score", "direction": direction}, points)
			best := 4.
			if direction == "down" {
				best = 3.
			}
			if got["latest"] != 4. || got["best"] != best || got["baseline"] != 3. || len(got["points"].([]map[string]any)) != len(points) {
				t.Fatal(got)
			}
			got = loopGoal(map[string]any{"direction": direction}, []map[string]any{points[0], points[3], points[4]})
			if got["latest"] != nil || got["best"] != nil || got["baseline"] != nil {
				t.Fatal(got)
			}
		})
	}
}

func TestRunEffectURLSchemes(t *testing.T) {
	for _, ref := range []string{"http://example.com", "https://example.com", "HTTP://example.com", "hTtPs://example.com", "url:http://example.com", "url:HTTPS://example.com", "javascript:alert(1)", "url:javascript:alert(1)", "ftp://example.com", "url:ftp://example.com", "https:///missing-host", "url:https://user:secret@example.com"} {
		t.Run(ref, func(t *testing.T) {
			err := ValidateRunRecorded(map[string]any{"summary": "Effect", "refs": []string{"card:loop"}, "payload": map[string]any{"run_id": "a", "outcome": "acted", "effect_refs": []string{ref}}})
			valid := strings.Contains(strings.ToLower(ref), "http") && !strings.Contains(ref, "missing-host") && !strings.Contains(ref, "user:")
			if valid && err != nil || !valid && (err == nil || !strings.Contains(err.Error(), "effect_refs")) {
				t.Fatal(ref, err)
			}
		})
	}
}
