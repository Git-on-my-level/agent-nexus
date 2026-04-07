package app

import (
	"context"
	"fmt"
	"strings"

	"organization-autorunner-cli/internal/config"
	"organization-autorunner-cli/internal/errnorm"
)

var topicsSubcommandSpec = subcommandSpec{
	command: "topics",
	valid: []string{
		"list", "get", "create", "patch", "timeline", "workspace",
		"archive", "unarchive", "trash", "restore",
	},
	examples: []string{"oar topics list", "oar topics create --from-file topic.json", "oar topics workspace --topic-id <topic-id>", "oar topics archive --topic-id <topic-id>"},
	aliases: map[string]string{
		"ls":     "list",
		"show":   "get",
		"update": "patch",
	},
}

var cardsSubcommandSpec = subcommandSpec{
	command:  "cards",
	valid:    []string{"list", "get", "create", "patch", "move", "archive", "trash", "purge", "restore", "timeline"},
	examples: []string{"oar cards list", "oar cards create --from-file card.json", "oar cards get --card-id <card-id>", "oar cards timeline --card-id <card-id>", "oar cards move --card-id <card-id> --from-file move.json", "oar cards archive --card-id <card-id>", "oar cards trash --card-id <card-id> --from-file trash.json"},
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
	case "archive":
		id, body, err := a.parseTopicIDAndOptionalJSONBody(args[1:], "topics archive")
		if err != nil {
			return nil, "topics archive", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "topics archive", "topics.archive", "topic_id", id, topicIDLookupSpec, nil, body)
		return result, "topics archive", callErr
	case "unarchive":
		id, body, err := a.parseTopicIDAndOptionalJSONBody(args[1:], "topics unarchive")
		if err != nil {
			return nil, "topics unarchive", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "topics unarchive", "topics.unarchive", "topic_id", id, topicIDLookupSpec, nil, body)
		return result, "topics unarchive", callErr
	case "trash":
		id, body, err := a.parseTopicIDAndBodyInput(args[1:], "topics trash")
		if err != nil {
			return nil, "topics trash", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "topics trash", "topics.trash", "topic_id", id, topicIDLookupSpec, nil, body)
		return result, "topics trash", callErr
	case "restore":
		id, body, err := a.parseTopicIDAndOptionalJSONBody(args[1:], "topics restore")
		if err != nil {
			return nil, "topics restore", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "topics restore", "topics.restore", "topic_id", id, topicIDLookupSpec, nil, body)
		return result, "topics restore", callErr
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
	case "create":
		body, err := a.parseJSONBodyInput(args[1:], "cards create")
		if err != nil {
			return nil, "cards create", err
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "cards create", "cards.create", nil, nil, body)
		return result, "cards create", callErr
	case "get":
		id, err := parseIDArg(args[1:], "card-id", "card id")
		if err != nil {
			return nil, "cards get", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "cards get", "cards.get", "card_id", id, cardIDLookupSpec, nil, nil)
		return result, "cards get", callErr
	case "timeline":
		id, err := parseIDArg(args[1:], "card-id", "card id")
		if err != nil {
			return nil, "cards timeline", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "cards timeline", "cards.timeline", "card_id", id, cardIDLookupSpec, nil, nil)
		return result, "cards timeline", callErr
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
	case "archive":
		id, body, err := a.parseCardIDAndOptionalJSONBody(args[1:], "cards archive")
		if err != nil {
			return nil, "cards archive", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "cards archive", "cards.archive", "card_id", id, cardIDLookupSpec, nil, body)
		return result, "cards archive", callErr
	case "trash":
		id, body, err := a.parseIDAndBodyInput(args[1:], "card-id", "card id", "cards trash")
		if err != nil {
			return nil, "cards trash", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "cards trash", "cards.trash", "card_id", id, cardIDLookupSpec, nil, body)
		return result, "cards trash", callErr
	case "restore":
		id, body, err := a.parseCardIDAndOptionalJSONBody(args[1:], "cards restore")
		if err != nil {
			return nil, "cards restore", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "cards restore", "cards.restore", "card_id", id, cardIDLookupSpec, nil, body)
		return result, "cards restore", callErr
	case "purge":
		id, body, err := a.parseCardIDAndOptionalJSONBody(args[1:], "cards purge")
		if err != nil {
			return nil, "cards purge", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "cards purge", "cards.purge", "card_id", id, cardIDLookupSpec, nil, body)
		return result, "cards purge", callErr
	default:
		return nil, "cards", cardsSubcommandSpec.unknownError(args[0])
	}
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

