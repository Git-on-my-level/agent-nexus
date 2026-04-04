package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestInboxDerivationAndAcknowledgmentSuppression(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	threadID := integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":            "Inbox thread",
		"type":             "incident",
		"status":           "active",
		"priority":         "p1",
		"tags":             []any{"ops"},
		"cadence":          "daily",
		"next_check_in_at": "2026-03-05T00:00:00Z",
		"current_summary":  "summary",
		"next_actions":     []any{"do x"},
		"key_artifacts":    []any{},
		"provenance":       map[string]any{"sources": []any{"inferred"}},
	})

	decisionResp := postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"decision_needed",
			"thread_id":"`+threadID+`",
			"refs":["topic:`+threadID+`"],
			"summary":"Need a decision",
			"payload":{},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer decisionResp.Body.Close()
	var createdDecision struct {
		Event map[string]any `json:"event"`
	}
	if err := json.NewDecoder(decisionResp.Body).Decode(&createdDecision); err != nil {
		t.Fatalf("decode decision event response: %v", err)
	}
	firstDecisionEventID, _ := createdDecision.Event["id"].(string)

	items := getInboxItems(t, h.baseURL)
	decisionItem, ok := findInboxItem(items, func(item map[string]any) bool {
		return asString(item["category"]) == "decision_needed" && asString(item["source_event_id"]) == firstDecisionEventID
	})
	if !ok {
		t.Fatalf("expected decision_needed inbox item for source_event_id=%s, got %#v", firstDecisionEventID, items)
	}
	firstDecisionItemID := asString(decisionItem["id"])
	if firstDecisionItemID == "" {
		t.Fatal("expected decision inbox item id")
	}

	ackPath := h.baseURL + "/inbox/" + url.PathEscape(firstDecisionItemID) + "/acknowledge"
	ackResp := postJSONExpectStatus(t, ackPath, `{
		"actor_id":"actor-1",
		"thread_id":"`+threadID+`"
	}`, http.StatusCreated)
	var acked struct {
		Event map[string]any `json:"event"`
	}
	if err := json.NewDecoder(ackResp.Body).Decode(&acked); err != nil {
		t.Fatalf("decode ack response: %v", err)
	}
	ackResp.Body.Close()
	assertActorStatementProvenance(t, acked.Event)

	itemsAfterAck := getInboxItems(t, h.baseURL)
	if _, stillThere := findInboxItem(itemsAfterAck, func(item map[string]any) bool {
		return asString(item["id"]) == firstDecisionItemID
	}); stillThere {
		t.Fatalf("expected acknowledged decision item to be suppressed, got %#v", itemsAfterAck)
	}

	secondDecisionResp := postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"decision_needed",
			"thread_id":"`+threadID+`",
			"refs":["topic:`+threadID+`"],
			"summary":"Need another decision",
			"payload":{},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer secondDecisionResp.Body.Close()
	var secondDecision struct {
		Event map[string]any `json:"event"`
	}
	if err := json.NewDecoder(secondDecisionResp.Body).Decode(&secondDecision); err != nil {
		t.Fatalf("decode second decision response: %v", err)
	}
	secondDecisionEventID, _ := secondDecision.Event["id"].(string)

	itemsAfterNewDecision := getInboxItems(t, h.baseURL)
	secondDecisionItem, ok := findInboxItem(itemsAfterNewDecision, func(item map[string]any) bool {
		return asString(item["category"]) == "decision_needed" && asString(item["source_event_id"]) == secondDecisionEventID
	})
	if !ok {
		t.Fatalf("expected new decision item after retrigger, got %#v", itemsAfterNewDecision)
	}

	// Clear decision item so work-item risk assertions are isolated.
	secondDecisionItemID := asString(secondDecisionItem["id"])
	postJSONExpectStatus(t, h.baseURL+"/inbox/ack", `{
		"actor_id":"actor-1",
		"thread_id":"`+threadID+`",
		"inbox_item_id":"`+secondDecisionItemID+`"
	}`, http.StatusCreated).Body.Close()

	createBoardResp := postJSONExpectStatus(t, h.baseURL+"/boards", `{
		"actor_id":"actor-1",
		"board":{
			"title":"Inbox board",
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
	if boardID == "" || boardUpdatedAt == "" {
		t.Fatalf("expected board id and updated_at, got %#v", createdBoard.Board)
	}

	dueSoon := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	cardResp := postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards", `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+boardUpdatedAt+`",
		"thread_id":"`+threadID+`",
		"title":"At-risk work item",
		"column_key":"ready",
		"due_at":"`+dueSoon+`"
	}`, http.StatusCreated)
	defer cardResp.Body.Close()
	var createdCard struct {
		Board map[string]any `json:"board"`
		Card  map[string]any `json:"card"`
	}
	if err := json.NewDecoder(cardResp.Body).Decode(&createdCard); err != nil {
		t.Fatalf("decode card response: %v", err)
	}
	cardID := asString(createdCard.Card["id"])
	cardBoardUpdatedAt := asString(createdCard.Board["updated_at"])
	if cardID == "" {
		t.Fatal("expected card id")
	}

	itemsWithRisk := getInboxItems(t, h.baseURL)
	riskItem, ok := findInboxItem(itemsWithRisk, func(item map[string]any) bool {
		return asString(item["category"]) == "risk_review" && asString(item["card_id"]) == cardID
	})
	if !ok {
		t.Fatalf("expected risk_review inbox item, got %#v", itemsWithRisk)
	}
	riskItemID := asString(riskItem["id"])

	postJSONExpectStatus(t, h.baseURL+"/inbox/ack", `{
		"actor_id":"actor-1",
		"thread_id":"`+threadID+`",
		"inbox_item_id":"`+riskItemID+`"
	}`, http.StatusCreated).Body.Close()

	itemsAfterRiskAck := getInboxItems(t, h.baseURL)
	if _, exists := findInboxItem(itemsAfterRiskAck, func(item map[string]any) bool {
		return asString(item["id"]) == riskItemID
	}); exists {
		t.Fatalf("expected acknowledged risk_review item to be suppressed, got %#v", itemsAfterRiskAck)
	}

	patchResp := patchJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards/"+threadID, `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+cardBoardUpdatedAt+`",
		"patch":{"title":"At-risk work item updated"}
	}`, http.StatusOK)
	patchResp.Body.Close()

	itemsAfterStatusChange := getInboxItems(t, h.baseURL)
	reappearedRisk, ok := findInboxItem(itemsAfterStatusChange, func(item map[string]any) bool {
		return asString(item["id"]) == riskItemID
	})
	if !ok {
		t.Fatalf("expected risk_review item to reappear after new trigger, got %#v", itemsAfterStatusChange)
	}
	if asString(reappearedRisk["category"]) != "risk_review" {
		t.Fatalf("unexpected reappeared risk item: %#v", reappearedRisk)
	}
}

func TestInterventionNeededDerivesInboxItem(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	threadID := integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":            "Intervention thread",
		"type":             "incident",
		"status":           "active",
		"priority":         "p1",
		"tags":             []any{"ops"},
		"cadence":          "daily",
		"next_check_in_at": "2026-03-05T00:00:00Z",
		"current_summary":  "summary",
		"next_actions":     []any{"do x"},
		"key_artifacts":    []any{},
		"provenance":       map[string]any{"sources": []any{"inferred"}},
	})

	eventResp := postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"intervention_needed",
			"thread_id":"`+threadID+`",
			"refs":["topic:`+threadID+`"],
			"summary":"Post the approved draft on LinkedIn",
			"payload":{},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer eventResp.Body.Close()

	var createdEvent struct {
		Event map[string]any `json:"event"`
	}
	if err := json.NewDecoder(eventResp.Body).Decode(&createdEvent); err != nil {
		t.Fatalf("decode intervention event response: %v", err)
	}
	eventID, _ := createdEvent.Event["id"].(string)
	if eventID == "" {
		t.Fatal("expected intervention event id")
	}

	items := getInboxItems(t, h.baseURL)
	item, ok := findInboxItem(items, func(item map[string]any) bool {
		return asString(item["category"]) == "intervention_needed" && asString(item["source_event_id"]) == eventID
	})
	if !ok {
		t.Fatalf("expected intervention_needed inbox item for source_event_id=%s, got %#v", eventID, items)
	}
	if got := asString(item["recommended_action"]); got != "take_action" {
		t.Fatalf("expected recommended_action take_action, got %#v", item)
	}
}

func TestDecisionNeedeSuppressedByDecisionMade(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	threadID := integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":            "Decision suppression thread",
		"type":             "incident",
		"status":           "active",
		"priority":         "p1",
		"tags":             []any{"ops"},
		"cadence":          "daily",
		"next_check_in_at": "2026-03-05T00:00:00Z",
		"current_summary":  "summary",
		"next_actions":     []any{"do x"},
		"key_artifacts":    []any{},
		"provenance":       map[string]any{"sources": []any{"inferred"}},
	})

	// Emit decision_needed — should appear in inbox.
	dnResp := postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"decision_needed",
			"thread_id":"`+threadID+`",
			"refs":["topic:`+threadID+`"],
			"summary":"Approve customer refunds",
			"payload":{},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer dnResp.Body.Close()

	items := getInboxItems(t, h.baseURL)
	decisionItem, ok := findInboxItem(items, func(item map[string]any) bool {
		return asString(item["category"]) == "decision_needed" && asString(item["thread_id"]) == threadID
	})
	if !ok {
		t.Fatalf("expected decision_needed inbox item, got %#v", items)
	}
	inboxItemID := asString(decisionItem["id"])
	if inboxItemID == "" {
		t.Fatal("expected inbox item id")
	}

	// Record decision_made referencing the inbox item — should suppress the inbox item.
	dmResp := postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"decision_made",
			"thread_id":"`+threadID+`",
			"refs":["topic:`+threadID+`","inbox:`+inboxItemID+`"],
			"summary":"Approved emergency refunds",
			"payload":{"notes":""},
			"provenance":{"sources":["actor_statement:ui"]}
		}
	}`, http.StatusCreated)
	dmResp.Body.Close()

	itemsAfterDecision := getInboxItems(t, h.baseURL)
	if _, stillThere := findInboxItem(itemsAfterDecision, func(item map[string]any) bool {
		return asString(item["id"]) == inboxItemID
	}); stillThere {
		t.Fatalf("expected decision_needed inbox item to be suppressed after decision_made, got %#v", itemsAfterDecision)
	}

	// A new decision_needed on the same thread should still appear (no over-suppression).
	dn2Resp := postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"decision_needed",
			"thread_id":"`+threadID+`",
			"refs":["topic:`+threadID+`"],
			"summary":"Another decision needed",
			"payload":{},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer dn2Resp.Body.Close()

	itemsAfterRetrigger := getInboxItems(t, h.baseURL)
	if _, ok := findInboxItem(itemsAfterRetrigger, func(item map[string]any) bool {
		return asString(item["category"]) == "decision_needed" && asString(item["thread_id"]) == threadID
	}); !ok {
		t.Fatalf("expected new decision_needed inbox item after retrigger, got %#v", itemsAfterRetrigger)
	}
}

