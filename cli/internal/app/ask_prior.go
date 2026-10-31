package app

import (
	"context"
	"net/url"
	"slices"
	"strings"

	"agent-nexus-cli/internal/config"
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
	names := []string{"event:" + anyString(ask["id"]), anyString(ask["ref"])}
	if slices.Contains(names, anyString(payload["request_event_ref"])) || slices.Contains(names, "event:"+anyString(payload["request_event_id"])) {
		return true
	}
	return slices.Contains(names, responseAskID(payload))
}

// findPriorAsk reads events newest first. Prior-ask lookup is advisory: a failed
// read must not stop an agent reaching a human, so it reports nothing found.
func (a *App) findPriorAsk(ctx context.Context, cfg config.Resolved, threadID, title string) *priorAsk {
	path := "/events?thread_id=" + url.QueryEscape(threadID) + "&type=human_attention_requested,human_attention_responded,human_attention_withdrawn,human_attention_retired,human_attention_reopened,human_attention_revised&limit=" + askPriorEventLimit
	result, err := a.invokeRawJSON(ctx, cfg, "ask prior", "GET", path, nil)
	if err != nil {
		return nil
	}
	events := asSlice(commandResultBody(result)["events"])
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
