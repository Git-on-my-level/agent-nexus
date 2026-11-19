package app

import (
	"context"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func (a *App) runAgentHandoffCommand(ctx context.Context, args []string, cfg config.Resolved) (string, *commandResult, error) {
	parsed, err := parseAgentHandoffCommand(args)
	if err != nil {
		return parsed.name, nil, err
	}
	result, err := a.invokeTypedJSON(ctx, cfg, parsed.name, parsed.id, parsed.params, nil, parsed.body)
	return parsed.name, result, err
}

type agentHandoffCommand struct {
	name, id string
	params   map[string]string
	body     map[string]any
}

func parseAgentHandoffCommand(args []string) (agentHandoffCommand, error) {
	const group = "agent handoffs"
	if len(args) == 0 {
		return agentHandoffCommand{name: group}, errnorm.Usage("invalid_args", "use anx agent handoffs send|get|ack")
	}
	action, args := args[0], args[1:]
	parsed := agentHandoffCommand{name: group + " " + action, id: "agent.handoffs." + action}
	fs := newSilentFlagSet(parsed.name)
	switch action {
	case "send":
		recipient := fs.String("to", "", "Recipient actor ID")
		deadline := fs.String("expires-at", "", "Pickup deadline (RFC3339)")
		var refs trackedStrings
		fs.Var(&refs, "ref", "Canonical reference (repeatable)")
		if err := fs.Parse(args); err != nil {
			return parsed, errnorm.Usage("invalid_flags", err.Error())
		}
		if len(fs.Args()) != 0 || *recipient == "" || *deadline == "" || len(refs.values) == 0 {
			return parsed, errnorm.Usage("invalid_args", "use --to <actor-id> --expires-at <RFC3339> --ref <ref>")
		}
		parsed.body = map[string]any{"recipient_actor_id": strings.TrimPrefix(*recipient, "actor:"), "expires_at": *deadline, "refs": refs.values}
	case "get", "ack":
		if len(args) == 0 || strings.HasPrefix(args[0], "-") || strings.ContainsAny(args[0], "/?#") {
			return parsed, errnorm.Usage("invalid_args", "provide a handoff ID")
		}
		parsed.params = map[string]string{"handoff_id": args[0]}
		args = args[1:]
		if action == "get" {
			if len(args) != 0 {
				return parsed, errnorm.Usage("invalid_args", "provide one handoff ID")
			}
			break
		}
		outcome := fs.String("outcome", "", "refs_verified or ref_blocked")
		blockedRef := fs.String("blocked-ref", "", "First inaccessible ref")
		if err := fs.Parse(args); err != nil {
			return parsed, errnorm.Usage("invalid_flags", err.Error())
		}
		if len(fs.Args()) != 0 || !(*outcome == "refs_verified" && *blockedRef == "" || *outcome == "ref_blocked" && *blockedRef != "") {
			return parsed, errnorm.Usage("invalid_args", "use --outcome refs_verified or --outcome ref_blocked --blocked-ref <ref>")
		}
		parsed.body = map[string]any{"outcome": *outcome}
		if *blockedRef != "" {
			parsed.body["blocked_ref"] = *blockedRef
		}
	default:
		return agentHandoffCommand{name: group}, errnorm.Usage("unknown_subcommand", "use anx agent handoffs send|get|ack")
	}
	return parsed, nil
}

func helpTopicWithAgentHandoffs(topic string) (string, bool) {
	switch strings.TrimSpace(topic) {
	case "agent handoffs", "agent handoffs send", "agent handoffs get", "agent handoffs ack":
		return `Addressed agent handoffs

  anx agent handoffs send --to <actor-id> --expires-at <RFC3339> --ref <ref> [--ref <ref> ...]
  anx agent handoffs get <handoff-id>
  anx agent handoffs ack <handoff-id> --outcome refs_verified
  anx agent handoffs ack <handoff-id> --outcome ref_blocked --blocked-ref <ref>

Address a registered workspace agent and attach up to eight ANX event,
Multica issue, or GitHub issue, pull request, or full commit refs.
Replaying the same send returns the original handoff. The recipient checks
the refs using authorized source tools, then acknowledges pickup. Read bridge
delivery and recipient pickup separately with get.
`, true
	default:
		return helpTopicTextRaw(topic)
	}
}
