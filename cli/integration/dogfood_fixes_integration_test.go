//go:build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Real binaries and a fresh local core; all config and hook settings live in
// the harness's temporary HOME. No service installation or real Git writes.
func TestDogfoodPromiseCreateAndNoCardHookAgainstCore(t *testing.T) {
	h := newPasskeyLiveCoreHarness(t)
	h.enrollHost(t, "worker")
	input := map[string]any{"title": "Verification initiative", "handle": "verify-dogfood", "plan": map[string]any{"steps": []any{map[string]any{"id": "verify", "title": "Verify regression", "after": []any{}}}}, "cadence": map[string]any{"every_seconds": 86400}, "goal": map[string]any{"metric": "score", "direction": "up", "target": 90}}
	created := h.runCLIExpectOK(t, "worker", input, "work", "create", "--from-file", "-")
	ref := mustStringPath(t, created.Payload, "result.work.ref")
	read := h.runCLIExpectOK(t, "worker", nil, "work", "get", ref)
	for _, key := range []string{"plan", "cadence", "goal"} {
		if value, ok := getPathValue(read.Payload, "result.work."+key); !ok || value == nil {
			t.Fatalf("%s missing on read: %s", key, read.Stdout)
		}
	}
	plan := h.runCLIExpectOK(t, "worker", nil, "plan", "show", ref)
	if !strings.Contains(plan.Stdout, "Verify regression") {
		t.Fatal("plan not installed", plan.Stdout)
	}
	// Notes exercise ordinary work; a cadence card keeps its receipt promise.
	loopRef := ref
	ordinary := h.runCLIExpectOK(t, "worker", map[string]any{"title": "Verification checkpoint", "handle": "verify-checkpoint"}, "work", "create", "--from-file", "-")
	ref = mustStringPath(t, ordinary.Payload, "result.work.ref")
	note := h.runCLIExpectOK(t, "worker", nil, "note", ref, "Next checkpoint", "--next", "1d", "Verify result")
	if strings.Contains(note.Stdout, "note_promise_unowned") {
		t.Fatal("creating actor was not assigned by default", note.Stdout)
	}
	cleared := h.runCLIExpectOK(t, "worker", nil, "note", ref, "Evidence arrived", "--evidence", "https://example.test/proof")
	if strings.Contains(cleared.Stdout, "note_clears_promise") {
		t.Fatal("obsolete promise clear warning", cleared.Stdout)
	}
	h.runCLIExpectOK(t, "worker", nil, "note", ref, "Follow up", "--next", "1d", "Verify tomorrow")
	// Brief facts are folded asynchronously. Establish projection readiness
	// before testing the one-shot hook, rather than relying on CLI startup time.
	deadline := time.Now().Add(5 * time.Second)
	for {
		oriented := h.runCLIExpectOK(t, "worker", nil, "orient")
		ready, loopTracked := false, false
		if raw, ok := getPathValue(oriented.Payload, "result.loops"); ok {
			for _, value := range raw.([]any) {
				row := value.(map[string]any)
				promise, _ := row["promise"].(map[string]any)
				if row["ref"] == loopRef && row["state"] == "moving" && promise["source"] == "cadence" && promise["what"] == "awaiting first run" {
					loopTracked = true
				}
			}
		}
		if raw, ok := getPathValue(oriented.Payload, "result.initiatives"); ok {
			for _, value := range raw.([]any) {
				row := value.(map[string]any)
				promise, _ := row["promise"].(map[string]any)
				if row["ref"] == ref && promise["what"] == "Verify tomorrow" {
					ready = true
					break
				}
			}
		}
		if ready && loopTracked {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("brief did not separate cadence tracking and the note promise", oriented.Stdout)
		}
		time.Sleep(200 * time.Millisecond)
	}
	h.runCLIExpectOK(t, "worker", nil, "integrations", "enable", "context.inject", "--harness", "generic", "--scope", "user")
	hook := h.runCLI(t, "worker", map[string]any{"schema_version": 1, "cwd": h.homeDir, "session_id": "no-card"}, "hook", "session.start")
	var brief struct {
		Context string  `json:"context"`
		Card    *string `json:"card_ref"`
	}
	if err := json.Unmarshal([]byte(hook.Stdout), &brief); err != nil {
		t.Fatal(hook.Stdout, hook.Stderr, err)
	}
	if brief.Card != nil || !strings.Contains(brief.Context, "Verify tomorrow") || !strings.Contains(brief.Context, "Reload the anx-participant skill") || len(brief.Context) > 6<<10 {
		t.Fatal("missing no-card brief", hook.Stdout, hook.Stderr)
	}
	missing := h.runCLI(t, "worker", nil, "ask", "Approve?", "--subject-ref", "card:missing-in-workspace", "--situation", "A launch decision is waiting.", "--impact", "The team cannot finish until we choose.", "--why", "This is the quickest safe approach.", "--recommend", "Proceed", "--body", "This is ready for review.")
	if missing.ExitCode != 3 || !strings.Contains(missing.Stdout, "same workspace") || !strings.Contains(missing.Stdout, "--workspace") {
		t.Fatal("misleading subject error", missing.Stdout, missing.Stderr)
	}
}
