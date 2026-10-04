package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"agent-nexus-core/internal/primitives"
)

type agentInboxAskStore interface {
	ListHumanAttentionInboxAsksPage(context.Context, string, string, int) (primitives.HumanAttentionInboxPage, error)
	MarkHumanAttentionAnswerRead(context.Context, string, string) (map[string]any, error)
}

func handleListAgentInboxAsks(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	store, ok := opts.primitiveStore.(agentInboxAskStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "agent inbox storage is not configured")
		return
	}
	principal, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}
	if !isAgentPrincipal(principal) {
		writeError(w, http.StatusForbidden, "agent_required", "agent inbox is only available to authenticated agents")
		return
	}
	limit, ok := parseOptionalPositiveInt(w, r.URL.Query().Get("limit"), 100, 200, "limit")
	if !ok {
		return
	}
	page, err := store.ListHumanAttentionInboxAsksPage(r.Context(), principal.ActorID, strings.TrimSpace(r.URL.Query().Get("cursor")), limit)
	if err != nil {
		if errors.Is(err, primitives.ErrInvalidCursor) {
			writeError(w, http.StatusBadRequest, "invalid_request", "cursor is invalid")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list agent inbox asks")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":     page.Items,
		"page_info": map[string]any{"next_cursor": page.NextCursor, "has_more": page.NextCursor != ""},
	})
}

func handleReadAgentInboxAnswer(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	store, ok := opts.primitiveStore.(agentInboxAskStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "agent inbox storage is not configured")
		return
	}
	principal, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}
	if !isAgentPrincipal(principal) {
		writeError(w, http.StatusForbidden, "agent_required", "only authenticated agents can mark their answers read")
		return
	}
	var request struct {
		AnswerEventID string `json:"answer_event_id"`
	}
	if !decodeJSONBody(w, r, &request) {
		return
	}
	request.AnswerEventID = strings.TrimSpace(request.AnswerEventID)
	if request.AnswerEventID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "answer_event_id is required")
		return
	}
	answer, err := store.MarkHumanAttentionAnswerRead(r.Context(), principal.ActorID, request.AnswerEventID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "answer not found for this agent")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to mark agent inbox answer read")
		return
	}
	status := http.StatusCreated
	if answer["already_read"] == true {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"answer": answer})
}
