// Shared request validation helpers for the canonical primitive endpoints:
// the registered-actor check mutations apply and raw string-slice extraction
// from JSON payloads. Event handlers live in events_handlers.go; artifact
// handlers in artifacts_handlers.go and artifact_reads.go.

package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"agent-nexus-core/internal/actors"
)

func requireRegisteredActorID(w http.ResponseWriter, r *http.Request, actorRegistry ActorRegistry, actorID string) (string, bool) {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "actor_id is required")
		return "", false
	}
	if actors.IsReservedServiceActorID(actorID) {
		writeError(w, http.StatusBadRequest, "invalid_request", "actor_id is reserved for system use")
		return "", false
	}

	if actorRegistry == nil {
		writeError(w, http.StatusServiceUnavailable, "actor_registry_unavailable", "actor registry is not configured")
		return "", false
	}

	exists, err := actorRegistry.Exists(r.Context(), actorID)
	if err != nil {
		if actorExistsDebugErrors() {
			writeError(w, http.StatusInternalServerError, "internal_error", fmt.Sprintf("failed to validate actor_id: %v", err))
			return "", false
		}
		writeReadInternalError(w, r, err, "failed to validate actor_id")
		return "", false
	}
	if !exists {
		writeError(w, http.StatusBadRequest, "unknown_actor_id", "actor_id is not registered")
		return "", false
	}

	return actorID, true
}

func extractStringSlice(raw any) ([]string, error) {
	switch values := raw.(type) {
	case []string:
		out := make([]string, len(values))
		copy(out, values)
		return out, nil
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				return nil, errors.New("list contains non-string values")
			}
			out = append(out, text)
		}
		return out, nil
	default:
		return nil, errors.New("must be a list of strings")
	}
}
