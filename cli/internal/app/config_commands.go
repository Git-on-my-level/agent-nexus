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
	if args[0] == "forge" && len(args) > 1 && configForgeSubcommandSpec.normalize(args[1]) == "list" {
		command = "config forge list"
		return a.runConfigForgeList(args[2:], catalog)
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
	var forgeData map[string]any
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
		case "forge":
			forgeData, err = runConfigForgeMutate(args[1:], current)
			if err != nil {
				return err
			}
		default:
			return configSubcommandSpec.unknownError(args[0])
		}
		return nil
	})
	if err != nil {
		return nil, command, err
	}
	if args[0] == "forge" {
		return &commandResult{Data: forgeData}, command, nil
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

// runConfigForgeList is a read: it reports the forge hosts registered for one
// workspace or, with no workspace argument, every workspace that has entries.
func (a *App) runConfigForgeList(args []string, catalog workspaceconfig.Catalog) (*commandResult, string, error) {
	command := "config forge list"
	data := map[string]any{}
	if len(args) == 0 {
		forges := map[string][]workspaceconfig.Forge{}
		for key, entries := range catalog.File.Forges {
			forges[key] = entries
		}
		data["forges"] = forges
		return &commandResult{Data: data}, command, nil
	}
	base, err := catalog.Target(args[0])
	if err != nil {
		return nil, command, errnorm.Usage("workspace_unknown", err.Error())
	}
	data["workspace"] = base
	data["forges"] = catalog.File.ForgesFor(base)
	return &commandResult{Data: data}, command, nil
}

// runConfigForgeMutate applies one forge registry write inside the workspace
// config update transaction.
func runConfigForgeMutate(args []string, current *workspaceconfig.Catalog) (map[string]any, error) {
	if len(args) == 0 {
		return nil, configForgeSubcommandSpec.requiredError()
	}
	switch configForgeSubcommandSpec.normalize(args[0]) {
	case "add":
		if len(args) != 4 {
			return nil, errnorm.Usage("invalid_args", "use anx help config forge add for command arguments")
		}
		base, err := current.Target(args[1])
		if err != nil {
			return nil, errnorm.Usage("workspace_unknown", err.Error())
		}
		if err = current.File.SetForge(args[1], args[2], args[3]); err != nil {
			return nil, errnorm.Usage("invalid_request", err.Error())
		}
		return map[string]any{"workspace": base, "host": strings.ToLower(strings.TrimSpace(args[2])), "kind": strings.ToLower(strings.TrimSpace(args[3])), "registered": true}, nil
	case "remove":
		if len(args) != 3 {
			return nil, errnorm.Usage("invalid_args", "use anx help config forge remove for command arguments")
		}
		base, err := current.Target(args[1])
		if err != nil {
			return nil, errnorm.Usage("workspace_unknown", err.Error())
		}
		removed, err := current.File.RemoveForge(args[1], args[2])
		if err != nil {
			return nil, errnorm.Usage("workspace_unknown", err.Error())
		}
		if !removed {
			return nil, errnorm.Usage("not_found", "forge host "+args[2]+" is not registered for "+base)
		}
		return map[string]any{"workspace": base, "host": strings.ToLower(strings.TrimSpace(args[2])), "removed": true}, nil
	default:
		return nil, configForgeSubcommandSpec.unknownError(args[0])
	}
}
