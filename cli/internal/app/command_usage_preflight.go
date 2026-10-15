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

	if len(args) >= 2 && args[0] == "bridge" && args[1] == "run" {
		return "bridge run", preflightFlagUsage(args[2:], map[string]preflightFlagSpec{"max-attempts": {kind: preflightFlagString}, "command-timeout": {kind: preflightFlagString}})
	}
	if len(args) >= 2 && args[0] == "bridge" && args[1] == "install" && !bridgeInstallTargetsRuntime(args[2:]) {
		return "bridge install", preflightFlagUsage(args[2:], map[string]preflightFlagSpec{"as": {kind: preflightFlagString}, "dry-run": {kind: preflightFlagBool}})
	}
	if args[0] == "integrations" {
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
			"dry-run": {kind: preflightFlagBool},
		})
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

type preflightFlagKind int

const (
	preflightFlagString preflightFlagKind = iota
	preflightFlagBool
)

type preflightFlagSpec struct {
	kind            preflightFlagKind
	requireNonEmpty bool
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
	case "workspace":
		return preflightWorkspaceSubcommand(args[1:])
	case "move":
		_, err := parseMoveCommand(args[1:])
		return err
	case "read":
		return nil
	case "url":
		return nil
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

func preflightSkillsArgs(args []string) error {
	if err := preflightSubcommand(args, skillsSubcommandSpec); err != nil {
		return err
	}
	if len(args) == 0 {
		return nil
	}
	command := skillsSubcommandSpec.normalize(args[0])
	return preflightFlagUsage(args[1:], skillsPreflightFlagSpecs()["skills "+command])
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

func preflightThreadsSubcommand(args []string) error {
	if len(args) == 0 || isHelpToken(args[0]) {
		return nil
	}
	if strings.TrimSpace(args[0]) == "patch" {
		return nil
	}
	return preflightSubcommand(args, threadsSubcommandSpec)
}

func preflightWorkspaceSubcommand(args []string) error {
	if len(args) == 0 || isHelpToken(args[0]) {
		return nil
	}
	if args[0] == "dashboard" {
		if len(args) == 2 && args[1] == "list" {
			return nil
		}
		if len(args) != 3 || args[1] != "set" || strings.HasPrefix(args[2], "-") {
			return errnorm.Usage("invalid_args", "usage: anx workspace dashboard set <doc|none>")
		}
		return nil
	}
	sub := strings.TrimSpace(args[0])
	if sub == "summary" {
		return nil
	}
	return errnorm.Usage("unknown_subcommand", fmt.Sprintf("unknown workspace subcommand %q; valid subcommands: summary; examples: `anx workspace summary`", sub))
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

func matchPreflightFlagSpec(args []string) (string, []string, bool) {
	specs := preflightFlagSpecs()
	for width := preflightMinInt(len(args), 3); width >= 1; width-- {
		path := strings.Join(args[:width], " ")
		if _, ok := specs[path]; ok {
			return path, args[width:], true
		}
	}
	return "", nil, false
}

func preflightFlagUsage(args []string, spec map[string]preflightFlagSpec) error {
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			continue
		}
		if arg == "--" {
			break
		}
		if arg == "-" {
			continue
		}
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		name = strings.TrimSpace(name)
		flagSpec, ok := spec[name]
		if !ok {
			return errnorm.Usage("invalid_flags", fmt.Sprintf("flag provided but not defined: -%s", name))
		}
		seen[name] = true
		switch flagSpec.kind {
		case preflightFlagBool:
			if hasValue {
				if _, err := strconvParseBool(value); err != nil {
					return errnorm.Usage("invalid_flags", fmt.Sprintf("invalid boolean value %q for --%s", value, name))
				}
			}
		default:
			if !hasValue {
				if i+1 >= len(args) || (strings.HasPrefix(args[i+1], "-") && !looksLikeStringFlagValue(args[i+1])) {
					return errnorm.Usage("invalid_flags", fmt.Sprintf("flag needs an argument: -%s", name))
				}
				if flagSpec.requireNonEmpty && strings.TrimSpace(args[i+1]) == "" {
					return errnorm.Usage("invalid_request", fmt.Sprintf("--%s must not be empty", name))
				}
				i++
			} else if flagSpec.requireNonEmpty && strings.TrimSpace(value) == "" {
				return errnorm.Usage("invalid_request", fmt.Sprintf("--%s must not be empty", name))
			}
		}
	}
	return validateLifecycleFilterFlags(seen["include-archived"], seen["archived-only"], seen["include-trashed"], seen["trashed-only"])
}

func preflightDocsIngestArgs(args []string) error {
	hasPath := false
	hasSource := false
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			continue
		}
		if arg == "--source" {
			hasSource = true
			if i+1 < len(args) {
				i++
			}
			continue
		}
		if strings.HasPrefix(arg, "--source=") {
			hasSource = true
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		hasPath = true
	}
	if !hasPath {
		return errnorm.Usage("invalid_request", "path is required; pass a directory of markdown files")
	}
	if !hasSource {
		return errnorm.Usage("invalid_request", "`--source` is required; pass a URL prefix joined with each relative path")
	}
	return nil
}

