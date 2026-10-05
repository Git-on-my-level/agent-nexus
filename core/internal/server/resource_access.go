package server

import (
	"agent-nexus-core/internal/primitives"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type resourceAccessCheckKey struct{}

func attachResourceAccessScope(r *http.Request, opts handlerOptions) {
	scope := primitives.AccessScope{PMActorID: pmAgentActorID(opts)}
	if principal, ok := cachedAuthenticatedPrincipal(r); ok {
		scope.ActorID = principal.ActorID
	}
	ctx := primitives.WithAccessScope(r.Context(), scope)
	if store, ok := opts.primitiveStore.(*primitives.Store); ok {
		ctx = context.WithValue(ctx, resourceAccessCheckKey{}, store.CheckResourceValues)
	}
	*r = *r.WithContext(ctx)
}
func authorizeResourceValues(w http.ResponseWriter, r *http.Request, values any) bool {
	check, ok := r.Context().Value(resourceAccessCheckKey{}).(func(context.Context, any) error)
	if !ok {
		return true
	}
	if err := check(r.Context(), values); err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, 404, "not_found", "resource not found")
		} else {
			writeError(w, 500, "internal_error", "resource authorization failed")
		}
		return false
	}
	return true
}
func authorizeResourceSelectors(w http.ResponseWriter, r *http.Request) bool {
	values := []string{}
	for _, part := range strings.Split(r.URL.EscapedPath(), "/") {
		if decoded, err := url.PathUnescape(part); err == nil {
			values = append(values, decoded)
		}
	}
	for _, entries := range r.URL.Query() {
		for _, entry := range entries {
			values = append(values, strings.Split(entry, ",")...)
		}
	}
	return authorizeResourceValues(w, r, values)
}

// Validate the original JSON before handlers discard unknown fields, return an
// idempotency replay, or create run attribution. Multipart/binary uploads retain
// their streaming decoder and authorize their resource selectors separately.
func authorizeResourceBody(w http.ResponseWriter, r *http.Request) bool {
	if isReadOnlyRequest(r.Method) || r.Body == nil || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return true
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		if !writeRequestTooLargeError(w, err) {
			writeError(w, 400, "invalid_json", "request body could not be read")
		}
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return true
	} // Handler owns syntax errors.
	return authorizeResourceValues(w, r, value)
}
