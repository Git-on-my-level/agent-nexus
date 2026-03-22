package controlplaneauth

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	HumanAuthModeWorkspaceLocal = "workspace_local"
	HumanAuthModeControlPlane   = "control_plane"

	GrantTypeWorkspaceHuman           = "workspace_human"
	WorkspaceServiceAssertionAudience = "oar-control-plane"
	DefaultClientAssertion            = 5 * time.Minute
)

type WorkspaceHumanClaims struct {
	WorkspaceID    string `json:"workspace_id"`
	OrganizationID string `json:"organization_id,omitempty"`
	Email          string `json:"email,omitempty"`
	DisplayName    string `json:"display_name,omitempty"`
	LaunchID       string `json:"launch_id,omitempty"`
	Scope          string `json:"scope,omitempty"`
	GrantType      string `json:"grant_type,omitempty"`
	jwt.RegisteredClaims
}

type WorkspaceHumanGrantSignerConfig struct {
	Issuer     string
	Audience   string
	PrivateKey ed25519.PrivateKey
	Now        func() time.Time
}

type WorkspaceHumanGrantInput struct {
	AccountID      string
	WorkspaceID    string
	OrganizationID string
	Email          string
	DisplayName    string
	LaunchID       string
	TTL            time.Duration
}

type WorkspaceHumanGrantSigner struct {
	issuer     string
	audience   string
	privateKey ed25519.PrivateKey
	now        func() time.Time
}

type WorkspaceHumanVerifierConfig struct {
	Issuer      string
	Audience    string
	WorkspaceID string
	PublicKey   ed25519.PublicKey
	Now         func() time.Time
}

type WorkspaceHumanIdentity struct {
	Issuer         string
	Subject        string
	Audience       string
	WorkspaceID    string
	OrganizationID string
	Email          string
	DisplayName    string
	LaunchID       string
	Scope          string
	ExpiresAt      string
}

type WorkspaceHumanVerifier struct {
	issuer      string
	audience    string
	workspaceID string
	publicKey   ed25519.PublicKey
	now         func() time.Time
}

type WorkspaceServiceIdentityConfig struct {
	ID         string
	PrivateKey ed25519.PrivateKey
	Now        func() time.Time
}

type WorkspaceServiceIdentity struct {
	id         string
	privateKey ed25519.PrivateKey
	now        func() time.Time
}

type WorkspaceServiceAssertionClaims struct {
	WorkspaceID    string `json:"workspace_id"`
	OrganizationID string `json:"organization_id,omitempty"`
	Purpose        string `json:"purpose,omitempty"`
	jwt.RegisteredClaims
}

type WorkspaceServiceAssertionVerifierConfig struct {
	IdentityID  string
	WorkspaceID string
	Audience    string
	PublicKey   ed25519.PublicKey
	Now         func() time.Time
}

type WorkspaceServiceAssertionIdentity struct {
	Issuer         string
	Subject        string
	Audience       string
	WorkspaceID    string
	OrganizationID string
	Purpose        string
	ExpiresAt      string
}

type WorkspaceServiceAssertionVerifier struct {
	identityID  string
	workspaceID string
	audience    string
	publicKey   ed25519.PublicKey
	now         func() time.Time
}

func NewWorkspaceHumanGrantSigner(config WorkspaceHumanGrantSignerConfig) (*WorkspaceHumanGrantSigner, error) {
	if strings.TrimSpace(config.Issuer) == "" {
		return nil, fmt.Errorf("issuer is required")
	}
	if strings.TrimSpace(config.Audience) == "" {
		return nil, fmt.Errorf("audience is required")
	}
	if len(config.PrivateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("private key must be %d bytes", ed25519.PrivateKeySize)
	}
	nowFn := config.Now
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	return &WorkspaceHumanGrantSigner{
		issuer:     strings.TrimSpace(config.Issuer),
		audience:   strings.TrimSpace(config.Audience),
		privateKey: append(ed25519.PrivateKey(nil), config.PrivateKey...),
		now:        nowFn,
	}, nil
}

