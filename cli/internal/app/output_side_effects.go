// Command side-effect classification feeding the envelope's mutates and
// side_effect_class action metadata.

package app

import (
	"strings"
)

// commandSideEffectClass is the CLI's command policy. New command families must
// be added here before they are published in help or meta commands.
func commandSideEffectClass(command string) string {

	if command == "ask presentation" {
		return "read_only"
	}
	if command == "ask revise" || command == "ask retire" {
		return "remote_coordination_write"
	}
	if command == "git sync" || command == "hook" {
		return "remote_coordination_write"
	}
	if command == "git enable" || command == "git disable" || command == "integrations bind" || command == "integrations map" {
		return "local_operational_write"
	}
	if command == "git status" || command == "git binding export" {
		return "read_only"
	}
	if isLoopWork(command) {
		return "remote_coordination_write"
	}
	if command == "pm install" || command == "pm uninstall" {
		return "local_operational_write"
	}
	if command == "pm card" || command == "pm context" || command == "pm status" || command == "pm presence" {
		return "read_only"
	}
	if command == "work move" || command == "mv" || command == "work group" || command == "work fold" || command == "work unfold" || strings.HasPrefix(command, "initiative ") {
		return "remote_coordination_write"
	}
	if command == "work tree" || command == "tree" || command == "work ls" || command == "where" || command == "work ungrouped" {
		return "read_only"
	}
	if command == "refs resolve" {
		return "read_only"
	}
	if strings.HasPrefix(command, "plan step ") || command == "plan set" {
		return "remote_coordination_write"
	}
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return "read_only"
	}
	if parts[0] == "help" || parts[0] == "debug" && len(parts) > 1 && parts[1] == "meta" {
		return "read_only"
	}
	if parts[0] == "integrations" {
		if len(parts) > 1 {
			switch parts[1] {
			case "enable", "disable":
				return "local_operational_write"
			case "run":
				return "remote_coordination_write"
			}
		}
		return "read_only"
	}
	if parts[0] == "skills" {
		if len(parts) > 1 && (parts[1] == "configure" || parts[1] == "sync" || parts[1] == "adopt") {
			// Conservative: a string does not retain quoted argv boundaries,
			// and repeated bool flags use their last value in the real parser.
			return "local_operational_write"
		}
		return "read_only"
	}
	if parts[0] == "config" && len(parts) > 1 {
		if parts[1] == "show" || parts[1] == "workspaces" || (parts[1] == "forge" && len(parts) > 2 && parts[2] == "list") {
			return "read_only"
		}
		return "local_operational_write"
	}
	if parts[0] == "series" {
		if len(parts) > 1 && parts[1] == "push" {
			if strings.Contains(command, "--from-command") {
				return "external_side_effect"
			}
			return "remote_coordination_write"
		}
		return "read_only"
	}
	if parts[0] == "lease" {
		if len(parts) > 1 && parts[1] == "show" {
			return "read_only"
		}
		return "remote_coordination_write"
	}
	if parts[0] == "deliveries" {
		if len(parts) > 1 && parts[1] == "list" {
			return "read_only"
		}
		return "remote_coordination_write"
	}
	if command == "notifications settings get" {
		return "read_only"
	}
	if command == "notifications settings set" {
		return "remote_coordination_write"
	}
	if parts[0] == "adapters" {
		if len(parts) > 1 && parts[1] == "list" {
			return "read_only"
		}
		return "remote_coordination_write"
	}
	if parts[0] == "api" {
		return "external_side_effect"
	}
	if strings.HasPrefix(command, "attention ") {
		if command == "attention place" || command == "attention agent set" || command == "attention feedback create" || command == "attention visits" {
			return "remote_coordination_write"
		}
		return "read_only"
	}
	if command == "workspace dashboard set" || command == "workspace dashboard pins set" {
		return "remote_coordination_write"
	}
	if parts[0] == "overview" {
		return "read_only"
	}
	if parts[0] == "move" {
		return "remote_coordination_write"
	}
	if parts[0] == "version" || parts[0] == "doctor" || parts[0] == "workspace" || parts[0] == "read" || parts[0] == "url" || parts[0] == "link" || parts[0] == "concepts" || parts[0] == "primitives" || parts[0] == "provenance" {
		return "read_only"
	}
	if parts[0] == "await" {
		return "remote_coordination_write"
	}
	if command == "host bridge-status" {
		return "read_only"
	}
	if parts[0] == "orient" || parts[0] == "context" {
		return "read_only"
	}
	if parts[0] == "note" || parts[0] == "done" {
		return "remote_coordination_write"
	}
	if command == "auth access-requests request" || command == "auth access-requests approve" || command == "auth access-requests deny" {
		return "remote_coordination_write"
	}
	if command == "host revoke" || command == "host enrollments approve" || command == "host enrollments deny" || command == "host tokens create" || command == "host tokens revoke" || command == "auth admins grant" || command == "auth admins revoke" || parts[0] == "inbox" && len(parts) > 1 && parts[1] == "read" {
		return "remote_coordination_write"
	}
	if command == "host token" {
		return "local_operational_write"
	}
	if command == "host enroll" || command == "host exclude" || command == "host include" || command == "runs ingest" {
		return "remote_coordination_write"
	}
	if strings.HasPrefix(command, "host bridge ") {
		return "remote_coordination_write"
	}
	if command == "host enroll --plan" {
		return "read_only"
	}
	if command == "host discover" {
		return "read_only"
	}
	if command == "sessions register" || command == "work participants register" {
		return "remote_coordination_write"
	}
	if parts[0] == "update" {
		if command == "update status" {
			return "read_only"
		}
		// Bool options use their final parsed value; --check=false still installs.
		if options, err := parseUpdateOptions(parts[1:]); err == nil && options.check {
			return "read_only"
		}
		return "local_operational_write"
	}
	if parts[0] == "sync" && len(parts) > 1 {
		if parts[1] == "status" {
			return "read_only"
		}
		return "local_operational_write"
	}
	if parts[0] == "bridge" && len(parts) > 1 {
		switch parts[1] {
		case "install", "add", "remove", "stop":
			return "local_operational_write"
		case "start", "run":
			return "external_side_effect"
		case "doctor":
			return "remote_coordination_write"
		case "status":
			return "read_only"
		}
	}
	if parts[0] == "import" {
		if len(parts) > 1 && parts[1] == "apply" {
			return "remote_coordination_write"
		}
		return "local_operational_write"
	}
	if parts[0] == "draft" && len(parts) > 1 && parts[1] == "commit" {
		return "remote_coordination_write"
	}
	for _, verb := range parts[1:] {
		if strings.HasPrefix(verb, "-") {
			break
		}
		switch verb {
		case "list", "get", "show", "search", "history", "context", "workspace", "summary", "status", "token-status", "whoami", "version", "help", "doctor", "capabilities", "inspect", "timeline", "messages", "content", "read", "walk", "check", "freshness", "explain", "validate", "render":
			return "read_only"
		case "dispatch", "exec":
			return "external_side_effect"
		case "use", "unset", "default", "install", "update", "start", "stop", "restart", "commit", "discard", "create", "assign", "move", "patch", "revise", "apply", "request", "submit", "delete", "archive", "trash", "restore", "purge":
			if parts[0] == "config" || parts[0] == "draft" || parts[0] == "install" || parts[0] == "bridge" {
				return "local_operational_write"
			}
			return "remote_coordination_write"
		}
	}
	return "remote_coordination_write"
}
