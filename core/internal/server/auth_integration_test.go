package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/commandcenter"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
	"agent-nexus-core/internal/series"
	"agent-nexus-core/internal/storage"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/golang-jwt/jwt/v5"
)

const testBootstrapToken = "bootstrap-token-for-tests"

type authIntegrationOptions struct {
	scopedInboxReader             bool
	bootstrapToken                string
	enableDevActorMode            bool
	allowPasskeyDevBypass         bool
	allowDevRegisterLinkedActor   bool
	allowUnauthenticatedWrites    bool
	webAuthnConfig                WebAuthnConfig
	workspaceID                   string
	workspaceAccessMode           string
	hostEnrollmentVerificationURL string
	workspaceHumanGrantVerifier   auth.WorkspaceHumanGrantIdentityVerifier
	workspaceManagedGrantVerifier auth.WorkspaceManagedAgentGrantIdentityVerifier
	accountStatusChecker          auth.AccountStatusChecker
	// wrapActorRegistry, if set, replaces the default *actors.Store passed to WithActorRegistry
	// (use for tests that inject failures from ActorRegistry.Exists).
	wrapActorRegistry func(base *actors.Store) ActorRegistry
}

type authIntegrationEnv struct {
	workspace           *storage.Workspace
	registry            *actors.Store
	authStore           *auth.Store
	passkeySessionStore *auth.PasskeySessionStore
	server              *httptest.Server
	primitiveStore      PrimitiveStore
}

func newAuthIntegrationEnv(t *testing.T, options authIntegrationOptions) authIntegrationEnv {
	requireIntegrationTest(t)
	t.Helper()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}

	registryStore := actors.NewStore(workspace.DB())
	if _, err := registryStore.EnsureSystemActor(context.Background(), time.Now().UTC()); err != nil {
		_ = workspace.Close()
		t.Fatalf("ensure system actor: %v", err)
	}
	var registry ActorRegistry = registryStore
	if options.wrapActorRegistry != nil {
		registry = options.wrapActorRegistry(registryStore)
	}

	authOptions := make([]auth.Option, 0, 2)
	if strings.TrimSpace(options.bootstrapToken) != "" {
		authOptions = append(authOptions, auth.WithBootstrapToken(options.bootstrapToken))
	}
	if options.allowDevRegisterLinkedActor {
		authOptions = append(authOptions, auth.WithAllowDevRegisterLinkedActor(true))
	}
	if options.accountStatusChecker != nil {
		authOptions = append(authOptions, auth.WithAccountStatusChecker(options.accountStatusChecker))
	}
	authStore := auth.NewStore(workspace.DB(), authOptions...)
	passkeySessionStore := auth.NewPasskeySessionStore(auth.DefaultPasskeySessionTTL)

	contractPath := filepath.Join("..", "..", "..", "contracts", "anx-schema.yaml")
	contract, err := schema.Load(contractPath)
	if err != nil {
		passkeySessionStore.Close()
		_ = workspace.Close()
		t.Fatalf("load schema contract: %v", err)
	}

	primitiveStore := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir, primitives.WithScopedInboxReader(options.scopedInboxReader))
	workspaceID := strings.TrimSpace(options.workspaceID)
	if workspaceID == "" {
		workspaceID = "ws_main"
	}
	handler := NewHandler(
		"0.2.2",
		WithActorRegistry(registry),
		WithAuthStore(authStore),
		WithSeriesStore(&series.Store{DB: workspace.DB(), Auth: authStore}),
		WithRunStore(commandcenter.NewStore(workspace.DB(), commandcenter.SQLIdentities{DB: workspace.DB()})),
		WithWorkspaceHumanGrantVerifier(options.workspaceHumanGrantVerifier),
		WithWorkspaceManagedAgentGrantVerifier(options.workspaceManagedGrantVerifier),
		WithWorkspaceGrantRateLimits(RouteRateLimits{AuthRequestsPerMinute: 1000, AuthBurst: 1000}),
		WithPasskeySessionStore(passkeySessionStore),
		WithHealthCheck(workspace.Ping),
		WithPrimitiveStore(primitiveStore),
		WithSchemaContract(contract),
		WithWebAuthnConfig(options.webAuthnConfig),
		WithWorkspaceID(workspaceID),
		WithWorkspaceAccessMode(options.workspaceAccessMode),
		WithHostEnrollmentVerificationURL(options.hostEnrollmentVerificationURL),
		WithEnableDevActorMode(options.enableDevActorMode),
		WithAllowPasskeyDevBypass(options.allowPasskeyDevBypass),
		WithAllowUnauthenticatedWrites(options.allowUnauthenticatedWrites),
	)
	server := httptest.NewServer(handler)

	t.Cleanup(func() {
		server.Close()
		passkeySessionStore.Close()
		_ = workspace.Close()
	})

	return authIntegrationEnv{
		workspace:           workspace,
		registry:            registryStore,
		authStore:           authStore,
		passkeySessionStore: passkeySessionStore,
		server:              server,
		primitiveStore:      primitiveStore,
	}
}

func newReadOnlyAuthIntegrationServer(t *testing.T, env authIntegrationEnv) *httptest.Server {
	t.Helper()

	contractPath := filepath.Join("..", "..", "..", "contracts", "anx-schema.yaml")
	contract, err := schema.Load(contractPath)
	if err != nil {
		t.Fatalf("load schema contract: %v", err)
	}

	handler := NewHandler(
		"0.2.2",
		WithActorRegistry(env.registry),
		WithAuthStore(env.authStore),
		WithPasskeySessionStore(env.passkeySessionStore),
		WithHealthCheck(env.workspace.Ping),
		WithPrimitiveStore(env.primitiveStore),
		WithSchemaContract(contract),
		WithWorkspaceID("ws_main"),
		WithWorkspaceAccessMode(WorkspaceAccessModeReadOnly),
	)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func authTestMinimalTopic(title string) map[string]any {
	return map[string]any{
		"title":         title,
		"summary":       "auth integration topic",
		"owner_refs":    []any{},
		"document_refs": []any{},
		"board_refs":    []any{},
		"related_refs":  []any{},
		"provenance":    map[string]any{"sources": []any{"inferred"}},
	}
}

func authTestThreadUpdatedBy(t *testing.T, serverURL, accessToken string, topic map[string]any) string {
	t.Helper()
	threadID := topicPrimaryThreadID(topic)
	if threadID == "" {
		t.Fatalf("topic missing primary thread: %#v", topic)
	}
	resp := getJSONExpectStatusWithAuth(t, serverURL+"/threads/"+threadID, accessToken, http.StatusOK)
	defer resp.Body.Close()
	var payload struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode thread: %v", err)
	}
	return asString(payload.Thread["updated_by"])
}

