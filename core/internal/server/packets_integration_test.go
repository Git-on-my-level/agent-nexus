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
	assertActorStatementProvenance(t, workOrderPayload.Event)

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
	assertActorStatementProvenance(t, workOrderEvent)

	receiptEvent := findEventByType(timeline.Events, "receipt_added")
	if receiptEvent == nil {
		t.Fatal("expected receipt_added event in timeline")
	}
	assertRefsContain(t, receiptEvent["refs"], "artifact:"+receiptID, "artifact:"+workOrderID)
	assertActorStatementProvenance(t, receiptEvent)

	reviewEvent := findEventByType(timeline.Events, "review_completed")
	if reviewEvent == nil {
		t.Fatal("expected review_completed event in timeline")
	}
	assertRefsContain(t, reviewEvent["refs"], "artifact:"+reviewID, "artifact:"+receiptID, "artifact:"+workOrderID)
	assertActorStatementProvenance(t, reviewEvent)
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

func TestPacketCreateRequestKeyReplaysSingleWrite(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	threadResp := postJSONExpectStatus(t, h.baseURL+"/threads", `{
		"actor_id":"actor-1",
		"thread":{
			"title":"Packet replay thread",
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
		t.Fatalf("decode thread response: %v", err)
	}
	threadID, _ := createdThread.Thread["id"].(string)
	if threadID == "" {
		t.Fatal("expected thread id")
	}

	workOrderBody := `{
		"actor_id":"actor-1",
		"request_key":"replay-work-order",
		"artifact":{
			"refs":["thread:` + threadID + `"],
			"summary":"work order artifact"
		},
		"packet":{
			"thread_id":"` + threadID + `",
			"objective":"Investigate and fix",
			"constraints":["no downtime"],
			"context_refs":["url:https://example.com/context"],
			"acceptance_criteria":["incident resolved"],
			"definition_of_done":["receipt published"]
		}
	}`

	firstWorkOrderResp := postJSONExpectStatus(t, h.baseURL+"/work_orders", workOrderBody, http.StatusCreated)
	defer firstWorkOrderResp.Body.Close()
	secondWorkOrderResp := postJSONExpectStatus(t, h.baseURL+"/work_orders", workOrderBody, http.StatusCreated)
	defer secondWorkOrderResp.Body.Close()

	var firstWorkOrder struct {
		Artifact map[string]any `json:"artifact"`
		Event    map[string]any `json:"event"`
	}
	if err := json.NewDecoder(firstWorkOrderResp.Body).Decode(&firstWorkOrder); err != nil {
		t.Fatalf("decode first work order response: %v", err)
	}
	var secondWorkOrder struct {
		Artifact map[string]any `json:"artifact"`
		Event    map[string]any `json:"event"`
	}
	if err := json.NewDecoder(secondWorkOrderResp.Body).Decode(&secondWorkOrder); err != nil {
		t.Fatalf("decode second work order response: %v", err)
	}
	workOrderID, _ := firstWorkOrder.Artifact["id"].(string)
	if workOrderID == "" {
		t.Fatal("expected server-issued work order id")
	}
	if secondWorkOrder.Artifact["id"] != workOrderID {
		t.Fatalf("expected replayed work order id %q, got %#v", workOrderID, secondWorkOrder.Artifact["id"])
	}
	if secondWorkOrder.Event["id"] != firstWorkOrder.Event["id"] {
		t.Fatalf("expected replayed work order event id %#v, got %#v", firstWorkOrder.Event["id"], secondWorkOrder.Event["id"])
	}

	receiptBody := `{
		"actor_id":"actor-1",
		"request_key":"replay-receipt",
		"artifact":{
			"refs":["thread:` + threadID + `","artifact:` + workOrderID + `"],
			"summary":"receipt artifact"
		},
		"packet":{
			"work_order_id":"` + workOrderID + `",
			"thread_id":"` + threadID + `",
			"outputs":["artifact:output-1"],
			"verification_evidence":["url:https://example.com/evidence"],
			"changes_summary":"changed things",
			"known_gaps":[]
		}
	}`

	firstReceiptResp := postJSONExpectStatus(t, h.baseURL+"/receipts", receiptBody, http.StatusCreated)
	defer firstReceiptResp.Body.Close()
	secondReceiptResp := postJSONExpectStatus(t, h.baseURL+"/receipts", receiptBody, http.StatusCreated)
	defer secondReceiptResp.Body.Close()

	var firstReceipt struct {
		Artifact map[string]any `json:"artifact"`
		Event    map[string]any `json:"event"`
	}
	if err := json.NewDecoder(firstReceiptResp.Body).Decode(&firstReceipt); err != nil {
		t.Fatalf("decode first receipt response: %v", err)
	}
	var secondReceipt struct {
		Artifact map[string]any `json:"artifact"`
		Event    map[string]any `json:"event"`
	}
	if err := json.NewDecoder(secondReceiptResp.Body).Decode(&secondReceipt); err != nil {
		t.Fatalf("decode second receipt response: %v", err)
	}
	receiptID, _ := firstReceipt.Artifact["id"].(string)
	if receiptID == "" {
		t.Fatal("expected server-issued receipt id")
	}
	if secondReceipt.Artifact["id"] != receiptID {
		t.Fatalf("expected replayed receipt id %q, got %#v", receiptID, secondReceipt.Artifact["id"])
	}
	if secondReceipt.Event["id"] != firstReceipt.Event["id"] {
		t.Fatalf("expected replayed receipt event id %#v, got %#v", firstReceipt.Event["id"], secondReceipt.Event["id"])
	}

	workOrdersResp, err := http.Get(h.baseURL + "/artifacts?thread_id=" + threadID + "&kind=work_order")
	if err != nil {
		t.Fatalf("GET /artifacts work_orders: %v", err)
	}
	defer workOrdersResp.Body.Close()
	var workOrdersListed struct {
		Artifacts []map[string]any `json:"artifacts"`
	}
	if err := json.NewDecoder(workOrdersResp.Body).Decode(&workOrdersListed); err != nil {
		t.Fatalf("decode listed work orders: %v", err)
	}
	if len(workOrdersListed.Artifacts) != 1 {
		t.Fatalf("expected one work order after replay, got %d", len(workOrdersListed.Artifacts))
	}

	receiptsResp, err := http.Get(h.baseURL + "/artifacts?thread_id=" + threadID + "&kind=receipt")
	if err != nil {
		t.Fatalf("GET /artifacts receipts: %v", err)
	}
	defer receiptsResp.Body.Close()
	var receiptsListed struct {
		Artifacts []map[string]any `json:"artifacts"`
	}
	if err := json.NewDecoder(receiptsResp.Body).Decode(&receiptsListed); err != nil {
		t.Fatalf("decode listed receipts: %v", err)
	}
	if len(receiptsListed.Artifacts) != 1 {
		t.Fatalf("expected one receipt after replay, got %d", len(receiptsListed.Artifacts))
	}

	timelineResp, err := http.Get(h.baseURL + "/threads/" + threadID + "/timeline")
	if err != nil {
		t.Fatalf("GET /threads/{id}/timeline: %v", err)
	}
	defer timelineResp.Body.Close()
	var timeline struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.NewDecoder(timelineResp.Body).Decode(&timeline); err != nil {
		t.Fatalf("decode timeline: %v", err)
	}
	if countEventsByType(timeline.Events, "work_order_created") != 1 {
		t.Fatalf("expected one work_order_created event, got %d", countEventsByType(timeline.Events, "work_order_created"))
	}
	if countEventsByType(timeline.Events, "receipt_added") != 1 {
		t.Fatalf("expected one receipt_added event, got %d", countEventsByType(timeline.Events, "receipt_added"))
	}
}

func countEventsByType(events []map[string]any, eventType string) int {
	count := 0
	for _, event := range events {
		if asString(event["type"]) == eventType {
			count++
		}
	}
	return count
}

func TestPacketConvenienceEndpointsRejectUnsafeArtifactIDs(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	threadResp := postJSONExpectStatus(t, h.baseURL+"/threads", `{
		"actor_id":"actor-1",
		"thread":{
			"title":"Packet ID safety thread",
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

	workOrderInvalidIDResp := postJSONExpectStatus(t, h.baseURL+"/work_orders", `{
		"actor_id":"actor-1",
		"artifact":{"id":"../../wo-bad","refs":["thread:`+threadID+`"]},
		"packet":{
			"work_order_id":"../../wo-bad",
			"thread_id":"`+threadID+`",
			"objective":"obj",
			"constraints":["none"],
			"context_refs":["url:https://example.com/context"],
			"acceptance_criteria":["done"],
			"definition_of_done":["published"]
		}
	}`, http.StatusBadRequest)
	assertErrorMessageContains(t, workOrderInvalidIDResp, "artifact.id")

	const workOrderID = "wo-valid-for-unsafe-id-tests"
	postJSONExpectStatus(t, h.baseURL+"/work_orders", `{
		"actor_id":"actor-1",
		"artifact":{"id":"`+workOrderID+`","refs":["thread:`+threadID+`"]},
		"packet":{
			"work_order_id":"`+workOrderID+`",
			"thread_id":"`+threadID+`",
			"objective":"obj",
			"constraints":["none"],
			"context_refs":["url:https://example.com/context"],
			"acceptance_criteria":["done"],
			"definition_of_done":["published"]
		}
	}`, http.StatusCreated).Body.Close()

	receiptInvalidIDResp := postJSONExpectStatus(t, h.baseURL+"/receipts", `{
		"actor_id":"actor-1",
		"artifact":{"id":"..","refs":["thread:`+threadID+`","artifact:`+workOrderID+`"]},
		"packet":{
			"receipt_id":"..",
			"work_order_id":"`+workOrderID+`",
			"thread_id":"`+threadID+`",
			"outputs":["artifact:output-1"],
			"verification_evidence":["url:https://example.com/evidence"],
			"changes_summary":"summary",
			"known_gaps":[]
		}
	}`, http.StatusBadRequest)
	assertErrorMessageContains(t, receiptInvalidIDResp, "artifact.id")

	const receiptID = "receipt-valid-for-unsafe-id-tests"
	postJSONExpectStatus(t, h.baseURL+"/receipts", `{
		"actor_id":"actor-1",
		"artifact":{"id":"`+receiptID+`","refs":["thread:`+threadID+`","artifact:`+workOrderID+`"]},
		"packet":{
			"receipt_id":"`+receiptID+`",
			"work_order_id":"`+workOrderID+`",
			"thread_id":"`+threadID+`",
			"outputs":["artifact:output-1"],
			"verification_evidence":["url:https://example.com/evidence"],
			"changes_summary":"summary",
			"known_gaps":[]
		}
	}`, http.StatusCreated).Body.Close()

	reviewInvalidIDResp := postJSONExpectStatus(t, h.baseURL+"/reviews", `{
		"actor_id":"actor-1",
		"artifact":{"id":"/tmp/review-bad","refs":["thread:`+threadID+`","artifact:`+receiptID+`","artifact:`+workOrderID+`"]},
		"packet":{
			"review_id":"/tmp/review-bad",
			"work_order_id":"`+workOrderID+`",
			"receipt_id":"`+receiptID+`",
			"outcome":"accept",
			"notes":"ok",
			"evidence_refs":["artifact:`+receiptID+`"]
		}
	}`, http.StatusBadRequest)
	assertErrorMessageContains(t, reviewInvalidIDResp, "artifact.id")
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

	var payload map[string]map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode error response: %v body=%s", err, string(body))
	}

	message, _ := payload["error"]["message"].(string)
	if !strings.Contains(message, want) {
		t.Fatalf("expected error message to contain %q, got %q", want, message)
	}
}
