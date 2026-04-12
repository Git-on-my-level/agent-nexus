package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"organization-autorunner-cli/internal/config"
	"organization-autorunner-cli/internal/errnorm"
	"organization-autorunner-cli/internal/profile"
)

func (a *App) runConfig(_ context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "config", configSubcommandSpec.requiredError()
	}
	sub := configSubcommandSpec.normalize(args[0])
	switch sub {
	case "use":
		result, err := a.runConfigUse(args[1:])
		return result, "config use", err
	case "unset":
		result, err := a.runConfigUnset(args[1:])
		return result, "config unset", err
	case "show":
		result := a.runConfigShow(cfg)
		return result, "config show", nil
	default:
		return nil, "config", configSubcommandSpec.unknownError(args[0])
	}
}

func (a *App) runConfigUse(args []string) (*commandResult, error) {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		return nil, errnorm.Usage("profile_required", "usage: oar config use <profile>")
	}
	agentName := strings.TrimSpace(args[0])
	homeDir, err := a.UserHomeDir()
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "home_dir", "failed to determine home directory", err)
	}
	profilePath, err := profile.SetActiveAgent(homeDir, agentName)
	if err != nil {
		if errors.Is(err, profile.ErrProfileNotFound) {
			return nil, errnorm.Local("profile_not_found", "profile not found; run `oar auth list` to inspect available profiles")
		}
		if errors.Is(err, profile.ErrPersistDefaultMarker) {
			return nil, errnorm.Wrap(errnorm.KindLocal, "default_profile_persist_failed", "failed to persist default profile selection", err)
		}
		return nil, errnorm.Wrap(errnorm.KindLocal, "profile_read_failed", "failed to read profile", err)
	}
	return &commandResult{
		Text: "Active profile: " + agentName,
		Data: map[string]any{
			"agent":             agentName,
			"active_profile":    agentName,
			"default_file_path": profile.DefaultAgentPath(homeDir),
			"profile_path":      profilePath,
		},
	}, nil
}

func (a *App) runConfigUnset(args []string) (*commandResult, error) {
	if len(args) != 0 {
		return nil, errnorm.Usage("unexpected_args", "usage: oar config unset")
	}
	homeDir, err := a.UserHomeDir()
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "home_dir", "failed to determine home directory", err)
	}
	if err := profile.ClearDefaultAgent(homeDir); err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "default_profile_clear_failed", "failed to clear default profile selection", err)
	}
	path := profile.DefaultAgentPath(homeDir)
	return &commandResult{
		Text: "Cleared default profile marker: " + path,
		Data: map[string]any{
			"default_file_path": path,
			"cleared":           true,
		},
	}, nil
}

func (a *App) runConfigShow(cfg config.Resolved) *commandResult {
	data := redactedConfigShowData(cfg)
	var lines []string
	lines = append(lines, "Effective configuration (secrets redacted):")
	lines = append(lines, "  Base URL: "+cfg.BaseURL)
	lines = append(lines, "  Agent profile: "+cfg.Agent)
	lines = append(lines, "  Profile path: "+cfg.ProfilePath)
	lines = append(lines, "  Timeout: "+cfg.Timeout.String())
	lines = append(lines, fmt.Sprintf("  JSON output: %t", cfg.JSON))
	if strings.TrimSpace(cfg.AccessToken) != "" {
		lines = append(lines, "  Access token: (redacted)")
	} else {
		lines = append(lines, "  Access token: (empty)")
	}
	if strings.TrimSpace(cfg.RefreshToken) != "" {
		lines = append(lines, "  Refresh token: (redacted)")
	} else {
		lines = append(lines, "  Refresh token: (empty)")
	}
	keys := make([]string, 0, len(cfg.Sources))
	for k := range cfg.Sources {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("  Source %s: %s", k, cfg.Sources[k]))
	}
	return &commandResult{
		Text: strings.Join(lines, "\n"),
		Data: data,
	}
}

func redactedConfigShowData(cfg config.Resolved) map[string]any {
	sources := make(map[string]string, len(cfg.Sources))
	for k, v := range cfg.Sources {
		sources[k] = v
	}
	out := map[string]any{
		"base_url":              cfg.BaseURL,
		"agent":                 cfg.Agent,
		"timeout":               cfg.Timeout.String(),
		"json":                  cfg.JSON,
		"no_color":              cfg.NoColor,
		"verbose":               cfg.Verbose,
		"headers":               cfg.Headers,
		"profile_path":          cfg.ProfilePath,
		"sources":               sources,
		"access_token_redacted": strings.TrimSpace(cfg.AccessToken) != "",
		"refresh_token_redacted": strings.TrimSpace(cfg.RefreshToken) != "",
	}
	if strings.TrimSpace(cfg.AccessToken) != "" {
		out["access_token"] = "(redacted)"
	} else {
		out["access_token"] = ""
	}
	if strings.TrimSpace(cfg.RefreshToken) != "" {
		out["refresh_token"] = "(redacted)"
	} else {
		out["refresh_token"] = ""
	}
	if strings.TrimSpace(cfg.TokenType) != "" {
		out["token_type"] = cfg.TokenType
	}
	if strings.TrimSpace(cfg.AccessTokenExpiresAt) != "" {
		out["access_token_expires_at"] = cfg.AccessTokenExpiresAt
	}
	if strings.TrimSpace(cfg.AgentID) != "" {
		out["agent_id"] = cfg.AgentID
	}
	if strings.TrimSpace(cfg.ActorID) != "" {
		out["actor_id"] = cfg.ActorID
	}
	if strings.TrimSpace(cfg.KeyID) != "" {
		out["key_id"] = cfg.KeyID
	}
	if strings.TrimSpace(cfg.Username) != "" {
		out["username"] = cfg.Username
	}
	if strings.TrimSpace(cfg.PrivateKeyPath) != "" {
		out["private_key_path"] = cfg.PrivateKeyPath
	}
	out["revoked"] = cfg.Revoked
	if strings.TrimSpace(cfg.CoreInstanceID) != "" {
		out["core_instance_id"] = cfg.CoreInstanceID
	}
	return out
}
