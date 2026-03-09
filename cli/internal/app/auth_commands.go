package app

import (
	"context"
	"fmt"
	"strings"

	"organization-autorunner-cli/internal/authcli"
	"organization-autorunner-cli/internal/config"
	"organization-autorunner-cli/internal/errnorm"
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
	default:
		return nil, "auth", authSubcommandSpec.unknownError(args[0])
	}
}

func (a *App) runAuthRegister(ctx context.Context, service *authcli.Service, args []string) (*commandResult, error) {
	fs := newSilentFlagSet("auth register")
	var usernameFlag trackedString
	fs.Var(&usernameFlag, "username", "Agent username")
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
	registered, err := service.Register(ctx, username)
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

func anyString(raw any) string {
	text, _ := raw.(string)
	return strings.TrimSpace(text)
}
