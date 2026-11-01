// Typed resource command dispatch: runTypedResource routes parsed commands to
// contract operations using the runtime preflight and id-lookup spec tables
// kept here, along with small shared query and value helpers. Per-resource
// command families live in sibling *_commands.go files.

package app

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

var (
	threadsListRuntimeFlags = []runtimeCommandFlagSpec{
		{name: "state", kind: preflightFlagString, usage: "Filter by lifecycle state (active, archived, trashed)"},
		{name: "q", kind: preflightFlagString, usage: "Search by thread id or title"},
		{name: "limit", kind: preflightFlagString, usage: "Limit the number of returned threads"},
		{name: "cursor", kind: preflightFlagString, usage: "Pagination cursor from a previous list response"},
		{name: "include-archived", kind: preflightFlagBool, usage: "Include archived threads"},
		{name: "archived-only", kind: preflightFlagBool, usage: "Show only archived threads"},
		{name: "include-trashed", kind: preflightFlagBool, usage: "Include trashed threads"},
		{name: "trashed-only", kind: preflightFlagBool, usage: "Show only trashed threads"},
	}
	artifactsListRuntimeFlags = []runtimeCommandFlagSpec{
		{name: "kind", kind: preflightFlagString, usage: "Filter by artifact kind"},
		{name: "backing-scope", kind: preflightFlagString, usage: "Filter backing artifacts: all, standalone, or backing_only"},
		{name: "thread-id", kind: preflightFlagString, usage: "Filter by thread id"},
		{name: "ids", kind: preflightFlagString, usage: "Comma-separated artifact ids (batch lookup)"},
		{name: "created-before", kind: preflightFlagString, usage: "Filter by created_at upper bound"},
		{name: "created-after", kind: preflightFlagString, usage: "Filter by created_at lower bound"},
		{name: "include-trashed", kind: preflightFlagBool, usage: "Include trashed artifacts"},
		{name: "trashed-only", kind: preflightFlagBool, usage: "Show only trashed artifacts"},
		{name: "include-archived", kind: preflightFlagBool, usage: "Include archived artifacts"},
		{name: "archived-only", kind: preflightFlagBool, usage: "Show only archived artifacts"},
	}
	inboxGetRuntimeFlags = []runtimeCommandFlagSpec{
		{name: "id", kind: preflightFlagString, usage: "Inbox item id or alias"},
		{name: "inbox-id", kind: preflightFlagString, usage: "Alias for --id"},
		{name: "inbox-item-id", kind: preflightFlagString, usage: "Inbox item id or alias"},
	}
	cardsListRuntimeFlags = []runtimeCommandFlagSpec{
		{name: "board", kind: preflightFlagString, usage: "Board ref, handle, or id; uses the board-scoped card list"},
		{name: "board-id", kind: preflightFlagString, usage: "Board id; equivalent to --board for scripts"},
		{name: "include-archived", kind: preflightFlagBool, usage: "Include archived cards"},
		{name: "archived-only", kind: preflightFlagBool, usage: "Show only archived cards"},
		{name: "include-trashed", kind: preflightFlagBool, usage: "Include trashed cards"},
		{name: "trashed-only", kind: preflightFlagBool, usage: "Show only trashed cards"},
	}
)

func resourceRuntimePreflightSpecs() map[string]map[string]preflightFlagSpec {
	return map[string]map[string]preflightFlagSpec{
		"threads list":   preflightSpecsFromRuntimeFlags(threadsListRuntimeFlags),
		"artifacts list": preflightSpecsFromRuntimeFlags(artifactsListRuntimeFlags),
		"inbox get":      preflightSpecsFromRuntimeFlags(inboxGetRuntimeFlags),
		"cards list":     preflightSpecsFromRuntimeFlags(cardsListRuntimeFlags),
	}
}

