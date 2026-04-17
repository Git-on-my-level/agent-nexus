package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
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
			"refs":["thread:`+threadID+`"],
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
		return asString(item["category"]) == "action_needed" && asString(item["source_event_id"]) == firstDecisionEventID
	})
	if !ok {
		t.Fatalf("expected action_needed inbox item for source_event_id=%s, got %#v", firstDecisionEventID, items)
	}
	firstDecisionItemID := asString(decisionItem["id"])
	if firstDecisionItemID == "" {
		t.Fatal("expected decision inbox item id")
	}

	ackPath := h.baseURL + "/inbox/" + url.PathEscape(firstDecisionItemID) + "/acknowledge"
	ackResp := postJSONExpectStatus(t, ackPath, `{
		"actor_id":"actor-1",
		"subject_ref":"thread:`+threadID+`"
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
			"refs":["thread:`+threadID+`"],
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
		return asString(item["category"]) == "action_needed" && asString(item["source_event_id"]) == secondDecisionEventID
	})
	if !ok {
		t.Fatalf("expected new decision item after retrigger, got %#v", itemsAfterNewDecision)
	}

	// Clear decision item so work-item risk assertions are isolated.
	secondDecisionItemID := asString(secondDecisionItem["id"])
	postJSONExpectStatus(t, h.baseURL+"/inbox/"+url.PathEscape(secondDecisionItemID)+"/acknowledge", `{
		"actor_id":"actor-1",
		"subject_ref":"thread:`+threadID+`"
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
		"title":"At-risk work item",
		"related_refs":["thread:`+threadID+`"],
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
	cardUpdatedAt := asString(createdCard.Card["updated_at"])
	if cardID == "" || cardUpdatedAt == "" {
		t.Fatal("expected card id and updated_at")
	}

	itemsWithRisk := getInboxItems(t, h.baseURL)
	riskItem, ok := findInboxItem(itemsWithRisk, func(item map[string]any) bool {
		return asString(item["category"]) == "risk_exception" && asString(item["card_id"]) == cardID
	})
	if !ok {
		t.Fatalf("expected risk_exception inbox item, got %#v", itemsWithRisk)
	}
	riskItemID := asString(riskItem["id"])

	postJSONExpectStatus(t, h.baseURL+"/inbox/"+url.PathEscape(riskItemID)+"/acknowledge", `{
		"actor_id":"actor-1",
		"subject_ref":"thread:`+threadID+`"
	}`, http.StatusCreated).Body.Close()

	itemsAfterRiskAck := getInboxItems(t, h.baseURL)
	if _, exists := findInboxItem(itemsAfterRiskAck, func(item map[string]any) bool {
		return asString(item["id"]) == riskItemID
	}); exists {
		t.Fatalf("expected acknowledged risk_exception item to be suppressed, got %#v", itemsAfterRiskAck)
	}

	patchResp := patchJSONExpectStatus(t, h.baseURL+"/cards/"+cardID, `{
		"actor_id":"actor-1",
		"if_updated_at":"`+cardUpdatedAt+`",
		"patch":{"title":"At-risk work item updated"}
	}`, http.StatusOK)
	patchResp.Body.Close()

	itemsAfterStatusChange := getInboxItems(t, h.baseURL)
	reappearedRisk, ok := findInboxItem(itemsAfterStatusChange, func(item map[string]any) bool {
		return asString(item["id"]) == riskItemID
	})
	if !ok {
		t.Fatalf("expected risk_exception item to reappear after new trigger, got %#v", itemsAfterStatusChange)
	}
	if asString(reappearedRisk["category"]) != "risk_exception" {
		t.Fatalf("unexpected reappeared risk item: %#v", reappearedRisk)
	}
}

func TestInboxAcknowledgmentResolvesTopicSubjectRefToBackingThread(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createTopicResp := postJSONExpectStatus(t, h.baseURL+"/topics", `{
		"actor_id":"actor-1",
		"topic":{
			"type":"initiative",
			"status":"active",
			"title":"Ack subject topic",
			"summary":"Topic for ack resolution",
			"owner_refs":["actor:actor-1"],
			"document_refs":[],
			"board_refs":[],
			"related_refs":[],
			"provenance":{"sources":["seed:inbox-ack-subject"]}
		}
	}`, http.StatusCreated)
	defer createTopicResp.Body.Close()

	var createdTopic struct {
		Topic map[string]any `json:"topic"`
	}
	if err := json.NewDecoder(createTopicResp.Body).Decode(&createdTopic); err != nil {
		t.Fatalf("decode create topic response: %v", err)
	}
	topicID := asString(createdTopic.Topic["id"])
	backingThreadID := asString(createdTopic.Topic["thread_id"])
	if topicID == "" || backingThreadID == "" {
		t.Fatalf("expected topic id and thread_id, got %#v", createdTopic.Topic)
	}
	if topicID == backingThreadID {
		t.Fatalf("expected topic id to differ from backing thread id for this test, got topic=%q thread=%q", topicID, backingThreadID)
	}

	decisionResp := postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"decision_needed",
			"thread_id":"`+backingThreadID+`",
			"refs":["thread:`+backingThreadID+`","topic:`+topicID+`"],
			"summary":"Need a decision",
			"payload":{},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer decisionResp.Body.Close()

	items := getInboxItems(t, h.baseURL)
	decisionItem, ok := findInboxItem(items, func(item map[string]any) bool {
		return asString(item["category"]) == "action_needed" && asString(item["thread_id"]) == backingThreadID
	})
	if !ok {
		t.Fatalf("expected decision inbox item, got %#v", items)
	}
	inboxItemID := asString(decisionItem["id"])

	ackResp := postJSONExpectStatus(t, h.baseURL+"/inbox/"+url.PathEscape(inboxItemID)+"/acknowledge", `{
		"actor_id":"actor-1",
		"subject_ref":"topic:`+topicID+`"
	}`, http.StatusCreated)
	var acked struct {
		Event map[string]any `json:"event"`
	}
	if err := json.NewDecoder(ackResp.Body).Decode(&acked); err != nil {
		t.Fatalf("decode ack response: %v", err)
	}
	ackResp.Body.Close()

	if got := asString(acked.Event["thread_id"]); got != backingThreadID {
		t.Fatalf("expected ack event thread_id=%q (backing thread), got %q", backingThreadID, got)
	}
}

func TestInboxAcknowledgmentResolvesCardSubjectRefViaCardRelatedThread(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	memberThreadID := createBoardThreadViaHTTP(t, h, "Card member thread")

	createBoardResp := postJSONExpectStatus(t, h.baseURL+"/boards", `{
		"actor_id":"actor-1",
		"board":{
			"title":"Card ack board",
			"refs":[]
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
		"title":"Ack card by subject_ref",
		"related_refs":["thread:`+memberThreadID+`"],
		"column_key":"ready",
		"due_at":"`+dueSoon+`"
	}`, http.StatusCreated)
	defer cardResp.Body.Close()

	var createdCard struct {
		Card map[string]any `json:"card"`
	}
	if err := json.NewDecoder(cardResp.Body).Decode(&createdCard); err != nil {
		t.Fatalf("decode card response: %v", err)
	}
	cardID := asString(createdCard.Card["id"])
	if cardID == "" {
		t.Fatal("expected card id")
	}

	items := getInboxItems(t, h.baseURL)
	riskItem, ok := findInboxItem(items, func(item map[string]any) bool {
		return asString(item["category"]) == "risk_exception" && asString(item["card_id"]) == cardID
	})
	if !ok {
		t.Fatalf("expected risk_exception inbox item, got %#v", items)
	}
	inboxItemID := asString(riskItem["id"])

	ackResp := postJSONExpectStatus(t, h.baseURL+"/inbox/"+url.PathEscape(inboxItemID)+"/acknowledge", `{
		"actor_id":"actor-1",
		"subject_ref":"card:`+cardID+`"
	}`, http.StatusCreated)
	defer ackResp.Body.Close()

	var acked struct {
		Event map[string]any `json:"event"`
	}
	if err := json.NewDecoder(ackResp.Body).Decode(&acked); err != nil {
		t.Fatalf("decode ack response: %v", err)
	}
	if got := asString(acked.Event["thread_id"]); got != memberThreadID {
		t.Fatalf("expected ack event thread_id=%q from card related thread, got %q", memberThreadID, got)
	}
}

func TestLegacyRiskReviewAckStillSuppressesWorkItemRiskAfterRebuild(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	threadID := integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":            "Legacy risk ack thread",
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

	createBoardResp := postJSONExpectStatus(t, h.baseURL+"/boards", `{
		"actor_id":"actor-1",
		"board":{
			"title":"Legacy risk ack board",
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

	dueSoon := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	cardResp := postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards", `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+boardUpdatedAt+`",
		"title":"Legacy-acked work item",
		"related_refs":["thread:`+threadID+`"],
		"column_key":"ready",
		"due_at":"`+dueSoon+`"
	}`, http.StatusCreated)
	defer cardResp.Body.Close()
	var createdCard struct {
		Card map[string]any `json:"card"`
	}
	if err := json.NewDecoder(cardResp.Body).Decode(&createdCard); err != nil {
		t.Fatalf("decode card response: %v", err)
	}
	cardID := asString(createdCard.Card["id"])
	if cardID == "" {
		t.Fatal("expected card id")
	}

	itemsWithRisk := getInboxItems(t, h.baseURL)
	riskItem, ok := findInboxItem(itemsWithRisk, func(item map[string]any) bool {
		return asString(item["category"]) == "risk_exception" && asString(item["card_id"]) == cardID
	})
	if !ok {
		t.Fatalf("expected risk_exception inbox item, got %#v", itemsWithRisk)
	}
	canonicalRiskID := asString(riskItem["id"])
	legacyRiskID := makeInboxItemID("risk_review", threadID, cardID, "")

	postJSONExpectStatus(t, h.baseURL+"/inbox/"+url.PathEscape(legacyRiskID)+"/acknowledge", `{
		"actor_id":"actor-1",
		"subject_ref":"thread:`+threadID+`"
	}`, http.StatusCreated).Body.Close()

	postJSONExpectStatus(t, h.baseURL+"/derived/rebuild", `{"actor_id":"actor-1"}`, http.StatusOK).Body.Close()

	itemsAfterAckAndRebuild := getInboxItems(t, h.baseURL)
	if _, exists := findInboxItem(itemsAfterAckAndRebuild, func(item map[string]any) bool {
		return asString(item["id"]) == canonicalRiskID || (asString(item["category"]) == "risk_exception" && asString(item["card_id"]) == cardID)
	}); exists {
		t.Fatalf("expected legacy risk_review ack to suppress canonical risk_exception item after rebuild, got %#v", itemsAfterAckAndRebuild)
	}
}

func TestInboxAcknowledgmentRejectsTopicSubjectRefWhenNoTopicRow(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	threadID := integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":            "Thread-only inbox ack seed",
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

	postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"decision_needed",
			"thread_id":"`+threadID+`",
			"refs":["thread:`+threadID+`"],
			"summary":"Need a decision",
			"payload":{},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated).Body.Close()

	items := getInboxItems(t, h.baseURL)
	decisionItem, ok := findInboxItem(items, func(item map[string]any) bool {
		return asString(item["category"]) == "action_needed" && asString(item["thread_id"]) == threadID
	})
	if !ok {
		t.Fatalf("expected decision inbox item, got %#v", items)
	}
	inboxItemID := asString(decisionItem["id"])

	ackResp := postJSONExpectStatus(t, h.baseURL+"/inbox/"+url.PathEscape(inboxItemID)+"/acknowledge", `{
		"actor_id":"actor-1",
		"subject_ref":"topic:`+threadID+`"
	}`, http.StatusBadRequest)
	defer ackResp.Body.Close()
	assertErrorCode(t, ackResp, "invalid_request")
}

