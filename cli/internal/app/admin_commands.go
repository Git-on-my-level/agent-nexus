package app

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

var authAdminsSubcommandSpec = subcommandSpec{command: "auth admins", valid: []string{"list", "grant", "revoke"}, examples: []string{"anx auth admins list", "anx auth admins grant codex.host-a", "anx auth admins revoke codex.host-a"}}
var hostEnrollmentsSubcommandSpec = subcommandSpec{command: "host enrollments", valid: []string{"list", "approve", "deny"}, examples: []string{"anx host enrollments list", "anx host enrollments approve ABCD-EFGH", "anx host enrollments deny ABCD-EFGH"}}
var hostTokensSubcommandSpec = subcommandSpec{command: "host tokens", valid: []string{"create", "get", "list", "revoke"}, examples: []string{"anx host tokens create --label fleet --expires-in 1h", "anx host tokens get <token-id>", "anx host tokens list", "anx host tokens revoke <token-id>"}}

func init() {
	localHelperTopics = append(localHelperTopics, localHelperTopic{Path: "host enroll", Summary: "Enroll this machine with interactive approval or a one-time fleet token.", Flags: []localHelperFlag{
		{Name: "--name <slug>", Description: "Workspace-local host slug."},
		{Name: "--token-stdin", Description: "Read the one-time enrollment token from piped stdin; mutually exclusive with --token and --plan."},
		{Name: "--token <secret>", Description: "Headless token (prefer --token-stdin to keep secrets out of argv)."},
		{Name: "--exclude <name>", Description: "Leave this legacy profile standalone; repeatable."},
		{Name: "--plan", Description: "Show the adoption plan without enrolling."},
	}, Examples: []string{"anx host enroll --name host-b", "anx --json host tokens create --label host-b --expires-in 1h | jq -er '.result.token' | ssh host-b 'anx host enroll --name host-b --token-stdin'"}})

	for _, spec := range []subcommandSpec{authAdminsSubcommandSpec, hostEnrollmentsSubcommandSpec, hostTokensSubcommandSpec} {
		for _, verb := range spec.valid {
			flags := []localHelperFlag{}
			if spec.command == "auth admins" && verb == "list" {
				flags = []localHelperFlag{{Name: "--limit <1..200>", Description: "Page size; default 50."}, {Name: "--cursor <cursor>", Description: "Continue from next_cursor."}}
			}
			if spec.command == "host tokens" && verb == "create" {
				flags = []localHelperFlag{{Name: "--label <label>", Description: "Audit label for this one-time token."}, {Name: "--expires-in <duration>", Description: "Lifetime from 10m to 24h; default 1h."}}
			}
			summary := "Explicit workspace administration. Grants and revocations of auth-admin require a human; host administration accepts granted agents."
			if spec.command == "host enrollments" && verb == "deny" {
				summary += " Deny a pending request or cancel an approval before completion; list includes both statuses."
			}
			if spec.command == "auth admins" && verb == "grant" {
				summary += " Granting an agent on host X trusts every process that can read X's shared host key and request that agent name. Agents cannot issue or revoke human invitations, revoke principals, or use the human lockout override."
			}
			localHelperTopics = append(localHelperTopics, localHelperTopic{Path: spec.command + " " + verb, Summary: summary, Flags: flags, Examples: spec.examples})
		}
	}
	localHelperTopics = append(localHelperTopics, localHelperTopic{Path: "host revoke", Summary: "Revoke a host by ID or slug. Agents cannot revoke their own host.", Examples: []string{"anx host revoke host-b"}})
}

func adminTarget(args []string) (string, error) {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" || strings.HasPrefix(args[0], "-") {
		return "", errnorm.Usage("invalid_args", "provide exactly one target as a leading positional argument")
	}
	return strings.TrimSpace(args[0]), nil
}

func (a *App) runAuthAdmins(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	name := "auth admins"
	if len(args) == 0 {
		return nil, name, authAdminsSubcommandSpec.requiredError()
	}
	name += " " + args[0]
	if args[0] == "list" {
		query, err := parseAuthAdminList(args[1:])
		if err != nil {
			return nil, name, err
		}
		r, e := a.invokeRawJSON(ctx, cfg, name, "GET", "/auth/admins?"+query.Encode(), nil)
		return r, name, e
	}
	if args[0] != "grant" && args[0] != "revoke" {
		return nil, name, authAdminsSubcommandSpec.unknownError(args[0])
	}
	target, e := adminTarget(args[1:])
	if e != nil {
		return nil, name, e
	}
	if args[0] == "grant" {
		fmt.Fprintf(a.Stderr, "Granting %s trusts every process that can read its host's shared key and request this agent name. Human invitations and principal revocation remain human-only. The agent grant allows fleet administration and inventory/audit reads.\n", target)
	}
	r, e := a.invokeRawJSON(ctx, cfg, name, "POST", "/auth/admins/"+url.PathEscape(target)+"/"+args[0], nil)
	return r, name, e
}