type resourceIDLookupSpec struct {
	idLabel        string
	resource       string
	resourcePlural string
	listCommand    string
	listCommandID  string
	listField      string
	idFieldPath    []string
	notFoundHints  []string
}

var (
	threadIDLookupSpec = resourceIDLookupSpec{
		idLabel:        "thread id",
		resource:       "thread",
		resourcePlural: "threads",
		listCommand:    "threads list",
		listCommandID:  "threads.list",
		listField:      "threads",
		notFoundHints:  []string{"thread not found"},
	}
	topicIDLookupSpec = resourceIDLookupSpec{
		idLabel:        "topic id",
		resource:       "topic",
		resourcePlural: "topics",
		listCommand:    "topics list",
		listCommandID:  "topics.list",
		listField:      "topics",
		notFoundHints:  []string{"topic not found"},
	}
	cardIDLookupSpec = resourceIDLookupSpec{
		idLabel:        "card id",
		resource:       "card",
		resourcePlural: "cards",
		listCommand:    "cards list",
		listCommandID:  "cards.list",
		listField:      "cards",
		notFoundHints:  []string{"card not found", "board card not found"},
	}
	artifactIDLookupSpec = resourceIDLookupSpec{
		idLabel:        "artifact id",
		resource:       "artifact",
		resourcePlural: "artifacts",
		listCommand:    "artifacts list",
		listCommandID:  "artifacts.list",
		listField:      "artifacts",
		notFoundHints:  []string{"artifact not found", "artifact content not found"},
	}
	boardIDLookupSpec = resourceIDLookupSpec{
		idLabel:        "board id",
		resource:       "board",
		resourcePlural: "boards",
		listCommand:    "boards list",
		listCommandID:  "boards.list",
		listField:      "boards",
		idFieldPath:    []string{"board", "id"},
		notFoundHints:  []string{"board not found"},
	}
	boardCardIDLookupSpec = resourceIDLookupSpec{
		idLabel:        "card id",
		resource:       "card",
		resourcePlural: "cards",
		listCommand:    "boards cards list",
		listCommandID:  "boards.cards.list",
		listField:      "cards",
		notFoundHints:  []string{"board not found", "board or card not found", "board card not found"},
	}
	documentIDLookupSpec = resourceIDLookupSpec{
		idLabel:        "document id",
		resource:       "document",
		resourcePlural: "documents",
		listCommand:    "docs list",
		listCommandID:  "docs.list",
		listField:      "documents",
		notFoundHints:  []string{"document not found"},
	}
	eventIDLookupSpec = resourceIDLookupSpec{
		idLabel:        "event id",
		resource:       "event",
		resourcePlural: "events",
		listCommand:    "events list",
		listCommandID:  "events.list",
		listField:      "events",
		notFoundHints:  []string{"event not found"},
	}
)

func threadsMutationUnsupportedErr(subcmd string) error {
	subcmd = strings.TrimSpace(subcmd)
	return errnorm.Usage(
		"unsupported_command",
		fmt.Sprintf(
			"`anx threads %s` is not supported: backing threads are read-only in the current contract. Use `anx topics ...`, `anx cards ...`, `anx boards ...`, or `anx events create` for writes. For reads, prefer `anx topics workspace` when you have a topic id; use `anx threads list`, `anx threads get`, `anx threads inspect`, `anx threads workspace` (diagnostic projection), or `anx threads timeline` for backing-thread tooling.",
			subcmd,
		),
	)
}

type queryParam struct {
	name   string
	values []string
}

