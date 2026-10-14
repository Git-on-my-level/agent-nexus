package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

// Acceptance for oss#394: a batch of three asks appears as one inbox item,
// and answering it records all three answers atomically.
func TestAskBatchFilesAtomicallyAndAnswersAsOneItem(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	agent := seedNotificationTestAgent(t, env, "ask.agent")
	ctx := context.Background()
	human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "batch-human", "batch-human-actor", "batch-human", "batch-human-token")

	threadID := integrationSeedThreadWithStore(t, env.primitiveStore, nil, agent.ActorID, map[string]any{
		"title": "Batch thread", "type": "incident", "status": "active", "priority": "p2",
		"tags": []any{"asks"}, "cadence": "daily", "next_check_in_at": "2026-03-06T00:00:00Z",
		"current_summary": "summary", "next_actions": []any{"check"}, "key_artifacts": []any{},
		"provenance": map[string]any{"sources": []any{"inferred"}},
	})

	asks := []map[string]any{}
	wantRefs := map[string]bool{}
	for i := 1; i <= 3; i++ {
		title := fmt.Sprintf("Batch decision %d: ship now?", i)
		asks = append(asks, map[string]any{
			"type":      "human_attention_requested",
			"thread_id": threadID,
			"summary":   title,
			"refs":      []string{"thread:" + threadID},
			"payload": map[string]any{
				"kind": "ask", "title": title,
				"subject_ref":        "thread:" + threadID,
				"requester_actor_id": agent.ActorID,
				"requester_label":    agent.Username,
				"response_proposals": []any{"Accept", "Wait"},
				"delivery":           "batch",
			},
			"provenance": map[string]any{"sources": []string{"event:cli.ask"}},
		})
	}

	status, result := hostHTTP(t, http.MethodPost, env.server.URL+"/asks/batch", agent.AccessToken, map[string]any{
		"request_key": "batch-file-1",
		"asks":        asks,
	})
	if status != http.StatusCreated {
		t.Fatalf("file ask batch: %d %#v", status, result)
	}
	batchID, _ := result["batch_id"].(string)
	if batchID == "" {
		t.Fatalf("batch filing returned no batch id: %#v", result)
	}
	events, _ := result["events"].([]any)
	if len(events) != 3 {
		t.Fatalf("expected three stored events, got %d", len(events))
	}
	for _, raw := range events {
		event, _ := raw.(map[string]any)
		payload, _ := event["payload"].(map[string]any)
		if payload["batch_id"] != batchID {
			t.Fatalf("stored event %v lost the batch id: %#v", event["id"], payload)
		}
		wantRefs[fmt.Sprint(event["id"])] = true
	}

	// Idempotent refiling with the same key replays the original batch.
	replayStatus, replayResult := hostHTTP(t, http.MethodPost, env.server.URL+"/asks/batch", agent.AccessToken, map[string]any{
		"request_key": "batch-file-1",
		"asks":        asks,
	})
	if replayStatus != http.StatusCreated {
		t.Fatalf("batch replay: %d %#v", replayStatus, replayResult)
	}
	if replayResult["batch_id"] != batchID {
		t.Fatalf("batch replay returned a different batch: %#v vs %s", replayResult["batch_id"], batchID)
	}

	if err := refreshDerivedTopicProjection(ctx, handlerOptions{primitiveStore: env.primitiveStore}, threadID, time.Now().UTC(), agent.ActorID); err != nil {
		t.Fatalf("refresh projection: %v", err)
	}
	items, err := env.primitiveStore.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{ThreadID: threadID})
	if err != nil {
		t.Fatalf("list inbox items: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one batch inbox item, got %d", len(items))
	}
	batchItem := items[0]
	payload := payloadFromDerivedInboxItem(batchItem)
	batch, _ := payload["batch"].(map[string]any)
	if batch == nil {
		t.Fatalf("derived item is not a batch item: %#v", payload)
	}
	sections, _ := batch["asks"].([]any)
	if batch["id"] != batchID || len(sections) != 3 {
		t.Fatalf("batch item lost members: %#v", batch)
	}

	// The agent still sees its three asks individually.
	listResp := getJSONExpectStatusWithAuth(t, env.server.URL+"/agent-inbox/asks?limit=200", agent.AccessToken, http.StatusOK)
	var page struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&page); err != nil {
		t.Fatalf("decode agent inbox: %v", err)
	}
	listResp.Body.Close()
	if len(page.Items) != 3 {
		t.Fatalf("expected three agent asks, got %d", len(page.Items))
	}

	// Answering with a partial answer set must be rejected without mutating.
	partial := []map[string]any{}
	for _, raw := range sections {
		section, _ := raw.(map[string]any)
		partial = append(partial, map[string]any{
			"ask_ref":       section["request_event_ref"],
			"response_text": "partial",
			"outcome":       "answered",
		})
		break
	}
	partialStatus, _ := hostHTTP(t, http.MethodPost, env.server.URL+"/inbox/"+url.PathEscape(batchItem.ID)+"/respond", human.AccessToken, map[string]any{"answers": partial})
	if partialStatus != http.StatusBadRequest {
		t.Fatalf("partial batch answer must be rejected, got %d", partialStatus)
	}

	answers := []map[string]any{}
	for i, raw := range sections {
		section, _ := raw.(map[string]any)
		answers = append(answers, map[string]any{
			"ask_ref":       section["request_event_ref"],
			"response_text": fmt.Sprintf("Answer %d", i+1),
			"outcome":       "answered",
		})
	}
	respondStatus, respondResult := hostHTTP(t, http.MethodPost, env.server.URL+"/inbox/"+url.PathEscape(batchItem.ID)+"/respond", human.AccessToken, map[string]any{
		"idempotency_key": "batch-answer-1",
		"answers":         answers,
	})
	if respondStatus != http.StatusCreated {
		t.Fatalf("batch respond: %d %#v", respondStatus, respondResult)
	}
	answered, _ := respondResult["answers"].([]any)
	if len(answered) != 3 {
		t.Fatalf("expected three recorded answers, got %#v", respondResult)
	}
	notify, _ := respondResult["notify"].(map[string]any)
	if notify["requested"] != true {
		t.Fatalf("batch with delivery=batch members must queue a wake: %#v", notify)
	}

	if err := refreshDerivedTopicProjection(ctx, handlerOptions{primitiveStore: env.primitiveStore}, threadID, time.Now().UTC(), agent.ActorID); err != nil {
		t.Fatalf("refresh projection after answer: %v", err)
	}
	items, err = env.primitiveStore.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{ThreadID: threadID})
	if err != nil {
		t.Fatalf("list inbox items after answer: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("answered batch item must leave the open projection, got %d", len(items))
	}

	// A retried submit with the same key replays the original combined result.
	retryStatus, retryResult := hostHTTP(t, http.MethodPost, env.server.URL+"/inbox/"+url.PathEscape(batchItem.ID)+"/respond", human.AccessToken, map[string]any{
		"idempotency_key": "batch-answer-1",
		"answers":         answers,
	})
	if retryStatus != http.StatusCreated {
		t.Fatalf("batch respond replay: %d %#v", retryStatus, retryResult)
	}
	if retryResult["batch_id"] != batchID {
		t.Fatalf("batch replay changed the batch id: %#v", retryResult)
	}

	// Every member is answered and every ask stays visible with its answer.
	listResp = getJSONExpectStatusWithAuth(t, env.server.URL+"/agent-inbox/asks?limit=200", agent.AccessToken, http.StatusOK)
	if err := json.NewDecoder(listResp.Body).Decode(&page); err != nil {
		t.Fatalf("decode agent inbox: %v", err)
	}
	listResp.Body.Close()
	answeredCount := 0
	for _, item := range page.Items {
		if item["answer"] == nil {
			continue
		}
		answeredCount++
		if item["status"] != "answered" {
			t.Fatalf("ask %v lost its answer: %#v", item["ask_id"], item)
		}
		askID := trimEventPrefix(fmt.Sprint(item["ask_id"]))
		if !wantRefs[askID] {
			t.Fatalf("unexpected answered ask: %s", askID)
		}
	}
	if answeredCount != 3 {
		t.Fatalf("expected three answered asks, got %d: %#v", answeredCount, page.Items)
	}
}