func TestFirstPasskeyRegistrationWithBootstrapToken(t *testing.T) {
	requireIntegrationTest(t)
	t.Parallel()

	env := newAuthIntegrationEnv(t, authIntegrationOptions{
		bootstrapToken: testBootstrapToken,
		webAuthnConfig: WebAuthnConfig{
			RPID:     "webauthn.io",
			RPOrigin: "https://webauthn.io",
		},
	})
	serverURL := env.server.URL

	optionsResp := postJSONExpectStatusWithHeaders(t, serverURL+"/auth/passkey/register/options", map[string]any{
		"display_name":    "Casey Human",
		"bootstrap_token": testBootstrapToken,
	}, map[string]string{"Origin": "https://webauthn.io"}, http.StatusOK)
	defer optionsResp.Body.Close()
	var optionsPayload struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(optionsResp.Body).Decode(&optionsPayload); err != nil {
		t.Fatalf("decode passkey options response: %v", err)
	}
	if strings.TrimSpace(optionsPayload.SessionID) == "" {
		t.Fatal("expected passkey register options to return a session_id")
	}

	claim, err := env.authStore.ResolveOnboardingClaim(context.Background(), testBootstrapToken, "", auth.PrincipalKindHuman)
	if err != nil {
		t.Fatalf("resolve bootstrap onboarding claim: %v", err)
	}
	userHandle := mustDecodeStdBase64(t, "1zMAAAAAAAAAAA==")
	sessionID := env.passkeySessionStore.Save(auth.PasskeySession{
		Kind:            auth.PasskeySessionKindRegistration,
		DisplayName:     "testuser1",
		UserHandle:      userHandle,
		OnboardingClaim: claim,
		SessionData: webauthn.SessionData{
			Challenge:        "sVt4ScceMzqFSnfAq8hgLzblvo3fa4_aFVEcIESHIJ0",
			RelyingPartyID:   "webauthn.io",
			UserID:           append([]byte(nil), userHandle...),
			Expires:          time.Now().UTC().Add(time.Minute),
			UserVerification: protocol.VerificationPreferred,
			CredParams: []protocol.CredentialParameter{
				{Type: protocol.PublicKeyCredentialType, Algorithm: -7},
			},
		},
	})

	verifyResp := postJSONExpectStatusWithHeaders(t, serverURL+"/auth/passkey/register/verify", map[string]any{
		"session_id":      sessionID,
		"bootstrap_token": testBootstrapToken,
		"credential":      mustDecodeJSONObject(t, samplePasskeyCredentialJSON),
	}, map[string]string{"Origin": "https://webauthn.io"}, http.StatusCreated)
	defer verifyResp.Body.Close()

	var verifyPayload struct {
		Agent struct {
			AgentID  string `json:"agent_id"`
			Username string `json:"username"`
		} `json:"agent"`
		Tokens struct {
			AccessToken string `json:"access_token"`
		} `json:"tokens"`
	}
	if err := json.NewDecoder(verifyResp.Body).Decode(&verifyPayload); err != nil {
		t.Fatalf("decode passkey verify response: %v", err)
	}
	if strings.TrimSpace(verifyPayload.Agent.AgentID) == "" || strings.TrimSpace(verifyPayload.Tokens.AccessToken) == "" {
		t.Fatalf("unexpected passkey verify payload: %#v", verifyPayload)
	}
	if !strings.HasPrefix(verifyPayload.Agent.Username, "passkey.testuser1.") {
		t.Fatalf("unexpected passkey username: %q", verifyPayload.Agent.Username)
	}
	if status := getBootstrapStatus(t, serverURL); status.Available {
		t.Fatal("expected bootstrap registration to be unavailable after first passkey principal")
	}
}

