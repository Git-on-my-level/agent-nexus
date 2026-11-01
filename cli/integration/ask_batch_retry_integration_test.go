//go:build integration

package integration

import (
	"fmt"
	"strings"
	"testing"
)

// Against real core, because a retry only finds a batch if batch members are stored
// in the shape the prior-ask lookup reads.
func TestAskBatchRetryFindsTheFiledBatch(t *testing.T) {
	h := newPasskeyLiveCoreHarness(t)
	h.enrollHost(t, "worker")
	created := h.runCLIExpectOK(t, "worker", map[string]any{"title": "Batch retry scenario", "phase": "ready"}, "work", "create", "--from-file", "-", "--handle", "batch-retry")
	card := mustStringPath(t, created.Payload, "result.work.ref")

	asks := []any{}
	for _, title := range []string{"Ship the flag now?", "Roll back the flag first?"} {
		asks = append(asks, map[string]any{
			"title":          title,
			"subject_ref":    card,
			"situation":      "A launch decision is waiting.",
			"impact":         "The team cannot finish until we choose.",
			"recommendation": map[string]any{"action": "Accept", "why": "This is the quickest safe approach."},
			"recommend":      "Accept",
			"alts":           []any{"Wait"},
		})
	}
	file := map[string]any{"asks": asks}
	ids := func(res string, payload map[string]any) [2]string {
		t.Helper()
		raw, _ := getPathValue(payload, "result.asks")
		rows, _ := raw.([]any)
		if len(rows) != 2 {
			t.Fatalf("expected both asks: %s", res)
		}
		var out [2]string
		for i, row := range rows {
			out[i] = fmt.Sprint(row.(map[string]any)["ask_id"])
		}
		return out
	}

	first := h.runCLIExpectOK(t, "worker", file, "ask", "batch", "--from-file", "-")
	filed := ids(first.Stdout, first.Payload)

	retry := h.runCLIExpectOK(t, "worker", file, "ask", "batch", "--from-file", "-")
	if got := ids(retry.Stdout, retry.Payload); got != filed {
		t.Fatalf("retry filed new asks: %v then %v", filed, got)
	}
	if got := mustStringPath(t, retry.Payload, "result.batch_id"); got != mustStringPath(t, first.Payload, "result.batch_id") || got == "" {
		t.Fatalf("retry lost the batch id: %s", retry.Stdout)
	}

	h.runCLIExpectOK(t, "worker", nil, "ask", "withdraw", filed[0], "--reason", "No longer needed")
	partial := h.runCLI(t, "worker", file, "ask", "batch", "--from-file", "-")
	if want := "asks[1] is " + filed[1]; partial.ExitCode == 0 || !strings.Contains(partial.Stdout, want) {
		t.Fatalf("partly repeated batch should name %s: %s", want, partial.Stdout)
	}
	h.runCLIExpectOK(t, "worker", nil, "ask", "withdraw", filed[1], "--reason", "No longer needed")

	again := h.runCLIExpectOK(t, "worker", file, "ask", "batch", "--from-file", "-")
	if got := ids(again.Stdout, again.Payload); got[0] == filed[0] || got[1] == filed[1] {
		t.Fatalf("withdrawn asks should be asked again as new asks: %v then %v", filed, got)
	}
}
