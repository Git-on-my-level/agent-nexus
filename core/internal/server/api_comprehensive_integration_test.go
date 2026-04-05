package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestComprehensiveHTTPAPIFlow(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	threadID := integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":            "Comprehensive thread",
		"type":             "incident",
		"status":           "active",
		"priority":         "p1",
		"tags":             []any{"ops", "backend"},
		"cadence":          "daily",
		"next_check_in_at": "2030-01-01T00:00:00Z",
		"current_summary":  "Investigating issue",
		"next_actions":     []any{"triage"},
		"key_artifacts":    []any{},
		"provenance":       map[string]any{"sources": []any{"inferred"}},
		"custom_unknown":   "preserve_me",
	})
	integrationPatchThread(t, h, "actor-1", threadID, map[string]any{"tags": []any{"backend"}}, nil)

	getThreadResp, err := http.Get(h.baseURL + "/threads/" + threadID)
	if err != nil {
		t.Fatalf("GET /threads/{id}: %v", err)
	}
	defer getThreadResp.Body.Close()
	if getThreadResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected get thread status: got %d", getThreadResp.StatusCode)
	}
	var loadedThread struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(getThreadResp.Body).Decode(&loadedThread); err != nil {
		t.Fatalf("decode thread response: %v", err)
	}
	if loadedThread.Thread["custom_unknown"] != "preserve_me" {
		t.Fatalf("expected unknown field preserved, got %#v", loadedThread.Thread["custom_unknown"])
	}
	tagsRaw, _ := loadedThread.Thread["tags"].([]any)
	tags := anyListToSortedStrings(tagsRaw)
	if len(tags) != 1 || tags[0] != "backend" {
		t.Fatalf("expected list replacement for tags, got %#v", loadedThread.Thread["tags"])
	}

	integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":            "Stale thread",
		"type":             "incident",
		"status":           "active",
		"priority":         "p2",
		"tags":             []any{"ops"},
		"cadence":          "daily",
		"next_check_in_at": "2020-01-01T00:00:00Z",
		"current_summary":  "Needs update",
		"next_actions":     []any{"follow up"},
		"key_artifacts":    []any{},
		"provenance":       map[string]any{"sources": []any{"inferred"}},
	})

	workOrderID := "wo-comprehensive"
	workOrderResp := postJSONExpectStatus(t, h.baseURL+"/work_orders", `{
		"actor_id":"actor-1",
		"artifact":{"id":"`+workOrderID+`","refs":["thread:`+threadID+`"],"summary":"work order"},
		"packet":{
			"work_order_id":"`+workOrderID+`",
			"subject_ref":"thread:`+threadID+`",
			"objective":"fix",
			"constraints":["none"],
			"context_refs":["url:https://example.com/context"],
			"acceptance_criteria":["fixed"],
			"definition_of_done":["receipt"]
		}
	}`, http.StatusCreated)
	defer workOrderResp.Body.Close()
	var workOrderPayload struct {
		Event map[string]any `json:"event"`
	}
	if err := json.NewDecoder(workOrderResp.Body).Decode(&workOrderPayload); err != nil {
		t.Fatalf("decode work order response: %v", err)
	}
	assertRefsContain(t, workOrderPayload.Event["refs"], "artifact:"+workOrderID)

	postJSONExpectStatus(t, h.baseURL+"/receipts", `{
		"actor_id":"actor-1",
		"artifact":{"id":"receipt-invalid","refs":["thread:`+threadID+`","artifact:`+workOrderID+`"],"summary":"receipt"},
		"packet":{
			"receipt_id":"receipt-invalid",
			"work_order_id":"`+workOrderID+`",
			"subject_ref":"thread:`+threadID+`",
			"outputs":[],
			"verification_evidence":["url:https://example.com/evidence"],
			"changes_summary":"summary",
			"known_gaps":[]
		}
	}`, http.StatusBadRequest).Body.Close()

	receiptID := "receipt-comprehensive"
	receiptResp := postJSONExpectStatus(t, h.baseURL+"/receipts", `{
		"actor_id":"actor-1",
		"artifact":{"id":"`+receiptID+`","refs":["thread:`+threadID+`","artifact:`+workOrderID+`"],"summary":"receipt"},
		"packet":{
			"receipt_id":"`+receiptID+`",
			"work_order_id":"`+workOrderID+`",
			"subject_ref":"thread:`+threadID+`",
			"outputs":["artifact:output-1"],
			"verification_evidence":["url:https://example.com/evidence"],
			"changes_summary":"summary",
			"known_gaps":[]
		}
	}`, http.StatusCreated)
	defer receiptResp.Body.Close()

	reviewID := "review-comprehensive"
	postJSONExpectStatus(t, h.baseURL+"/reviews", `{
		"actor_id":"actor-1",
		"artifact":{"id":"`+reviewID+`","refs":["thread:`+threadID+`","artifact:`+receiptID+`","artifact:`+workOrderID+`"],"summary":"review"},
		"packet":{
			"review_id":"`+reviewID+`",
			"subject_ref":"thread:`+threadID+`",
			"work_order_id":"`+workOrderID+`",
			"receipt_id":"`+receiptID+`",
			"outcome":"accept",
			"notes":"ok",
			"evidence_refs":["artifact:`+receiptID+`"]
		}
	}`, http.StatusCreated).Body.Close()

	timelineResp, err := http.Get(h.baseURL + "/threads/" + threadID + "/timeline")
	if err != nil {
		t.Fatalf("GET timeline: %v", err)
	}
	defer timelineResp.Body.Close()
	if timelineResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected timeline status: %d", timelineResp.StatusCode)
	}
	var timeline struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.NewDecoder(timelineResp.Body).Decode(&timeline); err != nil {
		t.Fatalf("decode timeline: %v", err)
	}
	receiptAdded := findEventByType(timeline.Events, "receipt_added")
	if receiptAdded == nil {
		t.Fatal("expected receipt_added event")
	}
	assertRefsContain(t, receiptAdded["refs"], "artifact:"+receiptID, "artifact:"+workOrderID)
	reviewCompleted := findEventByType(timeline.Events, "review_completed")
	if reviewCompleted == nil {
		t.Fatal("expected review_completed event")
	}
	assertRefsContain(t, reviewCompleted["refs"], "artifact:"+reviewID, "artifact:"+receiptID, "artifact:"+workOrderID)

	postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"decision_needed",
			"thread_id":"`+threadID+`",
			"refs":["topic:`+threadID+`"],
			"summary":"need decision",
			"payload":{},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated).Body.Close()

	createBoardResp := postJSONExpectStatus(t, h.baseURL+"/boards", `{
		"actor_id":"actor-1",
		"board":{
			"title":"Comprehensive inbox board",
			"refs":["thread:`+threadID+`"]
		}
	}`, http.StatusCreated)
	defer createBoardResp.Body.Close()
	var createdBoard struct {
		Board map[string]any `json:"board"`
	}
	if err := json.NewDecoder(createBoardResp.Body).Decode(&createdBoard); err != nil {
		t.Fatalf("decode board response: %v", err)
	}
	boardID := asString(createdBoard.Board["id"])
	boardUpdatedAt := asString(createdBoard.Board["updated_at"])
	cardCreateResp := postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards", `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+boardUpdatedAt+`",
		"thread_id":"`+threadID+`",
		"title":"Comprehensive work item",
		"column_key":"ready",
		"due_at":"`+time.Now().UTC().Add(24*time.Hour).Format(time.RFC3339)+`",
		"definition_of_done":["receipt","sign-off"]
	}`, http.StatusCreated)
	var comprehensiveCard struct {
		Card map[string]any `json:"card"`
	}
	if err := json.NewDecoder(cardCreateResp.Body).Decode(&comprehensiveCard); err != nil {
		t.Fatalf("decode card create: %v", err)
	}
	cardCreateResp.Body.Close()
	dod, ok := comprehensiveCard.Card["definition_of_done"].([]any)
	if !ok || len(dod) != 2 {
		t.Fatalf("expected definition_of_done on card payload, got %#v", comprehensiveCard.Card["definition_of_done"])
	}

	postJSONExpectStatus(t, h.baseURL+"/derived/rebuild", `{"actor_id":"actor-1"}`, http.StatusOK).Body.Close()

	inboxItems := getInboxItems(t, h.baseURL)
	categories := map[string]bool{}
	for _, item := range inboxItems {
		categories[asString(item["category"])] = true
	}
	if !categories["work_item_risk"] || !categories["stale_topic"] || !categories["decision_needed"] {
		t.Fatalf("expected inbox categories work_item_risk/stale_topic/decision_needed, got %#v", categories)
	}

	decisionItem, ok := findInboxItem(inboxItems, func(item map[string]any) bool {
		return asString(item["category"]) == "decision_needed" && asString(item["thread_id"]) == threadID
	})
	if !ok {
		t.Fatalf("expected decision_needed item for thread %s, got %#v", threadID, inboxItems)
	}
	decisionItemID := asString(decisionItem["id"])

	postJSONExpectStatus(t, h.baseURL+"/inbox/ack", `{
		"actor_id":"actor-1",
		"thread_id":"`+threadID+`",
		"inbox_item_id":"`+decisionItemID+`"
	}`, http.StatusCreated).Body.Close()

	inboxAfterAck := getInboxItems(t, h.baseURL)
	if _, exists := findInboxItem(inboxAfterAck, func(item map[string]any) bool {
		return asString(item["id"]) == decisionItemID
	}); exists {
		t.Fatalf("expected acknowledged inbox item to be suppressed, got %#v", inboxAfterAck)
	}

	newDecisionResp := postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"decision_needed",
			"thread_id":"`+threadID+`",
			"refs":["topic:`+threadID+`"],
			"summary":"retrigger decision",
			"payload":{},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer newDecisionResp.Body.Close()
	var newDecisionPayload struct {
		Event map[string]any `json:"event"`
	}
	if err := json.NewDecoder(newDecisionResp.Body).Decode(&newDecisionPayload); err != nil {
		t.Fatalf("decode retrigger decision response: %v", err)
	}
	newDecisionEventID := asString(newDecisionPayload.Event["id"])

	inboxAfterRetrigger := getInboxItems(t, h.baseURL)
	if _, ok := findInboxItem(inboxAfterRetrigger, func(item map[string]any) bool {
		return asString(item["category"]) == "decision_needed" && asString(item["source_event_id"]) == newDecisionEventID
	}); !ok {
		t.Fatalf("expected retriggered decision item, got %#v", inboxAfterRetrigger)
	}

	// PrimitiveStore accepts opaque thread bodies; strict enum checks live at HTTP ingress.
	// Keep a lightweight invariant check that the store still rejects missing actor context.
	_, ctErr := h.primitiveStore.CreateThread(context.Background(), "", map[string]any{
		"title":            "Invalid actor",
		"type":             "incident",
		"status":           "active",
		"priority":         "p1",
		"tags":             []any{},
		"cadence":          "daily",
		"next_check_in_at": "2030-01-01T00:00:00Z",
		"current_summary":  "summary",
		"next_actions":     []any{},
		"key_artifacts":    []any{},
		"provenance":       map[string]any{"sources": []any{"inferred"}},
	})
	if ctErr == nil {
		t.Fatal("expected CreateThread to reject empty actor id")
	}
	if !strings.Contains(ctErr.Error(), "actor") {
		t.Fatalf("expected actor id validation error, got: %v", ctErr)
	}

	postJSONExpectStatus(t, h.baseURL+"/work_orders", `{
		"actor_id":"actor-1",
		"artifact":{"id":"wo-mismatch-a","refs":["thread:`+threadID+`"]},
		"packet":{
			"work_order_id":"wo-mismatch-b",
			"subject_ref":"thread:`+threadID+`",
			"objective":"x",
			"constraints":["none"],
			"context_refs":["url:https://example.com/context"],
			"acceptance_criteria":["done"],
			"definition_of_done":["receipt"]
		}
	}`, http.StatusBadRequest).Body.Close()

	postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"receipt_added",
			"thread_id":"`+threadID+`",
			"refs":["artifact:only-one"],
			"summary":"bad refs",
			"payload":{},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusBadRequest).Body.Close()

}
