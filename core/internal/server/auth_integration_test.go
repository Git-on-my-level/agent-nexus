package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"organization-autorunner-core/internal/actors"
	"organization-autorunner-core/internal/auth"
	"organization-autorunner-core/internal/primitives"
	"organization-autorunner-core/internal/schema"
	"organization-autorunner-core/internal/storage"
)

func TestAgentAuthLifecycleAndActorCompatibility(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	registry := actors.NewStore(workspace.DB())
	if _, err := registry.EnsureSystemActor(context.Background(), time.Now().UTC()); err != nil {
		t.Fatalf("ensure system actor: %v", err)
	}
	authStore := auth.NewStore(workspace.DB())
	contractPath := filepath.Join("..", "..", "..", "contracts", "oar-schema.yaml")
	contract, err := schema.Load(contractPath)
	if err != nil {
		t.Fatalf("load schema contract: %v", err)
	}
	primitiveStore := primitives.NewStore(workspace.DB(), workspace.Layout().ArtifactContentDir)
	handler := NewHandler(
		"0.2.2",
		WithActorRegistry(registry),
		WithAuthStore(authStore),
		WithHealthCheck(workspace.Ping),
		WithPrimitiveStore(primitiveStore),
		WithSchemaContract(contract),
	)
	server := httptest.NewServer(handler)
	defer server.Close()

	publicKey1, privateKey1 := generateKeyPair(t)
	registerResp := postJSONExpectStatusWithAuth(t, server.URL+"/auth/agents/register", map[string]any{
		"username":   "Agent.One",
		"public_key": publicKey1,
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

	meResp := getJSONExpectStatusWithAuth(t, server.URL+"/agents/me", registerPayload.Tokens.AccessToken, http.StatusOK)
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

	patchResp := patchJSONExpectStatusWithAuth(t, server.URL+"/agents/me", map[string]any{"username": "renamed_agent"}, registerPayload.Tokens.AccessToken, http.StatusOK)
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

	dupResp := postJSONExpectStatusWithAuth(t, server.URL+"/auth/agents/register", map[string]any{
		"username":   "Renamed_Agent",
		"public_key": publicKey1,
	}, "", http.StatusConflict)
	defer dupResp.Body.Close()
	assertErrorCode(t, dupResp, "username_taken")

	refreshResp := postJSONExpectStatusWithAuth(t, server.URL+"/auth/token", map[string]any{
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

	oldRefreshResp := postJSONExpectStatusWithAuth(t, server.URL+"/auth/token", map[string]any{
		"grant_type":    "refresh_token",
		"refresh_token": registerPayload.Tokens.RefreshToken,
	}, "", http.StatusUnauthorized)
	defer oldRefreshResp.Body.Close()
	assertErrorCode(t, oldRefreshResp, "invalid_token")

	postJSON(t, server.URL+"/actors", `{"actor":{"id":"human-actor","display_name":"Human Actor","created_at":"2026-03-05T10:00:00Z"}}`, http.StatusCreated).Body.Close()

	threadResp := postJSONExpectStatusWithAuth(t, server.URL+"/threads", map[string]any{
		"thread": map[string]any{
			"title":            "Auth-backed thread",
			"type":             "incident",
			"status":           "active",
			"priority":         "p1",
			"tags":             []string{"auth"},
			"cadence":          "daily",
			"next_check_in_at": "2030-01-01T00:00:00Z",
			"current_summary":  "summary",
			"next_actions":     []string{"action"},
			"key_artifacts":    []string{},
			"provenance":       map[string]any{"sources": []string{"inferred"}},
		},
	}, refreshPayload.Tokens.AccessToken, http.StatusCreated)
	defer threadResp.Body.Close()
	var threadPayload struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(threadResp.Body).Decode(&threadPayload); err != nil {
		t.Fatalf("decode thread response: %v", err)
	}
	if asString(threadPayload.Thread["updated_by"]) != registerPayload.Agent.ActorID {
		t.Fatalf("expected thread updated_by to use authenticated actor mapping, got %#v", threadPayload.Thread["updated_by"])
	}

	mismatchResp := postJSONExpectStatusWithAuth(t, server.URL+"/threads", map[string]any{
		"actor_id": "human-actor",
		"thread": map[string]any{
			"title":            "Mismatch thread",
			"type":             "incident",
			"status":           "active",
			"priority":         "p1",
			"tags":             []string{"auth"},
			"cadence":          "daily",
			"next_check_in_at": "2030-01-01T00:00:00Z",
			"current_summary":  "summary",
			"next_actions":     []string{"action"},
			"key_artifacts":    []string{},
			"provenance":       map[string]any{"sources": []string{"inferred"}},
		},
	}, refreshPayload.Tokens.AccessToken, http.StatusForbidden)
	defer mismatchResp.Body.Close()
	assertErrorCode(t, mismatchResp, "key_mismatch")

	assertionSignedAt := time.Now().UTC().Format(time.RFC3339)
	assertionSig := signAssertion(t, privateKey1, registerPayload.Agent.AgentID, registerPayload.Key.KeyID, assertionSignedAt)
	assertionResp := postJSONExpectStatusWithAuth(t, server.URL+"/auth/token", map[string]any{
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
	replayResp := postJSONExpectStatusWithAuth(t, server.URL+"/auth/token", map[string]any{
		"grant_type": "assertion",
		"agent_id":   registerPayload.Agent.AgentID,
		"key_id":     registerPayload.Key.KeyID,
		"signed_at":  assertionSignedAt,
		"signature":  assertionSig,
	}, "", http.StatusUnauthorized)
	defer replayResp.Body.Close()
	assertErrorCode(t, replayResp, "key_mismatch")

	publicKey2, privateKey2 := generateKeyPair(t)
	rotateResp := postJSONExpectStatusWithAuth(t, server.URL+"/agents/me/keys/rotate", map[string]any{
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
	oldAssertionResp := postJSONExpectStatusWithAuth(t, server.URL+"/auth/token", map[string]any{
		"grant_type": "assertion",
		"agent_id":   registerPayload.Agent.AgentID,
		"key_id":     registerPayload.Key.KeyID,
		"signed_at":  oldSignedAt,
		"signature":  signAssertion(t, privateKey1, registerPayload.Agent.AgentID, registerPayload.Key.KeyID, oldSignedAt),
	}, "", http.StatusUnauthorized)
	defer oldAssertionResp.Body.Close()
	assertErrorCode(t, oldAssertionResp, "key_mismatch")

	newSignedAt := time.Now().UTC().Format(time.RFC3339)
	newAssertionResp := postJSONExpectStatusWithAuth(t, server.URL+"/auth/token", map[string]any{
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

	revokeResp := postJSONExpectStatusWithAuth(t, server.URL+"/agents/me/revoke", map[string]any{}, newAssertionPayload.Tokens.AccessToken, http.StatusOK)
	defer revokeResp.Body.Close()

	revokedRefreshResp := postJSONExpectStatusWithAuth(t, server.URL+"/auth/token", map[string]any{
		"grant_type":    "refresh_token",
		"refresh_token": newAssertionPayload.Tokens.RefreshToken,
	}, "", http.StatusForbidden)
	defer revokedRefreshResp.Body.Close()
	assertErrorCode(t, revokedRefreshResp, "agent_revoked")

	revokedMeResp := getJSONExpectStatusWithAuth(t, server.URL+"/agents/me", newAssertionPayload.Tokens.AccessToken, http.StatusForbidden)
	defer revokedMeResp.Body.Close()
	assertErrorCode(t, revokedMeResp, "agent_revoked")
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
