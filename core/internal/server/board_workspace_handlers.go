// The board workspace view: GET /boards/{id}/workspace and the builders that
// assemble its cards, documents, inbox, and summary sections from canonical
// records and topic projection state.

package server

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

func handleGetBoardWorkspace(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID string) {
	opts.summaryFormat = r.URL.Query().Get("summary") == "1"
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var ok bool
	boardID, ok = resolveHTTPResourceID(w, r, opts, "board", boardID, "board")
	if !ok {
		return
	}

	body, err := buildBoardWorkspacePayload(r.Context(), opts, boardID, planVisibility(r, opts))
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "board not found")
			return
		}
		writeInternalError(w, r, err, "failed to load board workspace")
		return
	}

	writeJSON(w, http.StatusOK, body)
}

func buildBoardWorkspacePayload(ctx context.Context, opts handlerOptions, boardID string, visibility ...func(string, string) bool) (map[string]any, error) {
	board, err := opts.primitiveStore.GetBoard(ctx, boardID)
	if err != nil {
		return nil, err
	}

	warnings := make([]map[string]any, 0)

	backingThreadID := strings.TrimSpace(anyString(board["thread_id"]))
	if backingThreadID == "" {
		backingThreadID = boardID
	}

	var backingThread any
	if backingThreadID != "" {
		thread, threadErr := opts.primitiveStore.GetThread(ctx, backingThreadID)
		if threadErr != nil {
			if errors.Is(threadErr, primitives.ErrNotFound) {
				warnings = append(warnings, map[string]any{
					"thread_id": backingThreadID,
					"message":   "board backing thread is no longer available",
				})
			} else {
				return nil, threadErr
			}
		} else {
			if thread != nil {
				primitives.StripThreadPlanningFieldsForAPI(thread)
			}
			backingThread = thread
		}
	}

	if docRefs := boardDocumentRefs(board); len(docRefs) > 0 {
		_, docID, refErr := schema.SplitTypedRef(docRefs[0])
		if refErr == nil && docID != "" {
			_, _, docErr := opts.primitiveStore.GetDocument(ctx, docID)
			if docErr != nil {
				if errors.Is(docErr, primitives.ErrNotFound) {
					warnings = append(warnings, map[string]any{
						"document_id": docID,
						"message":     "board primary document is no longer available",
					})
				} else {
					return nil, docErr
				}
			}
		}
	}

	var primaryTopic any
	if topicRef := strings.TrimSpace(anyString(board["primary_topic_ref"])); topicRef != "" {
		_, topicID, refErr := schema.SplitTypedRef(topicRef)
		if refErr == nil && topicID != "" {
			topic, topicErr := opts.primitiveStore.GetTopic(ctx, topicID)
			if topicErr != nil {
				if errors.Is(topicErr, primitives.ErrNotFound) {
					warnings = append(warnings, map[string]any{
						"topic_id": topicID,
						"message":  "board primary topic is no longer available",
					})
				} else {
					return nil, topicErr
				}
			} else {
				primaryTopic = topic
			}
		}
	}

	var cards []map[string]any
	if opts.summaryFormat {
		cards, err = opts.primitiveStore.ListCards(ctx, primitives.CardListFilter{BoardID: boardID, States: []string{"active"}, Limit: summaryCandidateLimit + 1})
	} else {
		cards, err = opts.primitiveStore.ListBoardCards(ctx, boardID)
	}
	if err != nil {
		return nil, err
	}

	cardsTruncated := false
	if opts.summaryFormat {
		cards, cardsTruncated = summaryCandidates(cards)
	}
	if len(visibility) > 0 {
		if store, ok := opts.primitiveStore.(interface {
			FilterCardAccess(context.Context, []map[string]any, func(string, string) bool) ([]map[string]any, error)
		}); ok {
			cards, err = store.FilterCardAccess(ctx, cards, visibility[0])
			if err != nil {
				return nil, err
			}
		}
	}
	if store, ok := opts.primitiveStore.(planStore); ok && opts.summaryFormat {
		var visible func(string, string) bool
		if len(visibility) > 0 {
			visible = visibility[0]
		}
		if err = store.EnrichCardPlans(ctx, cards, visible, time.Now().UTC()); err != nil {
			return nil, err
		}
		for _, card := range cards {
			card["summary_format"] = true
		}
	}
	threadIDs := collectBoardWorkspaceThreadIDs(backingThreadID, board, cards)
	now := time.Now().UTC()
	states, err := loadTopicProjectionStates(ctx, opts, threadIDs)
	if err != nil {
		return nil, err
	}

	cardSection, cardWarnings, err := buildBoardWorkspaceCardsSection(ctx, opts, board, cards, states)
	if err != nil {
		return nil, err
	}
	warnings = append(warnings, cardWarnings...)

	documentsSection, err := buildBoardWorkspaceDocumentsSection(ctx, opts, board, threadIDs)
	if err != nil {
		return nil, err
	}
	inboxSection, err := buildBoardWorkspaceInboxSection(ctx, opts, threadIDs, now, states)
	if err != nil {
		return nil, err
	}

	boardSummary := buildBoardWorkspaceSummary(board, cards, states, documentsSection, now)
	if cardsTruncated {
		boardSummary["truncated"] = true
	}
	freshness := aggregateTopicProjectionFreshness(states, threadIDs)
	response := map[string]any{
		"board_id":                boardID,
		"board":                   board,
		"primary_topic":           primaryTopic,
		"backing_thread":          backingThread,
		"cards":                   cardSection,
		"documents":               documentsSection,
		"inbox":                   inboxSection,
		"board_summary":           boardSummary,
		"projection_freshness":    freshness,
		"board_summary_freshness": cloneWorkspaceMap(freshness),
		"warnings":                map[string]any{"items": warnings, "count": len(warnings)},
		"section_kinds":           map[string]any{"board": "canonical", "primary_topic": "canonical", "backing_thread": "canonical", "cards": "convenience", "documents": "derived", "inbox": "derived", "board_summary": "derived"},
		"generated_at":            now.Format(time.RFC3339Nano),
	}
	if opts.summaryFormat {
		response["cards_truncated"] = cardsTruncated
	}
	return response, nil
}

