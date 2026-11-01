// `anx inbox` dispatch: list, get and stream, with inbox alias resolution and
// client-side inbox list filtering.

package app

import (
	"context"
	"crypto/sha1"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

const inboxAliasPrefix = "ibx_"

const inboxAliasDigestLength = 12

func (a *App) runInboxCommand(ctx context.Context, args []string, cfg config.Resolved, debug bool) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "inbox", inboxSubcommandSpec.requiredError()
	}
	sub := inboxSubcommandSpec.normalize(args[0])
	switch sub {
	case "summary":
		result, err := a.runInboxSummary(ctx, args[1:], cfg)
		return result, "inbox summary", err

	case "list":
		result, err := a.runInboxList(ctx, args[1:], cfg)
		return result, "inbox list", err
	case "get":
		result, commandName, err := a.runInboxGet(ctx, args[1:], cfg)
		return result, commandName, err
	case "respond":
		body, err := a.parseRespondBodyInput(args[1:], cfg)
		if err != nil {
			return nil, "inbox respond", err
		}
		bodyMap, ok := body.(map[string]any)
		if !ok {
			return nil, "inbox respond", errnorm.Usage("invalid_request", "inbox respond body must be a JSON object")
		}
		inboxItemID := strings.TrimSpace(anyString(bodyMap["inbox_item_id"]))
		if inboxItemID == "" {
			return nil, "inbox respond", errnorm.Usage("invalid_request", "inbox_item_id is required")
		}
		resolvedInboxItemID := inboxItemID
		if match, err := a.resolveInboxItemFromList(ctx, cfg, inboxItemID); err == nil {
			resolvedInboxItemID = match.ID
		} else if looksLikeInboxAlias(inboxItemID) {
			return nil, "inbox respond", err
		}
		apiBody := make(map[string]any, len(bodyMap))
		for k, v := range bodyMap {
			if k == "inbox_item_id" {
				continue
			}
			apiBody[k] = v
		}
		result, callErr := a.invokeTypedJSON(
			ctx,
			cfg,
			"inbox respond",
			"inbox.respond",
			map[string]string{"inbox_id": resolvedInboxItemID},
			nil,
			apiBody,
		)
		return result, "inbox respond", callErr
	case "read":
		result, err := a.runAgentInboxRead(ctx, args[1:], cfg)
		return result, "inbox read", err
	case "stream":
		commandName := "inbox stream"
		if debug {
			commandName = "debug inbox stream"
		}
		result, err := a.runInboxStream(ctx, args[1:], cfg, commandName, false)
		return result, "inbox stream", err
	case "tail":
		commandName := "inbox stream"
		if debug {
			commandName = "debug inbox stream"
		}
		result, err := a.runInboxStream(ctx, args[1:], cfg, commandName, true)
		return result, "inbox stream", err
	default:
		return nil, "inbox", inboxSubcommandSpec.unknownError(args[0])
	}
}

