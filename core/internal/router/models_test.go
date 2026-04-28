package router

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"testing"
	"time"
)

func TestAgentBridgeCheckinReadyForWorkspaceUsesWorkspaceSet(t *testing.T) {
	checkin := AgentBridgeCheckin{
		WorkspaceID:      "ws-main",
		WorkspaceIDs:     []string{"ws-main", "ws-ops"},
		BridgeInstanceID: "bridge-1",
		ExpiresAt:        time.Now().UTC().Add(time.Minute).Format(time.RFC3339),
	}

	if !checkin.ReadyForWorkspace("ws-ops", time.Now()) {
		t.Fatal("expected check-in to be ready for secondary workspace")
	}
	if checkin.ReadyForWorkspace("ws-missing", time.Now()) {
		t.Fatal("expected check-in not to be ready for missing workspace")
	}
}

func TestBridgeProofMessageUsesWorkspaceIDsContract(t *testing.T) {
	checkin := AgentBridgeCheckin{
		Handle:           "cursor",
		ActorID:          "actor-1",
		WorkspaceID:      "ws-main",
		WorkspaceIDs:     []string{"ws-main", "ws-ops"},
		BridgeInstanceID: "bridge-1",
		CheckedInAt:      "2026-04-29T00:00:00Z",
		ExpiresAt:        "2026-04-29T00:05:00Z",
	}

	got := string(bridgeProofMessage(checkin))
	want := `{"actor_id":"actor-1","bridge_instance_id":"bridge-1","checked_in_at":"2026-04-29T00:00:00Z","expires_at":"2026-04-29T00:05:00Z","handle":"cursor","v":"agent-bridge-checkin-proof/v1","workspace_ids":["ws-main","ws-ops"]}`
	if got != want {
		t.Fatalf("proof message mismatch\ngot:  %s\nwant: %s", got, want)
	}
}

func TestVerifyBridgeCheckinSignatureUsesWorkspaceIDsContract(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	checkin := AgentBridgeCheckin{
		Handle:           "cursor",
		ActorID:          "actor-1",
		WorkspaceID:      "ws-main",
		WorkspaceIDs:     []string{"ws-main", "ws-ops"},
		BridgeInstanceID: "bridge-1",
		CheckedInAt:      "2026-04-29T00:00:00Z",
		ExpiresAt:        "2026-04-29T00:05:00Z",
	}
	digest := sha256.Sum256(bridgeProofMessage(checkin))
	signature, err := ecdsa.SignASN1(rand.Reader, privateKey, digest[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	checkin.ProofSignatureB64 = base64.StdEncoding.EncodeToString(signature)

	if !VerifyBridgeCheckinSignature(base64.StdEncoding.EncodeToString(publicDER), checkin) {
		t.Fatal("expected signature to verify")
	}
	checkin.WorkspaceIDs = []string{"ws-main"}
	if VerifyBridgeCheckinSignature(base64.StdEncoding.EncodeToString(publicDER), checkin) {
		t.Fatal("expected signature verification to fail after workspace set changes")
	}
}
