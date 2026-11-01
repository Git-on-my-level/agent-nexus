package app

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/output"
)

const askPriorEventLimit = "200"

// priorAsk is the newest ask with the same question on a subject that is still
// open or already answered. Withdrawn and retired asks do not count, so asking
// again after either is an ordinary new ask.
type priorAsk struct {
	event    map[string]any
	ref      string
	response map[string]any
}

func askQuestionKey(title string) string {
	return strings.ToLower(strings.Join(strings.Fields(title), " "))
}

func askLifecycleNames(ev, ask map[string]any) bool {
	payload := asMap(ev["payload"])
	names := []string{"event:" + anyString(ask["id"])}
	// An absent ref must not match an event that names no ask.
	if ref := anyString(ask["ref"]); ref != "" {
		names = append(names, ref)
	}
	if slices.Contains(names, anyString(payload["request_event_ref"])) || slices.Contains(names, "event:"+anyString(payload["request_event_id"])) {
		return true
	}
	return slices.Contains(names, responseAskID(payload))
}

func (a *App) findPriorAsk(ctx context.Context, cfg config.Resolved, threadID, title string) *priorAsk {
	return priorAskIn(a.priorAskEvents(ctx, cfg, threadID), title)
}

// priorAskEvents reads events newest first. Prior-ask lookup is advisory: a failed
// read must not stop an agent reaching a human, so it reports nothing found.
func (a *App) priorAskEvents(ctx context.Context, cfg config.Resolved, threadID string) []any {
	path := "/events?thread_id=" + url.QueryEscape(threadID) + "&type=human_attention_requested,human_attention_responded,human_attention_withdrawn,human_attention_retired,human_attention_reopened,human_attention_revised&limit=" + askPriorEventLimit
	result, err := a.invokeRawJSON(ctx, cfg, "ask prior", "GET", path, nil)
	if err != nil {
		return nil
	}
	return asSlice(commandResultBody(result)["events"])
}

func priorAskIn(events []any, title string) *priorAsk {
	want := askQuestionKey(title)
	revisedTitles := map[string]string{}
	var replaced []string
	for _, raw := range events {
		ev := asMap(raw)
		payload := asMap(ev["payload"])
		switch anyString(ev["type"]) {
		case "human_attention_revised":
			id := anyString(payload["request_event_id"])
			if _, seen := revisedTitles[id]; !seen {
				revisedTitles[id] = anyString(asMap(payload["presentation"])["title"])
			}
		case humanAttentionRequestedEventType:
			if old := anyString(payload["supersedes"]); old != "" {
				replaced = append(replaced, old)
			}
		}
	}
	var ask map[string]any
	for _, raw := range events {
		ev := asMap(raw)
		payload := asMap(ev["payload"])
		if anyString(ev["type"]) != humanAttentionRequestedEventType || anyString(payload["kind"]) != "ask" {
			continue
		}
		if slices.Contains(replaced, "event:"+anyString(ev["id"])) || slices.Contains(replaced, anyString(ev["ref"])) {
			continue
		}
		if askQuestionKey(firstNonEmpty(revisedTitles[anyString(ev["id"])], anyString(payload["title"]))) == want {
			ask = ev
			break
		}
	}
	if ask == nil || anyString(ask["id"]) == "" {
		return nil
	}
	prior := &priorAsk{event: ask, ref: "event:" + anyString(ask["id"])}
	for _, raw := range events {
		ev := asMap(raw)
		eventType := anyString(ev["type"])
		if eventType == humanAttentionRequestedEventType || eventType == "human_attention_revised" || !askLifecycleNames(ev, ask) {
			continue
		}
		switch eventType {
		case "human_attention_responded":
			prior.response = ev
		case "human_attention_withdrawn", "human_attention_retired":
			return nil
		}
		// The newest lifecycle event decides; a reopened ask is simply open.
		break
	}
	return prior
}

// priorAskResult answers a repeated ask from the workspace instead of filing it
// twice, so a retry after a timeout is safe and an answered question stays
// answered until the agent says otherwise.
func priorAskResult(prior *priorAsk, subjectRef, kind string) *commandResult {
	body := map[string]any{"ask_id": prior.ref, "subject_ref": subjectRef, "kind": kind, "event": prior.event, "created": false, "status": "open"}
	if prior.response != nil {
		body["status"] = "answered"
		payload := asMap(prior.response["payload"])
		body["answer"] = firstNonEmpty(anyString(payload["response_text"]), anyString(prior.response["summary"]))
		return &commandResult{
			Data:     body,
			Warnings: []output.Warning{askWarning("ask_already_answered", "this question on this card is already answered; pass --again only if the facts changed", nil)},
		}
	}
	return &commandResult{
		Data:        body,
		Warnings:    []output.Warning{askWarning("ask_already_open", "this question on this card is already open; nothing new was filed", []string{"anx", "await", prior.ref})},
		NextActions: []output.NextAction{action("Wait for the answer", "anx", "await", prior.ref)},
	}
}

