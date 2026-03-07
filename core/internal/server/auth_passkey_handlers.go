package server

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"organization-autorunner-core/internal/auth"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

func handlePasskeyRegisterOptions(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if !requirePasskeyAuthDeps(w, opts) {
		return
	}

	var req struct {
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}

	displayName, err := auth.NormalizePasskeyDisplayName(req.DisplayName)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	userHandle, err := generatePasskeyUserHandle()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to generate passkey user handle")
		return
	}

	user := passkeyUser{
		userHandle:  userHandle,
		username:    displayName,
		displayName: displayName,
	}

	options, sessionData, err := opts.webAuthn.BeginRegistration(
		user,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to begin passkey registration")
		return
	}

	sessionID := opts.passkeySessionStore.Save(auth.PasskeySession{
		Kind:        auth.PasskeySessionKindRegistration,
		DisplayName: displayName,
		UserHandle:  userHandle,
		SessionData: *sessionData,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"session_id": sessionID,
		"options":    options,
	})
}

func handlePasskeyRegisterVerify(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if !requirePasskeyAuthDeps(w, opts) {
		return
	}

	var req struct {
		SessionID  string          `json:"session_id"`
		Credential json.RawMessage `json:"credential"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}
	req.SessionID = strings.TrimSpace(req.SessionID)
	if req.SessionID == "" || len(req.Credential) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "session_id and credential are required")
		return
	}

	session, ok := opts.passkeySessionStore.Consume(req.SessionID)
	if !ok || session.Kind != auth.PasskeySessionKindRegistration {
		writeError(w, http.StatusUnauthorized, "invalid_token", "passkey registration session is invalid or expired")
		return
	}

	displayName, err := auth.NormalizePasskeyDisplayName(session.DisplayName)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	user := passkeyUser{
		userHandle:  append([]byte(nil), session.UserHandle...),
		username:    displayName,
		displayName: displayName,
	}

	parsed, err := protocol.ParseCredentialCreationResponseBody(bytes.NewReader(req.Credential))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "passkey credential could not be parsed")
		return
	}

	credential, err := opts.webAuthn.CreateCredential(user, session.SessionData, parsed)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "passkey attestation could not be verified")
		return
	}

	agent, tokens, err := opts.authStore.RegisterPasskeyAgent(r.Context(), auth.RegisterPasskeyAgentInput{
		DisplayName: displayName,
		UserHandle:  session.UserHandle,
		Credential:  credential,
	})
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidRequest):
			writeError(w, http.StatusBadRequest, "invalid_request", sanitizeAuthError(err))
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

func handlePasskeyLoginOptions(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if !requirePasskeyAuthDeps(w, opts) {
		return
	}

	var req struct {
		Username string `json:"username"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}

	username := strings.TrimSpace(req.Username)
	var (
		options     *protocol.CredentialAssertion
		sessionData *webauthn.SessionData
		session     auth.PasskeySession
		err         error
	)

	if username == "" {
		options, sessionData, err = opts.webAuthn.BeginDiscoverableLogin()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to begin passkey login")
			return
		}
		session = auth.PasskeySession{
			Kind:        auth.PasskeySessionKindLoginDiscoverable,
			SessionData: *sessionData,
		}
	} else {
		identity, err := opts.authStore.GetPasskeyIdentityByUsername(r.Context(), username)
		if err != nil {
			if errors.Is(err, auth.ErrPasskeyNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "passkey user not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to load passkey identity")
			return
		}
		user := newPasskeyUser(identity)
		options, sessionData, err = opts.webAuthn.BeginLogin(user)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to begin passkey login")
			return
		}
		session = auth.PasskeySession{
			Kind:        auth.PasskeySessionKindLoginKnown,
			DisplayName: identity.DisplayName,
			UserHandle:  append([]byte(nil), identity.UserHandle...),
			SessionData: *sessionData,
		}
	}

	sessionID := opts.passkeySessionStore.Save(session)
	writeJSON(w, http.StatusOK, map[string]any{
		"session_id": sessionID,
		"options":    options,
	})
}

