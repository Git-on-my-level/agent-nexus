package server

import (
	"net/http"
	"strings"
)

// Route placeholders for the parallel runs, presence, and roster workstream.
func commandCenterStubRouteAccess(r *http.Request) routeAccessRequirement {
	path, method := r.URL.Path, r.Method
	authenticated := routeAccessRequirement{bucket: routeAccessAuthenticatedPrincipal, mutation: routeMutationNone, supported: true}
	write := routeAccessRequirement{bucket: routeAccessAuthenticatedPrincipal, mutation: routeMutationBusiness, supported: true}
	unsupported := routeAccessRequirement{}

	if path == "/runs" {
		if method == http.MethodGet {
			return authenticated
		}
		if method == http.MethodPost {
			return write
		}
	}
	if strings.HasPrefix(path, "/runs/") && strings.TrimPrefix(path, "/runs/") != "" && !strings.Contains(strings.TrimPrefix(path, "/runs/"), "/") && method == http.MethodGet {
		return authenticated
	}
	if path == "/agents" && method == http.MethodGet {
		return authenticated
	}
	if path == "/agents/me/presence" && method == http.MethodPatch {
		return write
	}
	if strings.HasPrefix(path, "/agents/") && strings.TrimPrefix(path, "/agents/") != "" && !strings.Contains(strings.TrimPrefix(path, "/agents/"), "/") && method == http.MethodGet {
		return authenticated
	}
	return unsupported
}

func handleCommandCenterStub(w http.ResponseWriter, r *http.Request) {
	if !commandCenterStubRouteAccess(r).supported {
		writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
		return
	}
	writeError(w, http.StatusNotImplemented, "not_implemented", "command center route is awaiting implementation")
}