func TestWorkspaceHumanGrantTokenExchangeAndValidation(t *testing.T) {
	requireIntegrationTest(t)
	t.Parallel()

	const workspaceID = "ws_external_grant"
	fixture := newWorkspaceHumanGrantTestFixture(t, workspaceID, "anx-core")
	env := newAuthIntegrationEnv(t, authIntegrationOptions{
		workspaceID:                 workspaceID,
		workspaceHumanGrantVerifier: fixture.verifier,
	})
	serverURL := env.server.URL

	exchangeGrant := func(assertion string, expectedStatus int) *http.Response {
		return postJSONExpectStatusWithAuth(t, serverURL+"/auth/token", map[string]any{
			"grant_type": auth.TokenGrantTypeWorkspaceHuman,
			"assertion":  assertion,
		}, "", expectedStatus)
	}

	t.Run("valid grant issues external_grant principal and refreshes", func(t *testing.T) {
		assertion := fixture.signGrantAssertion(t, workspaceHumanGrantTokenOptions{
			Subject:     "acct_valid",
			JTI:         "jti-valid-1",
			Email:       "valid@example.com",
			DisplayName: "Valid Human",
		})

		resp := exchangeGrant(assertion, http.StatusOK)
		defer resp.Body.Close()
		var payload struct {
			Agent struct {
				AgentID       string  `json:"agent_id"`
				ActorID       string  `json:"actor_id"`
				PrincipalKind *string `json:"principal_kind"`
				AuthMethod    *string `json:"auth_method"`
			} `json:"agent"`
			Tokens auth.TokenBundle `json:"tokens"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatalf("decode workspace grant response: %v", err)
		}
		if payload.Agent.AgentID == "" || payload.Agent.ActorID == "" {
			t.Fatalf("expected external grant principal identifiers, got %#v", payload.Agent)
		}
		if payload.Agent.PrincipalKind == nil || *payload.Agent.PrincipalKind != string(auth.PrincipalKindHuman) {
			t.Fatalf("expected principal_kind=human, got %#v", payload.Agent)
		}
		if payload.Agent.AuthMethod == nil || *payload.Agent.AuthMethod != auth.AuthMethodExternalGrant {
			t.Fatalf("expected auth_method=external_grant, got %#v", payload.Agent)
		}
		if payload.Tokens.AccessToken == "" || payload.Tokens.RefreshToken == "" {
			t.Fatalf("expected access+refresh tokens, got %#v", payload.Tokens)
		}

		refreshResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/token", map[string]any{
			"grant_type":    "refresh_token",
			"refresh_token": payload.Tokens.RefreshToken,
		}, "", http.StatusOK)
		defer refreshResp.Body.Close()
	})

	t.Run("wrong signature returns unauthorized", func(t *testing.T) {
		assertion := fixture.signGrantAssertion(t, workspaceHumanGrantTokenOptions{
			Subject:     "acct_bad_sig",
			JTI:         "jti-wrong-signature",
			KID:         fixture.primaryKID,
			SigningKey:  fixture.secondaryPrivateKey,
			Email:       "bad-signature@example.com",
			DisplayName: "Wrong Signature",
		})

		resp := exchangeGrant(assertion, http.StatusUnauthorized)
		defer resp.Body.Close()
		assertErrorCode(t, resp, "invalid_token")
	})

	t.Run("wrong audience returns unauthorized", func(t *testing.T) {
		assertion := fixture.signGrantAssertion(t, workspaceHumanGrantTokenOptions{
			Subject:  "acct_wrong_aud",
			JTI:      "jti-wrong-audience",
			Audience: "unexpected-audience",
		})

		resp := exchangeGrant(assertion, http.StatusUnauthorized)
		defer resp.Body.Close()
		assertErrorCode(t, resp, "invalid_token")
	})

	t.Run("wrong workspace_id returns unauthorized", func(t *testing.T) {
		assertion := fixture.signGrantAssertion(t, workspaceHumanGrantTokenOptions{
			Subject:     "acct_wrong_ws",
			JTI:         "jti-wrong-workspace",
			WorkspaceID: "ws_other",
			Scope:       "workspace:ws_other",
		})

		resp := exchangeGrant(assertion, http.StatusUnauthorized)
		defer resp.Body.Close()
		assertErrorCode(t, resp, "invalid_token")
	})

	t.Run("expired token returns unauthorized", func(t *testing.T) {
		assertion := fixture.signGrantAssertion(t, workspaceHumanGrantTokenOptions{
			Subject: "acct_expired",
			JTI:     "jti-expired",
			Now:     time.Now().UTC().Add(-10 * time.Minute),
			TTL:     time.Minute,
		})

		resp := exchangeGrant(assertion, http.StatusUnauthorized)
		defer resp.Body.Close()
		assertErrorCode(t, resp, "invalid_token")
	})

	t.Run("replay jti returns unauthorized on second use", func(t *testing.T) {
		assertion := fixture.signGrantAssertion(t, workspaceHumanGrantTokenOptions{
			Subject: "acct_replay",
			JTI:     "jti-replay",
		})

		first := exchangeGrant(assertion, http.StatusOK)
		first.Body.Close()

		second := exchangeGrant(assertion, http.StatusUnauthorized)
		defer second.Body.Close()
		assertErrorCode(t, second, "invalid_token")
	})

	t.Run("unknown kid forces jwks refresh and returns unauthorized", func(t *testing.T) {
		prime := fixture.signGrantAssertion(t, workspaceHumanGrantTokenOptions{
			Subject: "acct_prime",
			JTI:     "jti-prime",
		})
		primeResp := exchangeGrant(prime, http.StatusOK)
		primeResp.Body.Close()

		before := fixture.jwksHits.Load()
		unknown := fixture.signGrantAssertion(t, workspaceHumanGrantTokenOptions{
			Subject:    "acct_unknown_kid",
			JTI:        "jti-unknown-kid",
			KID:        "kid-unknown",
			SigningKey: fixture.secondaryPrivateKey,
		})
		resp := exchangeGrant(unknown, http.StatusUnauthorized)
		defer resp.Body.Close()
		assertErrorCode(t, resp, "invalid_token")
		if after := fixture.jwksHits.Load(); after <= before {
			t.Fatalf("expected unknown kid path to trigger jwks refresh, before=%d after=%d", before, after)
		}
	})

}

func TestWorkspaceHumanGrantMissingExpRejected(t *testing.T) {
	requireIntegrationTest(t)
	t.Parallel()

	const workspaceID = "ws_external_grant_missing_exp"
	fixture := newWorkspaceHumanGrantTestFixture(t, workspaceID, "anx-core")
	env := newAuthIntegrationEnv(t, authIntegrationOptions{
		workspaceID:                 workspaceID,
		workspaceHumanGrantVerifier: fixture.verifier,
	})
	serverURL := env.server.URL

	assertion := fixture.signGrantAssertion(t, workspaceHumanGrantTokenOptions{
		Subject: "acct_missing_exp",
		JTI:     "jti-missing-exp",
		OmitExp: true,
	})
	resp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/token", map[string]any{
		"grant_type": auth.TokenGrantTypeWorkspaceHuman,
		"assertion":  assertion,
	}, "", http.StatusUnauthorized)
	defer resp.Body.Close()
	assertErrorCode(t, resp, "invalid_token")
}

func TestWorkspaceHumanGrantUnknownKidRefreshFailureReturnsUnavailable(t *testing.T) {
	requireIntegrationTest(t)
	t.Parallel()

	const workspaceID = "ws_external_grant_unknown_kid_unavailable"
	fixture := newWorkspaceHumanGrantTestFixture(t, workspaceID, "anx-core")
	env := newAuthIntegrationEnv(t, authIntegrationOptions{
		workspaceID:                 workspaceID,
		workspaceHumanGrantVerifier: fixture.verifier,
	})
	serverURL := env.server.URL

	prime := fixture.signGrantAssertion(t, workspaceHumanGrantTokenOptions{
		Subject: "acct_prime_unavailable",
		JTI:     "jti-prime-unavailable",
	})
	primeResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/token", map[string]any{
		"grant_type": auth.TokenGrantTypeWorkspaceHuman,
		"assertion":  prime,
	}, "", http.StatusOK)
	primeResp.Body.Close()

	fixture.setJWKSStatusCode(http.StatusServiceUnavailable)
	t.Cleanup(func() {
		fixture.setJWKSStatusCode(http.StatusOK)
	})

	unknown := fixture.signGrantAssertion(t, workspaceHumanGrantTokenOptions{
		Subject:    "acct_unknown_kid_unavailable",
		JTI:        "jti-unknown-kid-unavailable",
		KID:        "kid-unknown-unavailable",
		SigningKey: fixture.secondaryPrivateKey,
	})
	resp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/token", map[string]any{
		"grant_type": auth.TokenGrantTypeWorkspaceHuman,
		"assertion":  unknown,
	}, "", http.StatusServiceUnavailable)
	defer resp.Body.Close()
	assertErrorCode(t, resp, "auth_unavailable")
}

func TestWorkspaceHumanGrantReplayBlockedAfterPostConsumptionFailure(t *testing.T) {
	requireIntegrationTest(t)
	t.Parallel()

	const workspaceID = "ws_external_grant_replay_after_failure"
	fixture := newWorkspaceHumanGrantTestFixture(t, workspaceID, "anx-core")
	env := newAuthIntegrationEnv(t, authIntegrationOptions{
		workspaceID:                 workspaceID,
		workspaceHumanGrantVerifier: fixture.verifier,
	})
	serverURL := env.server.URL

	seed := fixture.signGrantAssertion(t, workspaceHumanGrantTokenOptions{
		Subject: "acct_replay_after_failure",
		JTI:     "jti-seed-replay-after-failure",
	})
	seedResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/token", map[string]any{
		"grant_type": auth.TokenGrantTypeWorkspaceHuman,
		"assertion":  seed,
	}, "", http.StatusOK)
	var seedPayload struct {
		Agent struct {
			AgentID string `json:"agent_id"`
		} `json:"agent"`
	}
	if err := json.NewDecoder(seedResp.Body).Decode(&seedPayload); err != nil {
		seedResp.Body.Close()
		t.Fatalf("decode seed workspace grant response: %v", err)
	}
	seedResp.Body.Close()
	if strings.TrimSpace(seedPayload.Agent.AgentID) == "" {
		t.Fatal("expected seed token exchange to return agent id")
	}

	nowText := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := env.workspace.DB().Exec(
		`UPDATE agents
		 SET revoked_at = ?, updated_at = ?
		 WHERE id = ?`,
		nowText,
		nowText,
		seedPayload.Agent.AgentID,
	); err != nil {
		t.Fatalf("revoke seed agent for replay-after-failure scenario: %v", err)
	}

	assertion := fixture.signGrantAssertion(t, workspaceHumanGrantTokenOptions{
		Subject: "acct_replay_after_failure",
		JTI:     "jti-replay-after-failure",
	})
	first := postJSONExpectStatusWithAuth(t, serverURL+"/auth/token", map[string]any{
		"grant_type": auth.TokenGrantTypeWorkspaceHuman,
		"assertion":  assertion,
	}, "", http.StatusForbidden)
	defer first.Body.Close()
	assertErrorCode(t, first, "agent_revoked")

	second := postJSONExpectStatusWithAuth(t, serverURL+"/auth/token", map[string]any{
		"grant_type": auth.TokenGrantTypeWorkspaceHuman,
		"assertion":  assertion,
	}, "", http.StatusUnauthorized)
	defer second.Body.Close()
	assertErrorCode(t, second, "invalid_token")
}

func TestWorkspaceManagedAgentGrantTokenExchangeAndValidation(t *testing.T) {
	requireIntegrationTest(t)
	t.Parallel()

	const workspaceID = "ws_managed_grant"
	fixture := newWorkspaceHumanGrantTestFixture(t, workspaceID, "anx-core")
	env := newAuthIntegrationEnv(t, authIntegrationOptions{
		workspaceID:                   workspaceID,
		workspaceManagedGrantVerifier: fixture.managedVerifier,
	})
	serverURL := env.server.URL

	exchangeGrant := func(assertion string, expectedStatus int) *http.Response {
		return postJSONExpectStatusWithAuth(t, serverURL+"/auth/token", map[string]any{
			"grant_type": auth.TokenGrantTypeWorkspaceManagedAgent,
			"assertion":  assertion,
		}, "", expectedStatus)
	}

	t.Run("valid grant issues managed agent principal and human-only routes reject it", func(t *testing.T) {
		assertion := fixture.signManagedAgentGrantAssertion(t, workspaceManagedGrantTokenOptions{
			Subject:              "grant-subject-valid",
			JTI:                  "jti-managed-valid-1",
			OrganizationID:       "org_1",
			SlotID:               "slot_researcher",
			SlotName:             "Researcher",
			Provider:             "provider-alpha",
			ProviderConnectionID: "conn_1",
			OwnerAccountID:       "acct_owner",
			ExternalSubject:      "provider-user-1",
		})

		resp := exchangeGrant(assertion, http.StatusOK)
		defer resp.Body.Close()
		var payload struct {
			Agent struct {
				AgentID       string  `json:"agent_id"`
				ActorID       string  `json:"actor_id"`
				PrincipalKind *string `json:"principal_kind"`
				AuthMethod    *string `json:"auth_method"`
			} `json:"agent"`
			Tokens auth.TokenBundle `json:"tokens"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatalf("decode workspace managed grant response: %v", err)
		}
		if payload.Agent.AgentID == "" || payload.Agent.ActorID == "" {
			t.Fatalf("expected managed grant principal identifiers, got %#v", payload.Agent)
		}
		if payload.Agent.PrincipalKind == nil || *payload.Agent.PrincipalKind != string(auth.PrincipalKindAgent) {
			t.Fatalf("expected principal_kind=agent, got %#v", payload.Agent)
		}
		if payload.Agent.AuthMethod == nil || *payload.Agent.AuthMethod != auth.AuthMethodManagedGrant {
			t.Fatalf("expected auth_method=managed_grant, got %#v", payload.Agent)
		}
		if payload.Tokens.AccessToken == "" || payload.Tokens.RefreshToken == "" {
			t.Fatalf("expected access+refresh tokens, got %#v", payload.Tokens)
		}

		secretResp := postJSONExpectStatusWithAuth(t, serverURL+"/secrets", map[string]any{
			"name":  "managed-agent-secret",
			"value": "must-not-write",
		}, payload.Tokens.AccessToken, http.StatusForbidden)
		defer secretResp.Body.Close()
		assertErrorCode(t, secretResp, "human_only")
	})

	t.Run("slot rename reuses stable principal keyed by slot id", func(t *testing.T) {
		firstAssertion := fixture.signManagedAgentGrantAssertion(t, workspaceManagedGrantTokenOptions{
			Subject:              "grant-subject-rename-a",
			JTI:                  "jti-managed-rename-a",
			OrganizationID:       "org_1",
			SlotID:               "slot_stable",
			SlotName:             "Old Name",
			DisplayName:          "Old Display",
			Provider:             "provider-alpha",
			ProviderConnectionID: "conn_2",
			OwnerAccountID:       "acct_owner",
		})
		firstResp := exchangeGrant(firstAssertion, http.StatusOK)
		var firstPayload struct {
			Agent struct {
				AgentID string `json:"agent_id"`
				ActorID string `json:"actor_id"`
			} `json:"agent"`
		}
		if err := json.NewDecoder(firstResp.Body).Decode(&firstPayload); err != nil {
			firstResp.Body.Close()
			t.Fatalf("decode first managed grant response: %v", err)
		}
		firstResp.Body.Close()

		secondAssertion := fixture.signManagedAgentGrantAssertion(t, workspaceManagedGrantTokenOptions{
			Subject:              "grant-subject-rename-b",
			JTI:                  "jti-managed-rename-b",
			OrganizationID:       "org_1",
			SlotID:               "slot_stable",
			SlotName:             "New Name",
			DisplayName:          "New Display",
			Provider:             "provider-alpha",
			ProviderConnectionID: "conn_2",
			OwnerAccountID:       "acct_owner",
		})
		secondResp := exchangeGrant(secondAssertion, http.StatusOK)
		var secondPayload struct {
			Agent struct {
				AgentID string `json:"agent_id"`
				ActorID string `json:"actor_id"`
			} `json:"agent"`
		}
		if err := json.NewDecoder(secondResp.Body).Decode(&secondPayload); err != nil {
			secondResp.Body.Close()
			t.Fatalf("decode second managed grant response: %v", err)
		}
		secondResp.Body.Close()
		if firstPayload.Agent.AgentID != secondPayload.Agent.AgentID || firstPayload.Agent.ActorID != secondPayload.Agent.ActorID {
			t.Fatalf("expected stable slot principal reuse, first=%#v second=%#v", firstPayload.Agent, secondPayload.Agent)
		}
		var actorDisplayName string
		if err := env.workspace.DB().QueryRowContext(context.Background(), `SELECT display_name FROM actors WHERE id = ?`, firstPayload.Agent.ActorID).Scan(&actorDisplayName); err != nil {
			t.Fatalf("load managed actor display_name: %v", err)
		}
		if actorDisplayName != "New Name" {
			t.Fatalf("managed grant visible actor name must prefer latest slot_name, got %q", actorDisplayName)
		}
	})

	t.Run("validation failures return unauthorized", func(t *testing.T) {
		cases := []struct {
			name    string
			options workspaceManagedGrantTokenOptions
		}{
			{name: "wrong audience", options: workspaceManagedGrantTokenOptions{JTI: "jti-managed-wrong-aud", Audience: "wrong"}},
			{name: "wrong workspace", options: workspaceManagedGrantTokenOptions{JTI: "jti-managed-wrong-ws", WorkspaceID: "ws_other", Scope: "workspace:ws_other"}},
			{name: "wrong grant type", options: workspaceManagedGrantTokenOptions{JTI: "jti-managed-wrong-type", GrantType: auth.GrantTypeWorkspaceHuman}},
			{name: "wrong scope", options: workspaceManagedGrantTokenOptions{JTI: "jti-managed-wrong-scope", Scope: "workspace:other"}},
			{name: "expired", options: workspaceManagedGrantTokenOptions{JTI: "jti-managed-expired", Now: time.Now().UTC().Add(-10 * time.Minute), TTL: time.Minute}},
			{name: "missing jti", options: workspaceManagedGrantTokenOptions{OmitJTI: true}},
		}
		for _, tt := range cases {
			t.Run(tt.name, func(t *testing.T) {
				resp := exchangeGrant(fixture.signManagedAgentGrantAssertion(t, tt.options), http.StatusUnauthorized)
				defer resp.Body.Close()
				assertErrorCode(t, resp, "invalid_token")
			})
		}
	})

	t.Run("replay jti returns unauthorized on second use", func(t *testing.T) {
		assertion := fixture.signManagedAgentGrantAssertion(t, workspaceManagedGrantTokenOptions{
			JTI: "jti-managed-replay",
		})

		first := exchangeGrant(assertion, http.StatusOK)
		first.Body.Close()

		second := exchangeGrant(assertion, http.StatusUnauthorized)
		defer second.Body.Close()
		assertErrorCode(t, second, "invalid_token")
	})

	t.Run("revoked managed principal cannot receive tokens", func(t *testing.T) {
		seed := fixture.signManagedAgentGrantAssertion(t, workspaceManagedGrantTokenOptions{
			JTI:    "jti-managed-revoked-seed",
			SlotID: "slot_revoked",
		})
		seedResp := exchangeGrant(seed, http.StatusOK)
		var seedPayload struct {
			Agent struct {
				AgentID string `json:"agent_id"`
			} `json:"agent"`
		}
		if err := json.NewDecoder(seedResp.Body).Decode(&seedPayload); err != nil {
			seedResp.Body.Close()
			t.Fatalf("decode seed managed grant response: %v", err)
		}
		seedResp.Body.Close()
		nowText := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := env.workspace.DB().Exec(
			`UPDATE agents SET revoked_at = ?, updated_at = ? WHERE id = ?`,
			nowText,
			nowText,
			seedPayload.Agent.AgentID,
		); err != nil {
			t.Fatalf("revoke managed grant principal: %v", err)
		}

		assertion := fixture.signManagedAgentGrantAssertion(t, workspaceManagedGrantTokenOptions{
			JTI:    "jti-managed-revoked-new",
			SlotID: "slot_revoked",
		})
		resp := exchangeGrant(assertion, http.StatusForbidden)
		defer resp.Body.Close()
		assertErrorCode(t, resp, "agent_revoked")
	})
}

