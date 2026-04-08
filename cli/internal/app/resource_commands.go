// Package app implements CLI commands. Large pieces live in adjacent files:
// resource_transport.go (invokeTypedJSON, headers, commandSpecByID),
// resource_streaming.go (tail stream loop),
// event_reference_preflight.go (embedded contract rules for events create).
package app

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"organization-autorunner-cli/internal/config"
	"organization-autorunner-cli/internal/errnorm"
)

var idPattern = regexp.MustCompile(`^[A-Za-z0-9._:@/-]+$`)

const shortIDLength = 12
const inboxAliasPrefix = "ibx_"
const inboxAliasDigestLength = 12

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
		notFoundHints:  []string{"card not found"},
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
		notFoundHints:  []string{"board not found"},
	}
)

func threadsMutationUnsupportedErr(subcmd string) error {
	subcmd = strings.TrimSpace(subcmd)
	return errnorm.Usage(
		"unsupported_command",
		fmt.Sprintf(
			"`oar threads %s` is not supported: backing threads are read-only in the current contract. Use `oar topics ...`, `oar cards ...`, `oar boards ...`, or `oar events create` for writes. For reads, prefer `oar topics workspace` when you have a topic id; use `oar threads list`, `oar threads get`, `oar threads inspect`, `oar threads workspace` (diagnostic projection), or `oar threads timeline` for backing-thread tooling.",
			subcmd,
		),
	)
}

type queryParam struct {
	name   string
	values []string
}

type threadContextSelection struct {
	threadIDs              []string
	discoveryQuery         []queryParam
	discoveryType          string
	maxEventsSet           bool
	maxEvents              int
	includeArtifactContent bool
	fullID                 bool
}

type threadRecommendationsSelection struct {
	threadContextSelection
	fullSummary                bool
	includeRelatedEventContent bool
}

type eventTypeGuidance struct {
	Type             string
	Group            string
	Summary          string
	Constraints      []string
	PreferredCommand string
}

var eventTypeGroupOrder = []string{
	"Communication",
	"Decisions",
	"Interventions",
	"Topics And Documents",
	"Boards And Cards",
	"Exceptions",
	"Packet Lifecycle",
	"Inbox Lifecycle",
}

var eventTypeGroupDescriptions = map[string]string{
	"Communication":        "Direct communication or important non-structured information.",
	"Decisions":            "Request or record decisions tied to a topic.",
	"Interventions":        "Single clear path exists, but a human must act to complete it.",
	"Topics And Documents": "Durable work-subject and document lifecycle signals.",
	"Boards And Cards":     "Board and card workflow signals.",
	"Exceptions":           "Surface problems, risks, or escalations.",
	"Packet Lifecycle":     "Packet lifecycle facts, usually emitted by higher-level commands.",
	"Inbox Lifecycle":      "Inbox lifecycle facts, usually emitted by higher-level commands.",
}

var knownEventTypeGuidance = []eventTypeGuidance{
	{
		Type:    "message_posted",
		Group:   "Communication",
		Summary: "Use for direct communication that belongs on a backing thread; prefer topic/card/board surfaces as the primary operator nouns.",
		Constraints: []string{
			"thread_id is required when posting directly to a backing thread timeline.",
			"Use this type for messages, replies, or important non-structured information that should read like direct communication on a backing thread.",
			`event.refs may include "event:<parent_event_id>" for replies and "artifact:<artifact_id>" mentions.`,
		},
	},
	{
		Type:             "receipt_added",
		Group:            "Packet Lifecycle",
		PreferredCommand: "oar receipts create",
		Constraints: []string{
			`event.refs must include "artifact:<receipt_artifact_id>" and "card:<card_id>".`,
			`event.payload must include "subject_ref".`,
		},
	},
	{
		Type:             "review_completed",
		Group:            "Packet Lifecycle",
		PreferredCommand: "oar reviews create",
		Constraints: []string{
			`event.refs must include "artifact:<review_artifact_id>", "artifact:<receipt_artifact_id>", and "card:<card_id>".`,
			`event.payload must include "subject_ref".`,
			`Local CLI validation for "oar events create" enforces the bundled event reference rules.`,
		},
	},
	{
		Type:  "decision_needed",
		Group: "Decisions",
		Constraints: []string{
			`event.refs must include "topic:<topic_id>".`,
			`event.refs may include "artifact:<related_id>".`,
		},
	},
	{
		Type:    "intervention_needed",
		Group:   "Interventions",
		Summary: "Use when the next step is clear but a human must perform it.",
		Constraints: []string{
			`event.refs must include "topic:<topic_id>".`,
			`event.refs may include "artifact:<related_id>".`,
		},
	},
	{
		Type:  "decision_made",
		Group: "Decisions",
		Constraints: []string{
			`event.refs must include "topic:<topic_id>".`,
			`event.refs may include "artifact:<decision_artifact_id>".`,
		},
	},
	{
		Type:  "topic_created",
		Group: "Topics And Documents",
		Constraints: []string{
			`event.refs must include "topic:<topic_id>".`,
		},
	},
	{
		Type:  "topic_updated",
		Group: "Topics And Documents",
		Constraints: []string{
			`event.refs must include "topic:<topic_id>".`,
		},
	},
	{
		Type:  "topic_status_changed",
		Group: "Topics And Documents",
		Constraints: []string{
			`event.refs must include "topic:<topic_id>".`,
			`event.payload must include "from_status" and "to_status".`,
		},
	},
	{
		Type:  "document_created",
		Group: "Topics And Documents",
		Constraints: []string{
			`event.refs must include "document:<document_id>", "document_revision:<revision_id>", and "artifact:<artifact_id>".`,
		},
	},
	{
		Type:  "document_revised",
		Group: "Topics And Documents",
		Constraints: []string{
			`event.refs must include "document:<document_id>", "document_revision:<revision_id>", and "artifact:<artifact_id>".`,
		},
	},
	{
		Type:  "document_trashed",
		Group: "Topics And Documents",
		Constraints: []string{
			`event.refs must include "document:<document_id>".`,
		},
	},
	{
		Type:  "board_created",
		Group: "Boards And Cards",
		Constraints: []string{
			`event.refs must include "board:<board_id>".`,
		},
	},
	{
		Type:  "board_updated",
		Group: "Boards And Cards",
		Constraints: []string{
			`event.refs must include "board:<board_id>".`,
		},
	},
	{
		Type:  "card_created",
		Group: "Boards And Cards",
		Constraints: []string{
			`event.refs must include "card:<card_id>" and "board:<board_id>".`,
		},
	},
	{
		Type:  "card_updated",
		Group: "Boards And Cards",
		Constraints: []string{
			`event.refs must include "card:<card_id>" and "board:<board_id>".`,
		},
	},
	{
		Type:  "card_moved",
		Group: "Boards And Cards",
		Constraints: []string{
			`event.refs must include "card:<card_id>" and "board:<board_id>".`,
			`event.payload must include "column_key".`,
		},
	},
	{
		Type:  "card_resolved",
		Group: "Boards And Cards",
		Constraints: []string{
			`event.refs must include "card:<card_id>" and "board:<board_id>".`,
			`event.payload must include "resolution".`,
		},
	},
	{
		Type:  "exception_raised",
		Group: "Exceptions",
		Constraints: []string{
			`event.payload must include "subtype".`,
			"Attach topic/card/document refs when possible so the exception stays connected to the primary resource surface.",
		},
	},
	{
		Type:             "inbox_item_acknowledged",
		Group:            "Inbox Lifecycle",
		PreferredCommand: "oar inbox ack",
		Constraints: []string{
			"Usually created through the inbox acknowledgement flow rather than by authoring a raw event.",
		},
	},
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
	case "docs":
		return a.runDocsCommand(ctx, args, cfg)
	case "events":
		return a.runEventsCommand(ctx, args, cfg)
	case "inbox":
		return a.runInboxCommand(ctx, args, cfg)
	case "receipts":
		return a.runPacketsCreateCommand(ctx, resource, "packets.receipts.create", args, cfg)
	case "reviews":
		return a.runPacketsCreateCommand(ctx, resource, "packets.reviews.create", args, cfg)
	case "derived":
		return a.runDerivedCommand(ctx, args, cfg)
	default:
		return nil, resource, errnorm.Usage("unknown_command", fmt.Sprintf("unknown command %q", resource))
	}
}

func (a *App) runActorsCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
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
			return nil, "actors list", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar actors list`")
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
	case "register":
		fs := newSilentFlagSet("actors register")
		var idFlag, displayNameFlag, createdAtFlag trackedString
		var tagsFlag trackedStrings
		fs.Var(&idFlag, "id", "Actor id")
		fs.Var(&displayNameFlag, "display-name", "Actor display name")
		fs.Var(&createdAtFlag, "created-at", "Actor created_at timestamp (RFC3339)")
		fs.Var(&tagsFlag, "tag", "Actor tag (repeatable)")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "actors register", errnorm.Usage("invalid_flags", err.Error())
		}
		if len(fs.Args()) > 0 {
			return nil, "actors register", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar actors register`")
		}
		if strings.TrimSpace(idFlag.value) == "" {
			return nil, "actors register", errnorm.Usage("invalid_request", "id is required")
		}
		if strings.TrimSpace(displayNameFlag.value) == "" {
			return nil, "actors register", errnorm.Usage("invalid_request", "display-name is required")
		}
		createdAt := strings.TrimSpace(createdAtFlag.value)
		if createdAt == "" {
			return nil, "actors register", errnorm.Usage("invalid_request", "created-at is required")
		}
		if _, err := time.Parse(time.RFC3339, createdAt); err != nil {
			return nil, "actors register", errnorm.Usage("invalid_request", "created-at must be an RFC3339 datetime string")
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
		result, err := a.invokeTypedJSON(ctx, cfg, "actors register", "actors.register", nil, nil, body)
		return result, "actors register", err
	default:
		return nil, "actors", actorsSubcommandSpec.unknownError(args[0])
	}
}

