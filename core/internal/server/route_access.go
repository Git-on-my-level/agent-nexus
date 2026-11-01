// Route access classification and enforcement. Routes declare an access
// bucket and mutation policy; enforceRouteAccess turns a classified request
// into an authentication decision.

package server

import (
	"net"
	"net/http"
	"strings"
)

type routeAccessBucket string

const (
	routeAccessAlwaysPublic           routeAccessBucket = "always_public"
	routeAccessPublicAuthCeremony     routeAccessBucket = "public_auth_ceremony"
	routeAccessWorkspaceBusiness      routeAccessBucket = "workspace_business_surface"
	routeAccessDevOnlyLegacyActor     routeAccessBucket = "dev_only_legacy_actor_surface"
	routeAccessAuthenticatedPrincipal routeAccessBucket = "authenticated_principal_surface"
)

type routeMutationPolicy string

const (
	routeMutationNone                routeMutationPolicy = "none"
	routeMutationBusiness            routeMutationPolicy = "business"
	routeMutationRecovery            routeMutationPolicy = "recovery"
	routeMutationAuthCeremony        routeMutationPolicy = "auth_ceremony"
	routeMutationAuthAccessCeremony  routeMutationPolicy = "auth_access_ceremony"
	routeMutationPrincipalGrowth     routeMutationPolicy = "principal_growth"
	routeMutationInternalMaintenance routeMutationPolicy = "internal_maintenance"
)

type routeAccessRequirement struct {
	bucket    routeAccessBucket
	mutation  routeMutationPolicy
	supported bool
}

type routeAccessClassifier func(*http.Request) routeAccessRequirement

func exactRouteAccess(bucket routeAccessBucket, mutation routeMutationPolicy, methods ...string) routeAccessClassifier {
	allowed := make(map[string]struct{}, len(methods))
	for _, method := range methods {
		method = strings.ToUpper(strings.TrimSpace(method))
		if method != "" {
			allowed[method] = struct{}{}
		}
	}

	return func(r *http.Request) routeAccessRequirement {
		if len(allowed) > 0 {
			if _, ok := allowed[strings.ToUpper(strings.TrimSpace(r.Method))]; !ok {
				return routeAccessRequirement{}
			}
		}
		return routeAccessRequirement{bucket: bucket, mutation: mutation, supported: true}
	}
}

func isReadOnlyRequest(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet, http.MethodHead:
		return true
	default:
		return false
	}
}

func isLoopbackRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func enforceRouteAccess(w http.ResponseWriter, r *http.Request, opts handlerOptions, requirement routeAccessRequirement) bool {
	if !requirement.supported {
		return true
	}

	switch requirement.bucket {
	case routeAccessAlwaysPublic:
		return true
	case routeAccessPublicAuthCeremony:
		if r.Method == http.MethodPost && (r.URL.Path == "/agent-wakeups/claim" || r.URL.Path == "/agent-wakeups/complete" || r.URL.Path == "/agent-wakeups/fail") {
			return authenticateAgentWakeupMutation(w, r, opts)
		}
		return true
	case routeAccessWorkspaceBusiness:
		if isReadOnlyRequest(r.Method) && opts.enableDevActorMode {
			_, ok := authenticatePrincipalFromHeader(w, r, opts, false)
			return ok
		}
		if isReadOnlyRequest(r.Method) && opts.allowLoopbackVerificationReads && isLoopbackRequest(r) {
			_, ok := authenticatePrincipalFromHeader(w, r, opts, false)
			return ok
		}
		if !isReadOnlyRequest(r.Method) && opts.allowUnauthenticatedWrites {
			_, ok := authenticatePrincipalFromHeader(w, r, opts, false)
			return ok
		}
		_, ok := authenticatePrincipalFromHeader(w, r, opts, true)
		return ok
	case routeAccessDevOnlyLegacyActor:
		if !opts.enableDevActorMode {
			writeError(w, http.StatusForbidden, "dev_actor_mode_required", "legacy actor flows require explicit development mode")
			return false
		}
		_, ok := authenticatePrincipalFromHeader(w, r, opts, false)
		return ok
	case routeAccessAuthenticatedPrincipal:
		_, ok := authenticatePrincipalFromHeader(w, r, opts, true)
		return ok
	default:
		writeError(w, http.StatusForbidden, "access_denied", "request is not allowed on this deployment")
		return false
	}
}
