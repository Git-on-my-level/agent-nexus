package app

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// An ask was rejected for a recommendation over 240 characters, and only after
// that was fixed for a PR named without its URL. Preview and submit now report
// every violation in one response, each with its field path and limit.
func TestAskReportsEveryViolationAtOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ask.md")
	content := strings.Replace(askDecisionTemplate, "    Measure one daily report run before buying more capacity.", "    "+strings.Repeat("Measure it. ", 25), 1)
	content = strings.Replace(content, "evidence: []", "evidence:\n  - \"PR=https://example.test/pr/1\"", 1)
	content = strings.Replace(content, "if_no_answer: >-\n", "details: \"Shipped in PR #4242 last week.\"\nif_no_answer: >-\n", 1)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	payload := assertEnvelopeOK(t, runCLIForTest(t, dir, nil, nil, []string{"--json", "ask", "--from-file", path, "--preview"}))
	lint := asMap(asMap(payload["result"])["lint"])
	errs := strings.Join(stringList(lint["errors"]), "\n")
	for _, want := range []string{
		"recommendation.action is 300 characters; the limit is 240",
		"evidence[0] must be an object {label: <text>, url: <https://...>}; got the string \"PR=https://example.test/pr/1\"",
		"PR #4242 has no matching evidence URL",
	} {
		if !strings.Contains(errs, want) {
			t.Fatalf("preview missing %q in:\n%s", want, errs)
		}
	}
	if lint["valid"] != false {
		t.Fatal("preview must be invalid")
	}
	raw := runCLIForTest(t, dir, nil, nil, []string{"--json", "ask", "--from-file", path})
	for _, want := range []string{"recommendation.action is 300 characters", "evidence[0] must be an object", "PR #4242 has no matching evidence URL", `"forceable": false`} {
		if !strings.Contains(raw, want) {
			t.Fatalf("submit missing %q in:\n%s", want, raw)
		}
	}
}

// A YAML evidence entry that is not {label, url} is a payload shape error.
// Preview reports it, and --force must not submit the unfilled link. An
// over-limit field must not be what makes the refusal non-forceable.
func TestAskEvidenceShapeCannotBeForced(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ask.md")
	content := strings.Replace(askDecisionTemplate, "evidence: []", "evidence:\n  - \"PR=https://example.test/pr/1\"", 1)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	raw := runCLIForTest(t, dir, nil, nil, []string{"--json", "ask", "--from-file", path, "--force", "--reason", "the link is obvious"})
	assertEnvelopeError(t, raw)
	for _, want := range []string{`evidence[0] must be an object`, `"forceable": false`} {
		if !strings.Contains(raw, want) {
			t.Fatalf("forced submit missing %q in:\n%s", want, raw)
		}
	}
	if strings.Contains(raw, `"forceable": true`) {
		t.Fatalf("a bad evidence shape must not be forceable:\n%s", raw)
	}
}

// A missing title and a kind mismatch used to stop validation at the first
// one; they are reported with every other violation.
func TestAskFileStructureViolationsAreReportedTogether(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ask.md")
	content := strings.Replace(askDecisionTemplate, "    Measure one daily report run before buying more capacity.", "    "+strings.Repeat("Measure it. ", 25), 1)
	content = regexp.MustCompile(`(?m)^title:.*\n`).ReplaceAllString(content, "kind: task\n")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--preview"}, {}} {
		raw := runCLIForTest(t, dir, nil, nil, append([]string{"--json", "ask", "--from-file", path}, args...))
		for _, want := range []string{"title is required", `kind \"task\" does not match anx ask`, "recommendation.action is 300 characters"} {
			if !strings.Contains(raw, want) {
				t.Fatalf("%v missing %q in:\n%s", args, want, raw)
			}
		}
	}
}
