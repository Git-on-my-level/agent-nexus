package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"organization-autorunner-core/internal/primitives"
	"organization-autorunner-core/internal/schema"
)

type topicResourceBundle struct {
	PrimaryThread       map[string]any
	Boards              []map[string]any
	Cards               []map[string]any
	Documents           []map[string]any
	Threads             []map[string]any
	ProjectionFreshness map[string]any
	Inbox               []map[string]any
}

func handleListTopics(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	query := r.URL.Query()
	topicType := strings.TrimSpace(query.Get("type"))
	if topicType != "" && opts.contract != nil {
		if err := schema.ValidateEnum(opts.contract, "topic_type", topicType); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
	}
	status := strings.TrimSpace(query.Get("status"))
	if status != "" && opts.contract != nil {
		if err := schema.ValidateEnum(opts.contract, "topic_status", status); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
	}

	var limitFilter *int
	limitRaw := strings.TrimSpace(query.Get("limit"))
	if limitRaw != "" {
		parsed, err := strconv.Atoi(limitRaw)
		if err != nil || parsed < 1 || parsed > 1000 {
			writeError(w, http.StatusBadRequest, "invalid_request", "limit must be between 1 and 1000")
			return
		}
		limitFilter = &parsed
	}

	topics, nextCursor, err := opts.primitiveStore.ListTopics(r.Context(), primitives.TopicListFilter{
		Type:            topicType,
		Status:          status,
		Query:           strings.TrimSpace(query.Get("q")),
		Limit:           limitFilter,
		Cursor:          strings.TrimSpace(query.Get("cursor")),
		IncludeArchived: strings.TrimSpace(query.Get("include_archived")) == "true",
		ArchivedOnly:    strings.TrimSpace(query.Get("archived_only")) == "true",
		IncludeTrashed:  strings.TrimSpace(query.Get("include_trashed")) == "true",
		TrashedOnly:     strings.TrimSpace(query.Get("trashed_only")) == "true",
	})
	if err != nil {
		if errors.Is(err, primitives.ErrInvalidCursor) {
			writeError(w, http.StatusBadRequest, "invalid_request", "cursor is invalid")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list topics")
		return
	}

	response := map[string]any{"topics": topics}
	if nextCursor != "" {
		response["next_cursor"] = nextCursor
	}
	writeJSON(w, http.StatusOK, response)
}

func handleCreateTopic(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	if opts.contract == nil {
		writeError(w, http.StatusServiceUnavailable, "schema_unavailable", "schema contract is not configured")
		return
	}

	var req struct {
		ActorID    string         `json:"actor_id"`
		RequestKey string         `json:"request_key"`
		Topic      map[string]any `json:"topic"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if req.Topic == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "topic is required")
		return
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}
	if strings.TrimSpace(req.RequestKey) != "" && firstNonEmptyString(req.Topic["id"]) == "" {
		req.Topic["id"] = deriveRequestScopedID("topics.create", actorID, req.RequestKey, "topic")
	}

	replayStatus, replayPayload, replayed, err := readIdempotencyReplay(r.Context(), opts.primitiveStore, "topics.create", actorID, req.RequestKey, req)
	if writeIdempotencyError(w, err) {
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load idempotency replay")
		return
	}
	if replayed {
		writeJSON(w, replayStatus, replayPayload)
		return
	}

	if err := validateTopicWriteInput(opts.contract, req.Topic, true); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	result, err := opts.primitiveStore.CreateTopic(r.Context(), actorID, req.Topic)
	if err != nil {
		if errors.Is(err, primitives.ErrConflict) && strings.TrimSpace(req.RequestKey) != "" {
			topicID := firstNonEmptyString(req.Topic["id"])
			if topicID != "" {
				existing, loadErr := opts.primitiveStore.GetTopic(r.Context(), topicID)
				if loadErr == nil {
					response := map[string]any{"topic": existing}
					status, payload, replayErr := persistIdempotencyReplay(r.Context(), opts.primitiveStore, "topics.create", actorID, req.RequestKey, req, http.StatusCreated, response)
					if writeIdempotencyError(w, replayErr) {
						return
					}
					if replayErr != nil {
						writeError(w, http.StatusInternalServerError, "internal_error", "failed to persist idempotency replay")
						return
					}
					writeJSON(w, status, payload)
					return
				}
			}
		}
		if errors.Is(err, primitives.ErrInvalidTopicRequest) {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if errors.Is(err, primitives.ErrConflict) {
			writeError(w, http.StatusConflict, "conflict", "topic already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create topic")
		return
	}

	primaryThreadID := topicPrimaryThreadID(result.Topic)
	if primaryThreadID != "" {
		enqueueTopicProjectionsBestEffort(r.Context(), opts, []string{primaryThreadID}, time.Now().UTC())
	}

	status, payload, err := persistIdempotencyReplay(r.Context(), opts.primitiveStore, "topics.create", actorID, req.RequestKey, req, http.StatusCreated, map[string]any{
		"topic": result.Topic,
	})
	if writeIdempotencyError(w, err) {
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to persist idempotency replay")
		return
	}
	writeJSON(w, status, payload)
}

func handleGetTopic(w http.ResponseWriter, r *http.Request, opts handlerOptions, topicID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	topic, err := opts.primitiveStore.GetTopic(r.Context(), topicID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "topic not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load topic")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"topic": topic})
}

func handlePatchTopic(w http.ResponseWriter, r *http.Request, opts handlerOptions, topicID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	if opts.contract == nil {
		writeError(w, http.StatusServiceUnavailable, "schema_unavailable", "schema contract is not configured")
		return
	}

	var req struct {
		ActorID     string         `json:"actor_id"`
		Patch       map[string]any `json:"patch"`
		IfUpdatedAt *string        `json:"if_updated_at"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if req.Patch == nil || len(req.Patch) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "patch is required")
		return
	}
	if req.IfUpdatedAt != nil {
		ifUpdatedAt, ok := normalizeRequiredTimestamp(w, req.IfUpdatedAt, "if_updated_at")
		if !ok {
			return
		}
		req.IfUpdatedAt = &ifUpdatedAt
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}

	if err := validateTopicWriteInput(opts.contract, req.Patch, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	result, err := opts.primitiveStore.PatchTopic(r.Context(), actorID, topicID, req.Patch, req.IfUpdatedAt)
	if err != nil {
		switch {
		case errors.Is(err, primitives.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "topic not found")
		case errors.Is(err, primitives.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "topic has been updated; refresh and retry")
		case errors.Is(err, primitives.ErrInvalidTopicRequest):
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to patch topic")
		}
		return
	}

	if primaryThreadID := topicPrimaryThreadID(result.Topic); primaryThreadID != "" {
		enqueueTopicProjectionsBestEffort(r.Context(), opts, []string{primaryThreadID}, time.Now().UTC())
	}

	writeJSON(w, http.StatusOK, map[string]any{"topic": result.Topic})
}

func handleArchiveTopic(w http.ResponseWriter, r *http.Request, opts handlerOptions, topicID string) {
	handleTopicLifecycle(w, r, opts, topicID, "archive")
}

func handleUnarchiveTopic(w http.ResponseWriter, r *http.Request, opts handlerOptions, topicID string) {
	handleTopicLifecycle(w, r, opts, topicID, "unarchive")
}

func handleTrashTopic(w http.ResponseWriter, r *http.Request, opts handlerOptions, topicID string) {
	handleTopicLifecycleWithReason(w, r, opts, topicID, "trash")
}

func handleRestoreTopic(w http.ResponseWriter, r *http.Request, opts handlerOptions, topicID string) {
	handleTopicLifecycle(w, r, opts, topicID, "restore")
}

func handleTopicLifecycle(w http.ResponseWriter, r *http.Request, opts handlerOptions, topicID, action string) {
	handleTopicLifecycleWithReason(w, r, opts, topicID, action)
}

func handleTopicLifecycleWithReason(w http.ResponseWriter, r *http.Request, opts handlerOptions, topicID, action string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	var req struct {
		ActorID string `json:"actor_id"`
		Reason  string `json:"reason"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}

	var (
		topic map[string]any
		err   error
	)
	switch action {
	case "archive":
		topic, err = opts.primitiveStore.ArchiveTopic(r.Context(), actorID, topicID)
	case "unarchive":
		topic, err = opts.primitiveStore.UnarchiveTopic(r.Context(), actorID, topicID)
	case "trash":
		topic, err = opts.primitiveStore.TrashTopic(r.Context(), actorID, topicID, req.Reason)
	case "restore":
		topic, err = opts.primitiveStore.RestoreTopic(r.Context(), actorID, topicID)
	default:
		writeError(w, http.StatusBadRequest, "invalid_request", "unsupported topic lifecycle action")
		return
	}
	if err != nil {
		if writeTopicLifecycleStoreError(w, err) {
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to update topic")
		return
	}

	if primaryThreadID := topicPrimaryThreadID(topic); primaryThreadID != "" {
		enqueueTopicProjectionsBestEffort(r.Context(), opts, []string{primaryThreadID}, time.Now().UTC())
	}

	writeJSON(w, http.StatusOK, map[string]any{"topic": topic})
}

func handleGetTopicTimeline(w http.ResponseWriter, r *http.Request, opts handlerOptions, topicID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
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
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
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

	return map[string]any{
		"topic":     topic,
		"events":    events,
		"artifacts": artifacts,
		"cards":     bundle.Cards,
		"documents": bundle.Documents,
		"threads":   bundle.Threads,
	}, nil
}

func buildTopicWorkspacePayload(ctx context.Context, opts handlerOptions, topicID string) (map[string]any, error) {
	topic, err := opts.primitiveStore.GetTopic(ctx, topicID)
	if err != nil {
		return nil, err
	}

	bundle, err := buildTopicResourceBundle(ctx, opts, topic)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"topic":                topic,
		"cards":                bundle.Cards,
		"boards":               bundle.Boards,
		"documents":            bundle.Documents,
		"threads":              bundle.Threads,
		"inbox":                bundle.Inbox,
		"projection_freshness": bundle.ProjectionFreshness,
		"generated_at":         time.Now().UTC().Format(time.RFC3339Nano),
	}, nil
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

	projectionState, err := loadTopicProjectionState(ctx, opts, primaryThreadID)
	if err != nil {
		return topicResourceBundle{}, err
	}

	refEdges, err := opts.primitiveStore.ListRefEdgesBySource(ctx, "topic", anyString(topic["id"]))
	if err != nil {
		return topicResourceBundle{}, err
	}

	boardIDs := collectTopicRefEdgeTargetIDs(refEdges, "board")
	documentIDs := collectTopicRefEdgeTargetIDs(refEdges, "document")
	threadIDs := append([]string{primaryThreadID}, collectTopicRefEdgeTargetIDs(refEdges, "thread")...)

	boardMemberships, err := opts.primitiveStore.ListBoardMembershipsByThread(ctx, primaryThreadID)
	if err != nil {
		return topicResourceBundle{}, err
	}
	for _, membership := range boardMemberships {
		boardIDs = append(boardIDs, anyString(membership.Board["id"]))
		threadIDs = append(threadIDs, strings.TrimSpace(anyString(membership.Card["thread_id"])))
		if pinnedDocumentID := strings.TrimSpace(anyString(membership.Card["pinned_document_id"])); pinnedDocumentID != "" {
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
			boards = append(boards, board)
			documentIDs = append(documentIDs, topicBoardDocumentIDs(board)...)
		} else if !errors.Is(err, primitives.ErrNotFound) {
			return topicResourceBundle{}, err
		}

		cards, err := opts.primitiveStore.ListBoardCards(ctx, boardID)
		if err == nil {
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
			documents = append(documents, document)
		} else if !errors.Is(err, primitives.ErrNotFound) {
			return topicResourceBundle{}, err
		}
	}

	for _, document := range documents {
		if threadID := strings.TrimSpace(anyString(document["thread_id"])); threadID != "" {
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
			threads = append(threads, thread)
		} else if !errors.Is(err, primitives.ErrNotFound) {
			return topicResourceBundle{}, err
		}
	}

	cards := make([]map[string]any, 0)
	for _, boardCardGroups := range cardsByBoard {
		for _, boardCards := range boardCardGroups {
			for _, card := range boardCards {
				cards = append(cards, publicCardView(card))
			}
		}
	}

	inboxItems, err := opts.primitiveStore.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{ThreadID: primaryThreadID})
	if err != nil {
		return topicResourceBundle{}, err
	}
	inbox := make([]map[string]any, 0, len(inboxItems))
	for _, item := range inboxItems {
		inbox = append(inbox, payloadFromDerivedInboxItem(item))
	}

	return topicResourceBundle{
		PrimaryThread:       primaryThread,
		Boards:              dedupeAndSortResourceMaps(boards),
		Cards:               dedupeAndSortResourceMaps(cards),
		Documents:           dedupeAndSortResourceMaps(documents),
		Threads:             dedupeAndSortResourceMaps(threads),
		ProjectionFreshness: cloneWorkspaceMap(projectionState.Freshness),
		Inbox:               inbox,
	}, nil
}

func collectTopicRefEdgeTargetIDs(edges []primitives.RefEdge, targetType string) []string {
	out := make([]string, 0, len(edges))
	for _, edge := range edges {
		if strings.TrimSpace(edge.EdgeType) != "ref" || strings.TrimSpace(edge.TargetType) != strings.TrimSpace(targetType) {
			continue
		}
		if targetID := strings.TrimSpace(edge.TargetID); targetID != "" {
			out = append(out, targetID)
		}
	}
	return uniqueTopicIDs(out)
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

func writeTopicLifecycleStoreError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, primitives.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "topic not found")
		return true
	case errors.Is(err, primitives.ErrNotArchived):
		writeError(w, http.StatusConflict, "not_archived", "topic is not archived")
		return true
	case errors.Is(err, primitives.ErrNotTrashed):
		writeError(w, http.StatusConflict, "not_trashed", "topic is not trashed")
		return true
	case errors.Is(err, primitives.ErrAlreadyTrashed):
		writeError(w, http.StatusConflict, "already_trashed", "topic is trashed")
		return true
	case errors.Is(err, primitives.ErrInvalidTopicRequest):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return true
	default:
		return false
	}
}

func validateTopicWriteInput(contract *schema.Contract, topic map[string]any, createMode bool) error {
	if contract == nil {
		return fmt.Errorf("schema contract is required")
	}
	if topic == nil {
		return fmt.Errorf("topic is required")
	}

	if createMode {
		for _, field := range []string{"type", "status", "title", "summary", "owner_refs", "document_refs", "board_refs", "related_refs", "provenance"} {
			if _, exists := topic[field]; !exists {
				return fmt.Errorf("topic.%s is required", field)
			}
		}
	}

	if rawType, exists := topic["type"]; exists || createMode {
		topicType := strings.TrimSpace(anyString(rawType))
		if topicType == "" && createMode {
			return fmt.Errorf("topic.type is required")
		}
		if topicType != "" {
			if err := schema.ValidateEnum(contract, "topic_type", topicType); err != nil {
				return fmt.Errorf("topic.type: %w", err)
			}
		}
	}

	if rawStatus, exists := topic["status"]; exists || createMode {
		topicStatus := strings.TrimSpace(anyString(rawStatus))
		if topicStatus == "" && createMode {
			return fmt.Errorf("topic.status is required")
		}
		if topicStatus != "" {
			if err := schema.ValidateEnum(contract, "topic_status", topicStatus); err != nil {
				return fmt.Errorf("topic.status: %w", err)
			}
		}
	}

	if rawTitle, exists := topic["title"]; exists || createMode {
		title := strings.TrimSpace(anyString(rawTitle))
		if title == "" {
			if createMode {
				return fmt.Errorf("topic.title is required")
			}
		}
	}
	if rawSummary, exists := topic["summary"]; exists || createMode {
		summary := strings.TrimSpace(anyString(rawSummary))
		if summary == "" {
			if createMode {
				return fmt.Errorf("topic.summary is required")
			}
		}
	}

	for _, field := range []string{"owner_refs", "document_refs", "board_refs", "related_refs"} {
		raw, exists := topic[field]
		if !exists && !createMode {
			continue
		}
		refs, err := extractStringSlice(raw)
		if err != nil {
			return fmt.Errorf("topic.%s must be a list of strings", field)
		}
		if err := schema.ValidateTypedRefs(contract, refs); err != nil {
			return fmt.Errorf("topic.%s: %w", field, err)
		}
	}

	if raw, exists := topic["thread_id"]; exists && raw != nil {
		tid := strings.TrimSpace(anyString(raw))
		if tid == "" {
			return fmt.Errorf("topic.thread_id must be a non-empty string")
		}
	}

	if raw, exists := topic["provenance"]; exists {
		provenance, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("topic.provenance must be an object")
		}
		if err := schema.ValidateProvenance(contract, provenance); err != nil {
			return fmt.Errorf("topic.provenance: %w", err)
		}
	}

	return nil
}

func topicPrimaryThreadID(topic map[string]any) string {
	if topic == nil {
		return ""
	}
	if id := strings.TrimSpace(anyString(topic["thread_id"])); id != "" {
		return id
	}
	if ref := strings.TrimSpace(anyString(topic["thread_ref"])); strings.HasPrefix(ref, "thread:") {
		return strings.TrimSpace(strings.TrimPrefix(ref, "thread:"))
	}
	return ""
}

func topicRefIDs(topic map[string]any, field string, prefix string) []string {
	if topic == nil {
		return nil
	}
	refs, err := extractStringSlice(topic[field])
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(refs))
	needle := prefix + ":"
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" || !strings.HasPrefix(ref, needle) {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(ref, needle))
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
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
