package app

import (
	"fmt"
	"strings"

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

	if args[0] == "debug" {
		if len(args) < 2 || !isDiagnosticGroup(args[1]) {
			return "debug", errnorm.Usage("unknown_subcommand", "unknown debug group")
		}
		name, err := preflightConfigIndependentUsageWithDebug(args[1:], true)
		return "debug " + name, err
	}
	if isDiagnosticGroup(args[0]) && !debug {
		return args[0], errnorm.Usage("unknown_command", "unknown command "+args[0]+"; use anx debug "+args[0])
	}
	if args[0] == "work" && len(args) >= 2 && isDailyWorkVerb(args[1]) {
		name := "work " + args[1]
		return name, preflightFlagUsage(args[2:], preflightFlagSpecs()[name])
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
	if isWorkCommandRoot(args[0]) && !isReportLocalCommand(args) {
		if len(args) >= 2 && args[0] == "pm" && (args[1] == "serve" || args[1] == "ask") {
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
	kind preflightFlagKind
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
		return preflightConfigArgs(args[1:])
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
		return preflightSubcommand(args[1:], skillsSubcommandSpec)
	case "install":
		return preflightSubcommand(args[1:], installSubcommandSpec)
	case "draft":
		return preflightSubcommand(args[1:], draftSubcommandSpec)
	case "provenance":
		return preflightSubcommand(args[1:], provenanceSubcommandSpec)
	case "orient", "ask", "review", "escalate", "await":
		return nil
	case "secret":
		return preflightSubcommand(args[1:], secretSubcommandSpec)
	case "workspace":
		return preflightWorkspaceSubcommand(args[1:])
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
		if len(args) > 1 && reportSubcommandSpec.normalize(args[1]) == "render" {
			_, err := parseWorkCommand(args)
			return err
		}
		return preflightSubcommand(args[1:], reportSubcommandSpec)
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
				i++
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
		"import": {}, "install": {}, "skills": {}, "draft": {}, "provenance": {}, "orient": {}, "ask": {}, "review": {}, "escalate": {}, "await": {}, "secret": {}, "workspace": {}, "read": {}, "url": {}, "concepts": {}, "primitives": {},
		"topics": {}, "cards": {}, "artifacts": {}, "boards": {}, "docs": {},
		"api": {}, "report": {}, "help": {}, "--help": {}, "-h": {},
	}
}

func preflightFlagSpecs() map[string]map[string]preflightFlagSpec {
	specs := map[string]map[string]preflightFlagSpec{
		"host enroll": {"name": {kind: preflightFlagString}, "exclude": {kind: preflightFlagString}, "token": {kind: preflightFlagString}, "token-stdin": {kind: preflightFlagBool}, "plan": {kind: preflightFlagBool}},
		"host revoke": {}, "host enrollments list": {}, "host enrollments approve": {}, "host enrollments deny": {},
		"host tokens list": {}, "host tokens revoke": {}, "auth admins list": {}, "auth admins grant": {}, "auth admins revoke": {},
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
		"orient":     {"stale-hours": {kind: preflightFlagString}},
		"await":      {"until": {kind: preflightFlagString}, "timeout": {kind: preflightFlagString}},
		"work start": {},
		"work note":  {},
		"work block": {"ask": {kind: preflightFlagBool}, "recommend": {kind: preflightFlagString}, "alt": {kind: preflightFlagString}},
		"work done":  {"evidence": {kind: preflightFlagString}},
	})
	for _, kind := range []string{"ask", "review", "escalate"} {
		addLayer(map[string]map[string]preflightFlagSpec{kind: {
			"recommend": {kind: preflightFlagString}, "alt": {kind: preflightFlagString},
			"from-file": {kind: preflightFlagString}, "subject-ref": {kind: preflightFlagString},
			"thread-id": {kind: preflightFlagString}, "ref": {kind: preflightFlagString},
			"body": {kind: preflightFlagString}, "body-file": {kind: preflightFlagString},
			"title": {kind: preflightFlagString}, "request-id": {kind: preflightFlagString},
			"requester-actor-id": {kind: preflightFlagString}, "requester-agent-id": {kind: preflightFlagString},
			"requester-label": {kind: preflightFlagString}, "coverage-hint": {kind: preflightFlagString},
			"severity": {kind: preflightFlagString}, "actor-id": {kind: preflightFlagString},
			"dry-run": {kind: preflightFlagBool},
		}})
	}
	// Lowest precedence: lifecycle verbs derived from the resource registry.
	// `manualPreflightFlagSpecs` and `localHelperTopics` may still override.
	addLayer(derivedLifecyclePreflightSpecs())
	addLayer(resourceRuntimePreflightSpecs())
	for _, topic := range localHelperTopics {
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
		"inbox list": {
			"thread-id": valueFlag,
			"type":      valueFlag,
			"full-id":   boolFlag,
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
		"cards reopen":      {"card-id": valueFlag},
		"threads message":   {"thread": valueFlag, "thread-id": valueFlag},
		"threads reply":     {"thread": valueFlag, "thread-id": valueFlag},
		"boards workspace":  {"board-id": valueFlag},
		"boards cards list": {"board-id": valueFlag},
		"workspace summary": {"full-id": boolFlag},
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
	expected := map[string]int{"show": 0, "workspaces": 0, "use": 1, "map": 2, "unmap": 1}
	for _, arg := range args[1:] {
		if strings.HasPrefix(arg, "-") {
			return errnorm.Usage("invalid_flags", "config commands accept positional arguments only")
		}
	}
	if len(args)-1 != expected[args[0]] {
		return errnorm.Usage("invalid_args", "use anx help config "+args[0]+" for command arguments")
	}
	return nil
}
