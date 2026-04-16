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
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"organization-autorunner-core/internal/actors"
	"organization-autorunner-core/internal/auth"
	"organization-autorunner-core/internal/blob"
	"organization-autorunner-core/internal/primitives"
	"organization-autorunner-core/internal/schema"
	"organization-autorunner-core/internal/storage"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

const testBootstrapToken = "bootstrap-token-for-tests"

type authIntegrationOptions struct {
	bootstrapToken              string
	enableDevActorMode          bool
	allowPasskeyDevBypass       bool
	allowDevRegisterLinkedActor bool
	allowUnauthenticatedWrites  bool
	webAuthnConfig              WebAuthnConfig
	workspaceID                 string
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
	t.Helper()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}

	registry := actors.NewStore(workspace.DB())
	if _, err := registry.EnsureSystemActor(context.Background(), time.Now().UTC()); err != nil {
		_ = workspace.Close()
		t.Fatalf("ensure system actor: %v", err)
	}

	authOptions := make([]auth.Option, 0, 1)
	if strings.TrimSpace(options.bootstrapToken) != "" {
		authOptions = append(authOptions, auth.WithBootstrapToken(options.bootstrapToken))
	}
	if options.allowDevRegisterLinkedActor {
		authOptions = append(authOptions, auth.WithAllowDevRegisterLinkedActor(true))
	}
	authStore := auth.NewStore(workspace.DB(), authOptions...)
	passkeySessionStore := auth.NewPasskeySessionStore(auth.DefaultPasskeySessionTTL)

	contractPath := filepath.Join("..", "..", "..", "contracts", "oar-schema.yaml")
	contract, err := schema.Load(contractPath)
	if err != nil {
		passkeySessionStore.Close()
		_ = workspace.Close()
		t.Fatalf("load schema contract: %v", err)
	}

	primitiveStore := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	workspaceID := strings.TrimSpace(options.workspaceID)
	if workspaceID == "" {
		workspaceID = "ws_main"
	}
	handler := NewHandler(
		"0.2.2",
		WithActorRegistry(registry),
		WithAuthStore(authStore),
		WithPasskeySessionStore(passkeySessionStore),
		WithHealthCheck(workspace.Ping),
		WithPrimitiveStore(primitiveStore),
		WithSchemaContract(contract),
		WithWebAuthnConfig(options.webAuthnConfig),
		WithWorkspaceID(workspaceID),
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
		registry:            registry,
		authStore:           authStore,
		passkeySessionStore: passkeySessionStore,
		server:              server,
		primitiveStore:      primitiveStore,
	}
}

