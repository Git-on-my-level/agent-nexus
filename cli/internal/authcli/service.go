package authcli

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"organization-autorunner-cli/internal/config"
	"organization-autorunner-cli/internal/errnorm"
	"organization-autorunner-cli/internal/httpclient"
	"organization-autorunner-cli/internal/profile"
)

const minAccessTokenTTL = time.Minute

type Service struct {
	cfg config.Resolved
	now func() time.Time
}

type RegisterResult struct {
	Profile profile.Profile `json:"profile"`
	Agent   map[string]any  `json:"agent"`
	Key     map[string]any  `json:"key"`
}

type WhoAmIResult struct {
	Profile profile.Profile `json:"profile"`
	Server  map[string]any  `json:"server"`
}

type TokenStatusResult struct {
	ProfilePath        string `json:"profile_path"`
	Agent              string `json:"agent"`
	AgentID            string `json:"agent_id,omitempty"`
	Username           string `json:"username,omitempty"`
	HasAccessToken     bool   `json:"has_access_token"`
	HasRefreshToken    bool   `json:"has_refresh_token"`
	AccessExpiresAt    string `json:"access_expires_at,omitempty"`
	SecondsUntilExpiry int64  `json:"seconds_until_expiry,omitempty"`
	NeedsRefresh       bool   `json:"needs_refresh"`
	Revoked            bool   `json:"revoked"`
	CoreInstanceID     string `json:"core_instance_id,omitempty"`
	PrivateKeyPath     string `json:"private_key_path,omitempty"`
	Source             string `json:"source"`
}

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

