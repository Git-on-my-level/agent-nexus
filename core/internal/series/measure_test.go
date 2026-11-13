package series

import (
	"context"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

// An initiative with a measure stays an initiative (it is not recurring work)
// and its brief row carries the computed reading: latest, the reading about a
// week earlier, and whether the target is met.
func TestInitiativeMeasureShowsTheReadingOnTheInitiativeRow(t *testing.T) {
	s, _, _, writer := fixture(t)
	ctx := context.Background()
	store := primitives.NewTestStore(s.DB, t.TempDir())
	now := time.Now().UTC().Add(-time.Hour)
	card, err := store.CreateWork(ctx, "owner", "", map[string]any{"title": "Churn: 10% monthly", "initiative": true, "phase": "in_progress",
		"measure": map[string]any{"label": "Monthly paying churn", "direction": "down", "target": 10., "unit": "%", "series_ref": "series:builds?plan=paid"}})
	if err != nil {
		t.Fatal(err)
	}
	bare, err := store.CreateWork(ctx, "owner", "", map[string]any{"title": "Conversion: 35%", "initiative": true, "phase": "in_progress",
		"measure": map[string]any{"label": "Buyers who subscribe", "direction": "up", "target": 35.}})
	if err != nil {
		t.Fatal(err)
	}
	for d := 0; d < 10; d++ {
		v := 12. + float64(d)*0.5
		if err = s.Push(ctx, "builds", Point{TS: now.Add(-time.Duration(d) * 24 * time.Hour).Format(time.RFC3339Nano), Labels: map[string]string{"plan": "paid"}, Value: &v}, writer, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	for drained := false; !drained; {
		if drained, err = store.RefreshBriefInitiatives(ctx, 200); err != nil {
			t.Fatal(err)
		}
	}
	brief, err := store.Brief(ctx, primitives.BriefReader{ActorID: "owner", Kind: "human"}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if loops := brief["loops"].([]map[string]any); len(loops) != 0 {
		t.Fatalf("a measure must not make recurring work: %v", loops)
	}
	seen := 0
	for _, item := range brief["initiatives"].([]map[string]any) {
		m, _ := item["measure"].(map[string]any)
		switch item["ref"] {
		case card["ref"]:
			seen++
			if m["latest"] != 12. || m["previous"] != 15.5 || m["on_target"] != false || m["age_seconds"] == nil || len(m["points"].([]map[string]any)) != 10 || m["label"] != "Monthly paying churn" {
				t.Fatalf("measure=%v", m)
			}
		case bare["ref"]:
			seen++
			if m == nil || m["latest"] != nil || m["target"] != 35. {
				t.Fatalf("a measure with no series must read as no reading: %v", m)
			}
		}
	}
	if seen != 2 {
		t.Fatalf("initiatives=%v", brief["initiatives"])
	}
	if _, err = store.CreateWork(ctx, "owner", "", map[string]any{"title": "Bad measure", "measure": map[string]any{"label": "x", "direction": "sideways", "target": 1.}}); err == nil {
		t.Fatal("an invalid direction must be refused")
	}
}
