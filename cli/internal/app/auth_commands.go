package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"organization-autorunner-cli/internal/authcli"
	"organization-autorunner-cli/internal/config"
	"organization-autorunner-cli/internal/errnorm"
	"organization-autorunner-cli/internal/profile"
)

func (a *App) runAuth(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "auth", authSubcommandSpec.requiredError()
	}
	service := authcli.New(cfg)
	subcommand := authSubcommandSpec.normalize(args[0])
	switch subcommand {
	case "register":
		result, err := a.runAuthRegister(ctx, service, args[1:])
		return result, "auth register", err
	case "whoami":
		result, err := a.runAuthWhoAmI(ctx, service)
		return result, "auth whoami", err
	case "list":
		result, err := a.runAuthList(cfg)
		return result, "auth list", err
	case "update-username":
		result, err := a.runAuthUpdateUsername(ctx, service, args[1:])
		return result, "auth update-username", err
	case "rotate":
		result, err := a.runAuthRotate(ctx, service)
		return result, "auth rotate", err
	case "revoke":
		result, err := a.runAuthRevoke(ctx, service, args[1:])
		return result, "auth revoke", err
	case "token-status":
		result, err := a.runAuthTokenStatus(ctx, service)
		return result, "auth token-status", err
	case "invites":
		result, err := a.runAuthInvites(ctx, service, args[1:])
		return result, "auth invites", err
	case "bootstrap":
		result, err := a.runAuthBootstrap(ctx, service, args[1:])
		return result, "auth bootstrap", err
	case "principals":
		result, err := a.runAuthPrincipals(ctx, service, args[1:])
		return result, "auth principals", err
	case "audit":
		result, err := a.runAuthAudit(ctx, service, args[1:])
		return result, "auth audit", err
	default:
		return nil, "auth", authSubcommandSpec.unknownError(args[0])
	}
}

func (a *App) runAuthInvites(ctx context.Context, service *authcli.Service, args []string) (*commandResult, error) {
	if len(args) == 0 {
		return nil, authInvitesSubcommandSpec.requiredError()
	}
	subcommand := authInvitesSubcommandSpec.normalize(args[0])
	switch subcommand {
	case "list":
		return a.runAuthInvitesList(ctx, service)
	case "create":
		return a.runAuthInvitesCreate(ctx, service, args[1:])
	case "revoke":
		return a.runAuthInvitesRevoke(ctx, service, args[1:])
	default:
		return nil, authInvitesSubcommandSpec.unknownError(args[0])
	}
}

func (a *App) runAuthInvitesList(ctx context.Context, service *authcli.Service) (*commandResult, error) {
	result, err := service.ListInvites(ctx)
	if err != nil {
		return nil, err
	}
	if len(result.Invites) == 0 {
		return &commandResult{
			Text: "No invites found.",
			Data: map[string]any{"invites": []any{}, "count": 0},
		}, nil
	}
	var lines []string
	for _, invite := range result.Invites {
		status := "pending"
		if invite.RevokedAt != "" {
			status = "revoked"
		} else if invite.ConsumedAt != "" {
			status = "consumed"
		}
		lines = append(lines, fmt.Sprintf("  %s  kind=%s  status=%s", invite.ID, invite.Kind, status))
	}
	header := fmt.Sprintf("Invites (%d):", len(result.Invites))
	text := header + "\n" + strings.Join(lines, "\n")
	return &commandResult{Text: text, Data: map[string]any{"invites": result.Invites, "count": len(result.Invites)}}, nil
}

func (a *App) runAuthInvitesCreate(ctx context.Context, service *authcli.Service, args []string) (*commandResult, error) {
	fs := newSilentFlagSet("auth invites create")
	var kindFlag trackedString
	fs.Var(&kindFlag, "kind", "Invite kind (human, agent, or any)")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_auth_invites_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_auth_invites_args", "unexpected positional arguments")
	}
	kind := strings.TrimSpace(kindFlag.value)
	if kind == "" {
		return nil, errnorm.Usage("invite_kind_required", "kind is required")
	}
	if kind != "human" && kind != "agent" && kind != "any" {
		return nil, errnorm.Usage("invalid_invite_kind", "kind must be human, agent, or any")
	}
	result, err := service.CreateInvite(ctx, kind)
	if err != nil {
		return nil, err
	}
	text := strings.Join([]string{
		"Created invite successfully.",
		"Invite ID: " + result.Invite.ID,
		"Kind: " + result.Invite.Kind,
		"Token: " + result.Token,
		"",
		"Share the token with the recipient. The token is shown only once.",
	}, "\n")
	data := map[string]any{"invite": result.Invite, "token": result.Token}
	return &commandResult{Text: text, Data: data}, nil
}

