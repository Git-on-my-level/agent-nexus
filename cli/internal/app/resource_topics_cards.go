package app

import (
	"context"
	"strings"

	"organization-autorunner-cli/internal/config"
)

var topicsSubcommandSpec = subcommandSpec{
	command:  "topics",
	valid:    []string{"list", "get", "create", "patch", "timeline", "workspace"},
	examples: []string{"oar topics list", "oar topics create --from-file topic.json", "oar topics workspace --topic-id <topic-id>"},
	aliases: map[string]string{
		"ls":     "list",
		"show":   "get",
		"update": "patch",
	},
}

var cardsSubcommandSpec = subcommandSpec{
	command:  "cards",
	valid:    []string{"list", "get", "patch", "move"},
	examples: []string{"oar cards list", "oar cards get --card-id <card-id>", "oar cards move --card-id <card-id> --from-file move.json"},
	aliases: map[string]string{
		"ls":     "list",
		"show":   "get",
		"update": "patch",
	},
}

func (a *App) runTopicsCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 || isHelpToken(args[0]) {
		if text, ok := generatedHelpText("topics"); ok {
			return &commandResult{Text: text}, "topics", nil
		}
		return nil, "topics", topicsSubcommandSpec.requiredError()
	}
	sub := topicsSubcommandSpec.normalize(args[0])
	switch sub {
	case "list":
		result, err := a.invokeTypedJSON(ctx, cfg, "topics list", "topics.list", nil, nil, nil)
		return result, "topics list", err
	case "get":
		id, err := parseIDArg(args[1:], "topic-id", "topic id")
		if err != nil {
			return nil, "topics get", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "topics get", "topics.get", "topic_id", id, topicIDLookupSpec, nil, nil)
		return result, "topics get", callErr
	case "create":
		body, err := a.parseJSONBodyInput(args[1:], "topics create")
		if err != nil {
			return nil, "topics create", err
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "topics create", "topics.create", nil, nil, body)
		return result, "topics create", callErr
	case "patch":
		id, body, err := a.parseIDAndBodyInput(args[1:], "topic-id", "topic id", "topics patch")
		if err != nil {
			return nil, "topics patch", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "topics patch", "topics.patch", "topic_id", id, topicIDLookupSpec, nil, body)
		return result, "topics patch", callErr
	case "timeline":
		id, err := parseIDArg(args[1:], "topic-id", "topic id")
		if err != nil {
			return nil, "topics timeline", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "topics timeline", "topics.timeline", "topic_id", id, topicIDLookupSpec, nil, nil)
		return result, "topics timeline", callErr
	case "workspace":
		id, err := parseIDArg(args[1:], "topic-id", "topic id")
		if err != nil {
			return nil, "topics workspace", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "topics workspace", "topics.workspace", "topic_id", id, topicIDLookupSpec, nil, nil)
		return result, "topics workspace", callErr
	default:
		return nil, "topics", topicsSubcommandSpec.unknownError(args[0])
	}
}

func (a *App) runCardsCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 || isHelpToken(args[0]) {
		if text, ok := generatedHelpText("cards"); ok {
			return &commandResult{Text: text}, "cards", nil
		}
		return nil, "cards", cardsSubcommandSpec.requiredError()
	}
	sub := cardsSubcommandSpec.normalize(args[0])
	switch sub {
	case "list":
		result, err := a.invokeTypedJSON(ctx, cfg, "cards list", "cards.list", nil, nil, nil)
		return result, "cards list", err
	case "get":
		id, err := parseIDArg(args[1:], "card-id", "card id")
		if err != nil {
			return nil, "cards get", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "cards get", "cards.get", "card_id", id, cardIDLookupSpec, nil, nil)
		return result, "cards get", callErr
	case "patch":
		id, body, err := a.parseIDAndBodyInput(args[1:], "card-id", "card id", "cards patch")
		if err != nil {
			return nil, "cards patch", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "cards patch", "cards.patch", "card_id", id, cardIDLookupSpec, nil, body)
		return result, "cards patch", callErr
	case "move":
		id, body, err := a.parseIDAndBodyInput(args[1:], "card-id", "card id", "cards move")
		if err != nil {
			return nil, "cards move", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "cards move", "cards.move", "card_id", id, cardIDLookupSpec, nil, body)
		return result, "cards move", callErr
	default:
		return nil, "cards", cardsSubcommandSpec.unknownError(args[0])
	}
}

func (a *App) normalizeTopicMutationBody(ctx context.Context, cfg config.Resolved, commandID string, body map[string]any) error {
	switch commandID {
	case "topics.create":
		topic := nestedMutationMap(body, "topic")
		if err := a.normalizeMutationFields(ctx, cfg, topic, []mutationFieldSpec{
			{key: "primary_thread_ref", kind: mutationFieldTypedRef},
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
			{key: "primary_thread_ref", kind: mutationFieldTypedRef},
			{key: "owner_refs", kind: mutationFieldTypedRefList},
			{key: "document_refs", kind: mutationFieldTypedRefList},
			{key: "board_refs", kind: mutationFieldTypedRefList},
			{key: "related_refs", kind: mutationFieldTypedRefList},
		}); err != nil {
			return err
		}
		return nil
	default:
		return nil
	}
}

