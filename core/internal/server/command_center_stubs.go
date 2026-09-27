package server

import (
	"net/http"
	"strings"
)

// These route placeholders keep the generated contract discoverable until the
// identity and runs workstreams replace them with real handlers.
func commandCenterStubRouteAccess(r *http.Request) routeAccessRequirement {
	path, method := r.URL.Path, r.Method
	public := routeAccessRequirement{bucket: routeAccessPublicAuthCeremony, mutation: routeMutationAuthCeremony, supported: true}
	authenticated := routeAccessRequirement{bucket: routeAccessAuthenticatedPrincipal, mutation: routeMutationNone, supported: true}
	write := routeAccessRequirement{bucket: routeAccessAuthenticatedPrincipal, mutation: routeMutationBusiness, supported: true}
	unsupported := routeAccessRequirement{}

	if path == "/auth/hosts/enrollments" && method == http.MethodPost {
		return public
	}
	if path == "/auth/hosts/enrollments/headless" && method == http.MethodPost {
		return public
	}
	if path == "/auth/hosts/enrollments/pending" && method == http.MethodGet {
		return authenticated
	}
	if path == "/auth/hosts/enrollment-tokens" {
		if method == http.MethodGet {
			return authenticated
		}
		if method == http.MethodPost {
			return write
		}
	}
	if strings.HasPrefix(path, "/auth/hosts/enrollment-tokens/") {
		parts := strings.Split(strings.TrimPrefix(path, "/auth/hosts/enrollment-tokens/"), "/")
		if len(parts) == 2 && parts[0] != "" && parts[1] == "revoke" && method == http.MethodPost {
			return write
		}
	}
	if strings.HasPrefix(path, "/auth/hosts/enrollments/") {
		parts := strings.Split(strings.TrimPrefix(path, "/auth/hosts/enrollments/"), "/")
		if parts[0] != "" && len(parts) == 1 && method == http.MethodGet {
			return public
		}
		if parts[0] != "" && len(parts) == 2 && method == http.MethodPost {
			switch parts[1] {
			case "complete":
				return public
			case "approve", "deny":
				return write
			}
		}
	}
	if path == "/hosts" && method == http.MethodGet {
		return authenticated
	}
	if strings.HasPrefix(path, "/hosts/") {
		parts := strings.Split(strings.TrimPrefix(path, "/hosts/"), "/")
		if parts[0] != "" && len(parts) == 1 {
			switch method {
			case http.MethodGet:
				return authenticated
			case http.MethodPatch:
				return public // signed host proof or human bearer is checked by the real handler
			case http.MethodDelete:
				return write
			}
		}
		if parts[0] != "" && len(parts) == 3 && parts[1] == "bridge" && parts[2] == "check-in" && method == http.MethodPost {
			return public // signed host proof is checked by the real handler
		}
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