func (a *App) runInboxList(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("inbox list")
	var threadIDFlags trackedStrings
	var typeFlags trackedStrings
	var fullIDFlag trackedBool
	fs.Var(&threadIDFlags, "thread-id", "Filter by thread id (repeatable)")
	fs.Var(&typeFlags, "type", "Filter by inbox kind: ask, review, or escalate (repeatable)")
	fs.Var(&fullIDFlag, "full-id", "(debug/admin) Render full inbox ids in default text output (non-JSON)")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx inbox list`")
	}

	threadIDs := normalizeIDFilters(threadIDFlags.values)
	for _, threadID := range threadIDs {
		if err := validateID(threadID, "thread id"); err != nil {
			return nil, err
		}
	}
	if len(threadIDs) > 0 {
		resolvedThreadIDs, err := a.resolveThreadIDFilters(ctx, cfg, threadIDs)
		if err != nil {
			return nil, err
		}
		threadIDs = resolvedThreadIDs
	}
	typeFilters, err := normalizeInboxListTypeFilters(normalizeStringFilters(typeFlags.values))
	if err != nil {
		return nil, err
	}

	result, err := a.invokeTypedJSON(ctx, cfg, "inbox list", "inbox.list", nil, nil, nil)
	if err != nil {
		return nil, err
	}
	decorateInboxListResult(result, cfg)
	data := asMap(result.Data)
	body := asMap(data["body"])
	if body == nil {
		return result, nil
	}
	filteredBody := cloneMap(body)
	filteredItems := filteredInboxItems(asSlice(body["items"]), threadIDs, typeFilters)
	filteredBody["items"] = filteredItems
	filteredBody["returned_items"] = len(filteredItems)
	filteredBody["total_items"] = len(asSlice(body["items"]))
	if len(threadIDs) == 1 {
		filteredBody["thread_id"] = threadIDs[0]
	} else if len(threadIDs) > 1 {
		filteredBody["thread_ids"] = threadIDs
	}
	if len(typeFilters) > 0 {
		filteredBody["types"] = typeFilters
	}
	if fullIDFlag.set && fullIDFlag.value {
		filteredBody["full_id"] = true
	}

	data["body"] = filteredBody
	result.Data = data
	result.Text = formatTypedCommandText(
		"inbox.list",
		intValue(data["status_code"]),
		headerValues(data["headers"]),
		filteredBody,
		cfg.Verbose,
		cfg.Headers,
	)
	return result, nil
}

func decorateInboxListResult(result *commandResult, cfg config.Resolved) {
	if result == nil {
		return
	}
	data := asMap(result.Data)
	if data == nil {
		return
	}
	body := asMap(data["body"])
	if body == nil {
		return
	}
	enrichedBody := cloneMap(body)
	enrichInboxListBody(enrichedBody, cfg)
	data["body"] = enrichedBody
	result.Data = data
	result.Text = formatTypedCommandText(
		"inbox.list",
		intValue(data["status_code"]),
		headerValues(data["headers"]),
		enrichedBody,
		cfg.Verbose,
		cfg.Headers,
	)
}

func enrichInboxListBody(body map[string]any, cfg config.Resolved) bool {
	if body == nil {
		return false
	}
	changed := false
	viewing := viewingAsData(cfg)
	if len(viewing) > 0 {
		body["viewing_as"] = viewing
		changed = true
	}
	return changed
}

func (a *App) runInboxGet(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	fs := newSilentFlagSet("inbox get")
	var idFlag, inboxIDFlag, inboxItemIDFlag trackedString
	registerRuntimeStringFlag(fs, &idFlag, runtimeFlagSpec(inboxGetRuntimeFlags, "id"))
	registerRuntimeStringFlag(fs, &inboxIDFlag, runtimeFlagSpec(inboxGetRuntimeFlags, "inbox-id"))
	registerRuntimeStringFlag(fs, &inboxItemIDFlag, runtimeFlagSpec(inboxGetRuntimeFlags, "inbox-item-id"))
	if err := fs.Parse(args); err != nil {
		return nil, "inbox get", errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()

	rawID := firstNonEmpty(strings.TrimSpace(idFlag.value), strings.TrimSpace(inboxIDFlag.value), strings.TrimSpace(inboxItemIDFlag.value))
	if rawID == "" && len(positionals) > 0 {
		rawID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return nil, "inbox get", errnorm.Usage("invalid_args", "unexpected positional arguments for `anx inbox get`; use `--id <id-or-alias>`")
	}

	if rawID == "" {
		listResult, err := a.invokeTypedJSON(ctx, cfg, "inbox list", "inbox.list", nil, nil, nil)
		if err != nil {
			return nil, "inbox get", err
		}
		decorateInboxListResult(listResult, cfg)
		return listResult, "inbox list", nil
	}
	if err := validateID(rawID, "inbox item id"); err != nil {
		return nil, "inbox get", err
	}
	if strings.HasPrefix(rawID, "event:") {
		eventID := strings.TrimPrefix(rawID, "event:")
		result, err := a.invokeRawJSON(ctx, cfg, "inbox ask get", http.MethodGet, "/events/"+url.PathEscape(eventID), nil)
		if err != nil {
			return nil, "inbox get", err
		}
		event := asMap(commandResultBody(result)["event"])
		if anyString(event["type"]) != humanAttentionRequestedEventType {
			return nil, "inbox get", errnorm.Usage("invalid_request", "event id does not identify an ask; pass an inbox item id or ask event id")
		}
		return &commandResult{Data: map[string]any{"ask_id": rawID, "event": event}}, "inbox get", nil
	}

	listResult, err := a.invokeTypedJSON(ctx, cfg, "inbox list", "inbox.list", nil, nil, nil)
	if err != nil {
		return nil, "inbox get", err
	}
	match, err := resolveInboxItemFromListResult(listResult, rawID)
	if err != nil {
		return nil, "inbox get", err
	}

	result, callErr := a.invokeTypedJSON(
		ctx,
		cfg,
		"inbox get",
		"inbox.get",
		map[string]string{"inbox_item_id": match.ID},
		nil,
		nil,
	)
	return result, "inbox get", callErr
}

func (a *App) runInboxStream(ctx context.Context, args []string, cfg config.Resolved, commandName string, defaultFollow bool) (*commandResult, error) {
	fs := newSilentFlagSet(commandName)
	var lastEventIDFlag, cursorFlag trackedString
	var followFlag trackedBool
	var maxEventsFlag trackedInt
	fs.Var(&followFlag, "follow", "Keep stream open and reconnect when it drops")
	fs.Var(&lastEventIDFlag, "last-event-id", "Resume stream after this event id")
	fs.Var(&cursorFlag, "cursor", "Alias of --last-event-id")
	fs.Var(&maxEventsFlag, "max-events", "Exit after receiving N events (0 means unlimited)")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `anx %s`", commandName))
	}

	lastEventID := firstNonEmpty(lastEventIDFlag.value, cursorFlag.value)
	follow := defaultFollow
	if followFlag.set {
		follow = followFlag.value
	}
	return a.runTailStream(ctx, cfg, commandName, "inbox.stream", nil, lastEventID, follow, maxEventsFlag.value)
}

func looksLikeInboxAlias(raw string) bool {
	raw = strings.ToLower(strings.TrimSpace(raw))
	return strings.HasPrefix(raw, inboxAliasPrefix)
}

func (a *App) resolveInboxItemIDAndThread(ctx context.Context, cfg config.Resolved, inboxItemID string) (string, string, error) {
	match, err := a.resolveInboxItemFromList(ctx, cfg, inboxItemID)
	if err != nil {
		return "", "", err
	}
	subjectRef := strings.TrimSpace(anyString(match.Item["subject_ref"]))
	if subjectRef == "" {
		return "", "", errnorm.Usage(
			"invalid_request",
			fmt.Sprintf("subject_ref is required for inbox item %q (ensure inbox list output includes subject_ref)", match.ID),
		)
	}
	if err := validateTypedRefShape(subjectRef); err != nil {
		return "", "", err
	}
	return match.ID, subjectRef, nil
}

func (a *App) resolveInboxItemFromList(ctx context.Context, cfg config.Resolved, inboxItemID string) (inboxListMatch, error) {
	result, err := a.invokeTypedJSON(ctx, cfg, "inbox list", "inbox.list", nil, nil, nil)
	if err != nil {
		return inboxListMatch{}, err
	}
	match, err := resolveInboxItemFromListResult(result, inboxItemID)
	if err != nil {
		return inboxListMatch{}, err
	}
	return match, nil
}

type inboxListMatch struct {
	ID       string
	Alias    string
	ThreadID string
	Item     map[string]any
}

func resolveInboxItemFromListResult(result *commandResult, rawID string) (inboxListMatch, error) {
	items := listInboxMatches(result)
	if len(items) == 0 {
		return inboxListMatch{}, missingInboxItemIDError(rawID)
	}

	rawID = strings.TrimSpace(rawID)
	rawLower := strings.ToLower(rawID)
	for _, item := range items {
		if item.ID == rawID {
			return item, nil
		}
	}
	for _, item := range items {
		if item.Alias != "" && item.Alias == rawLower {
			return item, nil
		}
	}

	return inboxListMatch{}, missingInboxItemIDError(rawID)
}

func listInboxMatches(result *commandResult) []inboxListMatch {
	if result == nil {
		return nil
	}
	data, _ := result.Data.(map[string]any)
	body, _ := data["body"].(map[string]any)
	if body == nil {
		return nil
	}
	_ = addInboxAliasesToListField(body, "items")
	rawItems, _ := body["items"].([]any)
	if len(rawItems) == 0 {
		return nil
	}
	out := make([]inboxListMatch, 0, len(rawItems))
	seen := make(map[string]struct{}, len(rawItems))
	for _, rawItem := range rawItems {
		item, _ := rawItem.(map[string]any)
		if item == nil {
			continue
		}
		id := strings.TrimSpace(anyString(item["id"]))
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, inboxListMatch{
			ID:       id,
			Alias:    strings.ToLower(strings.TrimSpace(anyString(item["alias"]))),
			ThreadID: strings.TrimSpace(anyString(item["thread_id"])),
			Item:     item,
		})
	}
	return out
}

func missingInboxItemIDError(rawID string) error {
	return errnorm.Usage(
		"invalid_request",
		fmt.Sprintf(
			"inbox item id %q is missing. Run `anx inbox list` and retry with the exact displayed alias.",
			strings.TrimSpace(rawID),
		),
	)
}

func addInboxAliasesToListField(body map[string]any, field string) bool {
	items, _ := body[field].([]any)
	if len(items) == 0 {
		return false
	}

	ids := make([]string, 0, len(items))
	seenIDs := make(map[string]struct{}, len(items))
	for _, rawItem := range items {
		item, _ := rawItem.(map[string]any)
		if item == nil {
			continue
		}
		id := strings.TrimSpace(anyString(item["id"]))
		if id == "" {
			continue
		}
		if _, exists := seenIDs[id]; exists {
			continue
		}
		seenIDs[id] = struct{}{}
		ids = append(ids, id)
	}
	aliasByID := inboxAliasByID(ids)

	changed := false
	for _, rawItem := range items {
		item, _ := rawItem.(map[string]any)
		if item == nil {
			continue
		}
		id := strings.TrimSpace(anyString(item["id"]))
		if id == "" {
			continue
		}
		if applyInboxIdentifiers(item, aliasByID[id]) {
			changed = true
		}
	}
	return changed
}

func addInboxAliasToItemField(body map[string]any, field string) bool {
	item, _ := body[field].(map[string]any)
	if item == nil {
		return false
	}
	id := strings.TrimSpace(anyString(item["id"]))
	if id == "" {
		return false
	}
	alias := inboxAliasByID([]string{id})[id]
	return applyInboxIdentifiers(item, alias)
}

func applyInboxIdentifiers(item map[string]any, alias string) bool {
	changed := false
	if alias != "" {
		if currentAlias := strings.TrimSpace(anyString(item["alias"])); currentAlias != alias {
			item["alias"] = alias
			changed = true
		}
	}
	return changed
}

func inboxAliasByID(ids []string) map[string]string {
	if len(ids) == 0 {
		return map[string]string{}
	}
	aliasByID := make(map[string]string, len(ids))
	for _, id := range ids {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		digest := inboxAliasDigest(trimmed)
		aliasByID[trimmed] = inboxAliasPrefix + digest[:inboxAliasDigestLength]
	}
	return aliasByID
}

func inboxAliasDigest(id string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(id)))
	return fmt.Sprintf("%x", sum)
}

// canonicalInboxKindForCLIFilter reports inbox kinds accepted by `anx inbox --type`.
func canonicalInboxKindForCLIFilter(kind string) (string, bool) {
	kind = strings.TrimSpace(kind)
	switch kind {
	case "ask", "review", "escalate":
		return kind, true
	default:
		return kind, false
	}
}

func normalizeInboxListTypeFilters(types []string) ([]string, error) {
	if len(types) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(types))
	seen := make(map[string]struct{}, len(types))
	invalid := make([]string, 0)
	for _, inboxType := range types {
		canon, ok := canonicalInboxKindForCLIFilter(inboxType)
		if !ok {
			invalid = append(invalid, strings.TrimSpace(inboxType))
			continue
		}
		if _, ok := seen[canon]; ok {
			continue
		}
		seen[canon] = struct{}{}
		out = append(out, canon)
	}
	if len(invalid) > 0 {
		quoted := make([]string, 0, len(invalid))
		for _, value := range invalid {
			if value == "" {
				continue
			}
			quoted = append(quoted, fmt.Sprintf("%q", value))
		}
		if len(quoted) > 0 {
			return nil, errnorm.Usage(
				"invalid_flags",
				fmt.Sprintf("unsupported inbox kind: %s; use ask, review, or escalate", strings.Join(quoted, ", ")),
			)
		}
	}
	return out, nil
}

func filteredInboxItems(items []any, threadIDs []string, types []string) []any {
	if len(items) == 0 {
		return []any{}
	}
	threadFilter := make(map[string]struct{}, len(threadIDs))
	for _, threadID := range threadIDs {
		threadID = strings.TrimSpace(threadID)
		if threadID == "" {
			continue
		}
		threadFilter[threadID] = struct{}{}
	}
	typeFilter := make(map[string]struct{}, len(types))
	for _, inboxType := range types {
		inboxType = strings.TrimSpace(inboxType)
		if inboxType == "" {
			continue
		}
		typeFilter[inboxType] = struct{}{}
	}

	filtered := make([]any, 0, len(items))
	for _, raw := range items {
		item := asMap(raw)
		if item == nil {
			continue
		}
		if len(threadFilter) > 0 {
			threadID := strings.TrimSpace(anyString(item["thread_id"]))
			if _, ok := threadFilter[threadID]; !ok {
				continue
			}
		}
		if len(typeFilter) > 0 {
			canon, ok := canonicalInboxKindForCLIFilter(inboxItemKind(item))
			if !ok {
				continue
			}
			if _, ok := typeFilter[canon]; !ok {
				continue
			}
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func inboxItemKind(item map[string]any) string {
	if item == nil {
		return ""
	}
	return strings.TrimSpace(anyString(item["kind"]))
}