func TestInboxAcknowledgmentRejectsLegacyThreadIDBody(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	threadID := integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":            "Reject legacy ack body",
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

	postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"decision_needed",
			"thread_id":"`+threadID+`",
			"refs":["thread:`+threadID+`"],
			"summary":"Need a decision",
			"payload":{},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated).Body.Close()

	items := getInboxItems(t, h.baseURL)
	decisionItem, ok := findInboxItem(items, func(item map[string]any) bool {
		return asString(item["category"]) == "action_needed" && asString(item["thread_id"]) == threadID
	})
	if !ok {
		t.Fatalf("expected decision inbox item, got %#v", items)
	}
	inboxItemID := asString(decisionItem["id"])

	resp := postJSONExpectStatus(t, h.baseURL+"/inbox/"+url.PathEscape(inboxItemID)+"/acknowledge", `{
		"actor_id":"actor-1",
		"thread_id":"`+threadID+`"
	}`, http.StatusBadRequest)
	defer resp.Body.Close()
	assertErrorCode(t, resp, "invalid_request")
}

func TestInboxAcknowledgmentRejectsBoardSubjectRefWithoutBackingThread(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated).Body.Close()

	primaryThreadID := createBoardThreadViaHTTP(t, h, "Board ack thread")
	createBoardResp := postJSONExpectStatus(t, h.baseURL+"/boards", `{
		"actor_id":"actor-1",
		"board":{
			"title":"Ack board",
			"refs":["thread:`+primaryThreadID+`"]
		}
	}`, http.StatusCreated)
	defer createBoardResp.Body.Close()

	var createdBoard struct {
		Board map[string]any `json:"board"`
	}
	if err := json.NewDecoder(createBoardResp.Body).Decode(&createdBoard); err != nil {
		t.Fatalf("decode create board response: %v", err)
	}
	boardID := asString(createdBoard.Board["id"])
	if boardID == "" {
		t.Fatalf("expected board id, got %#v", createdBoard.Board)
	}

	if _, err := h.workspace.DB().Exec(
		`UPDATE boards SET thread_id = '' WHERE id = ?`,
		boardID,
	); err != nil {
		t.Fatalf("blank board thread_id: %v", err)
	}

	resp := postJSONExpectStatus(t, h.baseURL+"/inbox/inbox:test/acknowledge", `{
		"actor_id":"actor-1",
		"subject_ref":"board:`+boardID+`"
	}`, http.StatusBadRequest)
	defer resp.Body.Close()
	assertErrorCode(t, resp, "invalid_request")
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
			"refs":["thread:`+threadID+`"],
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
	_, ok := findInboxItem(items, func(item map[string]any) bool {
		return asString(item["category"]) == "risk_exception" && asString(item["source_event_id"]) == eventID
	})
	if !ok {
		t.Fatalf("expected risk_exception inbox item for source_event_id=%s, got %#v", eventID, items)
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
			"refs":["thread:`+threadID+`"],
			"summary":"Approve customer refunds",
			"payload":{},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer dnResp.Body.Close()

	items := getInboxItems(t, h.baseURL)
	decisionItem, ok := findInboxItem(items, func(item map[string]any) bool {
		return asString(item["category"]) == "action_needed" && asString(item["thread_id"]) == threadID
	})
	if !ok {
		t.Fatalf("expected action_needed inbox item, got %#v", items)
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
			"refs":["thread:`+threadID+`","inbox:`+inboxItemID+`"],
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
		t.Fatalf("expected action_needed inbox item to be suppressed after decision_made, got %#v", itemsAfterDecision)
	}

	// A new decision_needed on the same thread should still appear (no over-suppression).
	dn2Resp := postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"decision_needed",
			"thread_id":"`+threadID+`",
			"refs":["thread:`+threadID+`"],
			"summary":"Another decision needed",
			"payload":{},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer dn2Resp.Body.Close()

	itemsAfterRetrigger := getInboxItems(t, h.baseURL)
	if _, ok := findInboxItem(itemsAfterRetrigger, func(item map[string]any) bool {
		return asString(item["category"]) == "action_needed" && asString(item["thread_id"]) == threadID
	}); !ok {
		t.Fatalf("expected new action_needed inbox item after retrigger, got %#v", itemsAfterRetrigger)
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
			"refs":["thread:`+threadID+`"],
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
		return asString(item["category"]) == "risk_exception" && asString(item["thread_id"]) == threadID
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

	parts := strings.SplitN(inboxItemID, ":", 5)
	if len(parts) != 5 || parts[0] != "inbox" {
		t.Fatalf("unexpected inbox id shape %q", inboxItemID)
	}
	legacyID := strings.Join([]string{parts[0], "intervention_needed", parts[2], parts[3], parts[4]}, ":")
	legacyDetailResp, err := http.Get(h.baseURL + "/inbox/" + url.PathEscape(legacyID) + "?risk_horizon_days=30")
	if err != nil {
		t.Fatalf("GET /inbox/{legacy-id}?risk_horizon_days=30: %v", err)
	}
	defer legacyDetailResp.Body.Close()
	if legacyDetailResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected GET /inbox/{legacy-id}?risk_horizon_days=30 status: %d", legacyDetailResp.StatusCode)
	}
	var legacyDetail struct {
		Item map[string]any `json:"item"`
	}
	if err := json.NewDecoder(legacyDetailResp.Body).Decode(&legacyDetail); err != nil {
		t.Fatalf("decode legacy-id inbox item response: %v", err)
	}
	if got := asString(legacyDetail.Item["id"]); got != inboxItemID {
		t.Fatalf("expected legacy id lookup to return canonical id %q, got %q", inboxItemID, got)
	}
}

