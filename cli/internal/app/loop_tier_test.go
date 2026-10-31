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

func TestOrientDailyDigestKeepsQuietCardsVisible(t *testing.T) {
	brief := loadBriefFixture(t)
	brief["initiatives"] = []any{}
	got := renderOrientText(brief, "Reader")
	for _, raw := range asSlice(brief["daily_digest"]) {
		row := asMap(raw)
		want := fmt.Sprintf("daily digest:  %s %q", anyString(row["ref"]), anyString(row["title"]))
		if !strings.Contains(got, want) {
			t.Fatalf("missing digest card %q:\n%s", want, got)
		}
	}
	for _, reason := range []string{"default promise elapsed", "awaiting owner"} {
		if !strings.Contains(got, reason) {
			t.Fatalf("missing digest reason %q:\n%s", reason, got)
		}
	}
}

func TestOrientAgentReaderScopesInitiativesAndWaitingOnHuman(t *testing.T) {
	brief := map[string]any{
		"schema": "brief.v1",
		"principal": map[string]any{
			"id":      "agent:worker",
			"display": "worker on host-1",
			"kind":    "agent",
		},
		"decisions": []any{
			map[string]any{
				"ref":           "inbox:ask-incoming",
				"inbox_item_id": "ask-incoming",
				"title":         "Incoming ask for reader",
			},
		},
		"your_asks":        []any{},
		"answered_for_you": []any{},
		"initiatives": []any{
			map[string]any{
				"ref":                      "card:unrelated-overdue",
				"title":                    "Unrelated overdue",
				"state":                    "past_promise",
				"past_promise_for_seconds": float64(86400),
				"yours":                    false,
				"promise": map[string]any{
					"at":     "2026-10-10T00:00:00Z",
					"what":   "other agent checkpoint",
					"source": "note",
					"author": map[string]any{"id": "agent:other", "display": "worker on host-1", "kind": "agent"},
				},
			},
			map[string]any{
				"ref":   "card:assigned-no-promise",
				"title": "Assigned card",
				"state": "no_promise",
				"yours": true,
			},
			map[string]any{
				"ref":   "card:authored-promise",
				"title": "Authored promise",
				"state": "moving",
				"promise": map[string]any{
					"at":     "2026-10-18T00:00:00Z",
					"what":   "ship fix",
					"source": "note",
					"author": map[string]any{"id": "agent:worker", "display": "worker on host-1", "kind": "agent"},
				},
			},
		},
	}
	got := renderOrientText(brief, "worker on host-1 · workspace demo")
	if strings.Contains(got, "Incoming ask for reader") {
		t.Fatalf("orient must not fall back from your_asks to decisions:\n%s", got)
	}
	if !strings.Contains(got, "waiting on human:  none\n") {
		t.Fatalf("expected waiting on human:  none, got:\n%s", got)
	}
	if strings.Contains(got, "your initiatives:  unrelated-overdue") {
		t.Fatalf("unrelated initiative must not be labeled your initiatives:\n%s", got)
	}
	if !strings.Contains(got, "workspace:  unrelated-overdue  PAST PROMISE") {
		t.Fatalf("unrelated initiative should appear under workspace:\n%s", got)
	}
	if !strings.Contains(got, "your initiatives:  assigned-no-promise  no promise\n") {
		t.Fatalf("assigned initiative missing from your initiatives:\n%s", got)
	}
	if !strings.Contains(got, "your initiatives:  authored-promise  moving") {
		t.Fatalf("promise-authored initiative missing from your initiatives:\n%s", got)
	}
	if !strings.Contains(got, "next: anx note card:assigned-no-promise --next <date> \"<what>\"\n") {
		t.Fatalf("next: must pick from the agent's own initiatives, got:\n%s", got)
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
	doc := dailyJSON(t, out)
	got := doc["result"]
	if workspace := asMap(doc["workspace"]); workspace["base_url"] != server.URL || workspace["source"] == "" {
		t.Fatalf("workspace=%#v", workspace)
	}
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
		"2026-10-20":           time.Date(2026, 10, 20, 23, 59, 59, 0, time.UTC),
		"2026-10-20T10:00:00Z": time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC),
	} {
		got, ok := parsePromiseWhen(raw, now)
		if !ok || !got.Equal(want) {
			t.Fatalf("%s: got %v ok=%v, want %v", raw, got, ok, want)
		}
	}
	for _, raw := range []string{"", "0d", "-1d", "2w", "tomorrow", "2026-13-01", "3651d"} {
		if _, ok := parsePromiseWhen(raw, now); ok {
			t.Fatalf("%q must be rejected", raw)
		}
	}
	// Date-only promise in `anx note` resolves to 23:59:59 UTC (matching core's
	// plans.ParseWhen), so repeating the same instant as RFC3339 without
	// evidence is not flagged as a move.
	doc, _ := runNoteForTest(t, "2026-10-16T23:59:59Z", "card:x", "Still on track", "--next", "2026-10-16", "checkpoint")
	if gotAt := anyString(asMap(asMap(doc["result"])["next"])["at"]); gotAt != "2026-10-16T23:59:59Z" {
		t.Fatalf("result.next.at = %q, want 2026-10-16T23:59:59Z", gotAt)
	}
	if _, ok := warningCodes(doc)["note_moved_without_evidence"]; ok {
		t.Fatalf("same end-of-day UTC instant must not warn as a move: %#v", doc["warnings"])
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
	health   string
}

