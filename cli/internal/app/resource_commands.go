package app

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	contractsclient "organization-autorunner-contracts-go-client/client"

	"organization-autorunner-cli/internal/authcli"
	"organization-autorunner-cli/internal/config"
	"organization-autorunner-cli/internal/errnorm"
	"organization-autorunner-cli/internal/httpclient"
	"organization-autorunner-cli/internal/output"
	"organization-autorunner-cli/internal/streaming"
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
	commitmentIDLookupSpec = resourceIDLookupSpec{
		idLabel:        "commitment id",
		resource:       "commitment",
		resourcePlural: "commitments",
		listCommand:    "commitments list",
		listCommandID:  "commitments.list",
		listField:      "commitments",
		notFoundHints:  []string{"commitment not found"},
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
)

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
	Type        string
	Summary     string
	Constraints []string
}

var knownEventTypeGuidance = []eventTypeGuidance{
	{
		Type:    "message_posted",
		Summary: "Thread message or reply event.",
		Constraints: []string{
			"thread_id is required.",
			`event.refs may include "event:<parent_event_id>" for replies and "artifact:<artifact_id>" mentions.`,
		},
	},
	{
		Type:    "work_order_created",
		Summary: "Work order packet artifact created.",
		Constraints: []string{
			"thread_id is required.",
			`event.refs must include "artifact:<work_order_artifact_id>".`,
		},
	},
	{
		Type:    "work_order_claimed",
		Summary: "Work order claim marker.",
		Constraints: []string{
			"No specific reference-convention constraints are defined for this type.",
		},
	},
	{
		Type:    "receipt_added",
		Summary: "Receipt packet artifact added to the thread.",
		Constraints: []string{
			"thread_id is required.",
			`event.refs must include "artifact:<receipt_artifact_id>" and "artifact:<work_order_artifact_id>".`,
		},
	},
	{
		Type:    "review_completed",
		Summary: "Review packet artifact added for a receipt/work order.",
		Constraints: []string{
			"thread_id is required.",
			`event.refs must include "artifact:<review_artifact_id>", "artifact:<receipt_artifact_id>", and "artifact:<work_order_artifact_id>".`,
			`Local CLI validation for "oar events create" enforces at least 3 refs with prefix "artifact:".`,
		},
	},
	{
		Type:    "decision_needed",
		Summary: "Decision request event.",
		Constraints: []string{
			"thread_id is required.",
			`event.refs may include "artifact:<related_id>" and "snapshot:<commitment_id>".`,
		},
	},
	{
		Type:    "decision_made",
		Summary: "Decision outcome event.",
		Constraints: []string{
			"thread_id is required.",
			`event.refs may include "artifact:<decision_artifact_id>" and "snapshot:<commitment_id>".`,
		},
	},
	{
		Type:    "snapshot_updated",
		Summary: "Snapshot mutation event.",
		Constraints: []string{
			`thread_id is required when the updated snapshot is thread-scoped.`,
			`event.refs must include "snapshot:<snapshot_id>".`,
			`event.payload should include "changed_fields" as a list of field names.`,
		},
	},
	{
		Type:    "commitment_created",
		Summary: "Commitment snapshot created.",
		Constraints: []string{
			"thread_id is required.",
			`event.refs must include "snapshot:<commitment_id>".`,
		},
	},
	{
		Type:    "commitment_status_changed",
		Summary: "Commitment status transition.",
		Constraints: []string{
			"thread_id is required.",
			`event.refs must include "snapshot:<commitment_id>".`,
			`If payload.to_status is "done", refs must include "artifact:<receipt_artifact_id>" or "event:<decision_event_id>".`,
			`If payload.to_status is "canceled", refs must include "event:<decision_event_id>".`,
		},
	},
	{
		Type:    "exception_raised",
		Summary: "Exception signal event (for example, stale_thread).",
		Constraints: []string{
			"thread_id is required.",
			`event.payload must include "subtype".`,
		},
	},
	{
		Type:    "inbox_item_acknowledged",
		Summary: "Inbox item acknowledgement event.",
		Constraints: []string{
			"No specific reference-convention constraints are defined for this type.",
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
	case "threads":
		return a.runThreadsCommand(ctx, args, cfg)
	case "commitments":
		return a.runCommitmentsCommand(ctx, args, cfg)
	case "artifacts":
		return a.runArtifactsCommand(ctx, args, cfg)
	case "docs":
		return a.runDocsCommand(ctx, args, cfg)
	case "events":
		return a.runEventsCommand(ctx, args, cfg)
	case "inbox":
		return a.runInboxCommand(ctx, args, cfg)
	case "work-orders":
		return a.runPacketsCreateCommand(ctx, resource, "packets.work-orders.create", args, cfg)
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

func (a *App) runThreadsCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "threads", threadsSubcommandSpec.requiredError()
	}
	sub := threadsSubcommandSpec.normalize(args[0])
	switch sub {
	case "list":
		fs := newSilentFlagSet("threads list")
		var statusFlag, priorityFlag, staleFlag trackedString
		var tagsFlag, cadenceFlag trackedStrings
		fs.Var(&statusFlag, "status", "Filter by status")
		fs.Var(&priorityFlag, "priority", "Filter by priority")
		fs.Var(&staleFlag, "stale", "Filter by stale state (true/false)")
		fs.Var(&tagsFlag, "tag", "Filter by tag (repeatable)")
		fs.Var(&cadenceFlag, "cadence", "Filter by cadence (repeatable)")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "threads list", errnorm.Usage("invalid_flags", err.Error())
		}
		if len(fs.Args()) > 0 {
			return nil, "threads list", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar threads list`")
		}
		query := make([]queryParam, 0, 5)
		addSingleQuery(&query, "status", statusFlag.value)
		addSingleQuery(&query, "priority", priorityFlag.value)
		addSingleQuery(&query, "stale", staleFlag.value)
		addMultiQuery(&query, "tag", tagsFlag.values)
		addMultiQuery(&query, "cadence", cadenceFlag.values)
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
			"threads.get",
			"thread_id",
			id,
			threadIDLookupSpec,
			nil,
			nil,
		)
		return result, "threads get", callErr
	case "create":
		body, err := a.parseJSONBodyInput(args[1:], "threads create")
		if err != nil {
			return nil, "threads create", err
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "threads create", "threads.create", nil, nil, body)
		return result, "threads create", callErr
	case "patch":
		result, callErr := a.runThreadsPatchCommand(ctx, args[1:], cfg)
		return result, "threads patch", callErr
	case "propose-patch":
		result, callErr := a.runThreadsProposePatchCommand(ctx, args[1:], cfg)
		return result, "threads propose-patch", callErr
	case "apply":
		result, callErr := a.runThreadsApplyCommand(ctx, args[1:], cfg)
		return result, "threads apply", callErr
	case "timeline":
		id, err := parseIDArg(args[1:], "thread-id", "thread id")
		if err != nil {
			return nil, "threads timeline", err
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
		return result, "threads timeline", callErr
	case "context":
		selection, err := parseThreadContextSelectionArgs(args[1:], "threads context")
		if err != nil {
			return nil, "threads context", err
		}
		threadIDs, err := a.resolveThreadContextSelection(ctx, cfg, "threads context", selection, true)
		if err != nil {
			return nil, "threads context", err
		}
		query := threadContextQuery(selection)
		if len(threadIDs) == 1 {
			result, callErr := a.invokeTypedJSONWithIDResolution(
				ctx,
				cfg,
				"threads context",
				"threads.context",
				"thread_id",
				threadIDs[0],
				threadIDLookupSpec,
				query,
				nil,
			)
			if callErr != nil {
				return result, "threads context", callErr
			}
			data := asMap(result.Data)
			body := asMap(data["body"])
			if body != nil {
				addThreadContextCollaborationSummary(body)
				body["full_id"] = selection.fullID
				data["body"] = body
				result.Data = data
				result.Text = formatTypedCommandText(
					"threads.context",
					intValue(data["status_code"]),
					headerValues(data["headers"]),
					body,
					cfg.Verbose,
					cfg.Headers,
				)
			}
			return result, "threads context", nil
		}

		resolvedThreadIDs := make([]string, 0, len(threadIDs))
		threadRecords := make([]any, 0, len(threadIDs))
		contexts := make([]any, 0, len(threadIDs))
		recentEvents := make([]any, 0, len(threadIDs)*4)
		keyArtifacts := make([]any, 0, len(threadIDs)*2)
		openCommitments := make([]any, 0, len(threadIDs)*2)
		seenContextThreadIDs := make(map[string]struct{}, len(threadIDs))
		statusCode := http.StatusOK
		headers := map[string][]string{"Content-Type": {"application/json"}}
		capturedTransport := false

		for _, threadID := range threadIDs {
			contextResult, contextErr := a.invokeTypedJSONWithIDResolution(
				ctx,
				cfg,
				"threads context",
				"threads.context",
				"thread_id",
				threadID,
				threadIDLookupSpec,
				query,
				nil,
			)
			if contextErr != nil {
				return contextResult, "threads context", contextErr
			}
			data := asMap(contextResult.Data)
			if !capturedTransport {
				if code := intValue(data["status_code"]); code > 0 {
					statusCode = code
				}
				if responseHeaders := headerValues(data["headers"]); len(responseHeaders) > 0 {
					headers = responseHeaders
				}
				capturedTransport = true
			}
			body := asMap(data["body"])
			if body == nil {
				continue
			}
			addThreadContextCollaborationSummary(body)
			body["full_id"] = selection.fullID

			thread := asMap(body["thread"])
			resolvedContextThreadID := strings.TrimSpace(anyString(body["thread_id"]))
			if thread != nil {
				resolvedContextThreadID = firstNonEmpty(strings.TrimSpace(anyString(thread["id"])), resolvedContextThreadID)
			}
			if resolvedContextThreadID == "" {
				resolvedContextThreadID = strings.TrimSpace(threadID)
			}
			if resolvedContextThreadID != "" {
				if _, exists := seenContextThreadIDs[resolvedContextThreadID]; exists {
					continue
				}
				seenContextThreadIDs[resolvedContextThreadID] = struct{}{}
				resolvedThreadIDs = append(resolvedThreadIDs, resolvedContextThreadID)
			}

			if thread != nil {
				threadRecords = append(threadRecords, thread)
			}
			contexts = append(contexts, body)
			recentEvents = append(recentEvents, asSlice(body["recent_events"])...)
			keyArtifacts = append(keyArtifacts, asSlice(body["key_artifacts"])...)
			openCommitments = append(openCommitments, asSlice(body["open_commitments"])...)
		}
		resolvedThreadIDs = normalizeIDFilters(resolvedThreadIDs)
		if len(resolvedThreadIDs) == 0 {
			resolvedThreadIDs = threadIDs
		}

		aggregateBody := map[string]any{
			"thread_ids":         resolvedThreadIDs,
			"thread_count":       len(resolvedThreadIDs),
			"threads":            uniqueMapsByID(threadRecords),
			"contexts":           contexts,
			"recent_events":      uniqueMapsByID(recentEvents),
			"key_artifacts":      uniqueContextArtifactItems(keyArtifacts),
			"open_commitments":   uniqueMapsByID(openCommitments),
			"contexts_generated": true,
			"full_id":            selection.fullID,
		}
		sortEventsByCreatedAt(asSlice(aggregateBody["recent_events"]))
		addThreadContextCollaborationSummary(aggregateBody)

		data := map[string]any{
			"status_code": statusCode,
			"headers":     headers,
			"body":        aggregateBody,
		}
		result := &commandResult{Data: data}
		result.Text = formatTypedCommandText(
			"threads.context",
			statusCode,
			headers,
			aggregateBody,
			cfg.Verbose,
			cfg.Headers,
		)
		return result, "threads context", nil
	case "inspect":
		result, err := a.runThreadsInspectCommand(ctx, args[1:], cfg)
		return result, "threads inspect", err
	case "workspace":
		result, err := a.runThreadsWorkspaceCommand(ctx, args[1:], cfg)
		return result, "threads workspace", err
	case "recommendations":
		result, err := a.runThreadsRecommendationsCommand(ctx, args[1:], cfg)
		return result, "threads recommendations", err
	default:
		return nil, "threads", threadsSubcommandSpec.unknownError(args[0])
	}
}

func (a *App) runCommitmentsCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "commitments", commitmentsSubcommandSpec.requiredError()
	}
	sub := commitmentsSubcommandSpec.normalize(args[0])
	switch sub {
	case "list":
		fs := newSilentFlagSet("commitments list")
		var threadIDFlag, ownerFlag, statusFlag, dueBeforeFlag, dueAfterFlag trackedString
		fs.Var(&threadIDFlag, "thread-id", "Filter by thread id")
		fs.Var(&ownerFlag, "owner", "Filter by owner")
		fs.Var(&statusFlag, "status", "Filter by status")
		fs.Var(&dueBeforeFlag, "due-before", "Filter by due timestamp upper bound")
		fs.Var(&dueAfterFlag, "due-after", "Filter by due timestamp lower bound")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "commitments list", errnorm.Usage("invalid_flags", err.Error())
		}
		if len(fs.Args()) > 0 {
			return nil, "commitments list", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar commitments list`")
		}
		resolvedThreadID := strings.TrimSpace(threadIDFlag.value)
		if resolvedThreadID != "" {
			resolved, err := a.resolveThreadIDFilters(ctx, cfg, []string{resolvedThreadID})
			if err != nil {
				return nil, "commitments list", err
			}
			if len(resolved) > 0 {
				resolvedThreadID = resolved[0]
			}
		}
		query := make([]queryParam, 0, 5)
		addSingleQuery(&query, "thread_id", resolvedThreadID)
		addSingleQuery(&query, "owner", ownerFlag.value)
		addSingleQuery(&query, "status", statusFlag.value)
		addSingleQuery(&query, "due_before", dueBeforeFlag.value)
		addSingleQuery(&query, "due_after", dueAfterFlag.value)
		result, err := a.invokeTypedJSON(ctx, cfg, "commitments list", "commitments.list", nil, query, nil)
		return result, "commitments list", err
	case "get":
		id, err := parseIDArg(args[1:], "commitment-id", "commitment id")
		if err != nil {
			return nil, "commitments get", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(
			ctx,
			cfg,
			"commitments get",
			"commitments.get",
			"commitment_id",
			id,
			commitmentIDLookupSpec,
			nil,
			nil,
		)
		return result, "commitments get", callErr
	case "create":
		body, err := a.parseJSONBodyInput(args[1:], "commitments create")
		if err != nil {
			return nil, "commitments create", err
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "commitments create", "commitments.create", nil, nil, body)
		return result, "commitments create", callErr
	case "patch":
		result, callErr := a.runCommitmentsPatchCommand(ctx, args[1:], cfg)
		return result, "commitments patch", callErr
	case "propose-patch":
		result, callErr := a.runCommitmentsProposePatchCommand(ctx, args[1:], cfg)
		return result, "commitments propose-patch", callErr
	case "apply":
		result, callErr := a.runCommitmentsApplyCommand(ctx, args[1:], cfg)
		return result, "commitments apply", callErr
	default:
		return nil, "commitments", commitmentsSubcommandSpec.unknownError(args[0])
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
			fmt.Sprintf("%s requires exactly one thread; refine filters or pass one --thread-id. For multi-thread views, use `oar threads context`.", commandName),
		)
	}
	return threadIDs, nil
}

