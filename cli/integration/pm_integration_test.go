//go:build integration

package integration

import (
	"fmt"
	"os/exec"
	"testing"
)

func TestUnifiedPMDecisionDurabilityAndApprovalBoundary(t *testing.T) {
	h := newLiveCoreHarness(t)
	h.registerAgentBootstrap(t, "worker", "worker."+runToken())
	board := h.runCLIExpectOK(t, "worker", map[string]any{"board": map[string]any{"title": "Synthetic PM approval test", "document_refs": []any{}, "pinned_refs": []any{}, "provenance": map[string]any{"sources": []any{"inferred"}}}}, "boards", "create")
	work := h.runCLIExpectOK(t, "worker", map[string]any{"board_ref": mustStringPath(t, board.Payload, "data.board.ref"), "title": "Synthetic PM commitment"}, "work", "create", "--from-file", "-")
	ref := mustStringPath(t, work.Payload, "data.work.ref")
	h.runCLIExpectOK(t, "worker", nil, "pm", "context", "--work-ref", ref, "--limit", "1")
	input := map[string]any{"request_key": "synthetic-instruction", "work_ref": ref, "instruction": `{"next_action":"Review synthetic evidence"}`, "scope": "work.annotate", "target_revision": fmt.Sprint(mustIntPath(t, work.Payload, "data.work.version"))}
	proposal := h.runCLIExpectOK(t, "worker", input, "pm", "decisions", "create", "--from-file", "-")
	decisionID := mustStringPath(t, proposal.Payload, "data.id")
	replay := h.runCLIExpectOK(t, "worker", input, "pm", "decisions", "create", "--from-file", "-")
	if mustStringPath(t, replay.Payload, "data.id") != decisionID {
		t.Fatal("instruction replay duplicated intent")
	}
	denied := h.runCLI(t, "worker", map[string]any{"revision": 1, "approve": true, "text": "Attempt agent approval"}, "pm", "decisions", "answer", decisionID, "--from-file", "-")
	if denied.ExitCode == 0 || mustStringPath(t, denied.Payload, "error.code") != "forbidden" {
		t.Fatalf("agent approval was not forbidden: %s", denied.Stdout)
	}
	premature := h.runCLI(t, "worker", nil, "pm", "decisions", "dispatch", decisionID)
	if premature.ExitCode == 0 {
		t.Fatalf("unapproved source handoff succeeded: %s", premature.Stdout)
	}
	// A hard restart cannot erase a pending instruction or invent an action receipt.
	restartCoreForWorkTest(t, h)
	loaded := h.runCLIExpectOK(t, "worker", nil, "pm", "decisions", "get", decisionID)
	if mustStringPath(t, loaded.Payload, "data.status") != "awaiting_answer" {
		t.Fatalf("restart lost pending decision: %s", loaded.Stdout)
	}
	actions := h.runCLIExpectOK(t, "worker", nil, "pm", "actions", "list", "--limit", "1")
	items, _ := getPathValue(actions.Payload, "data.items")
	if rows, ok := items.([]any); !ok || len(rows) != 0 {
		t.Fatalf("unapproved decision generated actions: %s", actions.Stdout)
	}
	conversation := h.runCLIExpectOK(t, "worker", map[string]any{"request_key": "synthetic-conversation", "title": "Synthetic PM", "work_ref": ref}, "pm", "conversations", "create", "--from-file", "-")
	conversationID := mustStringPath(t, conversation.Payload, "data.id")
	response := h.runCLI(t, "worker", map[string]any{"request_key": "no-provider", "text": "Review this synthetic commitment"}, "pm", "conversations", "message", conversationID, "--from-file", "-")
	if response.ExitCode == 0 || mustStringPath(t, response.Payload, "error.code") != "unavailable" {
		t.Fatalf("unconfigured provider must fail honestly: %s", response.Stdout)
	}
	h.runCLIExpectOK(t, "worker", nil, "pm", "conversations", "get", conversationID)
}

func TestUnifiedPMPaginationAcrossRestarts(t *testing.T) {
	h := newLiveCoreHarness(t)
	h.registerAgentBootstrap(t, "reader", "reader."+runToken())
	for i := 0; i < 3; i++ {
		h.runCLIExpectOK(t, "reader", map[string]any{"request_key": fmt.Sprint("conversation-", i), "title": fmt.Sprint("Synthetic conversation ", i)}, "pm", "conversations", "create", "--from-file", "-")
	}
	first := h.runCLIExpectOK(t, "reader", nil, "pm", "conversations", "list", "--limit", "1")
	raw, _ := getPathValue(first.Payload, "data.items")
	if rows, ok := raw.([]any); !ok || len(rows) != 1 {
		t.Fatalf("limit not honored: %s", first.Stdout)
	}
	cursor := mustStringPath(t, first.Payload, "data.next_cursor")
	restartCoreForWorkTest(t, h)
	second := h.runCLIExpectOK(t, "reader", nil, "pm", "conversations", "list", "--limit", "1", "--cursor", cursor)
	next, _ := getPathValue(second.Payload, "data.items")
	if fmt.Sprint(raw) == fmt.Sprint(next) {
		t.Fatalf("cursor replayed first page: %s", second.Stdout)
	}
	wrongKind := h.runCLI(t, "reader", nil, "pm", "decisions", "list", "--limit", "1", "--cursor", cursor)
	if wrongKind.ExitCode == 0 {
		t.Fatalf("conversation cursor accepted for decisions: %s", wrongKind.Stdout)
	}
	invite := h.createInviteToken(t, "reader")
	h.registerAgentInvite(t, "other", "other."+runToken(), invite)
	wrongPrincipal := h.runCLI(t, "other", nil, "pm", "conversations", "list", "--limit", "1", "--cursor", cursor)
	if wrongPrincipal.ExitCode == 0 {
		t.Fatalf("cursor crossed principal boundary: %s", wrongPrincipal.Stdout)
	}
}

func restartCoreForWorkTest(t *testing.T, h *liveCoreHarness) {
	t.Helper()
	old := h.server
	if err := old.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_, _ = old.Process.Wait()
	cmd := exec.Command(old.Args[0], old.Args[1:]...)
	cmd.Env = old.Env
	cmd.Dir = old.Dir
	cmd.Stdout = h.logFile
	cmd.Stderr = h.logFile
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	h.server = cmd
	waitForHealthy(t, h.baseURL, h.logPath)
}