func authTestMinimalTopic(title string) map[string]any {
	return map[string]any{
		"type":          "initiative",
		"status":        "active",
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

func TestAgentAuthLifecycleAndActorCompatibility(t *testing.T) {
	t.Parallel()

	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	serverURL := env.server.URL

	publicKey1, privateKey1 := generateKeyPair(t)
	registerResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/agents/register", map[string]any{
		"username":        "Agent.One",
		"public_key":      publicKey1,
		"bootstrap_token": testBootstrapToken,
	}, "", http.StatusCreated)
	defer registerResp.Body.Close()

	var registerPayload struct {
		Agent struct {
			AgentID  string `json:"agent_id"`
			Username string `json:"username"`
			ActorID  string `json:"actor_id"`
		} `json:"agent"`
		Key struct {
			KeyID string `json:"key_id"`
		} `json:"key"`
		Tokens struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		} `json:"tokens"`
	}
	if err := json.NewDecoder(registerResp.Body).Decode(&registerPayload); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	if registerPayload.Agent.AgentID == "" || registerPayload.Agent.ActorID == "" || registerPayload.Key.KeyID == "" {
		t.Fatalf("unexpected register payload: %#v", registerPayload)
	}
	if registerPayload.Agent.Username != "agent.one" {
		t.Fatalf("expected normalized username agent.one, got %q", registerPayload.Agent.Username)
	}

	meResp := getJSONExpectStatusWithAuth(t, serverURL+"/agents/me", registerPayload.Tokens.AccessToken, http.StatusOK)
	defer meResp.Body.Close()
	var mePayload struct {
		Agent map[string]any   `json:"agent"`
		Keys  []map[string]any `json:"keys"`
	}
	if err := json.NewDecoder(meResp.Body).Decode(&mePayload); err != nil {
		t.Fatalf("decode me response: %v", err)
	}
	if asString(mePayload.Agent["agent_id"]) != registerPayload.Agent.AgentID {
		t.Fatalf("unexpected me agent id: %#v", mePayload.Agent)
	}
	if len(mePayload.Keys) != 1 {
		t.Fatalf("expected one key, got %#v", mePayload.Keys)
	}

	patchResp := patchJSONExpectStatusWithAuth(t, serverURL+"/agents/me", map[string]any{"username": "renamed_agent"}, registerPayload.Tokens.AccessToken, http.StatusOK)
	defer patchResp.Body.Close()
	var patchPayload struct {
		Agent map[string]any `json:"agent"`
	}
	if err := json.NewDecoder(patchResp.Body).Decode(&patchPayload); err != nil {
		t.Fatalf("decode patch response: %v", err)
	}
	if asString(patchPayload.Agent["username"]) != "renamed_agent" {
		t.Fatalf("unexpected patched username: %#v", patchPayload.Agent)
	}

	inviteToken := createInviteToken(t, serverURL, registerPayload.Tokens.AccessToken, map[string]any{
		"kind": "agent",
	})

	dupResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/agents/register", map[string]any{
		"username":     "Renamed_Agent",
		"public_key":   publicKey1,
		"invite_token": inviteToken,
	}, "", http.StatusConflict)
	defer dupResp.Body.Close()
	assertErrorCode(t, dupResp, "username_taken")

	refreshResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/token", map[string]any{
		"grant_type":    "refresh_token",
		"refresh_token": registerPayload.Tokens.RefreshToken,
	}, "", http.StatusOK)
	defer refreshResp.Body.Close()
	var refreshPayload struct {
		Tokens struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		} `json:"tokens"`
	}
	if err := json.NewDecoder(refreshResp.Body).Decode(&refreshPayload); err != nil {
		t.Fatalf("decode refresh response: %v", err)
	}

	oldRefreshResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/token", map[string]any{
		"grant_type":    "refresh_token",
		"refresh_token": registerPayload.Tokens.RefreshToken,
	}, "", http.StatusUnauthorized)
	defer oldRefreshResp.Body.Close()
	assertErrorCode(t, oldRefreshResp, "invalid_token")

	if _, err := env.registry.Register(context.Background(), actors.Actor{
		ID:          "human-actor",
		DisplayName: "Human Actor",
		CreatedAt:   "2026-03-05T10:00:00Z",
	}); err != nil {
		t.Fatalf("seed human actor: %v", err)
	}

	topicResp := postJSONExpectStatusWithAuth(t, serverURL+"/topics", map[string]any{
		"topic": authTestMinimalTopic("Auth-backed thread"),
	}, refreshPayload.Tokens.AccessToken, http.StatusCreated)
	defer topicResp.Body.Close()
	var createdTopic struct {
		Topic map[string]any `json:"topic"`
	}
	if err := json.NewDecoder(topicResp.Body).Decode(&createdTopic); err != nil {
		t.Fatalf("decode topic response: %v", err)
	}
	if got := authTestThreadUpdatedBy(t, serverURL, refreshPayload.Tokens.AccessToken, createdTopic.Topic); got != registerPayload.Agent.ActorID {
		t.Fatalf("expected backing thread updated_by to use authenticated actor mapping, got %q", got)
	}

	mismatchResp := postJSONExpectStatusWithAuth(t, serverURL+"/topics", map[string]any{
		"actor_id": "human-actor",
		"topic":    authTestMinimalTopic("Mismatch thread"),
	}, refreshPayload.Tokens.AccessToken, http.StatusForbidden)
	defer mismatchResp.Body.Close()
	assertErrorCode(t, mismatchResp, "key_mismatch")

	assertionSignedAt := time.Now().UTC().Format(time.RFC3339)
	assertionSig := signAssertion(t, privateKey1, registerPayload.Agent.AgentID, registerPayload.Key.KeyID, assertionSignedAt)
	assertionResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/token", map[string]any{
		"grant_type": "assertion",
		"agent_id":   registerPayload.Agent.AgentID,
		"key_id":     registerPayload.Key.KeyID,
		"signed_at":  assertionSignedAt,
		"signature":  assertionSig,
	}, "", http.StatusOK)
	defer assertionResp.Body.Close()
	var assertionPayload struct {
		Tokens struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		} `json:"tokens"`
	}
	if err := json.NewDecoder(assertionResp.Body).Decode(&assertionPayload); err != nil {
		t.Fatalf("decode assertion response: %v", err)
	}
	replayResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/token", map[string]any{
		"grant_type": "assertion",
		"agent_id":   registerPayload.Agent.AgentID,
		"key_id":     registerPayload.Key.KeyID,
		"signed_at":  assertionSignedAt,
		"signature":  assertionSig,
	}, "", http.StatusUnauthorized)
	defer replayResp.Body.Close()
	assertErrorCode(t, replayResp, "key_mismatch")

	publicKey2, privateKey2 := generateKeyPair(t)
	rotateResp := postJSONExpectStatusWithAuth(t, serverURL+"/agents/me/keys/rotate", map[string]any{
		"public_key": publicKey2,
	}, assertionPayload.Tokens.AccessToken, http.StatusOK)
	defer rotateResp.Body.Close()
	var rotatePayload struct {
		Key struct {
			KeyID string `json:"key_id"`
		} `json:"key"`
	}
	if err := json.NewDecoder(rotateResp.Body).Decode(&rotatePayload); err != nil {
		t.Fatalf("decode rotate response: %v", err)
	}
	if rotatePayload.Key.KeyID == "" {
		t.Fatal("expected rotated key id")
	}

	oldSignedAt := time.Now().UTC().Format(time.RFC3339)
	oldAssertionResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/token", map[string]any{
		"grant_type": "assertion",
		"agent_id":   registerPayload.Agent.AgentID,
		"key_id":     registerPayload.Key.KeyID,
		"signed_at":  oldSignedAt,
		"signature":  signAssertion(t, privateKey1, registerPayload.Agent.AgentID, registerPayload.Key.KeyID, oldSignedAt),
	}, "", http.StatusUnauthorized)
	defer oldAssertionResp.Body.Close()
	assertErrorCode(t, oldAssertionResp, "key_mismatch")

	newSignedAt := time.Now().UTC().Format(time.RFC3339)
	newAssertionResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/token", map[string]any{
		"grant_type": "assertion",
		"agent_id":   registerPayload.Agent.AgentID,
		"key_id":     rotatePayload.Key.KeyID,
		"signed_at":  newSignedAt,
		"signature":  signAssertion(t, privateKey2, registerPayload.Agent.AgentID, rotatePayload.Key.KeyID, newSignedAt),
	}, "", http.StatusOK)
	defer newAssertionResp.Body.Close()
	var newAssertionPayload struct {
		Tokens struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		} `json:"tokens"`
	}
	if err := json.NewDecoder(newAssertionResp.Body).Decode(&newAssertionPayload); err != nil {
		t.Fatalf("decode new assertion response: %v", err)
	}

	revokeResp := postJSONExpectStatusWithAuth(t, serverURL+"/agents/me/revoke", map[string]any{}, newAssertionPayload.Tokens.AccessToken, http.StatusOK)
	defer revokeResp.Body.Close()

	revokedRefreshResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/token", map[string]any{
		"grant_type":    "refresh_token",
		"refresh_token": newAssertionPayload.Tokens.RefreshToken,
	}, "", http.StatusForbidden)
	defer revokedRefreshResp.Body.Close()
	assertErrorCode(t, revokedRefreshResp, "agent_revoked")

	revokedMeResp := getJSONExpectStatusWithAuth(t, serverURL+"/agents/me", newAssertionPayload.Tokens.AccessToken, http.StatusForbidden)
	defer revokedMeResp.Body.Close()
	assertErrorCode(t, revokedMeResp, "agent_revoked")
}

func TestListAuthPrincipalsIncludesDerivedWakeRouting(t *testing.T) {
	t.Parallel()

	env := newAuthIntegrationEnv(t, authIntegrationOptions{
		bootstrapToken: testBootstrapToken,
		workspaceID:    "ws_local",
	})
	serverURL := env.server.URL

	publicKey, _ := generateKeyPair(t)
	registerResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/agents/register", map[string]any{
		"username":        "m4-hermes",
		"public_key":      publicKey,
		"bootstrap_token": testBootstrapToken,
	}, "", http.StatusCreated)
	defer registerResp.Body.Close()

	var registerPayload struct {
		Agent struct {
			AgentID string `json:"agent_id"`
			ActorID string `json:"actor_id"`
		} `json:"agent"`
		Tokens struct {
			AccessToken string `json:"access_token"`
		} `json:"tokens"`
	}
	if err := json.NewDecoder(registerResp.Body).Decode(&registerPayload); err != nil {
		t.Fatalf("decode register response: %v", err)
	}

	if _, err := env.authStore.UpdateRegistration(context.Background(), registerPayload.Agent.AgentID, auth.AgentRegistration{
		Handle:            "m4-hermes",
		ActorID:           registerPayload.Agent.ActorID,
		Status:            "active",
		BridgeInstanceID:  "bridge-hermes-1",
		BridgeCheckedInAt: "2099-03-20T12:00:00Z",
		BridgeExpiresAt:   "2099-03-20T12:05:00Z",
		WorkspaceBindings: []auth.AgentRegistrationWorkspaceBinding{
			{WorkspaceID: "ws_local", Enabled: true},
		},
	}); err != nil {
		t.Fatalf("update registration: %v", err)
	}

	page := listAuthPrincipalsPage(t, serverURL, registerPayload.Tokens.AccessToken, 50, "")
	if len(page.Principals) != 1 {
		t.Fatalf("expected one principal, got %#v", page.Principals)
	}
	wakeRouting, ok := page.Principals[0]["wake_routing"].(map[string]any)
	if !ok {
		t.Fatalf("expected wake_routing object, got %#v", page.Principals[0])
	}
	if got := asString(wakeRouting["state"]); got != "online" {
		t.Fatalf("expected online wake routing state, got %#v", wakeRouting)
	}
	if got := asString(wakeRouting["summary"]); got != "Online as @m4-hermes." {
		t.Fatalf("unexpected wake routing summary: %#v", wakeRouting)
	}
	if online, _ := wakeRouting["online"].(bool); !online {
		t.Fatalf("expected online=true, got %#v", wakeRouting)
	}
}

