//go:build integration

package integration

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
)

func TestUnifiedPMDecisionDurabilityAndApprovalBoundary(t *testing.T) {
	h := newPasskeyLiveCoreHarness(t)
	h.enrollHost(t, "worker")
	h.registerHumanPasskey(t, "maya", "Maya Chen", h.createHumanInviteToken(t))
	board := h.runCLIExpectOK(t, "worker", map[string]any{"board": map[string]any{"title": "Synthetic PM approval test", "document_refs": []any{}, "pinned_refs": []any{}, "provenance": map[string]any{"sources": []any{"inferred"}}}}, "boards", "create")
	work := h.runCLIExpectOK(t, "worker", map[string]any{"board_ref": mustStringPath(t, board.Payload, "result.board.ref"), "title": "Synthetic PM commitment"}, "work", "create", "--from-file", "-")
	ref := mustStringPath(t, work.Payload, "result.work.ref")
	h.runCLIExpectOK(t, "maya", nil, "pm", "context", "--work-ref", ref, "--limit", "1")
	input := map[string]any{"request_key": "synthetic-instruction", "work_ref": ref, "instruction": `{"next_action":"Review synthetic evidence"}`, "scope": "work.annotate", "target_revision": fmt.Sprint(mustIntPath(t, work.Payload, "result.work.version"))}
	proposal := h.runCLIExpectOK(t, "maya", input, "pm", "decisions", "create", "--from-file", "-")
	decisionID := mustStringPath(t, proposal.Payload, "result.id")
	replay := h.runCLIExpectOK(t, "maya", input, "pm", "decisions", "create", "--from-file", "-")
	if mustStringPath(t, replay.Payload, "result.id") != decisionID {
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
	if mustStringPath(t, loaded.Payload, "result.status") != "awaiting_answer" {
		t.Fatalf("restart lost pending decision: %s", loaded.Stdout)
	}
	actions := h.runCLIExpectOK(t, "worker", nil, "pm", "actions", "list", "--limit", "1")
	items, _ := getPathValue(actions.Payload, "result.items")
	if rows, ok := items.([]any); !ok || len(rows) != 0 {
		t.Fatalf("unapproved decision generated actions: %s", actions.Stdout)
	}
}

func restartCoreForWorkTest(t *testing.T, h *liveCoreHarness, extraEnv ...string) {
	t.Helper()
	old := h.server
	if err := old.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_, _ = old.Process.Wait()
	cmd := exec.Command(old.Args[0], old.Args[1:]...)
	cmd.Env = append(append([]string{}, old.Env...), extraEnv...)
	cmd.Dir = old.Dir
	cmd.Stdout = h.logFile
	cmd.Stderr = h.logFile
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	h.server = cmd
	waitForHealthy(t, h.baseURL, h.logPath)
}

func readFileOrEmpty(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(raw)
}
