package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
)

func TestRound6ResolutionEvidence(t *testing.T) {
	env := newPMStoreTestEnv(t)
	ctx := context.Background()
	db := env.workspace.DB()
	human := seedHumanPrincipalForLockoutTest(t, ctx, db, "r6-human", "r6-human-actor", "r6-human", "r6-token")
	machine := seedMachinePrincipalForLockoutTest(t, ctx, db, "r6-agent", "r6-agent-actor", "r6-agent", "r6-agent-token")
	store := env.primitiveStore.(*primitives.Store)
	board, err := store.CreateBoard(ctx, human.ActorID, map[string]any{"title": "Evidence board"})
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateWork(ctx, human.ActorID, asString(board["id"]), map[string]any{"title": "Evidence work"})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := store.CreateArtifact(ctx, human.ActorID, map[string]any{"kind": "text", "refs": []string{}, "title": "Accepted report"}, "report", "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	ref := "artifact:" + asString(artifact["id"])
	rt, err := NewPMRuntime(db, store, env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main", AgentActorID: machine.ActorID}})
	if err != nil {
		t.Fatal(err)
	}
	p := pm.Principal{WorkspaceID: "ws_main", ActorID: human.ActorID, Human: true}
	conv, err := rt.Service.CreateConversation(ctx, p, pm.CreateConversation{RequestKey: "r6-conv", Title: "Evidence", WorkRef: asString(work["ref"])})
	if err != nil {
		t.Fatal(err)
	}
	turn := pm.Turn{ID: "r6-turn", ConversationID: conv.ID, WorkspaceID: p.WorkspaceID, ActorID: p.ActorID, AgentActorID: machine.ActorID, Status: pm.Sending, Revision: 1, Deadline: time.Now().Add(time.Minute)}
	body, _ := json.Marshal(turn)
	if _, err := db.Exec(`INSERT INTO pm_records(kind,id,workspace_id,actor_id,parent_id,revision,body) VALUES('turn',?,?,?,?,1,?)`, turn.ID, p.WorkspaceID, p.ActorID, conv.ID, body); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Service.ClaimTurn(ctx, pm.Principal{WorkspaceID: p.WorkspaceID, ActorID: machine.ActorID}, pm.ClaimInput{RunnerID: "r6"}); err != nil {
		t.Fatal(err)
	}
	input := pm.DecisionInput{RequestKey: "r6-done", WorkRef: asString(work["ref"]), Scope: "work.phase", TargetRevision: "1", Instruction: "Finish", Payload: &pm.ActionPayload{Phase: "done", ResolutionRefs: []string{ref}}}
	call := func(method, path, token string, in any) *httptest.ResponseRecorder {
		var body []byte
		if in != nil {
			body, _ = json.Marshal(in)
		}
		rr := httptest.NewRecorder()
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		rt.ServeHTTP(rr, r)
		return rr
	}
	for _, missing := range []string{"artifact:does-not-exist", "event:does-not-exist", "document_revision:does-not-exist"} {
		for _, route := range []struct{ path, token string }{{"/pm/decisions", human.AccessToken}, {"/pm/turns/r6-turn/decisions", machine.AccessToken}} {
			input.Payload.ResolutionRefs = []string{ref, missing}
			rr := call("POST", route.path, route.token, input)
			if rr.Code != 400 || !strings.Contains(rr.Body.String(), "invalid_request") || !strings.Contains(rr.Body.String(), missing) {
				t.Fatalf("%s: %d %s", route.path, rr.Code, rr.Body)
			}
		}
	}
	input.Payload.ResolutionRefs = []string{ref}
	rr := call("POST", "/pm/decisions", human.AccessToken, input)
	if rr.Code != 201 {
		t.Fatalf("%d %s", rr.Code, rr.Body)
	}
	var decision pm.Decision
	if err := json.Unmarshal(rr.Body.Bytes(), &decision); err != nil {
		t.Fatal(err)
	}
	assertSummary := func(rr *httptest.ResponseRecorder, exists bool) {
		t.Helper()
		var out struct {
			Payload struct {
				Resolution []pm.ResolutionRef `json:"resolution"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if len(out.Payload.Resolution) != 1 {
			t.Fatalf("no projection: %s", rr.Body)
		}
		got := out.Payload.Resolution[0]
		if got.Ref != ref || got.Kind != "artifact" || got.Exists != exists || (exists && got.TitleOrSummary != "Accepted report") || (!exists && got.TitleOrSummary != "") {
			t.Fatalf("%+v", got)
		}
	}
	assertSummary(rr, true)
	// A replay has unchanged durable intent, and turn proposals get the same projection.
	assertSummary(call("POST", "/pm/decisions", human.AccessToken, input), true)
	rr = call("POST", "/pm/decisions/"+decision.ID+"/answer", human.AccessToken, pm.AnswerInput{Revision: 1, Approve: true, Text: "yes"})
	if rr.Code != 200 {
		t.Fatalf("%d %s", rr.Code, rr.Body)
	}
	assertSummary(rr, true)
	input.RequestKey = "r6-turn-proposal"
	assertSummary(call("POST", "/pm/turns/r6-turn/decisions", machine.AccessToken, input), true)
	var saved []byte
	if err := db.QueryRow(`SELECT body FROM pm_records WHERE kind='decision' AND id=?`, decision.ID).Scan(&saved); err != nil || bytes.Contains(saved, []byte(`"resolution":`)) {
		t.Fatalf("derived data persisted: %s %v", saved, err)
	}
	if _, err := store.TrashArtifact(ctx, human.ActorID, asString(artifact["id"]), "removed before dispatch"); err != nil {
		t.Fatal(err)
	}
	assertSummary(call("GET", "/pm/decisions/"+decision.ID, human.AccessToken, nil), false)
	list := call("GET", "/pm/decisions", human.AccessToken, nil)
	var page struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &page); err != nil || len(page.Items) != 2 {
		t.Fatalf("%s %v", list.Body, err)
	}
	item := httptest.NewRecorder()
	item.Body.Write(page.Items[0])
	assertSummary(item, false)
	for _, route := range []struct{ path, token string }{{"/pm/decisions", human.AccessToken}, {"/pm/turns/r6-turn/decisions", machine.AccessToken}} {
		input.RequestKey = "trashed"
		rr = call("POST", route.path, route.token, input)
		if rr.Code != 400 || !strings.Contains(rr.Body.String(), ref) {
			t.Fatalf("trashed: %d %s", rr.Code, rr.Body)
		}
	}
	rr = call("POST", "/pm/decisions/"+decision.ID+"/dispatch", human.AccessToken, struct{}{})
	if rr.Code != 200 {
		t.Fatalf("%d %s", rr.Code, rr.Body)
	}
	var action pm.Action
	if err := json.Unmarshal(rr.Body.Bytes(), &action); err != nil {
		t.Fatal(err)
	}
	if action.Status != pm.Failed || len(action.Attempts) != 1 || action.Attempts[0].SentAt != nil || !strings.Contains(action.Receipt.Detail, ref) {
		t.Fatalf("%+v", action)
	}
	after, err := store.GetWork(ctx, input.WorkRef)
	if err != nil || after["phase"] != work["phase"] || after["decision_revision"] != work["decision_revision"] {
		t.Fatalf("mutated: %+v %v", after, err)
	}
	// The canonical transaction independently rejects vanished evidence even when
	// the executor is called directly (covering a race after PM's preflight).
	_, err = executeWorkPhase(ctx, store, pm.Action{ActorID: p.ActorID, WorkRef: input.WorkRef, TargetRevision: "1", Payload: input.Payload})
	var native *pm.NativeExecutionError
	if !errors.As(err, &native) || native.WriteStarted || !strings.Contains(err.Error(), ref) {
		t.Fatalf("canonical gate: %v", err)
	}
	// Restored evidence supports a fresh approval and a real canonical completion.
	if _, err := store.RestoreArtifact(ctx, p.ActorID, asString(artifact["id"])); err != nil {
		t.Fatal(err)
	}
	input.RequestKey = "r6-restored"
	fresh, err := rt.Service.ProposeDecision(ctx, p, input)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err = rt.Service.AnswerDecision(ctx, p, fresh.ID, pm.AnswerInput{Revision: 1, Approve: true, Text: "accept restored evidence"})
	if err != nil {
		t.Fatal(err)
	}
	applied, err := rt.Service.DispatchDecision(ctx, p, fresh.ID)
	if err != nil || applied.Status != pm.Verified || !applied.Receipt.IndependentlyVerified {
		t.Fatalf("valid completion: %+v %v", applied, err)
	}
	completed, err := store.GetWork(ctx, input.WorkRef)
	if err != nil || completed["phase"] != "done" {
		t.Fatalf("completion: %+v %v", completed, err)
	}
	// Read-only projection is not an accepted request parameter.
	forged := map[string]any{"request_key": "forged", "work_ref": input.WorkRef, "scope": "work.phase", "target_revision": "1", "instruction": "Finish", "payload": map[string]any{"phase": "done", "resolution_refs": []string{ref}, "resolution": []any{map[string]any{"exists": true}}}}
	if rr := call("POST", "/pm/decisions", human.AccessToken, forged); rr.Code != 400 {
		t.Fatalf("projection accepted: %d %s", rr.Code, rr.Body)
	}
}

func TestRound6RuntimeAuthentication(t *testing.T) {
	env := newPMStoreTestEnv(t)
	ctx := context.Background()
	db := env.workspace.DB()
	h := seedHumanPrincipalForLockoutTest(t, ctx, db, "r6-auth", "r6-auth-actor", "r6-auth", "r6-auth-token")
	seedMachinePrincipalForLockoutTest(t, ctx, db, "r6-auth-agent", "r6-auth-agent-actor", "r6-auth-agent", "r6-machine-token")
	rt, err := NewPMRuntime(db, env.primitiveStore.(*primitives.Store), env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, header, path, code string
		status                   int
	}{{"missing", "", "/pm/decisions", "auth_required", 401}, {"malformed", "Basic abc", "/pm/decisions", "invalid_token", 401}, {"invalid", "Bearer invalid", "/pm/decisions", "invalid_token", 401}, {"valid", "Bearer " + h.AccessToken, "/pm/decisions", "", 200}, {"authorization", "Bearer r6-machine-token", "/pm/bindings", "forbidden", 403}, {"expired", "Bearer " + h.AccessToken, "/pm/decisions", "invalid_token", 401}} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "expired" {
				if _, err := db.Exec(`UPDATE auth_access_tokens SET expires_at='2000-01-01T00:00:00Z' WHERE agent_id=?`, h.AgentID); err != nil {
					t.Fatal(err)
				}
			}
			rr := httptest.NewRecorder()
			r := httptest.NewRequest("GET", tc.path, nil)
			r.Header.Set("Authorization", tc.header)
			rt.ServeHTTP(rr, r)
			if rr.Code != tc.status || !strings.Contains(rr.Body.String(), tc.code) {
				t.Fatalf("%d %s", rr.Code, rr.Body)
			}
			if tc.status == 401 {
				var got map[string]any
				if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				body := got["error"].(map[string]any)
				if body["recoverable"] != true || body["hint"] == "" {
					t.Fatal(body)
				}
			}
		})
	}
}
