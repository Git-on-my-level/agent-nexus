package app

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// The stream can starve under writer contention even after the answer is
// durable. Await must decide from one final durable read instead of
// reporting a timeout over an answer that already exists.
func TestAwaitRecoversTerminalAnswerAfterStreamStarvation(t *testing.T) {
	var gets atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/asks/ask-1":
			if gets.Add(1) == 1 {
				_, _ = w.Write([]byte(`{"status":"open","response":null}`))
				return
			}
			_, _ = w.Write([]byte(`{"status":"answered","response":{"response_text":"Late answer","outcome":"answered","responding_actor_id":"human-1"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/asks/ask-1/subscriptions":
			_, _ = w.Write([]byte(`{"id":"sub-1"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/stream/asks/ask-1":
			// Starved admission: hold the stream until the await budget ends.
			<-r.Context().Done()
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()
	a, out := dailyTestApp(t, server.URL)
	if exit := a.Run([]string{"--json", "--as", "worker", "await", "event:ask-1", "--timeout", "300ms"}); exit != 0 {
		t.Fatalf("exit=%d want=0; the durable answer must beat the starved stream: %s", exit, out.String())
	}
	result := asMap(dailyJSON(t, out)["result"])
	if result["answer"] != "Late answer" || result["responder"] != "human-1" {
		t.Fatalf("lost terminal response: %s", out.String())
	}
}

// A final durable read that still sees an open ask must not invent an answer:
// the await timeout stands.
func TestAwaitStillTimesOutWhenFinalReadStaysOpen(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/asks/ask-1":
			_, _ = w.Write([]byte(`{"status":"open","response":null}`))
		case r.Method == http.MethodPost && r.URL.Path == "/asks/ask-1/subscriptions":
			_, _ = w.Write([]byte(`{"id":"sub-1"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/stream/asks/ask-1":
			<-r.Context().Done()
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()
	a, out := dailyTestApp(t, server.URL)
	if exit := a.Run([]string{"--json", "--as", "worker", "await", "event:ask-1", "--timeout", "200ms"}); exit != 8 {
		t.Fatalf("exit=%d want=8; an open final read must not become an answer: %s", exit, out.String())
	}
}
