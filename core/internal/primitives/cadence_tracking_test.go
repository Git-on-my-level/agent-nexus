package primitives

import (
	"context"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/plans"
)

func TestCadenceCardsAreTrackedOnReadsAndBrief(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	card, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "Loop without receipts", "handle": "loop-tracking", "cadence": map[string]any{"every_seconds": 3600}})
	if err != nil {
		t.Fatal(err)
	}
	assigned := time.Now().UTC()
	check := func(now time.Time, want string, awaiting bool) {
		t.Helper()
		copy := workClone(card)
		if err := s.EnrichCardPlans(ctx, []map[string]any{copy}, nil, now); err != nil {
			t.Fatal(err)
		}
		h := copy["plan_health"].(plans.Health)
		if h.State != want || h.Promise == nil || h.Promise.Source != "cadence" {
			t.Errorf("plan health=%+v want=%s", h, want)
		}
		if awaiting && !strings.Contains(strings.ToLower(h.Reason), "awaiting first run") {
			t.Errorf("reason=%q", h.Reason)
		}
		if copy["work_summary"].(*WorkSummary).Status.State != want {
			t.Error("summary disagrees", copy["work_summary"])
		}
		refs, err := s.ResolveRefs(ctx, []string{workString(card["ref"])}, nil, now)
		if err != nil {
			t.Fatal(err)
		}
		if len(refs) != 1 || refs[0].PlanHealth.State != want {
			t.Errorf("ref preview=%+v", refs)
		}
		for drained := false; !drained; {
			drained, err = s.RefreshBriefInitiatives(ctx, 200)
			if err != nil {
				t.Fatal(err)
			}
		}
		brief, err := s.Brief(ctx, BriefReader{ActorID: "owner", Kind: "human"}, now)
		if err != nil {
			t.Fatal(err)
		}
		loops := brief["loops"].([]map[string]any)
		if len(loops) != 1 {
			t.Fatalf("loops=%v", loops)
		}
		if loops[0]["state"] != want || workMap(loops[0]["promise"])["source"] != "cadence" {
			t.Errorf("brief=%v want=%s", loops[0], want)
		}
		if want == plans.PromisePast && workMap(loops[0]["loop"])["health"] != "overdue" {
			t.Errorf("loop=%v", loops[0])
		}
	}
	check(assigned.Add(time.Hour), plans.PromiseMoving, true)
	check(assigned.Add(3*time.Hour), plans.PromisePast, false)
	event, err := s.AppendEvent(ctx, "owner", map[string]any{"type": "run_recorded", "summary": "Ran once", "refs": []string{workString(card["ref"])}, "payload": map[string]any{"run_id": "first", "outcome": "no_op"}})
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := time.Parse(time.RFC3339Nano, workString(event["ts"]))
	if err != nil {
		t.Fatal(err)
	}
	check(recorded.Add(time.Hour), plans.PromiseMoving, false)
	check(recorded.Add(3*time.Hour), plans.PromisePast, false)
}

func TestCadenceTrackingBoundaryAndCompletion(t *testing.T) {
	at := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	loop := map[string]any{"every_seconds": 3600, "next_run_due_at": loopTS(at.Add(time.Hour)), "health_reason": "no runs"}
	for _, tc := range []struct {
		phase string
		now   time.Time
		want  string
	}{
		{"ready", at.Add(2 * time.Hour), plans.PromiseMoving},
		{"ready", at.Add(2*time.Hour + time.Nanosecond), plans.PromisePast},
		{"done", at.Add(3 * time.Hour), plans.PromiseDone},
		{"cancelled", at.Add(3 * time.Hour), plans.PromiseDone},
	} {
		h, ok := cadenceTrackingHealth(loop, tc.phase, "card:loop", tc.now)
		if !ok || h.State != tc.want {
			t.Fatalf("health=%+v want=%s", h, tc.want)
		}
		if tc.want == plans.PromiseDone && h.Promise != nil {
			t.Fatal("terminal cadence still promised", h)
		}
	}
	if _, ok := cadenceTrackingHealth(map[string]any{}, "ready", "card:ordinary", at); ok {
		t.Fatal("ordinary card acquired cadence")
	}
}
