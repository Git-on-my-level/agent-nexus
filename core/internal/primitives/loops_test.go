package primitives

import (
	"math"
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
	for _, p := range []map[string]any{{"run_id": "a", "outcome": "failed"}, {"run_id": "a", "outcome": "acted", "x": 1}, {"run_id": "a", "outcome": "acted", "metrics": map[string]any{"x": math.NaN()}}, {"run_id": "a", "outcome": "acted", "experiment": map[string]any{"candidate": "a", "decision": "kept", "extra": 0}}, {"run_id": "a", "outcome": "acted", "effect_refs": []string{"http://example.com"}}} {
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
