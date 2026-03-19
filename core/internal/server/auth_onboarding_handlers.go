package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"organization-autorunner-core/internal/auth"
)

func handleBootstrapStatus(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.authStore == nil {
		writeError(w, http.StatusServiceUnavailable, "auth_unavailable", "auth store is not configured")
		return
	}

	available, err := opts.authStore.BootstrapRegistrationAvailable(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to determine bootstrap status")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"bootstrap_registration_available": available,
	})
}

func handleCreateInvite(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	principal, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}

	var req struct {
		Kind      string `json:"kind"`
		Note      string `json:"note"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}

	var expiresAt *time.Time
	if value := strings.TrimSpace(req.ExpiresAt); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "expires_at must be an RFC3339 datetime string")
			return
		}
		parsed = parsed.UTC()
		expiresAt = &parsed
	}

	invite, token, err := opts.authStore.CreateInvite(r.Context(), *principal, auth.CreateInviteInput{
		Kind:      req.Kind,
		Note:      req.Note,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidRequest):
			writeError(w, http.StatusBadRequest, "invalid_request", sanitizeAuthError(err))
		case errors.Is(err, auth.ErrAuthRequired):
			writeError(w, http.StatusUnauthorized, "auth_required", "authorization header is required")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to create invite")
		}
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"invite": invite,
		"token":  token,
	})
}

func handleListInvites(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if _, ok := requireAuthenticatedPrincipal(w, r, opts); !ok {
		return
	}

	invites, err := opts.authStore.ListInvites(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list invites")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"invites": invites})
}

func handleRevokeInvite(w http.ResponseWriter, r *http.Request, opts handlerOptions, inviteID string) {
	if _, ok := requireAuthenticatedPrincipal(w, r, opts); !ok {
		return
	}

	invite, err := opts.authStore.RevokeInvite(r.Context(), inviteID)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidRequest):
			writeError(w, http.StatusBadRequest, "invalid_request", sanitizeAuthError(err))
		case errors.Is(err, auth.ErrInviteNotFound):
			writeError(w, http.StatusNotFound, "not_found", "invite not found")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to revoke invite")
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"invite": invite})
}
