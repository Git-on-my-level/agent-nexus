// Package plans validates initiative graphs and derives reproducible read state.
package plans

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

const MaxSteps = 200
const DefaultStalledAfter = 72 * time.Hour

type Step struct {
	ID     string   `json:"id"`
	Title  string   `json:"title"`
	Ref    string   `json:"ref,omitempty"`
	After  []string `json:"after"`
	Due    string   `json:"due,omitempty"`
	Status string   `json:"status,omitempty"`
}
type Plan struct {
	Steps []Step `json:"steps"`
}
type Progress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}
type EffectiveStep struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	Resolvable bool   `json:"resolvable"`
}
type State struct {
	Steps          []EffectiveStep `json:"steps"`
	Progress       Progress        `json:"progress"`
	CriticalPath   []string        `json:"critical_path"`
	NextSteps      []string        `json:"next_steps"`
	Shape          string          `json:"shape"`
	HealthState    string          `json:"health_state"`
	Health         string          `json:"health"`
	LastMovementAt string          `json:"last_movement_at"`
}
type Fact struct {
	Known      bool
	Status     string
	MovementAt time.Time
}

var slug = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func Validate(p Plan) error {
	if p.Steps == nil || len(p.Steps) > MaxSteps {
		return fmt.Errorf("steps must be an array of at most %d steps", MaxSteps)
	}
	ids := map[string]bool{}
	for _, s := range p.Steps {
		if !slug.MatchString(s.ID) || len(s.ID) > 64 || ids[s.ID] {
			return fmt.Errorf("step id %q must be a unique slug of at most 64 bytes", s.ID)
		}
		ids[s.ID] = true
		if strings.TrimSpace(s.Title) == "" || len(s.Title) > 500 {
			return fmt.Errorf("step %s title must contain 1..500 bytes", s.ID)
		}
		if len(s.After) > 50 {
			return fmt.Errorf("step %s has more than 50 dependencies", s.ID)
		}
		if s.Status != "" && s.Status != "done" && s.Status != "active" && s.Status != "blocked" && s.Status != "not_started" {
			return fmt.Errorf("step %s has invalid status", s.ID)
		}
		if s.Ref != "" {
			if len(s.Ref) > 2048 {
				return fmt.Errorf("step %s ref is too long", s.ID)
			}
			kind, value, ok := strings.Cut(s.Ref, ":")
			local := ok && value != "" && strings.TrimSpace(value) == value && (kind == "card" || kind == "doc" || kind == "document" || kind == "topic")
			u, err := url.Parse(s.Ref)
			external := err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && u.User == nil
			if !local && !external && !IsIdentifierAlias(s.Ref) {
				return fmt.Errorf("step %s ref must be a card/doc/topic ref, external source ref or HTTP(S) URL", s.ID)
			}
		}
		if s.Due != "" {
			_, dateErr := time.Parse("2006-01-02", s.Due)
			_, instantErr := time.Parse(time.RFC3339, s.Due)
			if dateErr != nil && instantErr != nil {
				return fmt.Errorf("step %s due must be an RFC3339 instant or YYYY-MM-DD", s.ID)
			}
		}
	}
	for _, s := range p.Steps {
		seen := map[string]bool{}
		for _, id := range s.After {
			if !ids[id] || seen[id] {
				return fmt.Errorf("step %s dependency %q must exist and be unique", s.ID, id)
			}
			seen[id] = true
		}
	}
	if len(order(p)) != len(p.Steps) {
		return fmt.Errorf("plan dependencies contain a cycle")
	}
	return nil
}

