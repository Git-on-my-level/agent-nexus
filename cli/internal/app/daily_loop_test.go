package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func dailyTestApp(t *testing.T, serverURL string) (*App, *bytes.Buffer) {
	t.Helper()
	home := t.TempDir()
	writeAgentProfile(t, home, "worker", `{"agent_id":"agent-1","actor_id":"actor-1","username":"worker.host","access_token":"test-token","access_token_expires_at":"2099-01-01T00:00:00Z"}`)
	out := &bytes.Buffer{}
	a := New()
	a.Stdout = out
	a.Stderr = &bytes.Buffer{}
	a.UserHomeDir = func() (string, error) { return home, nil }
	a.Getenv = func(k string) string {
		if k == "ANX_BASE_URL" {
			return serverURL
		}
		return ""
	}
	return a, out
}
func dailyJSON(t *testing.T, out *bytes.Buffer) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatalf("one JSON document: %v: %s", err, out.String())
	}
	return v
}

func TestOrientIncludesSecondaryAssigneeAndAnswers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/agents/agent-1":
			fmt.Fprint(w, `{"agent":{"id":"agent-1","actor_id":"actor-1","handle":"worker.host","host_slug":"host","current_card_ref":"card:task"},"open_asks":[],"recent_notes":[]}`)
		case r.URL.Path == "/work":
			fmt.Fprint(w, `{"work":[{"ref":"card:task","title":"Task","phase":"in_progress","assignee_refs":["actor:other","actor:actor-1"]}],"has_more":false}`)
		case r.URL.Path == "/events" && r.URL.Query().Get("type") == "human_attention_requested":
			fmt.Fprint(w, `{"events":[{"id":"ask-2","payload":{"requester_actor_id":"actor-1","title":"Still pending","subject_ref":"card:task"}},{"id":"ask-1","payload":{"requester_actor_id":"actor-1","title":"Need answer","subject_ref":"card:task"}}]}`)
		case r.URL.Path == "/events" && r.URL.Query().Get("type") == "human_attention_responded":
			fmt.Fprint(w, `{"events":[{"id":"response-1","ts":"2026-09-27T00:00:00Z","payload":{"requester_actor_id":"actor-1","request_event_ref":"event:ask-1","response_text":"Yes","responding_actor_id":"human-1"}}]}`)
		case r.URL.Path == "/agent-notifications":
			fmt.Fprint(w, `{"items":[{"status":"unread","trigger_event_id":"response-1"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	a, out := dailyTestApp(t, server.URL)
	if exit := a.Run([]string{"--json", "--agent", "worker", "orient"}); exit != 0 {
		t.Fatalf("orient exit=%d output=%s", exit, out.String())
	}
	result := asMap(dailyJSON(t, out)["result"])
	if result["my_work_matched"] != float64(1) {
		t.Fatalf("secondary assignee omitted: %#v", result)
	}
	if len(asSlice(result["stale_items"])) != 1 {
		t.Fatalf("stale item omitted: %#v", result)
	}
	asks := asSlice(result["my_asks_and_answers"])
	if len(asks) != 2 || anyString(asMap(asks[0])["ask_id"]) != "event:ask-1" || asMap(asks[0])["answer_unread"] != true || anyString(asMap(asMap(asks[0])["answer"])["text"]) != "Yes" {
		t.Fatalf("answer omitted: %#v", asks)
	}
}

func TestAwaitOutcomesTimeoutAndReconnect(t *testing.T) {
	for _, tc := range []struct {
		name, answer, outcome string
		wantExit, connections int
		reconnect             bool
	}{
		{"answered", "Declined", "answered", 0, 1, false},
		{"approved", "Looks good", "approved", 0, 1, false},
		{"acknowledged", "Noted", "acknowledged", 0, 1, false},
		{"rejected", "Approved.", "rejected", 9, 1, false},
		{"timeout", "", "", 8, 1, false},
		{"reconnect", "After reconnect", "answered", 0, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var opened atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.URL.Path == "/events/ask-1":
					fmt.Fprint(w, `{"event":{"id":"ask-1","type":"human_attention_requested","thread_id":"thread-1"}}`)
				case r.URL.Path == "/events":
					fmt.Fprint(w, `{"events":[]}`)
				case r.URL.Path == "/stream/events":
					n := opened.Add(1)
					w.Header().Set("Content-Type", "text/event-stream")
					if tc.name == "timeout" {
						<-r.Context().Done()
						return
					}
					if tc.reconnect && n == 1 {
						fmt.Fprint(w, "id: first\nevent: event\ndata: {\"event\":{\"type\":\"other\"}}\n\n")
						return
					}
					payload := fmt.Sprintf(`{"event":{"id":"response-1","type":"human_attention_responded","payload":{"request_event_ref":"event:ask-1","response_text":%q,"outcome":%q,"responding_actor_id":"human-1","subject_ref":"card:task"}}}`, tc.answer, tc.outcome)
					fmt.Fprintf(w, "id: response-1\nevent: event\ndata: %s\n\n", payload)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			a, out := dailyTestApp(t, server.URL)
			exit := a.Run([]string{"--json", "--agent", "worker", "await", "event:ask-1", "--timeout", "700ms"})
			if exit != tc.wantExit {
				t.Fatalf("exit=%d want=%d output=%s", exit, tc.wantExit, out.String())
			}
			doc := dailyJSON(t, out)
			if tc.wantExit == 0 && anyString(asMap(doc["result"])["answer"]) != tc.answer {
				t.Fatalf("answer missing: %s", out.String())
			}
			if tc.wantExit == 0 && anyString(asMap(doc["result"])["outcome"]) != tc.outcome {
				t.Fatalf("outcome missing: %s", out.String())
			}
			if tc.wantExit == 9 {
				details := asMap(asMap(doc["error"])["details"])
				if anyString(details["outcome"]) != "rejected" || anyString(details["answer"]) != tc.answer || anyString(details["responder"]) != "human-1" {
					t.Fatalf("rejected response details missing: %s", out.String())
				}
			}
			if tc.wantExit != 0 && int(asMap(doc["error"])["exit_code"].(float64)) != tc.wantExit {
				t.Fatalf("wrong error document: %s", out.String())
			}
			if tc.name != "timeout" && int(opened.Load()) < tc.connections {
				t.Fatalf("connections=%d", opened.Load())
			}
		})
	}
}

func TestHumanGroupRemoved(t *testing.T) {
	a, out := dailyTestApp(t, "http://127.0.0.1:1")
	exit := a.Run([]string{"--json", "--agent", "worker", "human", "ask"})
	if exit != 2 || anyString(asMap(dailyJSON(t, out)["error"])["code"]) != "unknown_command" {
		t.Fatalf("old group dispatched: %s", out.String())
	}
}

func TestAwaitCardState(t *testing.T) {
	var done atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/cards/card:task":
			phase := "ready"
			if done.Load() {
				phase = "done"
			}
			fmt.Fprintf(w, `{"card":{"ref":"card:task","column_key":%q}}`, phase)
		case "/stream/events":
			done.Store(true)
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "id: moved\nevent: event\ndata: {\"event\":{\"type\":\"card_moved\"}}\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	a, out := dailyTestApp(t, server.URL)
	if exit := a.Run([]string{"--json", "--agent", "worker", "await", "card:task", "--until", "state=done", "--timeout", "1s"}); exit != 0 {
		t.Fatalf("exit=%d %s", exit, out.String())
	}
	result := asMap(dailyJSON(t, out)["result"])
	if result["state"] != "done" {
		t.Fatalf("state missing: %#v", result)
	}
}