func (a *App) runTypedResource(ctx context.Context, resource string, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 || (len(args) > 0 && isHelpToken(args[0])) {
		if text, ok := generatedHelpText(resource); ok {
			return &commandResult{Text: text}, resource, nil
		}
	}
	if len(args) > 1 && isHelpToken(args[1]) {
		if text, ok := generatedHelpText(resource + " " + strings.TrimSpace(args[0])); ok {
			return &commandResult{Text: text}, resource + " " + strings.TrimSpace(args[0]), nil
		}
	}

	switch resource {
	case "actors":
		return a.runActorsCommand(ctx, args, cfg)
	case "threads":
		return a.runThreadsCommand(ctx, args, cfg)
	case "topics":
		return a.runTopicsCommand(ctx, args, cfg)
	case "ref-edges":
		return a.runRefEdgesCommand(ctx, args, cfg)
	case "cards":
		return a.runCardsCommand(ctx, args, cfg)
	case "artifacts":
		return a.runArtifactsCommand(ctx, args, cfg)
	case "boards":
		return a.runBoardsCommand(ctx, args, cfg)
	case "workspace":
		return a.runWorkspaceCommand(ctx, args, cfg)
	case "docs":
		return a.runDocsCommand(ctx, args, cfg)
	case "events":
		return a.runEventsCommand(ctx, args, cfg)
	case "inbox":
		return a.runInboxCommand(ctx, args, cfg, false)
	case "derived":
		return a.runDerivedCommand(ctx, args, cfg)
	default:
		return nil, resource, errnorm.Usage("unknown_command", fmt.Sprintf("unknown command %q", resource))
	}
}

func commandResultBody(result *commandResult) map[string]any {
	if result == nil {
		return nil
	}
	data := asMap(result.Data)
	return asMap(data["body"])
}

func (a *App) runActorsCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 || isHelpToken(args[0]) {
		if text, ok := generatedHelpText("actors"); ok {
			return &commandResult{Text: text}, "actors", nil
		}
		return nil, "actors", actorsSubcommandSpec.requiredError()
	}
	sub := actorsSubcommandSpec.normalize(args[0])
	switch sub {
	case "list":
		fs := newSilentFlagSet("actors list")
		var queryFlag, cursorFlag trackedString
		var limitFlag trackedInt
		fs.Var(&queryFlag, "q", "Search by actor id or display name")
		fs.Var(&limitFlag, "limit", "Limit the number of returned actors")
		fs.Var(&cursorFlag, "cursor", "Pagination cursor from a previous list response")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "actors list", errnorm.Usage("invalid_flags", err.Error())
		}
		if len(fs.Args()) > 0 {
			return nil, "actors list", errnorm.Usage("invalid_args", "unexpected positional arguments for `anx actors list`")
		}
		if limitFlag.set && (limitFlag.value < 1 || limitFlag.value > 1000) {
			return nil, "actors list", errnorm.Usage("invalid_request", "limit must be between 1 and 1000")
		}
		query := make([]queryParam, 0, 3)
		addSingleQuery(&query, "q", queryFlag.value)
		if limitFlag.set {
			addSingleQuery(&query, "limit", strconv.Itoa(limitFlag.value))
		}
		addSingleQuery(&query, "cursor", cursorFlag.value)
		result, err := a.invokeTypedJSON(ctx, cfg, "actors list", "actors.list", nil, query, nil)
		return result, "actors list", err
	case "create":
		fs := newSilentFlagSet("actors create")
		var idFlag, displayNameFlag, createdAtFlag trackedString
		var tagsFlag trackedStrings
		fs.Var(&idFlag, "id", "Actor id")
		fs.Var(&displayNameFlag, "display-name", "Actor display name")
		fs.Var(&createdAtFlag, "created-at", "Actor created_at timestamp (RFC3339)")
		fs.Var(&tagsFlag, "tag", "Actor tag (repeatable)")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "actors create", errnorm.Usage("invalid_flags", err.Error())
		}
		if len(fs.Args()) > 0 {
			return nil, "actors create", errnorm.Usage("invalid_args", "unexpected positional arguments for `anx actors create`")
		}
		if strings.TrimSpace(idFlag.value) == "" {
			return nil, "actors create", errnorm.Usage("invalid_request", "id is required")
		}
		if strings.TrimSpace(displayNameFlag.value) == "" {
			return nil, "actors create", errnorm.Usage("invalid_request", "display-name is required")
		}
		createdAt := strings.TrimSpace(createdAtFlag.value)
		if createdAt == "" {
			return nil, "actors create", errnorm.Usage("invalid_request", "created-at is required")
		}
		if _, err := time.Parse(time.RFC3339, createdAt); err != nil {
			return nil, "actors create", errnorm.Usage("invalid_request", "created-at must be an RFC3339 datetime string")
		}
		body := map[string]any{
			"actor": map[string]any{
				"id":           strings.TrimSpace(idFlag.value),
				"display_name": strings.TrimSpace(displayNameFlag.value),
				"created_at":   createdAt,
			},
		}
		if len(tagsFlag.values) > 0 {
			body["actor"].(map[string]any)["tags"] = tagsFlag.values
		}
		result, err := a.invokeTypedJSON(ctx, cfg, "actors create", "actors.create", nil, nil, body)
		return result, "actors create", err
	default:
		return nil, "actors", actorsSubcommandSpec.unknownError(args[0])
	}
}

