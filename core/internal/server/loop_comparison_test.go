package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

// Portable harness: this exact file also compiles on main without loop code.
// Keep 10k events in every case; ordinary changes only the event type.
func TestLoopBriefComparison(t *testing.T) {
	if os.Getenv("ANX_BRIEF_COMPARE") != "1" {
		t.Skip("opt-in cross-revision benchmark")
	}
	ctx := context.Background()
	h := newPrimitivesTestServer(t)
	s := h.primitiveStore.(*primitives.Store)
	b, err := s.CreateBoard(ctx, "actor-1", map[string]any{"title": "Loops"})
	if err != nil {
		t.Fatal(err)
	}
	w, err := s.CreateWork(ctx, "actor-1", anyString(b["id"]), map[string]any{"title": "Dense loop", "cadence": map[string]any{"every_seconds": 3600}, "goal": map[string]any{"metric": "score", "direction": "up"}})
	if err != nil {
		t.Fatal(err)
	}
	kind := "run_recorded"
	if os.Getenv("ANX_BRIEF_BENCH_MODE") == "ordinary" {
		kind = "card_updated"
	}
	anchor := time.Now().UTC().Truncate(time.Second)
	tx, err := h.workspace.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	refs, _ := json.Marshal([]string{"card:" + anyString(w["id"])})
	for i := 0; i < 10000; i++ {
		payload, _ := json.Marshal(map[string]any{"subject_ref": "card:" + anyString(w["id"]), "summary": "Measured", "payload": map[string]any{"run_id": fmt.Sprint(i), "outcome": "measured", "metrics": map[string]any{"score": float64(i)}}})
		if _, err = tx.ExecContext(ctx, `INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES(?,?,?,'actor-1',?,?,?)`, fmt.Sprintf("scale-loop-%05d", i), kind, anchor.Add(-time.Duration(10000-i)*time.Second).Format(time.RFC3339Nano), w["thread_id"], string(refs), string(payload)); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for done := false; !done; {
		done, err = s.RefreshBriefInitiatives(ctx, 200)
		if err != nil {
			t.Fatal(err)
		}
	}
	pool, capture, err := perfguard.Open("file:" + h.workspace.Layout().DatabasePath + "?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	measured := primitives.NewTestStore(pool, h.workspace.Layout().ArtifactContentDir)
	now := time.Now().UTC()
	samples := []float64{}
	var queries, rows int
	var vm uint64
	for i := 0; i < 21; i++ {
		scope := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "actor-1"})
		capture.Start()
		start := time.Now()
		read, closeRead, err := measured.BeginResourceRead(scope)
		if err != nil {
			t.Fatal(err)
		}
		result, err := measured.Brief(read, primitives.BriefReader{ActorID: "actor-1", Kind: "human"}, now)
		closeRead()
		elapsed := float64(time.Since(start)) / float64(time.Millisecond)
		_, queries, rows = capture.Stop()
		vm = capture.Work().VMSteps
		if err != nil || capture.WorkError() != nil {
			t.Fatal(err, capture.WorkError())
		}
		if len(result["initiatives"].([]map[string]any)) != 1 {
			t.Fatal(result)
		}
		samples = append(samples, elapsed)
	}
	warm := append([]float64(nil), samples[1:]...)
	sort.Float64s(warm)
	t.Logf("COMPARISON mode=%s events=10000 first_ms=%.3f warm_median_ms=%.3f warm_p95_ms=%.3f SQL=%d rows=%d VM=%d samples=%v", kind, samples[0], (warm[(len(warm)-1)/2]+warm[len(warm)/2])/2, warm[len(warm)-2], queries, rows, vm, samples)
}