func (a *App) normalizeCardMutationBody(ctx context.Context, cfg config.Resolved, commandID string, body map[string]any) error {
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
		move := effectiveCardMoveMutationMap(body)
		if move == nil {
			return nil
		}
		return a.normalizeMutationFields(ctx, cfg, move, []mutationFieldSpec{
			{key: "resolution_refs", kind: mutationFieldTypedRefList},
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
	case "boards.create":
		return a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "board"), []mutationFieldSpec{
			{key: "pinned_refs", kind: mutationFieldTypedRefList},
		})
	case "boards.update":
		return a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "patch"), []mutationFieldSpec{
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
		resolvedBoard, err := a.resolveMaybeBoardID(ctx, cfg, rawBoardID)
		if err != nil {
			return err
		}
		if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoard, body, "before_card_id"); err != nil {
			return err
		}
		if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoard, body, "after_card_id"); err != nil {
			return err
		}
		if cardNest, ok := body["card"].(map[string]any); ok && cardNest != nil {
			if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoard, cardNest, "before_card_id"); err != nil {
				return err
			}
			return a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoard, cardNest, "after_card_id")
		}
		return nil
	case "boards.cards.add", "boards.cards.create":
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
		if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoard, body, "after_card_id"); err != nil {
			return err
		}
		if cardNest, ok := body["card"].(map[string]any); ok && cardNest != nil {
			if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoard, cardNest, "before_card_id"); err != nil {
				return err
			}
			return a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoard, cardNest, "after_card_id")
		}
		return nil
	case "boards.cards.move":
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
	case "inbox.acknowledge":
		return a.normalizeMutationFields(ctx, cfg, body, []mutationFieldSpec{
			{key: "subject_ref", kind: mutationFieldTypedRef},
		})
	case "packets.receipts.create":
		if err := a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "artifact"), []mutationFieldSpec{
			{key: "refs", kind: mutationFieldTypedRefList},
		}); err != nil {
			return err
		}
		return a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "packet"), []mutationFieldSpec{
			{key: "subject_ref", kind: mutationFieldTypedRef},
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
			{key: "receipt_ref", kind: mutationFieldTypedRef},
			{key: "evidence_refs", kind: mutationFieldTypedRefList},
		})
	default:
		return nil
	}
}

func (a *App) parseTopicIDAndBodyInput(args []string, commandName string) (string, map[string]any, error) {
	id, body, err := a.parseIDAndBodyInput(args, "topic-id", "topic id", commandName)
	if err != nil {
		return "", nil, err
	}
	bodyMap, ok := body.(map[string]any)
	if !ok {
		return "", nil, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body for `oar %s` must be an object", commandName))
	}
	return id, bodyMap, nil
}

