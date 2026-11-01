// Config-independent usage preflight: detects unknown commands/subcommands
// and invalid flags before host/config resolution so unenrolled hosts get
// usage errors instead of identity errors. The flag spec machinery lives in
// preflight_flag_specs.go and preflight_manual_specs.go, command-specific
// argument checks in preflight_command_args.go.

package app

import (
	"fmt"
	"strings"
	"time"

	"agent-nexus-cli/internal/errnorm"
)

func preflightConfigIndependentUsage(args []string) (string, error) {
	return preflightConfigIndependentUsageWithDebug(args, false)
}

func preflightConfigIndependentUsageWithDebug(args []string, debug bool) (string, error) {
	if len(args) == 0 {
		return "", nil
	}
	if _, help := commandHelpTopic(args); help {
		return "help", nil
	}

	if len(args) >= 2 && args[0] == "bridge" {
		flags := map[string]preflightFlagSpec{}
		switch args[1] {
		case "install":
			flags["dry-run"] = preflightFlagSpec{kind: preflightFlagBool}
		case "remove":
			flags["as"] = preflightFlagSpec{kind: preflightFlagString}
		case "add":
			for _, key := range []string{"as", "runtime", "max-attempts", "command-timeout"} {
				flags[key] = preflightFlagSpec{kind: preflightFlagString}
			}
			for _, key := range []string{"answers", "wake", "git", "spool"} {
				flags[key] = preflightFlagSpec{kind: preflightFlagBool}
			}
		case "run":
			flags["command-timeout"] = preflightFlagSpec{kind: preflightFlagString}
			flags["max-attempts"] = preflightFlagSpec{kind: preflightFlagString}
		case "start", "stop", "status", "doctor":
		default:
			return "bridge", preflightSubcommand(args[1:], bridgeSubcommandSpec)
		}
		return "bridge " + args[1], preflightFlagUsage(args[2:], flags)
	}
	if len(args) >= 2 && args[0] == "host" && args[1] == "bridge-status" {
		return "host bridge-status", preflightFlagUsage(args[2:], map[string]preflightFlagSpec{"from-file": {kind: preflightFlagString}})
	}
	if args[0] == "sync" {
		o, e := parseSyncOptions(args[1:])
		return strings.TrimSpace("sync " + o.verb), e
	}
	if args[0] == "git" {
		o, e := parseGitOptions(args[1:])
		return strings.TrimSpace("git " + o.verb), e
	}
	if args[0] == "hook" {
		_, e := parseAgentHook(args[1:])
		return "hook", e
	}
	if args[0] == "integrations" {
		if newHookIntegration(args[1:]) {
			o, e := parseHookOptions(args[1:])
			return "integrations " + o.verb, e
		}
		o, err := parseIntegrationOptions(args[1:])
		return strings.TrimSpace("integrations " + o.verb), err
	}
	if args[0] == "update" {
		o, err := parseUpdateOptions(args[1:])
		name := "update"
		if len(args) > 1 && !strings.HasPrefix(args[1], "-") {
			name += " " + o.verb
		}
		return name, err
	}
	if len(args) >= 2 && args[0] == "auth" && args[1] == "access-requests" {
		name, _, _, _, err := parseAccessRequestCommand(args[2:])
		return name, err
	}
	if len(args) >= 2 && args[0] == "inbox" && args[1] == "summary" {
		_, err := parseInboxSummary(args[2:])
		return "inbox summary", err
	}
	if args[0] == "series" || args[0] == "adapters" {
		p, err := parseSeriesCommand(args)
		return p.group + " " + p.verb, err
	}
	if args[0] == "deliveries" {
		p, err := parseDeliveriesCommand(args[1:])
		return "deliveries " + p.verb, err
	}
	if args[0] == "lease" {
		p, err := parseLeaseCommand(args[1:])
		return strings.TrimSpace("lease " + p.verb), err
	}
	if len(args) >= 2 && args[0] == "notifications" && args[1] == "settings" {
		p, err := parseNotificationSettingsCommand(args[2:])
		return "notifications settings " + p.verb, err
	}
	if args[0] == "debug" {
		if len(args) < 2 || !isDebugGroup(args[1]) {
			return "debug", errnorm.Usage("unknown_subcommand", "unknown debug group")
		}
		name, err := preflightConfigIndependentUsageWithDebug(args[1:], true)
		return "debug " + name, err
	}
	if isDiagnosticGroup(args[0]) && !debug {
		return args[0], errnorm.Usage("unknown_command", "unknown command "+args[0]+"; use anx debug "+args[0])
	}
	if (args[0] == "ask" || args[0] == "review" || args[0] == "escalate") && hasAskTemplate(args[1:]) {
		return args[0], validateAskTemplate(args[1:])
	}
	if args[0] == "ask" && len(args) > 1 && args[1] == "retire" {
		_, err := parseAskRetirement(args[2:])
		return "ask retire", err
	}
	if args[0] == "ask" && len(args) > 1 && args[1] == "revise" {
		return "ask revise", preflightAskRevise(args[2:])
	}
	if args[0] == "ask" && len(args) > 1 && args[1] == "presentation" {
		return "ask presentation", preflightFlagUsage(args[2:], map[string]preflightFlagSpec{})
	}
	if args[0] == "ask" && len(args) >= 2 && args[1] == "withdraw" {
		commandName := "ask withdraw"
		if err := preflightFlagUsage(args[2:], map[string]preflightFlagSpec{
			"reason": {kind: preflightFlagString}, "dry-run": {kind: preflightFlagBool},
		}); err != nil {
			return commandName, err
		}
		hasReason := false
		positionals := 0
		for i := 0; i < len(args[2:]); i++ {
			arg := strings.TrimSpace(args[i+2])
			if strings.HasPrefix(arg, "-") {
				name, _, inline := strings.Cut(strings.TrimLeft(arg, "-"), "=")
				if name == "reason" {
					if inline {
						hasReason = strings.TrimSpace(strings.TrimPrefix(arg, "--reason=")) != ""
					} else if i+3 < len(args) {
						hasReason = strings.TrimSpace(args[i+3]) != ""
						i++
					}
				}
				continue
			}
			positionals++
		}
		if positionals != 1 {
			return commandName, errnorm.Usage("invalid_args", "usage: anx ask withdraw <event:ask-id> --reason <text>")
		}
		if !hasReason {
			return commandName, errnorm.Usage("invalid_request", "--reason is required")
		}
		return commandName, nil
	}
	if args[0] == "ask" && len(args) >= 2 && args[1] == "batch" {
		return "ask batch", preflightFlagUsage(args[2:], map[string]preflightFlagSpec{
			"from-file": {kind: preflightFlagString}, "delivery": {kind: preflightFlagString},
			"on-answer": {kind: preflightFlagString}, "force": {kind: preflightFlagBool},
			"reason": {kind: preflightFlagString}, "actor-id": {kind: preflightFlagString},
			"dry-run": {kind: preflightFlagBool}, "again": {kind: preflightFlagBool},
		})
	}
	if isWorkTreeCommand(args) {
		p, err := parseWorkTreeCommand(args)
		return p.name, err
	}
	switch args[0] {
	case "orient":
		return "orient", parseOrientArgs(args[1:])
	case "note":
		_, err := parseNoteArgs(args[1:], time.Now())
		return "note", err
	case "done":
		_, _, err := parseDoneArgs(args[1:])
		return "done", err
	case "context":
		if len(args) < 2 {
			return "context", errnorm.Usage("invalid_args", "usage: anx context <card> [--limit N] [--cursor C]")
		}
		_, err := parseWorkCommand(append([]string{"work", "context"}, args[1:]...))
		return "context", err
	case "api":
		if len(args) >= 2 && args[1] != "call" && !isHelpToken(args[1]) {
			_, err := parseAPIOperationArgs(args[1:])
			return "api", err
		}
	}
	if args[0] == "work" && len(args) >= 2 && (args[1] == "note" || args[1] == "done" || args[1] == "context") {
		return "work " + args[1], errnorm.Usage("unknown_subcommand", fmt.Sprintf("unknown work subcommand %q; the loop verb is anx %s", args[1], args[1]))
	}
	if args[0] == "work" && len(args) >= 2 && isDailyWorkVerb(args[1]) {
		name := "work " + args[1]
		return name, preflightFlagUsage(args[2:], preflightFlagSpecs()[name])
	}
	if args[0] == "attention" {
		p, err := parseAttentionCommand(args[1:])
		return p.name, err
	}
	if args[0] == "overview" {
		name, _, err := parseOverviewCommand(args)
		return name, err
	}
	if args[0] == "plan" {
		if len(args) == 1 || len(args) == 2 && args[1] == "step" {
			return "plan", errnorm.Usage("subcommand_required", "run anx help plan")
		}
		parsed, err := parsePlanCommand(args)
		return parsed.name, err
	}
	if args[0] == "refs" {
		_, _, err := parseRefResolve(args)
		return "refs resolve", err
	}
	if len(args) >= 2 && args[0] == "pm" {
		if args[1] == "propose" {
			_, err := parsePMProposal(args[2:])
			return "pm propose", err
		}
		if args[1] == "card" {
			return "pm card", validatePMCardArgs(args[2:])
		}
	}
	if isWorkCommandRoot(args[0]) && !isReportLocalCommand(args) {
		if len(args) >= 2 && args[0] == "pm" && (args[1] == "serve" || args[1] == "curate" || args[1] == "ask" || args[1] == "install" || args[1] == "status" || args[1] == "uninstall") {
			commandName := "pm " + args[1]
			if err := preflightFlagUsage(args[2:], preflightFlagSpecs()[commandName]); err != nil {
				return commandName, err
			}
			return commandName, nil
		}
		if len(args) >= 2 && args[0] == "pm" && args[1] == "channels" {
			commandName := "pm channels"
			if len(args) >= 3 && args[2] == "doctor" {
				commandName = "pm channels doctor"
				if err := preflightFlagUsage(args[3:], preflightFlagSpecs()[commandName]); err != nil {
					return commandName, err
				}
			}
			return commandName, nil
		}
		if topic := strings.Join(args, " "); isWorkCommandGroup(topic) {
			return topic, nil
		}
		parsed, err := parseWorkCommand(args)
		return parsed.name, err
	}
	if rewritten, ok := applyCommandShapeCompatibilityAlias(args); ok {
		args = rewritten
	}
	if err := preflightKnownCommandShape(args); err != nil {
		return preflightShapeCommandName(args), err
	}
	commandName, commandArgs, ok := matchPreflightFlagSpec(args)
	if !ok {
		return preflightInferredCommandName(args), nil
	}
	if err := preflightFlagUsage(commandArgs, preflightFlagSpecs()[commandName]); err != nil {
		return commandName, err
	}
	if commandName == "auth admins list" {
		if _, err := parseAuthAdminList(commandArgs); err != nil {
			return commandName, err
		}
	}
	if commandName == "docs ingest" {
		if err := preflightDocsIngestArgs(commandArgs); err != nil {
			return commandName, err
		}
	}
	return commandName, nil
}

