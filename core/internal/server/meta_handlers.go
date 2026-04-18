package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"

	"agent-nexus-core/internal/auth"
)

func handleMetaHandshake(w http.ResponseWriter, _ *http.Request, opts handlerOptions, schemaVersion string) {
	payload, err := handshakePayload(opts, schemaVersion)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "meta_unavailable", "generated command metadata is not available")
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func handshakePayload(opts handlerOptions, schemaVersion string) (map[string]any, error) {
	commandRegistryDigest, err := loadCommandRegistryDigest(opts)
	if err != nil {
		return nil, err
	}
	humanAuthMode := auth.HumanAuthModeWorkspaceLocal
	if opts.workspaceHumanGrantVerifier != nil {
		humanAuthMode = auth.HumanAuthModeExternalGrant
	}
	payload := map[string]any{
		"core_version":            strings.TrimSpace(opts.coreVersion),
		"api_version":             strings.TrimSpace(opts.apiVersion),
		"schema_version":          strings.TrimSpace(schemaVersion),
		"command_registry_digest": strings.TrimSpace(commandRegistryDigest),
		"min_cli_version":         strings.TrimSpace(opts.minCLIVersion),
		"recommended_cli_version": strings.TrimSpace(opts.recommendedCLIVersion),
		"cli_download_url":        strings.TrimSpace(opts.cliDownloadURL),
		"core_instance_id":        strings.TrimSpace(opts.coreInstanceID),
		"dev_actor_mode":          opts.enableDevActorMode,
		"human_auth_mode":         humanAuthMode,
	}
	return payload, nil
}

func versionPayload(opts handlerOptions, schemaVersion string) (map[string]any, error) {
	commandRegistryDigest, err := loadCommandRegistryDigest(opts)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"schema_version":          strings.TrimSpace(schemaVersion),
		"command_registry_digest": strings.TrimSpace(commandRegistryDigest),
	}, nil
}

func handleMetaCommands(w http.ResponseWriter, _ *http.Request, opts handlerOptions) {
	payload, _, err := loadMetaCommandsPayload(opts)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "meta_unavailable", "generated command metadata is not available")
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func handleMetaCommandByID(w http.ResponseWriter, _ *http.Request, opts handlerOptions, commandID string) {
	_, commands, err := loadMetaCommandsPayload(opts)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "meta_unavailable", "generated command metadata is not available")
		return
	}

	commandID = strings.TrimSpace(commandID)
	for _, command := range commands {
		if strings.TrimSpace(anyString(command["command_id"])) == commandID {
			writeJSON(w, http.StatusOK, map[string]any{"command": command})
			return
		}
	}

	writeError(w, http.StatusNotFound, "not_found", "command metadata not found")
}

func handleMetaConcepts(w http.ResponseWriter, _ *http.Request, opts handlerOptions) {
	_, commands, err := loadMetaCommandsPayload(opts)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "meta_unavailable", "generated command metadata is not available")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"concepts": deriveMetaConcepts(commands)})
}

func handleMetaConceptByName(w http.ResponseWriter, _ *http.Request, opts handlerOptions, conceptName string) {
	_, commands, err := loadMetaCommandsPayload(opts)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "meta_unavailable", "generated command metadata is not available")
		return
	}

	conceptName = strings.ToLower(strings.TrimSpace(conceptName))
	if conceptName == "" {
		writeError(w, http.StatusNotFound, "not_found", "concept metadata not found")
		return
	}

	commandIDs := make([]string, 0)
	matchedCommands := make([]map[string]any, 0)
	for _, command := range commands {
		if !commandHasConcept(command, conceptName) {
			continue
		}
		commandID := strings.TrimSpace(anyString(command["command_id"]))
		if commandID == "" {
			continue
		}
		commandIDs = append(commandIDs, commandID)
		matchedCommands = append(matchedCommands, command)
	}
	if len(commandIDs) == 0 {
		writeError(w, http.StatusNotFound, "not_found", "concept metadata not found")
		return
	}
	sort.Strings(commandIDs)
	sort.Slice(matchedCommands, func(i, j int) bool {
		return strings.TrimSpace(anyString(matchedCommands[i]["command_id"])) < strings.TrimSpace(anyString(matchedCommands[j]["command_id"]))
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"concept": map[string]any{
			"name":          conceptName,
			"command_count": len(commandIDs),
			"command_ids":   commandIDs,
			"commands":      matchedCommands,
		},
	})
}

