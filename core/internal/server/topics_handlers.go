// HTTP handlers for topic CRUD and lifecycle (list, create, get, patch,
// archive/unarchive/trash/restore), topic write validation, and topic field
// extraction. The timeline and workspace composite views live in
// topic_workspace_handlers.go.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

type topicResourceBundle struct {
	CardsTruncated      bool
	ResourcesTruncated  bool
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
	states, parseErr := ParseListLifecycleStates(query)
	if parseErr != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", parseErr.Error())
		return
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
		States: states,
		Query:  strings.TrimSpace(query.Get("q")),
		Limit:  limitFilter,
		Cursor: strings.TrimSpace(query.Get("cursor")),
	})
	if err != nil {
		if errors.Is(err, primitives.ErrInvalidCursor) {
			writeError(w, http.StatusBadRequest, "invalid_request", "cursor is invalid")
			return
		}
		if topicListDebugErrors() {
			writeError(w, http.StatusInternalServerError, "internal_error", fmt.Sprintf("failed to list topics: %v", err))
			return
		}
		writeReadInternalError(w, r, err, "failed to list topics")
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
		req.Topic["id"] = deriveRequestScopedID("topics.create", actorID, req.RequestKey, "tp")
	}
	var hygiene markdownHygieneCollector
	if !hygiene.normalizeMapString(w, "topic.summary", req.Topic, "summary") {
		return
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
		if writePrimitiveQuotaViolationError(w, err) {
			return
		}
		if errors.Is(err, primitives.ErrConflict) && strings.TrimSpace(req.RequestKey) != "" {
			topicID := firstNonEmptyString(req.Topic["id"])
			if topicID != "" {
				existing, loadErr := opts.primitiveStore.GetTopic(r.Context(), topicID)
				if loadErr == nil {
					response := hygiene.attach(map[string]any{"topic": existing})
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
		notifyProjectionMaintainer(opts)
	}

	status, payload, err := persistIdempotencyReplay(r.Context(), opts.primitiveStore, "topics.create", actorID, req.RequestKey, req, http.StatusCreated, hygiene.attach(map[string]any{
		"topic": result.Topic,
	}))
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
	var ok bool
	topicID, ok = resolveHTTPResourceID(w, r, opts, "topic", topicID, "topic")
	if !ok {
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
	var ok bool
	topicID, ok = resolveHTTPResourceID(w, r, opts, "topic", topicID, "topic")
	if !ok {
		return
	}

	var req struct {
		ActorID     string         `json:"actor_id"`
		RequestKey  string         `json:"request_key"`
		Patch       map[string]any `json:"patch"`
		IfUpdatedAt *string        `json:"if_updated_at"`
	}
	if !decodeJSONBodyPreserveNumbers(w, r, &req) {
		return
	}
	// Preserve the complete decoded input before timestamp/markdown normalization.
	originalBody, err := json.Marshal(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid topic patch")
		return
	}
	if req.Patch == nil || len(req.Patch) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "patch is required")
		return
	}
	if strings.TrimSpace(req.RequestKey) != "" && req.IfUpdatedAt == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "if_updated_at is required for keyed topic patches")
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
	var hygiene markdownHygieneCollector
	if !hygiene.normalizeMapString(w, "patch.summary", req.Patch, "summary") {
		return
	}

	if err := validateTopicWriteInput(opts.contract, req.Patch, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	// Current visibility is required even when recovering an original receipt.
	if _, err := opts.primitiveStore.GetTopic(r.Context(), topicID); err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "topic not found")
		} else {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to load topic")
		}
		return
	}
	status, response, err := executeAtomicMutation(r.Context(), opts.primitiveStore, "topics.patch", actorID, req.RequestKey, struct {
		ID   string
		Body json.RawMessage
	}{topicID, originalBody}, http.StatusOK, func(ctx context.Context) (map[string]any, error) {
		result, err := opts.primitiveStore.PatchTopic(ctx, actorID, topicID, req.Patch, req.IfUpdatedAt)
		if err != nil {
			return nil, err
		}
		return hygiene.attach(map[string]any{"topic": result.Topic}), nil
	})
	if err != nil {
		if writePrimitiveQuotaViolationError(w, err) {
			return
		}
		switch {
		case errors.Is(err, primitives.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "topic not found")
		case errors.Is(err, primitives.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "topic has been updated; refresh and retry")
		case errors.Is(err, primitives.ErrInvalidTopicRequest), errors.Is(err, primitives.ErrInvalidWorkRequest):
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to patch topic")
		}
		return
	}

	notifyProjectionMaintainer(opts)
	writeJSON(w, status, response)
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
	var ok bool
	topicID, ok = resolveHTTPResourceID(w, r, opts, "topic", topicID, "topic")
	if !ok {
		return
	}

	var req struct {
		ActorID     string  `json:"actor_id"`
		Reason      string  `json:"reason"`
		IfUpdatedAt *string `json:"if_updated_at"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if action == "archive" && req.IfUpdatedAt != nil {
		normalized, ok := normalizeRequiredTimestamp(w, req.IfUpdatedAt, "if_updated_at")
		if !ok {
			return
		}
		req.IfUpdatedAt = &normalized
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
		topic, err = opts.primitiveStore.ArchiveTopicIfUpdatedAt(r.Context(), actorID, topicID, req.IfUpdatedAt)
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
		notifyProjectionMaintainer(opts)
	}

	writeJSON(w, http.StatusOK, map[string]any{"topic": topic})
}

func writeTopicLifecycleStoreError(w http.ResponseWriter, err error) bool {
	if writePrimitiveQuotaViolationError(w, err) {
		return true
	}
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
		for _, field := range []string{"title", "summary", "owner_refs", "document_refs", "board_refs", "related_refs", "provenance"} {
			if _, exists := topic[field]; !exists {
				return fmt.Errorf("topic.%s is required", field)
			}
		}
	}

	if _, exists := topic["status"]; exists {
		return fmt.Errorf("topic.status is not supported; topic.state is derived from lifecycle timestamps")
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

func topicListDebugErrors() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ANX_DEBUG_TOPIC_LIST_ERRORS"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
