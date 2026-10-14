package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

func TestAskBatchFastResponsePersistsCompleteReceiptAndWake(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	agent := seedNotificationTestAgent(t, env, "receipt.agent")
	ctx := context.Background()
	human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "receipt-human", "receipt-human-actor", "receipt-human", "receipt-human-token")
	thread := integrationSeedThreadWithStore(t, env.primitiveStore, nil, agent.ActorID, map[string]any{"title": "Receipt batch", "type": "incident", "status": "active", "priority": "p2", "tags": []any{}, "cadence": "daily", "current_summary": "summary", "next_actions": []any{}, "key_artifacts": []any{}, "provenance": map[string]any{"sources": []any{"inferred"}}})
	asks := []map[string]any{}
	for _, title := range []string{"First decision", "Second decision"} {
		asks = append(asks, map[string]any{"type": "human_attention_requested", "thread_id": thread, "refs": []string{"thread:" + thread}, "summary": title, "payload": map[string]any{"kind": "ask", "title": title, "subject_ref": "thread:" + thread, "requester_actor_id": agent.ActorID, "requester_label": agent.Username, "response_proposals": []any{"Yes", "No"}, "delivery": "batch"}, "provenance": map[string]any{"sources": []string{"inferred"}}})
	}
	status, filed := hostHTTP(t, http.MethodPost, env.server.URL+"/asks/batch", agent.AccessToken, map[string]any{"asks": asks})
	if status != http.StatusCreated {
		t.Fatalf("file: %d %v", status, filed)
	}
	if err := refreshDerivedTopicProjection(ctx, handlerOptions{primitiveStore: env.primitiveStore}, thread, time.Now().UTC(), agent.ActorID); err != nil {
		t.Fatal(err)
	}
	items, err := env.primitiveStore.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{ThreadID: thread})
	if err != nil || len(items) != 1 {
		t.Fatalf("batch item: %v %v", items, err)
	}
	answers := []map[string]any{}
	for _, section := range batchSectionsFromPayload(items[0].Data["batch"].(map[string]any)["asks"]) {
		answers = append(answers, map[string]any{"ask_ref": section["request_event_ref"], "response_text": "Yes", "outcome": "answered"})
	}
	request := map[string]any{"idempotency_key": "atomic-batch-receipt", "answers": answers}
	endpoint := env.server.URL + "/inbox/" + url.PathEscape(items[0].ID) + "/respond"
	status, first := hostHTTP(t, http.MethodPost, endpoint, human.AccessToken, request)
	if status != http.StatusCreated {
		t.Fatalf("answer: %d %v", status, first)
	}
	var raw string
	if err := env.workspace.DB().QueryRow(`SELECT response_json FROM human_attention_batch_response_claims WHERE batch_id=?`, filed["batch_id"]).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var durable map[string]any
	if err := json.Unmarshal([]byte(raw), &durable); err != nil || !reflect.DeepEqual(first, durable) {
		t.Fatalf("HTTP response differs from atomic receipt: response=%v durable=%v err=%v", first, durable, err)
	}
	if _, err := env.primitiveStore.GetDerivedInboxItem(ctx, items[0].ID); err != primitives.ErrNotFound {
		t.Fatalf("resolved batch persisted until maintenance: %v", err)
	}
	// A stale batch candidate must also be rejected without a generation guard.
	if err := env.primitiveStore.ReplaceDerivedInboxItems(ctx, thread, items); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"derived_inbox_items", "scope_inbox_live"} {
		var count int
		if err := env.workspace.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE id=?", items[0].ID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("resolved batch reopened in %s: count=%d err=%v", table, count, err)
		}
	}
	// No projection refresh or synchronous wake delivery occurs between answer
	// and retry. Reopening the store must still expose the durable wake work.
	reopened := primitives.NewTestStore(env.workspace.DB(), env.workspace.Layout().ArtifactContentDir)
	batches, err := reopened.ListHumanAttentionAnswerWakeBatches(ctx)
	if err != nil || len(batches) != 1 || batches[0].AnswerCount != 2 {
		t.Fatalf("durable wake batch: %v %v", batches, err)
	}
	status, replay := hostHTTP(t, http.MethodPost, endpoint, human.AccessToken, request)
	if status != http.StatusCreated || !reflect.DeepEqual(first, replay) {
		t.Fatalf("replay differs: %d first=%v replay=%v", status, first, replay)
	}
	batches, err = reopened.ListHumanAttentionAnswerWakeBatches(ctx)
	if err != nil || len(batches) != 1 || batches[0].AnswerCount != 2 {
		t.Fatalf("replay duplicated wake members: %v %v", batches, err)
	}
}