func mixedThreadSelectionMessage(commandName string) string {
	base := "--thread-id cannot be combined with discovery filters (--status/--priority/--stale/--tag/--cadence/--type). Choose one mode."
	discoveryExample := "oar threads context --status active"
	switch strings.TrimSpace(commandName) {
	case "threads context":
		return base + " For one thread, use `oar threads workspace --thread-id <thread-id>` for the canonical coordination view or `oar threads context --thread-id <thread-id>` for raw context. For discovery, remove `--thread-id` and use `" + discoveryExample + "`."
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

	contextResult, callErr := a.invokeTypedJSONWithIDResolution(
		ctx,
		cfg,
		"threads context",
		"threads.context",
		"thread_id",
		threadIDs[0],
		threadIDLookupSpec,
		threadContextQuery(selection),
		nil,
	)
	if callErr != nil {
		return nil, callErr
	}

	data := asMap(contextResult.Data)
	body := asMap(data["body"])
	if body == nil {
		return contextResult, nil
	}
	addThreadContextCollaborationSummary(body)

	thread := extractNestedMap(body, "thread")
	resolvedThreadID := firstNonEmpty(strings.TrimSpace(anyString(body["thread_id"])), strings.TrimSpace(anyString(thread["id"])), strings.TrimSpace(threadIDs[0]))

	inboxResult, err := a.invokeTypedJSON(ctx, cfg, "inbox list", "inbox.list", nil, nil, nil)
	if err != nil {
		return nil, err
	}
	inboxData := asMap(inboxResult.Data)
	inboxBody := extractNestedMap(inboxData, "body")
	inboxItems := filteredInboxItems(asSlice(inboxBody["items"]), []string{resolvedThreadID}, nil)

	contextSection := cloneMap(body)
	delete(contextSection, "thread")
	delete(contextSection, "collaboration_summary")
	delete(contextSection, "full_id")

	inspectBody := map[string]any{
		"thread_id":      resolvedThreadID,
		"full_id":        selection.fullID,
		"thread":         thread,
		"context":        contextSection,
		"collaboration":  asMap(body["collaboration_summary"]),
		"inbox":          map[string]any{"thread_id": resolvedThreadID, "items": inboxItems, "count": len(inboxItems), "full_id": selection.fullID},
		"context_source": "threads.context",
		"inbox_source":   "inbox.list",
	}

	data["body"] = inspectBody
	contextResult.Data = data
	contextResult.Text = formatTypedCommandText(
		"threads.inspect",
		intValue(data["status_code"]),
		headerValues(data["headers"]),
		inspectBody,
		cfg.Verbose,
		cfg.Headers,
	)
	return contextResult, nil
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

	contextResult, callErr := a.invokeTypedJSONWithIDResolution(
		ctx,
		cfg,
		"threads context",
		"threads.context",
		"thread_id",
		threadIDs[0],
		threadIDLookupSpec,
		threadContextQuery(selection.threadContextSelection),
		nil,
	)
	if callErr != nil {
		return nil, callErr
	}

	data := asMap(contextResult.Data)
	body := asMap(data["body"])
	if body == nil {
		return contextResult, nil
	}
	addThreadContextCollaborationSummary(body)

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

	data["body"] = recommendationBody
	contextResult.Data = data
	contextResult.Text = formatTypedCommandText(
		"threads.recommendations",
		intValue(data["status_code"]),
		headerValues(data["headers"]),
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
		contextResult, err := a.invokeTypedJSONWithIDResolution(
			ctx,
			cfg,
			"threads context",
			"threads.context",
			"thread_id",
			relatedThreadID,
			threadIDLookupSpec,
			threadContextQuery(selection),
			nil,
		)
		if err != nil {
			warnings = append(warnings, map[string]any{
				"thread_id": relatedThreadID,
				"message":   fmt.Sprintf("skipped related thread %s: %s", relatedThreadID, err.Error()),
			})
			continue
		}
		data := asMap(contextResult.Data)
		body := extractNestedMap(data, "body")
		if body == nil {
			continue
		}
		addThreadContextCollaborationSummary(body)
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
		var includeTombstoned bool
		fs.Var(&kindFlag, "kind", "Filter by artifact kind")
		fs.Var(&threadIDFlag, "thread-id", "Filter by thread id")
		fs.Var(&beforeFlag, "created-before", "Filter by created_at upper bound")
		fs.Var(&afterFlag, "created-after", "Filter by created_at lower bound")
		fs.BoolVar(&includeTombstoned, "include-tombstoned", false, "Include tombstoned artifacts")
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
		query := make([]queryParam, 0, 5)
		addSingleQuery(&query, "kind", kindFlag.value)
		addSingleQuery(&query, "thread_id", resolvedThreadID)
		addSingleQuery(&query, "created_before", beforeFlag.value)
		addSingleQuery(&query, "created_after", afterFlag.value)
		if includeTombstoned {
			query = append(query, queryParam{name: "include_tombstoned", values: []string{"true"}})
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
	case "tombstone":
		fs := newSilentFlagSet("artifacts tombstone")
		var artifactIDFlag trackedString
		var actorIDFlag trackedString
		var reasonFlag trackedString
		fs.Var(&artifactIDFlag, "artifact-id", "Artifact id to tombstone")
		fs.Var(&actorIDFlag, "actor-id", "Actor id")
		fs.Var(&reasonFlag, "reason", "Reason for tombstoning")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "artifacts tombstone", errnorm.Usage("invalid_flags", err.Error())
		}
		positionals := fs.Args()
		id := strings.TrimSpace(artifactIDFlag.value)
		if id == "" && len(positionals) > 0 {
			id = strings.TrimSpace(positionals[0])
			positionals = positionals[1:]
		}
		if err := validateID(id, "artifact id"); err != nil {
			return nil, "artifacts tombstone", err
		}
		if len(positionals) > 0 {
			return nil, "artifacts tombstone", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar artifacts tombstone`")
		}
		body := map[string]any{}
		actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
		if err != nil {
			return nil, "artifacts tombstone", err
		}
		if actorID != "" {
			body["actor_id"] = actorID
		} else if strings.TrimSpace(cfg.ActorID) != "" {
			body["actor_id"] = strings.TrimSpace(cfg.ActorID)
		}
		if strings.TrimSpace(reasonFlag.value) != "" {
			body["reason"] = strings.TrimSpace(reasonFlag.value)
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "artifacts tombstone", "artifacts.tombstone", map[string]string{"artifact_id": id}, nil, body)
		return result, "artifacts tombstone", callErr
	default:
		return nil, "artifacts", artifactsSubcommandSpec.unknownError(args[0])
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
		var threadIDFlag trackedString
		includeTombstoned := fs.Bool("include-tombstoned", false, "Include tombstoned documents")
		fs.Var(&threadIDFlag, "thread-id", "Filter by thread id")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "docs list", errnorm.Usage("invalid_flags", err.Error())
		}
		if len(fs.Args()) > 0 {
			return nil, "docs list", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar docs list`")
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
		query := make([]queryParam, 0, 2)
		addSingleQuery(&query, "thread_id", resolvedThreadID)
		if *includeTombstoned {
			query = append(query, queryParam{name: "include_tombstoned", values: []string{"true"}})
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
		return validationResult("docs validate-update", "docs.update", map[string]string{"document_id": id}, nil, body), "docs validate-update", nil
	case "history":
		id, err := parseIDArg(args[1:], "document-id", "document id")
		if err != nil {
			return nil, "docs history", err
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "docs history", "docs.history", map[string]string{"document_id": id}, nil, nil)
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
			"docs.revision.get",
			map[string]string{"document_id": documentID, "revision_id": revisionID},
			nil,
			nil,
		)
		return result, "docs revision get", callErr
	case "tombstone":
		fs := newSilentFlagSet("docs tombstone")
		var documentIDFlag trackedString
		var actorIDFlag trackedString
		var reasonFlag trackedString
		fs.Var(&documentIDFlag, "document-id", "Document id to tombstone")
		fs.Var(&actorIDFlag, "actor-id", "Actor id")
		fs.Var(&reasonFlag, "reason", "Reason for tombstoning")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "docs tombstone", errnorm.Usage("invalid_flags", err.Error())
		}
		positionals := fs.Args()
		id := strings.TrimSpace(documentIDFlag.value)
		if id == "" && len(positionals) > 0 {
			id = strings.TrimSpace(positionals[0])
			positionals = positionals[1:]
		}
		if err := validateID(id, "document id"); err != nil {
			return nil, "docs tombstone", err
		}
		if len(positionals) > 0 {
			return nil, "docs tombstone", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar docs tombstone`")
		}
		body := map[string]any{}
		actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
		if err != nil {
			return nil, "docs tombstone", err
		}
		if actorID != "" {
			body["actor_id"] = actorID
		} else if strings.TrimSpace(cfg.ActorID) != "" {
			body["actor_id"] = strings.TrimSpace(cfg.ActorID)
		}
		if strings.TrimSpace(reasonFlag.value) != "" {
			body["reason"] = strings.TrimSpace(reasonFlag.value)
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "docs tombstone", "docs.tombstone", map[string]string{"document_id": id}, nil, body)
		return result, "docs tombstone", callErr
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

func (a *App) runThreadsPatchCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	id, body, err := a.parseThreadPatchInput(args, "threads patch")
	if err != nil {
		return nil, err
	}
	return a.invokeTypedJSONWithIDResolution(
		ctx,
		cfg,
		"threads patch",
		"threads.patch",
		"thread_id",
		id,
		threadIDLookupSpec,
		nil,
		body,
	)
}

func (a *App) runCommitmentsPatchCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	id, body, err := a.parseCommitmentPatchInput(args, "commitments patch")
	if err != nil {
		return nil, err
	}
	return a.invokeTypedJSONWithIDResolution(
		ctx,
		cfg,
		"commitments patch",
		"commitments.patch",
		"commitment_id",
		id,
		commitmentIDLookupSpec,
		nil,
		body,
	)
}

func (a *App) runDocsUpdateCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	id, body, err := a.parseDocsUpdateInput(args, "docs update", cfg)
	if err != nil {
		return nil, err
	}
	return a.invokeTypedJSON(ctx, cfg, "docs update", "docs.update", map[string]string{"document_id": id}, nil, body)
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
	fs.Var(&threadIDFlags, "thread-id", "Thread id (repeatable)")
	fs.Var(&typeFlags, "type", "Filter by event type (repeatable)")
	fs.Var(&typesCSVFlag, "types", "Comma-separated event types")
	fs.Var(&actorIDFlag, "actor-id", "Filter by actor id")
	fs.Var(&mineFlag, "mine", "Filter to events authored by active profile actor_id")
	fs.Var(&fullIDFlag, "full-id", "Render full IDs in human output")
	fs.Var(&maxEventsFlag, "max-events", "Return at most N most-recent matching events (0 means unlimited)")
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
	expandedSnapshots := make(map[string]any)
	expandedArtifacts := make(map[string]any)
	statusCode := http.StatusOK
	headers := map[string][]string{"Content-Type": {"application/json"}}
	capturedResponse := false
	for _, threadID := range threadIDs {
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
		matching = append(matching, filtered...)
		mergeExpandedTimelineObjects(expandedSnapshots, asMap(body["snapshots"]))
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
		"snapshots":       expandedSnapshots,
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
		for _, guidance := range knownEventTypeGuidance {
			textLines = append(textLines, "- "+guidance.Type+": "+guidance.Summary)
			items = append(items, map[string]any{
				"type":    guidance.Type,
				"summary": guidance.Summary,
			})
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
		"Summary: " + guidance.Summary,
		"Constraints:",
	}
	for _, constraint := range guidance.Constraints {
		textLines = append(textLines, "- "+constraint)
	}
	data := map[string]any{
		"event_type":  guidance.Type,
		"summary":     guidance.Summary,
		"constraints": append([]string(nil), guidance.Constraints...),
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
	case "ack":
		body, err := a.parseAckBodyInput(ctx, args[1:], cfg)
		if err != nil {
			return nil, "inbox ack", err
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "inbox ack", "inbox.ack", nil, nil, body)
		return result, "inbox ack", callErr
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
	if len(threadIDs) == 0 && len(typeFilters) == 0 && !(fullIDFlag.set && fullIDFlag.value) {
		return result, nil
	}

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

func (a *App) runTailStream(ctx context.Context, cfg config.Resolved, commandName string, commandID string, query []queryParam, lastEventID string, follow bool, reconnect bool, maxEvents int) (*commandResult, error) {
	if maxEvents < 0 {
		return nil, errnorm.Usage("invalid_request", "--max-events must be >= 0")
	}

	authCfg, err := a.cfgWithResolvedAuthToken(ctx, cfg)
	if err != nil {
		return nil, err
	}
	client, err := httpclient.New(authCfg)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}

	cursor := strings.TrimSpace(lastEventID)
	received := 0

	for {
		callCtx := ctx
		headers := map[string]string{"Accept": "text/event-stream"}
		if cursor != "" {
			headers["Last-Event-ID"] = cursor
		}
		requestPath := streamPathForCommand(commandID, query, cursor)
		resp, streamErr := client.OpenStream(callCtx, httpclient.RawRequest{Method: http.MethodGet, Path: requestPath, Headers: headers})
		if streamErr != nil {
			if !follow || !reconnect {
				return nil, errnorm.Wrap(errnorm.KindNetwork, "stream_connect_failed", "failed to connect stream", streamErr)
			}
			time.Sleep(250 * time.Millisecond)
			continue
		}

		if resp.StatusCode >= http.StatusBadRequest {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			return nil, errnorm.FromHTTPFailure(resp.StatusCode, body)
		}

		reader := bufio.NewReader(resp.Body)
		dropped := false
		for {
			event, readErr := streaming.ReadEvent(reader)
			if readErr != nil {
				if readErr == io.EOF {
					dropped = true
					break
				}
				if !follow && isStreamReadTimeout(readErr) {
					dropped = false
					break
				}
				_ = resp.Body.Close()
				if !follow || !reconnect {
					return nil, errnorm.Wrap(errnorm.KindNetwork, "stream_read_failed", "failed to read stream", readErr)
				}
				dropped = true
				break
			}
			if strings.TrimSpace(event.ID) != "" {
				cursor = strings.TrimSpace(event.ID)
			}
			if err := a.writeStreamEvent(commandName, commandID, event, authCfg.JSON); err != nil {
				_ = resp.Body.Close()
				return nil, err
			}
			received++
			if maxEvents > 0 && received >= maxEvents {
				_ = resp.Body.Close()
				return &commandResult{RawWritten: true}, nil
			}
		}
		_ = resp.Body.Close()
		if !follow || !reconnect || !dropped {
			return &commandResult{RawWritten: true}, nil
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func streamPayload(commandID string, parsedData any) (string, any) {
	commandID = strings.TrimSpace(commandID)
	dataMap, ok := parsedData.(map[string]any)
	if !ok {
		return "data", parsedData
	}
	switch commandID {
	case "events.stream":
		if eventPayload, ok := dataMap["event"]; ok {
			return "event", eventPayload
		}
	case "inbox.stream":
		if itemPayload, ok := dataMap["item"]; ok {
			return "item", itemPayload
		}
	}
	if len(dataMap) == 1 {
		for key, value := range dataMap {
			key = strings.TrimSpace(key)
			if key == "" {
				return "data", parsedData
			}
			return key, value
		}
	}
	return "data", parsedData
}

func (a *App) writeStreamEvent(commandName string, commandID string, event streaming.Event, jsonMode bool) error {
	parsedData := parseResponseBody([]byte(event.Data))
	payloadKey, payloadValue := streamPayload(commandID, parsedData)
	frame := map[string]any{
		"id":          event.ID,
		"type":        event.Type,
		"payload_key": payloadKey,
		"payload":     payloadValue,
	}
	if payloadKey == "event" || payloadKey == "item" {
		frame[payloadKey] = payloadValue
	}
	if jsonMode {
		identity := resolveMachineCommandIdentity(commandName)
		envelope := output.Envelope{OK: true, Command: identity.Command, CommandID: identity.CommandID, Data: frame}
		if err := output.WriteEnvelopeJSON(a.Stdout, envelope); err != nil {
			return errnorm.Wrap(errnorm.KindLocal, "stdout_write_failed", "failed to write stream envelope", err)
		}
		return nil
	}
	line := fmt.Sprintf("[%s] %s", event.ID, event.Type)
	if strings.TrimSpace(event.Data) != "" {
		line += " " + strings.TrimSpace(event.Data)
	}
	if _, err := io.WriteString(a.Stdout, line+"\n"); err != nil {
		return errnorm.Wrap(errnorm.KindLocal, "stdout_write_failed", "failed to write stream event", err)
	}
	return nil
}

func (a *App) invokeArtifactContent(ctx context.Context, cfg config.Resolved, commandName string, pathParams map[string]string) (*commandResult, error) {
	authCfg, err := a.cfgWithResolvedAuthToken(ctx, cfg)
	if err != nil {
		return nil, err
	}
	client, err := httpclient.New(authCfg)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}
	headers := generatedHeaders(authCfg)
	delete(headers, "Accept")
	headers["Accept"] = "application/octet-stream, text/plain, application/json"
	callCtx, cancel := httpclient.WithTimeout(ctx, authCfg.Timeout)
	defer cancel()
	resp, body, invokeErr := client.Generated().Invoke(callCtx, "artifacts.content.get", pathParams, contractsclient.RequestOptions{Headers: headers})
	if resp != nil && resp.StatusCode >= http.StatusBadRequest {
		return nil, errnorm.FromHTTPFailure(resp.StatusCode, body)
	}
	if invokeErr != nil {
		return nil, errnorm.Wrap(errnorm.KindNetwork, "request_failed", "artifact content request failed", invokeErr)
	}

	if !authCfg.JSON {
		if len(body) > 0 {
			if _, err := a.Stdout.Write(body); err != nil {
				return nil, errnorm.Wrap(errnorm.KindLocal, "stdout_write_failed", "failed to write artifact content", err)
			}
		}
		return &commandResult{RawWritten: true}, nil
	}

	data := map[string]any{
		"status_code": resp.StatusCode,
		"headers":     normalizedHeaders(resp.Header),
		"body_base64": base64.StdEncoding.EncodeToString(body),
	}
	if utf8Body := strings.TrimSpace(string(body)); utf8Body != "" {
		data["body_text"] = utf8Body
	}
	if authCfg.Headers || authCfg.Verbose {
		text := formatArtifactContentText(resp.StatusCode, normalizedHeaders(resp.Header), body, authCfg.Verbose, authCfg.Headers)
		return &commandResult{Text: text, Data: data}, nil
	}
	text := fmt.Sprintf("%s status: %d\nbytes: %d", commandName, resp.StatusCode, len(body))
	return &commandResult{Text: text, Data: data}, nil
}

func (a *App) invokeTypedJSON(ctx context.Context, cfg config.Resolved, commandName string, commandID string, pathParams map[string]string, query []queryParam, body any) (*commandResult, error) {
	if body != nil {
		normalizedBody, err := a.normalizeMutationBodyIDs(ctx, cfg, commandID, body)
		if err != nil {
			return nil, err
		}
		body = normalizedBody
	}

	authCfg, err := a.cfgWithResolvedAuthToken(ctx, cfg)
	if err != nil {
		return nil, err
	}
	client, err := httpclient.New(authCfg)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}

	queryValues := queryValuesFromParams(query)

	callCtx, cancel := httpclient.WithTimeout(ctx, authCfg.Timeout)
	defer cancel()
	resp, responseBody, invokeErr := client.Generated().Invoke(callCtx, commandID, pathParams, contractsclient.RequestOptions{
		Query:   queryValues,
		Headers: generatedHeaders(authCfg),
		Body:    body,
	})
	if resp != nil && resp.StatusCode >= http.StatusBadRequest {
		return nil, errnorm.FromHTTPFailure(resp.StatusCode, responseBody)
	}
	if invokeErr != nil {
		return nil, errnorm.Wrap(errnorm.KindNetwork, "request_failed", fmt.Sprintf("%s request failed", commandName), invokeErr)
	}

	headersSorted := normalizedHeaders(resp.Header)
	parsedBody := parseResponseBody(responseBody)
	parsedBody, enriched := enrichListBodyWithShortIDs(commandID, parsedBody)
	if enriched {
		if encoded, marshalErr := json.Marshal(parsedBody); marshalErr == nil {
			responseBody = encoded
		}
	}
	data := map[string]any{
		"status_code": resp.StatusCode,
		"headers":     headersSorted,
		"body":        parsedBody,
	}
	text := formatTypedCommandText(commandID, resp.StatusCode, headersSorted, parsedBody, authCfg.Verbose, authCfg.Headers)
	return &commandResult{Text: text, Data: data}, nil
}

func validationResult(commandName string, commandID string, pathParams map[string]string, query []queryParam, body any) *commandResult {
	queryValues := queryValuesFromParams(query)
	method := strings.ToUpper(resolveCommandMethod(commandID))
	path := resolveCommandPath(commandID, pathParams, queryValues)

	data := map[string]any{
		"validated":  true,
		"command_id": commandID,
		"method":     method,
		"path":       path,
	}
	if len(pathParams) > 0 {
		data["path_params"] = pathParams
	}
	if len(queryValues) > 0 {
		data["query"] = queryValues
	}
	if body != nil {
		data["body"] = body
	}
	text := fmt.Sprintf("Validation passed for `oar %s` (%s %s).", commandName, method, path)
	return &commandResult{Text: text, Data: data}
}

func dryRunResult(commandName string, commandID string, pathParams map[string]string, query []queryParam, body any) *commandResult {
	result := validationResult(commandName, commandID, pathParams, query, body)
	if result == nil {
		return nil
	}
	data, _ := result.Data.(map[string]any)
	if data != nil {
		data["dry_run"] = true
	}
	result.Text = result.Text + " No request was sent."
	return result
}

func (a *App) invokeTypedJSONWithIDResolution(
	ctx context.Context,
	cfg config.Resolved,
	commandName string,
	commandID string,
	pathParamName string,
	rawID string,
	lookupSpec resourceIDLookupSpec,
	query []queryParam,
	body any,
) (*commandResult, error) {
	pathParams := map[string]string{pathParamName: rawID}
	result, err := a.invokeTypedJSON(ctx, cfg, commandName, commandID, pathParams, query, body)
	if err == nil {
		return result, nil
	}
	if !isResolvableResourceNotFoundError(err, lookupSpec) {
		return nil, err
	}

	resolvedID, resolveErr := a.resolveResourceIDFromList(ctx, cfg, rawID, lookupSpec)
	if resolveErr != nil {
		return nil, resolveErr
	}
	if resolvedID == rawID {
		return nil, missingResourceIDError(rawID, lookupSpec)
	}
	return a.invokeTypedJSON(ctx, cfg, commandName, commandID, map[string]string{pathParamName: resolvedID}, query, body)
}

func (a *App) invokeArtifactContentWithIDResolution(
	ctx context.Context,
	cfg config.Resolved,
	commandName string,
	pathParamName string,
	rawID string,
	lookupSpec resourceIDLookupSpec,
) (*commandResult, error) {
	result, err := a.invokeArtifactContent(ctx, cfg, commandName, map[string]string{pathParamName: rawID})
	if err == nil {
		return result, nil
	}
	if !isResolvableResourceNotFoundError(err, lookupSpec) {
		return nil, err
	}
	resolvedID, resolveErr := a.resolveResourceIDFromList(ctx, cfg, rawID, lookupSpec)
	if resolveErr != nil {
		return nil, resolveErr
	}
	if resolvedID == rawID {
		return nil, missingResourceIDError(rawID, lookupSpec)
	}
	return a.invokeArtifactContent(ctx, cfg, commandName, map[string]string{pathParamName: resolvedID})
}

func (a *App) cfgWithResolvedAuthToken(ctx context.Context, cfg config.Resolved) (config.Resolved, error) {
	svc := authcli.New(cfg)
	prof, err := svc.EnsureAccessToken(ctx)
	if err != nil {
		normalized := errnorm.Normalize(err)
		if normalized != nil && normalized.Code == "profile_not_found" {
			return cfg, nil
		}
		return config.Resolved{}, err
	}
	cfg.AccessToken = strings.TrimSpace(prof.AccessToken)
	if cfg.AccessToken == "" {
		return cfg, nil
	}
	return cfg, nil
}

func generatedHeaders(cfg config.Resolved) map[string]string {
	headers := map[string]string{
		"Accept":            "application/json",
		"X-OAR-CLI-Version": httpclient.CLIVersion,
	}
	if strings.TrimSpace(cfg.Agent) != "" {
		headers["X-OAR-Agent"] = strings.TrimSpace(cfg.Agent)
	}
	if strings.TrimSpace(cfg.AccessToken) != "" {
		headers["Authorization"] = "Bearer " + strings.TrimSpace(cfg.AccessToken)
	}
	return headers
}

func streamPathForCommand(commandID string, query []queryParam, cursor string) string {
	spec, ok := commandSpecByID(commandID)
	if !ok {
		return "/"
	}
	u := url.URL{Path: spec.Path}
	q := url.Values{}
	for _, param := range query {
		for _, value := range param.values {
			q.Add(param.name, value)
		}
	}
	if strings.TrimSpace(cursor) != "" {
		q.Set("last_event_id", strings.TrimSpace(cursor))
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func resolveCommandMethod(commandID string) string {
	spec, ok := commandSpecByID(commandID)
	if !ok {
		return http.MethodGet
	}
	return spec.Method
}

func resolveCommandPath(commandID string, pathParams map[string]string, query map[string][]string) string {
	spec, ok := commandSpecByID(commandID)
	if !ok {
		return "/"
	}
	resolved := spec.Path
	for _, param := range spec.PathParams {
		value := pathParams[param]
		resolved = strings.ReplaceAll(resolved, "{"+param+"}", url.PathEscape(value))
	}
	u := url.URL{Path: resolved}
	if len(query) > 0 {
		q := url.Values{}
		keys := make([]string, 0, len(query))
		for key := range query {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			for _, value := range query[key] {
				q.Add(key, value)
			}
		}
		u.RawQuery = q.Encode()
	}
	return u.String()
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
	var fromFileFlag, threadIDFlag, inboxItemIDFlag, actorIDFlag trackedString
	fs.Var(&fromFileFlag, "from-file", "Load JSON body from file path")
	fs.Var(&threadIDFlag, "thread-id", "Thread id")
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

	threadID := strings.TrimSpace(threadIDFlag.value)
	inboxItemID := strings.TrimSpace(inboxItemIDFlag.value)
	if inboxItemID == "" && len(positionals) > 0 {
		if threadID == "" && len(positionals) > 1 {
			threadID = strings.TrimSpace(positionals[0])
			inboxItemID = strings.TrimSpace(positionals[1])
			positionals = positionals[2:]
		} else {
			inboxItemID = strings.TrimSpace(positionals[0])
			positionals = positionals[1:]
		}
	}
	if len(positionals) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `oar inbox ack`")
	}
	if err := validateID(inboxItemID, "inbox item id"); err != nil {
		return nil, err
	}

	shouldResolveInboxItem := threadID == "" || looksLikeInboxAlias(inboxItemID)
	if shouldResolveInboxItem {
		resolvedInboxItemID, resolvedThreadID, err := a.resolveInboxItemIDAndThread(ctx, cfg, inboxItemID)
		if err != nil {
			return nil, err
		}
		inboxItemID = resolvedInboxItemID
		if threadID == "" {
			threadID = resolvedThreadID
		}
	}
	if err := validateID(threadID, "thread id"); err != nil {
		return nil, err
	}

	actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"thread_id":     threadID,
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
	threadID := strings.TrimSpace(match.ThreadID)
	if threadID == "" {
		return "", "", errnorm.Usage(
			"invalid_request",
			fmt.Sprintf("thread_id is required for inbox item %q (provide --thread-id or ensure it is present in `oar inbox list`)", match.ID),
		)
	}
	return match.ID, threadID, nil
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
		id := strings.TrimSpace(anyString(item["id"]))
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
	case "commitments.list":
		return body, addShortIDToListField(typedBody, "commitments")
	case "artifacts.list":
		return body, addShortIDToListField(typedBody, "artifacts")
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
	commitmentID := strings.TrimSpace(anyString(item["commitment_id"]))
	if commitmentID != "" {
		commitmentShortID := shortID(commitmentID)
		if current := strings.TrimSpace(anyString(item["commitment_short_id"])); current != commitmentShortID {
			item["commitment_short_id"] = commitmentShortID
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
		"open_commitments":  asSlice(body["open_commitments"]),
	}
	collaboration["recommendation_count"] = len(asSlice(collaboration["recommendations"]))
	collaboration["decision_request_count"] = len(asSlice(collaboration["decision_requests"]))
	collaboration["decision_count"] = len(asSlice(collaboration["decisions"]))
	collaboration["artifact_count"] = len(asSlice(collaboration["key_artifacts"]))
	collaboration["open_commitment_count"] = len(asSlice(collaboration["open_commitments"]))

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
		return errnorm.Usage("invalid_request", fmt.Sprintf("docs update payload failed local validation: %s", strings.Join(issues, "; ")))
	}
	return nil
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
		return nil, errnorm.Usage("invalid_request", "JSON body for `oar docs update` must be an object")
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

func validateEventsCreateBody(body any) error {
	payload, ok := body.(map[string]any)
	if !ok {
		return nil
	}
	rawEvent, hasEvent := payload["event"]
	if !hasEvent {
		return nil
	}
	event, ok := rawEvent.(map[string]any)
	if !ok {
		return nil
	}
	eventType := strings.TrimSpace(anyString(event["type"]))
	if eventType == "" {
		return nil
	}

	rule, known := localEventReferenceRules[eventType]
	if !known {
		// Keep local validation open for unknown event types, matching core behavior.
		return nil
	}

	if rule.threadRequired && strings.TrimSpace(anyString(event["thread_id"])) == "" {
		return errnorm.Usage("invalid_request", fmt.Sprintf("event.thread_id is required for event.type=%q", eventType))
	}

	rawRefs, _ := event["refs"]
	refs, _ := asStringList(rawRefs)
	if err := validateLocalEventRequiredRefPrefixes(eventType, refs, rule.requiredRefPrefixes); err != nil {
		return err
	}

	payloadMap := asMap(event["payload"])
	if payloadMap == nil {
		payloadMap = map[string]any{}
	}
	if err := validateLocalEventRequiredPayloadKeys(eventType, payloadMap, rule.requiredPayloadKeys); err != nil {
		return err
	}
	if eventType == "commitment_status_changed" {
		if err := validateLocalCommitmentStatusChangedRefs(payloadMap, refs); err != nil {
			return err
		}
	}

	return nil
}

type localEventReferenceRule struct {
	threadRequired      bool
	requiredRefPrefixes map[string]int
	requiredPayloadKeys []string
}

var localEventReferenceRules = map[string]localEventReferenceRule{
	"work_order_created": {
		threadRequired:      true,
		requiredRefPrefixes: map[string]int{"artifact": 1},
	},
	"receipt_added": {
		threadRequired:      true,
		requiredRefPrefixes: map[string]int{"artifact": 2},
	},
	"review_completed": {
		threadRequired:      true,
		requiredRefPrefixes: map[string]int{"artifact": 3},
	},
	"commitment_created": {
		threadRequired:      true,
		requiredRefPrefixes: map[string]int{"snapshot": 1},
	},
	"commitment_status_changed": {
		threadRequired:      true,
		requiredRefPrefixes: map[string]int{"snapshot": 1},
	},
	"decision_needed": {
		threadRequired: true,
	},
	"decision_made": {
		threadRequired: true,
	},
	"snapshot_updated": {
		requiredRefPrefixes: map[string]int{"snapshot": 1},
	},
	"exception_raised": {
		threadRequired:      true,
		requiredPayloadKeys: []string{"subtype"},
	},
	"message_posted": {
		threadRequired: true,
	},
	"inbox_item_acknowledged": {
		threadRequired:      true,
		requiredRefPrefixes: map[string]int{"inbox": 1},
	},
}

func validateLocalEventRequiredRefPrefixes(eventType string, refs []string, requiredByPrefix map[string]int) error {
	if len(requiredByPrefix) == 0 {
		return nil
	}

	actualByPrefix := make(map[string]int)
	for _, ref := range refs {
		prefix := typedRefPrefix(ref)
		if prefix == "" {
			continue
		}
		actualByPrefix[prefix]++
	}

	for prefix, requiredCount := range requiredByPrefix {
		if actualByPrefix[prefix] >= requiredCount {
			continue
		}
		if eventType == "review_completed" && prefix == "artifact" && requiredCount == 3 {
			return invalidReviewCompletedRefsError()
		}
		if requiredCount == 1 {
			return errnorm.Usage("invalid_request", fmt.Sprintf("event.refs must include a %q typed ref for event.type=%q", prefix+":<id>", eventType))
		}
		return errnorm.Usage("invalid_request", fmt.Sprintf("event.refs must include at least %d refs with prefix %q for event.type=%q", requiredCount, prefix, eventType))
	}

	return nil
}

func validateLocalEventRequiredPayloadKeys(eventType string, payload map[string]any, keys []string) error {
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		value, exists := payload[key]
		if !exists || value == nil {
			return errnorm.Usage("invalid_request", fmt.Sprintf("event.payload.%s is required for event.type=%q", key, eventType))
		}
	}
	return nil
}

func validateLocalCommitmentStatusChangedRefs(payload map[string]any, refs []string) error {
	status := localCommitmentTargetStatus(payload)
	if status == "" {
		return nil
	}

	hasArtifactRef := false
	hasEventRef := false
	for _, ref := range refs {
		prefix := typedRefPrefix(ref)
		if prefix == "artifact" {
			hasArtifactRef = true
		}
		if prefix == "event" {
			hasEventRef = true
		}
	}

	switch status {
	case "done":
		if hasArtifactRef || hasEventRef {
			return nil
		}
		return errnorm.Usage("invalid_request", "event.refs must include artifact:<receipt_id> or event:<decision_event_id> when event.type=\"commitment_status_changed\" and payload.to_status=\"done\"")
	case "canceled":
		if hasEventRef {
			return nil
		}
		return errnorm.Usage("invalid_request", "event.refs must include event:<decision_event_id> when event.type=\"commitment_status_changed\" and payload.to_status=\"canceled\"")
	default:
		return nil
	}
}

func localCommitmentTargetStatus(payload map[string]any) string {
	if toStatus, ok := payload["to_status"].(string); ok {
		return strings.TrimSpace(toStatus)
	}
	if status, ok := payload["status"].(string); ok {
		return strings.TrimSpace(status)
	}
	return ""
}

func typedRefPrefix(ref string) string {
	ref = strings.TrimSpace(ref)
	idx := strings.Index(ref, ":")
	if idx <= 0 {
		return ""
	}
	return strings.TrimSpace(ref[:idx])
}

func invalidReviewCompletedRefsError() error {
	return errnorm.Usage(
		"invalid_request",
		`event.type "review_completed" requires event.refs to include at least 3 refs prefixed with "artifact:" (for example: "artifact:work_order_1", "artifact:receipt_1", "artifact:review_1"). See `+"`oar events explain review_completed`"+` for full constraints.`,
	)
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

func normalizedHeaders(input http.Header) map[string][]string {
	out := make(map[string][]string, len(input))
	for key, values := range input {
		if strings.EqualFold(key, "Date") || strings.EqualFold(key, "Content-Length") || strings.EqualFold(key, "Connection") {
			continue
		}
		copied := append([]string(nil), values...)
		out[key] = copied
	}
	return out
}

func commandSpecByID(commandID string) (contractsclient.CommandSpec, bool) {
	commandID = strings.TrimSpace(commandID)
	for _, spec := range contractsclient.CommandRegistry {
		if strings.TrimSpace(spec.CommandID) == commandID {
			return spec, true
		}
	}
	return contractsclient.CommandSpec{}, false
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

func isStreamReadTimeout(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "context deadline exceeded") || strings.Contains(text, "client.timeout")
}
