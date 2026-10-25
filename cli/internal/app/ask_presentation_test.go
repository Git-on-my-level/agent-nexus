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

func goodAskPresentation() askPresentation {
	return askPresentation{Situation: "After a user erases their data, five background jobs can still recreate their monthly usage totals.", Impact: "Data we promised to delete comes back, creating privacy and compliance risk.", Recommendation: &askRecommendation{Action: "Make the jobs check whether the user was deleted.", Why: "Reuse the existing guard; about 3 extra database reads per usage write."}, Alternatives: []askAlternative{{Action: "Build a different design.", Consequence: "More retry logic and slower to build."}}, IfNoAnswer: "The database cost change stays blocked until Friday."}
}
func TestAskPlainLanguageLint(t *testing.T) {
	good := goodAskPresentation()
	for _, tc := range []struct {
		name, title      string
		p                askPresentation
		options          []any
		errors, warnings []string
	}{
		{name: "good", title: "Approve the guard that keeps deleted users' data erased?", p: good, options: []any{good.Recommendation.Action, good.Alternatives[0].Action}},
		{name: "good risk title", title: "Deleted users' usage data can reappear.", p: good, options: []any{good.Recommendation.Action}},
		{name: "uppercase hash", title: "Approve DEADBEEF?", p: good, warnings: []string{`"DEADBEEF"`}},
		{name: "long hash", title: "Approve " + strings.Repeat("a1", 32) + "?", p: good, warnings: []string{`"` + strings.Repeat("a1", 32) + `"`}},
		{name: "everyday language", title: "Approve 24/7 support for iPhone?", p: good, options: []any{"OK if NO data was defaced; OK if NO one acceded."}},
		{name: "English numbers", title: "Approve 10000000 more credits?", p: good},
		{name: "missing", title: "Approve?", errors: []string{"Missing situation", "Missing impact", "Missing recommendation", "Missing recommendation.why"}},
		{name: "production jargon", title: "Approve PR23397 existing-fence reuse on every aggregate creator?", p: askPresentation{Situation: "Recovered c10041 on m1: ordinary/once/absolute/admin/migration writers can create monthly_usage_totals after erasure inventories the subtree.", Impact: "An external_write_fence is needed.", Recommendation: good.Recommendation}, options: []any{"Approve existing-fence reuse plus root check; count new authority reads", "Keep PR23397 blocked; lead chooses UPDATE-only design"}, warnings: []string{`"PR23397"`, `"c10041"`, `"monthly_usage_totals"`, `"external_write_fence"`}},
		{name: "identifiers", title: "Approve usageTotals at abc123def in core/internal/store.go or main.py?", p: good, warnings: []string{`"usageTotals"`, `"abc123def"`, `"core/internal/store.go"`, `"main.py"`}},
		{name: "repeated acronym", title: "Approve SLA this week?", p: good, options: []any{"Keep SLA"}, warnings: []string{`"SLA"`}},
		{name: "explained", title: "Approve service level agreement (SLA)?", p: good, options: []any{"Keep SLA"}},
		{name: "parenthetical", title: "Approve usageTotals (monthly usage totals)?", p: good},
		{name: "English", title: "Should we replace the old system?", p: good, options: []any{"Let the US team review privacy/compliance risk and choose and/or defer."}},
		{name: "long", title: strings.Repeat("Choose ", 15), p: askPresentation{Situation: strings.Repeat("word ", 61), Impact: good.Impact, Recommendation: good.Recommendation}, warnings: []string{"80 characters", "60 words"}},
		{name: "not decision", title: "Background data update", p: good, warnings: []string{"not a question or decision"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			warnings, errs := lintAskPresentation(tc.title, tc.p, tc.options)
			check := func(got, want []string) {
				t.Helper()
				if len(want) == 0 && len(got) > 0 {
					t.Fatalf("unexpected: %v", got)
				}
				for _, part := range want {
					if !strings.Contains(strings.Join(got, "\n"), part) {
						t.Fatalf("missing %q: %v", part, got)
					}
				}
			}
			check(warnings, tc.warnings)
			check(errs, tc.errors)
		})
	}
}
func TestAskPreviewOfflineAndMissingFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("preview made network request: %s", r.URL)
		w.WriteHeader(500)
	}))
	defer server.Close()
	for _, jsonMode := range []bool{false, true} {
		args := []string{"--base-url", server.URL, "ask", "Approve the guard?", "--preview"}
		if jsonMode {
			args = append([]string{"--json"}, args...)
		}
		raw := runCLIForTest(t, t.TempDir(), nil, nil, args)
		if !strings.Contains(raw, "Missing situation") || !strings.Contains(raw, "Missing impact") || !strings.Contains(raw, "Missing recommendation") {
			t.Fatalf("%s", raw)
		}
		if jsonMode {
			payload := assertEnvelopeOK(t, raw)
			if asMap(asMap(payload["result"])["lint"])["valid"] != false {
				t.Fatal(raw)
			}
		} else if !strings.HasPrefix(raw, "Approve the guard?") {
			t.Fatal(raw)
		}
	}
}
func TestAskFrontmatterPreviewRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ask.md")
	content := strings.Replace(askDecisionTemplate, "refs: []", "subject_ref: card:launch\nthread_id: thr1\nrefs: []", 1)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	payload := assertEnvelopeOK(t, runCLIForTest(t, dir, nil, nil, []string{"--json", "ask", "--from-file", path, "--preview"}))
	result := asMap(payload["result"])
	preview := asMap(result["preview"])
	if asMap(result["lint"])["valid"] != true || len(asSlice(asMap(result["lint"])["warnings"])) > 0 {
		t.Fatalf("%#v", result)
	}
	if asMap(preview["recommendation"])["why"] == nil || len(asSlice(preview["alternatives"])) != 1 {
		t.Fatalf("%#v", preview)
	}
	// Dry-run carries the same fields on the existing event payload.
	dry := assertEnvelopeOK(t, runCLIForTest(t, dir, nil, nil, []string{"--json", "ask", "--from-file", path, "--dry-run"}))
	encoded, _ := json.Marshal(dry)
	if !strings.Contains(string(encoded), `"recommendation"`) || !strings.Contains(string(encoded), `"if_no_answer"`) {
		t.Fatal(string(encoded))
	}
}
func TestAskRequiredFieldsBlockWriteAndForceRecordsReason(t *testing.T) {
	args := []string{"--json", "ask", "Approve?", "--recommend", "Approve the guard.", "--subject-ref", "card:launch", "--thread-id", "thr1", "--dry-run"}
	raw := runCLIForTest(t, t.TempDir(), nil, nil, args)
	assertEnvelopeError(t, raw)
	if !strings.Contains(raw, "Missing situation") {
		t.Fatal(raw)
	}
	raw = runCLIForTest(t, t.TempDir(), nil, nil, append(args, "--force", "--reason", "Legacy authoring exception"))
	assertEnvelopeOK(t, raw)
	if !strings.Contains(raw, "authoring_override_reason") {
		t.Fatal(raw)
	}
}

func TestAskPreviewIncompleteFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ask.md")
	if err := os.WriteFile(path, []byte("---\ntitle: Approve the guard?\n---\n"), 0600); err != nil {
		t.Fatal(err)
	}
	payload := assertEnvelopeOK(t, runCLIForTest(t, dir, nil, nil, []string{"--json", "ask", "--from-file", path, "--preview"}))
	lint := asMap(asMap(payload["result"])["lint"])
	if lint["valid"] != false || !strings.Contains(fmt.Sprint(lint["errors"]), "Missing recommendation") {
		t.Fatalf("%#v", lint)
	}
}
func TestAskPreviewIncludesInvalidOptions(t *testing.T) {
	raw := runCLIForTest(t, t.TempDir(), nil, nil, []string{"--json", "ask", "Approve?", "--situation", "The launch is waiting.", "--impact", "Customers are delayed.", "--why", "Ready to ship.", "--recommend", "Approve the guard.", "--alt", strings.Repeat("x", 241), "--preview"})
	result := asMap(assertEnvelopeOK(t, raw)["result"])
	if asMap(result["lint"])["valid"] != false {
		t.Fatal(raw)
	}
}
func TestAskBatchRequiresStructuredFields(t *testing.T) {
	path := askBatchFixture(t, `{"asks":[{"title":"Approve?","subject_ref":"card:launch","thread_id":"thr1","recommend":"Approve."},{"title":"Wait?","subject_ref":"card:launch","thread_id":"thr1","recommend":"Wait."}]}`)
	raw := runCLIForTest(t, t.TempDir(), nil, nil, []string{"--json", "ask", "batch", "--from-file", path, "--dry-run"})
	assertEnvelopeError(t, raw)
	if !strings.Contains(raw, "Missing situation") || !strings.Contains(raw, "Missing impact") {
		t.Fatal(raw)
	}
}

