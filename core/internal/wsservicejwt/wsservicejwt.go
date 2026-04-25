// Package wsservicejwt implements Ed25519 (EdDSA) signing for short-lived JWT assertions
// issued by a hosted workspace service (anx-core) when calling the control plane—for
// example heartbeat POSTs and account-status checks. The JWT uses registered claims
// iss, sub, aud, iat, nbf, exp, plus custom claims workspace_id and purpose.
//
// The control plane verifies these assertions against the service identity and workspace
// public keys; claim names, time windows, and default audience must stay in lockstep with
// the control plane implementation (a different repository). When changing this package,
// update the corresponding verifier. See core/docs/http-api.md (section "Workspace service JWT assertions").
package wsservicejwt

import (
	"crypto/ed25519"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// DefaultAudience is the aud claim when the caller passes an empty audience
	// (same default as the heartbeat publisher and account-status client).
	DefaultAudience = "anx-control-plane"

	// AssertionTTL is the validity window from iat/nbf to exp (e.g. 60 seconds).
	AssertionTTL = 60 * time.Second

	// NotBeforeSkew is how far before "now" nbf is set, to tolerate clock skew.
	NotBeforeSkew = 30 * time.Second
)

// Sign creates a signed JWT using the workspace service private key. identityID is used
// for both iss and sub. If audience is empty, DefaultAudience is used. now should be the
// assertion issue time in UTC (callers may pass time.Now().UTC()).
func Sign(identityID string, privateKey ed25519.PrivateKey, audience, workspaceID, purpose string, now time.Time) (string, error) {
	identityID = strings.TrimSpace(identityID)
	workspaceID = strings.TrimSpace(workspaceID)
	purpose = strings.TrimSpace(purpose)
	if identityID == "" {
		return "", fmt.Errorf("wsservicejwt: identity id is required")
	}
	if workspaceID == "" {
		return "", fmt.Errorf("wsservicejwt: workspace_id is required")
	}
	if purpose == "" {
		return "", fmt.Errorf("wsservicejwt: purpose is required")
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("wsservicejwt: private key must be %d bytes", ed25519.PrivateKeySize)
	}
	aud := strings.TrimSpace(audience)
	if aud == "" {
		aud = DefaultAudience
	}
	now = now.UTC()
	claims := jwt.MapClaims{
		"iss":          identityID,
		"sub":          identityID,
		"aud":          aud,
		"iat":          now.Unix(),
		"nbf":          now.Add(-NotBeforeSkew).Unix(),
		"exp":          now.Add(AssertionTTL).Unix(),
		"workspace_id": workspaceID,
		"purpose":      purpose,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	signed, err := token.SignedString(privateKey)
	if err != nil {
		return "", fmt.Errorf("wsservicejwt: sign: %w", err)
	}
	return signed, nil
}