func (a *App) runThreadsCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "threads", threadsSubcommandSpec.requiredError()
	}
	sub := threadsSubcommandSpec.normalize(args[0])
	switch sub {
	case "list":
		fs := newSilentFlagSet("threads list")
		var statusFlag, priorityFlag, staleFlag, queryFlag, cursorFlag trackedString
		var limitFlag trackedInt
		var tagsFlag, cadenceFlag trackedStrings
		var includeArchived, archivedOnly, includeTrashed, trashedOnly bool
		fs.Var(&statusFlag, "status", "Filter by status")
		fs.Var(&priorityFlag, "priority", "Filter by priority")
		fs.Var(&staleFlag, "stale", "Filter by stale state (true/false)")
		fs.Var(&queryFlag, "q", "Search by thread id or title")
		fs.Var(&limitFlag, "limit", "Limit the number of returned threads")
		fs.Var(&cursorFlag, "cursor", "Pagination cursor from a previous list response")
		fs.Var(&tagsFlag, "tag", "Filter by tag (repeatable)")
		fs.Var(&cadenceFlag, "cadence", "Filter by cadence (repeatable)")
		fs.BoolVar(&includeArchived, "include-archived", false, "Include archived threads")
		fs.BoolVar(&archivedOnly, "archived-only", false, "Show only archived threads")
		fs.BoolVar(&includeTrashed, "include-trashed", false, "Include trashed threads")
		fs.BoolVar(&trashedOnly, "trashed-only", false, "Show only trashed threads")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "threads list", errnorm.Usage("invalid_flags", err.Error())
		}
		if len(fs.Args()) > 0 {
			return nil, "threads list", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar threads list`")
		}
		if limitFlag.set && (limitFlag.value < 1 || limitFlag.value > 1000) {
			return nil, "threads list", errnorm.Usage("invalid_request", "limit must be between 1 and 1000")
		}
		query := make([]queryParam, 0, 8)
		addSingleQuery(&query, "status", statusFlag.value)
		addSingleQuery(&query, "priority", priorityFlag.value)
		addSingleQuery(&query, "stale", staleFlag.value)
		addSingleQuery(&query, "q", queryFlag.value)
		if limitFlag.set {
			addSingleQuery(&query, "limit", strconv.Itoa(limitFlag.value))
		}
		addSingleQuery(&query, "cursor", cursorFlag.value)
		addMultiQuery(&query, "tag", tagsFlag.values)
		addMultiQuery(&query, "cadence", cadenceFlag.values)
		if includeArchived {
			query = append(query, queryParam{name: "include_archived", values: []string{"true"}})
		}
		if archivedOnly {
			query = append(query, queryParam{name: "archived_only", values: []string{"true"}})
		}
		if includeTrashed {
			query = append(query, queryParam{name: "include_trashed", values: []string{"true"}})
		}
		if trashedOnly {
			query = append(query, queryParam{name: "trashed_only", values: []string{"true"}})
		}
		result, err := a.invokeTypedJSON(ctx, cfg, "threads list", "threads.list", nil, query, nil)
		return result, "threads list", err
	case "get":
		id, err := parseIDArg(args[1:], "thread-id", "thread id")
		if err != nil {
			return nil, "threads get", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(
			ctx,
			cfg,
			"threads get",
			"threads.inspect",
			"thread_id",
			id,
			threadIDLookupSpec,
			nil,
			nil,
		)
		return result, "threads get", callErr
	case "create":
		return nil, "threads create", threadsMutationUnsupportedErr("create")
	case "patch":
		return nil, "threads patch", threadsMutationUnsupportedErr("patch")
	case "propose-patch":
		return nil, "threads propose-patch", threadsMutationUnsupportedErr("propose-patch")
	case "apply":
		return nil, "threads apply", threadsMutationUnsupportedErr("apply")
	case "timeline":
		fs := newSilentFlagSet("threads timeline")
		var threadIDFlag trackedString
		var includeArchived, archivedOnly, includeTrashed, trashedOnly bool
		fs.Var(&threadIDFlag, "thread-id", "Thread id")
		fs.BoolVar(&includeArchived, "include-archived", false, "Include archived events")
		fs.BoolVar(&archivedOnly, "archived-only", false, "Show only archived events")
		fs.BoolVar(&includeTrashed, "include-trashed", false, "Include trashed events")
		fs.BoolVar(&trashedOnly, "trashed-only", false, "Show only trashed events")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "threads timeline", errnorm.Usage("invalid_flags", err.Error())
		}
		positionals := fs.Args()
		id := strings.TrimSpace(threadIDFlag.value)
		if id == "" && len(positionals) > 0 {
			id = strings.TrimSpace(positionals[0])
			positionals = positionals[1:]
		}
		if err := validateID(id, "thread id"); err != nil {
			return nil, "threads timeline", err
		}
		if len(positionals) > 0 {
			return nil, "threads timeline", errnorm.Usage("invalid_args", "too many positional arguments")
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(
			ctx,
			cfg,
			"threads timeline",
			"threads.timeline",
			"thread_id",
			id,
			threadIDLookupSpec,
			nil,
			nil,
		)
		if callErr != nil {
			return result, "threads timeline", callErr
		}
		data := asMap(result.Data)
		body := asMap(data["body"])
		events := asSlice(body["events"])
		filtered := filterEventsByLifecycleState(events, includeArchived, archivedOnly, includeTrashed, trashedOnly)
		body["events"] = filtered
		data["body"] = body
		result.Data = data
		result.Text = formatTypedCommandText(
			"threads.timeline",
			intValue(data["status_code"]),
			headerValues(data["headers"]),
			body,
			cfg.Verbose,
			cfg.Headers,
		)
		return result, "threads timeline", nil
	case "context":
		result, err := a.runThreadsContextCommand(ctx, args[1:], cfg)
		return result, "threads context", err
	case "inspect":
		result, err := a.runThreadsInspectCommand(ctx, args[1:], cfg)
		return result, "threads inspect", err
	case "workspace":
		result, err := a.runThreadsWorkspaceCommand(ctx, args[1:], cfg)
		return result, "threads workspace", err
	case "review":
		result, err := a.runThreadsReviewCommand(ctx, args[1:], cfg)
		return result, "threads review", err
	case "recommendations":
		result, err := a.runThreadsRecommendationsCommand(ctx, args[1:], cfg)
		return result, "threads recommendations", err
	case "archive":
		return nil, "threads archive", threadsMutationUnsupportedErr("archive")
	case "unarchive":
		return nil, "threads unarchive", threadsMutationUnsupportedErr("unarchive")
	case "trash":
		return nil, "threads trash", threadsMutationUnsupportedErr("trash")
	case "restore":
		return nil, "threads restore", threadsMutationUnsupportedErr("restore")
	case "purge":
		return nil, "threads purge", threadsMutationUnsupportedErr("purge")
	default:
		return nil, "threads", threadsSubcommandSpec.unknownError(args[0])
	}
}

func parseThreadContextSelectionArgs(args []string, commandName string) (threadContextSelection, error) {
	fs := newSilentFlagSet(commandName)
	var threadIDFlags trackedStrings
	var statusFlag, priorityFlag, staleFlag, typeFlag trackedString
	var tagsFlag, cadenceFlag trackedStrings
	var maxEventsFlag trackedInt
	var includeArtifactContentFlag trackedBool
	var fullIDFlag trackedBool
	fs.Var(&threadIDFlags, "thread-id", "Thread id (repeatable)")
	fs.Var(&statusFlag, "status", "Discover threads by status")
	fs.Var(&priorityFlag, "priority", "Discover threads by priority")
	fs.Var(&staleFlag, "stale", "Discover threads by stale state (true/false)")
	fs.Var(&tagsFlag, "tag", "Discover threads by tag (repeatable)")
	fs.Var(&cadenceFlag, "cadence", "Discover threads by cadence (repeatable)")
	fs.Var(&typeFlag, "type", "Discover threads by type (local filter after list)")
	fs.Var(&maxEventsFlag, "max-events", "Maximum recent events to include")
	fs.Var(&includeArtifactContentFlag, "include-artifact-content", "Include key artifact content previews")
	fs.Var(&fullIDFlag, "full-id", "Render full ids in human output")
	if err := fs.Parse(args); err != nil {
		return threadContextSelection{}, errnorm.Usage("invalid_flags", err.Error())
	}

	positionals := append([]string(nil), fs.Args()...)
	threadIDs := make([]string, 0, len(threadIDFlags.values)+len(positionals))
	threadIDs = append(threadIDs, threadIDFlags.values...)
	threadIDs = append(threadIDs, positionals...)
	threadIDs = normalizeIDFilters(threadIDs)

	discoveryQuery := make([]queryParam, 0, 5)
	addSingleQuery(&discoveryQuery, "status", statusFlag.value)
	addSingleQuery(&discoveryQuery, "priority", priorityFlag.value)
	addSingleQuery(&discoveryQuery, "stale", staleFlag.value)
	addMultiQuery(&discoveryQuery, "tag", tagsFlag.values)
	addMultiQuery(&discoveryQuery, "cadence", cadenceFlag.values)

	if maxEventsFlag.set && maxEventsFlag.value < 0 {
		return threadContextSelection{}, errnorm.Usage("invalid_request", "--max-events must be >= 0")
	}

	return threadContextSelection{
		threadIDs:              threadIDs,
		discoveryQuery:         discoveryQuery,
		discoveryType:          strings.TrimSpace(typeFlag.value),
		maxEventsSet:           maxEventsFlag.set,
		maxEvents:              maxEventsFlag.value,
		includeArtifactContent: includeArtifactContentFlag.set && includeArtifactContentFlag.value,
		fullID:                 fullIDFlag.set && fullIDFlag.value,
	}, nil
}

func parseThreadRecommendationsArgs(args []string) (threadRecommendationsSelection, error) {
	fs := newSilentFlagSet("threads recommendations")
	var threadIDFlags trackedStrings
	var statusFlag, priorityFlag, staleFlag, typeFlag trackedString
	var tagsFlag, cadenceFlag trackedStrings
	var maxEventsFlag trackedInt
	var includeArtifactContentFlag trackedBool
	var includeRelatedEventContentFlag trackedBool
	var fullIDFlag, fullSummaryFlag trackedBool

	fs.Var(&threadIDFlags, "thread-id", "Thread id (repeatable)")
	fs.Var(&statusFlag, "status", "Discover threads by status")
	fs.Var(&priorityFlag, "priority", "Discover threads by priority")
	fs.Var(&staleFlag, "stale", "Discover threads by stale state (true/false)")
	fs.Var(&tagsFlag, "tag", "Discover threads by tag (repeatable)")
	fs.Var(&cadenceFlag, "cadence", "Discover threads by cadence (repeatable)")
	fs.Var(&typeFlag, "type", "Discover threads by type (local filter after list)")
	fs.Var(&maxEventsFlag, "max-events", "Maximum recent events to include")
	fs.Var(&includeArtifactContentFlag, "include-artifact-content", "Include key artifact content previews")
	fs.Var(&includeRelatedEventContentFlag, "include-related-event-content", "Hydrate related review items with full events.get payloads")
	fs.Var(&fullIDFlag, "full-id", "Render full ids in human output")
	fs.Var(&fullSummaryFlag, "full-summary", "Show full recommendation summaries in human output")
	if err := fs.Parse(args); err != nil {
		return threadRecommendationsSelection{}, errnorm.Usage("invalid_flags", err.Error())
	}

	positionals := append([]string(nil), fs.Args()...)
	threadIDs := make([]string, 0, len(threadIDFlags.values)+len(positionals))
	threadIDs = append(threadIDs, threadIDFlags.values...)
	threadIDs = append(threadIDs, positionals...)
	threadIDs = normalizeIDFilters(threadIDs)

	discoveryQuery := make([]queryParam, 0, 5)
	addSingleQuery(&discoveryQuery, "status", statusFlag.value)
	addSingleQuery(&discoveryQuery, "priority", priorityFlag.value)
	addSingleQuery(&discoveryQuery, "stale", staleFlag.value)
	addMultiQuery(&discoveryQuery, "tag", tagsFlag.values)
	addMultiQuery(&discoveryQuery, "cadence", cadenceFlag.values)

	if maxEventsFlag.set && maxEventsFlag.value < 0 {
		return threadRecommendationsSelection{}, errnorm.Usage("invalid_request", "--max-events must be >= 0")
	}

	return threadRecommendationsSelection{
		threadContextSelection: threadContextSelection{
			threadIDs:              threadIDs,
			discoveryQuery:         discoveryQuery,
			discoveryType:          strings.TrimSpace(typeFlag.value),
			maxEventsSet:           maxEventsFlag.set,
			maxEvents:              maxEventsFlag.value,
			includeArtifactContent: includeArtifactContentFlag.set && includeArtifactContentFlag.value,
			fullID:                 fullIDFlag.set && fullIDFlag.value,
		},
		fullSummary:                fullSummaryFlag.set && fullSummaryFlag.value,
		includeRelatedEventContent: includeRelatedEventContentFlag.set && includeRelatedEventContentFlag.value,
	}, nil
}

func (a *App) resolveThreadContextSelection(ctx context.Context, cfg config.Resolved, commandName string, selection threadContextSelection, allowMultiple bool) ([]string, error) {
	hasDiscoveryFilters := len(selection.discoveryQuery) > 0 || selection.discoveryType != ""
	if len(selection.threadIDs) > 0 && hasDiscoveryFilters {
		return nil, errnorm.Usage("invalid_request", mixedThreadSelectionMessage(commandName))
	}

	threadIDs := append([]string(nil), selection.threadIDs...)
	if len(threadIDs) == 0 {
		if !hasDiscoveryFilters {
			return nil, errnorm.Usage(
				"invalid_request",
				"thread id is required (provide --thread-id <thread-id>) or use discovery filters (--status/--priority/--stale/--tag/--cadence/--type)",
			)
		}
		listResult, err := a.invokeTypedJSON(ctx, cfg, "threads list", "threads.list", nil, selection.discoveryQuery, nil)
		if err != nil {
			return nil, err
		}
		threadIDs = threadIDsFromThreadsList(listResult, selection.discoveryType)
		if len(threadIDs) == 0 {
			return nil, errnorm.Usage(
				"invalid_request",
				commandName+" discovery returned no matching threads; run `oar threads list` and refine filters",
			)
		}
	}

	for _, threadID := range threadIDs {
		if err := validateID(threadID, "thread id"); err != nil {
			return nil, err
		}
	}
	if allowMultiple {
		return threadIDs, nil
	}
	if len(threadIDs) != 1 {
		return nil, errnorm.Usage(
			"invalid_request",
			fmt.Sprintf("%s requires exactly one thread; refine filters or pass one --thread-id. For operator coordination across topics, use `oar topics list` and `oar topics workspace`. For a multi-thread diagnostic backing projection, use `oar threads workspace` with discovery filters.", commandName),
		)
	}
	return threadIDs, nil
}

func mixedThreadSelectionMessage(commandName string) string {
	base := "--thread-id cannot be combined with discovery filters (--status/--priority/--stale/--tag/--cadence/--type). Choose one mode."
	discoveryExample := "oar threads inspect --status active"
	switch strings.TrimSpace(commandName) {
	case "threads context":
		return base + " For one thread, use `oar threads inspect --thread-id <thread-id>` or `oar threads workspace --thread-id <thread-id>` for backing-thread diagnostics. Prefer `oar topics workspace --topic-id <topic-id>` for primary coordination when you have a topic id. For discovery, remove `--thread-id` and use `" + discoveryExample + "`."
	case "threads recommendations":
		return base + " For one thread, use `oar threads recommendations --thread-id <thread-id>`. For discovery, remove `--thread-id` and use `" + discoveryExample + "`."
	case "threads workspace":
		return base + " For one thread, use `oar threads workspace --thread-id <thread-id>`. For discovery, remove `--thread-id` and use `" + discoveryExample + "`."
	case "threads inspect":
		return base + " For one thread, use `oar threads inspect --thread-id <thread-id>`. For discovery, remove `--thread-id` and use `" + discoveryExample + "`."
	default:
		return base + " For one thread, use `oar " + strings.TrimSpace(commandName) + " --thread-id <thread-id>`. For discovery, remove `--thread-id` and use `" + discoveryExample + "`."
	}
}

func threadContextQuery(selection threadContextSelection) []queryParam {
	query := make([]queryParam, 0, 2)
	if selection.maxEventsSet {
		addSingleQuery(&query, "max_events", fmt.Sprintf("%d", selection.maxEvents))
	}
	if selection.includeArtifactContent {
		addSingleQuery(&query, "include_artifact_content", "true")
	}
	return query
}

func (a *App) runThreadsInspectCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	selection, err := parseThreadContextSelectionArgs(args, "threads inspect")
	if err != nil {
		return nil, err
	}
	threadIDs, err := a.resolveThreadContextSelection(ctx, cfg, "threads inspect", selection, false)
	if err != nil {
		return nil, err
	}

	statusCode, headers, body, callErr := a.loadThreadContextEnvelope(ctx, cfg, threadIDs[0], selection)
	if callErr != nil {
		return nil, callErr
	}

	thread := extractNestedMap(body, "thread")
	if thread == nil {
		thread = map[string]any{}
	}
	resolvedThreadID := firstNonEmpty(strings.TrimSpace(anyString(body["thread_id"])), strings.TrimSpace(anyString(thread["id"])), strings.TrimSpace(threadIDs[0]))
	contextSection := cloneMap(body)
	if contextSection == nil {
		contextSection = map[string]any{}
	}
	contextSection["thread_id"] = resolvedThreadID
	contextSection["thread"] = thread
	contextSection["full_id"] = selection.fullID
	addThreadContextCollaborationSummary(contextSection)

	inboxResult, err := a.invokeTypedJSON(ctx, cfg, "inbox list", "inbox.list", nil, nil, nil)
	if err != nil {
		return nil, err
	}
	inboxData := asMap(inboxResult.Data)
	inboxBody := extractNestedMap(inboxData, "body")
	inboxItems := filteredInboxItems(asSlice(inboxBody["items"]), []string{resolvedThreadID}, nil)

	inspectBody := map[string]any{
		"thread_id":      resolvedThreadID,
		"full_id":        selection.fullID,
		"thread":         thread,
		"context":        contextSection,
		"collaboration":  asMap(contextSection["collaboration_summary"]),
		"inbox":          map[string]any{"thread_id": resolvedThreadID, "items": inboxItems, "count": len(inboxItems), "full_id": selection.fullID},
		"context_source": "threads.context",
		"inbox_source":   "inbox.list",
	}

	data := map[string]any{
		"status_code": statusCode,
		"headers":     headers,
		"body":        inspectBody,
	}
	result := &commandResult{Data: data}
	result.Text = formatTypedCommandText(
		"threads.inspect",
		statusCode,
		headers,
		inspectBody,
		cfg.Verbose,
		cfg.Headers,
	)
	return result, nil
}

func (a *App) runThreadsRecommendationsCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	selection, err := parseThreadRecommendationsArgs(args)
	if err != nil {
		return nil, err
	}
	threadIDs, err := a.resolveThreadContextSelection(ctx, cfg, "threads recommendations", selection.threadContextSelection, false)
	if err != nil {
		return nil, err
	}

	statusCode, headers, body, callErr := a.loadThreadContextEnvelope(ctx, cfg, threadIDs[0], selection.threadContextSelection)
	if callErr != nil {
		return nil, callErr
	}

	thread := extractNestedMap(body, "thread")
	resolvedThreadID := firstNonEmpty(strings.TrimSpace(anyString(body["thread_id"])), strings.TrimSpace(anyString(thread["id"])), strings.TrimSpace(threadIDs[0]))
	collaboration := asMap(body["collaboration_summary"])
	recommendations := normalizeRecommendationReviewEvents(asSlice(collaboration["recommendations"]))
	decisionRequests := normalizeRecommendationReviewEvents(asSlice(collaboration["decision_requests"]))
	decisions := normalizeRecommendationReviewEvents(asSlice(collaboration["decisions"]))

	inboxResult, err := a.invokeTypedJSON(ctx, cfg, "inbox list", "inbox.list", nil, nil, nil)
	if err != nil {
		return nil, err
	}
	inboxData := asMap(inboxResult.Data)
	inboxBody := extractNestedMap(inboxData, "body")
	pendingDecisions := filteredInboxItems(asSlice(inboxBody["items"]), []string{resolvedThreadID}, []string{"decision_needed"})
	relatedThreadReview, err := a.collectRelatedThreadRecommendationReview(ctx, cfg, resolvedThreadID, body, selection.threadContextSelection, selection.includeRelatedEventContent)
	if err != nil {
		return nil, err
	}

	recommendationBody := map[string]any{
		"thread_id":    resolvedThreadID,
		"thread":       thread,
		"full_id":      selection.fullID,
		"full_summary": selection.fullSummary,
		"recommendations": map[string]any{
			"items": recommendations,
			"count": len(recommendations),
		},
		"decision_requests": map[string]any{
			"items": decisionRequests,
			"count": len(decisionRequests),
		},
		"decisions": map[string]any{
			"items": decisions,
			"count": len(decisions),
		},
		"pending_decisions": map[string]any{
			"items": pendingDecisions,
			"count": len(pendingDecisions),
		},
		"related_threads":           relatedThreadReview["related_threads"],
		"related_recommendations":   relatedThreadReview["related_recommendations"],
		"related_decision_requests": relatedThreadReview["related_decision_requests"],
		"related_decisions":         relatedThreadReview["related_decisions"],
		"total_review_items":        len(recommendations) + len(decisionRequests) + len(decisions) + len(pendingDecisions) + intValue(relatedThreadReview["total_review_items"]),
		"follow_up":                 recommendationFollowUpHints(resolvedThreadID, recommendations, decisionRequests, decisions),
		"context_source":            "threads.context",
		"inbox_source":              "inbox.list",
	}
	if selection.includeRelatedEventContent {
		recommendationBody["related_event_content_enabled"] = true
		recommendationBody["related_event_content_count"] = intValue(relatedThreadReview["related_event_content_count"])
	}
	if warningCount := intValue(relatedThreadReview["warning_count"]); warningCount > 0 {
		recommendationBody["warnings"] = map[string]any{
			"items": relatedThreadReview["warnings"],
			"count": warningCount,
		}
	}

	data := map[string]any{
		"status_code": statusCode,
		"headers":     headers,
		"body":        recommendationBody,
	}
	contextResult := &commandResult{Data: data}
	contextResult.Text = formatTypedCommandText(
		"threads.recommendations",
		statusCode,
		headers,
		recommendationBody,
		cfg.Verbose,
		cfg.Headers,
	)
	return contextResult, nil
}

func (a *App) collectRelatedThreadRecommendationReview(ctx context.Context, cfg config.Resolved, rootThreadID string, rootBody map[string]any, selection threadContextSelection, includeRelatedEventContent bool) (map[string]any, error) {
	relatedThreadIDs := relatedThreadRefIDs(rootThreadID, rootBody)
	items := make([]any, 0, len(relatedThreadIDs))
	relatedRecommendations := make([]any, 0)
	relatedDecisionRequests := make([]any, 0)
	relatedDecisions := make([]any, 0)
	warnings := make([]any, 0)
	totalReviewItems := 0
	relatedEventContentCount := 0

	for _, relatedThreadID := range relatedThreadIDs {
		_, _, body, err := a.loadThreadContextEnvelope(ctx, cfg, relatedThreadID, selection)
		if err != nil {
			warnings = append(warnings, map[string]any{
				"thread_id": relatedThreadID,
				"message":   fmt.Sprintf("skipped related thread %s: %s", relatedThreadID, err.Error()),
			})
			continue
		}
		thread := extractNestedMap(body, "thread")
		collaboration := asMap(body["collaboration_summary"])
		recommendations := annotateRecommendationReviewEvents(normalizeRecommendationReviewEvents(asSlice(collaboration["recommendations"])), thread)
		decisionRequests := annotateRecommendationReviewEvents(normalizeRecommendationReviewEvents(asSlice(collaboration["decision_requests"])), thread)
		decisions := annotateRecommendationReviewEvents(normalizeRecommendationReviewEvents(asSlice(collaboration["decisions"])), thread)
		if includeRelatedEventContent {
			var hydrateWarnings []any
			recommendations, hydrateWarnings = a.hydrateRelatedReviewEvents(ctx, cfg, relatedThreadID, recommendations)
			warnings = append(warnings, hydrateWarnings...)
			decisionRequests, hydrateWarnings = a.hydrateRelatedReviewEvents(ctx, cfg, relatedThreadID, decisionRequests)
			warnings = append(warnings, hydrateWarnings...)
			decisions, hydrateWarnings = a.hydrateRelatedReviewEvents(ctx, cfg, relatedThreadID, decisions)
			warnings = append(warnings, hydrateWarnings...)
			relatedEventContentCount += hydratedRelatedReviewEventCount(recommendations)
			relatedEventContentCount += hydratedRelatedReviewEventCount(decisionRequests)
			relatedEventContentCount += hydratedRelatedReviewEventCount(decisions)
		}
		relatedRecommendations = append(relatedRecommendations, recommendations...)
		relatedDecisionRequests = append(relatedDecisionRequests, decisionRequests...)
		relatedDecisions = append(relatedDecisions, decisions...)
		threadReviewCount := len(recommendations) + len(decisionRequests) + len(decisions)
		totalReviewItems += threadReviewCount
		items = append(items, map[string]any{
			"thread_id": resolvedThreadIDFromContextBody(body, relatedThreadID),
			"thread":    thread,
			"recommendations": map[string]any{
				"items": recommendations,
				"count": len(recommendations),
			},
			"decision_requests": map[string]any{
				"items": decisionRequests,
				"count": len(decisionRequests),
			},
			"decisions": map[string]any{
				"items": decisions,
				"count": len(decisions),
			},
			"total_review_items": threadReviewCount,
		})
	}

	return map[string]any{
		"related_threads": map[string]any{
			"items": items,
			"count": len(items),
		},
		"related_recommendations": map[string]any{
			"items": relatedRecommendations,
			"count": len(relatedRecommendations),
		},
		"related_decision_requests": map[string]any{
			"items": relatedDecisionRequests,
			"count": len(relatedDecisionRequests),
		},
		"related_decisions": map[string]any{
			"items": relatedDecisions,
			"count": len(relatedDecisions),
		},
		"warnings":                    warnings,
		"warning_count":               len(warnings),
		"related_event_content_count": relatedEventContentCount,
		"total_review_items":          totalReviewItems,
	}, nil
}

func (a *App) hydrateRelatedReviewEvents(ctx context.Context, cfg config.Resolved, threadID string, events []any) ([]any, []any) {
	if len(events) == 0 {
		return []any{}, nil
	}
	out := make([]any, 0, len(events))
	warnings := make([]any, 0)
	for _, raw := range events {
		item := cloneMap(asMap(raw))
		if item == nil {
			continue
		}
		eventID := strings.TrimSpace(anyString(item["id"]))
		if eventID == "" {
			out = append(out, item)
			continue
		}
		result, err := a.invokeTypedJSON(ctx, cfg, "events get", "events.get", map[string]string{"event_id": eventID}, nil, nil)
		if err != nil {
			warnings = append(warnings, map[string]any{
				"thread_id": threadID,
				"event_id":  eventID,
				"message":   fmt.Sprintf("kept summary-only related event %s: %s", eventID, err.Error()),
			})
			out = append(out, item)
			continue
		}
		data := asMap(result.Data)
		body := extractNestedMap(data, "body")
		fullEvent := extractNestedMap(body, "event")
		if fullEvent == nil {
			out = append(out, item)
			continue
		}
		item["event"] = fullEvent
		if strings.TrimSpace(anyString(item["summary"])) == "" {
			item["summary"] = anyString(fullEvent["summary"])
		}
		if strings.TrimSpace(anyString(item["summary_preview"])) == "" {
			if preview := eventSummaryPreview(fullEvent); preview != "" {
				item["summary_preview"] = preview
			}
		}
		out = append(out, item)
	}
	return out, warnings
}

func hydratedRelatedReviewEventCount(events []any) int {
	count := 0
	for _, raw := range events {
		if item := asMap(raw); item != nil && asMap(item["event"]) != nil {
			count++
		}
	}
	return count
}

func relatedThreadRefIDs(rootThreadID string, body map[string]any) []string {
	if body == nil {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0)
	collectThreadRefIDs(body, rootThreadID, seen, &out)
	sort.Strings(out)
	return out
}

func collectThreadRefIDs(value any, rootThreadID string, seen map[string]struct{}, out *[]string) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			collectThreadRefIDs(item, rootThreadID, seen, out)
		}
	case map[string]any:
		for _, nested := range typed {
			collectThreadRefIDs(nested, rootThreadID, seen, out)
		}
	case string:
		ref := strings.TrimSpace(typed)
		if !strings.HasPrefix(ref, "thread:") {
			return
		}
		threadID := strings.TrimSpace(strings.TrimPrefix(ref, "thread:"))
		if threadID == "" || threadID == strings.TrimSpace(rootThreadID) {
			return
		}
		if _, ok := seen[threadID]; ok {
			return
		}
		seen[threadID] = struct{}{}
		*out = append(*out, threadID)
	}
}

func annotateRecommendationReviewEvents(events []any, thread map[string]any) []any {
	if len(events) == 0 {
		return []any{}
	}
	threadID := firstNonEmpty(strings.TrimSpace(anyString(thread["thread_id"])), strings.TrimSpace(anyString(thread["id"])))
	threadTitle := strings.TrimSpace(anyString(thread["title"]))
	out := make([]any, 0, len(events))
	for _, raw := range events {
		event := cloneMap(asMap(raw))
		if event == nil {
			continue
		}
		if threadID != "" {
			event["source_thread_id"] = threadID
		}
		if threadTitle != "" {
			event["source_thread_title"] = threadTitle
		}
		out = append(out, event)
	}
	return out
}

func resolvedThreadIDFromContextBody(body map[string]any, fallback string) string {
	thread := extractNestedMap(body, "thread")
	return firstNonEmpty(
		strings.TrimSpace(anyString(body["thread_id"])),
		strings.TrimSpace(anyString(thread["thread_id"])),
		strings.TrimSpace(anyString(thread["id"])),
		strings.TrimSpace(fallback),
	)
}

func (a *App) runArtifactsCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "artifacts", artifactsSubcommandSpec.requiredError()
	}
	sub := artifactsSubcommandSpec.normalize(args[0])
	switch sub {
	case "list":
		fs := newSilentFlagSet("artifacts list")
		var kindFlag, threadIDFlag, beforeFlag, afterFlag trackedString
		var includeTrashed bool
		var trashedOnly bool
		var includeArchived bool
		var archivedOnly bool
		fs.Var(&kindFlag, "kind", "Filter by artifact kind")
		fs.Var(&threadIDFlag, "thread-id", "Filter by thread id")
		fs.Var(&beforeFlag, "created-before", "Filter by created_at upper bound")
		fs.Var(&afterFlag, "created-after", "Filter by created_at lower bound")
		fs.BoolVar(&includeTrashed, "include-trashed", false, "Include trashed artifacts")
		fs.BoolVar(&trashedOnly, "trashed-only", false, "Show only trashed artifacts")
		fs.BoolVar(&includeArchived, "include-archived", false, "Include archived artifacts")
		fs.BoolVar(&archivedOnly, "archived-only", false, "Show only archived artifacts")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "artifacts list", errnorm.Usage("invalid_flags", err.Error())
		}
		if len(fs.Args()) > 0 {
			return nil, "artifacts list", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar artifacts list`")
		}
		resolvedThreadID := strings.TrimSpace(threadIDFlag.value)
		if resolvedThreadID != "" {
			resolved, err := a.resolveThreadIDFilters(ctx, cfg, []string{resolvedThreadID})
			if err != nil {
				return nil, "artifacts list", err
			}
			if len(resolved) > 0 {
				resolvedThreadID = resolved[0]
			}
		}
		query := make([]queryParam, 0, 6)
		addSingleQuery(&query, "kind", kindFlag.value)
		addSingleQuery(&query, "thread_id", resolvedThreadID)
		addSingleQuery(&query, "created_before", beforeFlag.value)
		addSingleQuery(&query, "created_after", afterFlag.value)
		if includeTrashed {
			query = append(query, queryParam{name: "include_trashed", values: []string{"true"}})
		}
		if trashedOnly {
			query = append(query, queryParam{name: "trashed_only", values: []string{"true"}})
		}
		if includeArchived {
			query = append(query, queryParam{name: "include_archived", values: []string{"true"}})
		}
		if archivedOnly {
			query = append(query, queryParam{name: "archived_only", values: []string{"true"}})
		}
		result, err := a.invokeTypedJSON(ctx, cfg, "artifacts list", "artifacts.list", nil, query, nil)
		return result, "artifacts list", err
	case "get":
		id, err := parseIDArg(args[1:], "artifact-id", "artifact id")
		if err != nil {
			return nil, "artifacts get", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(
			ctx,
			cfg,
			"artifacts get",
			"artifacts.get",
			"artifact_id",
			id,
			artifactIDLookupSpec,
			nil,
			nil,
		)
		return result, "artifacts get", callErr
	case "create":
		body, err := a.parseJSONBodyInput(args[1:], "artifacts create")
		if err != nil {
			return nil, "artifacts create", err
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "artifacts create", "artifacts.create", nil, nil, body)
		return result, "artifacts create", callErr
	case "content":
		id, err := parseIDArg(args[1:], "artifact-id", "artifact id")
		if err != nil {
			return nil, "artifacts content", err
		}
		result, callErr := a.invokeArtifactContentWithIDResolution(
			ctx,
			cfg,
			"artifacts content",
			"artifact_id",
			id,
			artifactIDLookupSpec,
		)
		return result, "artifacts content", callErr
	case "inspect":
		result, callErr := a.runArtifactsInspectCommand(ctx, args[1:], cfg)
		return result, "artifacts inspect", callErr
	case "trash":
		fs := newSilentFlagSet("artifacts trash")
		var artifactIDFlag trackedString
		var actorIDFlag trackedString
		var reasonFlag trackedString
		fs.Var(&artifactIDFlag, "artifact-id", "Artifact id to trash")
		fs.Var(&actorIDFlag, "actor-id", "Actor id")
		fs.Var(&reasonFlag, "reason", "Reason for trashing")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "artifacts trash", errnorm.Usage("invalid_flags", err.Error())
		}
		positionals := fs.Args()
		id := strings.TrimSpace(artifactIDFlag.value)
		if id == "" && len(positionals) > 0 {
			id = strings.TrimSpace(positionals[0])
			positionals = positionals[1:]
		}
		if err := validateID(id, "artifact id"); err != nil {
			return nil, "artifacts trash", err
		}
		if len(positionals) > 0 {
			return nil, "artifacts trash", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar artifacts trash`")
		}
		body := map[string]any{}
		actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
		if err != nil {
			return nil, "artifacts trash", err
		}
		if actorID != "" {
			body["actor_id"] = actorID
		} else if strings.TrimSpace(cfg.ActorID) != "" {
			body["actor_id"] = strings.TrimSpace(cfg.ActorID)
		}
		if strings.TrimSpace(reasonFlag.value) != "" {
			body["reason"] = strings.TrimSpace(reasonFlag.value)
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "artifacts trash", "artifacts.trash", map[string]string{"artifact_id": id}, nil, body)
		return result, "artifacts trash", callErr
	case "archive":
		fs := newSilentFlagSet("artifacts archive")
		var artifactIDFlag trackedString
		var actorIDFlag trackedString
		var reasonFlag trackedString
		fs.Var(&artifactIDFlag, "artifact-id", "Artifact id to archive")
		fs.Var(&actorIDFlag, "actor-id", "Actor id")
		fs.Var(&reasonFlag, "reason", "Reason for archiving")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "artifacts archive", errnorm.Usage("invalid_flags", err.Error())
		}
		positionals := fs.Args()
		id := strings.TrimSpace(artifactIDFlag.value)
		if id == "" && len(positionals) > 0 {
			id = strings.TrimSpace(positionals[0])
			positionals = positionals[1:]
		}
		if err := validateID(id, "artifact id"); err != nil {
			return nil, "artifacts archive", err
		}
		if len(positionals) > 0 {
			return nil, "artifacts archive", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar artifacts archive`")
		}
		body := map[string]any{}
		actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
		if err != nil {
			return nil, "artifacts archive", err
		}
		if actorID != "" {
			body["actor_id"] = actorID
		} else if strings.TrimSpace(cfg.ActorID) != "" {
			body["actor_id"] = strings.TrimSpace(cfg.ActorID)
		}
		if strings.TrimSpace(reasonFlag.value) != "" {
			body["reason"] = strings.TrimSpace(reasonFlag.value)
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "artifacts archive", "artifacts.archive", map[string]string{"artifact_id": id}, nil, body)
		return result, "artifacts archive", callErr
	case "unarchive":
		fs := newSilentFlagSet("artifacts unarchive")
		var artifactIDFlag trackedString
		var actorIDFlag trackedString
		var reasonFlag trackedString
		fs.Var(&artifactIDFlag, "artifact-id", "Artifact id to unarchive")
		fs.Var(&actorIDFlag, "actor-id", "Actor id")
		fs.Var(&reasonFlag, "reason", "Reason for unarchiving")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "artifacts unarchive", errnorm.Usage("invalid_flags", err.Error())
		}
		positionals := fs.Args()
		id := strings.TrimSpace(artifactIDFlag.value)
		if id == "" && len(positionals) > 0 {
			id = strings.TrimSpace(positionals[0])
			positionals = positionals[1:]
		}
		if err := validateID(id, "artifact id"); err != nil {
			return nil, "artifacts unarchive", err
		}
		if len(positionals) > 0 {
			return nil, "artifacts unarchive", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar artifacts unarchive`")
		}
		body := map[string]any{}
		actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
		if err != nil {
			return nil, "artifacts unarchive", err
		}
		if actorID != "" {
			body["actor_id"] = actorID
		} else if strings.TrimSpace(cfg.ActorID) != "" {
			body["actor_id"] = strings.TrimSpace(cfg.ActorID)
		}
		if strings.TrimSpace(reasonFlag.value) != "" {
			body["reason"] = strings.TrimSpace(reasonFlag.value)
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "artifacts unarchive", "artifacts.unarchive", map[string]string{"artifact_id": id}, nil, body)
		return result, "artifacts unarchive", callErr
	case "restore":
		fs := newSilentFlagSet("artifacts restore")
		var artifactIDFlag trackedString
		var actorIDFlag trackedString
		var reasonFlag trackedString
		fs.Var(&artifactIDFlag, "artifact-id", "Artifact id to restore")
		fs.Var(&actorIDFlag, "actor-id", "Actor id")
		fs.Var(&reasonFlag, "reason", "Reason for restoring")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "artifacts restore", errnorm.Usage("invalid_flags", err.Error())
		}
		positionals := fs.Args()
		id := strings.TrimSpace(artifactIDFlag.value)
		if id == "" && len(positionals) > 0 {
			id = strings.TrimSpace(positionals[0])
			positionals = positionals[1:]
		}
		if err := validateID(id, "artifact id"); err != nil {
			return nil, "artifacts restore", err
		}
		if len(positionals) > 0 {
			return nil, "artifacts restore", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar artifacts restore`")
		}
		body := map[string]any{}
		actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
		if err != nil {
			return nil, "artifacts restore", err
		}
		if actorID != "" {
			body["actor_id"] = actorID
		} else if strings.TrimSpace(cfg.ActorID) != "" {
			body["actor_id"] = strings.TrimSpace(cfg.ActorID)
		}
		if strings.TrimSpace(reasonFlag.value) != "" {
			body["reason"] = strings.TrimSpace(reasonFlag.value)
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "artifacts restore", "artifacts.restore", map[string]string{"artifact_id": id}, nil, body)
		return result, "artifacts restore", callErr
	case "purge":
		fs := newSilentFlagSet("artifacts purge")
		var artifactIDFlag trackedString
		var reasonFlag trackedString
		fs.Var(&artifactIDFlag, "artifact-id", "Artifact id to permanently delete")
		fs.Var(&reasonFlag, "reason", "Reason for permanent deletion")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "artifacts purge", errnorm.Usage("invalid_flags", err.Error())
		}
		positionals := fs.Args()
		id := strings.TrimSpace(artifactIDFlag.value)
		if id == "" && len(positionals) > 0 {
			id = strings.TrimSpace(positionals[0])
			positionals = positionals[1:]
		}
		if err := validateID(id, "artifact id"); err != nil {
			return nil, "artifacts purge", err
		}
		if len(positionals) > 0 {
			return nil, "artifacts purge", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar artifacts purge`")
		}
		body := map[string]any{}
		if strings.TrimSpace(reasonFlag.value) != "" {
			body["reason"] = strings.TrimSpace(reasonFlag.value)
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "artifacts purge", "artifacts.purge", map[string]string{"artifact_id": id}, nil, body)
		return result, "artifacts purge", callErr
	default:
		return nil, "artifacts", artifactsSubcommandSpec.unknownError(args[0])
	}
}

func (a *App) runBoardsCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "boards", boardsSubcommandSpec.requiredError()
	}
	sub := boardsSubcommandSpec.normalize(args[0])
	switch sub {
	case "list":
		fs := newSilentFlagSet("boards list")
		var statusFlag, queryFlag, cursorFlag trackedString
		var limitFlag trackedInt
		var labelFlag, ownerFlag trackedStrings
		var includeArchived, archivedOnly, includeTrashed, trashedOnly bool
		fs.Var(&statusFlag, "status", "Filter by board status")
		fs.Var(&queryFlag, "q", "Search by board id or title")
		fs.Var(&limitFlag, "limit", "Limit the number of returned boards")
		fs.Var(&cursorFlag, "cursor", "Pagination cursor from a previous list response")
		fs.Var(&labelFlag, "label", "Filter by label (repeatable)")
		fs.Var(&ownerFlag, "owner", "Filter by owner actor id (repeatable)")
		fs.BoolVar(&includeArchived, "include-archived", false, "Include archived boards")
		fs.BoolVar(&archivedOnly, "archived-only", false, "Show only archived boards")
		fs.BoolVar(&includeTrashed, "include-trashed", false, "Include trashed boards")
		fs.BoolVar(&trashedOnly, "trashed-only", false, "Show only trashed boards")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "boards list", errnorm.Usage("invalid_flags", err.Error())
		}
		if len(fs.Args()) > 0 {
			return nil, "boards list", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar boards list`")
		}
		if limitFlag.set && (limitFlag.value < 1 || limitFlag.value > 1000) {
			return nil, "boards list", errnorm.Usage("invalid_request", "limit must be between 1 and 1000")
		}
		query := make([]queryParam, 0, 6)
		addSingleQuery(&query, "status", statusFlag.value)
		addSingleQuery(&query, "q", queryFlag.value)
		if limitFlag.set {
			addSingleQuery(&query, "limit", strconv.Itoa(limitFlag.value))
		}
		addSingleQuery(&query, "cursor", cursorFlag.value)
		addMultiQuery(&query, "label", labelFlag.values)
		addMultiQuery(&query, "owner", ownerFlag.values)
		if includeArchived {
			query = append(query, queryParam{name: "include_archived", values: []string{"true"}})
		}
		if archivedOnly {
			query = append(query, queryParam{name: "archived_only", values: []string{"true"}})
		}
		if includeTrashed {
			query = append(query, queryParam{name: "include_trashed", values: []string{"true"}})
		}
		if trashedOnly {
			query = append(query, queryParam{name: "trashed_only", values: []string{"true"}})
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "boards list", "boards.list", nil, query, nil)
		return result, "boards list", callErr
	case "create":
		body, err := a.parseJSONBodyInput(args[1:], "boards create")
		if err != nil {
			return nil, "boards create", err
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "boards create", "boards.create", nil, nil, body)
		return result, "boards create", callErr
	case "get":
		id, err := parseIDArg(args[1:], "board-id", "board id")
		if err != nil {
			return nil, "boards get", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(
			ctx,
			cfg,
			"boards get",
			"boards.get",
			"board_id",
			id,
			boardIDLookupSpec,
			nil,
			nil,
		)
		return result, "boards get", callErr
	case "update":
		id, body, err := a.parseIDAndBodyInput(args[1:], "board-id", "board id", "boards update")
		if err != nil {
			return nil, "boards update", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(
			ctx,
			cfg,
			"boards update",
			"boards.update",
			"board_id",
			id,
			boardIDLookupSpec,
			nil,
			body,
		)
		return result, "boards update", callErr
	case "workspace":
		id, err := parseIDArg(args[1:], "board-id", "board id")
		if err != nil {
			return nil, "boards workspace", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(
			ctx,
			cfg,
			"boards workspace",
			"boards.workspace",
			"board_id",
			id,
			boardIDLookupSpec,
			nil,
			nil,
		)
		return result, "boards workspace", callErr
	case "archive":
		fs := newSilentFlagSet("boards archive")
		var boardIDFlag trackedString
		var actorIDFlag trackedString
		var reasonFlag trackedString
		fs.Var(&boardIDFlag, "board-id", "Board id to archive")
		fs.Var(&actorIDFlag, "actor-id", "Actor id")
		fs.Var(&reasonFlag, "reason", "Reason for archiving")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "boards archive", errnorm.Usage("invalid_flags", err.Error())
		}
		positionals := fs.Args()
		id := strings.TrimSpace(boardIDFlag.value)
		if id == "" && len(positionals) > 0 {
			id = strings.TrimSpace(positionals[0])
			positionals = positionals[1:]
		}
		if err := validateID(id, "board id"); err != nil {
			return nil, "boards archive", err
		}
		if len(positionals) > 0 {
			return nil, "boards archive", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar boards archive`")
		}
		body := map[string]any{}
		actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
		if err != nil {
			return nil, "boards archive", err
		}
		if actorID != "" {
			body["actor_id"] = actorID
		} else if strings.TrimSpace(cfg.ActorID) != "" {
			body["actor_id"] = strings.TrimSpace(cfg.ActorID)
		}
		if strings.TrimSpace(reasonFlag.value) != "" {
			body["reason"] = strings.TrimSpace(reasonFlag.value)
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(
			ctx,
			cfg,
			"boards archive",
			"boards.archive",
			"board_id",
			id,
			boardIDLookupSpec,
			nil,
			body,
		)
		return result, "boards archive", callErr
	case "unarchive":
		fs := newSilentFlagSet("boards unarchive")
		var boardIDFlag trackedString
		var actorIDFlag trackedString
		var reasonFlag trackedString
		fs.Var(&boardIDFlag, "board-id", "Board id to unarchive")
		fs.Var(&actorIDFlag, "actor-id", "Actor id")
		fs.Var(&reasonFlag, "reason", "Reason for unarchiving")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "boards unarchive", errnorm.Usage("invalid_flags", err.Error())
		}
		positionals := fs.Args()
		id := strings.TrimSpace(boardIDFlag.value)
		if id == "" && len(positionals) > 0 {
			id = strings.TrimSpace(positionals[0])
			positionals = positionals[1:]
		}
		if err := validateID(id, "board id"); err != nil {
			return nil, "boards unarchive", err
		}
		if len(positionals) > 0 {
			return nil, "boards unarchive", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar boards unarchive`")
		}
		body := map[string]any{}
		actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
		if err != nil {
			return nil, "boards unarchive", err
		}
		if actorID != "" {
			body["actor_id"] = actorID
		} else if strings.TrimSpace(cfg.ActorID) != "" {
			body["actor_id"] = strings.TrimSpace(cfg.ActorID)
		}
		if strings.TrimSpace(reasonFlag.value) != "" {
			body["reason"] = strings.TrimSpace(reasonFlag.value)
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(
			ctx,
			cfg,
			"boards unarchive",
			"boards.unarchive",
			"board_id",
			id,
			boardIDLookupSpec,
			nil,
			body,
		)
		return result, "boards unarchive", callErr
	case "trash":
		fs := newSilentFlagSet("boards trash")
		var boardIDFlag trackedString
		var actorIDFlag trackedString
		var reasonFlag trackedString
		fs.Var(&boardIDFlag, "board-id", "Board id to trash")
		fs.Var(&actorIDFlag, "actor-id", "Actor id")
		fs.Var(&reasonFlag, "reason", "Reason for trashing")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "boards trash", errnorm.Usage("invalid_flags", err.Error())
		}
		positionals := fs.Args()
		id := strings.TrimSpace(boardIDFlag.value)
		if id == "" && len(positionals) > 0 {
			id = strings.TrimSpace(positionals[0])
			positionals = positionals[1:]
		}
		if err := validateID(id, "board id"); err != nil {
			return nil, "boards trash", err
		}
		if len(positionals) > 0 {
			return nil, "boards trash", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar boards trash`")
		}
		body := map[string]any{}
		actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
		if err != nil {
			return nil, "boards trash", err
		}
		if actorID != "" {
			body["actor_id"] = actorID
		} else if strings.TrimSpace(cfg.ActorID) != "" {
			body["actor_id"] = strings.TrimSpace(cfg.ActorID)
		}
		if strings.TrimSpace(reasonFlag.value) != "" {
			body["reason"] = strings.TrimSpace(reasonFlag.value)
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(
			ctx,
			cfg,
			"boards trash",
			"boards.trash",
			"board_id",
			id,
			boardIDLookupSpec,
			nil,
			body,
		)
		return result, "boards trash", callErr
	case "restore":
		fs := newSilentFlagSet("boards restore")
		var boardIDFlag trackedString
		var actorIDFlag trackedString
		var reasonFlag trackedString
		fs.Var(&boardIDFlag, "board-id", "Board id to restore")
		fs.Var(&actorIDFlag, "actor-id", "Actor id")
		fs.Var(&reasonFlag, "reason", "Reason for restoring")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "boards restore", errnorm.Usage("invalid_flags", err.Error())
		}
		positionals := fs.Args()
		id := strings.TrimSpace(boardIDFlag.value)
		if id == "" && len(positionals) > 0 {
			id = strings.TrimSpace(positionals[0])
			positionals = positionals[1:]
		}
		if err := validateID(id, "board id"); err != nil {
			return nil, "boards restore", err
		}
		if len(positionals) > 0 {
			return nil, "boards restore", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar boards restore`")
		}
		body := map[string]any{}
		actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
		if err != nil {
			return nil, "boards restore", err
		}
		if actorID != "" {
			body["actor_id"] = actorID
		} else if strings.TrimSpace(cfg.ActorID) != "" {
			body["actor_id"] = strings.TrimSpace(cfg.ActorID)
		}
		if strings.TrimSpace(reasonFlag.value) != "" {
			body["reason"] = strings.TrimSpace(reasonFlag.value)
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(
			ctx,
			cfg,
			"boards restore",
			"boards.restore",
			"board_id",
			id,
			boardIDLookupSpec,
			nil,
			body,
		)
		return result, "boards restore", callErr
	case "purge":
		fs := newSilentFlagSet("boards purge")
		var boardIDFlag trackedString
		var reasonFlag trackedString
		fs.Var(&boardIDFlag, "board-id", "Board id to permanently delete")
		fs.Var(&reasonFlag, "reason", "Reason for permanent deletion")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "boards purge", errnorm.Usage("invalid_flags", err.Error())
		}
		positionals := fs.Args()
		id := strings.TrimSpace(boardIDFlag.value)
		if id == "" && len(positionals) > 0 {
			id = strings.TrimSpace(positionals[0])
			positionals = positionals[1:]
		}
		if err := validateID(id, "board id"); err != nil {
			return nil, "boards purge", err
		}
		if len(positionals) > 0 {
			return nil, "boards purge", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar boards purge`")
		}
		body := map[string]any{}
		if strings.TrimSpace(reasonFlag.value) != "" {
			body["reason"] = strings.TrimSpace(reasonFlag.value)
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(
			ctx,
			cfg,
			"boards purge",
			"boards.purge",
			"board_id",
			id,
			boardIDLookupSpec,
			nil,
			body,
		)
		return result, "boards purge", callErr
	case "cards":
		return a.runBoardCardsCommand(ctx, args[1:], cfg)
	default:
		return nil, "boards", boardsSubcommandSpec.unknownError(args[0])
	}
}

func (a *App) runBoardCardsCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "boards cards", boardsCardsSubcommandSpec.requiredError()
	}
	sub := boardsCardsSubcommandSpec.normalize(args[0])
	switch sub {
	case "list":
		boardID, err := parseIDArg(args[1:], "board-id", "board id")
		if err != nil {
			return nil, "boards cards list", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(
			ctx,
			cfg,
			"boards cards list",
			"boards.cards.list",
			"board_id",
			boardID,
			boardIDLookupSpec,
			nil,
			nil,
		)
		return result, "boards cards list", callErr
	case "create":
		boardID, body, err := a.parseBoardCardCreateInput(ctx, args[1:], cfg, "boards cards create")
		if err != nil {
			return nil, "boards cards create", err
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "boards cards create", "boards.cards.create", map[string]string{"board_id": boardID}, nil, body)
		return result, "boards cards create", callErr
	case "get":
		boardID, cardID, err := a.parseBoardCardBoardScopedTarget(args[1:], "boards cards get")
		if err != nil {
			return nil, "boards cards get", err
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "boards cards get", "boards.cards.get", map[string]string{"board_id": boardID, "id": cardID}, nil, nil)
		return result, "boards cards get", callErr
	case "update":
		pathParams, body, err := a.parseBoardCardUpdateInput(ctx, args[1:], cfg, "boards cards update")
		if err != nil {
			return nil, "boards cards update", err
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "boards cards update", "cards.patch", pathParams, nil, body)
		return result, "boards cards update", callErr
	case "move":
		boardID, identifier, body, err := a.parseBoardCardMoveInput(ctx, args[1:], cfg, "boards cards move")
		if err != nil {
			return nil, "boards cards move", err
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "boards cards move", "cards.move", map[string]string{"card_id": identifier, "board_id": boardID}, nil, body)
		return result, "boards cards move", callErr
	case "archive":
		pathParams, body, err := a.parseBoardCardArchiveInput(ctx, args[1:], cfg, "boards cards archive")
		if err != nil {
			return nil, "boards cards archive", err
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "boards cards archive", "cards.archive", pathParams, nil, body)
		return result, "boards cards archive", callErr
	default:
		return nil, "boards cards", boardsCardsSubcommandSpec.unknownError(args[0])
	}
}

func (a *App) runDocsCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "docs", docsSubcommandSpec.requiredError()
	}
	sub := docsSubcommandSpec.normalize(args[0])
	switch sub {
	case "list":
		fs := newSilentFlagSet("docs list")
		var threadIDFlag, queryFlag, cursorFlag trackedString
		var limitFlag trackedInt
		var includeTrashed, trashedOnly, includeArchived, archivedOnly bool
		fs.BoolVar(&includeTrashed, "include-trashed", false, "Include trashed documents")
		fs.BoolVar(&trashedOnly, "trashed-only", false, "Show only trashed documents")
		fs.BoolVar(&includeArchived, "include-archived", false, "Include archived documents")
		fs.BoolVar(&archivedOnly, "archived-only", false, "Show only archived documents")
		fs.Var(&threadIDFlag, "thread-id", "Filter by thread id")
		fs.Var(&queryFlag, "q", "Search by document id or title")
		fs.Var(&limitFlag, "limit", "Limit the number of returned documents")
		fs.Var(&cursorFlag, "cursor", "Pagination cursor from a previous list response")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "docs list", errnorm.Usage("invalid_flags", err.Error())
		}
		if len(fs.Args()) > 0 {
			return nil, "docs list", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar docs list`")
		}
		if limitFlag.set && (limitFlag.value < 1 || limitFlag.value > 1000) {
			return nil, "docs list", errnorm.Usage("invalid_request", "limit must be between 1 and 1000")
		}
		resolvedThreadID := strings.TrimSpace(threadIDFlag.value)
		if resolvedThreadID != "" {
			resolved, err := a.resolveThreadIDFilters(ctx, cfg, []string{resolvedThreadID})
			if err != nil {
				return nil, "docs list", err
			}
			if len(resolved) > 0 {
				resolvedThreadID = resolved[0]
			}
		}
		query := make([]queryParam, 0, 5)
		addSingleQuery(&query, "thread_id", resolvedThreadID)
		addSingleQuery(&query, "q", queryFlag.value)
		if limitFlag.set {
			addSingleQuery(&query, "limit", strconv.Itoa(limitFlag.value))
		}
		addSingleQuery(&query, "cursor", cursorFlag.value)
		if includeTrashed {
			query = append(query, queryParam{name: "include_trashed", values: []string{"true"}})
		}
		if trashedOnly {
			query = append(query, queryParam{name: "trashed_only", values: []string{"true"}})
		}
		if includeArchived {
			query = append(query, queryParam{name: "include_archived", values: []string{"true"}})
		}
		if archivedOnly {
			query = append(query, queryParam{name: "archived_only", values: []string{"true"}})
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "docs list", "docs.list", nil, query, nil)
		return result, "docs list", callErr
	case "create":
		body, dryRun, err := a.parseJSONBodyInputWithOptions(args[1:], "docs create", jsonBodyInputOptions{
			allowContentFile: true,
			allowDryRun:      true,
		})
		if err != nil {
			return nil, "docs create", err
		}
		if err := validateDocsCreateBody(body, "docs create"); err != nil {
			return nil, "docs create", err
		}
		if dryRun {
			return dryRunResult("docs create", "docs.create", nil, nil, body), "docs create", nil
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "docs create", "docs.create", nil, nil, body)
		return result, "docs create", callErr
	case "get":
		id, err := parseIDArg(args[1:], "document-id", "document id")
		if err != nil {
			return nil, "docs get", err
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "docs get", "docs.get", map[string]string{"document_id": id}, nil, nil)
		return result, "docs get", callErr
	case "content":
		result, callErr := a.runDocsContentCommand(ctx, args[1:], cfg)
		return result, "docs content", callErr
	case "update":
		result, callErr := a.runDocsUpdateCommand(ctx, args[1:], cfg)
		return result, "docs update", callErr
	case "propose-update":
		result, callErr := a.runDocsProposeUpdateCommand(ctx, args[1:], cfg)
		return result, "docs propose-update", callErr
	case "apply":
		result, callErr := a.runDocsApplyCommand(ctx, args[1:], cfg)
		return result, "docs apply", callErr
	case "validate-update":
		id, body, _, err := a.parseIDAndBodyInputWithOptions(args[1:], "document-id", "document id", "docs validate-update", jsonBodyInputOptions{
			allowContentFile: true,
		})
		if err != nil {
			return nil, "docs validate-update", err
		}
		if err := validateDocsUpdateBody(body, "docs validate-update"); err != nil {
			return nil, "docs validate-update", err
		}
		return validationResult("docs validate-update", "docs.revisions.create", map[string]string{"document_id": id}, nil, body), "docs validate-update", nil
	case "history":
		id, err := parseIDArg(args[1:], "document-id", "document id")
		if err != nil {
			return nil, "docs history", err
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "docs history", "docs.revisions.list", map[string]string{"document_id": id}, nil, nil)
		return result, "docs history", callErr
	case "revision":
		if len(args) < 2 {
			return nil, "docs revision", docsRevisionSubcommandSpec.requiredError()
		}
		if docsRevisionSubcommandSpec.normalize(args[1]) != "get" {
			return nil, "docs revision", docsRevisionSubcommandSpec.unknownError(args[1])
		}
		fs := newSilentFlagSet("docs revision get")
		var documentIDFlag trackedString
		var revisionIDFlag trackedString
		fs.Var(&documentIDFlag, "document-id", "Document id")
		fs.Var(&revisionIDFlag, "revision-id", "Revision id")
		if err := fs.Parse(args[2:]); err != nil {
			return nil, "docs revision get", errnorm.Usage("invalid_flags", err.Error())
		}
		positionals := fs.Args()

		documentID := strings.TrimSpace(documentIDFlag.value)
		revisionID := strings.TrimSpace(revisionIDFlag.value)
		if documentID == "" && len(positionals) > 0 {
			documentID = strings.TrimSpace(positionals[0])
			positionals = positionals[1:]
		}
		if revisionID == "" && len(positionals) > 0 {
			revisionID = strings.TrimSpace(positionals[0])
			positionals = positionals[1:]
		}
		if err := validateID(documentID, "document id"); err != nil {
			return nil, "docs revision get", err
		}
		if err := validateID(revisionID, "revision id"); err != nil {
			return nil, "docs revision get", err
		}
		if len(positionals) > 0 {
			return nil, "docs revision get", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar docs revision get`")
		}

		result, callErr := a.invokeTypedJSON(
			ctx,
			cfg,
			"docs revision get",
			"docs.revisions.get",
			map[string]string{"document_id": documentID, "revision_id": revisionID},
			nil,
			nil,
		)
		return result, "docs revision get", callErr
	case "trash":
		fs := newSilentFlagSet("docs trash")
		var documentIDFlag trackedString
		var actorIDFlag trackedString
		var reasonFlag trackedString
		fs.Var(&documentIDFlag, "document-id", "Document id to trash")
		fs.Var(&actorIDFlag, "actor-id", "Actor id")
		fs.Var(&reasonFlag, "reason", "Reason for trashing")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "docs trash", errnorm.Usage("invalid_flags", err.Error())
		}
		positionals := fs.Args()
		id := strings.TrimSpace(documentIDFlag.value)
		if id == "" && len(positionals) > 0 {
			id = strings.TrimSpace(positionals[0])
			positionals = positionals[1:]
		}
		if err := validateID(id, "document id"); err != nil {
			return nil, "docs trash", err
		}
		if len(positionals) > 0 {
			return nil, "docs trash", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar docs trash`")
		}
		body := map[string]any{}
		actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
		if err != nil {
			return nil, "docs trash", err
		}
		if actorID != "" {
			body["actor_id"] = actorID
		} else if strings.TrimSpace(cfg.ActorID) != "" {
			body["actor_id"] = strings.TrimSpace(cfg.ActorID)
		}
		if strings.TrimSpace(reasonFlag.value) != "" {
			body["reason"] = strings.TrimSpace(reasonFlag.value)
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "docs trash", "docs.trash", map[string]string{"document_id": id}, nil, body)
		return result, "docs trash", callErr
	case "archive":
		fs := newSilentFlagSet("docs archive")
		var documentIDFlag trackedString
		var actorIDFlag trackedString
		var reasonFlag trackedString
		fs.Var(&documentIDFlag, "document-id", "Document id to archive")
		fs.Var(&actorIDFlag, "actor-id", "Actor id")
		fs.Var(&reasonFlag, "reason", "Reason for archiving")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "docs archive", errnorm.Usage("invalid_flags", err.Error())
		}
		positionals := fs.Args()
		id := strings.TrimSpace(documentIDFlag.value)
		if id == "" && len(positionals) > 0 {
			id = strings.TrimSpace(positionals[0])
			positionals = positionals[1:]
		}
		if err := validateID(id, "document id"); err != nil {
			return nil, "docs archive", err
		}
		if len(positionals) > 0 {
			return nil, "docs archive", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar docs archive`")
		}
		body := map[string]any{}
		actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
		if err != nil {
			return nil, "docs archive", err
		}
		if actorID != "" {
			body["actor_id"] = actorID
		} else if strings.TrimSpace(cfg.ActorID) != "" {
			body["actor_id"] = strings.TrimSpace(cfg.ActorID)
		}
		if strings.TrimSpace(reasonFlag.value) != "" {
			body["reason"] = strings.TrimSpace(reasonFlag.value)
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "docs archive", "docs.archive", map[string]string{"document_id": id}, nil, body)
		return result, "docs archive", callErr
	case "unarchive":
		fs := newSilentFlagSet("docs unarchive")
		var documentIDFlag trackedString
		var actorIDFlag trackedString
		var reasonFlag trackedString
		fs.Var(&documentIDFlag, "document-id", "Document id to unarchive")
		fs.Var(&actorIDFlag, "actor-id", "Actor id")
		fs.Var(&reasonFlag, "reason", "Reason for unarchiving")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "docs unarchive", errnorm.Usage("invalid_flags", err.Error())
		}
		positionals := fs.Args()
		id := strings.TrimSpace(documentIDFlag.value)
		if id == "" && len(positionals) > 0 {
			id = strings.TrimSpace(positionals[0])
			positionals = positionals[1:]
		}
		if err := validateID(id, "document id"); err != nil {
			return nil, "docs unarchive", err
		}
		if len(positionals) > 0 {
			return nil, "docs unarchive", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar docs unarchive`")
		}
		body := map[string]any{}
		actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
		if err != nil {
			return nil, "docs unarchive", err
		}
		if actorID != "" {
			body["actor_id"] = actorID
		} else if strings.TrimSpace(cfg.ActorID) != "" {
			body["actor_id"] = strings.TrimSpace(cfg.ActorID)
		}
		if strings.TrimSpace(reasonFlag.value) != "" {
			body["reason"] = strings.TrimSpace(reasonFlag.value)
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "docs unarchive", "docs.unarchive", map[string]string{"document_id": id}, nil, body)
		return result, "docs unarchive", callErr
	case "restore":
		fs := newSilentFlagSet("docs restore")
		var documentIDFlag trackedString
		var actorIDFlag trackedString
		var reasonFlag trackedString
		fs.Var(&documentIDFlag, "document-id", "Document id to restore")
		fs.Var(&actorIDFlag, "actor-id", "Actor id")
		fs.Var(&reasonFlag, "reason", "Reason for restoring")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "docs restore", errnorm.Usage("invalid_flags", err.Error())
		}
		positionals := fs.Args()
		id := strings.TrimSpace(documentIDFlag.value)
		if id == "" && len(positionals) > 0 {
			id = strings.TrimSpace(positionals[0])
			positionals = positionals[1:]
		}
		if err := validateID(id, "document id"); err != nil {
			return nil, "docs restore", err
		}
		if len(positionals) > 0 {
			return nil, "docs restore", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar docs restore`")
		}
		body := map[string]any{}
		actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
		if err != nil {
			return nil, "docs restore", err
		}
		if actorID != "" {
			body["actor_id"] = actorID
		} else if strings.TrimSpace(cfg.ActorID) != "" {
			body["actor_id"] = strings.TrimSpace(cfg.ActorID)
		}
		if strings.TrimSpace(reasonFlag.value) != "" {
			body["reason"] = strings.TrimSpace(reasonFlag.value)
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "docs restore", "docs.restore", map[string]string{"document_id": id}, nil, body)
		return result, "docs restore", callErr
	case "purge":
		fs := newSilentFlagSet("docs purge")
		var documentIDFlag trackedString
		var reasonFlag trackedString
		fs.Var(&documentIDFlag, "document-id", "Document id to permanently delete")
		fs.Var(&reasonFlag, "reason", "Reason for permanent deletion")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "docs purge", errnorm.Usage("invalid_flags", err.Error())
		}
		positionals := fs.Args()
		id := strings.TrimSpace(documentIDFlag.value)
		if id == "" && len(positionals) > 0 {
			id = strings.TrimSpace(positionals[0])
			positionals = positionals[1:]
		}
		if err := validateID(id, "document id"); err != nil {
			return nil, "docs purge", err
		}
		if len(positionals) > 0 {
			return nil, "docs purge", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar docs purge`")
		}
		body := map[string]any{}
		if strings.TrimSpace(reasonFlag.value) != "" {
			body["reason"] = strings.TrimSpace(reasonFlag.value)
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "docs purge", "docs.purge", map[string]string{"document_id": id}, nil, body)
		return result, "docs purge", callErr
	default:
		return nil, "docs", docsSubcommandSpec.unknownError(args[0])
	}
}

