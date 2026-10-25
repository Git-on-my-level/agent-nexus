package plans

import (
	"regexp"
	"time"
)

// Health is the promise state of one card: past_promise, moving, no_promise,
// digest or done (contracts/anx-schema.yaml plan_health). It is computed on read
// from FoldPromise; there are no inactivity thresholds.
type Health struct {
	State                 string         `json:"state"`
	Reason                string         `json:"reason"`
	Since                 *string        `json:"since"`
	Promise               *HealthPromise `json:"promise,omitempty"`
	PastPromiseForSeconds *int64         `json:"past_promise_for_seconds,omitempty"`
	PromiseMoves          int            `json:"promise_moves,omitempty"`
	LastEvidenceAt        *string        `json:"last_evidence_at,omitempty"`
}

// HealthPromise is the counted promise in the BriefPromise wire shape.
type HealthPromise struct {
	At        string           `json:"at"`
	What      string           `json:"what"`
	Source    string           `json:"source"`
	SourceRef string           `json:"source_ref,omitempty"`
	Author    *HealthPrincipal `json:"author,omitempty"`
}

// HealthPrincipal is the author of a note promise. Display falls back to the id.
type HealthPrincipal struct {
	ID      string `json:"id"`
	Display string `json:"display"`
}

type NextStep struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Ref   string `json:"ref"`
}

// HealthInput is everything HealthFor reads for one card.
type HealthInput struct {
	Created, DefaultAt time.Time
	DefaultAuthor      string
	// Ref is the card ref used for promise source refs ("card:<handle>").
	Ref   string
	Plan  *Plan
	State State
	// Facts are the resolved step refs; a linked step resource moving is a
	// plan-step state change and resets the clock.
	Facts map[string]Fact
	// Phase is the card's stored phase; done or cancelled reads done.
	Phase   string
	CardDue string
	// Wake is the raw wake_condition annotation; its first date is a promise.
	Wake string
	// PlanEditedAt is authored activity, not evidence that meets a promise.
	PlanEditedAt time.Time
	// StepChanges overrides inferred ref movement when non-nil. Store reads
	// use the retained status-change clock, so a newly linked ref cannot
	// manufacture progress from its creation or metadata timestamp.
	StepChanges []time.Time
	// Observations are changed source observation times.
	Observations []time.Time
	// Notes are the card's message_posted facts; nil when unavailable.
	Notes []PromiseNote
}

// wakeTimePattern matches an embedded RFC3339 instant or YYYY-MM-DD date.
// Order matters: the instant alternative must win over its own date prefix.
var wakeTimePattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})|\d{4}-\d{2}-\d{2}`)

// WakeTime extracts the first dated signal inside a wake_condition annotation.
// The condition is prose, so this is deliberately lenient: the first
// RFC3339 instant or YYYY-MM-DD date in the first 1024 bytes wins, dates
// resolving to midnight UTC. Anything unparsable contributes no promise.
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

// HealthDone reports whether a stored phase is terminal (done or cancelled).
func HealthDone(phase string) bool {
	return Status(phase) == "done" || phase == "cancelled"
}

// PromiseInputFor builds the fold input for one card.
func PromiseInputFor(in HealthInput) PromiseInput {
	done := HealthDone(in.Phase)
	if in.Phase == "" && in.Plan != nil && len(in.Plan.Steps) > 0 && in.State.Progress.Total > 0 && in.State.Progress.Done == in.State.Progress.Total {
		done = true
	}
	out := PromiseInput{Created: in.Created, DefaultAt: in.DefaultAt, DefaultAuthor: in.DefaultAuthor, Ref: in.Ref, Done: done, Notes: in.Notes, CardDue: ParseDue(in.CardDue), Wake: WakeTime(in.Wake), Observations: in.Observations, StepChanges: in.StepChanges}
	if !in.PlanEditedAt.IsZero() {
		out.PlanEdits = []time.Time{in.PlanEditedAt}
	}
	if in.Plan != nil {
		for i, step := range in.Plan.Steps {
			if fact, ok := in.Facts[step.Ref]; in.StepChanges == nil && step.Ref != "" && ok && fact.Known && !fact.MovementAt.IsZero() {
				out.StepChanges = append(out.StepChanges, fact.MovementAt)
			}
			status := step.Status
			if i < len(in.State.Steps) {
				status = in.State.Steps[i].Status
			}
			if status == "done" {
				continue
			}
			if due := ParseDue(step.Due); !due.IsZero() {
				out.Steps = append(out.Steps, PromiseStep{ID: step.ID, Title: step.Title, Due: due})
			}
		}
	}
	return out
}

// HealthFor folds a card's promise facts and reads the result at now.
func HealthFor(in HealthInput, now time.Time) Health {
	return HealthFromState(FoldPromise(PromiseInputFor(in)).At(now), now)
}

// HealthFromState renders a read promise state in the PlanHealth shape.
func HealthFromState(s PromiseState, now time.Time) Health {
	h := Health{State: s.State, Reason: s.Reason(now), PromiseMoves: s.Moves}
	format := func(at time.Time) *string {
		if at.IsZero() {
			return nil
		}
		v := at.UTC().Format(time.RFC3339Nano)
		return &v
	}
	h.LastEvidenceAt = format(s.LastEvidenceAt)
	if s.Promise != nil {
		p := &HealthPromise{At: s.Promise.At.UTC().Format(time.RFC3339Nano), What: s.Promise.What, Source: s.Promise.Source, SourceRef: s.Promise.SourceRef}
		if s.Promise.Author != "" {
			p.Author = &HealthPrincipal{ID: s.Promise.Author, Display: s.Promise.Author}
		}
		h.Promise = p
	}
	if s.PastPromiseFor != nil {
		secs := int64(*s.PastPromiseFor / time.Second)
		h.PastPromiseForSeconds = &secs
	}
	switch s.State {
	case PromisePast:
		h.Since = format(s.Promise.At)
	case PromiseMoving, PromiseNone:
		h.Since = h.LastEvidenceAt
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
