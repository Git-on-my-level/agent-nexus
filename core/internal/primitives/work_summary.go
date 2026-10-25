package primitives

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"agent-nexus-core/internal/plans"
	series "agent-nexus-core/internal/seriesref"
)

// WorkSummary is the shared card presentation. Prose stays in the canonical
// card's summary until the HTTP boundary, so search and narrative derivation
// cannot accidentally treat presentation data as card content.
type WorkSummary struct {
	Metrics             []series.Metric   `json:"metrics,omitempty"`
	MetricsTruncated    bool              `json:"metrics_truncated,omitempty"`
	Hints               []string          `json:"hints,omitempty"`
	Steps               *plans.StepDigest `json:"steps,omitempty"`
	Status              SummaryStatus     `json:"status"`
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
func summaryChangeKey(input cardHealthInput, p *plans.Plan, facts map[string]plans.Fact, summary *WorkSummary) string {
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
	metrics := append([]series.Metric{}, summary.Metrics...)
	for i := range metrics {
		metrics[i].Age = 0
		metrics[i].Stale = false
		if metrics[i].Status == "stale" {
			metrics[i].Status = "ok"
		}
	}
	raw, _ := json.Marshal(struct {
		Metrics            []series.Metric
		MetricsTruncated   bool
		Input              cardHealthInput
		Plan               *plans.Plan
		Facts              map[string]plans.Fact
		Attention          *SummaryAttention
		AttentionTruncated bool
	}{metrics, summary.MetricsTruncated, input, p, selected, attention, summary.AttentionTruncated})
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

func summaryLabel(state string) string {
	if label := map[string]string{plans.PromisePast: "Past promise", plans.PromiseMoving: "Moving", plans.PromiseNone: "No promise", plans.PromiseDigest: "Daily digest", plans.PromiseDone: "Done"}[state]; label != "" {
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
func buildWorkSummary(input cardHealthInput, p *plans.Plan, state plans.State, facts map[string]plans.Fact, now time.Time) *WorkSummary {
	at := input.Activity
	if moved, err := time.Parse(time.RFC3339Nano, state.LastMovementAt); err == nil && moved.After(at) {
		at = moved
	}
	health := input.health("", p, state, facts, now)
	out := &WorkSummary{Status: SummaryStatus{State: health.State, Label: summaryLabel(health.State), Reason: health.Reason, Since: health.Since}, Owner: input.Owner, Due: input.Due}
	if p == nil || len(p.Steps) == 0 {
		out.Hints = []string{"no_plan"}
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
	if health.Promise != nil && health.Promise.Source == "note" && strings.TrimSpace(health.Promise.What) != "" {
		out.Next = &SummaryNext{Title: health.Promise.What, Ref: health.Promise.SourceRef}
	}
	return out
}

// BasicWorkSummary supports mutation echoes and lightweight test stores. Current
// reads replace it with the same presentation built from batched canonical facts.
func BasicWorkSummary(card map[string]any, now time.Time) *WorkSummary {
	input := cardHealthInput{Phase: firstNonEmptyString(workString(card["phase"]), workString(card["column_key"])), Due: workString(card["due_at"]), Owner: workString(card["owner"]), Source: workMap(card["source"]), Wake: workString(card["wake_condition"]), Handle: workString(card["handle"])}
	input.Created, _ = time.Parse(time.RFC3339Nano, workString(card["created_at"]))
	input.Activity = latestCardActivity(workString(card["updated_at"]))
	if input.Owner == "" {
		refs, _ := normalizeStringSlice(card["assignee_refs"])
		if len(refs) > 0 {
			input.Owner = refs[0]
		}
	}
	return buildWorkSummary(input, nil, plans.State{}, nil, now)
}
