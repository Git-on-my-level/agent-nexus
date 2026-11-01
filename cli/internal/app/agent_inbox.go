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
	var limit trackedInt
	var cursor trackedString
	fs.Var(&status, "status", "Filter my asks: open, answered, or all (default open)")
	fs.Var(&unread, "unread", "Show only answers with an unread wake notification")
	fs.Var(&limit, "limit", "Newest asks to read per request, 1..200 (default 50)")
	fs.Var(&cursor, "cursor", "Continue a previous listing from its next_cursor")
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
	pageSize := 50
	if limit.set {
		pageSize = limit.value
	}
	if pageSize < 1 || pageSize > 200 {
		return nil, errnorm.Usage("invalid_request", "--limit must be 1..200; continue with --cursor for more")
	}
	// One bounded page per request; the result carries the continuation cursor
	// instead of looping over the whole ask history.
	page, nextCursor, err := a.fetchOwnAsksPage(ctx, cfg, strings.TrimSpace(cursor.value), pageSize)
	if err != nil {
		return nil, err
	}
	items := page
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
		if delivery := anyString(item["delivery"]); delivery != "" {
			line += fmt.Sprintf(" delivery=%s", delivery)
		}
		if item["answer_unread"] == true {
			line += " answer_unread=true"
		}
		if answer := asMap(item["answer"]); answer != nil {
			line += fmt.Sprintf(" outcome=%s answer=%q", anyString(answer["outcome"]), anyString(answer["text"]))
		}
		lines = append(lines, line)
	}
	data := map[string]any{"items": filtered, "count": len(filtered), "status": statusValue, "unread_only": unread.value, "matched": len(items), "page_size": pageSize}
	if nextCursor != "" {
		data["next_cursor"] = nextCursor
		data["next"] = []string{"anx", "inbox", "list", "--status", statusValue, "--limit", fmt.Sprint(pageSize), "--cursor", nextCursor}
		lines = append(lines, fmt.Sprintf("more: anx inbox list --status %s --limit %d --cursor %s", statusValue, pageSize, nextCursor))
	}
	return &commandResult{Data: data, Text: strings.Join(lines, "\n")}, nil
}

// ownAsksLoadBound caps how many pages the caller may accumulate implicitly.
// Every read is one server page of at most 200 rows, so the bound is on
// client-side accumulation, never on a single request.
const ownAsksLoadBound = 5

// maxOwnAsksLookupPages caps `inbox read`, which scans until it finds its ask.
// Ten pages cover the 2000 most recent asks; older asks surface through the
// paginated list instead of an unbounded client loop.
const maxOwnAsksLookupPages = 10

func (a *App) resolveOwnAsksActor(ctx context.Context, cfg config.Resolved) (config.Resolved, string, error) {
	agent, _, err := a.dailyAgent(ctx, cfg)
	if err != nil {
		return cfg, "", err
	}
	actorID := strings.TrimSpace(anyString(agent["actor_id"]))
	if actorID == "" {
		return cfg, "", errnorm.Usage("identity_unresolved", "active agent has no actor id")
	}
	return cfg, actorID, nil
}

// fetchOwnAsksPage reads exactly one page of the caller's asks.
func (a *App) fetchOwnAsksPage(ctx context.Context, cfg config.Resolved, cursor string, limit int) ([]map[string]any, string, error) {
	cfg, actorID, err := a.resolveOwnAsksActor(ctx, cfg)
	if err != nil {
		return nil, "", err
	}
	return a.fetchOwnAsksPageForActor(ctx, cfg, actorID, cursor, limit)
}

func (a *App) fetchOwnAsksPageForActor(ctx context.Context, cfg config.Resolved, actorID, cursor string, limit int) ([]map[string]any, string, error) {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, "", errnorm.Usage("identity_unresolved", "active agent has no actor id")
	}
	if limit < 1 || limit > 200 {
		limit = 200
	}
	query := url.Values{"limit": []string{fmt.Sprint(limit)}}
	if cursor = strings.TrimSpace(cursor); cursor != "" {
		query.Set("cursor", cursor)
	}
	result, err := a.invokeRawJSON(ctx, cfg, "inbox asks", "GET", "/agent-inbox/asks?"+query.Encode(), nil)
	if err != nil {
		return nil, "", err
	}
	body := commandResultBody(result)
	items := make([]map[string]any, 0, limit)
	for _, raw := range asSlice(body["items"]) {
		if item := asMap(raw); item != nil {
			items = append(items, item)
		}
	}
	next := strings.TrimSpace(anyString(asMap(body["page_info"])["next_cursor"]))
	if next == cursor {
		next = ""
	}
	return items, next, nil
}

// loadOwnInboxAsksForActor accumulates at most ownAsksLoadBound pages.
// Callers that need everything must paginate explicitly; no CLI read path
// loops over the ask history without a bound.
func (a *App) loadOwnInboxAsksForActor(ctx context.Context, cfg config.Resolved, actorID string) ([]map[string]any, error) {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, errnorm.Usage("identity_unresolved", "active agent has no actor id")
	}
	items := make([]map[string]any, 0)
	cursor := ""
	seenCursors := map[string]bool{}
	for page := 0; page < ownAsksLoadBound; page++ {
		batch, next, err := a.fetchOwnAsksPageForActor(ctx, cfg, actorID, cursor, 200)
		if err != nil {
			return nil, err
		}
		items = append(items, batch...)
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
	var force trackedBool
	fs.Var(&force, "force", "Mark an answer read even though another session of this agent filed the ask")
	if err := fs.Parse(rest); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	ids := append(leading, fs.Args()...)
	if len(ids) != 1 {
		return nil, errnorm.Usage("invalid_args", "usage: anx inbox read <event:ask-id> [--force]")
	}
	wanted := normalizeAskRef(ids[0])
	if store, err := a.answerWaitStore(cfg); err == nil && !force.value {
		if _, other := store.origin(wanted); other {
			return nil, otherSessionAnswerError(wanted)
		}
	}
	cfg, actorID, err := a.resolveOwnAsksActor(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// Bounded lookup: scan newest-first and stop at the ask, or after
	// maxOwnAsksLookupPages pages; the paginated list covers older history.
	var answerEventID string
	scanned := 0
	cursor := ""
	for page := 0; page < maxOwnAsksLookupPages; page++ {
		batch, next, pageErr := a.fetchOwnAsksPageForActor(ctx, cfg, actorID, cursor, 200)
		if pageErr != nil {
			return nil, pageErr
		}
		scanned += len(batch)
		found := false
		for _, ask := range batch {
			if anyString(ask["ask_id"]) == wanted {
				answerEventID = anyString(asMap(ask["answer"])["response_event_id"])
				found = true
				break
			}
		}
		if found || next == "" {
			break
		}
		cursor = next
	}
	if answerEventID == "" {
		return nil, errnorm.Usage("not_found", fmt.Sprintf("answered ask %q was not found in the %d most recent asks; use anx inbox list --cursor to page through older history", wanted, scanned))
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