func (a *App) runEventsCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "events", eventsSubcommandSpec.requiredError()
	}
	sub := eventsSubcommandSpec.normalize(args[0])
	switch sub {
	case "list":
		result, err := a.runEventsListCommand(ctx, args[1:], cfg)
		return result, "events list", err
	case "get":
		id, err := parseIDArg(args[1:], "event-id", "event id")
		if err != nil {
			return nil, "events get", err
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "events get", "events.get", map[string]string{"event_id": id}, nil, nil)
		return result, "events get", callErr
	case "create":
		body, dryRun, err := a.parseJSONBodyInputWithOptions(args[1:], "events create", jsonBodyInputOptions{
			allowDryRun: true,
		})
		if err != nil {
			return nil, "events create", err
		}
		if err := validateEventsCreateInput(body, "events create"); err != nil {
			return nil, "events create", err
		}
		if dryRun {
			return dryRunResult("events create", "events.create", nil, nil, body), "events create", nil
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "events create", "events.create", nil, nil, body)
		return result, "events create", callErr
	case "validate":
		body, _, err := a.parseJSONBodyInputWithOptions(args[1:], "events validate", jsonBodyInputOptions{})
		if err != nil {
			return nil, "events validate", err
		}
		if err := validateEventsCreateInput(body, "events validate"); err != nil {
			return nil, "events validate", err
		}
		return validationResult("events validate", "events.create", nil, nil, body), "events validate", nil
	case "stream":
		result, err := a.runEventsStream(ctx, args[1:], cfg, "events stream", false)
		return result, "events stream", err
	case "tail":
		result, err := a.runEventsStream(ctx, args[1:], cfg, "events stream", true)
		return result, "events stream", err
	case "explain":
		result, err := a.runEventsExplainCommand(args[1:])
		return result, "events explain", err
	case "archive":
		fs := newSilentFlagSet("events archive")
		var eventIDFlag trackedString
		var actorIDFlag trackedString
		var reasonFlag trackedString
		fs.Var(&eventIDFlag, "event-id", "Event id to archive")
		fs.Var(&actorIDFlag, "actor-id", "Actor id")
		fs.Var(&reasonFlag, "reason", "Reason for archiving")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "events archive", errnorm.Usage("invalid_flags", err.Error())
		}
		positionals := fs.Args()
		id := strings.TrimSpace(eventIDFlag.value)
		if id == "" && len(positionals) > 0 {
			id = strings.TrimSpace(positionals[0])
			positionals = positionals[1:]
		}
		if err := validateID(id, "event id"); err != nil {
			return nil, "events archive", err
		}
		if len(positionals) > 0 {
			return nil, "events archive", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar events archive`")
		}
		body := map[string]any{}
		actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
		if err != nil {
			return nil, "events archive", err
		}
		if actorID != "" {
			body["actor_id"] = actorID
		} else if strings.TrimSpace(cfg.ActorID) != "" {
			body["actor_id"] = strings.TrimSpace(cfg.ActorID)
		}
		if strings.TrimSpace(reasonFlag.value) != "" {
			body["reason"] = strings.TrimSpace(reasonFlag.value)
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "events archive", "events.archive", map[string]string{"event_id": id}, nil, body)
		return result, "events archive", callErr
	case "unarchive":
		fs := newSilentFlagSet("events unarchive")
		var eventIDFlag trackedString
		var actorIDFlag trackedString
		var reasonFlag trackedString
		fs.Var(&eventIDFlag, "event-id", "Event id to unarchive")
		fs.Var(&actorIDFlag, "actor-id", "Actor id")
		fs.Var(&reasonFlag, "reason", "Reason for unarchiving")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "events unarchive", errnorm.Usage("invalid_flags", err.Error())
		}
		positionals := fs.Args()
		id := strings.TrimSpace(eventIDFlag.value)
		if id == "" && len(positionals) > 0 {
			id = strings.TrimSpace(positionals[0])
			positionals = positionals[1:]
		}
		if err := validateID(id, "event id"); err != nil {
			return nil, "events unarchive", err
		}
		if len(positionals) > 0 {
			return nil, "events unarchive", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar events unarchive`")
		}
		body := map[string]any{}
		actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
		if err != nil {
			return nil, "events unarchive", err
		}
		if actorID != "" {
			body["actor_id"] = actorID
		} else if strings.TrimSpace(cfg.ActorID) != "" {
			body["actor_id"] = strings.TrimSpace(cfg.ActorID)
		}
		if strings.TrimSpace(reasonFlag.value) != "" {
			body["reason"] = strings.TrimSpace(reasonFlag.value)
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "events unarchive", "events.unarchive", map[string]string{"event_id": id}, nil, body)
		return result, "events unarchive", callErr
	case "trash":
		fs := newSilentFlagSet("events trash")
		var eventIDFlag trackedString
		var actorIDFlag trackedString
		var reasonFlag trackedString
		fs.Var(&eventIDFlag, "event-id", "Event id to trash")
		fs.Var(&actorIDFlag, "actor-id", "Actor id")
		fs.Var(&reasonFlag, "reason", "Reason for trashing")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "events trash", errnorm.Usage("invalid_flags", err.Error())
		}
		positionals := fs.Args()
		id := strings.TrimSpace(eventIDFlag.value)
		if id == "" && len(positionals) > 0 {
			id = strings.TrimSpace(positionals[0])
			positionals = positionals[1:]
		}
		if err := validateID(id, "event id"); err != nil {
			return nil, "events trash", err
		}
		if len(positionals) > 0 {
			return nil, "events trash", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar events trash`")
		}
		body := map[string]any{}
		actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
		if err != nil {
			return nil, "events trash", err
		}
		if actorID != "" {
			body["actor_id"] = actorID
		} else if strings.TrimSpace(cfg.ActorID) != "" {
			body["actor_id"] = strings.TrimSpace(cfg.ActorID)
		}
		if strings.TrimSpace(reasonFlag.value) != "" {
			body["reason"] = strings.TrimSpace(reasonFlag.value)
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "events trash", "events.trash", map[string]string{"event_id": id}, nil, body)
		return result, "events trash", callErr
	case "restore":
		fs := newSilentFlagSet("events restore")
		var eventIDFlag trackedString
		var actorIDFlag trackedString
		var reasonFlag trackedString
		fs.Var(&eventIDFlag, "event-id", "Event id to restore")
		fs.Var(&actorIDFlag, "actor-id", "Actor id")
		fs.Var(&reasonFlag, "reason", "Reason for restoring")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "events restore", errnorm.Usage("invalid_flags", err.Error())
		}
		positionals := fs.Args()
		id := strings.TrimSpace(eventIDFlag.value)
		if id == "" && len(positionals) > 0 {
			id = strings.TrimSpace(positionals[0])
			positionals = positionals[1:]
		}
		if err := validateID(id, "event id"); err != nil {
			return nil, "events restore", err
		}
		if len(positionals) > 0 {
			return nil, "events restore", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar events restore`")
		}
		body := map[string]any{}
		actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
		if err != nil {
			return nil, "events restore", err
		}
		if actorID != "" {
			body["actor_id"] = actorID
		} else if strings.TrimSpace(cfg.ActorID) != "" {
			body["actor_id"] = strings.TrimSpace(cfg.ActorID)
		}
		if strings.TrimSpace(reasonFlag.value) != "" {
			body["reason"] = strings.TrimSpace(reasonFlag.value)
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "events restore", "events.restore", map[string]string{"event_id": id}, nil, body)
		return result, "events restore", callErr
	default:
		return nil, "events", eventsSubcommandSpec.unknownError(args[0])
	}
}

