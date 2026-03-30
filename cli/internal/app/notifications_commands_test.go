package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNotificationsListCallsDerivedEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/agent-notifications" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query()["status"]; len(got) != 1 || got[0] != "unread" {
			t.Fatalf("expected unread status filter, got %#v", r.URL.RawQuery)
		}
		if got := r.URL.Query().Get("order"); got != "asc" {
			t.Fatalf("expected asc order, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"wakeup_id":"wake_123","status":"unread"}]}`))
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "--base-url", server.URL, "notifications", "list", "--status", "unread", "--order", "asc"})
	payload := assertEnvelopeOK(t, raw)
	if got := anyStringValue(payload["command"]); got != "notifications list" {
		t.Fatalf("expected notifications list command, got %#v", payload)
	}
}

func TestNotificationsReadPostsWakeupMutation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/agent-notifications/read" {
			http.NotFound(w, r)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if got := strings.TrimSpace(anyStringValue(payload["wakeup_id"])); got != "wake_123" {
			t.Fatalf("expected wakeup_id wake_123, got %#v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"notification":{"wakeup_id":"wake_123","status":"read"}}`))
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "--base-url", server.URL, "notifications", "read", "--wakeup-id", "wake_123"})
	payload := assertEnvelopeOK(t, raw)
	if got := anyStringValue(payload["command"]); got != "notifications read" {
		t.Fatalf("expected notifications read command, got %#v", payload)
	}
}

func TestNotificationsDismissRequiresWakeupID(t *testing.T) {
	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "notifications", "dismiss"})
	payload := assertEnvelopeError(t, raw)
	if got := anyStringValue(payload["command"]); got != "notifications dismiss" {
		t.Fatalf("expected notifications dismiss command, got %#v", payload)
	}
	errorPayload, _ := payload["error"].(map[string]any)
	if message := anyStringValue(errorPayload["message"]); !strings.Contains(message, "--wakeup-id is required") {
		t.Fatalf("expected missing wakeup-id message, got %#v", payload)
	}
}
