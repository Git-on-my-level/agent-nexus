package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/commandcenter"
)

type hostRosterView struct {
	auth.Host
	Agents []commandcenter.Summary `json:"agents"`
}

// HostEnrollmentVerificationURL accepts a public, workspace-scoped web UI URL.
// It is deployment configuration; core does not infer web routes from its API origin.
func HostEnrollmentVerificationURL(workspaceURL string) (string, error) {
	workspaceURL = strings.TrimSpace(workspaceURL)
	if workspaceURL == "" {
		return "", nil
	}
	parsed, err := url.Parse(workspaceURL)
	if err != nil || parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || strings.ContainsAny(workspaceURL, "?#") {
		return "", errors.New("ANX_PUBLIC_WEB_UI_WORKSPACE_URL must be an absolute HTTP(S) workspace URL without credentials, query, or fragment")
	}
	parts := strings.Split(strings.TrimRight(parsed.EscapedPath(), "/"), "/")
	if len(parts) != 5 || parts[0] != "" || parts[1] != "o" || parts[2] == "" || parts[3] != "w" || parts[4] == "" {
		return "", errors.New("ANX_PUBLIC_WEB_UI_WORKSPACE_URL must end in /o/<organization>/w/<workspace>")
	}
	return strings.TrimRight(parsed.String(), "/") + "/access/hosts/enroll", nil
}