func noteServer(t *testing.T, rec *noteRecorder) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/cards/"):
			health := rec.health
			if health == "" {
				health = `{}`
			}
			fmt.Fprintf(w, `{"card":{"id":"card-1","ref":"card:x","thread_id":"thread-1","title":"X","column_key":"in_progress","plan_health":%s}}`, health)
		case r.URL.Path == "/events" && r.Method == http.MethodGet:
			rec.mu.Lock()
			prev := rec.previous
			rec.mu.Unlock()
			if prev == "" {
				fmt.Fprint(w, `{"events":[]}`)
				return
			}
			fmt.Fprintf(w, `{"events":[{"id":"e0","actor_id":"actor-1","ts":"2026-10-14T09:00:00Z","payload":{"next":{"at":%q}}}]}`, prev)
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
	if codes := warningCodes(doc); len(codes) != 1 || codes["note_promise_unowned"] == "" {
		t.Fatalf("expected warning for unassigned fixture card: %#v", doc["warnings"])
	}
}

func runNoteWithHealth(t *testing.T, health string, args ...string) (map[string]any, map[string]any) {
	t.Helper()
	rec := &noteRecorder{health: health}
	server := noteServer(t, rec)
	t.Cleanup(server.Close)
	a, out := dailyTestApp(t, server.URL)
	argv := append([]string{"--json", "--as", "worker", "note"}, args...)
	if exit := a.Run(argv); exit != 0 {
		t.Fatalf("exit=%d %s", exit, out.String())
	}
	return dailyJSON(t, out), asMap(asMap(rec.posted["event"])["payload"])
}

func TestEvidenceNoteKeepsLivePromise(t *testing.T) {
	at := time.Now().Add(6 * time.Hour).UTC().Format(time.RFC3339)
	doc, payload := runNoteWithHealth(t, fmt.Sprintf(`{"state":"moving","promise":{"at":%q,"what":"ship the fix","source":"note"}}`, at), "card:x", "Merged", "--evidence", "event:e1")
	next := asMap(payload["next"])
	if anyString(next["at"]) != at || anyString(next["what"]) != "ship the fix" {
		t.Fatalf("next=%#v", next)
	}
	if !strings.Contains(warningCodes(doc)["note_promise_kept"], "promise kept: ship the fix") {
		t.Fatalf("warnings=%#v", doc["warnings"])
	}
}