func (a *App) runAuthInvitesRevoke(ctx context.Context, service *authcli.Service, args []string) (*commandResult, error) {
	fs := newSilentFlagSet("auth invites revoke")
	var inviteIDFlag trackedString
	fs.Var(&inviteIDFlag, "invite-id", "Invite ID to revoke")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_auth_invites_revoke_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_auth_invites_revoke_args", "unexpected positional arguments")
	}
	inviteID := strings.TrimSpace(inviteIDFlag.value)
	if inviteID == "" {
		return nil, errnorm.Usage("invite_id_required", "invite-id is required")
	}
	result, err := service.RevokeInvite(ctx, inviteID)
	if err != nil {
		return nil, err
	}
	text := "Revoked invite " + result.Invite.ID
	data := map[string]any{"invite": result.Invite}
	return &commandResult{Text: text, Data: data}, nil
}

func (a *App) runAuthBootstrap(ctx context.Context, service *authcli.Service, args []string) (*commandResult, error) {
	if len(args) == 0 {
		return nil, authBootstrapSubcommandSpec.requiredError()
	}
	subcommand := authBootstrapSubcommandSpec.normalize(args[0])
	switch subcommand {
	case "status":
		return a.runAuthBootstrapStatus(ctx, service)
	default:
		return nil, authBootstrapSubcommandSpec.unknownError(args[0])
	}
}

func (a *App) runAuthPrincipals(ctx context.Context, service *authcli.Service, args []string) (*commandResult, error) {
	if len(args) == 0 {
		return nil, authPrincipalsSubcommandSpec.requiredError()
	}
	switch authPrincipalsSubcommandSpec.normalize(args[0]) {
	case "list":
		return a.runAuthPrincipalsList(ctx, service, args[1:])
	case "revoke":
		return a.runAuthPrincipalsRevoke(ctx, service, args[1:])
	default:
		return nil, authPrincipalsSubcommandSpec.unknownError(args[0])
	}
}

func (a *App) runAuthAudit(ctx context.Context, service *authcli.Service, args []string) (*commandResult, error) {
	if len(args) == 0 {
		return nil, authAuditSubcommandSpec.requiredError()
	}
	switch authAuditSubcommandSpec.normalize(args[0]) {
	case "list":
		return a.runAuthAuditList(ctx, service, args[1:])
	default:
		return nil, authAuditSubcommandSpec.unknownError(args[0])
	}
}

func (a *App) runAuthPrincipalsList(ctx context.Context, service *authcli.Service, args []string) (*commandResult, error) {
	limit, cursor, err := parseAuthListFlags("auth principals list", args)
	if err != nil {
		return nil, err
	}
	result, err := service.ListPrincipals(ctx, limit, cursor)
	if err != nil {
		return nil, err
	}
	if len(result.Principals) == 0 {
		data := map[string]any{
			"principals":                   []any{},
			"count":                        0,
			"active_human_principal_count": result.ActiveHumanPrincipalCount,
		}
		if result.NextCursor != "" {
			data["next_cursor"] = result.NextCursor
		}
		return &commandResult{Text: "No principals found.", Data: data}, nil
	}

	lines := make([]string, 0, len(result.Principals))
	for _, principal := range result.Principals {
		status := "active"
		if principal.Revoked {
			status = "revoked"
		}
		lines = append(
			lines,
			fmt.Sprintf("  %s  username=%s  kind=%s  auth=%s  status=%s", principal.AgentID, principal.Username, principal.PrincipalKind, principal.AuthMethod, status),
		)
	}
	text := fmt.Sprintf("Principals (%d):\n%s", len(result.Principals), strings.Join(lines, "\n"))
	data := map[string]any{
		"principals":                   result.Principals,
		"count":                        len(result.Principals),
		"active_human_principal_count": result.ActiveHumanPrincipalCount,
	}
	if result.NextCursor != "" {
		text += "\n\nNext cursor: " + result.NextCursor
		data["next_cursor"] = result.NextCursor
	}
	return &commandResult{Text: text, Data: data}, nil
}

