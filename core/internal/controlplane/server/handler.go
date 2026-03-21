package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"organization-autorunner-core/internal/controlplane"
)

const (
	defaultListLimit = 50
	maxListLimit     = 200
	jsonBodyLimit    = 1 << 20
)

type HealthCheckFunc func(ctx context.Context) error

type Config struct {
	HealthCheck    HealthCheckFunc
	WebAuthnConfig WebAuthnConfig
}

func NewHandler(service *controlplane.Service, config Config) http.Handler {
	if service == nil {
		panic("controlplane server requires a non-nil service")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if config.HealthCheck != nil {
			if err := config.HealthCheck(r.Context()); err != nil {
				writeError(w, http.StatusServiceUnavailable, "service_unavailable", "control-plane storage is not ready")
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	mux.HandleFunc("POST /account/passkeys/registrations/start", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Email       string `json:"email"`
			DisplayName string `json:"display_name"`
		}
		if !decodeJSONBody(w, r, &body) {
			return
		}

		rpID, origin, err := config.WebAuthnConfig.resolveForRequest(r)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "invalid_request", err.Error())
			return
		}

		sessionID, options, account, err := service.StartPasskeyRegistration(r.Context(), body.Email, body.DisplayName, rpID, origin)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"registration_session_id": sessionID,
			"public_key_options":      options,
			"account":                 account,
		})
	})

	mux.HandleFunc("POST /account/passkeys/registrations/finish", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			RegistrationSessionID string         `json:"registration_session_id"`
			Credential            map[string]any `json:"credential"`
		}
		if !decodeJSONBody(w, r, &body) {
			return
		}

		rpID, origin, err := config.WebAuthnConfig.resolveForRequest(r)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "invalid_request", err.Error())
			return
		}

		account, session, err := service.FinishPasskeyRegistration(r.Context(), body.RegistrationSessionID, body.Credential, rpID, origin)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"account": account,
			"session": session,
		})
	})

	mux.HandleFunc("POST /account/sessions/start", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Email string `json:"email"`
		}
		if !decodeJSONBody(w, r, &body) {
			return
		}

		rpID, origin, err := config.WebAuthnConfig.resolveForRequest(r)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "invalid_request", err.Error())
			return
		}

		sessionID, options, hint, err := service.StartAccountSession(r.Context(), body.Email, rpID, origin)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"session_id":         sessionID,
			"public_key_options": options,
			"account_hint":       hint,
		})
	})

	mux.HandleFunc("POST /account/sessions/finish", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			SessionID  string         `json:"session_id"`
			Credential map[string]any `json:"credential"`
		}
		if !decodeJSONBody(w, r, &body) {
			return
		}

		rpID, origin, err := config.WebAuthnConfig.resolveForRequest(r)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "invalid_request", err.Error())
			return
		}

		account, session, err := service.FinishAccountSession(r.Context(), body.SessionID, body.Credential, rpID, origin)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"account": account,
			"session": session,
		})
	})

	mux.HandleFunc("DELETE /account/sessions/current", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := requireIdentity(w, r, service)
		if !ok {
			return
		}
		if err := service.RevokeCurrentSession(r.Context(), identity); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"revoked": true})
	})

	mux.HandleFunc("GET /accounts", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := requireIdentity(w, r, service)
		if !ok {
			return
		}
		pageReq, ok := parsePageRequest(w, r)
		if !ok {
			return
		}
		page, err := service.ListAccounts(r.Context(), identity, pageReq)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writePage(w, http.StatusOK, "accounts", page.Items, page.NextCursor)
	})

	mux.HandleFunc("GET /organizations", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := requireIdentity(w, r, service)
		if !ok {
			return
		}
		pageReq, ok := parsePageRequest(w, r)
		if !ok {
			return
		}
		page, err := service.ListOrganizations(r.Context(), identity, pageReq)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writePage(w, http.StatusOK, "organizations", page.Items, page.NextCursor)
	})

	mux.HandleFunc("POST /organizations", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := requireIdentity(w, r, service)
		if !ok {
			return
		}
		var body struct {
			Slug        string `json:"slug"`
			DisplayName string `json:"display_name"`
			PlanTier    string `json:"plan_tier"`
		}
		if !decodeJSONBody(w, r, &body) {
			return
		}
		organization, membership, err := service.CreateOrganization(r.Context(), identity, body.Slug, body.DisplayName, body.PlanTier)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"organization": organization,
			"membership":   membership,
		})
	})

	mux.HandleFunc("GET /organizations/{organization_id}", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := requireIdentity(w, r, service)
		if !ok {
			return
		}
		organization, err := service.GetOrganization(r.Context(), identity, r.PathValue("organization_id"))
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"organization": organization})
	})

	mux.HandleFunc("PATCH /organizations/{organization_id}", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := requireIdentity(w, r, service)
		if !ok {
			return
		}
		var body struct {
			DisplayName *string `json:"display_name"`
			PlanTier    *string `json:"plan_tier"`
			Status      *string `json:"status"`
		}
		if !decodeJSONBody(w, r, &body) {
			return
		}
		organization, err := service.UpdateOrganization(r.Context(), identity, r.PathValue("organization_id"), body.DisplayName, body.PlanTier, body.Status)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"organization": organization})
	})

	mux.HandleFunc("GET /organizations/{organization_id}/memberships", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := requireIdentity(w, r, service)
		if !ok {
			return
		}
		pageReq, ok := parsePageRequest(w, r)
		if !ok {
			return
		}
		page, err := service.ListOrganizationMemberships(r.Context(), identity, r.PathValue("organization_id"), pageReq)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writePage(w, http.StatusOK, "memberships", page.Items, page.NextCursor)
	})

	mux.HandleFunc("PATCH /organizations/{organization_id}/memberships/{membership_id}", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := requireIdentity(w, r, service)
		if !ok {
			return
		}
		var body struct {
			Role   *string `json:"role"`
			Status *string `json:"status"`
		}
		if !decodeJSONBody(w, r, &body) {
			return
		}
		membership, err := service.UpdateOrganizationMembership(r.Context(), identity, r.PathValue("organization_id"), r.PathValue("membership_id"), body.Role, body.Status)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"membership": membership})
	})

	mux.HandleFunc("GET /organizations/{organization_id}/invites", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := requireIdentity(w, r, service)
		if !ok {
			return
		}
		pageReq, ok := parsePageRequest(w, r)
		if !ok {
			return
		}
		page, err := service.ListOrganizationInvites(r.Context(), identity, r.PathValue("organization_id"), pageReq)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writePage(w, http.StatusOK, "invites", page.Items, page.NextCursor)
	})

	mux.HandleFunc("POST /organizations/{organization_id}/invites", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := requireIdentity(w, r, service)
		if !ok {
			return
		}
		var body struct {
			Email string `json:"email"`
			Role  string `json:"role"`
		}
		if !decodeJSONBody(w, r, &body) {
			return
		}
		invite, inviteURL, err := service.CreateOrganizationInvite(r.Context(), identity, r.PathValue("organization_id"), body.Email, body.Role)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"invite":     invite,
			"invite_url": inviteURL,
		})
	})

	mux.HandleFunc("POST /organizations/{organization_id}/invites/{invite_id}/revoke", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := requireIdentity(w, r, service)
		if !ok {
			return
		}
		invite, err := service.RevokeOrganizationInvite(r.Context(), identity, r.PathValue("organization_id"), r.PathValue("invite_id"))
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"invite": invite})
	})

	mux.HandleFunc("GET /organizations/{organization_id}/usage-summary", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := requireIdentity(w, r, service)
		if !ok {
			return
		}
		summary, err := service.GetUsageSummary(r.Context(), identity, r.PathValue("organization_id"))
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"summary": summary})
	})

	mux.HandleFunc("GET /organizations/{organization_id}/workspace-inventory", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := requireIdentity(w, r, service)
		if !ok {
			return
		}
		pageReq, ok := parsePageRequest(w, r)
		if !ok {
			return
		}
		summary, err := service.GetUsageSummary(r.Context(), identity, r.PathValue("organization_id"))
		if err != nil {
			writeServiceError(w, err)
			return
		}
		page, err := service.ListWorkspaceInventory(r.Context(), identity, r.PathValue("organization_id"), pageReq)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"organization_id": r.PathValue("organization_id"),
			"summary":         summary,
			"workspaces":      page.Items,
			"next_cursor":     page.NextCursor,
		})
	})

	mux.HandleFunc("GET /workspaces", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := requireIdentity(w, r, service)
		if !ok {
			return
		}
		pageReq, ok := parsePageRequest(w, r)
		if !ok {
			return
		}
		page, err := service.ListWorkspaces(r.Context(), identity, strings.TrimSpace(r.URL.Query().Get("organization_id")), pageReq)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writePage(w, http.StatusOK, "workspaces", page.Items, page.NextCursor)
	})

	mux.HandleFunc("POST /workspaces", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := requireIdentity(w, r, service)
		if !ok {
			return
		}
		var body struct {
			OrganizationID           string `json:"organization_id"`
			Slug                     string `json:"slug"`
			DisplayName              string `json:"display_name"`
			Region                   string `json:"region"`
			WorkspaceTier            string `json:"workspace_tier"`
			ServiceIdentityID        string `json:"service_identity_id"`
			ServiceIdentityPublicKey string `json:"service_identity_public_key"`
		}
		if !decodeJSONBody(w, r, &body) {
			return
		}
		workspace, job, err := service.CreateWorkspace(r.Context(), identity, body.OrganizationID, body.Slug, body.DisplayName, body.Region, body.WorkspaceTier, body.ServiceIdentityID, body.ServiceIdentityPublicKey)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"workspace":        workspace,
			"provisioning_job": job,
		})
	})

	mux.HandleFunc("GET /workspaces/{workspace_id}", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := requireIdentity(w, r, service)
		if !ok {
			return
		}
		workspace, err := service.GetWorkspace(r.Context(), identity, r.PathValue("workspace_id"))
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"workspace": workspace})
	})

	mux.HandleFunc("POST /workspaces/{workspace_id}/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		authorization := strings.TrimSpace(r.Header.Get("Authorization"))
		token := authorization
		if strings.HasPrefix(strings.ToLower(token), "bearer ") {
			token = strings.TrimSpace(token[7:])
		}
		if token == "" {
			writeError(w, http.StatusUnauthorized, "auth_required", "workspace service assertion is required")
			return
		}
		var body controlplane.WorkspaceHeartbeatRequest
		if !decodeJSONBody(w, r, &body) {
			return
		}
		workspace, err := service.RecordWorkspaceHeartbeat(r.Context(), token, r.PathValue("workspace_id"), body)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"workspace": workspace})
	})

	mux.HandleFunc("GET /workspaces/{workspace_id}/routing-manifest", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := requireIdentity(w, r, service)
		if !ok {
			return
		}
		manifest, err := service.GetWorkspaceRoutingManifest(r.Context(), identity, r.PathValue("workspace_id"))
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"routing_manifest": manifest})
	})

	mux.HandleFunc("GET /provisioning/jobs", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := requireIdentity(w, r, service)
		if !ok {
			return
		}
		pageReq, ok := parsePageRequest(w, r)
		if !ok {
			return
		}
		page, err := service.ListProvisioningJobs(r.Context(), identity, controlplane.JobsFilter{
			OrganizationID: strings.TrimSpace(r.URL.Query().Get("organization_id")),
			WorkspaceID:    strings.TrimSpace(r.URL.Query().Get("workspace_id")),
			Page:           pageReq,
		})
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writePage(w, http.StatusOK, "jobs", page.Items, page.NextCursor)
	})

	mux.HandleFunc("GET /provisioning/jobs/{job_id}", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := requireIdentity(w, r, service)
		if !ok {
			return
		}
		job, err := service.GetProvisioningJob(r.Context(), identity, r.PathValue("job_id"))
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": job})
	})

	mux.HandleFunc("POST /workspaces/{workspace_id}/suspend", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := requireIdentity(w, r, service)
		if !ok {
			return
		}
		workspace, job, err := service.SuspendWorkspace(r.Context(), identity, r.PathValue("workspace_id"))
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"workspace": workspace, "provisioning_job": job})
	})

	mux.HandleFunc("POST /workspaces/{workspace_id}/resume", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := requireIdentity(w, r, service)
		if !ok {
			return
		}
		workspace, job, err := service.ResumeWorkspace(r.Context(), identity, r.PathValue("workspace_id"))
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"workspace": workspace, "provisioning_job": job})
	})

	mux.HandleFunc("POST /workspaces/{workspace_id}/restore", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := requireIdentity(w, r, service)
		if !ok {
			return
		}
		var body struct {
			BackupDir string `json:"backup_dir"`
		}
		if !decodeJSONBody(w, r, &body) {
			return
		}
		workspace, job, err := service.RestoreWorkspace(r.Context(), identity, r.PathValue("workspace_id"), body.BackupDir)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"workspace": workspace, "provisioning_job": job})
	})

	mux.HandleFunc("POST /workspaces/{workspace_id}/backups", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := requireIdentity(w, r, service)
		if !ok {
			return
		}
		var body struct {
			ScheduleName  string `json:"schedule_name"`
			RetentionDays int    `json:"retention_days"`
		}
		if !decodeJSONBody(w, r, &body) {
			return
		}
		workspace, job, err := service.RunWorkspaceBackup(r.Context(), identity, r.PathValue("workspace_id"), body.ScheduleName, body.RetentionDays)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"workspace": workspace, "provisioning_job": job})
	})

	mux.HandleFunc("POST /workspaces/{workspace_id}/upgrade", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := requireIdentity(w, r, service)
		if !ok {
			return
		}
		var body struct {
			DesiredVersion string `json:"desired_version"`
		}
		if !decodeJSONBody(w, r, &body) {
			return
		}
		workspace, job, err := service.RunWorkspaceUpgrade(r.Context(), identity, r.PathValue("workspace_id"), body.DesiredVersion)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"workspace": workspace, "provisioning_job": job})
	})

	mux.HandleFunc("POST /workspaces/{workspace_id}/restore-drills", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := requireIdentity(w, r, service)
		if !ok {
			return
		}
		var body struct {
			BackupDir string `json:"backup_dir"`
		}
		if !decodeJSONBody(w, r, &body) {
			return
		}
		workspace, job, err := service.RunWorkspaceRestoreDrill(r.Context(), identity, r.PathValue("workspace_id"), body.BackupDir)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"workspace": workspace, "provisioning_job": job})
	})

	mux.HandleFunc("POST /workspaces/{workspace_id}/replace", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := requireIdentity(w, r, service)
		if !ok {
			return
		}
		var body struct {
			BackupDir string `json:"backup_dir"`
		}
		if !decodeJSONBody(w, r, &body) {
			return
		}
		workspace, job, err := service.ReplaceWorkspace(r.Context(), identity, r.PathValue("workspace_id"), body.BackupDir)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"workspace": workspace, "provisioning_job": job})
	})

	mux.HandleFunc("POST /workspaces/{workspace_id}/decommission", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := requireIdentity(w, r, service)
		if !ok {
			return
		}
		workspace, job, err := service.DecommissionWorkspace(r.Context(), identity, r.PathValue("workspace_id"))
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"workspace": workspace, "provisioning_job": job})
	})

	mux.HandleFunc("POST /workspaces/{workspace_id}/launch-sessions", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := requireIdentity(w, r, service)
		if !ok {
			return
		}
		var body struct {
			ReturnPath *string `json:"return_path"`
		}
		if !decodeJSONBody(w, r, &body) {
			return
		}
		launchSession, err := service.CreateWorkspaceLaunchSession(r.Context(), identity, r.PathValue("workspace_id"), body.ReturnPath)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"launch_session": launchSession})
	})

	mux.HandleFunc("POST /workspaces/{workspace_id}/session-exchange", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ExchangeToken string `json:"exchange_token"`
		}
		if !decodeJSONBody(w, r, &body) {
			return
		}
		workspace, grant, err := service.ExchangeWorkspaceSession(r.Context(), r.PathValue("workspace_id"), body.ExchangeToken)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"workspace": workspace,
			"grant":     grant,
		})
	})

	mux.HandleFunc("GET /audit-events", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := requireIdentity(w, r, service)
		if !ok {
			return
		}
		pageReq, ok := parsePageRequest(w, r)
		if !ok {
			return
		}
		page, err := service.ListAuditEvents(r.Context(), identity, controlplane.AuditFilter{
			OrganizationID: strings.TrimSpace(r.URL.Query().Get("organization_id")),
			WorkspaceID:    strings.TrimSpace(r.URL.Query().Get("workspace_id")),
			AccountID:      strings.TrimSpace(r.URL.Query().Get("account_id")),
			Page:           pageReq,
		})
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writePage(w, http.StatusOK, "events", page.Items, page.NextCursor)
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("X-XSS-Protection", "0")
		mux.ServeHTTP(w, r)
	})
}

