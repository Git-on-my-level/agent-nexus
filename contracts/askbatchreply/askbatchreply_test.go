package askbatchreply

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

// fiveQuestionItem is an inbox item as GET /inbox returns a filed batch with
// one shared context: five questions, question 3 multi-select, question 4
// free text only.
func fiveQuestionItem() map[string]any {
	section := func(position int, ref, title string, proposals []any, extra map[string]any) map[string]any {
		s := map[string]any{"request_event_ref": ref, "title": title, "response_proposals": proposals, "position": float64(position), "subject_ref": "card:c1"}
		for k, v := range extra {
			s[k] = v
		}
		return s
	}
	return map[string]any{
		"id":    "inbox:ask:t1:askbatch_1:ev1",
		"title": "Retention policy interview",
		"batch": map[string]any{
			"id": "askbatch_1",
			"context": map[string]any{
				"situation": "We keep every log for 400 days today.",
				"impact":    "Storage grows 9% a month and audits want a stated policy.",
				"details":   "Full numbers in the storage review.",
				"evidence":  []any{map[string]any{"label": "Storage review", "url": "https://example.com/review"}},
			},
			// Deliberately out of order: position wins over list order.
			"asks": []any{
				section(2, "event:ev2", "Keep audit logs for 7 years?", []any{"Yes", "No"}, nil),
				section(1, "event:ev1", "Delete debug logs after 30 days?", []any{"Yes", "No", "After 90 days"},
					map[string]any{"recommendation": map[string]any{"action": "Yes", "why": "Nobody reads them later."}}),
				section(3, "event:ev3", "Which teams may read raw logs?", []any{"Platform", "Security", "Support"}, map[string]any{"multi_select": true, "note": "Pick every team that needs access."}),
				section(4, "event:ev4", "Anything else the policy must cover?", []any{}, nil),
				section(5, "event:ev5", "Publish the policy now?", []any{"Now", "Next quarter"}, nil),
			},
		},
	}
}

func mustBatch(t *testing.T) Batch {
	t.Helper()
	batch, err := FromInboxItem(fiveQuestionItem())
	if err != nil {
		t.Fatal(err)
	}
	return batch
}

func TestFromInboxItemOrdersByPosition(t *testing.T) {
	b := mustBatch(t)
	if b.ID != "askbatch_1" || b.InboxItemID != "inbox:ask:t1:askbatch_1:ev1" || b.Context == nil {
		t.Fatalf("batch = %+v", b)
	}
	var refs []string
	for _, q := range b.Questions {
		refs = append(refs, q.AskRef)
	}
	if !reflect.DeepEqual(refs, []string{"event:ev1", "event:ev2", "event:ev3", "event:ev4", "event:ev5"}) {
		t.Fatalf("order = %v", refs)
	}
	q1, _ := b.Question(1)
	if q1.Recommended != 0 || q1.Options[0].Why != "Nobody reads them later." {
		t.Fatalf("q1 = %+v", q1)
	}
	if q3, _ := b.Question(3); !q3.MultiSelect || q3.Note == "" {
		t.Fatalf("q3 = %+v", q3)
	}
}

// Acceptance: the reply `1a 2b 3ac 4: free text 5a` records 5 answers
// correctly, 3 being multi-select.
func TestParseAcceptanceReply(t *testing.T) {
	b := mustBatch(t)
	r := Parse(b, "1a 2b 3ac 4: free text 5a")
	if !r.Complete() {
		t.Fatalf("issues=%v missing=%v", r.Issues, r.Missing)
	}
	want := []Answer{
		{Number: 1, Choices: []int{0}},
		{Number: 2, Choices: []int{1}},
		{Number: 3, Choices: []int{0, 2}},
		{Number: 4, Text: "free text"},
		{Number: 5, Choices: []int{0}, AfterTextOf: 4},
	}
	if !reflect.DeepEqual(unfragmented(r.Answers), want) {
		t.Fatalf("answers = %+v", r.Answers)
	}
	body := RespondBody(b, r, "k1")
	answers := body["answers"].([]any)
	got := []string{}
	for _, raw := range answers {
		a := raw.(map[string]any)
		got = append(got, a["ask_ref"].(string)+"="+a["response_text"].(string))
	}
	wantText := []string{"event:ev1=Yes", "event:ev2=No", "event:ev3=Platform; Support", "event:ev4=free text", "event:ev5=Now"}
	if !reflect.DeepEqual(got, wantText) || body["partial"] != nil || body["idempotency_key"] != "k1" {
		t.Fatalf("body = %v", body)
	}
}

