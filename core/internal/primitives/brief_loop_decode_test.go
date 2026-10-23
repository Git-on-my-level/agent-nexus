package primitives

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestBriefLoopFactsDecodePreservesOutputAndBoundsAllocations(t *testing.T) {
	now := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	stamp := loopTS(now.Add(-time.Hour))
	facts := loopFacts{LastRun: &stamp, LastEffect: &stamp, Counts: []map[string]any{{"week": float64(1), "outcome": "acted", "n": float64(50), "effects": float64(50)}}}
	for i := 0; i < 50; i++ {
		facts.Runs = append(facts.Runs, map[string]any{"at": stamp, "outcome": "acted", "summary": strings.Repeat("receipt context ", 20), "run_id": fmt.Sprint(i), "experiment": map[string]any{"decision": "kept", "candidate": fmt.Sprint(i)}, "metrics": map[string]any{"score": i}, "effect_refs": []string{"https://example.test/evidence"}})
	}
	facts.Points = []map[string]any{{"at": stamp, "value": float64(42), "decision": "kept"}}
	raw, err := json.Marshal(facts)
	if err != nil {
		t.Fatal(err)
	}
	var full, brief loopFacts
	fullAllocs := testing.AllocsPerRun(20, func() {
		full = loopFacts{}
		if err := json.Unmarshal(raw, &full); err != nil {
			t.Fatal(err)
		}
	})
	briefAllocs := testing.AllocsPerRun(20, func() {
		brief = loopFacts{}
		if err := decodeBriefLoopFacts(raw, &brief); err != nil {
			t.Fatal(err)
		}
	})
	c := loopCard{phase: "ready", metadata: map[string]any{"cadence": map[string]any{"every_seconds": 3600}, "goal": map[string]any{"metric": "score", "direction": "up"}}}
	if !reflect.DeepEqual(computeLoop(c, full, now), computeLoop(c, brief, now)) {
		t.Fatal("brief loop output changed")
	}
	golden, err := os.ReadFile("testdata/brief_loop_decode.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var want, got any
	if err := json.Unmarshal(golden, &want); err != nil {
		t.Fatal(err)
	}
	rendered, err := json.Marshal(computeLoop(c, brief, now))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(rendered, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("brief loop golden changed: %s", rendered)
	}
	t.Logf("loop decode allocations full=%.0f brief=%.0f", fullAllocs, briefAllocs)
	if len(brief.Runs) > 12 || briefAllocs > fullAllocs*0.6 {
		t.Fatalf("brief decoded unused receipt detail: runs=%d allocations %.0f / %.0f", len(brief.Runs), briefAllocs, fullAllocs)
	}
}
