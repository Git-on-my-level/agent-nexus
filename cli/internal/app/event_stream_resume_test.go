package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEventsTailIgnoresResumeControlsForMaxEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stream/events" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: resume\ndata: {}\n\nid: previous\nevent: resume\ndata: {}\n\nid: visible\nevent: event\ndata: {\"event\":{\"id\":\"visible\"}}\n\n")
	}))
	defer server.Close()
	raw := runCLIForTest(t, t.TempDir(), map[string]string{}, nil, []string{"--json", "--base-url", server.URL, "debug", "events", "tail", "--max-events", "1"})
	result := assertEnvelopeOK(t, raw)["result"].(map[string]any)
	if result["id"] != "visible" || result["type"] != "event" || strings.Contains(raw, "resume") {
		t.Fatalf("control counted as delivery: %s", raw)
	}
}
