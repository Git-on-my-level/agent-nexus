// Concurrency-token checks, card/topic body fetches, and mutation body
// normalization shared by topics and cards write paths.

package app

import (
	"context"
	"fmt"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func normalizedStringsOrEmpty(values []string) []string {
	normalized := normalizeStringFilters(values)
	if normalized == nil {
		return []string{}
	}
	return normalized
}

func (a *App) readTextFlagFile(path string, commandName string, flagName string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errnorm.Usage("invalid_request", fmt.Sprintf("`%s` is required for `anx %s`", flagName, commandName))
	}
	content, err := a.readRawFile(path)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func (a *App) ensureCardPatchConcurrency(ctx context.Context, cfg config.Resolved, cardID string, body map[string]any) error {
	if strings.TrimSpace(anyString(body["if_updated_at"])) != "" {
		return finalizeOptionalMutationBodyActorID(body, cfg)
	}
	card, err := a.fetchCardBody(ctx, cfg, cardID)
	if err != nil {
		return err
	}
	if updatedAt := strings.TrimSpace(anyString(card["updated_at"])); updatedAt != "" {
		body["if_updated_at"] = updatedAt
	}
	if strings.TrimSpace(anyString(body["if_updated_at"])) == "" {
		return errnorm.Usage("invalid_request", "`if_updated_at` is required; run `anx cards get "+cardID+"` and retry with --if-updated-at <updated_at>")
	}
	return finalizeOptionalMutationBodyActorID(body, cfg)
}

func (a *App) ensureTopicPatchConcurrency(ctx context.Context, cfg config.Resolved, topicID string, body map[string]any) error {
	if strings.TrimSpace(anyString(body["if_updated_at"])) != "" {
		return finalizeOptionalMutationBodyActorID(body, cfg)
	}
	topic, err := a.fetchTopicBody(ctx, cfg, topicID)
	if err != nil {
		return err
	}
	if updatedAt := strings.TrimSpace(anyString(topic["updated_at"])); updatedAt != "" {
		body["if_updated_at"] = updatedAt
	}
	if strings.TrimSpace(anyString(body["if_updated_at"])) == "" {
		return errnorm.Usage("invalid_request", "`if_updated_at` is required; run `anx topics get "+topicID+"` and retry with --if-updated-at <updated_at>")
	}
	return finalizeOptionalMutationBodyActorID(body, cfg)
}

func (a *App) ensureCardRevisionBase(ctx context.Context, cfg config.Resolved, cardID string, body map[string]any) error {
	if strings.TrimSpace(anyString(body["if_base_revision"])) != "" {
		return finalizeOptionalMutationBodyActorID(body, cfg)
	}
	card, err := a.fetchCardBody(ctx, cfg, cardID)
	if err != nil {
		return err
	}
	baseRevision := strings.TrimSpace(anyString(card["head_revision_id"]))
	if baseRevision == "" {
		baseRevision = strings.TrimSpace(anyString(card["head_revision_ref"]))
		baseRevision = strings.TrimPrefix(baseRevision, "card_revision:")
	}
	if baseRevision != "" {
		body["if_base_revision"] = baseRevision
	}
	if strings.TrimSpace(anyString(body["if_base_revision"])) == "" {
		return errnorm.Usage("invalid_request", "`if_base_revision` is required; run `anx cards get "+cardID+"` and retry with --if-base-revision <revision-ref-or-handle>")
	}
	return finalizeOptionalMutationBodyActorID(body, cfg)
}

func (a *App) ensureCardMoveConcurrency(ctx context.Context, cfg config.Resolved, cardID string, body map[string]any) error {
	if strings.TrimSpace(anyString(body["if_board_updated_at"])) != "" {
		return finalizeOptionalMutationBodyActorID(body, cfg)
	}
	card, err := a.fetchCardBody(ctx, cfg, cardID)
	if err != nil {
		return err
	}
	boardID := strings.TrimSpace(anyString(card["board_id"]))
	if boardID == "" {
		boardRef := strings.TrimSpace(anyString(card["board_ref"]))
		if strings.HasPrefix(boardRef, "board:") {
			boardID = strings.TrimSpace(strings.TrimPrefix(boardRef, "board:"))
		}
	}
	if boardID == "" {
		return errnorm.Usage("invalid_request", "`if_board_updated_at` is required because the card response did not include board_id or board_ref")
	}
	boardResult, err := a.invokeTypedJSONWithIDResolution(ctx, cfg, "boards get", "boards.get", "board_id", boardID, boardIDLookupSpec, nil, nil)
	if err != nil {
		return err
	}
	boardBody := extractNestedMap(asMap(boardResult.Data), "body")
	board := extractNestedMap(boardBody, "board")
	if updatedAt := strings.TrimSpace(anyString(board["updated_at"])); updatedAt != "" {
		body["if_board_updated_at"] = updatedAt
	}
	if strings.TrimSpace(anyString(body["if_board_updated_at"])) == "" {
		return errnorm.Usage("invalid_request", "`if_board_updated_at` is required; run `anx boards get "+boardID+"` and retry with --if-board-updated-at <updated_at>")
	}
	return finalizeOptionalMutationBodyActorID(body, cfg)
}

func (a *App) fetchCardBody(ctx context.Context, cfg config.Resolved, cardID string) (map[string]any, error) {
	result, err := a.invokeTypedJSONWithIDResolution(ctx, cfg, "cards get", "cards.get", "card_id", cardID, cardIDLookupSpec, nil, nil)
	if err != nil {
		return nil, err
	}
	body := extractNestedMap(asMap(result.Data), "body")
	card := extractNestedMap(body, "card")
	if len(card) == 0 {
		return nil, errnorm.Usage("invalid_request", "cards get response did not include card metadata")
	}
	return card, nil
}

func (a *App) fetchTopicBody(ctx context.Context, cfg config.Resolved, topicID string) (map[string]any, error) {
	result, err := a.invokeTypedJSONWithIDResolution(ctx, cfg, "topics get", "topics.get", "topic_id", topicID, topicIDLookupSpec, nil, nil)
	if err != nil {
		return nil, err
	}
	body := extractNestedMap(asMap(result.Data), "body")
	topic := extractNestedMap(body, "topic")
	if len(topic) == 0 {
		return nil, errnorm.Usage("invalid_request", "topics get response did not include topic metadata")
	}
	return topic, nil
}

func (a *App) normalizeTopicMutationBody(ctx context.Context, cfg config.Resolved, commandID string, body map[string]any) error {
	switch commandID {
	case "topics.create":
		topic := nestedMutationMap(body, "topic")
		if err := a.normalizeMutationFields(ctx, cfg, topic, []mutationFieldSpec{
			{key: "owner_refs", kind: mutationFieldTypedRefList},
			{key: "document_refs", kind: mutationFieldTypedRefList},
			{key: "board_refs", kind: mutationFieldTypedRefList},
			{key: "related_refs", kind: mutationFieldTypedRefList},
		}); err != nil {
			return err
		}
		return nil
	case "topics.patch":
		patch := nestedMutationMap(body, "patch")
		if err := a.normalizeMutationFields(ctx, cfg, patch, []mutationFieldSpec{
			{key: "owner_refs", kind: mutationFieldTypedRefList},
			{key: "document_refs", kind: mutationFieldTypedRefList},
			{key: "board_refs", kind: mutationFieldTypedRefList},
			{key: "related_refs", kind: mutationFieldTypedRefList},
		}); err != nil {
			return err
		}
		return nil
	case "topics.trash":
		return nil
	default:
		return nil
	}
}

func (a *App) normalizeCardMutationBody(ctx context.Context, cfg config.Resolved, commandID string, pathParams map[string]string, body map[string]any) error {
	switch commandID {
	case "cards.create":
		card := nestedMutationMap(body, "card")
		if card == nil {
			return nil
		}
		if err := a.normalizeMutationFields(ctx, cfg, body, []mutationFieldSpec{
			{key: "board_ref", kind: mutationFieldTypedRef},
		}); err != nil {
			return err
		}
		return a.normalizeMutationFields(ctx, cfg, card, []mutationFieldSpec{
			{key: "assignee_refs", kind: mutationFieldTypedRefList},
			{key: "related_refs", kind: mutationFieldTypedRefList},
			{key: "topic_ref", kind: mutationFieldTypedRef},
			{key: "document_ref", kind: mutationFieldTypedRef},
		})
	case "cards.patch":
		patch := nestedMutationMap(body, "patch")
		if patch == nil {
			return nil
		}
		return a.normalizeMutationFields(ctx, cfg, patch, []mutationFieldSpec{
			{key: "assignee_refs", kind: mutationFieldTypedRefList},
			{key: "related_refs", kind: mutationFieldTypedRefList},
			{key: "resolution_refs", kind: mutationFieldTypedRefList},
			{key: "topic_ref", kind: mutationFieldTypedRef},
			{key: "document_ref", kind: mutationFieldTypedRef},
		})
	case "cards.move":
		if err := a.normalizeMutationFields(ctx, cfg, body, []mutationFieldSpec{
			{key: "resolution_refs", kind: mutationFieldTypedRefList},
			{key: "after_thread_id", kind: mutationFieldThreadID},
			{key: "before_thread_id", kind: mutationFieldThreadID},
		}); err != nil {
			return err
		}
		if pathParams == nil {
			return nil
		}
		rawBoardID := strings.TrimSpace(pathParams["board_id"])
		if rawBoardID == "" {
			return nil
		}
		if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, rawBoardID, body, "before_card_id"); err != nil {
			return err
		}
		return a.normalizeBoardMutationCardAnchorField(ctx, cfg, rawBoardID, body, "after_card_id")
	default:
		return nil
	}
}