func TestBootstrapAndInviteGatedRegistrationFlow(t *testing.T) {
	t.Parallel()

	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	serverURL := env.server.URL

	if status := getBootstrapStatus(t, serverURL); !status.Available {
		t.Fatal("expected bootstrap registration to be available before first principal")
	}

	noTokenResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/agents/register", map[string]any{
		"username":   "no.token",
		"public_key": mustGeneratePublicKey(t),
	}, "", http.StatusBadRequest)
	defer noTokenResp.Body.Close()
	assertErrorCode(t, noTokenResp, "invalid_request")

	passkeyNoTokenResp := postJSONExpectStatusWithHeaders(t, serverURL+"/auth/passkey/register/options", map[string]any{
		"display_name": "Casey Human",
	}, nil, http.StatusBadRequest)
	defer passkeyNoTokenResp.Body.Close()
	assertErrorCode(t, passkeyNoTokenResp, "invalid_request")

	firstPublicKey, _ := generateKeyPair(t)
	firstResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/agents/register", map[string]any{
		"username":        "bootstrap.first",
		"public_key":      firstPublicKey,
		"bootstrap_token": testBootstrapToken,
	}, "", http.StatusCreated)
	defer firstResp.Body.Close()

	var firstPayload struct {
		Tokens struct {
			AccessToken string `json:"access_token"`
		} `json:"tokens"`
	}
	if err := json.NewDecoder(firstResp.Body).Decode(&firstPayload); err != nil {
		t.Fatalf("decode bootstrap register response: %v", err)
	}

	if status := getBootstrapStatus(t, serverURL); status.Available {
		t.Fatal("expected bootstrap registration to be unavailable after first principal")
	}

	secondBootstrapResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/agents/register", map[string]any{
		"username":        "bootstrap.second",
		"public_key":      mustGeneratePublicKey(t),
		"bootstrap_token": testBootstrapToken,
	}, "", http.StatusUnauthorized)
	defer secondBootstrapResp.Body.Close()
	assertErrorCode(t, secondBootstrapResp, "invalid_token")

	missingInviteResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/agents/register", map[string]any{
		"username":   "invite.required",
		"public_key": mustGeneratePublicKey(t),
	}, "", http.StatusBadRequest)
	defer missingInviteResp.Body.Close()
	assertErrorCode(t, missingInviteResp, "invalid_request")

	invite, inviteToken := createInvite(t, serverURL, firstPayload.Tokens.AccessToken, map[string]any{
		"kind": "agent",
	})
	if invite["token"] != nil {
		t.Fatalf("invite metadata unexpectedly included token: %#v", invite)
	}

	secondResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/agents/register", map[string]any{
		"username":     "invite.agent",
		"public_key":   mustGeneratePublicKey(t),
		"invite_token": inviteToken,
	}, "", http.StatusCreated)
	secondResp.Body.Close()

	invites := listInvites(t, serverURL, firstPayload.Tokens.AccessToken)
	if len(invites) == 0 {
		t.Fatal("expected invite list to include created invite")
	}
	matched := false
	for _, listed := range invites {
		if asString(listed["id"]) != asString(invite["id"]) {
			continue
		}
		matched = true
		if asString(listed["kind"]) != "agent" {
			t.Fatalf("unexpected invite kind: %#v", listed)
		}
		if listed["token"] != nil {
			t.Fatalf("listed invite unexpectedly exposed token: %#v", listed)
		}
		if asString(listed["consumed_at"]) == "" {
			t.Fatalf("expected consumed invite metadata after registration: %#v", listed)
		}
	}
	if !matched {
		t.Fatalf("expected invite %q in list %#v", asString(invite["id"]), invites)
	}
}

func TestInviteLifecycleRejectsExpiredRevokedWrongKindAndReuse(t *testing.T) {
	t.Parallel()

	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	serverURL := env.server.URL

	bootstrapResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/agents/register", map[string]any{
		"username":        "invite.admin",
		"public_key":      mustGeneratePublicKey(t),
		"bootstrap_token": testBootstrapToken,
	}, "", http.StatusCreated)
	defer bootstrapResp.Body.Close()

	var bootstrapPayload struct {
		Tokens struct {
			AccessToken string `json:"access_token"`
		} `json:"tokens"`
	}
	if err := json.NewDecoder(bootstrapResp.Body).Decode(&bootstrapPayload); err != nil {
		t.Fatalf("decode bootstrap response: %v", err)
	}

	singleUseInvite, singleUseToken := createInvite(t, serverURL, bootstrapPayload.Tokens.AccessToken, map[string]any{
		"kind": "agent",
	})
	firstUseResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/agents/register", map[string]any{
		"username":     "single.use",
		"public_key":   mustGeneratePublicKey(t),
		"invite_token": singleUseToken,
	}, "", http.StatusCreated)
	firstUseResp.Body.Close()

	reuseResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/agents/register", map[string]any{
		"username":     "single.use.again",
		"public_key":   mustGeneratePublicKey(t),
		"invite_token": singleUseToken,
	}, "", http.StatusUnauthorized)
	defer reuseResp.Body.Close()
	assertErrorCode(t, reuseResp, "invalid_token")

	wrongKindInvite, wrongKindToken := createInvite(t, serverURL, bootstrapPayload.Tokens.AccessToken, map[string]any{
		"kind": "human",
	})
	wrongKindResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/agents/register", map[string]any{
		"username":     "wrong.kind",
		"public_key":   mustGeneratePublicKey(t),
		"invite_token": wrongKindToken,
	}, "", http.StatusUnauthorized)
	defer wrongKindResp.Body.Close()
	assertErrorCode(t, wrongKindResp, "invalid_token")

	revokedInvite, revokedToken := createInvite(t, serverURL, bootstrapPayload.Tokens.AccessToken, map[string]any{
		"kind": "agent",
	})
	revokeResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/invites/"+asString(revokedInvite["id"])+"/revoke", map[string]any{}, bootstrapPayload.Tokens.AccessToken, http.StatusOK)
	defer revokeResp.Body.Close()

	revokedUseResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/agents/register", map[string]any{
		"username":     "revoked.use",
		"public_key":   mustGeneratePublicKey(t),
		"invite_token": revokedToken,
	}, "", http.StatusUnauthorized)
	defer revokedUseResp.Body.Close()
	assertErrorCode(t, revokedUseResp, "invalid_token")

	expiredInvite, expiredToken := createInvite(t, serverURL, bootstrapPayload.Tokens.AccessToken, map[string]any{
		"kind": "agent",
	})
	if _, err := env.workspace.DB().ExecContext(
		context.Background(),
		`UPDATE auth_invites SET expires_at = ? WHERE id = ?`,
		time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano),
		asString(expiredInvite["id"]),
	); err != nil {
		t.Fatalf("force invite expiry: %v", err)
	}
	expiredUseResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/agents/register", map[string]any{
		"username":     "expired.use",
		"public_key":   mustGeneratePublicKey(t),
		"invite_token": expiredToken,
	}, "", http.StatusUnauthorized)
	defer expiredUseResp.Body.Close()
	assertErrorCode(t, expiredUseResp, "invalid_token")

	_ = singleUseInvite
	_ = wrongKindInvite
}