func TestInboxAskItemDerivationAndRespondAppendsAnswer(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	threadID := integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":            "Ask flow thread",
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

	askResp := postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"agent_ask_requested",
			"thread_id":"`+threadID+`",
			"refs":["thread:`+threadID+`","topic:topic_launch"],
			"summary":"Should we ship Friday?",
			"payload":{
				"query_text":"Should we ship Friday?",
				"asking_agent_id":"actor-1",
				"coverage_hint":"thin - 0 decisions",
				"subject_ref":"topic:topic_launch",
				"related_refs":["receipt:receipt_1"]
			},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer askResp.Body.Close()

	var createdAsk struct {
		Event map[string]any `json:"event"`
	}
	if err := json.NewDecoder(askResp.Body).Decode(&createdAsk); err != nil {
		t.Fatalf("decode ask event response: %v", err)
	}
	askEventID := asString(createdAsk.Event["id"])
	if askEventID == "" {
		t.Fatalf("expected ask event id, got %#v", createdAsk.Event)
	}

	items := getInboxItems(t, h.baseURL)
	askItem, ok := findInboxItem(items, func(item map[string]any) bool {
		return asString(item["kind"]) == "ask" && asString(item["source_event_id"]) == askEventID
	})
	if !ok {
		t.Fatalf("expected ask inbox item for source_event_id=%s, got %#v", askEventID, items)
	}
	askItemID := asString(askItem["id"])
	if askItemID == "" {
		t.Fatalf("expected ask inbox item id, got %#v", askItem)
	}
	if got := asString(askItem["query_text"]); got != "Should we ship Friday?" {
		t.Fatalf("expected query_text in ask item, got %#v", askItem)
	}

	respondResp := postJSONExpectStatus(t, h.baseURL+"/inbox/"+url.PathEscape(askItemID)+"/respond", `{
		"actor_id":"actor-1",
		"answer":"Ship Friday with rollback plan.",
		"save_as_decision":false,
		"notify_asking_agent":true
	}`, http.StatusCreated)
	defer respondResp.Body.Close()

	var responded struct {
		Event          map[string]any `json:"event"`
		Acknowledgment map[string]any `json:"acknowledgment"`
	}
	if err := json.NewDecoder(respondResp.Body).Decode(&responded); err != nil {
		t.Fatalf("decode respond response: %v", err)
	}
	if got := asString(responded.Event["type"]); got != "agent_ask_answered" {
		t.Fatalf("expected agent_ask_answered event, got %#v", responded.Event)
	}
	if got := asString(responded.Acknowledgment["type"]); got != "inbox_item_acknowledged" {
		t.Fatalf("expected inbox_item_acknowledged event, got %#v", responded.Acknowledgment)
	}
}

func TestInboxRespondRejectsNonAskItem(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	threadID := integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":            "Non ask thread",
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

	postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"decision_needed",
			"thread_id":"`+threadID+`",
			"refs":["thread:`+threadID+`"],
			"summary":"Need a decision",
			"payload":{},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated).Body.Close()

	items := getInboxItems(t, h.baseURL)
	decisionItem, ok := findInboxItem(items, func(item map[string]any) bool {
		return asString(item["category"]) == "action_needed" && asString(item["kind"]) != "ask"
	})
	if !ok {
		t.Fatalf("expected decision-backed inbox item, got %#v", items)
	}

	resp := postJSONExpectStatus(t, h.baseURL+"/inbox/"+url.PathEscape(asString(decisionItem["id"]))+"/respond", `{
		"actor_id":"actor-1",
		"answer":"no-op"
	}`, http.StatusBadRequest)
	defer resp.Body.Close()

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode respond error payload: %v", err)
	}
	errorMap, _ := body["error"].(map[string]any)
	if got := asString(errorMap["code"]); got != "invalid_request" {
		t.Fatalf("expected invalid_request code, got %#v", body)
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
