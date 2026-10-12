package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestAskTextValidation(t *testing.T) {
	for _, tc := range []struct {
		title any
		body  any
		code  string
	}{
		{strings.Repeat("界", 160), "## Context\nDetails", ""},
		{strings.Repeat("界", 161), "Details", "ask_title_too_long"},
		{"Decision\nContext", "Details", "ask_title_invalid"},
		{42, "Details", "ask_title_invalid"},
		{"", "Details", "ask_title_invalid"},
		{"Decision", 42, "ask_body_invalid"},
	} {
		if code, _ := validateAskText(map[string]any{"title": tc.title, "body": tc.body}); code != tc.code {
			t.Fatalf("%#v: %s", tc, code)
		}
	}
	if code, _ := validateAskText(map[string]any{"title": "Decision"}); code != "" {
		t.Fatal(code)
	}
}

func TestAskErrorCodesAreRecoverable(t *testing.T) {
	for _, code := range []string{"ask_title_invalid", "ask_title_too_long", "ask_body_invalid"} {
		payload := errorPayload(code, "test")
		if payload["recoverable"] != true {
			t.Fatalf("%s recoverable=%v", code, payload["recoverable"])
		}
		if hint, _ := payload["hint"].(string); hint == "" || hint == "Review error details and endpoint usage, then retry if appropriate." {
			t.Fatalf("%s used unknown-code metadata: %#v", code, payload)
		}
	}
}

func TestAskTextHTTPValidation(t *testing.T) {
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	for _, tc := range []struct {
		title any
		body  any
		code  string
	}{
		{strings.Repeat("界", 161), "Context", "ask_title_too_long"},
		{"Decision\nContext", "Details", "ask_title_invalid"},
		{"Decision", 42, "ask_body_invalid"},
	} {
		raw, _ := json.Marshal(map[string]any{"actor_id": "actor-1", "event": map[string]any{"type": "human_attention_requested", "thread_id": "thread-1", "summary": "Decision", "refs": []string{"thread:thread-1"}, "provenance": map[string]any{"sources": []string{"inferred"}}, "payload": map[string]any{"kind": "ask", "title": tc.title, "body": tc.body, "subject_ref": "card:card-1", "requester_actor_id": "actor-1", "response_proposals": []string{"Accept"}}}})
		resp := postJSONExpectStatus(t, h.baseURL+"/events", string(raw), http.StatusBadRequest)
		var data map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		errObj := data["error"].(map[string]any)
		if errObj["code"] != tc.code {
			t.Fatalf("%#v", data)
		}
		if errObj["recoverable"] != true {
			t.Fatalf("%s recoverable=%v payload=%#v", tc.code, errObj["recoverable"], data)
		}
	}
}