func TestAuthAuditAndPrincipalInventoryVisibility(t *testing.T) {
	t.Parallel()

	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	serverURL := env.server.URL

	adminPublicKey, _ := generateKeyPair(t)
	adminResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/agents/register", map[string]any{
		"username":        "audit.admin",
		"public_key":      adminPublicKey,
		"bootstrap_token": testBootstrapToken,
	}, "", http.StatusCreated)
	defer adminResp.Body.Close()

	var adminPayload struct {
		Agent struct {
			AgentID string `json:"agent_id"`
			ActorID string `json:"actor_id"`
		} `json:"agent"`
		Tokens struct {
			AccessToken string `json:"access_token"`
		} `json:"tokens"`
	}
	if err := json.NewDecoder(adminResp.Body).Decode(&adminPayload); err != nil {
		t.Fatalf("decode admin register response: %v", err)
	}

	consumedInvite, consumedToken := createInvite(t, serverURL, adminPayload.Tokens.AccessToken, map[string]any{
		"kind": "agent",
	})

	secondResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/agents/register", map[string]any{
		"username":     "audit.member",
		"public_key":   mustGeneratePublicKey(t),
		"invite_token": consumedToken,
	}, "", http.StatusCreated)
	defer secondResp.Body.Close()

	var secondPayload struct {
		Agent struct {
			AgentID string `json:"agent_id"`
			ActorID string `json:"actor_id"`
		} `json:"agent"`
		Tokens struct {
			AccessToken string `json:"access_token"`
		} `json:"tokens"`
	}
	if err := json.NewDecoder(secondResp.Body).Decode(&secondPayload); err != nil {
		t.Fatalf("decode second register response: %v", err)
	}

	revokedInvite, _ := createInvite(t, serverURL, adminPayload.Tokens.AccessToken, map[string]any{
		"kind": "agent",
	})
	revokeInviteResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/invites/"+asString(revokedInvite["id"])+"/revoke", map[string]any{}, adminPayload.Tokens.AccessToken, http.StatusOK)
	revokeInviteResp.Body.Close()

	revokePrincipalResp := postJSONExpectStatusWithAuth(t, serverURL+"/agents/me/revoke", map[string]any{}, secondPayload.Tokens.AccessToken, http.StatusOK)
	revokePrincipalResp.Body.Close()

	invites := listInvites(t, serverURL, adminPayload.Tokens.AccessToken)
	consumedInviteID := asString(consumedInvite["id"])
	revokedInviteID := asString(revokedInvite["id"])

	var consumedInviteRow map[string]any
	var revokedInviteRow map[string]any
	for _, invite := range invites {
		switch asString(invite["id"]) {
		case consumedInviteID:
			consumedInviteRow = invite
		case revokedInviteID:
			revokedInviteRow = invite
		}
	}
	if consumedInviteRow == nil {
		t.Fatalf("expected consumed invite %q in list %#v", consumedInviteID, invites)
	}
	if asString(consumedInviteRow["created_by_agent_id"]) != adminPayload.Agent.AgentID {
		t.Fatalf("unexpected consumed invite creator: %#v", consumedInviteRow)
	}
	if asString(consumedInviteRow["consumed_by_agent_id"]) != secondPayload.Agent.AgentID {
		t.Fatalf("unexpected consumed invite consumer: %#v", consumedInviteRow)
	}
	if asString(consumedInviteRow["consumed_by_actor_id"]) != secondPayload.Agent.ActorID {
		t.Fatalf("unexpected consumed invite consumer actor: %#v", consumedInviteRow)
	}
	if asString(consumedInviteRow["consumed_at"]) == "" {
		t.Fatalf("expected consumed invite timestamp: %#v", consumedInviteRow)
	}

	if revokedInviteRow == nil {
		t.Fatalf("expected revoked invite %q in list %#v", revokedInviteID, invites)
	}
	if asString(revokedInviteRow["revoked_by_agent_id"]) != adminPayload.Agent.AgentID {
		t.Fatalf("unexpected revoked invite actor: %#v", revokedInviteRow)
	}
	if asString(revokedInviteRow["revoked_by_actor_id"]) != adminPayload.Agent.ActorID {
		t.Fatalf("unexpected revoked invite actor_id: %#v", revokedInviteRow)
	}
	if asString(revokedInviteRow["revoked_at"]) == "" {
		t.Fatalf("expected revoked invite timestamp: %#v", revokedInviteRow)
	}

	firstPrincipalPage := listAuthPrincipalsPage(t, serverURL, adminPayload.Tokens.AccessToken, 1, "")
	if len(firstPrincipalPage.Principals) != 1 {
		t.Fatalf("expected one principal on first page, got %#v", firstPrincipalPage)
	}
	if asString(firstPrincipalPage.Principals[0]["agent_id"]) != secondPayload.Agent.AgentID {
		t.Fatalf("expected newest principal first, got %#v", firstPrincipalPage.Principals)
	}
	if !asBool(firstPrincipalPage.Principals[0]["revoked"]) {
		t.Fatalf("expected revoked principal to remain visible: %#v", firstPrincipalPage.Principals[0])
	}
	if firstPrincipalPage.NextCursor == "" {
		t.Fatal("expected next_cursor for principals pagination")
	}

	secondPrincipalPage := listAuthPrincipalsPage(t, serverURL, adminPayload.Tokens.AccessToken, 1, firstPrincipalPage.NextCursor)
	if len(secondPrincipalPage.Principals) != 1 {
		t.Fatalf("expected one principal on second page, got %#v", secondPrincipalPage)
	}
	if asString(secondPrincipalPage.Principals[0]["agent_id"]) != adminPayload.Agent.AgentID {
		t.Fatalf("expected admin principal on second page, got %#v", secondPrincipalPage.Principals)
	}
	if asBool(secondPrincipalPage.Principals[0]["revoked"]) {
		t.Fatalf("expected admin principal to remain active: %#v", secondPrincipalPage.Principals[0])
	}

	auditPage := listAuthAuditPage(t, serverURL, adminPayload.Tokens.AccessToken, 3, "")
	if len(auditPage.Events) != 3 {
		t.Fatalf("expected three audit events on first page, got %#v", auditPage)
	}
	if auditPage.NextCursor == "" {
		t.Fatal("expected next_cursor for audit pagination")
	}
	if asString(auditPage.Events[0]["event_type"]) != auth.AuthAuditEventPrincipalSelfRevoked {
		t.Fatalf("expected newest audit event to be principal_self_revoked, got %#v", auditPage.Events[0])
	}

	allEvents := append([]map[string]any{}, auditPage.Events...)
	cursor := auditPage.NextCursor
	for cursor != "" {
		page := listAuthAuditPage(t, serverURL, adminPayload.Tokens.AccessToken, 3, cursor)
		allEvents = append(allEvents, page.Events...)
		cursor = page.NextCursor
	}

	if len(allEvents) != 8 {
		t.Fatalf("expected 8 audit events, got %d: %#v", len(allEvents), allEvents)
	}

	findEvent := func(eventType string, inviteID string, subjectAgentID string) map[string]any {
		for _, event := range allEvents {
			if asString(event["event_type"]) != eventType {
				continue
			}
			if inviteID != "" && asString(event["invite_id"]) != inviteID {
				continue
			}
			if subjectAgentID != "" && asString(event["subject_agent_id"]) != subjectAgentID {
				continue
			}
			return event
		}
		return nil
	}

	bootstrapEvent := findEvent(auth.AuthAuditEventBootstrapConsumed, "", adminPayload.Agent.AgentID)
	if bootstrapEvent == nil {
		t.Fatal("expected bootstrap_consumed audit event")
	}
	if asString(mapValue(bootstrapEvent["metadata"], "principal_kind")) != "agent" {
		t.Fatalf("unexpected bootstrap audit metadata: %#v", bootstrapEvent)
	}

	inviteCreateEvent := findEvent(auth.AuthAuditEventInviteCreated, consumedInviteID, "")
	if inviteCreateEvent == nil {
		t.Fatal("expected invite_created event for consumed invite")
	}
	if asString(inviteCreateEvent["actor_agent_id"]) != adminPayload.Agent.AgentID {
		t.Fatalf("unexpected invite_created actor: %#v", inviteCreateEvent)
	}

	inviteConsumedEvent := findEvent(auth.AuthAuditEventInviteConsumed, consumedInviteID, secondPayload.Agent.AgentID)
	if inviteConsumedEvent == nil {
		t.Fatal("expected invite_consumed event")
	}
	if asString(inviteConsumedEvent["actor_agent_id"]) != secondPayload.Agent.AgentID {
		t.Fatalf("unexpected invite_consumed actor: %#v", inviteConsumedEvent)
	}
	if asString(mapValue(inviteConsumedEvent["metadata"], "invite_kind")) != "agent" {
		t.Fatalf("unexpected invite_consumed metadata: %#v", inviteConsumedEvent)
	}

	principalRegisteredEvent := findEvent(auth.AuthAuditEventPrincipalRegistered, consumedInviteID, secondPayload.Agent.AgentID)
	if principalRegisteredEvent == nil {
		t.Fatal("expected principal_registered event for invited principal")
	}
	if asString(mapValue(principalRegisteredEvent["metadata"], "auth_method")) != "public_key" {
		t.Fatalf("unexpected principal_registered metadata: %#v", principalRegisteredEvent)
	}

	inviteRevokedEvent := findEvent(auth.AuthAuditEventInviteRevoked, revokedInviteID, "")
	if inviteRevokedEvent == nil {
		t.Fatal("expected invite_revoked event")
	}
	if asString(inviteRevokedEvent["actor_agent_id"]) != adminPayload.Agent.AgentID {
		t.Fatalf("unexpected invite_revoked actor: %#v", inviteRevokedEvent)
	}

	selfRevokedEvent := findEvent(auth.AuthAuditEventPrincipalSelfRevoked, "", secondPayload.Agent.AgentID)
	if selfRevokedEvent == nil {
		t.Fatal("expected principal_self_revoked event")
	}
	if asString(selfRevokedEvent["actor_agent_id"]) != secondPayload.Agent.AgentID {
		t.Fatalf("unexpected principal_self_revoked actor: %#v", selfRevokedEvent)
	}
	if asString(mapValue(selfRevokedEvent["metadata"], "revocation_mode")) != "self" {
		t.Fatalf("unexpected principal_self_revoked metadata: %#v", selfRevokedEvent)
	}
	if allow, ok := mapValue(selfRevokedEvent["metadata"], "allow_human_lockout").(bool); !ok || allow {
		t.Fatalf("unexpected principal_self_revoked allow_human_lockout metadata: %#v", selfRevokedEvent)
	}
	if reason := asString(mapValue(selfRevokedEvent["metadata"], "human_lockout_reason")); reason != "" {
		t.Fatalf("unexpected principal_self_revoked human_lockout_reason metadata: %#v", selfRevokedEvent)
	}
}