func preflightRootCommands() map[string]struct{} {
	return map[string]struct{}{
		"version": {}, "doctor": {}, "update": {}, "bridge": {}, "auth": {}, "host": {}, "runs": {}, "config": {}, "debug": {}, "notifications": {},
		"import": {}, "install": {}, "skills": {}, "draft": {}, "provenance": {}, "orient": {}, "note": {}, "done": {}, "context": {}, "ask": {}, "review": {}, "escalate": {}, "await": {}, "secret": {}, "workspace": {}, "overview": {}, "read": {}, "url": {}, "concepts": {}, "primitives": {},
		"topics": {}, "cards": {}, "artifacts": {}, "boards": {}, "docs": {},
		"api": {}, "report": {}, "inbox": {}, "move": {}, "help": {}, "--help": {}, "-h": {},
	}
}

func preflightFlagSpecs() map[string]map[string]preflightFlagSpec {
	specs := map[string]map[string]preflightFlagSpec{
		"host enroll": {"name": {kind: preflightFlagString}, "exclude": {kind: preflightFlagString}, "token": {kind: preflightFlagString}, "token-stdin": {kind: preflightFlagBool}, "plan": {kind: preflightFlagBool}},
		"host revoke": {}, "host enrollments list": {}, "host enrollments approve": {}, "host enrollments deny": {},
		"host tokens list": {}, "host tokens revoke": {}, "auth admins list": {"limit": {kind: preflightFlagString}, "cursor": {kind: preflightFlagString}}, "auth admins grant": {}, "auth admins revoke": {},
	}
	addLayer := func(layer map[string]map[string]preflightFlagSpec) {
		for path, flags := range layer {
			current := specs[path]
			if current == nil {
				current = map[string]preflightFlagSpec{}
				specs[path] = current
			}
			for name, spec := range flags {
				current[name] = spec
			}
		}
	}
	addLayer(map[string]map[string]preflightFlagSpec{
		"await":      {"until": {kind: preflightFlagString}, "timeout": {kind: preflightFlagString}, "answers": {kind: preflightFlagBool}},
		"work start": {},
		"work block": {"ask": {kind: preflightFlagBool}, "recommend": {kind: preflightFlagString}, "alt": {kind: preflightFlagString}, "body": {kind: preflightFlagString}, "body-file": {kind: preflightFlagString}, "force": {kind: preflightFlagBool}, "reason": {kind: preflightFlagString}},
	})
	for _, kind := range []string{"ask", "review", "escalate"} {
		addLayer(map[string]map[string]preflightFlagSpec{kind: {
			"template": {kind: preflightFlagString}, "force": {kind: preflightFlagBool}, "reason": {kind: preflightFlagString}, "supersedes": {kind: preflightFlagString}, "evidence": {kind: preflightFlagString}, "on-answer": {kind: preflightFlagString}, "webhook": {kind: preflightFlagString}, "delivery": {kind: preflightFlagString},
			"recommend": {kind: preflightFlagString}, "alt": {kind: preflightFlagString},
			"from-file": {kind: preflightFlagString}, "subject-ref": {kind: preflightFlagString},
			"thread-id": {kind: preflightFlagString}, "ref": {kind: preflightFlagString},
			"body": {kind: preflightFlagString}, "body-file": {kind: preflightFlagString},
			"title": {kind: preflightFlagString}, "request-id": {kind: preflightFlagString},
			"requester-actor-id": {kind: preflightFlagString}, "requester-agent-id": {kind: preflightFlagString},
			"requester-label": {kind: preflightFlagString}, "coverage-hint": {kind: preflightFlagString},
			"severity": {kind: preflightFlagString}, "actor-id": {kind: preflightFlagString},
			"dry-run": {kind: preflightFlagBool},
			"by":      {kind: preflightFlagString}, "blocks": {kind: preflightFlagString}, "priority": {kind: preflightFlagString},
		}})
	}
	// Lowest precedence: lifecycle verbs derived from the resource registry.
	// `manualPreflightFlagSpecs` and `localHelperTopics` may still override.
	addLayer(derivedLifecyclePreflightSpecs())
	addLayer(resourceRuntimePreflightSpecs())
	for _, topic := range localHelperTopics {
		if strings.HasPrefix(topic.Path, "skills ") {
			continue
		}
		flags := map[string]preflightFlagSpec{}
		for _, flag := range topic.Flags {
			name, kind, ok := parseLocalHelperFlagSpec(flag.Name)
			if ok {
				flags[name] = preflightFlagSpec{kind: kind}
			}
		}
		if len(flags) > 0 {
			addLayer(map[string]map[string]preflightFlagSpec{strings.Join(strings.Fields(topic.Path), " "): flags})
		}
	}
	addLayer(manualPreflightFlagSpecs())
	addLayer(skillsPreflightFlagSpecs())
	return specs
}

