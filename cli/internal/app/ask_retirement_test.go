package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestAskRetireCommandAndUndo(t *testing.T) {
	t.Parallel()
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" && r.URL.Path == "/asks/event:ask-123" {
			_, _ = w.Write([]byte(`{"ask_id":"event:ask-123","status":"withdrawn_as_stale","retirement_event_ref":"event:retirement-1"}`))
			return
		}
		if r.Method != "POST" || r.URL.Path != "/asks/event:ask-123/retire" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var b map[string]any
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			t.Error(err)
		}
		bodies = append(bodies, b)
		_, _ = w.Write([]byte(`{"ask_id":"event:ask-123","status":"withdrawn_as_stale","retirement_event_ref":"event:retirement-1","notified_actor_id":"requester"}`))
	}))
	defer server.Close()
	home := t.TempDir()
	writeDerivedAgentFixture(t, home, "agent-a", `{"agent":"agent-a","username":"agent.alpha","actor_id":"actor_curator","access_token":"token-a","access_token_expires_at":"2099-01-01T00:00:00Z"}`)
	run := func(args ...string) {
		assertEnvelopeOK(t, runCLIForTest(t, home, nil, nil, append([]string{"--json", "--base-url", server.URL, "--as", "agent-a"}, args...)))
	}
	args := []string{"ask", "retire", "event:ask-123", "--reason", "No longer relevant", "--evidence", "card:done-work", "--evidence", "event:conclusion"}
	run(args...)
	run(args...)
	if len(bodies) != 2 || !reflect.DeepEqual(bodies[0], bodies[1]) || bodies[0]["request_key"] == "" {
		t.Fatal(bodies)
	}
	if !reflect.DeepEqual(bodies[0]["evidence"], []any{"card:done-work", "event:conclusion"}) {
		t.Fatal(bodies)
	}
	run("ask", "retire", "event:ask-123", "--undo", "--request-key", "stable-undo")
	if bodies[2]["if_retirement"] != "event:retirement-1" || bodies[2]["undo"] != true || bodies[2]["reason"] != nil || bodies[2]["request_key"] != "stable-undo" {
		t.Fatal(bodies)
	}
	run("ask", "retire", "event:ask-123", "--undo", "--if-retirement", "event:exact-retirement", "--dry-run")
	if len(bodies) != 3 {
		t.Fatal("dry-run wrote")
	}
}
func TestAskRetireUsageBeforeIdentity(t *testing.T) {
	for _, args := range [][]string{{"ask", "retire"}, {"ask", "retire", "event:a"}, {"ask", "retire", "event:a", "--reason", "stale"}, {"ask", "retire", "event:a", "--undo", "--reason", "stale"}, {"ask", "retire", "event:a", "--unknown"}} {
		p := assertEnvelopeError(t, runCLIForTest(t, t.TempDir(), nil, nil, append([]string{"--json"}, args...)))
		code := asMap(p["error"])["code"]
		if code != "invalid_args" && code != "invalid_request" && code != "invalid_flags" {
			t.Fatal(p)
		}
	}
}
