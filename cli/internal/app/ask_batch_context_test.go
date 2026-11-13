package app

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
)

const askBatchSharedContextJSON = `"context":{"title":"Log retention interview","situation":"Every log is kept for 400 days today, and storage grows by about nine percent each month as new teams start shipping their logs to us.","impact":"Auditors want a written retention policy before the next review, and the storage bill doubles within a year if nothing changes."}`

// fiveQuestions has only question, recommendation and alternatives per ask:
// no situation or impact of its own. Question 3 is multi-select.
func fiveQuestions() string {
	var asks []string
	for n := 1; n <= 5; n++ {
		extra := ""
		if n == 3 {
			extra = `,"multi_select":true,"note":"Pick every team that needs access."`
		}
		asks = append(asks, fmt.Sprintf(`{"question":"Should policy question %d pass?","subject_ref":"card:retention","thread_id":"thr1","recommendation":{"action":"Yes","why":"It matches the plan we agreed."},"alternatives":[{"action":"No","consequence":"Keeps the current cost."}]%s}`, n, extra))
	}
	return `"asks":[` + strings.Join(asks, ",") + `]`
}

func askBatchAgentHome(t *testing.T) string {
	home := t.TempDir()
	writeDerivedAgentFixture(t, home, "agent-a", `{"agent":"agent-a","username":"agent.alpha","actor_id":"actor_asker","access_token":"token-a","access_token_expires_at":"2099-01-01T00:00:00Z"}`)
	return home
}

// Acceptance (oss#538): a 5-question batch with one shared context passes
// lint without per-ask situation text.
func TestAskBatchSharedContextPassesLintWithoutPerAskSituation(t *testing.T) {
	t.Parallel()
	var captured atomic.Pointer[map[string]any]
	var requests atomic.Int64
	server := askBatchServer(t, &captured, &requests)
	defer server.Close()
	home := askBatchAgentHome(t)
	path := askBatchFixture(t, "{"+askBatchSharedContextJSON+","+fiveQuestions()+"}")

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "--base-url", server.URL, "--as", "agent-a", "ask", "batch", "--from-file", path})
	assertEnvelopeOK(t, raw)
	if strings.Contains(raw, "Missing situation") || strings.Contains(raw, "under 40 words") {
		t.Fatalf("per-ask situation lint was not waived: %s", raw)
	}
	body := *captured.Load()
	context, _ := body["context"].(map[string]any)
	if context["title"] != "Log retention interview" || !strings.HasPrefix(anyStringValue(context["situation"]), "Every log") {
		t.Fatalf("context not sent once at the top: %#v", body["context"])
	}
	asks, _ := body["asks"].([]any)
	if len(asks) != 5 {
		t.Fatalf("asks = %d", len(asks))
	}
	for i, rawAsk := range asks {
		payload := rawAsk.(map[string]any)["payload"].(map[string]any)
		if _, has := payload["situation"]; has {
			t.Fatalf("ask %d repeats the context: %#v", i, payload)
		}
		if payload["title"] != fmt.Sprintf("Should policy question %d pass?", i+1) {
			t.Fatalf("question alias lost: %#v", payload["title"])
		}
		if multi := payload["multi_select"] == true; multi != (i == 2) {
			t.Fatalf("ask %d multi_select = %v", i, payload["multi_select"])
		}
	}
	if asks[2].(map[string]any)["payload"].(map[string]any)["note"] != "Pick every team that needs access." {
		t.Fatal("note lost")
	}
}

func TestAskBatchWithoutContextStillRequiresSituation(t *testing.T) {
	t.Parallel()
	home := askBatchAgentHome(t)
	path := askBatchFixture(t, "{"+fiveQuestions()+"}")
	raw := runCLIForTestJSONError(t, home, map[string]string{}, []string{"--json", "--base-url", "http://127.0.0.1:1", "--as", "agent-a", "ask", "batch", "--from-file", path, "--dry-run"})
	if !strings.Contains(raw, "ask_authoring") || !strings.Contains(raw, "Missing situation") {
		t.Fatalf("expected per-ask situation lint: %s", raw)
	}
}

func TestAskBatchContextIsLintedOnce(t *testing.T) {
	t.Parallel()
	home := askBatchAgentHome(t)
	for name, tc := range map[string]struct{ context, want string }{
		"missing impact":    {`"context":{"situation":"Every log is kept for 400 days."}`, "Missing context.impact"},
		"missing situation": {`"context":{"impact":"Audits need a policy."}`, "context.situation is required"},
		"unknown field":     {`"context":{"situation":"S","why":"x"}`, "context.why is not a context field"},
	} {
		path := askBatchFixture(t, "{"+tc.context+","+fiveQuestions()+"}")
		raw := runCLIForTestJSONError(t, home, map[string]string{}, []string{"--json", "--base-url", "http://127.0.0.1:1", "--as", "agent-a", "ask", "batch", "--from-file", path, "--dry-run"})
		if !strings.Contains(raw, tc.want) {
			t.Fatalf("%s: want %q in %s", name, tc.want, raw)
		}
	}
	// A short context warns once, not once per question.
	path := askBatchFixture(t, `{"context":{"situation":"Logs are kept 400 days.","impact":"Audits need a policy."},`+fiveQuestions()+"}")
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "--base-url", "http://127.0.0.1:1", "--as", "agent-a", "ask", "batch", "--from-file", path, "--dry-run"})
	assertEnvelopeOK(t, raw)
	if n := strings.Count(raw, "under 40 words"); n != 1 {
		t.Fatalf("short context warned %d times: %s", n, raw)
	}
}

func TestAskBatchDryRunPreviewStatesContextOnce(t *testing.T) {
	t.Parallel()
	home := askBatchAgentHome(t)
	path := askBatchFixture(t, "{"+askBatchSharedContextJSON+","+fiveQuestions()+"}")
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "--base-url", "http://127.0.0.1:1", "--as", "agent-a", "ask", "batch", "--from-file", path, "--dry-run"})
	envelope := assertEnvelopeOK(t, raw)
	preview := anyStringValue(envelope["result"].(map[string]any)["preview"])
	if strings.Count(preview, "Every log is kept for 400 days") != 1 || !strings.Contains(preview, "3. Should policy question 3 pass? (pick any)") || !strings.Contains(preview, "a) Yes (recommended)") {
		t.Fatalf("preview:\n%s", preview)
	}
}