func TestGetInboxItemDetailByID(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	threadID := integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":            "Inbox detail thread",
		"type":             "incident",
		"status":           "active",
		"priority":         "p1",
		"tags":             []any{"ops"},
		"cadence":          "daily",
		"next_check_in_at": "2026-03-05T00:00:00Z",
		"current_summary":  "summary",
		"next_actions":     []any{"do x"},
		"key_artifacts":    []any{},
		"provenance":       map[string]any{"sources": []any{"inferred"}},
	})

	eventResp := postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"decision_needed",
			"thread_id":"`+threadID+`",
			"refs":["topic:`+threadID+`"],
			"summary":"Need a decision",
			"payload":{},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer eventResp.Body.Close()

	items := getInboxItems(t, h.baseURL)
	if len(items) == 0 {
		t.Fatalf("expected inbox items, got %#v", items)
	}
	inboxItemID := asString(items[0]["id"])
	if inboxItemID == "" {
		t.Fatalf("expected inbox item id, got %#v", items[0])
	}

	resp, err := http.Get(h.baseURL + "/inbox/" + url.PathEscape(inboxItemID))
	if err != nil {
		t.Fatalf("GET /inbox/{id}: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected GET /inbox/{id} status: %d", resp.StatusCode)
	}

	var payload struct {
		Item        map[string]any `json:"item"`
		GeneratedAt string         `json:"generated_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode /inbox/{id} response: %v", err)
	}
	if got := asString(payload.Item["id"]); got != inboxItemID {
		t.Fatalf("expected inbox item id %q, got %q payload=%#v", inboxItemID, got, payload)
	}
	if payload.GeneratedAt == "" {
		t.Fatalf("expected generated_at in response payload=%#v", payload)
	}

	missingResp, err := http.Get(h.baseURL + "/inbox/" + url.PathEscape("inbox:missing:item"))
	if err != nil {
		t.Fatalf("GET /inbox/{id} missing: %v", err)
	}
	defer missingResp.Body.Close()
	if missingResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for missing inbox item, got %d", missingResp.StatusCode)
	}
}

func TestInboxCustomRiskHorizonRetainsStaleExceptions(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	threadID := integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":            "Stale inbox thread",
		"type":             "incident",
		"status":           "active",
		"priority":         "p1",
		"tags":             []any{"ops"},
		"cadence":          "daily",
		"next_check_in_at": "2026-03-05T00:00:00Z",
		"current_summary":  "summary",
		"next_actions":     []any{"follow up"},
		"key_artifacts":    []any{},
		"provenance":       map[string]any{"sources": []any{"inferred"}},
	})

	resp, err := http.Get(h.baseURL + "/inbox?risk_horizon_days=30")
	if err != nil {
		t.Fatalf("GET /inbox?risk_horizon_days=30: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected GET /inbox?risk_horizon_days=30 status: %d", resp.StatusCode)
	}

	var payload struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode custom-horizon inbox response: %v", err)
	}

	staleItem, ok := findInboxItem(payload.Items, func(item map[string]any) bool {
		return asString(item["category"]) == "stale_topic" && asString(item["thread_id"]) == threadID
	})
	if !ok {
		t.Fatalf("expected stale exception on custom-horizon inbox read, got %#v", payload.Items)
	}

	inboxItemID := asString(staleItem["id"])
	if inboxItemID == "" {
		t.Fatalf("expected stale inbox item id, got %#v", staleItem)
	}

	detailResp, err := http.Get(h.baseURL + "/inbox/" + url.PathEscape(inboxItemID) + "?risk_horizon_days=30")
	if err != nil {
		t.Fatalf("GET /inbox/{id}?risk_horizon_days=30: %v", err)
	}
	defer detailResp.Body.Close()
	if detailResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected GET /inbox/{id}?risk_horizon_days=30 status: %d", detailResp.StatusCode)
	}

	var detailPayload struct {
		Item map[string]any `json:"item"`
	}
	if err := json.NewDecoder(detailResp.Body).Decode(&detailPayload); err != nil {
		t.Fatalf("decode custom-horizon inbox item response: %v", err)
	}
	if got := asString(detailPayload.Item["id"]); got != inboxItemID {
		t.Fatalf("expected stale inbox item id %q, got %q payload=%#v", inboxItemID, got, detailPayload)
	}
}

func getInboxItems(t *testing.T, baseURL string) []map[string]any {
	t.Helper()
	resp, err := http.Get(baseURL + "/inbox")
	if err != nil {
		t.Fatalf("GET /inbox: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected GET /inbox status: %d", resp.StatusCode)
	}

	var payload struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode /inbox response: %v", err)
	}
	return payload.Items
}

func findInboxItem(items []map[string]any, predicate func(map[string]any) bool) (map[string]any, bool) {
	for _, item := range items {
		if predicate(item) {
			return item, true
		}
	}
	return nil, false
}

func asString(raw any) string {
	text, _ := raw.(string)
	return text
}
