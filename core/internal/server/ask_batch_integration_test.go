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
	// The request only commits durable wake work. Maintenance dispatches it
	// once no open asks remain; delivery=none still contributes no wake entry.
	maintainer := NewAnswerWakeMaintainer(AnswerWakeMaintainerConfig{
		PrimitiveStore: store, WorkspaceID: "test-workspace", FlushWhenNoOpenAsks: true,
	})
	if err := maintainer.FlushTarget(ctx, agent.ActorID); err != nil {
		t.Fatalf("flush durable answer: %v", err)
	}
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
	// Delivery is durable follow-on work, outside the answer request. Only the
	// non-none member may ride the wake when maintenance dispatches the batch.
	before, err := store.ListAgentWakeups(ctx, primitives.AgentWakeupListFilter{TargetActorID: agent.ActorID})
	if err != nil || len(before) != 0 {
		t.Fatalf("answer dispatched a wake inline: %#v err=%v", before, err)
	}
	maintainer := NewAnswerWakeMaintainer(AnswerWakeMaintainerConfig{
		PrimitiveStore: store, WorkspaceID: "test-workspace", FlushWhenNoOpenAsks: true,
	})
	if err := maintainer.FlushTarget(ctx, agent.ActorID); err != nil {
		t.Fatalf("flush durable answer batch: %v", err)
	}
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

