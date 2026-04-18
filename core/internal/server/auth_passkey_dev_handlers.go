package server

import (
	"errors"
	"net/http"
	"strings"

	"agent-nexus-core/internal/auth"
)

func requireDevPasskeyBypass(w http.ResponseWriter, opts handlerOptions) bool {
	if !opts.allowPasskeyDevBypass {
		writeError(w, http.StatusForbidden, "dev_passkey_bypass_disabled", "passkey bypass is not enabled for this core instance")
		return false
	}
	return requirePasskeyAuthDeps(w, opts)
}

func handlePasskeyDevLookupError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrPasskeyNotFound):
		writeError(w, http.StatusNotFound, "not_found", "passkey principal not found")
	case errors.Is(err, auth.ErrAmbiguousPasskeyPrincipal):
		writeError(w, http.StatusBadRequest, "invalid_request", "multiple passkey principals match; specify username or display_name")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load passkey identity")
	}
}

// handlePasskeyDevRegister completes human onboarding with a stored synthetic passkey credential
// (no browser WebAuthn). Gated by the explicit passkey dev bypass capability.
func handlePasskeyDevRegister(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if !requireDevPasskeyBypass(w, opts) {
		return
	}

	var req struct {
		DisplayName     string `json:"display_name"`
		BootstrapToken  string `json:"bootstrap_token"`
		InviteToken     string `json:"invite_token"`
		ExistingActorID string `json:"existing_actor_id"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	displayName, err := auth.NormalizePasskeyDisplayName(req.DisplayName)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	claim, ok := resolveOnboardingClaim(w, r, opts, req.BootstrapToken, req.InviteToken, auth.PrincipalKindHuman)
	if !ok {
		return
	}

	userHandle, err := generatePasskeyUserHandle()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to generate passkey user handle")
		return
	}

	synthetic, err := auth.DevSyntheticPasskeyCredential()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to generate synthetic passkey credential")
		return
	}

	agent, tokens, err := opts.authStore.RegisterPasskeyAgent(r.Context(), auth.RegisterPasskeyAgentInput{
		DisplayName:     displayName,
		UserHandle:      userHandle,
		Credential:      &synthetic,
		ExistingActorID: strings.TrimSpace(req.ExistingActorID),
	}, claim)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidRequest):
			writeError(w, http.StatusBadRequest, "invalid_request", sanitizeAuthError(err))
		case isOnboardingTokenError(err):
			writeError(w, http.StatusUnauthorized, "invalid_token", "bootstrap or invite token is invalid, expired, revoked, or already consumed")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to register passkey agent")
		}
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"agent":  agent,
		"tokens": tokens,
	})
}

// handlePasskeyDevLogin issues tokens for an existing passkey principal without a WebAuthn assertion.
// When username and display_name are both empty, the workspace must have exactly one passkey principal.
func handlePasskeyDevLogin(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if !requireDevPasskeyBypass(w, opts) {
		return
	}

	var req struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	u := strings.TrimSpace(req.Username)
	d := strings.TrimSpace(req.DisplayName)
	if u != "" && d != "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "provide at most one of username or display_name")
		return
	}

	var (
		identity auth.PasskeyIdentity
		err      error
	)
	switch {
	case u != "":
		identity, err = opts.authStore.GetPasskeyIdentityByUsername(r.Context(), u)
	case d != "":
		identity, err = opts.authStore.GetPasskeyIdentityByDisplayName(r.Context(), d)
	default:
		identity, err = opts.authStore.GetSolePasskeyIdentity(r.Context())
	}
	if err != nil {
		handlePasskeyDevLookupError(w, err)
		return
	}
	if len(identity.Credentials) == 0 {
		writeError(w, http.StatusInternalServerError, "internal_error", "passkey principal has no credentials")
		return
	}

	tokens, err := opts.authStore.IssueTokenForPasskey(r.Context(), identity.Agent.AgentID, identity.Credentials[0])
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrAgentRevoked):
			writeError(w, http.StatusForbidden, "agent_revoked", "agent has been revoked")
		case errors.Is(err, auth.ErrPasskeyNotFound), errors.Is(err, auth.ErrAgentNotFound):
			writeError(w, http.StatusUnauthorized, "invalid_token", "passkey could not be verified")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to issue passkey token")
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"agent":  identity.Agent,
		"tokens": tokens,
	})
}