func (a *App) runArtifactsInspectCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	id, err := parseIDArg(args, "artifact-id", "artifact id")
	if err != nil {
		return nil, err
	}

	metadataResult, callErr := a.invokeTypedJSONWithIDResolution(
		ctx,
		cfg,
		"artifacts inspect",
		"artifacts.get",
		"artifact_id",
		id,
		artifactIDLookupSpec,
		nil,
		nil,
	)
	if callErr != nil {
		return nil, callErr
	}

	metadataData := asMap(metadataResult.Data)
	metadataBody := asMap(metadataData["body"])
	artifact := extractNestedMap(metadataBody, "artifact")
	artifactID := strings.TrimSpace(anyString(artifact["id"]))
	if artifactID == "" {
		artifactID = id
	}

	contentCfg := cfg
	contentCfg.JSON = true
	contentResult, contentErr := a.invokeArtifactContentWithIDResolution(
		ctx,
		contentCfg,
		"artifacts inspect",
		"artifact_id",
		artifactID,
		artifactIDLookupSpec,
	)
	if contentErr != nil {
		return nil, contentErr
	}

	contentData := asMap(contentResult.Data)
	contentBody := map[string]any{
		"status_code": contentData["status_code"],
		"headers":     contentData["headers"],
		"body_text":   contentData["body_text"],
		"body_base64": contentData["body_base64"],
	}
	if decoded, decodeErr := base64.StdEncoding.DecodeString(strings.TrimSpace(anyString(contentData["body_base64"]))); decodeErr == nil {
		contentBody["bytes"] = len(decoded)
	}

	body := map[string]any{
		"artifact": artifact,
		"content":  contentBody,
	}
	metadataData["body"] = body
	metadataResult.Data = metadataData
	metadataResult.Text = formatTypedCommandText(
		"artifacts.inspect",
		intValue(metadataData["status_code"]),
		headerValues(metadataData["headers"]),
		body,
		cfg.Verbose,
		cfg.Headers,
	)
	return metadataResult, nil
}

