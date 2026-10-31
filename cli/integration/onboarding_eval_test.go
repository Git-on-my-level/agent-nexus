//go:build integration

package integration

import (
	"fmt"
	"strings"
	"testing"
)

// A naive agent following only what the CLI teaches: it files decisions, retries
// after a timeout, posts evidence with no thought for the promise, and records a
// chat ruling. Scored from workspace reads, so it measures what the human sees in
// Watch and Decide rather than what the agent believes it did.
func TestNaiveAgentLeavesAReadableWorkspace(t *testing.T) {
	h := newPasskeyLiveCoreHarness(t)
	h.enrollHost(t, "worker")
	h.registerHumanPasskey(t, "operator", "Eval Operator", h.createHumanInviteToken(t))
	board := h.runCLIExpectOK(t, "worker", map[string]any{"board": map[string]any{"title": "Eval", "document_refs": []any{}, "pinned_refs": []any{}, "provenance": map[string]any{"sources": []any{"inferred"}}}}, "boards", "create")
	created := h.runCLIExpectOK(t, "worker", map[string]any{"board_ref": mustStringPath(t, board.Payload, "result.board.ref"), "title": "Eval outcome", "initiative": true}, "work", "create", "--from-file", "-")
	ref := mustStringPath(t, created.Payload, "result.work.ref")
	h.runCLIExpectOK(t, "worker", nil, "work", "start", ref)

	// A note without a promise is told how to make one, and the first promise holds.
	h.runCLIExpectOK(t, "worker", nil, "note", ref, "Started the rollout", "--next", "1d", "finish the rollout")
	kept := h.runCLIExpectOK(t, "worker", nil, "note", ref, "Half the fleet is done", "--evidence", "https://example.test/half")
	if !strings.Contains(kept.Stdout, "note_promise_kept") {
		t.Fatalf("evidence note did not explain the kept promise: %s", kept.Stdout)
	}
	card := h.runCLIExpectOK(t, "worker", nil, "cards", "get", ref)
	if got := mustStringPath(t, card.Payload, "result.card.plan_health.promise.what"); got != "finish the rollout" {
		t.Fatalf("promise did not survive evidence: %s", card.Stdout)
	}

	// Watch shows what the note said.
	waitForInitiativeNote(t, h, ref)
	if brief := h.runCLIExpectOK(t, "operator", nil, "orient"); !strings.Contains(brief.Stdout, "Half the fleet is done") {
		t.Fatalf("Watch has no note excerpt: %s", brief.Stdout)
	}

	// A chat ruling lands on the card and the next asker hears about it.
	ruling := "Roll out overnight"
	h.runCLIExpectOK(t, "worker", nil, "note", ref, ruling, "--decided-by", "Dana")

	ask := []string{"ask", "Ship the rollout to everyone?", "--subject-ref", ref,
		"--situation", "Half the fleet runs the new build with no errors in a day, which is what changed since last week.",
		"--impact", "The other half keeps the slower build until someone chooses, and the choice rests on that one clean day.",
		"--recommend", "Ship it", "--why", "One clean day and a one-line rollback", "--by", "2d", "--delivery", "await"}
	first := h.runCLIExpectOK(t, "worker", nil, ask...)
	askID := mustStringPath(t, first.Payload, "result.ask_id")
	if !strings.Contains(first.Stdout, "ask_prior_ruling") || !strings.Contains(first.Stdout, ruling) {
		t.Fatalf("asker was not told about the recorded ruling: %s", first.Stdout)
	}

	// A retry after a timeout files nothing new.
	retry := h.runCLIExpectOK(t, "worker", nil, ask...)
	if got := mustStringPath(t, retry.Payload, "result.ask_id"); got != askID || !strings.Contains(retry.Stdout, "ask_already_open") {
		t.Fatalf("retry filed a second ask: %s", retry.Stdout)
	}
	items := h.runCLIExpectOK(t, "operator", nil, "debug", "inbox", "list")
	raw, _ := getPathValue(items.Payload, "result.items")
	matching := 0
	for _, item := range raw.([]any) {
		row := item.(map[string]any)
		if row["request_event_ref"] == askID {
			matching++
		}
		if strings.Contains(fmt.Sprint(row["title"]), "Ship the rollout") && row["request_event_ref"] != askID {
			t.Fatalf("duplicate inbox item: %v", row)
		}
	}
	if matching != 1 {
		t.Fatalf("inbox items for the ask=%d: %s", matching, items.Stdout)
	}

	// Answered stays answered, and the agent can read the answer from a repeat.
	h.runCLIExpectOK(t, "operator", map[string]any{"inbox_item_id": findAskInboxID(t, h, askID), "response_text": "Ship it", "outcome": "answered"}, "debug", "inbox", "respond", "--from-file", "-")
	repeat := h.runCLIExpectOK(t, "worker", nil, ask...)
	if mustStringPath(t, repeat.Payload, "result.status") != "answered" || mustStringPath(t, repeat.Payload, "result.answer") != "Ship it" {
		t.Fatalf("repeat after answer: %s", repeat.Stdout)
	}
	again := h.runCLIExpectOK(t, "worker", nil, append(ask, "--again")...)
	if mustStringPath(t, again.Payload, "result.ask_id") == askID {
		t.Fatalf("--again reused the old ask: %s", again.Stdout)
	}

	// Done with evidence ends the card cleanly.
	done := h.runCLIExpectOK(t, "worker", nil, "done", ref, "--evidence", "https://example.test/all")
	if got := mustStringPath(t, done.Payload, "result.phase"); got != "done" {
		t.Fatalf("done: %s", done.Stdout)
	}
}
