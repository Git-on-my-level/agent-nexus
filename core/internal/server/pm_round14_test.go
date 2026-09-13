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

func TestRound14DecisionWorkLifecycle(t *testing.T) {
	for _, lifecycle := range []string{"trashed", "archived", "purged"} {
		for _, authority := range []string{"nexus", "github"} {
			t.Run(lifecycle+"/"+authority, func(t *testing.T) {
				env := newPMStoreTestEnv(t)
				ctx := context.Background()
				db := env.workspace.DB()
				human := seedHumanPrincipalForLockoutTest(t, ctx, db, "r14-human", "r14-actor", "r14-human", "r14-token")
				other := seedHumanPrincipalForLockoutTest(t, ctx, db, "r14-other", "r14-other-actor", "r14-other", "r14-other-token")
				store := env.primitiveStore.(*primitives.Store)
				board, err := store.CreateBoard(ctx, human.ActorID, map[string]any{"title": "Round14"})
				if err != nil {
					t.Fatal(err)
				}
				work, err := store.CreateWork(ctx, human.ActorID, asString(board["id"]), map[string]any{"title": "Target", "source": map[string]any{"authority": authority, "connection_id": "fixture", "native_id": "r14"}})
				if err != nil {
					t.Fatal(err)
				}
				rt, err := NewPMRuntime(db, store, env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}})
				if err != nil {
					t.Fatal(err)
				}
				token := human.AccessToken
				call := func(method, path string, in any, status int) map[string]any {
					t.Helper()
					raw, err := json.Marshal(in)
					if err != nil {
						t.Fatal(err)
					}
					req := httptest.NewRequest(method, path, bytes.NewReader(raw))
					req.Header.Set("Authorization", "Bearer "+token)
					rr := httptest.NewRecorder()
					rt.ServeHTTP(rr, req)
					if rr.Code != status {
						t.Fatalf("%s %s: %d %s", method, path, rr.Code, rr.Body)
					}
					var out map[string]any
					if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
						t.Fatal(err)
					}
					return out
				}
				in := pm.DecisionInput{RequestKey: "approved", WorkRef: asString(work["ref"]), Scope: "work.phase", Instruction: "Block", TargetRevision: asString(work["decision_revision"]), Payload: &pm.ActionPayload{Phase: "blocked"}}
				approved := call("POST", "/pm/decisions", in, 201)
				approvedID := approved["id"].(string)
				approved = call("POST", "/pm/decisions/"+approvedID+"/answer", pm.AnswerInput{Revision: 1, Approve: true, Text: "yes"}, 200)
				actionID := approved["action_id"].(string)
				in.RequestKey = "awaiting"
				in.Instruction = "Ready"
				in.Payload = &pm.ActionPayload{Phase: "ready"}
				awaiting := call("POST", "/pm/decisions", in, 201)
				awaitingID := awaiting["id"].(string)
				if _, ok := awaiting["payload"].(map[string]any)["resolution"]; ok {
					t.Fatal("empty resolution emitted")
				}
				switch lifecycle {
				case "trashed":
					_, err = store.TrashBoardCard(ctx, human.ActorID, "", asString(work["id"]), "removed", primitives.RemoveBoardCardInput{})
				case "archived":
					_, err = store.ArchiveBoardCard(ctx, human.ActorID, "", asString(work["id"]), primitives.RemoveBoardCardInput{})
				case "purged":
					_, err = db.ExecContext(ctx, "DELETE FROM cards WHERE id=?", work["id"])
				}
				if err != nil {
					t.Fatal(err)
				}
				wantPath := "none"
				if authority == "nexus" {
					wantPath = "nexus"
				}
				for _, id := range []string{approvedID, awaitingID} {
					got := call("GET", "/pm/decisions/"+id, nil, 200)
					if got["work_missing"] != true || got["target_current"] != false || got["already_at_target"] != false || got["can_answer"] != false || got["deliverable"] != (authority == "nexus") || got["delivery_path"] != wantPath {
						t.Fatal(got)
					}
				}
				checkMissing := func(out map[string]any, proposal ...bool) {
					t.Helper()
					e := out["error"].(map[string]any)
					detail := e["details"].(map[string]any)
					expected := fmt.Sprintf("Approved source revision has changed (approved at %s, source now unavailable). Approval refused (work_missing); inspect the work and create a fresh proposal if needed.", in.TargetRevision)
					if len(proposal) > 0 && proposal[0] {
						expected = fmt.Sprintf("Proposal target has changed (proposed at %s, source now unavailable). Approval refused (work_missing); decline it or wait for a fresh proposal.", in.TargetRevision)
					}
					if e["code"] != "source_revision_changed" || e["message"] != expected || detail["reason"] != "work_missing" || detail["current_revision"] != nil || detail["approved_revision"] != in.TargetRevision {
						t.Fatal(out)
					}
				}
				checkMissing(call("POST", "/pm/decisions/"+awaitingID+"/answer", pm.AnswerInput{Revision: 1, Approve: true, Text: "yes"}, 409), true)
				checkMissing(call("POST", "/pm/decisions/"+approvedID+"/dispatch", struct{}{}, 409))
				action := call("GET", "/pm/actions/"+actionID, nil, 200)
				if action["status"] != string(pm.Failed) || len(action["attempts"].([]any)) != 1 || action["deliverable"] != (authority == "nexus") || action["delivery_path"] != wantPath {
					t.Fatal(action)
				}
				failedBody, err := json.Marshal(action)
				if err != nil {
					t.Fatal(err)
				}
				checkMissing(call("POST", "/pm/actions/"+actionID+"/reconcile", struct{}{}, 409))
				checkMissing(call("POST", "/pm/decisions/"+approvedID+"/dispatch", struct{}{}, 409))
				afterFailed, err := json.Marshal(call("GET", "/pm/actions/"+actionID, nil, 200))
				if err != nil || !bytes.Equal(failedBody, afterFailed) {
					t.Fatalf("repeat dispatch or reconcile changed failed action: %s", afterFailed)
				}
				receipt := action["receipt"].(map[string]any)
				attempt := action["attempts"].([]any)[0].(map[string]any)
				if receipt["detail"] != "The task this approval refers to no longer exists (trashed or purged); nothing was sent" || attempt["sent_at"] != nil || attempt["status"] != "failed" || attempt["finished_at"] == nil {
					t.Fatal(action)
				}
				token = other.AccessToken
				call("POST", "/pm/actions/"+actionID+"/acknowledge", struct{}{}, 403)
				token = human.AccessToken
				ack := call("POST", "/pm/actions/"+actionID+"/acknowledge", struct{}{}, 200)
				if ack["status"] != string(pm.Acknowledged) || ack["acknowledged_at"] == nil || ack["receipt"].(map[string]any)["detail"] != receipt["detail"] || len(ack["attempts"].([]any)) != 1 {
					t.Fatal(ack)
				}
				checkMissing(call("POST", "/pm/actions/"+actionID+"/reconcile", struct{}{}, 409))
				// Exercise reconcile's source-call branch, not only a pending action guard.
				var stored pm.Action
				var raw []byte
				if err := db.QueryRow("SELECT body FROM pm_records WHERE kind='action' AND id=?", actionID).Scan(&raw); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(raw, &stored); err != nil {
					t.Fatal(err)
				}
				stored.Status = pm.Unknown
				raw, err = json.Marshal(stored)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = db.Exec("UPDATE pm_records SET body=? WHERE kind='action' AND id=?", raw, actionID); err != nil {
					t.Fatal(err)
				}
				checkMissing(call("POST", "/pm/actions/"+actionID+"/reconcile", struct{}{}, 409))
				var after []byte
				if err := db.QueryRow("SELECT body FROM pm_records WHERE kind='action' AND id=?", actionID).Scan(&after); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(raw, after) {
					t.Fatal("reconcile changed durable action")
				}
				if lifecycle == "purged" {
					// Existing pre-snapshot decisions cannot recover a source after
					// purge. Unknown must not become a false no-executor diagnosis.
					if _, err := db.Exec("UPDATE pm_records SET body=json_remove(body,'$.source_authority') WHERE kind IN ('decision','action')"); err != nil {
						t.Fatal(err)
					}
					for _, path := range []string{"/pm/decisions/" + approvedID, "/pm/actions/" + actionID} {
						legacy := call("GET", path, nil, 200)
						available, exists := legacy["deliverable"]
						if !exists || available != nil || legacy["delivery_path"] != "unknown" {
							t.Fatal(legacy)
						}
					}
					checkMissing(call("POST", "/pm/decisions/"+approvedID+"/dispatch", struct{}{}, 409))
					checkMissing(call("POST", "/pm/actions/"+actionID+"/reconcile", struct{}{}, 409))
				}
				in.RequestKey = "after-removal"
				call("POST", "/pm/decisions", in, 404)
				token = other.AccessToken
				call("POST", "/pm/decisions/"+approvedID+"/dispatch", struct{}{}, 403)
				call("POST", "/pm/decisions/"+awaitingID+"/answer", pm.AnswerInput{Revision: 1, Text: "no"}, 403)
				token = human.AccessToken
				declined := call("POST", "/pm/decisions/"+awaitingID+"/answer", pm.AnswerInput{Revision: 1, Text: "no"}, 200)
				if declined["status"] != "declined" {
					t.Fatal(fmt.Sprint(declined))
				}
			})
		}
	}
}
