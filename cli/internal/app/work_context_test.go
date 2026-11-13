package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkBriefingAndCheckpointRequestsUseCentralAPI(t *testing.T) {
	for _, tc := range []struct {
		name, commandID, method, path, input, request, response string
	}{
		{
			name:      "briefing",
			commandID: "work.briefing",
			method:    http.MethodGet,
			path:      "/work/card:launch/briefing",
			response:  `{"schema_version":1,"work":{"ref":"card:launch"},"checkpoint":null,"observation":null,"refresh":{},"coverage":{},"omitted":[]}`,
		},
		{
			name:      "checkpoint",
			commandID: "work.checkpoint",
			method:    http.MethodPut,
			path:      "/work/card:launch/checkpoint",
			input:     `{"if_version":0,"checkpoint":{"summary":"Progress","next_action":"Inspect the latest run","blockers":[]}}`,
			request:   `{"checkpoint":{"blockers":[],"next_action":"Inspect the latest run","summary":"Progress"},"if_version":0}`,
			response:  `{"checkpoint":{"version":1,"reporter":"agent:worker","updated_at":"2026-10-01T00:00:00Z","digest":"sha256:abc","content":{"summary":"Progress","next_action":"Inspect the latest run","blockers":[]}},"version":1,"unchanged":false}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/meta/handshake" {
					http.NotFound(w, r)
					return
				}
				calls++
				if r.Method != tc.method || r.URL.RequestURI() != tc.path {
					t.Errorf("request=%s %s want %s %s", r.Method, r.URL.RequestURI(), tc.method, tc.path)
				}
				body, _ := io.ReadAll(r.Body)
				if string(body) != tc.request {
					t.Errorf("request body=%s want %s", body, tc.request)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.response)
			}))
			defer server.Close()

			args := []string{"--json", "--base-url", server.URL, "work", tc.name, "card:launch"}
			var input io.Reader
			if tc.input != "" {
				args = append(args, "--from-file", "-")
				input = strings.NewReader(tc.input)
			}
			payload := assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), nil, input, args))
			if got := machineEnvelopeCommandID(payload); got != tc.commandID {
				t.Fatalf("command identity=%q want %q", got, tc.commandID)
			}
			if calls != 1 {
				t.Fatalf("API calls=%d want 1", calls)
			}
		})
	}
}

func TestWorkContextBriefRoutesDirectlyToPortableBriefing(t *testing.T) {
	calls := []string{}
	briefing := `{"schema_version":1,"work":{"ref":"card:launch","version":8},"checkpoint":{"version":3,"reporter":"agent:worker","updated_at":"2026-10-01T00:00:00Z","digest":"sha256:abc","content":{"summary":"Progress","next_action":"Inspect the latest run"}},"observation":{"status":"reported"},"refresh":{"state":"failed"},"coverage":{"work":"complete"},"omitted":[]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/meta/handshake" {
			http.NotFound(w, r)
			return
		}
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		if r.Method != http.MethodGet || r.URL.RequestURI() != "/work/card:launch/briefing" {
			t.Errorf("request=%s %s", r.Method, r.URL.RequestURI())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, briefing)
	}))
	defer server.Close()

	payload := assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), nil, nil, []string{"--json", "--base-url", server.URL, "context", "card:launch", "--brief"}))
	if got := machineEnvelopeCommandID(payload); got != "context" {
		t.Fatalf("command identity=%q want context", got)
	}
	got, _ := json.Marshal(payload["result"])
	var want any
	_ = json.Unmarshal([]byte(briefing), &want)
	wantJSON, _ := json.Marshal(want)
	if string(got) != string(wantJSON) {
		t.Fatalf("briefing response changed: got %s want %s", got, wantJSON)
	}
	if len(calls) != 1 || calls[0] != "GET /work/card:launch/briefing" {
		t.Fatalf("calls=%v", calls)
	}
}

