package primitives

import (
	"fmt"
	"strings"

	"agent-nexus-core/internal/plans"
)

// PossiblyMoot is the overtaken-by-events flag: every linked source whose
// workflow state is known has reached a terminal state, so the card or open
// ask may be moot. It is evidence for a human or agent to act on, never an
// automatic close; it can be stale the moment it is computed.
type PossiblyMoot struct {
	Flagged bool   `json:"flagged"`
	Reason  string `json:"reason,omitempty"`
	Sources int    `json:"sources"`
}

// possiblyMootPhases are the parked phases whose cards can be overtaken by
// their linked sources. Active work is not flagged: an in-progress card with
// everything around it done still needs somebody to land it.
var possiblyMootPhases = map[string]bool{"backlog": true, "ready": true}

// mootSourceTerminal reports whether a resolved source status means the work
// it tracks is over. Cancelled counts: a cancelled dependency cannot make the
// card actionable again. Status maps done, published, closed, resolved and
// merged (the observation-side word for a merged PR) to done.
func mootSourceTerminal(status string) bool {
	return plans.Status(status) == "done" || status == "cancelled"
}

// possiblyMootLinked selects the linked sources that participate in the flag:
// refs carrying a workflow state. Context-only refs (documents, topics,
// boards, threads) say nothing about whether the work happened, so they
// neither contribute nor block. Everything else must resolve: an unobserved
// source stays unknown and withholds the flag rather than counting as done.
func possiblyMootLinked(refs []string) []string {
	linked := make([]string, 0, len(refs))
	seen := map[string]bool{}
	for _, ref := range refs {
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		kind, _, _ := strings.Cut(ref, ":")
		switch kind {
		case "doc", "document", "topic", "board", "thread":
			continue
		}
		linked = append(linked, ref)
	}
	return linked
}

// PossiblyMootFromFacts evaluates linked source refs against the same batched
// ref facts the plan computation already resolved. Only sources ANX has
// actually observed may count as terminal: a ref with no fact, an unresolved
// one, or one whose workflow state is unknown keeps the flag off entirely, so
// a card is never closed over a source that might still be open. At least one
// known source is required, and every known source must be terminal.
func PossiblyMootFromFacts(refs []string, facts map[string]plans.Fact) PossiblyMoot {
	out := PossiblyMoot{}
	terminal := 0
	uniform := ""
	for _, ref := range possiblyMootLinked(refs) {
		fact, ok := facts[ref]
		if !ok || !fact.Known || fact.Status == "" || fact.Status == "unknown" {
			// Unknown stays unknown: it never counts as terminal, and one
			// unknown source is enough to withhold the flag.
			return PossiblyMoot{}
		}
		out.Sources++
		if mootSourceTerminal(fact.Status) {
			terminal++
		}
		if uniform == "" {
			uniform = fact.Status
		} else if uniform != fact.Status {
			uniform = "done"
		}
	}
	if out.Sources == 0 || terminal < out.Sources {
		return out
	}
	out.Flagged = true
	if uniform == "" || uniform == "done" {
		uniform = "done"
	}
	out.Reason = fmt.Sprintf("all %d linked source%s %s %s", out.Sources, pluralSources(out.Sources), isAre(out.Sources), uniform)
	return out
}

func pluralSources(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func isAre(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}

// cardPossiblyMoot evaluates the flag for one card: parked (backlog or ready)
// cards only, over the plan's step refs and the card's related_refs. The
// facts are the same batched ref resolution the plan computation already ran.
func cardPossiblyMoot(input cardHealthInput, p *plans.Plan, facts map[string]plans.Fact) PossiblyMoot {
	if !possiblyMootPhases[input.Phase] {
		return PossiblyMoot{}
	}
	refs := append(append([]string{}, input.Related...), planStepRefs(derefPlan(p))...)
	return PossiblyMootFromFacts(refs, facts)
}

func derefPlan(p *plans.Plan) plans.Plan {
	if p == nil {
		return plans.Plan{}
	}
	return *p
}
