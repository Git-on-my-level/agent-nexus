package app

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func runNoteOn(t *testing.T, rec *noteRecorder, args ...string) (map[string]any, int) {
	t.Helper()
	server := noteServer(t, rec)
	t.Cleanup(server.Close)
	return runNoteAgainst(t, server, args...)
}

func runNoteAgainst(t *testing.T, server *httptest.Server, args ...string) (map[string]any, int) {
	t.Helper()
	a, out := dailyTestApp(t, server.URL)
	exit := a.Run(append([]string{"--json", "--as", "worker", "note"}, args...))
	return dailyJSON(t, out), exit
}

// An agent wrote about ten promise notes on cards that stayed backlog for
// twelve hours. A promise or evidence note is progress: it starts a card that
// has not started when the writer owns it or nobody does.
func TestPromiseNoteStartsAnUnstartedCard(t *testing.T) {
	rec := &noteRecorder{column: "backlog"}
	doc, exit := runNoteOn(t, rec, "card:x", "Sized the reads", "--next", "1d", "publish sizing")
	if exit != 0 {
		t.Fatalf("exit=%d %v", exit, doc)
	}
	started := asMap(asMap(doc["result"])["started"])
	if started["from"] != "backlog" || started["to"] != "in_progress" || started["assigned"] != "actor:actor-1" {
		t.Fatalf("started=%#v doc=%v", started, doc)
	}
	joined := strings.Join(rec.writes, ",")
	if !strings.Contains(joined, "PATCH") || !strings.Contains(joined, "/move") {
		t.Fatalf("expected assign and move writes, got %v", rec.writes)
	}
}

func TestPromiseNoteLeavesSomeoneElsesCardAndSaysSo(t *testing.T) {
	rec := &noteRecorder{column: "backlog", assignee: `["actor:someone-else"]`}
	doc, exit := runNoteOn(t, rec, "card:x", "Looked at it", "--next", "1d", "follow up")
	if exit != 0 {
		t.Fatalf("exit=%d %v", exit, doc)
	}
	if asMap(doc["result"])["started"] != nil || len(rec.writes) != 0 {
		t.Fatalf("someone else's card must not move: %v %v", doc, rec.writes)
	}
	if msg := warningCodes(doc)["note_card_not_started"]; !strings.Contains(msg, "still backlog") || !strings.Contains(msg, "actor:someone-else") {
		t.Fatalf("warning must say the card has not started and who owns it: %v", doc["warnings"])
	}
}

func TestPlainNoteDoesNotStartACard(t *testing.T) {
	rec := &noteRecorder{column: "backlog"}
	doc, exit := runNoteOn(t, rec, "card:x", "Thinking about scope")
	if exit != 0 || asMap(doc["result"])["started"] != nil || len(rec.writes) != 0 {
		t.Fatalf("a plain note is not a start: exit=%d %v %v", exit, doc, rec.writes)
	}
}

// A busy workspace writer answers 503; the note retries with the same
// request_key inside the client timeout instead of failing or duplicating.
func TestNoteRetriesABusyWriterWithTheSameKey(t *testing.T) {
	rec := &noteRecorder{busy: 1}
	doc, exit := runNoteOn(t, rec, "card:x", "Progress", "--next", "1d", "next step")
	if exit != 0 {
		t.Fatalf("exit=%d %v", exit, doc)
	}
	if len(rec.keys) != 2 || rec.keys[0] == "" || rec.keys[0] != rec.keys[1] {
		t.Fatalf("retry must resend the same request_key: %v", rec.keys)
	}
}

