package series

import (
	"context"
	"fmt"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

func TestLoopSeriesGoalUsesDailyScopedValues(t *testing.T) {
	s, _, _, writer := fixture(t)
	ctx := context.Background()
	store := primitives.NewTestStore(s.DB, t.TempDir())
	now := time.Now().UTC().Add(-time.Hour)
	card, err := store.CreateWork(ctx, "owner", "", map[string]any{"title": "Series loop", "goal": map[string]any{"metric": "score", "direction": "down", "baseline": 100., "series_ref": "series:builds?loop=goal"}})
	if err != nil {
		t.Fatal(err)
	}
	for d := 0; d < 20; d++ {
		v := float64(d)
		at := now.Add(-time.Duration(d) * 24 * time.Hour)
		if err = s.Push(ctx, "builds", Point{TS: at.Format(time.RFC3339Nano), Labels: map[string]string{"loop": "goal"}, Value: &v}, writer, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = store.AppendEvent(ctx, "owner", map[string]any{"type": "run_recorded", "summary": "Receipt metric is ignored", "refs": []string{fmt.Sprint(card["ref"])}, "payload": map[string]any{"run_id": "one", "outcome": "measured", "metrics": map[string]any{"score": 999.}}}); err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateWork(ctx, "owner", "", map[string]any{"title": "Shared series goal", "goal": map[string]any{"metric": "score", "direction": "down", "series_ref": "series:builds?loop=goal"}})
	if err != nil {
		t.Fatal(err)
	}
	for drained := false; !drained; {
		drained, err = store.RefreshBriefInitiatives(ctx, 200)
		if err != nil {
			t.Fatal(err)
		}
	}
	brief, err := store.Brief(ctx, primitives.BriefReader{Kind: "human"}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range brief["initiatives"].([]map[string]any) {
		if item["ref"] == second["ref"] {
			found = true
			g := item["loop"].(map[string]any)["goal"].(map[string]any)
			if g["latest"] != 0. {
				t.Fatal(g)
			}
		}
	}
	if !found {
		t.Fatal("shared goal missing")
	}
	data, _, err := store.LoopReport(ctx, "", fmt.Sprint(card["ref"]), "", 24, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	goal := data["loop"].(map[string]any)["goal"].(map[string]any)
	if goal["latest"] != 0. || goal["best"] != 0. || len(goal["points"].([]map[string]any)) != 20 {
		t.Fatal(goal)
	}
	private, err := store.CreateWork(ctx, "owner", "", map[string]any{"title": "Private provenance"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PatchThread(ctx, "owner", fmt.Sprint(private["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	v := 42.
	if err = s.Push(ctx, "builds", Point{TS: now.Format(time.RFC3339Nano), Labels: map[string]string{"loop": "private-goal", "ref": fmt.Sprint(private["ref"])}, Value: &v}, writer, time.Now()); err != nil {
		t.Fatal(err)
	}
	hidden, err := store.CreateWork(ctx, "owner", "", map[string]any{"title": "Hidden metric loop", "goal": map[string]any{"metric": "score", "direction": "up", "series_ref": "series:builds?loop=private-goal"}})
	if err != nil {
		t.Fatal(err)
	}
	scope := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "stranger"})
	scope, closeRead, err := store.BeginResourceRead(scope)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRead()
	data, _, err = store.LoopReport(scope, "", fmt.Sprint(hidden["ref"]), "", 24, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	goal = data["loop"].(map[string]any)["goal"].(map[string]any)
	if goal["latest"] != nil || len(goal["points"].([]map[string]any)) != 0 {
		t.Fatal(goal)
	}
}
