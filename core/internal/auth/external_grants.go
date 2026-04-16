package auth

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var ErrExternalGrantInvalid = errors.New("external_grant_invalid")
var ErrExternalGrantUnavailable = errors.New("external_grant_unavailable")
var ErrExternalGrantReplay = errors.New("external_grant_replay")

const (
	HumanAuthModeWorkspaceLocal = "workspace_local"
	HumanAuthModeExternalGrant  = "external_grant"

	GrantTypeWorkspaceHuman      = "workspace_human"
	TokenGrantTypeWorkspaceHuman = "workspace_human_grant"

	DefaultWorkspaceHumanGrantTTL                = 5 * time.Minute
	DefaultWorkspaceHumanGrantLeeway             = 2 * time.Minute
	DefaultConsumedGrantJTIRetention             = DefaultWorkspaceHumanGrantTTL + DefaultWorkspaceHumanGrantLeeway
	defaultWorkspaceHumanGrantJWKSCacheTTL       = time.Hour
	defaultWorkspaceHumanGrantJWKSSWR            = time.Hour
	defaultWorkspaceHumanGrantUnknownKidCooldown = time.Minute
	defaultWorkspaceHumanGrantJWKSHTTPTimeout    = 5 * time.Second
)

type WorkspaceHumanGrantIdentityVerifier interface {
	Verify(ctx context.Context, assertion string) (WorkspaceHumanGrantIdentity, error)
}

