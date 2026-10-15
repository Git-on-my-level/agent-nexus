package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func loadBriefFixture(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "contracts", "fixtures", "brief", "brief.v1.json"))
	if err != nil {
		t.Fatalf("read shared brief fixture: %v", err)
	}
	var brief map[string]any
	if err := json.Unmarshal(raw, &brief); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return brief
}

// I7: orient renders from the shared brief.v1 fixture.
func TestOrientTextRendersSharedBriefFixture(t *testing.T) {
	got := renderOrientText(loadBriefFixture(t), "build-agent on host-1 · workspace demo")
	golden := filepath.Join("testdata", "orient_brief.golden.txt")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (UPDATE_GOLDEN=1 regenerates): %v", err)
	}
	if got != string(want) {
		t.Fatalf("orient text drifted from golden:\n--- got\n%s--- want\n%s", got, want)
	}
	for _, prefix := range []string{"you:", "answered for you:", "waiting on human:", "your initiatives:", "next:"} {
		if !strings.Contains(got, "\n"+prefix) && !strings.HasPrefix(got, prefix) {
			t.Fatalf("missing %q line:\n%s", prefix, got)
		}
	}
}

func briefServer(t *testing.T, brief []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/brief":
			w.Write(brief)
		case "/agents/agent-1":
			fmt.Fprint(w, `{"agent":{"id":"agent-1","actor_id":"actor-1","handle":"worker.host"},"open_asks":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestOrientJSONPrintsBriefVerbatim(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "contracts", "fixtures", "brief", "brief.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	server := briefServer(t, raw)
	defer server.Close()
	a, out := dailyTestApp(t, server.URL)
	if exit := a.Run([]string{"--json", "--as", "worker", "orient"}); exit != 0 {
		t.Fatalf("exit=%d %s", exit, out.String())
	}
	var want any
	_ = json.Unmarshal(raw, &want)
	got := dailyJSON(t, out)["result"]
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("result is not the brief verbatim:\n%s\n%s", gotJSON, wantJSON)
	}
}

func TestOrientRejectsStaleHours(t *testing.T) {
	if _, err := preflightConfigIndependentUsage([]string{"orient", "--stale-hours", "24"}); err == nil {
		t.Fatal("--stale-hours must be gone")
	}
}

func TestWorkNoteAndDoneAreRemoved(t *testing.T) {
	for _, argv := range [][]string{
		{"work", "note", "Progress", "card:x"},
		{"work", "done", "card:x", "--evidence", "https://example.test/x"},
		{"work", "context", "card:x"},
	} {
		_, err := preflightConfigIndependentUsage(argv)
		if err == nil || !strings.Contains(err.Error(), "unknown work subcommand") {
			t.Fatalf("%v must be rejected, got %v", argv, err)
		}
	}
}

func TestPromiseWhenGrammar(t *testing.T) {
	now := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	for raw, want := range map[string]time.Time{
		"2d":                   now.Add(48 * time.Hour),
		"36h":                  now.Add(36 * time.Hour),
		"2026-10-20":           time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC),
		"2026-10-20T10:00:00Z": time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC),
	} {
		got, ok := parsePromiseWhen(raw, now)
		if !ok || !got.Equal(want) {
			t.Fatalf("%s: got %v ok=%v", raw, got, ok)
		}
	}
	for _, raw := range []string{"", "0d", "-1d", "2w", "tomorrow", "2026-13-01", "3651d"} {
		if _, ok := parsePromiseWhen(raw, now); ok {
			t.Fatalf("%q must be rejected", raw)
		}
	}
}

func TestNoteFlagShape(t *testing.T) {
	now := time.Now()
	parsed, err := parseNoteArgs([]string{"card:x", "Rerunning sweeps", "--next", "2d", "checkpoint with bench data", "--evidence", "https://example.test/pr/1", "--evidence", "event:e1"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.cardRef != "card:x" || parsed.text != "Rerunning sweeps" || parsed.nextWhen != "2d" || parsed.nextWhat != "checkpoint with bench data" || len(parsed.evidence) != 2 {
		t.Fatalf("parsed=%#v", parsed)
	}
	parsed, err = parseNoteArgs([]string{"card:x", "--evidence", "event:e1", "--next", "2026-10-20", "enroll m6"}, now)
	if err != nil || parsed.text == "" || parsed.nextWhat != "enroll m6" {
		t.Fatalf("parsed=%#v err=%v", parsed, err)
	}
	for _, bad := range [][]string{
		{"card:x", "text", "--next", "soon"},
		{"card:x", "text", "--evidence", "not a ref"},
		{"card:x"},
		{},
	} {
		if _, err := parseNoteArgs(bad, now); err == nil {
			t.Fatalf("%v must be a usage error", bad)
		}
	}
}

type noteRecorder struct {
	mu       sync.Mutex
	posted   map[string]any
	previous string
}

func noteServer(t *testing.T, rec *noteRecorder) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/cards/"):
			fmt.Fprint(w, `{"card":{"id":"card-1","ref":"card:x","thread_id":"thread-1","title":"X","column_key":"in_progress"}}`)
		case r.URL.Path == "/events" && r.Method == http.MethodGet:
			rec.mu.Lock()
			prev := rec.previous
			rec.mu.Unlock()
			if prev == "" {
				fmt.Fprint(w, `{"events":[]}`)
				return
			}
			fmt.Fprintf(w, `{"events":[{"id":"e0","ts":"2026-10-14T09:00:00Z","payload":{"next":{"at":%q}}}]}`, prev)
		case r.URL.Path == "/events" && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			var doc map[string]any
			_ = json.Unmarshal(body, &doc)
			rec.mu.Lock()
			rec.posted = doc
			rec.mu.Unlock()
			fmt.Fprint(w, `{"event":{"id":"evt-9","type":"message_posted"}}`)
		case r.URL.Path == "/agents/me/presence":
			fmt.Fprint(w, `{}`)
		default:
			http.NotFound(w, r)
		}
	}))
}

func runNoteForTest(t *testing.T, previous string, args ...string) (map[string]any, *noteRecorder) {
	t.Helper()
	rec := &noteRecorder{previous: previous}
	server := noteServer(t, rec)
	t.Cleanup(server.Close)
	a, out := dailyTestApp(t, server.URL)
	argv := append([]string{"--json", "--as", "worker", "note"}, args...)
	if exit := a.Run(argv); exit != 0 {
		t.Fatalf("exit=%d %s", exit, out.String())
	}
	return dailyJSON(t, out), rec
}

func warningCodes(doc map[string]any) map[string]string {
	codes := map[string]string{}
	for _, raw := range asSlice(doc["warnings"]) {
		w := asMap(raw)
		codes[anyString(w["code"])] = anyString(w["message"])
	}
	return codes
}

func TestNotePostsNextAndEvidencePayload(t *testing.T) {
	doc, rec := runNoteForTest(t, "", "card:x", "Rerunning sweeps", "--next", "1d", "checkpoint", "--evidence", "https://example.test/pr/1")
	payload := asMap(asMap(rec.posted["event"])["payload"])
	if anyString(asMap(payload["next"])["at"]) != "1d" || anyString(asMap(payload["next"])["what"]) != "checkpoint" {
		t.Fatalf("payload.next=%#v", payload["next"])
	}
	if refs := stringList(payload["evidence_refs"]); len(refs) != 1 || refs[0] != "https://example.test/pr/1" {
		t.Fatalf("payload.evidence_refs=%#v", payload["evidence_refs"])
	}
	if anyString(asMap(doc["result"])["note_ref"]) != "event:evt-9" {
		t.Fatalf("result=%#v", doc["result"])
	}
	if len(warningCodes(doc)) != 0 {
		t.Fatalf("unexpected warnings %#v", doc["warnings"])
	}
}

func TestNoteWarningNoNext(t *testing.T) {
	doc, _ := runNoteForTest(t, "", "card:x", "Progress")
	if msg := warningCodes(doc)["note_no_next"]; !strings.Contains(msg, "no --next") {
		t.Fatalf("warnings=%#v", doc["warnings"])
	}
}

func TestNoteWarningBeyondHorizon(t *testing.T) {
	doc, _ := runNoteForTest(t, "", "card:x", "Progress", "--next", "5d", "later", "--evidence", "event:e1")
	if msg := warningCodes(doc)["note_beyond_horizon"]; !strings.Contains(msg, "beyond the 2-day horizon") {
		t.Fatalf("warnings=%#v", doc["warnings"])
	}
}

func TestNoteWarningMovedWithoutEvidence(t *testing.T) {
	doc, _ := runNoteForTest(t, "1d", "card:x", "Still going", "--next", "2d", "checkpoint")
	if msg := warningCodes(doc)["note_moved_without_evidence"]; !strings.Contains(msg, "promise moved without evidence") {
		t.Fatalf("warnings=%#v", doc["warnings"])
	}
	// With evidence the move is a reset, not a warning.
	doc, _ = runNoteForTest(t, "1d", "card:x", "Merged", "--next", "2d", "checkpoint", "--evidence", "event:e2")
	if _, ok := warningCodes(doc)["note_moved_without_evidence"]; ok {
		t.Fatalf("evidence must suppress the move warning: %#v", doc["warnings"])
	}
}

func TestDoneRequiresEvidence(t *testing.T) {
	if _, err := preflightConfigIndependentUsage([]string{"done", "card:x"}); err == nil {
		t.Fatal("done without --evidence must be a usage error")
	}
	if _, err := preflightConfigIndependentUsage([]string{"done", "card:x", "--evidence", "https://example.test/pr/1"}); err != nil {
		t.Fatal(err)
	}
}

func TestLoopTierCommandsAreStable(t *testing.T) {
	for _, path := range []string{"orient", "context", "note", "ask", "await", "done", "plan", "series push"} {
		if s, ok := loopTierStability(path); !ok || s != "stable" {
			t.Fatalf("%s must be stable", path)
		}
		text, ok := helpTopicText(path)
		if !ok || !strings.Contains(text, "Stability: stable") {
			t.Fatalf("help for %s must state stability:\n%s", path, text)
		}
	}
}

var _ = bytes.Buffer{}
