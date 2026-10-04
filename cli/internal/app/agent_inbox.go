package app

import (
	"context"
	"fmt"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func (a *App) runFirstClassInboxCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "inbox", inboxSubcommandSpec.requiredError()
	}
	sub := inboxSubcommandSpec.normalize(args[0])
	switch sub {
	case "list":
		result, err := a.runAgentInboxList(ctx, args[1:], cfg)
		if err != nil && len(args[1:]) == 0 && cfg.AgentID == "" && cfg.Agent == "" {
			result, err = a.runInboxList(ctx, nil, cfg)
		}
		return result, "inbox list", err
	case "get":
		result, commandName, err := a.runInboxGet(ctx, args[1:], cfg)
		return result, commandName, err
	case "respond":
		result, name, err := a.runTypedResource(ctx, "inbox", args, cfg)
		return result, name, err
	case "read":
		result, err := a.runAgentInboxRead(ctx, args[1:], cfg)
		return result, "inbox read", err
	case "stream", "tail":
		result, err := a.runInboxStream(ctx, args[1:], cfg, "inbox stream", sub == "tail")
		return result, "inbox stream", err
	default:
		return nil, "inbox", inboxSubcommandSpec.unknownError(args[0])
	}
}

func (a *App) runAgentInboxList(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("inbox list")
	var status trackedString
	var unread trackedBool
	fs.Var(&status, "status", "Filter my asks: open, answered, or all (default open)")
	fs.Var(&unread, "unread", "Show only answers with an unread wake notification")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx inbox list`")
	}
	statusValue := strings.ToLower(strings.TrimSpace(status.value))
	if statusValue == "" {
		statusValue = "open"
	}
	if statusValue != "open" && statusValue != "answered" && statusValue != "all" {
		return nil, errnorm.Usage("invalid_request", "--status must be one of: open, answered, all")
	}
	items, err := a.loadOwnInboxAsks(ctx, cfg)
	if err != nil {
		return nil, err
	}
	filtered := make([]any, 0, len(items))
	for _, item := range items {
		answered := asMap(item["answer"]) != nil
		if statusValue == "open" && answered || statusValue == "answered" && !answered {
			continue
		}
		if unread.value && item["answer_unread"] != true {
			continue
		}
		filtered = append(filtered, item)
	}
	lines := make([]string, 0, len(filtered))
	for _, raw := range filtered {
		item := asMap(raw)
		line := fmt.Sprintf("%s status=%s title=%q", anyString(item["ask_id"]), anyString(item["status"]), anyString(item["title"]))
		if item["answer_unread"] == true {
			line += " answer_unread=true"
		}
		if answer := asMap(item["answer"]); answer != nil {
			line += fmt.Sprintf(" outcome=%s answer=%q", anyString(answer["outcome"]), anyString(answer["text"]))
		}
		lines = append(lines, line)
	}
	data := map[string]any{"items": filtered, "count": len(filtered), "status": statusValue, "unread_only": unread.value, "matched": len(items)}
	return &commandResult{Data: data, Text: strings.Join(lines, "\n")}, nil
}

func (a *App) loadOwnInboxAsks(ctx context.Context, cfg config.Resolved) ([]map[string]any, error) {
	agent, _, err := a.dailyAgent(ctx, cfg)
	if err != nil {
		return nil, err
	}
	actorID := strings.TrimSpace(anyString(agent["actor_id"]))
	if actorID == "" {
		return nil, errnorm.Usage("identity_unresolved", "active agent has no actor id")
	}
	requests, err := a.invokeRawJSON(ctx, cfg, "inbox asks", "GET", "/events?type=human_attention_requested&limit=100", nil)
	if err != nil {
		return nil, err
	}
	answers, err := a.invokeRawJSON(ctx, cfg, "inbox answers", "GET", "/events?type=human_attention_responded&limit=100", nil)
	if err != nil {
		return nil, err
	}
	withdrawals, err := a.invokeRawJSON(ctx, cfg, "inbox withdrawn asks", "GET", "/events?type=human_attention_withdrawn&limit=100", nil)
	if err != nil {
		return nil, err
	}
	withdrawnByAsk := map[string]bool{}
	for _, raw := range asSlice(commandResultBody(withdrawals)["events"]) {
		payload := asMap(asMap(raw)["payload"])
		askID := strings.TrimSpace(anyString(payload["request_event_id"]))
		if askID == "" {
			askID = strings.TrimPrefix(strings.TrimSpace(anyString(payload["request_event_ref"])), "event:")
		}
		if askID != "" {
			withdrawnByAsk["event:"+askID] = true
		}
	}
	answerByAsk := map[string]map[string]any{}
	for _, raw := range asSlice(commandResultBody(answers)["events"]) {
		event := asMap(raw)
		payload := asMap(event["payload"])
		if anyString(payload["requester_actor_id"]) != actorID {
			continue
		}
		askID := responseAskID(payload)
		if askID == "" {
			continue
		}
		answerByAsk[askID] = map[string]any{
			"text": payload["response_text"], "outcome": payload["outcome"],
			"responder": payload["responding_actor_id"], "at": event["ts"],
			"response_event_ref": "event:" + anyString(event["id"]),
		}
	}
	notifications, err := a.invokeRawJSON(ctx, cfg, "inbox answer notifications", "GET", "/agent-notifications?status=unread", nil)
	if err != nil {
		return nil, err
	}
	unreadAnswers := map[string]bool{}
	for _, raw := range asSlice(commandResultBody(notifications)["items"]) {
		item := asMap(raw)
		unreadAnswers[anyString(item["trigger_event_id"])] = true
		for _, ref := range stringList(item["related_refs"]) {
			if strings.HasPrefix(ref, "event:") {
				unreadAnswers[strings.TrimPrefix(ref, "event:")] = true
			}
		}
	}
	items := make([]map[string]any, 0)
	for _, raw := range asSlice(commandResultBody(requests)["events"]) {
		event := asMap(raw)
		payload := asMap(event["payload"])
		if anyString(payload["requester_actor_id"]) != actorID {
			continue
		}
		askID := "event:" + anyString(event["id"])
		if withdrawnByAsk[askID] {
			continue
		}
		answer := answerByAsk[askID]
		answerUnread := false
		if answer != nil {
			answerUnread = unreadAnswers[strings.TrimPrefix(anyString(answer["response_event_ref"]), "event:")]
		}
		status := "open"
		if answer != nil {
			status = "answered"
		}
		items = append(items, map[string]any{
			"ask_id":        askID,
			"title":         payload["title"],
			"subject_ref":   payload["subject_ref"],
			"requested_at":  event["ts"],
			"status":        status,
			"answer":        answer,
			"answer_unread": answerUnread,
		})
	}
	return items, nil
}

func (a *App) runAgentInboxRead(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	leading, rest := splitLeadingPositionals(args)
	fs := newSilentFlagSet("inbox read")
	if err := fs.Parse(rest); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	ids := append(leading, fs.Args()...)
	if len(ids) != 1 {
		return nil, errnorm.Usage("invalid_args", "usage: anx inbox read <event:ask-id>")
	}
	wanted := strings.TrimSpace(ids[0])
	if !strings.HasPrefix(wanted, "event:") {
		wanted = "event:" + wanted
	}
	asks, err := a.loadOwnInboxAsks(ctx, cfg)
	if err != nil {
		return nil, err
	}
	var answerEventRef string
	for _, ask := range asks {
		if anyString(ask["ask_id"]) == wanted {
			answerEventRef = anyString(asMap(ask["answer"])["response_event_ref"])
			break
		}
	}
	if answerEventRef == "" {
		return nil, errnorm.Usage("not_found", fmt.Sprintf("answered ask %q was not found", wanted))
	}
	notifications, err := a.invokeRawJSON(ctx, cfg, "inbox answer notifications", "GET", "/agent-notifications", nil)
	if err != nil {
		return nil, err
	}
	for _, raw := range asSlice(commandResultBody(notifications)["items"]) {
		item := asMap(raw)
		if anyString(item["trigger_event_id"]) != strings.TrimPrefix(answerEventRef, "event:") && !inboxHasString(stringList(item["related_refs"]), answerEventRef) {
			continue
		}
		if anyString(item["status"]) == "read" || anyString(item["status"]) == "dismissed" {
			return &commandResult{Data: map[string]any{"ask_id": wanted, "answer_event_ref": answerEventRef, "notification_id": item["wakeup_id"], "status": anyString(item["status"]), "already_read": true}}, nil
		}
		result, _, err := a.runNotificationsCommand(ctx, []string{"read", "--wakeup-id", anyString(item["wakeup_id"])}, cfg)
		if err != nil {
			return nil, err
		}
		return &commandResult{Data: map[string]any{"ask_id": wanted, "answer_event_ref": answerEventRef, "notification_id": item["wakeup_id"], "status": "read", "notification": commandResultBody(result)}}, nil
	}
	return nil, errnorm.Usage("not_found", fmt.Sprintf("answer notification for %q was not found", wanted))
}

func inboxHasString(values []string, want string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == strings.TrimSpace(want) {
			return true
		}
	}
	return false
}
