package authcli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/httpclient"
)

type Service struct{ cfg config.Resolved }

type Invite struct {
	ID                string `json:"id"`
	Kind              string `json:"kind"`
	CreatedAt         string `json:"created_at"`
	ConsumedAt        string `json:"consumed_at,omitempty"`
	ConsumedByAgentID string `json:"consumed_by_agent_id,omitempty"`
	ConsumedByActorID string `json:"consumed_by_actor_id,omitempty"`
	RevokedAt         string `json:"revoked_at,omitempty"`
	RevokedByAgentID  string `json:"revoked_by_agent_id,omitempty"`
	RevokedByActorID  string `json:"revoked_by_actor_id,omitempty"`
}

type ListInvitesResult struct {
	Invites []Invite `json:"invites"`
}

type CreateInviteResult struct {
	Invite Invite `json:"invite"`
	Token  string `json:"token"`
}

type RevokeInviteResult struct {
	Invite Invite `json:"invite"`
}

type BootstrapStatusResult struct {
	BootstrapRegistrationAvailable bool `json:"bootstrap_registration_available"`
}

type PrincipalWorkspaceBinding struct {
	WorkspaceID string `json:"workspace_id"`
	Enabled     bool   `json:"enabled"`
}

type PrincipalRegistration struct {
	Handle            string                      `json:"handle"`
	ActorID           string                      `json:"actor_id"`
	Status            string                      `json:"status"`
	WorkspaceBindings []PrincipalWorkspaceBinding `json:"workspace_bindings,omitempty"`
	BridgeInstanceID  string                      `json:"bridge_instance_id,omitempty"`
	BridgeCheckedInAt string                      `json:"bridge_checked_in_at,omitempty"`
	BridgeExpiresAt   string                      `json:"bridge_expires_at,omitempty"`
}

type PrincipalWakeRouting struct {
	Applicable bool   `json:"applicable"`
	Handle     string `json:"handle"`
	Taggable   bool   `json:"taggable"`
	Online     bool   `json:"online"`
	State      string `json:"state"`
	Summary    string `json:"summary"`
}

type Principal struct {
	AgentID       string                 `json:"agent_id"`
	ActorID       string                 `json:"actor_id"`
	Username      string                 `json:"username"`
	PrincipalKind string                 `json:"principal_kind"`
	AuthMethod    string                 `json:"auth_method"`
	CreatedAt     string                 `json:"created_at"`
	UpdatedAt     string                 `json:"updated_at"`
	Revoked       bool                   `json:"revoked"`
	RevokedAt     string                 `json:"revoked_at,omitempty"`
	Registration  *PrincipalRegistration `json:"registration,omitempty"`
	WakeRouting   *PrincipalWakeRouting  `json:"wake_routing,omitempty"`
}

type ListPrincipalsResult struct {
	Principals                []Principal `json:"principals"`
	ActiveHumanPrincipalCount int         `json:"active_human_principal_count"`
	NextCursor                string      `json:"next_cursor,omitempty"`
}

type Revocation struct {
	Mode              string `json:"mode"`
	AlreadyRevoked    bool   `json:"already_revoked"`
	AllowHumanLockout bool   `json:"allow_human_lockout"`
}

type RevokeOptions struct {
	AllowHumanLockout  bool   `json:"allow_human_lockout"`
	HumanLockoutReason string `json:"human_lockout_reason,omitempty"`
}

type RevokePrincipalResult struct {
	Principal  Principal  `json:"principal"`
	Revocation Revocation `json:"revocation"`
}

type AuditEvent struct {
	EventID        string         `json:"event_id"`
	EventType      string         `json:"event_type"`
	OccurredAt     string         `json:"occurred_at"`
	ActorAgentID   string         `json:"actor_agent_id,omitempty"`
	ActorActorID   string         `json:"actor_actor_id,omitempty"`
	SubjectAgentID string         `json:"subject_agent_id,omitempty"`
	SubjectActorID string         `json:"subject_actor_id,omitempty"`
	InviteID       string         `json:"invite_id,omitempty"`
	Metadata       map[string]any `json:"metadata"`
}

