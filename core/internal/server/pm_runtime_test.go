package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/observation"
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
)

func TestPMRuntimeNativeDecisionAuthorizationAndReadback(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	principal := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "pm-human", "pm-human-actor", "pm-human", "pm-human-token")
	store := env.primitiveStore.(*primitives.Store)
	board, err := store.CreateBoard(ctx, principal.ActorID, map[string]any{"title": "Native PM"})
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateWork(ctx, principal.ActorID, asString(board["id"]), map[string]any{"title": "Deliver accepted work"})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewPMRuntime(env.workspace.DB(), store, env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()
	post := func(path string, body map[string]any, status int) map[string]any {
		t.Helper()
		resp := postJSONExpectStatusWithAuth(t, srv.URL+path, body, principal.AccessToken, status)
		defer resp.Body.Close()
		var out map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	d := post("/pm/decisions", map[string]any{"request_key": "decision", "work_ref": work["ref"], "instruction": `{"next_action":"Review acceptance evidence"}`, "scope": "work.annotate", "target_revision": "1"}, 201)
	answer := post("/pm/decisions/"+asString(d["id"])+"/answer", map[string]any{"revision": 1, "approve": true, "text": "Approved exact annotation"}, 200)
	if answer["status"] != "answered" {
		t.Fatal(answer)
	}
	action := post("/pm/decisions/"+asString(d["id"])+"/dispatch", map[string]any{}, 200)
	if action["status"] != "source_reported" {
		t.Fatalf("dispatch incorrectly verified or failed: %v", action)
	}
	receipt := post("/pm/actions/"+asString(action["id"])+"/reconcile", map[string]any{}, 200)
	if receipt["status"] != "verified" {
		t.Fatal(receipt)
	}
	updated, err := store.GetWork(ctx, asString(work["ref"]))
	if err != nil || updated["next_action"] != "Review acceptance evidence" {
		t.Fatalf("action not persisted: %v %v", updated, err)
	}
	// Restart service against same workspace storage: decision and receipt survive.
	restored, err := NewPMRuntime(env.workspace.DB(), store, env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/pm/actions/"+asString(action["id"]), nil)
	req.Header.Set("Authorization", "Bearer "+principal.AccessToken)
	rr := httptest.NewRecorder()
	restored.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	if _, err = env.workspace.DB().ExecContext(ctx, `UPDATE agents SET revoked_at=? WHERE id=?`, time.Now().UTC().Format(time.RFC3339Nano), principal.AgentID); err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	restored.ServeHTTP(rr, req)
	if rr.Code != 403 {
		t.Fatalf("revoked principal retained PM access: %d", rr.Code)
	}
}
func TestPMRuntimeDoesNotTrustBodyIdentityOrConfigureProvider(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	seed := seedHumanPrincipalForLockoutTest(t, context.Background(), env.workspace.DB(), "pm-second", "pm-second-actor", "pm-second", "pm-second-token")
	handler, err := NewPMRuntime(env.workspace.DB(), env.primitiveStore.(*primitives.Store), env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()
	postJSONExpectStatusWithAuth(t, srv.URL+"/pm/conversations", map[string]any{"request_key": "forged", "title": "forged", "actor_id": seed.ActorID}, "", 403)
	resp := postJSONExpectStatusWithAuth(t, srv.URL+"/pm/conversations", map[string]any{"request_key": "conversation", "title": "Context"}, seed.AccessToken, 201)
	defer resp.Body.Close()
	var c map[string]any
	if err = json.NewDecoder(resp.Body).Decode(&c); err != nil {
		t.Fatal(err)
	}
	postJSONExpectStatusWithAuth(t, srv.URL+"/pm/conversations/"+asString(c["id"])+"/messages", map[string]any{"request_key": "turn", "text": "What changed?"}, seed.AccessToken, 503)
}

func TestPMRuntimeBridgeFailsClosedWithoutRuntimeEnvelope(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	_, err := NewPMRuntime(env.workspace.DB(), env.primitiveStore.(*primitives.Store), env.authStore, PMRuntimeConfig{
		PM:            pm.Config{WorkspaceID: "ws_main", AgentActorID: "pm-agent", AgentHandle: "pm"},
		BridgeEnabled: true,
	})
	if err != pm.ErrUnavailable {
		t.Fatalf("bridge without independently enforced envelope started: %v", err)
	}
}

func TestPMRuntimeSourceWriteStaysUnavailable(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	principal := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "pm-source", "pm-source-actor", "pm-source", "pm-source-token")
	store := env.primitiveStore.(*primitives.Store)
	board, err := store.CreateBoard(ctx, principal.ActorID, map[string]any{"title": "Source PM"})
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateWork(ctx, principal.ActorID, asString(board["id"]), map[string]any{
		"title":  "External issue",
		"source": map[string]any{"authority": "github", "connection_id": "fixture", "native_id": "org/repo#1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewPMRuntime(env.workspace.DB(), store, env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()
	post := func(path string, body map[string]any, status int) map[string]any {
		t.Helper()
		resp := postJSONExpectStatusWithAuth(t, srv.URL+path, body, principal.AccessToken, status)
		defer resp.Body.Close()
		var out map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	d := post("/pm/decisions", map[string]any{"request_key": "github-write", "work_ref": work["ref"], "instruction": "close", "scope": "github", "target_revision": "abc"}, 201)
	answer := post("/pm/decisions/"+asString(d["id"])+"/answer", map[string]any{"revision": 1, "approve": true, "text": "Approved exact source write"}, 200)
	if answer["status"] != "answered" {
		t.Fatal(answer)
	}
	post("/pm/decisions/"+asString(d["id"])+"/dispatch", map[string]any{}, http.StatusServiceUnavailable)
}

func TestPMChannelIngressUnavailableUntilConfigured(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	resp := postJSONExpectStatusWithAuth(t, env.server.URL+"/pm/ingress/telegram", map[string]any{"update_id": 1}, "", http.StatusServiceUnavailable)
	defer resp.Body.Close()
	resp = postJSONExpectStatusWithAuth(t, env.server.URL+"/pm/ingress/discord", map[string]any{"type": 1}, "", http.StatusServiceUnavailable)
	defer resp.Body.Close()
}

func TestPMRuntimeReplyUsesConversationAuthorization(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	seed := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "reply-fixture", "reply-fixture-actor", "reply-fixture", "reply-fixture-token")
	handler, err := NewPMRuntime(env.workspace.DB(), env.primitiveStore.(*primitives.Store), env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main", AgentActorID: seed.ActorID}})
	if err != nil {
		t.Fatal(err)
	}
	turn := pm.Turn{ID: "fixture-turn", ConversationID: "fixture-conversation", WorkspaceID: "ws_main", ActorID: seed.ActorID, AgentActorID: seed.ActorID, Status: pm.Sending, Revision: 1, Deadline: time.Now().Add(time.Minute)}
	body, _ := json.Marshal(turn)
	if _, err = env.workspace.DB().ExecContext(ctx, `INSERT INTO pm_records(kind,id,workspace_id,actor_id,parent_id,revision,body) VALUES('turn',?,?,?,?,1,?)`, turn.ID, turn.WorkspaceID, turn.ActorID, turn.ConversationID, body); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()
	postJSONExpectStatusWithAuth(t, srv.URL+"/pm/turns/fixture-turn/complete", map[string]any{"text": "Evidence reviewed", "evidence_refs": []string{}}, seed.AccessToken, 200)
}

type matchingRevisionReader struct{}

func (matchingRevisionReader) Capabilities() observation.Capabilities {
	return observation.Capabilities{Source: "github", ReadOne: true}
}
func (matchingRevisionReader) Read(_ context.Context, target observation.Target) (observation.Report, error) {
	return observation.Report{Target: target, ReaderID: "fixture", ReaderRevision: "1", ObservedAt: time.Now().UTC(), Knowledge: "reported", SourceRevision: "abc", Facts: map[string]any{"phase": "review"}, Coverage: observation.Coverage{Complete: true}, Evidence: []observation.Evidence{{Kind: "issue", Reference: "https://example.test/issue/1", Knowledge: "reported"}}}, nil
}

func TestPMRuntimeSourceReconcileDoesNotVerifyUnsentWrite(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	principal := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "pm-unsent", "pm-unsent-actor", "pm-unsent", "pm-unsent-token")
	store := env.primitiveStore.(*primitives.Store)
	board, err := store.CreateBoard(ctx, principal.ActorID, map[string]any{"title": "Unsent source"})
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateWork(ctx, principal.ActorID, asString(board["id"]), map[string]any{
		"title": "External issue",
		"source": map[string]any{
			"authority":     "github",
			"connection_id": "fixture",
			"native_id":     "org/repo#1",
			"revision":      "abc",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ref := asString(work["ref"])
	runtime, err := NewObservationRuntime(store, []ObservationBinding{{
		WorkRef:        ref,
		SourceNativeID: "org/repo#1",
		Target:         observation.Target{WorkspaceID: "ws_main", ConnectionID: "fixture", Source: "github", Kind: "issue", NativeID: "1", Repository: "org/repo"},
		Reader:         matchingRevisionReader{},
		Policy:         observation.RefreshPolicy{Interval: time.Minute, StaleAfter: time.Hour, Timeout: time.Second, MaxBackoff: time.Hour},
	}})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewPMRuntime(env.workspace.DB(), store, env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}, Observation: runtime})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()
	post := func(path string, body map[string]any, status int) map[string]any {
		t.Helper()
		resp := postJSONExpectStatusWithAuth(t, srv.URL+path, body, principal.AccessToken, status)
		defer resp.Body.Close()
		var out map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	d := post("/pm/decisions", map[string]any{"request_key": "github-write", "work_ref": ref, "instruction": "close", "scope": "github", "target_revision": "abc"}, 201)
	answer := post("/pm/decisions/"+asString(d["id"])+"/answer", map[string]any{"revision": 1, "approve": true, "text": "Approved exact source write"}, 200)
	if answer["status"] != "answered" {
		t.Fatal(answer)
	}
	post("/pm/decisions/"+asString(d["id"])+"/dispatch", map[string]any{}, 503)
	post("/pm/actions/"+asString(answer["action_id"])+"/reconcile", map[string]any{}, 400)
	receipt, err := reconcileSourceRead(ctx, store, runtime, pm.Action{ID: asString(answer["action_id"]), WorkRef: ref, TargetRevision: "abc"})
	if err != nil || receipt.Status != pm.Unknown || receipt.IndependentlyVerified {
		t.Fatalf("unsent write verified: %+v %v", receipt, err)
	}

}

func newPMStoreTestEnv(t *testing.T) authIntegrationEnv {
	t.Helper()
	ws, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	registry := actors.NewStore(ws.DB())
	if _, err = registry.EnsureSystemActor(context.Background(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	return authIntegrationEnv{workspace: ws, registry: registry, authStore: auth.NewStore(ws.DB()), primitiveStore: primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)}
}
func TestPMPhaseCanonicalMutationAndSourceRequest(t *testing.T) {
	env := newPMStoreTestEnv(t)
	ctx := context.Background()
	human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "phase-human", "phase-human-actor", "phase-human", "phase-token")
	machine := seedMachinePrincipalForLockoutTest(t, ctx, env.workspace.DB(), "phase-agent", "phase-agent-actor", "phase-agent", "phase-agent-token")
	store := env.primitiveStore.(*primitives.Store)
	board, err := store.CreateBoard(ctx, human.ActorID, map[string]any{"title": "Phases"})
	if err != nil {
		t.Fatal(err)
	}
	rt, err := NewPMRuntime(env.workspace.DB(), store, env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main", AgentActorID: machine.ActorID}})
	if err != nil {
		t.Fatal(err)
	}
	p := pm.Principal{WorkspaceID: "ws_main", ActorID: human.ActorID, Human: true}
	for _, authority := range []string{"nexus", "github", "multica", "git", "ssh_git"} {
		t.Run(authority, func(t *testing.T) {
			w, err := store.CreateWork(ctx, p.ActorID, asString(board["id"]), map[string]any{"title": authority, "source": map[string]any{"authority": authority, "connection_id": "fixture", "native_id": authority, "revision": "r1"}})
			if err != nil {
				t.Fatal(err)
			}
			revision := "r1"
			if authority == "nexus" {
				revision = "1"
			}
			input := pm.DecisionInput{RequestKey: authority, WorkRef: asString(w["ref"]), Instruction: "Prose is not the target", Scope: "work.phase", TargetRevision: revision, Payload: &pm.ActionPayload{Phase: "ready"}}
			agent := pm.Principal{WorkspaceID: p.WorkspaceID, ActorID: machine.ActorID}
			if _, err = rt.Service.ProposeDecision(ctx, agent, input); !errors.Is(err, pm.ErrForbidden) {
				t.Fatalf("direct agent proposal: %v", err)
			}
			agent.Human = true
			if _, err = rt.Service.ProposeDecision(ctx, agent, input); !errors.Is(err, pm.ErrForbidden) {
				t.Fatalf("forged human proposal: %v", err)
			}
			d, err := rt.Service.ProposeDecision(ctx, p, input)
			if err != nil {
				t.Fatal(err)
			}
			d, err = rt.Service.AnswerDecision(ctx, p, d.ID, pm.AnswerInput{Revision: 1, Approve: true, Text: "yes"})
			if err != nil {
				t.Fatal(err)
			}
			agent.Human = false
			if _, err = rt.Service.DispatchDecision(ctx, agent, d.ID); !errors.Is(err, pm.ErrForbidden) {
				t.Fatalf("agent dispatched phase action: %v", err)
			}
			a, err := rt.Service.DispatchDecision(ctx, p, d.ID)
			if authority != "nexus" && !errors.Is(err, pm.ErrUnavailable) {
				t.Fatalf("missing path: %v", err)
			}
			if authority == "nexus" && err != nil {
				t.Fatal(err)
			}
			updated, err := store.GetWork(ctx, input.WorkRef)
			if err != nil {
				t.Fatal(err)
			}
			if authority != "nexus" {
				actions, err := rt.Service.ListActions(ctx, p)
				if err != nil {
					t.Fatal(err)
				}
				for _, saved := range actions {
					if saved.ID == d.ActionID {
						a = saved
					}
				}
				if a.Status != pm.Pending || a.Deliverable || len(a.Attempts) != 0 || a.Revision != 1 || updated["phase"] != w["phase"] || updated["version"] != w["version"] {
					t.Fatalf("source changed: %+v %v", a, updated)
				}
				return
			}
			if a.Status != pm.Reported || updated["phase"] != "ready" || updated["version"] != int64(2) {
				t.Fatalf("phase not committed: %+v %v", a, updated)
			}
			a, err = rt.Service.ReconcileAction(ctx, p, a.ID)
			if err != nil || a.Status != pm.Verified {
				t.Fatalf("readback: %+v %v", a, err)
			}
			// The executor's transaction rejects a revision changed since authorization.
			if _, err = executeWorkPhase(ctx, store, pm.Action{ActorID: p.ActorID, WorkRef: input.WorkRef, Scope: "work.phase", TargetRevision: "1", Payload: &pm.ActionPayload{Phase: "review"}}); !errors.Is(err, pm.ErrStale) {
				t.Fatalf("stale execute %v", err)
			}
			input.RequestKey = "stale"
			input.Payload = &pm.ActionPayload{Phase: "review"}
			stale, err := rt.Service.ProposeDecision(ctx, p, input)
			if err != nil {
				t.Fatal(err)
			}
			_, err = rt.Service.AnswerDecision(ctx, p, stale.ID, pm.AnswerInput{Revision: 1, Approve: true, Text: "yes"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = rt.Service.DispatchDecision(ctx, p, stale.ID); !errors.Is(err, pm.ErrStale) {
				t.Fatalf("stale dispatch %v", err)
			}
		})
	}
}

func TestPMRuntimeRespondRequiresConfiguredUnrevokedActor(t *testing.T) {
	env := newPMStoreTestEnv(t)
	ctx := context.Background()
	selected := seedMachinePrincipalForLockoutTest(t, ctx, env.workspace.DB(), "selected-pm", "selected-pm-actor", "selected-pm", "selected-token")
	other := seedMachinePrincipalForLockoutTest(t, ctx, env.workspace.DB(), "other-pm", "other-pm-actor", "other-pm", "other-token")
	rt, err := NewPMRuntime(env.workspace.DB(), env.primitiveStore.(*primitives.Store), env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main", AgentActorID: selected.ActorID}})
	if err != nil {
		t.Fatal(err)
	}
	p := pm.Principal{WorkspaceID: "ws_main", ActorID: other.ActorID}
	if _, err = rt.Service.ClaimTurn(ctx, p, pm.ClaimInput{}); !errors.Is(err, pm.ErrForbidden) {
		t.Fatalf("unselected agent claimed: %v", err)
	}
	p.ActorID = selected.ActorID
	if _, err = rt.Service.ClaimTurn(ctx, p, pm.ClaimInput{}); !errors.Is(err, pm.ErrEmpty) {
		t.Fatalf("selected actor denied: %v", err)
	}
	if _, err = env.workspace.DB().ExecContext(ctx, "UPDATE agents SET revoked_at=? WHERE id=?", time.Now().UTC().Format(time.RFC3339Nano), selected.AgentID); err != nil {
		t.Fatal(err)
	}
	if _, err = rt.Service.ClaimTurn(ctx, p, pm.ClaimInput{}); !errors.Is(err, pm.ErrForbidden) {
		t.Fatalf("revoked PM claimed: %v", err)
	}
}

func TestPMMaintenanceTickExpiresWithoutRunnerReadOrSender(t *testing.T) {
	env := newPMStoreTestEnv(t)
	ctx := context.Background()
	rt, err := NewPMRuntime(env.workspace.DB(), env.primitiveStore.(*primitives.Store), env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}})
	if err != nil {
		t.Fatal(err)
	}
	rt.Sender = nil
	turn := pm.Turn{ID: "expired", WorkspaceID: "ws_main", ActorID: "human", AgentActorID: "old-pm", Status: pm.Sending, Revision: 1, Deadline: time.Now().Add(-time.Minute)}
	body, err := json.Marshal(turn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = env.workspace.DB().ExecContext(ctx, `INSERT INTO pm_records(kind,id,workspace_id,actor_id,parent_id,revision,body) VALUES('turn',?,?,?,'',1,?)`, turn.ID, turn.WorkspaceID, turn.ActorID, body); err != nil {
		t.Fatal(err)
	}
	if err = rt.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if err = env.workspace.DB().QueryRowContext(ctx, `SELECT body FROM pm_records WHERE kind='turn' AND id=?`, turn.ID).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(body, &turn); err != nil {
		t.Fatal(err)
	}
	if turn.Status != pm.Failed || turn.Failure != "The PM did not answer before the deadline. Retry, or check that a runner is attached." {
		t.Fatalf("not expired %+v", turn)
	}
}

func TestPMRuntimeAgentOnlyProposesForRequestingHuman(t *testing.T) {
	env := newPMStoreTestEnv(t)
	ctx := context.Background()
	human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "turn-human", "turn-human-actor", "turn-human", "turn-human-token")
	machine := seedMachinePrincipalForLockoutTest(t, ctx, env.workspace.DB(), "turn-agent", "turn-agent-actor", "turn-agent", "turn-agent-token")
	store := env.primitiveStore.(*primitives.Store)
	board, err := store.CreateBoard(ctx, human.ActorID, map[string]any{"title": "Proposals"})
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateWork(ctx, human.ActorID, asString(board["id"]), map[string]any{"title": "Task"})
	if err != nil {
		t.Fatal(err)
	}
	rt, err := NewPMRuntime(env.workspace.DB(), store, env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main", AgentActorID: machine.ActorID}})
	if err != nil {
		t.Fatal(err)
	}
	humanP := pm.Principal{WorkspaceID: "ws_main", ActorID: human.ActorID, Human: true}
	agent := pm.Principal{WorkspaceID: "ws_main", ActorID: machine.ActorID}
	input := pm.DecisionInput{RequestKey: "proposal", WorkRef: asString(work["ref"]), Scope: "work.phase", Instruction: "Move", Payload: &pm.ActionPayload{Phase: "ready"}, TargetRevision: "1"}
	for _, owner := range []pm.Principal{humanP, agent} {
		c, err := rt.Service.CreateConversation(ctx, owner, pm.CreateConversation{RequestKey: owner.ActorID, Title: "Question"})
		if err != nil {
			t.Fatal(err)
		}
		turn, err := rt.Service.PostMessage(ctx, owner, c.ID, pm.MessageInput{RequestKey: "m", Text: "Next step?"})
		if err != nil {
			t.Fatal(err)
		}
		d, err := rt.Service.ProposeForTurn(ctx, agent, turn.ID, input)
		if !owner.Human {
			if !errors.Is(err, pm.ErrForbidden) {
				t.Fatalf("agent addressed itself: %+v %v", d, err)
			}
		} else {
			if err != nil || d.ActorID != human.ActorID {
				t.Fatalf("human proposal %+v %v", d, err)
			}
			if _, err = rt.Service.AnswerDecision(ctx, humanP, d.ID, pm.AnswerInput{Revision: 1, Approve: true, Text: "yes"}); err != nil {
				t.Fatal(err)
			}
		}
	}
}
