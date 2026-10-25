package app

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestAskTitleLintAndOverride(t *testing.T) {
	for _, tc := range []struct {
		name, title string
		flags       []string
		code        string
	}{
		{"soft", strings.Repeat("界", 101), nil, ""},
		{"override", strings.Repeat("界", 160), []string{"--force", "--reason", "Deliberate exception"}, ""},
		{"hard", strings.Repeat("界", 161), []string{"--force", "--reason", "Exception"}, "ask_title_too_long"},
		{"line", "Choose\nContext", nil, "ask_title_invalid"},
		{"missing reason", strings.Repeat("x", 101), []string{"--force"}, "invalid_request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"--json", "ask", tc.title, "--situation", "A launch decision is waiting.", "--impact", "The team cannot finish until we choose.", "--why", "This is the quickest safe approach.", "--recommend", "Accept", "--subject-ref", "card:launch", "--thread-id", "thr1", "--dry-run"}
			args = append(args, tc.flags...)
			raw := runCLIForTest(t, t.TempDir(), nil, nil, args)
			if tc.code == "" {
				assertEnvelopeOK(t, raw)
				return
			}
			payload := assertEnvelopeError(t, raw)
			if asMap(payload["error"])["code"] != tc.code {
				t.Fatalf("%s", raw)
			}
			if tc.code == "ask_authoring" && !strings.Contains(raw, "--body-file") {
				t.Fatalf("missing repair: %s", raw)
			}
		})
	}
}
func TestAskStructuredWithoutBody(t *testing.T) {
	payload := assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), nil, nil, []string{"--json", "ask", "Accept?", "--situation", "A launch decision is waiting.", "--impact", "The team cannot finish until we choose.", "--why", "This is the quickest safe approach.", "--recommend", "Accept", "--subject-ref", "card:launch", "--thread-id", "thr1", "--dry-run"}))
	if strings.Contains(fmt.Sprint(payload), "Missing decision body") {
		t.Fatal(payload)
	}

}
func TestAskTemplateFlagValueIsBody(t *testing.T) {
	assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), nil, nil, []string{"--json", "ask", "Accept?", "--body=--template", "--situation", "A launch decision is waiting.", "--impact", "The team cannot finish until we choose.", "--why", "This is the quickest safe approach.", "--recommend", "Accept", "--subject-ref", "card:launch", "--thread-id", "thr1", "--dry-run"}))
}
func TestAskDecisionTemplateOffline(t *testing.T) {
	raw := runCLIForTest(t, t.TempDir(), nil, nil, []string{"ask", "--template", "decision"})
	golden, err := os.ReadFile("testdata/ask-decision-template.md")
	if err != nil {
		t.Fatal(err)
	}
	if raw != string(golden) {
		t.Fatalf("template: %q", raw)
	}
	for _, section := range []string{"situation:", "impact:", "recommendation:", "alternatives:", "if_no_answer:"} {
		if !strings.Contains(raw, section) {
			t.Fatal(section)
		}
	}
	for _, args := range [][]string{{"ask", "--template", "unknown"}, {"ask", "--template", "decision", "--situation", "A launch decision is waiting.", "--impact", "The team cannot finish until we choose.", "--why", "This is the quickest safe approach.", "--recommend", "Accept"}} {
		payload := assertEnvelopeError(t, runCLIForTest(t, t.TempDir(), nil, nil, append([]string{"--json"}, args...)))
		if asMap(payload["error"])["code"] != "invalid_flags" {
			t.Fatalf("%#v", payload)
		}
	}
}

func TestAskNarrativeHelp(t *testing.T) {
	raw := runCLIForTest(t, t.TempDir(), nil, nil, []string{"ask", "--help"})
	for _, text := range []string{"short proposal", "busy boss with no context", "baseline, what changed", "Why it matters", "What I recommend", "tradeoff", "Alternatives", "If no answer", "250", "40", "Omit unused optional fields"} {
		if !strings.Contains(raw, text) {
			t.Fatalf("help missing %q: %s", text, raw)
		}
	}
}
