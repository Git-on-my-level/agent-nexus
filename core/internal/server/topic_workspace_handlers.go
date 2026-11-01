// Composite per-topic views: the timeline endpoint and the workspace view
// with its resource bundle (cards, boards, documents), ref-edge target and
// source collection, and the ID dedupe helpers those views share.

package server

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"agent-nexus-core/internal/primitives"
)

func handleGetTopicTimeline(w http.ResponseWriter, r *http.Request, opts handlerOptions, topicID string) {
	opts.summaryFormat = r.URL.Query().Get("summary") == "1"
	opts.readVisibility = planVisibility(r, opts)
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var ok bool
	topicID, ok = resolveHTTPResourceID(w, r, opts, "topic", topicID, "topic")
	if !ok {
		return
	}

	body, err := buildTopicTimelinePayload(r.Context(), opts, topicID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "topic not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load topic timeline")
		return
	}

	writeJSON(w, http.StatusOK, body)
}

func handleGetTopicWorkspace(w http.ResponseWriter, r *http.Request, opts handlerOptions, topicID string) {
	opts.summaryFormat = r.URL.Query().Get("summary") == "1"
	opts.readVisibility = planVisibility(r, opts)
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var ok bool
	topicID, ok = resolveHTTPResourceID(w, r, opts, "topic", topicID, "topic")
	if !ok {
		return
	}

	body, err := buildTopicWorkspacePayload(r.Context(), opts, topicID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "topic not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load topic workspace")
		return
	}

	writeJSON(w, http.StatusOK, body)
}

func buildTopicTimelinePayload(ctx context.Context, opts handlerOptions, topicID string) (map[string]any, error) {
	topic, err := opts.primitiveStore.GetTopic(ctx, topicID)
	if err != nil {
		return nil, err
	}

	bundle, err := buildTopicResourceBundle(ctx, opts, topic)
	if err != nil {
		return nil, err
	}
	primaryThreadID := topicPrimaryThreadID(topic)

	events, err := opts.primitiveStore.ListEventsByThread(ctx, primaryThreadID)
	if err != nil {
		return nil, err
	}
	artifacts, err := opts.primitiveStore.ListArtifacts(ctx, primitives.ArtifactListFilter{ThreadID: primaryThreadID})
	if err != nil {
		return nil, err
	}
	receipts, err := deriveAgentNotificationReceiptsByEvent(ctx, opts, primaryThreadID, events)
	if err != nil {
		return nil, err
	}

	response := map[string]any{
		"topic":                 topic,
		"events":                events,
		"artifacts":             artifacts,
		"cards":                 bundle.Cards,
		"documents":             bundle.Documents,
		"threads":               bundle.Threads,
		"notification_receipts": receipts,
	}
	if opts.summaryFormat {
		response["cards_truncated"] = bundle.CardsTruncated
	}
	return response, nil
}

func buildTopicWorkspacePayload(ctx context.Context, opts handlerOptions, topicID string) (map[string]any, error) {
	topic, err := opts.primitiveStore.GetTopic(ctx, topicID)
	if err != nil {
		return nil, err
	}

	var bundle topicResourceBundle
	if canonical, ok := opts.primitiveStore.(*primitives.Store); ok {
		var page primitives.TopicWorkspacePage
		primary := topicPrimaryThreadID(topic)
		page, err = canonical.TopicWorkspaceResources(ctx, anyString(topic["id"]), primary)
		if err == nil {
			bundle = topicResourceBundle{Boards: page.Boards, Cards: page.Cards, Documents: page.Documents, Threads: page.Threads, CardsTruncated: page.CardsTruncated, ResourcesTruncated: page.Truncated}
			var state topicProjectionState
			state, err = loadTopicProjectionState(ctx, opts, primary)
			bundle.ProjectionFreshness = state.Freshness
			if err == nil {
				var inbox []primitives.DerivedInboxItem
				inbox, err = canonical.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{ThreadID: primary, Limit: 51})
				if len(inbox) > 50 {
					inbox = inbox[:50]
					bundle.ResourcesTruncated = true
				}
				bundle.Inbox = []map[string]any{}
				for _, item := range inbox {
					bundle.Inbox = append(bundle.Inbox, payloadFromDerivedInboxItem(item))
				}
			}
			if opts.summaryFormat && err == nil {
				err = canonical.EnrichCardPlans(ctx, bundle.Cards, opts.readVisibility, time.Now().UTC())
			}
			for _, thread := range bundle.Threads {
				primitives.StripThreadPlanningFieldsForAPI(thread)
			}
			bundle.Cards = publicCardsView(bundle.Cards)
		}
	} else {
		bundle, err = buildTopicResourceBundle(ctx, opts, topic)
	}
	if err != nil {
		return nil, err
	}

	response := map[string]any{
		"topic":                topic,
		"cards":                bundle.Cards,
		"boards":               bundle.Boards,
		"documents":            bundle.Documents,
		"threads":              bundle.Threads,
		"inbox":                bundle.Inbox,
		"projection_freshness": bundle.ProjectionFreshness,
		"generated_at":         time.Now().UTC().Format(time.RFC3339Nano),
		"cards_truncated":      bundle.CardsTruncated, "resources_truncated": bundle.ResourcesTruncated,
	}
	if opts.summaryFormat {
		response["cards_truncated"] = bundle.CardsTruncated
	}
	return response, nil
}

