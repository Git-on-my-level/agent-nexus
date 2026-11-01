// Lifecycle events for boards and board cards: event payload builders for
// create/update/move/archive/trash and the best-effort emitters that append
// them to the event log.

package server

import (
	"context"
	"strings"
)

func buildBoardCreatedEvent(board map[string]any) map[string]any {
	payload := map[string]any{
		"board_id":      anyString(board["id"]),
		"board_ref":     "board:" + anyString(board["id"]),
		"thread_id":     nullableStringValue(anyString(board["thread_id"])),
		"document_refs": boardDocumentRefs(board),
		"refs":          boardRefs(board),
	}
	return buildBoardLifecycleEvent("board_created", board, nil, payload, "Board created: "+boardDisplayName(board))
}

func buildBoardUpdatedEvent(previousBoard, updatedBoard, patch map[string]any) map[string]any {
	changedFields := sortedMapKeys(patch)
	payload := map[string]any{
		"board_id":       anyString(updatedBoard["id"]),
		"changed_fields": changedFields,
	}
	return buildBoardLifecycleEvent("board_updated", updatedBoard, nil, payload, "Board updated: "+boardDisplayName(updatedBoard))
}

func buildCardCreatedEvent(board, card map[string]any) map[string]any {
	payload := map[string]any{
		"board_id":           anyString(board["id"]),
		"card_id":            anyString(card["id"]),
		"thread_id":          nullableStringValue(anyString(card["thread_id"])),
		"column_key":         anyString(card["column_key"]),
		"status":             nullableStringValue(anyString(card["status"])),
		"title":              nullableStringValue(anyString(card["title"])),
		"summary":            nullableStringValue(strings.TrimSpace(anyString(card["summary"]))),
		"assignee_refs":      cardAssigneeRefsAny(card),
		"document_ref":       nullableStringValue(documentRefForPublicCard(card)),
		"related_refs":       typedRefStringsToAnyList(cardRelatedRefs(card)),
		"definition_of_done": stringSliceAsAnyList(card["definition_of_done"]),
		"resolution_refs":    stringSliceAsAnyList(card["resolution_refs"]),
		"risk":               publicCardRisk(card),
	}
	payload["create_surface"] = card["create_surface"]

	if res := strings.TrimSpace(anyString(card["resolution"])); res != "" {
		payload["resolution"] = res
	} else {
		payload["resolution"] = nil
	}
	if v, ok := card["due_at"]; ok {
		if s := strings.TrimSpace(anyString(v)); s != "" {
			payload["due_at"] = v
		} else {
			payload["due_at"] = nil
		}
	} else {
		payload["due_at"] = nil
	}
	return buildCardLifecycleEvent("card_created", board, card, payload, "Card created: "+cardDisplayName(card))
}

func buildCardUpdatedEvent(board, previousCard, updatedCard map[string]any, changedFields []string) map[string]any {
	payload := map[string]any{
		"board_id":       anyString(board["id"]),
		"card_id":        anyString(updatedCard["id"]),
		"thread_id":      nullableStringValue(anyString(updatedCard["thread_id"])),
		"related_refs":   typedRefStringsToAnyList(cardRelatedRefs(updatedCard)),
		"document_ref":   nullableStringValue(documentRefForPublicCard(updatedCard)),
		"changed_fields": changedFields,
	}
	for _, field := range changedFields {
		switch field {
		case "assignee_refs":
			payload["previous_assignee_refs"] = cardAssigneeRefsAny(previousCard)
			payload["assignee_refs"] = cardAssigneeRefsAny(updatedCard)
		case "related_refs":
			payload["previous_related_refs"] = mergeRelatedRefsForPublicView(previousCard)
			payload["related_refs"] = mergeRelatedRefsForPublicView(updatedCard)
		case "resolution_refs":
			payload["previous_resolution_refs"] = stringSliceAsAnyList(previousCard["resolution_refs"])
			payload["resolution_refs"] = stringSliceAsAnyList(updatedCard["resolution_refs"])
		case "definition_of_done":
			payload["previous_definition_of_done"] = stringSliceAsAnyList(previousCard["definition_of_done"])
			payload["definition_of_done"] = stringSliceAsAnyList(updatedCard["definition_of_done"])
		case "resolution":
			payload["previous_resolution"] = nullableResolutionValue(previousCard["resolution"])
			payload["resolution"] = nullableResolutionValue(updatedCard["resolution"])
		case "risk":
			payload["previous_risk"] = publicCardRisk(previousCard)
			payload["risk"] = publicCardRisk(updatedCard)
		default:
			payload["previous_"+field] = nullableStringValue(anyString(previousCard[field]))
			payload[field] = nullableStringValue(anyString(updatedCard[field]))
		}
	}
	return buildCardLifecycleEvent("card_updated", board, updatedCard, payload, "Card updated: "+cardDisplayName(updatedCard))
}

