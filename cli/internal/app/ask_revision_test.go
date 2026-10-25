package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAskReviseCommandAndUndo(t *testing.T) {
	t.Parallel()
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" && r.URL.Path == "/asks/event:ask-123/presentation" {
			_, _ = w.Write([]byte(`{"ask_id":"event:ask-123","presentation_revision":3,"presentation":{"title":"Current?"}}`))
			return
		}
		if r.Method != "POST" || r.URL.Path != "/asks/event:ask-123/revise" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		bodies = append(bodies, body)
		_, _ = w.Write([]byte(`{"ask_id":"event:ask-123","presentation_revision":4,"presentation":{"title":"Plain question?"}}`))
	}))
	defer server.Close()
	home := t.TempDir()
	writeDerivedAgentFixture(t, home, "agent-a", `{"agent":"agent-a","username":"agent.alpha","actor_id":"actor_asker","access_token":"token-a","access_token_expires_at":"2099-01-01T00:00:00Z"}`)
	path := filepath.Join(t.TempDir(), "presentation.json")
	if err := os.WriteFile(path, []byte(`{"if_revision":3,"presentation":{"title":"Plain question?","details":"More context."}}`), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) map[string]any {
		return assertEnvelopeOK(t, runCLIForTest(t, home, map[string]string{}, nil, append([]string{"--json", "--base-url", server.URL, "--as", "agent-a"}, args...)))
	}
	run("ask", "presentation", "event:ask-123")
	result := run("ask", "revise", "event:ask-123", "--from-file", path)
	if anyStringValue(result["command"]) != "ask revise" {
		t.Fatal(result)
	}
	run("ask", "revise", "event:ask-123", "--from-file", path)
	if len(bodies) != 2 || !reflect.DeepEqual(bodies[0], bodies[1]) || bodies[0]["request_key"] == "" {
		t.Fatalf("retry body/key changed: %#v", bodies)
	}
	run("ask", "revise", "event:ask-123", "--undo", "--request-key", "undo-exact-key")
	if bodies[2]["if_revision"] != float64(3) || bodies[2]["undo"] != true || bodies[2]["request_key"] != "undo-exact-key" || bodies[2]["presentation"] != nil {
		t.Fatal(bodies[2])
	}
	run("ask", "revise", "event:ask-123", "--undo", "--if-revision", "2")
	if bodies[3]["if_revision"] != float64(2) {
		t.Fatal(bodies[3])
	}
	run("ask", "revise", "event:ask-123", "--from-file", path, "--dry-run")
	if len(bodies) != 4 {
		t.Fatal("dry run wrote")
	}
}

func TestAskRevisionUsageBeforeIdentity(t *testing.T) {
	for _, args := range [][]string{
		{"ask", "revise"},
		{"ask", "revise", "event:ask-1"},
		{"ask", "revise", "event:ask-1", "--from-file", "copy.json", "--undo"},
		{"ask", "revise", "event:ask-1", "--undo", "--if-revision", "-1"},
		{"ask", "revise", "event:ask-1", "--unknown"},
	} {
		raw := runCLIForTest(t, t.TempDir(), nil, nil, append([]string{"--json"}, args...))
		payload := assertEnvelopeError(t, raw)
		code := asMap(payload["error"])["code"]
		if code != "invalid_args" && code != "invalid_request" && code != "invalid_flags" {
			t.Fatalf("usage lost to identity: %s", raw)
		}
	}
}
