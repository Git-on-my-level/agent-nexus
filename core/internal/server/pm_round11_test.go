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

func TestRound11DecisionExecutorRegistry(t *testing.T) {
	env := newPMStoreTestEnv(t)
	ctx := context.Background()
	human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "r11-human", "r11-actor", "r11-human", "r11-token")
	store := env.primitiveStore.(*primitives.Store)
	board, err := store.CreateBoard(ctx, human.ActorID, map[string]any{"title": "Round11"})
	if err != nil {
		t.Fatal(err)
	}
	rt, err := NewPMRuntime(env.workspace.DB(), store, env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}})
	if err != nil {
		t.Fatal(err)
	}
	call := func(t *testing.T, method, path string, in any, status int) map[string]any {
		t.Helper()
		raw, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+human.AccessToken)
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
	for i, tc := range []struct{ authority, scope, path string }{
		{"nexus", "work.phase", "nexus"}, {"nexus", "work.annotate", "nexus"}, {"nexus", "github", "none"}, {"github", "work.phase", "none"}, {"github", "work.annotate", "none"},
	} {
		t.Run(fmt.Sprintf("%s/%s", tc.authority, tc.scope), func(t *testing.T) {
			w, err := store.CreateWork(ctx, human.ActorID, asString(board["id"]), map[string]any{"title": fmt.Sprint(i), "source": map[string]any{"authority": tc.authority, "connection_id": "fixture", "native_id": fmt.Sprint(i)}})
			if err != nil {
				t.Fatal(err)
			}
			in := pm.DecisionInput{RequestKey: fmt.Sprint(i), WorkRef: asString(w["ref"]), Scope: tc.scope, Instruction: "Ready", TargetRevision: "1"}
			if tc.scope == "work.phase" {
				in.Payload = &pm.ActionPayload{Phase: "ready"}
			}
			d := call(t, "POST", "/pm/decisions", in, 201)
			id := d["id"].(string)
			check := func(out map[string]any) {
				t.Helper()
				if out["delivery_path"] != tc.path || out["deliverable"] != (tc.path != "none") {
					t.Fatal(out)
				}
			}
			check(d)
			check(call(t, "GET", "/pm/decisions/"+id, nil, 200))
			check(call(t, "POST", "/pm/decisions", in, 200))
			page := call(t, "GET", "/pm/decisions", nil, 200)
			found := false
			for _, row := range page["items"].([]any) {
				v := row.(map[string]any)
				if v["id"] == id {
					check(v)
					found = true
				}
			}
			if !found {
				t.Fatal("decision absent from page")
			}
			if tc.authority == "github" && tc.scope == "work.annotate" {
				// Native annotations are forbidden on source-owned work independently of routing.
				call(t, "POST", "/pm/decisions/"+id+"/answer", pm.AnswerInput{Revision: 1, Approve: true, Text: "yes"}, 403)
				return
			}
			answered := call(t, "POST", "/pm/decisions/"+id+"/answer", pm.AnswerInput{Revision: 1, Approve: true, Text: "yes"}, 200)
			check(answered)
			actionID := answered["action_id"].(string)
			a := call(t, "GET", "/pm/actions/"+actionID, nil, 200)
			if a["deliverable"] != d["deliverable"] {
				t.Fatalf("decision/action mismatch: %v %v", d, a)
			}
			if tc.path == "none" {
				call(t, "POST", "/pm/decisions/"+id+"/dispatch", struct{}{}, 503)
				a = call(t, "POST", "/pm/actions/"+actionID+"/acknowledge", struct{}{}, 200)
				if a["closed_without_delivery"] != true {
					t.Fatal(a)
				}
				if tc.authority == "github" && a["receipt"].(map[string]any)["detail"] != "Closed without delivery: no delivery path is configured for GitHub; nothing was sent." {
					t.Fatal(a)
				}
			}
		})
	}
}