func buildCardArchivedEvent(board, card map[string]any) map[string]any {
	payload := map[string]any{
		"board_id":     anyString(board["id"]),
		"card_id":      anyString(card["id"]),
		"thread_id":    nullableStringValue(anyString(card["thread_id"])),
		"column_key":   nullableStringValue(anyString(card["column_key"])),
		"document_ref": nullableStringValue(documentRefForPublicCard(card)),
		"related_refs": typedRefStringsToAnyList(cardRelatedRefs(card)),
	}
	return buildCardLifecycleEvent("card_archived", board, card, payload, "Card archived: "+cardDisplayName(card))
}

func buildCardTrashedEvent(board, card map[string]any) map[string]any {
	payload := map[string]any{
		"board_id":     anyString(board["id"]),
		"card_id":      anyString(card["id"]),
		"thread_id":    nullableStringValue(anyString(card["thread_id"])),
		"column_key":   nullableStringValue(anyString(card["column_key"])),
		"document_ref": nullableStringValue(documentRefForPublicCard(card)),
		"related_refs": typedRefStringsToAnyList(cardRelatedRefs(card)),
		"trash_reason": nullableStringValue(anyString(card["trash_reason"])),
	}
	return buildCardLifecycleEvent("card_trashed", board, card, payload, "Card trashed: "+cardDisplayName(card))
}

func buildCardMovedEvent(board, previousCard, updatedCard map[string]any, beforeCardID, afterCardID, beforeThreadID, afterThreadID string) map[string]any {
	payload := map[string]any{
		"board_id":         anyString(board["id"]),
		"card_id":          anyString(updatedCard["id"]),
		"title":            nullableStringValue(anyString(updatedCard["title"])),
		"summary":          nullableStringValue(strings.TrimSpace(anyString(updatedCard["summary"]))),
		"from_column_key":  nullableStringValue(anyString(previousCard["column_key"])),
		"column_key":       nullableStringValue(anyString(updatedCard["column_key"])),
		"before_card_id":   nullableStringValue(strings.TrimSpace(beforeCardID)),
		"after_card_id":    nullableStringValue(strings.TrimSpace(afterCardID)),
		"before_thread_id": nullableStringValue(strings.TrimSpace(beforeThreadID)),
		"after_thread_id":  nullableStringValue(strings.TrimSpace(afterThreadID)),
	}
	event := buildCardLifecycleEvent("card_moved", board, updatedCard, payload, "Card moved: "+cardDisplayName(updatedCard))
	if source := anyString(previousCard["board_id"]); source != "" && source != anyString(board["id"]) {
		payload["before"] = map[string]any{"board_ref": "board:" + source, "column_key": previousCard["column_key"]}
		payload["after"] = map[string]any{"board_ref": "board:" + anyString(board["id"]), "column_key": updatedCard["column_key"]}
		refs, _ := event["refs"].([]string)
		event["refs"] = append(refs, "board:"+source)
	}
	return event
}

