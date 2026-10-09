package primitives_test

import (
	"context"
	"testing"
	"time"

	"agent-nexus-core/internal/plans"
)

func TestInitiativeBoardRoleSelectionAndFallback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, defaultBoard := newWorkTestStore(t)
	designated, err := s.CreateBoard(ctx, "actor-1", map[string]any{"title": "Initiatives", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	if designated["role"] != "initiatives" {
		t.Fatal(designated)
	}
	for _, id := range []string{defaultBoard, designated["id"].(string)} {
		if _, err := s.CreateWork(ctx, "actor-1", id, map[string]any{"title": "Work"}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	read := func(want int, hint bool, at time.Time, health string) {
		t.Helper()
		overview, err := s.OverviewVisible(ctx, nil, nil, nil, at, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		work := overview["work"].(map[string]any)
		tiles := overview["initiatives"].(map[string]any)
		if work["total"] != 2 || tiles["count"] != want || (tiles["hint"] != nil) != hint {
			t.Fatalf("work=%v tiles=%v", work, tiles)
		}
		for _, tile := range tiles["items"].([]map[string]any) {
			if tile["plan_state"] != nil || tile["geometry"] != nil || tile["health"].(map[string]any)["status"] != plans.LegacyHealth(health) || tile["plan_health"].(plans.Health).State != health {
				t.Fatal(tile)
			}
		}
	}
	read(1, false, now, "no_plan")
	updated, err := s.UpdateBoard(ctx, "actor-1", designated["id"].(string), map[string]any{"summary": "Edited"}, nil)
	if err != nil || updated["role"] != "initiatives" {
		t.Fatalf("board=%v err=%v", updated, err)
	}
	got, err := s.GetBoard(ctx, designated["id"].(string))
	if err != nil || got["role"] != "initiatives" {
		t.Fatalf("board=%v err=%v", got, err)
	}
	read(1, false, now.Add(2*time.Hour), "stale")
	if _, err = s.UpdateBoard(ctx, "actor-1", designated["id"].(string), map[string]any{"role": ""}, nil); err != nil {
		t.Fatal(err)
	}
	read(2, true, now, "no_plan")
	// An empty designated board still opts into explicit selection.
	empty, err := s.CreateBoard(ctx, "actor-1", map[string]any{"title": "Empty initiative board", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	read(0, false, now, "no_plan")
	// An archived designation still prevents unrelated work becoming initiatives.
	if _, err = s.ArchiveBoard(ctx, "actor-1", empty["id"].(string)); err != nil {
		t.Fatal(err)
	}
	read(0, false, now, "no_plan")
	if _, err = s.UpdateBoard(ctx, "actor-1", designated["id"].(string), map[string]any{"role": 1}, nil); err == nil {
		t.Fatal("accepted non-string role")
	}
}
