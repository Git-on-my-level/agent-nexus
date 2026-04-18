package app

import (
	"fmt"
	"strings"
	"unicode"

	"agent-nexus-cli/internal/errnorm"
)

type subcommandSpec struct {
	command  string
	valid    []string
	examples []string
	aliases  map[string]string
}

var apiSubcommandSpec = subcommandSpec{
	command:  "api",
	valid:    []string{"call"},
	examples: []string{"anx api call --method GET --path /readyz"},
}

var bridgeSubcommandSpec = subcommandSpec{
	command:  "bridge",
	valid:    []string{"install", "import-auth", "init-config", "start", "stop", "restart", "status", "logs", "workspace-id", "doctor"},
	examples: []string{"anx bridge install", "anx bridge import-auth --config ./agent.toml --from-profile agent-a", "anx bridge init-config --kind hermes --output ./agent.toml --workspace-id ws_main --workspace-path /absolute/path/to/hermes/workspace", "anx bridge workspace-id --handle hermes", "anx bridge start --config ./agent.toml", "anx bridge status --config ./agent.toml"},
}

var notificationsSubcommandSpec = subcommandSpec{
	command:  "notifications",
	valid:    []string{"list", "read", "dismiss"},
	examples: []string{"anx notifications list --status unread", "anx notifications read --wakeup-id wake_123", "anx notifications dismiss --wakeup-id wake_123"},
	aliases: map[string]string{
		"ls": "list",
	},
}

var configSubcommandSpec = subcommandSpec{
	command: "config",
	valid:   []string{"use", "show", "unset"},
	examples: []string{
		"anx config use agent-a",
		"anx config show",
		"anx config show --json   # optional: JSON envelope for scripts",
		"anx config unset",
	},
}

var authSubcommandSpec = subcommandSpec{
	command: "auth",
	valid:   []string{"register", "whoami", "list", "default", "update-username", "rotate", "revoke", "token-status", "invites", "bootstrap", "principals", "audit"},
	examples: []string{
		"anx auth register --username <username> --bootstrap-token <token>",
		"anx auth register --username <username> --invite-token <token>",
		"anx auth whoami",
		"anx auth list",
		"anx auth default <profile>",
		"anx auth invites list",
		"anx auth invites create --kind agent",
		"anx auth bootstrap status",
		"anx auth principals list",
		"anx auth principals revoke --agent-id <agent-id>",
		"anx auth principals revoke --agent-id <agent-id> --allow-human-lockout --human-lockout-reason 'incident recovery'",
		"anx auth audit list",
	},
	aliases: map[string]string{
		"status":   "token-status",
		"profiles": "list",
		"ls":       "list",
	},
}

var authInvitesSubcommandSpec = subcommandSpec{
	command:  "auth invites",
	valid:    []string{"list", "create", "revoke"},
	examples: []string{"anx auth invites list", "anx auth invites create --kind agent", "anx auth invites revoke --invite-id <id>"},
	aliases: map[string]string{
		"ls": "list",
	},
}

var authBootstrapSubcommandSpec = subcommandSpec{
	command:  "auth bootstrap",
	valid:    []string{"status"},
	examples: []string{"anx auth bootstrap status"},
}

var authPrincipalsSubcommandSpec = subcommandSpec{
	command:  "auth principals",
	valid:    []string{"list", "revoke"},
	examples: []string{"anx auth principals list", "anx auth principals list --handles-only", "anx auth principals list --taggable", "anx auth principals list --limit 20", "anx auth principals revoke --agent-id <agent-id>", "anx auth principals revoke --agent-id <agent-id> --allow-human-lockout --human-lockout-reason 'incident recovery'"},
	aliases: map[string]string{
		"ls": "list",
	},
}

var authAuditSubcommandSpec = subcommandSpec{
	command:  "auth audit",
	valid:    []string{"list"},
	examples: []string{"anx auth audit list", "anx auth audit list --limit 50"},
	aliases: map[string]string{
		"ls": "list",
	},
}

var actorsSubcommandSpec = subcommandSpec{
	command:  "actors",
	valid:    []string{"list", "register"},
	examples: []string{"anx actors list --q bot --limit 50", "anx actors register --id bot-1 --display-name \"Bot 1\" --created-at 2026-03-04T10:00:00Z"},
	aliases: map[string]string{
		"ls": "list",
	},
}

