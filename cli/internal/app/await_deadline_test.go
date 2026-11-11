package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"agent-nexus-cli/internal/errnorm"
)

// An overnight wait needed a 14-round shell loop grepping for code=timeout.
// --deadline is one command, absolute (safe to re-arm), and ends with its own
// exit code when no answer came.
func TestAwaitAnswersDeadlineReachedHasItsOwnExit(t *testing.T) {
	fake := &answerWaitFake{}
	app := answerWaitApp(t, fake)
	a, out, cfg := app("session-a")
	store, _ := a.answerWaitStore(cfg)
	_ = store.recordAsk("event:one", "one", time.Now())
	fake.set(nil, map[string]map[string]any{"one": {"status": "open"}})
	deadline := time.Now().Add(1500 * time.Millisecond).UTC().Format(time.RFC3339Nano)
	exit, doc := runAwaitAnswersForTest(t, a, out, "--deadline", deadline)
	errDoc := asMap(doc["error"])
	if exit != 14 || errDoc["code"] != "deadline_reached" {
		t.Fatalf("exit=%d doc=%v", exit, doc)
	}
	details := asMap(errDoc["details"])
	if !strings.Contains(anyString(details["rearm"]), "rerun the same command") || details["deadline"] == "" {
		t.Fatalf("details=%v", details)
	}
}

func TestAwaitAnswersDeadlineDeliversAnswers(t *testing.T) {
	fake := &answerWaitFake{}
	app := answerWaitApp(t, fake)
	a, out, cfg := app("session-a")
	store, _ := a.answerWaitStore(cfg)
	_ = store.recordAsk("event:one", "one", time.Now())
	fake.set(nil, map[string]map[string]any{"one": answeredOutcome("response-one")})
	exit, doc := runAwaitAnswersForTest(t, a, out, "--deadline", time.Now().Add(8*time.Hour).UTC().Format(time.RFC3339), "--settle", "0s")
	if exit != 0 || len(asSlice(asMap(doc["result"])["answers"])) != 1 {
		t.Fatalf("an answer before the deadline returns at once: exit=%d %v", exit, doc)
	}
}

func TestAwaitDeadlineFlagValidation(t *testing.T) {
	now := time.Date(2026, 11, 11, 4, 0, 0, 0, time.UTC)
	if _, _, err := parseAwaitDeadline(trackedString{value: "8h", set: true}, true, now); err == nil {
		t.Fatal("--deadline with --timeout must be refused")
	}
	if _, _, err := parseAwaitDeadline(trackedString{value: "soon", set: true}, false, now); err == nil {
		t.Fatal("unparseable deadline must be refused")
	}
	at, ok, err := parseAwaitDeadline(trackedString{value: "2026-11-11T12:00:00Z", set: true}, false, now)
	if err != nil || !ok || !at.Equal(time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("at=%v ok=%v err=%v", at, ok, err)
	}
	// A relative deadline moves on every re-arm: refused, naming the absolute time.
	_, ok, err = parseAwaitDeadline(trackedString{value: "8h", set: true}, false, now)
	var e *errnorm.Error
	if ok || !errors.As(err, &e) || e.Code != "deadline_relative" || !strings.Contains(e.Message, "--deadline 2026-11-11T12:00:00Z") {
		t.Fatalf("relative deadline must be refused with its absolute form: ok=%v err=%v", ok, err)
	}
	at, ok, err = parseAwaitDeadline(trackedString{value: "2026-11-12", set: true}, false, now)
	if err != nil || !ok || !at.Equal(time.Date(2026, 11, 12, 23, 59, 59, 0, time.UTC)) {
		t.Fatalf("date deadline at=%v ok=%v err=%v", at, ok, err)
	}
}