func buildTopicResourceBundle(ctx context.Context, opts handlerOptions, topic map[string]any) (topicResourceBundle, error) {
	primaryThreadID := topicPrimaryThreadID(topic)
	if primaryThreadID == "" {
		return topicResourceBundle{}, primitives.ErrNotFound
	}

	primaryThread, err := opts.primitiveStore.GetThread(ctx, primaryThreadID)
	if err != nil {
		return topicResourceBundle{}, err
	}

	if !scopedThreadVisible(ctx, opts, primaryThreadID) {
		return topicResourceBundle{}, primitives.ErrNotFound
	}
	projectionState, err := loadTopicProjectionState(ctx, opts, primaryThreadID)
	if err != nil {
		return topicResourceBundle{}, err
	}

	refEdges, err := opts.primitiveStore.ListRefEdgesBySource(ctx, "topic:"+anyString(topic["id"]), "ref")
	if err != nil {
		return topicResourceBundle{}, err
	}
	reverseRefEdges, err := opts.primitiveStore.ListRefEdgesByTarget(ctx, "topic:"+anyString(topic["id"]), "ref")
	if err != nil {
		return topicResourceBundle{}, err
	}

	boardIDs := collectTopicRefEdgeTargetIDs(ctx, opts, refEdges, "board")
	documentIDs := collectTopicRefEdgeTargetIDs(ctx, opts, refEdges, "document")
	threadIDs := append([]string{primaryThreadID}, collectTopicRefEdgeTargetIDs(ctx, opts, refEdges, "thread")...)
	reverseIDs := collectTopicReverseRefEdgeSourceIDs(ctx, opts, reverseRefEdges)
	boardIDs = append(boardIDs, reverseIDs.Boards...)
	documentIDs = append(documentIDs, reverseIDs.Documents...)
	threadIDs = append(threadIDs, reverseIDs.Threads...)
	for _, cardID := range reverseIDs.Cards {
		card, cardErr := opts.primitiveStore.GetBoardCard(ctx, "", cardID)
		if cardErr == nil {
			visible, err := scopedCards(ctx, opts, []map[string]any{card})
			if err != nil {
				return topicResourceBundle{}, err
			}
			if len(visible) == 0 {
				continue
			}
			if boardID := strings.TrimSpace(anyString(card["board_id"])); boardID != "" {
				boardIDs = append(boardIDs, boardID)
			}
			if threadID := strings.TrimSpace(anyString(card["thread_id"])); threadID != "" {
				threadIDs = append(threadIDs, threadID)
			}
			if pinnedDocumentID := pinnedDocumentIDFromCard(card); pinnedDocumentID != "" {
				documentIDs = append(documentIDs, pinnedDocumentID)
			}
		} else if !errors.Is(cardErr, primitives.ErrNotFound) {
			return topicResourceBundle{}, cardErr
		}
	}

	boardMemberships, err := opts.primitiveStore.ListBoardMembershipsByThread(ctx, primaryThreadID)
	if err != nil {
		return topicResourceBundle{}, err
	}
	for _, membership := range boardMemberships {
		visible, err := scopedCards(ctx, opts, []map[string]any{membership.Card})
		if err != nil {
			return topicResourceBundle{}, err
		}
		if len(visible) == 0 {
			continue
		}
		boardIDs = append(boardIDs, anyString(membership.Board["id"]))
		threadIDs = append(threadIDs, strings.TrimSpace(primaryRelatedThreadID(membership.Card)))
		if pinnedDocumentID := pinnedDocumentIDFromCard(membership.Card); pinnedDocumentID != "" {
			documentIDs = append(documentIDs, pinnedDocumentID)
		}
	}
	boardIDs = uniqueTopicIDs(boardIDs)

	boards := make([]map[string]any, 0, len(boardIDs))
	cardsByBoard := make(map[string][][]map[string]any, len(boardIDs))
	for _, boardID := range boardIDs {
		boardID = strings.TrimSpace(boardID)
		if boardID == "" {
			continue
		}
		board, err := opts.primitiveStore.GetBoard(ctx, boardID)
		if err == nil {
			if !scopedThreadVisible(ctx, opts, anyString(board["thread_id"])) {
				continue
			}
			boards = append(boards, board)
			documentIDs = append(documentIDs, topicBoardDocumentIDs(board)...)
		} else if !errors.Is(err, primitives.ErrNotFound) {
			return topicResourceBundle{}, err
		}

		cards, err := opts.primitiveStore.ListBoardCards(ctx, boardID)
		if err == nil {
			cards, err = scopedCards(ctx, opts, cards)
			if err != nil {
				return topicResourceBundle{}, err
			}
			cardsByBoard[boardID] = append(cardsByBoard[boardID], cards)
			for _, card := range cards {
				threadIDs = append(threadIDs, strings.TrimSpace(anyString(card["thread_id"])))
				if pinnedDocumentID := pinnedDocumentIDFromCard(card); pinnedDocumentID != "" {
					documentIDs = append(documentIDs, pinnedDocumentID)
				}
			}
		} else if !errors.Is(err, primitives.ErrNotFound) {
			return topicResourceBundle{}, err
		}
	}

	threadIDs = uniqueTopicIDs(threadIDs)
	for _, threadID := range threadIDs {
		if !scopedThreadVisible(ctx, opts, threadID) {
			continue
		}
		documentIDs = append(documentIDs, threadDocumentIDs(ctx, opts, threadID)...)
	}
	documentIDs = uniqueTopicIDs(documentIDs)

	documents := make([]map[string]any, 0, len(documentIDs))
	for _, documentID := range documentIDs {
		documentID = strings.TrimSpace(documentID)
		if documentID == "" {
			continue
		}
		document, _, err := opts.primitiveStore.GetDocument(ctx, documentID)
		if err == nil {
			if !scopedThreadVisible(ctx, opts, documentBackingThreadID(document)) {
				continue
			}
			documents = append(documents, document)
		} else if !errors.Is(err, primitives.ErrNotFound) {
			return topicResourceBundle{}, err
		}
	}

	for _, document := range documents {
		if threadID := documentBackingThreadID(document); threadID != "" {
			threadIDs = append(threadIDs, threadID)
		}
	}
	for _, boardCardGroups := range cardsByBoard {
		for _, cards := range boardCardGroups {
			for _, card := range cards {
				if threadID := strings.TrimSpace(anyString(card["thread_id"])); threadID != "" {
					threadIDs = append(threadIDs, threadID)
				}
			}
		}
	}
	threadIDs = uniqueTopicIDs(threadIDs)

	threads := make([]map[string]any, 0, len(threadIDs))
	for _, threadID := range threadIDs {
		threadID = strings.TrimSpace(threadID)
		if threadID == "" {
			continue
		}
		thread, err := opts.primitiveStore.GetThread(ctx, threadID)
		if err == nil {
			if !scopedThreadVisible(ctx, opts, threadID) {
				continue
			}
			threads = append(threads, thread)
		} else if !errors.Is(err, primitives.ErrNotFound) {
			return topicResourceBundle{}, err
		}
	}

	cards := make([]map[string]any, 0)
	for _, boardCardGroups := range cardsByBoard {
		for _, boardCards := range boardCardGroups {
			for _, card := range boardCards {
				cards = append(cards, card)
			}
		}
	}

	cards = dedupeAndSortResourceMaps(cards)
	cardsTruncated := false
	if opts.summaryFormat {
		cards, cardsTruncated = summaryCandidates(cards)
	}
	if store, ok := opts.primitiveStore.(planStore); ok && opts.summaryFormat {
		if err := store.EnrichCardPlans(ctx, cards, opts.readVisibility, time.Now().UTC()); err != nil {
			return topicResourceBundle{}, err
		}
		for _, card := range cards {
			card["summary_format"] = true
		}
	}
	cards = publicCardsView(cards)
	inboxItems, err := opts.primitiveStore.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{ThreadID: primaryThreadID})
	if err != nil {
		return topicResourceBundle{}, err
	}
	inbox := make([]map[string]any, 0, len(inboxItems))
	for _, item := range inboxItems {
		inbox = append(inbox, payloadFromDerivedInboxItem(item))
	}

	primitives.StripThreadPlanningFieldsForAPI(primaryThread)
	for i := range threads {
		primitives.StripThreadPlanningFieldsForAPI(threads[i])
	}
	threads = dedupeAndSortResourceMaps(threads)

	return topicResourceBundle{
		CardsTruncated:      cardsTruncated,
		PrimaryThread:       primaryThread,
		Boards:              dedupeAndSortResourceMaps(boards),
		Cards:               dedupeAndSortResourceMaps(cards),
		Documents:           dedupeAndSortResourceMaps(documents),
		Threads:             threads,
		ProjectionFreshness: cloneWorkspaceMap(projectionState.Freshness),
		Inbox:               inbox,
	}, nil
}

