package plans

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestSharedGraphCorpus(t *testing.T) {
	raw, err := os.ReadFile("../../../contracts/fixtures/initiative-plans/graphs.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name          string
		Plan          Plan
		Facts         map[string]string
		Shape, Health string
		CriticalPath  []string `json:"critical_path"`
		NextSteps     []string `json:"next_steps"`
		Done          int
	}
	if err = json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			if err := Validate(tc.Plan); err != nil {
				t.Fatal(err)
			}
			facts := map[string]Fact{}
			for ref, status := range tc.Facts {
				facts[ref] = Fact{Known: true, Status: status}
			}
			state := Compute(tc.Plan, facts, now, now, DefaultStalledAfter)
			if state.Shape != tc.Shape || state.Health != tc.Health || state.Progress.Done != tc.Done || !reflect.DeepEqual(state.CriticalPath, tc.CriticalPath) || !reflect.DeepEqual(state.NextSteps, tc.NextSteps) {
				t.Fatalf("state=%+v expected=%+v", state, tc)
			}
		})
	}
}

func TestRejectInvalidGraphs(t *testing.T) {
	for name, p := range map[string]Plan{
		"cycle":         {Steps: []Step{{ID: "a", Title: "a", After: []string{"b"}}, {ID: "b", Title: "b", After: []string{"a"}}}},
		"duplicate":     {Steps: []Step{{ID: "a", Title: "a"}, {ID: "a", Title: "a"}}},
		"dangling":      {Steps: []Step{{ID: "a", Title: "a", After: []string{"missing"}}}},
		"repeated-edge": {Steps: []Step{{ID: "a", Title: "a"}, {ID: "b", Title: "b", After: []string{"a", "a"}}}},
		"bad-ref":       {Steps: []Step{{ID: "a", Title: "a", Ref: "file:///secret"}}},
		"bad-due":       {Steps: []Step{{ID: "a", Title: "a", Due: "soon"}}},
		"bad-status":    {Steps: []Step{{ID: "a", Title: "a", Status: "nearly"}}},
		"nil-steps":     {},
	} {
		t.Run(name, func(t *testing.T) {
			if Validate(p) == nil {
				t.Fatal("accepted invalid graph")
			}
		})
	}
	p := Plan{Steps: make([]Step, MaxSteps+1)}
	if Validate(p) == nil {
		t.Fatal("accepted oversized graph")
	}
}

func TestHealthMovementAndNoncriticalBlocks(t *testing.T) {
	now := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	old := now.Add(-6 * 24 * time.Hour)
	p := Plan{Steps: []Step{{ID: "a", Title: "a", Ref: "https://github.com/org/repo/pull/1", Status: "active"}}}
	facts := map[string]Fact{p.Steps[0].Ref: {Known: true, Status: "in_progress", MovementAt: old}}
	if got := Compute(p, facts, old, now, DefaultStalledAfter); got.Health != "stalled" {
		t.Fatal(got)
	}
	facts[p.Steps[0].Ref] = Fact{Known: true, Status: "in_progress", MovementAt: now}
	if got := Compute(p, facts, old, now, DefaultStalledAfter); got.Health != "on_track" {
		t.Fatal(got)
	}
	if got := Compute(p, nil, now.Add(-2*time.Hour), now, time.Hour); got.Health != "stalled" {
		t.Fatal(got)
	}
	p.Steps = append(p.Steps, Step{ID: "b", Title: "b", After: []string{"a"}}, Step{ID: "c", Title: "c", Status: "blocked"})
	if got := Compute(p, nil, now, now, DefaultStalledAfter); got.Health != "blocked" {
		t.Fatal("noncritical block:", got)
	}
	// A finished plan reports "done", not "on_track": the legacy field names
	// every state it can name rather than flattening them into green.
	p = Plan{Steps: []Step{{ID: "a", Title: "a", Status: "done"}}}
	if got := Compute(p, nil, old, now, DefaultStalledAfter); got.Health != "done" || got.HealthState != "done" || len(got.CriticalPath) != 0 {
		t.Fatal(got)
	}
	if got := Compute(Plan{Steps: []Step{}}, nil, now, now, DefaultStalledAfter); got.Health != "no_plan" || got.HealthState != "no_plan" {
		t.Fatal("empty plan must not read on_track:", got)
	}
}
