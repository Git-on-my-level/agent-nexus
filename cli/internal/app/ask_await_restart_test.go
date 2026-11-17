package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// A server restart answers the stream reconnect with 502/503. That is an
// outage, not an outcome: a long wait for a human must reconnect and still
// return the answer instead of exiting 6 mid-wait.
func TestAwaitAskSurvivesServerRestart(t *testing.T) {
	var opens atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/asks/ask-1":
			_, _ = w.Write([]byte(`{"status":"open","response":null}`))
		case r.Method == http.MethodPost && r.URL.Path == "/asks/ask-1/subscriptions":
			_, _ = w.Write([]byte(`{"id":"sub-1"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/asks/ask-1/delivery":
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodGet && r.URL.Path == "/stream/asks/ask-1":
			switch opens.Add(1) {
			case 1:
				w.WriteHeader(http.StatusBadGateway)
			case 2:
				w.WriteHeader(http.StatusServiceUnavailable)
			default:
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "event: outcome\ndata: {\"status\":\"answered\",\"response\":{\"response_text\":\"Approve\",\"outcome\":\"answered\",\"responding_actor_id\":\"human-1\"}}\n\n")
			}
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	a, out := dailyTestApp(t, server.URL)
	if exit := a.Run([]string{"--json", "--as", "worker", "await", "event:ask-1", "--timeout", "10s"}); exit != 0 {
		t.Fatalf("exit=%d want=0 after restart: %s", exit, out.String())
	}
	if result := asMap(dailyJSON(t, out)["result"]); result["answer"] != "Approve" {
		t.Fatalf("lost answer after restart: %s", out.String())
	}
	if opens.Load() < 3 {
		t.Fatalf("stream opens=%d, want reconnects after 502 and 503", opens.Load())
	}
}

// The card await rides the same reconnect path.
func TestAwaitCardSurvivesServerRestart(t *testing.T) {
	var opens, gets atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/cards/card:task":
			phase := "ready"
			if gets.Add(1) > 1 {
				phase = "done"
			}
			fmt.Fprintf(w, `{"card":{"ref":"card:task","column_key":%q}}`, phase)
		case "/stream/events":
			if opens.Add(1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "id: moved\nevent: event\ndata: {\"event\":{\"type\":\"card_moved\"}}\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	a, out := dailyTestApp(t, server.URL)
	if exit := a.Run([]string{"--json", "--as", "worker", "await", "card:task", "--timeout", "10s"}); exit != 0 {
		t.Fatalf("exit=%d want=0 after restart: %s", exit, out.String())
	}
}
