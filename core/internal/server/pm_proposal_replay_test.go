package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

func TestPMProposalOriginalReceiptUsesScopedRealCoreAndCurrentAuthority(t *testing.T) {
	requireIntegrationTest(t)
	env := newPMStoreTestEnv(t)
	ctx := context.Background()
	db := env.workspace.DB()
	human := seedHumanPrincipalForLockoutTest(t, ctx, db, "proposal-reader", "proposal-reader-actor", "proposal-reader", "proposal-reader-token")
	agent := seedMachinePrincipalForLockoutTest(t, ctx, db, "proposal-pm", "proposal-pm-actor", "proposal-pm", "proposal-pm-token")
	store := env.primitiveStore.(*primitives.Store)
	work, err := store.CreateWork(ctx, human.ActorID, "", map[string]any{"title": "Original PM request"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PatchThread(ctx, human.ActorID, anyString(work["thread_id"]), map[string]any{"pm_actor_id": human.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	event, err := store.AppendEvent(ctx, human.ActorID, map[string]any{"type": "message_posted", "thread_id": work["thread_id"], "refs": []string{"thread:" + anyString(work["thread_id"])}, "summary": "Original proof", "payload": map[string]any{"text": "Accepted completion evidence"}})
	if err != nil {
		t.Fatal(err)
	}
	rt, err := newOnboardedPMRuntime(t, db, store, env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main", AgentActorID: agent.ActorID}})
	if err != nil {
		t.Fatal(err)
	}
	contract, err := schema.Load("../../../contracts/anx-schema.yaml")
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler("", WithSchemaContract(contract), WithAuthStore(env.authStore), WithActorRegistry(env.registry), WithPrimitiveStore(store), WithPMRuntime(rt))
	db.SetMaxOpenConns(1)
	body := pm.DecisionInput{ActorID: human.ActorID, RequestKey: "complete", ReplayKey: "original-completion", WorkRef: anyString(work["ref"]), Scope: "work.phase", Instruction: "Record completion", TargetRevision: anyString(work["decision_revision"]), Payload: &pm.ActionPayload{Phase: "done", ResolutionRefs: []string{anyString(event["ref"])}}}
	send := func(body pm.DecisionInput) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/pm/decisions", strings.NewReader(string(raw)))
		req.Header.Set("Authorization", "Bearer "+human.AccessToken)
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		return out
	}
	if _, err := db.Exec(`CREATE TRIGGER reject_real_proposal BEFORE INSERT ON idempotency_replays WHEN NEW.scope LIKE 'pm.decisions.create:%' BEGIN SELECT RAISE(ABORT,'receipt failed'); END`); err != nil {
		t.Fatal(err)
	}
	if out := send(body); out.Code < 400 {
		t.Fatal("failed receipt confirmed", out.Code, out.Body.String())
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM pm_records WHERE kind='decision'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial decision", count, err)
	}
	if _, err := db.Exec(`DROP TRIGGER reject_real_proposal`); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 8)
	for i := 0; i < 8; i++ {
		go func() { <-start; results <- send(body) }()
	}
	close(start)
	first := <-results
	if first.Code != 201 {
		t.Fatal(first.Code, first.Body.String())
	}
	for i := 1; i < 8; i++ {
		if next := <-results; next.Code != first.Code || next.Body.String() != first.Body.String() {
			t.Fatal("real-core receipt changed", next.Code, next.Body.String())
		}
	}
	if !strings.Contains(first.Body.String(), "Original proof") {
		t.Fatal("original scoped evidence missing", first.Body.String())
	}
	if err := db.QueryRow(`SELECT count(*) FROM pm_records WHERE kind='decision'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate decision", count, err)
	}
	// The live work projection can advance without changing an existing receipt.
	var version int64
	if err := db.QueryRow(`SELECT version FROM work_metadata WHERE card_id=?`, work["id"]).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PatchWork(ctx, human.ActorID, anyString(work["ref"]), version, map[string]any{"next_action": "Later annotation"}); err != nil {
		t.Fatal(err)
	}
	if next := send(body); next.Code != 201 || next.Body.String() != first.Body.String() {
		t.Fatal("newer work changed receipt", next.Code, next.Body.String())
	}
	changed := body
	changed.Payload = &pm.ActionPayload{Phase: "review"}
	if out := send(changed); out.Code != 409 {
		t.Fatal("changed payload accepted", out.Code, out.Body.String())
	}
	// Revoking the resource must hide even the old private receipt/evidence.
	if _, err := store.PatchThread(ctx, human.ActorID, anyString(work["thread_id"]), map[string]any{"pm_actor_id": "replacement"}, nil); err != nil {
		t.Fatal(err)
	}
	if out := send(body); out.Code != 403 && out.Code != 404 {
		t.Fatal("revoked resource receipt leaked", out.Code, out.Body.String())
	}
}
