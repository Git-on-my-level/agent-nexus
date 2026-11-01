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

const askBatchRetryFile = `{"asks":[
	{"title":"Ship the flag?","subject_ref":"card:launch","thread_id":"thr1","situation":"A launch decision is waiting.","impact":"The team cannot finish until we choose.","recommendation":{"action":"Ship","why":"This is the quickest safe approach."},"recommend":"Ship","alts":["Wait"],"coverage_hint":"thin"},
	{"title":"Roll back first?","subject_ref":"card:launch","thread_id":"thr1","situation":"A launch decision is waiting.","impact":"The team cannot finish until we choose.","recommendation":{"action":"Roll back","why":"This is the quickest safe approach."},"recommend":"Roll back","alts":["Wait"],"coverage_hint":"thin"}
]}`

// askBatchRetryServer files batches the way core does and can lose the response
// after the commit, which is what a client timeout looks like to the workspace.
type askBatchRetryServer struct {
	*httptest.Server
	mu            sync.Mutex
	events        []map[string]any
	posts         int
	subscribed    []string
	loseResponses bool
}

func newAskBatchRetryServer(t *testing.T) *askBatchRetryServer {
	t.Helper()
	s := &askBatchRetryServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/asks/batch":
			var body struct {
				Asks []map[string]any `json:"asks"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.posts++
			batch := fmt.Sprintf("askbatch_%d", s.posts)
			for _, ask := range body.Asks {
				ask["payload"].(map[string]any)["batch_id"] = batch
				s.events = append(s.events, ask)
			}
			if s.loseResponses {
				conn, _, _ := w.(http.Hijacker).Hijack()
				_ = conn.Close()
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"batch_id": batch})
		case r.Method == http.MethodGet && r.URL.Path == "/events":
			newestFirst := make([]map[string]any, 0, len(s.events))
			for i := len(s.events) - 1; i >= 0; i-- {
				newestFirst = append(newestFirst, s.events[i])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"events": newestFirst})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/subscriptions"):
			s.subscribed = append(s.subscribed, r.URL.Path)
			_, _ = w.Write([]byte(`{"id":"sub_1","kind":"bridge","label":"host resume"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *askBatchRetryServer) postCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.posts
}

// record adds a lifecycle event for the nth filed ask.
func (s *askBatchRetryServer) record(eventType string, n int, payload map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload["request_event_id"] = s.events[n]["id"]
	s.events = append(s.events, map[string]any{"id": fmt.Sprintf("ev_life_%d", len(s.events)), "type": eventType, "payload": payload})
}

func askBatchRetryArgs(s *askBatchRetryServer, path string, extra ...string) []string {
	return append([]string{"--json", "--base-url", s.URL, "--as", "agent-a", "ask", "batch", "--from-file", path}, extra...)
}

func askBatchRetryHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	writeDerivedAgentFixture(t, home, "agent-a", `{"agent":"agent-a","username":"agent.alpha","actor_id":"actor_asker","access_token":"token-a","access_token_expires_at":"2099-01-01T00:00:00Z"}`)
	return home
}

func askBatchRetryViews(t *testing.T, raw string) (map[string]any, []map[string]any) {
	t.Helper()
	result, _ := assertEnvelopeOK(t, raw)["result"].(map[string]any)
	views := []map[string]any{}
	for _, view := range asSlice(result["asks"]) {
		views = append(views, asMap(view))
	}
	if len(views) != 2 {
		t.Fatalf("expected both asks in the result: %s", raw)
	}
	return result, views
}

func TestAskBatchRetryAfterLostResponseFilesOnce(t *testing.T) {
	s := newAskBatchRetryServer(t)
	s.loseResponses = true
	home := askBatchRetryHome(t)
	path := askBatchFixture(t, askBatchRetryFile)

	first := runCLIForTest(t, home, map[string]string{}, nil, askBatchRetryArgs(s, path))
	if s.postCount() != 1 {
		t.Fatalf("first attempt posted %d times: %s", s.postCount(), first)
	}
	s.loseResponses = false

	raw := runCLIForTest(t, home, map[string]string{}, nil, askBatchRetryArgs(s, path))
	if s.postCount() != 1 {
		t.Fatalf("retry filed the batch again: %d posts", s.postCount())
	}
	result, views := askBatchRetryViews(t, raw)
	if result["created"] != false || result["batch_id"] != "askbatch_1" {
		t.Fatalf("retry result: %s", raw)
	}
	for i, view := range views {
		if view["ask_id"] != "event:"+anyStringValue(s.events[i]["id"]) || view["status"] != "open" || view["event"] != nil {
			t.Fatalf("asks[%d] is not the filed ask: %#v", i, view)
		}
	}
	if !strings.Contains(raw, "ask_already_open") {
		t.Fatalf("retry did not say the asks were already open: %s", raw)
	}
}

