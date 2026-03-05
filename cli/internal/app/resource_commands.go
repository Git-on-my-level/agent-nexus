package app

import (
	"bufio"
	"context"
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
		return nil, "threads", errnorm.Usage("subcommand_required", "expected one of: list, get, create, patch, timeline, context")
	}
	sub := strings.TrimSpace(args[0])
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
		result, callErr := a.invokeTypedJSON(ctx, cfg, "threads get", "threads.get", map[string]string{"thread_id": id}, nil, nil)
		return result, "threads get", callErr
	case "create":
		body, err := a.parseJSONBodyInput(args[1:], "threads create")
		if err != nil {
			return nil, "threads create", err
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "threads create", "threads.create", nil, nil, body)
		return result, "threads create", callErr
	case "patch", "update":
		id, body, err := a.parseIDAndBodyInput(args[1:], "thread-id", "thread id", "threads patch")
		if err != nil {
			return nil, "threads patch", err
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "threads patch", "threads.patch", map[string]string{"thread_id": id}, nil, body)
		return result, "threads patch", callErr
	case "timeline":
		id, err := parseIDArg(args[1:], "thread-id", "thread id")
		if err != nil {
			return nil, "threads timeline", err
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "threads timeline", "threads.timeline", map[string]string{"thread_id": id}, nil, nil)
		return result, "threads timeline", callErr
	case "context":
		fs := newSilentFlagSet("threads context")
		var threadIDFlag trackedString
		var maxEventsFlag trackedInt
		var includeArtifactContentFlag trackedBool
		fs.Var(&threadIDFlag, "thread-id", "Thread id")
		fs.Var(&maxEventsFlag, "max-events", "Maximum recent events to include")
		fs.Var(&includeArtifactContentFlag, "include-artifact-content", "Include key artifact content previews")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "threads context", errnorm.Usage("invalid_flags", err.Error())
		}
		positionals := fs.Args()
		threadID := strings.TrimSpace(threadIDFlag.value)
		if threadID == "" && len(positionals) > 0 {
			threadID = strings.TrimSpace(positionals[0])
			positionals = positionals[1:]
		}
		if err := validateID(threadID, "thread id"); err != nil {
			return nil, "threads context", err
		}
		if len(positionals) > 0 {
			return nil, "threads context", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar threads context`")
		}
		if maxEventsFlag.set && maxEventsFlag.value < 0 {
			return nil, "threads context", errnorm.Usage("invalid_request", "--max-events must be >= 0")
		}

		query := make([]queryParam, 0, 2)
		if maxEventsFlag.set {
			addSingleQuery(&query, "max_events", fmt.Sprintf("%d", maxEventsFlag.value))
		}
		if includeArtifactContentFlag.set && includeArtifactContentFlag.value {
			addSingleQuery(&query, "include_artifact_content", "true")
		}

		result, callErr := a.invokeTypedJSON(ctx, cfg, "threads context", "threads.context", map[string]string{"thread_id": threadID}, query, nil)
		return result, "threads context", callErr
	default:
		return nil, "threads", errnorm.Usage("unknown_subcommand", fmt.Sprintf("unknown threads subcommand %q", sub))
	}
}

func (a *App) runCommitmentsCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "commitments", errnorm.Usage("subcommand_required", "expected one of: list, get, create, update")
	}
	sub := strings.TrimSpace(args[0])
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
		query := make([]queryParam, 0, 5)
		addSingleQuery(&query, "thread_id", threadIDFlag.value)
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
		result, callErr := a.invokeTypedJSON(ctx, cfg, "commitments get", "commitments.get", map[string]string{"commitment_id": id}, nil, nil)
		return result, "commitments get", callErr
	case "create":
		body, err := a.parseJSONBodyInput(args[1:], "commitments create")
		if err != nil {
			return nil, "commitments create", err
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "commitments create", "commitments.create", nil, nil, body)
		return result, "commitments create", callErr
	case "update":
		id, body, err := a.parseIDAndBodyInput(args[1:], "commitment-id", "commitment id", "commitments update")
		if err != nil {
			return nil, "commitments update", err
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "commitments update", "commitments.patch", map[string]string{"commitment_id": id}, nil, body)
		return result, "commitments update", callErr
	default:
		return nil, "commitments", errnorm.Usage("unknown_subcommand", fmt.Sprintf("unknown commitments subcommand %q", sub))
	}
}

func (a *App) runArtifactsCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "artifacts", errnorm.Usage("subcommand_required", "expected one of: list, get, create, content")
	}
	sub := strings.TrimSpace(args[0])
	switch sub {
	case "list":
		fs := newSilentFlagSet("artifacts list")
		var kindFlag, threadIDFlag, beforeFlag, afterFlag trackedString
		fs.Var(&kindFlag, "kind", "Filter by artifact kind")
		fs.Var(&threadIDFlag, "thread-id", "Filter by thread id")
		fs.Var(&beforeFlag, "created-before", "Filter by created_at upper bound")
		fs.Var(&afterFlag, "created-after", "Filter by created_at lower bound")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "artifacts list", errnorm.Usage("invalid_flags", err.Error())
		}
		if len(fs.Args()) > 0 {
			return nil, "artifacts list", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar artifacts list`")
		}
		query := make([]queryParam, 0, 4)
		addSingleQuery(&query, "kind", kindFlag.value)
		addSingleQuery(&query, "thread_id", threadIDFlag.value)
		addSingleQuery(&query, "created_before", beforeFlag.value)
		addSingleQuery(&query, "created_after", afterFlag.value)
		result, err := a.invokeTypedJSON(ctx, cfg, "artifacts list", "artifacts.list", nil, query, nil)
		return result, "artifacts list", err
	case "get":
		id, err := parseIDArg(args[1:], "artifact-id", "artifact id")
		if err != nil {
			return nil, "artifacts get", err
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "artifacts get", "artifacts.get", map[string]string{"artifact_id": id}, nil, nil)
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
		result, callErr := a.invokeArtifactContent(ctx, cfg, "artifacts content", map[string]string{"artifact_id": id})
		return result, "artifacts content", callErr
	default:
		return nil, "artifacts", errnorm.Usage("unknown_subcommand", fmt.Sprintf("unknown artifacts subcommand %q", sub))
	}
}