func buildCardLifecycleEvent(eventType string, board, card map[string]any, payload map[string]any, summary string) map[string]any {
	refs := append([]string{"board:" + anyString(board["id"])}, boardRefs(board)...)
	if card != nil {
		if cardID := strings.TrimSpace(anyString(card["id"])); cardID != "" {
			refs = append(refs, "card:"+cardID)
		}
		if threadID := strings.TrimSpace(anyString(card["thread_id"])); threadID != "" {
			refs = append(refs, "thread:"+threadID)
		}
		if parentThreadID := primaryRelatedThreadID(card); parentThreadID != "" {
			refs = append(refs, "thread:"+parentThreadID)
		}
		if documentRef := documentRefForPublicCard(card); documentRef != "" {
			refs = append(refs, documentRef)
		}
	}
	refs = uniqueSortedStrings(refs)

	threadID := strings.TrimSpace(anyString(card["thread_id"]))
	event := map[string]any{
		"type":       eventType,
		"thread_id":  threadID,
		"refs":       refs,
		"summary":    strings.TrimSpace(summary),
		"payload":    payload,
		"provenance": eventProvenance(),
	}
	return event
}

func buildBoardLifecycleEvent(eventType string, board, card map[string]any, payload map[string]any, summary string) map[string]any {
	refs := append([]string{"board:" + anyString(board["id"])}, boardRefs(board)...)
	backingThreadID := strings.TrimSpace(anyString(board["thread_id"]))
	if backingThreadID != "" {
		refs = append(refs, "thread:"+backingThreadID)
	}
	if card != nil {
		if cardID := strings.TrimSpace(anyString(card["id"])); cardID != "" {
			refs = append(refs, "card:"+cardID)
		}
		if threadID := strings.TrimSpace(anyString(card["thread_id"])); threadID != "" {
			refs = append(refs, "thread:"+threadID)
		}
		if documentRef := documentRefForPublicCard(card); documentRef != "" {
			refs = append(refs, documentRef)
		}
	}
	refs = uniqueSortedStrings(refs)

	event := map[string]any{
		"type":       eventType,
		"thread_id":  backingThreadID,
		"refs":       refs,
		"summary":    strings.TrimSpace(summary),
		"payload":    payload,
		"provenance": eventProvenance(),
	}
	return event
}

func emitBoardLifecycleEvent(ctx context.Context, opts handlerOptions, actorID string, event map[string]any) error {
	if opts.primitiveStore == nil || event == nil {
		return nil
	}
	_, err := opts.primitiveStore.AppendEvent(ctx, actorID, event)
	if err != nil {
		return err
	}
	notifyProjectionMaintainer(opts)
	return nil
}

func emitBoardLifecycleEventBestEffort(ctx context.Context, opts handlerOptions, actorID string, event map[string]any) {
	_ = emitBoardLifecycleEvent(ctx, opts, actorID, event)
}

func emitCardLifecycleEvent(ctx context.Context, opts handlerOptions, actorID string, event map[string]any) (map[string]any, error) {
	if opts.primitiveStore == nil || event == nil {
		return nil, nil
	}
	stored, err := opts.primitiveStore.AppendEvent(ctx, actorID, event)
	if err != nil {
		return nil, err
	}
	notifyProjectionMaintainer(opts)
	// Projection work is durable and coalesced by the maintenance queue.
	// Never replay thread history on the mutation response path.
	return stored, nil
}

func emitCardLifecycleEventBestEffort(ctx context.Context, opts handlerOptions, actorID string, event map[string]any) {
	_, _ = emitCardLifecycleEvent(ctx, opts, actorID, event)
}

func loadBoardCardForEvent(ctx context.Context, opts handlerOptions, boardID, threadID string) (map[string]any, error) {
	return opts.primitiveStore.GetBoardCard(ctx, boardID, threadID)
}
