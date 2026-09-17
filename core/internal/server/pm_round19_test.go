package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
)

func TestRound19NativeContentAndMetadataFence(t *testing.T) {
	for _, scope := range []string{"work.phase", "work.annotate"} {
		for _, change := range []string{"content", "metadata", "none"} {
			for _, afterApproval := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/afterApproval=%v", scope, change, afterApproval), func(t *testing.T) {
					ctx := context.Background()
					env := newPMStoreTestEnv(t)
					human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "r19-human", "r19-actor", "r19-human", "r19-token")
					store := env.primitiveStore.(*primitives.Store)
					board, err := store.CreateBoard(ctx, human.ActorID, map[string]any{"title": "Round 19"})
					if err != nil {
						t.Fatal(err)
					}
					work, err := store.CreateWork(ctx, human.ActorID, asString(board["id"]), map[string]any{"title": "Acceptance v1", "source": map[string]any{"authority": "nexus"}})
					if err != nil {
						t.Fatal(err)
					}
					if work["decision_revision"] != "1.1" {
						t.Fatalf("composition: %+v", work)
					}
					rt, err := NewPMRuntime(env.workspace.DB(), store, env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}})
					if err != nil {
						t.Fatal(err)
					}
					call := func(path string, in any, status int) map[string]any {
						t.Helper()
						raw, err := json.Marshal(in)
						if err != nil {
							t.Fatal(err)
						}
						req := httptest.NewRequest("POST", path, bytes.NewReader(raw))
						req.Header.Set("Authorization", "Bearer "+human.AccessToken)
						rr := httptest.NewRecorder()
						rt.ServeHTTP(rr, req)
						if rr.Code != status {
							t.Fatalf("%s: %d %s", path, rr.Code, rr.Body)
						}
						var out map[string]any
						if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
							t.Fatal(err)
						}
						return out
					}
					in := pm.DecisionInput{RequestKey: "proposal", WorkRef: asString(work["ref"]), Scope: scope, Instruction: `{"next_action":"review"}`, TargetRevision: asString(work["decision_revision"])}
					if scope == "work.phase" {
						in.Instruction = "Move to ready"
						in.Payload = &pm.ActionPayload{Phase: "ready"}
					}
					d := call("/pm/decisions", in, 201)
					id := d["id"].(string)
					answer := pm.AnswerInput{Revision: 1, Approve: true, Text: "Approved"}
					if afterApproval {
						call("/pm/decisions/"+id+"/answer", answer, 200)
					}
					wantRevision := "1.1"
					switch change {
					case "content":
						title, summary := "Acceptance v2", "Changed acceptance criteria"
						if _, _, err := store.CreateCardRevision(ctx, human.ActorID, asString(work["id"]), primitives.CreateCardRevisionInput{Title: &title, Summary: &summary}); err != nil {
							t.Fatal(err)
						}
						wantRevision = "1.2"
					case "metadata":
						if _, err := store.PatchWork(ctx, human.ActorID, asString(work["ref"]), 1, map[string]any{"priority": "p1"}); err != nil {
							t.Fatal(err)
						}
						wantRevision = "2.1"
					}
					current, err := store.GetWork(ctx, asString(work["ref"]))
					if err != nil || current["decision_revision"] != wantRevision {
						t.Fatalf("revision: %+v %v", current, err)
					}
					if change != "none" {
						path, body := "/pm/decisions/"+id+"/answer", any(answer)
						if afterApproval {
							path, body = "/pm/decisions/"+id+"/dispatch", map[string]any{}
						}
						out := call(path, body, 409)["error"].(map[string]any)
						details := out["details"].(map[string]any)
						if out["code"] != "source_revision_changed" || details["approved_revision"] != "1.1" || details["current_revision"] != wantRevision {
							t.Fatalf("fence detail: %+v", out)
						}
						if !afterApproval && details["reason"] != "revision_changed" {
							t.Fatal(out)
						}
						return
					}
					if !afterApproval {
						call("/pm/decisions/"+id+"/answer", answer, 200)
					}
					action := call("/pm/decisions/"+id+"/dispatch", map[string]any{}, 200)
					if action["status"] != "verified" {
						t.Fatalf("native dispatch: %+v", action)
					}
					updated, err := store.GetWork(ctx, asString(work["ref"]))
					if err != nil {
						t.Fatal(err)
					}
					if updated["decision_revision"] != "2.1" || (scope == "work.phase" && updated["phase"] != "ready") || (scope == "work.annotate" && updated["next_action"] != "review") {
						t.Fatalf("mutation: %+v", updated)
					}
				})
			}
		}
	}
}