func TestAskBatchRetryCarriesAnAnswerAndRestoresResume(t *testing.T) {
	s := newAskBatchRetryServer(t)
	s.loseResponses = true
	home := askBatchRetryHome(t)
	path := askBatchFixture(t, askBatchRetryFile)
	env := map[string]string{"AGENTCTL_EXECUTION_ID": "exec-1"}

	first := runCLIForTest(t, home, env, nil, askBatchRetryArgs(s, path, "--delivery", "resume"))
	if s.postCount() != 1 {
		t.Fatalf("first attempt posted %d times: %s", s.postCount(), first)
	}
	s.loseResponses = false
	s.record("human_attention_responded", 0, map[string]any{"response_text": "Ship it"})

	raw := runCLIForTest(t, home, env, nil, askBatchRetryArgs(s, path, "--delivery", "resume"))
	if s.postCount() != 1 {
		t.Fatalf("retry filed the batch again: %d posts", s.postCount())
	}
	_, views := askBatchRetryViews(t, raw)
	if views[0]["status"] != "answered" || views[0]["answer"] != "Ship it" || views[1]["status"] != "open" {
		t.Fatalf("statuses: %#v", views)
	}
	want := "/asks/" + anyStringValue(s.events[1]["id"]) + "/subscriptions"
	if len(s.subscribed) != 1 || s.subscribed[0] != want {
		t.Fatalf("resume should be restored only for the open ask %s: %v", want, s.subscribed)
	}
}

func TestAskBatchFilesAgainOnlyWhenItShould(t *testing.T) {
	file := func(t *testing.T) (*askBatchRetryServer, string, string) {
		t.Helper()
		s := newAskBatchRetryServer(t)
		home := askBatchRetryHome(t)
		path := askBatchFixture(t, askBatchRetryFile)
		runCLIForTest(t, home, map[string]string{}, nil, askBatchRetryArgs(s, path))
		return s, home, path
	}

	t.Run("again files a new batch", func(t *testing.T) {
		s, home, path := file(t)
		runCLIForTest(t, home, map[string]string{}, nil, askBatchRetryArgs(s, path, "--again"))
		if s.postCount() != 2 {
			t.Fatalf("posts=%d", s.postCount())
		}
	})
	t.Run("withdrawn asks can be asked again", func(t *testing.T) {
		s, home, path := file(t)
		s.record("human_attention_withdrawn", 0, map[string]any{})
		s.record("human_attention_withdrawn", 1, map[string]any{})
		runCLIForTest(t, home, map[string]string{}, nil, askBatchRetryArgs(s, path))
		if s.postCount() != 2 {
			t.Fatalf("posts=%d", s.postCount())
		}
	})
	t.Run("a partly repeated batch files nothing and names the repeat", func(t *testing.T) {
		s, home, path := file(t)
		s.record("human_attention_withdrawn", 0, map[string]any{})
		raw := runCLIForTest(t, home, map[string]string{}, nil, askBatchRetryArgs(s, path))
		if s.postCount() != 1 {
			t.Fatalf("posts=%d", s.postCount())
		}
		if want := "asks[1] is event:" + anyStringValue(s.events[1]["id"]); !strings.Contains(raw, want) || !strings.Contains(raw, "--again") {
			t.Fatalf("error should name %s and --again: %s", want, raw)
		}
	})
	t.Run("dry run reads nothing", func(t *testing.T) {
		s, home, path := file(t)
		raw := runCLIForTest(t, home, map[string]string{}, nil, askBatchRetryArgs(s, path, "--dry-run"))
		if s.postCount() != 1 || strings.Contains(raw, "ask_already_open") {
			t.Fatalf("posts=%d %s", s.postCount(), raw)
		}
	})
}
