package primitives_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/primitives"
)

func TestPlanEditsRefStateAndHistory(t *testing.T) {
	ctx := context.Background()
	store, board := newWorkTestStore(t)
	parent, err := store.CreateWork(ctx, "actor-1", "", map[string]any{"title": "Initiative", "board_id": board})
	if err != nil {
		t.Fatal(err)
	}
	child, err := store.CreateWork(ctx, "actor-1", "", map[string]any{"title": "Implementation", "board_id": board})
	if err != nil {
		t.Fatal(err)
	}
	id := parent["id"].(string)
	token := parent["updated_at"].(string)
	p := plans.Plan{Steps: []plans.Step{{ID: "build", Title: "Build", Ref: child["ref"].(string), Status: "done", After: []string{}}, {ID: "ship", Title: "Ship", After: []string{"build"}}}}
	if err = store.SetCardPlan(ctx, "actor-1", id, token, p); err != nil {
		t.Fatal(err)
	}
	if err = store.SetCardPlan(ctx, "actor-1", id, token, plans.Plan{Steps: []plans.Step{}}); !errors.Is(err, primitives.ErrConflict) {
		t.Fatalf("stale edit=%v", err)
	}
	card, err := store.GetBoardCard(ctx, "", id)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.EnrichCardPlans(ctx, []map[string]any{card}, nil, time.Now(), 0); err != nil {
		t.Fatal(err)
	}
	state := card["plan_state"].(plans.State)
	if state.Progress.Done != 0 || state.Steps[0].Status != "not_started" {
		t.Fatalf("ref did not override prose: %+v", state)
	}
	if err = store.SetCardPlan(ctx, "actor-1", id, card["updated_at"].(string), p); err != nil {
		t.Fatal(err)
	}
	unchanged, err := store.GetBoardCard(ctx, "", id)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged["updated_at"] != card["updated_at"] {
		t.Fatal("no-op invented movement")
	}
	p.Steps[1].Status = "blocked"
	if err = store.SetCardPlan(ctx, "actor-1", id, card["updated_at"].(string), p); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListEvents(ctx, primitives.EventListFilter{Types: []string{"card_updated"}})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	snapshots := 0
	for _, e := range events {
		payload, _ := e["payload"].(map[string]any)
		if payload["card_id"] == id && payload["plan"] != nil {
			count++
			if payload["before_plan"] != nil {
				snapshots++
			}
		}
	}
	if count != 2 || snapshots != 1 {
		t.Fatalf("plan events=%d", count)
	}
	preview, err := store.ResolveRefs(ctx, []string{child["ref"].(string)}, func(string, string) bool { return false }, time.Now(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if preview[0].Resolvable || preview[0].Title != "" {
		t.Fatal("hidden ref leaked:", preview)
	}
}

func TestBatchRefsSharedCorpus(t *testing.T) {
	ctx := context.Background()
	store, board := newWorkTestStore(t)
	initiative, err := store.CreateWork(ctx, "actor-1", "", map[string]any{"handle": "initiative", "title": "Initiative", "board_id": board})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.CreateTopic(ctx, "actor-1", map[string]any{"title": "Launch", "summary": "Launch initiative"})
	if err != nil {
		t.Fatal(err)
	}
	createBoardTestDocument(t, ctx, store, createBoardTestThread(t, ctx, store, "Brief discussion"), "Brief")
	// Existing test helpers derive handles from titles: Brief -> brief, Portfolio -> portfolio.
	raw, err := os.ReadFile("../../../contracts/fixtures/initiative-plans/refs.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Refs       []string
		Resolvable []bool
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if err = store.SetCardPlan(ctx, "actor-1", initiative["id"].(string), initiative["updated_at"].(string), plans.Plan{Steps: []plans.Step{{ID: "brief", Title: "Brief", Ref: "doc:brief", Status: "done"}}}); err != nil {
		t.Fatal(err)
	}
	items, err := store.ResolveRefs(ctx, fixture.Refs, nil, time.Now(), 0)
	if err != nil {
		t.Fatal(err)
	}
	for i, item := range items {
		if item.Ref != fixture.Refs[i] || item.Resolvable != fixture.Resolvable[i] {
			t.Fatalf("item %d: %+v", i, item)
		}
		if (item.Kind == "topic" || item.Kind == "board") && item.URL != "" {
			t.Fatalf("unsupported detail route: %+v", item)
		}
		if item.Kind == "card" && item.URL != "/tasks/initiative" || item.Kind == "document" && item.URL != "/docs/brief" {
			t.Fatalf("incorrect detail route: %+v", item)
		}
	}
	if items[0].Progress == nil || items[0].Progress.Done != 1 {
		t.Fatal("context ref fallback status ignored:", items[0])
	}
	if _, err = store.ResolveRefs(ctx, make([]string, 201), nil, time.Now(), 0); err == nil {
		t.Fatal("accepted >200 refs")
	}
}

func TestExternalPlanRefAndPollFreshness(t *testing.T) {
	ctx := context.Background()
	store, board := newWorkTestStore(t)
	externalURL := "https://github.com/org/repo/pull/42"
	work, err := store.CreateWork(ctx, "actor-1", "", map[string]any{"title": "PR", "board_id": board, "phase": "in_progress", "source": map[string]any{"authority": "github", "connection_id": "github", "native_id": "pr42", "url": externalURL}})
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-6 * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
	_, err = store.SubmitWorkObservation(ctx, "actor-1", work["id"].(string), map[string]any{"idempotency_key": "first", "reader_id": "github", "reader_revision": "1", "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "source_sequence": 1, "status": "reported", "source_activity_at": old, "facts": map[string]any{"phase": "done"}, "evidence": []any{map[string]any{"url": externalURL}}})
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.ResolveRefs(ctx, []string{externalURL, "https://github.com/org/repo/pull/999"}, nil, time.Now(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !items[0].Resolvable || items[0].Phase != "done" || items[1].Resolvable {
		t.Fatal(items)
	}
	// A known URL projects source state; an unknown URL keeps the agent fallback.
	p := plans.Plan{Steps: []plans.Step{{ID: "known", Title: "Known", Ref: externalURL, Status: "blocked"}, {ID: "unknown", Title: "Unknown", Ref: items[1].Ref, Status: "active"}}}
	facts := map[string]plans.Fact{externalURL: {Known: true, Status: items[0].Phase, MovementAt: items[0].MovementAt}}
	state := plans.Compute(p, facts, time.Now().Add(-6*24*time.Hour), time.Now(), 0)
	if state.Progress.Done != 1 || state.Steps[1].Status != "active" || state.Health != "stalled" {
		t.Fatal(state)
	}
}
