package server

import (
	"encoding/json"
	"net/http"
	"reflect"
	"sort"
	"testing"
)

func TestCommitmentsCreateAndRestrictedTransitions(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	threadResp := postJSONExpectStatus(t, h.baseURL+"/threads", `{
		"actor_id":"actor-1",
		"thread":{
			"title":"Commitment test thread",
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
		t.Fatalf("decode created thread: %v", err)
	}
	threadID, _ := createdThread.Thread["id"].(string)
	if threadID == "" {
		t.Fatal("expected thread id")
	}

	commitmentResp := postJSONExpectStatus(t, h.baseURL+"/commitments", `{
		"actor_id":"actor-1",
		"commitment":{
			"thread_id":"`+threadID+`",
			"title":"Ship fix",
			"owner":"actor-1",
			"due_at":"2026-03-08T00:00:00Z",
			"status":"open",
			"definition_of_done":["merged"],
			"links":["url:https://example.com/work"],
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer commitmentResp.Body.Close()

	var createdCommitment struct {
		Commitment map[string]any `json:"commitment"`
	}
	if err := json.NewDecoder(commitmentResp.Body).Decode(&createdCommitment); err != nil {
		t.Fatalf("decode created commitment: %v", err)
	}
	commitmentID, _ := createdCommitment.Commitment["id"].(string)
	if commitmentID == "" {
		t.Fatal("expected commitment id")
	}

	threadAfterCreateResp, err := http.Get(h.baseURL + "/threads/" + threadID)
	if err != nil {
		t.Fatalf("GET /threads/{id}: %v", err)
	}
	defer threadAfterCreateResp.Body.Close()
	if threadAfterCreateResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected get thread status: got %d", threadAfterCreateResp.StatusCode)
	}

	var threadAfterCreate struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(threadAfterCreateResp.Body).Decode(&threadAfterCreate); err != nil {
		t.Fatalf("decode thread after create: %v", err)
	}
	openAfterCreate := sortedStringList(threadAfterCreate.Thread["open_commitments"])
	if !reflect.DeepEqual(openAfterCreate, []string{commitmentID}) {
		t.Fatalf("unexpected open_commitments after create: %#v", threadAfterCreate.Thread["open_commitments"])
	}

	rejectDoneResp := patchJSONExpectStatus(t, h.baseURL+"/commitments/"+commitmentID, `{
		"actor_id":"actor-1",
		"patch":{"status":"done"}
	}`, http.StatusBadRequest)
	defer rejectDoneResp.Body.Close()

	workOrderID := "work-order-for-commitment"
	workOrderResp := postJSONExpectStatus(t, h.baseURL+"/work_orders", `{
		"actor_id":"actor-1",
		"artifact":{
			"id":"`+workOrderID+`",
			"refs":["topic:`+threadID+`"],
			"summary":"work order"
		},
		"packet":{
			"work_order_id":"`+workOrderID+`",
			"subject_ref":"thread:`+threadID+`",
			"objective":"fix issue",
			"constraints":["none"],
			"context_refs":["url:https://example.com/context"],
			"acceptance_criteria":["issue fixed"],
			"definition_of_done":["receipt created"]
		}
	}`, http.StatusCreated)
	defer workOrderResp.Body.Close()

	receiptID := "receipt-for-commitment"
	receiptResp := postJSONExpectStatus(t, h.baseURL+"/receipts", `{
		"actor_id":"actor-1",
		"artifact":{
			"id":"`+receiptID+`",
			"refs":["thread:`+threadID+`","artifact:`+workOrderID+`"],
			"summary":"receipt"
		},
		"packet":{
			"receipt_id":"`+receiptID+`",
			"work_order_id":"`+workOrderID+`",
			"subject_ref":"thread:`+threadID+`",
			"outputs":["artifact:deliverable-1"],
			"verification_evidence":["url:https://example.com/evidence"],
			"changes_summary":"summary",
			"known_gaps":[]
		}
	}`, http.StatusCreated)
	defer receiptResp.Body.Close()

	var receiptPayload struct {
		Artifact map[string]any `json:"artifact"`
	}
	if err := json.NewDecoder(receiptResp.Body).Decode(&receiptPayload); err != nil {
		t.Fatalf("decode receipt artifact: %v", err)
	}
	createdReceiptID, _ := receiptPayload.Artifact["id"].(string)
	if createdReceiptID == "" {
		t.Fatal("expected receipt id")
	}

	doneResp := patchJSONExpectStatus(t, h.baseURL+"/commitments/"+commitmentID, `{
		"actor_id":"actor-1",
		"patch":{"status":"done"},
		"refs":["artifact:`+createdReceiptID+`"]
	}`, http.StatusOK)
	defer doneResp.Body.Close()

	var patchedDone struct {
		Commitment map[string]any `json:"commitment"`
	}
	if err := json.NewDecoder(doneResp.Body).Decode(&patchedDone); err != nil {
		t.Fatalf("decode done commitment: %v", err)
	}
	provenance, ok := patchedDone.Commitment["provenance"].(map[string]any)
	if !ok {
		t.Fatalf("expected commitment provenance, got %#v", patchedDone.Commitment["provenance"])
	}
	byField, ok := provenance["by_field"].(map[string]any)
	if !ok {
		t.Fatalf("expected provenance.by_field, got %#v", provenance["by_field"])
	}
	statusSources := sortedStringList(byField["status"])
	if !reflect.DeepEqual(statusSources, []string{"receipt:" + createdReceiptID}) {
		t.Fatalf("unexpected status provenance labels: %#v", byField["status"])
	}

	threadAfterDoneResp, err := http.Get(h.baseURL + "/threads/" + threadID)
	if err != nil {
		t.Fatalf("GET /threads/{id} after done: %v", err)
	}
	defer threadAfterDoneResp.Body.Close()
	if threadAfterDoneResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected get thread status after done: got %d", threadAfterDoneResp.StatusCode)
	}
	var threadAfterDone struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(threadAfterDoneResp.Body).Decode(&threadAfterDone); err != nil {
		t.Fatalf("decode thread after done: %v", err)
	}
	if open := sortedStringList(threadAfterDone.Thread["open_commitments"]); len(open) != 0 {
		t.Fatalf("expected open_commitments empty after done, got %#v", threadAfterDone.Thread["open_commitments"])
	}

	secondCommitmentResp := postJSONExpectStatus(t, h.baseURL+"/commitments", `{
		"actor_id":"actor-1",
		"commitment":{
			"thread_id":"`+threadID+`",
			"title":"Decide cancellation",
			"owner":"actor-1",
			"due_at":"2026-03-09T00:00:00Z",
			"status":"open",
			"definition_of_done":["decided"],
			"links":["url:https://example.com/decision"],
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer secondCommitmentResp.Body.Close()

	var secondCommitmentPayload struct {
		Commitment map[string]any `json:"commitment"`
	}
	if err := json.NewDecoder(secondCommitmentResp.Body).Decode(&secondCommitmentPayload); err != nil {
		t.Fatalf("decode second commitment: %v", err)
	}
	secondCommitmentID, _ := secondCommitmentPayload.Commitment["id"].(string)
	if secondCommitmentID == "" {
		t.Fatal("expected second commitment id")
	}

	decisionEventResp := postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"decision_made",
			"thread_id":"`+threadID+`",
			"refs":["topic:`+threadID+`"],
			"summary":"decision",
			"payload":{"outcome":"cancel"},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer decisionEventResp.Body.Close()

	var decisionPayload struct {
		Event map[string]any `json:"event"`
	}
	if err := json.NewDecoder(decisionEventResp.Body).Decode(&decisionPayload); err != nil {
		t.Fatalf("decode decision event: %v", err)
	}
	decisionEventID, _ := decisionPayload.Event["id"].(string)
	if decisionEventID == "" {
		t.Fatal("expected decision event id")
	}

	canceledResp := patchJSONExpectStatus(t, h.baseURL+"/commitments/"+secondCommitmentID, `{
		"actor_id":"actor-1",
		"patch":{"status":"canceled"},
		"refs":["event:`+decisionEventID+`"]
	}`, http.StatusOK)
	defer canceledResp.Body.Close()

	threadAfterCanceledResp, err := http.Get(h.baseURL + "/threads/" + threadID)
	if err != nil {
		t.Fatalf("GET /threads/{id} after canceled: %v", err)
	}
	defer threadAfterCanceledResp.Body.Close()
	if threadAfterCanceledResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected get thread status after canceled: got %d", threadAfterCanceledResp.StatusCode)
	}
	var threadAfterCanceled struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(threadAfterCanceledResp.Body).Decode(&threadAfterCanceled); err != nil {
		t.Fatalf("decode thread after canceled: %v", err)
	}
	if open := sortedStringList(threadAfterCanceled.Thread["open_commitments"]); len(open) != 0 {
		t.Fatalf("expected open_commitments empty after canceled, got %#v", threadAfterCanceled.Thread["open_commitments"])
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

	createdEvent := findEventByType(timeline.Events, "commitment_created")
	if createdEvent == nil {
		t.Fatal("expected commitment_created in timeline")
	}
	assertActorStatementProvenance(t, createdEvent)

	statusChangedEvent := findEventByType(timeline.Events, "commitment_status_changed")
	if statusChangedEvent == nil {
		t.Fatal("expected commitment_status_changed in timeline")
	}
	assertActorStatementProvenance(t, statusChangedEvent)
	if hasOpenCommitmentMaintenanceEvent(timeline.Events) {
		t.Fatalf("did not expect derived open_commitments maintenance event in timeline: %#v", timeline.Events)
	}
}

func TestCommitmentCreateRequestKeyReplaysSingleWrite(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	threadResp := postJSONExpectStatus(t, h.baseURL+"/threads", `{
		"actor_id":"actor-1",
		"thread":{
			"title":"Commitment replay thread",
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
		t.Fatalf("decode created thread: %v", err)
	}
	threadID, _ := createdThread.Thread["id"].(string)
	if threadID == "" {
		t.Fatal("expected thread id")
	}

	body := `{
		"actor_id":"actor-1",
		"request_key":"replay-commitment",
		"commitment":{
			"thread_id":"` + threadID + `",
			"title":"Ship fix",
			"owner":"actor-1",
			"due_at":"2026-03-08T00:00:00Z",
			"status":"open",
			"definition_of_done":["merged"],
			"links":["url:https://example.com/work"],
			"provenance":{"sources":["inferred"]}
		}
	}`

	firstResp := postJSONExpectStatus(t, h.baseURL+"/commitments", body, http.StatusCreated)
	defer firstResp.Body.Close()
	secondResp := postJSONExpectStatus(t, h.baseURL+"/commitments", body, http.StatusCreated)
	defer secondResp.Body.Close()

	var firstPayload struct {
		Commitment map[string]any `json:"commitment"`
	}
	if err := json.NewDecoder(firstResp.Body).Decode(&firstPayload); err != nil {
		t.Fatalf("decode first commitment response: %v", err)
	}
	var secondPayload struct {
		Commitment map[string]any `json:"commitment"`
	}
	if err := json.NewDecoder(secondResp.Body).Decode(&secondPayload); err != nil {
		t.Fatalf("decode second commitment response: %v", err)
	}
	if secondPayload.Commitment["id"] != firstPayload.Commitment["id"] {
		t.Fatalf("expected replayed commitment id %#v, got %#v", firstPayload.Commitment["id"], secondPayload.Commitment["id"])
	}

	threadAfterResp, err := http.Get(h.baseURL + "/threads/" + threadID)
	if err != nil {
		t.Fatalf("GET /threads/{id}: %v", err)
	}
	defer threadAfterResp.Body.Close()
	var threadAfter struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(threadAfterResp.Body).Decode(&threadAfter); err != nil {
		t.Fatalf("decode thread after commitment replay: %v", err)
	}
	if open := sortedStringList(threadAfter.Thread["open_commitments"]); len(open) != 1 {
		t.Fatalf("expected one open commitment after replay, got %#v", threadAfter.Thread["open_commitments"])
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
	if commitmentEventCount := countTimelineEvents(timeline.Events, "commitment_created"); commitmentEventCount != 1 {
		t.Fatalf("expected one commitment_created event, got %d", commitmentEventCount)
	}
	if hasOpenCommitmentMaintenanceEvent(timeline.Events) {
		t.Fatalf("did not expect derived open_commitments maintenance event in replay timeline: %#v", timeline.Events)
	}
}

func countTimelineEvents(events []map[string]any, eventType string) int {
	count := 0
	for _, event := range events {
		if asString(event["type"]) == eventType {
			count++
		}
	}
	return count
}

func hasOpenCommitmentMaintenanceEvent(events []map[string]any) bool {
	for _, event := range events {
		if asString(event["type"]) != "snapshot_updated" {
			continue
		}
		payload, _ := event["payload"].(map[string]any)
		changedFields := sortedStringList(payload["changed_fields"])
		if len(changedFields) == 1 && changedFields[0] == "open_commitments" {
			return true
		}
	}
	return false
}

func TestPatchCommitmentIfUpdatedAtOptimisticLocking(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	threadResp := postJSONExpectStatus(t, h.baseURL+"/threads", `{
		"actor_id":"actor-1",
		"thread":{
			"title":"Commitment lock thread",
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
		t.Fatalf("decode created thread: %v", err)
	}
	threadID, _ := createdThread.Thread["id"].(string)
	if threadID == "" {
		t.Fatal("expected thread id")
	}

	commitmentResp := postJSONExpectStatus(t, h.baseURL+"/commitments", `{
		"actor_id":"actor-1",
		"commitment":{
			"thread_id":"`+threadID+`",
			"title":"Lock me",
			"owner":"actor-1",
			"due_at":"2026-03-08T00:00:00Z",
			"status":"open",
			"definition_of_done":["merged"],
			"links":["url:https://example.com/work"],
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer commitmentResp.Body.Close()

	var createdCommitment struct {
		Commitment map[string]any `json:"commitment"`
	}
	if err := json.NewDecoder(commitmentResp.Body).Decode(&createdCommitment); err != nil {
		t.Fatalf("decode created commitment: %v", err)
	}
	commitmentID, _ := createdCommitment.Commitment["id"].(string)
	initialUpdatedAt, _ := createdCommitment.Commitment["updated_at"].(string)
	if commitmentID == "" || initialUpdatedAt == "" {
		t.Fatalf("expected commitment id and updated_at, got id=%q updated_at=%q", commitmentID, initialUpdatedAt)
	}

	matchedResp := patchJSONExpectStatus(t, h.baseURL+"/commitments/"+commitmentID, `{
		"actor_id":"actor-1",
		"if_updated_at":"`+initialUpdatedAt+`",
		"patch":{"title":"Lock me matched"}
	}`, http.StatusOK)
	defer matchedResp.Body.Close()

	var matched struct {
		Commitment map[string]any `json:"commitment"`
	}
	if err := json.NewDecoder(matchedResp.Body).Decode(&matched); err != nil {
		t.Fatalf("decode matched commitment patch: %v", err)
	}
	if matched.Commitment["title"] != "Lock me matched" {
		t.Fatalf("unexpected matched commitment title: %#v", matched.Commitment["title"])
	}

	timelineBeforeResp, err := http.Get(h.baseURL + "/threads/" + threadID + "/timeline")
	if err != nil {
		t.Fatalf("GET timeline before commitment conflict: %v", err)
	}
	defer timelineBeforeResp.Body.Close()
	if timelineBeforeResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected timeline before commitment conflict status: %d", timelineBeforeResp.StatusCode)
	}
	var timelineBefore struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.NewDecoder(timelineBeforeResp.Body).Decode(&timelineBefore); err != nil {
		t.Fatalf("decode timeline before commitment conflict: %v", err)
	}

	conflictResp := patchJSONExpectStatus(t, h.baseURL+"/commitments/"+commitmentID, `{
		"actor_id":"actor-1",
		"if_updated_at":"`+initialUpdatedAt+`",
		"patch":{"title":"Lock me stale"}
	}`, http.StatusConflict)
	defer conflictResp.Body.Close()

	var conflictBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(conflictResp.Body).Decode(&conflictBody); err != nil {
		t.Fatalf("decode commitment conflict response: %v", err)
	}
	if conflictBody.Error.Code != "conflict" {
		t.Fatalf("unexpected commitment conflict code: %#v", conflictBody.Error.Code)
	}

	timelineAfterResp, err := http.Get(h.baseURL + "/threads/" + threadID + "/timeline")
	if err != nil {
		t.Fatalf("GET timeline after commitment conflict: %v", err)
	}
	defer timelineAfterResp.Body.Close()
	if timelineAfterResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected timeline after commitment conflict status: %d", timelineAfterResp.StatusCode)
	}
	var timelineAfter struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.NewDecoder(timelineAfterResp.Body).Decode(&timelineAfter); err != nil {
		t.Fatalf("decode timeline after commitment conflict: %v", err)
	}
	if len(timelineAfter.Events) != len(timelineBefore.Events) {
		t.Fatalf("commitment conflict patch emitted event: before=%d after=%d", len(timelineBefore.Events), len(timelineAfter.Events))
	}

	getResp := requestJSONExpectStatus(t, http.MethodGet, h.baseURL+"/commitments/"+commitmentID, "", http.StatusOK)
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected get commitment status after conflict: %d", getResp.StatusCode)
	}
	var loaded struct {
		Commitment map[string]any `json:"commitment"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&loaded); err != nil {
		t.Fatalf("decode commitment after conflict: %v", err)
	}
	if loaded.Commitment["title"] != "Lock me matched" {
		t.Fatalf("commitment changed despite conflict: %#v", loaded.Commitment["title"])
	}

	noLockResp := patchJSONExpectStatus(t, h.baseURL+"/commitments/"+commitmentID, `{
		"actor_id":"actor-1",
		"patch":{"owner":"actor-2"}
	}`, http.StatusOK)
	defer noLockResp.Body.Close()
}

func sortedStringList(raw any) []string {
	out := make([]string, 0)
	switch values := raw.(type) {
	case []string:
		out = append(out, values...)
	case []any:
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				continue
			}
			out = append(out, text)
		}
	}
	sort.Strings(out)
	return out
}