func TestWorkCheckpointRejectsInvalidVersionAndRequiredContentLocally(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	for _, body := range []string{
		`{"checkpoint":{"summary":"Progress","next_action":"Next"}}`,
		`{"if_version":-1,"checkpoint":{"summary":"Progress","next_action":"Next"}}`,
		`{"if_version":1.5,"checkpoint":{"summary":"Progress","next_action":"Next"}}`,
		`{"if_version":0,"checkpoint":{"summary":"Progress"}}`,
	} {
		payload := assertEnvelopeError(t, runCLIForTest(t, t.TempDir(), nil, strings.NewReader(body), []string{"--json", "--base-url", server.URL, "work", "checkpoint", "card:launch", "--from-file", "-"}))
		if got := asMap(payload["error"])["code"]; got != "invalid_request" {
			t.Errorf("body=%s code=%v want invalid_request", body, got)
		}
	}
	if calls != 0 {
		t.Fatalf("invalid checkpoint reached server %d times", calls)
	}
}

func TestWorkBriefingAndCheckpointTextPreserveFullResponse(t *testing.T) {
	for _, tc := range []struct {
		name string
		body map[string]any
		keys []string
	}{
		{"work briefing", map[string]any{"schema_version": 1, "work": map[string]any{"version": 8}, "checkpoint": map[string]any{"version": 3, "reporter": "agent:worker", "content": map[string]any{"summary": "Progress"}}, "observation": map[string]any{"status": "reported"}, "refresh": map[string]any{"state": "failed"}, "coverage": map[string]any{"work": "complete"}, "omitted": []any{"history"}}, []string{"checkpoint", "reporter", "observation", "refresh", "coverage", "omitted"}},
		{"work checkpoint", map[string]any{"checkpoint": map[string]any{"version": 4, "reporter": "agent:worker", "digest": "sha256:abc", "content": map[string]any{"summary": "Progress"}}, "version": 4, "unchanged": false}, []string{"checkpoint", "reporter", "digest", "content", "version", "unchanged"}},
	} {
		text := formatWorkCommandText(tc.name, tc.body)
		for _, key := range tc.keys {
			if !strings.Contains(text, `"`+key+`"`) {
				t.Errorf("%s text dropped %s: %s", tc.name, key, text)
			}
		}
	}
}

func TestWorkBriefingAndCheckpointSideEffectClasses(t *testing.T) {
	if got := commandSideEffectClass("work briefing"); got != "read_only" {
		t.Errorf("work briefing side effect=%s want read_only", got)
	}
	if got := commandSideEffectClass("work checkpoint"); got != "remote_coordination_write" {
		t.Errorf("work checkpoint side effect=%s want remote_coordination_write", got)
	}
}

func TestWorkContextCommandsPreflightAndHelpOffline(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code string
	}{
		{[]string{"work", "briefing"}, "invalid_request"},
		{[]string{"work", "briefing", "card:launch", "--nonsense"}, "invalid_flags"},
		{[]string{"work", "checkpoint", "card:launch"}, "invalid_request"},
		{[]string{"context", "card:launch", "--nonsense"}, "invalid_flags"},
		{[]string{"context", "card:launch", "--brief", "--limit", "10"}, "invalid_request"},
	} {
		payload := assertEnvelopeError(t, runCLIForTest(t, t.TempDir(), nil, nil, append([]string{"--json"}, tc.args...)))
		if got := asMap(payload["error"])["code"]; got != tc.code {
			t.Errorf("args=%v code=%v want %s", tc.args, got, tc.code)
		}
	}
	for _, args := range [][]string{{"work", "briefing", "--help"}, {"work", "checkpoint", "--help"}} {
		payload := assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), nil, nil, append([]string{"--json"}, args...)))
		raw, _ := json.Marshal(payload["result"])
		if !strings.Contains(string(raw), "anx ") {
			t.Errorf("help for %v is not discoverable: %s", args, raw)
		}
	}
}
