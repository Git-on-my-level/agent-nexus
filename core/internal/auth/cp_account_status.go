package auth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Account status cache: fresh TTL 5m; fail-open allowed when last successful check
// was active and age < 10m (2× fresh TTL); beyond that, CP failures fail closed.
const (
	accountStatusCacheFreshTTL   = 5 * time.Minute
	accountStatusCacheMaxStale   = 2 * accountStatusCacheFreshTTL
	accountStatusHTTPTimeout     = 3 * time.Second
	accountStatusAssertionTTL    = 60 * time.Second
	accountStatusPurpose         = "account_status_check"
	defaultAccountStatusAudience = "anx-control-plane"
)

// ErrAccountInactive is returned by AccountStatusChecker when the control plane
// reports the account is not active.
var ErrAccountInactive = errors.New("auth: account inactive on control plane")

// AccountStatusChecker is implemented by the hosted control plane HTTP client.
// OSS/local mode passes a nil checker.
type AccountStatusChecker interface {
	CheckActive(ctx context.Context, accountID string) (active bool, err error)
}

type accountStatusCacheEntry struct {
	active    bool
	checkedAt time.Time
}

// WorkspaceServiceAssertionSigner signs short-lived workspace→CP JWT assertions.
type WorkspaceServiceAssertionSigner interface {
	Sign(ctx context.Context) (jwt string, err error)
}

type ed25519WorkspaceServiceSigner struct {
	identityID  string
	privateKey  ed25519.PrivateKey
	audience    string
	workspaceID string
	now         func() time.Time
}

// NewEd25519WorkspaceServiceAssertionSigner builds a signer compatible with the
// heartbeat publisher JWT shape (iss/sub/aud/workspace_id/purpose).
func NewEd25519WorkspaceServiceAssertionSigner(identityID string, privateKey ed25519.PrivateKey, audience, workspaceID string) WorkspaceServiceAssertionSigner {
	if strings.TrimSpace(audience) == "" {
		audience = defaultAccountStatusAudience
	}
	return &ed25519WorkspaceServiceSigner{
		identityID:  strings.TrimSpace(identityID),
		privateKey:  privateKey,
		audience:    audience,
		workspaceID: strings.TrimSpace(workspaceID),
		now:         time.Now,
	}
}

func (s *ed25519WorkspaceServiceSigner) Sign(ctx context.Context) (string, error) {
	if ctx != nil && ctx.Err() != nil {
		return "", ctx.Err()
	}
	if s == nil || len(s.privateKey) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("service assertion signer: invalid private key")
	}
	if strings.TrimSpace(s.identityID) == "" || strings.TrimSpace(s.workspaceID) == "" {
		return "", fmt.Errorf("service assertion signer: identity and workspace_id are required")
	}
	nowFn := s.now
	if nowFn == nil {
		nowFn = time.Now
	}
	now := nowFn().UTC()
	claims := jwt.MapClaims{
		"iss":          s.identityID,
		"sub":          s.identityID,
		"aud":          s.audience,
		"iat":          now.Unix(),
		"nbf":          now.Add(-30 * time.Second).Unix(),
		"exp":          now.Add(accountStatusAssertionTTL).Unix(),
		"workspace_id": s.workspaceID,
		"purpose":      accountStatusPurpose,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	signed, err := token.SignedString(s.privateKey)
	if err != nil {
		return "", fmt.Errorf("sign service assertion: %w", err)
	}
	return signed, nil
}

// HTTPAccountStatusCheckerConfig configures the control plane account status client.
type HTTPAccountStatusCheckerConfig struct {
	BaseURL     string
	WorkspaceID string
	HTTPClient  *http.Client
	Signer      WorkspaceServiceAssertionSigner
	Now         func() time.Time
	Logf        func(format string, args ...any)
}

// HTTPAccountStatusChecker calls POST /v1/internal/accounts/status on the control plane.
type HTTPAccountStatusChecker struct {
	baseURL     string
	workspaceID string
	httpClient  *http.Client
	signer      WorkspaceServiceAssertionSigner
	now         func() time.Time
	logf        func(format string, args ...any)

	cache sync.Map // accountID -> accountStatusCacheEntry
}

