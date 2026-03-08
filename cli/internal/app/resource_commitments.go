package app

import (
	"context"
	"fmt"
	"strings"

	"organization-autorunner-cli/internal/config"
	"organization-autorunner-cli/internal/errnorm"
)

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
