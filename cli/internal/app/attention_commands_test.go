package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAttentionCLIAndPreflight(t *testing.T) {
	tests := []struct {
		args         []string
		method, path string
	}{
		{[]string{"attention", "place", "card:launch", "--for", "actor:human", "--to", "watch", "--reason", "Launch progress", "--rank", "2"}, "POST", "/attention/placements"},
		{[]string{"attention", "agent", "get"}, "GET", "/attention/agent"},
		{[]string{"attention", "agent", "set", "actor:agent"}, "PUT", "/attention/agent"},
		{[]string{"attention", "agent", "set", "none"}, "PUT", "/attention/agent"},
		{[]string{"attention", "feedback", "list", "--since", "24h", "--for", "actor:human"}, "GET", "/attention/feedback"},
		{[]string{"attention", "items", "--surface", "all"}, "GET", "/attention/items"},
		{[]string{"attention", "changes", "--scope-ref", "builtin:initiatives"}, "GET", "/attention/changes"},
	}
	for _, tc := range tests {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tc.method || r.URL.Path != tc.path {
					t.Errorf("request=%s %s", r.Method, r.URL)
				}
				if tc.path == "/attention/placements" {
					var body map[string]any
					json.NewDecoder(r.Body).Decode(&body)
					if body["for"] != "actor:human" || body["rank"] != float64(2) {
						t.Error(body)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"items":[],"next_cursor":null}`))
			}))
			defer server.Close()
			argv := append([]string{"--json", "--base-url", server.URL}, tc.args...)
			assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), map[string]string{}, nil, argv))
		})
	}
	for _, args := range [][]string{{"place"}, {"place", "card:x", "--for", "actor:h", "--to", "unknown", "--reason", "x"}, {"place", "card:x", "--for", "actor:h", "--to", "watch", "--reason", strings.Repeat("é", 201)}, {"feedback", "list", "--since", "bad"}, {"items", "--unknown", "x"}, {"agent", "set"}, {"changes"}} {
		if _, err := parseAttentionCommand(args); err == nil {
			t.Fatalf("invalid args accepted: %v", args)
		}
	}
	for _, name := range []string{"attention place", "attention agent set"} {
		if commandSideEffectClass(name) != "remote_coordination_write" {
			t.Fatal(name)
		}
	}
	for _, name := range []string{"attention items", "attention feedback list", "attention agent get"} {
		if commandSideEffectClass(name) != "read_only" {
			t.Fatal(name)
		}
	}
}
