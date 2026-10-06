package server

import (
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/resourceaccess"
	"context"
	"errors"
	"net/http"
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
	if !validateResourceText(w, values) {
		return false
	}
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

func validateResourceText(w http.ResponseWriter, values any) bool {
	if err := resourceaccess.ValidateText(values); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return false
	}
	return true
}
func authorizeResourceSelectors(w http.ResponseWriter, r *http.Request) bool {
	values := []string{}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch r.URL.Path {
	case "/artifacts/attachments", "/docs/search", "/work/capabilities", "/inbox/summary":
		parts = nil // Exact service operations have no path resource parameter.
	}
	// Only resource positions have selector meaning. Arbitrary query values or
	// path segments on health/auth/identity routes must not become existence probes.
	for i := 0; i+1 < len(parts); i++ {
		switch parts[i] {
		case "boards", "cards", "threads", "topics", "docs", "documents", "artifacts", "events", "inbox", "work", "runs", "revisions":
			kind := map[string]string{"boards": "board", "cards": "card", "threads": "thread", "topics": "topic", "docs": "document", "documents": "document", "artifacts": "artifact", "events": "event", "inbox": "inbox", "work": "card", "runs": "run"}[parts[i]]
			value := parts[i+1]
			if kind != "" && !strings.Contains(value, ":") {
				value = kind + ":" + value
			}
			values = append(values, value)
		}
	}
	var keys []string
	switch r.URL.Path {
	case "/events", "/stream/events":
		keys = []string{"thread_id", "topic_id"}
	case "/artifacts":
		keys = []string{"thread_id", "ids"}
	case "/docs":
		keys = []string{"thread_id"}
	case "/stream/inbox", "/stream/agent-notification-receipts":
		keys = []string{"thread_id"}
	case "/ref-edges":
		keys = []string{"source_ref", "target_ref"}
	}
	for _, key := range keys {
		for _, entry := range r.URL.Query()[key] {
			values = append(values, strings.Split(entry, ",")...)
		}
	}
	return authorizeResourceValues(w, r, values)
}
