package auth

import (
	"strings"
	"time"
)

const (
	WakeRoutingStateUnknown      = "unknown"
	WakeRoutingStateRevoked      = "revoked"
	WakeRoutingStateUnregistered = "unregistered"
	WakeRoutingStateDisabled     = "disabled"
	WakeRoutingStateOffline      = "offline"
	WakeRoutingStateOnline       = "online"
)

type WakeRoutingStatus struct {
	Applicable bool   `json:"applicable"`
	Handle     string `json:"handle"`
	Taggable   bool   `json:"taggable"`
	Online     bool   `json:"online"`
	State      string `json:"state"`
	Summary    string `json:"summary"`
}

func parseWakeRoutingTimestamp(value string) (time.Time, bool) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return time.Time{}, false
	}
	if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return parsed, true
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func DescribeWakeRouting(principal AuthPrincipalSummary, workspaceID string, now time.Time) WakeRoutingStatus {
	handle := strings.TrimSpace(principal.Username)
	base := WakeRoutingStatus{
		Applicable: true,
		Handle:     handle,
		Taggable:   false,
		Online:     false,
		State:      WakeRoutingStateOffline,
		Summary:    "",
	}

	if strings.TrimSpace(principal.PrincipalKind) != string(PrincipalKindAgent) {
		return WakeRoutingStatus{Applicable: false, Handle: handle}
	}
	if principal.Revoked {
		base.State = WakeRoutingStateRevoked
		base.Summary = "Revoked agent principals cannot be tagged."
		return base
	}
	if handle == "" {
		base.State = WakeRoutingStateUnknown
		base.Summary = "No username is set for `@handle` routing."
		return base
	}

	if principal.HostID != "" {
		if principal.HostExcluded {
			base.State = WakeRoutingStateDisabled
			base.Summary = "Agent name is excluded on its host."
			return base
		}
		base.Taggable = true
		base.Online = principal.HostBridgeOnline
		if base.Online {
			base.State = WakeRoutingStateOnline
			base.Summary = "Online as @" + handle + "."
		} else {
			base.State = WakeRoutingStateOffline
			base.Summary = "Host bridge is offline."
		}
		return base
	}
	registration := principal.Registration
	if registration == nil {
		base.State = WakeRoutingStateUnregistered
		base.Summary = "Legacy standalone agent has no wake setup. Enroll its machine as a host to use host-based wake routing."
		return base
	}

	registeredHandle := strings.TrimSpace(registration.Handle)
	if registeredHandle != "" && registeredHandle != handle {
		base.State = WakeRoutingStateUnknown
		base.Summary = "Legacy wake setup does not match this agent handle."
		return base
	}

	registeredActorID := strings.TrimSpace(registration.ActorID)
	if registeredActorID == "" || registeredActorID != strings.TrimSpace(principal.ActorID) {
		base.State = WakeRoutingStateUnknown
		base.Summary = "Legacy wake setup does not match this agent identity."
		return base
	}

	status := strings.TrimSpace(registration.Status)
	if status == "" {
		status = "active"
	}
	if status == "disabled" {
		base.State = WakeRoutingStateDisabled
		base.Summary = "Legacy wake setup is disabled."
		return base
	}

	targetWorkspaceID := strings.TrimSpace(workspaceID)
	enabledBindings := make([]AgentRegistrationWorkspaceBinding, 0, len(registration.WorkspaceBindings))
	for _, binding := range registration.WorkspaceBindings {
		if strings.TrimSpace(binding.WorkspaceID) == "" {
			continue
		}
		if binding.Enabled {
			enabledBindings = append(enabledBindings, binding)
		}
	}

	if targetWorkspaceID == "" {
		if len(enabledBindings) == 0 {
			base.State = WakeRoutingStateUnregistered
			base.Summary = "Legacy wake setup is not enabled for a workspace."
			return base
		}
	} else if !registration.SupportsWorkspace(targetWorkspaceID) {
		base.State = WakeRoutingStateUnregistered
		base.Summary = "Legacy wake setup is not enabled for this workspace."
		return base
	}

	base.Taggable = true
	bridgeInstanceID := strings.TrimSpace(registration.BridgeInstanceID)
	checkedInAt, checkedInOK := parseWakeRoutingTimestamp(registration.BridgeCheckedInAt)
	expiresAt, expiresOK := parseWakeRoutingTimestamp(registration.BridgeExpiresAt)

	if bridgeInstanceID == "" || !checkedInOK || !expiresOK {
		base.State = WakeRoutingStateOffline
		base.Summary = "Offline. This legacy agent has no fresh bridge heartbeat."
		return base
	}
	if expiresAt.Before(now) {
		base.State = WakeRoutingStateOffline
		base.Summary = "Offline. This legacy agent's last bridge heartbeat is stale."
		return base
	}
	if checkedInAt.After(expiresAt) {
		base.State = WakeRoutingStateUnknown
		base.Summary = "Bridge heartbeat timing is inconsistent right now."
		return base
	}
	if targetWorkspaceID != "" && len(registration.BridgeWorkspaceIDs) > 0 {
		covered := false
		for _, workspaceID := range registration.BridgeWorkspaceIDs {
			if strings.TrimSpace(workspaceID) == targetWorkspaceID {
				covered = true
				break
			}
		}
		if !covered {
			base.State = WakeRoutingStateOffline
			base.Summary = "Offline. The agent bridge has not checked in for this workspace."
			return base
		}
	}

	return WakeRoutingStatus{
		Applicable: true,
		Handle:     handle,
		Taggable:   true,
		Online:     true,
		State:      WakeRoutingStateOnline,
		Summary:    "Online as @" + handle + ".",
	}
}
