package primitives_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"agent-nexus-core/internal/plans"
)

func TestHealthInputsAgreeAcrossWorkCardAndRefReads(t *testing.T) {
	ctx := context.Background()
	s, board := newWorkTestStore(t)
	now := time.Now().UTC()
	assertHealth := func(id, ref, want string) {
		t.Helper()
		work, err := s.GetWork(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		card, err := s.GetBoardCard(ctx, "", id)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.EnrichCardPlans(ctx, []map[string]any{work, card}, nil, now, time.Hour); err != nil {
			t.Fatal(err)
		}
		previews, err := s.ResolveRefs(ctx, []string{ref}, nil, now, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		health := work["plan_health"].(plans.Health)
		if state, ok := work["plan_state"].(plans.State); ok && state.HealthState != health.State {
			t.Fatalf("state=%+v health=%+v", state, health)
		}
		if health.State != want || !reflect.DeepEqual(health, card["plan_health"]) || !reflect.DeepEqual(health, *previews[0].PlanHealth) {
			t.Fatalf("work=%+v card=%+v preview=%+v want=%s", health, card["plan_health"], previews[0].PlanHealth, want)
		}
	}
	card, err := s.CreateWork(ctx, "actor-1", board, map[string]any{"title": "Due annotation"})
	if err != nil {
		t.Fatal(err)
	}
	id, ref := card["id"].(string), card["ref"].(string)
	if err = s.SetCardPlan(ctx, "actor-1", id, card["updated_at"].(string), plans.Plan{Steps: []plans.Step{{ID: "ship", Title: "Ship"}}}); err != nil {
		t.Fatal(err)
	}
	work, err := s.GetWork(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	work, err = s.PatchWork(ctx, "actor-1", id, work["version"].(int64), map[string]any{"due_at": now.Add(-time.Hour).Format(time.RFC3339Nano)})
	if err != nil {
		t.Fatal(err)
	}
	assertHealth(id, ref, "at_risk")
	if _, err = s.PatchWork(ctx, "actor-1", id, work["version"].(int64), map[string]any{"due_at": nil}); err != nil {
		t.Fatal(err)
	}
	assertHealth(id, ref, "on_track")
	old := now.Add(-72 * time.Hour).Format(time.RFC3339Nano)
	source, err := s.CreateWork(ctx, "actor-1", board, map[string]any{"title": "Source activity", "source": map[string]any{"authority": "generic-provider", "connection_id": "source", "native_id": "opaque"}})
	if err != nil {
		t.Fatal(err)
	}
	id, ref = source["id"].(string), source["ref"].(string)
	// No phase change: updated_at must not masquerade as source movement.
	_, err = s.SubmitWorkObservation(ctx, "actor-1", id, map[string]any{"idempotency_key": "old", "reader_id": "any-adapter", "reader_revision": "1", "observed_at": now.Format(time.RFC3339Nano), "status": "reported", "source_activity_at": old, "facts": map[string]any{"phase": "unknown"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.SubmitWorkObservation(ctx, "actor-1", id, map[string]any{"idempotency_key": "recent", "reader_id": "any-adapter", "reader_revision": "1", "observed_at": now.Add(time.Second).Format(time.RFC3339Nano), "status": "reported", "source_activity_at": now.Add(-time.Minute).Format(time.RFC3339Nano), "facts": map[string]any{"phase": "unknown"}})
	if err != nil {
		t.Fatal(err)
	}
	assertHealth(id, ref, "no_plan")
	activity := now.Add(-time.Minute).Format(time.RFC3339Nano)
	_, err = s.SubmitWorkObservation(ctx, "actor-1", id, map[string]any{"idempotency_key": "poll", "reader_id": "any-adapter", "reader_revision": "1", "observed_at": now.Add(2 * time.Second).Format(time.RFC3339Nano), "status": "reported", "source_activity_at": activity, "facts": map[string]any{"phase": "unknown"}})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(3 * time.Hour)
	assertHealth(id, ref, "stale")
}
