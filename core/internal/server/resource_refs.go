package server

import (
	"context"
	"errors"
	"net/http"

	"agent-nexus-core/internal/primitives"
)

func resolveHTTPResourceID(w http.ResponseWriter, r *http.Request, opts handlerOptions, typ, raw, label string) (string, bool) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return "", false
	}
	resolved, err := opts.primitiveStore.ResolveResourceRef(r.Context(), primitives.ResourceRefInput{Type: typ, Ref: raw})
	if err != nil {
		switch {
		case errors.Is(err, primitives.ErrInvalidResourceRef):
			writeError(w, http.StatusBadRequest, "invalid_request", label+" is invalid")
		case errors.Is(err, primitives.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", label+" not found")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to resolve "+label)
		}
		return "", false
	}
	return resolved.ID, true
}

func resolveResourceIDForInternalUse(ctx context.Context, opts handlerOptions, typ, raw string) (string, bool) {
	if opts.primitiveStore == nil {
		return "", false
	}
	resolved, err := opts.primitiveStore.ResolveResourceRef(ctx, primitives.ResourceRefInput{Type: typ, Ref: raw})
	if err != nil {
		return "", false
	}
	return resolved.ID, true
}
