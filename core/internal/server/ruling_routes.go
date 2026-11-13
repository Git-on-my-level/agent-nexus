package server

import (
	"net/http"
	"strings"
)

// registerCurationAndRulingRoutes registers the upkeep routes: the curation
// formula and its proposals, and standing rulings. Reading rulings is open to
// every member, because an agent checks them before it asks; making or
// revoking one is a human act, enforced in the handlers.
func registerCurationAndRulingRoutes(registerRoute func(string, routeAccessClassifier, http.HandlerFunc), opts handlerOptions) {
	registerRoute("/curation/candidates", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) { handleCurationCandidates(w, r, opts) })
	registerRoute("/curation/proposals", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationBusiness, http.MethodPost), func(w http.ResponseWriter, r *http.Request) { handleCurationPropose(w, r, opts) })
	registerRoute("/curation/ungroup", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationBusiness, http.MethodPost), func(w http.ResponseWriter, r *http.Request) { handleCurationUngroup(w, r, opts) })
	registerRoute("/curation/run", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationBusiness, http.MethodPost), func(w http.ResponseWriter, r *http.Request) { handleCurationRun(w, r, opts) })
	registerRoute("/curation/metrics", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) { handleCurationMetrics(w, r, opts) })
	registerRoute("/rulings", func(r *http.Request) routeAccessRequirement {
		switch r.Method {
		case http.MethodGet:
			return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, mutation: routeMutationNone, supported: true}
		case http.MethodPost:
			return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, mutation: routeMutationBusiness, supported: true}
		}
		return routeAccessRequirement{}
	}, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			handleListRulings(w, r, opts)
			return
		}
		handleCreateRuling(w, r, opts)
	})
	registerRoute("/rulings/", func(r *http.Request) routeAccessRequirement {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/rulings/"), "/")
		if len(parts) == 2 && parts[0] != "" && (parts[1] == "revoke" || parts[1] == "applied") && r.Method == http.MethodPost {
			return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, mutation: routeMutationBusiness, supported: true}
		}
		return routeAccessRequirement{}
	}, func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/rulings/"), "/")
		if parts[1] == "applied" {
			handleRulingApplied(w, r, opts, parts[0])
			return
		}
		handleRevokeRuling(w, r, opts, parts[0])
	})
}