func (a *App) runDocsContentCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	id, err := parseIDArg(args, "document-id", "document id")
	if err != nil {
		return nil, err
	}
	result, callErr := a.invokeTypedJSON(ctx, cfg, "docs content", "docs.get", map[string]string{"document_id": id}, nil, nil)
	if callErr != nil {
		return nil, callErr
	}

	data := asMap(result.Data)
	body := asMap(data["body"])
	document := extractNestedMap(body, "document")
	revision := extractNestedMap(body, "revision")
	content := firstNonEmpty(anyString(revision["content"]), anyString(body["content"]), anyString(body["body_text"]))
	contentBody := map[string]any{
		"document": document,
		"revision": revision,
		"content":  content,
	}
	data["body"] = contentBody
	result.Data = data
	result.Text = formatTypedCommandText(
		"docs.content",
		intValue(data["status_code"]),
		headerValues(data["headers"]),
		contentBody,
		cfg.Verbose,
		cfg.Headers,
	)
	return result, nil
}

func (a *App) runDocsUpdateCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	id, body, err := a.parseDocsUpdateInput(args, "docs update", cfg)
	if err != nil {
		return nil, err
	}
	wireBody := normalizeDocsRevisionRequestForContract(body)
	return a.invokeTypedJSON(ctx, cfg, "docs update", "docs.revisions.create", map[string]string{"document_id": id}, nil, wireBody)
}

func (a *App) runEventsListCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("events list")
	var threadIDFlags trackedStrings
	var typesCSVFlag trackedString
	var actorIDFlag trackedString
	var maxEventsFlag trackedInt
	var mineFlag trackedBool
	var fullIDFlag trackedBool
	var typeFlags trackedStrings
	var includeArchived, archivedOnly, includeTrashed, trashedOnly bool
	fs.Var(&threadIDFlags, "thread-id", "Thread id (repeatable)")
	fs.Var(&typeFlags, "type", "Filter by event type (repeatable)")
	fs.Var(&typesCSVFlag, "types", "Comma-separated event types")
	fs.Var(&actorIDFlag, "actor-id", "Filter by actor id")
	fs.Var(&mineFlag, "mine", "Filter to events authored by active profile actor_id")
	fs.Var(&fullIDFlag, "full-id", "Render full IDs in human output")
	fs.Var(&maxEventsFlag, "max-events", "Return at most N most-recent matching events (0 means unlimited)")
	fs.BoolVar(&includeArchived, "include-archived", false, "Include archived events")
	fs.BoolVar(&archivedOnly, "archived-only", false, "Show only archived events")
	fs.BoolVar(&includeTrashed, "include-trashed", false, "Include trashed events")
	fs.BoolVar(&trashedOnly, "trashed-only", false, "Show only trashed events")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}

	positionals := append([]string(nil), fs.Args()...)
	threadIDs := make([]string, 0, len(threadIDFlags.values)+len(positionals))
	threadIDs = append(threadIDs, threadIDFlags.values...)
	threadIDs = append(threadIDs, positionals...)
	threadIDs = normalizeIDFilters(threadIDs)
	if len(threadIDs) == 0 {
		return nil, errnorm.Usage("invalid_request", "thread id is required (provide --thread-id <thread-id>)")
	}
	for _, threadID := range threadIDs {
		if err := validateID(threadID, "thread id"); err != nil {
			return nil, err
		}
	}
	if maxEventsFlag.set && maxEventsFlag.value < 0 {
		return nil, errnorm.Usage("invalid_request", "--max-events must be >= 0")
	}
	if mineFlag.set && mineFlag.value && strings.TrimSpace(actorIDFlag.value) != "" && strings.TrimSpace(actorIDFlag.value) != "me" {
		return nil, errnorm.Usage("invalid_request", "--mine cannot be combined with --actor-id unless --actor-id=me")
	}

	typeFilters := normalizeEventTypeFilters(typeFlags.values, typesCSVFlag.value)
	actorFilter := strings.TrimSpace(actorIDFlag.value)
	if mineFlag.set && mineFlag.value {
		actorFilter = "me"
	}
	resolvedActorID, err := resolveActorIDAlias(actorFilter, cfg)
	if err != nil {
		return nil, err
	}

	resolvedThreadIDs := make([]string, 0, len(threadIDs))
	allEvents := make([]any, 0, 32)
	matching := make([]any, 0, 32)
	expandedArtifacts := make(map[string]any)
	statusCode := http.StatusOK
	headers := map[string][]string{"Content-Type": {"application/json"}}
	capturedResponse := false
	for _, threadID := range threadIDs {
		// `events list` keeps the user-facing registry identity (`events.list`)
		// even though the current implementation composes backing-thread
		// timelines client-side.
		timelineResult, callErr := a.invokeTypedJSONWithIDResolution(
			ctx,
			cfg,
			"events list",
			"threads.timeline",
			"thread_id",
			threadID,
			threadIDLookupSpec,
			nil,
			nil,
		)
		if callErr != nil {
			return nil, callErr
		}
		data := asMap(timelineResult.Data)
		body := asMap(data["body"])
		if !capturedResponse {
			capturedResponse = true
			if code := intValue(data["status_code"]); code > 0 {
				statusCode = code
			}
			if responseHeaders := headerValues(data["headers"]); len(responseHeaders) > 0 {
				headers = responseHeaders
			}
		}
		threadEvents := asSlice(body["events"])
		allEvents = append(allEvents, threadEvents...)
		filtered := filterEventsByType(threadEvents, typeFilters)
		filtered = filterEventsByActorID(filtered, resolvedActorID)
		filtered = filterEventsByLifecycleState(filtered, includeArchived, archivedOnly, includeTrashed, trashedOnly)
		matching = append(matching, filtered...)
		mergeExpandedTimelineObjects(expandedArtifacts, asMap(body["artifacts"]))
		resolvedThreadID := firstNonEmpty(anyString(body["thread_id"]), eventThreadIDFromList(threadEvents), threadID)
		if resolvedThreadID != "" {
			resolvedThreadIDs = append(resolvedThreadIDs, resolvedThreadID)
		}
	}
	resolvedThreadIDs = normalizeIDFilters(resolvedThreadIDs)
	if len(resolvedThreadIDs) > 1 {
		sortEventsByCreatedAt(matching)
	}
	if maxEventsFlag.set && maxEventsFlag.value > 0 && len(matching) > maxEventsFlag.value {
		matching = matching[len(matching)-maxEventsFlag.value:]
	}
	matching = enrichEventsForList(matching)

	listBody := map[string]any{
		"thread_id":       firstNonEmpty(eventThreadIDFromList(matching), eventThreadIDFromList(allEvents)),
		"thread_ids":      resolvedThreadIDs,
		"full_id":         fullIDFlag.set && fullIDFlag.value,
		"events":          matching,
		"total_events":    len(allEvents),
		"returned_events": len(matching),
		"artifacts":       expandedArtifacts,
	}
	if len(resolvedThreadIDs) == 1 {
		listBody["thread_id"] = resolvedThreadIDs[0]
	}
	if len(typeFilters) > 0 {
		listBody["types"] = typeFilters
	}
	if resolvedActorID != "" {
		listBody["actor_id"] = resolvedActorID
	}
	if maxEventsFlag.set {
		listBody["max_events"] = maxEventsFlag.value
	}
	if includeArchived {
		listBody["include_archived"] = true
	}
	if archivedOnly {
		listBody["archived_only"] = true
	}
	if includeTrashed {
		listBody["include_trashed"] = true
	}
	if trashedOnly {
		listBody["trashed_only"] = true
	}

	resultData := map[string]any{
		"status_code": statusCode,
		"headers":     headers,
		"body":        listBody,
	}
	result := &commandResult{Data: resultData}
	result.Text = formatTypedCommandText(
		"events.list",
		intValue(resultData["status_code"]),
		headerValues(resultData["headers"]),
		listBody,
		cfg.Verbose,
		cfg.Headers,
	)
	return result, nil
}

func (a *App) runEventsExplainCommand(args []string) (*commandResult, error) {
	fs := newSilentFlagSet("events explain")
	var eventTypeFlag trackedString
	fs.Var(&eventTypeFlag, "type", "Event type to explain")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	eventType := strings.TrimSpace(eventTypeFlag.value)
	if eventType == "" && len(positionals) > 0 {
		eventType = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `oar events explain`")
	}
	if eventType == "" {
		textLines := []string{
			"Known event types (open enum; unknown types are still accepted):",
		}
		items := make([]any, 0, len(knownEventTypeGuidance))
		for _, group := range eventTypeGroupOrder {
			groupItems := make([]eventTypeGuidance, 0)
			for _, guidance := range knownEventTypeGuidance {
				if guidance.Group == group {
					groupItems = append(groupItems, guidance)
				}
			}
			if len(groupItems) == 0 {
				continue
			}
			if description := strings.TrimSpace(eventTypeGroupDescriptions[group]); description != "" {
				textLines = append(textLines, "", group+": "+description)
			} else {
				textLines = append(textLines, "", group+":")
			}
			for _, guidance := range groupItems {
				line := "- " + guidance.Type
				summary := strings.TrimSpace(guidance.Summary)
				switch {
				case summary != "" && strings.TrimSpace(guidance.PreferredCommand) != "":
					line += ": " + summary + " Prefer `" + guidance.PreferredCommand + "`."
				case summary != "":
					line += ": " + summary
				case strings.TrimSpace(guidance.PreferredCommand) != "":
					line += ": prefer `" + guidance.PreferredCommand + "`"
				}
				textLines = append(textLines, line)
				items = append(items, map[string]any{
					"type":              guidance.Type,
					"group":             guidance.Group,
					"group_description": eventTypeGroupDescriptions[guidance.Group],
					"summary":           guidance.Summary,
					"preferred_command": guidance.PreferredCommand,
				})
			}
		}
		textLines = append(textLines, "For details: oar events explain <event-type>")
		data := map[string]any{
			"known_event_types": items,
			"hint":              "oar events explain <event-type>",
		}
		return &commandResult{Text: strings.Join(textLines, "\n"), Data: data}, nil
	}

	guidance, ok := eventTypeGuidanceFor(eventType)
	if !ok {
		return nil, errnorm.Usage(
			"invalid_request",
			fmt.Sprintf("unknown event type %q; known types: %s", eventType, strings.Join(knownEventTypeNames(), ", ")),
		)
	}

	textLines := []string{
		"Event type: " + guidance.Type,
		"Group: " + guidance.Group,
	}
	if summary := strings.TrimSpace(guidance.Summary); summary != "" {
		textLines = append(textLines, "Usage hint: "+summary)
	}
	if preferred := strings.TrimSpace(guidance.PreferredCommand); preferred != "" {
		textLines = append(textLines, "Preferred command: "+preferred)
	}
	textLines = append(textLines, "Constraints:")
	for _, constraint := range guidance.Constraints {
		textLines = append(textLines, "- "+constraint)
	}
	data := map[string]any{
		"event_type":        guidance.Type,
		"group":             guidance.Group,
		"group_description": eventTypeGroupDescriptions[guidance.Group],
		"summary":           guidance.Summary,
		"preferred_command": guidance.PreferredCommand,
		"constraints":       append([]string(nil), guidance.Constraints...),
	}
	return &commandResult{Text: strings.Join(textLines, "\n"), Data: data}, nil
}

func (a *App) runInboxCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "inbox", inboxSubcommandSpec.requiredError()
	}
	sub := inboxSubcommandSpec.normalize(args[0])
	switch sub {
	case "list":
		result, err := a.runInboxList(ctx, args[1:], cfg)
		return result, "inbox list", err
	case "get":
		result, commandName, err := a.runInboxGet(ctx, args[1:], cfg)
		return result, commandName, err
	case "acknowledge", "ack":
		body, err := a.parseAckBodyInput(ctx, args[1:], cfg)
		if err != nil {
			return nil, "inbox ack", err
		}
		bodyMap, ok := body.(map[string]any)
		if !ok {
			return nil, "inbox ack", errnorm.Usage("invalid_request", "inbox ack body must be a JSON object")
		}
		inboxItemID := strings.TrimSpace(anyString(bodyMap["inbox_item_id"]))
		if inboxItemID == "" {
			return nil, "inbox ack", errnorm.Usage("invalid_request", "inbox_item_id is required")
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
			"inbox acknowledge",
			"inbox.acknowledge",
			map[string]string{"inbox_id": inboxItemID},
			nil,
			apiBody,
		)
		return result, "inbox acknowledge", callErr
	case "stream":
		result, err := a.runInboxStream(ctx, args[1:], cfg, "inbox stream", false)
		return result, "inbox stream", err
	case "tail":
		result, err := a.runInboxStream(ctx, args[1:], cfg, "inbox stream", true)
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
	fs.Var(&typeFlags, "type", "Filter by inbox item type/category/kind (repeatable)")
	fs.Var(&fullIDFlag, "full-id", "Render full inbox ids in human output")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `oar inbox list`")
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
	typeFilters := normalizeStringFilters(typeFlags.values)

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
	body["category_reference"] = inboxCategoryReferenceMap()
	items := asSlice(body["items"])
	for _, raw := range items {
		item := asMap(raw)
		if item == nil {
			continue
		}
		category := firstNonEmpty(
			strings.TrimSpace(anyString(item["category"])),
			strings.TrimSpace(anyString(item["type"])),
			strings.TrimSpace(anyString(item["kind"])),
		)
		if category == "" {
			continue
		}
		description := inboxCategoryDescription(category)
		if description == "" {
			continue
		}
		if strings.TrimSpace(anyString(item["category_description"])) == description {
			continue
		}
		item["category_description"] = description
		changed = true
	}
	return changed
}

