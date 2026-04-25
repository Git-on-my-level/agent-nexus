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
	"os"
	"strings"
	"sync"
	"time"

	"agent-nexus-core/internal/wsservicejwt"
)

// Account status cache: fresh TTL 5m; fail-open allowed when last successful check
// was active and age < 10m (2× fresh TTL); beyond that, remote failures fail closed.
const (
	accountStatusCacheFreshTTL = 5 * time.Minute
	accountStatusCacheMaxStale = 2 * accountStatusCacheFreshTTL
	accountStatusHTTPTimeout   = 3 * time.Second
	accountStatusPurpose       = "account_status_check"

	// defaultAccountStatusAudience is the default JWT aud claim when the signer is
	// constructed without an explicit audience. Deployments normally override this
	// via ANX_ACCOUNT_STATUS_AUDIENCE or by passing a non-empty audience to
	// NewEd25519WorkspaceServiceAssertionSigner.
	defaultAccountStatusAudience = "anx-control-plane"

	// DefaultAccountStatusAudience matches the default used by
	// NewEd25519WorkspaceServiceAssertionSigner when audience is empty.
	DefaultAccountStatusAudience = defaultAccountStatusAudience

	defaultAccountStatusEndpointPath = "v1/internal/accounts/status"
)

// AccountStatusBaseURLFromEnv returns the HTTP base URL for optional account status checks
// from ANX_ACCOUNT_STATUS_URL. Optional path/audience are ANX_ACCOUNT_STATUS_PATH and
// ANX_ACCOUNT_STATUS_AUDIENCE (see HTTPAccountStatusChecker / main wiring).
func AccountStatusBaseURLFromEnv() string {
	return strings.TrimSpace(os.Getenv("ANX_ACCOUNT_STATUS_URL"))
}

// ErrAccountInactive is returned by AccountStatusChecker when the account status
// endpoint reports the account is not active.
var ErrAccountInactive = errors.New("auth: account inactive (account status check)")

// AccountStatusChecker is implemented by the optional HTTP account-status client.
// OSS/local mode passes a nil checker.
type AccountStatusChecker interface {
	CheckActive(ctx context.Context, accountID string) (active bool, err error)
}

type accountStatusCacheEntry struct {
	active    bool
	checkedAt time.Time
}

// WorkspaceServiceAssertionSigner signs short-lived workspace service JWT assertions.
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
	return wsservicejwt.Sign(s.identityID, s.privateKey, s.audience, s.workspaceID, accountStatusPurpose, now)
}

// HTTPAccountStatusCheckerConfig configures the HTTP account status client.
type HTTPAccountStatusCheckerConfig struct {
	BaseURL      string
	EndpointPath string
	WorkspaceID  string
	HTTPClient   *http.Client
	Signer       WorkspaceServiceAssertionSigner
	Now          func() time.Time
	Logf         func(format string, args ...any)
}

// HTTPAccountStatusChecker calls POST {BaseURL}/{EndpointPath} (see config).
type HTTPAccountStatusChecker struct {
	baseURL     string
	workspaceID string
	httpClient  *http.Client
	signer      WorkspaceServiceAssertionSigner
	now         func() time.Time
	logf        func(format string, args ...any)

	cache sync.Map // accountID -> accountStatusCacheEntry
}

// NewHTTPAccountStatusChecker constructs a cached HTTP account status checker.
func NewHTTPAccountStatusChecker(cfg HTTPAccountStatusCheckerConfig) (*HTTPAccountStatusChecker, error) {
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		return nil, fmt.Errorf("account status base URL is required")
	}
	if _, err := url.Parse(base); err != nil {
		return nil, fmt.Errorf("parse account status base URL: %w", err)
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
	endpointPath := strings.TrimSpace(cfg.EndpointPath)
	if endpointPath == "" {
		endpointPath = defaultAccountStatusEndpointPath
	}
	u, err := joinBaseURLPath(base, endpointPath)
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

func joinBaseURLPath(base, relPath string) (string, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	relPath = strings.Trim(relPath, "/")
	if relPath == "" {
		return url.JoinPath(base)
	}
	segments := strings.Split(relPath, "/")
	rest := make([]string, 0, len(segments))
	for _, seg := range segments {
		if seg != "" {
			rest = append(rest, seg)
		}
	}
	if len(rest) == 0 {
		return url.JoinPath(base)
	}
	return url.JoinPath(base, rest...)
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

	active, err := c.fetchActiveRemote(ctx, accountID)
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

	if errors.Is(err, ErrAccountStatusUnreachable) {
		if hadCache {
			age := now.Sub(prevEntry.checkedAt)
			if age < accountStatusCacheMaxStale {
				if prevEntry.active {
					c.logf("event=session_exchange account_id=%s workspace_id=%s outcome=account_status_unreachable (fail_open_stale_cache age=%s)", accountID, c.workspaceID, age.String())
					c.logSessionExchange(accountID, "ok")
					return true, nil
				}
				c.logSessionExchange(accountID, "account_disabled")
				return false, ErrAccountInactive
			}
		}
		c.logSessionExchange(accountID, "account_status_unreachable")
		return false, ErrAccountStatusUnreachable
	}

	return false, err
}

func (c *HTTPAccountStatusChecker) logSessionExchange(accountID, outcome string) {
	if c == nil {
		return
	}
	c.logf("event=session_exchange account_id=%s workspace_id=%s outcome=%s", accountID, c.workspaceID, outcome)
}

func (c *HTTPAccountStatusChecker) fetchActiveRemote(ctx context.Context, accountID string) (bool, error) {
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
		return false, fmt.Errorf("%w: %v", ErrAccountStatusUnreachable, err)
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
		return false, fmt.Errorf("%w: account status HTTP API returned %s", ErrAccountStatusUnreachable, resp.Status)
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
