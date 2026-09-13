package server

import (
	"context"
	"fmt"
	"testing"
	"time"

	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
)

func TestRound10RefreshBookkeepingPreservesDecisionFence(t *testing.T) {
	for _, authority := range []string{"nexus", "github"} {
		t.Run(authority, func(t *testing.T) {
			env := newPMStoreTestEnv(t)
			ctx := context.Background()
			store := env.primitiveStore.(*primitives.Store)
			board, err := store.CreateBoard(ctx, "actor", map[string]any{"title": "Round10"})
			if err != nil {
				t.Fatal(err)
			}
			work, err := store.CreateWork(ctx, "actor", asString(board["id"]), map[string]any{"title": "Refresh fence", "source": map[string]any{"authority": authority, "connection_id": "fixture", "native_id": "issue-10"}})
			if err != nil {
				t.Fatal(err)
			}
			ref := asString(work["ref"])
			st, err := pm.NewStore(env.workspace.DB())
			if err != nil {
				t.Fatal(err)
			}
			executions := 0
			svc, err := pm.NewService(st, pm.Config{WorkspaceID: "ws_main"}, pm.Dependencies{
				Authorize: func(context.Context, pm.Principal, string, string) error { return nil },
				DecisionWork: func(ctx context.Context, _ pm.Principal, ref string) (pm.DecisionWork, error) {
					w, err := store.GetWork(ctx, ref)
					if err != nil {
						return pm.DecisionWork{}, err
					}
					return pm.DecisionWork{Revision: primitives.WorkDecisionRevision(w), Phase: asString(w["phase"])}, nil
				},
				CurrentRevision: func(ctx context.Context, _ pm.Principal, ref string) (string, error) {
					return currentWorkDecisionRevision(ctx, store, ref)
				},
				CheckDelivery: func(context.Context, pm.Action) error { return nil },
				Execute: func(context.Context, pm.Action) (pm.Receipt, error) {
					executions++
					return pm.Receipt{Status: pm.Delivered, ExternalID: "source-action-10"}, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			p := pm.Principal{WorkspaceID: "ws_main", ActorID: "actor", Human: true}
			d, err := svc.ProposeDecision(ctx, p, pm.DecisionInput{RequestKey: "before-outage", WorkRef: ref, Instruction: "Assign", Scope: "assignment", TargetRevision: asString(work["decision_revision"])})
			if err != nil {
				t.Fatal(err)
			}
			d, err = svc.AnswerDecision(ctx, p, d.ID, pm.AnswerInput{Revision: d.Revision, Approve: true, Text: "yes"})
			if err != nil {
				t.Fatal(err)
			}
			assertFence := func(want string) {
				t.Helper()
				got, err := store.GetWork(ctx, ref)
				if err != nil {
					t.Fatal(err)
				}
				revision, err := currentWorkDecisionRevision(ctx, store, ref)
				if err != nil || revision != want || got["decision_revision"] != want || publicWork(got)["decision_revision"] != want {
					t.Fatalf("get/dispatch fence: %+v revision=%q want=%q err=%v", got, revision, want, err)
				}
				page, err := store.ListWork(ctx, primitives.WorkListFilter{})
				items := page.Work
				if err != nil || len(items) != 1 || items[0]["decision_revision"] != want || publicWork(items[0])["decision_revision"] != want {
					t.Fatalf("list fence: %+v %v", items, err)
				}
			}
			observe := func(i int, status, revision string) {
				t.Helper()
				observation := map[string]any{"idempotency_key": fmt.Sprint(i), "reader_id": "fixture", "reader_revision": "v1", "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "status": status, "facts": map[string]any{}}
				if status == "error" {
					observation["error"] = "source unreachable"
				} else {
					observation["source_revision"] = revision
				}
				if _, err := store.SubmitWorkObservation(ctx, "reader", ref, observation); err != nil {
					t.Fatal(err)
				}
			}
			for i := 0; i < 20; i++ {
				if _, err := store.RequestWorkRefresh(ctx, "actor", ref); err != nil {
					t.Fatal(err)
				}
				observe(i, "error", "")
				assertFence("1")
			}
			got, err := store.GetWork(ctx, ref)
			if err != nil || got["version"] != int64(1) {
				t.Fatalf("bookkeeping version: %+v %v", got, err)
			}
			refresh := got["refresh"].(map[string]any)
			if refresh["last_error"] != "source unreachable" || refresh["last_attempt_at"] == nil {
				t.Fatalf("failure health lost: %+v", refresh)
			}
			action, err := svc.DispatchDecision(ctx, p, d.ID)
			if err != nil || action.Status != pm.Delivered || executions != 1 {
				t.Fatalf("approval invalidated by refresh: %+v %v executions=%d", action, err, executions)
			}
			if authority == "nexus" {
				version := int64(1)
				if _, err := store.MoveBoardCard(ctx, "actor", "", asString(work["id"]), primitives.MoveBoardCardInput{ColumnKey: "ready", IfWorkVersion: &version}); err != nil {
					t.Fatal(err)
				}
				assertFence("2")
				observe(20, "reported", "external-ignored")
				assertFence("2")
			} else {
				observe(20, "reported", "source-r1")
				assertFence("source-r1")
				observe(21, "reported", "source-r2")
				assertFence("source-r2")
				observe(22, "error", "")
				assertFence("source-r2")
			}
		})
	}
}
