// `anx move` command parsing, dispatch and destination workspace config, plus
// the small map/value helpers shared across the move engine files.

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

type parsedMoveCommand struct {
	kind, ref, target string
	dryRun            bool
	connectionMaps    map[string]string
}

func parseMoveCommand(args []string) (parsedMoveCommand, error) {
	out := parsedMoveCommand{}
	if len(args) == 0 {
		return out, errnorm.Usage("subcommand_required", "usage: anx move card|topic <ref> --to <workspace-alias> [--dry-run]")
	}
	out.kind = strings.TrimSpace(args[0])
	if out.kind != "card" && out.kind != "topic" {
		return out, errnorm.Usage("unknown_subcommand", fmt.Sprintf("unknown move subcommand %q; expected card or topic", out.kind))
	}
	fs := newSilentFlagSet("move " + out.kind)
	var target trackedString
	var connectionMaps trackedStrings
	fs.Var(&target, "to", "Destination workspace alias")
	fs.Var(&connectionMaps, "connection-map", "Map a source connection id to a destination-local connection id (source=destination)")
	fs.BoolVar(&out.dryRun, "dry-run", false, "Show the planned move without writing")
	tail := args[1:]
	var leading []string
	for len(tail) > 0 && !strings.HasPrefix(tail[0], "-") {
		leading = append(leading, tail[0])
		tail = tail[1:]
	}
	if err := fs.Parse(tail); err != nil {
		return out, errnorm.Usage("invalid_flags", err.Error())
	}
	positions := append(leading, fs.Args()...)
	if len(positions) != 1 || strings.TrimSpace(positions[0]) == "" {
		return out, errnorm.Usage("invalid_request", "one source resource ref is required")
	}
	out.ref = strings.TrimSpace(positions[0])
	if out.ref == "." || out.ref == ".." || strings.ContainsAny(out.ref, "/\\?#%") {
		return out, errnorm.Usage("invalid_request", "resource id must be a ref, handle or id, not a URL or path")
	}
	if !target.set || strings.TrimSpace(target.value) == "" {
		return out, errnorm.Usage("invalid_request", "--to <workspace-alias> is required")
	}
	out.target = strings.TrimSpace(target.value)
	out.connectionMaps = make(map[string]string, len(connectionMaps.values))
	for _, raw := range connectionMaps.values {
		sourceID, destinationID, ok := strings.Cut(raw, "=")
		sourceID, destinationID = strings.TrimSpace(sourceID), strings.TrimSpace(destinationID)
		if !ok || sourceID == "" || destinationID == "" {
			return out, errnorm.Usage("invalid_request", "--connection-map must be <source-connection-id>=<destination-connection-id>")
		}
		if previous, exists := out.connectionMaps[sourceID]; exists && previous != destinationID {
			return out, errnorm.Usage("invalid_request", "--connection-map contains conflicting mappings for source connection "+sourceID)
		}
		out.connectionMaps[sourceID] = destinationID
	}
	return out, nil
}

func (a *App) runMoveCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	parsed, err := parseMoveCommand(args)
	if err != nil {
		name := "move"
		if parsed.kind != "" {
			name += " " + parsed.kind
		}
		return nil, name, err
	}
	name := "move " + parsed.kind
	targetCfg, err := a.moveDestinationConfig(ctx, cfg, parsed.target)
	if err != nil {
		return nil, name, err
	}
	if strings.TrimRight(cfg.BaseURL, "/") == strings.TrimRight(targetCfg.BaseURL, "/") {
		return nil, name, errnorm.Usage("invalid_request", "source and destination workspaces are the same")
	}
	if parsed.kind == "card" {
		result, err := a.moveCard(ctx, cfg, targetCfg, parsed.ref, parsed.dryRun, parsed.connectionMaps)
		return result, name, err
	}
	result, err := a.moveTopic(ctx, cfg, targetCfg, parsed.ref, parsed.dryRun, parsed.connectionMaps)
	return result, name, err
}

func (a *App) moveDestinationConfig(ctx context.Context, source config.Resolved, alias string) (config.Resolved, error) {
	catalog, _, err := a.workspaceCatalog(source)
	if err != nil {
		return config.Resolved{}, errnorm.Wrap(errnorm.KindLocal, "workspace_config_invalid", "cannot read user-global workspace aliases: "+err.Error(), err)
	}
	base, ok := catalog.File.Aliases[alias]
	if !ok {
		return config.Resolved{}, errnorm.Usage("workspace_unknown", "unknown workspace alias "+alias+"; run anx config workspaces")
	}
	name, identitySource, err := a.identityName(source)
	if err != nil {
		return config.Resolved{}, err
	}
	dest := source
	dest.BaseURL = strings.TrimRight(base, "/")
	dest.Sources = copyStringMap(source.Sources)
	dest.Sources["base_url"] = "flag:--workspace"
	dest.As = name
	dest.IdentitySource = identitySource
	// Workspace host tokens are scoped to one local enrollment. Never send the
	// source bearer token, actor id, or host identity to the destination.
	dest.AccessToken = ""
	dest.AccessTokenExpiresAt = ""
	dest.HostID = ""
	dest.HostKeyID = ""
	dest.HostKeyPath = ""
	dest.AgentID = ""
	dest.ActorID = ""
	dest.Username = ""
	return a.resolveHostAgent(ctx, dest)
}

func copyStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in)+1)
	for key, value := range in {
		out[key] = value
	}
	return out
}

func moveFieldString(object map[string]any, key string) string {
	return strings.TrimSpace(anyString(object[key]))
}

func moveStringList(raw any) []string {
	switch values := raw.(type) {
	case []string:
		return append([]string(nil), values...)
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if s := strings.TrimSpace(anyString(value)); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func moveUnique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func moveExtractObject(raw any, key string) map[string]any {
	entry := asMap(raw)
	if nested := asMap(entry[key]); nested != nil {
		return nested
	}
	return entry
}

func moveMapGet(ctx context.Context, a *App, cfg config.Resolved, method, path string, body any, key string) (map[string]any, error) {
	result, err := moveCall(ctx, a, cfg, method, path, body)
	if err != nil {
		return nil, err
	}
	if key == "" {
		return result, nil
	}
	value := asMap(result[key])
	if value == nil {
		return nil, errnorm.Internal("move_response_invalid", "move API response omitted "+key)
	}
	return value, nil
}

func moveInt64(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	default:
		return 0
	}
}

func moveInt64Pointer(value int64) *int64 { return &value }

func cloneMoveMap(source map[string]any) map[string]any {
	copy := make(map[string]any, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}

func stringPointer(value string) *string { return &value }

func moveJSONEqual(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftJSON) == string(rightJSON)
}
