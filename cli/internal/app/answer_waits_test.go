package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agent-nexus-cli/internal/config"
)

type answerWaitFake struct {
	mu       sync.Mutex
	page     []map[string]any
	asks     map[string]map[string]any
	failing  bool
	mutating atomic.Int32
	pageHits atomic.Int32
	askReads atomic.Int32
}

func (f *answerWaitFake) set(page []map[string]any, asks map[string]map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.page, f.asks = page, asks
}

func (f *answerWaitFake) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		f.mutating.Add(1)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case f.failing:
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"error":{"code":"unavailable","message":"down"}}`)
	case r.URL.Path == "/agents/agent-1":
		fmt.Fprint(w, `{"agent":{"id":"agent-1","actor_id":"actor-1","handle":"worker.host"}}`)
	case r.URL.Path == "/agent-inbox/asks":
		f.pageHits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"items": f.page, "page_info": map[string]any{"has_more": false}})
	case strings.HasPrefix(r.URL.Path, "/asks/"):
		f.askReads.Add(1)
		ask := f.asks[strings.TrimPrefix(r.URL.Path, "/asks/")]
		if ask == nil {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"error":{"code":"not_found","message":"missing"}}`)
			return
		}
		_ = json.NewEncoder(w).Encode(ask)
	default:
		http.NotFound(w, r)
	}
}

func answeredRow(ask, response string, unread bool) map[string]any {
	return map[string]any{"ask_id": "event:" + ask, "status": "answered", "title": ask, "answer_unread": unread,
		"answer": map[string]any{"text": "Yes", "outcome": "answered", "response_event_id": response, "at": "2026-11-01T00:00:00Z"}}
}

func answeredOutcome(response string) map[string]any {
	return map[string]any{"status": "answered", "subject_ref": "card:x", "response": map[string]any{
		"response_text": "Yes", "outcome": "answered", "responding_actor_id": "human", "response_event_id": response, "at": "2026-11-01T00:00:00Z"}}
}

// answerWaitApp returns one app per session over a shared home, as two harness
// sessions on one host have.
func answerWaitApp(t *testing.T, fake *answerWaitFake) func(session string) (*App, *bytes.Buffer, config.Resolved) {
	t.Helper()
	previous := answerWaitPoll
	answerWaitPoll = 10 * time.Millisecond
	t.Cleanup(func() { answerWaitPoll = previous })
	server := httptest.NewServer(http.HandlerFunc(fake.handler))
	t.Cleanup(server.Close)
	base, _ := dailyTestApp(t, server.URL)
	home, _ := base.UserHomeDir()
	return func(session string) (*App, *bytes.Buffer, config.Resolved) {
		a, out := dailyTestApp(t, server.URL)
		a.UserHomeDir = func() (string, error) { return home, nil }
		getenv := a.Getenv
		a.Getenv = func(k string) string {
			if k == "CLAUDE_CODE_SESSION_ID" {
				return session
			}
			if k == "HOME" {
				return home
			}
			return getenv(k)
		}
		return a, out, config.Resolved{BaseURL: server.URL, As: "worker", ConfigDir: home + "/.config/anx"}
	}
}

func runAwaitAnswersForTest(t *testing.T, a *App, out *bytes.Buffer, args ...string) (int, map[string]any) {
	t.Helper()
	out.Reset()
	exit := a.Run(append([]string{"--json", "--as", "worker", "await", "--answers"}, args...))
	return exit, dailyJSON(t, out)
}

