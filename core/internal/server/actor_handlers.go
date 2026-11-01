// HTTP handlers for the actor registry surface: registering actors
// (development mode only) and listing them.

package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"agent-nexus-core/internal/actors"
)

func handleRegisterActor(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.actorRegistry == nil {
		writeError(w, http.StatusServiceUnavailable, "actor_registry_unavailable", "actor registry is not configured")
		return
	}

	if !opts.enableDevActorMode {
		writeError(w, http.StatusForbidden, "dev_actor_mode_disabled", "actor creation is disabled outside development mode")
		return
	}

	var req struct {
		Actor struct {
			ID          string   `json:"id"`
			DisplayName string   `json:"display_name"`
			Tags        []string `json:"tags"`
			CreatedAt   string   `json:"created_at"`
		} `json:"actor"`
	}

	if !decodeJSONBody(w, r, &req) {
		return
	}

	req.Actor.ID = strings.TrimSpace(req.Actor.ID)
	req.Actor.DisplayName = strings.TrimSpace(req.Actor.DisplayName)
	req.Actor.CreatedAt = strings.TrimSpace(req.Actor.CreatedAt)

	if req.Actor.ID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "actor.id is required")
		return
	}
	if req.Actor.DisplayName == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "actor.display_name is required")
		return
	}
	if req.Actor.CreatedAt == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "actor.created_at is required")
		return
	}
	if _, err := time.Parse(time.RFC3339, req.Actor.CreatedAt); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "actor.created_at must be an RFC3339 timestamp")
		return
	}
	if actors.IsReservedServiceActorID(req.Actor.ID) {
		writeError(w, http.StatusBadRequest, "invalid_request", "actor.id is reserved")
		return
	}

	registered, err := opts.actorRegistry.Register(r.Context(), actors.Actor{
		ID:          req.Actor.ID,
		DisplayName: req.Actor.DisplayName,
		Tags:        req.Actor.Tags,
		CreatedAt:   req.Actor.CreatedAt,
	})
	if err != nil {
		if errors.Is(err, actors.ErrAlreadyExists) {
			writeError(w, http.StatusConflict, "actor_exists", "actor with this id already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to register actor")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"actor": registered})
}

func handleListActors(w http.ResponseWriter, r *http.Request, actorRegistry ActorRegistry) {
	if actorRegistry == nil {
		writeError(w, http.StatusServiceUnavailable, "actor_registry_unavailable", "actor registry is not configured")
		return
	}

	var limitFilter *int
	limitRaw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if limitRaw != "" {
		parsed, err := strconv.Atoi(limitRaw)
		if err != nil || parsed < 1 || parsed > 1000 {
			writeError(w, http.StatusBadRequest, "invalid_request", "limit must be between 1 and 1000")
			return
		}
		limitFilter = &parsed
	}

	listed, nextCursor, err := actorRegistry.List(r.Context(), actors.ActorListFilter{
		Query:  strings.TrimSpace(r.URL.Query().Get("q")),
		Limit:  limitFilter,
		Cursor: strings.TrimSpace(r.URL.Query().Get("cursor")),
	})
	if err != nil {
		if errors.Is(err, actors.ErrInvalidCursor) {
			writeError(w, http.StatusBadRequest, "invalid_request", "cursor is invalid")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list actors")
		return
	}

	response := map[string]any{"actors": listed}
	if nextCursor != "" {
		response["next_cursor"] = nextCursor
	}
	writeJSON(w, http.StatusOK, response)
}
