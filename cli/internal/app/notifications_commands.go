package app

import (
	"context"
	"strings"

	"organization-autorunner-cli/internal/config"
	"organization-autorunner-cli/internal/errnorm"
)

func (a *App) runNotificationsCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 || isHelpToken(args[0]) {
		return nil, "notifications", notificationsSubcommandSpec.requiredError()
	}
	sub := notificationsSubcommandSpec.normalize(args[0])
	switch sub {
	case "list":
		result, err := a.runNotificationsList(ctx, args[1:], cfg)
		return result, "notifications list", err
	case "read":
		result, err := a.runNotificationsRead(ctx, args[1:], cfg)
		return result, "notifications read", err
	case "dismiss":
		result, err := a.runNotificationsDismiss(ctx, args[1:], cfg)
		return result, "notifications dismiss", err
	default:
		return nil, "notifications", notificationsSubcommandSpec.unknownError(args[0])
	}
}

func (a *App) runNotificationsList(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("notifications list")
	var statusFlags trackedStrings
	var orderFlag trackedString
	fs.Var(&statusFlags, "status", "Filter by notification status (repeatable)")
	fs.Var(&orderFlag, "order", "Sort order: asc or desc")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `oar notifications list`")
	}

	order := strings.ToLower(strings.TrimSpace(orderFlag.value))
	if order == "" {
		order = "desc"
	}
	if order != "asc" && order != "desc" {
		return nil, errnorm.Usage("invalid_request", "--order must be one of: asc, desc")
	}

	query := make([]queryParam, 0, len(statusFlags.values)+1)
	for _, raw := range statusFlags.values {
		status := strings.ToLower(strings.TrimSpace(raw))
		if status != "unread" && status != "read" && status != "dismissed" {
			return nil, errnorm.Usage("invalid_request", "--status must be one of: unread, read, dismissed")
		}
		query = append(query, queryParam{name: "status", values: []string{status}})
	}
	query = append(query, queryParam{name: "order", values: []string{order}})
	return a.invokeTypedJSON(ctx, cfg, "notifications list", "notifications.list", nil, query, nil)
}

func (a *App) runNotificationsRead(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	return a.runNotificationMutation(ctx, args, cfg, "notifications read", "notifications.read")
}

func (a *App) runNotificationsDismiss(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	return a.runNotificationMutation(ctx, args, cfg, "notifications dismiss", "notifications.dismiss")
}

func (a *App) runNotificationMutation(ctx context.Context, args []string, cfg config.Resolved, commandName string, commandID string) (*commandResult, error) {
	fs := newSilentFlagSet(commandName)
	var wakeupIDFlag trackedString
	fs.Var(&wakeupIDFlag, "wakeup-id", "Wakeup artifact id / notification id")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `oar "+commandName+"`")
	}
	wakeupID := strings.TrimSpace(wakeupIDFlag.value)
	if wakeupID == "" {
		return nil, errnorm.Usage("invalid_request", "--wakeup-id is required")
	}
	body := map[string]any{
		"wakeup_id": wakeupID,
	}
	return a.invokeTypedJSON(ctx, cfg, commandName, commandID, nil, nil, body)
}
