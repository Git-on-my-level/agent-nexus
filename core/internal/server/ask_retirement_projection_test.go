package server

import (
	"agent-nexus-core/internal/primitives"
	"testing"
)

func TestRetirementAuditProjectionUsesExactIdentity(t *testing.T) {
	retired := func(id, ref string) map[string]any {
		return map[string]any{"id": id, "ref": ref, "type": primitives.AskRetiredEventType, "payload": map[string]any{"request_event_id": "ask"}}
	}
	reopened := map[string]any{"type": primitives.AskReopenedEventType, "payload": map[string]any{"request_event_id": "ask", "retirement_event_ref": "event:first-retirement"}}
	first := retired("retire-uuid", "event:first-retirement")
	for _, events := range [][]map[string]any{{first, reopened}, {reopened, first}} {
		if _, closed := decidedInboxItemIDs(events)["event:ask"]; closed {
			t.Fatal("public-handle undo did not reopen")
		}
	}
	later := retired("later-uuid", "event:later-retirement")
	for _, events := range [][]map[string]any{{first, reopened, later}, {later, reopened, first}} {
		if _, closed := decidedInboxItemIDs(events)["event:ask"]; !closed {
			t.Fatal("earlier undo reopened a later retirement")
		}
	}
	withdrawn := map[string]any{"type": humanAttentionWithdrawnEventType, "payload": map[string]any{"request_event_id": "ask"}}
	if _, closed := decidedInboxItemIDs([]map[string]any{withdrawn, first, reopened})["event:ask"]; !closed {
		t.Fatal("ordinary withdrawal was undone")
	}
}
