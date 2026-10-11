package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

// Exercise real authentication, scoped SQLite, projections and the mounted
// native/PM handlers. A selected PM is an ordinary reader outside a lease.
func TestPMDirectReadsHaveOrdinaryAgentVisibility(t *testing.T) {
	if testing.Short() {
		t.Skip("real-core privacy integration")
	}
	ctx := context.Background()
	env := newPMStoreTestEnv(t)
	store := env.primitiveStore.(*primitives.Store)
	db := env.workspace.DB()
	requester := seedHumanPrincipalForLockoutTest(t, ctx, db, "direct-requester", "direct-requester-actor", "direct-requester", "direct-requester-token")
	owner := seedHumanPrincipalForLockoutTest(t, ctx, db, "direct-owner", "direct-owner-actor", "direct-owner", "direct-owner-token")
	agent := seedMachinePrincipalForLockoutTest(t, ctx, db, "direct-pm", "direct-pm-actor", "direct-pm", "direct-pm-token")
	rt, err := newOnboardedPMRuntime(t, db, store, env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main", AgentActorID: agent.ActorID}})
	if err != nil {
		t.Fatal(err)
	}
	contract, err := schema.Load("../../../contracts/anx-schema.yaml")
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler("", WithSchemaContract(contract), WithAuthStore(env.authStore), WithActorRegistry(env.registry), WithPrimitiveStore(store), WithPMRuntime(rt))
	work := func(actor, title string, private bool) map[string]any {
		t.Helper()
		w, err := store.CreateWork(ctx, actor, "", map[string]any{"title": title})
		if err != nil {
			t.Fatal(err)
		}
		if private {
			if _, err = store.PatchThread(ctx, actor, anyString(w["thread_id"]), map[string]any{"pm_actor_id": actor}, nil); err != nil {
				t.Fatal(err)
			}
		}
		return w
	}
	public := work(owner.ActorID, "PublicCardMarker", false)
	secret := work(owner.ActorID, "OtherOwnerSecretMarker", true)
	requested := work(requester.ActorID, "RequesterPrivateMarker", true)
	owned := work(agent.ActorID, "AgentOwnedMarker", true)
	decision := func(w map[string]any, key string) (pm.Decision, string) {
		t.Helper()
		p := pm.Principal{WorkspaceID: "ws_main", ActorID: owner.ActorID, Human: true}
		d, err := rt.Service.ProposeDecision(ctx, p, pm.DecisionInput{RequestKey: key, WorkRef: anyString(w["ref"]), Scope: "work.phase", Instruction: key, TargetRevision: "1.1", Payload: &pm.ActionPayload{Phase: "ready"}})
		if err != nil {
			t.Fatal(err)
		}
		d, err = rt.Service.AnswerDecision(ctx, p, d.ID, pm.AnswerInput{Revision: d.Revision, Approve: true, Text: key})
		if err != nil {
			t.Fatal(err)
		}
		return d, d.ActionID
	}
	privateDecision, privateAction := decision(secret, "OtherOwnerDecisionMarker")
	publicDecision, publicAction := decision(public, "PublicDecisionMarker")
	_, err = store.AppendEvent(ctx, owner.ActorID, map[string]any{"type": "human_attention_requested", "thread_id": secret["thread_id"], "refs": []string{anyString(secret["ref"])}, "payload": map[string]any{"subject_ref": secret["ref"], "requester_actor_id": owner.ActorID, "title": "OtherOwnerAskMarker"}})
	if err != nil {
		t.Fatal(err)
	}
	conversation := func(actor, key, ref string) (pm.Conversation, pm.Turn) {
		t.Helper()
		p := pm.Principal{WorkspaceID: "ws_main", ActorID: actor, Human: true}
		c, err := rt.Service.CreateConversation(ctx, p, pm.CreateConversation{RequestKey: key, Title: key, WorkRef: ref})
		if err != nil {
			t.Fatal(err)
		}
		turn, err := rt.Service.PostMessage(ctx, p, c.ID, pm.MessageInput{RequestKey: key, Text: key})
		if err != nil {
			t.Fatal(err)
		}
		return c, turn
	}
	_, turn := conversation(requester.ActorID, "RequesterConversationMarker", anyString(requested["ref"]))
	otherConversation, otherTurn := conversation(owner.ActorID, "OtherOwnerConversationMarker", anyString(secret["ref"]))
	call := func(method, path, token, lease string, body any, status int) string {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
		r.Header.Set("Authorization", "Bearer "+token)
		if lease != "" {
			r.Header.Set("X-ANX-PM-Lease-Token", lease)
		}
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, r)
		if out.Code != status {
			t.Fatalf("%s %s: %d, want %d: %s", method, path, out.Code, status, out.Body)
		}
		return out.Body.String()
	}
	assertHidden := func(body string) {
		t.Helper()
		for _, marker := range []string{"OtherOwnerSecretMarker", "OtherOwnerDecisionMarker", "OtherOwnerAskMarker", "OtherOwnerConversationMarker", "RequesterPrivateMarker", "RequesterConversationMarker"} {
			if strings.Contains(body, marker) {
				t.Fatalf("private content escaped: %s", body)
			}
		}
	}
	for _, path := range []string{"/pm/context?limit=8", "/pm/decisions?limit=1", "/pm/actions?limit=1", "/pm/conversations?limit=1", "/work?limit=8", "/events?limit=8", "/inbox", "/overview"} {
		got := call("GET", path, agent.AccessToken, "", nil, 200)
		assertHidden(got)
		if strings.HasPrefix(path, "/pm/decisions?") || strings.HasPrefix(path, "/pm/actions?") {
			var page pm.Page[map[string]any]
			if err := json.Unmarshal([]byte(got), &page); err != nil || len(page.Items) != 1 || page.HasMore || page.NextCursor != "" || !strings.Contains(got, "PublicDecisionMarker") {
				t.Fatalf("private row affected visible pagination: %s", got)
			}
		}
		// Supplying a credential on another route never enables runner scope.
		assertHidden(call("GET", path, agent.AccessToken, "forged", nil, 200))
	}
	for _, path := range []string{"/pm/decisions/" + privateDecision.ID, "/pm/actions/" + privateAction, "/pm/conversations/" + otherConversation.ID, "/pm/turns/" + otherTurn.ID, "/work/" + anyString(secret["id"]), "/threads/" + anyString(secret["thread_id"])} {
		assertHidden(call("GET", path, agent.AccessToken, "", nil, 404))
	}
	for _, tc := range []struct {
		path   string
		body   any
		status int
	}{
		{"/pm/decisions/" + privateDecision.ID + "/answer", map[string]any{"revision": 1, "approve": true, "text": "yes"}, 403},
		{"/pm/decisions/" + privateDecision.ID + "/dispatch", struct{}{}, 403},
		{"/pm/actions/" + privateAction + "/reconcile", struct{}{}, 404},
		{"/pm/actions/" + privateAction + "/acknowledge", struct{}{}, 403},
		{"/pm/conversations/" + otherConversation.ID + "/messages", pm.MessageInput{RequestKey: "forged", Text: "Read"}, 404},
	} {
		assertHidden(call("POST", tc.path, agent.AccessToken, "forged", tc.body, tc.status))
	}
	assertHidden(call("GET", "/pm/bindings", agent.AccessToken, "", nil, 403))
	assertHidden(call("GET", "/pm/presence", agent.AccessToken, "", nil, 200))
	assertHidden(call("GET", "/pm/context?work_ref="+url.QueryEscape(anyString(secret["ref"])), agent.AccessToken, "", nil, 403))
	for _, path := range []string{"/pm/decisions/" + publicDecision.ID, "/pm/actions/" + publicAction, "/pm/context?work_ref=" + url.QueryEscape(anyString(owned["ref"]))} {
		assertHidden(call("GET", path, agent.AccessToken, "", nil, 200))
	}
	if got := call("GET", "/pm/context?work_ref="+url.QueryEscape(anyString(secret["ref"])), owner.AccessToken, "", nil, 200); !strings.Contains(got, "OtherOwnerSecretMarker") {
		t.Fatal("owner lost private access")
	}
	var claimed pm.Turn
	if err := json.Unmarshal([]byte(call("POST", "/pm/turns/claim", agent.AccessToken, "", pm.ClaimInput{RunnerID: "privacy"}, 200)), &claimed); err != nil || claimed.ID != turn.ID || claimed.LeaseToken == "" {
		t.Fatalf("claim: %+v %v", claimed, err)
	}
	assertHidden(call("GET", "/pm/context?limit=8", agent.AccessToken, claimed.LeaseToken, nil, 200))
	for _, view := range []string{"", "cards", "card"} {
		body := map[string]any{"lease_token": claimed.LeaseToken, "limit": 8, "view": view}
		if view == "card" {
			body["context_ref"] = requested["ref"]
		}
		got := call("POST", "/pm/turns/"+turn.ID+"/context", agent.AccessToken, "", body, 200)
		if !strings.Contains(got, "RequesterPrivateMarker") || strings.Contains(got, "OtherOwner") {
			t.Fatalf("leased %q context: %s", view, got)
		}
	}
	// A claimed proposal can still target the requester's private card.
	call("POST", "/pm/turns/"+turn.ID+"/decisions", agent.AccessToken, "", map[string]any{"lease_token": claimed.LeaseToken, "request_key": "requester-note", "work_ref": requested["ref"], "scope": "work.note", "instruction": "Keep a note", "target_revision": "1.1", "payload": map[string]any{"note": "Requested note"}}, 200)
	call("GET", "/pm/turns/"+turn.ID, agent.AccessToken, claimed.LeaseToken, nil, 200)
	assertHidden(call("GET", "/pm/turns/"+turn.ID, agent.AccessToken, "wrong", nil, 404))
	assertHidden(call("GET", "/pm/turns/"+otherTurn.ID, agent.AccessToken, claimed.LeaseToken, nil, 404))
	call("POST", "/pm/turns/"+turn.ID+"/complete", agent.AccessToken, "", map[string]any{"lease_token": claimed.LeaseToken, "text": "Finished"}, 200)
	call("GET", "/pm/turns/"+turn.ID, agent.AccessToken, claimed.LeaseToken, nil, 200)
	assertHidden(call("GET", "/pm/turns/"+turn.ID, agent.AccessToken, "wrong", nil, 404))
}

func TestPMRunnerScopeAllowlist(t *testing.T) {
	for _, tc := range []struct {
		method, path, lease string
		want                bool
	}{
		{"POST", "/pm/turns/claim", "", true},
		{"POST", "/pm/turns/id/context", "", true},
		{"GET", "/pm/turns/id", "lease", true},
		{"GET", "/pm/turns/id", "", false},
		{"GET", "/pm/context", "lease", false},
		{"POST", "/pm/decisions", "lease", false},
		{"POST", "/pm/turns/id/new-route", "lease", false},
		{"GET", "/pm/turns/id/context", "lease", false},
		{"GET", "/pm/turns/id/", "lease", false},
		{"POST", "/pm/turns//context", "lease", false},
	} {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		r.Header.Set("X-ANX-PM-Lease-Token", tc.lease)
		if got := pmRunnerRequest(r); got != tc.want {
			t.Errorf("%s %s: %v, want %v", tc.method, tc.path, got, tc.want)
		}
	}
}