func TestEvidenceNoteRenewsLapsedPromiseWithItsText(t *testing.T) {
	_, payload := runNoteWithHealth(t, `{"state":"past_promise","promise":{"at":"2020-01-01T00:00:00Z","what":"ship the fix","source":"note"}}`, "card:x", "Merged", "--evidence", "event:e1")
	next := asMap(payload["next"])
	at, err := time.Parse(time.RFC3339, anyString(next["at"]))
	if err != nil || !at.After(time.Now()) || anyString(next["what"]) != "ship the fix" {
		t.Fatalf("next=%#v err=%v", next, err)
	}
}

func TestEvidenceNoteStartsDefaultClockWhenNothingIsLive(t *testing.T) {
	_, payload := runNoteWithHealth(t, `{"state":"no_promise"}`, "card:x", "Merged", "--evidence", "event:e1")
	if anyString(asMap(payload["next"])["what"]) != "report progress" {
		t.Fatalf("payload=%#v", payload)
	}
}

func TestEvidenceNoteLeavesOtherClocksAlone(t *testing.T) {
	for _, health := range []string{
		`{"state":"done"}`,
		`{"state":"digest"}`,
		`{"state":"moving","promise":{"at":"2099-01-01T00:00:00Z","what":"Cadence","source":"cadence"}}`,
		`{}`,
	} {
		_, payload := runNoteWithHealth(t, health, "card:x", "Merged", "--evidence", "event:e1")
		if _, ok := payload["next"]; ok {
			t.Fatalf("%s: next=%#v", health, payload["next"])
		}
	}
}

func TestNoteMetEndsThePromise(t *testing.T) {
	doc, payload := runNoteWithHealth(t, `{"state":"moving","promise":{"at":"2099-01-01T00:00:00Z","what":"ship","source":"note"}}`, "card:x", "Shipped", "--evidence", "event:e1", "--met")
	if _, ok := payload["next"]; ok {
		t.Fatalf("next=%#v", payload["next"])
	}
	if len(warningCodes(doc)) != 0 {
		t.Fatalf("warnings=%#v", doc["warnings"])
	}
}

func TestNoteWithoutEvidenceLeavesLivePromiseQuiet(t *testing.T) {
	doc, payload := runNoteWithHealth(t, `{"state":"moving","promise":{"at":"2099-01-01T00:00:00Z","what":"ship","source":"note"}}`, "card:x", "Still on it")
	if _, ok := payload["next"]; ok || len(warningCodes(doc)) != 0 {
		t.Fatalf("payload=%#v warnings=%#v", payload, doc["warnings"])
	}
}

func TestNoteDecidedByRecordsTheRuling(t *testing.T) {
	_, payload := runNoteWithHealth(t, `{}`, "card:x", "Ship Friday", "--decided-by", "Dana")
	if anyString(payload["decided_by"]) != "Dana" || anyString(payload["text"]) != "Decided by Dana: Ship Friday" {
		t.Fatalf("payload=%#v", payload)
	}
}

func TestNoteFlagMisuseFailsBeforeAnyRequest(t *testing.T) {
	now := time.Now()
	for _, bad := range [][]string{
		{"card:x", "Shipped", "--met"},
		{"card:x", "Shipped", "--met", "--evidence", "event:e", "--next", "1d"},
		{"card:x", "--decided-by", "Dana"},
		{"card:x", "Ship", "--decided-by", " "},
	} {
		if _, err := parseNoteArgs(bad, now); err == nil {
			t.Fatalf("%v must be a usage error", bad)
		}
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

func TestOrientShowsQuietCurationActions(t *testing.T) {
	b := map[string]any{"principal": map[string]any{"kind": "agent"}, "curation_suggestions": []any{map[string]any{"key": "k", "action": "batch", "title": "Group delivery", "apply_argv": []any{"anx", "api", "createCurationProposal", "--json-body", `{"resolution":"apply"}`}, "dismiss_argv": []any{"anx", "api", "createCurationProposal", "--json-body", `{"resolution":"dismiss"}`}}}}
	text := renderOrientText(b, "worker")
	if !strings.Contains(text, "suggestion: k batch") || !strings.Contains(text, `next anx api createCurationProposal --json-body '{"resolution":"apply"}'`) || !strings.Contains(text, "waiting on human:  none") {
		t.Fatal(text)
	}
}
