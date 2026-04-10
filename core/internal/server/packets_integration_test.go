package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func integrationSeedBoardAndCard(t *testing.T, h primitivesTestHarness, actorID, parentThreadID string) (boardID, cardID, cardBackingThreadID string) {
	t.Helper()
	createBoardResp := postJSONExpectStatus(t, h.baseURL+"/boards", fmt.Sprintf(`{
		"actor_id":%q,
		"board":{"title":"Packet test board","refs":["thread:%s"]}
	}`, actorID, parentThreadID), http.StatusCreated)
	defer createBoardResp.Body.Close()
	var boardPayload struct {
		Board map[string]any `json:"board"`
	}
	if err := json.NewDecoder(createBoardResp.Body).Decode(&boardPayload); err != nil {
		t.Fatalf("decode board: %v", err)
	}
	boardID = asString(boardPayload.Board["id"])
	boardUpdatedAt := asString(boardPayload.Board["updated_at"])
	cardResp := postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards", fmt.Sprintf(`{
		"actor_id":%q,
		"if_board_updated_at":%q,
		"title":"Packet card",
		"related_refs":["thread:%s"],
		"column_key":"backlog"
	}`, actorID, boardUpdatedAt, parentThreadID), http.StatusCreated)
	defer cardResp.Body.Close()
	var cardPayload struct {
		Card map[string]any `json:"card"`
	}
	if err := json.NewDecoder(cardResp.Body).Decode(&cardPayload); err != nil {
		t.Fatalf("decode card: %v", err)
	}
	cardID = asString(cardPayload.Card["id"])
	cardBackingThreadID = asString(cardPayload.Card["thread_id"])
	if boardID == "" || cardID == "" || cardBackingThreadID == "" {
		t.Fatalf("expected board, card, and card backing thread ids, got board=%q card=%q thread=%q", boardID, cardID, cardBackingThreadID)
	}
	return boardID, cardID, cardBackingThreadID
}

func TestPacketConvenienceEndpointsAndTimeline(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	parentThreadID := integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":            "Packet flow thread",
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
	_, cardID, cardBackingThreadID := integrationSeedBoardAndCard(t, h, "actor-1", parentThreadID)
	cardRef := "card:" + cardID

	receiptID := "receipt-1"
	receiptFailureResp := postJSONExpectStatus(t, h.baseURL+"/packets/receipts", `{
		"actor_id":"actor-1",
		"artifact":{
			"id":"`+receiptID+`",
			"refs":["`+cardRef+`"],
			"summary":"receipt artifact"
		},
		"packet":{
			"receipt_id":"`+receiptID+`",
			"subject_ref":"`+cardRef+`",
			"outputs":[],
			"verification_evidence":["url:https://example.com/evidence"],
			"changes_summary":"changed things",
			"known_gaps":[]
		}
	}`, http.StatusBadRequest)
	defer receiptFailureResp.Body.Close()

	receiptSuccessResp := postJSONExpectStatus(t, h.baseURL+"/packets/receipts", `{
		"actor_id":"actor-1",
		"artifact":{
			"id":"`+receiptID+`",
			"refs":["`+cardRef+`"],
			"summary":"receipt artifact"
		},
		"packet":{
			"receipt_id":"`+receiptID+`",
			"subject_ref":"`+cardRef+`",
			"outputs":["artifact:output-1"],
			"verification_evidence":["url:https://example.com/evidence"],
			"changes_summary":"changed things",
			"known_gaps":[]
		}
	}`, http.StatusCreated)
	defer receiptSuccessResp.Body.Close()
	var receiptPayload struct {
		Artifact map[string]any `json:"artifact"`
		Event    map[string]any `json:"event"`
	}
	if err := json.NewDecoder(receiptSuccessResp.Body).Decode(&receiptPayload); err != nil {
		t.Fatalf("decode receipt response: %v", err)
	}
	if receiptPayload.Artifact["kind"] != "receipt" {
		t.Fatalf("unexpected receipt kind: %#v", receiptPayload.Artifact["kind"])
	}
	assertRefsContain(t, receiptPayload.Artifact["refs"], "artifact:"+receiptID, cardRef)

	reviewID := "review-1"
	reviewResp := postJSONExpectStatus(t, h.baseURL+"/packets/reviews", `{
		"actor_id":"actor-1",
		"artifact":{
			"id":"`+reviewID+`",
			"refs":["`+cardRef+`","artifact:`+receiptID+`"],
			"summary":"review artifact"
		},
		"packet":{
			"review_id":"`+reviewID+`",
			"subject_ref":"`+cardRef+`",
			"receipt_id":"`+receiptID+`",
			"outcome":"accept",
			"notes":"looks good",
			"evidence_refs":["artifact:`+receiptID+`"]
		}
	}`, http.StatusCreated)
	defer reviewResp.Body.Close()
	var reviewPayload struct {
		Artifact map[string]any `json:"artifact"`
		Event    map[string]any `json:"event"`
	}
	if err := json.NewDecoder(reviewResp.Body).Decode(&reviewPayload); err != nil {
		t.Fatalf("decode review response: %v", err)
	}
	assertRefsContain(t, reviewPayload.Artifact["refs"], "artifact:"+reviewID, "artifact:"+receiptID, cardRef)

	if resp, err := http.Get(h.baseURL + "/artifacts/" + reviewID); err != nil {
		t.Fatalf("GET /artifacts/{review_id}: %v", err)
	} else {
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("unexpected GET /artifacts/{review_id} status: %d", resp.StatusCode)
		}
	}

	timelineResp, err := http.Get(h.baseURL + "/threads/" + cardBackingThreadID + "/timeline")
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

	receiptEvent := findEventByType(timeline.Events, "receipt_added")
	if receiptEvent == nil {
		t.Fatal("expected receipt_added event in timeline")
	}
	assertRefsContain(t, receiptEvent["refs"], "artifact:"+receiptID, cardRef)
	assertActorStatementProvenance(t, receiptEvent)

	reviewEvent := findEventByType(timeline.Events, "review_completed")
	if reviewEvent == nil {
		t.Fatal("expected review_completed event in timeline")
	}
	assertRefsContain(t, reviewEvent["refs"], "artifact:"+reviewID, "artifact:"+receiptID, cardRef)
	assertActorStatementProvenance(t, reviewEvent)

	cardTimelineResp, err := http.Get(h.baseURL + "/cards/" + cardID + "/timeline")
	if err != nil {
		t.Fatalf("GET /cards/{id}/timeline: %v", err)
	}
	defer cardTimelineResp.Body.Close()
	if cardTimelineResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected card timeline status: %d", cardTimelineResp.StatusCode)
	}
	var cardTimeline struct {
		Card   map[string]any   `json:"card"`
		Events []map[string]any `json:"events"`
	}
	if err := json.NewDecoder(cardTimelineResp.Body).Decode(&cardTimeline); err != nil {
		t.Fatalf("decode card timeline: %v", err)
	}
	if asString(cardTimeline.Card["id"]) != cardID {
		t.Fatalf("expected card id in timeline, got %#v", cardTimeline.Card["id"])
	}
	if findEventByType(cardTimeline.Events, "receipt_added") == nil {
		t.Fatal("expected receipt_added on card timeline")
	}
}

func TestPacketValidationErrors(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	parentThreadID := integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":            "Packet validation thread",
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
	_, cardID, _ := integrationSeedBoardAndCard(t, h, "actor-1", parentThreadID)
	cardRef := "card:" + cardID

	respMissingOutputs := postJSONExpectStatus(t, h.baseURL+"/packets/receipts", `{
		"actor_id":"actor-1",
		"artifact":{"id":"rc-missing-out","refs":["`+cardRef+`"]},
		"packet":{
			"receipt_id":"rc-missing-out",
			"subject_ref":"`+cardRef+`",
			"outputs":[],
			"verification_evidence":["url:https://example.com/evidence"],
			"changes_summary":"x",
			"known_gaps":[]
		}
	}`, http.StatusBadRequest)
	assertErrorMessageContains(t, respMissingOutputs, "packet.outputs")

	respBadTypedRef := postJSONExpectStatus(t, h.baseURL+"/packets/receipts", `{
		"actor_id":"actor-1",
		"artifact":{"id":"rc-bad-ref","refs":["`+cardRef+`"]},
		"packet":{
			"receipt_id":"rc-bad-ref",
			"subject_ref":"`+cardRef+`",
			"outputs":["artifact:out-1"],
			"verification_evidence":["not-a-typed-ref"],
			"changes_summary":"x",
			"known_gaps":[]
		}
	}`, http.StatusBadRequest)
	assertErrorMessageContains(t, respBadTypedRef, "packet.verification_evidence")

	respIDMismatch := postJSONExpectStatus(t, h.baseURL+"/packets/receipts", `{
		"actor_id":"actor-1",
		"artifact":{"id":"rc-one","refs":["`+cardRef+`"]},
		"packet":{
			"receipt_id":"rc-two",
			"subject_ref":"`+cardRef+`",
			"outputs":["artifact:out-1"],
			"verification_evidence":["url:https://example.com/evidence"],
			"changes_summary":"x",
			"known_gaps":[]
		}
	}`, http.StatusBadRequest)
	assertErrorMessageContains(t, respIDMismatch, "must equal artifact.id")
}

func TestPacketCreateRequestKeyReplaysSingleWrite(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	parentThreadID := integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":            "Packet replay thread",
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
	_, cardID, cardBackingThreadID := integrationSeedBoardAndCard(t, h, "actor-1", parentThreadID)
	cardRef := "card:" + cardID

	receiptBody := `{
		"actor_id":"actor-1",
		"request_key":"replay-receipt",
		"artifact":{
			"refs":["` + cardRef + `"],
			"summary":"receipt artifact"
		},
		"packet":{
			"subject_ref":"` + cardRef + `",
			"outputs":["artifact:output-1"],
			"verification_evidence":["url:https://example.com/evidence"],
			"changes_summary":"changed things",
			"known_gaps":[]
		}
	}`

	firstReceiptResp := postJSONExpectStatus(t, h.baseURL+"/packets/receipts", receiptBody, http.StatusCreated)
	defer firstReceiptResp.Body.Close()
	secondReceiptResp := postJSONExpectStatus(t, h.baseURL+"/packets/receipts", receiptBody, http.StatusCreated)
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

	receiptsResp, err := http.Get(h.baseURL + "/artifacts?thread_id=" + cardBackingThreadID + "&kind=receipt")
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

	timelineReplayResp, err := http.Get(h.baseURL + "/threads/" + cardBackingThreadID + "/timeline")
	if err != nil {
		t.Fatalf("GET /threads/{id}/timeline: %v", err)
	}
	defer timelineReplayResp.Body.Close()
	var timelineReplay struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.NewDecoder(timelineReplayResp.Body).Decode(&timelineReplay); err != nil {
		t.Fatalf("decode timeline: %v", err)
	}
	if countEventsByType(timelineReplay.Events, "receipt_added") != 1 {
		t.Fatalf("expected one receipt_added event, got %d", countEventsByType(timelineReplay.Events, "receipt_added"))
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

	parentThreadID := integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":            "Packet ID safety thread",
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
	_, cardID, _ := integrationSeedBoardAndCard(t, h, "actor-1", parentThreadID)
	cardRef := "card:" + cardID

	receiptInvalidIDResp := postJSONExpectStatus(t, h.baseURL+"/packets/receipts", `{
		"actor_id":"actor-1",
		"artifact":{"id":"..","refs":["`+cardRef+`"]},
		"packet":{
			"receipt_id":"..",
			"subject_ref":"`+cardRef+`",
			"outputs":["artifact:output-1"],
			"verification_evidence":["url:https://example.com/evidence"],
			"changes_summary":"summary",
			"known_gaps":[]
		}
	}`, http.StatusBadRequest)
	assertErrorMessageContains(t, receiptInvalidIDResp, "artifact.id")

	const receiptID = "receipt-valid-for-unsafe-id-tests"
	postJSONExpectStatus(t, h.baseURL+"/packets/receipts", `{
		"actor_id":"actor-1",
		"artifact":{"id":"`+receiptID+`","refs":["`+cardRef+`"]},
		"packet":{
			"receipt_id":"`+receiptID+`",
			"subject_ref":"`+cardRef+`",
			"outputs":["artifact:output-1"],
			"verification_evidence":["url:https://example.com/evidence"],
			"changes_summary":"summary",
			"known_gaps":[]
		}
	}`, http.StatusCreated).Body.Close()

	reviewInvalidIDResp := postJSONExpectStatus(t, h.baseURL+"/packets/reviews", `{
		"actor_id":"actor-1",
		"artifact":{"id":"/tmp/review-bad","refs":["`+cardRef+`","artifact:`+receiptID+`"]},
		"packet":{
			"review_id":"/tmp/review-bad",
			"subject_ref":"`+cardRef+`",
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