func (a *App) runAuthRevoke(ctx context.Context, service *authcli.Service, args []string) (*commandResult, error) {
	opts, err := parseSelfRevokeOptions("auth revoke", args)
	if err != nil {
		return nil, err
	}
	result, err := service.RevokeCurrentPrincipal(ctx, opts)
	if err != nil {
		return nil, err
	}
	cfg := service.Config()
	prof, ok, err := profile.Load(cfg.ProfilePath)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "profile_read_failed", "failed to read profile", err)
	}
	if !ok {
		return nil, errnorm.Local("profile_not_found", "profile not found; run `oar auth register` first")
	}
	prof.Revoked = true
	prof.AccessToken = ""
	prof.RefreshToken = ""
	prof.AccessTokenExpiresAt = ""
	if err := profile.Save(cfg.ProfilePath, prof); err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "profile_persist_failed", "failed to persist revoked profile", err)
	}
	text := "Revoked agent profile and cleared local tokens."
	if result.Revocation.AllowHumanLockout {
		text += " Break-glass human lockout was used."
	}
	data := map[string]any{"profile": prof, "revocation": result.Revocation}
	return &commandResult{Text: text, Data: data}, nil
}

func (a *App) runAuthPrincipalsRevoke(ctx context.Context, service *authcli.Service, args []string) (*commandResult, error) {
	agentID, opts, err := parsePrincipalRevokeOptions("auth principals revoke", args)
	if err != nil {
		return nil, err
	}

	result, err := service.RevokePrincipal(ctx, agentID, opts)
	if err != nil {
		return nil, err
	}

	status := "revoked"
	if result.Revocation.AlreadyRevoked {
		status = "already-revoked"
	}
	text := fmt.Sprintf("Principal %s %s.", result.Principal.AgentID, status)
	if result.Revocation.AllowHumanLockout {
		text += " Break-glass human lockout was used."
	}
	return &commandResult{
		Text: text,
		Data: map[string]any{
			"principal":  result.Principal,
			"revocation": result.Revocation,
		},
	}, nil
}

