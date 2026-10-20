package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agent-nexus-core/internal/auth"
)

const testGrantIssuer = "https://issuer.example"

// subjectGrantVerifier accepts assertions shaped "valid:<subject>:<jti>" as
// if a trusted issuer had signed them, and rejects anything else.
type subjectGrantVerifier struct{}

func (subjectGrantVerifier) Verify(_ context.Context, assertion string) (auth.WorkspaceHumanGrantIdentity, error) {
	parts := strings.Split(assertion, ":")
	if len(parts) != 3 || parts[0] != "valid" {
		return auth.WorkspaceHumanGrantIdentity{}, auth.ErrExternalGrantInvalid
	}
	return auth.WorkspaceHumanGrantIdentity{
		Issuer:    testGrantIssuer,
		Subject:   parts[1],
		JTI:       parts[2],
		GrantType: auth.GrantTypeWorkspaceHuman,
	}, nil
}

type grantExchangeHarness struct {
	handler http.Handler
	db      *sql.DB
}

func newGrantExchangeHarness(t *testing.T, extra ...HandlerOption) grantExchangeHarness {
	t.Helper()
	workspace, err := initializeTestWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	return grantExchangeHarness{
		handler: NewHandler(
			"0.2.2",
			append([]HandlerOption{
				WithAuthStore(auth.NewStore(workspace.DB())),
				WithWorkspaceHumanGrantVerifier(subjectGrantVerifier{}),
			}, extra...)...,
		),
		db: workspace.DB(),
	}
}

func (h grantExchangeHarness) post(t *testing.T, remoteAddr string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/auth/token", strings.NewReader(string(encoded)))
	req.RemoteAddr = remoteAddr
	rr := httptest.NewRecorder()
	h.handler.ServeHTTP(rr, req)
	return rr
}

func humanGrantBody(assertion string) map[string]any {
	return map[string]any{"grant_type": auth.TokenGrantTypeWorkspaceHuman, "assertion": assertion}
}

