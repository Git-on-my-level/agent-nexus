package main

import "agent-nexus-core/internal/primitives"

// Capacity quotas are opt-in deployment policy. Request body, attachment upload,
// route rate, and storage integrity limits are configured independently.
func localQuotaEnforcementEnabled() bool {
	return envBool("ANX_ENFORCE_LOCAL_QUOTAS", false)
}

func effectiveWorkspaceQuota(enforce bool, configured primitives.WorkspaceQuota) primitives.WorkspaceQuota {
	if !enforce {
		return primitives.WorkspaceQuota{}
	}
	return configured
}