func (a *App) runHostEnrollments(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	name := "host enrollments"
	if len(args) == 0 {
		return nil, name, hostEnrollmentsSubcommandSpec.requiredError()
	}
	name += " " + args[0]
	if args[0] != "list" && args[0] != "approve" && args[0] != "deny" {
		return nil, name, hostEnrollmentsSubcommandSpec.unknownError(args[0])
	}
	target := ""
	if args[0] == "list" {
		if len(args) != 1 {
			return nil, name, errnorm.Usage("invalid_args", "list takes no arguments")
		}
	} else {
		var err error
		target, err = adminTarget(args[1:])
		if err != nil {
			return nil, name, err
		}
	}
	result, err := a.invokeRawJSON(ctx, cfg, "host enrollments list", "GET", "/auth/hosts/enrollments/pending", nil)
	if err != nil || args[0] == "list" {
		return result, name, err
	}
	// Resolve the public code using the protected list of pending and approved ceremonies; never fetch poll secrets.
	data := asMap(flattenEnvelopeData(result.Data, false))
	id := ""
	for _, raw := range asSlice(data["enrollments"]) {
		item := asMap(raw)
		if strings.EqualFold(anyString(item["user_code"]), target) || anyString(item["id"]) == target {
			id = anyString(item["id"])
			break
		}
	}
	if id == "" {
		return nil, name, errnorm.New(errnorm.KindRemote, "not_found", "host enrollment awaiting completion not found")
	}
	r, e := a.invokeRawJSON(ctx, cfg, name, "POST", "/auth/hosts/enrollments/"+url.PathEscape(id)+"/"+args[0], nil)
	return r, name, e
}

func (a *App) runHostTokens(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	name := "host tokens"
	if len(args) == 0 {
		return nil, name, hostTokensSubcommandSpec.requiredError()
	}
	name += " " + args[0]
	path := "/auth/hosts/enrollment-tokens"
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return nil, name, errnorm.Usage("invalid_args", "list takes no arguments")
		}
		r, e := a.invokeRawJSON(ctx, cfg, name, "GET", path, nil)
		return r, name, e
	case "get":
		// One token by id, for watching whether a specific grant was redeemed
		// without reading the workspace's whole token history.
		id, e := adminTarget(args[1:])
		if e != nil {
			return nil, name, e
		}
		r, e := a.invokeRawJSON(ctx, cfg, name, "GET", path+"/"+url.PathEscape(id), nil)
		return r, name, e
	case "revoke":
		id, e := adminTarget(args[1:])
		if e != nil {
			return nil, name, e
		}
		r, e := a.invokeRawJSON(ctx, cfg, name, "POST", path+"/"+url.PathEscape(id)+"/revoke", nil)
		return r, name, e
	case "create":
		fs := newSilentFlagSet(name)
		label := fs.String("label", "", "Audit label")
		lifetime := fs.String("expires-in", "1h", "Lifetime from 10m to 24h")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, name, errnorm.Usage("invalid_flags", err.Error())
		}
		if len(fs.Args()) > 0 || strings.TrimSpace(*label) == "" || len(*label) > 120 {
			return nil, name, errnorm.Usage("invalid_args", "--label is required (at most 120 characters); unexpected positional arguments are rejected")
		}
		duration, err := time.ParseDuration(*lifetime)
		if err != nil || duration < 10*time.Minute || duration > 24*time.Hour {
			return nil, name, errnorm.Usage("invalid_args", "--expires-in must be from 10m to 24h")
		}
		r, e := a.invokeRawJSON(ctx, cfg, name, "POST", path, map[string]any{"label": *label, "expires_in_seconds": int64(duration / time.Second)})
		return r, name, e
	default:
		return nil, name, hostTokensSubcommandSpec.unknownError(args[0])
	}
}

func parseAuthAdminList(args []string) (url.Values, error) {
	fs := newSilentFlagSet("auth admins list")
	limit := fs.Int("limit", 50, "Page size")
	cursor := fs.String("cursor", "", "Page cursor")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if fs.NArg() != 0 || *limit < 1 || *limit > 200 {
		return nil, errnorm.Usage("invalid_args", "limit must be 1..200; no positional arguments")
	}
	query := url.Values{"limit": {strconv.Itoa(*limit)}}
	if *cursor != "" {
		query.Set("cursor", *cursor)
	}
	return query, nil
}
