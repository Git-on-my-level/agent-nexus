package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestHumanAttentionDerivationAndResponseSuppressesItem(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	threadID := integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":           "Human attention thread",
		"type":            "incident",
		"status":          "active",
		"priority":        "p1",
		"tags":            []any{"ops"},
		"cadence":         "daily",
		"current_summary": "summary",
		"next_actions":    []any{"do x"},
		"key_artifacts":   []any{},
		"provenance":      map[string]any{"sources": []any{"inferred"}},
	})

	created := createHumanAttentionEvent(t, h.baseURL, threadID, "ask", "Should we ship Friday?", "topic:launch", []string{"artifact:analysis"}, map[string]any{
		"body":               "I found conflicting dates.",
		"coverage_hint":      "thin - 0 decisions",
		"requester_actor_id": "actor-agent",
		"requester_agent_id": "agent-a",
		"requester_label":    "agent-a",
	})
	requestEventID := asString(created["id"])
	if requestEventID == "" {
		t.Fatalf("expected request event id, got %#v", created)
	}

	items := getInboxItems(t, h.baseURL)
	item, ok := findInboxItem(items, func(candidate map[string]any) bool {
		return asString(candidate["kind"]) == "ask" && asString(candidate["source_event_id"]) == requestEventID
	})
	if !ok {
		t.Fatalf("expected human ask inbox item for source_event_id=%s, got %#v", requestEventID, items)
	}
	if got := asString(item["body"]); got != "I found conflicting dates." {
		t.Fatalf("expected item body, got %#v", item)
	}
	rp, ok := item["response_proposals"].([]any)
	if !ok || len(rp) < 1 {
		t.Fatalf("expected response_proposals on inbox item, got %#v", item["response_proposals"])
	}
	if got := asString(item["requester_agent_id"]); got != "agent-a" {
		t.Fatalf("expected requester_agent_id, got %#v", item)
	}
	status, _ := item["notification_target_status"].(map[string]any)
	if got := asString(status["state"]); got != "unresolved" {
		t.Fatalf("expected unresolved notification state without registered agent, got %#v", status)
	}

	itemID := asString(item["id"])
	resp := postJSONExpectStatus(t, h.baseURL+"/inbox/"+url.PathEscape(itemID)+"/respond", `{
		"actor_id":"actor-1",
		"response_text":"Ship Friday with a rollback plan.",
		"notify_mode":"none",
		"related_refs":["artifact:decision_note"]
	}`, http.StatusCreated)
	defer resp.Body.Close()

	var response struct {
		Event  map[string]any `json:"event"`
		Notify map[string]any `json:"notify"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := asString(response.Event["type"]); got != "human_attention_responded" {
		t.Fatalf("expected generic response event, got %#v", response.Event)
	}
	payload, _ := response.Event["payload"].(map[string]any)
	if got := asString(payload["response_text"]); got != "Ship Friday with a rollback plan." {
		t.Fatalf("expected response_text payload, got %#v", payload)
	}
	if got := asString(response.Notify["mode"]); got != "none" {
		t.Fatalf("expected no notification target metadata, got %#v", response.Notify)
	}

	itemsAfterResponse := getInboxItems(t, h.baseURL)
	if _, stillThere := findInboxItem(itemsAfterResponse, func(candidate map[string]any) bool {
		return asString(candidate["id"]) == itemID
	}); stillThere {
		t.Fatalf("expected response event to suppress original item, got %#v", itemsAfterResponse)
	}
}

func TestHumanAttentionSupportsReviewAndEscalateKinds(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	threadID := integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":           "Human attention kinds",
		"type":            "incident",
		"status":          "active",
		"priority":        "p1",
		"tags":            []any{},
		"cadence":         "daily",
		"current_summary": "summary",
		"next_actions":    []any{},
		"key_artifacts":   []any{},
		"provenance":      map[string]any{"sources": []any{"inferred"}},
	})

	createHumanAttentionEvent(t, h.baseURL, threadID, "review", "Please review launch notes", "document:launch_notes", nil, nil)
	createHumanAttentionEvent(t, h.baseURL, threadID, "escalate", "Possible leaked secret", "artifact:scan_result", nil, map[string]any{"severity": "high"})

	items := getInboxItems(t, h.baseURL)
	if _, ok := findInboxItem(items, func(item map[string]any) bool {
		return asString(item["kind"]) == "review" && asString(item["title"]) == "Please review launch notes"
	}); !ok {
		t.Fatalf("expected review inbox item, got %#v", items)
	}
	escalation, ok := findInboxItem(items, func(item map[string]any) bool {
		return asString(item["kind"]) == "escalate" && asString(item["severity"]) == "high"
	})
	if !ok {
		t.Fatalf("expected escalation inbox item, got %#v", items)
	}

	postJSONExpectStatus(t, h.baseURL+"/inbox/"+url.PathEscape(asString(escalation["id"]))+"/respond", `{
		"actor_id":"actor-1",
		"response_text":"Investigating now.",
		"notify_mode":"none"
	}`, http.StatusCreated).Body.Close()
}

func TestHumanAttentionResponseRequiresResolvableTargetOrExplicitNone(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	threadID := integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":           "Notification target thread",
		"type":            "incident",
		"status":          "active",
		"priority":        "p1",
		"tags":            []any{},
		"cadence":         "daily",
		"current_summary": "summary",
		"next_actions":    []any{},
		"key_artifacts":   []any{},
		"provenance":      map[string]any{"sources": []any{"inferred"}},
	})
	createHumanAttentionEvent(t, h.baseURL, threadID, "ask", "Need unavailable requester", "thread:"+threadID, nil, map[string]any{
		"requester_actor_id": "actor-missing",
		"requester_agent_id": "agent-missing",
	})

	items := getInboxItems(t, h.baseURL)
	item, ok := findInboxItem(items, func(candidate map[string]any) bool {
		return asString(candidate["kind"]) == "ask"
	})
	if !ok {
		t.Fatalf("expected human ask item, got %#v", items)
	}

	resp := postJSONExpectStatus(t, h.baseURL+"/inbox/"+url.PathEscape(asString(item["id"]))+"/respond", `{
		"actor_id":"actor-1",
		"response_text":"Answer text"
	}`, http.StatusConflict)
	defer resp.Body.Close()
	assertErrorCode(t, resp, "notification_target_required")
}

func createHumanAttentionEvent(t *testing.T, baseURL, threadID, kind, title, subjectRef string, relatedRefs []string, extra map[string]any) map[string]any {
	t.Helper()
	if relatedRefs == nil {
		relatedRefs = []string{}
	}
	payload := map[string]any{
		"kind":               kind,
		"title":              title,
		"subject_ref":        subjectRef,
		"related_refs":       relatedRefs,
		"requester_actor_id": "actor-1",
		"response_proposals": []any{"Recommended response.", "Alternative suggestion."},
	}
	for key, value := range extra {
		payload[key] = value
	}
	refs := []string{"thread:" + threadID, subjectRef}
	refs = append(refs, relatedRefs...)
	body := map[string]any{
		"actor_id": "actor-1",
		"event": map[string]any{
			"type":       "human_attention_requested",
			"thread_id":  threadID,
			"refs":       refs,
			"summary":    title,
			"payload":    payload,
			"provenance": map[string]any{"sources": []any{"inferred"}},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal human attention event: %v", err)
	}
	resp := postJSONExpectStatus(t, baseURL+"/events", string(raw), http.StatusCreated)
	defer resp.Body.Close()
	var created struct {
		Event map[string]any `json:"event"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode human attention event response: %v", err)
	}
	return created.Event
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

