package primitives

import (
	"testing"
	"time"

	"agent-nexus-core/internal/plans"
)

func TestDisplayedNextActionFollowsTheNewerPromise(t *testing.T) {
	t.Parallel()
	noteAt := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	answerAt := noteAt.Add(-time.Hour).Format(time.RFC3339Nano)
	later := noteAt.Add(time.Hour).Format(time.RFC3339Nano)
	notes := []plans.PromiseNote{{EventID: "e1", At: noteAt}}
	promise := &plans.HealthPromise{Source: "note", SourceRef: "event:e1", What: "Check the loop ledger"}

	if got := displayedNextAction("Go left", answerAt, notes, promise); got != "Check the loop ledger" {
		t.Fatalf("newer note = %q", got)
	}
	if got := displayedNextAction("Go left", "", notes, promise); got != "Check the loop ledger" {
		t.Fatalf("unstamped answer = %q", got)
	}
	if got := displayedNextAction("Go left", later, notes, promise); got != "Go left" {
		t.Fatalf("newer answer = %q", got)
	}
	if got := displayedNextAction("", "", notes, promise); got != "" {
		t.Fatalf("empty annotation grew a next action: %q", got)
	}
	if got := displayedNextAction("", later, notes, promise); got != "" {
		t.Fatalf("newer clear = %q", got)
	}
	step := &plans.HealthPromise{Source: "plan_step", What: "Retire loops"}
	if got := displayedNextAction("Go left", answerAt, notes, step); got != "Go left" {
		t.Fatalf("milestone must not replace next_action: %q", got)
	}
}