func TestExplicitDevModeKeepsLegacyActorFlowAndAnonymousWorkspaceAccess(t *testing.T) {
	requireIntegrationTest(t)
	t.Parallel()

	env := newAuthIntegrationEnv(t, authIntegrationOptions{
		enableDevActorMode:         true,
		allowUnauthenticatedWrites: true,
	})
	serverURL := env.server.URL

	createActorResp := postJSONExpectStatusWithAuth(t, serverURL+"/actors", map[string]any{
		"actor": map[string]any{
			"id":           "dev-actor",
			"display_name": "Dev Actor",
			"created_at":   "2026-03-05T10:00:00Z",
		},
	}, "", http.StatusCreated)
	createActorResp.Body.Close()

	listActorsResp := getJSONExpectStatusWithAuth(t, serverURL+"/actors", "", http.StatusOK)
	defer listActorsResp.Body.Close()
	var actorsPayload struct {
		Actors []actors.Actor `json:"actors"`
	}
	if err := json.NewDecoder(listActorsResp.Body).Decode(&actorsPayload); err != nil {
		t.Fatalf("decode actors response: %v", err)
	}
	if len(actorsPayload.Actors) == 0 {
		t.Fatal("expected dev actor list to include at least one actor")
	}

	createTopicResp := postJSONExpectStatusWithAuth(t, serverURL+"/topics", map[string]any{
		"actor_id": "dev-actor",
		"topic":    authTestMinimalTopic("Dev mode topic"),
	}, "", http.StatusCreated)
	createTopicResp.Body.Close()

	listThreadsResp := getJSONExpectStatusWithAuth(t, serverURL+"/threads", "", http.StatusOK)
	defer listThreadsResp.Body.Close()
	var threadsPayload struct {
		Threads []map[string]any `json:"threads"`
	}
	if err := json.NewDecoder(listThreadsResp.Body).Decode(&threadsPayload); err != nil {
		t.Fatalf("decode threads response: %v", err)
	}
	if len(threadsPayload.Threads) != 1 {
		t.Fatalf("expected one thread in dev mode, got %d", len(threadsPayload.Threads))
	}
}

