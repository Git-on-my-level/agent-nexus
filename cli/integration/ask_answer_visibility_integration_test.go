//go:build integration

package integration

import (
	"fmt"
	"strings"
	"testing"
)

// Regression for oss#389: an agent that filed asks without any delivery wiring
// must still see its own answered asks through `anx inbox list --status
// answered`, matching what `anx orient` reports under answered_for_you.
func TestInboxListAnsweredIncludesOwnAnsweredAsks(t *testing.T) {
	h := newPasskeyLiveCoreHarness(t)
	h.enrollHost(t, "worker")
	h.registerHumanPasskey(t, "operator", "S6 Operator", h.createHumanInviteToken(t))
	id := mustStringPath(t, h.runCLIExpectOK(t, "worker", nil, "auth", "whoami").Payload, "result.agent.id")
	stmt := fmt.Sprintf("UPDATE agents SET metadata_json=json_set(metadata_json,'$.identity_kind','derived','$.host_id','host-s6','$.host_slug','s6','$.name','worker') WHERE id='%s';", id)
	if out, err := runLegacySQLFixture(t, h, stmt); err != nil {
		t.Fatalf("seed derived identity: %v %s", err, out)
	}

	askIDs := map[string]bool{}
	for i := 1; i <= 5; i++ {
		ask := h.runCLIExpectOK(t, "worker", nil, "ask", fmt.Sprintf("Decision %d: ship now?", i), "--situation", "A launch decision is waiting.", "--impact", "The team cannot finish until we choose.", "--why", "This is the quickest safe approach.", "--recommend", "Yes", "--alt", "Wait")
		askID := mustStringPath(t, ask.Payload, "result.ask_id")
		if askID == "" {
			t.Fatalf("ask %d id missing: %s", i, ask.Stdout)
		}
		askIDs[askID] = true
	}

	for askID := range askIDs {
		h.runCLIExpectOK(t, "operator", map[string]any{
			"inbox_item_id": findAskInboxID(t, h, askID),
			"response_text": "Approved",
			"outcome":       "answered",
		}, "debug", "inbox", "respond", "--from-file", "-")
	}

	listed := h.runCLIExpectOK(t, "worker", nil, "inbox", "list", "--status", "answered")
	items, ok := getPathValue(listed.Payload, "result.items")
	if !ok {
		t.Fatalf("inbox list returned no items: %s", listed.Stdout)
	}
	seen := map[string]bool{}
	for _, raw := range items.([]any) {
		row := raw.(map[string]any)
		seen[fmt.Sprint(row["ask_id"])] = true
		if row["answer"] == nil {
			t.Fatalf("answered listing included an item without an answer: %#v", row)
		}
		if fmt.Sprint(row["delivery"]) != "batch" {
			t.Fatalf("answered ask lost its recorded delivery mode: %#v", row)
		}
	}
	for askID := range askIDs {
		if !seen[askID] {
			t.Fatalf("answered ask %s missing from anx inbox list --status answered: %s", askID, listed.Stdout)
		}
	}

	oriented := h.runCLIExpectOK(t, "worker", nil, "orient")
	if got := mustStringPath(t, oriented.Payload, "result.schema"); got != "brief.v1" {
		t.Fatalf("orient schema=%s: %s", got, oriented.Stdout)
	}
	ownAsks, ok := getPathValue(oriented.Payload, "result.answered_for_you")
	if !ok {
		t.Fatalf("orient returned no answered_for_you: %s", oriented.Stdout)
	}
	orientTitles := map[string]bool{}
	orientRefs := []string{}
	for _, raw := range ownAsks.([]any) {
		row := raw.(map[string]any)
		if row["answer"] == nil || strings.TrimSpace(fmt.Sprint(row["answer"])) == "" {
			continue
		}
		orientTitles[fmt.Sprint(row["title"])] = true
		orientRefs = append(orientRefs, fmt.Sprint(row["ref"]))
	}
	for i := 1; i <= 5; i++ {
		title := fmt.Sprintf("Decision %d: ship now?", i)
		if !orientTitles[title] {
			t.Fatalf("orient omitted answered ask %q: %s", title, oriented.Stdout)
		}
	}
	for askID := range askIDs {
		eventID := strings.TrimPrefix(askID, "event:")
		matched := false
		for _, ref := range orientRefs {
			if ref == askID || strings.Contains(ref, eventID) {
				matched = true
				break
			}
		}
		if !matched {
			t.Fatalf("orient disagreed with inbox list: ask %s has no answer under orient: %s", askID, oriented.Stdout)
		}
	}
}

