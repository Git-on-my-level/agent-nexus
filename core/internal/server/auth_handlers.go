package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"organization-autorunner-core/internal/auth"
	"organization-autorunner-core/internal/controlplaneauth"
)

type principalContextKey struct{}

func cachedAuthenticatedPrincipal(r *http.Request) (*auth.Principal, bool) {
	if r == nil {
		return nil, false
	}
	principal, ok := r.Context().Value(principalContextKey{}).(*auth.Principal)
	if !ok || principal == nil {
		return nil, false
	}
	return principal, true
}

func cacheAuthenticatedPrincipal(r *http.Request, principal *auth.Principal) {
	if r == nil || principal == nil {
		return
	}
	ctx := context.WithValue(r.Context(), principalContextKey{}, principal)
	*r = *r.WithContext(ctx)
}

func handleRegisterAgent(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.authStore == nil {
		writeError(w, http.StatusServiceUnavailable, "auth_unavailable", "auth store is not configured")
		return
	}

	var req struct {
		Username        string `json:"username"`
		PublicKey       string `json:"public_key"`
		BootstrapToken  string `json:"bootstrap_token"`
		InviteToken     string `json:"invite_token"`
		ExistingActorID string `json:"existing_actor_id"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	claim, ok := resolveOnboardingClaim(w, r, opts, req.BootstrapToken, req.InviteToken, auth.PrincipalKindAgent)
	if !ok {
		return
	}

	agent, key, tokens, err := opts.authStore.RegisterAgent(r.Context(), auth.RegisterAgentInput{
		Username:        req.Username,
		PublicKey:       req.PublicKey,
		ExistingActorID: req.ExistingActorID,
	}, claim)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrUsernameTaken):
			writeError(w, http.StatusConflict, "username_taken", "username is already taken")
		case errors.Is(err, auth.ErrInvalidRequest):
			writeError(w, http.StatusBadRequest, "invalid_request", sanitizeAuthError(err))
		case isOnboardingTokenError(err):
			writeError(w, http.StatusUnauthorized, "invalid_token", "bootstrap or invite token is invalid, expired, revoked, or already consumed")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to register agent")
		}
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"agent":  agent,
		"key":    key,
		"tokens": tokens,
	})
}

func handleIssueAuthToken(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.authStore == nil {
		writeError(w, http.StatusServiceUnavailable, "auth_unavailable", "auth store is not configured")
		return
	}

	var req struct {
		GrantType    string `json:"grant_type"`
		RefreshToken string `json:"refresh_token"`
		AgentID      string `json:"agent_id"`
		KeyID        string `json:"key_id"`
		SignedAt     string `json:"signed_at"`
		Signature    string `json:"signature"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	var (
		tokens auth.TokenBundle
		err    error
	)
	switch strings.TrimSpace(req.GrantType) {
	case "refresh_token":
		tokens, err = opts.authStore.IssueTokenFromRefresh(r.Context(), req.RefreshToken)
	case "assertion":
		tokens, err = opts.authStore.IssueTokenFromAssertion(r.Context(), auth.AssertionInput{
			AgentID:   req.AgentID,
			KeyID:     req.KeyID,
			SignedAt:  req.SignedAt,
			Signature: req.Signature,
		})
	default:
		writeError(w, http.StatusBadRequest, "invalid_request", "grant_type must be refresh_token or assertion")
		return
	}

	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidToken):
			writeError(w, http.StatusUnauthorized, "invalid_token", "token is invalid, expired, or revoked")
		case errors.Is(err, auth.ErrAgentRevoked):
			writeError(w, http.StatusForbidden, "agent_revoked", "agent has been revoked")
		case errors.Is(err, auth.ErrKeyMismatch):
			writeError(w, http.StatusUnauthorized, "key_mismatch", "key assertion could not be validated")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to issue token")
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"tokens": tokens})
}