func preflightSpecsFromRuntimeFlags(flags []runtimeCommandFlagSpec) map[string]preflightFlagSpec {
	out := map[string]preflightFlagSpec{}
	for _, flag := range flags {
		out[flag.name] = preflightFlagSpec{kind: flag.kind}
	}
	return out
}

func parseLocalHelperFlagSpec(raw string) (string, preflightFlagKind, bool) {
	parts := strings.Fields(strings.TrimSpace(raw))
	if len(parts) == 0 {
		return "", preflightFlagString, false
	}
	name := strings.TrimPrefix(strings.TrimSpace(parts[0]), "--")
	if name == "" || strings.HasPrefix(name, "-") {
		return "", preflightFlagString, false
	}
	if len(parts) == 1 {
		return name, preflightFlagBool, true
	}
	return name, preflightFlagString, true
}

func manualPreflightFlagSpecs() map[string]map[string]preflightFlagSpec {
	boolFlag := preflightFlagSpec{kind: preflightFlagBool}
	valueFlag := preflightFlagSpec{kind: preflightFlagString}
	lifecycle := map[string]preflightFlagSpec{
		"include-archived": boolFlag,
		"archived-only":    boolFlag,
		"include-trashed":  boolFlag,
		"trashed-only":     boolFlag,
	}
	listFlags := map[string]preflightFlagSpec{
		"state":   valueFlag,
		"q":       valueFlag,
		"limit":   valueFlag,
		"cursor":  valueFlag,
		"full-id": boolFlag,
	}
	merge := func(maps ...map[string]preflightFlagSpec) map[string]preflightFlagSpec {
		out := map[string]preflightFlagSpec{}
		for _, flags := range maps {
			for name, spec := range flags {
				out[name] = spec
			}
		}
		return out
	}
	return map[string]map[string]preflightFlagSpec{
		"move card": {
			"to": valueFlag, "connection-map": valueFlag, "dry-run": boolFlag,
		},
		"move topic": {
			"to": valueFlag, "connection-map": valueFlag, "dry-run": boolFlag,
		},
		"api call": {
			"method":    valueFlag,
			"path":      valueFlag,
			"from-file": valueFlag,
			"header":    valueFlag,
		},
		"auth invites create": {
			"kind": valueFlag,
		},
		"auth invites revoke": {
			"invite-id": valueFlag,
		},
		"auth principals list": {
			"limit":        valueFlag,
			"cursor":       valueFlag,
			"taggable":     boolFlag,
			"handles-only": boolFlag,
		},
		"auth audit list": {
			"limit":  valueFlag,
			"cursor": valueFlag,
		},
		"notifications list": {
			"status": valueFlag,
			"order":  valueFlag,
		},
		"notifications read": {
			"wakeup-id": valueFlag,
		},
		"notifications dismiss": {
			"wakeup-id": valueFlag,
		},
		"secret create": {
			"from-stdin":  boolFlag,
			"description": valueFlag,
		},
		"secret get": {
			"reveal": boolFlag,
		},
		"secret update": {
			"from-stdin":  boolFlag,
			"description": valueFlag,
		},
		"secret exec": {
			"secret": valueFlag,
		},
		"import scan": {
			"input":                valueFlag,
			"out":                  valueFlag,
			"max-preview-bytes":    valueFlag,
			"max-text-cache-bytes": valueFlag,
		},
		"import dedupe": {
			"inventory": valueFlag,
			"out":       valueFlag,
		},
		"import plan": {
			"inventory":           valueFlag,
			"dedupe":              valueFlag,
			"out":                 valueFlag,
			"source-name":         valueFlag,
			"collector-threshold": valueFlag,
		},
		"import apply": {
			"plan":    valueFlag,
			"out":     valueFlag,
			"execute": boolFlag,
		},
		"install skill": {
			"path":       valueFlag,
			"write-file": valueFlag,
			"force":      boolFlag,
		},
		"boards patch": {
			"board-id":      valueFlag,
			"from-file":     valueFlag,
			"role":          valueFlag,
			"if-updated-at": {kind: preflightFlagString, requireNonEmpty: true},
			"dry-run":       boolFlag,
		},
		"topics list": merge(listFlags, lifecycle),
		"topics patch": {
			"topic-id":      valueFlag,
			"from-file":     valueFlag,
			"title":         valueFlag,
			"summary":       valueFlag,
			"if-updated-at": valueFlag,
			"actor-id":      valueFlag,
			"dry-run":       boolFlag,
		},
		// Lifecycle verbs (archive/unarchive/trash/restore/purge) for topics,
		// boards, docs, cards, artifacts, and events are derived from
		// `lifecycleResourceSpecs()` in lifecycle_spec.go. Add overrides here
		// only when a parser exposes a flag outside the standard convention.
		"docs list": merge(map[string]preflightFlagSpec{
			"thread-id": valueFlag,
			"q":         valueFlag,
			"tag":       valueFlag,
			"knowledge": boolFlag,
			"limit":     valueFlag,
			"cursor":    valueFlag,
		}, lifecycle),
		"boards list": merge(listFlags, map[string]preflightFlagSpec{
			"owner": valueFlag,
		}, lifecycle),
		"actors list": {
			"q":      valueFlag,
			"limit":  valueFlag,
			"cursor": valueFlag,
		},
		"events get": {
			"id":       valueFlag,
			"event-id": valueFlag,
		},
		"events list": merge(map[string]preflightFlagSpec{
			"thread-id":     valueFlag,
			"type":          valueFlag,
			"types":         valueFlag,
			"event-group":   valueFlag,
			"backing-scope": valueFlag,
			"actor-id":      valueFlag,
			"mine":          boolFlag,
			"full-id":       boolFlag,
			"max-events":    valueFlag,
			"max":           valueFlag,
		}, lifecycle),
		"events explain": {
			"type": valueFlag,
		},
		"inbox summary": {"limit": {kind: preflightFlagString}},
		"inbox list": {
			"thread-id": valueFlag,
			"type":      valueFlag,
			"full-id":   boolFlag,
			"status":    valueFlag,
			"unread":    boolFlag,
			"limit":     valueFlag,
			"cursor":    valueFlag,
		},
		"inbox respond": {
			"from-file":     valueFlag,
			"inbox-item-id": valueFlag,
			"response-text": valueFlag,
			"outcome":       valueFlag,
			"notify-mode":   valueFlag,
			"actor-id":      valueFlag,
		},
		"topics messages": lifecycle,
		"docs messages":   lifecycle,
		"cards messages":  lifecycle,
		"docs search": {
			"q":         valueFlag,
			"tag":       valueFlag,
			"host":      valueFlag,
			"knowledge": boolFlag,
			"limit":     valueFlag,
			"cursor":    valueFlag,
		},
		"docs put": {
			"title":       valueFlag,
			"source":      valueFlag,
			"tags":        valueFlag,
			"hosts":       valueFlag,
			"verified-at": valueFlag,
			"handle":      valueFlag,
			"body":        valueFlag,
			"body-file":   valueFlag,
			"actor-id":    valueFlag,
		},
		"docs ingest": {
			"source":      valueFlag,
			"tags":        valueFlag,
			"hosts":       valueFlag,
			"verified-at": valueFlag,
			"actor-id":    valueFlag,
		},
		"docs get": {
			"document-id": valueFlag,
			"format":      valueFlag,
		},
		"docs comment": {
			"document-id": valueFlag,
			"body":        valueFlag,
			"reply-to":    valueFlag,
			"actor-id":    valueFlag,
		},
		"docs comments": {
			"document-id": valueFlag,
			"limit":       valueFlag,
			"cursor":      valueFlag,
		},
		"docs comments reply": {
			"document-id": valueFlag,
			"comment-id":  valueFlag,
			"body":        valueFlag,
			"actor-id":    valueFlag,
		},
		"docs comments edit": {
			"document-id": valueFlag,
			"comment-id":  valueFlag,
			"body":        valueFlag,
			"actor-id":    valueFlag,
		},
		"docs comments delete": {
			"document-id": valueFlag,
			"comment-id":  valueFlag,
			"actor-id":    valueFlag,
		},
		"docs revise": {
			"document-id": valueFlag,
			"proposal-id": valueFlag,
			"from-file":   valueFlag,
			"body-file":   valueFlag,
			"actor-id":    valueFlag,
			"apply":       boolFlag,
			"propose":     boolFlag,
		},
		"cards create": {
			"board":               valueFlag,
			"board-id":            valueFlag,
			"title":               valueFlag,
			"body":                valueFlag,
			"body-file":           valueFlag,
			"from-file":           valueFlag,
			"actor-id":            valueFlag,
			"request-key":         valueFlag,
			"if-board-updated-at": valueFlag,
			"column":              valueFlag,
			"topic":               valueFlag,
			"document-ref":        valueFlag,
			"risk":                valueFlag,
			"due-at":              valueFlag,
			"before-card-id":      valueFlag,
			"after-card-id":       valueFlag,
			"assignee-ref":        valueFlag,
			"ref":                 valueFlag,
			"done":                valueFlag,
			"dry-run":             boolFlag,
		},
		"cards patch": {
			"card-id":       valueFlag,
			"from-file":     valueFlag,
			"title":         valueFlag,
			"summary":       valueFlag,
			"column-key":    valueFlag,
			"if-updated-at": valueFlag,
			"actor-id":      valueFlag,
		},
		"cards revise": {"card-id": valueFlag, "body-file": valueFlag, "from-file": valueFlag},
		"cards move": {
			"card-id":             valueFlag,
			"column":              valueFlag,
			"if-board-updated-at": valueFlag,
			"actor-id":            valueFlag,
			"from-file":           valueFlag,
			"dry-run":             boolFlag,
		},
		"cards assign": {"card-id": valueFlag},
		"cards resolve": {
			"card-id":             valueFlag,
			"column":              valueFlag,
			"resolution":          valueFlag,
			"resolution-ref":      valueFlag,
			"reason":              valueFlag,
			"body":                valueFlag,
			"body-file":           valueFlag,
			"summary":             valueFlag,
			"if-board-updated-at": valueFlag,
			"actor-id":            valueFlag,
			"from-file":           valueFlag,
			"dry-run":             boolFlag,
		},
		"cards reopen":            {"card-id": valueFlag},
		"threads message":         {"thread": valueFlag, "thread-id": valueFlag},
		"threads reply":           {"thread": valueFlag, "thread-id": valueFlag},
		"boards workspace":        {"board-id": valueFlag},
		"boards cards list":       {"board-id": valueFlag},
		"workspace summary":       {"full-id": boolFlag},
		"overview":                {},
		"workspace dashboard set": {},
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

func preflightConfigArgs(args []string) error {
	if len(args) == 0 {
		return nil
	}
	if args[0] == "ask-delivery" {
		if len(args) > 2 {
			return errnorm.Usage("invalid_args", "use anx help config ask-delivery for command arguments")
		}
		return nil
	}
	for _, arg := range args[1:] {
		if strings.HasPrefix(arg, "-") {
			return errnorm.Usage("invalid_flags", "config commands accept positional arguments only")
		}
	}
	if args[0] == "forge" {
		return preflightConfigForgeArgs(args[1:])
	}
	expected := map[string]int{"show": 0, "workspaces": 0, "use": 1, "map": 2, "unmap": 1}
	if len(args)-1 != expected[args[0]] {
		return errnorm.Usage("invalid_args", "use anx help config "+args[0]+" for command arguments")
	}
	return nil
}

func preflightConfigForgeArgs(args []string) error {
	if len(args) == 0 {
		return configForgeSubcommandSpec.requiredError()
	}
	bounds := map[string][2]int{"add": {3, 3}, "remove": {2, 2}, "list": {0, 1}}
	count, ok := bounds[args[0]]
	if !ok {
		return configForgeSubcommandSpec.unknownError(args[0])
	}
	if given := len(args) - 1; given < count[0] || given > count[1] {
		return errnorm.Usage("invalid_args", "use anx help config forge "+args[0]+" for command arguments")
	}
	return nil
}
