package server

import (
	"errors"
	"net/http"
	"strings"

	"agent-nexus-core/internal/auth"
)

func authAdminRouteAccess(r *http.Request) routeAccessRequirement {
	if r.URL.Path == "/auth/admins" {
		return exactRouteAccess(routeAccessAuthenticatedPrincipal, routeMutationNone, http.MethodGet)(r)
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/auth/admins/"), "/")
	if len(parts) == 2 && parts[0] != "" && (parts[1] == "grant" || parts[1] == "revoke") {
		return exactRouteAccess(routeAccessAuthenticatedPrincipal, routeMutationBusiness, http.MethodPost)(r)
	}
	return routeAccessRequirement{}
}

func handleAuthAdminRoutes(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if r.URL.Path == "/auth/admins" && r.Method == http.MethodGet {
		if _, ok := requireAuthAdminPrincipal(w, r, opts); !ok {
			return
		}
		items, err := opts.authStore.ListAuthAdmins(r.Context())
		if err != nil {
			writeError(w, 500, "internal_error", "failed to list auth admins")
			return
		}
		writeJSON(w, 200, map[string]any{"admins": items})
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/auth/admins/"), "/")
	if len(parts) != 2 || parts[0] == "" || (parts[1] != "grant" && parts[1] != "revoke") || r.Method != http.MethodPost {
		writeError(w, 404, "not_found", "endpoint not found")
		return
	}
	actor, ok := requireHumanPrincipal(w, r, opts)
	if !ok {
		return
	}
	item, err := opts.authStore.SetAuthAdmin(r.Context(), parts[0], parts[1] == "grant", *actor)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrHumanRequired):
			writeError(w, 403, "human_required", "only humans can change auth-admin grants")
		case errors.Is(err, auth.ErrAgentNotFound):
			writeError(w, 404, "not_found", "agent principal not found")
		case errors.Is(err, auth.ErrInvalidRequest):
			writeError(w, 400, "invalid_request", "auth-admin grants require an active agent principal")
		default:
			writeError(w, 500, "internal_error", "failed to change auth-admin grant")
		}
		return
	}
	writeJSON(w, 200, map[string]any{"admin": item})
}