func getBootstrapStatus(t *testing.T, serverURL string) struct {
	Available              bool
	DevPasskeyBypassActive bool
} {
	t.Helper()
	resp := getJSONExpectStatusWithAuth(t, serverURL+"/auth/bootstrap/status", "", http.StatusOK)
	defer resp.Body.Close()

	var payload struct {
		Available              bool `json:"bootstrap_registration_available"`
		DevPasskeyBypassActive bool `json:"dev_passkey_bypass_available"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode bootstrap status response: %v", err)
	}
	return struct {
		Available              bool
		DevPasskeyBypassActive bool
	}{
		Available:              payload.Available,
		DevPasskeyBypassActive: payload.DevPasskeyBypassActive,
	}
}

func createInviteToken(t *testing.T, serverURL string, accessToken string, payload map[string]any) string {
	t.Helper()
	_, token := createInvite(t, serverURL, accessToken, payload)
	return token
}

func createInvite(t *testing.T, serverURL string, accessToken string, payload map[string]any) (map[string]any, string) {
	t.Helper()
	resp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/invites", payload, accessToken, http.StatusCreated)
	defer resp.Body.Close()

	var decoded struct {
		Invite map[string]any `json:"invite"`
		Token  string         `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode create invite response: %v", err)
	}
	if strings.TrimSpace(decoded.Token) == "" {
		t.Fatalf("expected create invite response to include token: %#v", decoded)
	}
	return decoded.Invite, decoded.Token
}

func listInvites(t *testing.T, serverURL string, accessToken string) []map[string]any {
	t.Helper()
	resp := getJSONExpectStatusWithAuth(t, serverURL+"/auth/invites", accessToken, http.StatusOK)
	defer resp.Body.Close()

	var payload struct {
		Invites []map[string]any `json:"invites"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode invites list response: %v", err)
	}
	return payload.Invites
}

func listAuthPrincipalsPage(t *testing.T, serverURL string, accessToken string, limit int, cursor string) struct {
	Principals []map[string]any `json:"principals"`
	NextCursor string           `json:"next_cursor"`
} {
	t.Helper()
	url := fmt.Sprintf("%s/auth/principals?limit=%d", serverURL, limit)
	if strings.TrimSpace(cursor) != "" {
		url += "&cursor=" + cursor
	}
	resp := getJSONExpectStatusWithAuth(t, url, accessToken, http.StatusOK)
	defer resp.Body.Close()

	var payload struct {
		Principals []map[string]any `json:"principals"`
		NextCursor string           `json:"next_cursor"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode auth principals response: %v", err)
	}
	return payload
}

