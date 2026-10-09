package primitives_test

import (
	"context"
	"testing"
	"time"

	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/primitives"
)

func TestExternalEvidenceResolutionContract(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, board := newWorkTestStore(t)
	at := "2026-10-05T10:00:00Z"
	evidence := []map[string]any{
		{"authority": "github", "connection_id": "gh", "native_id": "org/repo#264", "aliases": []string{"https://github.com/org/repo/issues/264"}, "title": "Fix the gate", "url": "https://github.com/org/repo/pull/264", "status": "merged", "observed_at": at, "source_activity_at": at},
		{"authority": "tracker", "connection_id": "tracker-connection", "native_id": "issue-uuid", "identifier_aliases": []string{"TASK-627"}, "title": "Core", "url": "https://tracker.test/workspace/issues/issue-uuid", "status": "in_progress", "observed_at": at},
	}
	card, err := s.CreateWork(ctx, "actor-1", board, map[string]any{"title": "Release B", "source_refs": evidence})
	if err != nil {
		t.Fatal(err)
	}
	refs := []string{"github:org/repo#264", "org/repo#264", "https://github.com/org/repo/pull/264", "https://github.com/org/repo/issues/264", "TASK-627", "github:org/repo#999", "github:broken", "card:missing", "org/repo#264"}
	items, err := s.ResolveRefs(ctx, refs, nil, time.Now(), 0)
	if err != nil {
		t.Fatal(err)
	}
	for i, item := range items {
		if item.Ref != refs[i] {
			t.Fatal("order changed", items)
		}
		switch {
		case i < 4 || i == 8:
			if !item.Resolvable || item.Kind != "external" || item.Authority != "github" || item.NativeID != "org/repo#264" || item.Status != "merged" || item.Source != "evidence" || item.Title != "Fix the gate" || item.ObservedAt != at {
				t.Fatalf("%+v", item)
			}
		case i == 4:
			if !item.Resolvable || item.NativeID != "issue-uuid" || item.Status != "in_progress" || item.Authority != "tracker" {
				t.Fatal(item)
			}
		default:
			if item.Resolvable {
				t.Fatal(item)
			}
		}
	}
	p := plans.Plan{Steps: []plans.Step{{ID: "build", Title: "Build", Ref: refs[0]}, {ID: "ship", Title: "Ship", After: []string{"build"}, Ref: refs[5]}}}
	if err = s.SetCardPlan(ctx, "actor-1", card["id"].(string), card["updated_at"].(string), p); err != nil {
		t.Fatal(err)
	}
	card, err = s.GetBoardCard(ctx, "", card["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.EnrichCardPlans(ctx, []map[string]any{card}, nil, time.Now(), 0); err != nil {
		t.Fatal(err)
	}
	if card["plan_state"].(plans.State).Progress.Done != 1 || card["status_mismatch"] != true || card["column_key"] != "backlog" || card["next_step"] != (*plans.NextStep)(nil) {
		t.Fatal(card)
	}
	// Private initiative evidence must not leak source title or status.
	if _, err = s.PatchThread(ctx, "actor-1", card["thread_id"].(string), map[string]any{"pm_actor_id": "private"}, nil); err != nil {
		t.Fatal(err)
	}
	items, err = s.ResolveRefs(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "reader"}), refs, func(_ string, owner string) bool { return owner == "" }, time.Now(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if items[0].Resolvable || items[0].Title != "" || items[0].Status != "" || items[4].Resolvable || items[4].URL != "" || items[4].Title != "" || items[4].NativeID != "" {
		t.Fatal(items)
	}
}

func TestExternalObservationAliasesAndConnectionAmbiguity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, board := newWorkTestStore(t)
	for _, connection := range []string{"one", "two"} {
		card, err := s.CreateWork(ctx, "actor-1", board, map[string]any{"title": "Source", "source": map[string]any{"authority": "github", "connection_id": connection, "native_id": "org/repo/issues/9", "url": "https://github.com/org/repo/issues/9"}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.SubmitWorkObservation(ctx, "actor-1", card["id"].(string), map[string]any{"idempotency_key": "read", "reader_id": "github", "reader_revision": "v1", "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "status": "reported", "facts": map[string]any{"title": "Observed", "phase": "blocked", "native_status": "pending", "aliases": []string{"github:org/repo#9"}}, "evidence": []any{map[string]any{"url": "https://github.com/org/repo/issues/9"}}})
		if err != nil {
			t.Fatal(err)
		}
		items, err := s.ResolveRefs(ctx, []string{"github:org/repo#9"}, nil, time.Now(), 0)
		if err != nil {
			t.Fatal(err)
		}
		if connection == "one" && (items[0].Source != "evidence" || items[0].Status != "pending" || items[0].Phase != "blocked" || items[0].ConnectionID != "one") {
			t.Fatal(items)
		}
		if connection == "two" && items[0].Resolvable {
			t.Fatal("must not collapse distinct connections", items)
		}
	}
}