func TestWorkspaceGrantRateLimitKeysValidGrantsBySubject(t *testing.T) {
	t.Parallel()
	h := newGrantExchangeHarness(t)
	const caller = "198.51.100.7:40000"

	// One server-side caller exchanges grants for 25 different people.
	for i := 0; i < 25; i++ {
		rr := h.post(t, caller, humanGrantBody(fmt.Sprintf("valid:person-%d:jti-many-%d", i, i)))
		if rr.Code != http.StatusOK {
			t.Fatalf("person %d: got %d, want 200: %s", i, rr.Code, rr.Body.String())
		}
	}

	// One person over their own budget is limited, from any address.
	for i := 0; i < 10; i++ {
		rr := h.post(t, caller, humanGrantBody(fmt.Sprintf("valid:busy:jti-busy-%d", i)))
		if rr.Code != http.StatusOK {
			t.Fatalf("busy exchange %d: got %d, want 200: %s", i, rr.Code, rr.Body.String())
		}
	}
	limited := h.post(t, "203.0.113.9:40000", humanGrantBody("valid:busy:jti-busy-over"))
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("busy subject over budget: got %d, want 429: %s", limited.Code, limited.Body.String())
	}
	if limited.Header().Get("Retry-After") == "" {
		t.Fatalf("expected Retry-After on subject limit")
	}

	// Other people from the same caller are unaffected.
	if rr := h.post(t, caller, humanGrantBody("valid:person-late:jti-late")); rr.Code != http.StatusOK {
		t.Fatalf("other subject after busy limit: got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestWorkspaceGrantRateLimitStillLimitsFailedAttemptsPerAddress(t *testing.T) {
	t.Parallel()
	h := newGrantExchangeHarness(t)
	const attacker = "198.51.100.66:40000"

	for i := 0; i < 10; i++ {
		rr := h.post(t, attacker, humanGrantBody("forged"))
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: got %d, want 401: %s", i, rr.Code, rr.Body.String())
		}
	}
	if rr := h.post(t, attacker, humanGrantBody("forged")); rr.Code != http.StatusTooManyRequests {
		t.Fatalf("11th invalid attempt: got %d, want 429: %s", rr.Code, rr.Body.String())
	}
	// Once the address has spent its budget it is rejected before
	// verification, even with a grant that would verify.
	if rr := h.post(t, attacker, humanGrantBody("valid:someone:jti-after-abuse")); rr.Code != http.StatusTooManyRequests {
		t.Fatalf("valid grant from exhausted address: got %d, want 429: %s", rr.Code, rr.Body.String())
	}
	// Other addresses keep their own budget.
	if rr := h.post(t, "198.51.100.67:40000", humanGrantBody("valid:someone:jti-other-addr")); rr.Code != http.StatusOK {
		t.Fatalf("valid grant from another address: got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestWorkspaceGrantReplaysCountAgainstAddress(t *testing.T) {
	t.Parallel()
	h := newGrantExchangeHarness(t)
	const caller = "198.51.100.70:40000"

	if rr := h.post(t, caller, humanGrantBody("valid:replayed:jti-replay")); rr.Code != http.StatusOK {
		t.Fatalf("first exchange: got %d: %s", rr.Code, rr.Body.String())
	}
	// Replays verify but are refused. Each one is charged to the address
	// (and to the subject's own budget).
	for i := 0; i < 9; i++ {
		if rr := h.post(t, caller, humanGrantBody("valid:replayed:jti-replay")); rr.Code != http.StatusUnauthorized {
			t.Fatalf("replay %d: got %d, want 401: %s", i, rr.Code, rr.Body.String())
		}
	}
	// Nine charged replays leave the address one failed attempt.
	if rr := h.post(t, caller, humanGrantBody("forged")); rr.Code != http.StatusUnauthorized {
		t.Fatalf("forged attempt: got %d, want 401: %s", rr.Code, rr.Body.String())
	}
	if rr := h.post(t, caller, humanGrantBody("valid:fresh:jti-fresh")); rr.Code != http.StatusTooManyRequests {
		t.Fatalf("address after replays: got %d, want 429: %s", rr.Code, rr.Body.String())
	}
}

func TestWorkspaceGrantAccessOnlyExchangeStoresNoRefreshSession(t *testing.T) {
	t.Parallel()
	h := newGrantExchangeHarness(t)
	const caller = "198.51.100.80:40000"

	body := humanGrantBody("valid:reader:jti-access-only")
	body["issue_refresh_token"] = false
	rr := h.post(t, caller, body)
	if rr.Code != http.StatusOK {
		t.Fatalf("access-only exchange: got %d: %s", rr.Code, rr.Body.String())
	}
	var payload struct {
		Agent  auth.Agent     `json:"agent"`
		Tokens map[string]any `json:"tokens"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := payload.Tokens["refresh_token"]; ok {
		t.Fatalf("access-only exchange returned a refresh token: %v", payload.Tokens)
	}
	access, _ := payload.Tokens["access_token"].(string)
	if access == "" || payload.Tokens["token_type"] != "Bearer" {
		t.Fatalf("expected a bearer access token: %v", payload.Tokens)
	}
	if got := countAuthRows(t, h.db, `SELECT COUNT(*) FROM auth_refresh_sessions WHERE agent_id = ?`, payload.Agent.AgentID); got != 0 {
		t.Fatalf("access-only exchange stored %d refresh sessions", got)
	}
	if got := countAuthRows(t, h.db, `SELECT COUNT(*) FROM auth_access_tokens WHERE agent_id = ?`, payload.Agent.AgentID); got != 1 {
		t.Fatalf("access-only exchange stored %d access tokens, want 1", got)
	}

	me := httptest.NewRequest(http.MethodGet, "/agents/me", nil)
	me.Header.Set("Authorization", "Bearer "+access)
	meRes := httptest.NewRecorder()
	h.handler.ServeHTTP(meRes, me)
	if meRes.Code != http.StatusOK {
		t.Fatalf("access-only token rejected: %d %s", meRes.Code, meRes.Body.String())
	}

	// The default, and an explicit true, keep issuing a refresh session.
	for i, value := range []any{nil, true} {
		body := humanGrantBody(fmt.Sprintf("valid:reader:jti-default-%d", i))
		if value != nil {
			body["issue_refresh_token"] = value
		}
		rr := h.post(t, caller, body)
		if rr.Code != http.StatusOK {
			t.Fatalf("default exchange %d: got %d: %s", i, rr.Code, rr.Body.String())
		}
		var bundle struct {
			Tokens auth.TokenBundle `json:"tokens"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &bundle); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if bundle.Tokens.RefreshToken == "" {
			t.Fatalf("default exchange %d returned no refresh token", i)
		}
	}
	if got := countAuthRows(t, h.db, `SELECT COUNT(*) FROM auth_refresh_sessions WHERE agent_id = ?`, payload.Agent.AgentID); got != 2 {
		t.Fatalf("default exchanges stored %d refresh sessions, want 2", got)
	}
}

func TestIssueRefreshTokenRejectedOutsideGrantExchanges(t *testing.T) {
	t.Parallel()
	h := newGrantExchangeHarness(t)

	for _, grantType := range []string{"refresh_token", "assertion", "host_assertion"} {
		rr := h.post(t, "198.51.100.90:40000", map[string]any{"grant_type": grantType, "issue_refresh_token": false})
		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "issue_refresh_token") {
			t.Fatalf("%s with issue_refresh_token: got %d %s", grantType, rr.Code, rr.Body.String())
		}
	}
}

