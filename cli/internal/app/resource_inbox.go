package app

import (
	"context"
	"fmt"
	"strings"

	"organization-autorunner-cli/internal/config"
	"organization-autorunner-cli/internal/errnorm"
)

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
		body, err := a.parseAckBodyInput(args[1:])
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
