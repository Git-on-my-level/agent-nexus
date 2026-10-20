package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/pm"
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

func handleIssueAuthToken(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.authStore == nil {
		writeError(w, http.StatusServiceUnavailable, "auth_unavailable", "auth store is not configured")
		return
	}

	var req struct {
		GrantType       string `json:"grant_type"`
		RefreshToken    string `json:"refresh_token"`
		AgentID         string `json:"agent_id"`
		HostID          string `json:"host_id"`
		ExistingActorID string `json:"existing_actor_id"`
		AgentName       string `json:"agent_name"`
		KeyID           string `json:"key_id"`
		SignedAt        string `json:"signed_at"`
		Signature       string `json:"signature"`
		Assertion       string `json:"assertion"`
		// Nil keeps the default; only grant exchanges accept it.
		IssueRefreshToken *bool `json:"issue_refresh_token"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	switch grantType := strings.TrimSpace(req.GrantType); {
	case req.IssueRefreshToken == nil:
	case grantType == auth.TokenGrantTypeWorkspaceHuman, grantType == auth.TokenGrantTypeWorkspaceManagedAgent:
	default:
		writeError(w, http.StatusBadRequest, "invalid_request", "issue_refresh_token applies only to workspace_human_grant and workspace_managed_agent_grant")
		return
	}

	var (
		tokens auth.TokenBundle
		err    error
	)
	switch strings.TrimSpace(req.GrantType) {
	case "host_assertion":
		agent, issued, issueErr := opts.authStore.IssueHostAgentToken(r.Context(), req.HostID, req.KeyID, req.AgentName, req.SignedAt, req.Signature, req.ExistingActorID)
		if issueErr != nil {
			hostError(w, issueErr)
			return
		}
		opts.agentChanges.publish()
		writeJSON(w, http.StatusOK, map[string]any{"agent": agent, "tokens": issued})
		return
	case "refresh_token":
		tokens, err = opts.authStore.IssueTokenFromRefresh(r.Context(), req.RefreshToken)
	case "assertion":
		tokens, err = opts.authStore.IssueTokenFromAssertion(r.Context(), auth.AssertionInput{
			AgentID:   req.AgentID,
			KeyID:     req.KeyID,
			SignedAt:  req.SignedAt,
			Signature: req.Signature,
		})
	case auth.TokenGrantTypeWorkspaceHuman:
		if !allowHumanCredentialCeremony(w, r, opts) {
			return
		}
		if opts.workspaceHumanGrantVerifier == nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "grant_type workspace_human_grant is not enabled")
			return
		}
		var identity auth.WorkspaceHumanGrantIdentity
		exchangeWorkspaceGrant(w, r, opts, req.IssueRefreshToken, workspaceGrantExchange{
			grantType: auth.TokenGrantTypeWorkspaceHuman,
			label:     "workspace grant",
			verify: func(ctx context.Context) (string, string, error) {
				var err error
				identity, err = opts.workspaceHumanGrantVerifier.Verify(ctx, req.Assertion)
				return identity.Issuer, identity.Subject, err
			},
			exchange: func(ctx context.Context, options auth.GrantExchangeOptions) (auth.Agent, auth.TokenBundle, error) {
				return opts.authStore.IssueTokenFromWorkspaceHumanGrant(ctx, identity, options)
			},
		})
		return
	case auth.TokenGrantTypeWorkspaceManagedAgent:
		if opts.workspaceManagedGrantVerifier == nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "grant_type workspace_managed_agent_grant is not enabled")
			return
		}
		var identity auth.WorkspaceManagedAgentGrantIdentity
		exchangeWorkspaceGrant(w, r, opts, req.IssueRefreshToken, workspaceGrantExchange{
			grantType: auth.TokenGrantTypeWorkspaceManagedAgent,
			label:     "workspace managed-agent grant",
			verify: func(ctx context.Context) (string, string, error) {
				var err error
				identity, err = opts.workspaceManagedGrantVerifier.Verify(ctx, req.Assertion)
				// The managed principal is the slot, whatever subject the
				// issuer puts on the grant, so budget each slot separately.
				return identity.Issuer, strings.Join([]string{identity.Subject, identity.OrganizationID, identity.SlotID}, "\n"), err
			},
			exchange: func(ctx context.Context, options auth.GrantExchangeOptions) (auth.Agent, auth.TokenBundle, error) {
				return opts.authStore.IssueTokenFromWorkspaceManagedAgentGrant(ctx, identity, options)
			},
		})
		return
	default:
		writeError(w, http.StatusBadRequest, "invalid_request", "grant_type must be refresh_token, assertion, workspace_human_grant, or workspace_managed_agent_grant")
		return
	}

	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidToken):
			writeError(w, http.StatusUnauthorized, "invalid_token", "token is invalid, expired, or revoked")
		case errors.Is(err, auth.ErrAccountDisabled):
			writeError(w, http.StatusUnauthorized, "session_ended_by_account_status", "Your organization session has ended. Sign in again.")
		case errors.Is(err, auth.ErrAccountStatusUnreachable):
			writeError(w, http.StatusServiceUnavailable, "account_status_unreachable", "Account status could not be verified. Try again shortly.")
		case errors.Is(err, auth.ErrAgentRevoked):
			writeError(w, http.StatusForbidden, "agent_revoked", "agent has been revoked")
		case errors.Is(err, auth.ErrKeyMismatch):
			writeError(w, http.StatusUnauthorized, "key_mismatch", "key assertion could not be validated")
		default:
			writeInternalError(w, r, err, "failed to issue token")
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"tokens": tokens})
}

type workspaceGrantExchange struct {
	grantType string
	label     string
	// verify checks the grant signature and claims and returns its issuer
	// and the subject the per-subject budget is keyed by.
	verify func(context.Context) (issuer string, subject string, err error)
	// exchange records the verified grant and issues its tokens.
	exchange func(context.Context, auth.GrantExchangeOptions) (auth.Agent, auth.TokenBundle, error)
}

// exchangeWorkspaceGrant applies two rate limits around grant verification.
//
//  1. Before any signature work, a caller address that has spent its budget
//     on failed attempts is rejected. Only attempts that fail (unverifiable,
//     unavailable or replayed grants) are charged to the address, so one
//     server-side caller exchanging valid grants for many people is not
//     limited by its shared address.
//  2. After a grant verifies, the request is charged to a budget keyed by
//     issuer, subject and grant type (for a managed agent, also its
//     organization and slot), so each person or managed agent has its own
//     allowance however many callers share an address.
func exchangeWorkspaceGrant(w http.ResponseWriter, r *http.Request, opts handlerOptions, issueRefreshToken *bool, grant workspaceGrantExchange) {
	addressScope := "anonymous"
	if host := requestClientHost(r); host != "" {
		addressScope = "addr:" + host
	}
	addressLimiter := opts.workspaceHumanGrantRateLimiter
	if blocked, retryAfter := addressLimiter.blocked("auth", addressScope, time.Now().UTC()); blocked {
		writeRateLimitedError(w, grant.grantType, retryAfter)
		return
	}
	chargeAddress := func() { addressLimiter.charge("auth", addressScope, time.Now().UTC()) }

	issuer, subject, verifyErr := grant.verify(r.Context())
	if verifyErr != nil {
		chargeAddress()
		switch {
		case errors.Is(verifyErr, auth.ErrExternalGrantUnavailable):
			writeError(w, http.StatusServiceUnavailable, "auth_unavailable", grant.label+" verification is temporarily unavailable")
		default:
			writeError(w, http.StatusUnauthorized, "invalid_token", grant.label+" assertion could not be validated")
		}
		return
	}
	if allowed, retryAfter := opts.workspaceGrantSubjectRateLimiter.allow("auth", workspaceGrantSubjectScope(grant.grantType, issuer, subject), time.Now().UTC()); !allowed {
		writeRateLimitedError(w, grant.grantType, retryAfter)
		return
	}

	options := auth.GrantExchangeOptions{AccessOnly: issueRefreshToken != nil && !*issueRefreshToken}
	agent, issuedTokens, exchangeErr := grant.exchange(r.Context(), options)
	if exchangeErr != nil {
		switch {
		case errors.Is(exchangeErr, auth.ErrExternalGrantReplay):
			chargeAddress()
			writeError(w, http.StatusUnauthorized, "invalid_token", grant.label+" assertion has already been consumed")
		case errors.Is(exchangeErr, auth.ErrAgentRevoked):
			writeError(w, http.StatusForbidden, "agent_revoked", "agent has been revoked")
		case errors.Is(exchangeErr, auth.ErrInvalidRequest):
			writeError(w, http.StatusBadRequest, "invalid_request", sanitizeAuthError(exchangeErr))
		default:
			writeInternalError(w, r, exchangeErr, "failed to issue token")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"agent":  agent,
		"tokens": issuedTokens,
	})
}

// workspaceGrantSubjectScope hashes the verified identity so the limiter
// holds a fixed-size key per subject.
func workspaceGrantSubjectScope(grantType string, issuer string, subject string) string {
	sum := sha256.Sum256([]byte(grantType + "\n" + strings.TrimSpace(issuer) + "\n" + strings.TrimSpace(subject)))
	return "subject:" + hex.EncodeToString(sum[:])
}

func handleGetCurrentAgent(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	principal, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}
	agent, err := opts.authStore.GetSelfPrincipal(r.Context(), principal.AgentID)
	if err != nil {
		writeInternalError(w, r, err, "failed to resolve current principal")
		return
	}
	state := pm.Presence{State: "not_onboarded"}
	if opts.pmRuntime != nil && opts.pmRuntime.Service != nil {
		state, err = opts.pmRuntime.Service.Presence(r.Context(), pm.Principal{WorkspaceID: opts.pmRuntime.cfg.PM.WorkspaceID, ActorID: principal.ActorID, Human: principal.PrincipalKind == "human"})
		if err != nil {
			writeInternalError(w, r, err, "failed to read PM state")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"agent": agent, "pm": map[string]any{"state": state.State, "last_seen": state.LastSeen, "runner": state.Runner, "host": state.Host}})
}

func handleRevokePrincipal(w http.ResponseWriter, r *http.Request, opts handlerOptions, agentID string) {
	principal, ok := requireHumanPrincipal(w, r, opts)
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
		case errors.Is(err, auth.ErrHumanRequired):
			writeError(w, 403, "human_required", "an active human principal is required")
		case errors.Is(err, auth.ErrAgentNotFound):
			writeError(w, http.StatusNotFound, "not_found", "principal not found")
		case errors.Is(err, auth.ErrLastActivePrincipal):
			writeError(w, http.StatusConflict, "last_active_principal", "refusing to revoke the last active human principal without allow_human_lockout=true and human_lockout_reason")
		case errors.Is(err, auth.ErrInvalidRequest):
			writeError(w, http.StatusBadRequest, "invalid_request", sanitizeAuthError(err))
		case errors.Is(err, auth.ErrAuthRequired):
			writeError(w, http.StatusUnauthorized, "auth_required", "authorization header is required")
		default:
			writeInternalError(w, r, err, "failed to revoke principal")
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

func requireAuthAdminPrincipal(w http.ResponseWriter, r *http.Request, opts handlerOptions) (*auth.Principal, bool) {
	principal, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return nil, false
	}
	if !isAuthAdminPrincipal(principal) {
		writeError(w, http.StatusForbidden, "auth_admin_required", "auth administration requires a human or auth-admin principal")
		return nil, false
	}
	return principal, true
}

func requireHumanPrincipal(w http.ResponseWriter, r *http.Request, opts handlerOptions) (*auth.Principal, bool) {
	principal, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return nil, false
	}
	if !isHumanPrincipal(principal) {
		writeError(w, http.StatusForbidden, "human_required", "a human principal is required")
		return nil, false
	}
	return principal, true
}

func isAuthAdminPrincipal(principal *auth.Principal) bool {
	if principal == nil {
		return false
	}
	return principal.AuthAdmin || isHumanPrincipal(principal)
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
		switch {
		case errors.Is(err, auth.ErrInvalidToken):
			writeError(w, http.StatusUnauthorized, "invalid_token", "token is invalid, expired, or revoked")
		case errors.Is(err, auth.ErrAgentRevoked):
			writeError(w, http.StatusForbidden, "agent_revoked", "agent has been revoked")
		default:
			writeInternalError(w, r, err, "failed to authenticate token")
		}
		return nil, false
	}

	principalCopy := principal
	cacheAuthenticatedPrincipal(r, &principalCopy)
	attachResourceAccessScope(r, opts)
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
		if actors.IsReservedServiceActorID(principal.ActorID) {
			writeError(w, http.StatusBadRequest, "invalid_request", "actor_id is reserved for system use")
			return "", false
		}
		return principal.ActorID, true
	}
	if requestedActorID != principal.ActorID {
		writeError(w, http.StatusForbidden, "key_mismatch", "actor_id does not match authenticated principal")
		return "", false
	}

	if opts.actorRegistry != nil {
		exists, err := opts.actorRegistry.Exists(r.Context(), requestedActorID)
		if err != nil {
			message := "failed to validate actor_id"
			if actorExistsDebugErrors() {
				message = sanitizeFaultText(fmt.Sprintf("failed to validate actor_id: %v", err), 240)
			}
			writeInternalError(w, r, err, message)
			return "", false
		}
		if !exists {
			writeError(w, http.StatusBadRequest, "unknown_actor_id", "actor_id is not registered")
			return "", false
		}
	}

	if actors.IsReservedServiceActorID(requestedActorID) {
		writeError(w, http.StatusBadRequest, "invalid_request", "actor_id is reserved for system use")
		return "", false
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
	if principalKind == auth.PrincipalKindHuman && !allowHumanCredentialCeremony(w, r, opts) {
		return auth.OnboardingClaim{}, false
	}
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
			writeInternalError(w, r, err, "failed to validate onboarding token")
		}
		return auth.OnboardingClaim{}, false
	}

	return claim, true
}

// A public onboarding/login ceremony derives authority from its human-issued
// credential, never from an agent's administration bearer. Anonymous ceremonies
// still require bootstrap/invite, WebAuthn, or external issuer proof.
func allowHumanCredentialCeremony(w http.ResponseWriter, r *http.Request, opts handlerOptions) bool {
	principal, ok := resolveOptionalPrincipal(w, r, opts)
	if !ok {
		return false
	}
	if principal != nil && !isHumanPrincipal(principal) {
		writeError(w, http.StatusForbidden, "human_required", "agent credentials cannot mint human identities or credentials")
		return false
	}
	return true
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

func actorExistsDebugErrors() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ANX_DEBUG_ACTOR_EXISTS_ERRORS"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
