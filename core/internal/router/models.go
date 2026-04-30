package router

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"agent-nexus-core/internal/auth"
)

const (
	WakeArtifactKind   = "agent_wake"
	MessagePostedEvent = "message_posted"
)

type WorkspaceBinding = auth.AgentRegistrationWorkspaceBinding
type AgentRegistration = auth.AgentRegistration

type AgentBridgeCheckin struct {
	Handle            string   `json:"handle"`
	ActorID           string   `json:"actor_id"`
	WorkspaceID       string   `json:"workspace_id"`
	WorkspaceIDs      []string `json:"workspace_ids"`
	BridgeInstanceID  string   `json:"bridge_instance_id"`
	CheckedInAt       string   `json:"checked_in_at"`
	ExpiresAt         string   `json:"expires_at"`
	ProofSignatureB64 string   `json:"proof_signature_b64"`
}

func (c AgentBridgeCheckin) ReadyForWorkspace(workspaceID string, now time.Time) bool {
	if !stringSliceContains(c.effectiveWorkspaceIDs(), workspaceID) {
		return false
	}
	expiresAt, ok := parseUTCISO(c.ExpiresAt)
	if !ok {
		return false
	}
	return !expiresAt.Before(now.UTC())
}

func (c AgentBridgeCheckin) effectiveWorkspaceIDs() []string {
	out := make([]string, 0, len(c.WorkspaceIDs)+1)
	seen := map[string]struct{}{}
	for _, workspaceID := range c.WorkspaceIDs {
		trimmed := strings.TrimSpace(workspaceID)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	if trimmed := strings.TrimSpace(c.WorkspaceID); trimmed != "" {
		if _, ok := seen[trimmed]; !ok {
			out = append([]string{trimmed}, out...)
		}
	}
	return out
}

func stringSliceContains(values []string, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func WakeupRequestKey(workspaceID string, threadID string, messageEventID string, actorID string) string {
	return "wake-req-" + sha256Text(workspaceID, threadID, messageEventID, actorID)[:24]
}

func WakeupArtifactID(workspaceID string, threadID string, messageEventID string, actorID string) string {
	return "wake_" + sha256Text(workspaceID, threadID, messageEventID, actorID)[:24]
}

func sha256Text(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

func bridgeProofMessage(checkin AgentBridgeCheckin) []byte {
	payload := map[string]any{
		"actor_id":           checkin.ActorID,
		"bridge_instance_id": checkin.BridgeInstanceID,
		"checked_in_at":      checkin.CheckedInAt,
		"expires_at":         checkin.ExpiresAt,
		"handle":             checkin.Handle,
		"v":                  "agent-bridge-checkin-proof/v1",
		"workspace_ids":      checkin.effectiveWorkspaceIDs(),
	}
	encoded, _ := json.Marshal(payload)
	return encoded
}

func VerifyBridgeCheckinSignature(publicKeyB64 string, checkin AgentBridgeCheckin) bool {
	publicKeyDER, err := base64.StdEncoding.DecodeString(strings.TrimSpace(publicKeyB64))
	if err != nil {
		return false
	}
	parsed, err := x509.ParsePKIXPublicKey(publicKeyDER)
	if err != nil {
		return false
	}
	publicKey, ok := parsed.(*ecdsa.PublicKey)
	if !ok {
		return false
	}
	signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(checkin.ProofSignatureB64))
	if err != nil {
		return false
	}
	hash := sha256.Sum256(bridgeProofMessage(checkin))
	return ecdsa.VerifyASN1(publicKey, hash[:], signature)
}

func decodeIntoMap[T any](value map[string]any) (T, error) {
	var out T
	encoded, err := json.Marshal(value)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(encoded, &out)
	return out, err
}
