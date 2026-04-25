package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/auth"

	"github.com/google/uuid"
)

type cpRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f cpRoundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func cpJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func seedExternalGrantRefreshForCPHandlerTest(t *testing.T, db *sql.DB, externalSubject, refreshToken string) {
	t.Helper()
	ctx := context.Background()
	issuer := "https://cp.example"
	agentID, actorID, username := stableExternalGrantPrincipalIDsServerTest(issuer, externalSubject)
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	metaBytes, err := json.Marshal(map[string]any{
		"principal_kind":   "human",
		"auth_method":      auth.AuthMethodExternalGrant,
		"external_subject": externalSubject,
		"external_issuer":  issuer,
	})
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	meta := string(metaBytes)
	if _, err := db.ExecContext(ctx,
		`INSERT INTO actors(id, display_name, tags_json, created_at, metadata_json) VALUES (?, ?, ?, ?, '{}')`,
		actorID, username, `["agent","human","external_grant"]`, now,
	); err != nil {
		t.Fatalf("actor: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO agents(id, username, actor_id, created_at, updated_at, revoked_at, metadata_json) VALUES (?, ?, ?, ?, ?, NULL, ?)`,
		agentID, username, actorID, now, now, meta,
	); err != nil {
		t.Fatalf("agent: %v", err)
	}
	sessionID := "refresh_" + uuid.NewString()
	expires := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	if _, err := db.ExecContext(ctx,
		`INSERT INTO auth_refresh_sessions(id, agent_id, token_hash, created_at, expires_at, revoked_at, replaced_by_session_id) VALUES (?, ?, ?, ?, ?, NULL, NULL)`,
		sessionID, agentID, hashRefreshTokenServerTest(refreshToken), now, expires,
	); err != nil {
		t.Fatalf("refresh session: %v", err)
	}
}

func stableExternalGrantPrincipalIDsServerTest(issuer, subject string) (string, string, string) {
	digest := sha256.Sum256([]byte(strings.TrimSpace(issuer) + "\n" + strings.TrimSpace(subject)))
	hexDigest := hex.EncodeToString(digest[:])
	usernameSuffix := hexDigest
	if len(usernameSuffix) > 54 {
		usernameSuffix = usernameSuffix[:54]
	}
	return "agent_ext_" + hexDigest, "actor_ext_" + hexDigest, "external." + usernameSuffix
}

func hashRefreshTokenServerTest(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func TestHandleIssueAuthToken_Refresh_SessionEndedByCP(t *testing.T) {
	t.Parallel()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	_ = pub

	client := &http.Client{Transport: cpRoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return cpJSONResponse(http.StatusOK, `{"active":false}`), nil
	})}
	signer := auth.NewEd25519WorkspaceServiceAssertionSigner("svc", priv, "anx-control-plane", "ws_main")
	checker, err := auth.NewHTTPAccountStatusChecker(auth.HTTPAccountStatusCheckerConfig{
		BaseURL:     "http://cp.test",
		WorkspaceID: "ws_main",
		HTTPClient:  client,
		Signer:      signer,
		Logf:        func(string, ...any) {},
	})
	if err != nil {
		t.Fatalf("checker: %v", err)
	}

	env := newAuthIntegrationEnv(t, authIntegrationOptions{
		accountStatusChecker: checker,
	})
	const refresh = "rt-cp-disabled"
	seedExternalGrantRefreshForCPHandlerTest(t, env.workspace.DB(), "acct_h1", refresh)

	body := map[string]any{"grant_type": "refresh_token", "refresh_token": refresh}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/auth/token", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	env.server.Config.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json: %v", err)
	}
	if payload.Error.Code != "session_ended_by_account_status" {
		t.Fatalf("code=%q", payload.Error.Code)
	}
}

func TestHandleIssueAuthToken_Refresh_CPUnreachable(t *testing.T) {
	t.Parallel()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	_ = pub

	client := &http.Client{Transport: cpRoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return cpJSONResponse(http.StatusServiceUnavailable, `{}`), nil
	})}
	signer := auth.NewEd25519WorkspaceServiceAssertionSigner("svc", priv, "anx-control-plane", "ws_main")
	checker, err := auth.NewHTTPAccountStatusChecker(auth.HTTPAccountStatusCheckerConfig{
		BaseURL:     "http://cp.test",
		WorkspaceID: "ws_main",
		HTTPClient:  client,
		Signer:      signer,
		Logf:        func(string, ...any) {},
	})
	if err != nil {
		t.Fatalf("checker: %v", err)
	}

	env := newAuthIntegrationEnv(t, authIntegrationOptions{
		accountStatusChecker: checker,
	})
	const refresh = "rt-cp-down"
	seedExternalGrantRefreshForCPHandlerTest(t, env.workspace.DB(), "acct_h2", refresh)

	body := map[string]any{"grant_type": "refresh_token", "refresh_token": refresh}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/auth/token", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	env.server.Config.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json: %v", err)
	}
	if payload.Error.Code != "account_status_unreachable" {
		t.Fatalf("code=%q", payload.Error.Code)
	}
}
