package server

import (
	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPMNoPinOverviewAndUnpinnedCardStayReaderScoped(t *testing.T) {
	ctx := context.Background()
	env := newPMStoreTestEnv(t)
	human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "overview-human", "overview-human-actor", "overview-human", "overview-human-token")
	machine := seedMachinePrincipalForLockoutTest(t, ctx, env.workspace.DB(), "overview-pm", "overview-pm-actor", "overview-pm", "overview-pm-token")
	store := env.primitiveStore.(*primitives.Store)
	child, err := store.CreateWork(ctx, human.ActorID, "", map[string]any{"title": "Plan step card"})
	if err != nil {
		t.Fatal(err)
	}
	parent, err := store.CreateWork(ctx, human.ActorID, "", map[string]any{"title": "Open initiative"})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SetCardPlan(ctx, human.ActorID, anyString(parent["id"]), anyString(parent["updated_at"]), plans.Plan{Steps: []plans.Step{{ID: "step", Title: "Follow this", Ref: anyString(child["ref"])}}}); err != nil {
		t.Fatal(err)
	}
	secret, err := store.CreateWork(ctx, "other-owner", "", map[string]any{"title": "PrivateOverviewSecret"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PatchThread(ctx, "other-owner", anyString(secret["thread_id"]), map[string]any{"pm_actor_id": "other-owner"}, nil); err != nil {
		t.Fatal(err)
	}
	rt, err := newOnboardedPMRuntime(t, env.workspace.DB(), store, env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main", AgentActorID: machine.ActorID}})
	if err != nil {
		t.Fatal(err)
	}
	hp := pm.Principal{WorkspaceID: "ws_main", ActorID: human.ActorID, Human: true}
	agent := pm.Principal{WorkspaceID: "ws_main", ActorID: machine.ActorID}
	_, err = rt.Service.ProposeDecision(ctx, hp, pm.DecisionInput{RequestKey: "decision", WorkRef: anyString(child["ref"]), Scope: "work.phase", Instruction: "Decide the next status", TargetRevision: "1.1", Payload: &pm.ActionPayload{Phase: "ready"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = rt.Service.ProposeDecision(ctx, hp, pm.DecisionInput{RequestKey: "private", WorkRef: anyString(child["ref"]), Scope: "work.note", Instruction: "PrivateOverviewSecret " + anyString(secret["ref"]), TargetRevision: "1.1", Payload: &pm.ActionPayload{Note: "Private evidence"}})
	if err != nil {
		t.Fatal(err)
	}
	askJSON, _ := json.Marshal(map[string]any{"kind": "ask", "title": "Choose the launch date", "body": "Which date works?", "response_proposals": []string{"Friday", "Monday"}, "recipient_actor_id": human.ActorID, "subject_ref": child["ref"]})
	if _, err = env.workspace.DB().Exec(`INSERT INTO derived_inbox_items(id,thread_id,category,trigger_at,generated_at,data_json) VALUES('overview-ask',?,'ask','2026-10-10T00:00:00Z','2026-10-10T00:00:00Z',?)`, child["thread_id"], string(askJSON)); err != nil {
		t.Fatal(err)
	}
	if _, err = store.AppendEvent(ctx, human.ActorID, map[string]any{"type": "message_posted", "refs": []string{}, "thread_id": child["thread_id"], "payload": map[string]any{"text": "Recent update without summary"}}); err != nil {
		t.Fatal(err)
	}
	c, err := rt.Service.CreateConversation(ctx, hp, pm.CreateConversation{RequestKey: "overview", Title: "No pins"})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := rt.Service.PostMessage(ctx, hp, c.ID, pm.MessageInput{RequestKey: "m", Text: "What needs my decision?"})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := rt.Service.ClaimTurn(ctx, agent, pm.ClaimInput{RunnerID: "runner"})
	if err != nil {
		t.Fatal(err)
	}
	read := func(view, ref, token string) (int, string) {
		raw, _ := json.Marshal(map[string]any{"view": view, "context_ref": ref, "lease_token": token})
		req := httptest.NewRequest("POST", "/pm/turns/"+turn.ID+"/context", strings.NewReader(string(raw)))
		req.Header.Set("Authorization", "Bearer "+machine.AccessToken)
		rr := httptest.NewRecorder()
		rt.ServeHTTP(rr, req)
		return rr.Code, rr.Body.String()
	}
	code, body := read("cards", "", claim.LeaseToken)
	if code != 200 || !strings.Contains(body, "Open initiative") || !strings.Contains(body, "Decide the next status") || !strings.Contains(body, "Choose the launch date") || strings.Contains(body, "PrivateOverviewSecret") {
		t.Fatalf("overview %d %s", code, body)
	}
	if !strings.Contains(body, "Which date works?") || !strings.Contains(body, "Friday") || !strings.Contains(body, "Recent update without summary") {
		t.Fatal(body)
	}
	for _, ref := range []string{anyString(parent["ref"]), anyString(child["ref"])} {
		code, body = read("card", ref, claim.LeaseToken)
		if code != 200 || !strings.Contains(body, ref) {
			t.Fatalf("unpin %d %s", code, body)
		}
	}
	code, body = read("card", anyString(secret["ref"]), claim.LeaseToken)
	if strings.Contains(body, "PrivateOverviewSecret") {
		t.Fatalf("secret %d %s", code, body)
	}
	code, _ = read("cards", "", "wrong")
	if code == 200 {
		t.Fatal("overview without lease")
	}
	// Populate each routing window with denied candidates. A preview stays
	// partial rather than refilling from older accessible records.
	for i := 0; i < 64; i++ {
		card, e := store.CreateWork(ctx, "other-owner", "", map[string]any{"title": "PrivateOverviewSecret candidate"})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = env.workspace.DB().Exec(`UPDATE cards SET column_key='blocked',updated_at='2040-01-01T00:00:00Z',thread_id=? WHERE id=?`, secret["thread_id"], card["id"]); e != nil {
			t.Fatal(e)
		}
	}
	for i := 0; i < 32; i++ {
		if _, e := store.AppendEvent(ctx, "other-owner", map[string]any{"id": fmt.Sprintf("private-overview-event-%d", i), "type": "message_posted", "refs": []string{}, "ts": "2040-01-01T00:00:00Z", "thread_id": secret["thread_id"], "payload": map[string]any{"text": "PrivateOverviewSecret activity"}}); e != nil {
			t.Fatal(e)
		}
	}
	for i := 0; i < 16; i++ {
		id := fmt.Sprintf("aaa-private-decision-%02d", i)
		d := pm.Decision{ID: id, WorkspaceID: "ws_main", ActorID: human.ActorID, WorkRef: anyString(secret["ref"]), Instruction: "PrivateOverviewSecret decision", Status: pm.AwaitingAnswer}
		raw, _ := json.Marshal(d)
		if _, e := env.workspace.DB().Exec(`INSERT INTO pm_records(kind,id,workspace_id,actor_id,revision,body) VALUES('decision',?,'ws_main',?,1,?)`, id, human.ActorID, raw); e != nil {
			t.Fatal(e)
		}
		for _, recipient := range []string{"", human.ActorID} {
			raw, _ = json.Marshal(map[string]any{"title": "PrivateOverviewSecret ask", "subject_ref": secret["ref"], "recipient_actor_id": recipient})
			if _, e := env.workspace.DB().Exec(`INSERT INTO derived_inbox_items(id,thread_id,category,trigger_at,generated_at,data_json) VALUES(?,?,'ask','1940-01-01T00:00:00Z','1940-01-01T00:00:00Z',?)`, fmt.Sprintf("private-ask-%s-%d", recipient, i), secret["thread_id"], raw); e != nil {
				t.Fatal(e)
			}
		}
	}
	code, body = read("cards", "", claim.LeaseToken)
	var page pm.ContextPage
	if code != 200 || json.Unmarshal([]byte(body), &page) != nil || len(page.Items)+len(page.Asks)+len(page.Decisions)+len(page.Activity) != 0 || len(page.Limitations) != 1 {
		t.Fatalf("denied windows %d %s", code, body)
	}
	code, body = read("card", anyString(child["ref"]), claim.LeaseToken)
	if code != 200 || !strings.Contains(body, "Plan step card") || strings.Contains(body, "PrivateOverviewSecret") {
		t.Fatalf("follow after partial overview %d %s", code, body)
	}
}
