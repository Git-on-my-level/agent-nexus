package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
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

func resolvedPublicIdentity(ctx context.Context, opts handlerOptions, typ, id string) (string, string) {
	id = strings.TrimSpace(id)
	if opts.primitiveStore != nil {
		if resolved, err := opts.primitiveStore.ResolveResourceRef(ctx, primitives.ResourceRefInput{Type: typ, Ref: id}); err == nil {
			return resolved.CanonicalRef, resolved.Handle
		}
	}
	return typ + ":" + id, id
}

func copyStringAnyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func resolveListFilterResourceRef(w http.ResponseWriter, r *http.Request, opts handlerOptions, typ, raw, label string) (primitives.ResolvedResourceRef, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return primitives.ResolvedResourceRef{}, true
	}
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return primitives.ResolvedResourceRef{}, false
	}
	resolved, err := opts.primitiveStore.ResolveResourceRef(r.Context(), primitives.ResourceRefInput{Type: typ, Ref: raw})
	if err == nil {
		return resolved, true
	}
	if errors.Is(err, primitives.ErrInvalidResourceRef) {
		writeError(w, http.StatusBadRequest, "invalid_request", label+" must be a "+typ+" public ref or handle")
		return primitives.ResolvedResourceRef{}, false
	}

	fallback := raw
	if strings.Contains(raw, ":") {
		prefix, suffix, splitErr := schema.SplitTypedRef(raw)
		if splitErr != nil || prefix != typ {
			writeError(w, http.StatusBadRequest, "invalid_request", label+" must be a "+typ+" public ref or handle")
			return primitives.ResolvedResourceRef{}, false
		}
		fallback = suffix
	}
	return primitives.ResolvedResourceRef{Type: typ, ID: strings.TrimSpace(fallback), Handle: strings.TrimSpace(fallback), CanonicalRef: typ + ":" + strings.TrimSpace(fallback)}, true
}

func resolvedRefStorageCandidates(resolved primitives.ResolvedResourceRef) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 2)
	for _, value := range []string{resolved.ID, resolved.Handle} {
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
