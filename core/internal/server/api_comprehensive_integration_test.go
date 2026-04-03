package server

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestComprehensiveHTTPAPIFlow(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	threadResp := postJSONExpectStatus(t, h.baseURL+"/threads", `{
		"actor_id":"actor-1",
		"thread":{
			"title":"Comprehensive thread",
			"type":"incident",
			"status":"active",
			"priority":"p1",
			"tags":["ops","backend"],
			"cadence":"daily",
			"next_check_in_at":"2030-01-01T00:00:00Z",
			"current_summary":"Investigating issue",
			"next_actions":["triage"],
			"key_artifacts":[],
			"provenance":{"sources":["inferred"]},
			"custom_unknown":"preserve_me"
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
		t.Fatal("expected thread id")
	}

	patchResp := patchJSONExpectStatus(t, h.baseURL+"/threads/"+threadID, `{
		"actor_id":"actor-1",
		"patch":{"tags":["backend"]}
	}`, http.StatusOK)
	defer patchResp.Body.Close()

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
	tags := sortedStringList(loadedThread.Thread["tags"])
	if len(tags) != 1 || tags[0] != "backend" {
		t.Fatalf("expected list replacement for tags, got %#v", loadedThread.Thread["tags"])
	}

	staleThreadResp := postJSONExpectStatus(t, h.baseURL+"/threads", `{
		"actor_id":"actor-1",
		"thread":{
			"title":"Stale thread",
			"type":"incident",
			"status":"active",
			"priority":"p2",
			"tags":["ops"],
			"cadence":"daily",
			"next_check_in_at":"2020-01-01T00:00:00Z",
			"current_summary":"Needs update",
			"next_actions":["follow up"],
			"key_artifacts":[],
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer staleThreadResp.Body.Close()

	dueSoon := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)

	commitment1Resp := postJSONExpectStatus(t, h.baseURL+"/commitments", `{
		"actor_id":"actor-1",
		"commitment":{
			"thread_id":"`+threadID+`",
			"title":"Commitment one",
			"owner":"actor-1",
			"due_at":"`+dueSoon+`",
			"status":"open",
			"definition_of_done":["done"],
			"links":["url:https://example.com/c1"],
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer commitment1Resp.Body.Close()
	var commitment1Payload struct {
		Commitment map[string]any `json:"commitment"`
	}
	if err := json.NewDecoder(commitment1Resp.Body).Decode(&commitment1Payload); err != nil {
		t.Fatalf("decode commitment1 response: %v", err)
	}
	commitment1ID := asString(commitment1Payload.Commitment["id"])

	commitment2Resp := postJSONExpectStatus(t, h.baseURL+"/commitments", `{
		"actor_id":"actor-1",
		"commitment":{
			"thread_id":"`+threadID+`",
			"title":"Commitment two",
			"owner":"actor-1",
			"due_at":"`+dueSoon+`",
			"status":"open",
			"definition_of_done":["done"],
			"links":["url:https://example.com/c2"],
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer commitment2Resp.Body.Close()
	var commitment2Payload struct {
		Commitment map[string]any `json:"commitment"`
	}
	if err := json.NewDecoder(commitment2Resp.Body).Decode(&commitment2Payload); err != nil {
		t.Fatalf("decode commitment2 response: %v", err)
	}
	commitment2ID := asString(commitment2Payload.Commitment["id"])

	threadAfterCommitmentsResp, err := http.Get(h.baseURL + "/threads/" + threadID)
	if err != nil {
		t.Fatalf("GET /threads/{id} after commitments: %v", err)
	}
	defer threadAfterCommitmentsResp.Body.Close()
	if threadAfterCommitmentsResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected get thread status: got %d", threadAfterCommitmentsResp.StatusCode)
	}
	var threadAfterCommitments struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(threadAfterCommitmentsResp.Body).Decode(&threadAfterCommitments); err != nil {
		t.Fatalf("decode thread after commitments response: %v", err)
	}
	openCommitments := sortedStringList(threadAfterCommitments.Thread["open_commitments"])
	if len(openCommitments) < 2 || !containsString(openCommitments, commitment1ID) || !containsString(openCommitments, commitment2ID) {
		t.Fatalf("expected thread.open_commitments to include both commitments, got %#v", threadAfterCommitments.Thread["open_commitments"])
	}

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

	patchJSONExpectStatus(t, h.baseURL+"/commitments/"+commitment1ID, `{
		"actor_id":"actor-1",
		"patch":{"status":"done"}
	}`, http.StatusBadRequest).Body.Close()

	doneResp := patchJSONExpectStatus(t, h.baseURL+"/commitments/"+commitment1ID, `{
		"actor_id":"actor-1",
		"patch":{"status":"done"},
		"refs":["artifact:`+receiptID+`"]
	}`, http.StatusOK)
	defer doneResp.Body.Close()
	var donePayload struct {
		Commitment map[string]any `json:"commitment"`
	}
	if err := json.NewDecoder(doneResp.Body).Decode(&donePayload); err != nil {
		t.Fatalf("decode done response: %v", err)
	}
	provenance, _ := donePayload.Commitment["provenance"].(map[string]any)
	byField, _ := provenance["by_field"].(map[string]any)
	statusSources := sortedStringList(byField["status"])
	if len(statusSources) != 1 || statusSources[0] != "receipt:"+receiptID {
		t.Fatalf("unexpected status provenance: %#v", byField["status"])
	}

	postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"decision_needed",
			"thread_id":"`+threadID+`",
			"refs":["thread:`+threadID+`"],
			"summary":"need decision",
			"payload":{},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated).Body.Close()

	postJSONExpectStatus(t, h.baseURL+"/derived/rebuild", `{"actor_id":"actor-1"}`, http.StatusOK).Body.Close()

	inboxItems := getInboxItems(t, h.baseURL)
	categories := map[string]bool{}
	for _, item := range inboxItems {
		categories[asString(item["category"])] = true
	}
	if !categories["commitment_risk"] || !categories["exception"] || !categories["decision_needed"] {
		t.Fatalf("expected inbox categories commitment_risk/exception/decision_needed, got %#v", categories)
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
			"refs":["thread:`+threadID+`"],
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

	postJSONExpectStatus(t, h.baseURL+"/threads", `{
		"actor_id":"actor-1",
		"thread":{
			"title":"Invalid strict enum",
			"type":"incident",
			"status":"not_a_real_status",
			"priority":"p1",
			"tags":[],
			"cadence":"daily",
			"next_check_in_at":"2030-01-01T00:00:00Z",
			"current_summary":"summary",
			"next_actions":[],
			"key_artifacts":[],
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusBadRequest).Body.Close()

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

	patchJSONExpectStatus(t, h.baseURL+"/threads/"+threadID, `{
		"actor_id":"actor-1",
		"patch":{"open_commitments":["x"]}
	}`, http.StatusBadRequest).Body.Close()
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
