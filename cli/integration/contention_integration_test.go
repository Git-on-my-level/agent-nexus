//go:build integration

package integration

import (
	"fmt"
	"testing"
)

// Two independent core processes cover host CPU contention; two concurrent
// agents per core cover SQLite writer contention. Each runs 20 block/start/await
// iterations. Separate boards avoid deliberate optimistic-token conflicts.
func TestConcurrentMovesAndAnsweredAwaitAgainstCore(t *testing.T) {
	for core := 0; core < 2; core++ {
		t.Run(fmt.Sprintf("core-%d", core), func(t *testing.T) {
			t.Parallel()
			h := newPasskeyLiveCoreHarness(t)
			for agent := 0; agent < 2; agent++ {
				name := fmt.Sprintf("writer-%d", agent)
				h.enrollHost(t, name)
				board := h.runCLIExpectOK(t, name, map[string]any{"board": map[string]any{"title": name}}, "boards", "create")
				boardRef := mustStringPath(t, board.Payload, "result.board.ref")
				work := h.runCLIExpectOK(t, name, map[string]any{"board_ref": boardRef, "title": name}, "work", "create", "--from-file", "-")
				ref := mustStringPath(t, work.Payload, "result.work.ref")
				ask := h.runCLIExpectOK(t, name, nil, "ask", "Proceed with concurrent writes?", "--subject-ref", ref, "--recommend", "Proceed", "--force", "--reason", "Local contention fixture")
				askID := mustStringPath(t, ask.Payload, "result.ask_id")
				inbox := h.runCLIExpectOK(t, name, nil, "debug", "inbox", "list")
				items, _ := getPathValue(inbox.Payload, "result.items")
				found := false
				for _, raw := range items.([]any) {
					item := raw.(map[string]any)
					if item["request_event_ref"] == askID {
						h.postCore(t, "/inbox/"+item["id"].(string)+"/respond", h.adminToken, map[string]any{"inbox_item_id": item["id"], "response_text": "Proceed", "outcome": "answered"})
						found = true
					}
				}
				if !found {
					t.Fatalf("missing ask: %s", inbox.Stdout)
				}
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					for iteration := 0; iteration < 20; iteration++ {
						h.runCLIExpectOK(t, name, nil, "work", "start", ref)
						h.runCLIExpectOK(t, name, nil, "work", "block", "Temporary fixture dependency", ref)
						h.runCLIExpectOK(t, name, nil, "work", "start", ref)
						out := h.runCLIExpectOK(t, name, nil, "await", askID, "--timeout", "2s")
						if got := mustStringPath(t, out.Payload, "result.answer"); got != "Proceed" {
							t.Fatalf("iteration=%d answer=%q", iteration, got)
						}
					}
					t.Log("completed 20 iterations: 60 moves and 20 durable terminal awaits")
				})
			}
		})
	}
}
