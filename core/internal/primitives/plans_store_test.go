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
	t.Parallel()
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
	preview, err := store.ResolveRefs(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "selected-pm", PMActorID: "selected-pm"}), []string{child["ref"].(string)}, func(string, string) bool { return false }, time.Now(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if preview[0].Resolvable || preview[0].Title != "" {
		t.Fatal("hidden ref leaked:", preview)
	}
}

func TestBatchRefsSharedCorpus(t *testing.T) {
	t.Parallel()
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

func TestRefPreviewKeepsPrivatePlanOutOfPublicCardPreview(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, board := newWorkTestStore(t)
	initiative, err := store.CreateWork(ctx, "actor-1", board, map[string]any{"title": "Initiative"})
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := store.CreateWork(ctx, "actor-1", board, map[string]any{"title": "Hidden work"})
	if err != nil {
		t.Fatal(err)
	}
	readable, err := store.CreateWork(ctx, "actor-1", board, map[string]any{"title": "Readable work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PatchThread(ctx, "actor-1", hidden["thread_id"].(string), map[string]any{"pm_actor_id": "private-owner"}, nil); err != nil {
		t.Fatal(err)
	}
	// Main's central field inventory makes a plan that references private work
	// private as a whole, while leaving its public containing card readable.
	p := plans.Plan{Steps: []plans.Step{
		{ID: "c-readable", Title: "Later readable", After: []string{}},
		{ID: "b-readable", Title: "First readable", Ref: readable["ref"].(string), After: []string{}},
		{ID: "a-hidden", Title: "Secret step title", Ref: hidden["ref"].(string), After: []string{}},
	}}
	if err = store.SetCardPlan(ctx, "actor-1", initiative["id"].(string), initiative["updated_at"].(string), p); err != nil {
		t.Fatal(err)
	}
	items, err := store.ResolveRefs(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "reader"}), []string{initiative["ref"].(string)}, func(_ string, owner string) bool { return owner != "private-owner" }, time.Now(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !items[0].Resolvable || items[0].Progress != nil || items[0].NextStep != nil {
		t.Fatalf("public preview leaked private plan: %+v", items[0])
	}
	items, err = store.ResolveRefs(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "private-owner"}), []string{initiative["ref"].(string)}, nil, time.Now(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if items[0].NextStep == nil || items[0].NextStep.Title != "Secret step title" {
		t.Fatalf("owner preview lost private plan: %+v", items[0])
	}
}

func TestExternalPlanRefAndPollFreshness(t *testing.T) {
	t.Parallel()
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
	if !items[0].Resolvable || items[0].Phase != "done" || items[1].Resolvable || items[1].Source != "" || items[1].Status != "" {
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

func TestCardAndStepMessagesResetPlanRecency(t *testing.T) {
	t.Parallel()
	for _, subject := range []string{"card", "step"} {
		t.Run(subject, func(t *testing.T) {
			ctx := context.Background()
			ws, err := initializeTestWorkspace(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
			board, err := s.CreateBoard(ctx, "actor-1", map[string]any{"title": "Initiatives"})
			if err != nil {
				t.Fatal(err)
			}
			parent, err := s.CreateWork(ctx, "actor-1", board["id"].(string), map[string]any{"title": "Initiative"})
			if err != nil {
				t.Fatal(err)
			}
			child, err := s.CreateWork(ctx, "actor-1", board["id"].(string), map[string]any{"title": "Step"})
			if err != nil {
				t.Fatal(err)
			}
			p := plans.Plan{Steps: []plans.Step{{ID: "build", Title: "Build", Ref: child["ref"].(string)}}}
			if err = s.SetCardPlan(ctx, "actor-1", parent["id"].(string), parent["updated_at"].(string), p); err != nil {
				t.Fatal(err)
			}
			old := time.Now().UTC().Add(-96 * time.Hour).Format(time.RFC3339Nano)
			if _, err = ws.DB().Exec(`UPDATE cards SET updated_at=?`, old); err != nil {
				t.Fatal(err)
			}
			if _, err = ws.DB().Exec(`UPDATE card_plans SET updated_at=?`, old); err != nil {
				t.Fatal(err)
			}
			if _, err = ws.DB().Exec(`UPDATE work_metadata SET updated_at=?`, old); err != nil {
				t.Fatal(err)
			}
			card, err := s.GetBoardCard(ctx, "", parent["id"].(string))
			if err != nil {
				t.Fatal(err)
			}
			if err = s.EnrichCardPlans(ctx, []map[string]any{card}, nil, time.Now(), 0); err != nil {
				t.Fatal(err)
			}
			if card["plan_health"].(plans.Health).State != "stale" {
				t.Fatal(card["plan_health"])
			}
			thread := parent["thread_id"].(string)
			if subject == "step" {
				thread = child["thread_id"].(string)
			}
			if _, err = s.AppendEvent(ctx, "actor-1", map[string]any{"type": "message_posted", "thread_id": thread, "refs": []string{}, "payload": map[string]any{"text": "Verified progress"}}); err != nil {
				t.Fatal(err)
			}
			if err = s.EnrichCardPlans(ctx, []map[string]any{card}, nil, time.Now(), 0); err != nil {
				t.Fatal(err)
			}
			if card["plan_health"].(plans.Health).State != "on_track" {
				t.Fatal(card["plan_health"])
			}
		})
	}
}

// The three lists an initiative is read by, over a real card plan: a step whose
// linked card is done carries the completion anchor the Overview dates it from,
// a step in flight is current, and only ready unstarted work is next.
func TestPlanStepDigestOverRealCards(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, board := newWorkTestStore(t)
	card := func(title string) map[string]any {
		w, err := store.CreateWork(ctx, "actor-1", "", map[string]any{"title": title, "board_id": board})
		if err != nil {
			t.Fatal(err)
		}
		return w
	}
	initiative, finished, plain := card("Initiative"), card("Design the flow"), card("Unplanned work")
	evidence, err := store.AppendEvent(ctx, "actor-1", map[string]any{"type": "completion_evidence", "refs": []string{}, "summary": "Design reviewed"})
	if err != nil {
		t.Fatal(err)
	}
	refs := []string{"event:" + evidence["id"].(string)}
	resolution := "done"
	if _, err = store.MoveBoardCard(ctx, "actor-1", board, finished["id"].(string), primitives.MoveBoardCardInput{ColumnKey: "done", Resolution: &resolution, ResolutionRefs: &refs}); err != nil {
		t.Fatal(err)
	}
	p := plans.Plan{Steps: []plans.Step{
		{ID: "design", Title: "Design the flow", Ref: finished["ref"].(string), After: []string{}},
		{ID: "build", Title: "Build the flow", Status: "active", After: []string{"design"}},
		{ID: "ship", Title: "Ship it", After: []string{"build"}},
		{ID: "measure", Title: "Measure it", After: []string{"design"}},
	}}
	if err = store.SetCardPlan(ctx, "actor-1", initiative["id"].(string), initiative["updated_at"].(string), p); err != nil {
		t.Fatal(err)
	}
	row, err := store.GetBoardCard(ctx, "", initiative["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.EnrichCardPlans(ctx, []map[string]any{row}, nil, time.Now().UTC(), 0); err != nil {
		t.Fatal(err)
	}
	digest, ok := row["plan_step_digest"].(plans.StepDigest)
	if !ok {
		t.Fatalf("digest=%#v", row["plan_step_digest"])
	}
	if len(digest.Completed.Items) != 1 || digest.Completed.Items[0].ID != "design" || digest.Completed.Items[0].Ref != finished["ref"].(string) {
		t.Fatalf("completed=%+v", digest.Completed)
	}
	if _, err = time.Parse(time.RFC3339Nano, digest.Completed.Items[0].At); err != nil {
		t.Fatalf("completion anchor %q: %v", digest.Completed.Items[0].At, err)
	}
	if len(digest.Current.Items) != 1 || digest.Current.Items[0].ID != "build" || digest.Current.Items[0].At != "" {
		t.Fatalf("current=%+v", digest.Current)
	}
	// Ship waits on a step in flight; Measure's only dependency is done.
	if len(digest.Next.Items) != 1 || digest.Next.Items[0].ID != "measure" {
		t.Fatalf("next=%+v", digest.Next)
	}
	// A card with no plan reports no digest rather than three empty lists.
	planless, err := store.GetBoardCard(ctx, "", plain["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.EnrichCardPlans(ctx, []map[string]any{planless}, nil, time.Now().UTC(), 0); err != nil {
		t.Fatal(err)
	}
	if planless["plan_step_digest"] != nil {
		t.Fatalf("planless digest=%#v", planless["plan_step_digest"])
	}
}
