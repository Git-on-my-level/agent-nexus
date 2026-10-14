package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func dailyTestApp(t *testing.T, serverURL string) (*App, *bytes.Buffer) {
	t.Helper()
	home := t.TempDir()
	writeDerivedAgentFixture(t, home, "worker", `{"agent_id":"agent-1","actor_id":"actor-1","username":"worker.host","access_token":"test-token","access_token_expires_at":"2099-01-01T00:00:00Z"}`)
	_ = runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "--base-url", serverURL, "--as", "worker", "version"})
	out := &bytes.Buffer{}
	a := newTestApp(t)
	a.Stdout = out
	a.Stderr = &bytes.Buffer{}
	a.UserHomeDir = func() (string, error) { return home, nil }
	a.Getenv = func(k string) string {
		if k == "HOME" {
			return home
		}
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
		{"needs_context", "More evidence", "needs_context", 10, 1, false},
		{"withdrawn", "Closed", "withdrawn", 11, 1, false},
		{"expired", "Expired", "expired", 12, 1, false},
		{"reconnect", "After reconnect", "answered", 0, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var opened atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.URL.Path == "/asks/ask-1/subscriptions" || r.URL.Path == "/asks/ask-1/delivery":
					fmt.Fprint(w, `{"id":"sub-1"}`)
				case r.URL.Path == "/events/ask-1":
					fmt.Fprint(w, `{"event":{"id":"ask-1","type":"human_attention_requested","thread_id":"thread-1"}}`)
				case r.URL.Path == "/events":
					fmt.Fprint(w, `{"events":[]}`)
				case r.URL.Path == "/asks/ask-1":
					fmt.Fprint(w, `{"status":"open"}`)
				case r.URL.Path == "/stream/asks/ask-1":
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
					status := "answered"
					if tc.wantExit >= 10 {
						status = tc.outcome
					}
					payload := fmt.Sprintf(`{"status":%q,"response":{"response_event_id":"response-1","response_text":%q,"outcome":%q,"responding_actor_id":"human-1"}}`, status, tc.answer, tc.outcome)
					fmt.Fprintf(w, "event: outcome\ndata: %s\n\n", payload)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			a, out := dailyTestApp(t, server.URL)
			exit := a.Run([]string{"--json", "--as", "worker", "await", "event:ask-1", "--timeout", "700ms"})
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

func TestAwaitAccessRequestUsesTypedRefAndExitCodes(t *testing.T) {
	for _, tc := range []struct {
		name, outcome string
		wantExit      int
	}{
		{"approved", "approved", 0},
		{"denied", "rejected", 9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var subscribed, streamed string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.URL.Path == "/asks/access-request:req-1/subscriptions" || r.URL.Path == "/asks/access-request:req-1/delivery":
					subscribed = r.URL.Path
					fmt.Fprint(w, `{"id":"sub-1"}`)
				case r.URL.Path == "/asks/access-request:req-1":
					fmt.Fprint(w, `{"status":"open"}`)
				case r.URL.Path == "/stream/asks/access-request:req-1":
					streamed = r.URL.Path
					w.Header().Set("Content-Type", "text/event-stream")
					payload := fmt.Sprintf(`{"status":"answered","access_request_ref":"access-request:req-1","response":{"response_text":"Access request %s","outcome":%q,"responding_actor_id":"human-1"}}`, tc.name, tc.outcome)
					fmt.Fprintf(w, "event: outcome\ndata: %s\n\n", payload)
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			a, out := dailyTestApp(t, server.URL)
			exit := a.Run([]string{"--json", "--as", "worker", "await", "access-request:req-1", "--timeout", "2s"})
			if exit != tc.wantExit {
				t.Fatalf("exit=%d want=%d output=%s", exit, tc.wantExit, out.String())
			}
			if subscribed == "" || streamed == "" {
				t.Fatalf("typed ref was rewritten: subscribed=%q streamed=%q", subscribed, streamed)
			}
			doc := dailyJSON(t, out)
			if tc.wantExit == 0 && anyString(asMap(doc["result"])["outcome"]) != "approved" {
				t.Fatalf("approved result: %s", out.String())
			}
			if tc.wantExit == 9 {
				errDoc := asMap(doc["error"])
				if anyString(errDoc["message"]) != "access request denied" {
					t.Fatalf("denied message: %s", out.String())
				}
			}
		})
	}
}

func TestAwaitAnswersReturnsUnreadAnswerBatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/agents/agent-1":
			fmt.Fprint(w, `{"agent":{"id":"agent-1","actor_id":"actor-1","handle":"worker.host"}}`)
		case r.URL.Path == "/agent-inbox/asks":
			fmt.Fprint(w, `{"items":[{"ask_id":"event:ask-1","status":"answered","title":"First","answer":{"text":"Yes","outcome":"answered","response_event_id":"response-1"},"answer_unread":true},{"ask_id":"event:ask-2","status":"answered","title":"Second","answer":{"text":"Ship it","outcome":"approved","response_event_id":"response-2"},"answer_unread":true}],"page_info":{"has_more":false}}`)
		case r.URL.Path == "/agent-notifications":
			fmt.Fprint(w, `{"items":[{"wakeup_id":"wake-1","status":"unread","trigger_event_id":"response-2","related_refs":["event:response-1","event:response-2"]}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	a, out := dailyTestApp(t, server.URL)
	if exit := a.Run([]string{"--json", "--as", "worker", "await", "--answers", "--timeout", "1s"}); exit != 0 {
		t.Fatalf("await answers exit=%d output=%s", exit, out.String())
	}
	result := asMap(dailyJSON(t, out)["result"])
	answers := asSlice(result["answers"])
	if result["count"] != float64(2) || len(answers) != 2 {
		t.Fatalf("expected both answers in the returned batch: %#v", result)
	}
}

func TestAwaitAnswersFindsWakeForAnswerBehindInterveningEvents(t *testing.T) {
	var pages int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/agents/agent-1":
			fmt.Fprint(w, `{"agent":{"id":"agent-1","actor_id":"actor-1","handle":"worker.host"}}`)
		case r.URL.Path == "/agent-notifications":
			fmt.Fprint(w, `{"items":[{"wakeup_id":"wake-old","status":"unread","trigger_event_id":"response-old","related_refs":["event:response-old"]}]}`)
		case r.URL.Path == "/agent-inbox/asks":
			pages++
			if r.URL.Query().Get("cursor") == "" {
				items := make([]map[string]any, 200)
				for i := range items {
					items[i] = map[string]any{"ask_id": fmt.Sprintf("event:open-%03d", i), "status": "open", "answer_unread": false}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "page_info": map[string]any{"next_cursor": "older-page", "has_more": true}})
				return
			}
			if r.URL.Query().Get("cursor") != "older-page" {
				t.Errorf("unexpected inbox cursor %q", r.URL.Query().Get("cursor"))
			}
			fmt.Fprint(w, `{"items":[{"ask_id":"event:old-ask","status":"answered","title":"Old answer","answer":{"response_event_id":"response-old","text":"Yes"},"answer_unread":true}],"page_info":{"has_more":false}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	a, out := dailyTestApp(t, server.URL)
	if exit := a.Run([]string{"--json", "--as", "worker", "await", "--answers", "--timeout", "1s"}); exit != 0 {
		t.Fatalf("await answers exit=%d output=%s", exit, out.String())
	}
	result := asMap(dailyJSON(t, out)["result"])
	answers := asSlice(result["answers"])
	if pages != 2 || result["count"] != float64(1) || len(answers) != 1 || anyString(asMap(answers[0])["ask_id"]) != "event:old-ask" {
		t.Fatalf("await lost answer behind intervening events: pages=%d result=%#v", pages, result)
	}
}

func TestAwaitAnswersFlagsNotifiedAnswersBeyondTheScanWindow(t *testing.T) {
	page := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/agents/agent-1":
			fmt.Fprint(w, `{"agent":{"id":"agent-1","actor_id":"actor-1","handle":"worker.host"}}`)
		case r.URL.Path == "/agent-notifications":
			fmt.Fprint(w, `{"items":[{"trigger_event_id":"response-window","related_refs":["event:response-window"]},{"trigger_event_id":"response-beyond","related_refs":["event:response-beyond"]}]}`)
		case r.URL.Path == "/agent-inbox/asks":
			page++
			body := map[string]any{}
			if page == 2 {
				body["items"] = []map[string]any{{"ask_id": "event:window-ask", "status": "answered", "answer": map[string]any{"response_event_id": "response-window", "text": "Yes"}, "answer_unread": true}}
			} else {
				body["items"] = []map[string]any{{"ask_id": fmt.Sprintf("event:open-%d", page), "status": "open", "answer_unread": false}}
			}
			if page < 10 {
				body["page_info"] = map[string]any{"next_cursor": fmt.Sprintf("p%d", page), "has_more": true}
			} else {
				body["page_info"] = map[string]any{"has_more": false}
			}
			_ = json.NewEncoder(w).Encode(body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	a, out := dailyTestApp(t, server.URL)
	if exit := a.Run([]string{"--json", "--as", "worker", "await", "--answers", "--timeout", "2s"}); exit != 0 {
		t.Fatalf("await answers exit=%d output=%s", exit, out.String())
	}
	result := asMap(dailyJSON(t, out)["result"])
	answers := asSlice(result["answers"])
	if result["count"] != float64(1) || len(answers) != 1 || anyString(asMap(answers[0])["ask_id"]) != "event:window-ask" {
		t.Fatalf("window answer missing: %#v", result)
	}
	if result["incomplete"] != true {
		t.Fatalf("partial batch must be marked incomplete: %#v", result)
	}
	unmatched := asSlice(result["unmatched_notification_ids"])
	if len(unmatched) != 1 || anyString(unmatched[0]) != "response-beyond" {
		t.Fatalf("unmatched notification ids missing: %#v", result)
	}
	warnings := asSlice(dailyJSON(t, out)["warnings"])
	foundWarning := false
	for _, raw := range warnings {
		if anyString(asMap(raw)["code"]) == "ask_delivery" && strings.Contains(anyString(asMap(raw)["message"]), "inbox list --status answered --unread") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Fatalf("continuation warning missing: %s", out.String())
	}
}

func TestAwaitAnswersTimeoutNamesUnmatchedNotifications(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/agents/agent-1":
			fmt.Fprint(w, `{"agent":{"id":"agent-1","actor_id":"actor-1","handle":"worker.host"}}`)
		case r.URL.Path == "/agent-notifications":
			fmt.Fprint(w, `{"items":[{"trigger_event_id":"response-beyond","related_refs":["event:response-beyond"]}]}`)
		case r.URL.Path == "/agent-inbox/asks":
			fmt.Fprint(w, `{"items":[{"ask_id":"event:open-1","status":"open","answer_unread":false}],"page_info":{"has_more":false}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	a, out := dailyTestApp(t, server.URL)
	if exit := a.Run([]string{"--json", "--as", "worker", "await", "--answers", "--timeout", "300ms"}); exit != 8 {
		t.Fatalf("await answers exit=%d output=%s", exit, out.String())
	}
	doc := dailyJSON(t, out)
	err := asMap(doc["error"])
	details := asMap(err["details"])
	unmatched := asSlice(details["unmatched_notification_ids"])
	if len(unmatched) != 1 || anyString(unmatched[0]) != "response-beyond" {
		t.Fatalf("timeout must name unmatched notifications: %s", out.String())
	}
	if !strings.Contains(anyString(details["continuation"]), "inbox list --status answered --unread") {
		t.Fatalf("timeout must carry the continuation path: %s", out.String())
	}
}

func TestAwaitAnswersWaitsForDurableWakeDuringQuietWindow(t *testing.T) {
	var inboxQueries atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/agents/agent-1":
			fmt.Fprint(w, `{"agent":{"id":"agent-1","actor_id":"actor-1","handle":"worker.host"}}`)
		case "/agent-notifications":
			fmt.Fprint(w, `{"items":[]}`)
		case "/agent-inbox/asks":
			inboxQueries.Add(1)
			fmt.Fprint(w, `{"items":[{"ask_id":"event:ask-quiet","status":"answered","answer":{"response_event_id":"response-quiet","text":"Yes"},"answer_unread":true}],"page_info":{"has_more":false}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	a, out := dailyTestApp(t, server.URL)
	if exit := a.Run([]string{"--json", "--as", "worker", "await", "--answers", "--timeout", "75ms"}); exit == 0 {
		t.Fatalf("await returned before a durable wake existed: %s", out.String())
	}
	if inboxQueries.Load() != 0 {
		t.Fatalf("await inspected %d inbox page(s) without a wake", inboxQueries.Load())
	}
}

func TestInboxListFiltersOpenAnsweredAllAndUnread(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/agents/agent-1":
			fmt.Fprint(w, `{"agent":{"id":"agent-1","actor_id":"actor-1","handle":"worker.host"}}`)
		case r.URL.Path == "/agent-inbox/asks":
			fmt.Fprint(w, `{"items":[{"ask_id":"event:ask-1","status":"answered","title":"Answered","answer":{"text":"Yes","outcome":"answered","response_event_id":"response-1"},"answer_unread":true},{"ask_id":"event:ask-2","status":"open","title":"Still open","answer":null,"answer_unread":false}],"page_info":{"has_more":false}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	for _, tc := range []struct {
		name       string
		args       []string
		wantCount  int
		wantAsk    string
		wantUnread bool
	}{
		{name: "open", args: []string{"--status", "open"}, wantCount: 1, wantAsk: "event:ask-2"},
		{name: "answered", args: []string{"--status", "answered"}, wantCount: 1, wantAsk: "event:ask-1", wantUnread: true},
		{name: "all", args: []string{"--status", "all"}, wantCount: 2},
		{name: "unread answers", args: []string{"--status", "answered", "--unread"}, wantCount: 1, wantAsk: "event:ask-1", wantUnread: true},
		{name: "unread implies answered", args: []string{"--unread"}, wantCount: 1, wantAsk: "event:ask-1", wantUnread: true},
		{name: "explicit open with unread", args: []string{"--status", "open", "--unread"}, wantCount: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, out := dailyTestApp(t, server.URL)
			args := append([]string{"--json", "--as", "worker", "inbox", "list"}, tc.args...)
			if exit := a.Run(args); exit != 0 {
				t.Fatalf("inbox list exit=%d output=%s", exit, out.String())
			}
			result := asMap(dailyJSON(t, out)["result"])
			items := asSlice(result["items"])
			if len(items) != tc.wantCount {
				t.Fatalf("matched %d asks, want %d: %#v", len(items), tc.wantCount, result)
			}
			if tc.wantAsk != "" {
				item := asMap(items[0])
				if anyString(item["ask_id"]) != tc.wantAsk || item["answer_unread"] != tc.wantUnread {
					t.Fatalf("wrong ask or read state: %#v", item)
				}
			}
		})
	}
}

// One request reads exactly one bounded page; older answers behind newer
// activity surface through the returned continuation cursor, never an
// implicit client-side loop over the ask history.
func TestInboxListStaysBoundedAndContinuesThroughCursor(t *testing.T) {
	var pages []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/agents/agent-1":
			fmt.Fprint(w, `{"agent":{"id":"agent-1","actor_id":"actor-1","handle":"worker.host"}}`)
		case "/agent-inbox/asks":
			pages = append(pages, r.URL.Query().Get("cursor"))
			if r.URL.Query().Get("cursor") == "" {
				items := make([]map[string]any, 200)
				for i := range items {
					items[i] = map[string]any{"ask_id": fmt.Sprintf("event:open-%03d", i), "status": "open", "answer_unread": false}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "page_info": map[string]any{"next_cursor": "older-page", "has_more": true}})
				return
			}
			if r.URL.Query().Get("cursor") != "older-page" {
				t.Errorf("unexpected inbox cursor %q", r.URL.Query().Get("cursor"))
			}
			fmt.Fprint(w, `{"items":[{"ask_id":"event:old-ask","status":"answered","title":"Old ask answered today","answer":{"response_event_id":"response-old","text":"Yes"},"answer_unread":true}],"page_info":{"has_more":false}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	a, out := dailyTestApp(t, server.URL)
	if exit := a.Run([]string{"--json", "--as", "worker", "inbox", "list", "--status", "answered"}); exit != 0 {
		t.Fatalf("inbox list exit=%d output=%s", exit, out.String())
	}
	result := asMap(dailyJSON(t, out)["result"])
	if len(pages) != 1 {
		t.Fatalf("inbox list must read exactly one page, read %d: %v", len(pages), pages)
	}
	if len(asSlice(result["items"])) != 0 || anyString(result["next_cursor"]) != "older-page" {
		t.Fatalf("expected an empty bounded page plus continuation cursor: %#v", result)
	}
	next := asSlice(result["next"])
	if len(next) != 9 || anyString(next[2]) != "list" || anyString(next[7]) != "--cursor" || anyString(next[8]) != "older-page" {
		t.Fatalf("continuation command missing: %#v", next)
	}

	// Following the printed command reaches the older answer.
	out.Reset()
	if exit := a.Run([]string{"--json", "--as", "worker", "inbox", "list", "--status", "answered", "--limit", "50", "--cursor", "older-page"}); exit != 0 {
		t.Fatalf("continuation exit=%d output=%s", exit, out.String())
	}
	continued := asMap(dailyJSON(t, out)["result"])
	items := asSlice(continued["items"])
	if len(items) != 1 || anyString(asMap(items[0])["ask_id"]) != "event:old-ask" {
		t.Fatalf("older answer behind newer activity was lost: %#v", continued)
	}
}

func TestInboxReadMarksOnlyTheSelectedAnswerReadBeforeWakeDelivery(t *testing.T) {
	var readAnswer string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/agents/agent-1":
			fmt.Fprint(w, `{"agent":{"id":"agent-1","actor_id":"actor-1","handle":"worker.host"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/agent-inbox/asks":
			fmt.Fprint(w, `{"items":[{"ask_id":"event:ask-1","status":"answered","answer":{"response_event_id":"response-1","response_event_ref":"event:response-1"},"answer_unread":true}],"page_info":{"has_more":false}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/agent-inbox/answers/read":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode read request: %v", err)
			}
			readAnswer = anyString(body["answer_event_id"])
			fmt.Fprint(w, `{"answer":{"answer_event_id":"response-1","read":true,"already_read":false}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	a, out := dailyTestApp(t, server.URL)
	if exit := a.Run([]string{"--json", "--as", "worker", "inbox", "read", "event:ask-1"}); exit != 0 {
		t.Fatalf("inbox read exit=%d output=%s", exit, out.String())
	}
	result := asMap(dailyJSON(t, out)["result"])
	if readAnswer != "response-1" || result["status"] != "read" || result["ask_id"] != "event:ask-1" {
		t.Fatalf("answer read state was not persisted individually: request=%q result=%#v", readAnswer, result)
	}
}

func TestHumanGroupRemoved(t *testing.T) {
	a, out := dailyTestApp(t, "http://127.0.0.1:1")
	exit := a.Run([]string{"--json", "--as", "worker", "human", "ask"})
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
	if exit := a.Run([]string{"--json", "--as", "worker", "await", "card:task", "--until", "state=done", "--timeout", "1s"}); exit != 0 {
		t.Fatalf("exit=%d %s", exit, out.String())
	}
	result := asMap(dailyJSON(t, out)["result"])
	if result["state"] != "done" {
		t.Fatalf("state missing: %#v", result)
	}
}
