package wsservicejwt

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestSign_ExpectedClaimsAndEdDSA(t *testing.T) {
	t.Parallel()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)
	tok, err := Sign("svc-1", priv, "anx-control-plane", "ws-9", "heartbeat", now)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	claims := jwt.MapClaims{}
	_, err = jwt.ParseWithClaims(tok, claims, func(token *jwt.Token) (any, error) {
		if token.Method.Alg() != jwt.SigningMethodEdDSA.Alg() {
			t.Fatalf("expected EdDSA, got %s", token.Method.Alg())
		}
		return priv.Public().(ed25519.PublicKey), nil
	}, jwt.WithoutClaimsValidation())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if claims["iss"] != "svc-1" || claims["sub"] != "svc-1" {
		t.Fatalf("iss/sub: %#v", claims)
	}
	if claims["aud"] != "anx-control-plane" {
		t.Fatalf("aud: %v", claims["aud"])
	}
	if claims["workspace_id"] != "ws-9" || claims["purpose"] != "heartbeat" {
		t.Fatalf("custom claims: %#v", claims)
	}
	iat := int64(claims["iat"].(float64))
	exp := int64(claims["exp"].(float64))
	nbf := int64(claims["nbf"].(float64))
	if exp-iat != int64(AssertionTTL.Seconds()) {
		t.Fatalf("exp-iat: got %d want %d", exp-iat, int64(AssertionTTL.Seconds()))
	}
	if iat-nbf != int64(NotBeforeSkew.Seconds()) {
		t.Fatalf("iat-nbf: got %d want %d", iat-nbf, int64(NotBeforeSkew.Seconds()))
	}
}

func TestSign_EmptyAudienceUsesDefault(t *testing.T) {
	t.Parallel()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tok, err := Sign("s", priv, "  ", "w", "p", time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	claims := jwt.MapClaims{}
	_, _ = jwt.ParseWithClaims(tok, claims, func(token *jwt.Token) (any, error) {
		return priv.Public().(ed25519.PublicKey), nil
	}, jwt.WithoutClaimsValidation())
	if claims["aud"] != DefaultAudience {
		t.Fatalf("aud: %v", claims["aud"])
	}
}

func TestSign_ValidationErrors(t *testing.T) {
	t.Parallel()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now().UTC()
	if _, err := Sign("", priv, "", "w", "p", now); err == nil {
		t.Fatal("expected error for empty identity")
	}
	if _, err := Sign("id", priv, "", "", "p", now); err == nil {
		t.Fatal("expected error for empty workspace")
	}
	if _, err := Sign("id", priv, "", "w", "", now); err == nil {
		t.Fatal("expected error for empty purpose")
	}
	bad := make([]byte, ed25519.PrivateKeySize-1)
	if _, err := Sign("id", ed25519.PrivateKey(bad), "", "w", "p", now); err == nil {
		t.Fatal("expected error for bad key size")
	}
}