func TestAdminPrincipalRevocationAndLastActiveSafeguards(t *testing.T) {
	t.Parallel()

	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	serverURL := env.server.URL

	adminResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/agents/register", map[string]any{
		"username":        "admin.revoke",
		"public_key":      mustGeneratePublicKey(t),
		"bootstrap_token": testBootstrapToken,
	}, "", http.StatusCreated)
	defer adminResp.Body.Close()

	var adminPayload struct {
		Agent struct {
			AgentID string `json:"agent_id"`
			ActorID string `json:"actor_id"`
		} `json:"agent"`
		Tokens struct {
			AccessToken string `json:"access_token"`
		} `json:"tokens"`
	}
	if err := json.NewDecoder(adminResp.Body).Decode(&adminPayload); err != nil {
		t.Fatalf("decode admin register response: %v", err)
	}

	memberInvite, memberToken := createInvite(t, serverURL, adminPayload.Tokens.AccessToken, map[string]any{
		"kind": "agent",
	})
	_ = memberInvite

	memberResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/agents/register", map[string]any{
		"username":     "member.revoke",
		"public_key":   mustGeneratePublicKey(t),
		"invite_token": memberToken,
	}, "", http.StatusCreated)
	defer memberResp.Body.Close()

	var memberPayload struct {
		Agent struct {
			AgentID string `json:"agent_id"`
			ActorID string `json:"actor_id"`
		} `json:"agent"`
	}
	if err := json.NewDecoder(memberResp.Body).Decode(&memberPayload); err != nil {
		t.Fatalf("decode member register response: %v", err)
	}

	unauthorizedResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/principals/"+memberPayload.Agent.AgentID+"/revoke", map[string]any{}, "", http.StatusUnauthorized)
	defer unauthorizedResp.Body.Close()
	assertErrorCode(t, unauthorizedResp, "auth_required")

	revokeResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/principals/"+memberPayload.Agent.AgentID+"/revoke", map[string]any{}, adminPayload.Tokens.AccessToken, http.StatusOK)
	defer revokeResp.Body.Close()
	var revokePayload map[string]any
	if err := json.NewDecoder(revokeResp.Body).Decode(&revokePayload); err != nil {
		t.Fatalf("decode admin revoke response: %v", err)
	}
	if !asBool(revokePayload["ok"]) {
		t.Fatalf("expected ok response, got %#v", revokePayload)
	}
	principalObj, _ := revokePayload["principal"].(map[string]any)
	if principalObj == nil || !asBool(principalObj["revoked"]) {
		t.Fatalf("expected revoked principal in response, got %#v", revokePayload)
	}
	if asString(principalObj["agent_id"]) != memberPayload.Agent.AgentID {
		t.Fatalf("unexpected revoked principal id: %#v", revokePayload)
	}
	revocationObj, _ := revokePayload["revocation"].(map[string]any)
	if revocationObj == nil || asString(revocationObj["mode"]) != "admin" {
		t.Fatalf("unexpected admin revocation metadata: %#v", revokePayload)
	}
	if asBool(revocationObj["already_revoked"]) {
		t.Fatalf("expected first admin revoke not to be idempotent: %#v", revokePayload)
	}
	if asBool(revocationObj["allow_human_lockout"]) {
		t.Fatalf("unexpected human lockout metadata on ordinary revoke: %#v", revokePayload)
	}

	secondRevokeResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/principals/"+memberPayload.Agent.AgentID+"/revoke", map[string]any{}, adminPayload.Tokens.AccessToken, http.StatusOK)
	defer secondRevokeResp.Body.Close()
	var secondRevokePayload map[string]any
	if err := json.NewDecoder(secondRevokeResp.Body).Decode(&secondRevokePayload); err != nil {
		t.Fatalf("decode second admin revoke response: %v", err)
	}
	secondRevocationObj, _ := secondRevokePayload["revocation"].(map[string]any)
	if secondRevocationObj == nil || !asBool(secondRevocationObj["already_revoked"]) {
		t.Fatalf("expected idempotent admin revoke response, got %#v", secondRevokePayload)
	}

	events, _, err := env.authStore.ListAuditEvents(context.Background(), auth.AuthAuditListFilter{})
	if err != nil {
		t.Fatalf("list auth audit events after admin revoke flow: %v", err)
	}

	adminRevokedCount := 0
	for _, event := range events {
		if event.EventType != auth.AuthAuditEventPrincipalRevoked {
			continue
		}
		if event.SubjectAgentID == nil {
			continue
		}
		switch *event.SubjectAgentID {
		case memberPayload.Agent.AgentID:
			adminRevokedCount++
			if event.ActorAgentID == nil || *event.ActorAgentID != adminPayload.Agent.AgentID {
				t.Fatalf("unexpected admin revoke actor: %#v", event)
			}
			if mode, _ := event.Metadata["revocation_mode"].(string); mode != "admin" {
				t.Fatalf("unexpected admin revoke metadata: %#v", event)
			}
		}
	}
	if adminRevokedCount != 1 {
		t.Fatalf("expected exactly one audit event for member admin revoke, got %d in %#v", adminRevokedCount, events)
	}
}