func preflightKnownCommandShape(args []string) error {
	root := strings.TrimSpace(args[0])
	if _, ok := preflightRootCommands()[root]; !ok && !isDiagnosticGroup(root) {
		return errnorm.Usage("unknown_command", fmt.Sprintf("unknown command %q", root))
	}

	switch root {
	case "host":
		if len(args) >= 2 {
			switch args[1] {
			case "enrollments":
				return preflightSubcommand(args[2:], hostEnrollmentsSubcommandSpec)
			case "tokens":
				return preflightSubcommand(args[2:], hostTokensSubcommandSpec)
			}
		}
		if len(args) > 2 && args[1] == "discover" && !hasHelpToken(args[2:]) {
			return errnorm.Usage("invalid_args", "host discover takes no arguments")
		}
		return nil
	case "runs":
		return nil
	case "api":
		return preflightSubcommand(args[1:], apiSubcommandSpec)
	case "auth":
		if err := preflightSubcommand(args[1:], authSubcommandSpec); err != nil {
			return err
		}
		if len(args) >= 3 {
			switch authSubcommandSpec.normalize(args[1]) {
			case "invites":
				return preflightSubcommand(args[2:], authInvitesSubcommandSpec)
			case "bootstrap":
				return preflightSubcommand(args[2:], authBootstrapSubcommandSpec)
			case "principals":
				return preflightSubcommand(args[2:], authPrincipalsSubcommandSpec)
			case "access-requests":
				return preflightSubcommand(args[2:], accessRequestsSubcommandSpec)
			case "admins":
				return preflightSubcommand(args[2:], authAdminsSubcommandSpec)
			case "audit":
				return preflightSubcommand(args[2:], authAuditSubcommandSpec)
			}
		}
	case "bridge":
		return preflightSubcommand(args[1:], bridgeSubcommandSpec)
	case "config":
		if err := preflightSubcommand(args[1:], configSubcommandSpec); err != nil {
			return err
		}
		if err := preflightConfigArgs(args[1:]); err != nil {
			return err
		}
		if len(args) >= 3 && configSubcommandSpec.normalize(args[1]) == "forge" {
			return preflightSubcommand(args[2:], configForgeSubcommandSpec)
		}
		return nil
	case "meta":
		if err := preflightSubcommand(args[1:], metaSubcommandSpec); err != nil {
			return err
		}
		if len(args) >= 3 && metaSubcommandSpec.normalize(args[1]) == "ops" {
			return preflightSubcommand(args[2:], metaOpsSubcommandSpec)
		}
	case "notifications":
		return preflightSubcommand(args[1:], notificationsSubcommandSpec)
	case "import":
		return preflightSubcommand(args[1:], importSubcommandSpec)
	case "skills":
		return preflightSkillsArgs(args[1:])
	case "install":
		return preflightSubcommand(args[1:], installSubcommandSpec)
	case "draft":
		return preflightSubcommand(args[1:], draftSubcommandSpec)
	case "provenance":
		return preflightSubcommand(args[1:], provenanceSubcommandSpec)
	case "orient", "note", "done", "context", "ask", "review", "escalate", "await":
		return nil
	case "secret":
		return preflightSubcommand(args[1:], secretSubcommandSpec)
	case "attention":
		_, err := parseAttentionCommand(args[1:])
		return err
	case "workspace":
		return preflightWorkspaceSubcommand(args[1:])
	case "move":
		_, err := parseMoveCommand(args[1:])
		return err
	case "read":
		return nil
	case "url":
		if len(args) == 1 || isHelpToken(args[1]) {
			return nil
		}
		_, err := parseURLArgs(args[1:])
		return err
	case "link":
		if len(args) == 1 || (len(args) == 2 && isHelpToken(args[1])) {
			return nil
		}
		_, err := parseLinkArgs(args[1:])
		return err
	case "actors":
		return preflightSubcommand(args[1:], actorsSubcommandSpec)
	case "threads":
		return preflightThreadsSubcommand(args[1:])
	case "topics":
		return preflightSubcommand(args[1:], topicsSubcommandSpec)
	case "ref-edges":
		return preflightSubcommand(args[1:], refEdgesSubcommandSpec)
	case "cards":
		return preflightSubcommand(args[1:], cardsSubcommandSpec)
	case "artifacts":
		return preflightSubcommand(args[1:], artifactsSubcommandSpec)
	case "boards":
		if err := preflightSubcommand(args[1:], boardsSubcommandSpec); err != nil {
			return err
		}
		if len(args) >= 3 && boardsSubcommandSpec.normalize(args[1]) == "cards" {
			return preflightSubcommand(args[2:], boardsCardsSubcommandSpec)
		}
	case "docs":
		if err := preflightSubcommand(args[1:], docsSubcommandSpec); err != nil {
			return err
		}
		if len(args) >= 3 && docsSubcommandSpec.normalize(args[1]) == "revision" {
			return preflightSubcommand(args[2:], docsRevisionSubcommandSpec)
		}
		if len(args) >= 3 && docsSubcommandSpec.normalize(args[1]) == "comments" {
			token := docsCommentsSubcommandSpec.normalize(args[2])
			for _, valid := range docsCommentsSubcommandSpec.valid {
				if token == valid {
					return preflightSubcommand(args[2:], docsCommentsSubcommandSpec)
				}
			}
		}
	case "report":
		if len(args) <= 1 {
			return preflightSubcommand(args[1:], reportSubcommandSpec)
		}
		switch reportSubcommandSpec.normalize(args[1]) {
		case "render":
			_, err := parseWorkCommand(args)
			return err
		case "init":
			_, err := parseReportInitArgs(args[2:])
			return err
		case "preview":
			_, err := parseReportPreviewArgs(args[2:])
			return err
		default:
			return preflightSubcommand(args[1:], reportSubcommandSpec)
		}
	case "events":
		return preflightSubcommand(args[1:], eventsSubcommandSpec)
	case "inbox":
		return preflightSubcommand(args[1:], inboxSubcommandSpec)
	case "derived":
		return preflightSubcommand(args[1:], derivedSubcommandSpec)
	}
	return nil
}