// repeatedAskBatch answers a batch already on the card without filing it twice.
// The server files a batch atomically, so a retry after a lost response finds every
// ask. Finding only some means the batch changed, and filing the rest would hide
// that from the agent, so nothing is filed until it decides. A nil result means none
// of the asks exist yet.
func (a *App) repeatedAskBatch(ctx context.Context, cfg config.Resolved, entries []askBatchEntry, threadID string) (*commandResult, error) {
	events := a.priorAskEvents(ctx, cfg, threadID)
	priors := make([]*priorAsk, len(entries))
	repeated := []string{}
	for index := range entries {
		if priors[index] = priorAskIn(events, entries[index].Title); priors[index] != nil {
			repeated = append(repeated, fmt.Sprintf("asks[%d] is %s", index, priors[index].ref))
		}
	}
	if len(repeated) == 0 {
		return nil, nil
	}
	if len(repeated) < len(entries) {
		return nil, errnorm.Usage("invalid_request", "nothing was filed: "+strings.Join(repeated, ", ")+" already on this card; remove them from the batch, or pass --again if the facts changed")
	}
	result := &commandResult{}
	askViews := make([]map[string]any, 0, len(entries))
	registrationFailures := 0
	for index := range entries {
		entry, prior := &entries[index], priors[index]
		repeat := priorAskResult(prior, entry.subject, "ask")
		for _, warning := range repeat.Warnings {
			warning.Message = fmt.Sprintf("asks[%d]: %s", index, warning.Message)
			result.Warnings = append(result.Warnings, warning)
		}
		result.NextActions = append(result.NextActions, repeat.NextActions...)
		view := asMap(repeat.Data)
		delete(view, "event")
		view["title"] = entry.Title
		askViews = append(askViews, view)
		// Another agent's ask delivers to that agent, so this call has nothing to report.
		if cfg.ActorID == "" || anyString(asMap(prior.event["payload"])["requester_actor_id"]) != cfg.ActorID {
			continue
		}
		// The lost response may have cost the resume registration too, and only this
		// retry can restore it.
		entry.deliveryInput.AskID = strings.TrimPrefix(prior.ref, "event:")
		decided, err := a.resolveAskDelivery(cfg, entry.deliveryInput)
		if err != nil {
			return nil, errnorm.Usage("invalid_request", fmt.Sprintf("asks[%d]: %v", index, err))
		}
		entry.decided = decided
		view["delivery"] = askDeliveryResultView(decided, prior.ref)
		if decided.Mode == askDeliveryResume && prior.response == nil {
			if err := a.registerAskBatchResume(ctx, cfg, entry, prior.ref); err != nil {
				registrationFailures++
				view["registration_error"] = err.Error()
			}
		}
	}
	if registrationFailures > 0 {
		result.Warnings = append(result.Warnings, askBatchRegistrationWarning(registrationFailures))
	}
	result.Data = map[string]any{
		"batch_id":  anyString(asMap(priors[0].event["payload"])["batch_id"]),
		"ask_count": len(askViews),
		"asks":      askViews,
		"created":   false,
	}
	return result, nil
}

// priorRulings warns about decisions already recorded on the subject, which is
// the only way an agent in another session finds out the human already ruled.
func (a *App) priorRulings(ctx context.Context, cfg config.Resolved, threadID string) []output.Warning {
	path := "/events?thread_id=" + url.QueryEscape(threadID) + "&type=message_posted&limit=50"
	result, err := a.invokeRawJSON(ctx, cfg, "ask rulings", "GET", path, nil)
	if err != nil {
		return nil
	}
	for _, raw := range asSlice(commandResultBody(result)["events"]) {
		ev := asMap(raw)
		payload := asMap(ev["payload"])
		if anyString(payload["decided_by"]) != "" {
			return []output.Warning{askWarning("ask_prior_ruling", "this card has a recorded ruling; check it still leaves this open: "+truncateRunes(anyString(payload["text"]), 200), nil)}
		}
	}
	return nil
}