func TestAwaitAnswersBlocksPastAnswersThatAlreadyExisted(t *testing.T) {
	fake := &answerWaitFake{}
	fake.set([]map[string]any{answeredRow("old", "response-old", true)}, nil)
	app := answerWaitApp(t, fake)
	a, out, _ := app("session-a")
	if exit, doc := runAwaitAnswersForTest(t, a, out, "--timeout", "150ms"); exit != 8 {
		t.Fatalf("an answer from before the first wait must not fire: exit=%d %v", exit, doc)
	}
	fake.set([]map[string]any{answeredRow("new", "response-new", true), answeredRow("old", "response-old", true)}, nil)
	exit, doc := runAwaitAnswersForTest(t, a, out, "--timeout", "2s", "--settle", "0")
	result := asMap(doc["result"])
	answers := asSlice(result["answers"])
	if exit != 0 || len(answers) != 1 || anyString(asMap(answers[0])["ask_id"]) != "event:new" {
		t.Fatalf("expected only the new answer: exit=%d %v", exit, doc)
	}
	if exit, doc := runAwaitAnswersForTest(t, a, out, "--timeout", "150ms"); exit != 8 {
		t.Fatalf("a re-armed wait must block on what it already returned: exit=%d %v", exit, doc)
	}
	if fake.mutating.Load() != 0 {
		t.Fatalf("await --answers wrote to the server %d time(s); it must never mark anything", fake.mutating.Load())
	}
}

func TestAwaitAnswersDeliversOwnAskAnsweredBeforeFirstWait(t *testing.T) {
	fake := &answerWaitFake{}
	app := answerWaitApp(t, fake)
	a, out, cfg := app("session-a")
	store, err := a.answerWaitStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.recordAsk("event:mine", "Mine?", time.Now()); err != nil {
		t.Fatal(err)
	}
	// Absent from the page: an old ask answered late sits behind newer asks.
	fake.set(nil, map[string]map[string]any{"mine": answeredOutcome("response-mine")})
	exit, doc := runAwaitAnswersForTest(t, a, out, "--timeout", "2s")
	answers := asSlice(asMap(doc["result"])["answers"])
	if exit != 0 || len(answers) != 1 {
		t.Fatalf("own answer must fire even on the first wait: exit=%d %v", exit, doc)
	}
	first := asMap(answers[0])
	if first["title"] != "Mine?" || anyString(asMap(first["answer"])["response_event_id"]) != "response-mine" {
		t.Fatalf("lookup by id lost the row shape: %v", first)
	}
	if exit, doc := runAwaitAnswersForTest(t, a, out, "--timeout", "150ms"); exit != 8 {
		t.Fatalf("own answer must be handed over once: exit=%d %v", exit, doc)
	}
}

func TestAwaitAnswersLeavesOtherSessionsAnswersAlone(t *testing.T) {
	fake := &answerWaitFake{}
	app := answerWaitApp(t, fake)
	a, outA, cfg := app("session-a")
	b, outB, cfgB := app("session-b")
	storeA, err := a.answerWaitStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := storeA.recordAsk("event:a-ask", "A?", time.Now()); err != nil {
		t.Fatal(err)
	}
	storeB, err := b.answerWaitStore(cfgB)
	if err != nil {
		t.Fatal(err)
	}
	if err := storeB.recordAsk("event:b-ask", "B?", time.Now()); err != nil {
		t.Fatal(err)
	}
	row := answeredRow("a-ask", "response-a", true)
	fake.set([]map[string]any{row, {"ask_id": "event:b-ask", "status": "open"}}, map[string]map[string]any{
		"a-ask": answeredOutcome("response-a"),
		"b-ask": {"status": "open"},
	})
	exit, doc := runAwaitAnswersForTest(t, b, outB, "--timeout", "150ms")
	if exit != 8 {
		t.Fatalf("session B must not receive session A's answer: exit=%d %v", exit, doc)
	}
	if details := asMap(asMap(doc["error"])["details"]); details["other_session_unread"] != float64(1) {
		t.Fatalf("timeout must count what other sessions have unread: %v", doc)
	}
	exit, doc = runAwaitAnswersForTest(t, b, outB, "--timeout", "2s", "--all", "--settle", "0")
	if exit != 0 || len(asSlice(asMap(doc["result"])["answers"])) != 1 || asMap(doc["result"])["scope"] != "all" {
		t.Fatalf("--all must widen to the other session's answer: exit=%d %v", exit, doc)
	}
	exit, doc = runAwaitAnswersForTest(t, a, outA, "--timeout", "2s", "--settle", "0")
	if exit != 0 || len(asSlice(asMap(doc["result"])["answers"])) != 1 {
		t.Fatalf("session A must still receive its own answer after B looked: exit=%d %v", exit, doc)
	}
	if fake.mutating.Load() != 0 {
		t.Fatalf("waits wrote to the server: %d", fake.mutating.Load())
	}
}