var metaSubcommandSpec = subcommandSpec{
	command:  "meta",
	valid:    []string{"health", "readyz", "version", "handshake", "ops", "commands", "command", "concepts", "concept", "docs", "doc", "skill"},
	examples: []string{"anx meta health", "anx meta readyz", "anx meta commands", "anx meta command --command-id threads.list", "anx meta docs", "anx meta doc agent-guide", "anx meta skill cursor --write-dir ~/.cursor/skills/anx-cli-onboard"},
}

var metaOpsSubcommandSpec = subcommandSpec{
	command:  "meta ops",
	valid:    []string{"health"},
	examples: []string{"anx meta ops health"},
}

var draftSubcommandSpec = subcommandSpec{
	command:  "draft",
	valid:    []string{"create", "list", "commit", "discard"},
	examples: []string{"anx draft list", "anx draft commit --draft-id <draft-id>"},
}

var provenanceSubcommandSpec = subcommandSpec{
	command:  "provenance",
	valid:    []string{"walk"},
	examples: []string{"anx provenance walk --from event:<event-id> --depth 2"},
}

var threadsSubcommandSpec = subcommandSpec{
	command:  "threads",
	valid:    []string{"list", "get", "timeline", "context", "inspect", "workspace", "review", "recommendations"},
	examples: []string{"anx topics workspace --topic-id <topic-id>", "anx threads list --status active", "anx threads workspace --status active --type initiative --full-id"},
	aliases: map[string]string{
		"ls": "list",
	},
}

var artifactsSubcommandSpec = subcommandSpec{
	command:  "artifacts",
	valid:    []string{"list", "get", "create", "content", "inspect", "archive", "unarchive", "trash", "restore", "purge"},
	examples: []string{"anx artifacts list --kind packet", "anx artifacts inspect --artifact-id <artifact-id>"},
	aliases: map[string]string{
		"ls":   "list",
		"show": "inspect",
	},
}

var boardsSubcommandSpec = subcommandSpec{
	command:  "boards",
	valid:    []string{"list", "create", "get", "update", "workspace", "archive", "unarchive", "trash", "restore", "purge", "cards"},
	examples: []string{"anx boards list --status active", "anx boards workspace --board-id <board-id>", "anx boards get --board-id <board-id> --json", "anx boards cards create --board-id <board-id> --title \"Buy groceries\" --column backlog"},
	aliases: map[string]string{
		"ls":   "list",
		"show": "get",
	},
}

var boardsCardsSubcommandSpec = subcommandSpec{
	command:  "boards cards",
	valid:    []string{"list", "create", "create-batch", "get", "update", "move", "archive"},
	examples: []string{"anx boards cards list --board-id <board-id>", "anx boards cards create --board-id <board-id> --title \"Buy groceries\" --column backlog", "anx boards get --board-id <board-id> --json   # copy board.updated_at for the next command", "anx boards cards create-batch --board-id <board-id> --from-file batch.json", "anx boards cards create-batch <board-id> --from-file batch.json --request-key my-run-1 --if-board-updated-at \"<board-updated-at>\"", "anx boards cards update --card-id <card-id> --if-updated-at <card-updated-at> --status done"},
	aliases: map[string]string{
		"ls":     "list",
		"add":    "create",
		"batch":  "create-batch",
		"remove": "archive",
		"show":   "get",
	},
}

var docsSubcommandSpec = subcommandSpec{
	command:  "docs",
	valid:    []string{"list", "create", "get", "content", "history", "revision", "trash", "archive", "unarchive", "restore", "purge"},
	examples: []string{"anx docs list --thread-id <thread-id>", "anx docs content --document-id <document-id>", "anx docs apply --proposal-id <proposal-id>"},
	aliases: map[string]string{
		"ls":   "list",
		"read": "content",
		"cat":  "content",
	},
}

var docsRevisionSubcommandSpec = subcommandSpec{
	command:  "docs revision",
	valid:    []string{"get"},
	examples: []string{"anx docs revision get --document-id <document-id> --revision-id <revision-id>"},
}

var eventsSubcommandSpec = subcommandSpec{
	command:  "events",
	valid:    []string{"list", "get", "create", "validate", "stream", "tail", "explain", "archive", "unarchive", "trash", "restore"},
	examples: []string{"anx events list --thread-id <thread-id> --type actor_statement --mine --full-id", "anx events tail --max-events 20"},
	aliases: map[string]string{
		"watch": "stream",
		"ls":    "list",
	},
}

