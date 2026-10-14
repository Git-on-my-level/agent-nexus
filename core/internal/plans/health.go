package plans

import (
	"fmt"
	"regexp"
	"time"
)

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

// DefaultDarkAfter is the horizon beyond which an active card with no nearer
// dated signal reads dark: nothing will bring it back to attention until then.
const DefaultDarkAfter = 7 * 24 * time.Hour

// DarkInput carries the per-card dark inputs. Wake is the raw wake_condition
// annotation; Threshold is the per-card horizon, where zero means the default.
type DarkInput struct {
	Wake      string
	Threshold time.Duration
}

// wakeTimePattern matches an embedded RFC3339 instant or YYYY-MM-DD date.
// Order matters: the instant alternative must win over its own date prefix.
var wakeTimePattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})|\d{4}-\d{2}-\d{2}`)

// WakeTime extracts the first dated signal inside a wake_condition annotation.
// The condition is prose, so this is deliberately lenient: the first
// RFC3339 instant or YYYY-MM-DD date in the first 1024 bytes wins, dates
// resolving to midnight UTC. Anything unparsable contributes no signal. The
// bound can split an instant at the cut, whose date prefix then reads as
// midnight — accepted, since the scan is a hint over prose, not a parser.
func WakeTime(wake string) time.Time {
	if len(wake) > 1024 {
		wake = wake[:1024]
	}
	for _, match := range wakeTimePattern.FindAllString(wake, -1) {
		if at, err := time.Parse(time.RFC3339Nano, match); err == nil {
			return at
		}
		if at, err := time.Parse("2006-01-02", match); err == nil {
			return at
		}
	}
	return time.Time{}
}

// NextSignal returns the earliest dated thing that will surface this card
// again: the earliest open (unfinished) step due date, the card due date and
// the wake_condition date. A signal in the past still counts — it was
// scheduled, so the card is not dark about the future. Zero when none exists.
func NextSignal(p *Plan, state State, cardDue, wake string) time.Time {
	signal := earliestDue(p, state, cardDue)
	if at := WakeTime(wake); !at.IsZero() && (signal.IsZero() || at.Before(signal)) {
		signal = at
	}
	return signal
}

// earliestDue is the earliest date among the card due and open step dues.
func earliestDue(p *Plan, state State, cardDue string) time.Time {
	earliest := time.Time{}
	consider := func(raw string) {
		d, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			d, err = time.Parse("2006-01-02", raw)
		}
		if err == nil && (earliest.IsZero() || d.Before(earliest)) {
			earliest = d
		}
	}
	consider(cardDue)
	if p != nil {
		for i, step := range state.Steps {
			if step.Status == "done" || i >= len(p.Steps) {
				continue
			}
			consider(p.Steps[i].Due)
		}
	}
	return earliest
}

// DarkOutcome decides the dark state for a card that would otherwise read
// on_track (or, for a planless card, in progress). The reason, the anchor
// instant (zero when there is none) and whether dark applies come back; the
// state is always "dark" when ok is true.
func DarkOutcome(p *Plan, state State, cardDue string, dark DarkInput, now time.Time) (reason string, anchor time.Time, ok bool) {
	signal := NextSignal(p, state, cardDue, dark.Wake)
	horizon := dark.Threshold
	if horizon <= 0 {
		horizon = DefaultDarkAfter
	}
	if signal.IsZero() {
		return "Nothing is dated: no open step due date, card due date or wake date, so no signal will surface this card.", time.Time{}, true
	}
	if signal.After(now.Add(horizon)) {
		days := int((signal.Sub(now) + 24*time.Hour - time.Nanosecond) / (24 * time.Hour))
		return fmt.Sprintf("Next dated signal is %s, %d days out; nothing prompts this card until then.", formatSignal(signal), days), signal, true
	}
	return "", time.Time{}, false
}

func formatSignal(at time.Time) string {
	// A date-only signal parses to UTC midnight; show it as the date it names.
	if at.Location() == time.UTC && at.Hour() == 0 && at.Minute() == 0 && at.Second() == 0 && at.Nanosecond() == 0 {
		return at.Format("2006-01-02")
	}
	return at.UTC().Format(time.RFC3339)
}

// HealthFor supplies the shared plan and initiative health contract. Anchors are derived, never persisted transition times.
func HealthFor(p *Plan, state State, movement, created, now time.Time, threshold time.Duration, cardDue string, dark DarkInput, cardPhase ...string) Health {
	if threshold <= 0 {
		threshold = DefaultStalledAfter
	}
	anchor := movement
	h := Health{State: "on_track", Reason: "Open steps are progressing."}
	if p == nil || len(p.Steps) == 0 {
		// Planless work still has workflow, blocked and due facts. Callers that
		// omit phase retain the legacy initiative no-plan vocabulary.
		phase := ""
		if len(cardPhase) > 0 {
			phase = cardPhase[0]
		}
		switch {
		case Status(phase) == "done":
			h.State, h.Reason = "done", "The card is complete."
		case phase == "cancelled":
			h.State, h.Reason = "cancelled", "The card was cancelled."
		case phase == "blocked":
			h.State, h.Reason = "blocked", "The card is blocked."
		default:
			h.State, h.Reason, anchor = "no_plan", "Card has no plan steps.", created
			if len(cardPhase) == 0 {
				h.Reason = "Initiative has no plan steps."
			}
			d, err := time.Parse(time.RFC3339, cardDue)
			if err != nil {
				d, err = time.Parse("2006-01-02", cardDue)
			}
			if len(cardPhase) > 0 && err == nil && !d.After(now.Add(24*time.Hour)) {
				h.State, h.Reason, anchor = "at_risk", "The card is overdue or due within 24 hours.", d.Add(-24*time.Hour)
			} else if movement.IsZero() || now.Sub(movement) >= threshold {
				h.State, h.Reason = "stale", "Card has no plan and no recent card or discussion activity."
				if len(cardPhase) == 0 {
					h.Reason = "Initiative has no plan and no recent card or discussion activity."
				}
				if !movement.IsZero() {
					anchor = movement.Add(threshold)
				}
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
		default:
			if reason, at, ok := DarkOutcome(p, state, cardDue, dark, now); ok {
				h.State, h.Reason = "dark", reason
				anchor = at
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
//
// A state this field can name is never collapsed into "on_track". An
// initiative with no plan steps has no basis for a green chip, and reporting
// one made every planless initiative read as healthy; the same went for
// at_risk and done. Only a genuinely unnamed state falls back to on_track.
// Dark is genuinely unnamed there: legacy readers saw on_track for these
// cards before dark existed, and they keep seeing it.
func LegacyHealth(state string) string {
	switch state {
	case "stale", "stalled":
		return "stalled"
	case "blocked", "no_plan", "at_risk", "done", "on_track", "cancelled":
		return state
	default:
		return "on_track"
	}
}