func TestAwaitAnswersSettlesWhileAsksStayOpenAndFlushesWhenNoneDo(t *testing.T) {
	fake := &answerWaitFake{}
	app := answerWaitApp(t, fake)
	a, out, cfg := app("session-a")
	store, err := a.answerWaitStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two"} {
		if err := store.recordAsk("event:"+id, id, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	fake.set(nil, map[string]map[string]any{"one": answeredOutcome("response-one"), "two": {"status": "open"}})
	go func() {
		time.Sleep(100 * time.Millisecond)
		fake.set(nil, map[string]map[string]any{"one": answeredOutcome("response-one"), "two": answeredOutcome("response-two")})
	}()
	started := time.Now()
	exit, doc := runAwaitAnswersForTest(t, a, out, "--timeout", "10s", "--settle", "8s")
	if exit != 0 || len(asSlice(asMap(doc["result"])["answers"])) != 2 {
		t.Fatalf("a close second answer belongs in the same batch: exit=%d %v", exit, doc)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("no open asks remain, so the settle window must end early; took %s", elapsed)
	}
}

func TestAwaitAnswersReturnsHeldAnswersAtTheDeadline(t *testing.T) {
	fake := &answerWaitFake{}
	app := answerWaitApp(t, fake)
	a, out, cfg := app("session-a")
	store, _ := a.answerWaitStore(cfg)
	_ = store.recordAsk("event:one", "one", time.Now())
	_ = store.recordAsk("event:two", "two", time.Now())
	fake.set(nil, map[string]map[string]any{"one": answeredOutcome("response-one"), "two": {"status": "open"}})
	exit, doc := runAwaitAnswersForTest(t, a, out, "--timeout", "150ms", "--settle", "1h")
	if exit != 0 || len(asSlice(asMap(doc["result"])["answers"])) != 1 {
		t.Fatalf("a held answer must never become a timeout: exit=%d %v", exit, doc)
	}
}

func TestAwaitAnswersWakesWhenOwnAskIsWithdrawn(t *testing.T) {
	fake := &answerWaitFake{}
	app := answerWaitApp(t, fake)
	a, out, cfg := app("session-a")
	store, _ := a.answerWaitStore(cfg)
	_ = store.recordAsk("event:gone", "Gone?", time.Now())
	fake.set(nil, map[string]map[string]any{"gone": {"status": "withdrawn", "subject_ref": "card:x", "response": map[string]any{"response_event_id": "response-gone"}}})
	exit, doc := runAwaitAnswersForTest(t, a, out, "--timeout", "2s")
	answers := asSlice(asMap(doc["result"])["answers"])
	if exit != 0 || len(answers) != 1 || asMap(answers[0])["status"] != "withdrawn" || asMap(answers[0])["answer"] != nil {
		t.Fatalf("a withdrawn ask must end the wait with no answer: exit=%d %v", exit, doc)
	}
}

func TestAwaitAnswersSurvivesAnOutageWhileWaiting(t *testing.T) {
	fake := &answerWaitFake{}
	app := answerWaitApp(t, fake)
	a, out, cfg := app("session-a")
	store, _ := a.answerWaitStore(cfg)
	_ = store.recordAsk("event:one", "one", time.Now())
	fake.set(nil, map[string]map[string]any{"one": {"status": "open"}})
	go func() {
		time.Sleep(50 * time.Millisecond)
		fake.mu.Lock()
		fake.failing = true
		fake.mu.Unlock()
		time.Sleep(100 * time.Millisecond)
		fake.mu.Lock()
		fake.failing = false
		fake.asks = map[string]map[string]any{"one": answeredOutcome("response-one")}
		fake.mu.Unlock()
	}()
	exit, doc := runAwaitAnswersForTest(t, a, out, "--timeout", "20s", "--settle", "0")
	if exit != 0 || len(asSlice(asMap(doc["result"])["answers"])) != 1 {
		t.Fatalf("a long wait must outlast a server restart: exit=%d %v", exit, doc)
	}
}

func TestAwaitAnswersFirstAllReturnsOtherSessionsAnswers(t *testing.T) {
	fake := &answerWaitFake{}
	app := answerWaitApp(t, fake)
	a, _, cfg := app("session-a")
	b, outB, _ := app("session-b")
	storeA, _ := a.answerWaitStore(cfg)
	_ = storeA.recordAsk("event:a-ask", "A?", time.Now())
	fake.set([]map[string]any{answeredRow("a-ask", "response-a", true)}, map[string]map[string]any{"a-ask": answeredOutcome("response-a")})
	exit, doc := runAwaitAnswersForTest(t, b, outB, "--timeout", "2s", "--all", "--settle", "0")
	if exit != 0 || len(asSlice(asMap(doc["result"])["answers"])) != 1 {
		t.Fatalf("a first --all must not baseline another session's answer away: exit=%d %v", exit, doc)
	}
}

func TestAwaitAnswersKeepsPendingWhenCursorSaveFails(t *testing.T) {
	fake := &answerWaitFake{}
	app := answerWaitApp(t, fake)
	a, out, cfg := app("session-a")
	store, _ := a.answerWaitStore(cfg)
	_ = store.recordAsk("event:mine", "Mine?", time.Now())
	if err := os.MkdirAll(filepath.Join(store.wait, "seen.json"), 0o700); err != nil {
		t.Fatal(err) // a directory in the cursor's place makes the rename fail
	}
	fake.set(nil, map[string]map[string]any{"mine": answeredOutcome("response-mine")})
	exit, doc := runAwaitAnswersForTest(t, a, out, "--timeout", "300ms", "--settle", "0")
	if exit == 0 && len(store.pending()) != 1 {
		t.Fatalf("a failed save must keep the ask owed: %v", doc)
	}
	if err := os.Remove(filepath.Join(store.wait, "seen.json")); err != nil {
		t.Fatal(err)
	}
	if len(store.pending()) != 1 {
		t.Fatal("pending entry lost")
	}
	exit, doc = runAwaitAnswersForTest(t, a, out, "--timeout", "2s", "--settle", "0")
	if exit != 0 || len(asSlice(asMap(doc["result"])["answers"])) != 1 {
		t.Fatalf("the answer must come back after the state recovers: exit=%d %v", exit, doc)
	}
}

func TestAwaitAnswersRetriesAnOutageAtStartup(t *testing.T) {
	fake := &answerWaitFake{failing: true}
	app := answerWaitApp(t, fake)
	a, out, cfg := app("session-a")
	store, _ := a.answerWaitStore(cfg)
	_ = store.recordAsk("event:one", "one", time.Now())
	go func() {
		time.Sleep(100 * time.Millisecond)
		fake.mu.Lock()
		fake.failing = false
		fake.asks = map[string]map[string]any{"one": answeredOutcome("response-one")}
		fake.mu.Unlock()
	}()
	exit, doc := runAwaitAnswersForTest(t, a, out, "--timeout", "20s", "--settle", "0")
	if exit != 0 || len(asSlice(asMap(doc["result"])["answers"])) != 1 {
		t.Fatalf("arming during an outage must still wait: exit=%d %v", exit, doc)
	}
}

func TestAwaitAnswersBoundsReadsPerPollAndReachesEveryAsk(t *testing.T) {
	fake := &answerWaitFake{}
	app := answerWaitApp(t, fake)
	a, out, cfg := app("session-a")
	store, _ := a.answerWaitStore(cfg)
	asks := map[string]map[string]any{}
	for i := 0; i < answerWaitPollBudget*3; i++ {
		id := fmt.Sprintf("ask-%d", i)
		_ = store.recordAsk("event:"+id, id, time.Now())
		asks[id] = map[string]any{"status": "open"}
	}
	// Whichever ask sorts last on disk takes a later turn than the first poll.
	pending := store.pending()
	last := strings.TrimPrefix(pending[len(pending)-1].AskID, "event:")
	asks[last] = answeredOutcome("response-last")
	fake.set(nil, asks)
	exit, doc := runAwaitAnswersForTest(t, a, out, "--timeout", "5s", "--settle", "0")
	answers := asSlice(asMap(doc["result"])["answers"])
	if exit != 0 || len(answers) != 1 || anyString(asMap(answers[0])["ask_id"]) != "event:"+last {
		t.Fatalf("rotation must reach the last ask: exit=%d %v", exit, doc)
	}
	if peak := fake.askReads.Load(); peak < int32(answerWaitPollBudget*2) {
		t.Fatalf("the last ask should have needed later polls, read %d", peak)
	}
}

func TestAwaitAnswersStopsOnAnAuthFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/agents/agent-1" {
			fmt.Fprint(w, `{"agent":{"id":"agent-1","actor_id":"actor-1","handle":"worker.host"}}`)
			return
		}
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"error":{"code":"forbidden","message":"no"}}`)
	}))
	defer server.Close()
	a, out := dailyTestApp(t, server.URL)
	started := time.Now()
	if exit := a.Run([]string{"--json", "--as", "worker", "await", "--answers", "--timeout", "20s"}); exit == 0 || exit == 8 {
		t.Fatalf("a refusal must end the wait with its own error: exit=%d %s", exit, out.String())
	}
	if time.Since(started) > 10*time.Second {
		t.Fatal("a refusal must not be retried until the timeout")
	}
}

func TestNormalizeAskRefAcceptsEverySpellingTheCLIPrints(t *testing.T) {
	for raw, want := range map[string]string{
		"event:ev_x":                   "event:ev_x",
		"ev_x":                         "event:ev_x",
		"  ev_x ":                      "event:ev_x",
		"ask:thread:ev_x:ev_x":         "event:ev_x",
		"inbox:ask:thread:subj:ev_x":   "event:ev_x",
		"review:thread:none:ev_x":      "event:ev_x",
		"escalate:thread:subj:ev_x":    "event:ev_x",
		"11111111-2222-3333-4444-5555": "event:11111111-2222-3333-4444-5555",
	} {
		if got := normalizeAskRef(raw); got != want {
			t.Errorf("normalizeAskRef(%q)=%q, want %q", raw, got, want)
		}
	}
}

func TestInboxReadAcceptsBriefRefAndGuardsOtherSessions(t *testing.T) {
	fake := &answerWaitFake{}
	marks := atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/agent-inbox/answers/read" {
			marks.Add(1)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"answer":{"read":true,"already_read":false}}`)
			return
		}
		fake.handler(w, r)
	}))
	defer server.Close()
	a, out := dailyTestApp(t, server.URL)
	home, _ := a.UserHomeDir()
	session := "session-b"
	getenv := a.Getenv
	a.Getenv = func(k string) string {
		if k == "CLAUDE_CODE_SESSION_ID" {
			return session
		}
		return getenv(k)
	}
	cfg := config.Resolved{BaseURL: server.URL, As: "worker", ConfigDir: home + "/.config/anx"}
	session = "session-a"
	storeA, err := a.answerWaitStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_ = storeA.recordAsk("event:ev_a", "A?", time.Now())
	session = "session-b"
	fake.set([]map[string]any{answeredRow("ev_a", "response-a", true), answeredRow("ev_u", "response-u", true)}, nil)

	if exit := a.Run([]string{"--json", "--base-url", server.URL, "--config-dir", cfg.ConfigDir, "--as", "worker", "inbox", "read", "ask:thread:ev_a:ev_a"}); exit == 0 || marks.Load() != 0 {
		t.Fatalf("another session's answer must not be marked: exit=%d marks=%d %s", exit, marks.Load(), out.String())
	}
	if !strings.Contains(out.String(), "other_session_answer") {
		t.Fatalf("refusal must name itself: %s", out.String())
	}
	out.Reset()
	if exit := a.Run([]string{"--json", "--base-url", server.URL, "--config-dir", cfg.ConfigDir, "--as", "worker", "inbox", "read", "ask:thread:ev_u:ev_u"}); exit != 0 || marks.Load() != 1 {
		t.Fatalf("the ref orient prints must work: exit=%d marks=%d %s", exit, marks.Load(), out.String())
	}
	out.Reset()
	if exit := a.Run([]string{"--json", "--base-url", server.URL, "--config-dir", cfg.ConfigDir, "--as", "worker", "inbox", "read", "event:ev_a", "--force"}); exit != 0 || marks.Load() != 2 {
		t.Fatalf("--force must override: exit=%d marks=%d %s", exit, marks.Load(), out.String())
	}
}