var inboxSubcommandSpec = subcommandSpec{
	command:  "inbox",
	valid:    []string{"list", "get", "acknowledge", "ack", "stream", "tail"},
	examples: []string{"anx inbox get --id <id-or-alias>", "anx inbox acknowledge --inbox-item-id <id-or-alias>"},
	aliases: map[string]string{
		"ls":    "list",
		"ack":   "acknowledge",
		"watch": "stream",
	},
}

var derivedSubcommandSpec = subcommandSpec{
	command:  "derived",
	valid:    []string{"rebuild"},
	examples: []string{"anx derived rebuild --actor-id <actor-id>"},
}

func packetCreateSubcommandSpec(resource string) subcommandSpec {
	trimmed := strings.TrimSpace(resource)
	return subcommandSpec{
		command:  trimmed,
		valid:    []string{"create"},
		examples: []string{fmt.Sprintf("anx %s create", trimmed)},
	}
}

func (s subcommandSpec) normalize(raw string) string {
	token := strings.ToLower(strings.TrimSpace(raw))
	if token == "" {
		return ""
	}
	if canonical, ok := s.aliases[token]; ok {
		return canonical
	}
	return token
}

func (s subcommandSpec) requiredError() *errnorm.Error {
	message := fmt.Sprintf("expected one of: %s; examples: %s", strings.Join(s.valid, ", "), joinExamples(s.examples))
	return errnorm.Usage("subcommand_required", message)
}

func (s subcommandSpec) unknownError(raw string) *errnorm.Error {
	raw = strings.TrimSpace(raw)
	parts := []string{
		fmt.Sprintf("unknown %s subcommand %q", strings.TrimSpace(s.command), raw),
		"valid subcommands: " + strings.Join(s.valid, ", "),
	}
	if suggestion := s.suggestion(raw); suggestion != "" {
		parts = append(parts, "did you mean `"+suggestion+"`?")
	}
	parts = append(parts, "examples: "+joinExamples(s.examples))
	return errnorm.Usage("unknown_subcommand", strings.Join(parts, "; "))
}

func (s subcommandSpec) suggestion(raw string) string {
	token := strings.ToLower(strings.TrimSpace(raw))
	if token == "" {
		return ""
	}
	if canonical, ok := s.aliases[token]; ok {
		return s.commandPath(canonical)
	}
	if strings.TrimSpace(s.command) == "inbox" && looksLikePositionalID(raw) {
		return "anx inbox ack --inbox-item-id <id-or-alias>"
	}
	if closest := closestSubcommand(token, s.valid); closest != "" {
		return s.commandPath(closest)
	}
	return ""
}

func (s subcommandSpec) commandPath(subcommand string) string {
	return strings.Join(strings.Fields("anx "+strings.TrimSpace(s.command)+" "+strings.TrimSpace(subcommand)), " ")
}

func joinExamples(examples []string) string {
	formatted := make([]string, 0, len(examples))
	for _, example := range examples {
		example = strings.TrimSpace(example)
		if example == "" {
			continue
		}
		formatted = append(formatted, "`"+example+"`")
	}
	if len(formatted) == 0 {
		return "`anx help`"
	}
	return strings.Join(formatted, "; ")
}

func looksLikePositionalID(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "-") {
		return false
	}
	if strings.Contains(raw, ":") {
		return true
	}
	hasLetter := false
	for _, r := range raw {
		if unicode.IsLetter(r) {
			hasLetter = true
			break
		}
	}
	return !hasLetter
}

func closestSubcommand(token string, options []string) string {
	token = strings.ToLower(strings.TrimSpace(token))
	if token == "" {
		return ""
	}

	best := ""
	bestDistance := -1
	for _, option := range options {
		option = strings.ToLower(strings.TrimSpace(option))
		if option == "" {
			continue
		}
		if strings.HasPrefix(option, token) || strings.HasPrefix(token, option) {
			return option
		}
		distance := levenshteinDistance(token, option)
		if bestDistance == -1 || distance < bestDistance {
			bestDistance = distance
			best = option
		}
	}
	if best == "" {
		return ""
	}
	maxDistance := 1
	if len(token) >= 5 {
		maxDistance = 2
	}
	if len(token) >= 10 {
		maxDistance = 3
	}
	if bestDistance > maxDistance {
		return ""
	}
	return best
}

func levenshteinDistance(a string, b string) int {
	if a == b {
		return 0
	}
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := 0; j <= len(b); j++ {
		prev[j] = j
	}

	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			curr[j] = min3Int(del, ins, sub)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func min3Int(values ...int) int {
	best := values[0]
	for _, v := range values[1:] {
		if v < best {
			best = v
		}
	}
	return best
}