// Two sessions share one derived agent. Each wait must see only its own ask,
// nothing it does may hide the other's answer, and the ref orient prints works.
func TestAwaitAnswersIsScopedToTheAskingSession(t *testing.T) {
	h := newPasskeyLiveCoreHarness(t)
	h.enrollHost(t, "worker")
	h.registerHumanPasskey(t, "operator", "Operator", h.createHumanInviteToken(t))
	workerID := mustStringPath(t, h.runCLIExpectOK(t, "worker", nil, "auth", "whoami").Payload, "result.agent.id")
	stmt := fmt.Sprintf("UPDATE agents SET metadata_json=json_set(metadata_json,'$.identity_kind','derived','$.host_id','host-s6','$.host_slug','s6','$.name','worker') WHERE id='%s';", workerID)
	if out, err := runLegacySQLFixture(t, h, stmt); err != nil {
		t.Fatalf("seed derived identity: %v %s", err, out)
	}
	session := func(id string) { t.Setenv("CLAUDE_CODE_SESSION_ID", id) }
	fileAsk := func(id, title string) string {
		t.Helper()
		session(id)
		ask := h.runCLIExpectOK(t, "worker", nil, "ask", title+": ship now?", "--situation", "A launch decision is waiting.", "--impact", "The team cannot finish until we choose.", "--why", "This is the quickest safe approach.", "--recommend", "Yes", "--alt", "Wait")
		return mustStringPath(t, ask.Payload, "result.ask_id")
	}
	askA := fileAsk("a", "A")
	fileAsk("b", "B")
	inboxA := findAskInboxID(t, h, askA)
	h.runCLIExpectOK(t, "operator", map[string]any{"inbox_item_id": inboxA, "response_text": "Approved", "outcome": "answered"}, "debug", "inbox", "respond", "--from-file", "-")

	session("b")
	timedOut := h.runCLI(t, "worker", nil, "await", "--answers", "--timeout", "2s")
	if timedOut.ExitCode != 8 {
		t.Fatalf("session b received session a's answer: exit=%d %s", timedOut.ExitCode, timedOut.Stdout)
	}
	refused := h.runCLI(t, "worker", nil, "inbox", "read", strings.TrimPrefix(inboxA, "inbox:"))
	if refused.ExitCode == 0 || !strings.Contains(refused.Stdout+refused.Stderr, "other_session_answer") {
		t.Fatalf("session b marked session a's answer read: exit=%d %s %s", refused.ExitCode, refused.Stdout, refused.Stderr)
	}

	session("a")
	delivered := h.runCLIExpectOK(t, "worker", nil, "await", "--answers", "--timeout", "20s", "--settle", "0")
	answers, _ := getPathValue(delivered.Payload, "result.answers")
	if rows, ok := answers.([]any); !ok || len(rows) != 1 || fmt.Sprint(rows[0].(map[string]any)["ask_id"]) != askA {
		t.Fatalf("session a must get exactly its own answer: %s", delivered.Stdout)
	}
	again := h.runCLI(t, "worker", nil, "await", "--answers", "--timeout", "2s")
	if again.ExitCode != 8 {
		t.Fatalf("re-armed wait fired again: exit=%d %s", again.ExitCode, again.Stdout)
	}
	listed := h.runCLIExpectOK(t, "worker", nil, "inbox", "list", "--unread")
	if !strings.Contains(listed.Stdout, askA) {
		t.Fatalf("a wait must not change read state: %s", listed.Stdout)
	}
	read := h.runCLIExpectOK(t, "worker", nil, "inbox", "read", strings.TrimPrefix(inboxA, "inbox:"))
	if got := mustStringPath(t, read.Payload, "result.ask_id"); got != askA {
		t.Fatalf("orient's ref must resolve to the ask: %s", read.Stdout)
	}
}
