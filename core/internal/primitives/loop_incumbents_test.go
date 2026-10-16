package primitives

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestLoopIncumbentsSQLCacheAndImprovement(t *testing.T) {
	for _, tc := range []struct {
		direction                                    string
		before, after, rejectedBefore, rejectedAfter float64
		improved                                     int
	}{
		{"up", 5, 6, 100, 90, 1}, {"up", 5, 4, 90, 100, 0}, {"down", 5, 4, -100, -90, 1}, {"down", 5, 6, -90, -100, 0},
	} {
		t.Run(fmt.Sprintf("%s/%g", tc.direction, tc.after), func(t *testing.T) {
			ctx := context.Background()
			ws, err := initializeTestWorkspace(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
			board, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Incumbents"})
			if err != nil {
				t.Fatal(err)
			}
			card, err := s.CreateWork(ctx, "owner", workString(board["id"]), map[string]any{"title": "Optimizer", "goal": map[string]any{"metric": "score", "direction": tc.direction}})
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			for i, run := range []struct {
				age      time.Duration
				value    float64
				decision string
			}{
				{10 * 24 * time.Hour, tc.before, ""}, {9 * 24 * time.Hour, tc.rejectedBefore, "rejected"}, {24 * time.Hour, tc.after, "kept"}, {time.Hour, tc.rejectedAfter, "rejected"}, {30 * time.Minute, tc.rejectedAfter, "reverted"},
			} {
				payload := map[string]any{"run_id": fmt.Sprint(i), "outcome": "measured", "metrics": map[string]any{"score": run.value}}
				if run.decision != "" {
					payload["experiment"] = map[string]any{"candidate": fmt.Sprint(i), "decision": run.decision, "score_after": run.value}
					delete(payload, "metrics")
				}
				event, err := s.AppendEvent(ctx, "owner", map[string]any{"type": "run_recorded", "summary": "Candidate evaluated", "refs": []string{workString(card["ref"])}, "payload": payload})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = ws.DB().Exec(`UPDATE events SET ts=? WHERE id=?`, loopTS(now.Add(-run.age)), event["id"]); err != nil {
					t.Fatal(err)
				}
			}
			check := func(cached bool) {
				t.Helper()
				at := time.Now().UTC()
				scope := WithRequestAccessScope(ctx, AccessScope{ActorID: "owner"})
				cache, err := s.cachedLoopFacts(scope, []string{workString(card["id"])}, at)
				if err != nil || (len(cache) > 0) != cached {
					t.Fatal("cache", cache, err)
				}
				states, facts, err := s.loadLoopStates(scope, []string{workString(card["id"])}, at)
				if err != nil {
					t.Fatal(err)
				}
				g := workMap(states[workString(card["id"])]["goal"])
				f := facts[workString(card["id"])]
				best := tc.before
				if tc.direction == "up" && tc.after > best || tc.direction == "down" && tc.after < best {
					best = tc.after
				}
				if g["latest"] != tc.after || g["best"] != best || len(g["points"].([]map[string]any)) != 5 {
					t.Fatal(g)
				}
				if f.BestBefore == nil || *f.BestBefore != tc.before || f.BestAfter == nil || *f.BestAfter != tc.after {
					t.Fatal(f)
				}
				report, _, err := s.LoopReport(scope, workString(board["ref"]), "", "", 24, at)
				if err != nil {
					t.Fatal(err)
				}
				if workMap(report["stats"])["improved_7d"] != tc.improved {
					t.Fatal(report)
				}
			}
			check(false)
			for done := false; !done; {
				done, err = s.RefreshBriefInitiatives(ctx, 200)
				if err != nil {
					t.Fatal(err)
				}
			}
			check(true)
		})
	}
}

func TestLoopLatestIncumbentBeyondDisplayLimit(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	card, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "Rejection streak", "goal": map[string]any{"metric": "score", "direction": "up"}})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 62; i++ {
		payload := map[string]any{"run_id": fmt.Sprint(i), "outcome": "measured", "metrics": map[string]any{"score": float64(i + 1)}}
		if i > 0 {
			payload["experiment"] = map[string]any{"candidate": fmt.Sprint(i), "decision": "rejected"}
		}
		if _, err = s.AppendEvent(ctx, "owner", map[string]any{"type": "run_recorded", "summary": "Measured", "refs": []string{workString(card["ref"])}, "payload": payload}); err != nil {
			t.Fatal(err)
		}
	}
	for done := false; !done; {
		done, err = s.RefreshBriefInitiatives(ctx, 200)
		if err != nil {
			t.Fatal(err)
		}
	}
	states, _, err := s.loadLoopStates(ctx, []string{workString(card["id"])}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	g := workMap(states[workString(card["id"])]["goal"])
	if g["latest"] != 1. || g["best"] != 1. || len(g["points"].([]map[string]any)) != 60 {
		t.Fatal(g)
	}
}