func handleGetCurrentAgent(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	principal, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}

	agent, err := opts.authStore.GetAgent(r.Context(), principal.AgentID)
	if err != nil {
		if errors.Is(err, auth.ErrAgentNotFound) {
			writeError(w, http.StatusUnauthorized, "invalid_token", "authenticated agent no longer exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load agent profile")
		return
	}

	keys, err := opts.authStore.ListKeys(r.Context(), principal.AgentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load agent keys")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"agent": agent, "keys": keys})
}

func handlePatchCurrentAgent(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	principal, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}
	if principal.AuthMethod == auth.AuthMethodControlPlane {
		writeError(w, http.StatusForbidden, "access_denied", "control-plane-backed principals cannot modify workspace-local auth state")
		return
	}

	var req struct {
		Username     string                  `json:"username"`
		Registration *auth.AgentRegistration `json:"registration"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Username) != "" && req.Registration != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "username and registration cannot be updated in the same request")
		return
	}
	if strings.TrimSpace(req.Username) == "" && req.Registration == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "username or registration is required")
		return
	}

	var (
		agent auth.Agent
		err   error
	)
	if req.Registration != nil {
		agent, err = opts.authStore.UpdateRegistration(r.Context(), principal.AgentID, *req.Registration)
	} else {
		agent, err = opts.authStore.UpdateUsername(r.Context(), principal.AgentID, req.Username)
	}
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidRequest):
			writeError(w, http.StatusBadRequest, "invalid_request", sanitizeAuthError(err))
		case errors.Is(err, auth.ErrUsernameTaken):
			writeError(w, http.StatusConflict, "username_taken", "username is already taken")
		case errors.Is(err, auth.ErrAgentNotFound):
			writeError(w, http.StatusUnauthorized, "invalid_token", "authenticated agent no longer exists")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to update agent profile")
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"agent": agent})
}

func handleRotateCurrentAgentKey(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	principal, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}
	if principal.AuthMethod == auth.AuthMethodControlPlane {
		writeError(w, http.StatusForbidden, "access_denied", "control-plane-backed principals cannot rotate workspace-local keys")
		return
	}

	var req struct {
		PublicKey string `json:"public_key"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	key, err := opts.authStore.RotateKey(r.Context(), principal.AgentID, req.PublicKey)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidRequest):
			writeError(w, http.StatusBadRequest, "invalid_request", sanitizeAuthError(err))
		case errors.Is(err, auth.ErrAgentRevoked):
			writeError(w, http.StatusForbidden, "agent_revoked", "agent has been revoked")
		case errors.Is(err, auth.ErrAgentNotFound):
			writeError(w, http.StatusUnauthorized, "invalid_token", "authenticated agent no longer exists")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to rotate agent key")
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"key": key})
}

func handleRevokeCurrentAgent(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	principal, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}
	if principal.AuthMethod == auth.AuthMethodControlPlane {
		writeError(w, http.StatusForbidden, "access_denied", "control-plane-backed principals cannot be revoked locally")
		return
	}

	req, ok := decodeRevokePrincipalRequest(w, r)
	if !ok {
		return
	}

	result, err := opts.authStore.RevokeAgent(r.Context(), principal.AgentID, auth.RevokeAgentInput{
		Actor:              *principal,
		Mode:               auth.RevocationModeSelf,
		AllowHumanLockout:  req.AllowHumanLockout,
		HumanLockoutReason: req.HumanLockoutReason,
	})
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrAgentNotFound):
			writeError(w, http.StatusUnauthorized, "invalid_token", "authenticated agent no longer exists")
		case errors.Is(err, auth.ErrLastActivePrincipal):
			writeError(w, http.StatusConflict, "last_active_principal", "refusing to revoke the last active human principal without allow_human_lockout=true and human_lockout_reason")
		case errors.Is(err, auth.ErrInvalidRequest):
			writeError(w, http.StatusBadRequest, "invalid_request", sanitizeAuthError(err))
		case errors.Is(err, auth.ErrAuthRequired):
			writeError(w, http.StatusUnauthorized, "auth_required", "authorization header is required")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to revoke agent")
		}
		return
	}

	writeRevokePrincipalResponse(w, result, opts.workspaceID)
}