func parseSelfRevokeOptions(commandName string, args []string) (authcli.RevokeOptions, error) {
	fs := newSilentFlagSet(commandName)
	var allowHumanLockoutFlag trackedBool
	var humanLockoutReasonFlag trackedString
	fs.Var(&allowHumanLockoutFlag, "allow-human-lockout", "Explicit break-glass override to revoke the last active human principal")
	fs.Var(&humanLockoutReasonFlag, "human-lockout-reason", "Required reason when using --allow-human-lockout")
	if err := fs.Parse(args); err != nil {
		return authcli.RevokeOptions{}, errnorm.Usage("invalid_auth_revoke_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return authcli.RevokeOptions{}, errnorm.Usage("invalid_auth_revoke_args", "unexpected positional arguments")
	}
	allowHumanLockout := allowHumanLockoutFlag.value
	humanLockoutReason := strings.TrimSpace(humanLockoutReasonFlag.value)
	if allowHumanLockout && humanLockoutReason == "" {
		return authcli.RevokeOptions{}, errnorm.Usage("human_lockout_reason_required", "human-lockout-reason is required when allow-human-lockout is set")
	}
	if !allowHumanLockout && humanLockoutReason != "" {
		return authcli.RevokeOptions{}, errnorm.Usage("human_lockout_reason_requires_allow", "human-lockout-reason requires --allow-human-lockout")
	}
	return authcli.RevokeOptions{
		AllowHumanLockout:  allowHumanLockout,
		HumanLockoutReason: humanLockoutReason,
	}, nil
}

func parsePrincipalRevokeOptions(commandName string, args []string) (string, authcli.RevokeOptions, error) {
	fs := newSilentFlagSet(commandName)
	var agentIDFlag trackedString
	var allowHumanLockoutFlag trackedBool
	var humanLockoutReasonFlag trackedString
	fs.Var(&agentIDFlag, "agent-id", "Principal agent ID to revoke")
	fs.Var(&allowHumanLockoutFlag, "allow-human-lockout", "Explicit break-glass override to revoke the last active human principal")
	fs.Var(&humanLockoutReasonFlag, "human-lockout-reason", "Required reason when using --allow-human-lockout")
	if err := fs.Parse(args); err != nil {
		return "", authcli.RevokeOptions{}, errnorm.Usage("invalid_auth_principals_revoke_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return "", authcli.RevokeOptions{}, errnorm.Usage("invalid_auth_principals_revoke_args", "unexpected positional arguments")
	}
	agentID := strings.TrimSpace(agentIDFlag.value)
	if agentID == "" {
		return "", authcli.RevokeOptions{}, errnorm.Usage("agent_id_required", "agent-id is required")
	}
	allowHumanLockout := allowHumanLockoutFlag.value
	humanLockoutReason := strings.TrimSpace(humanLockoutReasonFlag.value)
	if allowHumanLockout && humanLockoutReason == "" {
		return "", authcli.RevokeOptions{}, errnorm.Usage("human_lockout_reason_required", "human-lockout-reason is required when allow-human-lockout is set")
	}
	if !allowHumanLockout && humanLockoutReason != "" {
		return "", authcli.RevokeOptions{}, errnorm.Usage("human_lockout_reason_requires_allow", "human-lockout-reason requires --allow-human-lockout")
	}
	return agentID, authcli.RevokeOptions{
		AllowHumanLockout:  allowHumanLockout,
		HumanLockoutReason: humanLockoutReason,
	}, nil
}

func (a *App) runAuthAuditList(ctx context.Context, service *authcli.Service, args []string) (*commandResult, error) {
	limit, cursor, err := parseAuthListFlags("auth audit list", args)
	if err != nil {
		return nil, err
	}
	result, err := service.ListAudit(ctx, limit, cursor)
	if err != nil {
		return nil, err
	}
	if len(result.Events) == 0 {
		data := map[string]any{"events": []any{}, "count": 0}
		if result.NextCursor != "" {
			data["next_cursor"] = result.NextCursor
		}
		return &commandResult{Text: "No auth audit events found.", Data: data}, nil
	}

	lines := make([]string, 0, len(result.Events))
	for _, event := range result.Events {
		parts := []string{event.OccurredAt, event.EventType}
		if event.ActorAgentID != "" {
			parts = append(parts, "actor="+event.ActorAgentID)
		}
		if event.SubjectAgentID != "" {
			parts = append(parts, "subject="+event.SubjectAgentID)
		}
		if event.InviteID != "" {
			parts = append(parts, "invite="+event.InviteID)
		}
		lines = append(lines, "  "+strings.Join(parts, "  "))
	}
	text := fmt.Sprintf("Auth audit events (%d):\n%s", len(result.Events), strings.Join(lines, "\n"))
	data := map[string]any{"events": result.Events, "count": len(result.Events)}
	if result.NextCursor != "" {
		text += "\n\nNext cursor: " + result.NextCursor
		data["next_cursor"] = result.NextCursor
	}
	return &commandResult{Text: text, Data: data}, nil
}

func parseAuthListFlags(commandName string, args []string) (int, string, error) {
	fs := newSilentFlagSet(commandName)
	var limitFlag trackedString
	var cursorFlag trackedString
	fs.Var(&limitFlag, "limit", "Maximum number of results to return")
	fs.Var(&cursorFlag, "cursor", "Opaque pagination cursor from a previous response")
	if err := fs.Parse(args); err != nil {
		return 0, "", errnorm.Usage("invalid_auth_list_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return 0, "", errnorm.Usage("invalid_auth_list_args", "unexpected positional arguments")
	}

	limit := 0
	if strings.TrimSpace(limitFlag.value) != "" {
		parsed, err := parsePositiveInt(limitFlag.value)
		if err != nil {
			return 0, "", errnorm.Usage("invalid_request", "limit must be a positive integer")
		}
		limit = parsed
	}
	return limit, strings.TrimSpace(cursorFlag.value), nil
}

func parsePositiveInt(raw string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, err
	}
	if value <= 0 {
		return 0, fmt.Errorf("value must be greater than zero")
	}
	return value, nil
}

func (a *App) runAuthBootstrapStatus(ctx context.Context, service *authcli.Service) (*commandResult, error) {
	result, err := service.BootstrapStatus(ctx)
	if err != nil {
		return nil, err
	}
	status := "not available"
	if result.BootstrapRegistrationAvailable {
		status = "available"
	}
	text := strings.Join([]string{
		"Bootstrap registration: " + status,
		"",
		"If bootstrap is available, you can register the first principal with:",
		"  oar auth register --username <name> --bootstrap-token <token>",
	}, "\n")
	data := map[string]any{"bootstrap_registration_available": result.BootstrapRegistrationAvailable}
	return &commandResult{Text: text, Data: data}, nil
}

func (a *App) runAuthRegister(ctx context.Context, service *authcli.Service, args []string) (*commandResult, error) {
	fs := newSilentFlagSet("auth register")
	var usernameFlag trackedString
	var bootstrapTokenFlag trackedString
	var inviteTokenFlag trackedString
	fs.Var(&usernameFlag, "username", "Agent username")
	fs.Var(&bootstrapTokenFlag, "bootstrap-token", "Bootstrap token for first principal registration")
	fs.Var(&inviteTokenFlag, "invite-token", "Invite token for subsequent principal registration")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_auth_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_auth_args", "unexpected positional arguments for `oar auth register`")
	}
	username := strings.TrimSpace(usernameFlag.value)
	if username == "" {
		username = strings.TrimSpace(a.Getenv("OAR_USERNAME"))
	}
	if username == "" {
		return nil, errnorm.Usage("invalid_request", "username is required; use --username or OAR_USERNAME")
	}
	bootstrapToken := strings.TrimSpace(bootstrapTokenFlag.value)
	inviteToken := strings.TrimSpace(inviteTokenFlag.value)
	if bootstrapToken != "" && inviteToken != "" {
		return nil, errnorm.Usage("invalid_request", "cannot specify both --bootstrap-token and --invite-token")
	}
	registered, err := service.RegisterWithToken(ctx, username, bootstrapToken, inviteToken)
	if err != nil {
		return nil, err
	}
	cfg := service.Config()
	text := strings.Join([]string{
		"Registered agent profile successfully.",
		"Agent: " + registered.Profile.Agent,
		"Agent ID: " + registered.Profile.AgentID,
		"Username: " + registered.Profile.Username,
		"Profile path: " + cfg.ProfilePath,
		authWakeRoutingHint(registered.Profile.Username),
	}, "\n")
	data := map[string]any{
		"profile":      registered.Profile,
		"registered":   registered.Agent,
		"active_key":   registered.Key,
		"profile_path": cfg.ProfilePath,
	}
	return &commandResult{Text: text, Data: data}, nil
}

func (a *App) runAuthWhoAmI(ctx context.Context, service *authcli.Service) (*commandResult, error) {
	result, err := service.WhoAmI(ctx)
	if err != nil {
		return nil, err
	}
	serverAgent, _ := result.Server["agent"].(map[string]any)
	hintHandle := strings.TrimSpace(anyString(serverAgent["username"]))
	if hintHandle == "" {
		hintHandle = result.Profile.Username
	}
	text := strings.Join([]string{
		"Local profile: " + result.Profile.Agent,
		"Local username: " + result.Profile.Username,
		"Local agent ID: " + result.Profile.AgentID,
		"Local actor ID: " + result.Profile.ActorID,
		"Server username: " + anyString(serverAgent["username"]),
		"Server agent ID: " + anyString(serverAgent["agent_id"]),
		"Server actor ID: " + anyString(serverAgent["actor_id"]),
		authWakeRoutingHint(hintHandle),
	}, "\n")
	redacted := result.Profile
	redacted.AccessToken = ""
	redacted.RefreshToken = ""
	redacted.PrivateKeyPath = ""
	data := map[string]any{
		"profile": redacted,
		"server":  result.Server,
	}
	return &commandResult{Text: text, Data: data}, nil
}

func authWakeRoutingHint(username string) string {
	handle := strings.TrimSpace(username)
	if handle == "" {
		return "Wake registration help: oar help bridge; oar meta doc agent-bridge; oar meta doc wake-routing"
	}
	return fmt.Sprintf(
		"Wake registration help: oar help bridge; oar meta doc agent-bridge; oar meta doc wake-routing (document id: agentreg.%s)",
		handle,
	)
}

func (a *App) runAuthUpdateUsername(ctx context.Context, service *authcli.Service, args []string) (*commandResult, error) {
	fs := newSilentFlagSet("auth update-username")
	var usernameFlag trackedString
	fs.Var(&usernameFlag, "username", "New username")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_auth_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_auth_args", "unexpected positional arguments for `oar auth update-username`")
	}
	username := strings.TrimSpace(usernameFlag.value)
	if username == "" {
		username = strings.TrimSpace(a.Getenv("OAR_USERNAME"))
	}
	if username == "" {
		return nil, errnorm.Usage("invalid_request", "username is required; use --username or OAR_USERNAME")
	}
	result, err := service.UpdateUsername(ctx, username)
	if err != nil {
		return nil, err
	}
	text := "Updated username to " + result.Profile.Username
	data := map[string]any{"profile": result.Profile, "server": result.Server}
	return &commandResult{Text: text, Data: data}, nil
}

