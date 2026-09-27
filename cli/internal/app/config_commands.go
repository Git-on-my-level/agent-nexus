package app

import (
	"context"
	"strings"

	"agent-nexus-cli/internal/config"
)

func (a *App) runConfig(_ context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) != 1 || args[0] != "show" {
		return nil, "config", configSubcommandSpec.unknownError(firstConfigArg(args))
	}
	return &commandResult{Data: redactedConfigShowData(cfg)}, "config show", nil
}

func firstConfigArg(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return ""
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