func buildBoardWorkspaceCardsSection(ctx context.Context, opts handlerOptions, board map[string]any, cards []map[string]any, states map[string]topicProjectionState) (map[string]any, []map[string]any, error) {
	threadIDsForMsgs := make([]string, 0, len(cards))
	for _, card := range cards {
		if tid := strings.TrimSpace(anyString(card["thread_id"])); tid != "" {
			threadIDsForMsgs = append(threadIDsForMsgs, tid)
		}
	}
	msgCounts := map[string]int{}
	if len(threadIDsForMsgs) > 0 {
		var err error
		msgCounts, err = opts.primitiveStore.BatchCountMessagePostedEventsByThreadIDs(ctx, threadIDsForMsgs)
		if err != nil {
			return nil, nil, err
		}
	}

	items := make([]map[string]any, 0, len(cards))
	warnings := make([]map[string]any, 0)
	for _, card := range cards {
		threadID := strings.TrimSpace(anyString(card["thread_id"]))
		var (
			thread any
			err    error
		)
		if threadID != "" {
			thread, err = opts.primitiveStore.GetThread(ctx, threadID)
			if err != nil {
				if errors.Is(err, primitives.ErrNotFound) {
					warnings = append(warnings, map[string]any{"thread_id": threadID, "message": "board card backing thread was not found"})
					thread = nil
				} else {
					return nil, nil, err
				}
			}
			if tm, ok := thread.(map[string]any); ok && tm != nil {
				primitives.StripThreadPlanningFieldsForAPI(tm)
			}
		}

		pinnedDocument, err := loadBoardCardPinnedDocument(ctx, opts, card)
		if err != nil {
			if errors.Is(err, primitives.ErrNotFound) {
				warnings = append(warnings, map[string]any{"thread_id": threadID, "message": "card pinned document is no longer available"})
				pinnedDocument = nil
			} else {
				return nil, nil, err
			}
		}

		summary := boardCardDerivedSummary(card, threadID, states, time.Now().UTC())
		freshness := boardCardDerivedFreshness(threadID, states)
		pubCard := publicCardView(card)
		derived := map[string]any{
			"summary":   summary,
			"freshness": freshness,
		}
		if threadID != "" {
			derived["timeline_message_count"] = msgCounts[threadID]
		}
		items = append(items, map[string]any{
			"board_ref":            "board:" + anyString(board["id"]),
			"card":                 pubCard,
			"summary":              summary,
			"projection_freshness": freshness,
			"membership":           pubCard,
			"backing": map[string]any{
				"thread_id":           nullableStringValue(threadID),
				"thread":              thread,
				"pinned_document_ref": nullableTypedRef("document", pinnedDocumentIDFromCard(card)),
				"pinned_document":     pinnedDocument,
			},
			"derived": derived,
		})
	}

	return map[string]any{"items": items, "count": len(items)}, warnings, nil
}