func (a *App) parseTopicIDAndOptionalJSONBody(args []string, commandName string) (string, map[string]any, error) {
	fs := newSilentFlagSet(commandName)
	var topicIDFlag, fromFile trackedString
	fs.Var(&topicIDFlag, "topic-id", "Topic id")
	fs.Var(&fromFile, "from-file", "Load JSON body from file path")
	if err := fs.Parse(args); err != nil {
		return "", nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	id := strings.TrimSpace(topicIDFlag.value)
	if id == "" && len(positionals) > 0 {
		id = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if err := validateID(id, "topic id"); err != nil {
		return "", nil, err
	}
	if len(positionals) > 0 {
		return "", nil, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `oar %s`", commandName))
	}
	payload, err := a.readBodyInput(strings.TrimSpace(fromFile.value))
	if err != nil {
		return "", nil, err
	}
	if len(payload) == 0 {
		return id, map[string]any{}, nil
	}
	decoded, err := decodeJSONPayload(payload)
	if err != nil {
		return "", nil, err
	}
	bodyMap, ok := decoded.(map[string]any)
	if !ok {
		return "", nil, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body for `oar %s` must be an object", commandName))
	}
	return id, bodyMap, nil
}

func (a *App) parseCardIDAndOptionalJSONBody(args []string, commandName string) (string, map[string]any, error) {
	fs := newSilentFlagSet(commandName)
	var cardIDFlag, fromFile trackedString
	fs.Var(&cardIDFlag, "card-id", "Card id")
	fs.Var(&fromFile, "from-file", "Load JSON body from file path")
	if err := fs.Parse(args); err != nil {
		return "", nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	id := strings.TrimSpace(cardIDFlag.value)
	if id == "" && len(positionals) > 0 {
		id = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if err := validateID(id, "card id"); err != nil {
		return "", nil, err
	}
	if len(positionals) > 0 {
		return "", nil, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `oar %s`", commandName))
	}
	payload, err := a.readBodyInput(strings.TrimSpace(fromFile.value))
	if err != nil {
		return "", nil, err
	}
	if len(payload) == 0 {
		return id, map[string]any{}, nil
	}
	decoded, err := decodeJSONPayload(payload)
	if err != nil {
		return "", nil, err
	}
	bodyMap, ok := decoded.(map[string]any)
	if !ok {
		return "", nil, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body for `oar %s` must be an object", commandName))
	}
	return id, bodyMap, nil
}

var refEdgesSubcommandSpec = subcommandSpec{
	command:  "ref-edges",
	valid:    []string{"list"},
	examples: []string{`oar ref-edges list --target-ref card:<card-id>`},
}

func (a *App) runRefEdgesCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 || isHelpToken(args[0]) {
		if text, ok := generatedHelpText("ref-edges"); ok {
			return &commandResult{Text: text}, "ref-edges", nil
		}
		return nil, "ref-edges", refEdgesSubcommandSpec.requiredError()
	}
	sub := refEdgesSubcommandSpec.normalize(args[0])
	if sub != "list" {
		return nil, "ref-edges", refEdgesSubcommandSpec.unknownError(args[0])
	}
	fs := newSilentFlagSet("ref-edges list")
	var sourceRef, targetRef, relation trackedString
	fs.Var(&sourceRef, "source-ref", "Typed source ref for forward lookup")
	fs.Var(&targetRef, "target-ref", "Typed target ref for reverse lookup")
	fs.Var(&relation, "relation", "Optional relation filter")
	if err := fs.Parse(args[1:]); err != nil {
		return nil, "ref-edges list", errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, "ref-edges list", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar ref-edges list`")
	}
	sourceRefValue := strings.TrimSpace(sourceRef.value)
	targetRefValue := strings.TrimSpace(targetRef.value)
	if (sourceRefValue == "" && targetRefValue == "") || (sourceRefValue != "" && targetRefValue != "") {
		return nil, "ref-edges list", errnorm.Usage("invalid_request", "specify exactly one of --source-ref or --target-ref")
	}
	q := make([]queryParam, 0, 3)
	addSingleQuery(&q, "source_ref", sourceRefValue)
	addSingleQuery(&q, "target_ref", targetRefValue)
	addSingleQuery(&q, "relation", strings.TrimSpace(relation.value))
	result, err := a.invokeTypedJSON(ctx, cfg, "ref-edges list", "ref_edges.list", nil, q, nil)
	return result, "ref-edges list", err
}
