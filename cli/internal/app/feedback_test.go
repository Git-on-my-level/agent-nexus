package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Findings about ANX itself reached the maintainer only by chat. anx feedback
// files them on card:anx-feedback (created once) and links an upstream issue.
func TestFeedbackCreatesTheFeedbackCardOnceAndPostsTheNote(t *testing.T) {
	var mu sync.Mutex
	exists := false
	var creates, notes int
	var posted map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/cards/") && r.Method == http.MethodGet:
			if !exists {
				w.WriteHeader(404)
				fmt.Fprint(w, `{"error":{"code":"not_found","message":"card not found"}}`)
				return
			}
			fmt.Fprint(w, `{"card":{"id":"fb-1","ref":"card:anx-feedback","thread_id":"thread-fb","title":"ANX product feedback","column_key":"backlog"}}`)
		case r.URL.Path == "/work" && r.Method == http.MethodPost:
			creates++
			exists = true
			w.WriteHeader(201)
			fmt.Fprint(w, `{"work":{"id":"fb-1","ref":"card:anx-feedback"}}`)
		case r.URL.Path == "/events" && r.Method == http.MethodPost:
			notes++
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &posted)
			fmt.Fprint(w, `{"event":{"id":"evt-fb","type":"message_posted"}}`)
		default:
			fmt.Fprint(w, `{}`)
		}
	}))
	defer server.Close()
	a, out := dailyTestApp(t, server.URL)
	for i := 0; i < 2; i++ {
		out.Reset()
		if exit := a.Run([]string{"--json", "--as", "worker", "feedback", fmt.Sprintf("await --answers needs a deadline %d", i), "--evidence", "https://example.test/log"}); exit != 0 {
			t.Fatalf("exit=%d %s", exit, out.String())
		}
	}
	doc := dailyJSON(t, out)
	result := asMap(doc["result"])
	if creates != 1 || notes != 2 || result["card_ref"] != "card:anx-feedback" || result["note_ref"] != "event:evt-fb" {
		t.Fatalf("creates=%d notes=%d result=%v", creates, notes, result)
	}
	if !strings.HasPrefix(anyString(result["upstream_issue_url"]), "https://github.com/Git-on-my-level/agent-nexus/issues/new?") {
		t.Fatalf("upstream link: %v", result["upstream_issue_url"])
	}
	payload := asMap(asMap(posted["event"])["payload"])
	if !strings.HasPrefix(anyString(payload["text"]), "ANX feedback: await --answers") || asMap(payload["feedback"])["cli_version"] == nil {
		t.Fatalf("payload %v", payload)
	}
}

func TestFeedbackRequiresText(t *testing.T) {
	if _, err := parseFeedbackArgs(nil); err == nil {
		t.Fatal("feedback without text must be a usage error")
	}
	if p, err := parseFeedbackArgs([]string{"text", "--dry-run"}); err != nil || !p.dryRun {
		t.Fatalf("%v %v", p, err)
	}
}