func handleRevokePrincipal(w http.ResponseWriter, r *http.Request, opts handlerOptions, agentID string) {
	principal, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}

	req, ok := decodeRevokePrincipalRequest(w, r)
	if !ok {
		return
	}

	result, err := opts.authStore.RevokeAgent(r.Context(), agentID, auth.RevokeAgentInput{
		Actor:              *principal,
		Mode:               auth.RevocationModeAdmin,
		AllowHumanLockout:  req.AllowHumanLockout,
		HumanLockoutReason: req.HumanLockoutReason,
	})
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrAgentNotFound):
			writeError(w, http.StatusNotFound, "not_found", "principal not found")
		case errors.Is(err, auth.ErrLastActivePrincipal):
			writeError(w, http.StatusConflict, "last_active_principal", "refusing to revoke the last active human principal without allow_human_lockout=true and human_lockout_reason")
		case errors.Is(err, auth.ErrInvalidRequest):
			writeError(w, http.StatusBadRequest, "invalid_request", sanitizeAuthError(err))
		case errors.Is(err, auth.ErrAuthRequired):
			writeError(w, http.StatusUnauthorized, "auth_required", "authorization header is required")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to revoke principal")
		}
		return
	}

	writeRevokePrincipalResponse(w, result, opts.workspaceID)
}

func decodeRevokePrincipalRequest(w http.ResponseWriter, r *http.Request) (struct {
	AllowHumanLockout  bool   `json:"allow_human_lockout"`
	HumanLockoutReason string `json:"human_lockout_reason"`
}, bool) {
	var req struct {
		AllowHumanLockout  bool   `json:"allow_human_lockout"`
		HumanLockoutReason string `json:"human_lockout_reason"`
	}
	if r == nil || r.Body == nil {
		return req, true
	}
	if !decodeJSONBodyAllowEmpty(w, r, &req) {
		return req, false
	}
	return req, true
}

func writeRevokePrincipalResponse(w http.ResponseWriter, result auth.RevokeAgentResult, workspaceID string) {
	principal := enrichAuthPrincipalSummary(result.Principal, workspaceID, time.Now().UTC())
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"principal":  principal,
		"revocation": result.Revocation,
	})
}

func requireAuthenticatedPrincipal(w http.ResponseWriter, r *http.Request, opts handlerOptions) (*auth.Principal, bool) {
	principal, ok := authenticatePrincipalFromHeader(w, r, opts, true)
	if !ok {
		return nil, false
	}
	if principal == nil {
		writeError(w, http.StatusUnauthorized, "auth_required", "authorization header is required")
		return nil, false
	}
	return principal, true
}

func resolveOptionalPrincipal(w http.ResponseWriter, r *http.Request, opts handlerOptions) (*auth.Principal, bool) {
	return authenticatePrincipalFromHeader(w, r, opts, false)
}

func authenticatePrincipalFromHeader(w http.ResponseWriter, r *http.Request, opts handlerOptions, required bool) (*auth.Principal, bool) {
	if principal, ok := cachedAuthenticatedPrincipal(r); ok {
		return principal, true
	}

	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" {
		if required {
			writeError(w, http.StatusUnauthorized, "auth_required", "authorization header is required")
			return nil, false
		}
		return nil, true
	}

	if opts.authStore == nil {
		writeError(w, http.StatusServiceUnavailable, "auth_unavailable", "auth store is not configured")
		return nil, false
	}

	token, err := parseBearerToken(header)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_token", "authorization header must be Bearer <token>")
		return nil, false
	}

	principal, err := opts.authStore.AuthenticateAccessToken(r.Context(), token)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidToken) && opts.controlPlaneHumanVerifier != nil {
			identity, verifyErr := opts.controlPlaneHumanVerifier.Verify(token)
			if verifyErr == nil {
				hydratedPrincipal, hydrateErr := opts.authStore.EnsureControlPlanePrincipal(r.Context(), auth.EnsureControlPlanePrincipalInput{
					Issuer:         identity.Issuer,
					Subject:        identity.Subject,
					WorkspaceID:    identity.WorkspaceID,
					OrganizationID: identity.OrganizationID,
					Email:          identity.Email,
					DisplayName:    identity.DisplayName,
					LaunchID:       identity.LaunchID,
				})
				if hydrateErr != nil {
					writeError(w, http.StatusInternalServerError, "internal_error", "failed to hydrate control-plane principal")
					return nil, false
				}
				principalCopy := hydratedPrincipal
				cacheAuthenticatedPrincipal(r, &principalCopy)
				return &principalCopy, true
			}
		}
		switch {
		case errors.Is(err, auth.ErrInvalidToken):
			writeError(w, http.StatusUnauthorized, "invalid_token", "token is invalid, expired, or revoked")
		case errors.Is(err, auth.ErrAgentRevoked):
			writeError(w, http.StatusForbidden, "agent_revoked", "agent has been revoked")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to authenticate token")
		}
		return nil, false
	}

	principalCopy := principal
	cacheAuthenticatedPrincipal(r, &principalCopy)
	return &principalCopy, true
}