func (a *App) runDocsCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "docs", errnorm.Usage("subcommand_required", "expected one of: create, get, update, history, revision")
	}
	sub := strings.TrimSpace(args[0])
	switch sub {
	case "create":
		body, err := a.parseJSONBodyInput(args[1:], "docs create")
		if err != nil {
			return nil, "docs create", err
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
	case "update":
		id, body, err := a.parseIDAndBodyInput(args[1:], "document-id", "document id", "docs update")
		if err != nil {
			return nil, "docs update", err
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "docs update", "docs.update", map[string]string{"document_id": id}, nil, body)
		return result, "docs update", callErr
	case "history":
		id, err := parseIDArg(args[1:], "document-id", "document id")
		if err != nil {
			return nil, "docs history", err
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "docs history", "docs.history", map[string]string{"document_id": id}, nil, nil)
		return result, "docs history", callErr
	case "revision":
		if len(args) < 2 {
			return nil, "docs revision", errnorm.Usage("subcommand_required", "expected `oar docs revision get`")
		}
		if strings.TrimSpace(args[1]) != "get" {
			return nil, "docs revision", errnorm.Usage("unknown_subcommand", fmt.Sprintf("unknown docs revision subcommand %q", strings.TrimSpace(args[1])))
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
	default:
		return nil, "docs", errnorm.Usage("unknown_subcommand", fmt.Sprintf("unknown docs subcommand %q", sub))
	}
}

func (a *App) runEventsCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "events", errnorm.Usage("subcommand_required", "expected one of: get, create, stream")
	}
	sub := strings.TrimSpace(args[0])
	switch sub {
	case "get":
		id, err := parseIDArg(args[1:], "event-id", "event id")
		if err != nil {
			return nil, "events get", err
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "events get", "events.get", map[string]string{"event_id": id}, nil, nil)
		return result, "events get", callErr
	case "create":
		body, err := a.parseJSONBodyInput(args[1:], "events create")
		if err != nil {
			return nil, "events create", err
		}
		if err := validateEventsCreateBody(body); err != nil {
			return nil, "events create", err
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "events create", "events.create", nil, nil, body)
		return result, "events create", callErr
	case "stream":
		result, err := a.runEventsStream(ctx, args[1:], cfg, "events stream", false)
		return result, "events stream", err
	case "tail":
		result, err := a.runEventsStream(ctx, args[1:], cfg, "events tail", true)
		return result, "events tail", err
	default:
		return nil, "events", errnorm.Usage("unknown_subcommand", fmt.Sprintf("unknown events subcommand %q", sub))
	}
}