func (s *WorkspaceHumanGrantSigner) Sign(input WorkspaceHumanGrantInput) (string, string, error) {
	if s == nil {
		return "", "", fmt.Errorf("workspace human grant signer is not configured")
	}
	accountID := strings.TrimSpace(input.AccountID)
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	if accountID == "" {
		return "", "", fmt.Errorf("account_id is required")
	}
	if workspaceID == "" {
		return "", "", fmt.Errorf("workspace_id is required")
	}
	ttl := input.TTL
	if ttl <= 0 {
		return "", "", fmt.Errorf("ttl must be positive")
	}

	now := s.now().UTC()
	expiresAt := now.Add(ttl)
	claims := WorkspaceHumanClaims{
		WorkspaceID:    workspaceID,
		OrganizationID: strings.TrimSpace(input.OrganizationID),
		Email:          strings.TrimSpace(input.Email),
		DisplayName:    strings.TrimSpace(input.DisplayName),
		LaunchID:       strings.TrimSpace(input.LaunchID),
		Scope:          "workspace:" + workspaceID,
		GrantType:      GrantTypeWorkspaceHuman,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   accountID,
			Audience:  jwt.ClaimStrings{s.audience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-time.Minute)),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	signed, err := token.SignedString(s.privateKey)
	if err != nil {
		return "", "", fmt.Errorf("sign workspace human grant: %w", err)
	}
	return signed, expiresAt.Format(time.RFC3339Nano), nil
}

func NewWorkspaceHumanVerifier(config WorkspaceHumanVerifierConfig) (*WorkspaceHumanVerifier, error) {
	if strings.TrimSpace(config.Issuer) == "" {
		return nil, fmt.Errorf("issuer is required")
	}
	if strings.TrimSpace(config.Audience) == "" {
		return nil, fmt.Errorf("audience is required")
	}
	if strings.TrimSpace(config.WorkspaceID) == "" {
		return nil, fmt.Errorf("workspace_id is required")
	}
	if len(config.PublicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("public key must be %d bytes", ed25519.PublicKeySize)
	}
	nowFn := config.Now
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	return &WorkspaceHumanVerifier{
		issuer:      strings.TrimSpace(config.Issuer),
		audience:    strings.TrimSpace(config.Audience),
		workspaceID: strings.TrimSpace(config.WorkspaceID),
		publicKey:   append(ed25519.PublicKey(nil), config.PublicKey...),
		now:         nowFn,
	}, nil
}

func (v *WorkspaceHumanVerifier) Verify(tokenString string) (WorkspaceHumanIdentity, error) {
	if v == nil {
		return WorkspaceHumanIdentity{}, fmt.Errorf("workspace human verifier is not configured")
	}
	claims := WorkspaceHumanClaims{}
	token, err := jwt.ParseWithClaims(
		strings.TrimSpace(tokenString),
		&claims,
		func(token *jwt.Token) (any, error) {
			if token == nil || token.Method == nil || token.Method.Alg() != jwt.SigningMethodEdDSA.Alg() {
				return nil, fmt.Errorf("unexpected signing method")
			}
			return v.publicKey, nil
		},
		jwt.WithAudience(v.audience),
		jwt.WithIssuer(v.issuer),
		jwt.WithTimeFunc(v.now),
	)
	if err != nil {
		return WorkspaceHumanIdentity{}, fmt.Errorf("verify workspace human token: %w", err)
	}
	if token == nil || !token.Valid {
		return WorkspaceHumanIdentity{}, fmt.Errorf("workspace human token is invalid")
	}
	if strings.TrimSpace(claims.Subject) == "" {
		return WorkspaceHumanIdentity{}, fmt.Errorf("workspace human token is missing subject")
	}
	if strings.TrimSpace(claims.WorkspaceID) != v.workspaceID {
		return WorkspaceHumanIdentity{}, fmt.Errorf("workspace human token is scoped to the wrong workspace")
	}
	if strings.TrimSpace(claims.Scope) != "workspace:"+v.workspaceID {
		return WorkspaceHumanIdentity{}, fmt.Errorf("workspace human token has invalid scope")
	}
	if strings.TrimSpace(claims.GrantType) != GrantTypeWorkspaceHuman {
		return WorkspaceHumanIdentity{}, fmt.Errorf("workspace human token has invalid grant_type")
	}

	expiresAt := ""
	if claims.ExpiresAt != nil {
		expiresAt = claims.ExpiresAt.Time.UTC().Format(time.RFC3339Nano)
	}

	return WorkspaceHumanIdentity{
		Issuer:         claims.Issuer,
		Subject:        claims.Subject,
		Audience:       v.audience,
		WorkspaceID:    claims.WorkspaceID,
		OrganizationID: strings.TrimSpace(claims.OrganizationID),
		Email:          strings.TrimSpace(claims.Email),
		DisplayName:    strings.TrimSpace(claims.DisplayName),
		LaunchID:       strings.TrimSpace(claims.LaunchID),
		Scope:          strings.TrimSpace(claims.Scope),
		ExpiresAt:      expiresAt,
	}, nil
}

