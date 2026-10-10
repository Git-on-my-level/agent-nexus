package app

import (
	"context"
	"net/url"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

var accessRequestsSubcommandSpec = subcommandSpec{command: "auth access-requests", valid: []string{"request", "list", "approve", "deny", "summary"}, examples: []string{"anx auth access-requests request --grant auth-admin --reason 'Enroll a build host'", "anx auth access-requests list", "anx auth access-requests approve <request-id>"}}

func init() {
	for _, verb := range accessRequestsSubcommandSpec.valid {
		flags := []localHelperFlag{}
		if verb == "request" {
			flags = []localHelperFlag{{Name: "--grant <name>", Description: "Named grant; currently auth-admin."}, {Name: "--reason <text>", Description: "Why this agent needs the grant."}}
		}
		summary := "Agents request their own named grants; only humans list or decide access. Retries preserve the original request and decision."
		if verb == "request" {
			summary += " A pending request returns `anx await access-request:<id>`. That command exits 0 when approved and 9 when denied, and the decision wakes the requesting agent the same way an ask answer does."
		}
		localHelperTopics = append(localHelperTopics, localHelperTopic{Path: "auth access-requests " + verb, Summary: summary, Flags: flags, Examples: accessRequestsSubcommandSpec.examples})
	}
}

// Shared parsing makes usage errors win over host enrollment and token lookup.
func parseAccessRequestCommand(args []string) (name, method, path string, body any, err error) {
	name = "auth access-requests"
	if len(args) == 0 {
		err = accessRequestsSubcommandSpec.requiredError()
		return
	}
	verb := args[0]
	name += " " + verb
	switch verb {
	case "request":
		fs := newSilentFlagSet(name)
		grant, reason := fs.String("grant", "", "Named grant"), fs.String("reason", "", "Reason for requesting access")
		if e := fs.Parse(args[1:]); e != nil {
			err = errnorm.Usage("invalid_flags", e.Error())
			return
		}
		if len(fs.Args()) != 0 || strings.TrimSpace(*grant) != "auth-admin" || strings.TrimSpace(*reason) == "" || len([]rune(strings.TrimSpace(*reason))) > 4000 {
			err = errnorm.Usage("invalid_request", "request requires --grant auth-admin and a nonempty --reason of at most 4000 characters")
			return
		}
		method, path, body = "POST", "/auth/access-requests", map[string]any{"grant": strings.TrimSpace(*grant), "reason": strings.TrimSpace(*reason)}
	case "list", "summary":
		if len(args) != 1 {
			err = errnorm.Usage("invalid_args", verb+" takes no arguments")
			return
		}
		method, path = "GET", "/auth/access-requests"
		if verb == "summary" {
			path = "/auth/access/summary"
		}
	case "approve", "deny":
		target, e := adminTarget(args[1:])
		if e != nil {
			err = e
			return
		}
		method, path = "POST", "/auth/access-requests/"+url.PathEscape(target)+"/"+verb
	default:
		err = accessRequestsSubcommandSpec.unknownError(verb)
	}
	return
}

func (a *App) runAccessRequests(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	name, method, path, body, err := parseAccessRequestCommand(args)
	if err != nil {
		return nil, name, err
	}
	result, err := a.invokeRawJSON(ctx, cfg, name, method, path, body)
	return result, name, err
}