func (a *App) runInboxCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "inbox", errnorm.Usage("subcommand_required", "expected one of: list, ack, stream")
	}
	sub := strings.TrimSpace(args[0])
	switch sub {
	case "list":
		result, err := a.invokeTypedJSON(ctx, cfg, "inbox list", "inbox.list", nil, nil, nil)
		return result, "inbox list", err
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
		result, err := a.runInboxStream(ctx, args[1:], cfg, "inbox tail", true)
		return result, "inbox tail", err
	default:
		return nil, "inbox", errnorm.Usage("unknown_subcommand", fmt.Sprintf("unknown inbox subcommand %q", sub))
	}
}

func (a *App) runPacketsCreateCommand(ctx context.Context, resource string, commandID string, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, resource, errnorm.Usage("subcommand_required", fmt.Sprintf("expected `%s create`", resource))
	}
	if strings.TrimSpace(args[0]) != "create" {
		return nil, resource, errnorm.Usage("unknown_subcommand", fmt.Sprintf("unknown %s subcommand %q", resource, strings.TrimSpace(args[0])))
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
		return nil, "derived", errnorm.Usage("subcommand_required", "expected `derived rebuild`")
	}
	if strings.TrimSpace(args[0]) != "rebuild" {
		return nil, "derived", errnorm.Usage("unknown_subcommand", fmt.Sprintf("unknown derived subcommand %q", strings.TrimSpace(args[0])))
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
			if err := a.writeStreamEvent(commandName, event, authCfg.JSON); err != nil {
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

func (a *App) writeStreamEvent(commandName string, event streaming.Event, jsonMode bool) error {
	parsedData := parseResponseBody([]byte(event.Data))
	payload := map[string]any{
		"id":   event.ID,
		"type": event.Type,
		"data": parsedData,
	}
	if jsonMode {
		envelope := output.Envelope{OK: true, Command: commandName, Data: payload}
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
	text := fmt.Sprintf("%s status: %d\nbytes: %d", commandName, resp.StatusCode, len(body))
	return &commandResult{Text: text, Data: data}, nil
}

func (a *App) invokeTypedJSON(ctx context.Context, cfg config.Resolved, commandName string, commandID string, pathParams map[string]string, query []queryParam, body any) (*commandResult, error) {
	authCfg, err := a.cfgWithResolvedAuthToken(ctx, cfg)
	if err != nil {
		return nil, err
	}
	client, err := httpclient.New(authCfg)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}

	queryValues := make(map[string][]string, len(query))
	for _, param := range query {
		if len(param.values) == 0 {
			continue
		}
		queryValues[param.name] = append([]string(nil), param.values...)
	}

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
	data := map[string]any{
		"status_code": resp.StatusCode,
		"headers":     headersSorted,
		"body":        parseResponseBody(responseBody),
	}
	text := formatAPICallText(strings.ToUpper(resolveCommandMethod(commandID)), resolveCommandPath(commandID, pathParams, queryValues), resp.StatusCode, headersSorted, responseBody)
	return &commandResult{Text: text, Data: data}, nil
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

func (a *App) parseJSONBodyInput(args []string, commandName string) (any, error) {
	fs := newSilentFlagSet(commandName)
	var fromFileFlag trackedString
	fs.Var(&fromFileFlag, "from-file", "Load JSON body from file path")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `oar %s`", commandName))
	}
	payload, err := a.readBodyInput(strings.TrimSpace(fromFileFlag.value))
	if err != nil {
		return nil, err
	}
	if len(payload) == 0 {
		return nil, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body is required for `oar %s` (provide stdin or --from-file)", commandName))
	}
	return decodeJSONPayload(payload)
}

func (a *App) parseIDAndBodyInput(args []string, idFlag string, idLabel string, commandName string) (string, any, error) {
	fs := newSilentFlagSet(commandName)
	var idArgFlag, fromFileFlag trackedString
	fs.Var(&idArgFlag, idFlag, idLabel)
	fs.Var(&fromFileFlag, "from-file", "Load JSON body from file path")
	if err := fs.Parse(args); err != nil {
		return "", nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	id := strings.TrimSpace(idArgFlag.value)
	if id == "" && len(positionals) > 0 {
		id = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if err := validateID(id, idLabel); err != nil {
		return "", nil, err
	}
	if len(positionals) > 0 {
		return "", nil, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `oar %s`", commandName))
	}
	payload, err := a.readBodyInput(strings.TrimSpace(fromFileFlag.value))
	if err != nil {
		return "", nil, err
	}
	if len(payload) == 0 {
		return "", nil, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body is required for `oar %s` (provide stdin or --from-file)", commandName))
	}
	body, err := decodeJSONPayload(payload)
	if err != nil {
		return "", nil, err
	}
	return id, body, nil
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

	if threadID == "" {
		resolvedThreadID, err := a.resolveInboxThreadID(ctx, cfg, inboxItemID)
		if err != nil {
			return nil, err
		}
		threadID = resolvedThreadID
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

func (a *App) resolveInboxThreadID(ctx context.Context, cfg config.Resolved, inboxItemID string) (string, error) {
	result, err := a.invokeTypedJSON(ctx, cfg, "inbox list", "inbox.list", nil, nil, nil)
	if err != nil {
		return "", err
	}
	data, _ := result.Data.(map[string]any)
	body, _ := data["body"].(map[string]any)
	rawItems, _ := body["items"].([]any)
	for _, rawItem := range rawItems {
		item, _ := rawItem.(map[string]any)
		if item == nil {
			continue
		}
		if strings.TrimSpace(anyString(item["id"])) != inboxItemID {
			continue
		}
		threadID := strings.TrimSpace(anyString(item["thread_id"]))
		if threadID != "" {
			return threadID, nil
		}
		break
	}
	return "", errnorm.Usage(
		"invalid_request",
		fmt.Sprintf("thread_id is required for inbox item %q (provide --thread-id or ensure it is present in `oar inbox list`)", inboxItemID),
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
		return nil, errnorm.Usage("invalid_json", "input body must be valid JSON")
	}
	return parsed, nil
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
	if anyString(event["type"]) != "review_completed" {
		return nil
	}
	rawRefs, hasRefs := event["refs"]
	if !hasRefs {
		return invalidReviewCompletedRefsError()
	}
	refs, ok := asStringList(rawRefs)
	if !ok {
		return invalidReviewCompletedRefsError()
	}
	artifactRefs := 0
	for _, ref := range refs {
		if strings.HasPrefix(strings.TrimSpace(ref), "artifact:") {
			artifactRefs++
		}
	}
	if artifactRefs < 3 {
		return invalidReviewCompletedRefsError()
	}
	return nil
}

func invalidReviewCompletedRefsError() error {
	return errnorm.Usage(
		"invalid_request",
		`event.type "review_completed" requires event.refs to include at least 3 refs prefixed with "artifact:" (for example: "artifact:work_order_1", "artifact:receipt_1", "artifact:review_1")`,
	)
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
