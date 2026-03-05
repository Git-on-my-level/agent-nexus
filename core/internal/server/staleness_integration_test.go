package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestStalenessRebuildEmitsSingleStaleExceptionAndInboxException(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createResp := postJSONExpectStatus(t, h.baseURL+"/threads", `{
		"actor_id":"actor-1",
		"thread":{
			"title":"Daily stale thread",
			"type":"incident",
			"status":"active",
			"priority":"p1",
			"tags":["ops"],
			"cadence":"daily",
			"next_check_in_at":"2020-01-01T00:00:00Z",
			"current_summary":"summary",
			"next_actions":["do x"],
			"key_artifacts":[],
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer createResp.Body.Close()

	var created struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode thread response: %v", err)
	}
	threadID, _ := created.Thread["id"].(string)
	if threadID == "" {
		t.Fatal("expected thread id")
	}

	postJSONExpectStatus(t, h.baseURL+"/derived/rebuild", `{"actor_id":"actor-1"}`, http.StatusOK).Body.Close()

	staleCount := countStaleThreadExceptions(t, h.baseURL, threadID)
	if staleCount != 1 {
		t.Fatalf("expected exactly one stale_thread exception after first rebuild, got %d", staleCount)
	}
	staleEvent := findStaleThreadExceptionEvent(t, h.baseURL, threadID)
	if staleEvent == nil {
		t.Fatal("expected stale_thread exception event in timeline")
	}
	assertInferredProvenance(t, staleEvent)

	items := getInboxItems(t, h.baseURL)
	if _, ok := findInboxItem(items, func(item map[string]any) bool {
		return asString(item["category"]) == "exception" && asString(item["thread_id"]) == threadID
	}); !ok {
		t.Fatalf("expected stale exception inbox item, got %#v", items)
	}

	postJSONExpectStatus(t, h.baseURL+"/derived/rebuild", `{"actor_id":"actor-1"}`, http.StatusOK).Body.Close()

	staleCountAgain := countStaleThreadExceptions(t, h.baseURL, threadID)
	if staleCountAgain != 1 {
		t.Fatalf("expected idempotent stale exception emission, got %d", staleCountAgain)
	}

	postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"decision_made",
			"thread_id":"`+threadID+`",
			"refs":["thread:`+threadID+`"],
			"summary":"decision made",
			"payload":{"outcome":"resolved"},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated).Body.Close()

	itemsAfterDecision := getInboxItems(t, h.baseURL)
	if _, ok := findInboxItem(itemsAfterDecision, func(item map[string]any) bool {
		return asString(item["category"]) == "exception" && asString(item["thread_id"]) == threadID
	}); ok {
		t.Fatalf("expected stale exception inbox item to be suppressed after new decision activity, got %#v", itemsAfterDecision)
	}
}

func countStaleThreadExceptions(t *testing.T, baseURL string, threadID string) int {
	t.Helper()

	resp, err := http.Get(baseURL + "/threads/" + threadID + "/timeline")
	if err != nil {
		t.Fatalf("GET /threads/{id}/timeline: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected timeline status: %d", resp.StatusCode)
	}

	var payload struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode timeline response: %v", err)
	}

	count := 0
	for _, event := range payload.Events {
		eventType, _ := event["type"].(string)
		if eventType != "exception_raised" {
			continue
		}
		payloadObj, _ := event["payload"].(map[string]any)
		subtype, _ := payloadObj["subtype"].(string)
		if subtype == "stale_thread" {
			count++
		}
	}
	return count
}

func findStaleThreadExceptionEvent(t *testing.T, baseURL string, threadID string) map[string]any {
	t.Helper()

	resp, err := http.Get(baseURL + "/threads/" + threadID + "/timeline")
	if err != nil {
		t.Fatalf("GET /threads/{id}/timeline: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected timeline status: %d", resp.StatusCode)
	}

	var payload struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode timeline response: %v", err)
	}

	for _, event := range payload.Events {
		eventType, _ := event["type"].(string)
		if eventType != "exception_raised" {
			continue
		}
		payloadObj, _ := event["payload"].(map[string]any)
		subtype, _ := payloadObj["subtype"].(string)
		if subtype == "stale_thread" {
			return event
		}
	}
	return nil
}
