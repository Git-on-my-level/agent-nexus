// Input parsing for `anx topics` mutations: create, patch, and topic id+body
// handling.

package app

import (
	"context"
	"fmt"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

// Review guideline: commands with at most one required scalar request-body field
// must expose flag-based non-JSON input and keep JSON stdin/--from-file as the
// advanced compatibility path.

func (a *App) parseTopicCreateInput(args []string, cfg config.Resolved, commandName string) (any, bool, error) {
	fs := newSilentFlagSet(commandName)
	var fromFileFlag, titleFlag, summaryFlag, actorIDFlag trackedString
	var ownerRefFlags, documentRefFlags, boardRefFlags, relatedRefFlags trackedStrings
	var dryRunFlag trackedBool
	fs.Var(&fromFileFlag, "from-file", "Advanced JSON request body from file or stdin with -")
	fs.Var(&titleFlag, "title", "Topic title")
	fs.Var(&summaryFlag, "summary", "Topic summary")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	fs.Var(&ownerRefFlags, "owner-ref", "Owner typed ref, repeatable")
	fs.Var(&documentRefFlags, "document-ref", "Linked document typed ref, repeatable")
	fs.Var(&boardRefFlags, "board-ref", "Linked board typed ref, repeatable")
	fs.Var(&relatedRefFlags, "ref", "Additional related typed ref, repeatable")
	fs.Var(&dryRunFlag, "dry-run", "Validate and render request without sending the mutation")
	if err := fs.Parse(args); err != nil {
		return nil, false, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, false, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `anx %s`", commandName))
	}
	fieldFlagsSet := strings.TrimSpace(titleFlag.value) != "" ||
		strings.TrimSpace(summaryFlag.value) != "" ||
		len(ownerRefFlags.values) > 0 ||
		len(documentRefFlags.values) > 0 ||
		len(boardRefFlags.values) > 0 ||
		len(relatedRefFlags.values) > 0 ||
		strings.TrimSpace(actorIDFlag.value) != ""
	if strings.TrimSpace(fromFileFlag.value) != "" || !fieldFlagsSet {
		if fieldFlagsSet {
			return nil, false, errnorm.Usage("invalid_args", fmt.Sprintf("field flags cannot be combined with JSON body input for `anx %s`", commandName))
		}
		body, _, err := a.parseJSONBodyInputWithOptions(args, commandName, jsonBodyInputOptions{allowDryRun: true})
		if err != nil {
			return nil, false, err
		}
		if bodyMap, ok := body.(map[string]any); ok {
			if err := finalizeOptionalMutationBodyActorID(bodyMap, cfg); err != nil {
				return nil, false, err
			}
		}
		return body, dryRunFlag.set && dryRunFlag.value, nil
	}
	title := strings.TrimSpace(titleFlag.value)
	if title == "" {
		return nil, false, errnorm.Usage("invalid_request", "`--title` is required for `anx topics create`")
	}
	summary := strings.TrimSpace(summaryFlag.value)
	if summary == "" {
		return nil, false, errnorm.Usage("invalid_request", "`--summary` is required for `anx topics create`")
	}
	body := map[string]any{
		"topic": map[string]any{
			"title":         title,
			"summary":       summary,
			"owner_refs":    normalizedStringsOrEmpty(ownerRefFlags.values),
			"document_refs": normalizedStringsOrEmpty(documentRefFlags.values),
			"board_refs":    normalizedStringsOrEmpty(boardRefFlags.values),
			"related_refs":  normalizedStringsOrEmpty(relatedRefFlags.values),
			"provenance":    map[string]any{"sources": []any{"event:anx-cli"}},
		},
	}
	actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
	if err != nil {
		return nil, false, err
	}
	if actorID != "" {
		body["actor_id"] = actorID
	}
	if err := finalizeOptionalMutationBodyActorID(body, cfg); err != nil {
		return nil, false, err
	}
	return body, dryRunFlag.set && dryRunFlag.value, nil
}

func (a *App) parseTopicPatchInput(ctx context.Context, args []string, cfg config.Resolved, commandName string) (string, map[string]any, bool, error) {
	leadingTopicID, args := popLeadingPositional(args)
	fs := newSilentFlagSet(commandName)
	var topicIDFlag, fromFileFlag, titleFlag, summaryFlag, ifUpdatedAtFlag, actorIDFlag trackedString
	var dryRunFlag trackedBool
	fs.Var(&topicIDFlag, "topic-id", "Topic id")
	fs.Var(&fromFileFlag, "from-file", "Advanced JSON patch request body from file")
	fs.Var(&titleFlag, "title", "Topic title")
	fs.Var(&summaryFlag, "summary", "Topic summary")
	fs.Var(&ifUpdatedAtFlag, "if-updated-at", "Topic updated_at concurrency token; discovered from topics get when omitted")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	fs.Var(&dryRunFlag, "dry-run", "Validate and render request without sending the mutation")
	if err := fs.Parse(args); err != nil {
		return "", nil, false, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	topicID := firstNonEmpty(strings.TrimSpace(topicIDFlag.value), leadingTopicID)
	if topicID == "" && len(positionals) > 0 {
		topicID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if err := validateID(topicID, "topic id"); err != nil {
		return "", nil, false, err
	}
	if len(positionals) > 0 {
		return "", nil, false, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `anx %s`", commandName))
	}

	// Only patch-field flags select JSON vs flag mode; metadata flags stay compatible with stdin/--from-file.
	fieldFlagsSet := strings.TrimSpace(titleFlag.value) != "" ||
		strings.TrimSpace(summaryFlag.value) != ""
	if strings.TrimSpace(fromFileFlag.value) != "" || !fieldFlagsSet {
		if fieldFlagsSet {
			return "", nil, false, errnorm.Usage("invalid_args", fmt.Sprintf("field flags cannot be combined with JSON body input for `anx %s`", commandName))
		}
		payload, err := a.readBodyInput(strings.TrimSpace(fromFileFlag.value))
		if err != nil {
			return "", nil, false, err
		}
		if len(payload) == 0 {
			return "", nil, false, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body is required for `anx %s` (provide stdin or --from-file)", commandName))
		}
		bodyAny, err := decodeJSONPayload(payload)
		if err != nil {
			return "", nil, false, err
		}
		bodyMap, ok := bodyAny.(map[string]any)
		if !ok {
			return "", nil, false, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body for `anx %s` must be an object", commandName))
		}
		return topicID, bodyMap, dryRunFlag.set && dryRunFlag.value, nil
	}

	patch := map[string]any{}
	if title := strings.TrimSpace(titleFlag.value); title != "" {
		patch["title"] = title
	}
	if summary := strings.TrimSpace(summaryFlag.value); summary != "" {
		patch["summary"] = summary
	}
	if len(patch) == 0 {
		return "", nil, false, errnorm.Usage("invalid_request", "`anx topics patch` requires --title, --summary, or --from-file")
	}
	body := map[string]any{"patch": patch}
	if ifUpdatedAt := strings.TrimSpace(ifUpdatedAtFlag.value); ifUpdatedAt != "" {
		body["if_updated_at"] = ifUpdatedAt
	}
	actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
	if err != nil {
		return "", nil, false, err
	}
	if actorID != "" {
		body["actor_id"] = actorID
	}
	if err := a.ensureTopicPatchConcurrency(ctx, cfg, topicID, body); err != nil {
		return "", nil, false, err
	}
	return topicID, body, dryRunFlag.set && dryRunFlag.value, nil
}

func normalizeTopicFlagRef(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, ":") {
		return raw
	}
	return "topic:" + raw
}

func (a *App) parseTopicIDAndBodyInput(args []string, commandName string) (string, map[string]any, error) {
	id, body, err := a.parseIDAndBodyInput(args, "topic-id", "topic id", commandName)
	if err != nil {
		return "", nil, err
	}
	bodyMap, ok := body.(map[string]any)
	if !ok {
		return "", nil, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body for `anx %s` must be an object", commandName))
	}
	return id, bodyMap, nil
}