func TestHumanPrincipalRevocationSafeguards(t *testing.T) {
	t.Parallel()

	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	serverURL := env.server.URL
	ctx := context.Background()

	adminResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/agents/register", map[string]any{
		"username":        "lockout-admin",
		"public_key":      mustGeneratePublicKey(t),
		"bootstrap_token": testBootstrapToken,
	}, "", http.StatusCreated)
	defer adminResp.Body.Close()

	var adminPayload struct {
		Agent struct {
			AgentID string `json:"agent_id"`
			ActorID string `json:"actor_id"`
		} `json:"agent"`
		Tokens struct {
			AccessToken string `json:"access_token"`
		} `json:"tokens"`
	}
	if err := json.NewDecoder(adminResp.Body).Decode(&adminPayload); err != nil {
		t.Fatalf("decode admin register response: %v", err)
	}

	seededMachine := seedMachinePrincipalForLockoutTest(t, ctx, env.workspace.DB(), "agent-machine-lockout", "actor-machine-lockout", "machine.lockout", "machine-lockout-token")
	seededHumanOne := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "agent-human-lockout-1", "actor-human-lockout-1", "human.lockout.one", "human-lockout-one-token")

	blockedResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/principals/"+seededHumanOne.AgentID+"/revoke", map[string]any{}, adminPayload.Tokens.AccessToken, http.StatusConflict)
	defer blockedResp.Body.Close()
	assertErrorCode(t, blockedResp, "last_active_principal")

	machineResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/principals/"+seededMachine.AgentID+"/revoke", map[string]any{}, adminPayload.Tokens.AccessToken, http.StatusOK)
	defer machineResp.Body.Close()
	var machinePayload map[string]any
	if err := json.NewDecoder(machineResp.Body).Decode(&machinePayload); err != nil {
		t.Fatalf("decode machine revoke response: %v", err)
	}
	machineRevocation, _ := machinePayload["revocation"].(map[string]any)
	if machineRevocation == nil || asBool(machineRevocation["allow_human_lockout"]) {
		t.Fatalf("unexpected machine revoke payload: %#v", machinePayload)
	}

	seededHumanTwo := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "agent-human-lockout-2", "actor-human-lockout-2", "human.lockout.two", "human-lockout-two-token")

	allowedResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/principals/"+seededHumanOne.AgentID+"/revoke", map[string]any{}, adminPayload.Tokens.AccessToken, http.StatusOK)
	defer allowedResp.Body.Close()
	var allowedPayload map[string]any
	if err := json.NewDecoder(allowedResp.Body).Decode(&allowedPayload); err != nil {
		t.Fatalf("decode allowed revoke response: %v", err)
	}
	allowedRevocation, _ := allowedPayload["revocation"].(map[string]any)
	if allowedRevocation == nil || asBool(allowedRevocation["allow_human_lockout"]) {
		t.Fatalf("expected ordinary revoke response for first human, got %#v", allowedPayload)
	}

	lastHumanBlockedResp := postJSONExpectStatusWithAuth(t, serverURL+"/agents/me/revoke", map[string]any{}, seededHumanTwo.AccessToken, http.StatusConflict)
	defer lastHumanBlockedResp.Body.Close()
	assertErrorCode(t, lastHumanBlockedResp, "last_active_principal")

	breakGlassResp := postJSONExpectStatusWithAuth(t, serverURL+"/agents/me/revoke", map[string]any{
		"allow_human_lockout":  true,
		"human_lockout_reason": "restore workspace access",
	}, seededHumanTwo.AccessToken, http.StatusOK)
	defer breakGlassResp.Body.Close()
	var breakGlassPayload map[string]any
	if err := json.NewDecoder(breakGlassResp.Body).Decode(&breakGlassPayload); err != nil {
		t.Fatalf("decode break-glass self revoke response: %v", err)
	}
	breakGlassRevocation, _ := breakGlassPayload["revocation"].(map[string]any)
	if breakGlassRevocation == nil || !asBool(breakGlassRevocation["allow_human_lockout"]) {
		t.Fatalf("expected break-glass self revoke metadata, got %#v", breakGlassPayload)
	}

	events, _, err := env.authStore.ListAuditEvents(context.Background(), auth.AuthAuditListFilter{})
	if err != nil {
		t.Fatalf("list auth audit events after human revoke flow: %v", err)
	}

	humanLockoutEventSeen := false
	for _, event := range events {
		if event.EventType != auth.AuthAuditEventPrincipalHumanLockoutRevoked {
			continue
		}
		if event.SubjectAgentID == nil || *event.SubjectAgentID != seededHumanTwo.AgentID {
			continue
		}
		humanLockoutEventSeen = true
		if event.ActorAgentID == nil || *event.ActorAgentID != seededHumanTwo.AgentID {
			t.Fatalf("unexpected human lockout actor: %#v", event)
		}
		if mode, _ := event.Metadata["revocation_mode"].(string); mode != "self" {
			t.Fatalf("unexpected human lockout metadata: %#v", event)
		}
		if reason, _ := event.Metadata["human_lockout_reason"].(string); reason != "restore workspace access" {
			t.Fatalf("unexpected human lockout reason metadata: %#v", event)
		}
		if allow, _ := event.Metadata["allow_human_lockout"].(bool); !allow {
			t.Fatalf("expected allow_human_lockout metadata in %#v", event)
		}
	}
	if !humanLockoutEventSeen {
		t.Fatalf("expected principal_human_lockout_revoked audit event in %#v", events)
	}
}