func buildBoardWorkspaceDocumentsSection(ctx context.Context, opts handlerOptions, board map[string]any, threadIDs []string) (map[string]any, error) {
	seen := map[string]map[string]any{}
	for _, documentRef := range boardDocumentRefs(board) {
		documentID := strings.TrimPrefix(documentRef, "document:")
		if documentID == "" {
			continue
		}
		document, _, err := opts.primitiveStore.GetDocument(ctx, documentID)
		if err != nil {
			if errors.Is(err, primitives.ErrNotFound) {
				continue
			}
			return nil, err
		}
		seen[documentID] = document
	}
	for _, threadID := range threadIDs {
		documents, err := buildThreadContextDocuments(ctx, opts, threadID)
		if err != nil {
			return nil, err
		}
		for _, document := range documents {
			documentID := strings.TrimSpace(anyString(document["id"]))
			if documentID == "" {
				continue
			}
			seen[documentID] = document
		}
	}

	items := mapValues(seen)
	sort.SliceStable(items, func(i int, j int) bool {
		leftUpdated := strings.TrimSpace(anyString(items[i]["updated_at"]))
		rightUpdated := strings.TrimSpace(anyString(items[j]["updated_at"]))
		if leftUpdated != rightUpdated {
			return leftUpdated > rightUpdated
		}
		return strings.TrimSpace(anyString(items[i]["id"])) < strings.TrimSpace(anyString(items[j]["id"]))
	})
	return map[string]any{"items": items, "count": len(items)}, nil
}

func buildBoardWorkspaceInboxSection(ctx context.Context, opts handlerOptions, threadIDs []string, now time.Time, states map[string]topicProjectionState) (map[string]any, error) {
	items := make([]map[string]any, 0)
	for _, threadID := range threadIDs {
		threadItems, err := opts.primitiveStore.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{ThreadID: threadID})
		if err != nil {
			return nil, err
		}
		for _, item := range threadItems {
			copy := payloadFromDerivedInboxItem(item)
			if strings.TrimSpace(anyString(copy["thread_id"])) == "" {
				copy["thread_id"] = threadID
			}
			items = append(items, copy)
		}
	}

	sort.SliceStable(items, func(i int, j int) bool {
		leftTrigger := strings.TrimSpace(anyString(items[i]["trigger_at"]))
		rightTrigger := strings.TrimSpace(anyString(items[j]["trigger_at"]))
		if leftTrigger != rightTrigger {
			return leftTrigger > rightTrigger
		}
		return strings.TrimSpace(anyString(items[i]["id"])) < strings.TrimSpace(anyString(items[j]["id"]))
	})
	return map[string]any{
		"items":                items,
		"count":                len(items),
		"generated_at":         now.Format(time.RFC3339Nano),
		"projection_freshness": aggregateTopicProjectionFreshness(states, threadIDs),
	}, nil
}

