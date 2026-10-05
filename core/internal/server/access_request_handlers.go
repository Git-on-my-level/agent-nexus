package server

import (
	"net/http"
	"strings"
)

func accessRequestRouteAccess(r *http.Request) routeAccessRequirement {
	if r.URL.Path == "/auth/access/summary" {
		return exactRouteAccess(routeAccessAuthenticatedPrincipal, routeMutationNone, http.MethodGet)(r)
	}
	if r.URL.Path == "/auth/access-requests" {
		if r.Method == http.MethodGet {
			return exactRouteAccess(routeAccessAuthenticatedPrincipal, routeMutationNone, http.MethodGet)(r)
		}
		return exactRouteAccess(routeAccessAuthenticatedPrincipal, routeMutationBusiness, http.MethodPost)(r)
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/auth/access-requests/"), "/")
	if len(parts) == 2 && parts[0] != "" && (parts[1] == "approve" || parts[1] == "deny") {
		return exactRouteAccess(routeAccessAuthenticatedPrincipal, routeMutationBusiness, http.MethodPost)(r)
	}
	return routeAccessRequirement{}
}

func handleAccessRequestRoutes(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	writeError(w, http.StatusNotImplemented, "not_implemented", "access request implementation pending")
}
