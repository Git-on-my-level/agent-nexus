package app

import (
	"context"
	"fmt"
	"strings"

	"organization-autorunner-cli/internal/config"
	"organization-autorunner-cli/internal/errnorm"
)

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
	body, err := a.parseJSONBodyInput(args[1:], "derived rebuild")
	if err != nil {
		return nil, "derived rebuild", err
	}
	result, callErr := a.invokeTypedJSON(ctx, cfg, "derived rebuild", "derived.rebuild", nil, nil, body)
	return result, "derived rebuild", callErr
}