func order(p Plan) []string {
	indegree := map[string]int{}
	children := map[string][]string{}
	for _, s := range p.Steps {
		indegree[s.ID] = len(s.After)
		for _, id := range s.After {
			children[id] = append(children[id], s.ID)
		}
	}
	ready := []string{}
	for id, n := range indegree {
		if n == 0 {
			ready = append(ready, id)
		}
	}
	out := []string{}
	for len(ready) > 0 {
		sort.Strings(ready)
		id := ready[0]
		ready = ready[1:]
		out = append(out, id)
		for _, child := range children[id] {
			indegree[child]--
			if indegree[child] == 0 {
				ready = append(ready, child)
			}
		}
	}
	return out
}

// Status maps known native/source phases without guessing completion from absence.
func Status(phase string) string {
	switch phase {
	case "done", "published", "closed", "resolved", "merged":
		return "done"
	case "blocked":
		return "blocked"
	case "active", "in_progress", "in_review", "review", "open":
		return "active"
	default:
		return "not_started"
	}
}

// Compute requires a validated plan. Facts are principal-scoped by the caller.
func Compute(p Plan, facts map[string]Fact, movement, now time.Time, threshold time.Duration) State {
	if threshold <= 0 {
		threshold = DefaultStalledAfter
	}
	out := State{Steps: []EffectiveStep{}, CriticalPath: []string{}, NextSteps: []string{}, Shape: "chain", Health: "on_track", Progress: Progress{Total: len(p.Steps)}}
	steps := map[string]Step{}
	status := map[string]string{}
	children := map[string][]string{}
	roots := 0
	branching := false
	for _, s := range p.Steps {
		steps[s.ID] = s
		state := s.Status
		if state == "" {
			state = "not_started"
		}
		fact := facts[s.Ref]
		if s.Ref != "" && fact.Known && fact.Status != "" && fact.Status != "unknown" {
			state = Status(fact.Status)
		}
		status[s.ID] = state
		out.Steps = append(out.Steps, EffectiveStep{s.ID, state, s.Ref != "" && fact.Known})
		if state == "done" {
			out.Progress.Done++
		}
		if fact.MovementAt.After(movement) {
			movement = fact.MovementAt
		}
		if len(s.After) == 0 {
			roots++
		}
		if len(s.After) > 1 {
			branching = true
		}
		for _, id := range s.After {
			children[id] = append(children[id], s.ID)
		}
	}
	for _, list := range children {
		if len(list) > 1 {
			branching = true
		}
	}
	if branching {
		out.Shape = "dag"
	} else if roots > 1 {
		out.Shape = "lanes"
	}
	ids := order(p)
	// Longest unfinished-weight path to each node; sorted ids break ties.
	weights, prefix := map[string]int{}, map[string]int{}
	paths := map[string][]string{}
	longest := 0
	best := []string{}
	for _, id := range ids {
		if status[id] != "done" {
			weights[id] = 1
		}
		prior := []string{}
		for _, dep := range steps[id].After {
			if prefix[dep] > prefix[id] || (prefix[dep] == prefix[id] && strings.Join(paths[dep], "\x00") < strings.Join(prior, "\x00")) {
				prefix[id] = prefix[dep]
				prior = paths[dep]
			}
		}
		prefix[id] += weights[id]
		paths[id] = append(append([]string{}, prior...), id)
		if len(children[id]) == 0 && (prefix[id] > longest || (prefix[id] == longest && strings.Join(paths[id], "\x00") < strings.Join(best, "\x00"))) {
			longest = prefix[id]
			best = paths[id]
		}
		if status[id] != "done" && status[id] != "blocked" {
			ready := true
			for _, dep := range steps[id].After {
				if status[dep] != "done" {
					ready = false
				}
			}
			if ready {
				out.NextSteps = append(out.NextSteps, id)
			}
		}
	}
	for _, id := range best {
		if status[id] != "done" {
			out.CriticalPath = append(out.CriticalPath, id)
		}
	}
	out.HealthState = HealthFor(&p, out, movement, movement, now, threshold, "").State
	out.Health = LegacyHealth(out.HealthState)
	out.LastMovementAt = movement.UTC().Format(time.RFC3339Nano)
	return out
}
