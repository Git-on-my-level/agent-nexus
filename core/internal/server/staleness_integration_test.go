package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestStalenessRebuildDoesNotEmitCadenceBasedExceptions(t *testing.T) {
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

	if count := countStaleThreadExceptions(t, h.baseURL, threadID); count != 0 {
		t.Fatalf("expected no inferred stale_topic exceptions, got %d", count)
	}
	if threadListedAsStale(t, h.baseURL, threadID) {
		t.Fatalf("expected thread not listed stale (no cadence inference), thread=%s", threadID)
	}
	items := getInboxItems(t, h.baseURL)
	if _, ok := findInboxItem(items, func(item map[string]any) bool {
		return asString(item["category"]) == "risk_exception" && asString(item["thread_id"]) == threadID
	}); ok {
		t.Fatalf("expected no risk_exception inbox item from cadence staleness, got %#v", items)
	}
}

func TestStalenessActorStatementAndDocumentActivityKeepsThreadNotStale(t *testing.T) {
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
	if threadListedAsStale(t, h.baseURL, threadID) {
		t.Fatalf("expected thread %s not stale after rebuild", threadID)
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
		t.Fatalf("unexpected stale flag after actor_statement for thread %s", threadID)
	}

	postJSONExpectStatus(t, h.baseURL+"/derived/rebuild", `{"actor_id":"actor-1"}`, http.StatusOK).Body.Close()

	docCreateResp := postJSONExpectStatus(t, h.baseURL+"/docs", `{
		"actor_id":"actor-1",
		"document":{"id":"stale-doc-1","thread_id":"`+threadID+`","title":"Runbook"},
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
		t.Fatalf("unexpected stale after document update for thread %s", threadID)
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
		t.Fatalf("expected thread %s not stale after card activity", threadID)
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

	resp, err := http.Get(baseURL + "/threads?state=active&limit=1000")
	if err != nil {
		t.Fatalf("GET /threads: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected thread list status: %d", resp.StatusCode)
	}

	var payload struct {
		Threads []map[string]any `json:"threads"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode thread list: %v", err)
	}
	for _, thread := range payload.Threads {
		if asString(thread["id"]) != threadID {
			continue
		}
		stale, _ := thread["stale"].(bool)
		return stale
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