func countAuthRows(t *testing.T, db *sql.DB, query string, arg string) int {
	t.Helper()
	var count int
	if err := db.QueryRow(query, arg).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return count
}

// slowGrantVerifier counts verifications and holds each one open, so
// concurrent requests overlap inside verification.
type slowGrantVerifier struct {
	calls *atomic.Int64
}

func (v slowGrantVerifier) Verify(ctx context.Context, assertion string) (auth.WorkspaceHumanGrantIdentity, error) {
	v.calls.Add(1)
	time.Sleep(50 * time.Millisecond)
	return subjectGrantVerifier{}.Verify(ctx, assertion)
}

func TestWorkspaceGrantConcurrentBurstCannotOutrunAddressBudget(t *testing.T) {
	t.Parallel()
	calls := &atomic.Int64{}
	h := newGrantExchangeHarness(t, WithWorkspaceHumanGrantVerifier(slowGrantVerifier{calls: calls}))
	const attacker = "198.51.100.99:40000"

	const burst = 60
	codes := make([]int, burst)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < burst; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			codes[i] = h.post(t, attacker, humanGrantBody("forged")).Code
		}(i)
	}
	close(start)
	wg.Wait()

	limited := 0
	for _, code := range codes {
		if code == http.StatusTooManyRequests {
			limited++
		}
	}
	if got := calls.Load(); got > 10 {
		t.Fatalf("a synchronized burst reached verification %d times; the address budget is 10", got)
	}
	if limited < burst-10 {
		t.Fatalf("expected at least %d of %d concurrent attempts limited before verification, got %d", burst-10, burst, limited)
	}
}

// managedSlotGrantVerifier accepts "valid:<subject>:<slot>:<jti>".
type managedSlotGrantVerifier struct{}

func (managedSlotGrantVerifier) Verify(_ context.Context, assertion string) (auth.WorkspaceManagedAgentGrantIdentity, error) {
	parts := strings.Split(assertion, ":")
	if len(parts) != 4 || parts[0] != "valid" {
		return auth.WorkspaceManagedAgentGrantIdentity{}, auth.ErrExternalGrantInvalid
	}
	return auth.WorkspaceManagedAgentGrantIdentity{
		Issuer:               testGrantIssuer,
		Subject:              parts[1],
		WorkspaceID:          "ws_main",
		OrganizationID:       "org-1",
		SlotID:               parts[2],
		Provider:             "generic",
		ProviderConnectionID: "conn-1",
		OwnerAccountID:       "owner-1",
		JTI:                  parts[3],
		GrantType:            auth.GrantTypeWorkspaceManagedAgent,
	}, nil
}

