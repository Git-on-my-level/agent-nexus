//go:build integration

package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func findAskInboxID(t *testing.T, h *liveCoreHarness, askID string) string {
	t.Helper()
	items := h.runCLIExpectOK(t, "operator", nil, "debug", "inbox", "list")
	raw, ok := getPathValue(items.Payload, "result.items")
	if !ok {
		t.Fatalf("inbox items missing: %s", items.Stdout)
	}
	for _, item := range raw.([]any) {
		row := item.(map[string]any)
		if row["request_event_ref"] == askID {
			return row["id"].(string)
		}
	}
	t.Fatalf("ask %s missing from inbox: %s", askID, items.Stdout)
	return ""
}

// This exercises the compiled CLI against core. The legacy identity fixture
// uses core's registered SQLite functions so ownership triggers run normally.
func TestDailyLoopAgainstCore(t *testing.T) {
	h := newPasskeyLiveCoreHarness(t)
	h.enrollHost(t, "worker")
	h.registerHumanPasskey(t, "operator", "S6 Operator", h.createHumanInviteToken(t))
	id := mustStringPath(t, h.runCLIExpectOK(t, "worker", nil, "auth", "whoami").Payload, "result.agent.id")
	stmt := fmt.Sprintf("UPDATE agents SET metadata_json=json_set(metadata_json,'$.identity_kind','derived','$.host_id','host-s6','$.host_slug','s6','$.name','worker') WHERE id='%s';", id)
	if out, err := runLegacySQLFixture(t, h, stmt); err != nil {
		t.Fatalf("seed derived identity: %v %s", err, out)
	}
	board := h.runCLIExpectOK(t, "worker", map[string]any{"board": map[string]any{"title": "S6 daily loop", "document_refs": []any{}, "pinned_refs": []any{}, "provenance": map[string]any{"sources": []any{"inferred"}}}}, "boards", "create")
	boardRef := mustStringPath(t, board.Payload, "result.board.ref")
	created := h.runCLIExpectOK(t, "worker", map[string]any{"board_ref": boardRef, "title": "S6 task"}, "work", "create", "--from-file", "-")
	ref := mustStringPath(t, created.Payload, "result.work.ref")
	initial := h.runCLIExpectOK(t, "worker", nil, "orient")
	if got := mustStringPath(t, initial.Payload, "result.schema"); got != "brief.v1" {
		t.Fatalf("orient schema=%s: %s", got, initial.Stdout)
	}
	if _, hasNote := initiativeProgress(initial.Payload, ref); hasNote {
		t.Fatalf("unnoted work already has a progress note: %s", initial.Stdout)
	}
	h.runCLIExpectOK(t, "worker", nil, "work", "start", ref)
	noted := h.runCLIExpectOK(t, "worker", nil, "note", ref, "Built core path")
	if got := mustStringPath(t, noted.Payload, "result.card_ref"); got != ref || mustStringPath(t, noted.Payload, "result.note") != "Built core path" {
		t.Fatalf("note result: %s", noted.Stdout)
	}
	waitForInitiativeNote(t, h, ref)
	blocked := h.runCLIExpectOK(t, "worker", nil, "work", "block", "Need operator answer", "--ask", "--situation", "A launch decision is waiting.", "--impact", "The team cannot finish until we choose.", "--why", "This is the quickest safe approach.", "--recommend", "Proceed")
	askID := mustStringPath(t, blocked.Payload, "result.ask_id")
	timed := h.runCLI(t, "worker", nil, "await", askID, "--timeout", "150ms")
	if timed.ExitCode != 8 {
		t.Fatalf("await timeout exit=%d: %s", timed.ExitCode, timed.Stdout)
	}
	responded := h.runCLIExpectOK(t, "operator", map[string]any{"inbox_item_id": findAskInboxID(t, h, askID), "response_text": "Proceed", "outcome": "answered"}, "debug", "inbox", "respond", "--from-file", "-")
	answered := h.runCLI(t, "worker", nil, "await", askID, "--timeout", "1s")
	if answered.ExitCode != 0 {
		events := h.runCLIExpectOK(t, "worker", nil, "debug", "events", "list", "--type", "human_attention_responded")
		t.Fatalf("await failed: %s; respond=%s; events=%s", answered.Stdout, responded.Stdout, events.Stdout)
	}
	if answer := mustStringPath(t, answered.Payload, "result.answer"); answer != "Proceed" {
		t.Fatalf("answer=%q", answer)
	}
	afterAnswer := h.runCLIExpectOK(t, "worker", nil, "orient")
	if !briefHasAnswer(t, afterAnswer.Payload, "Proceed") {
		t.Fatalf("orient omitted answered ask: %s", afterAnswer.Stdout)
	}
	ask := h.runCLIExpectOK(t, "worker", nil, "ask", "Ship now?", "--situation", "A launch decision is waiting.", "--impact", "The team cannot finish until we choose.", "--why", "This is the quickest safe approach.", "--recommend", "Yes", "--alt", "Wait")
	if got := mustStringPath(t, ask.Payload, "result.ask_id"); got == "" {
		t.Fatal("ask id missing")
	}
	reviewFile := filepath.Join(t.TempDir(), "review.md")
	if err := os.WriteFile(reviewFile, []byte(fmt.Sprintf("---\ntitle: Review result\nsubject_ref: %s\nsituation: A launch decision is waiting.\nimpact: The team cannot finish until we choose.\nrecommendation: {action: Proceed, why: This is the quickest safe approach.}\nrecommended_response: Approve\n---\nInspect the result.\n", ref)), 0o600); err != nil {
		t.Fatal(err)
	}
	h.runCLIExpectOK(t, "worker", nil, "review", "--from-file", reviewFile)
	escalated := h.runCLIExpectOK(t, "worker", nil, "escalate", "Urgent decision", "--subject-ref", ref, "--situation", "A launch decision is waiting.", "--impact", "The team cannot finish until we choose.", "--why", "This is the quickest safe approach.", "--recommend", "Proceed")
	escalateID := mustStringPath(t, escalated.Payload, "result.ask_id")
	h.runCLIExpectOK(t, "operator", map[string]any{"inbox_item_id": findAskInboxID(t, h, escalateID), "response_text": "No, wait", "outcome": "rejected"}, "debug", "inbox", "respond", "--from-file", "-")
	rejected := h.runCLI(t, "worker", nil, "await", escalateID, "--timeout", "1s")
	if rejected.ExitCode != 9 {
		t.Fatalf("reject exit=%d: %s", rejected.ExitCode, rejected.Stdout)
	}
	finished := h.runCLIExpectOK(t, "worker", nil, "done", ref, "--evidence", "https://example.test/receipt")
	if got := mustStringPath(t, finished.Payload, "result.phase"); got != "done" || mustStringPath(t, finished.Payload, "result.card_ref") != ref {
		t.Fatalf("done result: %s", finished.Stdout)
	}
	finalWork := h.runCLIExpectOK(t, "worker", nil, "work", "get", ref)
	if got := mustStringPath(t, finalWork.Payload, "result.work.phase"); got != "done" {
		t.Fatalf("work not done: %s", finalWork.Stdout)
	}
	final := h.runCLIExpectOK(t, "worker", nil, "orient")
	if refValue, _ := getPathValue(final.Payload, "result.me.current_card_ref"); refValue != nil && refValue != "" {
		t.Fatalf("presence not cleared: %s", final.Stdout)
	}
}

func initiativeProgress(payload map[string]any, ref string) (placement string, hasNote bool) {
	raw, ok := getPathValue(payload, "result.initiatives")
	if !ok {
		return "", false
	}
	for _, item := range raw.([]any) {
		row := item.(map[string]any)
		if fmt.Sprint(row["ref"]) != ref {
			continue
		}
		_, hasNote = row["note"].(map[string]any)
		return fmt.Sprint(row["placement"]), hasNote
	}
	return "", false
}

func waitForInitiativeNote(t *testing.T, h *liveCoreHarness, ref string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last string
	for {
		oriented := h.runCLIExpectOK(t, "worker", nil, "orient")
		placement, hasNote := initiativeProgress(oriented.Payload, ref)
		last = oriented.Stdout
		if hasNote && placement == "in_progress" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("initiative %s placement=%q has_note=%v: %s", ref, placement, hasNote, last)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func briefHasAnswer(t *testing.T, payload map[string]any, answer string) bool {
	t.Helper()
	raw, ok := getPathValue(payload, "result.answered_for_you")
	if !ok {
		return false
	}
	for _, item := range raw.([]any) {
		row := item.(map[string]any)
		if fmt.Sprint(row["answer"]) == answer {
			return true
		}
	}
	return false
}