func handlePasskeyLoginVerify(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if !requirePasskeyAuthDeps(w, opts) {
		return
	}

	var req struct {
		SessionID  string          `json:"session_id"`
		Credential json.RawMessage `json:"credential"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}
	req.SessionID = strings.TrimSpace(req.SessionID)
	if req.SessionID == "" || len(req.Credential) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "session_id and credential are required")
		return
	}

	session, ok := opts.passkeySessionStore.Consume(req.SessionID)
	if !ok {
		writeError(w, http.StatusUnauthorized, "invalid_token", "passkey login session is invalid or expired")
		return
	}

	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(req.Credential))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "passkey credential could not be parsed")
		return
	}

	var (
		identity   auth.PasskeyIdentity
		credential *webauthn.Credential
	)
	switch session.Kind {
	case auth.PasskeySessionKindLoginKnown:
		identity, err = opts.authStore.GetPasskeyIdentityByUserHandle(r.Context(), session.UserHandle)
		if err != nil {
			handlePasskeyLookupError(w, err)
			return
		}
		credential, err = opts.webAuthn.ValidateLogin(newPasskeyUser(identity), session.SessionData, parsed)
	case auth.PasskeySessionKindLoginDiscoverable:
		credentialUserLookup := func(rawID []byte, userHandle []byte) (webauthn.User, error) {
			loadedIdentity, err := opts.authStore.GetPasskeyIdentityByUserHandle(r.Context(), userHandle)
			if err != nil {
				return nil, err
			}
			identity = loadedIdentity
			return newPasskeyUser(loadedIdentity), nil
		}
		_, credential, err = opts.webAuthn.ValidatePasskeyLogin(
			credentialUserLookup,
			session.SessionData,
			parsed,
		)
	default:
		writeError(w, http.StatusUnauthorized, "invalid_token", "passkey login session is invalid")
		return
	}
	if err != nil {
		if errors.Is(err, auth.ErrPasskeyNotFound) {
			writeError(w, http.StatusUnauthorized, "invalid_token", "passkey could not be verified")
			return
		}
		writeError(w, http.StatusUnauthorized, "invalid_token", "passkey could not be verified")
		return
	}

	tokens, err := opts.authStore.IssueTokenForPasskey(r.Context(), identity.Agent.AgentID, *credential)
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

func requirePasskeyAuthDeps(w http.ResponseWriter, opts handlerOptions) bool {
	if opts.authStore == nil || opts.passkeySessionStore == nil || opts.webAuthn == nil {
		writeError(w, http.StatusServiceUnavailable, "auth_unavailable", "passkey auth is not configured")
		return false
	}
	return true
}

func handlePasskeyLookupError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrPasskeyNotFound):
		writeError(w, http.StatusNotFound, "not_found", "passkey user not found")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load passkey identity")
	}
}

type passkeyUser struct {
	userHandle  []byte
	username    string
	displayName string
	credentials []webauthn.Credential
}

func newPasskeyUser(identity auth.PasskeyIdentity) passkeyUser {
	return passkeyUser{
		userHandle:  append([]byte(nil), identity.UserHandle...),
		username:    identity.Agent.Username,
		displayName: identity.DisplayName,
		credentials: append([]webauthn.Credential(nil), identity.Credentials...),
	}
}

func (u passkeyUser) WebAuthnID() []byte {
	return append([]byte(nil), u.userHandle...)
}

func (u passkeyUser) WebAuthnName() string {
	return u.username
}

func (u passkeyUser) WebAuthnDisplayName() string {
	return u.displayName
}

func (u passkeyUser) WebAuthnCredentials() []webauthn.Credential {
	return append([]webauthn.Credential(nil), u.credentials...)
}

func generatePasskeyUserHandle() ([]byte, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return nil, err
	}
	return value, nil
}