func (a *App) normalizeCardMutationBody(ctx context.Context, cfg config.Resolved, commandID string, body map[string]any) error {
	switch commandID {
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
			{key: "thread_ref", kind: mutationFieldTypedRef},
			{key: "document_ref", kind: mutationFieldTypedRef},
		})
	case "cards.move":
		move := nestedMutationMap(body, "move")
		if move == nil {
			return nil
		}
		return a.normalizeMutationFields(ctx, cfg, move, []mutationFieldSpec{
			{key: "resolution_refs", kind: mutationFieldTypedRefList},
			{key: "before_card_ref", kind: mutationFieldTypedRef},
			{key: "after_card_ref", kind: mutationFieldTypedRef},
		})
	default:
		return nil
	}
}

func (a *App) normalizeMutationCommandBody(ctx context.Context, cfg config.Resolved, commandID string, pathParams map[string]string, body map[string]any) error {
	if strings.HasPrefix(commandID, "topics.") {
		return a.normalizeTopicMutationBody(ctx, cfg, commandID, body)
	}
	if strings.HasPrefix(commandID, "cards.") {
		return a.normalizeCardMutationBody(ctx, cfg, commandID, body)
	}
	return a.normalizeMutationCommandBodyLegacy(ctx, cfg, commandID, pathParams, body)
}

func (a *App) normalizeMutationCommandBodyLegacy(ctx context.Context, cfg config.Resolved, commandID string, pathParams map[string]string, body map[string]any) error {
	switch commandID {
	case "threads.create":
		return a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "thread"), []mutationFieldSpec{
			{key: "key_artifacts", kind: mutationFieldTypedRefList},
		})
	case "threads.patch":
		return a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "patch"), []mutationFieldSpec{
			{key: "key_artifacts", kind: mutationFieldTypedRefList},
		})
	case "commitments.create":
		return a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "commitment"), []mutationFieldSpec{
			{key: "thread_id", kind: mutationFieldThreadID},
			{key: "links", kind: mutationFieldTypedRefList},
		})
	case "commitments.patch":
		if err := a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "patch"), []mutationFieldSpec{
			{key: "links", kind: mutationFieldTypedRefList},
		}); err != nil {
			return err
		}
		return a.normalizeMutationFields(ctx, cfg, body, []mutationFieldSpec{
			{key: "refs", kind: mutationFieldTypedRefList},
		})
	case "boards.create":
		return a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "board"), []mutationFieldSpec{
			{key: "primary_thread_id", kind: mutationFieldThreadID},
			{key: "pinned_refs", kind: mutationFieldTypedRefList},
		})
	case "boards.update":
		return a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "patch"), []mutationFieldSpec{
			{key: "pinned_refs", kind: mutationFieldTypedRefList},
		})
	case "boards.cards.add", "boards.cards.create":
		return a.normalizeMutationFields(ctx, cfg, body, []mutationFieldSpec{
			{key: "thread_id", kind: mutationFieldThreadID},
			{key: "parent_thread", kind: mutationFieldThreadID},
			{key: "before_thread_id", kind: mutationFieldThreadID},
			{key: "after_thread_id", kind: mutationFieldThreadID},
		})
	case "boards.cards.move":
		if err := a.normalizeMutationFields(ctx, cfg, body, []mutationFieldSpec{
			{key: "before_thread_id", kind: mutationFieldThreadID},
			{key: "after_thread_id", kind: mutationFieldThreadID},
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
		resolvedBoard, err := a.resolveMaybeBoardID(ctx, cfg, rawBoardID)
		if err != nil {
			return err
		}
		if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoard, body, "before_card_id"); err != nil {
			return err
		}
		return a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoard, body, "after_card_id")
	case "docs.create", "docs.update", "docs.revisions.create":
		return a.normalizeMutationFields(ctx, cfg, body, []mutationFieldSpec{
			{key: "refs", kind: mutationFieldTypedRefList},
		})
	case "events.create":
		return a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "event"), []mutationFieldSpec{
			{key: "thread_id", kind: mutationFieldThreadID},
			{key: "refs", kind: mutationFieldTypedRefList},
		})
	case "inbox.ack":
		return a.normalizeMutationFields(ctx, cfg, body, []mutationFieldSpec{
			{key: "thread_id", kind: mutationFieldThreadID},
		})
	case "packets.work-orders.create":
		if err := a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "artifact"), []mutationFieldSpec{
			{key: "refs", kind: mutationFieldTypedRefList},
		}); err != nil {
			return err
		}
		return a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "packet"), []mutationFieldSpec{
			{key: "subject_ref", kind: mutationFieldTypedRef},
			{key: "context_refs", kind: mutationFieldTypedRefList},
		})
	case "packets.receipts.create":
		if err := a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "artifact"), []mutationFieldSpec{
			{key: "refs", kind: mutationFieldTypedRefList},
		}); err != nil {
			return err
		}
		return a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "packet"), []mutationFieldSpec{
			{key: "subject_ref", kind: mutationFieldTypedRef},
			{key: "work_order_ref", kind: mutationFieldTypedRef},
			{key: "outputs", kind: mutationFieldTypedRefList},
			{key: "verification_evidence", kind: mutationFieldTypedRefList},
		})
	case "packets.reviews.create":
		if err := a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "artifact"), []mutationFieldSpec{
			{key: "refs", kind: mutationFieldTypedRefList},
		}); err != nil {
			return err
		}
		return a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "packet"), []mutationFieldSpec{
			{key: "subject_ref", kind: mutationFieldTypedRef},
			{key: "work_order_ref", kind: mutationFieldTypedRef},
			{key: "receipt_ref", kind: mutationFieldTypedRef},
			{key: "evidence_refs", kind: mutationFieldTypedRefList},
		})
	default:
		return nil
	}
}