func TestParseMalformedReplyNamesUnparsedParts(t *testing.T) {
	b := mustBatch(t)
	r := Parse(b, "1a 2ab 3z 7b hello 4:")
	if r.OK() {
		t.Fatal("malformed reply parsed as OK")
	}
	var fragments []string
	for _, issue := range r.Issues {
		fragments = append(fragments, issue.Fragment)
	}
	if !reflect.DeepEqual(fragments, []string{"2ab", "3z", "7b", "hello", "4:"}) {
		t.Fatalf("fragments = %v (%v)", fragments, r.Issues)
	}
	problems := Problems(r)
	for _, want := range []string{`"2ab": question 2 takes one option`, `"3z": question 3 has options a-c`, `"7b": there is no question 7`, `"hello": not an answer`, `"4:": write your answer to question 4`} {
		if !strings.Contains(problems, want) {
			t.Fatalf("problems missing %q:\n%s", want, problems)
		}
	}
	if !reflect.DeepEqual(r.Missing, []int{2, 3, 4, 5}) {
		t.Fatalf("missing = %v", r.Missing)
	}
}

func TestParseVariants(t *testing.T) {
	b := mustBatch(t)
	for _, tc := range []struct {
		name, reply string
		want        []Answer
		missing     []int
	}{
		{"commas and case", "1A, 2b;3CA", []Answer{{Number: 1, Choices: []int{0}}, {Number: 2, Choices: []int{1}}, {Number: 3, Choices: []int{0, 2}}}, []int{4, 5}},
		{"spaced and dotted", "1 a 2. b 3) c", []Answer{{Number: 1, Choices: []int{0}}, {Number: 2, Choices: []int{1}}, {Number: 3, Choices: []int{2}}}, []int{4, 5}},
		{"lines", "1a\n4: keep the\nretention table short\n5b", []Answer{{Number: 1, Choices: []int{0}}, {Number: 4, Text: "keep the\nretention table short"}, {Number: 5, Choices: []int{1}}}, []int{2, 3}},
		{"text keeps numbers that are not items", "4: we need 2 more weeks and 3pm reviews", []Answer{{Number: 4, Text: "we need 2 more weeks and 3pm reviews"}}, []int{1, 2, 3, 5}},
		{"text for an option question", "2: only for finance", []Answer{{Number: 2, Text: "only for finance"}}, []int{1, 3, 4, 5}},
		{"duplicate letters", "3aa", []Answer{{Number: 3, Choices: []int{0}}}, []int{1, 2, 4, 5}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Parse(b, tc.reply)
			if len(r.Issues) != 0 || !reflect.DeepEqual(unfragmented(r.Answers), tc.want) || !reflect.DeepEqual(r.Missing, tc.missing) {
				t.Fatalf("answers=%+v issues=%v missing=%v", r.Answers, r.Issues, r.Missing)
			}
		})
	}
}

func TestParseRejectsRepeatsAndLettersForTextQuestion(t *testing.T) {
	b := mustBatch(t)
	r := Parse(b, "1a 1b 4a")
	if len(r.Issues) != 2 || r.Issues[0].Fragment != "1b" || !strings.Contains(r.Issues[0].Reason, "answered twice") ||
		r.Issues[1].Fragment != "4a" || !strings.Contains(r.Issues[1].Reason, "4: your own words") {
		t.Fatalf("issues = %v", r.Issues)
	}
	if empty := Parse(b, "   "); empty.OK() || len(empty.Issues) != 1 {
		t.Fatalf("empty = %+v", empty)
	}
}

func TestPartialReplyBody(t *testing.T) {
	b := mustBatch(t)
	r := Parse(b, "1a 5b")
	if !r.OK() || r.Complete() {
		t.Fatalf("r = %+v", r)
	}
	body := RespondBody(b, r, "")
	if body["partial"] != true || len(body["answers"].([]any)) != 2 {
		t.Fatalf("body = %v", body)
	}
	summary := Summary(b, r)
	if !strings.Contains(summary, "1. Delete debug logs after 30 days?\n   a) Yes") || !strings.Contains(summary, "Still open: 2, 3, 4") {
		t.Fatalf("summary:\n%s", summary)
	}
}

func TestParseSetsAsideTrailingProse(t *testing.T) {
	b := mustBatch(t)
	// A sign-off after a blank line is not an answer: it is set aside and
	// reported, never silently dropped and never a reason to refuse the
	// answers above it.
	r := Parse(b, "1a 2b 3ac 4: free text 5a\r\n\r\nThanks,\r\nDana\r\n")
	if !r.Complete() || len(r.Answers) != 5 || !reflect.DeepEqual(r.Ignored, []string{"Thanks,\nDana"}) {
		t.Fatalf("answers=%+v issues=%v ignored=%q", r.Answers, r.Issues, r.Ignored)
	}
	// Prose before any answer is still an issue: nothing was understood.
	if lead := Parse(b, "Hi team\n\n1a"); lead.OK() || lead.Issues[0].Fragment != "Hi" {
		t.Fatalf("lead = %+v", lead)
	}
}

