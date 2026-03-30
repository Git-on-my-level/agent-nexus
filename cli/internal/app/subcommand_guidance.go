package app

import (
	"fmt"
	"strings"
	"unicode"

	"organization-autorunner-cli/internal/errnorm"
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
	examples: []string{"oar api call --method GET --path /readyz"},
}

var bridgeSubcommandSpec = subcommandSpec{
	command:  "bridge",
	valid:    []string{"install", "import-auth", "init-config", "start", "stop", "restart", "status", "logs", "workspace-id", "doctor"},
	examples: []string{"oar bridge install", "oar bridge import-auth --config ./agent.toml --from-profile agent-a", "oar bridge init-config --kind hermes --output ./agent.toml --workspace-id ws_main", "oar bridge workspace-id --handle hermes", "oar bridge start --config ./agent.toml", "oar bridge status --config ./agent.toml"},
}

var notificationsSubcommandSpec = subcommandSpec{
	command:  "notifications",
	valid:    []string{"list", "read", "dismiss"},
	examples: []string{"oar notifications list --status unread", "oar notifications read --wakeup-id wake_123", "oar notifications dismiss --wakeup-id wake_123"},
	aliases: map[string]string{
		"ls": "list",
	},
}

var authSubcommandSpec = subcommandSpec{
	command: "auth",
	valid:   []string{"register", "whoami", "list", "update-username", "rotate", "revoke", "token-status", "invites", "bootstrap", "principals", "audit"},
	examples: []string{
		"oar auth register --username <username> --bootstrap-token <token>",
		"oar auth register --username <username> --invite-token <token>",
		"oar auth whoami",
		"oar auth list",
		"oar auth invites list",
		"oar auth invites create --kind agent",
		"oar auth bootstrap status",
		"oar auth principals list",
		"oar auth principals revoke --agent-id <agent-id>",
		"oar auth principals revoke --agent-id <agent-id> --allow-human-lockout --human-lockout-reason 'incident recovery'",
		"oar auth audit list",
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
	examples: []string{"oar auth invites list", "oar auth invites create --kind agent", "oar auth invites revoke --invite-id <id>"},
	aliases: map[string]string{
		"ls": "list",
	},
}

var authBootstrapSubcommandSpec = subcommandSpec{
	command:  "auth bootstrap",
	valid:    []string{"status"},
	examples: []string{"oar auth bootstrap status"},
}

var authPrincipalsSubcommandSpec = subcommandSpec{
	command:  "auth principals",
	valid:    []string{"list", "revoke"},
	examples: []string{"oar auth principals list", "oar auth principals list --limit 20", "oar auth principals revoke --agent-id <agent-id>", "oar auth principals revoke --agent-id <agent-id> --allow-human-lockout --human-lockout-reason 'incident recovery'"},
	aliases: map[string]string{
		"ls": "list",
	},
}

var authAuditSubcommandSpec = subcommandSpec{
	command:  "auth audit",
	valid:    []string{"list"},
	examples: []string{"oar auth audit list", "oar auth audit list --limit 50"},
	aliases: map[string]string{
		"ls": "list",
	},
}

var actorsSubcommandSpec = subcommandSpec{
	command:  "actors",
	valid:    []string{"list", "register"},
	examples: []string{"oar actors list --q bot --limit 50", "oar actors register --id bot-1 --display-name \"Bot 1\" --created-at 2026-03-04T10:00:00Z"},
	aliases: map[string]string{
		"ls": "list",
	},
}

var metaSubcommandSpec = subcommandSpec{
	command:  "meta",
	valid:    []string{"health", "livez", "readyz", "version", "handshake", "ops", "commands", "command", "concepts", "concept", "docs", "doc", "skill"},
	examples: []string{"oar meta health", "oar meta readyz", "oar meta ops health", "oar meta commands", "oar meta command --command-id threads.list", "oar meta docs", "oar meta doc agent-guide", "oar meta skill cursor --write-dir ~/.cursor/skills/oar-cli-onboard"},
}

var metaOpsSubcommandSpec = subcommandSpec{
	command:  "meta ops",
	valid:    []string{"health"},
	examples: []string{"oar meta ops health"},
}

var draftSubcommandSpec = subcommandSpec{
	command:  "draft",
	valid:    []string{"create", "list", "commit", "discard"},
	examples: []string{"oar draft list", "oar draft commit --draft-id <draft-id>"},
}

var provenanceSubcommandSpec = subcommandSpec{
	command:  "provenance",
	valid:    []string{"walk"},
	examples: []string{"oar provenance walk --from event:<event-id> --depth 2"},
}

var threadsSubcommandSpec = subcommandSpec{
	command:  "threads",
	valid:    []string{"list", "get", "create", "patch", "propose-patch", "apply", "timeline", "context", "inspect", "workspace", "review", "recommendations"},
	examples: []string{"oar threads list --status active", "oar threads review --thread-id <thread-id>", "oar threads workspace --status active --type initiative --full-id"},
	aliases: map[string]string{
		"ls":     "list",
		"update": "patch",
	},
}

var commitmentsSubcommandSpec = subcommandSpec{
	command:  "commitments",
	valid:    []string{"list", "get", "create", "patch", "propose-patch", "apply"},
	examples: []string{"oar commitments list --status open", "oar commitments get --commitment-id <commitment-id>"},
	aliases: map[string]string{
		"ls":      "list",
		"show":    "get",
		"inspect": "get",
	},
}

var artifactsSubcommandSpec = subcommandSpec{
	command:  "artifacts",
	valid:    []string{"list", "get", "create", "content", "inspect", "tombstone"},
	examples: []string{"oar artifacts list --kind packet", "oar artifacts inspect --artifact-id <artifact-id>"},
	aliases: map[string]string{
		"ls":   "list",
		"show": "inspect",
	},
}

var boardsSubcommandSpec = subcommandSpec{
	command:  "boards",
	valid:    []string{"list", "create", "get", "update", "workspace", "cards"},
	examples: []string{"oar boards list --status active", "oar boards workspace --board-id <board-id>", "oar boards cards move --board-id <board-id> --thread-id <thread-id> --column review --if-board-updated-at <timestamp>"},
	aliases: map[string]string{
		"ls":   "list",
		"show": "get",
	},
}

var boardsCardsSubcommandSpec = subcommandSpec{
	command:  "boards cards",
	valid:    []string{"list", "add", "update", "move", "remove"},
	examples: []string{"oar boards cards list --board-id <board-id>", "oar boards cards add --board-id <board-id> --thread-id <thread-id>", "oar boards cards move --board-id <board-id> --thread-id <thread-id> --column review --if-board-updated-at <timestamp>"},
	aliases: map[string]string{
		"ls": "list",
	},
}

var docsSubcommandSpec = subcommandSpec{
	command:  "docs",
	valid:    []string{"list", "create", "get", "content", "update", "propose-update", "apply", "validate-update", "history", "revision", "tombstone"},
	examples: []string{"oar docs list --thread-id <thread-id>", "oar docs content --document-id <document-id>", "oar docs apply --proposal-id <proposal-id>"},
	aliases: map[string]string{
		"ls":   "list",
		"read": "content",
		"cat":  "content",
	},
}

var docsRevisionSubcommandSpec = subcommandSpec{
	command:  "docs revision",
	valid:    []string{"get"},
	examples: []string{"oar docs revision get --document-id <document-id> --revision-id <revision-id>"},
}

var eventsSubcommandSpec = subcommandSpec{
	command:  "events",
	valid:    []string{"list", "get", "create", "validate", "stream", "tail", "explain"},
	examples: []string{"oar events list --thread-id <thread-id> --type actor_statement --mine --full-id", "oar events tail --max-events 20"},
	aliases: map[string]string{
		"watch": "stream",
		"ls":    "list",
	},
}

var inboxSubcommandSpec = subcommandSpec{
	command:  "inbox",
	valid:    []string{"list", "get", "ack", "stream", "tail"},
	examples: []string{"oar inbox get --id <id-or-alias>", "oar inbox ack --inbox-item-id <id-or-alias>"},
	aliases: map[string]string{
		"ls":    "list",
		"watch": "stream",
	},
}

var derivedSubcommandSpec = subcommandSpec{
	command:  "derived",
	valid:    []string{"rebuild"},
	examples: []string{"oar derived rebuild --actor-id <actor-id>"},
}

func packetCreateSubcommandSpec(resource string) subcommandSpec {
	trimmed := strings.TrimSpace(resource)
	return subcommandSpec{
		command:  trimmed,
		valid:    []string{"create"},
		examples: []string{fmt.Sprintf("oar %s create", trimmed)},
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
		return "oar inbox ack --inbox-item-id <id-or-alias>"
	}
	if closest := closestSubcommand(token, s.valid); closest != "" {
		return s.commandPath(closest)
	}
	return ""
}

func (s subcommandSpec) commandPath(subcommand string) string {
	return strings.Join(strings.Fields("oar "+strings.TrimSpace(s.command)+" "+strings.TrimSpace(subcommand)), " ")
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
		return "`oar help`"
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