func collectTopicRefEdgeTargetIDs(ctx context.Context, opts handlerOptions, edges []primitives.RefEdge, targetType string) []string {
	out := make([]string, 0, len(edges))
	for _, edge := range edges {
		if strings.TrimSpace(edge.Relation) != "ref" {
			continue
		}
		prefix, id, ok := splitTypedRefFromEdge(edge.TargetRef)
		if !ok || strings.TrimSpace(prefix) != strings.TrimSpace(targetType) {
			continue
		}
		if targetID := strings.TrimSpace(id); targetID != "" {
			if resolved, ok := resolveResourceIDForInternalUse(ctx, opts, targetType, edge.TargetRef); ok {
				targetID = resolved
			}
			out = append(out, targetID)
		}
	}
	return uniqueTopicIDs(out)
}

type topicReverseRefSourceIDs struct {
	Boards    []string
	Cards     []string
	Documents []string
	Threads   []string
}

func collectTopicReverseRefEdgeSourceIDs(ctx context.Context, opts handlerOptions, edges []primitives.RefEdge) topicReverseRefSourceIDs {
	var out topicReverseRefSourceIDs
	for _, edge := range edges {
		if strings.TrimSpace(edge.Relation) != "ref" {
			continue
		}
		prefix, id, ok := splitTypedRefFromEdge(edge.SourceRef)
		if !ok {
			continue
		}
		switch strings.TrimSpace(prefix) {
		case "board":
			if resolved, ok := resolveResourceIDForInternalUse(ctx, opts, "board", edge.SourceRef); ok {
				id = resolved
			}
			out.Boards = append(out.Boards, id)
		case "card":
			if resolved, ok := resolveResourceIDForInternalUse(ctx, opts, "card", edge.SourceRef); ok {
				id = resolved
			}
			out.Cards = append(out.Cards, id)
		case "document":
			if resolved, ok := resolveResourceIDForInternalUse(ctx, opts, "document", edge.SourceRef); ok {
				id = resolved
			}
			out.Documents = append(out.Documents, id)
		case "thread":
			if resolved, ok := resolveResourceIDForInternalUse(ctx, opts, "thread", edge.SourceRef); ok {
				id = resolved
			}
			out.Threads = append(out.Threads, id)
		}
	}
	out.Boards = uniqueTopicIDs(out.Boards)
	out.Cards = uniqueTopicIDs(out.Cards)
	out.Documents = uniqueTopicIDs(out.Documents)
	out.Threads = uniqueTopicIDs(out.Threads)
	return out
}

