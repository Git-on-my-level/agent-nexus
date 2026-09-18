package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHumanCommandRequiresRecommendedResponse(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json", "human", "ask", "Q?",
		"--subject-ref", "topic:t1",
		"--thread-id", "thr1",
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if anyStringValue(errObj["code"]) != "invalid_request" {
		t.Fatalf("%#v", payload)
	}
	msg := strings.ToLower(anyStringValue(errObj["message"]))
	if !strings.Contains(msg, "recommended") {
		t.Fatalf("expected recommended-response validation, got %q", anyStringValue(errObj["message"]))
	}
}

func TestHumanCommandRejectsTooManyProposals(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	args := []string{
		"--json", "human", "ask", "Q",
		"--subject-ref", "topic:t1",
		"--thread-id", "thr1",
		"--recommended-response", "A",
	}
	for i := 0; i < 6; i++ {
		args = append(args, "--proposal", string(rune('B'+i)))
	}
	raw := runCLIForTest(t, home, map[string]string{}, nil, args)
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if anyStringValue(errObj["code"]) != "invalid_request" {
		t.Fatalf("%#v", payload)
	}
}

func TestHumanCommandFromFileRejectsMixedFlags(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	path := filepath.Join(t.TempDir(), "h.md")
	content := "---\ntitle: T\nsubject_ref: topic:x\nthread_id: thr1\nrecommended_response: R\n---\nBody\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json", "human", "ask", "--from-file", path, "--subject-ref", "topic:y",
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if anyStringValue(errObj["code"]) != "invalid_request" {
		t.Fatalf("%#v", payload)
	}
}

func TestHumanCommandFromFileRejectsPositionalTitle(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	path := filepath.Join(t.TempDir(), "h.md")
	content := "---\ntitle: T\nsubject_ref: topic:x\nthread_id: thr1\nrecommended_response: R\n---\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json", "human", "ask", "Extra title", "--from-file", path,
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if anyStringValue(errObj["code"]) != "invalid_request" {
		t.Fatalf("%#v", payload)
	}
}

func TestHumanCommandFromFileKindMismatch(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	path := filepath.Join(t.TempDir(), "h.md")
	content := "---\ntitle: T\nsubject_ref: topic:x\nthread_id: thr1\nrecommended_response: R\nkind: review\n---\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json", "human", "ask", "--from-file", path,
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if anyStringValue(errObj["code"]) != "invalid_request" {
		t.Fatalf("%#v", payload)
	}
}

func TestHumanCommandFromFileCreatesEvent(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/events" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"event":{"id":"evt1","type":"human_attention_requested","thread_id":"thr1"}}`))
	}))
	defer server.Close()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-a", `{"agent":"agent-a","username":"agent.alpha","actor_id":"actor_asker","access_token":"token-a","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

	path := filepath.Join(t.TempDir(), "req.md")
	content := "---\ntitle: Confirm launch\ncoverage_hint: thin\nsubject_ref: topic:launch\nthread_id: thr1\nrecommended_response: Ship May 15\nproposals:\n  - Wait for legal\n---\n\nMore detail in markdown.\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json", "--base-url", server.URL, "--agent", "agent-a",
		"human", "ask", "--from-file", path,
	})
	assertEnvelopeOK(t, raw)

	event, _ := captured["event"].(map[string]any)
	pl, _ := event["payload"].(map[string]any)
	if strings.TrimSpace(anyStringValue(pl["body"])) != "More detail in markdown." {
		t.Fatalf("body: %#v", pl["body"])
	}
	rp, _ := pl["response_proposals"].([]any)
	if len(rp) != 2 {
		t.Fatalf("response_proposals: %#v", rp)
	}
}

func TestHumanCommandFromFileRequiresFrontmatterFields(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	path := filepath.Join(t.TempDir(), "bad.md")
	content := "---\ntitle: Only title\n---\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json", "human", "review", "--from-file", path,
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if anyStringValue(errObj["code"]) != "invalid_request" {
		t.Fatalf("%#v", payload)
	}
}

// A human-attention request must be expressible with the noun the operator
// actually sees. Before this, only `topic:` subjects resolved their backing
// thread, so `anx human review --subject-ref document:...` — the command's own
// documented example — failed with "thread id is required", forcing the agent
// to go and find a thread id the operator has never heard of.
func TestHumanCommandResolvesThreadFromThreadBackedSubjects(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		subjectRef   string
		getPath      string
		getBody      string
		wantThreadID string
	}{
		{
			name:         "card subject",
			subjectRef:   "card:implement-login",
			getPath:      "/cards/card:implement-login",
			getBody:      `{"card":{"id":"implement-login","thread_id":"thr-card"}}`,
			wantThreadID: "thr-card",
		},
		{
			name:         "document subject",
			subjectRef:   "document:launch_notes",
			getPath:      "/docs/document:launch_notes",
			getBody:      `{"document":{"id":"launch_notes","thread_id":"thr-doc"}}`,
			wantThreadID: "thr-doc",
		},
		{
			name:         "topic subject",
			subjectRef:   "topic:launch",
			getPath:      "/topics/topic:launch",
			getBody:      `{"topic":{"id":"launch","thread_id":"thr-topic"}}`,
			wantThreadID: "thr-topic",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var captured map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet && r.URL.Path == tc.getPath {
					_, _ = w.Write([]byte(tc.getBody))
					return
				}
				if r.Method == http.MethodPost && r.URL.Path == "/events" {
					if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
						t.Fatalf("decode: %v", err)
					}
					_, _ = w.Write([]byte(`{"event":{"id":"evt1","type":"human_attention_requested"}}`))
					return
				}
				http.NotFound(w, r)
			}))
			defer server.Close()

			home := t.TempDir()
			writeAgentProfile(t, home, "agent-a", `{"agent":"agent-a","username":"agent.alpha","actor_id":"actor_asker","access_token":"token-a","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

			raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
				"--json", "--base-url", server.URL, "--agent", "agent-a",
				"human", "ask", "Which launch date?",
				"--subject-ref", tc.subjectRef,
				"--recommended-response", "Use May 15.",
			})

			// No --thread-id was passed: the CLI must have grounded it itself.
			if captured == nil {
				payload := assertEnvelopeError(t, raw)
				t.Fatalf("expected the request to be posted, got error: %#v", payload)
			}
			event, _ := captured["event"].(map[string]any)
			if got := anyStringValue(event["thread_id"]); got != tc.wantThreadID {
				t.Fatalf("thread_id = %q, want %q: %#v", got, tc.wantThreadID, captured)
			}
			// The operator-visible subject must survive as the subject_ref.
			payload, _ := event["payload"].(map[string]any)
			if got := anyStringValue(payload["subject_ref"]); got != tc.subjectRef {
				t.Fatalf("subject_ref = %q, want %q", got, tc.subjectRef)
			}
		})
	}
}