func preflightSubcommand(args []string, spec subcommandSpec) error {
	if len(args) == 0 || isHelpToken(args[0]) {
		return nil
	}
	raw := strings.TrimSpace(args[0])
	normalized := spec.normalize(raw)
	for _, valid := range spec.valid {
		if normalized == valid {
			return nil
		}
	}
	return spec.unknownError(raw)
}

func preflightShapeCommandName(args []string) string {
	if len(args) == 0 {
		return "root"
	}
	root := strings.TrimSpace(args[0])
	switch root {
	case "auth":
		if len(args) >= 3 {
			sub := authSubcommandSpec.normalize(args[1])
			if sub == "invites" || sub == "bootstrap" || sub == "principals" || sub == "audit" {
				return strings.Join(args[:2], " ")
			}
		}
	case "boards":
		if len(args) >= 3 && boardsSubcommandSpec.normalize(args[1]) == "cards" {
			return "boards cards"
		}
	case "docs":
		if len(args) >= 3 && docsSubcommandSpec.normalize(args[1]) == "revision" {
			return "docs revision"
		}
		if len(args) >= 3 && docsSubcommandSpec.normalize(args[1]) == "comments" {
			token := docsCommentsSubcommandSpec.normalize(args[2])
			for _, valid := range docsCommentsSubcommandSpec.valid {
				if token == valid {
					return "docs comments " + token
				}
			}
		}
	case "meta":
		if len(args) >= 3 && metaSubcommandSpec.normalize(args[1]) == "ops" {
			return "meta ops"
		}
	}
	return root
}