func buildBoardWorkspaceSummary(board map[string]any, cards []map[string]any, states map[string]topicProjectionState, documentsSection map[string]any, now time.Time) map[string]any {
	cardsByColumn := map[string]any{
		"backlog":     0,
		"ready":       0,
		"in_progress": 0,
		"blocked":     0,
		"review":      0,
		"done":        0,
	}

	threadIDs := map[string]struct{}{}
	backingThreadID := strings.TrimSpace(anyString(board["thread_id"]))
	if backingThreadID != "" {
		threadIDs[backingThreadID] = struct{}{}
	}
	for _, threadRef := range boardThreadRefs(board) {
		_, threadID, err := schema.SplitTypedRef(threadRef)
		if err != nil || threadID == "" {
			continue
		}
		threadIDs[threadID] = struct{}{}
	}
	for _, card := range cards {
		columnKey := strings.TrimSpace(anyString(card["column_key"]))
		if _, ok := cardsByColumn[columnKey]; ok {
			cardsByColumn[columnKey] = workspaceIntValue(cardsByColumn[columnKey]) + 1
		}
		threadID := strings.TrimSpace(anyString(card["thread_id"]))
		if threadID != "" {
			threadIDs[threadID] = struct{}{}
		}
	}

	documentCount := workspaceIntValue(documentsSection["count"])
	latestActivityAt := strings.TrimSpace(anyString(board["updated_at"]))
	unresolvedCardCount := 0
	resolvedCardCount := 0
	atRiskCardCount := 0
	dueSoonCardCount := 0
	overdueCardCount := 0
	blockedCardCount := 0
	staleCardCount := 0
	for threadID := range threadIDs {
		projection := states[threadID].Projection
		latestActivityAt = laterTimestamp(latestActivityAt, projection.LastActivityAt)
	}
	for _, card := range cards {
		if !boardCardCountsAsOpenWorkItem(card) {
			resolvedCardCount++
		} else {
			unresolvedCardCount++
		}
		riskState, _, _ := boardCardRiskState(card, now, defaultInboxRiskHorizon)
		switch riskState {
		case "overdue":
			atRiskCardCount++
			overdueCardCount++
		case "due_soon":
			atRiskCardCount++
			dueSoonCardCount++
		case "blocked":
			atRiskCardCount++
			blockedCardCount++
		}
		threadID := strings.TrimSpace(anyString(card["thread_id"]))
		if state, ok := states[threadID]; ok && state.Projection.Stale && boardCardCountsAsOpenWorkItem(card) {
			staleCardCount++
		}
		if updatedAt := strings.TrimSpace(anyString(card["updated_at"])); updatedAt != "" {
			latestActivityAt = laterTimestamp(latestActivityAt, updatedAt)
		}
	}

	return map[string]any{
		"card_count":            len(cards),
		"cards_by_column":       cardsByColumn,
		"unresolved_card_count": unresolvedCardCount,
		"resolved_card_count":   resolvedCardCount,
		"at_risk_card_count":    atRiskCardCount,
		"due_soon_card_count":   dueSoonCardCount,
		"overdue_card_count":    overdueCardCount,
		"blocked_card_count":    blockedCardCount,
		"stale_card_count":      staleCardCount,
		"document_count":        documentCount,
		"latest_activity_at":    nullableStringValue(latestActivityAt),
		"has_document_refs":     len(boardDocumentRefs(board)) > 0,
		"thread_id":             nullableStringValue(backingThreadID),
		"document_refs":         boardDocumentRefs(board),
	}
}

func collectBoardWorkspaceThreadIDs(primaryThreadID string, board map[string]any, cards []map[string]any) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(cards)+1)
	primaryThreadID = strings.TrimSpace(primaryThreadID)
	if primaryThreadID != "" {
		seen[primaryThreadID] = struct{}{}
		out = append(out, primaryThreadID)
	}
	for _, threadRef := range boardThreadRefs(board) {
		_, threadID, err := schema.SplitTypedRef(threadRef)
		if err != nil || threadID == "" {
			continue
		}
		if _, ok := seen[threadID]; ok {
			continue
		}
		seen[threadID] = struct{}{}
		out = append(out, threadID)
	}
	for _, card := range cards {
		threadID := strings.TrimSpace(anyString(card["thread_id"]))
		if threadID == "" {
			continue
		}
		if _, ok := seen[threadID]; ok {
			continue
		}
		seen[threadID] = struct{}{}
		out = append(out, threadID)
	}
	return out
}