func listAuthAuditPage(t *testing.T, serverURL string, accessToken string, limit int, cursor string) struct {
	Events     []map[string]any `json:"events"`
	NextCursor string           `json:"next_cursor"`
} {
	t.Helper()
	url := fmt.Sprintf("%s/auth/audit?limit=%d", serverURL, limit)
	if strings.TrimSpace(cursor) != "" {
		url += "&cursor=" + cursor
	}
	resp := getJSONExpectStatusWithAuth(t, url, accessToken, http.StatusOK)
	defer resp.Body.Close()

	var payload struct {
		Events     []map[string]any `json:"events"`
		NextCursor string           `json:"next_cursor"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode auth audit response: %v", err)
	}
	return payload
}

type lockoutPrincipalSeed struct {
	AgentID     string
	ActorID     string
	Username    string
	AccessToken string
}

func seedMachinePrincipalForLockoutTest(t *testing.T, ctx context.Context, db *sql.DB, agentID string, actorID string, username string, accessToken string) lockoutPrincipalSeed {
	t.Helper()

	seed := lockoutPrincipalSeed{
		AgentID:     agentID,
		ActorID:     actorID,
		Username:    username,
		AccessToken: accessToken,
	}
	now := time.Date(2026, 3, 21, 10, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	if _, err := db.ExecContext(
		ctx,
		`INSERT INTO actors(id, display_name, tags_json, created_at, metadata_json)
		 VALUES (?, ?, ?, ?, '{}')`,
		actorID,
		username,
		`["agent"]`,
		now,
	); err != nil {
		t.Fatalf("insert machine actor: %v", err)
	}
	if _, err := db.ExecContext(
		ctx,
		`INSERT INTO agents(id, username, actor_id, created_at, updated_at, revoked_at, metadata_json)
		 VALUES (?, ?, ?, ?, ?, NULL, '{}')`,
		agentID,
		username,
		actorID,
		now,
		now,
	); err != nil {
		t.Fatalf("insert machine agent: %v", err)
	}
	insertAuthAccessTokenForLockoutTest(t, ctx, db, agentID, accessToken, now)
	return seed
}

func seedHumanPrincipalForLockoutTest(t *testing.T, ctx context.Context, db *sql.DB, agentID string, actorID string, username string, accessToken string) lockoutPrincipalSeed {
	t.Helper()

	seed := lockoutPrincipalSeed{
		AgentID:     agentID,
		ActorID:     actorID,
		Username:    username,
		AccessToken: accessToken,
	}
	now := time.Date(2026, 3, 21, 10, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	publicKeyB64, _ := generateKeyPair(t)
	publicKey, err := base64.StdEncoding.DecodeString(publicKeyB64)
	if err != nil {
		t.Fatalf("decode public key: %v", err)
	}
	if _, err := db.ExecContext(
		ctx,
		`INSERT INTO actors(id, display_name, tags_json, created_at, metadata_json)
		 VALUES (?, ?, ?, ?, '{}')`,
		actorID,
		username,
		`["agent","human","passkey"]`,
		now,
	); err != nil {
		t.Fatalf("insert human actor: %v", err)
	}
	if _, err := db.ExecContext(
		ctx,
		`INSERT INTO agents(id, username, actor_id, created_at, updated_at, revoked_at, metadata_json)
		 VALUES (?, ?, ?, ?, ?, NULL, '{}')`,
		agentID,
		username,
		actorID,
		now,
		now,
	); err != nil {
		t.Fatalf("insert human agent: %v", err)
	}
	if _, err := db.ExecContext(
		ctx,
		`INSERT INTO passkey_credentials(
			credential_id,
			agent_id,
			user_handle,
			public_key,
			attestation_type,
			transport,
			sign_count,
			backup_eligible,
			backup_state,
			aaguid,
			attachment,
			created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"credential-"+agentID,
		agentID,
		[]byte("user-"+agentID),
		publicKey,
		"none",
		"",
		0,
		0,
		0,
		[]byte{},
		"",
		now,
	); err != nil {
		t.Fatalf("insert human passkey credential: %v", err)
	}
	insertAuthAccessTokenForLockoutTest(t, ctx, db, agentID, accessToken, now)
	return seed
}

func insertAuthAccessTokenForLockoutTest(t *testing.T, ctx context.Context, db *sql.DB, agentID string, accessToken string, now string) {
	t.Helper()

	expiresAt := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	if _, err := db.ExecContext(
		ctx,
		`INSERT INTO auth_access_tokens(id, agent_id, token_hash, created_at, expires_at, revoked_at)
		 VALUES (?, ?, ?, ?, ?, NULL)`,
		"access-"+agentID,
		agentID,
		authHashTokenForLockoutTest(accessToken),
		now,
		expiresAt,
	); err != nil {
		t.Fatalf("insert access token: %v", err)
	}
}

func authHashTokenForLockoutTest(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

type workspaceHumanGrantTestFixture struct {
	issuer              string
	audience            string
	workspaceID         string
	primaryKID          string
	primaryPrivateKey   ed25519.PrivateKey
	secondaryPrivateKey ed25519.PrivateKey
	verifier            auth.WorkspaceHumanGrantIdentityVerifier
	managedVerifier     auth.WorkspaceManagedAgentGrantIdentityVerifier
	jwksHits            atomic.Int64
	jwksServer          *httptest.Server
	jwksStateMu         sync.Mutex
	jwksStatusCode      int
}

type workspaceHumanGrantTokenOptions struct {
	Subject     string
	JTI         string
	Audience    string
	WorkspaceID string
	Scope       string
	GrantType   string
	KID         string
	SigningKey  ed25519.PrivateKey
	Now         time.Time
	TTL         time.Duration
	Email       string
	DisplayName string
	OmitExp     bool
}

type workspaceManagedGrantTokenOptions struct {
	Subject              string
	JTI                  string
	Audience             string
	WorkspaceID          string
	OrganizationID       string
	SlotID               string
	SlotName             string
	DisplayName          string
	Provider             string
	ProviderConnectionID string
	OwnerAccountID       string
	ExternalSubject      string
	Scope                string
	GrantType            string
	KID                  string
	SigningKey           ed25519.PrivateKey
	Now                  time.Time
	TTL                  time.Duration
	OmitExp              bool
	OmitJTI              bool
}

func newWorkspaceHumanGrantTestFixture(t *testing.T, workspaceID string, audience string) *workspaceHumanGrantTestFixture {
	requireIntegrationTest(t)
	t.Helper()

	primaryPublic, primaryPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate workspace grant primary key: %v", err)
	}
	_, secondaryPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate workspace grant secondary key: %v", err)
	}

	fixture := &workspaceHumanGrantTestFixture{
		audience:            strings.TrimSpace(audience),
		workspaceID:         strings.TrimSpace(workspaceID),
		primaryKID:          "kid-primary",
		primaryPrivateKey:   primaryPrivate,
		secondaryPrivateKey: secondaryPrivate,
		jwksStatusCode:      http.StatusOK,
	}

	jwksPayload := map[string]any{
		"keys": []map[string]any{
			{
				"kty": "OKP",
				"crv": "Ed25519",
				"kid": fixture.primaryKID,
				"x":   base64.RawURLEncoding.EncodeToString(primaryPublic),
				"use": "sig",
				"alg": "EdDSA",
			},
		},
	}
	fixture.jwksServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.jwksHits.Add(1)
		fixture.jwksStateMu.Lock()
		statusCode := fixture.jwksStatusCode
		fixture.jwksStateMu.Unlock()
		if statusCode != http.StatusOK {
			w.WriteHeader(statusCode)
			return
		}
		writeJSON(w, http.StatusOK, jwksPayload)
	}))
	t.Cleanup(func() {
		fixture.jwksServer.Close()
	})
	fixture.issuer = fixture.jwksServer.URL

	jwksURL, err := auth.WorkspaceHumanGrantJWKSURL(fixture.issuer)
	if err != nil {
		t.Fatalf("derive workspace grant jwks url: %v", err)
	}
	resolver, err := auth.NewWorkspaceHumanGrantJWKResolver(auth.WorkspaceHumanGrantJWKResolverConfig{
		JWKSURL: jwksURL,
	})
	if err != nil {
		t.Fatalf("new workspace grant jwks resolver: %v", err)
	}
	verifier, err := auth.NewWorkspaceHumanGrantVerifier(auth.WorkspaceHumanGrantVerifierConfig{
		Issuer:      fixture.issuer,
		Audience:    fixture.audience,
		WorkspaceID: fixture.workspaceID,
		Resolver:    resolver,
	})
	if err != nil {
		t.Fatalf("new workspace grant verifier: %v", err)
	}
	fixture.verifier = verifier
	managedVerifier, err := auth.NewWorkspaceManagedAgentGrantVerifier(auth.WorkspaceManagedAgentGrantVerifierConfig{
		Issuer:      fixture.issuer,
		Audience:    fixture.audience,
		WorkspaceID: fixture.workspaceID,
		Resolver:    resolver,
	})
	if err != nil {
		t.Fatalf("new workspace managed grant verifier: %v", err)
	}
	fixture.managedVerifier = managedVerifier
	return fixture
}

func (f *workspaceHumanGrantTestFixture) signGrantAssertion(t *testing.T, options workspaceHumanGrantTokenOptions) string {
	t.Helper()

	now := options.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	ttl := options.TTL
	if ttl <= 0 {
		ttl = auth.DefaultWorkspaceHumanGrantTTL
	}
	subject := strings.TrimSpace(options.Subject)
	if subject == "" {
		subject = "acct-default"
	}
	jti := strings.TrimSpace(options.JTI)
	if jti == "" {
		jti = fmt.Sprintf("jti-%d", now.UnixNano())
	}
	audience := strings.TrimSpace(options.Audience)
	if audience == "" {
		audience = f.audience
	}
	workspaceID := strings.TrimSpace(options.WorkspaceID)
	if workspaceID == "" {
		workspaceID = f.workspaceID
	}
	scope := strings.TrimSpace(options.Scope)
	if scope == "" {
		scope = "workspace:" + workspaceID
	}
	grantType := strings.TrimSpace(options.GrantType)
	if grantType == "" {
		grantType = auth.GrantTypeWorkspaceHuman
	}
	kid := strings.TrimSpace(options.KID)
	if kid == "" {
		kid = f.primaryKID
	}
	signingKey := options.SigningKey
	if len(signingKey) == 0 {
		signingKey = f.primaryPrivateKey
	}

	claims := auth.WorkspaceHumanGrantClaims{
		WorkspaceID: workspaceID,
		Email:       strings.TrimSpace(options.Email),
		DisplayName: strings.TrimSpace(options.DisplayName),
		Scope:       scope,
		GrantType:   grantType,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    f.issuer,
			Subject:   subject,
			Audience:  jwt.ClaimStrings{audience},
			ID:        jti,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-time.Minute)),
		},
	}
	if !options.OmitExp {
		claims.ExpiresAt = jwt.NewNumericDate(now.Add(ttl))
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(signingKey)
	if err != nil {
		t.Fatalf("sign workspace human grant token: %v", err)
	}
	return signed
}

