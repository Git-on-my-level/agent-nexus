package primitives

import (
	"context"
	"errors"
	"testing"
	"time"

	"agent-nexus-core/internal/plans"
)

func TestPublishedEvidenceKeysInheritOwnershipBeforeSelection(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"opaque-release-key", "https://source.test/confidential/42", "github:team/repo#42"} {
		t.Run(key, func(t *testing.T) {
			ctx := context.Background()
			ws, err := initializeTestWorkspace(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
			board, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Private board"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.PatchThread(ctx, "owner", anyStringValue(board["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
				t.Fatal(err)
			}
			private, err := s.CreateWork(ctx, "owner", anyStringValue(board["id"]), map[string]any{"title": "Confidential evidence", "source_refs": []any{map[string]any{"authority": "tracker", "connection_id": "private", "native_id": "42", "aliases": []string{key}, "status": "in_progress"}}})
			if err != nil {
				t.Fatal(err)
			}
			public, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Public board"})
			if err != nil {
				t.Fatal(err)
			}
			parent, err := s.CreateWork(ctx, "owner", anyStringValue(public["id"]), map[string]any{"title": "Public plan"})
			if err != nil {
				t.Fatal(err)
			}
			id := anyStringValue(parent["id"])
			p := plans.Plan{Steps: []plans.Step{{ID: "secret", Title: "Confidential stored step", Ref: key}}}
			if err = s.SetCardPlan(ctx, "owner", id, anyStringValue(parent["updated_at"]), p); err != nil {
				t.Fatal(err)
			}
			parent, err = s.GetBoardCard(ctx, "", id)
			if err != nil {
				t.Fatal(err)
			}
			for _, actor := range []string{"stranger", "unauthorized-agent", "owner", "selected-pm"} {
				scoped := WithAccessScope(ctx, AccessScope{ActorID: actor, PMActorID: "selected-pm"})
				allowed := actor == "owner" || actor == "selected-pm"
				loaded, _, _, err := s.loadPlans(scoped, []string{id})
				if err != nil || (len(loaded) > 0) != allowed {
					t.Fatalf("%s plan: %+v %v", actor, loaded, err)
				}
				var count int
				if err = s.db.QueryRowContext(scoped, "SELECT count(*) FROM card_plans WHERE card_id=? LIMIT 1", id).Scan(&count); err != nil || (count == 1) != allowed {
					t.Fatalf("%s aggregate: %d %v", actor, count, err)
				}
				if !s.CanAccessResource(scoped, "card", id) {
					t.Fatal("plan ownership incorrectly hides its public parent")
				}
				preview, err := s.ResolveRefs(scoped, []string{anyStringValue(parent["ref"]), key}, nil, time.Now(), 0)
				if err != nil || (preview[0].NextStep != nil) != allowed || preview[1].Resolvable != allowed {
					t.Fatalf("%s preview: %+v %v", actor, preview, err)
				}
				if !allowed && !errors.Is(s.SetCardPlan(scoped, actor, id, anyStringValue(parent["updated_at"]), p), ErrNotFound) {
					t.Fatal("unauthorized evidence key accepted in a plan mutation")
				}
			}
			// Publishing after the plan is stored and withdrawing a key must take
			// effect in the same statement snapshot; no derived-plan repair is needed.
			if _, err = s.PatchWork(ctx, "owner", anyStringValue(private["id"]), 1, map[string]any{"source_refs": []any{}}); err != nil {
				t.Fatal(err)
			}
			scoped := WithAccessScope(ctx, AccessScope{ActorID: "stranger"})
			loaded, _, _, err := s.loadPlans(scoped, []string{id})
			if err != nil || len(loaded) != 1 {
				t.Fatalf("withdrawn key still constrains plan: %+v %v", loaded, err)
			}
			// A late publication constrains the already stored plan atomically.
			if _, err = s.PatchWork(ctx, "owner", anyStringValue(private["id"]), 2, map[string]any{"source_refs": []any{map[string]any{"authority": "tracker", "connection_id": "private", "native_id": "42", "aliases": []string{key}}}}); err != nil {
				t.Fatal(err)
			}
			loaded, _, _, err = s.loadPlans(scoped, []string{id})
			if err != nil || len(loaded) != 0 {
				t.Fatalf("late publication did not constrain plan: %+v %v", loaded, err)
			}
			// Purging the publisher must retain key ownership and dependent plans.
			if _, err = s.ArchiveBoardCard(ctx, "owner", anyStringValue(board["id"]), anyStringValue(private["id"]), RemoveBoardCardInput{}); err != nil {
				t.Fatal(err)
			}
			if err = s.PurgeArchivedBoardCard(ctx, anyStringValue(board["id"]), anyStringValue(private["id"])); err != nil {
				t.Fatal(err)
			}
			loaded, _, _, err = s.loadPlans(scoped, []string{id})
			if err != nil || len(loaded) != 0 {
				t.Fatalf("purge exposed stored private plan: %+v %v", loaded, err)
			}
			if err = s.CheckResourceValues(scoped, map[string]any{"ref": key}); !errors.Is(err, ErrNotFound) {
				t.Fatalf("purge discarded key ownership: %v", err)
			}
		})
	}
}
