package plans

import (
	"sort"
	"time"
)

// The three short lists an initiative is read by: what just finished, what is
// moving now, and what comes next. Everything here is derived from the plan and
// the effective state the same read already computed, so the digest costs one
// pass over at most MaxSteps steps and issues no query of its own.
const (
	// StepDigestWindow is how recently a step must have moved to count as just
	// finished. A week is one working cycle: long enough that a reader who was
	// away over a weekend still sees what landed, short enough that it is news.
	StepDigestWindow = 7 * 24 * time.Hour
	// StepDigestLimit bounds every list. The remainder is counted, never listed.
	StepDigestLimit = 3
)

type DigestStep struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Ref    string `json:"ref,omitempty"`
	Status string `json:"status"`
	// At is the step's movement anchor, and only completed steps carry one.
	At string `json:"at,omitempty"`
}

type DigestList struct {
	Items []DigestStep `json:"items"`
	More  int          `json:"more"`
}

type StepDigest struct {
	WindowHours int        `json:"window_hours"`
	Completed   DigestList `json:"completed"`
	Current     DigestList `json:"current"`
	Next        DigestList `json:"next"`
}

func boundedSteps(items []DigestStep, limit int) DigestList {
	if len(items) > limit {
		return DigestList{Items: items[:limit], More: len(items) - limit}
	}
	return DigestList{Items: items, More: 0}
}

// Digest requires a validated plan and the state computed from it.
//
// A step is listed as completed only when a linked resource is what finished
// it and that resource moved inside the window. A step marked done inline has
// no completion time anywhere in the system, and dating it from the plan's
// last edit — or from a comment on a card whose phase says nothing — would
// report old work as finished this morning, so it is left out rather than
// guessed at. The anchor is the resource's last movement, which for a card
// that is done is the best completion time core holds.
//
// Current is what is moving or stuck; Next is the ready work that has not
// started, so the same step never appears in both. Both keep the plan's
// topological order with lexicographic ties, the same order geometry uses, so
// a tile and a graph cannot disagree about which step comes first.
func Digest(p Plan, state State, facts map[string]Fact, now time.Time, window time.Duration, limit int) StepDigest {
	if window <= 0 {
		window = StepDigestWindow
	}
	if limit <= 0 {
		limit = StepDigestLimit
	}
	out := StepDigest{WindowHours: int(window / time.Hour)}
	steps, status, ready := map[string]Step{}, map[string]string{}, map[string]bool{}
	for _, step := range p.Steps {
		steps[step.ID] = step
	}
	for _, step := range state.Steps {
		status[step.ID] = step.Status
	}
	for _, id := range state.NextSteps {
		ready[id] = true
	}
	type finished struct {
		step DigestStep
		at   time.Time
	}
	completed, current, next := []finished{}, []DigestStep{}, []DigestStep{}
	cutoff := now.Add(-window)
	for _, id := range order(p) {
		step := steps[id]
		row := DigestStep{ID: id, Title: step.Title, Ref: step.Ref, Status: status[id]}
		switch row.Status {
		case "done":
			/*
			 * Done because the linked resource is done, not because the plan
			 * says so: an inline "done" step that happens to point at a card
			 * with no known phase would otherwise be dated by a comment on
			 * that card and reported as finished yesterday.
			 */
			fact := facts[step.Ref]
			at := fact.MovementAt
			if step.Ref == "" || !fact.Known || Status(fact.Status) != "done" || at.IsZero() || at.Before(cutoff) {
				continue
			}
			row.At = at.UTC().Format(time.RFC3339Nano)
			completed = append(completed, finished{row, at})
		case "active", "blocked":
			current = append(current, row)
		default:
			if ready[id] {
				next = append(next, row)
			}
		}
	}
	// Newest movement first; the plan's own order breaks ties, so two steps
	// that moved together do not swap between reads.
	sort.SliceStable(completed, func(i, j int) bool { return completed[i].at.After(completed[j].at) })
	done := make([]DigestStep, 0, len(completed))
	for _, row := range completed {
		done = append(done, row.step)
	}
	out.Completed = boundedSteps(done, limit)
	out.Current = boundedSteps(current, limit)
	out.Next = boundedSteps(next, limit)
	return out
}
