package plans

import "time"

type Health struct {
	State  string  `json:"state"`
	Reason string  `json:"reason"`
	Since  *string `json:"since"`
}
type NextStep struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Ref   string `json:"ref"`
}

// HealthFor supplies the shared plan and initiative health contract. Anchors are derived, never persisted transition times.
func HealthFor(p *Plan, state State, movement, created, now time.Time, threshold time.Duration, cardDue string) Health {
	if threshold <= 0 {
		threshold = DefaultStalledAfter
	}
	anchor := movement
	h := Health{State: "on_track", Reason: "Open steps are progressing."}
	if p == nil || len(p.Steps) == 0 {
		h.State, h.Reason, anchor = "no_plan", "Initiative has no plan steps.", created
		if movement.IsZero() || now.Sub(movement) >= threshold {
			h.State, h.Reason = "stale", "Initiative has no plan and no recent card or discussion activity."
			if !movement.IsZero() {
				anchor = movement.Add(threshold)
			}
		}
	} else if state.Progress.Done == state.Progress.Total {
		h.State, h.Reason = "done", "All plan steps are complete."
	} else {
		blocked := false
		var riskAt time.Time
		due := func(raw string) {
			d, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				d, err = time.Parse("2006-01-02", raw)
			}
			if err == nil && !d.After(now.Add(24*time.Hour)) {
				onset := d.Add(-24 * time.Hour)
				if riskAt.IsZero() || onset.Before(riskAt) {
					riskAt = onset
				}
			}
		}
		due(cardDue)
		for i, step := range state.Steps {
			if step.Status == "done" {
				continue
			}
			if step.Status == "blocked" {
				blocked = true
			}
			due(p.Steps[i].Due)
		}
		switch {
		case blocked:
			h.State, h.Reason = "blocked", "An unfinished step or dependency is blocked."
		case !riskAt.IsZero():
			h.State, h.Reason, anchor = "at_risk", "An open step or initiative is overdue or due within 24 hours.", riskAt
		case movement.IsZero() || now.Sub(movement) >= threshold:
			h.State, h.Reason = "stale", "No card, plan or step activity within the configured stale threshold."
			if !movement.IsZero() {
				anchor = movement.Add(threshold)
			}
		}
	}
	if !anchor.IsZero() {
		at := anchor.UTC().Format(time.RFC3339Nano)
		h.Since = &at
	}
	return h
}

// ReadyStep prefers ready steps on the critical path, then the established
// lexicographic ready order. Visibility must be checked before exposing a title.
func ReadyStep(p Plan, state State, readable func(Step) bool) *NextStep {
	ready := map[string]bool{}
	steps := map[string]Step{}
	for _, id := range state.NextSteps {
		ready[id] = true
	}
	for _, step := range p.Steps {
		steps[step.ID] = step
	}
	for _, ids := range [][]string{state.CriticalPath, state.NextSteps} {
		for _, id := range ids {
			step := steps[id]
			if ready[id] && (readable == nil || readable(step)) {
				return &NextStep{ID: step.ID, Title: step.Title, Ref: step.Ref}
			}
		}
	}
	return nil
}

// LegacyHealth preserves the pre-detailed-health client vocabulary. Detailed
// state remains available in the additive plan_health field.
func LegacyHealth(state string) string {
	switch state {
	case "stale", "stalled":
		return "stalled"
	case "blocked":
		return "blocked"
	default:
		return "on_track"
	}
}
