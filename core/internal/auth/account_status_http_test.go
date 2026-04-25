package auth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-nexus-core/internal/storage"

	"github.com/google/uuid"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func jsonHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Header:     make(http.Header),
	}
}

func insertExternalGrantHumanWithRefreshToken(t *testing.T, ctx context.Context, db *sql.DB, externalSubject, refreshToken string) {
	t.Helper()
	issuer := "https://cp.example"
	agentID, actorID, username := stableExternalGrantPrincipalIDs(issuer, externalSubject)

	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	meta, err := principalMetadataJSON(PrincipalKindHuman, AuthMethodExternalGrant, map[string]any{
		"external_subject": externalSubject,
		"external_issuer":  issuer,
	})
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	if _, err := db.ExecContext(
		ctx,
		`INSERT INTO actors(id, display_name, tags_json, created_at, metadata_json)
		 VALUES (?, ?, ?, ?, '{}')`,
		actorID,
		username,
		`["agent","human","external_grant"]`,
		now,
	); err != nil {
		t.Fatalf("insert actor: %v", err)
	}
	if _, err := db.ExecContext(
		ctx,
		`INSERT INTO agents(id, username, actor_id, created_at, updated_at, revoked_at, metadata_json)
		 VALUES (?, ?, ?, ?, ?, NULL, ?)`,
		agentID,
		username,
		actorID,
		now,
		now,
		meta,
	); err != nil {
		t.Fatalf("insert agent: %v", err)
	}

	sessionID := "refresh_" + uuid.NewString()
	expires := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	if _, err := db.ExecContext(
		ctx,
		`INSERT INTO auth_refresh_sessions(id, agent_id, token_hash, created_at, expires_at, revoked_at, replaced_by_session_id)
		 VALUES (?, ?, ?, ?, ?, NULL, NULL)`,
		sessionID,
		agentID,
		hashToken(refreshToken),
		now,
		expires,
	); err != nil {
		t.Fatalf("insert refresh session: %v", err)
	}
}

func TestIssueTokenFromRefresh_AccountStatusCacheFreshSkipsSecondCPCall(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	defer workspace.Close()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	_ = pub

	var hits int
	var hitsMu sync.Mutex
	client := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		hitsMu.Lock()
		hits++
		hitsMu.Unlock()
		if r.URL.Path != "/v1/internal/accounts/status" {
			return jsonHTTPResponse(http.StatusNotFound, `{"error":"not_found"}`), nil
		}
		return jsonHTTPResponse(http.StatusOK, `{"active":true,"checked_at":"2026-04-19T12:00:00Z"}`), nil
	})}

	var clockMu sync.Mutex
	clock := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	nowFn := func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return clock
	}

	signer := NewEd25519WorkspaceServiceAssertionSigner("svc-id", priv, defaultAccountStatusAudience, "ws_main")
	checker, err := NewHTTPAccountStatusChecker(HTTPAccountStatusCheckerConfig{
		BaseURL:     "http://cp.test",
		WorkspaceID: "ws_main",
		HTTPClient:  client,
		Signer:      signer,
		Now:         nowFn,
		Logf:        func(string, ...any) {},
	})
	if err != nil {
		t.Fatalf("checker: %v", err)
	}

	store := NewStore(workspace.DB(), WithAccountStatusChecker(checker))
	const refresh = "refresh-token-test-abc"
	const sub = "acct_cp_123"
	insertExternalGrantHumanWithRefreshToken(t, ctx, workspace.DB(), sub, refresh)

	bundle1, err := store.IssueTokenFromRefresh(ctx, refresh)
	if err != nil {
		t.Fatalf("first refresh: %v", err)
	}

	clockMu.Lock()
	clock = clock.Add(2 * time.Minute)
	clockMu.Unlock()

	if _, err := store.IssueTokenFromRefresh(ctx, bundle1.RefreshToken); err != nil {
		t.Fatalf("second refresh: %v", err)
	}

	hitsMu.Lock()
	n := hits
	hitsMu.Unlock()
	if n != 1 {
		t.Fatalf("expected exactly 1 account status HTTP call, got %d", n)
	}
}

func TestIssueTokenFromRefresh_AccountInactiveDoesNotRotateRefresh(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	defer workspace.Close()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	_ = pub

	client := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(http.StatusOK, `{"active":false,"checked_at":"2026-04-19T12:00:00Z"}`), nil
	})}

	signer := NewEd25519WorkspaceServiceAssertionSigner("svc-id", priv, defaultAccountStatusAudience, "ws_main")
	checker, err := NewHTTPAccountStatusChecker(HTTPAccountStatusCheckerConfig{
		BaseURL:     "http://cp.test",
		WorkspaceID: "ws_main",
		HTTPClient:  client,
		Signer:      signer,
		Now:         func() time.Time { return time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC) },
		Logf:        func(string, ...any) {},
	})
	if err != nil {
		t.Fatalf("checker: %v", err)
	}

	store := NewStore(workspace.DB(), WithAccountStatusChecker(checker))
	const refresh = "refresh-inactive-test"
	const sub = "acct_inactive"
	insertExternalGrantHumanWithRefreshToken(t, ctx, workspace.DB(), sub, refresh)

	_, err = store.IssueTokenFromRefresh(ctx, refresh)
	if !errors.Is(err, ErrAccountDisabled) {
		t.Fatalf("expected ErrAccountDisabled, got %v", err)
	}

	var replaced sql.NullString
	err = workspace.DB().QueryRowContext(ctx,
		`SELECT replaced_by_session_id FROM auth_refresh_sessions WHERE token_hash = ?`,
		hashToken(refresh),
	).Scan(&replaced)
	if err != nil {
		t.Fatalf("query session: %v", err)
	}
	if replaced.Valid {
		t.Fatalf("expected refresh session not rotated, got replaced_by=%v", replaced.String)
	}

	_, err = store.IssueTokenFromRefresh(ctx, refresh)
	if !errors.Is(err, ErrAccountDisabled) {
		t.Fatalf("second refresh: expected ErrAccountDisabled, got %v", err)
	}
}