func NewWorkspaceServiceIdentity(config WorkspaceServiceIdentityConfig) (*WorkspaceServiceIdentity, error) {
	if strings.TrimSpace(config.ID) == "" {
		return nil, fmt.Errorf("service identity id is required")
	}
	if len(config.PrivateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("private key must be %d bytes", ed25519.PrivateKeySize)
	}
	nowFn := config.Now
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	return &WorkspaceServiceIdentity{
		id:         strings.TrimSpace(config.ID),
		privateKey: append(ed25519.PrivateKey(nil), config.PrivateKey...),
		now:        nowFn,
	}, nil
}

func (i *WorkspaceServiceIdentity) ID() string {
	if i == nil {
		return ""
	}
	return i.id
}

func (i *WorkspaceServiceIdentity) PublicKeyBase64() string {
	if i == nil {
		return ""
	}
	publicKey := i.privateKey.Public().(ed25519.PublicKey)
	return base64.StdEncoding.EncodeToString(publicKey)
}

func (i *WorkspaceServiceIdentity) SignClientAssertion(audience string, ttl time.Duration, extraClaims map[string]any) (string, string, error) {
	if i == nil {
		return "", "", fmt.Errorf("workspace service identity is not configured")
	}
	audience = strings.TrimSpace(audience)
	if audience == "" {
		return "", "", fmt.Errorf("audience is required")
	}
	if ttl <= 0 {
		ttl = DefaultClientAssertion
	}

	now := i.now().UTC()
	expiresAt := now.Add(ttl)
	claims := jwt.MapClaims{
		"iss": i.id,
		"sub": i.id,
		"aud": audience,
		"iat": now.Unix(),
		"nbf": now.Add(-time.Minute).Unix(),
		"exp": expiresAt.Unix(),
	}
	for key, value := range extraClaims {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		claims[key] = value
	}

	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	signed, err := token.SignedString(i.privateKey)
	if err != nil {
		return "", "", fmt.Errorf("sign client assertion: %w", err)
	}
	return signed, expiresAt.Format(time.RFC3339Nano), nil
}

func NewWorkspaceServiceAssertionVerifier(config WorkspaceServiceAssertionVerifierConfig) (*WorkspaceServiceAssertionVerifier, error) {
	if strings.TrimSpace(config.IdentityID) == "" {
		return nil, fmt.Errorf("identity_id is required")
	}
	if strings.TrimSpace(config.WorkspaceID) == "" {
		return nil, fmt.Errorf("workspace_id is required")
	}
	if strings.TrimSpace(config.Audience) == "" {
		return nil, fmt.Errorf("audience is required")
	}
	if len(config.PublicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("public key must be %d bytes", ed25519.PublicKeySize)
	}
	nowFn := config.Now
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	return &WorkspaceServiceAssertionVerifier{
		identityID:  strings.TrimSpace(config.IdentityID),
		workspaceID: strings.TrimSpace(config.WorkspaceID),
		audience:    strings.TrimSpace(config.Audience),
		publicKey:   append(ed25519.PublicKey(nil), config.PublicKey...),
		now:         nowFn,
	}, nil
}

