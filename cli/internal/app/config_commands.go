package app

import (
	"context"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/workspaceconfig"
)

func (a *App) runConfig(_ context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "config", configSubcommandSpec.unknownError("")
	}
	command := "config " + args[0]
	if err := preflightConfigArgs(args); err != nil {
		return nil, command, err
	}
	if args[0] == "show" {
		return &commandResult{Data: redactedConfigShowData(cfg)}, command, nil
	}
	catalog, dir, err := a.workspaceCatalog(cfg)
	if err != nil {
		return nil, command, err
	}
	if args[0] == "workspaces" {
		cwd, err := a.workspaceCWD()
		if err != nil {
			return nil, command, err
		}
		rule, _, err := catalog.MatchingRule(cwd, a.workspaceHome())
		if err != nil {
			return nil, command, err
		}
		selected, selectionErr := a.resolveWorkspace(cfg, nil)
		data := map[string]any{"workspaces": catalog.Workspaces, "default": catalog.File.Default, "directory_rules": catalog.File.Rules, "cwd": cwd, "matching_rule": rule, "base_url": selected.BaseURL, "source": selected.Sources["base_url"]}
		if selectionErr != nil {
			data["selection_error"] = selectionErr.Error()
		}
		return &commandResult{Data: data}, command, nil
	}
	var base, pattern string
	err = workspaceconfig.Update(dir, func(current *workspaceconfig.Catalog) error {
		switch args[0] {
		case "use":
			var err error
			base, err = current.Target(args[1])
			if err != nil {
				return errnorm.Usage("workspace_unknown", err.Error())
			}
			// Store the URL so later alias discovery cannot change a user's choice.
			current.File.Default = base
		case "map":
			var err error
			pattern, err = workspaceconfig.NormalizeGlob(args[1], a.workspaceHome())
			if err != nil {
				return errnorm.Usage("invalid_directory_glob", err.Error())
			}
			base, err = current.Target(args[2])
			if err != nil {
				return errnorm.Usage("workspace_unknown", err.Error())
			}
			current.File.Rules[pattern] = base
		case "unmap":
			var err error
			pattern, err = workspaceconfig.NormalizeGlob(args[1], a.workspaceHome())
			if err != nil {
				return errnorm.Usage("invalid_directory_glob", err.Error())
			}
			// Accept hand-authored ~/ rules as well as canonical paths written by map.
			for raw := range current.File.Rules {
				normalized, err := workspaceconfig.NormalizeGlob(raw, a.workspaceHome())
				if err != nil {
					return err
				}
				if normalized == pattern {
					delete(current.File.Rules, raw)
				}
			}
		default:
			return configSubcommandSpec.unknownError(args[0])
		}
		return nil
	})
	if err != nil {
		return nil, command, err
	}
	data := map[string]any{"base_url": base}
	if args[0] == "use" {
		data["default"] = base
	}
	if pattern != "" {
		data["path_glob"] = pattern
	}
	if args[0] == "unmap" {
		data = map[string]any{"path_glob": pattern, "removed": true}
	}
	return &commandResult{Data: data}, command, nil
}

func redactedConfigShowData(cfg config.Resolved) map[string]any {
	data := map[string]any{"base_url": cfg.BaseURL, "as": cfg.As, "identity_source": cfg.IdentitySource, "timeout": cfg.Timeout.String(), "json": cfg.JSON, "sources": cfg.Sources}
	if cfg.HostID != "" {
		data["host_id"] = cfg.HostID
	}
	if cfg.AgentID != "" {
		data["agent_id"] = cfg.AgentID
	}
	if strings.TrimSpace(cfg.AccessToken) != "" {
		data["access_token_redacted"] = true
	}
	return data
}