func New(cfg config.Resolved) *Service {
	return &Service{cfg: cfg, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) Config() config.Resolved {
	return s.cfg
}

func (s *Service) Register(ctx context.Context, username string) (RegisterResult, error) {
	return s.RegisterWithToken(ctx, username, "", "", "")
}

func (s *Service) RegisterWithToken(ctx context.Context, username, bootstrapToken, inviteToken, existingActorID string) (RegisterResult, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return RegisterResult{}, errnorm.Usage("invalid_request", "username is required")
	}

	publicKey, privateKey, err := profile.GenerateEd25519KeyPair()
	if err != nil {
		return RegisterResult{}, errnorm.Wrap(errnorm.KindLocal, "key_generation_failed", "failed to generate key pair", err)
	}

	client, err := s.newClient("")
	if err != nil {
		return RegisterResult{}, errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}

	reqBody := map[string]any{
		"username":   username,
		"public_key": publicKey,
	}
	if bootstrapToken != "" {
		reqBody["bootstrap_token"] = bootstrapToken
	}
	if inviteToken != "" {
		reqBody["invite_token"] = inviteToken
	}
	if strings.TrimSpace(existingActorID) != "" {
		reqBody["existing_actor_id"] = strings.TrimSpace(existingActorID)
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return RegisterResult{}, errnorm.Wrap(errnorm.KindInternal, "json_encode_failed", "failed to encode register request", err)
	}
	resp, err := client.RawCall(ctx, httpclient.RawRequest{Method: http.MethodPost, Path: "/auth/agents/register", Body: body})
	if err != nil {
		return RegisterResult{}, classifyRegisterTransportError(err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return RegisterResult{}, classifyRegisterHTTPFailure(resp.StatusCode, resp.Body)
	}

	var payload struct {
		Agent  map[string]any `json:"agent"`
		Key    map[string]any `json:"key"`
		Tokens struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			TokenType    string `json:"token_type"`
			ExpiresIn    int64  `json:"expires_in"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		return RegisterResult{}, errnorm.Wrap(errnorm.KindRemote, "invalid_response", "register response is not valid JSON", err)
	}

	agentID := anyString(payload.Agent["agent_id"])
	actorID := anyString(payload.Agent["actor_id"])
	serverUsername := anyString(payload.Agent["username"])
	keyID := anyString(payload.Key["key_id"])
	if agentID == "" || keyID == "" || payload.Tokens.AccessToken == "" || payload.Tokens.RefreshToken == "" {
		return RegisterResult{}, errnorm.Local("invalid_response", "register response missing required fields")
	}

	keyPath := defaultKeyPathFromProfilePath(s.cfg.ProfilePath, s.cfg.Agent)
	if err := profile.SavePrivateKey(keyPath, privateKey); err != nil {
		return RegisterResult{}, errnorm.Wrap(errnorm.KindLocal, "key_persist_failed", "failed to persist private key", err)
	}

	handshake, _ := s.fetchHandshake(ctx)
	accessExpiry := s.now().Add(time.Duration(payload.Tokens.ExpiresIn) * time.Second).Format(time.RFC3339Nano)
	prof := profile.Profile{
		Version:              profile.ProfileVersion,
		Agent:                s.cfg.Agent,
		BaseURL:              s.cfg.BaseURL,
		JSON:                 boolPtr(s.cfg.JSON),
		Username:             firstNonEmpty(serverUsername, username),
		AgentID:              agentID,
		ActorID:              actorID,
		KeyID:                keyID,
		PrivateKeyPath:       keyPath,
		AccessToken:          payload.Tokens.AccessToken,
		RefreshToken:         payload.Tokens.RefreshToken,
		TokenType:            firstNonEmpty(payload.Tokens.TokenType, "Bearer"),
		AccessTokenExpiresAt: accessExpiry,
		CoreInstanceID:       anyString(handshake["core_instance_id"]),
	}
	if err := profile.Save(s.cfg.ProfilePath, prof); err != nil {
		return RegisterResult{}, errnorm.Wrap(errnorm.KindLocal, "profile_persist_failed", "failed to persist profile", err)
	}
	updated, ok, err := profile.Load(s.cfg.ProfilePath)
	if err != nil {
		return RegisterResult{}, errnorm.Wrap(errnorm.KindLocal, "profile_read_failed", "failed to verify persisted profile", err)
	}
	if !ok {
		return RegisterResult{}, errnorm.Local("profile_missing", "profile not found after save")
	}
	return RegisterResult{Profile: updated, Agent: payload.Agent, Key: payload.Key}, nil
}

func (s *Service) WhoAmI(ctx context.Context) (WhoAmIResult, error) {
	prof, err := s.ensureAccessToken(ctx)
	if err != nil {
		return WhoAmIResult{}, err
	}
	client, err := s.newClient(prof.AccessToken)
	if err != nil {
		return WhoAmIResult{}, errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}
	resp, err := client.RawCall(ctx, httpclient.RawRequest{Method: http.MethodGet, Path: "/agents/me"})
	if err != nil {
		return WhoAmIResult{}, errnorm.Wrap(errnorm.KindNetwork, "request_failed", "whoami request failed", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		if isTokenInvalid(resp.StatusCode, resp.Body) {
			refreshed, refreshErr := s.forceRefresh(ctx, prof)
			if refreshErr == nil {
				client, err = s.newClient(refreshed.AccessToken)
				if err == nil {
					resp, err = client.RawCall(ctx, httpclient.RawRequest{Method: http.MethodGet, Path: "/agents/me"})
				}
			}
		}
	}
	if err != nil {
		return WhoAmIResult{}, errnorm.Wrap(errnorm.KindNetwork, "request_failed", "whoami retry failed", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return WhoAmIResult{}, errnorm.FromHTTPFailure(resp.StatusCode, resp.Body)
	}

	var payload map[string]any
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		return WhoAmIResult{}, errnorm.Wrap(errnorm.KindRemote, "invalid_response", "whoami response is not valid JSON", err)
	}
	current, ok, err := profile.Load(s.cfg.ProfilePath)
	if err != nil {
		return WhoAmIResult{}, errnorm.Wrap(errnorm.KindLocal, "profile_read_failed", "failed to load profile", err)
	}
	if !ok {
		return WhoAmIResult{}, errnorm.Local("profile_not_found", "profile not found")
	}
	return WhoAmIResult{Profile: current, Server: payload}, nil
}

func (s *Service) UpdateUsername(ctx context.Context, username string) (WhoAmIResult, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return WhoAmIResult{}, errnorm.Usage("invalid_request", "username is required")
	}
	prof, err := s.ensureAccessToken(ctx)
	if err != nil {
		return WhoAmIResult{}, err
	}
	client, err := s.newClient(prof.AccessToken)
	if err != nil {
		return WhoAmIResult{}, errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}
	body, _ := json.Marshal(map[string]any{"username": username})
	resp, err := client.RawCall(ctx, httpclient.RawRequest{Method: http.MethodPatch, Path: "/agents/me", Body: body})
	if err != nil {
		return WhoAmIResult{}, errnorm.Wrap(errnorm.KindNetwork, "request_failed", "update username request failed", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return WhoAmIResult{}, errnorm.FromHTTPFailure(resp.StatusCode, resp.Body)
	}
	var payload map[string]any
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		return WhoAmIResult{}, errnorm.Wrap(errnorm.KindRemote, "invalid_response", "update response is not valid JSON", err)
	}
	if agentObj, ok := payload["agent"].(map[string]any); ok {
		prof.Username = firstNonEmpty(anyString(agentObj["username"]), username)
	}
	if err := profile.Save(s.cfg.ProfilePath, prof); err != nil {
		return WhoAmIResult{}, errnorm.Wrap(errnorm.KindLocal, "profile_persist_failed", "failed to persist profile", err)
	}
	return s.WhoAmI(ctx)
}

func (s *Service) RotateKey(ctx context.Context) (WhoAmIResult, error) {
	prof, err := s.ensureAccessToken(ctx)
	if err != nil {
		return WhoAmIResult{}, err
	}
	if strings.TrimSpace(prof.AgentID) == "" {
		return WhoAmIResult{}, errnorm.Local("profile_invalid", "profile missing agent_id; re-register required")
	}

	publicKey, privateKey, err := profile.GenerateEd25519KeyPair()
	if err != nil {
		return WhoAmIResult{}, errnorm.Wrap(errnorm.KindLocal, "key_generation_failed", "failed to generate key pair", err)
	}

	client, err := s.newClient(prof.AccessToken)
	if err != nil {
		return WhoAmIResult{}, errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}
	body, _ := json.Marshal(map[string]any{"public_key": publicKey})
	resp, err := client.RawCall(ctx, httpclient.RawRequest{Method: http.MethodPost, Path: "/agents/me/keys/rotate", Body: body})
	if err != nil {
		return WhoAmIResult{}, errnorm.Wrap(errnorm.KindNetwork, "request_failed", "rotate request failed", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return WhoAmIResult{}, errnorm.FromHTTPFailure(resp.StatusCode, resp.Body)
	}
	var payload map[string]any
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		return WhoAmIResult{}, errnorm.Wrap(errnorm.KindRemote, "invalid_response", "rotate response is not valid JSON", err)
	}
	keyObj, _ := payload["key"].(map[string]any)
	newKeyID := anyString(keyObj["key_id"])
	if strings.TrimSpace(newKeyID) == "" {
		return WhoAmIResult{}, errnorm.Local("invalid_response", "rotate response missing key_id")
	}

	keyPath := firstNonEmpty(strings.TrimSpace(prof.PrivateKeyPath), defaultKeyPathFromProfilePath(s.cfg.ProfilePath, prof.Agent))
	if err := profile.SavePrivateKey(keyPath, privateKey); err != nil {
		return WhoAmIResult{}, errnorm.Wrap(errnorm.KindLocal, "key_persist_failed", "failed to persist rotated key", err)
	}
	prof.PrivateKeyPath = keyPath
	prof.KeyID = newKeyID
	if err := profile.Save(s.cfg.ProfilePath, prof); err != nil {
		return WhoAmIResult{}, errnorm.Wrap(errnorm.KindLocal, "profile_persist_failed", "failed to persist rotated key metadata", err)
	}
	return s.WhoAmI(ctx)
}

func (s *Service) Revoke(ctx context.Context) (WhoAmIResult, error) {
	result, err := s.RevokeCurrentPrincipal(ctx, RevokeOptions{})
	if err != nil {
		return WhoAmIResult{}, err
	}
	prof, ok, loadErr := profile.Load(s.cfg.ProfilePath)
	if loadErr != nil {
		return WhoAmIResult{}, errnorm.Wrap(errnorm.KindLocal, "profile_read_failed", "failed to read profile", loadErr)
	}
	if !ok {
		return WhoAmIResult{}, errnorm.Local("profile_not_found", "profile not found; run `oar auth register` first")
	}
	prof.Revoked = true
	prof.AccessToken = ""
	prof.RefreshToken = ""
	prof.AccessTokenExpiresAt = ""
	if err := profile.Save(s.cfg.ProfilePath, prof); err != nil {
		return WhoAmIResult{}, errnorm.Wrap(errnorm.KindLocal, "profile_persist_failed", "failed to persist revoked profile", err)
	}
	return WhoAmIResult{
		Profile: prof,
		Server: map[string]any{
			"ok":         true,
			"principal":  result.Principal,
			"revocation": result.Revocation,
		},
	}, nil
}

func (s *Service) RevokeCurrentPrincipal(ctx context.Context, opts RevokeOptions) (RevokePrincipalResult, error) {
	prof, err := s.ensureAccessToken(ctx)
	if err != nil {
		return RevokePrincipalResult{}, err
	}
	client, err := s.newClient(prof.AccessToken)
	if err != nil {
		return RevokePrincipalResult{}, errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}
	body, _ := json.Marshal(map[string]any{
		"allow_human_lockout":  opts.AllowHumanLockout,
		"human_lockout_reason": strings.TrimSpace(opts.HumanLockoutReason),
	})
	resp, err := client.RawCall(ctx, httpclient.RawRequest{Method: http.MethodPost, Path: "/agents/me/revoke", Body: body})
	if err != nil {
		return RevokePrincipalResult{}, errnorm.Wrap(errnorm.KindNetwork, "request_failed", "revoke request failed", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return RevokePrincipalResult{}, errnorm.FromHTTPFailure(resp.StatusCode, resp.Body)
	}
	var payload struct {
		Principal  Principal  `json:"principal"`
		Revocation Revocation `json:"revocation"`
	}
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		return RevokePrincipalResult{}, errnorm.Wrap(errnorm.KindRemote, "invalid_response", "revoke response is not valid JSON", err)
	}
	return RevokePrincipalResult{Principal: payload.Principal, Revocation: payload.Revocation}, nil
}

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
		return RevokePrincipalResult{}, errnorm.Usage("invalid_request", "use `oar auth revoke` to revoke the current profile")
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

func (s *Service) TokenStatus(ctx context.Context) (TokenStatusResult, error) {
	prof, ok, err := profile.Load(s.cfg.ProfilePath)
	if err != nil {
		return TokenStatusResult{}, errnorm.Wrap(errnorm.KindLocal, "profile_read_failed", "failed to read profile", err)
	}
	if !ok {
		return TokenStatusResult{}, errnorm.Local("profile_not_found", "profile not found; run `oar auth register` first")
	}

	now := s.now()
	expiresAt, hasExpiry := profile.ParseAccessTokenExpiry(prof.AccessTokenExpiresAt)
	secondsRemaining := int64(0)
	needsRefresh := true
	if hasExpiry {
		secondsRemaining = int64(time.Until(expiresAt).Seconds())
		needsRefresh = time.Until(expiresAt) <= minAccessTokenTTL
	}
	status := TokenStatusResult{
		ProfilePath:        s.cfg.ProfilePath,
		Agent:              prof.Agent,
		AgentID:            prof.AgentID,
		Username:           prof.Username,
		HasAccessToken:     strings.TrimSpace(prof.AccessToken) != "",
		HasRefreshToken:    strings.TrimSpace(prof.RefreshToken) != "",
		AccessExpiresAt:    prof.AccessTokenExpiresAt,
		SecondsUntilExpiry: secondsRemaining,
		NeedsRefresh:       needsRefresh,
		Revoked:            prof.Revoked,
		CoreInstanceID:     prof.CoreInstanceID,
		PrivateKeyPath:     prof.PrivateKeyPath,
		Source:             now.Format(time.RFC3339Nano),
	}
	return status, nil
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

func (s *Service) EnsureAccessToken(ctx context.Context) (profile.Profile, error) {
	return s.ensureAccessToken(ctx)
}

func (s *Service) ensureAccessToken(ctx context.Context) (profile.Profile, error) {
	prof, ok, err := profile.Load(s.cfg.ProfilePath)
	if err != nil {
		return profile.Profile{}, errnorm.Wrap(errnorm.KindLocal, "profile_read_failed", "failed to read profile", err)
	}
	if !ok {
		return profile.Profile{}, errnorm.Local("profile_not_found", "profile not found; run `oar auth register` first")
	}
	if prof.Revoked {
		return profile.Profile{}, errnorm.Local("agent_revoked", "profile is revoked and cannot authenticate")
	}

	expiresAt, hasExpiry := profile.ParseAccessTokenExpiry(prof.AccessTokenExpiresAt)
	if strings.TrimSpace(prof.AccessToken) != "" && hasExpiry && time.Until(expiresAt) > minAccessTokenTTL {
		return prof, nil
	}

	return s.forceRefresh(ctx, prof)
}

func (s *Service) forceRefresh(ctx context.Context, prof profile.Profile) (profile.Profile, error) {
	if prof.Revoked {
		return profile.Profile{}, errnorm.Local("agent_revoked", "profile is revoked and cannot authenticate")
	}
	if strings.TrimSpace(prof.RefreshToken) != "" {
		updated, err := s.refreshWithRefreshToken(ctx, prof)
		if err == nil {
			return updated, nil
		}
		if !isRecoverableRefreshError(err) {
			return profile.Profile{}, err
		}
	}
	return s.refreshWithAssertion(ctx, prof)
}

func (s *Service) refreshWithRefreshToken(ctx context.Context, prof profile.Profile) (profile.Profile, error) {
	client, err := s.newClient("")
	if err != nil {
		return profile.Profile{}, errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}
	body, _ := json.Marshal(map[string]any{"grant_type": "refresh_token", "refresh_token": prof.RefreshToken})
	resp, err := client.RawCall(ctx, httpclient.RawRequest{Method: http.MethodPost, Path: "/auth/token", Body: body})
	if err != nil {
		return profile.Profile{}, errnorm.Wrap(errnorm.KindNetwork, "request_failed", "token refresh request failed", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return profile.Profile{}, errnorm.FromHTTPFailure(resp.StatusCode, resp.Body)
	}
	return s.applyTokenBundle(prof, resp.Body)
}

func (s *Service) refreshWithAssertion(ctx context.Context, prof profile.Profile) (profile.Profile, error) {
	if strings.TrimSpace(prof.AgentID) == "" || strings.TrimSpace(prof.KeyID) == "" {
		return profile.Profile{}, errnorm.Local("profile_invalid", "profile missing agent_id/key_id; cannot use assertion flow")
	}
	keyPath := firstNonEmpty(strings.TrimSpace(prof.PrivateKeyPath), defaultKeyPathFromProfilePath(s.cfg.ProfilePath, prof.Agent))
	privateKey, err := profile.LoadPrivateKey(keyPath)
	if err != nil {
		return profile.Profile{}, errnorm.Wrap(errnorm.KindLocal, "key_load_failed", "failed to load private key for assertion", err)
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return profile.Profile{}, errnorm.Local("key_invalid", "private key has invalid length")
	}
	signedAt := s.now().Format(time.RFC3339)
	message := buildAssertionMessage(prof.AgentID, prof.KeyID, signedAt)
	signature := ed25519.Sign(privateKey, []byte(message))
	signatureEncoded := base64.StdEncoding.EncodeToString(signature)

	client, err := s.newClient("")
	if err != nil {
		return profile.Profile{}, errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}
	body, _ := json.Marshal(map[string]any{
		"grant_type": "assertion",
		"agent_id":   prof.AgentID,
		"key_id":     prof.KeyID,
		"signed_at":  signedAt,
		"signature":  signatureEncoded,
	})
	resp, err := client.RawCall(ctx, httpclient.RawRequest{Method: http.MethodPost, Path: "/auth/token", Body: body})
	if err != nil {
		return profile.Profile{}, errnorm.Wrap(errnorm.KindNetwork, "request_failed", "assertion token request failed", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return profile.Profile{}, errnorm.FromHTTPFailure(resp.StatusCode, resp.Body)
	}
	updated, applyErr := s.applyTokenBundle(prof, resp.Body)
	if applyErr != nil {
		return profile.Profile{}, applyErr
	}
	updated.PrivateKeyPath = keyPath
	if err := profile.Save(s.cfg.ProfilePath, updated); err != nil {
		return profile.Profile{}, errnorm.Wrap(errnorm.KindLocal, "profile_persist_failed", "failed to persist profile after assertion refresh", err)
	}
	return updated, nil
}

func (s *Service) applyTokenBundle(prof profile.Profile, raw []byte) (profile.Profile, error) {
	var payload struct {
		Tokens struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			TokenType    string `json:"token_type"`
			ExpiresIn    int64  `json:"expires_in"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return profile.Profile{}, errnorm.Wrap(errnorm.KindRemote, "invalid_response", "token response is not valid JSON", err)
	}
	if strings.TrimSpace(payload.Tokens.AccessToken) == "" || strings.TrimSpace(payload.Tokens.RefreshToken) == "" {
		return profile.Profile{}, errnorm.Local("invalid_response", "token response missing token fields")
	}
	prof.AccessToken = strings.TrimSpace(payload.Tokens.AccessToken)
	prof.RefreshToken = strings.TrimSpace(payload.Tokens.RefreshToken)
	prof.TokenType = firstNonEmpty(strings.TrimSpace(payload.Tokens.TokenType), "Bearer")
	prof.AccessTokenExpiresAt = s.now().Add(time.Duration(payload.Tokens.ExpiresIn) * time.Second).Format(time.RFC3339Nano)
	if err := profile.Save(s.cfg.ProfilePath, prof); err != nil {
		return profile.Profile{}, errnorm.Wrap(errnorm.KindLocal, "profile_persist_failed", "failed to persist refreshed tokens", err)
	}
	updated, ok, err := profile.Load(s.cfg.ProfilePath)
	if err != nil {
		return profile.Profile{}, errnorm.Wrap(errnorm.KindLocal, "profile_read_failed", "failed to reload profile after token update", err)
	}
	if !ok {
		return profile.Profile{}, errnorm.Local("profile_not_found", "profile disappeared after token update")
	}
	return updated, nil
}

func (s *Service) fetchHandshake(ctx context.Context) (map[string]any, error) {
	client, err := s.newClient("")
	if err != nil {
		return nil, err
	}
	resp, err := client.RawCall(ctx, httpclient.RawRequest{Method: http.MethodGet, Path: "/meta/handshake"})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, errnorm.FromHTTPFailure(resp.StatusCode, resp.Body)
	}
	var payload map[string]any
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func (s *Service) newClient(accessToken string) (*httpclient.Client, error) {
	cfg := s.cfg
	cfg.AccessToken = strings.TrimSpace(accessToken)
	return httpclient.New(cfg)
}

func buildAssertionMessage(agentID string, keyID string, signedAt string) string {
	return "oar-auth-token|" + strings.TrimSpace(agentID) + "|" + strings.TrimSpace(keyID) + "|" + strings.TrimSpace(signedAt)
}

func inferRootDirFromProfilePath(profilePath string) string {
	profileDir := filepath.Dir(profilePath)
	return filepath.Dir(profileDir)
}

func defaultKeyPathFromProfilePath(profilePath string, agent string) string {
	agent = strings.TrimSpace(agent)
	if agent == "" {
		agent = "default"
	}
	return filepath.Join(inferRootDirFromProfilePath(profilePath), "keys", agent+".ed25519")
}

func anyString(raw any) string {
	text, _ := raw.(string)
	return strings.TrimSpace(text)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func classifyRegisterTransportError(cause error) error {
	if cause == nil {
		return errnorm.Network("request_failed", "register request failed")
	}
	if errors.Is(cause, context.DeadlineExceeded) {
		return errnorm.Wrap(
			errnorm.KindNetwork,
			"auth_registration_unavailable",
			"auth registration failed because core is not reachable yet",
			cause,
		)
	}
	return errnorm.Wrap(errnorm.KindNetwork, "request_failed", "register request failed", cause)
}

func classifyRegisterHTTPFailure(statusCode int, responseBody []byte) error {
	failure := errnorm.FromHTTPFailure(statusCode, responseBody)
	normalized := errnorm.Normalize(failure)
	if normalized == nil {
		return failure
	}

	if statusCode == http.StatusServiceUnavailable || statusCode >= http.StatusInternalServerError {
		return errnorm.WithDetails(
			errnorm.Wrap(
				errnorm.KindRemote,
				"auth_registration_unavailable",
				"auth registration is temporarily unavailable; core may still be starting, retry shortly",
				failure,
			),
			normalized.Details,
		)
	}
	if normalized.Code == "internal_error" || normalized.Code == "auth_unavailable" {
		return errnorm.WithDetails(
			errnorm.Wrap(
				errnorm.KindRemote,
				"auth_registration_unavailable",
				"auth registration is temporarily unavailable; core may still be starting, retry shortly",
				failure,
			),
			normalized.Details,
		)
	}
	return failure
}

func boolPtr(v bool) *bool {
	return &v
}

func isTokenInvalid(status int, body []byte) bool {
	if status != http.StatusUnauthorized && status != http.StatusForbidden {
		return false
	}
	code := extractErrorCode(body)
	return code == "invalid_token"
}

func isRecoverableRefreshError(err error) bool {
	normalized := errnorm.Normalize(err)
	if normalized == nil {
		return false
	}
	return normalized.Code == "invalid_token" || normalized.Code == "key_mismatch"
}

func extractErrorCode(body []byte) string {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	errObj, ok := payload["error"].(map[string]any)
	if !ok {
		return ""
	}
	code, _ := errObj["code"].(string)
	return strings.TrimSpace(code)
}
