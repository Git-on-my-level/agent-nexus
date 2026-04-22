package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
)

// errorOnExistsActorRegistry delegates all ActorRegistry methods except Exists,
// which always fails (simulates SQLite / store errors on actor lookup).
type errorOnExistsActorRegistry struct {
	ActorRegistry
}

func (e *errorOnExistsActorRegistry) Exists(ctx context.Context, actorID string) (bool, error) {
	_ = ctx
	_ = actorID
	return false, fmt.Errorf("injected actor Exists failure")
}

func TestActorRegistryExistsQueryErrorReturns500(t *testing.T) {
	t.Parallel()

	env := newAuthIntegrationEnv(t, authIntegrationOptions{
		bootstrapToken: testBootstrapToken,
		wrapActorRegistry: func(base *actors.Store) ActorRegistry {
			return &errorOnExistsActorRegistry{ActorRegistry: base}
		},
	})
	serverURL := env.server.URL

	publicKey, _ := generateKeyPair(t)
	registerResp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/agents/register", map[string]any{
		"username":        "exists.error.agent",
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
	actorID := registerPayload.Agent.ActorID
	token := registerPayload.Tokens.AccessToken
	if actorID == "" || token == "" {
		t.Fatalf("unexpected register payload: %#v", registerPayload)
	}

	boardResp := postJSONExpectStatusWithAuth(t, serverURL+"/boards", map[string]any{
		"actor_id": actorID,
		"board": map[string]any{
			"title":  "exists error probe",
			"labels": []any{"test"},
			"refs":   []any{},
		},
	}, token, http.StatusInternalServerError)
	defer boardResp.Body.Close()

	var errPayload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(boardResp.Body).Decode(&errPayload); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if errPayload.Error.Code != "internal_error" {
		t.Fatalf("expected internal_error, got %#v", errPayload)
	}
	if errPayload.Error.Message != "failed to validate actor_id" {
		t.Fatalf("expected generic validate message without ANX_DEBUG_ACTOR_EXISTS_ERRORS, got %q", errPayload.Error.Message)
	}
}

func TestWorkspaceHumanGrantActorRowAllowsExplicitActorIDOnWrites(t *testing.T) {
	t.Parallel()

	const workspaceID = "ws_grant_actor_row_board"
	fixture := newWorkspaceHumanGrantTestFixture(t, workspaceID, "anx-core")
	env := newAuthIntegrationEnv(t, authIntegrationOptions{
		workspaceID:                 workspaceID,
		workspaceHumanGrantVerifier: fixture.verifier,
	})
	serverURL := env.server.URL

	assertion := fixture.signGrantAssertion(t, workspaceHumanGrantTokenOptions{
		Subject:     "acct_board_row",
		JTI:         "jti-board-row-1",
		Email:       "board-row@example.com",
		DisplayName: "Board Row",
	})
	resp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/token", map[string]any{
		"grant_type": auth.TokenGrantTypeWorkspaceHuman,
		"assertion":  assertion,
	}, "", http.StatusOK)
	defer resp.Body.Close()

	var payload struct {
		Agent struct {
			ActorID string `json:"actor_id"`
		} `json:"agent"`
		Tokens auth.TokenBundle `json:"tokens"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode grant response: %v", err)
	}
	actorID := payload.Agent.ActorID
	token := payload.Tokens.AccessToken
	if actorID == "" || token == "" {
		t.Fatalf("unexpected token exchange payload: %#v", payload)
	}

	ctx := context.Background()
	exists, err := env.registry.Exists(ctx, actorID)
	if err != nil {
		t.Fatalf("Exists after grant exchange: %v", err)
	}
	if !exists {
		t.Fatalf("expected actors row for grant principal %q", actorID)
	}

	boardResp := postJSONExpectStatusWithAuth(t, serverURL+"/boards", map[string]any{
		"actor_id": actorID,
		"board": map[string]any{
			"title":  "grant actor row board",
			"labels": []any{"e2e"},
			"refs":   []any{},
		},
	}, token, http.StatusCreated)
	boardResp.Body.Close()
}
