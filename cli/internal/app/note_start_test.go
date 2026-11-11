package app

import (
	"net/http/httptest"
	"strings"
	"testing"
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
	if len(rec.keys) != 1 || rec.keys[0] != first || asMap(dailyJSON(t, out)["result"])["resumed"] != true {
		t.Fatalf("rerun must resend the stored note with key %s, sent %v", first, rec.keys)
	}
	out.Reset()
	rec.mu.Lock()
	rec.keys = nil
	rec.mu.Unlock()
	if exit := a.Run([]string{"--json", "--as", "worker", "note", "card:x", "Progress", "--next", "1d", "next step"}); exit != 0 || len(rec.keys) != 1 || rec.keys[0] == first {
		t.Fatalf("after confirmation the same text is a new note with a new key: exit=%d keys=%v", exit, rec.keys)
	}
}
