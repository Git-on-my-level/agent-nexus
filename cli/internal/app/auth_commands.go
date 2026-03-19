package app

import (
	"context"
	"fmt"
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
		result, err := a.runAuthRevoke(ctx, service)
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
		line := fmt.Sprintf("  %s  kind=%s  status=%s", invite.ID, invite.Kind, status)
		if invite.Note != "" {
			line += "  note=" + invite.Note
		}
		lines = append(lines, line)
	}
	header := fmt.Sprintf("Invites (%d):", len(result.Invites))
	text := header + "\n" + strings.Join(lines, "\n")
	return &commandResult{Text: text, Data: map[string]any{"invites": result.Invites, "count": len(result.Invites)}}, nil
}

func (a *App) runAuthInvitesCreate(ctx context.Context, service *authcli.Service, args []string) (*commandResult, error) {
	fs := newSilentFlagSet("auth invites create")
	var kindFlag trackedString
	var noteFlag trackedString
	fs.Var(&kindFlag, "kind", "Invite kind (human, agent, or any)")
	fs.Var(&noteFlag, "note", "Optional note describing the invite")
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
	result, err := service.CreateInvite(ctx, kind, strings.TrimSpace(noteFlag.value))
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
	text := strings.Join([]string{
		"Local profile: " + result.Profile.Agent,
		"Local username: " + result.Profile.Username,
		"Local agent ID: " + result.Profile.AgentID,
		"Server username: " + anyString(serverAgent["username"]),
		"Server agent ID: " + anyString(serverAgent["agent_id"]),
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

func (a *App) runAuthRevoke(ctx context.Context, service *authcli.Service) (*commandResult, error) {
	result, err := service.Revoke(ctx)
	if err != nil {
		return nil, err
	}
	text := "Revoked agent profile and cleared local tokens."
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
