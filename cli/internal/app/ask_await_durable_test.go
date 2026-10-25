package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAwaitTerminalOutcomeNeedsNoSubscriptionOrStream(t *testing.T) {
	for _, tc := range []struct {
		status, outcome string
		exit            int
	}{
		{"answered", "answered", 0},
		{"answered", "rejected", 9},
		{"needs_context", "needs_context", 10},
		{"withdrawn", "", 11},
		{"withdrawn_as_stale", "", 11},
		{"expired", "", 12},
	} {
		t.Run(tc.status+tc.outcome, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && r.URL.Path == "/asks/ask-1" {
					fmt.Fprintf(w, `{"status":%q,"response":{"response_text":"Durable answer","outcome":%q,"responding_actor_id":"human-1"}}`, tc.status, tc.outcome)
					return
				}
				// A blocked writer or unavailable stream must never be consulted.
				t.Errorf("terminal await made unnecessary request: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			defer server.Close()
			a, out := dailyTestApp(t, server.URL)
			if exit := a.Run([]string{"--json", "--as", "worker", "await", "event:ask-1", "--timeout", "500ms"}); exit != tc.exit {
				t.Fatalf("exit=%d want=%d: %s", exit, tc.exit, out.String())
			}
			doc := dailyJSON(t, out)
			result := asMap(doc["result"])
			if tc.exit != 0 {
				result = asMap(asMap(doc["error"])["details"])
			}
			if tc.status == "answered" && (result["answer"] != "Durable answer" || result["responder"] != "human-1") {
				t.Fatalf("lost terminal response: %s", out.String())
			}
		})
	}
}

func TestAwaitDurableReadTimeoutAndNotFound(t *testing.T) {
	for _, tc := range []struct {
		name string
		exit int
	}{{"timeout", 8}, {"not_found", 3}} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/asks/ask-1" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				if tc.name == "timeout" {
					<-r.Context().Done()
					return
				}
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"error":{"code":"not_found","message":"ask not found"}}`)
			}))
			defer server.Close()
			a, out := dailyTestApp(t, server.URL)
			if exit := a.Run([]string{"--json", "--as", "worker", "await", "event:ask-1", "--timeout", "100ms"}); exit != tc.exit {
				t.Fatalf("exit=%d want=%d: %s", exit, tc.exit, out.String())
			}
		})
	}
}

func TestAwaitRetirementDoesNotAcknowledgeAnswerDelivery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/asks/ask-1":
			fmt.Fprint(w, `{"status":"open"}`)
		case "/asks/ask-1/subscriptions":
			fmt.Fprint(w, `{"id":"await-sub"}`)
		case "/stream/asks/ask-1":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: outcome\ndata: {\"status\":\"withdrawn_as_stale\"}\n\n")
		default:
			t.Errorf("retirement touched answer delivery: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	a, out := dailyTestApp(t, server.URL)
	if exit := a.Run([]string{"--json", "--as", "worker", "await", "event:ask-1", "--timeout", "500ms"}); exit != 11 {
		t.Fatalf("retirement status lost: exit=%d %s", exit, out.String())
	}
}