func loadMetaCommandsPayload(opts handlerOptions) (map[string]any, []map[string]any, error) {
	candidates := make([]string, 0, 1+len(defaultMetaCommandsPathCandidates()))
	if strings.TrimSpace(opts.metaCommandsPath) != "" {
		candidates = append(candidates, strings.TrimSpace(opts.metaCommandsPath))
	} else {
		candidates = append(candidates, defaultMetaCommandsPathCandidates()...)
	}
	candidates = uniqueNonEmptyStrings(candidates)

	loadErrors := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		content, err := os.ReadFile(candidate)
		if err != nil {
			loadErrors = append(loadErrors, fmt.Sprintf("%s: %v", candidate, err))
			continue
		}

		var payload map[string]any
		if err := json.Unmarshal(content, &payload); err != nil {
			loadErrors = append(loadErrors, fmt.Sprintf("%s: decode failed: %v", candidate, err))
			continue
		}

		commandsRaw, ok := payload["commands"].([]any)
		if !ok {
			loadErrors = append(loadErrors, fmt.Sprintf("%s: missing commands list", candidate))
			continue
		}

		commands := make([]map[string]any, 0, len(commandsRaw))
		valid := true
		for _, raw := range commandsRaw {
			commandMap, ok := raw.(map[string]any)
			if !ok {
				valid = false
				loadErrors = append(loadErrors, fmt.Sprintf("%s: command entry has unexpected shape", candidate))
				break
			}
			commands = append(commands, commandMap)
		}
		if !valid {
			continue
		}

		return payload, commands, nil
	}

	return nil, nil, fmt.Errorf("failed to load generated command metadata: %s", strings.Join(loadErrors, "; "))
}

func deriveMetaConcepts(commands []map[string]any) []map[string]any {
	commandIDsByConcept := make(map[string]map[string]struct{})
	for _, command := range commands {
		commandID := strings.TrimSpace(anyString(command["command_id"]))
		if commandID == "" {
			continue
		}
		for _, concept := range anyStringSlice(command["concepts"]) {
			normalized := strings.ToLower(strings.TrimSpace(concept))
			if normalized == "" {
				continue
			}
			existing, ok := commandIDsByConcept[normalized]
			if !ok {
				existing = map[string]struct{}{}
				commandIDsByConcept[normalized] = existing
			}
			existing[commandID] = struct{}{}
		}
	}

	concepts := make([]map[string]any, 0, len(commandIDsByConcept))
	for conceptName, commandSet := range commandIDsByConcept {
		commandIDs := make([]string, 0, len(commandSet))
		for commandID := range commandSet {
			commandIDs = append(commandIDs, commandID)
		}
		sort.Strings(commandIDs)
		concepts = append(concepts, map[string]any{
			"name":          conceptName,
			"command_count": len(commandIDs),
			"command_ids":   commandIDs,
		})
	}

	sort.Slice(concepts, func(i, j int) bool {
		return anyString(concepts[i]["name"]) < anyString(concepts[j]["name"])
	})
	return concepts
}

func commandHasConcept(command map[string]any, conceptName string) bool {
	for _, concept := range anyStringSlice(command["concepts"]) {
		if strings.ToLower(strings.TrimSpace(concept)) == conceptName {
			return true
		}
	}
	return false
}

func anyString(raw any) string {
	value, _ := raw.(string)
	return value
}

func anyStringSlice(raw any) []string {
	itemsRaw, ok := raw.([]any)
	if !ok {
		return nil
	}
	items := make([]string, 0, len(itemsRaw))
	for _, item := range itemsRaw {
		value, ok := item.(string)
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		items = append(items, value)
	}
	return items
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := map[string]struct{}{}
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
	return out
}