// NewHTTPAccountStatusChecker constructs a cached CP account status checker.
func NewHTTPAccountStatusChecker(cfg HTTPAccountStatusCheckerConfig) (*HTTPAccountStatusChecker, error) {
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		return nil, fmt.Errorf("control plane base URL is required")
	}
	if _, err := url.Parse(base); err != nil {
		return nil, fmt.Errorf("parse control plane base URL: %w", err)
	}
	ws := strings.TrimSpace(cfg.WorkspaceID)
	if ws == "" {
		return nil, fmt.Errorf("workspace_id is required")
	}
	if cfg.Signer == nil {
		return nil, fmt.Errorf("service assertion signer is required")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: accountStatusHTTPTimeout}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	logf := cfg.Logf
	if logf == nil {
		logf = log.Printf
	}
	u, err := url.JoinPath(strings.TrimRight(base, "/"), "v1", "internal", "accounts", "status")
	if err != nil {
		return nil, fmt.Errorf("build account status URL: %w", err)
	}
	return &HTTPAccountStatusChecker{
		baseURL:     u,
		workspaceID: ws,
		httpClient:  client,
		signer:      cfg.Signer,
		now:         now,
		logf:        logf,
	}, nil
}

// CheckActive implements AccountStatusChecker.
func (c *HTTPAccountStatusChecker) CheckActive(ctx context.Context, accountID string) (bool, error) {
	if c == nil {
		return true, nil
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return false, fmt.Errorf("account id is required")
	}
	now := c.now().UTC()

	if raw, ok := c.cache.Load(accountID); ok {
		entry := raw.(accountStatusCacheEntry)
		age := now.Sub(entry.checkedAt)
		if age < accountStatusCacheFreshTTL {
			if !entry.active {
				c.logSessionExchange(accountID, "account_disabled")
				return false, ErrAccountInactive
			}
			c.logSessionExchange(accountID, "ok")
			return true, nil
		}
	}

	raw, hadCache := c.cache.Load(accountID)
	var prevEntry accountStatusCacheEntry
	if hadCache {
		prevEntry = raw.(accountStatusCacheEntry)
	}

	active, err := c.fetchActiveFromCP(ctx, accountID)
	if err == nil {
		if !active {
			c.cache.Store(accountID, accountStatusCacheEntry{active: false, checkedAt: now})
			c.logSessionExchange(accountID, "account_disabled")
			return false, ErrAccountInactive
		}
		c.cache.Store(accountID, accountStatusCacheEntry{active: true, checkedAt: now})
		c.logSessionExchange(accountID, "ok")
		return true, nil
	}

	if errors.Is(err, ErrCPUnreachable) {
		if hadCache {
			age := now.Sub(prevEntry.checkedAt)
			if age < accountStatusCacheMaxStale {
				if prevEntry.active {
					c.logf("event=session_exchange account_id=%s workspace_id=%s outcome=cp_unreachable (fail_open_stale_cache age=%s)", accountID, c.workspaceID, age.String())
					c.logSessionExchange(accountID, "ok")
					return true, nil
				}
				c.logSessionExchange(accountID, "account_disabled")
				return false, ErrAccountInactive
			}
		}
		c.logSessionExchange(accountID, "cp_unreachable")
		return false, ErrCPUnreachable
	}

	return false, err
}

func (c *HTTPAccountStatusChecker) logSessionExchange(accountID, outcome string) {
	if c == nil {
		return
	}
	c.logf("event=session_exchange account_id=%s workspace_id=%s outcome=%s", accountID, c.workspaceID, outcome)
}

func (c *HTTPAccountStatusChecker) fetchActiveFromCP(ctx context.Context, accountID string) (bool, error) {
	token, err := c.signer.Sign(ctx)
	if err != nil {
		return false, err
	}
	body, err := json.Marshal(map[string]string{
		"sub":          accountID,
		"workspace_id": c.workspaceID,
	})
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrCPUnreachable, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode == http.StatusOK {
		var parsed struct {
			Active bool `json:"active"`
		}
		if err := json.Unmarshal(respBody, &parsed); err != nil {
			return false, fmt.Errorf("parse account status response: %w", err)
		}
		return parsed.Active, nil
	}

	if resp.StatusCode >= 500 {
		return false, fmt.Errorf("%w: control plane returned %s", ErrCPUnreachable, resp.Status)
	}

	msg := strings.TrimSpace(string(respBody))
	if msg == "" {
		msg = resp.Status
	}
	return false, fmt.Errorf("account status request rejected: %s: %s", resp.Status, msg)
}

func shouldCheckHostedHumanAccountStatus(principalKind, authMethod string) bool {
	if strings.TrimSpace(principalKind) != string(PrincipalKindHuman) {
		return false
	}
	m := strings.TrimSpace(authMethod)
	return m == AuthMethodExternalGrant || m == "control_plane"
}
