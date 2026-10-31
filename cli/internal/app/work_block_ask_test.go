package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkBlockAskValidationBeforeMutation(t *testing.T) {
	for _, tc := range []struct {
		name, why, code string
		flags           []string
	}{
		{"long title", strings.Repeat("界", 161), "ask_title_too_long", nil},
		{"hard limit", strings.Repeat("界", 161), "ask_title_too_long", []string{"--force", "--reason", "Exception"}},
		{"multiline", "Decision\nContext", "ask_title_invalid", nil},
		{"missing override reason", strings.Repeat("x", 101), "invalid_request", []string{"--force"}},
		{"invalid body", "Need a decision", "ask_authoring", []string{"--body", "Please see the report"}},
		{"missing body file", "Need a decision", "file_read_failed", []string{"--body-file", "missing-ask-body.md"}},
		{"invalid proposal", "Need a decision", "invalid_request", []string{"--alt", strings.Repeat("x", 241)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writes := 0
			phase := "in_progress"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method != http.MethodGet {
					writes++
					phase = "blocked"
				}
				if r.URL.Path == "/cards/card:task" {
					fmt.Fprintf(w, `{"card":{"ref":"card:task","column_key":%q,"thread_id":"thread-1"}}`, phase)
					return
				}
				http.NotFound(w, r)
			}))
			defer server.Close()
			a, out := dailyTestApp(t, server.URL)
			args := []string{"--json", "--as", "worker", "work", "block", tc.why, "card:task", "--ask", "--situation", "A launch decision is waiting.", "--impact", "The team cannot finish until we choose.", "--why", "This is the quickest safe approach.", "--recommend", "Accept"}
			exit := a.Run(append(args, tc.flags...))
			wantExit := 2
			if tc.code == "file_read_failed" {
				wantExit = 1
			}
			if exit != wantExit {
				t.Fatalf("exit=%d output=%s", exit, out.String())
			}
			doc := assertEnvelopeError(t, out.String())
			if anyString(asMap(doc["error"])["code"]) != tc.code {
				t.Fatalf("%s", out.String())
			}
			if writes != 0 || phase != "in_progress" {
				t.Fatalf("writes=%d phase=%s", writes, phase)
			}
		})
	}
}

func TestWorkBlockAskBodyAndOverride(t *testing.T) {
	for _, mode := range []string{"inline", "file", "stdin", "override"} {
		t.Run(mode, func(t *testing.T) {
			why := "Need a decision"
			body := "## Decision\nAccept?\n## Context\nExecution needs approval.\n## Options\nAccept or wait.\n## Recommendation\nAccept.\n## Evidence\nCurrent execution."
			flags := []string{"--body", body}
			switch mode {
			case "file":
				path := filepath.Join(t.TempDir(), "ask.md")
				if err := os.WriteFile(path, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
				flags = []string{"--body-file", path}
			case "stdin":
				flags = []string{"--body-file", "-"}
			case "override":
				why = strings.Repeat("界", 160)
				flags = append(flags, "--force", "--reason", "Deliberate exception")
			}
			var ask map[string]any
			var blocker, presenceNote string
			phase := "in_progress"
			moves, messages := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/cards/card:task":
					fmt.Fprintf(w, `{"card":{"id":"card:task","ref":"card:task","board_id":"board-1","column_key":%q,"thread_id":"thread-1","updated_at":"2026-10-12T00:00:00Z"}}`, phase)
				case r.Method == http.MethodGet && r.URL.Path == "/boards/board-1":
					fmt.Fprint(w, `{"board":{"id":"board-1","updated_at":"2026-10-12T00:00:00Z"}}`)
				case r.Method == http.MethodPost && r.URL.Path == "/cards/card:task/move":
					var input map[string]any
					if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
						t.Error(err)
					}
					phase = anyString(input["column_key"])
					moves++
					fmt.Fprint(w, `{"card":{"ref":"card:task","column_key":"blocked"}}`)
				case r.Method == http.MethodPost && r.URL.Path == "/events":
					var input map[string]any
					if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
						t.Error(err)
					}
					event := asMap(input["event"])
					payload := asMap(event["payload"])
					if anyString(event["type"]) == humanAttentionRequestedEventType {
						ask = payload
					} else {
						blocker = anyString(payload["text"])
						messages++
					}
					fmt.Fprint(w, `{"event":{"id":"ask-1","thread_id":"thread-1"}}`)
				case r.Method == http.MethodPatch && r.URL.Path == "/agents/me/presence":
					var input map[string]any
					if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
						t.Error(err)
					}
					presenceNote = anyString(input["note"])
					fmt.Fprint(w, `{}`)
				case r.Method == http.MethodGet && r.URL.Path == "/events":
					fmt.Fprint(w, `{"events":[]}`)
				case r.Method == http.MethodGet && r.URL.Path == "/brief":
					// Best-effort ask_no_deadline count; a failed read is tolerated.
					http.NotFound(w, r)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			a, out := dailyTestApp(t, server.URL)
			if mode == "stdin" {
				a.Stdin = strings.NewReader(body)
			}
			args := []string{"--json", "--as", "worker", "work", "block", why, "card:task", "--ask", "--situation", "A launch decision is waiting.", "--impact", "The team cannot finish until we choose.", "--why", "This is the quickest safe approach.", "--recommend", "Accept"}
			if exit := a.Run(append(args, flags...)); exit != 0 {
				t.Fatalf("exit=%d output=%s", exit, out.String())
			}
			result := asMap(assertEnvelopeOK(t, out.String())["result"])
			if anyString(result["ask_id"]) != "event:ask-1" || anyString(result["phase"]) != "blocked" {
				t.Fatalf("%s", out.String())
			}
			if moves != 1 || messages != 1 || phase != "blocked" || blocker != why || presenceNote != why {
				t.Fatalf("moves=%d messages=%d phase=%s blocker=%q presence=%q", moves, messages, phase, blocker, presenceNote)
			}
			if anyString(ask["title"]) != why || anyString(ask["body"]) != body {
				t.Fatalf("ask=%#v", ask)
			}
			override := anyString(ask["authoring_override_reason"])
			if mode == "override" && override != "Deliberate exception" {
				t.Fatalf("override=%q", override)
			}
			if mode != "override" && override != "" {
				t.Fatalf("unexpected override=%q", override)
			}
		})
	}
}