func hostRoster(ctx context.Context, opts handlerOptions) (map[string]commandcenter.Summary, error) {
	if opts.runStore == nil {
		return nil, errors.New("agent roster is unavailable")
	}
	items, err := opts.runStore.Roster(ctx, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	byID := make(map[string]commandcenter.Summary, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	return byID, nil
}

func hostWithRoster(host auth.Host, byID map[string]commandcenter.Summary) hostRosterView {
	out := hostRosterView{Host: host, Agents: []commandcenter.Summary{}}
	for _, child := range host.Agents {
		if item, ok := byID[child.ID]; ok {
			out.Agents = append(out.Agents, item)
		}
	}
	return out
}

func hostRouteAccess(r *http.Request) routeAccessRequirement {
	path, method := r.URL.Path, r.Method
	public := routeAccessRequirement{bucket: routeAccessPublicAuthCeremony, mutation: routeMutationAuthCeremony, supported: true}
	authenticated := routeAccessRequirement{bucket: routeAccessAuthenticatedPrincipal, mutation: routeMutationNone, supported: true}
	write := routeAccessRequirement{bucket: routeAccessAuthenticatedPrincipal, mutation: routeMutationBusiness, supported: true}
	unsupported := routeAccessRequirement{}
	if path == "/auth/hosts/enrollments" && method == http.MethodPost {
		return public
	}
	if path == "/auth/hosts/enrollments/headless" && method == http.MethodPost {
		return public
	}
	if path == "/auth/hosts/enrollments/pending" && method == http.MethodGet {
		return authenticated
	}
	if path == "/auth/hosts/enrollment-tokens" {
		if method == http.MethodGet {
			return authenticated
		}
		if method == http.MethodPost {
			return write
		}
	}
	if strings.HasPrefix(path, "/auth/hosts/enrollment-tokens/") {
		parts := strings.Split(strings.TrimPrefix(path, "/auth/hosts/enrollment-tokens/"), "/")
		if len(parts) == 2 && parts[0] != "" && parts[1] == "revoke" && method == http.MethodPost {
			return write
		}
	}
	if strings.HasPrefix(path, "/auth/hosts/enrollments/") {
		parts := strings.Split(strings.TrimPrefix(path, "/auth/hosts/enrollments/"), "/")
		if parts[0] != "" && len(parts) == 1 && method == http.MethodGet {
			return public
		}
		if parts[0] != "" && len(parts) == 2 && method == http.MethodPost {
			switch parts[1] {
			case "complete":
				return public
			case "approve", "deny":
				return write
			}
		}
	}
	if path == "/hosts" && method == http.MethodGet {
		return authenticated
	}
	if strings.HasPrefix(path, "/hosts/") {
		parts := strings.Split(strings.TrimPrefix(path, "/hosts/"), "/")
		if parts[0] != "" && len(parts) == 1 {
			switch method {
			case http.MethodGet:
				return authenticated
			case http.MethodPatch:
				return public // signed host proof or human bearer is checked by the real handler
			case http.MethodDelete:
				return write
			}
		}
		if parts[0] != "" && len(parts) == 3 && parts[1] == "bridge" && parts[2] == "check-in" && method == http.MethodPost {
			return public // signed host proof is checked by the real handler
		}
	}
	return unsupported
}

func hostError(w http.ResponseWriter, err error) {
	status, code := http.StatusInternalServerError, "internal_error"
	switch {
	case errors.Is(err, auth.ErrAuthAdminRequired):
		status, code = http.StatusForbidden, "auth_admin_required"
	case errors.Is(err, auth.ErrHostSelfRevoke):
		status, code = http.StatusForbidden, "host_self_revoke"
	case errors.Is(err, auth.ErrInvalidRequest):
		status, code = http.StatusBadRequest, "invalid_request"
	case errors.Is(err, auth.ErrInvalidToken):
		status, code = http.StatusUnauthorized, "invalid_token"
	case errors.Is(err, auth.ErrKeyMismatch):
		status, code = http.StatusUnauthorized, "key_mismatch"
	case errors.Is(err, auth.ErrHostNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, auth.ErrHostRevoked):
		status, code = http.StatusForbidden, "host_revoked"
	case errors.Is(err, auth.ErrAgentExcluded):
		status, code = http.StatusForbidden, "agent_excluded"
	case errors.Is(err, auth.ErrAgentRevoked):
		status, code = http.StatusForbidden, "agent_revoked"
	case errors.Is(err, auth.ErrHostSlugTaken):
		status, code = http.StatusConflict, "host_slug_taken"
	case errors.Is(err, auth.ErrAgentHandleTaken):
		status, code = http.StatusConflict, "agent_handle_taken"
	case errors.Is(err, auth.ErrAdoptionConflict):
		status, code = http.StatusConflict, "adoption_conflict"
	case errors.Is(err, auth.ErrAdoptionProofInvalid):
		status, code = http.StatusUnauthorized, "adoption_proof_invalid"
	case errors.Is(err, auth.ErrEnrollmentPending):
		status, code = http.StatusConflict, "enrollment_pending"
	case errors.Is(err, auth.ErrEnrollmentDenied):
		status, code = http.StatusForbidden, "enrollment_denied"
	case errors.Is(err, auth.ErrEnrollmentExpired):
		status, code = http.StatusGone, "enrollment_expired"
	case errors.Is(err, auth.ErrEnrollmentConsumed):
		status, code = http.StatusConflict, "enrollment_consumed"
	case errors.Is(err, auth.ErrEnrollmentCapacity):
		status, code = http.StatusTooManyRequests, "enrollment_capacity"
	}
	writeError(w, status, code, code)
}
func hostAdmin(w http.ResponseWriter, r *http.Request, opts handlerOptions) (*auth.Principal, bool) {
	return requireAuthAdminPrincipal(w, r, opts)
}

// Host patch retains its pre-existing human-only bearer policy.
func humanHostAdmin(w http.ResponseWriter, r *http.Request, opts handlerOptions) (*auth.Principal, bool) {
	p, ok := requireAuthAdminPrincipal(w, r, opts)
	if !ok {
		return nil, false
	}
	if !isHumanPrincipal(p) {
		writeError(w, http.StatusForbidden, "forbidden", "human auth-admin required")
		return nil, false
	}
	return p, true
}
func hostRawBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	b, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil || len(b) > 1<<20 {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid body")
		return nil, false
	}
	r.Body = io.NopCloser(bytes.NewReader(b))
	return b, true
}
func hostProof(w http.ResponseWriter, r *http.Request, opts handlerOptions, id, kind string, body []byte) bool {
	if r.Header.Get("Authorization") != "" {
		writeError(w, http.StatusForbidden, "forbidden", "host key proof required")
		return false
	}
	key, signed, sig := r.Header.Get("X-ANX-Host-Key-Id"), r.Header.Get("X-ANX-Host-Signed-At"), r.Header.Get("X-ANX-Host-Signature")
	if key == "" || signed == "" || sig == "" {
		writeError(w, http.StatusUnauthorized, "auth_required", "host proof required")
		return false
	}
	if err := opts.authStore.VerifyHostProof(r.Context(), id, key, signed, sig, kind, body); err != nil {
		hostError(w, err)
		return false
	}
	// Host identity alone grants no private-board access. Wakeup ceremonies
	// resolve the delegated target actor separately before installing its scope.
	if strings.HasPrefix(r.URL.Path, "/hosts/") {
		attachResourceAccessScope(r, opts)
	}
	return true
}
func handleHostAuthRoutes(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.authStore == nil {
		writeError(w, 503, "auth_unavailable", "auth store unavailable")
		return
	}
	path := r.URL.Path
	if path == "/auth/hosts/enrollments" && r.Method == http.MethodPost {
		var in auth.HostEnrollmentInput
		if !decodeJSONBody(w, r, &in) {
			return
		}
		out, err := opts.authStore.StartHostEnrollment(r.Context(), in, requestClientHost(r))
		if err != nil {
			hostError(w, err)
			return
		}
		response := map[string]any{"enrollment_id": out.EnrollmentID, "user_code": out.UserCode, "poll_token": out.PollToken, "poll_interval_seconds": out.PollIntervalSeconds, "expires_at": out.ExpiresAt}
		if opts.hostEnrollmentVerificationURL != "" {
			response["verification_url"] = opts.hostEnrollmentVerificationURL
		}
		writeJSON(w, 201, response)
		return
	}
	if path == "/auth/hosts/enrollments/headless" && r.Method == http.MethodPost {
		var in auth.HostEnrollmentInput
		if !decodeJSONBody(w, r, &in) {
			return
		}
		h, err := opts.authStore.CompleteHeadlessHostEnrollment(r.Context(), in)
		if err != nil {
			hostError(w, err)
			return
		}
		opts.agentChanges.publish()
		writeJSON(w, 201, map[string]any{"host": h})
		return
	}
	if path == "/auth/hosts/enrollments/pending" && r.Method == http.MethodGet {
		if _, ok := hostAdmin(w, r, opts); !ok {
			return
		}
		out, err := opts.authStore.PendingHostEnrollments(r.Context())
		if err != nil {
			hostError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"enrollments": out})
		return
	}
	if path == "/auth/hosts/enrollment-tokens" {
		admin, ok := hostAdmin(w, r, opts)
		if !ok {
			return
		}
		switch r.Method {
		case http.MethodGet:
			out, err := opts.authStore.ListHostEnrollmentTokens(r.Context())
			if err != nil {
				hostError(w, err)
				return
			}
			writeJSON(w, 200, map[string]any{"enrollment_tokens": out})
			return
		case http.MethodPost:
			var in struct {
				Label            string  `json:"label"`
				ExpiresAt        *string `json:"expires_at"`
				ExpiresInSeconds *int64  `json:"expires_in_seconds"`
			}
			if !decodeJSONBody(w, r, &in) {
				return
			}
			var item auth.HostEnrollmentToken
			var secret string
			var err error
			if in.ExpiresInSeconds != nil {
				if in.ExpiresAt != nil || *in.ExpiresInSeconds < 600 || *in.ExpiresInSeconds > 86400 {
					hostError(w, auth.ErrInvalidRequest)
					return
				}
				item, secret, err = opts.authStore.CreateHostEnrollmentTokenWithTTL(r.Context(), in.Label, time.Duration(*in.ExpiresInSeconds)*time.Second, *admin)
			} else {
				var expiry time.Time
				if in.ExpiresAt == nil {
					hostError(w, auth.ErrInvalidRequest)
					return
				}
				expiry, err = time.Parse(time.RFC3339, *in.ExpiresAt)
				if err != nil {
					hostError(w, auth.ErrInvalidRequest)
					return
				}
				item, secret, err = opts.authStore.CreateHostEnrollmentToken(r.Context(), in.Label, expiry, *admin)
			}
			if err != nil {
				hostError(w, err)
				return
			}
			writeJSON(w, 201, map[string]any{"enrollment_token": item, "token": secret})
			return
		}
	}
	if strings.HasPrefix(path, "/auth/hosts/enrollment-tokens/") {
		parts := strings.Split(strings.TrimPrefix(path, "/auth/hosts/enrollment-tokens/"), "/")
		if len(parts) == 2 && parts[1] == "revoke" && r.Method == http.MethodPost {
			admin, ok := hostAdmin(w, r, opts)
			if !ok {
				return
			}
			out, err := opts.authStore.RevokeHostEnrollmentToken(r.Context(), parts[0], *admin)
			if err != nil {
				hostError(w, err)
				return
			}
			writeJSON(w, 200, map[string]any{"enrollment_token": out})
			return
		}
	}
	if strings.HasPrefix(path, "/auth/hosts/enrollments/") {
		parts := strings.Split(strings.TrimPrefix(path, "/auth/hosts/enrollments/"), "/")
		id := parts[0]
		if len(parts) == 1 && r.Method == http.MethodGet {
			e, err := opts.authStore.PollHostEnrollment(r.Context(), id, r.Header.Get("X-ANX-Enrollment-Token"))
			if err != nil {
				hostError(w, err)
				return
			}
			writeJSON(w, 200, map[string]any{"enrollment": e, "poll_interval_seconds": 3})
			return
		}
		if len(parts) == 2 && r.Method == http.MethodPost {
			switch parts[1] {
			case "approve", "deny":
				admin, ok := hostAdmin(w, r, opts)
				if !ok {
					return
				}
				e, err := opts.authStore.DecideHostEnrollment(r.Context(), id, parts[1] == "approve", *admin)
				if err != nil {
					hostError(w, err)
					return
				}
				writeJSON(w, 200, map[string]any{"enrollment": e, "poll_interval_seconds": 3})
				return
			case "complete":
				var in struct {
					PollToken string `json:"poll_token"`
					Signature string `json:"signature"`
				}
				if !decodeJSONBody(w, r, &in) {
					return
				}
				h, err := opts.authStore.CompleteHostEnrollment(r.Context(), id, in.PollToken, in.Signature)
				if err != nil {
					hostError(w, err)
					return
				}
				opts.agentChanges.publish()
				writeJSON(w, 201, map[string]any{"host": h})
				return
			}
		}
	}
	writeError(w, 404, "not_found", "endpoint not found")
}
func handleHostRoutes(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.authStore == nil {
		writeError(w, 503, "auth_unavailable", "auth store unavailable")
		return
	}
	if r.URL.Path == "/hosts" && r.Method == http.MethodGet {
		if _, ok := requireAuthenticatedPrincipal(w, r, opts); !ok {
			return
		}
		byID, err := hostRoster(r.Context(), opts)
		if err != nil {
			writeError(w, 503, "runs_unavailable", "agent roster is unavailable")
			return
		}
		items, err := opts.authStore.ListHosts(r.Context())
		if err != nil {
			hostError(w, err)
			return
		}
		views := make([]hostRosterView, 0, len(items))
		for _, host := range items {
			views = append(views, hostWithRoster(host, byID))
		}
		writeJSON(w, 200, map[string]any{"hosts": views})
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/hosts/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, 404, "not_found", "endpoint not found")
		return
	}
	id := parts[0]
	if len(parts) == 3 && parts[1] == "bridge" && parts[2] == "check-in" && r.Method == http.MethodPost {
		raw, ok := hostRawBody(w, r)
		if !ok {
			return
		}
		if !hostProof(w, r, opts, id, "bridge-check-in", raw) {
			return
		}
		var in struct {
			BridgeInstanceID string `json:"bridge_instance_id"`
			CheckedInAt      string `json:"checked_in_at"`
			ExpiresAt        string `json:"expires_at"`
		}
		if !decodeJSONBody(w, r, &in) {
			return
		}
		b, err := opts.authStore.CheckInHostBridge(r.Context(), id, in.BridgeInstanceID, in.CheckedInAt, in.ExpiresAt)
		if err != nil {
			hostError(w, err)
			return
		}
		opts.agentChanges.publish()
		writeJSON(w, 200, map[string]any{"bridge": b})
		return
	}
	if len(parts) != 1 {
		writeError(w, 404, "not_found", "endpoint not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, ok := requireAuthenticatedPrincipal(w, r, opts); !ok {
			return
		}
		byID, err := hostRoster(r.Context(), opts)
		if err != nil {
			writeError(w, 503, "runs_unavailable", "agent roster is unavailable")
			return
		}
		h, err := opts.authStore.GetHost(r.Context(), id)
		if err != nil {
			hostError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"host": hostWithRoster(h, byID)})
	case http.MethodPatch:
		raw, ok := hostRawBody(w, r)
		if !ok {
			return
		}
		var admin *auth.Principal
		if r.Header.Get("Authorization") != "" {
			admin, ok = humanHostAdmin(w, r, opts)
			if !ok {
				return
			}
		} else if !hostProof(w, r, opts, id, "patch", raw) {
			return
		}
		var in map[string]json.RawMessage
		if !decodeJSONBody(w, r, &in) {
			return
		}
		var display *string
		var excluded *[]string
		if v, ok := in["display_name"]; ok {
			var x string
			if json.Unmarshal(v, &x) != nil {
				hostError(w, auth.ErrInvalidRequest)
				return
			}
			display = &x
		}
		if v, ok := in["excluded_names"]; ok {
			var x []string
			if json.Unmarshal(v, &x) != nil {
				hostError(w, auth.ErrInvalidRequest)
				return
			}
			excluded = &x
		}
		if len(in) == 0 || len(in) > 2 {
			hostError(w, auth.ErrInvalidRequest)
			return
		}
		for k := range in {
			if k != "display_name" && k != "excluded_names" {
				hostError(w, auth.ErrInvalidRequest)
				return
			}
		}
		h, err := opts.authStore.PatchHost(r.Context(), id, display, excluded, admin)
		if err != nil {
			hostError(w, err)
			return
		}
		opts.agentChanges.publish()
		byID, err := hostRoster(r.Context(), opts)
		if err != nil {
			writeError(w, 503, "runs_unavailable", "agent roster is unavailable")
			return
		}
		writeJSON(w, 200, map[string]any{"host": hostWithRoster(h, byID)})
	case http.MethodDelete:
		admin, ok := hostAdmin(w, r, opts)
		if !ok {
			return
		}
		h, err := opts.authStore.RevokeHost(r.Context(), id, *admin)
		if err != nil {
			hostError(w, err)
			return
		}
		opts.agentChanges.publish()
		byID, err := hostRoster(r.Context(), opts)
		if err != nil {
			writeError(w, 503, "runs_unavailable", "agent roster is unavailable")
			return
		}
		writeJSON(w, 200, map[string]any{"host": hostWithRoster(h, byID)})
	default:
		writeError(w, 404, "not_found", "endpoint not found")
	}
}
