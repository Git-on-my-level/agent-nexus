package auth

import (
	"testing"
	"time"
)

func TestDescribeWakeRoutingMarksFreshHeartbeatOnline(t *testing.T) {
	t.Parallel()

	status := DescribeWakeRouting(
		AuthPrincipalSummary{
			ActorID:       "actor-m4-hermes",
			Username:      "m4-hermes",
			PrincipalKind: string(PrincipalKindAgent),
			Registration: &AgentRegistration{
				Handle:            "m4-hermes",
				ActorID:           "actor-m4-hermes",
				Status:            "active",
				BridgeInstanceID:  "bridge-hermes-1",
				BridgeCheckedInAt: "2099-03-20T12:00:00Z",
				BridgeExpiresAt:   "2099-03-20T12:05:00Z",
				WorkspaceBindings: []AgentRegistrationWorkspaceBinding{
					{WorkspaceID: "ws_main", Enabled: true},
				},
			},
		},
		"ws_main",
		time.Date(2099, 3, 20, 12, 1, 0, 0, time.UTC),
	)

	if !status.Applicable || !status.Taggable || !status.Online {
		t.Fatalf("expected wake routing to be online and taggable, got %#v", status)
	}
	if status.State != WakeRoutingStateOnline {
		t.Fatalf("expected online state, got %#v", status)
	}
	if status.Summary != "Online as @m4-hermes." {
		t.Fatalf("unexpected online summary: %#v", status)
	}
}

func TestDescribeWakeRoutingTreatsStaleHeartbeatAsOffline(t *testing.T) {
	t.Parallel()

	status := DescribeWakeRouting(
		AuthPrincipalSummary{
			ActorID:       "actor-m4-hermes",
			Username:      "m4-hermes",
			PrincipalKind: string(PrincipalKindAgent),
			Registration: &AgentRegistration{
				Handle:            "m4-hermes",
				ActorID:           "actor-m4-hermes",
				Status:            "active",
				BridgeInstanceID:  "bridge-hermes-1",
				BridgeCheckedInAt: "2026-03-20T12:00:00Z",
				BridgeExpiresAt:   "2026-03-20T12:05:00Z",
				WorkspaceBindings: []AgentRegistrationWorkspaceBinding{
					{WorkspaceID: "ws_main", Enabled: true},
				},
			},
		},
		"ws_main",
		time.Date(2026, 3, 20, 12, 6, 0, 0, time.UTC),
	)

	if !status.Applicable || !status.Taggable || status.Online {
		t.Fatalf("expected wake routing to stay taggable but offline, got %#v", status)
	}
	if status.State != WakeRoutingStateOffline {
		t.Fatalf("expected offline state, got %#v", status)
	}
	if status.Summary != "Offline. The agent is registered for this workspace, but its last bridge heartbeat is stale." {
		t.Fatalf("unexpected offline summary: %#v", status)
	}
}