func TestRenderTextStatesContextOnce(t *testing.T) {
	b := mustBatch(t)
	text := RenderText(b, RenderOptions{Details: true, Reasons: true})
	if strings.Count(text, "We keep every log for 400 days today.") != 1 {
		t.Fatalf("context not stated exactly once:\n%s", text)
	}
	for _, want := range []string{
		"Retention policy interview (5 questions)",
		"1. Delete debug logs after 30 days?\n   a) Yes (recommended)\n      Nobody reads them later.\n   b) No\n   c) After 90 days",
		"3. Which teams may read raw logs? (pick any)\n   Pick every team that needs access.",
		"4. Anything else the policy must cover?\n   Answer as 4: your words",
		"Storage review: https://example.com/review",
		"question 3 takes several letters, e.g. 3ab",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	html := RenderHTML(b, RenderOptions{Details: true})
	if strings.Count(html, "We keep every log for 400 days today.") != 1 || !strings.Contains(html, "<strong>a)</strong> Yes <em>(recommended)</em>") {
		t.Fatalf("html:\n%s", html)
	}
	quiet := RenderText(b, RenderOptions{OmitReplyHint: true}) + RenderHTML(b, RenderOptions{OmitReplyHint: true})
	if strings.Contains(quiet, "Reply with one line") || strings.Contains(quiet, "4: your words") {
		t.Fatalf("reply hint not omitted:\n%s", quiet)
	}
	b.Title = "<script>x</script>"
	if strings.Contains(RenderHTML(b, RenderOptions{}), "<script>") {
		t.Fatal("html not escaped")
	}
}

func TestSplitTextKeepsBatchIdentityAndLimit(t *testing.T) {
	b := mustBatch(t)
	for i := range b.Questions {
		b.Questions[i].Note = strings.Repeat("long note ", 40)
	}
	parts := SplitText(b, RenderOptions{}, 700)
	if len(parts) < 2 {
		t.Fatalf("expected a split, got %d part(s)", len(parts))
	}
	joined := strings.Join(parts, "\n")
	for n := 1; n <= 5; n++ {
		if !strings.Contains(joined, "\n"+string(rune('0'+n))+". ") {
			t.Fatalf("question %d lost in split", n)
		}
	}
	for i, part := range parts {
		if utf8.RuneCountInString(part) > 700 {
			t.Fatalf("part %d is %d runes", i, utf8.RuneCountInString(part))
		}
		if !strings.Contains(part, "Retention policy interview") || !strings.HasSuffix(part, ")") {
			t.Fatalf("part %d lacks batch identity:\n%s", i, part)
		}
	}
	if !strings.Contains(parts[len(parts)-1], "Reply with one line") {
		t.Fatal("reply hint must end the last part")
	}
	if single := SplitText(mustBatch(t), RenderOptions{}, 4000); len(single) != 1 || strings.Contains(single[0], "part 1") {
		t.Fatalf("short batch split: %v", single)
	}
}

func unfragmented(answers []Answer) []Answer {
	out := append([]Answer(nil), answers...)
	for i := range out {
		out[i].Fragment = ""
	}
	return out
}

// Review (oss#538): an answer read out of the tail of someone's own words is
// never silent. Words that start their own line keep everything after them.
func TestFreeTextTailIsNamed(t *testing.T) {
	b := mustBatch(t)
	// On their own line the reviewer's two inputs are each one answer.
	for _, own := range []string{"4: the retention period is label 5a", "4: keep it until step 2b is done"} {
		r := Parse(b, own)
		if !r.OK() || len(r.Answers) != 1 || r.Answers[0].Text != own[3:] || len(Cautions(r)) != 0 {
			t.Fatalf("%q => %+v issues=%v", own, r.Answers, r.Issues)
		}
	}
	// After another answer on the same line, the tail is read as an answer
	// and the summary says so, naming what was read and how to avoid it.
	r := Parse(b, "1a 4: the retention period is label 5a")
	if !r.OK() || len(r.Answers) != 3 || r.Answers[2].Number != 5 || r.Answers[2].AfterTextOf != 4 || r.Answers[1].Text != "the retention period is label" {
		t.Fatalf("tail answers = %+v", r.Answers)
	}
	summary := Summary(b, r)
	if !strings.Contains(summary, `Check: "5a" came after your own words for question 4, so it was read as an answer to question 5`) || !strings.Contains(summary, "put that answer on its own line") {
		t.Fatalf("summary does not name the tail answer:\n%s", summary)
	}
	// The second input is refused (the words after "2b" are not answers);
	// the problems now say why the line broke there.
	r = Parse(b, "1a 4: keep it until step 2b is done")
	if r.OK() {
		t.Fatalf("expected refusal: %+v", r)
	}
	problems := Problems(r)
	for _, want := range []string{`"is": not an answer`, `"2b" came after your own words for question 4, so it was read as an answer to question 2`} {
		if !strings.Contains(problems, want) {
			t.Fatalf("missing %q in:\n%s", want, problems)
		}
	}
}