type ListAuditResult struct {
	Events     []AuditEvent `json:"events"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

func New(cfg config.Resolved) *Service { return &Service{cfg: cfg} }

func (s *Service) Config() config.Resolved { return s.cfg }

func (s *Service) RevokePrincipal(ctx context.Context, agentID string, opts RevokeOptions) (RevokePrincipalResult, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return RevokePrincipalResult{}, errnorm.Usage("invalid_request", "agent-id is required")
	}
	prof, err := s.ensureAccessToken(ctx)
	if err != nil {
		return RevokePrincipalResult{}, err
	}
	if strings.TrimSpace(prof.AgentID) != "" && agentID == strings.TrimSpace(prof.AgentID) {
		return RevokePrincipalResult{}, errnorm.Usage("invalid_request", "a principal cannot revoke itself through this administrative command")
	}
	client, err := s.newClient(prof.AccessToken)
	if err != nil {
		return RevokePrincipalResult{}, errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}
	body, _ := json.Marshal(map[string]any{
		"allow_human_lockout":  opts.AllowHumanLockout,
		"human_lockout_reason": strings.TrimSpace(opts.HumanLockoutReason),
	})
	path := "/auth/principals/" + agentID + "/revoke"
	resp, err := client.RawCall(ctx, httpclient.RawRequest{Method: http.MethodPost, Path: path, Body: body})
	if err != nil {
		return RevokePrincipalResult{}, errnorm.Wrap(errnorm.KindNetwork, "request_failed", "revoke principal request failed", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return RevokePrincipalResult{}, errnorm.FromHTTPFailure(resp.StatusCode, resp.Body)
	}
	var payload struct {
		Principal  Principal  `json:"principal"`
		Revocation Revocation `json:"revocation"`
	}
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		return RevokePrincipalResult{}, errnorm.Wrap(errnorm.KindRemote, "invalid_response", "revoke principal response is not valid JSON", err)
	}
	return RevokePrincipalResult{Principal: payload.Principal, Revocation: payload.Revocation}, nil
}

func (s *Service) ListInvites(ctx context.Context) (ListInvitesResult, error) {
	prof, err := s.ensureAccessToken(ctx)
	if err != nil {
		return ListInvitesResult{}, err
	}
	client, err := s.newClient(prof.AccessToken)
	if err != nil {
		return ListInvitesResult{}, errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}
	resp, err := client.RawCall(ctx, httpclient.RawRequest{Method: http.MethodGet, Path: "/auth/invites"})
	if err != nil {
		return ListInvitesResult{}, errnorm.Wrap(errnorm.KindNetwork, "request_failed", "list invites request failed", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return ListInvitesResult{}, errnorm.FromHTTPFailure(resp.StatusCode, resp.Body)
	}
	var payload struct {
		Invites []Invite `json:"invites"`
	}
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		return ListInvitesResult{}, errnorm.Wrap(errnorm.KindRemote, "invalid_response", "list invites response is not valid JSON", err)
	}
	return ListInvitesResult{Invites: payload.Invites}, nil
}

func (s *Service) CreateInvite(ctx context.Context, kind string) (CreateInviteResult, error) {
	prof, err := s.ensureAccessToken(ctx)
	if err != nil {
		return CreateInviteResult{}, err
	}
	client, err := s.newClient(prof.AccessToken)
	if err != nil {
		return CreateInviteResult{}, errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}
	body, _ := json.Marshal(map[string]any{
		"kind": kind,
	})
	resp, err := client.RawCall(ctx, httpclient.RawRequest{Method: http.MethodPost, Path: "/auth/invites", Body: body})
	if err != nil {
		return CreateInviteResult{}, errnorm.Wrap(errnorm.KindNetwork, "request_failed", "create invite request failed", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return CreateInviteResult{}, errnorm.FromHTTPFailure(resp.StatusCode, resp.Body)
	}
	var payload struct {
		Invite Invite `json:"invite"`
		Token  string `json:"token"`
	}
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		return CreateInviteResult{}, errnorm.Wrap(errnorm.KindRemote, "invalid_response", "create invite response is not valid JSON", err)
	}
	return CreateInviteResult{Invite: payload.Invite, Token: payload.Token}, nil
}

func (s *Service) RevokeInvite(ctx context.Context, inviteID string) (RevokeInviteResult, error) {
	inviteID = strings.TrimSpace(inviteID)
	if inviteID == "" {
		return RevokeInviteResult{}, errnorm.Usage("invalid_request", "invite-id is required")
	}
	prof, err := s.ensureAccessToken(ctx)
	if err != nil {
		return RevokeInviteResult{}, err
	}
	client, err := s.newClient(prof.AccessToken)
	if err != nil {
		return RevokeInviteResult{}, errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}
	path := "/auth/invites/" + inviteID + "/revoke"
	resp, err := client.RawCall(ctx, httpclient.RawRequest{Method: http.MethodPost, Path: path, Body: []byte("{}")})
	if err != nil {
		return RevokeInviteResult{}, errnorm.Wrap(errnorm.KindNetwork, "request_failed", "revoke invite request failed", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return RevokeInviteResult{}, errnorm.FromHTTPFailure(resp.StatusCode, resp.Body)
	}
	var payload struct {
		Invite Invite `json:"invite"`
	}
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		return RevokeInviteResult{}, errnorm.Wrap(errnorm.KindRemote, "invalid_response", "revoke invite response is not valid JSON", err)
	}
	return RevokeInviteResult{Invite: payload.Invite}, nil
}

func (s *Service) BootstrapStatus(ctx context.Context) (BootstrapStatusResult, error) {
	client, err := s.newClient("")
	if err != nil {
		return BootstrapStatusResult{}, errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}
	resp, err := client.RawCall(ctx, httpclient.RawRequest{Method: http.MethodGet, Path: "/auth/bootstrap/status"})
	if err != nil {
		return BootstrapStatusResult{}, errnorm.Wrap(errnorm.KindNetwork, "request_failed", "bootstrap status request failed", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return BootstrapStatusResult{}, errnorm.FromHTTPFailure(resp.StatusCode, resp.Body)
	}
	var payload struct {
		BootstrapRegistrationAvailable bool `json:"bootstrap_registration_available"`
	}
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		return BootstrapStatusResult{}, errnorm.Wrap(errnorm.KindRemote, "invalid_response", "bootstrap status response is not valid JSON", err)
	}
	return BootstrapStatusResult{BootstrapRegistrationAvailable: payload.BootstrapRegistrationAvailable}, nil
}

func (s *Service) ListPrincipals(ctx context.Context, limit int, cursor string) (ListPrincipalsResult, error) {
	prof, err := s.ensureAccessToken(ctx)
	if err != nil {
		return ListPrincipalsResult{}, err
	}
	client, err := s.newClient(prof.AccessToken)
	if err != nil {
		return ListPrincipalsResult{}, errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}

	path := "/auth/principals"
	query := url.Values{}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	if strings.TrimSpace(cursor) != "" {
		query.Set("cursor", strings.TrimSpace(cursor))
	}
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}

	resp, err := client.RawCall(ctx, httpclient.RawRequest{Method: http.MethodGet, Path: path})
	if err != nil {
		return ListPrincipalsResult{}, errnorm.Wrap(errnorm.KindNetwork, "request_failed", "list principals request failed", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return ListPrincipalsResult{}, errnorm.FromHTTPFailure(resp.StatusCode, resp.Body)
	}

	var payload struct {
		Principals                []Principal `json:"principals"`
		ActiveHumanPrincipalCount int         `json:"active_human_principal_count"`
		NextCursor                string      `json:"next_cursor"`
	}
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		return ListPrincipalsResult{}, errnorm.Wrap(errnorm.KindRemote, "invalid_response", "list principals response is not valid JSON", err)
	}
	return ListPrincipalsResult{
		Principals:                payload.Principals,
		ActiveHumanPrincipalCount: payload.ActiveHumanPrincipalCount,
		NextCursor:                payload.NextCursor,
	}, nil
}

func (s *Service) ListAudit(ctx context.Context, limit int, cursor string) (ListAuditResult, error) {
	prof, err := s.ensureAccessToken(ctx)
	if err != nil {
		return ListAuditResult{}, err
	}
	client, err := s.newClient(prof.AccessToken)
	if err != nil {
		return ListAuditResult{}, errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}

	path := "/auth/audit"
	query := url.Values{}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	if strings.TrimSpace(cursor) != "" {
		query.Set("cursor", strings.TrimSpace(cursor))
	}
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}

	resp, err := client.RawCall(ctx, httpclient.RawRequest{Method: http.MethodGet, Path: path})
	if err != nil {
		return ListAuditResult{}, errnorm.Wrap(errnorm.KindNetwork, "request_failed", "list auth audit request failed", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return ListAuditResult{}, errnorm.FromHTTPFailure(resp.StatusCode, resp.Body)
	}

	var payload struct {
		Events     []AuditEvent `json:"events"`
		NextCursor string       `json:"next_cursor"`
	}
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		return ListAuditResult{}, errnorm.Wrap(errnorm.KindRemote, "invalid_response", "list auth audit response is not valid JSON", err)
	}
	for i := range payload.Events {
		if payload.Events[i].Metadata == nil {
			payload.Events[i].Metadata = map[string]any{}
		}
	}
	return ListAuditResult{Events: payload.Events, NextCursor: payload.NextCursor}, nil
}

type accessContext struct{ AccessToken, AgentID string }

func (s *Service) ensureAccessToken(context.Context) (accessContext, error) {
	token := strings.TrimSpace(s.cfg.AccessToken)
	if token == "" {
		return accessContext{}, errnorm.Local("auth_required", "resolve an enrolled host agent or supply a human bearer")
	}
	return accessContext{AccessToken: token, AgentID: s.cfg.AgentID}, nil
}

func (s *Service) newClient(accessToken string) (*httpclient.Client, error) {
	cfg := s.cfg
	cfg.AccessToken = strings.TrimSpace(accessToken)
	return httpclient.New(cfg)
}
