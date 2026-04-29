package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestComprehensiveHTTPAPIFlow(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	threadID := integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":           "Comprehensive thread",
		"type":            "incident",
		"status":          "active",
		"current_summary": "Investigating issue",
		"next_actions":    []any{"triage"},
		"key_artifacts":   []any{},
		"provenance":      map[string]any{"sources": []any{"inferred"}},
		"custom_unknown":  "preserve_me",
	})
	integrationPatchThread(t, h, "actor-1", threadID, map[string]any{"title": "Comprehensive thread (patched)"}, nil)

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
	if _, has := loadedThread.Thread["tags"]; has {
		t.Fatalf("expected dumb threads to omit tags in HTTP response, got %#v", loadedThread.Thread["tags"])
	}
	if asString(loadedThread.Thread["title"]) != "Comprehensive thread (patched)" {
		t.Fatalf("unexpected thread title: %#v", loadedThread.Thread["title"])
	}

	integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":            "Stale thread",
		"type":             "incident",
		"status":           "active",
		"cadence":          "daily",
		"next_check_in_at": "2020-01-01T00:00:00Z",
		"current_summary":  "Needs update",
		"next_actions":     []any{"follow up"},
		"key_artifacts":    []any{},
		"provenance":       map[string]any{"sources": []any{"inferred"}},
	})

	firstAttention := createHumanAttentionEvent(t, h.baseURL, threadID, "ask", "need decision", "thread:"+threadID, nil, nil)
	firstAttentionEventID := asString(firstAttention["id"])
	if firstAttentionEventID == "" {
		t.Fatal("expected first human attention event id")
	}

	inboxBoardResp := postJSONExpectStatus(t, h.baseURL+"/boards", `{
		"actor_id":"actor-1",
		"board":{
			"title":"Comprehensive inbox board",
			"refs":["thread:`+threadID+`"]
		}
	}`, http.StatusCreated)
	defer inboxBoardResp.Body.Close()
	var createdBoard struct {
		Board map[string]any `json:"board"`
	}
	if err := json.NewDecoder(inboxBoardResp.Body).Decode(&createdBoard); err != nil {
		t.Fatalf("decode board response: %v", err)
	}
	boardID := asString(createdBoard.Board["id"])
	boardUpdatedAt := asString(createdBoard.Board["updated_at"])
	cardCreateResp := postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards", `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+boardUpdatedAt+`",
		"title":"Comprehensive work item",
		"related_refs":["thread:`+threadID+`"],
		"column_key":"ready",
		"due_at":"`+time.Now().UTC().Add(24*time.Hour).Format(time.RFC3339)+`",
		"definition_of_done":["receipt","sign-off"]
	}`, http.StatusCreated)
	var comprehensiveCard struct {
		Card map[string]any `json:"card"`
	}
	if err := json.NewDecoder(cardCreateResp.Body).Decode(&comprehensiveCard); err != nil {
		t.Fatalf("decode card create: %v", err)
	}
	cardCreateResp.Body.Close()
	dod, ok := comprehensiveCard.Card["definition_of_done"].([]any)
	if !ok || len(dod) != 2 {
		t.Fatalf("expected definition_of_done on card payload, got %#v", comprehensiveCard.Card["definition_of_done"])
	}

	postJSONExpectStatus(t, h.baseURL+"/derived/rebuild", `{"actor_id":"actor-1"}`, http.StatusOK).Body.Close()

	inboxItems := getInboxItems(t, h.baseURL)
	firstInboxItem, ok := findInboxItem(inboxItems, func(item map[string]any) bool {
		return asString(item["source_event_id"]) == firstAttentionEventID
	})
	if !ok {
		t.Fatalf("expected human inbox row from first attention request, got %#v", inboxItems)
	}
	if asString(firstInboxItem["kind"]) != "ask" {
		t.Fatalf("unexpected inbox kind: %#v", firstInboxItem["kind"])
	}

	newDecision := createHumanAttentionEvent(t, h.baseURL, threadID, "ask", "retrigger decision", "thread:"+threadID, nil, nil)
	newDecisionEventID := asString(newDecision["id"])
	if newDecisionEventID == "" {
		t.Fatal("expected retrigger human attention event id")
	}

	inboxAfterRetrigger := getInboxItems(t, h.baseURL)
	if _, ok := findInboxItem(inboxAfterRetrigger, func(item map[string]any) bool {
		return asString(item["source_event_id"]) == newDecisionEventID
	}); !ok {
		t.Fatalf("expected retrigger human inbox row, got %#v", inboxAfterRetrigger)
	}

	// PrimitiveStore accepts opaque thread bodies; strict enum checks live at HTTP ingress.
	// Keep a lightweight invariant check that the store still rejects missing actor context.
	_, ctErr := h.primitiveStore.CreateThread(context.Background(), "", map[string]any{
		"title":           "Invalid actor",
		"type":            "incident",
		"status":          "active",
		"current_summary": "summary",
		"next_actions":    []any{},
		"key_artifacts":   []any{},
		"provenance":      map[string]any{"sources": []any{"inferred"}},
	})
	if ctErr == nil {
		t.Fatal("expected CreateThread to reject empty actor id")
	}
	if !strings.Contains(ctErr.Error(), "actor") {
		t.Fatalf("expected actor id validation error, got: %v", ctErr)
	}

	postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"card_created",
			"thread_id":"`+threadID+`",
			"refs":["card:only-one"],
			"summary":"bad refs",
			"payload":{},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusBadRequest).Body.Close()

}
