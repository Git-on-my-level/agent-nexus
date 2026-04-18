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
	BridgeCheckedInEvent = "agent_bridge_checked_in"
	WakeArtifactKind     = "agent_wake"
	WakeRequestEvent     = "agent_wakeup_requested"
	MessagePostedEvent   = "message_posted"
)

type WorkspaceBinding = auth.AgentRegistrationWorkspaceBinding
type AgentRegistration = auth.AgentRegistration

type AgentBridgeCheckin struct {
	Handle            string `json:"handle"`
	ActorID           string `json:"actor_id"`
	WorkspaceID       string `json:"workspace_id"`
	BridgeInstanceID  string `json:"bridge_instance_id"`
	CheckedInAt       string `json:"checked_in_at"`
	ExpiresAt         string `json:"expires_at"`
	ProofSignatureB64 string `json:"proof_signature_b64"`
}

func (c AgentBridgeCheckin) ReadyForWorkspace(workspaceID string, now time.Time) bool {
	if c.WorkspaceID != workspaceID {
		return false
	}
	expiresAt, ok := parseUTCISO(c.ExpiresAt)
	if !ok {
		return false
	}
	return !expiresAt.Before(now.UTC())
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
	values := []struct {
		Key   string
		Value string
	}{
		{Key: "actor_id", Value: checkin.ActorID},
		{Key: "bridge_instance_id", Value: checkin.BridgeInstanceID},
		{Key: "checked_in_at", Value: checkin.CheckedInAt},
		{Key: "expires_at", Value: checkin.ExpiresAt},
		{Key: "handle", Value: checkin.Handle},
		{Key: "v", Value: "agent-bridge-checkin-proof/v1"},
		{Key: "workspace_id", Value: checkin.WorkspaceID},
	}
	var builder strings.Builder
	builder.WriteByte('{')
	for i, item := range values {
		if i > 0 {
			builder.WriteByte(',')
		}
		keyJSON, _ := json.Marshal(item.Key)
		valueJSON, _ := json.Marshal(item.Value)
		builder.Write(keyJSON)
		builder.WriteByte(':')
		builder.Write(valueJSON)
	}
	builder.WriteByte('}')
	return []byte(builder.String())
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
