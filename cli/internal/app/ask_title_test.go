package app

import (
	"strings"
	"testing"
)

func TestAskTitleLintAndOverride(t *testing.T) {
	for _, tc := range []struct {
		name, title string
		flags       []string
		code        string
	}{
		{"soft", strings.Repeat("界", 101), nil, "ask_authoring"},
		{"override", strings.Repeat("界", 160), []string{"--force", "--reason", "Deliberate exception"}, ""},
		{"hard", strings.Repeat("界", 161), []string{"--force", "--reason", "Exception"}, "ask_title_too_long"},
		{"line", "Choose\nContext", nil, "ask_title_invalid"},
		{"missing reason", strings.Repeat("x", 101), []string{"--force"}, "invalid_request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"--json", "ask", tc.title, "--recommend", "Accept", "--subject-ref", "card:launch", "--thread-id", "thr1", "--dry-run"}
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
func TestAskMissingBodyWarns(t *testing.T) {
	payload := assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), nil, nil, []string{"--json", "ask", "Accept?", "--recommend", "Accept", "--subject-ref", "card:launch", "--thread-id", "thr1", "--dry-run"}))
	warnings := payload["warnings"].([]any)
	if len(warnings) == 0 || !strings.Contains(anyString(asMap(warnings[0])["message"]), "Missing decision body") {
		t.Fatalf("%#v", payload)
	}
}
func TestAskTemplateFlagValueIsBody(t *testing.T) {
	assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), nil, nil, []string{"--json", "ask", "Accept?", "--body=--template", "--recommend", "Accept", "--subject-ref", "card:launch", "--thread-id", "thr1", "--dry-run"}))
}
func TestAskDecisionTemplateOffline(t *testing.T) {
	raw := runCLIForTest(t, t.TempDir(), nil, nil, []string{"ask", "--template", "decision"})
	if raw != askDecisionTemplate {
		t.Fatalf("template: %q", raw)
	}
	for _, section := range []string{"Decision", "Context", "Options with consequences", "Recommendation", "Evidence"} {
		if !strings.Contains(raw, "## "+section) {
			t.Fatal(section)
		}
	}
	for _, args := range [][]string{{"ask", "--template", "unknown"}, {"ask", "--template", "decision", "--recommend", "Accept"}} {
		payload := assertEnvelopeError(t, runCLIForTest(t, t.TempDir(), nil, nil, append([]string{"--json"}, args...)))
		if asMap(payload["error"])["code"] != "invalid_flags" {
			t.Fatalf("%#v", payload)
		}
	}
}
