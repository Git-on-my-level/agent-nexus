package app

import (
	"strings"

	"agent-nexus-cli/internal/registry"
)

// Keep human requirements visible even for commands with custom help. This is
// presentation only; core always decides whether a principal may act.
func commandRequiresHuman(cmd registry.Command) bool {
	for _, code := range cmd.ErrorCodes {
		if code == "human_required" {
			return true
		}
	}
	// Older contracts omit human_required from several human-action schemas.
	// Keep their help explicit without changing authentication or retry behavior.
	if strings.HasPrefix(cmd.Path, "/secrets") || strings.HasPrefix(cmd.Path, "/auth/invites") || strings.HasSuffix(cmd.Path, "/purge") {
		return true
	}
	switch cmd.Path {
	case "/readyz":
		return cmd.Method == "GET"
	case "/notification-settings":
		return cmd.Method == "PUT"
	case "/hosts/{host_id}":
		return cmd.Method == "PATCH"
	case "/curation/ungroup", "/derived/rebuild", "/ops/blob-usage/rebuild":
		return cmd.Method == "POST"
	case "/pm/bindings":
		return true
	case "/pm/decisions":
		return cmd.Method == "POST"
	case "/pm/decisions/{decision_id}/answer", "/pm/decisions/{decision_id}/dispatch", "/pm/actions/{action_id}/acknowledge", "/pm/actions/{action_id}/reconcile":
		return cmd.Method == "POST"
	}
	return false
}

func hostExclusionHelp(topic string) (string, bool) {
	if topic != "host exclude" && topic != "host include" {
		return "", false
	}
	return "Usage: anx " + topic + " <agent-name>\n\nChange which agent names this host may derive using its host-key proof. Servers that reserve host edits for humans need a signed-in browser session: open Access > Hosts and edit this host there. Self-hosted servers may accept the host proof directly.\n", true
}

func humanSessionHelp(topic string, text string) string {
	meta, err := registry.LoadEmbedded()
	if err != nil {
		return text
	}
	cmd, found := commandByCLIPath(meta.Commands, mapRuntimePathToRegistryPath(topic))
	if found && commandRequiresHuman(cmd) {
		text = strings.TrimRight(text, "\n") + "\n\nHuman authorization: servers that gate this action on a signed-in browser session require the human to open the workspace and perform it there. Agent enrollment cannot supply that session. Core still enforces its own principal permissions.\n"
	}
	return text
}