func TestFirstPasskeyRegistrationWithBootstrapToken(t *testing.T) {
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

func TestWriteAuthToggleRejectsUnauthenticatedWritesWhenDisabled(t *testing.T) {
	t.Parallel()

	env := newAuthIntegrationEnv(t, authIntegrationOptions{
		bootstrapToken:             testBootstrapToken,
		allowUnauthenticatedWrites: false,
	})
	serverURL := env.server.URL

	if _, err := env.registry.Register(context.Background(), actors.Actor{
		ID:          "human-actor",
		DisplayName: "Human Actor",
		CreatedAt:   "2026-03-05T10:00:00Z",
	}); err != nil {
		t.Fatalf("seed human actor: %v", err)
	}

	listActorsResp := getJSONExpectStatusWithAuth(t, serverURL+"/actors", "", http.StatusUnauthorized)
	defer listActorsResp.Body.Close()
	assertErrorCode(t, listActorsResp, "auth_required")

	noAuthResp := postJSONExpectStatusWithAuth(t, serverURL+"/topics", map[string]any{
		"actor_id": "human-actor",
		"topic":    authTestMinimalTopic("Strict auth topic"),
	}, "", http.StatusUnauthorized)
	defer noAuthResp.Body.Close()
	assertErrorCode(t, noAuthResp, "auth_required")

	publicKey, _ := generateKeyPair(t)
	registerResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/agents/register", map[string]any{
		"username":        "strict.auth",
		"public_key":      publicKey,
		"bootstrap_token": testBootstrapToken,
	}, "", http.StatusCreated)
	defer registerResp.Body.Close()

	var registerPayload struct {
		Agent struct {
			ActorID string `json:"actor_id"`
		} `json:"agent"`
		Tokens struct {
			AccessToken string `json:"access_token"`
		} `json:"tokens"`
	}
	if err := json.NewDecoder(registerResp.Body).Decode(&registerPayload); err != nil {
		t.Fatalf("decode register response: %v", err)
	}

	authenticatedResp := postJSONExpectStatusWithAuth(t, serverURL+"/topics", map[string]any{
		"topic": authTestMinimalTopic("Authorized topic"),
	}, registerPayload.Tokens.AccessToken, http.StatusCreated)
	defer authenticatedResp.Body.Close()

	var authedTopicPayload struct {
		Topic map[string]any `json:"topic"`
	}
	if err := json.NewDecoder(authenticatedResp.Body).Decode(&authedTopicPayload); err != nil {
		t.Fatalf("decode topic response: %v", err)
	}
	if got := authTestThreadUpdatedBy(t, serverURL, registerPayload.Tokens.AccessToken, authedTopicPayload.Topic); got != registerPayload.Agent.ActorID {
		t.Fatalf("expected updated_by to match authenticated actor, got %q", got)
	}
}