// When the note cannot be confirmed in time the command says rerunning is
// safe, and the rerun resends the stored note with the same request_key.
func TestUnconfirmedNoteRerunReusesItsRequestKey(t *testing.T) {
	rec := &noteRecorder{busy: 100}
	server := noteServer(t, rec)
	t.Cleanup(server.Close)
	a, out := dailyTestApp(t, server.URL)
	if exit := a.Run([]string{"--json", "--as", "worker", "--timeout", "2s", "note", "card:x", "Progress", "--next", "1d", "next step"}); exit == 0 {
		t.Fatalf("a note the server never confirmed must fail: %s", out.String())
	}
	failed := dailyJSON(t, out)
	details := asMap(asMap(failed["error"])["details"])
	if details["safe_to_rerun"] != true || !strings.HasPrefix(anyString(details["request_key"]), "note:") {
		t.Fatalf("failure must say rerunning is safe: %v", failed)
	}
	first := anyString(details["request_key"])
	rec.mu.Lock()
	rec.busy, rec.keys = 0, nil
	rec.mu.Unlock()
	out.Reset()
	if exit := a.Run([]string{"--json", "--as", "worker", "note", "card:x", "Progress", "--next", "1d", "next step"}); exit != 0 {
		t.Fatalf("rerun exit=%d %s", exit, out.String())
	}
	rerun := dailyJSON(t, out)
	if len(rec.keys) != 1 || rec.keys[0] != first || asMap(rerun["result"])["resumed"] != true || !strings.Contains(fmt.Sprint(rerun["warnings"]), "note_resumed") {
		t.Fatalf("rerun must resend the stored note with key %s and warn note_resumed, sent %v: %v", first, rec.keys, rerun)
	}
	out.Reset()
	rec.mu.Lock()
	rec.keys = nil
	rec.mu.Unlock()
	if exit := a.Run([]string{"--json", "--as", "worker", "note", "card:x", "Progress", "--next", "1d", "next step"}); exit != 0 || len(rec.keys) != 1 || rec.keys[0] == first {
		t.Fatalf("after confirmation the same text is a new note with a new key: exit=%d keys=%v", exit, rec.keys)
	}
}

