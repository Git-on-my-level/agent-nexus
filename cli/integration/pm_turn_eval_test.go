//go:build integration

package integration

import (
	"fmt"
	"testing"
)

// Five-question deterministic stub runner against a seeded real core. The stub
// chooses one documented tool per question; this measures the scripted policy,
// not LLM tool choice or live-runner latency. Core validates all payloads/dedupe.
func TestPMTurnEval(t *testing.T) {
	h := newPasskeyLiveCoreHarness(t)
	h.enrollHost(t, "pm")
	h.selectPMAgent(t, "pm")
	connectPMForIntegration(t, h, "pm")
	h.registerHumanPasskey(t, "reader", "Eval reader", h.createHumanInviteToken(t))
	card := h.runCLIExpectOK(t, "pm", map[string]any{"title": "Eval card"}, "work", "create", "--from-file", "-")
	ref := mustStringPath(t, card.Payload, "result.work.ref")
	evidence := h.runCLIExpectOK(t, "pm", map[string]any{"event": map[string]any{"type": "message_posted", "summary": "Acceptance evidence", "provenance": map[string]any{"sources": []string{"inferred"}}, "refs": []string{ref}, "payload": map[string]any{"text": "Acceptance evidence"}}}, "debug", "events", "create", "--from-file", "-")
	evidenceRef := mustStringPath(t, evidence.Payload, "result.event.ref")
	conversation := h.runCLIExpectOK(t, "reader", map[string]any{"request_key": "eval", "title": "Eval", "work_ref": ref}, "pm", "conversations", "create", "--from-file", "-")
	conversationID := mustStringPath(t, conversation.Payload, "result.id")
	doneID := ""
	for i, question := range []string{"What needs my decision?", "Summarize card X", "Mark X done", "Add a note to X", "Mark X done"} {
		t.Run(fmt.Sprint(i+1), func(t *testing.T) {
			turn := h.runCLIExpectOK(t, "reader", map[string]any{"request_key": fmt.Sprint(i), "text": question}, "pm", "conversations", "message", conversationID, "--from-file", "-")
			turnID := mustStringPath(t, turn.Payload, "result.id")
			claimed := h.runCLIExpectOK(t, "pm", nil, "pm", "turns", "claim")
			token := mustStringPath(t, claimed.Payload, "result.lease_token")
			t.Setenv("ANX_PM_TURN_ID", turnID)
			t.Setenv("ANX_PM_LEASE_TOKEN", token)
			calls := 1
			httpCalls := 1
			switch i {
			case 0:
				got := h.runCLIExpectOK(t, "pm", nil, "pm", "context")
				if evalFirstItem(t, got.Payload)["title"] != "Eval card" {
					t.Fatal(got.Stdout)
				}
			case 1:
				got := h.runCLIExpectOK(t, "pm", nil, "pm", "card", ref)
				if evalFirstItem(t, got.Payload)["ref"] != ref {
					t.Fatal(got.Stdout)
				}
			case 2, 4:
				got := h.runCLIExpectOK(t, "pm", nil, "pm", "propose", ref, "--status", "done", "--why", "Acceptance evidence is available", "--evidence", evidenceRef)
				httpCalls = 2
				if mustStringPath(t, got.Payload, "result.payload.status") != "done" || evalResolutionRef(t, got.Payload) != evidenceRef {
					t.Fatal(got.Stdout)
				}
				id := mustStringPath(t, got.Payload, "result.id")
				if i == 2 {
					doneID = id
				} else if doneID != id {
					t.Fatalf("duplicate decisions %s != %s", doneID, id)
				}
			case 3:
				got := h.runCLIExpectOK(t, "pm", nil, "pm", "propose", ref, "--note", "Review with the owner", "--why", "Keep review context on the card", "--evidence", evidenceRef)
				httpCalls = 2
				if mustStringPath(t, got.Payload, "result.payload.note") != "Review with the owner" || mustStringPath(t, got.Payload, "result.scope") != "note" || fmt.Sprint(got.Payload["result"].(map[string]any)["payload"].(map[string]any)["evidence_refs"]) != fmt.Sprint([]string{evidenceRef}) {
					t.Fatal(got.Stdout)
				}
			}
			h.runCLIExpectOK(t, "pm", map[string]any{"lease_token": token, "text": "Stub answer", "evidence_refs": []string{ref}}, "pm", "turns", "complete", turnID, "--from-file", "-")
			t.Logf("question=%q tool_calls=%d http_calls=%d payload=correct", question, calls, httpCalls)
		})
	}
	decisions := h.runCLIExpectOK(t, "reader", nil, "pm", "decisions", "list", "--limit", "20")
	items, _ := getPathValue(decisions.Payload, "result.items")
	if len(items.([]any)) != 2 {
		t.Fatalf("want one done and one note: %s", decisions.Stdout)
	}
	t.Log("dedupe=one awaiting done decision across separate turns")
}

func evalFirstItem(t *testing.T, p map[string]any) map[string]any {
	t.Helper()
	items, _ := getPathValue(p, "result.items")
	rows, ok := items.([]any)
	if !ok || len(rows) != 1 {
		t.Fatal(p)
	}
	return rows[0].(map[string]any)
}
func evalResolutionRef(t *testing.T, p map[string]any) string {
	t.Helper()
	items, _ := getPathValue(p, "result.payload.resolution_refs")
	rows, ok := items.([]any)
	if !ok || len(rows) != 1 {
		t.Fatal(p)
	}
	return rows[0].(string)
}
