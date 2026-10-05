package app

import (
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/output"
	"agent-nexus-cli/internal/workspaceconfig"
)

// commandSideEffectClass is the CLI's command policy. New command families must
// be added here before they are published in help or meta commands.
func commandSideEffectClass(command string) string {
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
	if parts[0] == "skills" {
		if len(parts) > 1 && (parts[1] == "configure" || parts[1] == "sync" || parts[1] == "adopt") {
			// Conservative: a string does not retain quoted argv boundaries,
			// and repeated bool flags use their last value in the real parser.
			return "local_operational_write"
		}
		return "read_only"
	}
	if parts[0] == "config" && len(parts) > 1 {
		if parts[1] == "show" || parts[1] == "workspaces" {
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
	if parts[0] == "adapters" {
		if len(parts) > 1 && parts[1] == "list" {
			return "read_only"
		}
		return "remote_coordination_write"
	}
	if parts[0] == "api" {
		return "external_side_effect"
	}
	if command == "workspace dashboard set" {
		return "remote_coordination_write"
	}
	if parts[0] == "overview" {
		return "read_only"
	}
	if parts[0] == "move" {
		return "remote_coordination_write"
	}
	if parts[0] == "version" || parts[0] == "doctor" || parts[0] == "workspace" || parts[0] == "read" || parts[0] == "url" || parts[0] == "concepts" || parts[0] == "primitives" || parts[0] == "provenance" {
		return "read_only"
	}
	if parts[0] == "orient" || parts[0] == "await" {
		return "read_only"
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
	if parts[0] == "bridge" && len(parts) > 1 {
		switch parts[1] {
		case "install", "stop":
			return "local_operational_write"
		case "start":
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

func action(label string, argv ...string) output.NextAction {
	_, remaining, _, err := parseGlobalFlags(argv[1:])
	if err != nil {
		remaining = argv[1:]
	}
	class := commandSideEffectClass(strings.Join(remaining, " "))
	return output.NextAction{Label: label, Argv: argv, Mutates: class != "read_only", SideEffectClass: class}
}

// deriveNextActions only offers exact runnable commands with concrete targets.
// Callers can add state-specific derivations without modifying the renderer.
func deriveNextActions(command string, argv []string, value any) []output.NextAction {
	root, _ := value.(map[string]any)
	if root == nil {
		return []output.NextAction{}
	}
	var actions []output.NextAction
	if strings.HasPrefix(command, "update") {
		if command != "update status" {
			actions = append(actions, action("Inspect update status", "anx", "update", "status"))
		}
		actions = append(actions, action("Inspect managed skills", "anx", "skills", "status"))
		if command == "update status" && asBool(root["managed"]) {
			actions = append(actions, action("Check and update CLI", "anx", "update", "now"))
		}
	}
	if command == "host enroll --plan" {
		actions = append(actions, action("Enroll host", "anx", "host", "enroll"))
	}
	if command == "sessions register" {
		if session := asMap(root["session"]); anyString(session["session_id"]) != "" {
			actions = append(actions, action("Read session", "anx", "sessions", "get", anyString(session["session_id"])))
		}
	}
	if command == "host enroll" {
		prefix := []string{"anx"}
		if dir := anyString(root["config_dir"]); dir != "" {
			prefix = append(prefix, "--config-dir", dir)
		}
		if alias := anyString(root["workspace_alias"]); alias != "" {
			actions = append(actions, action("Make workspace default", append(append([]string{}, prefix...), "config", "use", alias)...))
			actions = append(actions, action("Check host", append(append([]string{}, prefix...), "--workspace", alias, "host", "status")...))
		} else {
			actions = append(actions, action("Check host", append(append([]string{}, prefix...), "host", "status")...))
		}
		actions = append(actions, action("Sync ANX skills", append(append([]string{}, prefix...), "skills", "sync")...))
	}
	if command == "host token" {
		actions = append(actions, action("Check identity", "anx", "auth", "whoami"))
	}
	if command == "host bridge check-in" {
		actions = append(actions, action("Check host", "anx", "host", "status"))
	}
	if command == "runs ingest" {
		if run := asMap(root["run"]); anyString(run["id"]) != "" {
			actions = append(actions, action("Read run", "anx", "runs", "get", anyString(run["id"])))
		}
	}
	if (command == "ask" || command == "review" || command == "escalate" || command == "work block") && anyString(root["ask_id"]) != "" {
		actions = append(actions, action("Wait for answer", "anx", "await", anyString(root["ask_id"])))
	}
	if command == "await" {
		subject := anyString(root["subject_ref"])
		if strings.HasPrefix(subject, "card:") {
			switch anyString(root["outcome"]) {
			case "answered", "approved":
				if answer := anyString(root["answer"]); answer != "" {
					label := "Record answer"
					if anyString(root["outcome"]) == "approved" {
						label = "Record approval"
					}
					actions = append(actions, action(label, "anx", "work", "note", answer, subject))
				}
			case "acknowledged":
				actions = append(actions, action("Review acknowledged work", "anx", "work", "get", subject))
			}
		} else {
			actions = append(actions, action("Orient after response", "anx", "orient"))
		}
	}
	if command == "orient" {
		for _, raw := range asSlice(root["next"]) {
			argv := stringList(raw)
			if len(argv) >= 2 && argv[0] == "anx" && !strings.Contains(strings.Join(argv, " "), "<") {
				actions = append(actions, action("Continue work", argv...))
			}
		}
	}
	if cursor := firstNonEmpty(anyString(root["next_cursor"]), anyString(root["cursor_next"])); cursor != "" && root["has_more"] != false {
		next := append([]string{"anx"}, argv...)
		for i := 0; i < len(next); i++ {
			if next[i] == "--cursor" && i+1 < len(next) {
				next = append(next[:i], next[i+2:]...)
				break
			}
			if strings.HasPrefix(next[i], "--cursor=") {
				next = append(next[:i], next[i+1:]...)
				break
			}
		}
		next = append(next, "--cursor", cursor)
		actions = append(actions, action("Next page", next...))
	}
	if command == "cards.create" || command == "cards create" {
		card := asMap(root["card"])
		ref := firstNonEmpty(anyString(card["ref"]), anyString(root["card_ref"]))
		if ref == "" && anyString(card["id"]) != "" {
			ref = "card:" + anyString(card["id"])
		}
		if ref != "" {
			actions = append(actions, action("View card", "anx", "cards", "get", ref))
			actions = append(actions, action("Assign card", "anx", "cards", "assign", ref, "--assignee-ref", "me"))
		}
	}
	parts := strings.Fields(command)
	if len(parts) == 2 && (parts[1] == "create" || parts[1] == "patch" || parts[1] == "move" || parts[1] == "assign") {
		kind := parts[0]
		field := map[string]string{"cards": "card", "docs": "document", "topics": "topic", "boards": "board", "work": "work"}[kind]
		if field != "" {
			record := asMap(root[field])
			ref := firstNonEmpty(anyString(record["ref"]), anyString(root[field+"_ref"]))
			if ref == "" && anyString(record["id"]) != "" {
				ref = map[string]string{"cards": "card:", "docs": "doc:", "topics": "topic:", "boards": "board:", "work": "card:"}[kind] + anyString(record["id"])
			}
			if ref != "" && command != "cards create" {
				actions = append(actions, action("View result", "anx", kind, "get", ref))
			}
		}
	}
	if strings.Contains(command, "propose") || strings.HasSuffix(command, "revise") {
		if id := firstNonEmpty(anyString(root["proposal_id"]), anyString(root["draft_id"])); id != "" {
			if command == "docs revise" {
				actions = append(actions, action("Apply proposal", "anx", "docs", "revise", "--apply", "--proposal-id", id))
			}
		}
	}
	return actions
}

func doctorVersionWarnings(value any) ([]output.Warning, []output.NextAction) {
	root, _ := value.(map[string]any)
	var warnings []output.Warning
	var repairs []output.NextAction
	for _, check := range doctorChecksOf(root["checks"]) {
		if check.Status != "warn" || check.Name != "cli_version" {
			continue
		}
		warnings = append(warnings, output.Warning{Code: "cli_version", Message: check.Message})
		if version := strings.TrimSpace(check.RecommendedCLIVersion); version != "" {
			repairs = append(repairs, action("Update CLI", "anx", "update", "--version", version))
		}
	}
	if warnings == nil {
		return []output.Warning{}, []output.NextAction{}
	}
	return warnings, repairs
}

func doctorChecksOf(raw any) []doctorCheck {
	if typed, ok := raw.([]doctorCheck); ok {
		return typed
	}
	items := asSlice(raw)
	if len(items) == 0 {
		return nil
	}
	out := make([]doctorCheck, 0, len(items))
	for _, item := range items {
		check := asMap(item)
		out = append(out, doctorCheck{
			Name:                  anyString(check["name"]),
			Status:                anyString(check["status"]),
			Message:               anyString(check["message"]),
			RecommendedCLIVersion: anyString(check["recommended_cli_version"]),
		})
	}
	return out
}

func resultWarnings(command string, argv []string, value any) ([]output.Warning, []output.NextAction) {
	if command == "doctor" {
		return doctorVersionWarnings(value)
	}
	parts := strings.Fields(command)
	if len(parts) == 0 || parts[len(parts)-1] != "list" && parts[len(parts)-1] != "search" {
		return []output.Warning{}, []output.NextAction{}
	}
	root, _ := value.(map[string]any)
	if root == nil {
		return []output.Warning{}, []output.NextAction{}
	}
	owner := ""
	actorFlags := map[string]bool{"--owner": true, "--owner-ref": true, "--actor-id": true, "--assignee-ref": true}
	for i, tok := range argv {
		if actorFlags[tok] && i+1 < len(argv) {
			owner = argv[i+1]
		}
		for flag := range actorFlags {
			if strings.HasPrefix(tok, flag+"=") {
				owner = strings.TrimPrefix(tok, flag+"=")
			}
		}
	}
	if owner == "" {
		return []output.Warning{}, []output.NextAction{}
	}
	foundList := false
	for _, key := range []string{"work", "items", "cards", "events", "topics", "documents", "actors"} {
		if items, ok := root[key]; ok {
			foundList = true
			if len(asSlice(items)) > 0 {
				return []output.Warning{}, []output.NextAction{}
			}
		}
	}
	if !foundList {
		return []output.Warning{}, []output.NextAction{}
	}
	unfiltered := []string{"anx"}
	for i := 0; i < len(argv); i++ {
		if actorFlags[argv[i]] && i+1 < len(argv) {
			i++
			continue
		}
		name, _, _, isFlag := parseLongOptionToken(argv[i])
		if isFlag && actorFlags["--"+name] {
			continue
		}
		unfiltered = append(unfiltered, argv[i])
	}
	return []output.Warning{{Code: "empty_actor_filter", Message: "No records matched this actor filter; verify the actor reference."}}, []output.NextAction{
		action("List without actor filter", unfiltered...),
		action("Find actor", "anx", "debug", "actors", "list", "--q", strings.TrimPrefix(owner, "actor:")),
	}
}

func normalizeActorArgs(args []string, cfg config.Resolved) ([]string, error) {
	out := append([]string(nil), args...)
	for i := range out {
		name, value, inline, flag := parseLongOptionToken(out[i])
		if !flag || (name != "owner" && name != "owner-ref" && name != "assignee-ref") {
			continue
		}
		index := i
		if !inline {
			index++
			if index >= len(out) {
				continue
			}
			value = out[index]
		}
		if value == "me" {
			if cfg.ActorID == "" {
				return nil, errnorm.Usage("actor_required", "me requires a resolved derived agent; pass --as <name>")
			}
			value = cfg.ActorID
		}
		if value != "" && !strings.Contains(value, ":") {
			value = "actor:" + value
		}
		if inline {
			out[i] = "--" + name + "=" + value
		} else {
			out[index] = value
		}
	}
	return out, nil
}

func deriveErrorActions(command string, err *errnorm.Error) []output.NextAction {
	if err == nil {
		return []output.NextAction{}
	}
	if err.Code == "not_found" || strings.HasSuffix(err.Code, "_not_found") {
		parts := strings.Fields(command)
		if len(parts) > 0 {
			switch parts[0] {
			case "docs":
				query := quotedErrorWord(err.Message)
				if details, ok := err.Details.(map[string]any); ok {
					query = firstNonEmpty(anyString(details["requested_ref"]), anyString(details["ref"]), query)
				}
				if query != "" {
					return []output.NextAction{action("Search documents", "anx", "docs", "search", query)}
				}
				return []output.NextAction{action("Find reference", "anx", "docs", "list")}
			case "cards", "topics", "boards", "work", "artifacts":
				return []output.NextAction{action("Find reference", "anx", parts[0], "list")}
			}
		}
	}
	switch err.Code {
	case "workspace_ambiguous":
		prefix := []string{"anx"}
		if details, ok := err.Details.(map[string]any); ok {
			if dir := anyString(details["config_dir"]); dir != "" {
				prefix = append(prefix, "--config-dir", dir)
			}
		}
		actions := []output.NextAction{action("List workspaces", append(append([]string{}, prefix...), "config", "workspaces")...)}
		if details, ok := err.Details.(map[string]any); ok {
			if workspaces, ok := details["workspaces"].([]workspaceconfig.Workspace); ok {
				for _, ws := range workspaces {
					if ws.Enrolled {
						actions = append(actions, action("Use workspace "+ws.Alias, append(append([]string{}, prefix...), "config", "use", ws.Alias)...))
					}
				}
			}
		}
		return actions
	case "workspace_unknown":
		return []output.NextAction{action("List workspaces", "anx", "config", "workspaces")}
	case "archived_report_document":
		if details, ok := err.Details.(map[string]any); ok {
			if ref := strings.TrimSpace(anyString(details["document_ref"])); ref != "" {
				return []output.NextAction{action("Unarchive document", "anx", "docs", "unarchive", ref)}
			}
		}
	case "identity_unresolved":
		return []output.NextAction{action("Select agent", "anx", "--as", "codex", "auth", "whoami")}
	case "host_not_enrolled":
		return []output.NextAction{action("Enroll host", "anx", "host", "enroll")}
	case "no_current_task":
		return []output.NextAction{action("Orient", "anx", "orient"), action("Find work", "anx", "work", "list")}
	case "timeout":
		if details, ok := err.Details.(map[string]any); ok {
			if target := anyString(details["target"]); target != "" {
				return []output.NextAction{action("Keep waiting", "anx", "await", target)}
			}
		}
	case "rejected":
		if details, ok := err.Details.(map[string]any); ok {
			if subject := anyString(details["subject_ref"]); strings.HasPrefix(subject, "card:") {
				return []output.NextAction{action("Revise rejected work", "anx", "work", "get", subject)}
			}
		}
		return []output.NextAction{action("Orient after rejection", "anx", "orient")}
	case "invalid_update_policy", "invalid_update_version", "invalid_update_args", "invalid_update_flags":
		return []output.NextAction{action("Inspect update contract", "anx", "help", "update")}
	case "unmanaged_install", "update_binary_changed", "update_locked", "update_probe_failed", "checksum_mismatch", "update_rollback_failed", "update_rollback_record_failed":
		return []output.NextAction{action("Inspect update status", "anx", "update", "status"), action("Inspect update contract", "anx", "help", "update")}
	case "update_skills_failed":
		return []output.NextAction{action("Inspect skills", "anx", "skills", "status"), action("Sync managed skills", "anx", "skills", "sync")}
	case "cli_outdated":
		if details, ok := err.Details.(map[string]any); ok {
			if version := strings.TrimSpace(anyString(details["recommended_cli_version"])); version != "" {
				return []output.NextAction{action("Update CLI", "anx", "update", "--version", version)}
			}
		}
		return []output.NextAction{action("Update CLI", "anx", "update")}
	case "unknown_command", "unknown_subcommand":
		parts := strings.Fields(command)
		unknown := quotedErrorWord(err.Message)
		if err.Code == "unknown_command" {
			candidates := make([]string, 0, len(preflightRootCommands()))
			for candidate := range preflightRootCommands() {
				if !strings.HasPrefix(candidate, "-") {
					candidates = append(candidates, candidate)
				}
			}
			if closest := closestCommand(unknown, candidates); closest != "" {
				return []output.NextAction{action("Did you mean", "anx", closest)}
			}
		}
		if len(parts) > 0 {
			candidates := validSubcommands(strings.Join(parts, " "))
			if closest := closestCommand(unknown, candidates); closest != "" {
				return []output.NextAction{action("Did you mean", append(append([]string{"anx"}, parts...), closest)...)}
			}
			if err.Code == "unknown_command" {
				return []output.NextAction{action("Show available commands", "anx", "help")}
			}
			return []output.NextAction{action("Show available commands", append([]string{"anx", "help"}, parts...)...)}
		}
		return []output.NextAction{action("Show commands", "anx", "help")}
	}
	return []output.NextAction{}
}

func quotedErrorWord(message string) string {
	start := strings.IndexByte(message, '"')
	if start < 0 {
		return ""
	}
	end := strings.IndexByte(message[start+1:], '"')
	if end < 0 {
		return ""
	}
	return message[start+1 : start+1+end]
}

func closestCommand(input string, candidates []string) string {
	best, distance := "", 3
	for _, candidate := range candidates {
		d := editDistance(input, candidate)
		if d < distance || d == distance && candidate < best {
			best, distance = candidate, d
		}
	}
	return best
}

func editDistance(a, b string) int {
	previous := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current := make([]int, len(b)+1)
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			current[j] = min(current[j-1]+1, previous[j]+1, previous[j-1]+cost)
		}
		previous = current
	}
	return previous[len(b)]
}

func validSubcommands(group string) []string {
	switch group {
	case "cards":
		return cardsSubcommandSpec.valid
	case "docs":
		return docsSubcommandSpec.valid
	case "topics":
		return topicsSubcommandSpec.valid
	case "boards":
		return boardsSubcommandSpec.valid
	case "notifications":
		return notificationsSubcommandSpec.valid
	case "inbox":
		return inboxSubcommandSpec.valid
	case "auth":
		return authSubcommandSpec.valid
	case "config":
		return configSubcommandSpec.valid
	case "debug":
		return []string{"threads", "events", "ref-edges", "derived", "actors", "inbox", "meta"}
	case "debug threads":
		return threadsSubcommandSpec.valid
	case "debug events":
		return eventsSubcommandSpec.valid
	case "debug inbox":
		return inboxSubcommandSpec.valid
	case "debug actors":
		return actorsSubcommandSpec.valid
	case "debug derived":
		return derivedSubcommandSpec.valid
	case "debug meta":
		return metaSubcommandSpec.valid
	case "auth invites":
		return authInvitesSubcommandSpec.valid
	case "auth bootstrap":
		return authBootstrapSubcommandSpec.valid
	case "auth principals":
		return authPrincipalsSubcommandSpec.valid
	case "auth audit":
		return authAuditSubcommandSpec.valid
	}
	return nil
}

func sanitizeEnvelopeResult(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			switch strings.ToLower(key) {
			case "access_token", "refresh_token", "private_key", "private_key_path", "authorization":
				continue
			}
			out[key] = sanitizeEnvelopeResult(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = sanitizeEnvelopeResult(item)
		}
		return out
	default:
		return value
	}
}