func resolveWriteActorID(w http.ResponseWriter, r *http.Request, opts handlerOptions, requestedActorID string) (string, bool) {
	principal, ok := resolveOptionalPrincipal(w, r, opts)
	if !ok {
		return "", false
	}

	requestedActorID = strings.TrimSpace(requestedActorID)
	if principal == nil {
		if !opts.allowUnauthenticatedWrites {
			writeError(w, http.StatusUnauthorized, "auth_required", "authorization header is required")
			return "", false
		}
		return requireRegisteredActorID(w, r, opts.actorRegistry, requestedActorID)
	}

	if requestedActorID == "" {
		return principal.ActorID, true
	}
	if requestedActorID != principal.ActorID {
		writeError(w, http.StatusForbidden, "key_mismatch", "actor_id does not match authenticated principal")
		return "", false
	}

	if opts.actorRegistry != nil {
		exists, err := opts.actorRegistry.Exists(r.Context(), requestedActorID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to validate actor_id")
			return "", false
		}
		if !exists {
			writeError(w, http.StatusBadRequest, "unknown_actor_id", "actor_id is not registered")
			return "", false
		}
	}

	return requestedActorID, true
}

func parseBearerToken(value string) (string, error) {
	parts := strings.Fields(value)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", fmt.Errorf("invalid authorization header")
	}
	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", fmt.Errorf("empty bearer token")
	}
	return token, nil
}

func sanitizeAuthError(err error) string {
	message := strings.TrimSpace(err.Error())
	message = strings.TrimPrefix(message, auth.ErrInvalidRequest.Error()+":")
	message = strings.TrimSpace(message)
	if message == "" {
		return "invalid request"
	}
	return message
}

func resolveOnboardingClaim(w http.ResponseWriter, r *http.Request, opts handlerOptions, bootstrapToken string, inviteToken string, principalKind auth.PrincipalKind) (auth.OnboardingClaim, bool) {
	if opts.authStore == nil {
		writeError(w, http.StatusServiceUnavailable, "auth_unavailable", "auth store is not configured")
		return auth.OnboardingClaim{}, false
	}

	claim, err := opts.authStore.ResolveOnboardingClaim(r.Context(), bootstrapToken, inviteToken, principalKind)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidRequest):
			writeError(w, http.StatusBadRequest, "invalid_request", sanitizeAuthError(err))
		case errors.Is(err, auth.ErrBootstrapRequired), errors.Is(err, auth.ErrInviteRequired), errors.Is(err, auth.ErrOnboardingRequired):
			writeError(w, http.StatusBadRequest, "invalid_request", onboardingRequiredMessage(err))
		case isOnboardingTokenError(err):
			writeError(w, http.StatusUnauthorized, "invalid_token", "bootstrap or invite token is invalid, expired, revoked, or already consumed")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to validate onboarding token")
		}
		return auth.OnboardingClaim{}, false
	}

	return claim, true
}

func onboardingRequiredMessage(err error) string {
	switch {
	case errors.Is(err, auth.ErrBootstrapRequired):
		return "bootstrap_token is required for first principal registration"
	case errors.Is(err, auth.ErrInviteRequired):
		return "invite_token is required for this registration"
	default:
		return "bootstrap_token or invite_token is required"
	}
}

func isOnboardingTokenError(err error) bool {
	return errors.Is(err, auth.ErrInvalidToken) || errors.Is(err, auth.ErrInviteKindMismatch)
}

func controlPlaneHumanAuthEnabled(opts handlerOptions) bool {
	return strings.TrimSpace(opts.humanAuthMode) == controlplaneauth.HumanAuthModeControlPlane
}