func splitTypedRefFromEdge(ref string) (string, string, bool) {
	idx := strings.Index(ref, ":")
	if idx <= 0 || idx >= len(ref)-1 {
		return "", "", false
	}
	return ref[:idx], ref[idx+1:], true
}

func topicBoardDocumentIDs(board map[string]any) []string {
	refs, err := extractStringSlice(board["document_refs"])
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if strings.HasPrefix(strings.TrimSpace(ref), "document:") {
			out = append(out, strings.TrimSpace(strings.TrimPrefix(ref, "document:")))
		}
	}
	return uniqueTopicIDs(out)
}

func threadDocumentIDs(ctx context.Context, opts handlerOptions, threadID string) []string {
	if opts.primitiveStore == nil {
		return nil
	}
	documents, _, err := opts.primitiveStore.ListDocuments(ctx, primitives.DocumentListFilter{ThreadID: threadID})
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(documents))
	for _, document := range documents {
		if id := strings.TrimSpace(anyString(document["id"])); id != "" {
			out = append(out, id)
		}
	}
	return out
}

func uniqueTopicIDs(values []string) []string {
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
	return out
}

func dedupeAndSortResourceMaps(items []map[string]any) []map[string]any {
	seen := make(map[string]struct{}, len(items))
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		id := strings.TrimSpace(anyString(item["id"]))
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		left := strings.TrimSpace(anyString(out[i]["updated_at"]))
		right := strings.TrimSpace(anyString(out[j]["updated_at"]))
		if left == right {
			return strings.TrimSpace(anyString(out[i]["id"])) < strings.TrimSpace(anyString(out[j]["id"]))
		}
		return left > right
	})
	return out
}