// delivery=none asks must produce no agent wake server-side, while the answer
// itself stays recorded and visible. Batch mode keeps queueing the wake.
func TestAskRespondSuppressesWakeForDeliveryNone(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	agent := seedNotificationTestAgent(t, env, "ask.agent")
	ctx := context.Background()
	human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "none-human", "none-human-actor", "none-human", "none-human-token")
	store := env.primitiveStore.(*primitives.Store)

	threadID := integrationSeedThreadWithStore(t, env.primitiveStore, nil, agent.ActorID, map[string]any{
		"title": "Delivery none thread", "type": "incident", "status": "active", "priority": "p2",
		"tags": []any{"asks"}, "cadence": "daily", "next_check_in_at": "2026-03-06T00:00:00Z",
		"current_summary": "summary", "next_actions": []any{"check"}, "key_artifacts": []any{},
		"provenance": map[string]any{"sources": []any{"inferred"}},
	})

	fileAsk := func(delivery string) string {
		title := "FYI ask (" + delivery + ")"
		resp := postJSONExpectStatusWithAuth(t, env.server.URL+"/events", map[string]any{
			"event": map[string]any{
				"type":      "human_attention_requested",
				"thread_id": threadID,
				"summary":   title,
				"refs":      []string{"thread:" + threadID},
				"payload": map[string]any{
					"kind": "ask", "title": title,
					"subject_ref":        "thread:" + threadID,
					"requester_actor_id": agent.ActorID,
					"requester_label":    agent.Username,
					"response_proposals": []any{"Noted"},
					"delivery":           delivery,
				},
				"provenance": map[string]any{"sources": []string{"event:cli.ask"}},
			},
		}, agent.AccessToken, http.StatusCreated)
		var created struct {
			Event map[string]any `json:"event"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
			t.Fatalf("decode ask: %v", err)
		}
		resp.Body.Close()
		return fmt.Sprint(created.Event["id"])
	}

	noneAskID := fileAsk("none")
	batchAskID := fileAsk("batch")

	if err := refreshDerivedTopicProjection(ctx, handlerOptions{primitiveStore: env.primitiveStore}, threadID, time.Now().UTC(), agent.ActorID); err != nil {
		t.Fatalf("refresh projection: %v", err)
	}
	items, err := env.primitiveStore.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{ThreadID: threadID})
	if err != nil {
		t.Fatalf("list inbox items: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected two open asks, got %d", len(items))
	}
	itemBySource := map[string]primitives.DerivedInboxItem{}
	for _, item := range items {
		itemBySource[item.SourceEventID] = item
	}

	status, result := hostHTTP(t, http.MethodPost, env.server.URL+"/inbox/"+url.PathEscape(itemBySource[noneAskID].ID)+"/respond", human.AccessToken, map[string]any{
		"response_text": "Recorded for the record", "outcome": "answered", "notify_mode": "original",
	})
	if status != http.StatusCreated {
		t.Fatalf("answer none ask: %d %#v", status, result)
	}
	notify, _ := result["notify"].(map[string]any)
	if notify["requested"] != false || notify["queued"] != false {
		t.Fatalf("delivery=none ask must not queue a wake: %#v", notify)
	}
	if batches, err := store.ListHumanAttentionAnswerWakeBatches(ctx); err != nil || len(batches) != 0 {
		t.Fatalf("delivery=none answer must not create a wake batch: %v %#v", err, batches)
	}

	// The answer stays recorded and visible through the agent inbox.
	listResp := getJSONExpectStatusWithAuth(t, env.server.URL+"/agent-inbox/asks?limit=200", agent.AccessToken, http.StatusOK)
	var page struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&page); err != nil {
		t.Fatalf("decode agent inbox: %v", err)
	}
	listResp.Body.Close()
	answered := 0
	for _, item := range page.Items {
		if item["answer"] == nil {
			continue
		}
		answered++
		if trimEventPrefix(fmt.Sprint(item["ask_id"])) != noneAskID {
			t.Fatalf("unexpected answered ask: %#v", item)
		}
	}
	if answered != 1 {
		t.Fatalf("answered none ask must stay visible: %#v", page.Items)
	}

	status, result = hostHTTP(t, http.MethodPost, env.server.URL+"/inbox/"+url.PathEscape(itemBySource[batchAskID].ID)+"/respond", human.AccessToken, map[string]any{
		"response_text": "Wake me", "outcome": "answered",
	})
	if status != http.StatusCreated {
		t.Fatalf("answer batch ask: %d %#v", status, result)
	}
	// The flush fires because no open asks remain, so the debounced batch is
	// already dispatched into the agent's durable wakeup.
	wakeups, err := store.ListAgentWakeups(ctx, primitives.AgentWakeupListFilter{TargetActorID: agent.ActorID})
	if err != nil {
		t.Fatalf("list agent wakeups: %v", err)
	}
	if len(wakeups) != 1 {
		t.Fatalf("delivery=batch answer must deliver one wake, got %#v", wakeups)
	}
	refs := strings.Join(wakeups[0].Refs, " ")
	if !strings.Contains(refs, "event:"+batchAskID) {
		t.Fatalf("wake must reference its ask: %#v", wakeups[0])
	}
	if strings.Contains(refs, "event:"+noneAskID) {
		t.Fatalf("delivery=none ask must not ride the wake: %#v", wakeups[0])
	}
}

// A batch mixing delivery modes answers atomically while only non-none
// members queue wakes; filing validation rejects cross-thread batches,
// oversized batches and client-supplied batch ids.
func TestAskBatchValidationAndMixedDeliveryWakes(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	agent := seedNotificationTestAgent(t, env, "ask.agent")
	ctx := context.Background()
	human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "mixed-human", "mixed-human-actor", "mixed-human", "mixed-human-token")
	store := env.primitiveStore.(*primitives.Store)

	threadID := integrationSeedThreadWithStore(t, env.primitiveStore, nil, agent.ActorID, map[string]any{
		"title": "Mixed thread", "type": "incident", "status": "active", "priority": "p2",
		"tags": []any{"asks"}, "cadence": "daily", "next_check_in_at": "2026-03-06T00:00:00Z",
		"current_summary": "summary", "next_actions": []any{"check"}, "key_artifacts": []any{},
		"provenance": map[string]any{"sources": []any{"inferred"}},
	})
	otherThread := integrationSeedThreadWithStore(t, env.primitiveStore, nil, agent.ActorID, map[string]any{
		"title": "Other thread", "type": "incident", "status": "active", "priority": "p2",
		"tags": []any{"asks"}, "cadence": "daily", "next_check_in_at": "2026-03-06T00:00:00Z",
		"current_summary": "summary", "next_actions": []any{"check"}, "key_artifacts": []any{},
		"provenance": map[string]any{"sources": []any{"inferred"}},
	})

	batchAsk := func(title, thread, delivery string) map[string]any {
		return map[string]any{
			"type":      "human_attention_requested",
			"thread_id": thread,
			"summary":   title,
			"refs":      []string{"thread:" + thread},
			"payload": map[string]any{
				"kind": "ask", "title": title,
				"subject_ref":        "thread:" + thread,
				"requester_actor_id": agent.ActorID,
				"requester_label":    agent.Username,
				"response_proposals": []any{"Yes", "No"},
				"delivery":           delivery,
			},
			"provenance": map[string]any{"sources": []string{"event:cli.ask"}},
		}
	}

	// Cross-thread batch is rejected.
	status, _ := hostHTTP(t, http.MethodPost, env.server.URL+"/asks/batch", agent.AccessToken, map[string]any{
		"asks": []map[string]any{batchAsk("One", threadID, "batch"), batchAsk("Two", otherThread, "batch")},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("cross-thread batch must be rejected, got %d", status)
	}
	// One ask cannot form a batch.
	status, _ = hostHTTP(t, http.MethodPost, env.server.URL+"/asks/batch", agent.AccessToken, map[string]any{
		"asks": []map[string]any{batchAsk("Solo", threadID, "batch")},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("single-ask batch must be rejected, got %d", status)
	}
	// Over the cap is rejected.
	oversized := []map[string]any{}
	for i := 0; i < primitives.MaxAskBatchMembers+1; i++ {
		oversized = append(oversized, batchAsk(fmt.Sprintf("Ask %d", i), threadID, "batch"))
	}
	status, _ = hostHTTP(t, http.MethodPost, env.server.URL+"/asks/batch", agent.AccessToken, map[string]any{"asks": oversized})
	if status != http.StatusBadRequest {
		t.Fatalf("oversized batch must be rejected, got %d", status)
	}
	// Client-supplied batch ids are rejected.
	withClientBatch := batchAsk("Client batch", threadID, "batch")
	withClientBatch["payload"].(map[string]any)["batch_id"] = "askbatch_client"
	status, _ = hostHTTP(t, http.MethodPost, env.server.URL+"/asks/batch", agent.AccessToken, map[string]any{
		"asks": []map[string]any{withClientBatch, batchAsk("Client batch two", threadID, "batch")},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("client-supplied batch_id must be rejected, got %d", status)
	}

	// Mixed delivery: one none, one batch. Atomic answer, one wake entry only.
	mixed := []map[string]any{batchAsk("None member", threadID, "none"), batchAsk("Batch member", threadID, "batch")}
	status, result := hostHTTP(t, http.MethodPost, env.server.URL+"/asks/batch", agent.AccessToken, map[string]any{"asks": mixed})
	if status != http.StatusCreated {
		t.Fatalf("mixed batch filing: %d %#v", status, result)
	}
	batchID, _ := result["batch_id"].(string)

	if err := refreshDerivedTopicProjection(ctx, handlerOptions{primitiveStore: env.primitiveStore}, threadID, time.Now().UTC(), agent.ActorID); err != nil {
		t.Fatalf("refresh projection: %v", err)
	}
	items, err := env.primitiveStore.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{ThreadID: threadID})
	if err != nil {
		t.Fatalf("list inbox items: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one mixed batch item, got %d", len(items))
	}
	sections, _ := items[0].Data["batch"].(map[string]any)["asks"].([]any)
	if len(sections) != 2 {
		t.Fatalf("expected two sections, got %#v", items[0].Data["batch"])
	}
	answers := []map[string]any{}
	noneEventRef := ""
	for _, raw := range sections {
		section, _ := raw.(map[string]any)
		if section["delivery"] == "none" {
			noneEventRef, _ = section["request_event_ref"].(string)
		}
		answers = append(answers, map[string]any{
			"ask_ref":       section["request_event_ref"],
			"response_text": "Mixed answer",
			"outcome":       "answered",
		})
	}
	status, result = hostHTTP(t, http.MethodPost, env.server.URL+"/inbox/"+url.PathEscape(items[0].ID)+"/respond", human.AccessToken, map[string]any{"answers": answers})
	if status != http.StatusCreated {
		t.Fatalf("mixed batch respond: %d %#v", status, result)
	}
	// Only the non-none member may ride the wake. The flush dispatches it
	// because the batch left no open asks behind.
	wakeups, err := store.ListAgentWakeups(ctx, primitives.AgentWakeupListFilter{TargetActorID: agent.ActorID})
	if err != nil {
		t.Fatalf("list agent wakeups: %v", err)
	}
	if len(wakeups) != 1 {
		t.Fatalf("expected one dispatched wake, got %#v", wakeups)
	}
	refs := strings.Join(wakeups[0].Refs, " ")
	if strings.Contains(refs, "event:") && strings.Contains(refs, noneEventRef) {
		t.Fatalf("delivery=none member must not ride the batch wake: %#v", wakeups[0])
	}
	_ = batchID
}

// Batch membership is assigned by the dedicated handler; a single ask must
// not be able to join or forge someone else's decision item through
// POST /events.
func TestEventsCreateRejectsClientBatchIDOnAsks(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	agent := seedNotificationTestAgent(t, env, "ask.agent")

	threadID := integrationSeedThreadWithStore(t, env.primitiveStore, nil, agent.ActorID, map[string]any{
		"title": "Forged batch thread", "type": "incident", "status": "active", "priority": "p2",
		"tags": []any{"asks"}, "cadence": "daily", "next_check_in_at": "2026-03-06T00:00:00Z",
		"current_summary": "summary", "next_actions": []any{"check"}, "key_artifacts": []any{},
		"provenance": map[string]any{"sources": []any{"inferred"}},
	})
	status, _ := hostHTTP(t, http.MethodPost, env.server.URL+"/events", agent.AccessToken, map[string]any{
		"event": map[string]any{
			"type":      "human_attention_requested",
			"thread_id": threadID,
			"summary":   "Forged member",
			"refs":      []string{"thread:" + threadID},
			"payload": map[string]any{
				"kind": "ask", "title": "Forged member",
				"subject_ref":        "thread:" + threadID,
				"requester_actor_id": agent.ActorID,
				"response_proposals": []any{"Yes"},
				"batch_id":           "askbatch_forged",
			},
			"provenance": map[string]any{"sources": []string{"event:cli.ask"}},
		},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("client-supplied batch_id on a single ask must be rejected, got %d", status)
	}
}

// Withdrawing one member shrinks the batch item's sections; the remaining
// members still answer atomically afterwards.
func TestAskBatchShrinksWhenMemberWithdrawn(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	agent := seedNotificationTestAgent(t, env, "ask.agent")
	ctx := context.Background()
	human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "shrink-human", "shrink-human-actor", "shrink-human", "shrink-human-token")

	threadID := integrationSeedThreadWithStore(t, env.primitiveStore, nil, agent.ActorID, map[string]any{
		"title": "Shrink thread", "type": "incident", "status": "active", "priority": "p2",
		"tags": []any{"asks"}, "cadence": "daily", "next_check_in_at": "2026-03-06T00:00:00Z",
		"current_summary": "summary", "next_actions": []any{"check"}, "key_artifacts": []any{},
		"provenance": map[string]any{"sources": []any{"inferred"}},
	})
	member := func(title string) map[string]any {
		return map[string]any{
			"type":      "human_attention_requested",
			"thread_id": threadID,
			"summary":   title,
			"refs":      []string{"thread:" + threadID},
			"payload": map[string]any{
				"kind": "ask", "title": title,
				"subject_ref":        "thread:" + threadID,
				"requester_actor_id": agent.ActorID,
				"requester_label":    agent.Username,
				"response_proposals": []any{"Yes", "No"},
				"delivery":           "batch",
			},
			"provenance": map[string]any{"sources": []string{"event:cli.ask"}},
		}
	}
	status, filed := hostHTTP(t, http.MethodPost, env.server.URL+"/asks/batch", agent.AccessToken, map[string]any{
		"asks": []map[string]any{member("Withdrawn member"), member("Kept member A"), member("Kept member B")},
	})
	if status != http.StatusCreated {
		t.Fatalf("file batch: %d %#v", status, filed)
	}
	events, _ := filed["events"].([]any)
	if len(events) != 3 {
		t.Fatalf("expected three members, got %d", len(events))
	}
	withdrawnEvent, _ := events[0].(map[string]any)
	withdrawnID := fmt.Sprint(withdrawnEvent["id"])

	if err := refreshDerivedTopicProjection(ctx, handlerOptions{primitiveStore: env.primitiveStore}, threadID, time.Now().UTC(), agent.ActorID); err != nil {
		t.Fatalf("refresh projection: %v", err)
	}
	items, err := env.primitiveStore.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{ThreadID: threadID})
	if err != nil || len(items) != 1 {
		t.Fatalf("expected one batch item, got %d (%v)", len(items), err)
	}
	batchItem := items[0]

	// The requester withdraws the first member (same shape the CLI sends).
	status, result := hostHTTP(t, http.MethodPost, env.server.URL+"/events", agent.AccessToken, map[string]any{
		"event": map[string]any{
			"type":      "human_attention_withdrawn",
			"thread_id": threadID,
			"summary":   "Withdrawn member",
			"refs":      []string{"event:" + withdrawnID},
			"payload": map[string]any{
				"request_event_id":      withdrawnID,
				"request_event_ref":     "event:" + withdrawnID,
				"withdrawn_by_actor_id": agent.ActorID,
				"reason":                "Duplicate of the kept ask",
			},
			"provenance": map[string]any{"sources": []string{"event:cli.ask"}},
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("withdraw member: %d %#v", status, result)
	}

	if err := refreshDerivedTopicProjection(ctx, handlerOptions{primitiveStore: env.primitiveStore}, threadID, time.Now().UTC(), agent.ActorID); err != nil {
		t.Fatalf("refresh projection after withdrawal: %v", err)
	}
	items, err = env.primitiveStore.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{ThreadID: threadID})
	if err != nil || len(items) != 1 {
		t.Fatalf("batch item must survive a member withdrawal, got %d (%v)", len(items), err)
	}
	if got := items[0].ID; got != batchItem.ID {
		t.Fatalf("batch item id must stay stable across withdrawals: %s -> %s", batchItem.ID, got)
	}
	sections, _ := items[0].Data["batch"].(map[string]any)["asks"].([]any)
	if len(sections) != 2 {
		t.Fatalf("expected two remaining sections, got %#v", items[0].Data["batch"])
	}

	answers := []map[string]any{}
	for _, raw := range sections {
		section, _ := raw.(map[string]any)
		answers = append(answers, map[string]any{
			"ask_ref":       section["request_event_ref"],
			"response_text": "Still answered together",
			"outcome":       "answered",
		})
	}
	status, result = hostHTTP(t, http.MethodPost, env.server.URL+"/inbox/"+url.PathEscape(items[0].ID)+"/respond", human.AccessToken, map[string]any{"answers": answers})
	if status != http.StatusCreated {
		t.Fatalf("answer shrunken batch: %d %#v", status, result)
	}

	// A retried submit without the idempotency key fails closed.
	status, result = hostHTTP(t, http.MethodPost, env.server.URL+"/inbox/"+url.PathEscape(items[0].ID)+"/respond", human.AccessToken, map[string]any{"answers": answers})
	if status != http.StatusConflict {
		t.Fatalf("answered batch must 409 without a replay key, got %d %#v", status, result)
	}
}
