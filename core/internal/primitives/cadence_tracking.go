package primitives

import (
	"time"

	"agent-nexus-core/internal/plans"
)

// Cadence tracking shares the loop reader's scoped receipts and anchor. The
// promise is the overdue boundary, not next_run_due_at (one cadence earlier).
// Existing promise states remain wire compatible; loop carries richer health.
func cadenceTrackingHealth(loop map[string]any, phase, ref string, now time.Time) (plans.Health, bool) {
	every, _ := workInt(loop["every_seconds"])
	next := plans.ParseDue(workString(loop["next_run_due_at"]))
	if every <= 0 || next.IsZero() {
		return plans.Health{}, false
	}
	at := next.Add(time.Duration(every) * time.Second)
	last := loopTimeValue(loop["last_run_at"])
	s := plans.PromiseFold{Done: plans.HealthDone(phase), Promise: &plans.Promise{At: at, What: "run receipt", Source: "cadence", SourceRef: ref}, LastEvidenceAt: plans.ParseDue(last)}.At(now)
	h := plans.HealthFromState(s, now)
	if h.State != plans.PromiseDone {
		h.Reason = workString(loop["health_reason"])
		if last == "" && h.State == plans.PromiseMoving {
			h.Reason = "Awaiting first run."
			h.Promise.What = "awaiting first run"
		}
	}
	return h, true
}
