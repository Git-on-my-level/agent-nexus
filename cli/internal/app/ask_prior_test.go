package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// priorAskServer serves one card and a canned event list, newest first, and counts
// asks actually filed.
func priorAskServer(t *testing.T, events, notes string) (*httptest.Server, *int) {
	t.Helper()
	var mu sync.Mutex
	filed := new(int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/cards/"):
			fmt.Fprint(w, `{"card":{"id":"card-1","ref":"card:x","thread_id":"thread-1","title":"X"}}`)
		case r.URL.Path == "/events" && r.Method == http.MethodGet:
			if r.URL.Query().Get("type") == "message_posted" {
				fmt.Fprintf(w, `{"events":[%s]}`, notes)
				return
			}
			fmt.Fprintf(w, `{"events":[%s]}`, events)
		case r.URL.Path == "/events" && r.Method == http.MethodPost:
			mu.Lock()
			*filed++
			mu.Unlock()
			var doc map[string]any
			_ = json.NewDecoder(r.Body).Decode(&doc)
			json.NewEncoder(w).Encode(map[string]any{"event": map[string]any{"id": asMap(doc["event"])["id"], "type": humanAttentionRequestedEventType}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, filed
}

const askPriorRequest = `{"id":"ev_1","ref":"event:h1","type":"human_attention_requested","payload":{"kind":"ask","title":"Ship  it?"}}`

func runAskForTest(t *testing.T, server *httptest.Server, extra ...string) map[string]any {
	t.Helper()
	a, out := dailyTestApp(t, server.URL)
	argv := append([]string{"--json", "--as", "worker", "ask", "--title", "Ship it?", "--subject-ref", "card:x", "--situation", "Reports used to take ten minutes and now take forty five, which is what changed this week.", "--impact", "Everyone reading the morning report waits longer, and the choice depends on whether the delay is queueing.", "--recommend", "Measure first", "--why", "Costs one day", "--by", "2d", "--delivery", "await"}, extra...)
	if exit := a.Run(argv); exit != 0 {
		t.Fatalf("exit=%d %s", exit, out.String())
	}
	return dailyJSON(t, out)
}

func TestAskPriorState(t *testing.T) {
	for _, tc := range []struct {
		name, lifecycle, status, warning string
		filed                            int
	}{
		{"open returns it", "", "open", "ask_already_open", 0},
		{"answered by id", `{"id":"r","type":"human_attention_responded","payload":{"request_event_id":"ev_1","response":"Yes"}},`, "answered", "ask_already_answered", 0},
		{"answered by handle", `{"id":"r","type":"human_attention_responded","payload":{"request_event_ref":"event:h1","response":"Yes"}},`, "answered", "ask_already_answered", 0},
		{"withdrawn files anew", `{"id":"w","type":"human_attention_withdrawn","payload":{"request_event_ref":"event:ev_1"}},`, "", "", 1},
		{"retired files anew", `{"id":"w","type":"human_attention_retired","payload":{"request_event_id":"ev_1"}},`, "", "", 1},
		{"reopened is open", `{"id":"o","type":"human_attention_reopened","payload":{"request_event_id":"ev_1"}},{"id":"w","type":"human_attention_retired","payload":{"request_event_id":"ev_1"}},`, "open", "ask_already_open", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, filed := priorAskServer(t, tc.lifecycle+askPriorRequest, "")
			doc := runAskForTest(t, server)
			if *filed != tc.filed {
				t.Fatalf("filed=%d doc=%v", *filed, doc)
			}
			if tc.warning != "" && warningCodes(doc)[tc.warning] == "" {
				t.Fatalf("warnings=%#v", doc["warnings"])
			}
			if tc.status != "" {
				result := asMap(doc["result"])
				if anyString(result["status"]) != tc.status || anyString(result["ask_id"]) != "event:ev_1" {
					t.Fatalf("result=%#v", result)
				}
				if tc.status == "answered" && anyString(result["answer"]) != "Yes" {
					t.Fatalf("answer=%#v", result["answer"])
				}
			}
		})
	}
}

func TestAskPriorIgnoresOtherQuestionsAndAgainOverrides(t *testing.T) {
	other, filed := priorAskServer(t, strings.Replace(askPriorRequest, "Ship  it?", "Hold it?", 1), "")
	runAskForTest(t, other)
	if *filed != 1 {
		t.Fatalf("different question filed=%d", *filed)
	}
	same, filed := priorAskServer(t, askPriorRequest, "")
	runAskForTest(t, same, "--again")
	if *filed != 1 {
		t.Fatalf("--again filed=%d", *filed)
	}
}

func TestAskWarnsAboutRecordedRuling(t *testing.T) {
	server, filed := priorAskServer(t, "", `{"id":"n","type":"message_posted","payload":{"text":"Decided by Dana: ship Friday","decided_by":"Dana"}}`)
	doc := runAskForTest(t, server)
	if *filed != 1 || !strings.Contains(warningCodes(doc)["ask_prior_ruling"], "Decided by Dana: ship Friday") {
		t.Fatalf("filed=%d warnings=%#v", *filed, doc["warnings"])
	}
}