func (a *App) runAuthRotate(ctx context.Context, service *authcli.Service) (*commandResult, error) {
	result, err := service.RotateKey(ctx)
	if err != nil {
		return nil, err
	}
	text := strings.Join([]string{
		"Rotated auth key successfully.",
		"Agent: " + result.Profile.Agent,
		"Key ID: " + result.Profile.KeyID,
		"Key path: " + result.Profile.PrivateKeyPath,
	}, "\n")
	data := map[string]any{"profile": result.Profile, "server": result.Server}
	return &commandResult{Text: text, Data: data}, nil
}

func (a *App) runAuthTokenStatus(ctx context.Context, service *authcli.Service) (*commandResult, error) {
	status, err := service.TokenStatus(ctx)
	if err != nil {
		return nil, err
	}
	text := strings.Join([]string{
		"Agent: " + status.Agent,
		"Agent ID: " + status.AgentID,
		"Username: " + status.Username,
		fmt.Sprintf("Has access token: %t", status.HasAccessToken),
		fmt.Sprintf("Has refresh token: %t", status.HasRefreshToken),
		"Access expires at: " + status.AccessExpiresAt,
		fmt.Sprintf("Needs refresh: %t", status.NeedsRefresh),
		fmt.Sprintf("Revoked: %t", status.Revoked),
	}, "\n")
	return &commandResult{Text: text, Data: status}, nil
}

