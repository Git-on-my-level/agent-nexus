package app

import (
	"agent-nexus-cli/internal/errnorm"
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

// A card read that fails after the completion event must be retried even if
// no further event ever arrives (an idle workspace).
func TestAwaitCardRetriesReadAfterTransientFailure(t *testing.T) {
	var gets, opens atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/cards/card:task":
			switch gets.Add(1) {
			case 1:
				fmt.Fprint(w, `{"card":{"ref":"card:task","column_key":"ready"}}`)
			case 2:
				w.WriteHeader(http.StatusServiceUnavailable)
			default:
				fmt.Fprint(w, `{"card":{"ref":"card:task","column_key":"done"}}`)
			}
		case "/stream/events":
			w.Header().Set("Content-Type", "text/event-stream")
			if opens.Add(1) == 1 {
				fmt.Fprint(w, "id: moved\nevent: event\ndata: {\"event\":{\"type\":\"card_moved\"}}\n\n")
				w.(http.Flusher).Flush()
			}
			// Then stay idle: no further events.
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	a, out := dailyTestApp(t, server.URL)
	if exit := a.Run([]string{"--json", "--as", "worker", "await", "card:task", "--timeout", "10s"}); exit != 0 {
		t.Fatalf("exit=%d want=0: the card was done after one failed read: %s", exit, out.String())
	}
}

// resolution_refs are stored sorted: the answer is the newest cited message,
// not the first ref.
func TestAwaitCardPicksTheNewestResolutionMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/cards/card:ask":
			fmt.Fprint(w, `{"card":{"ref":"card:ask","column_key":"done","resolution_refs":["event:aaa-old-evidence","event:zzz-answer"]}}`)
		case "/events/aaa-old-evidence":
			fmt.Fprint(w, `{"event":{"type":"message_posted","ts":"2026-11-17T08:00:00Z","actor_id":"actor-other","payload":{"text":"earlier evidence"}}}`)
		case "/events/zzz-answer":
			fmt.Fprint(w, `{"event":{"type":"message_posted","ts":"2026-11-17T09:00:00Z","actor_id":"actor-lane","payload":{"text":"ANSWER: yes"}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	a, out := dailyTestApp(t, server.URL)
	if exit := a.Run([]string{"--json", "--as", "worker", "await", "card:ask", "--timeout", "5s"}); exit != 0 {
		t.Fatalf("exit=%d %s", exit, out.String())
	}
	if got := asMap(asMap(dailyJSON(t, out)["result"])["resolution"]); got["text"] != "ANSWER: yes" {
		t.Fatalf("resolution=%#v, want the newest message", got)
	}
}

// Awaits started while the server is down wait for it instead of exiting 6.
func TestAwaitsStartedDuringAnOutageKeepWaiting(t *testing.T) {
	var cardGets, askGets atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/cards/card:task":
			if cardGets.Add(1) <= 2 {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			fmt.Fprint(w, `{"card":{"ref":"card:task","column_key":"done"}}`)
		case r.URL.Path == "/asks/ask-1":
			if askGets.Add(1) <= 2 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			fmt.Fprint(w, `{"status":"answered","response":{"response_text":"Approve","outcome":"answered","responding_actor_id":"human-1"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	for _, args := range [][]string{{"await", "card:task"}, {"await", "event:ask-1"}} {
		a, out := dailyTestApp(t, server.URL)
		if exit := a.Run(append([]string{"--json", "--as", "worker"}, append(args, "--timeout", "10s")...)); exit != 0 {
			t.Fatalf("%v exit=%d want=0: %s", args, exit, out.String())
		}
	}
}

// A transition made while the stream was down is not replayed: reconnecting
// must re-read the card.
func TestAwaitCardRereadsAfterReconnect(t *testing.T) {
	var gets, opens atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/cards/card:task":
			phase := "ready"
			if gets.Add(1) > 1 {
				phase = "done" // moved during the outage
			}
			fmt.Fprintf(w, `{"card":{"ref":"card:task","column_key":%q}}`, phase)
		case "/stream/events":
			if opens.Add(1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			<-r.Context().Done() // idle: the move is never replayed
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	a, out := dailyTestApp(t, server.URL)
	if exit := a.Run([]string{"--json", "--as", "worker", "await", "card:task", "--timeout", "10s"}); exit != 0 {
		t.Fatalf("exit=%d want=0: %s", exit, out.String())
	}
}

func TestTransientAwaitErrorRetriesOnlyRecoverableFailures(t *testing.T) {
	no, yes := false, true
	cases := []struct {
		err  error
		want bool
	}{
		{&errnorm.Error{Kind: errnorm.KindNetwork, Code: "network_error", Recoverable: &no}, false}, // tls, canceled
		{&errnorm.Error{Kind: errnorm.KindNetwork, Code: "network_error", Recoverable: &yes}, true},
		{&errnorm.Error{Kind: errnorm.KindRemote, Code: "remote_error", Details: map[string]any{"status": 503}}, true},
		{&errnorm.Error{Kind: errnorm.KindRemote, Code: "remote_error", Details: map[string]any{"status": 429}}, true},
		{&errnorm.Error{Kind: errnorm.KindRemote, Code: "remote_error", Details: map[string]any{"status": 404}}, false},
	}
	for i, c := range cases {
		if got := transientAwaitError(c.err); got != c.want {
			t.Errorf("case %d: got %v want %v", i, got, c.want)
		}
	}
}