// A note whose retry key cannot be stored is not sent: an unconfirmed send
// could never be retried safely.
func TestNoteIsNotSentWhenItsKeyCannotBeStored(t *testing.T) {
	rec := &noteRecorder{}
	server := noteServer(t, rec)
	t.Cleanup(server.Close)
	a, out := dailyTestApp(t, server.URL)
	home, _ := a.UserHomeDir()
	// A file where the pending-notes directory belongs makes every save fail.
	if err := os.WriteFile(filepath.Join(home, ".config", "anx", "pending-notes"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if exit := a.Run([]string{"--json", "--as", "worker", "note", "card:x", "Progress", "--next", "1d", "next step"}); exit == 0 {
		t.Fatalf("a note with no stored key must not be sent: %s", out.String())
	}
	if code := anyString(asMap(dailyJSON(t, out)["error"])["code"]); code != "note_key_unsaved" || len(rec.keys) != 0 {
		t.Fatalf("code=%s sent=%v", code, rec.keys)
	}
}

// An unconfirmed note stays resendable with its own key however old it is;
// a rerun days later must not mint a new key and post a duplicate.
func TestOldUnconfirmedNoteKeepsItsKey(t *testing.T) {
	rec := &noteRecorder{busy: 100}
	server := noteServer(t, rec)
	t.Cleanup(server.Close)
	a, out := dailyTestApp(t, server.URL)
	start := time.Now()
	a.now = func() time.Time { return start }
	if exit := a.Run([]string{"--json", "--as", "worker", "--timeout", "2s", "note", "card:x", "Progress"}); exit == 0 {
		t.Fatalf("unconfirmed note must fail: %s", out.String())
	}
	first := anyString(asMap(asMap(dailyJSON(t, out)["error"])["details"])["request_key"])
	rec.mu.Lock()
	rec.busy, rec.keys = 0, nil
	rec.mu.Unlock()
	a.now = func() time.Time { return start.Add(72 * time.Hour) }
	out.Reset()
	if exit := a.Run([]string{"--json", "--as", "worker", "note", "card:x", "Progress"}); exit != 0 {
		t.Fatalf("rerun exit=%d %s", exit, out.String())
	}
	result := asMap(dailyJSON(t, out)["result"])
	if len(rec.keys) != 1 || rec.keys[0] != first || result["first_attempted_at"] != start.UTC().Format(time.RFC3339) {
		t.Fatalf("three days later the rerun must resend key %s: sent %v result %v", first, rec.keys, result)
	}
}

// A corrupt or unreadable pending record is not "no earlier send": replacing
// it with a new key could post the note twice, so the note is refused.
func TestCorruptPendingNoteRecordRefusesTheNote(t *testing.T) {
	rec := &noteRecorder{}
	server := noteServer(t, rec)
	t.Cleanup(server.Close)
	a, out := dailyTestApp(t, server.URL)
	home, _ := a.UserHomeDir()
	root := filepath.Join(home, ".config", "anx", "pending-notes")
	// Create the store with one real unconfirmed send, then corrupt it.
	rec.busy = 100
	if exit := a.Run([]string{"--json", "--as", "worker", "--timeout", "2s", "note", "card:x", "Progress"}); exit == 0 {
		t.Fatalf("unconfirmed note must fail: %s", out.String())
	}
	records, _ := filepath.Glob(filepath.Join(root, "*", "*.json"))
	if len(records) != 1 {
		t.Fatalf("one pending record expected: %v", records)
	}
	if err := os.WriteFile(records[0], []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec.mu.Lock()
	rec.busy, rec.keys = 0, nil
	rec.mu.Unlock()
	out.Reset()
	if exit := a.Run([]string{"--json", "--as", "worker", "note", "card:x", "Progress"}); exit == 0 || len(rec.keys) != 0 {
		t.Fatalf("a corrupt record must refuse the note: exit=%d sent=%v %s", exit, rec.keys, out.String())
	}
	if code := anyString(asMap(dailyJSON(t, out)["error"])["code"]); code != "note_key_unsaved" {
		t.Fatalf("code=%s", code)
	}
}

// Two identical invocations racing claim one key: the loser resends the
// winner's record instead of minting its own.
func TestPendingNoteClaimIsExclusive(t *testing.T) {
	store := &pendingNoteStore{dir: t.TempDir()}
	first := pendingNote{Key: "note:first", Body: map[string]any{"request_key": "note:first"}, Created: time.Now()}
	if err := store.claim("fp", first); err != nil {
		t.Fatal(err)
	}
	if err := store.claim("fp", pendingNote{Key: "note:second", Body: map[string]any{"request_key": "note:second"}, Created: time.Now()}); !errors.Is(err, errClaimed) {
		t.Fatalf("second claim must lose: %v", err)
	}
	got, ok, err := store.load("fp")
	if err != nil || !ok || got.Key != "note:first" {
		t.Fatalf("the winner's record must stand: %v %v %v", got, ok, err)
	}
	if err := store.clear("fp"); err != nil {
		t.Fatal(err)
	}
	if err := store.clear("fp"); err != nil {
		t.Fatalf("clearing a missing record is fine: %v", err)
	}
}

// A process that lost the claim and finds the record gone must not mint a
// new key: the winner already sent this exact note.
func TestLostClaimAfterWinnerSettledDoesNotResend(t *testing.T) {
	rec := &noteRecorder{}
	server := noteServer(t, rec)
	t.Cleanup(server.Close)
	a, out := dailyTestApp(t, server.URL)
	home, _ := a.UserHomeDir()
	// Simulate the interleaving: every claim loses (a record appears), and
	// the record is gone again by the reload.
	claims := 0
	pendingClaimHook = func(path string) {
		claims++
		_ = os.WriteFile(path, []byte(`{"request_key":"note:winner","body":{"request_key":"note:winner"}}`), 0o600)
	}
	pendingLoadHook = func(path string) {
		if claims > 0 {
			_ = os.Remove(path)
		}
	}
	t.Cleanup(func() { pendingClaimHook, pendingLoadHook = nil, nil })
	_ = home
	if exit := a.Run([]string{"--json", "--as", "worker", "note", "card:x", "Progress"}); exit == 0 || len(rec.keys) != 0 {
		t.Fatalf("must not send after losing a settled claim: exit=%d sent=%v %s", exit, rec.keys, out.String())
	}
	if code := anyString(asMap(dailyJSON(t, out)["error"])["code"]); code != "note_sent_concurrently" {
		t.Fatalf("code=%s", code)
	}
}