func (v *WorkspaceServiceAssertionVerifier) Verify(tokenString string) (WorkspaceServiceAssertionIdentity, error) {
	if v == nil {
		return WorkspaceServiceAssertionIdentity{}, fmt.Errorf("workspace service verifier is not configured")
	}
	claims := WorkspaceServiceAssertionClaims{}
	token, err := jwt.ParseWithClaims(
		strings.TrimSpace(tokenString),
		&claims,
		func(token *jwt.Token) (any, error) {
			if token == nil || token.Method == nil || token.Method.Alg() != jwt.SigningMethodEdDSA.Alg() {
				return nil, fmt.Errorf("unexpected signing method")
			}
			return v.publicKey, nil
		},
		jwt.WithAudience(v.audience),
		jwt.WithIssuer(v.identityID),
		jwt.WithTimeFunc(v.now),
	)
	if err != nil {
		return WorkspaceServiceAssertionIdentity{}, fmt.Errorf("verify workspace service assertion: %w", err)
	}
	if token == nil || !token.Valid {
		return WorkspaceServiceAssertionIdentity{}, fmt.Errorf("workspace service assertion is invalid")
	}
	if strings.TrimSpace(claims.Subject) != v.identityID {
		return WorkspaceServiceAssertionIdentity{}, fmt.Errorf("workspace service assertion has the wrong subject")
	}
	if strings.TrimSpace(claims.WorkspaceID) != v.workspaceID {
		return WorkspaceServiceAssertionIdentity{}, fmt.Errorf("workspace service assertion is scoped to the wrong workspace")
	}
	if strings.TrimSpace(claims.Purpose) != "heartbeat" {
		return WorkspaceServiceAssertionIdentity{}, fmt.Errorf("workspace service assertion has invalid purpose")
	}

	expiresAt := ""
	if claims.ExpiresAt != nil {
		expiresAt = claims.ExpiresAt.Time.UTC().Format(time.RFC3339Nano)
	}

	return WorkspaceServiceAssertionIdentity{
		Issuer:         claims.Issuer,
		Subject:        claims.Subject,
		Audience:       v.audience,
		WorkspaceID:    claims.WorkspaceID,
		OrganizationID: strings.TrimSpace(claims.OrganizationID),
		Purpose:        strings.TrimSpace(claims.Purpose),
		ExpiresAt:      expiresAt,
	}, nil
}

func ParseEd25519PublicKeyBase64(raw string) (ed25519.PublicKey, error) {
	decoded, err := decodeBase64(raw)
	if err != nil {
		return nil, err
	}
	if len(decoded) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("unexpected public key length %d", len(decoded))
	}
	return ed25519.PublicKey(decoded), nil
}

func ParseEd25519PrivateKeyBase64(raw string) (ed25519.PrivateKey, error) {
	decoded, err := decodeBase64(raw)
	if err != nil {
		return nil, err
	}
	if len(decoded) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("unexpected private key length %d", len(decoded))
	}
	return ed25519.PrivateKey(decoded), nil
}

func decodeBase64(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("value is required")
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err == nil {
		return decoded, nil
	}
	decoded, err = base64.RawStdEncoding.DecodeString(raw)
	if err == nil {
		return decoded, nil
	}
	decoded, err = base64.URLEncoding.DecodeString(raw)
	if err == nil {
		return decoded, nil
	}
	decoded, err = base64.RawURLEncoding.DecodeString(raw)
	if err == nil {
		return decoded, nil
	}
	return nil, fmt.Errorf("decode base64 value: %w", err)
}