func TestAnswerSessionKeyPrefersExplicitThenSingleHarness(t *testing.T) {
	env := map[string]string{}
	a := &App{Getenv: func(k string) string { return env[k] }}
	if a.answerSessionKey() != "" {
		t.Fatal("no marker must mean no session")
	}
	env["CLAUDE_CODE_SESSION_ID"] = "c"
	claude := a.answerSessionKey()
	env["CODEX_THREAD_ID"] = "x"
	if a.answerSessionKey() != "" {
		t.Fatal("two inherited harness markers without an execution id are ambiguous")
	}
	env["AGENTCTL_EXECUTION_ID"] = "exec"
	managed := a.answerSessionKey()
	if managed == "" || managed == claude {
		t.Fatalf("a managed child must not borrow its parent's session: %q", managed)
	}
	env["ANX_SESSION"] = "role"
	if key := a.answerSessionKey(); key == managed || key == "" {
		t.Fatal("ANX_SESSION must win")
	}
}

func TestOrientNextOnlySuggestsMarkingThisSessionsAnswer(t *testing.T) {
	render := func(items ...any) string {
		return renderOrientText(map[string]any{"answered_for_you": items}, "me")
	}
	other := map[string]any{"ref": "inbox:ask:t:s:ev_o", "title": "O?", "answer": "No", "session_origin": "other_session"}
	unknown := map[string]any{"ref": "inbox:ask:t:s:ev_u", "title": "U?", "answer": "No"}
	mine := map[string]any{"ref": "inbox:ask:t:s:ev_m", "title": "M?", "answer": "Yes", "session_origin": "this_session", "ask_id": "event:ev_m"}
	if text := render(other); !strings.Contains(text, "next: anx inbox list --unread") || strings.Contains(text, "anx inbox read") || !strings.Contains(text, "another session's ask") {
		t.Fatalf("a fresh session must not be told to mark another session's answer read:\n%s", text)
	}
	if text := render(unknown); !strings.Contains(text, "next: anx inbox list --unread") {
		t.Fatalf("unknown origin must not suggest marking read:\n%s", text)
	}
	if text := render(other, mine); !strings.Contains(text, "next: anx inbox read event:ev_m") {
		t.Fatalf("this session's own answer is the next step:\n%s", text)
	}
}