func requireIdentity(w http.ResponseWriter, r *http.Request, service *controlplane.Service) (controlplane.RequestIdentity, bool) {
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	token := authorization
	if token == "" {
		writeError(w, http.StatusUnauthorized, "auth_required", "authorization token is required")
		return controlplane.RequestIdentity{}, false
	}
	if strings.HasPrefix(strings.ToLower(token), "bearer ") {
		token = strings.TrimSpace(token[7:])
	}
	identity, err := service.AuthenticateAccessToken(r.Context(), token)
	if err != nil {
		writeServiceError(w, err)
		return controlplane.RequestIdentity{}, false
	}
	return identity, true
}

func parsePageRequest(w http.ResponseWriter, r *http.Request) (controlplane.PageRequest, bool) {
	pageReq := controlplane.PageRequest{
		Cursor: strings.TrimSpace(r.URL.Query().Get("cursor")),
	}
	limitRaw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if limitRaw == "" {
		return pageReq, true
	}
	limit, err := strconv.Atoi(limitRaw)
	if err != nil || limit < 1 || limit > maxListLimit {
		writeError(w, http.StatusBadRequest, "invalid_request", "limit must be between 1 and 200")
		return controlplane.PageRequest{}, false
	}
	pageReq.Limit = limit
	return pageReq, true
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, target any) bool {
	if r.Body == nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is required")
		return false
	}
	defer r.Body.Close()

	decoder := json.NewDecoder(io.LimitReader(r.Body, jsonBodyLimit))
	if err := decoder.Decode(target); err != nil {
		if errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid_json", "request body is required")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return false
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must contain a single JSON value")
		return false
	}
	return true
}

func writePage[T any](w http.ResponseWriter, status int, key string, items []T, nextCursor string) {
	payload := map[string]any{key: items}
	if nextCursor != "" {
		payload["next_cursor"] = nextCursor
	}
	writeJSON(w, status, payload)
}

func writeServiceError(w http.ResponseWriter, err error) {
	var apiErr *controlplane.APIError
	if errors.As(err, &apiErr) {
		writeError(w, apiErr.Status, apiErr.Code, apiErr.Message)
		return
	}
	writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
}

func writeError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, payload map[string]any) {
	body, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, `{"error":{"code":"internal_error","message":"failed to encode response"}}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func shutdownServer(server *http.Server, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return server.Shutdown(ctx)
}
