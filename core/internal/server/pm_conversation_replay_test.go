package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

func TestPMConversationOriginalReceiptAtomicRecovery(t *testing.T) {
	requireIntegrationTest(t)
	env := newPMStoreTestEnv(t)
	ctx := context.Background()
	db := env.workspace.DB()
	human := seedHumanPrincipalForLockoutTest(t, ctx, db, "conversation-human", "conversation-human-actor", "conversation-human", "conversation-human-token")
	agent := seedMachinePrincipalForLockoutTest(t, ctx, db, "conversation-pm", "conversation-pm-actor", "conversation-pm", "conversation-pm-token")
	store := env.primitiveStore.(*primitives.Store)
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
	call := func(path string, body any, token string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", path, strings.NewReader(string(raw)))
		req.Header.Set("Authorization", "Bearer "+token)
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		return out
	}
	in := pm.CreateConversation{ActorID: human.ActorID, RequestKey: "conversation", ReplayKey: "conversation-receipt", Title: "PRIVATE QUESTION"}
	results := make(chan *httptest.ResponseRecorder, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- call("/pm/conversations", in, human.AccessToken) }()
	}
	wg.Wait()
	close(results)
	var original string
	var c pm.Conversation
	for out := range results {
		if out.Code != 201 {
			t.Fatal(out.Code, out.Body.String())
		}
		if original == "" {
			original = out.Body.String()
			if err = json.Unmarshal(out.Body.Bytes(), &c); err != nil {
				t.Fatal(err)
			}
		} else if original != out.Body.String() {
			t.Fatal("receipt changed", original, out.Body.String())
		}
	}
	for _, query := range []string{"SELECT count(*) FROM pm_records WHERE kind='conversation'", "SELECT count(*) FROM threads WHERE id='" + c.ThreadID + "'", "SELECT count(*) FROM idempotency_replays WHERE scope='pm.conversations.create:ws_main'"} {
		var count int
		if err = db.QueryRow(query).Scan(&count); err != nil || count != 1 {
			t.Fatal(query, count, err)
		}
	}
	for _, change := range []func(*pm.CreateConversation){
		func(in *pm.CreateConversation) { in.Title = "changed" },
		func(in *pm.CreateConversation) { in.RequestKey = "different" },
		func(in *pm.CreateConversation) { in.ActorID = "" },
	} {
		changed := in
		change(&changed)
		if out := call("/pm/conversations", changed, human.AccessToken); out.Code != 409 {
			t.Fatal("changed body accepted", out.Code, out.Body.String())
		}
	}
	// A second key cannot claim canonical state created by the first receipt.
	changed := in
	changed.ReplayKey = "other-receipt"
	if out := call("/pm/conversations", changed, human.AccessToken); out.Code != 409 {
		t.Fatal("canonical ID adopted", out.Code, out.Body.String())
	}
	if _, err = db.Exec(`UPDATE pm_records SET body=json_set(body,'$.title','Later title') WHERE kind='conversation' AND id=?`, c.ID); err != nil {
		t.Fatal(err)
	}
	if out := call("/pm/conversations", in, human.AccessToken); out.Code != 201 || out.Body.String() != original {
		t.Fatal("original receipt advanced", out.Code, out.Body.String())
	}
	// Force failures at every canonical boundary. The thread must not survive.
	for i, trigger := range []string{
		`CREATE TRIGGER fail_conversation BEFORE INSERT ON threads BEGIN SELECT RAISE(ABORT,'thread failure'); END`,
		`CREATE TRIGGER fail_conversation BEFORE INSERT ON pm_records WHEN NEW.kind='conversation' BEGIN SELECT RAISE(ABORT,'conversation failure'); END`,
		`CREATE TRIGGER fail_conversation BEFORE INSERT ON idempotency_replays WHEN NEW.scope='pm.conversations.create:ws_main' BEGIN SELECT RAISE(ABORT,'receipt failure'); END`,
	} {
		if _, err = db.Exec(trigger); err != nil {
			t.Fatal(err)
		}
		failed := in
		failed.RequestKey, failed.ReplayKey = fmt.Sprintf("failed-%d", i), fmt.Sprintf("failed-receipt-%d", i)
		out := call("/pm/conversations", failed, human.AccessToken)
		if out.Code < 400 {
			t.Fatal("failure accepted", out.Code, out.Body.String())
		}
		if _, err = db.Exec(`DROP TRIGGER fail_conversation`); err != nil {
			t.Fatal(err)
		}
		var count int
		if err = db.QueryRow(`SELECT count(*) FROM threads WHERE json_extract(body_json,'$.pm_actor_id')=?`, human.ActorID).Scan(&count); err != nil || count != 1 {
			t.Fatal("thread escaped rollback", count, err)
		}
		if err = db.QueryRow(`SELECT count(*) FROM pm_records WHERE kind='conversation'`).Scan(&count); err != nil || count != 1 {
			t.Fatal("conversation escaped rollback", count, err)
		}
		if out = call("/pm/conversations", failed, human.AccessToken); out.Code != 201 {
			t.Fatal("retry failed", out.Code, out.Body.String())
		}
		// Remove successful probes so the next rollback check starts at one.
		var probe pm.Conversation
		_ = json.Unmarshal(out.Body.Bytes(), &probe)
		for _, q := range []string{`DELETE FROM pm_records WHERE id=?`, `DELETE FROM threads WHERE id=?`, `DELETE FROM idempotency_replays WHERE request_key=?`} {
			id := probe.ID
			if strings.Contains(q, "threads") {
				id = probe.ThreadID
			}
			if strings.Contains(q, "idempotency") {
				id = failed.ReplayKey
			}
			if _, err = db.Exec(q, id); err != nil {
				t.Fatal(err)
			}
		}
	}
	if out := call("/pm/disconnect", map[string]any{}, agent.AccessToken); out.Code != 200 {
		t.Fatal("disconnect", out.Code, out.Body.String())
	}
	if out := call("/pm/conversations", in, human.AccessToken); out.Code != 201 || out.Body.String() != original {
		t.Fatal("disconnected recovery", out.Code, out.Body.String())
	}
	changed = in
	changed.RequestKey, changed.ReplayKey = "fresh-after-disconnect", "fresh-receipt"
	if out := call("/pm/conversations", changed, human.AccessToken); out.Code != 409 || !strings.Contains(out.Body.String(), "pm_not_onboarded") {
		t.Fatal("fresh write bypassed gate", out.Code, out.Body.String())
	}
	if _, err = store.PatchThread(ctx, human.ActorID, c.ThreadID, map[string]any{"pm_actor_id": "replacement-owner"}, nil); err != nil {
		t.Fatal(err)
	}
	if out := call("/pm/conversations", in, human.AccessToken); (out.Code != 403 && out.Code != 404 && !(out.Code == 409 && strings.Contains(out.Body.String(), "pm_not_onboarded"))) || strings.Contains(out.Body.String(), "PRIVATE") {
		t.Fatal("revoked receipt leaked", out.Code, out.Body.String())
	}
}
