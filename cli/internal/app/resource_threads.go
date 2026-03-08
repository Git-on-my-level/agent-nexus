package app

import (
	"context"
	"fmt"
	"strings"

	"organization-autorunner-cli/internal/config"
	"organization-autorunner-cli/internal/errnorm"
)

func (a *App) runThreadsCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "threads", errnorm.Usage("subcommand_required", "expected one of: list, get, create, update")
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
	case "update":
		id, body, err := a.parseIDAndBodyInput(args[1:], "thread-id", "thread id", "threads update")
		if err != nil {
			return nil, "threads update", err
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "threads update", "threads.patch", map[string]string{"thread_id": id}, nil, body)
		return result, "threads update", callErr
	default:
		return nil, "threads", errnorm.Usage("unknown_subcommand", fmt.Sprintf("unknown threads subcommand %q", sub))
	}
}
