package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func newGrantExchangeHarness(t *testing.T) grantExchangeHarness {
	t.Helper()
	workspace, err := initializeTestWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	return grantExchangeHarness{
		handler: NewHandler(
			"0.2.2",
			WithAuthStore(auth.NewStore(workspace.DB())),
			WithWorkspaceHumanGrantVerifier(subjectGrantVerifier{}),
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
