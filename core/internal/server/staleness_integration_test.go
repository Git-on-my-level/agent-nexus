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

	threadID := integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":            "Daily stale thread",
		"type":             "incident",
		"status":           "active",
		"priority":         "p1",
		"tags":             []any{"ops"},
		"cadence":          "daily",
		"next_check_in_at": "2020-01-01T00:00:00Z",
		"current_summary":  "summary",
		"next_actions":     []any{"do x"},
		"key_artifacts":    []any{},
		"provenance":       map[string]any{"sources": []any{"inferred"}},
	})

	postJSONExpectStatus(t, h.baseURL+"/derived/rebuild", `{"actor_id":"actor-1"}`, http.StatusOK).Body.Close()

	staleCount := countStaleThreadExceptions(t, h.baseURL, threadID)
	if staleCount != 1 {
		t.Fatalf("expected exactly one stale_topic exception after first rebuild, got %d", staleCount)
	}
	staleEvent := findStaleThreadExceptionEvent(t, h.baseURL, threadID)
	if staleEvent == nil {
		t.Fatal("expected stale_topic exception event in timeline")
	}
	assertInferredProvenance(t, staleEvent)

	items := getInboxItems(t, h.baseURL)
	if _, ok := findInboxItem(items, func(item map[string]any) bool {
		return asString(item["category"]) == "risk_exception" && asString(item["thread_id"]) == threadID
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
		return asString(item["category"]) == "risk_exception" && asString(item["thread_id"]) == threadID
	}); ok {
		t.Fatalf("expected stale exception inbox item to be suppressed after new decision activity, got %#v", itemsAfterDecision)
	}
}

func TestStalenessClearsAfterActorStatementAndDocumentActivity(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	threadID := integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":            "Daily stale collaboration thread",
		"type":             "incident",
		"status":           "active",
		"priority":         "p1",
		"tags":             []any{"ops"},
		"cadence":          "daily",
		"next_check_in_at": "2020-01-01T00:00:00Z",
		"current_summary":  "summary",
		"next_actions":     []any{"do x"},
		"key_artifacts":    []any{},
		"provenance":       map[string]any{"sources": []any{"inferred"}},
	})

	postJSONExpectStatus(t, h.baseURL+"/derived/rebuild", `{"actor_id":"actor-1"}`, http.StatusOK).Body.Close()
	if !threadListedAsStale(t, h.baseURL, threadID) {
		t.Fatalf("expected thread %s to be stale after rebuild", threadID)
	}

	postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"actor_statement",
			"thread_id":"`+threadID+`",
			"refs":["thread:`+threadID+`"],
			"summary":"shared an update",
			"payload":{"statement":"progress update"},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated).Body.Close()

	if threadListedAsStale(t, h.baseURL, threadID) {
		t.Fatalf("expected actor_statement activity to clear stale thread %s", threadID)
	}
	itemsAfterStatement := getInboxItems(t, h.baseURL)
	if _, ok := findInboxItem(itemsAfterStatement, func(item map[string]any) bool {
		return asString(item["category"]) == "risk_exception" && asString(item["thread_id"]) == threadID
	}); ok {
		t.Fatalf("expected stale exception inbox item to be suppressed after actor_statement, got %#v", itemsAfterStatement)
	}

	postJSONExpectStatus(t, h.baseURL+"/derived/rebuild", `{"actor_id":"actor-1"}`, http.StatusOK).Body.Close()

	docCreateResp := postJSONExpectStatus(t, h.baseURL+"/docs", `{
		"actor_id":"actor-1",
		"document":{"id":"stale-doc-1","thread_id":"`+threadID+`","title":"Runbook","labels":["ops"]},
		"refs":["thread:`+threadID+`"],
		"content":"initial text",
		"content_type":"text"
	}`, http.StatusCreated)
	defer docCreateResp.Body.Close()

	var createdDoc struct {
		Revision map[string]any `json:"revision"`
	}
	if err := json.NewDecoder(docCreateResp.Body).Decode(&createdDoc); err != nil {
		t.Fatalf("decode create doc response: %v", err)
	}
	baseRevisionID := asString(createdDoc.Revision["revision_id"])
	if baseRevisionID == "" {
		t.Fatal("expected base revision id")
	}

	postJSONExpectStatus(t, h.baseURL+"/derived/rebuild", `{"actor_id":"actor-1"}`, http.StatusOK).Body.Close()

	postJSONExpectStatus(t, h.baseURL+"/docs/stale-doc-1/revisions", `{
		"actor_id":"actor-1",
		"if_base_revision":"`+baseRevisionID+`",
		"document":{"title":"Runbook updated"},
		"refs":["thread:`+threadID+`"],
		"content":"updated text",
		"content_type":"text"
	}`, http.StatusCreated).Body.Close()

	if threadListedAsStale(t, h.baseURL, threadID) {
		t.Fatalf("expected document update activity to clear stale thread %s", threadID)
	}
	itemsAfterDoc := getInboxItems(t, h.baseURL)
	if _, ok := findInboxItem(itemsAfterDoc, func(item map[string]any) bool {
		return asString(item["category"]) == "risk_exception" && asString(item["thread_id"]) == threadID
	}); ok {
		t.Fatalf("expected stale exception inbox item to stay suppressed after document update, got %#v", itemsAfterDoc)
	}
}

func TestStalenessRebuildTreatsRecentCardActivityAsFresh(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	threadID := integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":            "Card-backed stale thread",
		"type":             "incident",
		"status":           "active",
		"priority":         "p1",
		"tags":             []any{"ops"},
		"cadence":          "daily",
		"next_check_in_at": "2020-01-01T00:00:00Z",
		"current_summary":  "summary",
		"next_actions":     []any{"do x"},
		"key_artifacts":    []any{},
		"provenance":       map[string]any{"sources": []any{"inferred"}},
	})

	createBoardResp := postJSONExpectStatus(t, h.baseURL+"/boards", `{
		"actor_id":"actor-1",
		"board":{
			"title":"Staleness board",
			"refs":["thread:`+threadID+`"]
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
	boardUpdatedAt := asString(createdBoard.Board["updated_at"])

	postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards", `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+boardUpdatedAt+`",
		"title":"Fresh board activity",
		"related_refs":["thread:`+threadID+`"],
		"column_key":"ready"
	}`, http.StatusCreated).Body.Close()

	postJSONExpectStatus(t, h.baseURL+"/derived/rebuild", `{"actor_id":"actor-1"}`, http.StatusOK).Body.Close()

	if threadListedAsStale(t, h.baseURL, threadID) {
		t.Fatalf("expected recent card activity to keep thread %s fresh", threadID)
	}
	items := getInboxItems(t, h.baseURL)
	if _, ok := findInboxItem(items, func(item map[string]any) bool {
		return asString(item["category"]) == "risk_exception" && asString(item["thread_id"]) == threadID
	}); ok {
		t.Fatalf("expected no stale exception inbox item after recent card activity, got %#v", items)
	}
}

func threadListedAsStale(t *testing.T, baseURL string, threadID string) bool {
	t.Helper()

	resp, err := http.Get(baseURL + "/threads?stale=true")
	if err != nil {
		t.Fatalf("GET /threads?stale=true: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected stale thread list status: %d", resp.StatusCode)
	}

	var payload struct {
		Threads []map[string]any `json:"threads"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode stale thread list: %v", err)
	}
	for _, thread := range payload.Threads {
		if asString(thread["id"]) == threadID {
			return true
		}
	}
	return false
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
		if subtype == "stale_topic" {
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
		if subtype == "stale_topic" {
			return event
		}
	}
	return nil
}
