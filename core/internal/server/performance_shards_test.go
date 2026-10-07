package server

import (
	"encoding/json"
	"os"
	"sort"
	"strconv"
	"testing"
)

type performanceShardWeight struct {
	Key          string `json:"key"`
	Milliseconds int    `json:"milliseconds"`
}

func performanceShardSelection(t *testing.T) (int, int) {
	t.Helper()
	v := os.Getenv("ANX_PERFORMANCE_SHARD")
	if v == "" {
		return 0, 1
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > 4 {
		t.Fatal("ANX_PERFORMANCE_SHARD must be 1..4")
	}
	return n, 4
}

// LPT uses measured case costs, grouping all phases and both principals. An
// inventory change requires an explicit new weight, never silently losing cases.
func performanceShardAssignments(t *testing.T, budgets []routeBudget) map[string]int {
	t.Helper()
	raw, err := os.ReadFile("testdata/performance_shard_weights.json")
	if err != nil {
		t.Fatal(err)
	}
	var weights []performanceShardWeight
	if err := json.Unmarshal(raw, &weights); err != nil {
		t.Fatal(err)
	}
	expected := map[string]bool{}
	for _, b := range budgets {
		expected[performanceCaseKey(b.Method, b.Path, b.Case)] = true
	}
	seen := map[string]bool{}
	for _, w := range weights {
		if !expected[w.Key] || seen[w.Key] || w.Milliseconds <= 0 {
			t.Fatalf("invalid/stale shard weight %q", w.Key)
		}
		seen[w.Key] = true
	}
	if len(seen) != len(expected) {
		t.Fatal("shard weights do not cover every route case")
	}
	sort.Slice(weights, func(i, j int) bool {
		if weights[i].Milliseconds == weights[j].Milliseconds {
			return weights[i].Key < weights[j].Key
		}
		return weights[i].Milliseconds > weights[j].Milliseconds
	})
	loads := [4]int{}
	out := map[string]int{}
	for _, w := range weights {
		shard := 0
		for i := 1; i < 4; i++ {
			if loads[i] < loads[shard] {
				shard = i
			}
		}
		loads[shard] += w.Milliseconds
		out[w.Key] = shard + 1
	}
	return out
}

func performanceShardBudgets(t *testing.T, budgets []routeBudget, shard, count int) []routeBudget {
	t.Helper()
	assignments := performanceShardAssignments(t, budgets)
	if shard == 0 && count == 1 {
		return budgets
	}
	if count != 4 || shard < 1 || shard > count {
		t.Fatal("invalid route shard")
	}
	var selected []routeBudget
	for _, b := range budgets {
		if assignments[performanceCaseKey(b.Method, b.Path, b.Case)] == shard {
			selected = append(selected, b)
		}
	}
	if len(selected) == 0 {
		t.Fatal("empty route shard")
	}
	return selected
}

func TestPerformanceShardInventory(t *testing.T) {
	budgets := performanceBudgets(t)
	if len(budgets) < 109 {
		t.Fatal("scale route inventory shrank below 109 cases")
	}
	seen := map[string]bool{}
	for shard := 1; shard <= 4; shard++ {
		for _, b := range performanceShardBudgets(t, budgets, shard, 4) {
			key := performanceCaseKey(b.Method, b.Path, b.Case)
			if seen[key] {
				t.Fatal("duplicate route across shards")
			}
			seen[key] = true
		}
	}
	if len(seen) != len(budgets) {
		t.Fatal("route shards omit cases")
	}
}