func TestHostedModeProtectsWorkspaceReadsAndBlocksLegacyActorFlows(t *testing.T) {
	t.Parallel()

	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	serverURL := env.server.URL

	healthResp, err := http.Get(serverURL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	healthResp.Body.Close()
	if healthResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /health unexpected status: got %d", healthResp.StatusCode)
	}

	handshakeResp, err := http.Get(serverURL + "/meta/handshake")
	if err != nil {
		t.Fatalf("GET /meta/handshake: %v", err)
	}
	handshakeResp.Body.Close()
	if handshakeResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /meta/handshake unexpected status: got %d", handshakeResp.StatusCode)
	}

	tokenResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/token", map[string]any{
		"grant_type": "invalid",
	}, "", http.StatusBadRequest)
	defer tokenResp.Body.Close()
	assertErrorCode(t, tokenResp, "invalid_request")

	threadsResp := getJSONExpectStatusWithAuth(t, serverURL+"/threads", "", http.StatusUnauthorized)
	defer threadsResp.Body.Close()
	assertErrorCode(t, threadsResp, "auth_required")

	actorsResp := getJSONExpectStatusWithAuth(t, serverURL+"/actors", "", http.StatusUnauthorized)
	defer actorsResp.Body.Close()
	assertErrorCode(t, actorsResp, "auth_required")

	principalsResp := getJSONExpectStatusWithAuth(t, serverURL+"/auth/principals", "", http.StatusUnauthorized)
	defer principalsResp.Body.Close()
	assertErrorCode(t, principalsResp, "auth_required")

	auditResp := getJSONExpectStatusWithAuth(t, serverURL+"/auth/audit", "", http.StatusUnauthorized)
	defer auditResp.Body.Close()
	assertErrorCode(t, auditResp, "auth_required")

	opsHealthResp := getJSONExpectStatusWithAuth(t, serverURL+"/ops/health", "", http.StatusUnauthorized)
	defer opsHealthResp.Body.Close()
	assertErrorCode(t, opsHealthResp, "auth_required")

	usageResp := getJSONExpectStatusWithAuth(t, serverURL+"/ops/usage-summary", "", http.StatusUnauthorized)
	defer usageResp.Body.Close()
	assertErrorCode(t, usageResp, "auth_required")

	usageV1Resp := getJSONExpectStatusWithAuth(t, serverURL+"/v1/usage/summary", "", http.StatusUnauthorized)
	defer usageV1Resp.Body.Close()
	assertErrorCode(t, usageV1Resp, "auth_required")

	rebuildBlobUsageResp := postJSONExpectStatusWithAuth(t, serverURL+"/ops/blob-usage/rebuild", map[string]any{}, "", http.StatusUnauthorized)
	defer rebuildBlobUsageResp.Body.Close()
	assertErrorCode(t, rebuildBlobUsageResp, "auth_required")

	createActorResp := postJSONExpectStatusWithAuth(t, serverURL+"/actors", map[string]any{
		"actor": map[string]any{
			"id":           "legacy-actor",
			"display_name": "Legacy Actor",
			"created_at":   "2026-03-05T10:00:00Z",
		},
	}, "", http.StatusForbidden)
	defer createActorResp.Body.Close()
	assertErrorCode(t, createActorResp, "dev_actor_mode_required")

	publicKey, _ := generateKeyPair(t)
	registerResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/agents/register", map[string]any{
		"username":        "hosted.reader",
		"public_key":      publicKey,
		"bootstrap_token": testBootstrapToken,
	}, "", http.StatusCreated)
	defer registerResp.Body.Close()

	var registerPayload struct {
		Tokens struct {
			AccessToken string `json:"access_token"`
		} `json:"tokens"`
	}
	if err := json.NewDecoder(registerResp.Body).Decode(&registerPayload); err != nil {
		t.Fatalf("decode register response: %v", err)
	}

	authedThreadsResp := getJSONExpectStatusWithAuth(t, serverURL+"/threads", registerPayload.Tokens.AccessToken, http.StatusOK)
	authedThreadsResp.Body.Close()

	authedActorsResp := getJSONExpectStatusWithAuth(t, serverURL+"/actors", registerPayload.Tokens.AccessToken, http.StatusOK)
	authedActorsResp.Body.Close()

	authedOpsHealthResp := getJSONExpectStatusWithAuth(t, serverURL+"/ops/health", registerPayload.Tokens.AccessToken, http.StatusOK)
	authedOpsHealthResp.Body.Close()

	authedUsageResp := getJSONExpectStatusWithAuth(t, serverURL+"/ops/usage-summary", registerPayload.Tokens.AccessToken, http.StatusOK)
	var usagePayload struct {
		Summary map[string]any `json:"summary"`
	}
	if err := json.NewDecoder(authedUsageResp.Body).Decode(&usagePayload); err != nil {
		t.Fatalf("decode usage summary response: %v", err)
	}
	authedUsageResp.Body.Close()
	if usagePayload.Summary == nil {
		t.Fatal("expected usage summary payload")
	}

	authedUsageV1Resp := getJSONExpectStatusWithAuth(t, serverURL+"/v1/usage/summary", registerPayload.Tokens.AccessToken, http.StatusOK)
	var usageV1Payload struct {
		Summary map[string]any `json:"summary"`
	}
	if err := json.NewDecoder(authedUsageV1Resp.Body).Decode(&usageV1Payload); err != nil {
		t.Fatalf("decode v1 usage summary response: %v", err)
	}
	authedUsageV1Resp.Body.Close()
	if usageV1Payload.Summary == nil {
		t.Fatal("expected v1 usage summary payload")
	}
	summaryUsage, ok := usageV1Payload.Summary["usage"].(map[string]any)
	if !ok || summaryUsage == nil {
		t.Fatal("expected usage section in v1 usage summary payload")
	}
	for _, key := range []string{"artifact_count", "artifact_bytes", "document_count", "blob_count", "blob_bytes", "event_count", "agent_count"} {
		if _, exists := summaryUsage[key]; !exists {
			t.Fatalf("expected key %q in v1 usage summary payload", key)
		}
	}

	authedRebuildBlobUsageResp := postJSONExpectStatusWithAuth(t, serverURL+"/ops/blob-usage/rebuild", map[string]any{}, registerPayload.Tokens.AccessToken, http.StatusOK)
	var rebuildPayload struct {
		Rebuild map[string]any `json:"rebuild"`
	}
	if err := json.NewDecoder(authedRebuildBlobUsageResp.Body).Decode(&rebuildPayload); err != nil {
		t.Fatalf("decode blob usage rebuild response: %v", err)
	}
	authedRebuildBlobUsageResp.Body.Close()
	if rebuildPayload.Rebuild == nil {
		t.Fatal("expected blob usage rebuild payload")
	}
}

func TestExplicitDevModeKeepsLegacyActorFlowAndAnonymousWorkspaceAccess(t *testing.T) {
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

func TestConcurrentFreshAuthRegistrationsSucceed(t *testing.T) {
	t.Parallel()

	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	serverURL := env.server.URL

	initialPublicKey, _ := generateKeyPair(t)
	initialResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/agents/register", map[string]any{
		"username":        "bootstrap.concurrent",
		"public_key":      initialPublicKey,
		"bootstrap_token": testBootstrapToken,
	}, "", http.StatusCreated)
	defer initialResp.Body.Close()

	var initialPayload struct {
		Tokens struct {
			AccessToken string `json:"access_token"`
		} `json:"tokens"`
	}
	if err := json.NewDecoder(initialResp.Body).Decode(&initialPayload); err != nil {
		t.Fatalf("decode bootstrap register response: %v", err)
	}

	const concurrentRegistrations = 8
	type input struct {
		username    string
		publicKey   string
		inviteToken string
	}
	inputs := make([]input, 0, concurrentRegistrations)
	for i := 0; i < concurrentRegistrations; i++ {
		publicKey, _ := generateKeyPair(t)
		inputs = append(inputs, input{
			username:    fmt.Sprintf("user-%d", i+1),
			publicKey:   publicKey,
			inviteToken: createInviteToken(t, serverURL, initialPayload.Tokens.AccessToken, map[string]any{"kind": "agent"}),
		})
	}

	type result struct {
		index  int
		status int
		body   string
		err    error
	}
	results := make(chan result, concurrentRegistrations)
	client := &http.Client{Timeout: 10 * time.Second}
	var wg sync.WaitGroup
	for i, in := range inputs {
		wg.Add(1)
		go func(index int, in input) {
			defer wg.Done()
			payload, err := json.Marshal(map[string]any{
				"username":     in.username,
				"public_key":   in.publicKey,
				"invite_token": in.inviteToken,
			})
			if err != nil {
				results <- result{index: index, err: err}
				return
			}

			req, err := http.NewRequest(http.MethodPost, serverURL+"/auth/agents/register", bytes.NewReader(payload))
			if err != nil {
				results <- result{index: index, err: err}
				return
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := client.Do(req)
			if err != nil {
				results <- result{index: index, err: err}
				return
			}
			defer resp.Body.Close()

			rawBody, readErr := io.ReadAll(resp.Body)
			if readErr != nil {
				results <- result{index: index, err: readErr}
				return
			}
			results <- result{
				index:  index,
				status: resp.StatusCode,
				body:   string(rawBody),
			}
		}(i, in)
	}
	wg.Wait()
	close(results)

	failures := make([]string, 0)
	for result := range results {
		if result.err != nil {
			failures = append(failures, fmt.Sprintf("request %d failed: %v", result.index+1, result.err))
			continue
		}
		if result.status != http.StatusCreated {
			failures = append(
				failures,
				fmt.Sprintf("request %d: status=%d body=%s", result.index+1, result.status, result.body),
			)
		}
	}
	if len(failures) > 0 {
		t.Fatalf("expected all concurrent registrations to succeed:\n%s", strings.Join(failures, "\n"))
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