func (a *App) normalizeMutationCommandBody(ctx context.Context, cfg config.Resolved, commandID string, pathParams map[string]string, body map[string]any) error {
	if strings.HasPrefix(commandID, "topics.") {
		return a.normalizeTopicMutationBody(ctx, cfg, commandID, body)
	}
	if strings.HasPrefix(commandID, "cards.") {
		return a.normalizeCardMutationBody(ctx, cfg, commandID, pathParams, body)
	}
	return a.normalizeMutationCommandBodyLegacy(ctx, cfg, commandID, pathParams, body)
}

func (a *App) normalizeMutationCommandBodyLegacy(ctx context.Context, cfg config.Resolved, commandID string, pathParams map[string]string, body map[string]any) error {
	switch commandID {
	case "boards.create":
		return a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "board"), []mutationFieldSpec{
			{key: "primary_topic_ref", kind: mutationFieldTypedRef},
			{key: "pinned_refs", kind: mutationFieldTypedRefList},
		})
	case "boards.patch":
		return a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "patch"), []mutationFieldSpec{
			{key: "primary_topic_ref", kind: mutationFieldTypedRef},
			{key: "pinned_refs", kind: mutationFieldTypedRefList},
		})
	case "cards.create":
		rawBoardID := strings.TrimSpace(anyString(body["board_id"]))
		if rawBoardID == "" {
			refStr := strings.TrimSpace(anyString(body["board_ref"]))
			if strings.HasPrefix(refStr, "board:") {
				rawBoardID = strings.TrimSpace(strings.TrimPrefix(refStr, "board:"))
			}
		}
		if rawBoardID == "" {
			return nil
		}
		if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, rawBoardID, body, "before_card_id"); err != nil {
			return err
		}
		if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, rawBoardID, body, "after_card_id"); err != nil {
			return err
		}
		if cardNest, ok := body["card"].(map[string]any); ok && cardNest != nil {
			if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, rawBoardID, cardNest, "before_card_id"); err != nil {
				return err
			}
			return a.normalizeBoardMutationCardAnchorField(ctx, cfg, rawBoardID, cardNest, "after_card_id")
		}
		return nil
	case "boards.cards.add":
		if pathParams == nil {
			return nil
		}
		rawBoardID := strings.TrimSpace(pathParams["board_id"])
		if rawBoardID == "" {
			return nil
		}
		if err := a.normalizeMutationFields(ctx, cfg, body, []mutationFieldSpec{
			{key: "thread_id", kind: mutationFieldThreadID},
			{key: "after_thread_id", kind: mutationFieldThreadID},
			{key: "parent_thread", kind: mutationFieldThreadID},
		}); err != nil {
			return err
		}
		if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, rawBoardID, body, "before_card_id"); err != nil {
			return err
		}
		if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, rawBoardID, body, "after_card_id"); err != nil {
			return err
		}
		if cardNest, ok := body["card"].(map[string]any); ok && cardNest != nil {
			if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, rawBoardID, cardNest, "before_card_id"); err != nil {
				return err
			}
			return a.normalizeBoardMutationCardAnchorField(ctx, cfg, rawBoardID, cardNest, "after_card_id")
		}
		return nil
	case "boards.cards.batch_add":
		if pathParams == nil {
			return nil
		}
		rawBoardID := strings.TrimSpace(pathParams["board_id"])
		if rawBoardID == "" {
			return nil
		}
		rawItems, ok := body["items"].([]any)
		if !ok {
			return nil
		}
		for _, el := range rawItems {
			item, ok := el.(map[string]any)
			if !ok {
				continue
			}
			if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, rawBoardID, item, "before_card_id"); err != nil {
				return err
			}
			if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, rawBoardID, item, "after_card_id"); err != nil {
				return err
			}
			if cardNest, ok := item["card"].(map[string]any); ok && cardNest != nil {
				if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, rawBoardID, cardNest, "before_card_id"); err != nil {
					return err
				}
				if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, rawBoardID, cardNest, "after_card_id"); err != nil {
					return err
				}
			}
		}
		return nil
	case "docs.create", "docs.put", "docs.update", "docs.revisions.create":
		if rev, ok := body["revision"].(map[string]any); ok && rev != nil {
			if err := a.normalizeMutationFields(ctx, cfg, rev, []mutationFieldSpec{
				{key: "refs", kind: mutationFieldTypedRefList},
			}); err != nil {
				return err
			}
		}
		return a.normalizeMutationFields(ctx, cfg, body, []mutationFieldSpec{
			{key: "refs", kind: mutationFieldTypedRefList},
		})
	case "events.create":
		return a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "event"), []mutationFieldSpec{
			{key: "thread_id", kind: mutationFieldThreadID},
			{key: "refs", kind: mutationFieldTypedRefList},
		})
	case "inbox.respond":
		return a.normalizeMutationFields(ctx, cfg, body, []mutationFieldSpec{
			{key: "related_refs", kind: mutationFieldTypedRefList},
		})
	case "agent.notifications.read", "agent.notifications.dismiss":
		raw := strings.TrimSpace(anyString(body["wakeup_id"]))
		if raw != "" {
			body["wakeup_id"] = raw
		}
		return nil
	default:
		return nil
	}
}