func (a *App) runPacketsCreateCommand(ctx context.Context, resource string, commandID string, args []string, cfg config.Resolved) (*commandResult, string, error) {
	spec := packetCreateSubcommandSpec(resource)
	if len(args) == 0 {
		return nil, resource, spec.requiredError()
	}
	if spec.normalize(args[0]) != "create" {
		return nil, resource, spec.unknownError(args[0])
	}
	body, err := a.parseJSONBodyInput(args[1:], resource+" create")
	if err != nil {
		return nil, resource + " create", err
	}
	result, callErr := a.invokeTypedJSON(ctx, cfg, resource+" create", commandID, nil, nil, body)
	return result, resource + " create", callErr
}

func (a *App) runDerivedCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "derived", derivedSubcommandSpec.requiredError()
	}
	if derivedSubcommandSpec.normalize(args[0]) != "rebuild" {
		return nil, "derived", derivedSubcommandSpec.unknownError(args[0])
	}
	body, err := a.parseDerivedRebuildBodyInput(args[1:], cfg)
	if err != nil {
		return nil, "derived rebuild", err
	}
	result, callErr := a.invokeTypedJSON(ctx, cfg, "derived rebuild", "derived.rebuild", nil, nil, body)
	return result, "derived rebuild", callErr
}

func uniqueStrings(values []string) []any {
	seen := make(map[string]struct{}, len(values))
	out := make([]any, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func minInt(a int, b int) int {
	if a <= b {
		return a
	}
	return b
}

func intValue(raw any) int {
	switch typed := raw.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func headerValues(raw any) map[string][]string {
	if typed, ok := raw.(map[string][]string); ok {
		return typed
	}
	if typed, ok := raw.(map[string]any); ok {
		out := make(map[string][]string, len(typed))
		for key, value := range typed {
			items, _ := value.([]any)
			if len(items) == 0 {
				continue
			}
			list := make([]string, 0, len(items))
			for _, item := range items {
				text := strings.TrimSpace(anyString(item))
				if text == "" {
					continue
				}
				list = append(list, text)
			}
			if len(list) > 0 {
				out[key] = list
			}
		}
		return out
	}
	return nil
}

func sortedMapKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func addSingleQuery(out *[]queryParam, name string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	*out = append(*out, queryParam{name: name, values: []string{value}})
}

func addMultiQuery(out *[]queryParam, name string, values []string) {
	clean := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		clean = append(clean, value)
	}
	if len(clean) == 0 {
		return
	}
	*out = append(*out, queryParam{name: name, values: clean})
}

func queryValuesFromParams(query []queryParam) map[string][]string {
	values := make(map[string][]string, len(query))
	for _, param := range query {
		if len(param.values) == 0 {
			continue
		}
		values[param.name] = append([]string(nil), param.values...)
	}
	return values
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