func TestIssueTokenFromRefresh_CPUnreachableFailOpenWithWarmActiveCache(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	defer workspace.Close()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	_ = pub

	var call int
	var callMu sync.Mutex
	client := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		callMu.Lock()
		call++
		c := call
		callMu.Unlock()
		if c == 1 {
			return jsonHTTPResponse(http.StatusOK, `{"active":true}`), nil
		}
		return jsonHTTPResponse(http.StatusServiceUnavailable, `{"error":"cp_down"}`), nil
	})}

	var clockMu sync.Mutex
	clock := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	nowFn := func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return clock
	}

	var failOpenLogged bool
	var logMu sync.Mutex
	signer := NewEd25519WorkspaceServiceAssertionSigner("svc-id", priv, defaultAccountStatusAudience, "ws_main")
	checker, err := NewHTTPAccountStatusChecker(HTTPAccountStatusCheckerConfig{
		BaseURL:     "http://cp.test",
		WorkspaceID: "ws_main",
		HTTPClient:  client,
		Signer:      signer,
		Now:         nowFn,
		Logf: func(format string, args ...any) {
			if strings.Contains(format, "fail_open_stale_cache") {
				logMu.Lock()
				failOpenLogged = true
				logMu.Unlock()
			}
		},
	})
	if err != nil {
		t.Fatalf("checker: %v", err)
	}

	store := NewStore(workspace.DB(), WithAccountStatusChecker(checker))
	const refresh = "refresh-failopen"
	const sub = "acct_failopen"
	insertExternalGrantHumanWithRefreshToken(t, ctx, workspace.DB(), sub, refresh)

	bundle1, err := store.IssueTokenFromRefresh(ctx, refresh)
	if err != nil {
		t.Fatalf("first refresh: %v", err)
	}

	clockMu.Lock()
	clock = clock.Add(7 * time.Minute)
	clockMu.Unlock()

	if _, err := store.IssueTokenFromRefresh(ctx, bundle1.RefreshToken); err != nil {
		t.Fatalf("second refresh (fail-open): %v", err)
	}
	logMu.Lock()
	logged := failOpenLogged
	logMu.Unlock()
	if !logged {
		t.Fatalf("expected fail-open stale-cache warning log")
	}
}

func TestIssueTokenFromRefresh_CPUnreachableFailClosedColdCache(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	defer workspace.Close()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	_ = pub

	client := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(http.StatusServiceUnavailable, `{"error":"cp_down"}`), nil
	})}

	signer := NewEd25519WorkspaceServiceAssertionSigner("svc-id", priv, defaultAccountStatusAudience, "ws_main")
	checker, err := NewHTTPAccountStatusChecker(HTTPAccountStatusCheckerConfig{
		BaseURL:     "http://cp.test",
		WorkspaceID: "ws_main",
		HTTPClient:  client,
		Signer:      signer,
		Now:         func() time.Time { return time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC) },
		Logf:        func(string, ...any) {},
	})
	if err != nil {
		t.Fatalf("checker: %v", err)
	}

	store := NewStore(workspace.DB(), WithAccountStatusChecker(checker))
	const refresh = "refresh-cold"
	const sub = "acct_cold"
	insertExternalGrantHumanWithRefreshToken(t, ctx, workspace.DB(), sub, refresh)

	_, err = store.IssueTokenFromRefresh(ctx, refresh)
	if !errors.Is(err, ErrAccountStatusUnreachable) {
		t.Fatalf("expected ErrAccountStatusUnreachable, got %v", err)
	}
}

func TestHTTPAccountStatusChecker_CustomEndpointPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	var sawPath string
	var pathMu sync.Mutex
	client := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		pathMu.Lock()
		sawPath = r.URL.Path
		pathMu.Unlock()
		return jsonHTTPResponse(http.StatusOK, `{"active":true}`), nil
	})}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	_ = pub

	signer := NewEd25519WorkspaceServiceAssertionSigner("svc-id", priv, defaultAccountStatusAudience, "ws_custom")
	checker, err := NewHTTPAccountStatusChecker(HTTPAccountStatusCheckerConfig{
		BaseURL:      "http://cp.test",
		EndpointPath: "custom/status",
		WorkspaceID:  "ws_custom",
		HTTPClient:   client,
		Signer:       signer,
		Now:          func() time.Time { return time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC) },
		Logf:         func(string, ...any) {},
	})
	if err != nil {
		t.Fatalf("checker: %v", err)
	}

	active, err := checker.CheckActive(ctx, "acct_custom_path")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !active {
		t.Fatalf("expected active=true")
	}
	pathMu.Lock()
	p := sawPath
	pathMu.Unlock()
	if p != "/custom/status" {
		t.Fatalf("request path = %q, want /custom/status", p)
	}
}

func TestAccountStatusBaseURLFromEnv(t *testing.T) {
	t.Setenv("ANX_ACCOUNT_STATUS_URL", "")
	if base := AccountStatusBaseURLFromEnv(); base != "" {
		t.Fatalf("empty env: got base=%q", base)
	}

	t.Setenv("ANX_ACCOUNT_STATUS_URL", "https://status.example")
	if base := AccountStatusBaseURLFromEnv(); base != "https://status.example" {
		t.Fatalf("set URL: got base=%q", base)
	}
}