// An answered batch item is gone from the open projection. A no-key retry must
// still authorize against the stored answers so an inaccessible id looks absent.
func TestAskBatchAnsweredRetryAuthorizes(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	agent := seedNotificationTestAgent(t, env, "ask.agent")
	ctx := context.Background()
	human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "retry-human", "retry-human-actor", "retry-human", "retry-human-token")
	stranger := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "retry-stranger", "retry-stranger-actor", "retry-stranger", "retry-stranger-token")
	store := env.primitiveStore.(*primitives.Store)

	threadID := integrationSeedThreadWithStore(t, env.primitiveStore, nil, agent.ActorID, map[string]any{
		"title": "Private batch thread", "type": "incident", "status": "active", "priority": "p2",
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
		"asks": []map[string]any{member("Private member A"), member("Private member B")},
	})
	if status != http.StatusCreated {
		t.Fatalf("file batch: %d %#v", status, filed)
	}
	if err := refreshDerivedTopicProjection(ctx, handlerOptions{primitiveStore: env.primitiveStore}, threadID, time.Now().UTC(), agent.ActorID); err != nil {
		t.Fatalf("refresh projection: %v", err)
	}
	items, err := env.primitiveStore.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{ThreadID: threadID})
	if err != nil || len(items) != 1 {
		t.Fatalf("expected one batch item, got %d (%v)", len(items), err)
	}
	batchItem := items[0]
	if _, err := store.PatchThread(ctx, agent.ActorID, threadID, map[string]any{"pm_actor_id": human.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	sections, _ := batchItem.Data["batch"].(map[string]any)["asks"].([]any)
	answers := []map[string]any{}
	for _, raw := range sections {
		section, _ := raw.(map[string]any)
		answers = append(answers, map[string]any{
			"ask_ref":       section["request_event_ref"],
			"response_text": "Authorized answer",
			"outcome":       "answered",
		})
	}
	status, result := hostHTTP(t, http.MethodPost, env.server.URL+"/inbox/"+url.PathEscape(batchItem.ID)+"/respond", human.AccessToken, map[string]any{"answers": answers})
	if status != http.StatusCreated {
		t.Fatalf("owner batch respond: %d %#v", status, result)
	}
	status, result = hostHTTP(t, http.MethodPost, env.server.URL+"/inbox/"+url.PathEscape(batchItem.ID)+"/respond", stranger.AccessToken, map[string]any{"answers": answers})
	if status != http.StatusNotFound {
		t.Fatalf("unauthorized answered-batch retry must look absent, got %d %#v", status, result)
	}
	status, result = hostHTTP(t, http.MethodPost, env.server.URL+"/inbox/"+url.PathEscape(batchItem.ID)+"/respond", human.AccessToken, map[string]any{"answers": answers})
	if status != http.StatusConflict {
		t.Fatalf("authorized no-key retry must 409, got %d %#v", status, result)
	}
}

// Regression (review round 1, P1): an answered batch behind a private thread
// must be indistinguishable from a missing item for a principal who cannot
// see it — 404, never a 409 that discloses the answer's existence.
func TestAnsweredPrivateBatchIsAbsentToStrangers(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	agent := seedNotificationTestAgent(t, env, "ask.agent")
	ctx := context.Background()
	db := env.workspace.DB()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, db, "batch-owner", "batch-owner-actor", "batch-owner", "batch-owner-token")
	stranger := seedHumanPrincipalForLockoutTest(t, ctx, db, "batch-stranger", "batch-stranger-actor", "batch-stranger", "batch-stranger-token")
	store := env.primitiveStore.(*primitives.Store)

	board, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Private batch board"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := store.CreateBoardCard(ctx, owner.ActorID, anyString(board["id"]), primitives.AddBoardCardInput{Title: "Private batch card", ColumnKey: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	cardID := anyString(card.Card["id"])
	threadID := anyString(card.Card["thread_id"])
	if _, err := store.PatchThread(ctx, owner.ActorID, threadID, map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
		t.Fatal(err)
	}

	// Simulate the filed batch the way the store lays it down: shared batch id
	// on a private thread with the private card as every member's subject.
	for index, id := range []string{"batch-oracle-a", "batch-oracle-b"} {
		if _, err := store.AppendEvent(ctx, owner.ActorID, map[string]any{
			"id": id, "type": "human_attention_requested", "thread_id": threadID,
			"refs": []string{"thread:" + threadID, "card:" + cardID},
			"payload": map[string]any{
				"kind": "ask", "title": fmt.Sprintf("Private decision %d", index+1),
				"subject_ref":        "card:" + cardID,
				"requester_actor_id": agent.ActorID,
				"requester_label":    agent.Username,
				"response_proposals": []any{"Yes"},
				"delivery":           "batch",
				"batch_id":           "askbatch_oracle",
			},
			"provenance": map[string]any{"sources": []string{"event:cli.ask"}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := refreshDerivedTopicProjection(ctx, handlerOptions{primitiveStore: env.primitiveStore}, threadID, time.Now().UTC(), owner.ActorID); err != nil {
		t.Fatalf("refresh projection: %v", err)
	}
	items, err := env.primitiveStore.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{ThreadID: threadID})
	if err != nil || len(items) != 1 {
		t.Fatalf("expected one private batch item, got %d (%v)", len(items), err)
	}
	batchItemID := items[0].ID

	// Before the answer, a stranger already sees nothing.
	status, _ := hostHTTP(t, http.MethodPost, env.server.URL+"/inbox/"+url.PathEscape(batchItemID)+"/respond", stranger.AccessToken, map[string]any{
		"response_text": "sneak", "outcome": "answered",
	})
	if status != http.StatusNotFound {
		t.Fatalf("open private batch must 404 for a stranger, got %d", status)
	}

	// The owner answers the whole batch.
	sections, _ := items[0].Data["batch"].(map[string]any)["asks"].([]any)
	answers := []map[string]any{}
	for _, raw := range sections {
		section, _ := raw.(map[string]any)
		answers = append(answers, map[string]any{
			"ask_ref":       section["request_event_ref"],
			"response_text": "Owner decision",
			"outcome":       "answered",
		})
	}
	status, result := hostHTTP(t, http.MethodPost, env.server.URL+"/inbox/"+url.PathEscape(batchItemID)+"/respond", owner.AccessToken, map[string]any{"answers": answers})
	if status != http.StatusCreated {
		t.Fatalf("owner batch respond: %d %#v", status, result)
	}
	if err := refreshDerivedTopicProjection(ctx, handlerOptions{primitiveStore: env.primitiveStore}, threadID, time.Now().UTC(), owner.ActorID); err != nil {
		t.Fatalf("refresh after answer: %v", err)
	}

	// After the answer, the batch claim must stay invisible: a stranger's
	// response attempt reports the item missing, never "already answered".
	status, body := hostHTTP(t, http.MethodPost, env.server.URL+"/inbox/"+url.PathEscape(batchItemID)+"/respond", stranger.AccessToken, map[string]any{
		"response_text": "sneak", "outcome": "answered",
	})
	if status != http.StatusNotFound {
		t.Fatalf("answered private batch must 404 for a stranger, got %d %#v", status, body)
	}
	if code, _ := body["error"].(map[string]any); code != nil && anyString(code["code"]) == "conflict" {
		t.Fatalf("stranger must not learn the private batch was answered: %#v", body)
	}
	// The owner still replays the stored result by key.
	status, body = hostHTTP(t, http.MethodPost, env.server.URL+"/inbox/"+url.PathEscape(batchItemID)+"/respond", owner.AccessToken, map[string]any{"answers": answers})
	if status != http.StatusConflict {
		t.Fatalf("owner without the key sees the honest conflict, got %d", status)
	}
}

// Regression (review round 1, P2): a member withdrawn between the reader's
// load and the submit aborts the whole batch with the withdrawn ask named,
// leaving every other member uncommitted; the shrunk list then answers.
func TestAskBatchWithdrawnMemberAbortsAndNamesTheAsk(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	agent := seedNotificationTestAgent(t, env, "ask.agent")
	ctx := context.Background()
	human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "abort-human", "abort-human-actor", "abort-human", "abort-human-token")

	threadID := integrationSeedThreadWithStore(t, env.primitiveStore, nil, agent.ActorID, map[string]any{
		"title": "Abort thread", "type": "incident", "status": "active", "priority": "p2",
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
		"asks": []map[string]any{member("Abort withdrawn"), member("Abort kept A"), member("Abort kept B")},
	})
	if status != http.StatusCreated {
		t.Fatalf("file batch: %d %#v", status, filed)
	}
	events, _ := filed["events"].([]any)
	withdrawnID := anyString(events[0].(map[string]any)["id"])

	if err := refreshDerivedTopicProjection(ctx, handlerOptions{primitiveStore: env.primitiveStore}, threadID, time.Now().UTC(), agent.ActorID); err != nil {
		t.Fatalf("refresh projection: %v", err)
	}
	items, err := env.primitiveStore.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{ThreadID: threadID})
	if err != nil || len(items) != 1 {
		t.Fatalf("expected one batch item, got %d (%v)", len(items), err)
	}
	staleSections, _ := items[0].Data["batch"].(map[string]any)["asks"].([]any)

	// Withdraw the first member WITHOUT refreshing the projection, so the
	// reader is still looking at the pre-withdrawal section list.
	status, result := hostHTTP(t, http.MethodPost, env.server.URL+"/events", agent.AccessToken, map[string]any{
		"event": map[string]any{
			"type":      "human_attention_withdrawn",
			"thread_id": threadID,
			"summary":   "Abort withdrawn",
			"refs":      []string{"event:" + withdrawnID},
			"payload": map[string]any{
				"request_event_id":      withdrawnID,
				"request_event_ref":     "event:" + withdrawnID,
				"withdrawn_by_actor_id": agent.ActorID,
				"reason":                "No longer needed",
			},
			"provenance": map[string]any{"sources": []string{"event:cli.ask"}},
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("withdraw member: %d %#v", status, result)
	}

	fullAnswers := []map[string]any{}
	for _, raw := range staleSections {
		section, _ := raw.(map[string]any)
		fullAnswers = append(fullAnswers, map[string]any{
			"ask_ref":       section["request_event_ref"],
			"response_text": "Submitted against the stale list",
			"outcome":       "answered",
		})
	}
	status, result = hostHTTP(t, http.MethodPost, env.server.URL+"/inbox/"+url.PathEscape(items[0].ID)+"/respond", human.AccessToken, map[string]any{"answers": fullAnswers})
	if status != http.StatusConflict {
		t.Fatalf("stale submit must conflict, got %d %#v", status, result)
	}
	message := ""
	if errMap, ok := result["error"].(map[string]any); ok {
		message = anyString(errMap["message"])
	}
	if !strings.Contains(message, "was withdrawn") || !strings.Contains(message, "event:"+withdrawnID) {
		t.Fatalf("conflict must name the withdrawn ask: %q", message)
	}

	// Nothing committed: the still-open members have no answers.
	listResp := getJSONExpectStatusWithAuth(t, env.server.URL+"/agent-inbox/asks?limit=200", agent.AccessToken, http.StatusOK)
	var page struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	listResp.Body.Close()
	for _, item := range page.Items {
		if item["answer"] != nil {
			t.Fatalf("no member may be answered by the aborted submit: %#v", item)
		}
	}

	// The shrunk list answers the remaining members.
	if err := refreshDerivedTopicProjection(ctx, handlerOptions{primitiveStore: env.primitiveStore}, threadID, time.Now().UTC(), agent.ActorID); err != nil {
		t.Fatalf("refresh projection: %v", err)
	}
	items, err = env.primitiveStore.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{ThreadID: threadID})
	if err != nil || len(items) != 1 {
		t.Fatalf("batch item must survive the withdrawal, got %d (%v)", len(items), err)
	}
	shrunkSections, _ := items[0].Data["batch"].(map[string]any)["asks"].([]any)
	if len(shrunkSections) != 2 {
		t.Fatalf("expected two open sections after withdrawal, got %#v", items[0].Data["batch"])
	}
	shrunkAnswers := []map[string]any{}
	for _, raw := range shrunkSections {
		section, _ := raw.(map[string]any)
		shrunkAnswers = append(shrunkAnswers, map[string]any{
			"ask_ref":       section["request_event_ref"],
			"response_text": "Answered against the shrunk list",
			"outcome":       "answered",
		})
	}
	status, result = hostHTTP(t, http.MethodPost, env.server.URL+"/inbox/"+url.PathEscape(items[0].ID)+"/respond", human.AccessToken, map[string]any{"answers": shrunkAnswers})
	if status != http.StatusCreated {
		t.Fatalf("shrunk batch respond: %d %#v", status, result)
	}
}

// Regression (review round 1): the filing replay row commits with the member
// events, so the same key against a different body conflicts, and a private
// subject reports 404 the same way a single ask does.
func TestAskBatchFilingReplayConflictAndMissingSubject(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	agent := seedNotificationTestAgent(t, env, "ask.agent")

	threadID := integrationSeedThreadWithStore(t, env.primitiveStore, nil, agent.ActorID, map[string]any{
		"title": "Replay thread", "type": "incident", "status": "active", "priority": "p2",
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
	first := []map[string]any{member("Replay A"), member("Replay B")}
	status, filed := hostHTTP(t, http.MethodPost, env.server.URL+"/asks/batch", agent.AccessToken, map[string]any{
		"request_key": "replay-key-1", "asks": first,
	})
	if status != http.StatusCreated {
		t.Fatalf("file: %d %#v", status, filed)
	}

	// Same key, different body: the key belongs to the original batch.
	status, result := hostHTTP(t, http.MethodPost, env.server.URL+"/asks/batch", agent.AccessToken, map[string]any{
		"request_key": "replay-key-1", "asks": []map[string]any{member("Replay A"), member("Replay CHANGED")},
	})
	if status != http.StatusConflict {
		t.Fatalf("changed body under the same key must conflict, got %d %#v", status, result)
	}

	// A subject the caller cannot resolve is 404, like a single ask.
	status, result = hostHTTP(t, http.MethodPost, env.server.URL+"/asks/batch", agent.AccessToken, map[string]any{
		"asks": []map[string]any{member("Replay A"), func() map[string]any {
			ghost := member("Ghost subject")
			ghost["payload"].(map[string]any)["subject_ref"] = "card:no-such-card-anywhere"
			ghost["refs"] = []string{"thread:" + threadID, "card:no-such-card-anywhere"}
			return ghost
		}()},
	})
	if status != http.StatusNotFound {
		t.Fatalf("unresolvable subject must 404, got %d %#v", status, result)
	}
}
