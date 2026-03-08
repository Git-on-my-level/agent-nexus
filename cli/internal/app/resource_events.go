package app

import (
	"context"
	"fmt"
	"strings"

	"organization-autorunner-cli/internal/config"
	"organization-autorunner-cli/internal/errnorm"
)

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