func TestHumanAttentionRequestedRejectsInvalidResponseProposals(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	threadID := integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":           "Proposal validation thread",
		"type":            "incident",
		"status":          "active",
		"priority":        "p1",
		"tags":            []any{},
		"cadence":         "daily",
		"current_summary": "summary",
		"next_actions":    []any{},
		"key_artifacts":   []any{},
		"provenance":      map[string]any{"sources": []any{"inferred"}},
	})

	basePayload := map[string]any{
		"kind":               "ask",
		"title":              "Question",
		"subject_ref":        "thread:" + threadID,
		"related_refs":       []any{},
		"requester_actor_id": "actor-1",
	}

	postBad := func(payload map[string]any) {
		t.Helper()
		refs := []any{"thread:" + threadID, "thread:" + threadID}
		body := map[string]any{
			"actor_id": "actor-1",
			"event": map[string]any{
				"type":       "human_attention_requested",
				"thread_id":  threadID,
				"refs":       refs,
				"summary":    "Question",
				"payload":    payload,
				"provenance": map[string]any{"sources": []any{"inferred"}},
			},
		}
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		resp := postJSONExpectStatus(t, h.baseURL+"/events", string(raw), http.StatusBadRequest)
		defer resp.Body.Close()
		assertErrorCode(t, resp, "invalid_request")
	}

	t.Run("missing", func(t *testing.T) {
		postBad(basePayload)
	})
	t.Run("empty_after_trim", func(t *testing.T) {
		p := map[string]any{}
		for k, v := range basePayload {
			p[k] = v
		}
		p["response_proposals"] = []any{"", "  "}
		postBad(p)
	})
	t.Run("too_many", func(t *testing.T) {
		p := map[string]any{}
		for k, v := range basePayload {
			p[k] = v
		}
		p["response_proposals"] = []any{"a", "b", "c", "d", "e", "f", "g"}
		postBad(p)
	})
	t.Run("too_long", func(t *testing.T) {
		p := map[string]any{}
		for k, v := range basePayload {
			p[k] = v
		}
		p["response_proposals"] = []any{strings.Repeat("x", 241)}
		postBad(p)
	})
	t.Run("non_string", func(t *testing.T) {
		p := map[string]any{}
		for k, v := range basePayload {
			p[k] = v
		}
		p["response_proposals"] = []any{"ok", 99}
		postBad(p)
	})
}

func asString(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return fmt.Sprint(value)
}
