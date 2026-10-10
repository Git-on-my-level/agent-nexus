package server

import (
	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPMCardContextAndApprovedNote(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newPMStoreTestEnv(t)
	human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "cards-human", "cards-human-actor", "cards-human", "cards-human-token")
	machine := seedMachinePrincipalForLockoutTest(t, ctx, env.workspace.DB(), "cards-pm", "cards-pm-actor", "cards-pm", "cards-pm-token")
	store := env.primitiveStore.(*primitives.Store)
	w, err := store.CreateWork(ctx, human.ActorID, "", map[string]any{"title": "Pinned card", "summary": "Full card prose"})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SetCardPlan(ctx, human.ActorID, anyString(w["id"]), anyString(w["updated_at"]), plans.Plan{Steps: []plans.Step{{ID: "done", Title: "Accepted", Status: "done"}, {ID: "next", Title: "Next"}}}); err != nil {
		t.Fatal(err)
	}
	ref := anyString(w["ref"])
	rt, err := newOnboardedPMRuntime(t, env.workspace.DB(), store, env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main", AgentActorID: machine.ActorID}})
	if err != nil {
		t.Fatal(err)
	}
	hp := pm.Principal{WorkspaceID: "ws_main", ActorID: human.ActorID, Human: true}
	agent := pm.Principal{WorkspaceID: "ws_main", ActorID: machine.ActorID}
	c, err := rt.Service.CreateConversation(ctx, hp, pm.CreateConversation{RequestKey: "cards", Title: "Cards", WorkRef: ref})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := rt.Service.PostMessage(ctx, hp, c.ID, pm.MessageInput{RequestKey: "m", Text: "Summarize"})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := rt.Service.ClaimTurn(ctx, agent, pm.ClaimInput{RunnerID: "runner"})
	if err != nil {
		t.Fatal(err)
	}
	read := func(view string) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"view": view, "lease_token": claim.LeaseToken, "context_ref": ref})
		req := httptest.NewRequest("POST", "/pm/turns/"+turn.ID+"/context", strings.NewReader(string(raw)))
		req.Header.Set("Authorization", "Bearer "+machine.AccessToken)
		rr := httptest.NewRecorder()
		rt.ServeHTTP(rr, req)
		if rr.Code != 200 {
			t.Fatalf("context: %d %s", rr.Code, rr.Body)
		}
		var page map[string]any
		_ = json.Unmarshal(rr.Body.Bytes(), &page)
		items := page["items"].([]any)
		if len(items) != 1 {
			t.Fatal(page)
		}
		return items[0].(map[string]any)
	}
	secret, err := store.CreateWork(ctx, "another-owner", "", map[string]any{"title": "PrivateDecisionSecret"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PatchThread(ctx, "another-owner", anyString(secret["thread_id"]), map[string]any{"pm_actor_id": "another-owner"}, nil); err != nil {
		t.Fatal(err)
	}
	// A public-card decision inherits private evidence in its reason. The PM
	// can see it, but the requesting human cannot.
	_, err = rt.Service.ProposeDecision(ctx, hp, pm.DecisionInput{RequestKey: "private-ref", WorkRef: ref, Scope: "work.phase", Instruction: "PrivateDecisionSecret " + anyString(secret["ref"]), TargetRevision: "1.1", Payload: &pm.ActionPayload{Phase: "ready"}})
	if err != nil {
		t.Fatal(err)
	}
	ask, err := store.AppendEvent(ctx, human.ActorID, map[string]any{"type": "human_attention_requested", "thread_id": w["thread_id"], "refs": []string{ref}, "payload": map[string]any{"subject_ref": ref, "requester_actor_id": human.ActorID, "title": "TrashedAskSecret"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.TrashEvent(ctx, human.ActorID, anyString(ask["id"]), "No longer needed"); err != nil {
		t.Fatal(err)
	}
	compact := read("cards")
	if compact["title"] != "Pinned card" || compact["phase"] != "backlog" || compact["health"] == nil || compact["plan_progress"].(map[string]any)["done"] != float64(1) {
		t.Fatal(compact)
	}
	visible, _ := json.Marshal(compact)
	if strings.Contains(string(visible), "PrivateDecisionSecret") || strings.Contains(string(visible), "TrashedAskSecret") {
		t.Fatalf("private/lifecycle data leaked: %s", visible)
	}
	// The turn PM may not launder another reader's private evidence.
	ambient := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: machine.ActorID, PMActorID: machine.ActorID})
	if _, e := rt.Service.ProposeForTurn(ambient, agent, turn.ID, pm.DecisionInput{RequestKey: "private-evidence", WorkRef: ref, Scope: "work.phase", Instruction: "Private proof", TargetRevision: "1.1", Payload: &pm.ActionPayload{Phase: "ready", EvidenceRefs: []string{anyString(secret["ref"])}}}, claim.LeaseToken); e == nil {
		t.Fatal("PM admitted another reader's private evidence")
	}
	// Rename after pinning: the old handle remains an exact permitted pin.
	oldHandle := strings.TrimPrefix(ref, "card:")
	if _, e := env.workspace.DB().Exec(`UPDATE cards SET handle='renamed-pinned-card' WHERE id=?`, w["id"]); e != nil {
		t.Fatal(e)
	}
	if _, e := env.workspace.DB().Exec(`INSERT INTO resource_handle_aliases(id,resource_type,alias_handle,resource_id,canonical_handle,created_at) VALUES('pm-pinned-alias','card',?,?,'renamed-pinned-card','2026-10-10T00:00:00Z')`, oldHandle, w["id"]); e != nil {
		t.Fatal(e)
	}
	c.ContextRefs = []string{ref, "card:renamed-pinned-card"}
	conversationJSON, _ := json.Marshal(c)
	if _, e := env.workspace.DB().Exec(`UPDATE pm_records SET body=? WHERE kind='conversation' AND id=?`, conversationJSON, c.ID); e != nil {
		t.Fatal(e)
	}
	multiReq := httptest.NewRequest("POST", "/pm/turns/"+turn.ID+"/context", strings.NewReader(`{"view":"cards","lease_token":"`+claim.LeaseToken+`"}`))
	multiReq.Header.Set("Authorization", "Bearer "+machine.AccessToken)
	multiResult := httptest.NewRecorder()
	rt.ServeHTTP(multiResult, multiReq)
	var multiPage pm.ContextPage
	if multiResult.Code != 200 || json.Unmarshal(multiResult.Body.Bytes(), &multiPage) != nil || len(multiPage.Items) != 2 {
		t.Fatalf("alias pins: %d %s", multiResult.Code, multiResult.Body)
	}
	seenRefs := map[string]bool{}
	for _, item := range multiPage.Items {
		seenRefs[anyString(item.(map[string]any)["ref"])] = true
	}
	if !seenRefs[ref] || !seenRefs["card:renamed-pinned-card"] {
		t.Fatalf("pins collapsed: %+v", multiPage)
	}
	full := read("card")
	if full["summary"] != "Full card prose" || full["decision_revision"] == nil || full["plan"] == nil {
		t.Fatal(full)
	}
	d, err := rt.Service.ProposeForTurn(ctx, agent, turn.ID, pm.DecisionInput{RequestKey: "note", WorkRef: ref, Scope: "work.note", Instruction: "Keep context", TargetRevision: anyString(full["decision_revision"]), Payload: &pm.ActionPayload{Note: "Human-approved note"}}, claim.LeaseToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = rt.Service.AnswerDecision(ctx, agent, d.ID, pm.AnswerInput{Revision: 1, Approve: true, Text: "Forged"}); err == nil {
		t.Fatal("PM approved")
	}
	if _, err = store.GetEvent(ctx, "pm_note_"+"pm_unknown"); err == nil {
		t.Fatal("premature message")
	}
	if _, err = rt.Service.AnswerDecision(ctx, hp, d.ID, pm.AnswerInput{Revision: 1, Approve: true, Text: "Approved"}); err != nil {
		t.Fatal(err)
	}
	action, err := rt.Service.DispatchDecision(ctx, hp, d.ID)
	if err != nil || action.Status != pm.Verified {
		t.Fatalf("dispatch %+v %v", action, err)
	}
	e, err := store.GetEvent(ctx, "pm_note_"+action.ID)
	if err != nil || e["payload"].(map[string]any)["text"] != "Human-approved note" {
		t.Fatal(e, err)
	}
	// Repeated approved action execution keeps one deterministic message.
	if _, err = executeCardNote(ctx, store, action); err != nil {
		t.Fatal(err)
	}
	var count int
	_ = env.workspace.DB().QueryRow(`SELECT count(*) FROM events WHERE id=?`, "pm_note_"+action.ID).Scan(&count)
	if count != 1 {
		t.Fatal(count)
	}
	// A revision change fences a stale approved note before any write.
	if _, e := env.workspace.DB().Exec(`UPDATE work_metadata SET version=version+1 WHERE card_id=?`, w["id"]); e != nil {
		t.Fatal(e)
	}
	if _, e := executeCardNote(ctx, store, action); !errors.Is(e, pm.ErrStale) {
		t.Fatalf("stale note: %v", e)
	}
	// Revocation after pinning prevents ambient PM access on the next read.
	if _, err = store.PatchThread(ctx, human.ActorID, anyString(w["thread_id"]), map[string]any{"pm_actor_id": "another-owner"}, nil); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"view": "card", "lease_token": claim.LeaseToken, "context_ref": ref})
	req := httptest.NewRequest("POST", "/pm/turns/"+turn.ID+"/context", strings.NewReader(string(raw)))
	req.Header.Set("Authorization", "Bearer "+machine.AccessToken)
	rr := httptest.NewRecorder()
	rt.ServeHTTP(rr, req)
	if strings.Contains(rr.Body.String(), "Pinned card") || strings.Contains(rr.Body.String(), "Human-approved note") {
		t.Fatalf("requester scope leaked: %s", rr.Body)
	}
}
