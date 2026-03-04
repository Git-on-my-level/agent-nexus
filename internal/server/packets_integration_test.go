package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestPacketConvenienceEndpointsAndTimeline(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	threadResp := postJSONExpectStatus(t, h.baseURL+"/threads", `{
		"actor_id":"actor-1",
		"thread":{
			"title":"Packet flow thread",
			"type":"incident",
			"status":"active",
			"priority":"p1",
			"tags":["ops"],
			"cadence":"daily",
			"next_check_in_at":"2026-03-05T00:00:00Z",
			"current_summary":"summary",
			"next_actions":["do x"],
			"key_artifacts":[],
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer threadResp.Body.Close()

	var createdThread struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(threadResp.Body).Decode(&createdThread); err != nil {
		t.Fatalf("decode create thread response: %v", err)
	}
	threadID, _ := createdThread.Thread["id"].(string)
	if threadID == "" {
		t.Fatal("expected created thread id")
	}

	workOrderID := "work-order-1"
	workOrderResp := postJSONExpectStatus(t, h.baseURL+"/work_orders", `{
		"actor_id":"actor-1",
		"artifact":{
			"id":"`+workOrderID+`",
			"refs":["thread:`+threadID+`"],
			"summary":"work order artifact"
		},
		"packet":{
			"work_order_id":"`+workOrderID+`",
			"thread_id":"`+threadID+`",
			"objective":"Investigate and fix",
			"constraints":["no downtime"],
			"context_refs":["url:https://example.com/context"],
			"acceptance_criteria":["incident resolved"],
			"definition_of_done":["receipt published"]
		}
	}`, http.StatusCreated)
	defer workOrderResp.Body.Close()

	var workOrderPayload struct {
		Artifact map[string]any `json:"artifact"`
		Event    map[string]any `json:"event"`
	}
	if err := json.NewDecoder(workOrderResp.Body).Decode(&workOrderPayload); err != nil {
		t.Fatalf("decode work order response: %v", err)
	}
	if workOrderPayload.Artifact["kind"] != "work_order" {
		t.Fatalf("unexpected work order kind: %#v", workOrderPayload.Artifact["kind"])
	}
	if workOrderPayload.Event["type"] != "work_order_created" {
		t.Fatalf("unexpected work order event type: %#v", workOrderPayload.Event["type"])
	}

	if resp, err := http.Get(h.baseURL + "/artifacts/" + workOrderID); err != nil {
		t.Fatalf("GET /artifacts/{work_order_id}: %v", err)
	} else {
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("unexpected GET /artifacts/{work_order_id} status: %d", resp.StatusCode)
		}
	}

	receiptID := "receipt-1"
	receiptFailureResp := postJSONExpectStatus(t, h.baseURL+"/receipts", `{
		"actor_id":"actor-1",
		"artifact":{
			"id":"`+receiptID+`",
			"refs":["thread:`+threadID+`","artifact:`+workOrderID+`"],
			"summary":"receipt artifact"
		},
		"packet":{
			"receipt_id":"`+receiptID+`",
			"work_order_id":"`+workOrderID+`",
			"thread_id":"`+threadID+`",
			"outputs":[],
			"verification_evidence":["url:https://example.com/evidence"],
			"changes_summary":"changed things",
			"known_gaps":[]
		}
	}`, http.StatusBadRequest)
	defer receiptFailureResp.Body.Close()

	receiptSuccessResp := postJSONExpectStatus(t, h.baseURL+"/receipts", `{
		"actor_id":"actor-1",
		"artifact":{
			"id":"`+receiptID+`",
			"refs":["thread:`+threadID+`","artifact:`+workOrderID+`"],
			"summary":"receipt artifact"
		},
		"packet":{
			"receipt_id":"`+receiptID+`",
			"work_order_id":"`+workOrderID+`",
			"thread_id":"`+threadID+`",
			"outputs":["artifact:output-1"],
			"verification_evidence":["url:https://example.com/evidence"],
			"changes_summary":"changed things",
			"known_gaps":[]
		}
	}`, http.StatusCreated)
	defer receiptSuccessResp.Body.Close()

	reviewID := "review-1"
	reviewResp := postJSONExpectStatus(t, h.baseURL+"/reviews", `{
		"actor_id":"actor-1",
		"artifact":{
			"id":"`+reviewID+`",
			"refs":["thread:`+threadID+`","artifact:`+receiptID+`","artifact:`+workOrderID+`"],
			"summary":"review artifact"
		},
		"packet":{
			"review_id":"`+reviewID+`",
			"work_order_id":"`+workOrderID+`",
			"receipt_id":"`+receiptID+`",
			"outcome":"accept",
			"notes":"looks good",
			"evidence_refs":["artifact:`+receiptID+`"]
		}
	}`, http.StatusCreated)
	defer reviewResp.Body.Close()

	if resp, err := http.Get(h.baseURL + "/artifacts/" + reviewID); err != nil {
		t.Fatalf("GET /artifacts/{review_id}: %v", err)
	} else {
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("unexpected GET /artifacts/{review_id} status: %d", resp.StatusCode)
		}
	}

	timelineResp, err := http.Get(h.baseURL + "/threads/" + threadID + "/timeline")
	if err != nil {
		t.Fatalf("GET /threads/{id}/timeline: %v", err)
	}
	defer timelineResp.Body.Close()
	if timelineResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected timeline status: %d", timelineResp.StatusCode)
	}

	var timeline struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.NewDecoder(timelineResp.Body).Decode(&timeline); err != nil {
		t.Fatalf("decode timeline response: %v", err)
	}

	workOrderEvent := findEventByType(timeline.Events, "work_order_created")
	if workOrderEvent == nil {
		t.Fatal("expected work_order_created event in timeline")
	}
	assertRefsContain(t, workOrderEvent["refs"], "artifact:"+workOrderID, "thread:"+threadID)

	receiptEvent := findEventByType(timeline.Events, "receipt_added")
	if receiptEvent == nil {
		t.Fatal("expected receipt_added event in timeline")
	}
	assertRefsContain(t, receiptEvent["refs"], "artifact:"+receiptID, "artifact:"+workOrderID)

	reviewEvent := findEventByType(timeline.Events, "review_completed")
	if reviewEvent == nil {
		t.Fatal("expected review_completed event in timeline")
	}
	assertRefsContain(t, reviewEvent["refs"], "artifact:"+reviewID, "artifact:"+receiptID, "artifact:"+workOrderID)
}

func TestPacketValidationErrors(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	threadResp := postJSONExpectStatus(t, h.baseURL+"/threads", `{
		"actor_id":"actor-1",
		"thread":{
			"title":"Packet validation thread",
			"type":"incident",
			"status":"active",
			"priority":"p1",
			"tags":["ops"],
			"cadence":"daily",
			"next_check_in_at":"2026-03-05T00:00:00Z",
			"current_summary":"summary",
			"next_actions":["do x"],
			"key_artifacts":[],
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer threadResp.Body.Close()
	var threadPayload struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(threadResp.Body).Decode(&threadPayload); err != nil {
		t.Fatalf("decode thread response: %v", err)
	}
	threadID, _ := threadPayload.Thread["id"].(string)
	if threadID == "" {
		t.Fatal("expected thread id")
	}

	respMissingField := postJSONExpectStatus(t, h.baseURL+"/work_orders", `{
		"actor_id":"actor-1",
		"artifact":{"id":"wo-missing","refs":["thread:`+threadID+`"]},
		"packet":{
			"work_order_id":"wo-missing",
			"thread_id":"`+threadID+`",
			"constraints":["none"],
			"context_refs":["url:https://example.com/context"],
			"acceptance_criteria":["done"],
			"definition_of_done":["published"]
		}
	}`, http.StatusBadRequest)
	assertErrorMessageContains(t, respMissingField, "packet.objective is required")

	respBadTypedRef := postJSONExpectStatus(t, h.baseURL+"/work_orders", `{
		"actor_id":"actor-1",
		"artifact":{"id":"wo-bad-ref","refs":["thread:`+threadID+`"]},
		"packet":{
			"work_order_id":"wo-bad-ref",
			"thread_id":"`+threadID+`",
			"objective":"obj",
			"constraints":["none"],
			"context_refs":["invalidref"],
			"acceptance_criteria":["done"],
			"definition_of_done":["published"]
		}
	}`, http.StatusBadRequest)
	assertErrorMessageContains(t, respBadTypedRef, "packet.context_refs")

	respIDMismatch := postJSONExpectStatus(t, h.baseURL+"/work_orders", `{
		"actor_id":"actor-1",
		"artifact":{"id":"wo-one","refs":["thread:`+threadID+`"]},
		"packet":{
			"work_order_id":"wo-two",
			"thread_id":"`+threadID+`",
			"objective":"obj",
			"constraints":["none"],
			"context_refs":["url:https://example.com/context"],
			"acceptance_criteria":["done"],
			"definition_of_done":["published"]
		}
	}`, http.StatusBadRequest)
	assertErrorMessageContains(t, respIDMismatch, "must equal artifact.id")
}

func findEventByType(events []map[string]any, eventType string) map[string]any {
	for _, event := range events {
		if typeText, _ := event["type"].(string); typeText == eventType {
			return event
		}
	}
	return nil
}

func assertRefsContain(t *testing.T, rawRefs any, expected ...string) {
	t.Helper()

	refs := make(map[string]struct{})
	switch values := rawRefs.(type) {
	case []string:
		for _, value := range values {
			refs[value] = struct{}{}
		}
	case []any:
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				continue
			}
			refs[text] = struct{}{}
		}
	default:
		t.Fatalf("unexpected refs type: %#v", rawRefs)
	}

	for _, want := range expected {
		if _, ok := refs[want]; !ok {
			t.Fatalf("expected refs to include %q, got %#v", want, rawRefs)
		}
	}
}

func assertErrorMessageContains(t *testing.T, resp *http.Response, want string) {
	t.Helper()
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read error response: %v", err)
	}

	var payload map[string]map[string]string
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode error response: %v body=%s", err, string(body))
	}

	message := payload["error"]["message"]
	if !strings.Contains(message, want) {
		t.Fatalf("expected error message to contain %q, got %q", want, message)
	}
}