func (f *workspaceHumanGrantTestFixture) signManagedAgentGrantAssertion(t *testing.T, options workspaceManagedGrantTokenOptions) string {
	t.Helper()

	now := options.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	ttl := options.TTL
	if ttl <= 0 {
		ttl = auth.DefaultWorkspaceHumanGrantTTL
	}
	subject := strings.TrimSpace(options.Subject)
	if subject == "" {
		subject = "managed-grant-subject"
	}
	jti := strings.TrimSpace(options.JTI)
	if jti == "" && !options.OmitJTI {
		jti = fmt.Sprintf("jti-managed-%d", now.UnixNano())
	}
	audience := strings.TrimSpace(options.Audience)
	if audience == "" {
		audience = f.audience
	}
	workspaceID := strings.TrimSpace(options.WorkspaceID)
	if workspaceID == "" {
		workspaceID = f.workspaceID
	}
	organizationID := strings.TrimSpace(options.OrganizationID)
	if organizationID == "" {
		organizationID = "org_default"
	}
	slotID := strings.TrimSpace(options.SlotID)
	if slotID == "" {
		slotID = "slot_default"
	}
	slotName := strings.TrimSpace(options.SlotName)
	if slotName == "" {
		slotName = "Managed Agent"
	}
	provider := strings.TrimSpace(options.Provider)
	if provider == "" {
		provider = "provider-alpha"
	}
	providerConnectionID := strings.TrimSpace(options.ProviderConnectionID)
	if providerConnectionID == "" {
		providerConnectionID = "conn_default"
	}
	ownerAccountID := strings.TrimSpace(options.OwnerAccountID)
	if ownerAccountID == "" {
		ownerAccountID = "acct_default"
	}
	scope := strings.TrimSpace(options.Scope)
	if scope == "" {
		scope = "workspace:" + workspaceID
	}
	grantType := strings.TrimSpace(options.GrantType)
	if grantType == "" {
		grantType = auth.GrantTypeWorkspaceManagedAgent
	}
	kid := strings.TrimSpace(options.KID)
	if kid == "" {
		kid = f.primaryKID
	}
	signingKey := options.SigningKey
	if len(signingKey) == 0 {
		signingKey = f.primaryPrivateKey
	}

	claims := auth.WorkspaceManagedAgentGrantClaims{
		WorkspaceID:          workspaceID,
		OrganizationID:       organizationID,
		SlotID:               slotID,
		SlotName:             slotName,
		DisplayName:          strings.TrimSpace(options.DisplayName),
		Provider:             provider,
		ProviderConnectionID: providerConnectionID,
		OwnerAccountID:       ownerAccountID,
		ExternalSubject:      strings.TrimSpace(options.ExternalSubject),
		Scope:                scope,
		GrantType:            grantType,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    f.issuer,
			Subject:   subject,
			Audience:  jwt.ClaimStrings{audience},
			ID:        jti,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-time.Minute)),
		},
	}
	if !options.OmitExp {
		claims.ExpiresAt = jwt.NewNumericDate(now.Add(ttl))
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(signingKey)
	if err != nil {
		t.Fatalf("sign workspace managed agent grant token: %v", err)
	}
	return signed
}

func (f *workspaceHumanGrantTestFixture) setJWKSStatusCode(status int) {
	f.jwksStateMu.Lock()
	f.jwksStatusCode = status
	f.jwksStateMu.Unlock()
}

func mustGeneratePublicKey(t *testing.T) string {
	t.Helper()
	publicKey, _ := generateKeyPair(t)
	return publicKey
}

func generateKeyPair(t *testing.T) (string, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key pair: %v", err)
	}
	return base64.StdEncoding.EncodeToString(publicKey), privateKey
}

func signAssertion(t *testing.T, privateKey ed25519.PrivateKey, agentID string, keyID string, signedAt string) string {
	t.Helper()
	message := auth.BuildAssertionMessage(agentID, keyID, signedAt)
	signature := ed25519.Sign(privateKey, []byte(message))
	return base64.StdEncoding.EncodeToString(signature)
}

func mapValue(raw any, key string) any {
	decoded, _ := raw.(map[string]any)
	if decoded == nil {
		return nil
	}
	return decoded[key]
}

func asBool(raw any) bool {
	value, _ := raw.(bool)
	return value
}

func postJSONExpectStatusWithAuth(t *testing.T, url string, payload any, accessToken string, expectedStatus int) *http.Response {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new POST request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(accessToken) != "" {
		request.Header.Set("Authorization", "Bearer "+accessToken)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("POST %s failed: %v", url, err)
	}
	if response.StatusCode != expectedStatus {
		defer response.Body.Close()
		var body map[string]any
		_ = json.NewDecoder(response.Body).Decode(&body)
		t.Fatalf("POST %s unexpected status: got %d want %d body=%#v", url, response.StatusCode, expectedStatus, body)
	}
	return response
}

func patchJSONExpectStatusWithAuth(t *testing.T, url string, payload any, accessToken string, expectedStatus int) *http.Response {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	headers := map[string]string{}
	if strings.TrimSpace(accessToken) != "" {
		headers["Authorization"] = "Bearer " + accessToken
	}
	request, err := http.NewRequest(http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new PATCH request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(accessToken) != "" {
		request.Header.Set("Authorization", "Bearer "+accessToken)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("PATCH %s failed: %v", url, err)
	}
	if response.StatusCode != expectedStatus {
		defer response.Body.Close()
		var body map[string]any
		_ = json.NewDecoder(response.Body).Decode(&body)
		t.Fatalf("PATCH %s unexpected status: got %d want %d body=%#v", url, response.StatusCode, expectedStatus, body)
	}
	return response
}

func postJSONExpectStatusWithHeaders(t *testing.T, url string, payload any, headers map[string]string, expectedStatus int) *http.Response {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new POST request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("POST %s failed: %v", url, err)
	}
	if response.StatusCode != expectedStatus {
		defer response.Body.Close()
		var decoded map[string]any
		_ = json.NewDecoder(response.Body).Decode(&decoded)
		t.Fatalf("POST %s unexpected status: got %d want %d body=%#v", url, response.StatusCode, expectedStatus, decoded)
	}
	return response
}

func getJSONExpectStatusWithAuth(t *testing.T, url string, accessToken string, expectedStatus int) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new GET request: %v", err)
	}
	if strings.TrimSpace(accessToken) != "" {
		request.Header.Set("Authorization", "Bearer "+accessToken)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET %s failed: %v", url, err)
	}
	if response.StatusCode != expectedStatus {
		defer response.Body.Close()
		var body map[string]any
		_ = json.NewDecoder(response.Body).Decode(&body)
		t.Fatalf("GET %s unexpected status: got %d want %d body=%#v", url, response.StatusCode, expectedStatus, body)
	}
	return response
}

func assertErrorCode(t *testing.T, response *http.Response, expectedCode string) {
	t.Helper()
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	if payload.Error.Code != expectedCode {
		t.Fatalf("unexpected error code: got %q want %q", payload.Error.Code, expectedCode)
	}
}

func mustDecodeStdBase64(t *testing.T, raw string) []byte {
	t.Helper()
	value, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("decode base64 %q: %v", raw, err)
	}
	return value
}

func mustDecodeJSONObject(t *testing.T, raw string) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode JSON object: %v", err)
	}
	return payload
}

func TestPasskeyDevBypassDisabledWithoutDedicatedFlag(t *testing.T) {
	requireIntegrationTest(t)
	t.Parallel()

	env := newAuthIntegrationEnv(t, authIntegrationOptions{
		bootstrapToken:        testBootstrapToken,
		enableDevActorMode:    true,
		allowPasskeyDevBypass: false,
	})
	serverURL := env.server.URL

	status := getBootstrapStatus(t, serverURL)
	if status.DevPasskeyBypassActive {
		t.Fatal("expected bootstrap status to report passkey dev bypass unavailable")
	}

	resp := postJSONExpectStatusWithHeaders(t, serverURL+"/auth/passkey/dev/register", map[string]any{
		"display_name":    "Dev Human",
		"bootstrap_token": testBootstrapToken,
	}, nil, http.StatusForbidden)
	defer resp.Body.Close()
	assertErrorCode(t, resp, "dev_passkey_bypass_disabled")
}

