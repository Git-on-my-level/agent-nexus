package primitives

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"agent-nexus-core/internal/plans"
)

// nextAskDecidedCode is the machine reason for a next note whose cited ask
// is already decided. It is additive evidence on PossiblyMoot, not a close.
const nextAskDecidedCode = "next_ask_decided"

// nextAskRefsPerNote caps explicit event refs read from one note.
const nextAskRefsPerNote = 8

// noteEventRef matches an explicit event:<id> citation. Free text that never
// spells that ref is ignored.
var noteEventRef = regexp.MustCompile(`(?i)\bevent[\p{Z}\t\f\v ]*:[\p{Z}\t\f\v ]*([A-Za-z0-9][A-Za-z0-9_-]*)`)

// PossiblyMoot is the overtaken-by-events flag: every linked source whose
// workflow state is known has reached a terminal state, or the card's latest
// note cites an inbox ask the reader can see that is already decided. It is
// evidence for a human or agent to act on, never an automatic close; it can
// be stale the moment it is computed.
type PossiblyMoot struct {
	Flagged   bool   `json:"flagged"`
	Reason    string `json:"reason,omitempty"`
	Sources   int    `json:"sources"`
	Code      string `json:"code,omitempty"`
	AskRef    string `json:"ask_ref,omitempty"`
	DecidedAt string `json:"decided_at,omitempty"`
}

// possiblyMootPhases are the parked phases whose cards can be overtaken by
// their linked sources. Active work is not flagged: an in-progress card with
// everything around it done still needs somebody to land it.
var possiblyMootPhases = map[string]bool{"backlog": true, "ready": true}

// nextAskOpenPhase follows the phase the brief shows (briefPlacement), not
// the raw column key. A stored blocked card is placed in in_progress and a
// stored in_review card is placed in review, so both are open for this rule.
// done and cancelled stay closed. Source-terminal possibly_moot stays on the
// stored backlog and ready phases.
func nextAskOpenPhase(phase string) bool {
	switch briefPlacement(phase) {
	case "backlog", "ready", "in_progress", "review":
		return true
	}
	return false
}

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

// citedAsk is one event ref resolved to an inbox ask or review.
type citedAsk struct {
	ID         string
	Kind       string // ask or review
	State      string
	Outcome    string
	Reason     string
	AnsweredAt string
	ExpiresAt  string
	Visible    bool
}

// eventRefsInNote returns explicit event ids in appearance order. It does
// not infer an ask from prose that lacks the ref.
func eventRefsInNote(text string) []string {
	if text == "" {
		return nil
	}
	seen := map[string]bool{}
	out := []string{}
	for _, match := range noteEventRef.FindAllStringSubmatch(text, -1) {
		if len(match) < 2 {
			continue
		}
		id := match[1]
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
		if len(out) >= nextAskRefsPerNote {
			break
		}
	}
	return out
}

// noteCitationText is the latest note's body and next step, the text brief.v1
// shows as note and, when that note carries --next, as the promise.
func noteCitationText(payloadJSON string) string {
	if payloadJSON == "" {
		return ""
	}
	var event map[string]any
	if json.Unmarshal([]byte(payloadJSON), &event) != nil {
		return ""
	}
	payload := asMapValue(event["payload"])
	next := asMapValue(payload["next"])
	return strings.TrimSpace(strings.Join([]string{
		anyStringValue(payload["text"]),
		anyStringValue(payload["body"]),
		anyStringValue(next["what"]),
	}, "\n"))
}

// askCitedDecided reports whether this ask or review is already decided, and
// the instant to show as evidence. needs_context is still waiting. An ask the
// reader cannot see is never decided here: flagging it would reveal the answer.
func (a citedAsk) askCitedDecided(now time.Time) (bool, string) {
	if !a.Visible || (a.Kind != "ask" && a.Kind != "review") {
		return false, ""
	}
	// Expiry wins over an answer that is still waiting. needs_context does
	// not keep an ask open once expires_at has passed.
	if a.ExpiresAt != "" {
		at, ok := parseMootTime(a.ExpiresAt)
		if ok && !now.Before(at) {
			return true, at.UTC().Format(time.RFC3339)
		}
	}
	switch a.State {
	case "withdrawn", "expired", "resolved":
		return true, mootStamp(a.AnsweredAt)
	case "answered":
		if a.Outcome == "needs_context" {
			return false, ""
		}
		return true, mootStamp(a.AnsweredAt)
	}
	if a.Reason == "expired" {
		if at := mootStamp(a.AnsweredAt); at != "" {
			return true, at
		}
	}
	return false, ""
}

func parseMootTime(raw string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if at, err := time.Parse(layout, strings.TrimSpace(raw)); err == nil {
			return at, true
		}
	}
	return time.Time{}, false
}

func mootStamp(raw string) string {
	at, ok := parseMootTime(raw)
	if !ok {
		return ""
	}
	return at.UTC().Format(time.RFC3339)
}

// nextAskDecidedMoot flags when the note text cites an event ref that resolves
// to a visible inbox ask or review that is answered, resolved, withdrawn, or
// expired. The earliest decided citation wins. Missing and invisible asks are
// omitted, so the evidence never names them and an invisible decision cannot
// raise the flag.
func nextAskDecidedMoot(text string, asks map[string]citedAsk, now time.Time) PossiblyMoot {
	var chosen *citedAsk
	chosenAt := ""
	for _, id := range eventRefsInNote(text) {
		ask, ok := asks[id]
		if !ok {
			ask, ok = asks[strings.ToLower(id)]
		}
		if !ok {
			continue
		}
		decided, at := ask.askCitedDecided(now)
		if !decided {
			continue
		}
		ref := "event:" + ask.ID
		if chosen == nil || at < chosenAt || (at == chosenAt && ref < "event:"+chosen.ID) {
			copy := ask
			chosen = &copy
			chosenAt = at
		}
	}
	if chosen == nil {
		return PossiblyMoot{}
	}
	return PossiblyMoot{
		Flagged:   true,
		Reason:    nextAskDecidedCode,
		Code:      nextAskDecidedCode,
		AskRef:    "event:" + chosen.ID,
		DecidedAt: chosenAt,
	}
}

// mergeNextAsk keeps a linked-source flag and adds the ask evidence beside it.
// When only the ask rule flags, its reason code replaces the empty source reason.
func mergeNextAsk(base, ask PossiblyMoot) PossiblyMoot {
	if !ask.Flagged {
		return base
	}
	out := base
	out.Flagged = true
	out.Code = ask.Code
	out.AskRef = ask.AskRef
	out.DecidedAt = ask.DecidedAt
	if !base.Flagged {
		out.Reason = ask.Reason
	}
	return out
}
