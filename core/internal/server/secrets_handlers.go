package server

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"organization-autorunner-core/internal/auth"
	"organization-autorunner-core/internal/secrets"
)

func handleListSecrets(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	_, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}
	if opts.secretsStore == nil {
		writeJSON(w, http.StatusOK, map[string]any{"secrets": []any{}})
		return
	}
	list, err := opts.secretsStore.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list secrets")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"secrets": list})
}

func handleCreateSecret(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	principal, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}
	if !isHumanPrincipal(principal) {
		writeError(w, http.StatusForbidden, "human_only", "only human principals may create secrets")
		return
	}
	if opts.secretsStore == nil || !opts.secretsStore.HasEncryptor() {
		writeError(w, http.StatusServiceUnavailable, "secrets_not_configured", "secrets encryption is not configured (set OAR_SECRETS_KEY)")
		return
	}

	var req struct {
		Name        string `json:"name"`
		Value       string `json:"value"`
		Description string `json:"description"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	if req.Value == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "value is required")
		return
	}

	meta, err := opts.secretsStore.Create(r.Context(), secrets.CreateSecretInput{
		Name:        req.Name,
		Value:       req.Value,
		Description: req.Description,
		ActorID:     principal.ActorID,
	})
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			writeError(w, http.StatusConflict, "resource_exists", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create secret")
		return
	}

	if opts.authStore != nil {
		recordSecretAuditEvent(r.Context(), opts.authStore, principal, auth.AuthAuditEventInput{
			EventType:    auth.AuthAuditEventSecretCreated,
			ActorAgentID: principal.AgentID,
			ActorActorID: principal.ActorID,
			Metadata:     map[string]any{"secret_name": meta.Name, "secret_id": meta.ID},
		})
	}

	writeJSON(w, http.StatusCreated, map[string]any{"secret": meta})
}

func handleUpdateSecret(w http.ResponseWriter, r *http.Request, opts handlerOptions, secretID string) {
	principal, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}
	if !isHumanPrincipal(principal) {
		writeError(w, http.StatusForbidden, "human_only", "only human principals may update secrets")
		return
	}
	if opts.secretsStore == nil || !opts.secretsStore.HasEncryptor() {
		writeError(w, http.StatusServiceUnavailable, "secrets_not_configured", "secrets encryption is not configured (set OAR_SECRETS_KEY)")
		return
	}

	var req struct {
		Value       string  `json:"value"`
		Description *string `json:"description"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if req.Value == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "value is required")
		return
	}

	meta, err := opts.secretsStore.Update(r.Context(), secretID, req.Value, req.Description, principal.ActorID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "not_found", "secret not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to update secret")
		return
	}

	if opts.authStore != nil {
		recordSecretAuditEvent(r.Context(), opts.authStore, principal, auth.AuthAuditEventInput{
			EventType:    auth.AuthAuditEventSecretUpdated,
			ActorAgentID: principal.AgentID,
			ActorActorID: principal.ActorID,
			Metadata:     map[string]any{"secret_name": meta.Name, "secret_id": meta.ID},
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"secret": meta})
}

func handleDeleteSecret(w http.ResponseWriter, r *http.Request, opts handlerOptions, secretID string) {
	principal, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}
	if !isHumanPrincipal(principal) {
		writeError(w, http.StatusForbidden, "human_only", "only human principals may delete secrets")
		return
	}
	if opts.secretsStore == nil {
		writeError(w, http.StatusNotFound, "not_found", "secret not found")
		return
	}

	meta, getErr := opts.secretsStore.GetByID(r.Context(), secretID)

	err := opts.secretsStore.Delete(r.Context(), secretID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "not_found", "secret not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to delete secret")
		return
	}

	if opts.authStore != nil && getErr == nil {
		recordSecretAuditEvent(r.Context(), opts.authStore, principal, auth.AuthAuditEventInput{
			EventType:    auth.AuthAuditEventSecretDeleted,
			ActorAgentID: principal.AgentID,
			ActorActorID: principal.ActorID,
			Metadata:     map[string]any{"secret_name": meta.Name, "secret_id": meta.ID},
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "secret_id": secretID})
}

func handleRevealSecret(w http.ResponseWriter, r *http.Request, opts handlerOptions, secretID string) {
	principal, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}
	if opts.secretsStore == nil || !opts.secretsStore.HasEncryptor() {
		writeError(w, http.StatusServiceUnavailable, "secrets_not_configured", "secrets encryption is not configured (set OAR_SECRETS_KEY)")
		return
	}

	name, value, err := opts.secretsStore.Reveal(r.Context(), secretID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "not_found", "secret not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to reveal secret")
		return
	}

	if opts.authStore != nil {
		recordSecretAuditEvent(r.Context(), opts.authStore, principal, auth.AuthAuditEventInput{
			EventType:    auth.AuthAuditEventSecretRevealed,
			ActorAgentID: principal.AgentID,
			ActorActorID: principal.ActorID,
			Metadata:     map[string]any{"secret_name": name, "secret_id": secretID},
		})
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "value": value})
}

func handleRevealSecretsBatch(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	principal, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}
	if opts.secretsStore == nil || !opts.secretsStore.HasEncryptor() {
		writeError(w, http.StatusServiceUnavailable, "secrets_not_configured", "secrets encryption is not configured (set OAR_SECRETS_KEY)")
		return
	}

	var req struct {
		Names []string `json:"names"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if len(req.Names) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "names array is required and must not be empty")
		return
	}

	pairs, err := opts.secretsStore.RevealBatchByNames(r.Context(), req.Names)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to reveal secrets")
		return
	}

	if opts.authStore != nil {
		for _, p := range pairs {
			recordSecretAuditEvent(r.Context(), opts.authStore, principal, auth.AuthAuditEventInput{
				EventType:    auth.AuthAuditEventSecretRevealed,
				ActorAgentID: principal.AgentID,
				ActorActorID: principal.ActorID,
				Metadata:     map[string]any{"secret_name": p.Name, "reveal_source": "batch"},
			})
		}
	}

	secretsResult := make([]map[string]string, len(pairs))
	for i, p := range pairs {
		secretsResult[i] = map[string]string{"name": p.Name, "value": p.Value}
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"secrets": secretsResult})
}

func recordSecretAuditEvent(ctx context.Context, authStore *auth.Store, principal *auth.Principal, input auth.AuthAuditEventInput) {
	input.OccurredAt = time.Now().UTC()
	if err := authStore.RecordSecretAuditEvent(ctx, input); err != nil {
		log.Printf("warning: failed to record secret audit event (%s): %v", input.EventType, err)
	}
}