func TestPasskeyDevBypassCanRunWithoutDevActorMode(t *testing.T) {
	requireIntegrationTest(t)
	t.Parallel()

	env := newAuthIntegrationEnv(t, authIntegrationOptions{
		bootstrapToken:        testBootstrapToken,
		enableDevActorMode:    false,
		allowPasskeyDevBypass: true,
	})
	serverURL := env.server.URL

	status := getBootstrapStatus(t, serverURL)
	if !status.DevPasskeyBypassActive {
		t.Fatal("expected bootstrap status to report passkey dev bypass available")
	}

	regResp := postJSONExpectStatusWithHeaders(t, serverURL+"/auth/passkey/dev/register", map[string]any{
		"display_name":    "Dev Operator",
		"bootstrap_token": testBootstrapToken,
	}, nil, http.StatusCreated)
	defer regResp.Body.Close()
	var regPayload struct {
		Agent struct {
			AgentID       string  `json:"agent_id"`
			Username      string  `json:"username"`
			PrincipalKind *string `json:"principal_kind"`
			AuthMethod    *string `json:"auth_method"`
		} `json:"agent"`
		Tokens struct {
			AccessToken string `json:"access_token"`
		} `json:"tokens"`
	}
	if err := json.NewDecoder(regResp.Body).Decode(&regPayload); err != nil {
		t.Fatalf("decode dev register: %v", err)
	}
	if regPayload.Agent.AgentID == "" || regPayload.Tokens.AccessToken == "" {
		t.Fatalf("unexpected payload %#v", regPayload)
	}
	if regPayload.Agent.PrincipalKind == nil || *regPayload.Agent.PrincipalKind != "human" {
		t.Fatalf("expected human principal, got %#v", regPayload.Agent)
	}
	if regPayload.Agent.AuthMethod == nil || *regPayload.Agent.AuthMethod != auth.AuthMethodPasskey {
		t.Fatalf("expected passkey auth_method, got %#v", regPayload.Agent)
	}

	loginResp := postJSONExpectStatusWithHeaders(t, serverURL+"/auth/passkey/dev/login", map[string]any{}, nil, http.StatusOK)
	defer loginResp.Body.Close()
	var loginPayload struct {
		Agent struct {
			AgentID string `json:"agent_id"`
		} `json:"agent"`
		Tokens struct {
			AccessToken string `json:"access_token"`
		} `json:"tokens"`
	}
	if err := json.NewDecoder(loginResp.Body).Decode(&loginPayload); err != nil {
		t.Fatalf("decode dev login: %v", err)
	}
	if loginPayload.Agent.AgentID != regPayload.Agent.AgentID {
		t.Fatalf("expected same agent, got %q vs %q", loginPayload.Agent.AgentID, regPayload.Agent.AgentID)
	}
	if loginPayload.Tokens.AccessToken == "" {
		t.Fatal("expected access token on dev login")
	}
}

func TestPasskeyDevRegisterLinkedSeedActorAndSoleLogin(t *testing.T) {
	requireIntegrationTest(t)
	t.Parallel()

	env := newAuthIntegrationEnv(t, authIntegrationOptions{
		bootstrapToken:              testBootstrapToken,
		enableDevActorMode:          false,
		allowPasskeyDevBypass:       true,
		allowDevRegisterLinkedActor: true,
	})
	serverURL := env.server.URL

	ctx := context.Background()
	_, err := env.workspace.DB().ExecContext(ctx, `
		INSERT INTO actors(id, display_name, tags_json, created_at, metadata_json)
		VALUES (?, ?, ?, ?, ?)`,
		"actor-dev-human-operator",
		"Jordan (Human operator)",
		`["human","operator"]`,
		"2026-01-01T07:55:00.000Z",
		`{}`,
	)
	if err != nil {
		t.Fatalf("seed fixture actor: %v", err)
	}

	regResp := postJSONExpectStatusWithHeaders(t, serverURL+"/auth/passkey/dev/register", map[string]any{
		"display_name":      "Jordan (Human operator)",
		"bootstrap_token":   testBootstrapToken,
		"existing_actor_id": "actor-dev-human-operator",
	}, nil, http.StatusCreated)
	defer regResp.Body.Close()

	loginResp := postJSONExpectStatusWithHeaders(t, serverURL+"/auth/passkey/dev/login", map[string]any{}, nil, http.StatusOK)
	defer loginResp.Body.Close()
	var loginPayload struct {
		Agent struct {
			ActorID string `json:"actor_id"`
		} `json:"agent"`
	}
	if err := json.NewDecoder(loginResp.Body).Decode(&loginPayload); err != nil {
		t.Fatalf("decode dev login: %v", err)
	}
	if loginPayload.Agent.ActorID != "actor-dev-human-operator" {
		t.Fatalf("expected linked actor id, got %q", loginPayload.Agent.ActorID)
	}
}

func TestPasskeyDevLoginRequiresDisambiguation(t *testing.T) {
	requireIntegrationTest(t)
	t.Parallel()

	env := newAuthIntegrationEnv(t, authIntegrationOptions{
		bootstrapToken:        testBootstrapToken,
		enableDevActorMode:    false,
		allowPasskeyDevBypass: true,
	})
	serverURL := env.server.URL

	reg1Resp := postJSONExpectStatusWithHeaders(t, serverURL+"/auth/passkey/dev/register", map[string]any{
		"display_name":    "Operator One",
		"bootstrap_token": testBootstrapToken,
	}, nil, http.StatusCreated)
	defer reg1Resp.Body.Close()
	var reg1 struct {
		Agent struct {
			Username string `json:"username"`
		} `json:"agent"`
		Tokens struct {
			AccessToken string `json:"access_token"`
		} `json:"tokens"`
	}
	if err := json.NewDecoder(reg1Resp.Body).Decode(&reg1); err != nil {
		t.Fatalf("decode first dev register: %v", err)
	}

	_, inviteToken := createInvite(t, serverURL, reg1.Tokens.AccessToken, map[string]any{
		"kind": "human",
	})

	reg2Resp := postJSONExpectStatusWithHeaders(t, serverURL+"/auth/passkey/dev/register", map[string]any{
		"display_name": "Operator Two",
		"invite_token": inviteToken,
	}, nil, http.StatusCreated)
	defer reg2Resp.Body.Close()

	ambResp := postJSONExpectStatusWithHeaders(t, serverURL+"/auth/passkey/dev/login", map[string]any{}, nil, http.StatusBadRequest)
	defer ambResp.Body.Close()
	assertErrorCode(t, ambResp, "invalid_request")

	okResp := postJSONExpectStatusWithHeaders(t, serverURL+"/auth/passkey/dev/login", map[string]any{
		"username": reg1.Agent.Username,
	}, nil, http.StatusOK)
	defer okResp.Body.Close()
	var okPayload struct {
		Agent struct {
			Username string `json:"username"`
		} `json:"agent"`
	}
	if err := json.NewDecoder(okResp.Body).Decode(&okPayload); err != nil {
		t.Fatalf("decode disambiguated dev login: %v", err)
	}
	if okPayload.Agent.Username != reg1.Agent.Username {
		t.Fatalf("expected username %q, got %q", reg1.Agent.Username, okPayload.Agent.Username)
	}
}

const samplePasskeyCredentialJSON = `{
  "id": "6Jry73M_WVWDoXLsGxRsBVVHpPWDpNy1ETGXUEvJLdTAn5Ew6nDGU6W8iO3ZkcLEqr-CBwvx0p2WAxzt8RiwQQ",
  "rawId": "6Jry73M_WVWDoXLsGxRsBVVHpPWDpNy1ETGXUEvJLdTAn5Ew6nDGU6W8iO3ZkcLEqr-CBwvx0p2WAxzt8RiwQQ",
  "response": {
    "attestationObject": "o2NmbXRkbm9uZWdhdHRTdG10oGhhdXRoRGF0YVjEdKbqkhPJnC90siSSsyDPQCYqlMGpUKA5fyklC2CEHvBBAAAAAAAAAAAAAAAAAAAAAAAAAAAAQOia8u9zP1lVg6Fy7BsUbAVVR6T1g6TctRExl1BLyS3UwJ-RMOpwxlOlvIjt2ZHCxKq_ggcL8dKdlgMc7fEYsEGlAQIDJiABIVgg--n_QvZithDycYmnifk6vMHiwBP6kugn2PlsnvkrcSgiWCBAlBYm2B-rMtQlp5MxGTLoGDHoktxb0p364Hy2BH9U2Q",
    "clientDataJSON": "eyJjaGFsbGVuZ2UiOiJzVnQ0U2NjZU16cUZTbmZBcThoZ0x6Ymx2bzNmYTRfYUZWRWNJRVNISUowIiwib3JpZ2luIjoiaHR0cHM6Ly93ZWJhdXRobi5pbyIsInR5cGUiOiJ3ZWJhdXRobi5jcmVhdGUifQ"
  },
  "type": "public-key"
}`