func TestAskPreviewFlagDetection(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{[]string{"Choose?", "--preview"}, true},
		{[]string{"Choose?", "-preview"}, true},
		{[]string{"Choose?", "-preview=1"}, true},
		{[]string{"Choose?", "-body", "-preview"}, false},
		{[]string{"Choose?", "--preview=TRUE"}, true},
		{[]string{"Choose?", "--preview=1"}, true},
		{[]string{"Choose?", "--preview=false"}, false},
		{[]string{"Choose?", "--preview", "--preview=false"}, false},
		{[]string{"Choose?", "--body", "--preview"}, false},
		{[]string{"Choose?", "--body=--preview"}, false},
		{[]string{"Choose?", "--force", "--preview"}, true},
	} {
		if got := hasAskPreview(tc.args); got != tc.want {
			t.Errorf("%v: %t", tc.args, got)
		}
	}
}

func TestAskMisplacedPreviewCannotPublish(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("misplaced preview sent %s %s", r.Method, r.URL)
		w.WriteHeader(500)
	}))
	defer server.Close()
	for _, flag := range []string{"--preview", "-preview", "-preview=true"} {
		raw := runCLIForTest(t, t.TempDir(), nil, nil, []string{"--json", "--base-url", server.URL, "ask", "--situation", "A launch is waiting.", "--impact", "The team is delayed.", "--recommend", "Approve the guard.", "--why", "It saves time.", "--subject-ref", "card:launch", "--thread-id", "thr1", "Approve?", flag})
		payload := assertEnvelopeError(t, raw)
		if asMap(payload["error"])["code"] != "invalid_flags" {
			t.Fatal(raw)
		}
	}
}

func TestAskPreviewMatchesDecide(t *testing.T) {
	raw, err := os.ReadFile("testdata/ask-decide-presentation.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		askPresentation
		Title      string
		Paragraphs []string
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	got := renderAskPreview(fixture.Title, "", fixture.askPresentation, nil, nil)
	want := fixture.Title + "\n\n" + strings.Join(fixture.Paragraphs, "\n") + "\n"
	if got != want {
		t.Fatalf("preview drifted from Decide: got %q want %q", got, want)
	}
}

func TestAskForcePreservesPartialRecommendation(t *testing.T) {
	p := askPresentation{Recommendation: &askRecommendation{Why: "Keep this explanation."}}
	payload := map[string]any{}
	p.apply(payload)
	if !strings.Contains(anyString(payload["details"]), p.Recommendation.Why) || payload["recommendation"] != nil {
		t.Fatalf("%#v", payload)
	}
	p.Recommendation = &askRecommendation{Action: strings.Repeat("x", 241)}
	if p.validateBounds() == nil {
		t.Fatal("partial action bypassed hard bound")
	}
	p.Recommendation = &askRecommendation{Why: "Keep this explanation."}
	p.Details = strings.Repeat("x", 32000)
	if p.validateBounds() == nil {
		t.Fatal("preserved partial copy bypassed details bound")
	}
}

func TestAskAlternativeConsequenceJargon(t *testing.T) {
	p := goodAskPresentation()
	p.Alternatives[0].Consequence = "Needs usageTotals."
	warnings, _ := lintAskPresentation("Approve?", p, nil)
	if !strings.Contains(strings.Join(warnings, "\n"), `"usageTotals" in alternative 1 consequence`) {
		t.Fatal(warnings)
	}
}

func TestAskPreviewRejectsIgnoredDeliveryFlags(t *testing.T) {
	for _, flags := range [][]string{{"--dry-run"}, {"--delivery", "none"}, {"--on-answer", "echo done"}, {"--webhook", "https://example.org/answer"}} {
		args := append([]string{"--json", "ask", "Approve?", "--preview"}, flags...)
		raw := runCLIForTest(t, t.TempDir(), nil, nil, args)
		payload := assertEnvelopeError(t, raw)
		if asMap(payload["error"])["code"] != "invalid_flags" {
			t.Fatal(raw)
		}
	}
}
