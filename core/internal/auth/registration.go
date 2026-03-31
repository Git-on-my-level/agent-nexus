package auth

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const AgentRegistrationVersion = "agent-registration/v1"

type AgentRegistrationWorkspaceBinding struct {
	WorkspaceID string `json:"workspace_id"`
	Enabled     bool   `json:"enabled"`
}

type AgentRegistration struct {
	Version                    string                              `json:"version"`
	Handle                     string                              `json:"handle"`
	ActorID                    string                              `json:"actor_id"`
	DeliveryMode               string                              `json:"delivery_mode,omitempty"`
	DriverKind                 string                              `json:"driver_kind,omitempty"`
	ResumePolicy               string                              `json:"resume_policy,omitempty"`
	Status                     string                              `json:"status,omitempty"`
	WorkspaceBindings          []AgentRegistrationWorkspaceBinding `json:"workspace_bindings"`
	AdapterKind                string                              `json:"adapter_kind,omitempty"`
	BridgeInstanceID           string                              `json:"bridge_instance_id,omitempty"`
	BridgeSigningPublicKeySPKI string                              `json:"bridge_signing_public_key_spki_b64,omitempty"`
	BridgeCheckedInAt          string                              `json:"bridge_checked_in_at,omitempty"`
	BridgeExpiresAt            string                              `json:"bridge_expires_at,omitempty"`
	BridgeCheckinEventID       string                              `json:"bridge_checkin_event_id,omitempty"`
	BridgeCheckinTTLSeconds    int                                 `json:"bridge_checkin_ttl_seconds,omitempty"`
	UpdatedAt                  string                              `json:"updated_at,omitempty"`
}

func normalizeAgentRegistration(input AgentRegistration) AgentRegistration {
	output := AgentRegistration{
		Version:                    strings.TrimSpace(input.Version),
		Handle:                     strings.TrimSpace(input.Handle),
		ActorID:                    strings.TrimSpace(input.ActorID),
		DeliveryMode:               strings.TrimSpace(input.DeliveryMode),
		DriverKind:                 strings.TrimSpace(input.DriverKind),
		ResumePolicy:               strings.TrimSpace(input.ResumePolicy),
		Status:                     strings.TrimSpace(input.Status),
		AdapterKind:                strings.TrimSpace(input.AdapterKind),
		BridgeInstanceID:           strings.TrimSpace(input.BridgeInstanceID),
		BridgeSigningPublicKeySPKI: strings.TrimSpace(input.BridgeSigningPublicKeySPKI),
		BridgeCheckedInAt:          strings.TrimSpace(input.BridgeCheckedInAt),
		BridgeExpiresAt:            strings.TrimSpace(input.BridgeExpiresAt),
		BridgeCheckinEventID:       strings.TrimSpace(input.BridgeCheckinEventID),
		BridgeCheckinTTLSeconds:    input.BridgeCheckinTTLSeconds,
		UpdatedAt:                  strings.TrimSpace(input.UpdatedAt),
	}
	if output.Version == "" {
		output.Version = AgentRegistrationVersion
	}
	if output.DeliveryMode == "" {
		output.DeliveryMode = "pull"
	}
	if output.DriverKind == "" {
		output.DriverKind = "custom"
	}
	if output.ResumePolicy == "" {
		output.ResumePolicy = "resume_or_create"
	}
	if output.Status == "" {
		output.Status = "pending"
	}
	if output.BridgeCheckinTTLSeconds < 0 {
		output.BridgeCheckinTTLSeconds = 0
	}
	bindings := make([]AgentRegistrationWorkspaceBinding, 0, len(input.WorkspaceBindings))
	seen := make(map[string]struct{}, len(input.WorkspaceBindings))
	for _, item := range input.WorkspaceBindings {
		workspaceID := strings.TrimSpace(item.WorkspaceID)
		if workspaceID == "" {
			continue
		}
		key := workspaceID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		bindings = append(bindings, AgentRegistrationWorkspaceBinding{
			WorkspaceID: workspaceID,
			Enabled:     item.Enabled,
		})
	}
	sort.SliceStable(bindings, func(i, j int) bool {
		return bindings[i].WorkspaceID < bindings[j].WorkspaceID
	})
	output.WorkspaceBindings = bindings
	return output
}

func (r AgentRegistration) SupportsWorkspace(workspaceID string) bool {
	target := strings.TrimSpace(workspaceID)
	if target == "" {
		return false
	}
	for _, item := range r.WorkspaceBindings {
		if item.Enabled && strings.TrimSpace(item.WorkspaceID) == target {
			return true
		}
	}
	return false
}

func registrationFromMetadataJSON(metadataJSON string) (*AgentRegistration, error) {
	metadataJSON = strings.TrimSpace(metadataJSON)
	if metadataJSON == "" {
		return nil, nil
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
		return nil, fmt.Errorf("decode principal metadata: %w", err)
	}
	raw, ok := metadata["wake_registration"]
	if !ok || raw == nil {
		return nil, nil
	}
	registrationMap, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("decode principal metadata: wake_registration is not an object")
	}
	encoded, err := json.Marshal(registrationMap)
	if err != nil {
		return nil, fmt.Errorf("encode wake_registration: %w", err)
	}
	var registration AgentRegistration
	if err := json.Unmarshal(encoded, &registration); err != nil {
		return nil, fmt.Errorf("decode wake_registration: %w", err)
	}
	normalized := normalizeAgentRegistration(registration)
	return &normalized, nil
}

func mergeRegistrationMetadataJSON(metadataJSON string, registration AgentRegistration) (string, error) {
	metadataJSON = strings.TrimSpace(metadataJSON)
	metadata := map[string]any{}
	if metadataJSON != "" {
		if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
			return "", fmt.Errorf("decode principal metadata: %w", err)
		}
	}
	metadata["wake_registration"] = normalizeAgentRegistration(registration)
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("encode principal metadata: %w", err)
	}
	return string(encoded), nil
}
