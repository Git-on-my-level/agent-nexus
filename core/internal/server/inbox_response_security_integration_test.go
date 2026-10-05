package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"
)

func responseSecurityFixture(t *testing.T) (primitivesTestHarness, string) {
	t.Helper()
	h := newPrimitivesTestServerWithHumanPrincipal(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Requester","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated).Body.Close()
	threadID := integrationSeedThread(t, h, "actor-1", map[string]any{"title": "Security ask", "type": "incident", "status": "active", "priority": "p1", "tags": []any{}, "cadence": "daily", "current_summary": "summary", "next_actions": []any{}, "key_artifacts": []any{}, "provenance": map[string]any{"sources": []any{"inferred"}}})
	createHumanAttentionEvent(t, h.baseURL, threadID, "ask", "Approve release", "thread:"+threadID, nil, nil)
	items := getInboxItems(t, h.baseURL)
	item, ok := findInboxItem(items, func(candidate map[string]any) bool { return asString(candidate["kind"]) == "ask" })
	if !ok {
		t.Fatalf("ask missing: %#v", items)
	}
	return h, asString(item["id"])
}

func TestInboxResponseRequiresHumanAcrossWritePaths(t *testing.T) {
	requireIntegrationTest(t)
	h, itemID := responseSecurityFixture(t)
	ctx := context.Background()
	standalone := seedMachinePrincipalForLockoutTest(t, ctx, h.workspace.DB(), "standalone-security", "actor-standalone-security", "standalone-security", "standalone-security-token")
	derived := seedMachinePrincipalForLockoutTest(t, ctx, h.workspace.DB(), "derived-security", "actor-derived-security", "derived-security.host-security", "derived-security-token")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := h.workspace.DB().Exec(`INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at) VALUES('host-security','host-security','Security host','tester','host','[]',?)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := h.workspace.DB().Exec(`INSERT INTO host_agents(host_id,name,agent_id,identity_kind) VALUES('host-security','derived-security','derived-security','derived')`); err != nil {
		t.Fatal(err)
	}
	endpoint := h.baseURL + "/inbox/" + url.PathEscape(itemID) + "/respond"
	for _, token := range []string{standalone.AccessToken, derived.AccessToken} {
		status, body := hostHTTP(t, "POST", endpoint, token, map[string]any{"response_text": "Approved", "outcome": "approved", "notify_mode": "none"})
		if status != http.StatusForbidden || body["error"].(map[string]any)["code"] != "human_required" {
			t.Fatalf("agent inbox response: %d %#v", status, body)
		}
		status, body = hostHTTP(t, "POST", h.baseURL+"/events", token, map[string]any{"event": map[string]any{"type": "human_attention_responded"}})
		if status != http.StatusForbidden || body["error"].(map[string]any)["code"] != "protected_event_type" {
			t.Fatalf("agent generic response: %d %#v", status, body)
		}
	}
	status, body := hostHTTP(t, "POST", endpoint, h.humanAccessToken, map[string]any{"response_text": "Approved", "outcome": "approved", "notify_mode": "none"})
	if status != http.StatusCreated || body["event"].(map[string]any)["type"] != "human_attention_responded" {
		t.Fatalf("human inbox response: %d %#v", status, body)
	}
}

func TestInboxResponseConcurrentReplayIsSingleEvent(t *testing.T) {
	requireIntegrationTest(t)
	h, itemID := responseSecurityFixture(t)
	endpoint := h.baseURL + "/inbox/" + url.PathEscape(itemID) + "/respond"
	body := map[string]any{"idempotency_key": "reply-once-1", "response_text": "Approved", "outcome": "approved", "notify_mode": "none"}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		status  int
		eventID string
		err     error
	}
	send := func(data []byte) result {
		req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(data))
		if err != nil {
			return result{err: err}
		}
		req.Header.Set("Authorization", "Bearer "+h.humanAccessToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return result{err: err}
		}
		defer resp.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			return result{err: err}
		}
		if event, ok := payload["event"].(map[string]any); ok {
			return result{status: resp.StatusCode, eventID: asString(event["id"])}
		}
		return result{status: resp.StatusCode, err: fmt.Errorf("response: %#v", payload)}
	}
	const workers = 8
	start := make(chan struct{})
	results := make(chan result, workers)
	var group sync.WaitGroup
	for i := 0; i < workers; i++ {
		group.Add(1)
		go func() { defer group.Done(); <-start; results <- send(raw) }()
	}
	close(start)
	group.Wait()
	close(results)
	var firstID string
	for got := range results {
		if got.err != nil || got.status != http.StatusCreated || got.eventID == "" {
			t.Fatalf("concurrent retry: status=%d event=%q err=%v", got.status, got.eventID, got.err)
		}
		if firstID == "" {
			firstID = got.eventID
		} else if firstID != got.eventID {
			t.Fatalf("duplicate events %s and %s", firstID, got.eventID)
		}
	}
	var count int
	if err := h.workspace.DB().QueryRow(`SELECT COUNT(*) FROM events WHERE type='human_attention_responded'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("response event count=%d err=%v", count, err)
	}
	status, payload := hostHTTP(t, "POST", endpoint, h.humanAccessToken, map[string]any{"idempotency_key": "reply-once-1", "response_text": "Changed", "outcome": "approved", "notify_mode": "none"})
	if status != http.StatusConflict || payload["error"].(map[string]any)["code"] != "idempotency_conflict" {
		t.Fatalf("key reused with changed body: %d %#v", status, payload)
	}
	status, payload = hostHTTP(t, "POST", endpoint, h.humanAccessToken, map[string]any{"idempotency_key": "reply-twice-2", "response_text": "Other", "outcome": "approved", "notify_mode": "none"})
	if status != http.StatusConflict || payload["error"].(map[string]any)["code"] != "conflict" {
		t.Fatalf("second answer accepted: %d %#v", status, payload)
	}
}
