package primitives

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"agent-nexus-core/internal/plans"
)

// WorkSummary is the shared card presentation. Prose stays in the canonical
// card's summary until the HTTP boundary, so search and narrative derivation
// cannot accidentally treat presentation data as card content.
type WorkSummary struct {
	Steps               *plans.StepDigest `json:"steps,omitempty"`
	Status              SummaryStatus     `json:"status"`
	SetStatus           *SummaryStatus    `json:"set_status,omitempty"`
	Progress            *SummaryProgress  `json:"progress,omitempty"`
	Next                *SummaryNext      `json:"next,omitempty"`
	Owner               string            `json:"owner,omitempty"`
	Due                 string            `json:"due,omitempty"`
	Age                 *int64            `json:"age,omitempty"`
	CreatedAt           string            `json:"created_at,omitempty"`
	LastMovementAt      string            `json:"last_movement_at,omitempty"`
	Source              map[string]any    `json:"source,omitempty"`
	ResolutionTruncated bool              `json:"resolution_truncated,omitempty"`
	Attention           *SummaryAttention `json:"attention,omitempty"`
	AttentionTruncated  bool              `json:"attention_truncated,omitempty"`
}
type SummaryStatus struct {
	State  string  `json:"state"`
	Label  string  `json:"label"`
	Reason string  `json:"reason,omitempty"`
	Since  *string `json:"since,omitempty"`
}
type SummaryProgress struct {
	Done      int    `json:"done"`
	Total     int    `json:"total"`
	Unit      string `json:"unit"`
	Truncated bool   `json:"truncated,omitempty"`
}
type SummaryNext struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Ref   string `json:"ref"`
	More  int    `json:"more"`
}
type SummaryAttention struct {
	Count     int    `json:"count"`
	OldestAge int64  `json:"oldest_age"`
	OldestAt  string `json:"oldest_at,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

// Stream identity uses canonical inputs, never health age thresholds or the
// rolling completed-step window. Select only this card's admitted facts; other
// cards in the shared resolution batch cannot invalidate its event identity.
func summaryChangeKey(input cardHealthInput, p *plans.Plan, facts map[string]plans.Fact, summary *WorkSummary, threshold time.Duration) string {
	selected := map[string]plans.Fact{}
	refs := append([]string{}, input.Children...)
	if p != nil {
		for _, step := range p.Steps {
			if step.Ref != "" {
				refs = append(refs, step.Ref)
			}
		}
	}
	for _, ref := range refs {
		selected[ref] = facts[ref]
	}
	var attention *SummaryAttention
	if summary.Attention != nil {
		copy := *summary.Attention
		copy.OldestAge = 0
		attention = &copy
	}
	raw, _ := json.Marshal(struct {
		Input              cardHealthInput
		Plan               *plans.Plan
		Facts              map[string]plans.Fact
		Attention          *SummaryAttention
		AttentionTruncated bool
		Threshold          time.Duration
	}{input, p, selected, attention, summary.AttentionTruncated, threshold})
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

func summaryLabel(state string) string {
	if label := map[string]string{"on_track": "In progress", "no_plan": "No plan", "stale": "Stale", "blocked": "Blocked", "at_risk": "At risk", "done": "Done", "cancelled": "Cancelled", "backlog": "Backlog", "ready": "Ready", "in_progress": "In progress", "review": "In review", "in_review": "In review", "unknown": "Unknown"}[state]; label != "" {
		return label
	}
	return strings.ReplaceAll(state, "_", " ")
}
func ageSeconds(now, at time.Time) int64 {
	if at.After(now) {
		return 0
	}
	return int64(now.Sub(at) / time.Second)
}
func summaryPhaseAgrees(phase string, health plans.Health, state plans.State) bool {
	switch health.State {
	case "done":
		return plans.Status(phase) == "done"
	case "cancelled":
		return phase == "cancelled"
	case "blocked":
		return phase == "blocked"
	case "on_track":
		if plans.Status(phase) == "active" {
			return true
		}
		// A ready/backlog card with an untouched plan is still waiting to start.
		if phase == "backlog" || phase == "ready" {
			for _, step := range state.Steps {
				if step.Status != "not_started" {
					return false
				}
			}
			return true
		}
	}
	return false
}
func buildWorkSummary(input cardHealthInput, p *plans.Plan, state plans.State, facts map[string]plans.Fact, now time.Time, threshold time.Duration) *WorkSummary {
	at := input.Activity
	if moved, err := time.Parse(time.RFC3339Nano, state.LastMovementAt); err == nil && moved.After(at) {
		at = moved
	}
	health := plans.HealthFor(p, state, at, input.Created, now, threshold, input.Due, input.Phase)
	out := &WorkSummary{Status: SummaryStatus{State: health.State, Label: summaryLabel(health.State), Reason: health.Reason, Since: health.Since}, Owner: input.Owner, Due: input.Due}
	if input.Phase != "" && !summaryPhaseAgrees(input.Phase, health, state) {
		out.SetStatus = &SummaryStatus{State: input.Phase, Label: summaryLabel(input.Phase)}
	}
	if !input.Created.IsZero() {
		age := ageSeconds(now, input.Created)
		out.Age = &age
		out.CreatedAt = input.Created.UTC().Format(time.RFC3339Nano)
	}
	if !at.IsZero() {
		out.LastMovementAt = at.UTC().Format(time.RFC3339Nano)
	}
	if authority := workString(input.Source["authority"]); authority != "" && authority != "nexus" {
		out.Source = input.Source
	}
	if p != nil {
		digest := plans.Digest(*p, state, facts, now, plans.StepDigestWindow, plans.StepDigestLimit, func(step plans.Step) bool { return step.Ref == "" || facts[step.Ref].Known })
		out.Steps = &digest
	}
	if p != nil && len(p.Steps) > 0 {
		out.Progress = &SummaryProgress{Done: state.Progress.Done, Total: state.Progress.Total, Unit: "steps"}
		out.ResolutionTruncated = planRefsTruncated(*p, facts)
		if next := plans.ReadyStep(*p, state, func(step plans.Step) bool { return step.Ref == "" || facts[step.Ref].Known }); next != nil {
			count := 0
			byID := map[string]plans.Step{}
			for _, step := range p.Steps {
				byID[step.ID] = step
			}
			for _, id := range state.NextSteps {
				step := byID[id]
				if step.Ref == "" || facts[step.Ref].Known {
					count++
				}
			}
			out.Next = &SummaryNext{ID: next.ID, Title: next.Title, Ref: next.Ref, More: count - 1}
		}
	} else if len(input.Children) > 0 {
		out.Progress = &SummaryProgress{Total: len(input.Children), Unit: "cards"}
		for _, ref := range input.Children {
			fact, resolved := facts[ref]
			if !resolved {
				out.Progress.Truncated = true
				out.ResolutionTruncated = true
			}
			if fact.Known && plans.Status(fact.Status) == "done" {
				out.Progress.Done++
			}
		}
	}
	return out
}

// BasicWorkSummary supports mutation echoes and lightweight test stores. Current
// reads replace it with the same presentation built from batched canonical facts.
func BasicWorkSummary(card map[string]any, now time.Time, threshold time.Duration) *WorkSummary {
	input := cardHealthInput{Phase: firstNonEmptyString(workString(card["phase"]), workString(card["column_key"])), Due: workString(card["due_at"]), Owner: workString(card["owner"]), Source: workMap(card["source"])}
	input.Created, _ = time.Parse(time.RFC3339Nano, workString(card["created_at"]))
	input.Activity = latestCardActivity(workString(card["updated_at"]))
	if input.Owner == "" {
		refs, _ := normalizeStringSlice(card["assignee_refs"])
		if len(refs) > 0 {
			input.Owner = refs[0]
		}
	}
	return buildWorkSummary(input, nil, plans.State{}, nil, now, threshold)
}
