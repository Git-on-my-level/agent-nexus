package primitives

import (
	"context"
	"testing"
	"time"

	"agent-nexus-core/internal/plans"
)

// Only an explicit promise change may change a promise. A plain message by the
// promise's author (cards message without next) and a plan-step add must leave
// the counted promise alone; health stays past_promise instead of collapsing
// to no_promise.
func TestAuthorEditsKeepThePromise(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	workspace, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	store := NewTestStore(workspace.DB(), workspace.Layout().ArtifactContentDir)
	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{"title": "Initiatives", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().UTC().Add(-48 * time.Hour).Format("2006-01-02")
	card, err := store.CreateWork(ctx, "agent-1", anyStringValue(board["id"]), map[string]any{"title": "Ledger cleanup"})
	if err != nil {
		t.Fatal(err)
	}
	thread := anyStringValue(card["thread_id"])
	note := func(payload map[string]any) {
		t.Helper()
		if _, err = store.AppendEvent(ctx, "agent-1", map[string]any{
			"type": "message_posted", "thread_id": thread,
			"refs":    []string{anyStringValue(card["ref"]), "thread:" + thread},
			"payload": payload,
		}); err != nil {
			t.Fatal(err)
		}
	}
	// The author's promise, already past due.
	note(map[string]any{"text": "shipping the cleanup", "next": map[string]any{"at": past, "what": "Ship the cleanup"}})
	if drained, err := store.RefreshBriefInitiatives(ctx, 200); err != nil || !drained {
		t.Fatalf("refresh drained=%v err=%v", drained, err)
	}
	visible := func(_, owner string) bool { return owner == "" || owner == "reader" }
	now := time.Now().UTC()
	read := func() plans.Health {
		t.Helper()
		item, err := store.GetWork(ctx, anyStringValue(card["id"]))
		if err != nil {
			t.Fatal(err)
		}
		if err = store.EnrichCardPlans(WithAccessScope(ctx, AccessScope{ActorID: "reader"}), []map[string]any{item}, visible, now); err != nil {
			t.Fatal(err)
		}
		health, _ := item["plan_health"].(plans.Health)
		return health
	}
	if health := read(); health.State != plans.PromisePast || health.Promise == nil || health.Promise.Source != "note" {
		t.Fatalf("baseline health = %+v", health)
	}
	briefPromise := func() *plans.Promise {
		t.Helper()
		folds, err := store.PromiseFolds(ctx, []string{anyStringValue(card["id"])})
		if err != nil {
			t.Fatal(err)
		}
		return folds[anyStringValue(card["id"])].Promise
	}
	if fold := briefPromise(); fold == nil || fold.Source != "note" {
		t.Fatalf("baseline fold = %+v", fold)
	}

	// A plain message by the same author must not clear the promise.
	note(map[string]any{"text": "x"})
	if health := read(); health.State != plans.PromisePast || health.Promise == nil || health.Promise.Source != "note" {
		t.Fatalf("plain message erased the promise: %+v", health)
	}

	// A plan-step add must not meet the promise either.
	updated, err := store.GetWork(ctx, anyStringValue(card["id"]))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SetCardPlan(ctx, "agent-1", anyStringValue(card["id"]), anyStringValue(updated["updated_at"]), plans.Plan{Steps: []plans.Step{
		{ID: "survey", Title: "Survey the ledger"},
		{ID: "retire", Title: "Retire dead rows"},
	}}); err != nil {
		t.Fatal(err)
	}
	if drained, err := store.RefreshBriefInitiatives(ctx, 200); err != nil || !drained {
		t.Fatalf("refresh drained=%v err=%v", drained, err)
	}
	if fold := briefPromise(); fold == nil || fold.Source != "note" {
		t.Fatalf("plan-step add erased the promise: %+v", fold)
	}
	if health := read(); health.State != plans.PromisePast || health.Promise == nil {
		t.Fatalf("plan-step add erased the read-path promise: %+v", health)
	}
	// Adding to an already materialized plan used to manufacture a status
	// change solely because its ID/status signature grew.
	updated, err = store.GetWork(ctx, anyStringValue(card["id"]))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SetCardPlan(ctx, "agent-1", anyStringValue(card["id"]), anyStringValue(updated["updated_at"]), plans.Plan{Steps: []plans.Step{{ID: "survey", Title: "Survey"}, {ID: "retire", Title: "Retire"}, {ID: "check", Title: "Check"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err = store.RefreshBriefInitiatives(ctx, 200); err != nil {
		t.Fatal(err)
	}
	if p := briefPromise(); p == nil || p.What != "Ship the cleanup" {
		t.Fatalf("expanded plan replaced promise: %+v", p)
	}
	updated, err = store.GetWork(ctx, anyStringValue(card["id"]))
	if err != nil {
		t.Fatal(err)
	}
	version, _ := workInt(updated["version"])
	if _, err = store.PatchWork(ctx, "agent-1", anyStringValue(card["id"]), version, map[string]any{"next_action": "A different action"}); err != nil {
		t.Fatal(err)
	}
	// Plain chatter must not evict an older promise from the 50-fact read.
	for i := 0; i < 55; i++ {
		note(map[string]any{"text": "ordinary update"})
	}
	if _, err = store.RefreshBriefInitiatives(ctx, 200); err != nil {
		t.Fatal(err)
	}
	if p := briefPromise(); p == nil || p.What != "Ship the cleanup" {
		t.Fatalf("plain notes evicted promise: %+v", p)
	}
	if health := read(); health.State != plans.PromisePast || health.Promise == nil {
		t.Fatalf("next_action/chatter erased promise: %+v", health)
	}
	if _, err = store.CreateWork(ctx, "agent-1", "", map[string]any{"title": "New linked child", "under": card["ref"]}); err != nil {
		t.Fatal(err)
	}

	if _, err = store.RefreshBriefInitiatives(ctx, 200); err != nil {
		t.Fatal(err)
	}
	if p := briefPromise(); p == nil || p.What != "Ship the cleanup" {
		t.Fatalf("linked child creation erased the canonical promise: %+v", p)
	}
	if health := read(); health.State != plans.PromisePast || health.Promise == nil {
		t.Fatalf("linked child creation erased read health: %+v", health)
	}
}