type WorkspaceHumanGrantClaims struct {
	WorkspaceID string `json:"workspace_id"`
	Email       string `json:"email,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Scope       string `json:"scope,omitempty"`
	GrantType   string `json:"grant_type,omitempty"`
	jwt.RegisteredClaims
}

type WorkspaceHumanGrantIdentity struct {
	Issuer      string
	Subject     string
	Audience    string
	WorkspaceID string
	Email       string
	DisplayName string
	Scope       string
	GrantType   string
	JTI         string
	ExpiresAt   string
}

type WorkspaceHumanGrantVerifierConfig struct {
	Issuer      string
	Audience    string
	WorkspaceID string
	Leeway      time.Duration
	Now         func() time.Time
	Resolver    *WorkspaceHumanGrantJWKResolver
}

type WorkspaceHumanGrantVerifier struct {
	issuer      string
	audience    string
	workspaceID string
	leeway      time.Duration
	now         func() time.Time
	resolver    *WorkspaceHumanGrantJWKResolver
}

type WorkspaceHumanGrantJWKResolverConfig struct {
	JWKSURL                   string
	CacheTTL                  time.Duration
	StaleWhileRevalidate      time.Duration
	UnknownKidRefreshCooldown time.Duration
	HTTPClient                *http.Client
	Now                       func() time.Time
}

type WorkspaceHumanGrantJWKResolver struct {
	jwksURL                   string
	cacheTTL                  time.Duration
	staleWhileRevalidate      time.Duration
	unknownKidRefreshCooldown time.Duration
	httpClient                *http.Client
	now                       func() time.Time

	mu                        sync.Mutex
	keys                      map[string]ed25519.PublicKey
	fetchedAt                 time.Time
	lastUnknownKidRefreshTry  time.Time
	lastUnknownKidRefreshErr  bool
	unknownKidRefreshInFlight bool
}

type workspaceHumanGrantJWKSResponse struct {
	Keys []workspaceHumanGrantJWK `json:"keys"`
}

type workspaceHumanGrantJWK struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Use string `json:"use,omitempty"`
	Alg string `json:"alg,omitempty"`
}

func WorkspaceHumanGrantJWKSURL(issuer string) (string, error) {
	issuer = strings.TrimSpace(issuer)
	if issuer == "" {
		return "", fmt.Errorf("%w: issuer is required", ErrExternalGrantInvalid)
	}
	parsed, err := url.Parse(issuer)
	if err != nil {
		return "", fmt.Errorf("%w: parse issuer: %v", ErrExternalGrantInvalid, err)
	}
	if strings.TrimSpace(parsed.Scheme) == "" || strings.TrimSpace(parsed.Host) == "" {
		return "", fmt.Errorf("%w: issuer must include scheme and host", ErrExternalGrantInvalid)
	}
	joined, err := url.JoinPath(strings.TrimRight(issuer, "/"), ".well-known", "jwks.json")
	if err != nil {
		return "", fmt.Errorf("%w: derive jwks url: %v", ErrExternalGrantInvalid, err)
	}
	return joined, nil
}

func NewWorkspaceHumanGrantJWKResolver(config WorkspaceHumanGrantJWKResolverConfig) (*WorkspaceHumanGrantJWKResolver, error) {
	jwksURL := strings.TrimSpace(config.JWKSURL)
	if jwksURL == "" {
		return nil, fmt.Errorf("jwks_url is required")
	}
	parsedURL, err := url.Parse(jwksURL)
	if err != nil {
		return nil, fmt.Errorf("parse jwks_url: %w", err)
	}
	if strings.TrimSpace(parsedURL.Scheme) == "" || strings.TrimSpace(parsedURL.Host) == "" {
		return nil, fmt.Errorf("jwks_url must include scheme and host")
	}
	cacheTTL := config.CacheTTL
	if cacheTTL <= 0 {
		cacheTTL = defaultWorkspaceHumanGrantJWKSCacheTTL
	}
	staleWhileRevalidate := config.StaleWhileRevalidate
	if staleWhileRevalidate < 0 {
		staleWhileRevalidate = 0
	}
	if staleWhileRevalidate == 0 {
		staleWhileRevalidate = defaultWorkspaceHumanGrantJWKSSWR
	}
	unknownKidRefreshCooldown := config.UnknownKidRefreshCooldown
	if unknownKidRefreshCooldown <= 0 {
		unknownKidRefreshCooldown = defaultWorkspaceHumanGrantUnknownKidCooldown
	}
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultWorkspaceHumanGrantJWKSHTTPTimeout}
	} else if httpClient.Timeout <= 0 {
		clone := *httpClient
		clone.Timeout = defaultWorkspaceHumanGrantJWKSHTTPTimeout
		httpClient = &clone
	}
	nowFn := config.Now
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	return &WorkspaceHumanGrantJWKResolver{
		jwksURL:                   jwksURL,
		cacheTTL:                  cacheTTL,
		staleWhileRevalidate:      staleWhileRevalidate,
		unknownKidRefreshCooldown: unknownKidRefreshCooldown,
		httpClient:                httpClient,
		now:                       nowFn,
	}, nil
}

func NewWorkspaceHumanGrantVerifier(config WorkspaceHumanGrantVerifierConfig) (*WorkspaceHumanGrantVerifier, error) {
	if strings.TrimSpace(config.Issuer) == "" {
		return nil, fmt.Errorf("issuer is required")
	}
	if strings.TrimSpace(config.Audience) == "" {
		return nil, fmt.Errorf("audience is required")
	}
	if strings.TrimSpace(config.WorkspaceID) == "" {
		return nil, fmt.Errorf("workspace_id is required")
	}
	if config.Resolver == nil {
		return nil, fmt.Errorf("resolver is required")
	}
	leeway := config.Leeway
	if leeway <= 0 {
		leeway = DefaultWorkspaceHumanGrantLeeway
	}
	nowFn := config.Now
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	return &WorkspaceHumanGrantVerifier{
		issuer:      strings.TrimSpace(config.Issuer),
		audience:    strings.TrimSpace(config.Audience),
		workspaceID: strings.TrimSpace(config.WorkspaceID),
		leeway:      leeway,
		now:         nowFn,
		resolver:    config.Resolver,
	}, nil
}

func (v *WorkspaceHumanGrantVerifier) Verify(ctx context.Context, assertion string) (WorkspaceHumanGrantIdentity, error) {
	if v == nil {
		return WorkspaceHumanGrantIdentity{}, fmt.Errorf("%w: verifier is not configured", ErrExternalGrantUnavailable)
	}
	assertion = strings.TrimSpace(assertion)
	if assertion == "" {
		return WorkspaceHumanGrantIdentity{}, fmt.Errorf("%w: assertion is required", ErrExternalGrantInvalid)
	}

	claims := WorkspaceHumanGrantClaims{}
	token, err := jwt.ParseWithClaims(
		assertion,
		&claims,
		func(token *jwt.Token) (any, error) {
			if token == nil || token.Method == nil || token.Method.Alg() != jwt.SigningMethodEdDSA.Alg() {
				return nil, fmt.Errorf("%w: unexpected signing method", ErrExternalGrantInvalid)
			}
			kid, _ := token.Header["kid"].(string)
			key, resolveErr := v.resolver.Resolve(ctx, strings.TrimSpace(kid))
			if resolveErr != nil {
				return nil, resolveErr
			}
			return key, nil
		},
		jwt.WithAudience(v.audience),
		jwt.WithIssuer(v.issuer),
		jwt.WithLeeway(v.leeway),
		jwt.WithTimeFunc(v.now),
	)
	if err != nil {
		if errors.Is(err, ErrExternalGrantUnavailable) {
			return WorkspaceHumanGrantIdentity{}, fmt.Errorf("%w: verify workspace grant: %v", ErrExternalGrantUnavailable, err)
		}
		return WorkspaceHumanGrantIdentity{}, fmt.Errorf("%w: verify workspace grant: %v", ErrExternalGrantInvalid, err)
	}
	if token == nil || !token.Valid {
		return WorkspaceHumanGrantIdentity{}, fmt.Errorf("%w: workspace grant is invalid", ErrExternalGrantInvalid)
	}
	if claims.ExpiresAt == nil {
		return WorkspaceHumanGrantIdentity{}, fmt.Errorf("%w: exp is required", ErrExternalGrantInvalid)
	}
	if strings.TrimSpace(claims.Subject) == "" {
		return WorkspaceHumanGrantIdentity{}, fmt.Errorf("%w: subject is required", ErrExternalGrantInvalid)
	}
	if strings.TrimSpace(claims.ID) == "" {
		return WorkspaceHumanGrantIdentity{}, fmt.Errorf("%w: jti is required", ErrExternalGrantInvalid)
	}
	if strings.TrimSpace(claims.WorkspaceID) != v.workspaceID {
		return WorkspaceHumanGrantIdentity{}, fmt.Errorf("%w: workspace_id is invalid", ErrExternalGrantInvalid)
	}
	if strings.TrimSpace(claims.Scope) != "workspace:"+v.workspaceID {
		return WorkspaceHumanGrantIdentity{}, fmt.Errorf("%w: scope is invalid", ErrExternalGrantInvalid)
	}
	if strings.TrimSpace(claims.GrantType) != GrantTypeWorkspaceHuman {
		return WorkspaceHumanGrantIdentity{}, fmt.Errorf("%w: grant_type is invalid", ErrExternalGrantInvalid)
	}

	expiresAt := ""
	if claims.ExpiresAt != nil {
		expiresAt = claims.ExpiresAt.Time.UTC().Format(time.RFC3339Nano)
	}
	return WorkspaceHumanGrantIdentity{
		Issuer:      strings.TrimSpace(claims.Issuer),
		Subject:     strings.TrimSpace(claims.Subject),
		Audience:    v.audience,
		WorkspaceID: strings.TrimSpace(claims.WorkspaceID),
		Email:       strings.TrimSpace(claims.Email),
		DisplayName: strings.TrimSpace(claims.DisplayName),
		Scope:       strings.TrimSpace(claims.Scope),
		GrantType:   strings.TrimSpace(claims.GrantType),
		JTI:         strings.TrimSpace(claims.ID),
		ExpiresAt:   expiresAt,
	}, nil
}

func (r *WorkspaceHumanGrantJWKResolver) Resolve(ctx context.Context, kid string) (ed25519.PublicKey, error) {
	if r == nil {
		return nil, fmt.Errorf("%w: jwks resolver is not configured", ErrExternalGrantUnavailable)
	}
	kid = strings.TrimSpace(kid)
	if kid == "" {
		return nil, fmt.Errorf("%w: token header kid is required", ErrExternalGrantInvalid)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	now := r.now().UTC()
	cachedKeys, fetchedAt, hasCache := r.cachedSnapshot()
	if !hasCache {
		fetched, fetchErr := r.fetchAndStore(ctx, now)
		if fetchErr != nil {
			return nil, fmt.Errorf("%w: fetch jwks: %v", ErrExternalGrantUnavailable, fetchErr)
		}
		if key, ok := fetched[kid]; ok {
			return append(ed25519.PublicKey(nil), key...), nil
		}
		return nil, fmt.Errorf("%w: kid %q not found", ErrExternalGrantInvalid, kid)
	}

	cacheAge := now.Sub(fetchedAt)
	if cacheAge <= r.cacheTTL {
		if key, ok := cachedKeys[kid]; ok {
			return append(ed25519.PublicKey(nil), key...), nil
		}
		shouldRefresh, hadRecentRefreshFailure := r.shouldRefreshUnknownKid(now)
		if shouldRefresh {
			fetched, fetchErr := r.fetchAndStore(ctx, now)
			if fetchErr != nil {
				r.markUnknownKidRefreshResult(true)
				return nil, fmt.Errorf("%w: refresh jwks for unknown kid: %v", ErrExternalGrantUnavailable, fetchErr)
			}
			r.markUnknownKidRefreshResult(false)
			if key, ok := fetched[kid]; ok {
				return append(ed25519.PublicKey(nil), key...), nil
			}
		} else if hadRecentRefreshFailure {
			return nil, fmt.Errorf("%w: jwks refresh for unknown kid is in cooldown after failure", ErrExternalGrantUnavailable)
		}
		return nil, fmt.Errorf("%w: kid %q not found", ErrExternalGrantInvalid, kid)
	}

	if cacheAge <= r.cacheTTL+r.staleWhileRevalidate {
		if fetched, fetchErr := r.fetchAndStore(ctx, now); fetchErr == nil {
			if key, ok := fetched[kid]; ok {
				return append(ed25519.PublicKey(nil), key...), nil
			}
			return nil, fmt.Errorf("%w: kid %q not found", ErrExternalGrantInvalid, kid)
		} else {
			if _, ok := cachedKeys[kid]; ok {
				return append(ed25519.PublicKey(nil), cachedKeys[kid]...), nil
			}
			return nil, fmt.Errorf("%w: refresh jwks: %v", ErrExternalGrantUnavailable, fetchErr)
		}
	}

	fetched, fetchErr := r.fetchAndStore(ctx, now)
	if fetchErr != nil {
		return nil, fmt.Errorf("%w: refresh jwks: %v", ErrExternalGrantUnavailable, fetchErr)
	}
	if key, ok := fetched[kid]; ok {
		return append(ed25519.PublicKey(nil), key...), nil
	}
	return nil, fmt.Errorf("%w: kid %q not found", ErrExternalGrantInvalid, kid)
}

func (r *WorkspaceHumanGrantJWKResolver) shouldRefreshUnknownKid(now time.Time) (bool, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.unknownKidRefreshInFlight {
		return false, true
	}
	if !r.lastUnknownKidRefreshTry.IsZero() && now.Sub(r.lastUnknownKidRefreshTry) < r.unknownKidRefreshCooldown {
		return false, r.lastUnknownKidRefreshErr
	}
	r.lastUnknownKidRefreshTry = now
	r.lastUnknownKidRefreshErr = false
	r.unknownKidRefreshInFlight = true
	return true, false
}

func (r *WorkspaceHumanGrantJWKResolver) markUnknownKidRefreshResult(failed bool) {
	r.mu.Lock()
	r.lastUnknownKidRefreshErr = failed
	r.unknownKidRefreshInFlight = false
	r.mu.Unlock()
}

func (r *WorkspaceHumanGrantJWKResolver) cachedSnapshot() (map[string]ed25519.PublicKey, time.Time, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.keys) == 0 {
		return nil, time.Time{}, false
	}
	keys := make(map[string]ed25519.PublicKey, len(r.keys))
	for kid, key := range r.keys {
		keys[kid] = append(ed25519.PublicKey(nil), key...)
	}
	return keys, r.fetchedAt, true
}

func (r *WorkspaceHumanGrantJWKResolver) fetchAndStore(ctx context.Context, now time.Time) (map[string]ed25519.PublicKey, error) {
	keys, err := r.fetchJWKS(ctx)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.keys = keys
	r.fetchedAt = now
	r.mu.Unlock()

	copied := make(map[string]ed25519.PublicKey, len(keys))
	for kid, key := range keys {
		copied[kid] = append(ed25519.PublicKey(nil), key...)
	}
	return copied, nil
}

func (r *WorkspaceHumanGrantJWKResolver) fetchJWKS(ctx context.Context) (map[string]ed25519.PublicKey, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, r.jwksURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build jwks request: %w", err)
	}
	request.Header.Set("Accept", "application/json")

	response, err := r.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request jwks: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request jwks: unexpected status %d", response.StatusCode)
	}

	var payload workspaceHumanGrantJWKSResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode jwks payload: %w", err)
	}

	keys := make(map[string]ed25519.PublicKey)
	for _, item := range payload.Keys {
		kid := strings.TrimSpace(item.Kid)
		if kid == "" {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(item.Kty), "OKP") {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(item.Crv), "Ed25519") {
			continue
		}
		if use := strings.TrimSpace(item.Use); use != "" && !strings.EqualFold(use, "sig") {
			continue
		}
		if alg := strings.TrimSpace(item.Alg); alg != "" && !strings.EqualFold(alg, "EdDSA") {
			continue
		}
		decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(item.X))
		if err != nil {
			decoded, err = decodeBase64(strings.TrimSpace(item.X))
			if err != nil {
				continue
			}
		}
		if len(decoded) != ed25519.PublicKeySize {
			continue
		}
		keys[kid] = append(ed25519.PublicKey(nil), decoded...)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("jwks payload does not contain any Ed25519 signing keys")
	}
	return keys, nil
}