func (a *App) runInboxGet(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	fs := newSilentFlagSet("inbox get")
	var idFlag, inboxIDFlag, inboxItemIDFlag trackedString
	var riskHorizonFlag trackedInt
	fs.Var(&idFlag, "id", "Inbox item id or alias")
	fs.Var(&inboxIDFlag, "inbox-id", "Alias for --id")
	fs.Var(&inboxItemIDFlag, "inbox-item-id", "Inbox item id or alias")
	fs.Var(&riskHorizonFlag, "risk-horizon-days", "Derived inbox risk horizon days")
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
		return nil, "inbox get", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar inbox get`; use `--id <id-or-alias>`")
	}
	query := make([]queryParam, 0, 1)
	if riskHorizonFlag.set {
		addSingleQuery(&query, "risk_horizon_days", fmt.Sprintf("%d", riskHorizonFlag.value))
	}

	if rawID == "" {
		listResult, err := a.invokeTypedJSON(ctx, cfg, "inbox list", "inbox.list", nil, query, nil)
		if err != nil {
			return nil, "inbox get", err
		}
		decorateInboxListResult(listResult, cfg)
		return listResult, "inbox list", nil
	}
	if err := validateID(rawID, "inbox item id"); err != nil {
		return nil, "inbox get", err
	}

	listResult, err := a.invokeTypedJSON(ctx, cfg, "inbox list", "inbox.list", nil, query, nil)
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
		query,
		nil,
	)
	return result, "inbox get", callErr
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

func (a *App) runEventsStream(ctx context.Context, args []string, cfg config.Resolved, commandName string, defaultFollow bool) (*commandResult, error) {
	fs := newSilentFlagSet(commandName)
	var threadIDFlag, typesCSVFlag, lastEventIDFlag, cursorFlag trackedString
	var followFlag trackedBool
	var reconnectFlag trackedBool
	var maxEventsFlag trackedInt
	var typeFlags trackedStrings
	fs.Var(&threadIDFlag, "thread-id", "Stream events for one thread id")
	fs.Var(&typeFlags, "type", "Filter by event type (repeatable)")
	fs.Var(&typesCSVFlag, "types", "Comma-separated event types")
	fs.Var(&followFlag, "follow", "Keep stream open and reconnect when it drops")
	fs.Var(&lastEventIDFlag, "last-event-id", "Resume stream after this event id")
	fs.Var(&cursorFlag, "cursor", "Alias of --last-event-id")
	fs.Var(&reconnectFlag, "reconnect", "Deprecated alias for --follow (default false)")
	fs.Var(&maxEventsFlag, "max-events", "Exit after receiving N events (0 means unlimited)")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `oar %s`", commandName))
	}

	query := make([]queryParam, 0, 4)
	addSingleQuery(&query, "thread_id", threadIDFlag.value)
	addMultiQuery(&query, "type", typeFlags.values)
	addSingleQuery(&query, "types", typesCSVFlag.value)
	lastEventID := firstNonEmpty(lastEventIDFlag.value, cursorFlag.value)
	follow := defaultFollow
	if followFlag.set {
		follow = followFlag.value
	}
	if reconnectFlag.set {
		follow = reconnectFlag.value
	}
	reconnect := follow
	return a.runTailStream(ctx, cfg, commandName, "events.stream", query, lastEventID, follow, reconnect, maxEventsFlag.value)
}

func (a *App) runInboxStream(ctx context.Context, args []string, cfg config.Resolved, commandName string, defaultFollow bool) (*commandResult, error) {
	fs := newSilentFlagSet(commandName)
	var riskHorizonFlag trackedInt
	var lastEventIDFlag, cursorFlag trackedString
	var followFlag trackedBool
	var reconnectFlag trackedBool
	var maxEventsFlag trackedInt
	fs.Var(&riskHorizonFlag, "risk-horizon-days", "Derived inbox risk horizon days")
	fs.Var(&followFlag, "follow", "Keep stream open and reconnect when it drops")
	fs.Var(&lastEventIDFlag, "last-event-id", "Resume stream after this event id")
	fs.Var(&cursorFlag, "cursor", "Alias of --last-event-id")
	fs.Var(&reconnectFlag, "reconnect", "Deprecated alias for --follow (default false)")
	fs.Var(&maxEventsFlag, "max-events", "Exit after receiving N events (0 means unlimited)")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `oar %s`", commandName))
	}

	query := make([]queryParam, 0, 2)
	if riskHorizonFlag.set {
		addSingleQuery(&query, "risk_horizon_days", fmt.Sprintf("%d", riskHorizonFlag.value))
	}
	lastEventID := firstNonEmpty(lastEventIDFlag.value, cursorFlag.value)
	follow := defaultFollow
	if followFlag.set {
		follow = followFlag.value
	}
	if reconnectFlag.set {
		follow = reconnectFlag.value
	}
	reconnect := follow
	return a.runTailStream(ctx, cfg, commandName, "inbox.stream", query, lastEventID, follow, reconnect, maxEventsFlag.value)
}

type jsonBodyInputOptions struct {
	allowDryRun      bool
	allowContentFile bool
}

func (a *App) parseJSONBodyInput(args []string, commandName string) (any, error) {
	body, _, err := a.parseJSONBodyInputWithOptions(args, commandName, jsonBodyInputOptions{})
	return body, err
}

func (a *App) parseJSONBodyInputWithOptions(args []string, commandName string, options jsonBodyInputOptions) (any, bool, error) {
	fs := newSilentFlagSet(commandName)
	var fromFileFlag, contentFileFlag trackedString
	var dryRunFlag trackedBool
	fs.Var(&fromFileFlag, "from-file", "Load JSON body from file path")
	if options.allowContentFile {
		fs.Var(&contentFileFlag, "content-file", "Load request content field from file path")
	}
	if options.allowDryRun {
		fs.Var(&dryRunFlag, "dry-run", "Validate and render request without sending the mutation")
	}
	if err := fs.Parse(args); err != nil {
		return nil, false, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, false, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `oar %s`", commandName))
	}
	payload, err := a.readBodyInput(strings.TrimSpace(fromFileFlag.value))
	if err != nil {
		return nil, false, err
	}
	if len(payload) == 0 {
		return nil, false, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body is required for `oar %s` (provide stdin or --from-file)", commandName))
	}
	body, err := decodeJSONPayload(payload)
	if err != nil {
		return nil, false, err
	}
	if options.allowContentFile {
		body, err = a.applyContentFileOverride(body, strings.TrimSpace(contentFileFlag.value), commandName)
		if err != nil {
			return nil, false, err
		}
	}
	return body, dryRunFlag.set && dryRunFlag.value, nil
}

func (a *App) parseIDAndBodyInput(args []string, idFlag string, idLabel string, commandName string) (string, any, error) {
	id, body, _, err := a.parseIDAndBodyInputWithOptions(args, idFlag, idLabel, commandName, jsonBodyInputOptions{})
	return id, body, err
}

func (a *App) parseIDAndBodyInputWithOptions(args []string, idFlag string, idLabel string, commandName string, options jsonBodyInputOptions) (string, any, bool, error) {
	fs := newSilentFlagSet(commandName)
	var idArgFlag, fromFileFlag, contentFileFlag trackedString
	var dryRunFlag trackedBool
	fs.Var(&idArgFlag, idFlag, idLabel)
	fs.Var(&fromFileFlag, "from-file", "Load JSON body from file path")
	if options.allowContentFile {
		fs.Var(&contentFileFlag, "content-file", "Load request content field from file path")
	}
	if options.allowDryRun {
		fs.Var(&dryRunFlag, "dry-run", "Validate and render request without sending the mutation")
	}
	if err := fs.Parse(args); err != nil {
		return "", nil, false, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	id := strings.TrimSpace(idArgFlag.value)
	if id == "" && len(positionals) > 0 {
		id = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if err := validateID(id, idLabel); err != nil {
		return "", nil, false, err
	}
	if len(positionals) > 0 {
		return "", nil, false, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `oar %s`", commandName))
	}
	payload, err := a.readBodyInput(strings.TrimSpace(fromFileFlag.value))
	if err != nil {
		return "", nil, false, err
	}
	if len(payload) == 0 {
		return "", nil, false, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body is required for `oar %s` (provide stdin or --from-file)", commandName))
	}
	body, err := decodeJSONPayload(payload)
	if err != nil {
		return "", nil, false, err
	}
	if options.allowContentFile {
		body, err = a.applyContentFileOverride(body, strings.TrimSpace(contentFileFlag.value), commandName)
		if err != nil {
			return "", nil, false, err
		}
	}
	return id, body, dryRunFlag.set && dryRunFlag.value, nil
}

func (a *App) parseBoardCardBoardScopedTarget(args []string, commandName string) (string, string, error) {
	fs := newSilentFlagSet(commandName)
	var boardIDFlag, cardIDFlag trackedString
	fs.Var(&boardIDFlag, "board-id", "Board id")
	fs.Var(&cardIDFlag, "card-id", "Card id")
	if err := fs.Parse(args); err != nil {
		return "", "", errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	boardID := strings.TrimSpace(boardIDFlag.value)
	if boardID == "" && len(positionals) > 0 {
		boardID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	cardID := strings.TrimSpace(cardIDFlag.value)
	if cardID == "" && len(positionals) > 0 {
		cardID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return "", "", errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `oar %s`", commandName))
	}
	if err := validateID(boardID, "board id"); err != nil {
		return "", "", err
	}
	if err := validateID(cardID, "card id"); err != nil {
		return "", "", err
	}
	return boardID, cardID, nil
}

func (a *App) parseBoardCardCreateInput(ctx context.Context, args []string, cfg config.Resolved, commandName string) (string, any, error) {
	fs := newSilentFlagSet(commandName)
	var boardIDFlag, cardIDFlag trackedString
	var titleFlag, bodyFlag trackedString
	var actorIDFlag, requestKeyFlag, ifBoardUpdatedAtFlag, fromFileFlag trackedString
	var columnFlag, assigneeFlag, priorityFlag, statusFlag trackedString
	var beforeCardIDFlag, afterCardIDFlag trackedString
	var pinnedDocumentIDFlag trackedString
	fs.Var(&boardIDFlag, "board-id", "Board id")
	fs.Var(&cardIDFlag, "card-id", "Card id")
	fs.Var(&titleFlag, "title", "Card title")
	fs.Var(&bodyFlag, "body", "Card body markdown")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	fs.Var(&requestKeyFlag, "request-key", "Request key")
	fs.Var(&ifBoardUpdatedAtFlag, "if-board-updated-at", "Board updated_at concurrency token")
	fs.Var(&columnFlag, "column", "Target board column key")
	fs.Var(&beforeCardIDFlag, "before-card-id", "Place before this card id in the target column")
	fs.Var(&afterCardIDFlag, "after-card-id", "Place after this card id in the target column")
	fs.Var(&assigneeFlag, "assignee", "Assignee actor reference")
	fs.Var(&priorityFlag, "priority", "Priority")
	fs.Var(&statusFlag, "status", "Card status")
	fs.Var(&pinnedDocumentIDFlag, "pinned-document-id", "Pinned document id")
	fs.Var(&fromFileFlag, "from-file", "Load JSON body from file path")
	if err := fs.Parse(args); err != nil {
		return "", nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	boardID := strings.TrimSpace(boardIDFlag.value)
	if boardID == "" && len(positionals) > 0 {
		boardID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return "", nil, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `oar %s`", commandName))
	}
	if err := validateID(boardID, "board id"); err != nil {
		return "", nil, err
	}
	if err := validatePlacementFlags(beforeCardIDFlag.value, afterCardIDFlag.value, commandName); err != nil {
		return "", nil, err
	}

	skipStdin := hasAnyBoardMutationFieldFlags(cardIDFlag, titleFlag, bodyFlag, actorIDFlag, requestKeyFlag, ifBoardUpdatedAtFlag, columnFlag, beforeCardIDFlag, afterCardIDFlag, assigneeFlag, priorityFlag, statusFlag, pinnedDocumentIDFlag)
	payload, err := a.readBoardCardBodyInput(fromFileFlag.value, skipStdin)
	if err != nil {
		return "", nil, err
	}
	if len(payload) > 0 {
		if hasAnyBoardMutationFieldFlags(cardIDFlag, titleFlag, bodyFlag, actorIDFlag, requestKeyFlag, ifBoardUpdatedAtFlag, columnFlag, beforeCardIDFlag, afterCardIDFlag, assigneeFlag, priorityFlag, statusFlag, pinnedDocumentIDFlag) {
			return "", nil, errnorm.Usage("invalid_args", fmt.Sprintf("field flags cannot be combined with JSON body input for `oar %s`", commandName))
		}
		body, err := decodeJSONPayload(payload)
		if err != nil {
			return "", nil, err
		}
		bodyMap, _ := body.(map[string]any)
		if bodyMap == nil {
			return "", nil, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body for `oar %s` must be an object", commandName))
		}
		resolvedBoardID, err := a.resolveMaybeBoardID(ctx, cfg, boardID)
		if err != nil {
			return "", nil, err
		}
		if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoardID, bodyMap, "before_card_id"); err != nil {
			return "", nil, err
		}
		if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoardID, bodyMap, "after_card_id"); err != nil {
			return "", nil, err
		}
		if cardNest, ok := bodyMap["card"].(map[string]any); ok && cardNest != nil {
			if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoardID, cardNest, "before_card_id"); err != nil {
				return "", nil, err
			}
			if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoardID, cardNest, "after_card_id"); err != nil {
				return "", nil, err
			}
		}
		return resolvedBoardID, bodyMap, nil
	}

	body := map[string]any{}
	actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
	if err != nil {
		return "", nil, err
	}
	if actorID != "" {
		body["actor_id"] = actorID
	}
	if cardID := strings.TrimSpace(cardIDFlag.value); cardID != "" {
		body["card_id"] = cardID
	}
	if requestKey := strings.TrimSpace(requestKeyFlag.value); requestKey != "" {
		body["request_key"] = requestKey
	}
	if ifBoardUpdatedAt := strings.TrimSpace(ifBoardUpdatedAtFlag.value); ifBoardUpdatedAt != "" {
		body["if_board_updated_at"] = ifBoardUpdatedAt
	}
	if title := strings.TrimSpace(titleFlag.value); title != "" {
		body["title"] = title
	}
	if bodyText := strings.TrimSpace(bodyFlag.value); bodyText != "" {
		body["body"] = bodyText
	}
	if column := strings.TrimSpace(columnFlag.value); column != "" {
		body["column_key"] = column
	}
	resolvedBoardID, err := a.resolveMaybeBoardID(ctx, cfg, boardID)
	if err != nil {
		return "", nil, err
	}
	if beforeCard := strings.TrimSpace(beforeCardIDFlag.value); beforeCard != "" {
		resolved, err := a.resolveMaybeBoardCardID(ctx, cfg, resolvedBoardID, beforeCard)
		if err != nil {
			return "", nil, err
		}
		body["before_card_id"] = resolved
	}
	if afterCard := strings.TrimSpace(afterCardIDFlag.value); afterCard != "" {
		resolved, err := a.resolveMaybeBoardCardID(ctx, cfg, resolvedBoardID, afterCard)
		if err != nil {
			return "", nil, err
		}
		body["after_card_id"] = resolved
	}
	if assignee := strings.TrimSpace(assigneeFlag.value); assignee != "" {
		body["assignee"] = assignee
	}
	if priority := strings.TrimSpace(priorityFlag.value); priority != "" {
		body["priority"] = priority
	}
	if status := strings.TrimSpace(statusFlag.value); status != "" {
		body["status"] = status
	}
	if pinnedDocumentID := strings.TrimSpace(pinnedDocumentIDFlag.value); pinnedDocumentID != "" {
		body["pinned_document_id"] = pinnedDocumentID
	}
	return resolvedBoardID, body, nil
}

func (a *App) parseBoardCardUpdateInput(ctx context.Context, args []string, cfg config.Resolved, commandName string) (map[string]string, any, error) {
	fs := newSilentFlagSet(commandName)
	var cardIDFlag trackedString
	var titleFlag, bodyFlag trackedString
	var actorIDFlag, ifUpdatedAtFlag, fromFileFlag trackedString
	var assigneeFlag, priorityFlag, statusFlag trackedString
	var pinnedDocumentIDFlag trackedString
	var clearPinnedDocumentFlag trackedBool
	fs.Var(&cardIDFlag, "card-id", "Card id")
	fs.Var(&titleFlag, "title", "Card title")
	fs.Var(&bodyFlag, "body", "Card body markdown")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	fs.Var(&ifUpdatedAtFlag, "if-updated-at", "Card updated_at concurrency token")
	fs.Var(&assigneeFlag, "assignee", "Assignee actor reference")
	fs.Var(&priorityFlag, "priority", "Priority")
	fs.Var(&statusFlag, "status", "Card status")
	fs.Var(&pinnedDocumentIDFlag, "pinned-document-id", "Pinned document id")
	fs.Var(&fromFileFlag, "from-file", "Load JSON body from file path")
	fs.Var(&clearPinnedDocumentFlag, "clear-pinned-document", "Clear the pinned document id")
	if err := fs.Parse(args); err != nil {
		return nil, nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	cardID := strings.TrimSpace(cardIDFlag.value)
	if cardID == "" && len(positionals) > 0 {
		cardID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return nil, nil, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `oar %s`", commandName))
	}
	if err := validateID(cardID, "card id"); err != nil {
		return nil, nil, err
	}

	skipStdin := hasAnyBoardMutationFieldFlags(titleFlag, bodyFlag, actorIDFlag, ifUpdatedAtFlag, assigneeFlag, priorityFlag, statusFlag, pinnedDocumentIDFlag, clearPinnedDocumentFlag)
	payload, err := a.readBoardCardBodyInput(fromFileFlag.value, skipStdin)
	if err != nil {
		return nil, nil, err
	}
	if len(payload) > 0 {
		if hasAnyBoardMutationFieldFlags(titleFlag, bodyFlag, actorIDFlag, ifUpdatedAtFlag, assigneeFlag, priorityFlag, statusFlag, pinnedDocumentIDFlag, clearPinnedDocumentFlag) {
			return nil, nil, errnorm.Usage("invalid_args", fmt.Sprintf("field flags cannot be combined with JSON body input for `oar %s`", commandName))
		}
		body, err := decodeJSONPayload(payload)
		if err != nil {
			return nil, nil, err
		}
		bodyMap, _ := body.(map[string]any)
		if bodyMap == nil {
			return nil, nil, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body for `oar %s` must be an object", commandName))
		}
		pathParams, err := buildBoardCardCommandPathParams(cardID)
		if err != nil {
			return nil, nil, err
		}
		return pathParams, bodyMap, nil
	}

	patch := map[string]any{}
	if title := strings.TrimSpace(titleFlag.value); title != "" {
		patch["title"] = title
	}
	if bodyText := strings.TrimSpace(bodyFlag.value); bodyText != "" {
		patch["body"] = bodyText
	}
	if assignee := strings.TrimSpace(assigneeFlag.value); assignee != "" {
		patch["assignee"] = assignee
	}
	if priority := strings.TrimSpace(priorityFlag.value); priority != "" {
		patch["priority"] = priority
	}
	if status := strings.TrimSpace(statusFlag.value); status != "" {
		patch["status"] = status
	}
	if clearPinnedDocumentFlag.set && clearPinnedDocumentFlag.value {
		if strings.TrimSpace(pinnedDocumentIDFlag.value) != "" {
			return nil, nil, errnorm.Usage("invalid_request", fmt.Sprintf("--pinned-document-id and --clear-pinned-document cannot be combined for `oar %s`", commandName))
		}
		patch["pinned_document_id"] = nil
	} else if pinnedDocumentID := strings.TrimSpace(pinnedDocumentIDFlag.value); pinnedDocumentID != "" {
		patch["pinned_document_id"] = pinnedDocumentID
	}
	body := map[string]any{
		"patch": patch,
	}
	if ifUpdated := strings.TrimSpace(ifUpdatedAtFlag.value); ifUpdated != "" {
		body["if_updated_at"] = ifUpdated
	}
	actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
	if err != nil {
		return nil, nil, err
	}
	if actorID != "" {
		body["actor_id"] = actorID
	}
	pathParams, err := buildBoardCardCommandPathParams(cardID)
	if err != nil {
		return nil, nil, err
	}
	return pathParams, body, nil
}

func (a *App) parseBoardCardMoveInput(ctx context.Context, args []string, cfg config.Resolved, commandName string) (string, string, any, error) {
	fs := newSilentFlagSet(commandName)
	var boardIDFlag, cardIDFlag trackedString
	var actorIDFlag, ifBoardUpdatedAtFlag, fromFileFlag trackedString
	var columnFlag, beforeCardIDFlag, afterCardIDFlag trackedString
	fs.Var(&boardIDFlag, "board-id", "Board id")
	fs.Var(&cardIDFlag, "card-id", "Card id")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	fs.Var(&ifBoardUpdatedAtFlag, "if-board-updated-at", "Board updated_at concurrency token")
	fs.Var(&fromFileFlag, "from-file", "Load JSON body from file path")
	fs.Var(&columnFlag, "column", "Target board column key")
	fs.Var(&beforeCardIDFlag, "before-card-id", "Place before this card id")
	fs.Var(&afterCardIDFlag, "after-card-id", "Place after this card id")
	if err := fs.Parse(args); err != nil {
		return "", "", nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	boardID := strings.TrimSpace(boardIDFlag.value)
	if boardID == "" && len(positionals) > 0 {
		boardID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	cardID := strings.TrimSpace(cardIDFlag.value)
	if cardID == "" && len(positionals) > 0 {
		cardID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return "", "", nil, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `oar %s`", commandName))
	}
	if err := validatePlacementFlags(beforeCardIDFlag.value, afterCardIDFlag.value, commandName); err != nil {
		return "", "", nil, err
	}
	if err := validateID(boardID, "board id"); err != nil {
		return "", "", nil, err
	}
	identifier := strings.TrimSpace(cardID)
	if err := validateID(identifier, "card id"); err != nil {
		return "", "", nil, err
	}
	resolvedBoardID, err := a.resolveMaybeBoardID(ctx, cfg, boardID)
	if err != nil {
		return "", "", nil, err
	}
	skipStdin := hasAnyBoardMutationFieldFlags(actorIDFlag, ifBoardUpdatedAtFlag, columnFlag, beforeCardIDFlag, afterCardIDFlag)
	payload, err := a.readBoardCardBodyInput(fromFileFlag.value, skipStdin)
	if err != nil {
		return "", "", nil, err
	}
	if len(payload) > 0 {
		if hasAnyBoardMutationFieldFlags(actorIDFlag, ifBoardUpdatedAtFlag, columnFlag, beforeCardIDFlag, afterCardIDFlag) {
			return "", "", nil, errnorm.Usage("invalid_args", fmt.Sprintf("field flags cannot be combined with JSON body input for `oar %s`", commandName))
		}
		body, err := decodeJSONPayload(payload)
		if err != nil {
			return "", "", nil, err
		}
		bodyMap, _ := body.(map[string]any)
		if bodyMap == nil {
			return "", "", nil, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body for `oar %s` must be an object", commandName))
		}
		if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoardID, bodyMap, "before_card_id"); err != nil {
			return "", "", nil, err
		}
		if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoardID, bodyMap, "after_card_id"); err != nil {
			return "", "", nil, err
		}
		return resolvedBoardID, identifier, bodyMap, nil
	}

	body := map[string]any{
		"if_board_updated_at": strings.TrimSpace(ifBoardUpdatedAtFlag.value),
	}
	actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
	if err != nil {
		return "", "", nil, err
	}
	if actorID != "" {
		body["actor_id"] = actorID
	}
	if column := strings.TrimSpace(columnFlag.value); column != "" {
		body["column_key"] = column
	}
	if beforeCardID := strings.TrimSpace(beforeCardIDFlag.value); beforeCardID != "" {
		resolved, err := a.resolveMaybeBoardCardID(ctx, cfg, resolvedBoardID, beforeCardID)
		if err != nil {
			return "", "", nil, err
		}
		body["before_card_id"] = resolved
	}
	if afterCardID := strings.TrimSpace(afterCardIDFlag.value); afterCardID != "" {
		resolved, err := a.resolveMaybeBoardCardID(ctx, cfg, resolvedBoardID, afterCardID)
		if err != nil {
			return "", "", nil, err
		}
		body["after_card_id"] = resolved
	}
	return resolvedBoardID, identifier, body, nil
}

func (a *App) parseBoardCardArchiveInput(ctx context.Context, args []string, cfg config.Resolved, commandName string) (map[string]string, any, error) {
	fs := newSilentFlagSet(commandName)
	var cardIDFlag trackedString
	var actorIDFlag, ifBoardUpdatedAtFlag, fromFileFlag trackedString
	fs.Var(&cardIDFlag, "card-id", "Card id")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	fs.Var(&ifBoardUpdatedAtFlag, "if-board-updated-at", "Board updated_at concurrency token")
	fs.Var(&fromFileFlag, "from-file", "Load JSON body from file path")
	if err := fs.Parse(args); err != nil {
		return nil, nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	cardID := strings.TrimSpace(cardIDFlag.value)
	if cardID == "" && len(positionals) > 0 {
		cardID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return nil, nil, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `oar %s`", commandName))
	}
	if err := validateID(cardID, "card id"); err != nil {
		return nil, nil, err
	}

	skipStdin := hasAnyBoardMutationFieldFlags(actorIDFlag, ifBoardUpdatedAtFlag)
	payload, err := a.readBoardCardBodyInput(fromFileFlag.value, skipStdin)
	if err != nil {
		return nil, nil, err
	}
	if len(payload) > 0 {
		if hasAnyBoardMutationFieldFlags(actorIDFlag, ifBoardUpdatedAtFlag) {
			return nil, nil, errnorm.Usage("invalid_args", fmt.Sprintf("field flags cannot be combined with JSON body input for `oar %s`", commandName))
		}
		body, err := decodeJSONPayload(payload)
		if err != nil {
			return nil, nil, err
		}
		bodyMap, _ := body.(map[string]any)
		if bodyMap == nil {
			return nil, nil, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body for `oar %s` must be an object", commandName))
		}
		pathParams, err := buildBoardCardCommandPathParams(cardID)
		if err != nil {
			return nil, nil, err
		}
		return pathParams, bodyMap, nil
	}

	body := map[string]any{}
	actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
	if err != nil {
		return nil, nil, err
	}
	if actorID != "" {
		body["actor_id"] = actorID
	}
	if ifBoardUpdatedAt := strings.TrimSpace(ifBoardUpdatedAtFlag.value); ifBoardUpdatedAt != "" {
		body["if_board_updated_at"] = ifBoardUpdatedAt
	}
	pathParams, err := buildBoardCardCommandPathParams(cardID)
	if err != nil {
		return nil, nil, err
	}
	return pathParams, body, nil
}

func buildBoardCardCommandPathParams(cardID string) (map[string]string, error) {
	cardID = strings.TrimSpace(cardID)
	if cardID == "" {
		return nil, errnorm.Usage("invalid_request", "card id is required")
	}
	return map[string]string{"card_id": cardID}, nil
}

func (a *App) normalizeBoardMutationCardAnchorField(ctx context.Context, cfg config.Resolved, boardID string, body map[string]any, field string) error {
	rawID := strings.TrimSpace(anyString(body[field]))
	if rawID == "" {
		return nil
	}
	resolvedID, err := a.resolveMaybeBoardCardID(ctx, cfg, boardID, rawID)
	if err != nil {
		return err
	}
	body[field] = resolvedID
	return nil
}

func (a *App) resolveMaybeBoardCardID(ctx context.Context, cfg config.Resolved, boardID, rawCardID string) (string, error) {
	rawCardID = strings.TrimSpace(rawCardID)
	if rawCardID == "" {
		return "", nil
	}
	if !shouldResolveDisplayedShortID(rawCardID) {
		return rawCardID, nil
	}
	resolvedBoard, err := a.resolveMaybeBoardID(ctx, cfg, boardID)
	if err != nil {
		return "", err
	}
	result, err := a.invokeTypedJSON(ctx, cfg, "boards cards list", "boards.cards.list", map[string]string{"board_id": resolvedBoard}, nil, nil)
	if err != nil {
		return "", err
	}
	ids := listResourceIDs(result, boardCardIDLookupSpec)
	if len(ids) == 0 {
		return "", missingResourceIDError(rawCardID, boardCardIDLookupSpec)
	}
	for _, id := range ids {
		if id == rawCardID {
			return id, nil
		}
	}
	matches := make([]string, 0, len(ids))
	for _, id := range ids {
		if strings.HasPrefix(id, rawCardID) {
			matches = append(matches, id)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		sort.Strings(matches)
		return "", ambiguousResourceIDError(rawCardID, boardCardIDLookupSpec, matches)
	}
	return "", missingResourceIDError(rawCardID, boardCardIDLookupSpec)
}

func validatePlacementFlags(before string, after string, commandName string) error {
	if strings.TrimSpace(before) != "" && strings.TrimSpace(after) != "" {
		return errnorm.Usage("invalid_request", fmt.Sprintf("--before-card-id and --after-card-id cannot be combined for `oar %s`", commandName))
	}
	return nil
}

func hasAnyBoardMutationFieldFlags(values ...any) bool {
	for _, value := range values {
		switch typed := value.(type) {
		case trackedString:
			if strings.TrimSpace(typed.value) != "" {
				return true
			}
		case trackedBool:
			if typed.set {
				return true
			}
		}
	}
	return false
}

func (a *App) resolveMaybeBoardID(ctx context.Context, cfg config.Resolved, rawID string) (string, error) {
	if !shouldResolveDisplayedShortID(rawID) {
		return rawID, nil
	}
	return a.resolveResourceIDFromList(ctx, cfg, rawID, boardIDLookupSpec)
}

func (a *App) resolveMaybeThreadID(ctx context.Context, cfg config.Resolved, rawID string) (string, error) {
	if !shouldResolveDisplayedShortID(rawID) {
		return rawID, nil
	}
	resolved, err := a.resolveThreadIDFilters(ctx, cfg, []string{rawID})
	if err != nil {
		return "", err
	}
	if len(resolved) == 0 {
		return rawID, nil
	}
	return resolved[0], nil
}

func parseIDArg(args []string, idFlag string, idLabel string) (string, error) {
	fs := newSilentFlagSet(idLabel)
	var idArgFlag trackedString
	fs.Var(&idArgFlag, idFlag, idLabel)
	if err := fs.Parse(args); err != nil {
		return "", errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	id := strings.TrimSpace(idArgFlag.value)
	if id == "" && len(positionals) > 0 {
		id = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return "", errnorm.Usage("invalid_args", "too many positional arguments")
	}
	if err := validateID(id, idLabel); err != nil {
		return "", err
	}
	return id, nil
}

func validateID(id string, label string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errnorm.Usage("invalid_request", fmt.Sprintf("%s is required", label))
	}
	if !idPattern.MatchString(id) {
		return errnorm.Usage("invalid_request", fmt.Sprintf("%s %q contains invalid characters", label, id))
	}
	return nil
}

func validateTypedRefShape(ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return errnorm.Usage("invalid_request", "subject_ref is required")
	}
	idx := strings.Index(ref, ":")
	if idx <= 0 || idx >= len(ref)-1 {
		return errnorm.Usage("invalid_request", fmt.Sprintf("subject_ref %q must be a typed ref (prefix:id)", ref))
	}
	return nil
}

func (a *App) parseDerivedRebuildBodyInput(args []string, cfg config.Resolved) (any, error) {
	fs := newSilentFlagSet("derived rebuild")
	var fromFileFlag, actorIDFlag trackedString
	fs.Var(&fromFileFlag, "from-file", "Load JSON body from file path")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `oar derived rebuild`")
	}

	payload, err := a.readBodyInput(strings.TrimSpace(fromFileFlag.value))
	if err != nil {
		return nil, err
	}

	var body map[string]any
	if len(payload) == 0 {
		body = map[string]any{}
	} else {
		decoded, decodeErr := decodeJSONPayload(payload)
		if decodeErr != nil {
			return nil, decodeErr
		}
		parsed, ok := decoded.(map[string]any)
		if !ok {
			return nil, errnorm.Usage("invalid_request", "JSON body for `oar derived rebuild` must be an object")
		}
		body = parsed
	}

	actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
	if err != nil {
		return nil, err
	}
	if actorID != "" {
		body["actor_id"] = actorID
	}
	return body, nil
}

func (a *App) parseAckBodyInput(ctx context.Context, args []string, cfg config.Resolved) (any, error) {
	fs := newSilentFlagSet("inbox ack")
	var fromFileFlag, subjectRefFlag, inboxItemIDFlag, actorIDFlag trackedString
	fs.Var(&fromFileFlag, "from-file", "Load JSON body from file path")
	fs.Var(&subjectRefFlag, "subject-ref", "Subject ref")
	fs.Var(&inboxItemIDFlag, "inbox-item-id", "Inbox item id")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()

	payload, err := a.readBodyInput(strings.TrimSpace(fromFileFlag.value))
	if err != nil {
		return nil, err
	}
	if len(payload) > 0 {
		if len(positionals) > 0 {
			return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `oar inbox ack`")
		}
		return decodeJSONPayload(payload)
	}

	subjectRef := strings.TrimSpace(subjectRefFlag.value)
	inboxItemID := strings.TrimSpace(inboxItemIDFlag.value)
	if inboxItemID == "" && len(positionals) > 0 {
		inboxItemID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `oar inbox ack`")
	}
	if err := validateID(inboxItemID, "inbox item id"); err != nil {
		return nil, err
	}

	shouldResolveInboxItem := subjectRef == "" || looksLikeInboxAlias(inboxItemID)
	if shouldResolveInboxItem {
		resolvedInboxItemID, resolvedSubjectRef, err := a.resolveInboxItemIDAndThread(ctx, cfg, inboxItemID)
		if err != nil {
			return nil, err
		}
		inboxItemID = resolvedInboxItemID
		if subjectRef == "" {
			subjectRef = resolvedSubjectRef
		}
	}
	if err := validateTypedRefShape(subjectRef); err != nil {
		return nil, err
	}

	actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"subject_ref":   subjectRef,
		"inbox_item_id": inboxItemID,
	}
	if actorID != "" {
		body["actor_id"] = actorID
	}
	return body, nil
}

func looksLikeInboxAlias(raw string) bool {
	raw = strings.ToLower(strings.TrimSpace(raw))
	return strings.HasPrefix(raw, inboxAliasPrefix)
}

func (a *App) resolveInboxItemIDAndThread(ctx context.Context, cfg config.Resolved, inboxItemID string) (string, string, error) {
	result, err := a.invokeTypedJSON(ctx, cfg, "inbox list", "inbox.list", nil, nil, nil)
	if err != nil {
		return "", "", err
	}
	match, err := resolveInboxItemFromListResult(result, inboxItemID)
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

type inboxListMatch struct {
	ID       string
	ShortID  string
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
		if item.ShortID != "" && item.ShortID == rawID {
			return item, nil
		}
	}

	prefixMatches := make([]inboxListMatch, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		match := strings.HasPrefix(item.ID, rawID)
		if !match && item.Alias != "" {
			match = strings.HasPrefix(item.Alias, rawLower)
		}
		if !match && item.ShortID != "" {
			match = strings.HasPrefix(item.ShortID, rawID)
		}
		if !match {
			continue
		}
		if _, exists := seen[item.ID]; exists {
			continue
		}
		seen[item.ID] = struct{}{}
		prefixMatches = append(prefixMatches, item)
	}
	if len(prefixMatches) == 1 {
		return prefixMatches[0], nil
	}
	if len(prefixMatches) > 1 {
		sort.Slice(prefixMatches, func(i int, j int) bool {
			return prefixMatches[i].ID < prefixMatches[j].ID
		})
		return inboxListMatch{}, ambiguousInboxItemIDError(rawID, prefixMatches)
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
			ShortID:  strings.TrimSpace(anyString(item["short_id"])),
			Alias:    strings.ToLower(strings.TrimSpace(anyString(item["alias"]))),
			ThreadID: strings.TrimSpace(anyString(item["thread_id"])),
			Item:     item,
		})
	}
	return out
}

func ambiguousInboxItemIDError(rawID string, matches []inboxListMatch) error {
	samples := make([]string, 0, minInt(3, len(matches)))
	for idx, match := range matches {
		if idx >= 3 {
			break
		}
		samples = append(samples, fmt.Sprintf("%s (alias=%s)", match.ID, match.Alias))
	}
	return errnorm.Usage(
		"invalid_request",
		fmt.Sprintf(
			"inbox item id %q is ambiguous: %d inbox items match. Use a longer id/alias or the canonical id. Matches: %s",
			rawID,
			len(matches),
			strings.Join(samples, ", "),
		),
	)
}

func missingInboxItemIDError(rawID string) error {
	return errnorm.Usage(
		"invalid_request",
		fmt.Sprintf(
			"inbox item id %q is missing: no canonical id, alias, or unique prefix match was found. Run `oar inbox list` and retry with alias or canonical id.",
			strings.TrimSpace(rawID),
		),
	)
}

func resolveActorIDAlias(raw string, cfg config.Resolved) (string, error) {
	actorID := strings.TrimSpace(raw)
	if actorID == "" {
		return "", nil
	}
	if actorID != "me" {
		return actorID, nil
	}
	resolved := strings.TrimSpace(cfg.ActorID)
	if resolved != "" {
		return resolved, nil
	}
	return "", errnorm.Usage(
		"invalid_request",
		fmt.Sprintf("--actor-id me requires actor_id in active profile (%s)", strings.TrimSpace(cfg.ProfilePath)),
	)
}

func (a *App) resolveResourceIDFromList(ctx context.Context, cfg config.Resolved, rawID string, spec resourceIDLookupSpec) (string, error) {
	result, err := a.invokeTypedJSON(ctx, cfg, spec.listCommand, spec.listCommandID, nil, nil, nil)
	if err != nil {
		return "", err
	}
	ids := listResourceIDs(result, spec)
	if len(ids) == 0 {
		return "", missingResourceIDError(rawID, spec)
	}

	rawID = strings.TrimSpace(rawID)
	for _, id := range ids {
		if id == rawID {
			return id, nil
		}
	}

	matches := make([]string, 0, len(ids))
	for _, id := range ids {
		if strings.HasPrefix(id, rawID) {
			matches = append(matches, id)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		sort.Strings(matches)
		return "", ambiguousResourceIDError(rawID, spec, matches)
	}
	return "", missingResourceIDError(rawID, spec)
}

func listResourceIDs(result *commandResult, spec resourceIDLookupSpec) []string {
	if result == nil {
		return nil
	}
	data, _ := result.Data.(map[string]any)
	body, _ := data["body"].(map[string]any)
	if body == nil {
		return nil
	}
	rawItems, _ := body[spec.listField].([]any)
	if len(rawItems) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(rawItems))
	out := make([]string, 0, len(rawItems))
	for _, rawItem := range rawItems {
		item, _ := rawItem.(map[string]any)
		if item == nil {
			continue
		}
		id := extractResourceListItemID(item, spec)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func extractResourceListItemID(item map[string]any, spec resourceIDLookupSpec) string {
	path := spec.idFieldPath
	if len(path) == 0 {
		path = []string{"id"}
	}
	var current any = item
	for _, segment := range path {
		typed, _ := current.(map[string]any)
		if typed == nil {
			return ""
		}
		current = typed[segment]
	}
	return strings.TrimSpace(anyString(current))
}

func isResolvableResourceNotFoundError(err error, spec resourceIDLookupSpec) bool {
	normalized := errnorm.Normalize(err)
	if normalized == nil || normalized.Kind != errnorm.KindRemote || normalized.Code != "not_found" {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(normalized.Message))
	if message == "" || message == "endpoint not found" {
		return false
	}
	for _, hint := range spec.notFoundHints {
		if message == strings.ToLower(strings.TrimSpace(hint)) {
			return true
		}
	}
	return false
}

func ambiguousResourceIDError(rawID string, spec resourceIDLookupSpec, matches []string) error {
	samples := make([]string, 0, minInt(3, len(matches)))
	for idx, match := range matches {
		if idx >= 3 {
			break
		}
		samples = append(samples, fmt.Sprintf("%s (short_id=%s)", match, shortID(match)))
	}
	message := fmt.Sprintf(
		"%s %q is ambiguous: %d %s ids share that prefix. Use a longer prefix or the canonical id. Matches: %s",
		spec.idLabel,
		rawID,
		len(matches),
		spec.resource,
		strings.Join(samples, ", "),
	)
	return errnorm.Usage("invalid_request", message)
}

func missingResourceIDError(rawID string, spec resourceIDLookupSpec) error {
	message := fmt.Sprintf(
		"%s %q is missing: no canonical %s id or unique prefix match was found. If this value was truncated, run `oar %s` and retry with a unique short_id or canonical id.",
		spec.idLabel,
		rawID,
		spec.resource,
		spec.listCommand,
	)
	return errnorm.Usage("invalid_request", message)
}

func enrichListBodyWithShortIDs(commandID string, body any) (any, bool) {
	typedBody, _ := body.(map[string]any)
	if typedBody == nil {
		return body, false
	}
	switch strings.TrimSpace(commandID) {
	case "threads.list":
		return body, addShortIDToListField(typedBody, "threads")
	case "artifacts.list":
		return body, addShortIDToListField(typedBody, "artifacts")
	case "boards.list":
		return body, addShortIDToNestedListField(typedBody, "boards", []string{"board"})
	case "inbox.list":
		return body, addInboxAliasesToListField(typedBody, "items")
	case "inbox.get":
		return body, addInboxAliasToItemField(typedBody, "item")
	default:
		return body, false
	}
}

func addShortIDToListField(body map[string]any, field string) bool {
	items, _ := body[field].([]any)
	if len(items) == 0 {
		return false
	}
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
		currentShortID := strings.TrimSpace(anyString(item["short_id"]))
		expectedShortID := shortID(id)
		if currentShortID == expectedShortID {
			continue
		}
		item["short_id"] = expectedShortID
		changed = true
	}
	return changed
}

func addShortIDToNestedListField(body map[string]any, field string, path []string) bool {
	items, _ := body[field].([]any)
	if len(items) == 0 {
		return false
	}
	changed := false
	for _, rawItem := range items {
		item, _ := rawItem.(map[string]any)
		if item == nil {
			continue
		}
		target := item
		for _, segment := range path {
			nested, _ := target[segment].(map[string]any)
			if nested == nil {
				target = nil
				break
			}
			target = nested
		}
		if target == nil {
			continue
		}
		id := strings.TrimSpace(anyString(target["id"]))
		if id == "" {
			continue
		}
		expectedShortID := shortID(id)
		if strings.TrimSpace(anyString(target["short_id"])) == expectedShortID {
			continue
		}
		target["short_id"] = expectedShortID
		changed = true
	}
	return changed
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
	id := strings.TrimSpace(anyString(item["id"]))
	if id != "" {
		expectedShortID := shortID(id)
		if currentShortID := strings.TrimSpace(anyString(item["short_id"])); currentShortID != expectedShortID {
			item["short_id"] = expectedShortID
			changed = true
		}
	}
	if alias != "" {
		if currentAlias := strings.TrimSpace(anyString(item["alias"])); currentAlias != alias {
			item["alias"] = alias
			changed = true
		}
	}
	threadID := strings.TrimSpace(anyString(item["thread_id"]))
	if threadID != "" {
		threadShortID := shortID(threadID)
		if current := strings.TrimSpace(anyString(item["thread_short_id"])); current != threadShortID {
			item["thread_short_id"] = threadShortID
			changed = true
		}
	}
	sourceEventID := strings.TrimSpace(anyString(item["source_event_id"]))
	if sourceEventID != "" {
		sourceShortID := shortID(sourceEventID)
		if current := strings.TrimSpace(anyString(item["source_event_short_id"])); current != sourceShortID {
			item["source_event_short_id"] = sourceShortID
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

func shortID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) <= shortIDLength {
		return id
	}
	return id[:shortIDLength]
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

func normalizeEventTypeFilters(explicit []string, csv string) []string {
	out := make([]string, 0, len(explicit)+2)
	seen := make(map[string]struct{}, len(explicit)+2)
	appendValue := func(raw string) {
		value := strings.TrimSpace(raw)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, value := range explicit {
		appendValue(value)
	}
	for _, value := range strings.Split(strings.TrimSpace(csv), ",") {
		appendValue(value)
	}
	return out
}

func normalizeStringFilters(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func filterEventsByType(events []any, types []string) []any {
	if len(events) == 0 {
		return []any{}
	}
	if len(types) == 0 {
		return append([]any(nil), events...)
	}
	allowed := make(map[string]struct{}, len(types))
	for _, eventType := range types {
		eventType = strings.TrimSpace(eventType)
		if eventType == "" {
			continue
		}
		allowed[eventType] = struct{}{}
	}
	if len(allowed) == 0 {
		return append([]any(nil), events...)
	}
	filtered := make([]any, 0, len(events))
	for _, raw := range events {
		event := asMap(raw)
		if event == nil {
			continue
		}
		eventType := strings.TrimSpace(anyString(event["type"]))
		if _, ok := allowed[eventType]; !ok {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered
}

func eventThreadIDFromList(events []any) string {
	for _, raw := range events {
		event := asMap(raw)
		if event == nil {
			continue
		}
		threadID := strings.TrimSpace(anyString(event["thread_id"]))
		if threadID != "" {
			return threadID
		}
	}
	return ""
}

func normalizeIDFilters(rawIDs []string) []string {
	if len(rawIDs) == 0 {
		return nil
	}
	out := make([]string, 0, len(rawIDs))
	seen := make(map[string]struct{}, len(rawIDs))
	for _, raw := range rawIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
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
			if _, ok := typeFilter[inboxItemType(item)]; !ok {
				continue
			}
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func inboxItemType(item map[string]any) string {
	if item == nil {
		return ""
	}
	return firstNonEmpty(
		strings.TrimSpace(anyString(item["type"])),
		strings.TrimSpace(anyString(item["category"])),
		strings.TrimSpace(anyString(item["kind"])),
	)
}

func filterEventsByActorID(events []any, actorID string) []any {
	actorID = strings.TrimSpace(actorID)
	if len(events) == 0 {
		return []any{}
	}
	if actorID == "" {
		return append([]any(nil), events...)
	}
	filtered := make([]any, 0, len(events))
	for _, raw := range events {
		event := asMap(raw)
		if event == nil {
			continue
		}
		if strings.TrimSpace(anyString(event["actor_id"])) != actorID {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered
}

func filterEventsByLifecycleState(events []any, includeArchived, archivedOnly, includeTrashed, trashedOnly bool) []any {
	if includeArchived && !archivedOnly && includeTrashed && !trashedOnly {
		return events
	}
	out := make([]any, 0, len(events))
	for _, raw := range events {
		event := asMap(raw)
		if event == nil {
			continue
		}
		isArchived := anyString(event["archived_at"]) != ""
		isTrashed := anyString(event["trashed_at"]) != ""

		if trashedOnly {
			if isTrashed {
				out = append(out, raw)
			}
			continue
		}
		if archivedOnly {
			if isArchived && !isTrashed {
				out = append(out, raw)
			}
			continue
		}
		if isTrashed && !includeTrashed {
			continue
		}
		if isArchived && !includeArchived {
			continue
		}
		out = append(out, raw)
	}
	return out
}

func sortEventsByCreatedAt(events []any) {
	if len(events) <= 1 {
		return
	}
	sort.SliceStable(events, func(i int, j int) bool {
		left := asMap(events[i])
		right := asMap(events[j])
		if left == nil || right == nil {
			return false
		}
		leftTS, leftOK := eventCanonicalTimestamp(left)
		rightTS, rightOK := eventCanonicalTimestamp(right)
		if leftOK && rightOK {
			if leftTS.Equal(rightTS) {
				return strings.TrimSpace(anyString(left["id"])) < strings.TrimSpace(anyString(right["id"]))
			}
			return leftTS.Before(rightTS)
		}
		if leftOK != rightOK {
			return leftOK
		}
		return false
	})
}

func eventCanonicalTimestamp(event map[string]any) (time.Time, bool) {
	for _, field := range []string{"ts", "created_at"} {
		raw := strings.TrimSpace(anyString(event[field]))
		if raw == "" {
			continue
		}
		if ts, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			return ts, true
		}
		if ts, err := time.Parse(time.RFC3339, raw); err == nil {
			return ts, true
		}
	}
	return time.Time{}, false
}

func enrichEventsForList(events []any) []any {
	if len(events) == 0 {
		return []any{}
	}
	out := make([]any, 0, len(events))
	for _, raw := range events {
		event := asMap(raw)
		if event == nil {
			continue
		}
		copy := cloneMap(event)
		id := strings.TrimSpace(anyString(copy["id"]))
		if id != "" && strings.TrimSpace(anyString(copy["short_id"])) == "" {
			copy["short_id"] = shortID(id)
		}
		if preview := eventSummaryPreview(copy); preview != "" {
			copy["summary_preview"] = preview
		}
		out = append(out, copy)
	}
	return out
}

func normalizeRecommendationReviewEvents(events []any) []any {
	if len(events) == 0 {
		return []any{}
	}
	out := make([]any, 0, len(events))
	for _, raw := range events {
		event := asMap(raw)
		if event == nil {
			continue
		}
		copy := cloneMap(event)
		id := strings.TrimSpace(anyString(copy["id"]))
		if id != "" && strings.TrimSpace(anyString(copy["short_id"])) == "" {
			copy["short_id"] = shortID(id)
		}
		if preview := eventSummaryPreview(copy); preview != "" && strings.TrimSpace(anyString(copy["summary_preview"])) == "" {
			copy["summary_preview"] = preview
		}
		if len(stringList(copy["provenance_sources"])) == 0 {
			provenance := asMap(copy["provenance"])
			if len(provenance) > 0 {
				sources := stringList(provenance["sources"])
				if len(sources) > 0 {
					copy["provenance_sources"] = sources
				}
			}
		}
		out = append(out, copy)
	}
	sortEventsByCreatedAt(out)
	return out
}

func recommendationFollowUpHints(threadID string, sections ...[]any) map[string]any {
	eventIDs := make([]string, 0, 8)
	for _, section := range sections {
		for _, raw := range section {
			event := asMap(raw)
			if event == nil {
				continue
			}
			eventID := strings.TrimSpace(anyString(event["id"]))
			if eventID == "" {
				continue
			}
			eventIDs = append(eventIDs, eventID)
		}
	}
	eventIDs = normalizeIDFilters(eventIDs)

	examples := make([]string, 0, 3)
	for _, eventID := range eventIDs {
		examples = append(examples, "oar events get --event-id "+eventID+" --json")
		if len(examples) >= 3 {
			break
		}
	}

	hints := map[string]any{
		"events_get_template":          "oar events get --event-id <event-id> --json",
		"events_get_examples":          examples,
		"recommendations_list_command": "",
		"decisions_list_command":       "",
		"context_refresh_command":      "",
	}
	if strings.TrimSpace(threadID) != "" {
		hints["recommendations_list_command"] = "oar events list --thread-id " + threadID + " --type actor_statement --full-id --json"
		hints["decisions_list_command"] = "oar events list --thread-id " + threadID + " --type decision_needed --type decision_made --full-id --json"
		hints["context_refresh_command"] = "oar threads context --thread-id " + threadID + " --include-artifact-content --full-id --json"
	}
	return hints
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

func eventSummaryPreview(event map[string]any) string {
	preview := strings.TrimSpace(anyString(event["summary"]))
	if preview != "" {
		return truncatePreview(preview)
	}
	payload := asMap(event["payload"])
	if payload != nil {
		for _, key := range []string{"recommendation", "decision", "summary", "statement", "message", "title", "content", "text"} {
			if value := compactPreviewValue(payload[key]); value != "" {
				return truncatePreview(value)
			}
		}
		if encoded, err := json.Marshal(payload); err == nil {
			if value := strings.TrimSpace(string(encoded)); value != "" && value != "{}" {
				return truncatePreview(value)
			}
		}
	}
	refs := stringList(event["refs"])
	if len(refs) > 0 {
		return truncatePreview(strings.Join(refs, ", "))
	}
	return truncatePreview(firstNonEmpty(
		strings.TrimSpace(anyString(event["ts"])),
		strings.TrimSpace(anyString(event["created_at"])),
	))
}

func compactPreviewValue(raw any) string {
	switch typed := raw.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []string:
		return strings.TrimSpace(strings.Join(typed, "; "))
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			text := strings.TrimSpace(anyString(item))
			if text == "" {
				continue
			}
			parts = append(parts, text)
			if len(parts) >= 3 {
				break
			}
		}
		return strings.TrimSpace(strings.Join(parts, "; "))
	default:
		if raw == nil {
			return ""
		}
		encoded, err := json.Marshal(raw)
		if err != nil {
			return strings.TrimSpace(fmt.Sprintf("%v", raw))
		}
		return strings.TrimSpace(string(encoded))
	}
}

func truncatePreview(raw string) string {
	const maxRunes = 120
	normalized := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if normalized == "" {
		return ""
	}
	runes := []rune(normalized)
	if len(runes) <= maxRunes {
		return normalized
	}
	return strings.TrimSpace(string(runes[:maxRunes])) + "..."
}

func addThreadContextCollaborationSummary(body map[string]any) bool {
	if body == nil {
		return false
	}
	recentEvents := enrichEventsForList(asSlice(body["recent_events"]))
	body["recent_events"] = recentEvents

	collaboration := map[string]any{
		"recommendations":   filterEventsByType(recentEvents, []string{"actor_statement"}),
		"decision_requests": filterEventsByType(recentEvents, []string{"decision_needed"}),
		"decisions":         filterEventsByType(recentEvents, []string{"decision_made"}),
		"key_artifacts":     asSlice(body["key_artifacts"]),
		"open_cards":        asSlice(body["open_cards"]),
	}
	collaboration["recommendation_count"] = len(asSlice(collaboration["recommendations"]))
	collaboration["decision_request_count"] = len(asSlice(collaboration["decision_requests"]))
	collaboration["decision_count"] = len(asSlice(collaboration["decisions"]))
	collaboration["artifact_count"] = len(asSlice(collaboration["key_artifacts"]))
	collaboration["open_card_count"] = len(asSlice(collaboration["open_cards"]))

	body["collaboration_summary"] = collaboration
	return true
}

func threadIDsFromThreadsList(result *commandResult, typeFilter string) []string {
	if result == nil {
		return nil
	}
	data := asMap(result.Data)
	body := asMap(data["body"])
	if body == nil {
		return nil
	}
	items := asSlice(body["threads"])
	if len(items) == 0 {
		return nil
	}
	typeFilter = strings.TrimSpace(typeFilter)
	out := make([]string, 0, len(items))
	for _, raw := range items {
		thread := asMap(raw)
		if thread == nil {
			continue
		}
		if typeFilter != "" && strings.TrimSpace(anyString(thread["type"])) != typeFilter {
			continue
		}
		threadID := strings.TrimSpace(anyString(thread["id"]))
		if threadID == "" {
			continue
		}
		out = append(out, threadID)
	}
	return normalizeIDFilters(out)
}

func uniqueMapsByID(items []any) []any {
	if len(items) == 0 {
		return []any{}
	}
	out := make([]any, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, raw := range items {
		item := asMap(raw)
		if item == nil {
			continue
		}
		id := strings.TrimSpace(anyString(item["id"]))
		if id == "" {
			out = append(out, item)
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, item)
	}
	return out
}

func mergeExpandedTimelineObjects(dst map[string]any, src map[string]any) {
	if len(dst) == 0 && len(src) == 0 {
		return
	}
	for id, raw := range src {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, exists := dst[id]; exists {
			continue
		}
		dst[id] = raw
	}
}

func uniqueContextArtifactItems(items []any) []any {
	if len(items) == 0 {
		return []any{}
	}
	out := make([]any, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, raw := range items {
		item := asMap(raw)
		if item == nil {
			continue
		}
		artifact := asMap(item["artifact"])
		key := firstNonEmpty(
			strings.TrimSpace(anyString(item["id"])),
			strings.TrimSpace(anyString(item["ref"])),
			strings.TrimSpace(anyString(artifact["id"])),
		)
		if key == "" {
			out = append(out, item)
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func (a *App) applyContentFileOverride(body any, contentFile string, commandName string) (any, error) {
	contentFile = strings.TrimSpace(contentFile)
	if contentFile == "" {
		return body, nil
	}
	content, err := a.readRawFile(contentFile)
	if err != nil {
		return nil, err
	}

	payload, ok := body.(map[string]any)
	if !ok {
		return nil, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body for `oar %s` must be an object when --content-file is provided", commandName))
	}
	payload["content"] = string(content)
	return payload, nil
}

func (a *App) readRawFile(path string) ([]byte, error) {
	readFile := a.ReadFile
	if readFile == nil {
		readFile = os.ReadFile
	}
	content, err := readFile(path)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "file_read_failed", fmt.Sprintf("failed to read file %s", path), err)
	}
	return content, nil
}

func (a *App) readBodyInput(fromFile string) ([]byte, error) {
	if fromFile != "" {
		readFile := a.ReadFile
		if readFile == nil {
			readFile = os.ReadFile
		}
		content, err := readFile(fromFile)
		if err != nil {
			return nil, errnorm.Wrap(errnorm.KindLocal, "file_read_failed", fmt.Sprintf("failed to read file %s", fromFile), err)
		}
		if len(strings.TrimSpace(string(content))) == 0 {
			return nil, nil
		}
		return content, nil
	}
	content, err := a.readStdinBody()
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "stdin_read_failed", "failed to read stdin", err)
	}
	return content, nil
}

// readBoardCardBodyInput loads JSON from --from-file or stdin. When skipStdin is true and
// from-file is empty, stdin is not read so flag-only invocations stay safe under non-TTY
// stdin (for example Make recipes) without blocking or failing stdin probing.
func (a *App) readBoardCardBodyInput(fromFile string, skipStdin bool) ([]byte, error) {
	fromFile = strings.TrimSpace(fromFile)
	if fromFile != "" {
		return a.readBodyInput(fromFile)
	}
	if skipStdin {
		return nil, nil
	}
	return a.readBodyInput("")
}

func decodeJSONPayload(payload []byte) (any, error) {
	var parsed any
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return nil, detailedJSONDecodeError(payload, err)
	}
	return parsed, nil
}

func detailedJSONDecodeError(payload []byte, decodeErr error) error {
	parseDetail := strings.TrimSpace(decodeErr.Error())
	if parseDetail == "" {
		parseDetail = "unknown parse error"
	}
	line, column := jsonErrorLineColumn(payload, decodeErr)
	if line > 0 && column > 0 {
		return errnorm.Usage("invalid_json", fmt.Sprintf("input body must be valid JSON (line %d, column %d): %s", line, column, parseDetail))
	}
	return errnorm.Usage("invalid_json", fmt.Sprintf("input body must be valid JSON: %s", parseDetail))
}

func jsonErrorLineColumn(payload []byte, decodeErr error) (int, int) {
	var syntaxErr *json.SyntaxError
	if errors.As(decodeErr, &syntaxErr) {
		return lineColumnFromOffset(payload, syntaxErr.Offset)
	}

	var typeErr *json.UnmarshalTypeError
	if errors.As(decodeErr, &typeErr) {
		return lineColumnFromOffset(payload, typeErr.Offset)
	}

	if errors.Is(decodeErr, io.ErrUnexpectedEOF) {
		return lineColumnFromOffset(payload, int64(len(payload)))
	}
	return 0, 0
}

func lineColumnFromOffset(payload []byte, offset int64) (int, int) {
	if offset <= 0 {
		return 1, 1
	}
	if offset > int64(len(payload)) {
		offset = int64(len(payload))
	}
	line := 1
	column := 1
	limit := offset - 1
	for idx := int64(0); idx < limit && idx < int64(len(payload)); idx++ {
		if payload[idx] == '\n' {
			line++
			column = 1
			continue
		}
		column++
	}
	return line, column
}

func validateEventsCreateInput(body any, commandName string) error {
	payload, ok := body.(map[string]any)
	if !ok {
		return errnorm.Usage("invalid_request", fmt.Sprintf("JSON body for `oar %s` must be an object", commandName))
	}

	issues := validateDraftBody("events.create", payload)
	if len(issues) > 0 {
		return errnorm.Usage("invalid_request", fmt.Sprintf("events payload failed local validation: %s", strings.Join(issues, "; ")))
	}
	return validateEventsCreateBody(body)
}

func validateDocsCreateBody(body any, commandName string) error {
	payload, ok := body.(map[string]any)
	if !ok {
		return errnorm.Usage("invalid_request", fmt.Sprintf("JSON body for `oar %s` must be an object", commandName))
	}

	issues := make([]string, 0, 8)
	rawDocument, hasDocument := payload["document"]
	if !hasDocument || rawDocument == nil {
		issues = append(issues, "document is required")
	} else {
		if _, ok := rawDocument.(map[string]any); !ok {
			issues = append(issues, "document must be an object")
		}
	}

	rawContent, hasContent := payload["content"]
	if !hasContent || rawContent == nil {
		issues = append(issues, "content is required")
	}

	rawContentType, hasContentType := payload["content_type"]
	contentType := strings.TrimSpace(anyString(rawContentType))
	if !hasContentType {
		issues = append(issues, "content_type is required")
	} else if contentType == "" {
		issues = append(issues, "content_type must be a non-empty string")
	} else if contentType != "text" && contentType != "structured" && contentType != "binary" {
		issues = append(issues, fmt.Sprintf("content_type %q must be one of: text, structured, binary", contentType))
	}

	appendDocsCommonValidationIssues(payload, &issues)
	if len(issues) > 0 {
		return errnorm.Usage("invalid_request", fmt.Sprintf("docs create payload failed local validation: %s", strings.Join(issues, "; ")))
	}
	return nil
}

func validateDocsUpdateBody(body any, commandName string) error {
	payload, ok := body.(map[string]any)
	if !ok {
		return errnorm.Usage("invalid_request", fmt.Sprintf("JSON body for `oar %s` must be an object", commandName))
	}

	if rev, ok := payload["revision"].(map[string]any); ok && len(rev) > 0 {
		return validateContractDocsRevisionCreateBody(payload, commandName)
	}

	issues := make([]string, 0, 8)
	rawContent, hasContent := payload["content"]
	if !hasContent || rawContent == nil {
		issues = append(issues, "content is required")
	}

	rawContentType, hasContentType := payload["content_type"]
	contentType := strings.TrimSpace(anyString(rawContentType))
	if !hasContentType {
		issues = append(issues, "content_type is required")
	} else if contentType == "" {
		issues = append(issues, "content_type must be a non-empty string")
	} else if contentType != "text" && contentType != "structured" && contentType != "binary" {
		issues = append(issues, fmt.Sprintf("content_type %q must be one of: text, structured, binary", contentType))
	}

	rawBaseRevision, hasBaseRevision := payload["if_base_revision"]
	baseRevision := strings.TrimSpace(anyString(rawBaseRevision))
	if !hasBaseRevision {
		issues = append(issues, "if_base_revision is required")
	} else if baseRevision == "" {
		issues = append(issues, "if_base_revision must be a non-empty string")
	}

	appendDocsCommonValidationIssues(payload, &issues)
	if len(issues) > 0 {
		return errnorm.Usage("invalid_request", fmt.Sprintf("docs.revisions.create payload failed local validation: %s", strings.Join(issues, "; ")))
	}
	return nil
}

func validateContractDocsRevisionCreateBody(payload map[string]any, commandName string) error {
	issues := make([]string, 0, 8)
	rev, ok := payload["revision"].(map[string]any)
	if !ok || len(rev) == 0 {
		issues = append(issues, "revision must be a non-empty object")
	} else {
		if strings.TrimSpace(anyString(rev["body_markdown"])) == "" {
			issues = append(issues, "revision.body_markdown is required")
		}
		provenance, ok := rev["provenance"].(map[string]any)
		if !ok || provenance == nil {
			issues = append(issues, "revision.provenance is required")
		} else {
			validateProvenance(provenance, "revision.provenance", &issues)
		}
		if _, has := rev["refs"]; !has {
			issues = append(issues, "revision.refs is required")
		} else if _, ok := asStringList(rev["refs"]); !ok {
			issues = append(issues, "revision.refs must be a list of strings")
		} else {
			validateTypedRefs(stringList(rev["refs"]), "revision.refs", &issues)
		}
	}
	appendDocsCommonValidationIssues(payload, &issues)
	if len(issues) > 0 {
		return errnorm.Usage("invalid_request", fmt.Sprintf("docs.revisions.create payload failed local validation: %s", strings.Join(issues, "; ")))
	}
	return nil
}

func normalizeDocsRevisionRequestForContract(body map[string]any) map[string]any {
	if body == nil {
		return body
	}
	if rev, ok := body["revision"].(map[string]any); ok && len(rev) > 0 {
		return body
	}
	contentType := strings.TrimSpace(anyString(body["content_type"]))
	if contentType != "text" {
		return body
	}
	baseRevision := strings.TrimSpace(anyString(body["if_base_revision"]))
	if baseRevision == "" {
		return body
	}
	content := body["content"]
	bodyMD, _ := content.(string)
	if strings.TrimSpace(bodyMD) == "" && content != nil {
		bodyMD = anyString(content)
	}
	provenance, _ := body["provenance"].(map[string]any)
	if provenance == nil {
		provenance = map[string]any{"sources": []any{}}
	}
	refs := body["refs"]
	if refs == nil {
		refs = []any{}
	}
	rev := map[string]any{
		"body_markdown": strings.TrimSpace(bodyMD),
		"provenance":    provenance,
		"refs":          refs,
	}
	if doc, ok := body["document"].(map[string]any); ok {
		if title := strings.TrimSpace(anyString(doc["title"])); title != "" {
			rev["summary"] = title
		}
	}
	out := map[string]any{
		"revision":         rev,
		"if_base_revision": baseRevision,
	}
	if id := strings.TrimSpace(anyString(body["actor_id"])); id != "" {
		out["actor_id"] = id
	}
	if raw := strings.TrimSpace(anyString(body["if_document_updated_at"])); raw != "" {
		out["if_document_updated_at"] = raw
	}
	return out
}

func appendDocsCommonValidationIssues(payload map[string]any, issues *[]string) {
	if rawDocument, hasDocument := payload["document"]; hasDocument {
		if _, ok := rawDocument.(map[string]any); !ok {
			*issues = append(*issues, "document must be an object when provided")
		}
	}

	if rawActorID, hasActorID := payload["actor_id"]; hasActorID {
		if strings.TrimSpace(anyString(rawActorID)) == "" {
			*issues = append(*issues, "actor_id must be a non-empty string when provided")
		}
	}

	if rawRefs, hasRefs := payload["refs"]; hasRefs {
		refs, ok := asStringList(rawRefs)
		if !ok {
			*issues = append(*issues, "refs must be an array of strings when provided")
		} else {
			for _, ref := range refs {
				if err := validateTypedRef(ref); err != nil {
					*issues = append(*issues, fmt.Sprintf("refs contains invalid typed ref %q", ref))
				}
			}
		}
	}
}

func ensureDocsUpdateActorIdentity(body any, cfg config.Resolved) (any, error) {
	payload, ok := body.(map[string]any)
	if !ok {
		return nil, errnorm.Usage("invalid_request", "JSON body for `oar docs revisions create` must be an object")
	}

	if actorID := strings.TrimSpace(anyString(payload["actor_id"])); actorID != "" {
		return payload, nil
	}
	if actorID := strings.TrimSpace(cfg.ActorID); actorID != "" {
		payload["actor_id"] = actorID
		return payload, nil
	}

	return nil, errnorm.Usage(
		"invalid_request",
		"No active actor identity. Run: oar auth register --username <name> or oar auth whoami to inspect current profile.",
	)
}

func typedRefPrefix(ref string) string {
	ref = strings.TrimSpace(ref)
	idx := strings.Index(ref, ":")
	if idx <= 0 {
		return ""
	}
	return strings.TrimSpace(ref[:idx])
}

func eventTypeGuidanceFor(eventType string) (eventTypeGuidance, bool) {
	eventType = strings.TrimSpace(eventType)
	for _, guidance := range knownEventTypeGuidance {
		if guidance.Type == eventType {
			return guidance, true
		}
	}
	return eventTypeGuidance{}, false
}

func knownEventTypeNames() []string {
	names := make([]string, 0, len(knownEventTypeGuidance))
	for _, guidance := range knownEventTypeGuidance {
		names = append(names, guidance.Type)
	}
	return names
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
