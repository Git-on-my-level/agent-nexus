package app

import (
	"context"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/output"
)

// Core keeps Decide to decisions: it returns the open ask for a repeat, the
// human's answer for a question a standing ruling covers, and stores the rest
// as an fyi when the human said this is not a decision. This file reports
// those outcomes, and checks them before a subject card would be created for
// an ask that is never filed.

// askTitleKey mirrors core's AskTitleKey: letters and digits only, case
// folded, single spaces.
func askTitleKey(title string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(title) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
			continue
		}
		space = true
	}
	return b.String()
}

// askTitlePattern mirrors core's AskTitlePattern: the key with every run of
// digits collapsed, so dated copies of one digest read as one subject.
func askTitlePattern(title string) string {
	var b strings.Builder
	digits := false
	for _, r := range askTitleKey(title) {
		if unicode.IsDigit(r) {
			if !digits {
				b.WriteByte('#')
			}
			digits = true
			continue
		}
		digits = false
		b.WriteRune(r)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// askCostMaxRunes is core's bound on stakes.cost, checked here so a long
// value fails before any write.
const askCostMaxRunes = 80

// applyAskIntent adds the optional intent fields to an ask payload.
func applyAskIntent(payload map[string]any, again, irreversible bool, cost string) {
	if again {
		payload["again"] = true
	}
	stakes := map[string]any{}
	if irreversible {
		stakes["irreversible"] = true
	}
	if cost = strings.TrimSpace(cost); cost != "" {
		stakes["cost"] = cost
	}
	if len(stakes) > 0 {
		payload["stakes"] = stakes
	}
}

// askRedirectResult turns a core redirect into the command result. ok is false
// when the ask was filed normally.
func askRedirectResult(body map[string]any, subjectRef, kind string) (*commandResult, bool) {
	ruling := asMap(body["ruling"])
	switch {
	case body["deduplicated"] == true:
		event := asMap(body["event"])
		return priorAskResult(&priorAsk{event: event, ref: "event:" + anyString(event["id"])}, subjectRef, kind), true
	case body["answered_by_ruling"] == true:
		return &commandResult{
			Data: map[string]any{"created": false, "status": "answered", "answer": anyString(ruling["answer"]), "ruling": ruling, "subject_ref": subjectRef, "kind": kind},
			Warnings: []output.Warning{askWarning("ask_covered_by_ruling", "a standing ruling already answers this, so nothing reached the human: "+truncateRunes(anyString(ruling["answer"]), 200)+
				"; act on it and cite "+anyString(ruling["ref"])+", or pass --again if the facts changed", nil)},
		}, true
	case anyString(body["redirected"]) != "":
		data := map[string]any{"created": false, "status": "told", "ruling": ruling, "subject_ref": subjectRef, "kind": kind, "redirected": body["redirected"]}
		if event := asMap(body["event"]); len(event) > 0 {
			data["event"] = event
		}
		return &commandResult{
			Data:     data,
			Warnings: []output.Warning{askWarning("ask_not_a_decision", "the human marked this kind of ask as not a decision, so it was stored as an fyi on the card instead of a decision; use anx tell for it next time", []string{"anx", "tell", subjectRef, "<headline>"})},
		}, true
	}
	return nil, false
}

// coveringRuling reads the standing rulings once (one bounded page) and
// returns the ruling core would apply to this title, plus up to three live
// answer rulings whose questions read alike. Whether a similar ruling covers
// the new question is the asker's judgment, so those only produce a warning.
// The read is advisory: a failure reports nothing.
func (a *App) coveringRuling(ctx context.Context, cfg config.Resolved, title string) (map[string]any, []map[string]any) {
	result, err := a.invokeRawJSON(ctx, cfg, "ask rulings", "GET", "/rulings", nil)
	if err != nil {
		return nil, nil
	}
	key, pattern := askTitleKey(title), askTitlePattern(title)
	type scored struct {
		ruling map[string]any
		score  float64
	}
	similar := []scored{}
	for _, raw := range asSlice(commandResultBody(result)["rulings"]) {
		ruling := asMap(raw)
		question := anyString(ruling["question"])
		switch anyString(ruling["kind"]) {
		case "answer":
			if askTitleKey(question) == key {
				return ruling, nil
			}
			if score := titleSimilarity(question, title); score >= 0.5 {
				similar = append(similar, scored{ruling, score})
			}
		case "not_a_decision":
			if askTitlePattern(question) == pattern && anyString(ruling["requester"]) == cfg.ActorID {
				return ruling, nil
			}
		}
	}
	sort.SliceStable(similar, func(i, j int) bool { return similar[i].score > similar[j].score })
	out := []map[string]any{}
	for _, s := range similar {
		if len(out) < 3 {
			out = append(out, s.ruling)
		}
	}
	return nil, out
}

// similarRulingWarning names rulings that may already answer the question.
func similarRulingWarning(similar []map[string]any) []output.Warning {
	if len(similar) == 0 {
		return nil
	}
	parts := make([]string, 0, len(similar))
	for _, ruling := range similar {
		parts = append(parts, anyString(ruling["ref"])+" \""+truncateRunes(anyString(ruling["question"]), 80)+"\" → "+truncateRunes(anyString(ruling["answer"]), 80))
	}
	return []output.Warning{askWarning("ask_similar_ruling", "a standing ruling may already answer this; if one does, act on it and withdraw this ask: "+strings.Join(parts, "; "), []string{"anx", "ask", "withdraw", "<ask-id>", "--reason", "covered by a standing ruling"})}
}

// openAskOfMine finds this agent's open ask with the same title from its
// brief, whichever card it is on. Core would return it anyway; finding it
// first avoids creating a subject card for an ask that is never filed.
func (a *App) openAskOfMine(ctx context.Context, cfg config.Resolved, title string) *priorAsk {
	result, err := a.invokeRawJSON(ctx, cfg, "ask prior", "GET", "/brief", nil)
	if err != nil {
		return nil
	}
	key := askTitleKey(title)
	for _, raw := range asSlice(commandResultBody(result)["your_asks"]) {
		ask := asMap(raw)
		if ref := anyString(ask["event_ref"]); ref != "" && anyString(ask["kind"]) == "ask" && askTitleKey(anyString(ask["title"])) == key {
			return &priorAsk{ref: ref, event: map[string]any{"id": strings.TrimPrefix(ref, "event:"), "payload": map[string]any{"title": ask["title"], "requester_actor_id": cfg.ActorID}}}
		}
	}
	return nil
}

// askBeforeSubject answers an ask that would not be filed, before a subject
// card is created for it: a repeat of the agent's own open ask, or one a
// standing ruling settles. A nil result means the ask should proceed; the
// similar rulings come back for the caller's warnings.
func (a *App) askBeforeSubject(ctx context.Context, cfg config.Resolved, title, kind string) (*commandResult, []map[string]any) {
	if prior := a.openAskOfMine(ctx, cfg, title); prior != nil {
		result := priorAskResult(prior, "", kind)
		// This session now waits on the open ask it would have filed.
		if warning := a.recordAskForSession(cfg, prior.ref, title); warning != nil {
			result.Warnings = append(result.Warnings, *warning)
		}
		return result, nil
	}
	ruling, similar := a.coveringRuling(ctx, cfg, title)
	if ruling != nil && anyString(ruling["kind"]) == "answer" {
		// Core counts a ruling's uses when it applies one; this ask never
		// reaches core, so say so. Best effort: the answer stands either way.
		_, _ = a.invokeRawJSON(ctx, cfg, "ask rulings", "POST", "/rulings/"+url.PathEscape(anyString(ruling["id"]))+"/applied", map[string]any{})
		result, _ := askRedirectResult(map[string]any{"answered_by_ruling": true, "ruling": ruling}, "", kind)
		return result, nil
	}
	return nil, similar
}

// settleRedirectedAskWait fixes the session's wait list after a redirect. The
// ask was recorded before publishing so a lost response could not cost the
// answer; an ask core never stored must not be waited on, and a repeat waits
// on the open ask instead.
func (a *App) settleRedirectedAskWait(cfg config.Resolved, localRef, title string, body map[string]any) {
	store, err := a.answerWaitStore(cfg)
	if err != nil {
		return
	}
	store.dropPending(localRef)
	if body["deduplicated"] == true {
		if id := anyString(asMap(body["event"])["id"]); id != "" {
			_ = store.recordAsk("event:"+id, title, time.Now())
		}
	}
}