func (a *App) runAuthList(cfg config.Resolved) (*commandResult, error) {
	homeDir, err := a.UserHomeDir()
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "home_dir", "failed to determine home directory", err)
	}
	agents, err := profile.ListAgents(homeDir)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "list_profiles", "failed to list agent profiles", err)
	}
	if len(agents) == 0 {
		return &commandResult{
			Text: "No agent profiles found.\nRegister with: oar --base-url <url> --agent <name> auth register --username <username>",
			Data: map[string]any{"profiles": []any{}, "count": 0},
		}, nil
	}

	type profileSummary struct {
		Agent    string `json:"agent"`
		BaseURL  string `json:"base_url"`
		AgentID  string `json:"agent_id,omitempty"`
		Username string `json:"username,omitempty"`
		Revoked  bool   `json:"revoked,omitempty"`
		Active   bool   `json:"active"`
		Path     string `json:"path"`
	}

	summaries := make([]profileSummary, 0, len(agents))
	var lines []string

	for _, agentName := range agents {
		path := profile.ProfilePath(homeDir, agentName)
		prof, ok, loadErr := profile.Load(path)
		if loadErr != nil || !ok {
			summaries = append(summaries, profileSummary{
				Agent:  agentName,
				Active: agentName == cfg.Agent,
				Path:   path,
			})
			status := "(unreadable)"
			if agentName == cfg.Agent {
				status += " *active*"
			}
			lines = append(lines, fmt.Sprintf("  %s  %s", agentName, status))
			continue
		}

		active := agentName == cfg.Agent
		summaries = append(summaries, profileSummary{
			Agent:    agentName,
			BaseURL:  prof.BaseURL,
			AgentID:  prof.AgentID,
			Username: prof.Username,
			Revoked:  prof.Revoked,
			Active:   active,
			Path:     path,
		})

		marker := "  "
		if active {
			marker = "* "
		}
		status := prof.BaseURL
		if prof.Revoked {
			status += " (revoked)"
		}
		if prof.Username != "" {
			status = prof.Username + "  " + status
		}
		lines = append(lines, fmt.Sprintf("%s%-16s %s", marker, agentName, status))
	}

	header := fmt.Sprintf("Agent profiles (%d):", len(agents))
	text := header + "\n" + strings.Join(lines, "\n")

	return &commandResult{
		Text: text,
		Data: map[string]any{"profiles": summaries, "count": len(summaries)},
	}, nil
}

func anyString(raw any) string {
	text, _ := raw.(string)
	return strings.TrimSpace(text)
}
