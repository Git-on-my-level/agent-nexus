package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"agent-nexus-cli/internal/authcli"
	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func (a *App) runAuth(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 || isHelpToken(args[0]) {
		if text, ok := helpTopicText("auth"); ok {
			return &commandResult{Text: text}, "auth", nil
		}
		return nil, "auth", authSubcommandSpec.requiredError()
	}
	service := authcli.New(cfg)
	subcommand := authSubcommandSpec.normalize(args[0])
	switch subcommand {
	case "whoami":
		result, err := a.runHostWhoAmI(ctx, cfg)
		return result, "auth whoami", err
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
	if len(args) == 0 || isHelpToken(args[0]) {
		if text, ok := generatedHelpText("auth invites"); ok {
			return &commandResult{Text: text}, nil
		}
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
	fs.Var(&kindFlag, "kind", "Invite kind (human or any)")
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
	if kind != "human" && kind != "any" {
		return nil, errnorm.Usage("invalid_invite_kind", "kind must be human or any")
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
	if len(args) == 0 || isHelpToken(args[0]) {
		if text, ok := generatedHelpText("auth bootstrap"); ok {
			return &commandResult{Text: text}, nil
		}
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
	if len(args) == 0 || isHelpToken(args[0]) {
		if text, ok := helpTopicText("auth principals"); ok {
			return &commandResult{Text: text}, nil
		}
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
	if len(args) == 0 || isHelpToken(args[0]) {
		if text, ok := helpTopicText("auth audit"); ok {
			return &commandResult{Text: text}, nil
		}
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
	limit, cursor, taggableOnly, handlesOnly, err := parseAuthPrincipalListFlags("auth principals list", args)
	if err != nil {
		return nil, err
	}
	result, err := service.ListPrincipals(ctx, limit, cursor)
	if err != nil {
		return nil, err
	}
	principals := result.Principals
	if taggableOnly {
		principals = filterTaggablePrincipals(principals)
	}
	handles := collectPrincipalHandles(principals)
	if len(principals) == 0 {
		data := map[string]any{
			"principals":                   []any{},
			"handles":                      []any{},
			"count":                        0,
			"active_human_principal_count": result.ActiveHumanPrincipalCount,
			"taggable_only":                taggableOnly,
			"handles_only":                 handlesOnly,
		}
		if result.NextCursor != "" {
			data["next_cursor"] = result.NextCursor
		}
		emptyText := "No principals found."
		if handlesOnly {
			emptyText = "No taggable handles found."
		}
		if result.NextCursor != "" {
			emptyText += "\n\nNext cursor: " + result.NextCursor
		}
		return &commandResult{Text: emptyText, Data: data}, nil
	}

	if handlesOnly {
		text := fmt.Sprintf("Taggable handles (%d):\n%s", len(handles), strings.Join(prefixHandles(handles), "\n"))
		data := map[string]any{
			"principals":                   principals,
			"handles":                      handles,
			"count":                        len(handles),
			"active_human_principal_count": result.ActiveHumanPrincipalCount,
			"taggable_only":                true,
			"handles_only":                 true,
		}
		if result.NextCursor != "" {
			text += "\n\nNext cursor: " + result.NextCursor
			data["next_cursor"] = result.NextCursor
		}
		return &commandResult{Text: text, Data: data}, nil
	}

	lines := make([]string, 0, len(principals))
	for _, principal := range principals {
		status := "active"
		if principal.Revoked {
			status = "revoked"
		}
		handle := principalWakeHandle(principal)
		wakeState := principalWakeState(principal)
		line := fmt.Sprintf("  %s  username=%s  kind=%s  auth=%s  status=%s", principal.AgentID, principal.Username, principal.PrincipalKind, principal.AuthMethod, status)
		if handle != "" {
			line = fmt.Sprintf("%s  handle=@%s  wake=%s", line, handle, wakeState)
		}
		lines = append(lines, line)
	}
	text := fmt.Sprintf("Principals (%d):\n%s", len(principals), strings.Join(lines, "\n"))
	data := map[string]any{
		"principals":                   principals,
		"handles":                      handles,
		"count":                        len(principals),
		"active_human_principal_count": result.ActiveHumanPrincipalCount,
		"taggable_only":                taggableOnly,
		"handles_only":                 false,
	}
	if result.NextCursor != "" {
		text += "\n\nNext cursor: " + result.NextCursor
		data["next_cursor"] = result.NextCursor
	}
	return &commandResult{Text: text, Data: data}, nil
}

func parseAuthPrincipalListFlags(commandName string, args []string) (int, string, bool, bool, error) {
	fs := newSilentFlagSet(commandName)
	var limitFlag trackedString
	var cursorFlag trackedString
	var taggableOnly trackedBool
	var handlesOnly trackedBool
	fs.Var(&limitFlag, "limit", "Maximum number of results to return")
	fs.Var(&cursorFlag, "cursor", "Opaque pagination cursor from a previous response")
	fs.Var(&taggableOnly, "taggable", "Show only principals whose wake routing is taggable")
	fs.Var(&handlesOnly, "handles-only", "Print only currently taggable @handles")
	if err := fs.Parse(args); err != nil {
		return 0, "", false, false, errnorm.Usage("invalid_auth_list_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return 0, "", false, false, errnorm.Usage("invalid_auth_list_args", "unexpected positional arguments")
	}

	limit := 0
	if strings.TrimSpace(limitFlag.value) != "" {
		parsed, err := parsePositiveInt(limitFlag.value)
		if err != nil {
			return 0, "", false, false, errnorm.Usage("invalid_request", "limit must be a positive integer")
		}
		limit = parsed
	}
	return limit, strings.TrimSpace(cursorFlag.value), bool(taggableOnly.value || handlesOnly.value), bool(handlesOnly.value), nil
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
		"Bootstrap the first human through the workspace Access flow, then run `anx host enroll`.",
	}, "\n")
	data := map[string]any{"bootstrap_registration_available": result.BootstrapRegistrationAvailable}
	return &commandResult{Text: text, Data: data}, nil
}

func filterTaggablePrincipals(principals []authcli.Principal) []authcli.Principal {
	filtered := make([]authcli.Principal, 0, len(principals))
	for _, principal := range principals {
		if principal.WakeRouting != nil && principal.WakeRouting.Taggable {
			filtered = append(filtered, principal)
		}
	}
	return filtered
}

func collectPrincipalHandles(principals []authcli.Principal) []string {
	seen := map[string]struct{}{}
	handles := make([]string, 0, len(principals))
	for _, principal := range principals {
		if principal.WakeRouting == nil || !principal.WakeRouting.Taggable {
			continue
		}
		handle := principalWakeHandle(principal)
		if handle == "" {
			continue
		}
		if _, exists := seen[handle]; exists {
			continue
		}
		seen[handle] = struct{}{}
		handles = append(handles, handle)
	}
	return handles
}

func prefixHandles(handles []string) []string {
	lines := make([]string, 0, len(handles))
	for _, handle := range handles {
		lines = append(lines, "@"+handle)
	}
	return lines
}

func principalWakeHandle(principal authcli.Principal) string {
	if principal.WakeRouting != nil {
		handle := strings.TrimSpace(principal.WakeRouting.Handle)
		if handle != "" {
			return handle
		}
	}
	return strings.TrimSpace(principal.Username)
}

func principalWakeState(principal authcli.Principal) string {
	if principal.WakeRouting == nil {
		return "unknown"
	}
	state := strings.TrimSpace(principal.WakeRouting.State)
	if state == "" {
		return "unknown"
	}
	return state
}

func anyString(raw any) string {
	text, _ := raw.(string)
	return strings.TrimSpace(text)
}