func preflightInferredCommandName(args []string) string {
	if len(args) == 0 {
		return ""
	}
	for width := preflightMinInt(len(args), 3); width >= 1; width-- {
		path := strings.Join(args[:width], " ")
		if _, ok := preflightFlagSpecs()[path]; ok {
			return path
		}
	}
	if len(args) >= 2 && !strings.HasPrefix(args[1], "-") {
		return strings.Join(args[:2], " ")
	}
	return strings.TrimSpace(args[0])
}

func preflightRootCommands() map[string]struct{} {
	return map[string]struct{}{
		"version": {}, "doctor": {}, "update": {}, "bridge": {}, "auth": {}, "host": {}, "runs": {}, "config": {}, "debug": {}, "notifications": {},
		"import": {}, "install": {}, "skills": {}, "draft": {}, "provenance": {}, "orient": {}, "note": {}, "done": {}, "context": {}, "ask": {}, "review": {}, "escalate": {}, "await": {}, "secret": {}, "workspace": {}, "overview": {}, "attention": {}, "read": {}, "url": {}, "link": {}, "concepts": {}, "primitives": {},
		"topics": {}, "cards": {}, "artifacts": {}, "boards": {}, "docs": {},
		"api": {}, "report": {}, "inbox": {}, "move": {}, "help": {}, "--help": {}, "-h": {},
	}
}

func preflightMinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func looksLikeStringFlagValue(raw string) bool {
	raw = strings.TrimSpace(raw)
	return raw == "-" || raw == "--help" || raw == "-h" || looksLikeNegativeNumericFlagValue(raw)
}

func looksLikeNegativeNumericFlagValue(raw string) bool {
	raw = strings.TrimSpace(raw)
	if len(raw) < 2 || raw[0] != '-' {
		return false
	}
	for _, r := range raw[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func hasHelpToken(args []string) bool {
	for _, arg := range args {
		if isHelpToken(arg) {
			return true
		}
	}
	return false
}
