//go:build integration

package integration

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
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

// This exercises the compiled CLI against core. Until S5 lands, the fixture
// marks a legacy profile as derived so the presence endpoint accepts it.
func TestDailyLoopAgainstCore(t *testing.T) {
	t.Setenv("ANX_INTEGRATION_PORT", "8092")
	h := newPasskeyLiveCoreHarness(t)
	h.registerAgentBootstrap(t, "worker", "worker."+runToken())
	h.registerHumanPasskey(t, "operator", "S6 Operator", h.createInviteTokenKind(t, "worker", "human"))
	id := mustStringPath(t, h.runCLIExpectOK(t, "worker", nil, "auth", "whoami").Payload, "result.agent.id")
	sqlite, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Fatalf("sqlite3 is required for the derived-agent integration fixture: %v", err)
	}
	db := filepath.Join(h.workspace, "state.sqlite")
	if _, err := os.Stat(db); err != nil {
		t.Fatal(err)
	}
	stmt := fmt.Sprintf("UPDATE agents SET metadata_json=json_set(metadata_json,'$.identity_kind','derived','$.host_id','host-s6','$.host_slug','s6','$.name','worker') WHERE id='%s';", id)
	if out, err := exec.Command(sqlite, db, stmt).CombinedOutput(); err != nil {
		t.Fatalf("seed derived identity: %v %s", err, out)
	}
	board := h.runCLIExpectOK(t, "worker", map[string]any{"board": map[string]any{"title": "S6 daily loop", "document_refs": []any{}, "pinned_refs": []any{}, "provenance": map[string]any{"sources": []any{"inferred"}}}}, "boards", "create")
	boardRef := mustStringPath(t, board.Payload, "result.board.ref")
	created := h.runCLIExpectOK(t, "worker", map[string]any{"board_ref": boardRef, "title": "S6 task"}, "work", "create", "--from-file", "-")
	ref := mustStringPath(t, created.Payload, "result.work.ref")
	initial := h.runCLIExpectOK(t, "worker", nil, "orient")
	if got := mustIntPath(t, initial.Payload, "result.my_work_matched"); got != 0 {
		t.Fatalf("unassigned work shown as mine: %s", initial.Stdout)
	}
	h.runCLIExpectOK(t, "worker", nil, "work", "start", ref)
	h.runCLIExpectOK(t, "worker", nil, "work", "note", "Built core path")
	oriented := h.runCLIExpectOK(t, "worker", nil, "orient")
	if got := mustIntPath(t, oriented.Payload, "result.my_work_matched"); got != 1 {
		t.Fatalf("work absent: %s", oriented.Stdout)
	}
	blocked := h.runCLIExpectOK(t, "worker", nil, "work", "block", "Need operator answer", "--ask", "--recommend", "Proceed")
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
	if raw, ok := getPathValue(afterAnswer.Payload, "result.my_asks_and_answers"); !ok || len(raw.([]any)) == 0 {
		t.Fatalf("orient omitted answered ask: %s", afterAnswer.Stdout)
	}
	ask := h.runCLIExpectOK(t, "worker", nil, "ask", "Ship now?", "--recommend", "Yes", "--alt", "Wait")
	if got := mustStringPath(t, ask.Payload, "result.ask_id"); got == "" {
		t.Fatal("ask id missing")
	}
	reviewFile := filepath.Join(t.TempDir(), "review.md")
	if err := os.WriteFile(reviewFile, []byte(fmt.Sprintf("---\ntitle: Review result\nsubject_ref: %s\nrecommended_response: Approve\n---\nInspect the result.\n", ref)), 0o600); err != nil {
		t.Fatal(err)
	}
	h.runCLIExpectOK(t, "worker", nil, "review", "--from-file", reviewFile)
	escalated := h.runCLIExpectOK(t, "worker", nil, "escalate", "Urgent decision", "--subject-ref", ref, "--recommend", "Proceed")
	escalateID := mustStringPath(t, escalated.Payload, "result.ask_id")
	h.runCLIExpectOK(t, "operator", map[string]any{"inbox_item_id": findAskInboxID(t, h, escalateID), "response_text": "No, wait", "outcome": "rejected"}, "debug", "inbox", "respond", "--from-file", "-")
	rejected := h.runCLI(t, "worker", nil, "await", escalateID, "--timeout", "1s")
	if rejected.ExitCode != 9 {
		t.Fatalf("reject exit=%d: %s", rejected.ExitCode, rejected.Stdout)
	}
	h.runCLIExpectOK(t, "worker", nil, "work", "done", "--evidence", "https://example.test/receipt")
	final := h.runCLIExpectOK(t, "worker", nil, "orient")
	if refValue, _ := getPathValue(final.Payload, "result.me.current_card_ref"); refValue != nil && refValue != "" {
		t.Fatalf("presence not cleared: %s", final.Stdout)
	}
}
