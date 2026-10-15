package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func askDryRun(t *testing.T, extra ...string) map[string]any {
	t.Helper()
	args := append([]string{"--json", "ask", "Accept?", "--body", "Context", "--recommend", "Accept", "--subject-ref", "card:launch", "--thread-id", "thr1", "--dry-run"}, extra...)
	return assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), nil, nil, args))
}

func TestAskWarningNoDeadline(t *testing.T) {
	codes := warningCodes(askDryRun(t))
	if msg := codes["ask_no_deadline"]; !strings.Contains(msg, "no --by or --blocks") {
		t.Fatalf("warnings=%#v", codes)
	}
	if _, ok := warningCodes(askDryRun(t, "--by", "2d"))["ask_no_deadline"]; ok {
		t.Fatal("--by must silence ask_no_deadline")
	}
	if _, ok := warningCodes(askDryRun(t, "--blocks", "card:report"))["ask_no_deadline"]; ok {
		t.Fatal("--blocks must silence ask_no_deadline")
	}
}

func TestAskByBlocksPriorityPayload(t *testing.T) {
	doc := askDryRun(t, "--by", "2026-10-20", "--blocks", "card:report", "--blocks", "card:dashboard", "--priority", "p1")
	payload := asMap(asMap(asMap(asMap(doc["result"])["body"])["event"])["payload"])
	if payload["by"] != "2026-10-20" || payload["priority"] != "p1" {
		t.Fatalf("payload=%#v", payload)
	}
	if blocks := stringList(payload["blocks"]); len(blocks) != 2 || blocks[0] != "card:report" {
		t.Fatalf("blocks=%#v", payload["blocks"])
	}
}

func TestAskMalformedByAndBlocksAreUsageErrors(t *testing.T) {
	for _, tc := range []struct {
		flags []string
		code  string
	}{
		{[]string{"--by", "friday"}, "invalid_by"},
		{[]string{"--blocks", "not a ref"}, "invalid_request"},
		{[]string{"--priority", "urgent"}, "invalid_request"},
	} {
		args := append([]string{"--json", "ask", "Accept?", "--body", "Context", "--recommend", "Accept", "--subject-ref", "card:launch", "--thread-id", "thr1", "--dry-run"}, tc.flags...)
		payload := assertEnvelopeError(t, runCLIForTest(t, t.TempDir(), nil, nil, args))
		if asMap(payload["error"])["code"] != tc.code {
			t.Fatalf("%v: %#v", tc.flags, payload)
		}
	}
}

func askSubjectServer(t *testing.T, card string, brief ...string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/agents/agent-1":
			fmt.Fprint(w, `{"agent":{"id":"agent-1","actor_id":"actor-1","handle":"worker.host"},"open_asks":[]}`)
		case r.URL.Path == "/brief" && len(brief) > 0:
			fmt.Fprint(w, brief[0])
		case r.URL.Path == "/work" && r.Method == http.MethodPost:
			fmt.Fprint(w, `{"work":{"ref":"card:new-subject","board_ref":"board:tasks"}}`)
		case strings.HasPrefix(r.URL.Path, "/cards/"):
			fmt.Fprint(w, card)
		case r.URL.Path == "/events" && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			var doc map[string]any
			_ = json.Unmarshal(body, &doc)
			fmt.Fprintf(w, `{"event":{"id":%q,"type":"human_attention_requested"}}`, anyString(asMap(doc["event"])["id"]))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestAskWarningOvertakenSubject(t *testing.T) {
	server := askSubjectServer(t, `{"card":{"id":"card-1","ref":"card:labels","thread_id":"thread-1","possibly_moot":{"flagged":true,"reason":"all 2 linked sources are merged"}}}`)
	defer server.Close()
	a, out := dailyTestApp(t, server.URL)
	if exit := a.Run([]string{"--json", "--as", "worker", "ask", "Pick label scheme?", "--body", "Context", "--recommend", "Short", "--subject-ref", "card:labels", "--by", "1d"}); exit != 0 {
		t.Fatalf("exit=%d %s", exit, out.String())
	}
	doc := dailyJSON(t, out)
	msg := warningCodes(doc)["ask_overtaken_subject"]
	if !strings.Contains(msg, "since the subject last moved") || !strings.Contains(msg, "anx ask withdraw event:") || !strings.Contains(msg, "--reason overtaken") {
		t.Fatalf("warnings=%#v", doc["warnings"])
	}
}

func TestAskWarningAutoSubject(t *testing.T) {
	server := askSubjectServer(t, `{"card":{"id":"card-2","ref":"card:new-subject","thread_id":"thread-2"}}`)
	defer server.Close()
	a, out := dailyTestApp(t, server.URL)
	if exit := a.Run([]string{"--json", "--as", "worker", "ask", "Ship it?", "--body", "Context", "--recommend", "Yes", "--by", "1d"}); exit != 0 {
		t.Fatalf("exit=%d %s", exit, out.String())
	}
	doc := dailyJSON(t, out)
	msg := warningCodes(doc)["ask_auto_created_card"]
	if !strings.Contains(msg, "No --subject-ref") || !strings.Contains(msg, "created card:new-subject on board:tasks") || !strings.Contains(msg, "pass --subject-ref card:<slug>") {
		t.Fatalf("warnings=%#v", doc["warnings"])
	}
	if strings.Contains(msg, "Suggestions:") {
		t.Fatalf("a failed initiative read must omit suggestions: %q", msg)
	}
	stderr := a.Stderr.(*bytes.Buffer).String()
	if !strings.Contains(stderr, "warning: "+msg) {
		t.Fatalf("stderr=%q", stderr)
	}
}

func askNoDeadlineMessage(t *testing.T, brief ...string) string {
	t.Helper()
	server := askSubjectServer(t, `{"card":{"id":"card-1","ref":"card:labels","thread_id":"thread-1"}}`, brief...)
	defer server.Close()
	a, out := dailyTestApp(t, server.URL)
	if exit := a.Run([]string{"--json", "--as", "worker", "ask", "Pick label scheme?", "--body", "Context", "--recommend", "Short", "--subject-ref", "card:labels"}); exit != 0 {
		t.Fatalf("exit=%d %s", exit, out.String())
	}
	return warningCodes(dailyJSON(t, out))["ask_no_deadline"]
}

func TestAskWarningNoDeadlineCountsDeadlineDecisions(t *testing.T) {
	msg := askNoDeadlineMessage(t, `{"decisions":[{"decide_by":"2026-10-20T00:00:00Z"},{"decide_by":null},{"decide_by":"2026-10-21T00:00:00Z"},{}],"truncated":{"decisions":false}}`)
	if !strings.Contains(msg, "no --by or --blocks") || !strings.Contains(msg, "sort below 2 decisions that have a deadline") {
		t.Fatalf("message=%q", msg)
	}
	msg = askNoDeadlineMessage(t, `{"decisions":[{"decide_by":"2026-10-20T00:00:00Z"}],"truncated":{"decisions":true}}`)
	if !strings.Contains(msg, "sort below at least 1 decisions that have a deadline") {
		t.Fatalf("message=%q", msg)
	}
}

func TestAskWarningNoDeadlineSurvivesBriefFailure(t *testing.T) {
	// The test server answers /brief with 404; the ask still succeeds and the
	// warning keeps its trigger phrase without a count.
	msg := askNoDeadlineMessage(t)
	if !strings.Contains(msg, "no --by or --blocks") || strings.Contains(msg, "decisions that have a deadline") {
		t.Fatalf("message=%q", msg)
	}
}
