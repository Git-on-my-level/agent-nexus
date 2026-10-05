package app

import (
	"context"
	"fmt"
	"net/url"
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
	case "summary":
		result, err := a.runInboxSummary(ctx, args[1:], cfg)
		return result, "inbox summary", err
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
	if !status.set && unread.value {
		statusValue = "answered"
	} else if statusValue == "" {
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
	return a.loadOwnInboxAsksForActor(ctx, cfg, actorID)
}

func (a *App) loadOwnInboxAsksForActor(ctx context.Context, cfg config.Resolved, actorID string) ([]map[string]any, error) {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, errnorm.Usage("identity_unresolved", "active agent has no actor id")
	}
	items := make([]map[string]any, 0)
	cursor := ""
	seenCursors := map[string]bool{}
	for {
		query := url.Values{"limit": []string{"200"}}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		result, err := a.invokeRawJSON(ctx, cfg, "inbox asks", "GET", "/agent-inbox/asks?"+query.Encode(), nil)
		if err != nil {
			return nil, err
		}
		body := commandResultBody(result)
		for _, raw := range asSlice(body["items"]) {
			if item := asMap(raw); item != nil {
				items = append(items, item)
			}
		}
		pageInfo := asMap(body["page_info"])
		next := strings.TrimSpace(anyString(pageInfo["next_cursor"]))
		if next == "" {
			break
		}
		if seenCursors[next] || next == cursor {
			return nil, errnorm.New(errnorm.KindInternal, "invalid_pagination", "agent inbox pagination returned a repeated cursor")
		}
		seenCursors[next] = true
		cursor = next
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
	var answerEventID string
	for _, ask := range asks {
		if anyString(ask["ask_id"]) == wanted {
			answerEventID = anyString(asMap(ask["answer"])["response_event_id"])
			break
		}
	}
	if answerEventID == "" {
		return nil, errnorm.Usage("not_found", fmt.Sprintf("answered ask %q was not found", wanted))
	}
	marked, err := a.invokeRawJSON(ctx, cfg, "inbox answer read", "POST", "/agent-inbox/answers/read", map[string]any{"answer_event_id": answerEventID})
	if err != nil {
		return nil, err
	}
	answer := asMap(commandResultBody(marked)["answer"])
	return &commandResult{Data: map[string]any{
		"ask_id": wanted, "answer_event_ref": "event:" + answerEventID,
		"status": "read", "already_read": answer["already_read"], "answer": answer,
	}}, nil
}

func parseInboxSummary(args []string) (int, error) {
	fs := newSilentFlagSet("inbox summary")
	limit := fs.Int("limit", 5, "Top visible human asks (0..50); zero returns the count only")
	if err := fs.Parse(args); err != nil {
		return 0, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) != 0 || *limit < 0 || *limit > 50 {
		return 0, errnorm.Usage("invalid_args", "usage: anx inbox summary [--limit 0..50]")
	}
	return *limit, nil
}

func (a *App) runInboxSummary(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	limit, err := parseInboxSummary(args)
	if err != nil {
		return nil, err
	}
	return a.invokeTypedJSON(ctx, cfg, "inbox summary", "inbox.summary", nil, []queryParam{{name: "limit", values: []string{fmt.Sprint(limit)}}}, nil)
}
