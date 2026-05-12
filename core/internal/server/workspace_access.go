package server

import (
	"fmt"
	"net/http"
	"strings"
)

const (
	WorkspaceAccessModeReadWrite = "read_write"
	WorkspaceAccessModeReadOnly  = "read_only"
)

// NormalizeWorkspaceAccessMode returns a canonical access mode or an error if raw is invalid.
// Empty raw defaults to read/write.
func NormalizeWorkspaceAccessMode(raw string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" {
		return WorkspaceAccessModeReadWrite, nil
	}
	switch v {
	case WorkspaceAccessModeReadWrite, WorkspaceAccessModeReadOnly:
		return v, nil
	default:
		return "", fmt.Errorf("invalid workspace access mode %q (expected read_write or read_only)", strings.TrimSpace(raw))
	}
}

// enrichRouteMutationPolicy fills mutation when route classifiers only set the access bucket.
// Call sites that already set mutation are unchanged.
func enrichRouteMutationPolicy(r *http.Request, req routeAccessRequirement) routeAccessRequirement {
	if !req.supported || req.mutation != "" || r == nil {
		return req
	}
	path := strings.TrimSpace(r.URL.Path)
	method := strings.ToUpper(strings.TrimSpace(r.Method))

	if req.bucket == routeAccessWorkspaceBusiness && method == http.MethodPost {
		// Narrow recovery surface: lifecycle endpoints that primarily shrink footprint or move content into reclaim paths.
		// POST …/restore and …/unarchive remain business mutations—they typically expand active counts again.
		for _, suf := range []string{"/purge", "/trash", "/archive"} {
			if strings.HasSuffix(path, suf) {
				req.mutation = routeMutationRecovery
				return req
			}
		}
		switch path {
		case "/ops/blob-usage/rebuild", "/derived/rebuild", "/agent-bridge/check-in":
			req.mutation = routeMutationInternalMaintenance
			return req
		}
		if strings.HasPrefix(path, "/agent-wakeups/") {
			req.mutation = routeMutationInternalMaintenance
			return req
		}
	}

	switch req.bucket {
	case routeAccessWorkspaceBusiness:
		if isReadOnlyRequest(method) {
			req.mutation = routeMutationNone
		} else {
			req.mutation = routeMutationBusiness
		}
	case routeAccessAuthenticatedPrincipal:
		if isReadOnlyRequest(method) {
			req.mutation = routeMutationNone
		} else {
			req.mutation = routeMutationBusiness
		}
	case routeAccessDevOnlyLegacyActor:
		if isReadOnlyRequest(method) {
			req.mutation = routeMutationNone
		} else {
			req.mutation = routeMutationBusiness
		}
	case routeAccessPublicAuthCeremony:
		if isReadOnlyRequest(method) {
			req.mutation = routeMutationNone
		} else {
			req.mutation = routeMutationAuthAccessCeremony
		}
	default:
		req.mutation = routeMutationNone
	}
	return req
}

func enforceWorkspaceWriteAccess(w http.ResponseWriter, opts handlerOptions, requirement routeAccessRequirement) bool {
	if strings.TrimSpace(opts.workspaceAccessMode) != WorkspaceAccessModeReadOnly {
		return true
	}
	if !requirement.supported {
		return true
	}
	if requirement.mutation != routeMutationBusiness && requirement.mutation != routeMutationPrincipalGrowth {
		return true
	}
	writeDetailedError(w, http.StatusLocked, "workspace_read_only",
		"workspace is read-only until quota enforcement is released",
		map[string]any{
			"access_mode": WorkspaceAccessModeReadOnly,
		},
	)
	return false
}