func TestWorkspaceManagedGrantBudgetIsPerSlotWhateverTheSubject(t *testing.T) {
	t.Parallel()
	h := newGrantExchangeHarness(t, WithWorkspaceManagedAgentGrantVerifier(managedSlotGrantVerifier{}))
	managedBody := func(assertion string) map[string]any {
		return map[string]any{"grant_type": auth.TokenGrantTypeWorkspaceManagedAgent, "assertion": assertion}
	}

	// A different subject on every grant for the same slot does not split
	// the slot's budget.
	for i := 0; i < 10; i++ {
		rr := h.post(t, fmt.Sprintf("198.51.100.%d:40000", 100+i), managedBody(fmt.Sprintf("valid:sub-%d:slot-a:jti-slot-a-%d", i, i)))
		if rr.Code != http.StatusOK {
			t.Fatalf("slot exchange %d: got %d: %s", i, rr.Code, rr.Body.String())
		}
	}
	if rr := h.post(t, "198.51.100.120:40000", managedBody("valid:sub-new:slot-a:jti-slot-a-over")); rr.Code != http.StatusTooManyRequests {
		t.Fatalf("slot over budget with a new subject: got %d, want 429: %s", rr.Code, rr.Body.String())
	}
	// Another slot has its own budget.
	if rr := h.post(t, "198.51.100.120:40000", managedBody("valid:sub-new:slot-b:jti-slot-b")); rr.Code != http.StatusOK {
		t.Fatalf("other slot: got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestWorkspaceGrantReplayDoesNotSpendPrincipalBudget(t *testing.T) {
	t.Parallel()
	h := newGrantExchangeHarness(t)

	if rr := h.post(t, "198.51.100.130:40000", humanGrantBody("valid:victim:jti-victim-0")); rr.Code != http.StatusOK {
		t.Fatalf("first exchange: got %d: %s", rr.Code, rr.Body.String())
	}
	// Someone replays the consumed grant from another address until that
	// address is limited.
	for i := 0; i < 15; i++ {
		rr := h.post(t, "203.0.113.50:40000", humanGrantBody("valid:victim:jti-victim-0"))
		if rr.Code != http.StatusUnauthorized && rr.Code != http.StatusTooManyRequests {
			t.Fatalf("replay %d: got %d: %s", i, rr.Code, rr.Body.String())
		}
	}
	// The victim still has the rest of their budget: 9 more exchanges.
	for i := 1; i < 10; i++ {
		rr := h.post(t, "198.51.100.130:40000", humanGrantBody(fmt.Sprintf("valid:victim:jti-victim-%d", i)))
		if rr.Code != http.StatusOK {
			t.Fatalf("victim exchange %d after replays: got %d: %s", i, rr.Code, rr.Body.String())
		}
	}
}

func TestPrincipalRateLimiterNeverResetsALimitedPrincipalWhenFull(t *testing.T) {
	t.Parallel()
	limiter := newWorkspaceGrantPrincipalRateLimiter(RouteRateLimits{AuthRequestsPerMinute: 60, AuthBurst: 2})
	limiter.maxKeys = 2
	now := time.Now().UTC()

	// "limited" spends its burst; "busy" spends part of its burst.
	for i := 0; i < 2; i++ {
		if ok, _ := limiter.allow("auth", "limited", now); !ok {
			t.Fatalf("limited spend %d refused", i)
		}
	}
	if ok, _ := limiter.allow("auth", "busy", now); !ok {
		t.Fatal("busy spend refused")
	}
	// Full, and neither bucket is idle: a new principal is refused rather
	// than evicting state.
	if ok, retry := limiter.allow("auth", "new", now); ok || retry <= 0 {
		t.Fatalf("expected a new principal to be refused while every bucket carries state, ok=%t retry=%s", ok, retry)
	}
	if ok, _ := limiter.allow("auth", "limited", now); ok {
		t.Fatal("limited principal regained budget")
	}
	// After a second "busy" is full again and idle, so it is the one
	// evicted; "limited" (spent again just now) keeps its state.
	later := now.Add(1500 * time.Millisecond)
	if ok, _ := limiter.allow("auth", "limited", later); !ok {
		t.Fatal("limited principal should have refilled one request")
	}
	if ok, _ := limiter.allow("auth", "new", later); !ok {
		t.Fatal("new principal should replace the idle bucket")
	}
	if _, ok := limiter.buckets["auth:busy"]; ok {
		t.Fatal("expected the idle bucket to be evicted")
	}
	if ok, _ := limiter.allow("auth", "limited", later); ok {
		t.Fatal("limited principal must keep its spent state across eviction")
	}
}

func TestRouteRateLimiterEvictsIdleScopesBeforeLimitedOnes(t *testing.T) {
	t.Parallel()
	limiter := newRouteRateLimiter(RouteRateLimits{AuthRequestsPerMinute: 60, AuthBurst: 1})
	limiter.maxKeys = 2
	now := time.Now().UTC()

	limiter.allow("auth", "addr:old-idle", now)
	limiter.allow("auth", "addr:limited", now.Add(5*time.Second))
	// Within the same second as the limited spend, so only old-idle is idle.
	limiter.allow("auth", "addr:new", now.Add(5*time.Second+100*time.Millisecond))
	if _, ok := limiter.buckets["auth:addr:limited"]; !ok {
		t.Fatal("limited scope must not be evicted while an idle one exists")
	}
	if ok, _ := limiter.allow("auth", "addr:limited", now.Add(5*time.Second+200*time.Millisecond)); ok {
		t.Fatal("limited scope regained budget")
	}
}
