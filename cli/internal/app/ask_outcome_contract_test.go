package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// Every outcome the contract accepts must reach core unchanged. The CLI once
// hard-coded four of the six, so a human sending an ask back for context, or
// closing it as resolved, was rejected before it left the machine.
func TestInboxRespondAcceptsEveryContractOutcome(t *testing.T) {
	t.Parallel()

	for _, outcome := range []string{"answered", "approved", "rejected", "acknowledged", "needs_context", "resolved"} {
		outcome := outcome
		t.Run(outcome, func(t *testing.T) {
			t.Parallel()
			var sent string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/inbox/"+url.PathEscape("inbox:1")+"/respond" {
					http.NotFound(w, r)
					return
				}
				body, _ := io.ReadAll(r.Body)
				var payload map[string]any
				_ = json.Unmarshal(body, &payload)
				sent = anyStringValue(payload["outcome"])
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"event":{"id":"event_response"}}`))
			}))
			defer server.Close()

			home := t.TempDir()
			writeDerivedAgentFixture(t, home, "agent-a", `{"agent":"agent-a","actor_id":"actor-a1","access_token":"token-a","access_token_expires_at":"2099-01-01T00:00:00Z"}`)
			raw := runCLIForTest(t, home, map[string]string{}, failingStdinReader{}, []string{
				"--json", "--base-url", server.URL, "--as", "agent-a",
				"debug", "inbox", "respond",
				"--inbox-item-id", "inbox:1",
				"--response-text", "Need the failing log first.",
				"--outcome", outcome,
			})
			assertEnvelopeOK(t, raw)
			if sent != outcome {
				t.Fatalf("core received outcome %q, want %q", sent, outcome)
			}
		})
	}
}

func TestDraftInboxRespondAcceptsEveryContractOutcome(t *testing.T) {
	t.Parallel()

	for _, outcome := range []string{"answered", "approved", "rejected", "acknowledged", "needs_context", "resolved"} {
		errs := validateDraftInboxRespond(map[string]any{"inbox_item_id": "inbox:1", "response_text": "ok", "outcome": outcome})
		if len(errs) != 0 {
			t.Fatalf("outcome %q rejected: %v", outcome, errs)
		}
	}
	if errs := validateDraftInboxRespond(map[string]any{"inbox_item_id": "inbox:1", "response_text": "ok", "outcome": "maybe"}); len(errs) != 1 {
		t.Fatalf("unknown outcome accepted: %v", errs)
	}
}
