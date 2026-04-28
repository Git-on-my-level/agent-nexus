package app

import (
	"context"
	"fmt"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

const humanAttentionRequestedEventType = "human_attention_requested"

func humanUsageText() string {
	return strings.TrimSpace(`Local Help: human

Ask a human for attention. These commands create human Inbox items; agents do not read or manage Inbox directly.

Usage:
  anx human ask "<question>" --subject-ref <ref> [flags]
  anx human review "<request>" --subject-ref <ref> [flags]
  anx human escalate "<problem>" --subject-ref <ref> [flags]

Flags:
  --subject-ref <ref>         Subject typed ref for the human attention item.
  --thread-id <thread-id>     Backing thread id when subject_ref is not thread:<id>.
  --ref <typed-ref>           Additional related typed ref (repeatable).
  --body <text>               Optional detailed body.
  --body-file <path>          Read detailed body from a file.
  --title <text>              Title override; defaults to positional text.
  --request-id <id>           Optional stable request id for inbox identity.
  --requester-actor-id <id>   Override requester actor id (defaults to profile actor_id).
  --requester-agent-id <id>   Override requester agent id (defaults to profile agent id/name).
  --requester-label <text>    Override requester label shown to humans.
  --coverage-hint <text>      Ask-only context hint shown to humans.
  --severity <level>          Escalate-only severity: low, medium, high, critical.
  --actor-id <id|me>          Event author actor_id; defaults from active profile.

Examples:
  anx human ask "Which launch date should I use?" --subject-ref topic:launch
  anx human review "Please review the launch notes" --subject-ref document:launch_notes --ref artifact:draft
  anx human escalate "Possible leaked secret in logs" --subject-ref artifact:scan_result --severity high
`)
}

func (a *App) runHumanCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "human", errnorm.Usage("subcommand_required", "human requires ask, review, or escalate")
	}
	kind := strings.ToLower(strings.TrimSpace(args[0]))
	switch kind {
	case "ask", "review", "escalate":
		result, err := a.runHumanAttentionCommand(ctx, kind, args[1:], cfg)
		return result, "human " + kind, err
	default:
		return nil, "human", errnorm.Usage("unknown_command", fmt.Sprintf("unknown human subcommand %q", args[0]))
	}
}

func (a *App) runHumanAttentionCommand(ctx context.Context, kind string, args []string, cfg config.Resolved) (*commandResult, error) {
	leadingPositionals, flagArgs := splitLeadingPositionals(args)

	fs := newSilentFlagSet("human " + kind)
	var (
		threadIDFlag         trackedString
		subjectRefFlag       trackedString
		bodyFlag             trackedString
		bodyFileFlag         trackedString
		titleFlag            trackedString
		requestIDFlag        trackedString
		requesterActorIDFlag trackedString
		requesterAgentIDFlag trackedString
		requesterLabelFlag   trackedString
		coverageHintFlag     trackedString
		severityFlag         trackedString
		actorIDFlag          trackedString
		refFlags             trackedStrings
	)
	fs.Var(&threadIDFlag, "thread-id", "Backing thread id")
	fs.Var(&subjectRefFlag, "subject-ref", "Subject typed ref")
	fs.Var(&bodyFlag, "body", "Detailed body text")
	fs.Var(&bodyFileFlag, "body-file", "Read detailed body from file")
	fs.Var(&titleFlag, "title", "Title override")
	fs.Var(&requestIDFlag, "request-id", "Optional request identifier")
	fs.Var(&requesterActorIDFlag, "requester-actor-id", "Requester actor id")
	fs.Var(&requesterAgentIDFlag, "requester-agent-id", "Requester agent id")
	fs.Var(&requesterLabelFlag, "requester-label", "Requester label")
	fs.Var(&coverageHintFlag, "coverage-hint", "Ask coverage hint")
	fs.Var(&severityFlag, "severity", "Escalation severity")
	fs.Var(&actorIDFlag, "actor-id", "Actor id for the event author")
	fs.Var(&refFlags, "ref", "Additional related typed ref (repeatable)")
	if err := fs.Parse(flagArgs); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}

	trailingPositionals := fs.Args()
	title := strings.TrimSpace(titleFlag.value)
	if title == "" {
		titleTokens := append([]string{}, leadingPositionals...)
		titleTokens = append(titleTokens, trailingPositionals...)
		title = strings.TrimSpace(strings.Join(titleTokens, " "))
	}
	if title == "" {
		return nil, errnorm.Usage("invalid_request", "title/question text is required")
	}

	body := strings.TrimSpace(bodyFlag.value)
	if strings.TrimSpace(bodyFileFlag.value) != "" {
		content, err := a.readBodyInput(bodyFileFlag.value)
		if err != nil {
			return nil, err
		}
		body = strings.TrimSpace(string(content))
	}

	threadID := strings.TrimSpace(threadIDFlag.value)
	subjectRef := strings.TrimSpace(subjectRefFlag.value)
	if subjectRef == "" {
		return nil, errnorm.Usage("invalid_request", "--subject-ref is required")
	}
	if err := validateTypedRefShape(subjectRef); err != nil {
		return nil, err
	}
	if prefix, id, splitErr := splitTypedRef(subjectRef); splitErr == nil && prefix == "thread" && threadID == "" {
		threadID = strings.TrimSpace(id)
	}
	if err := validateID(threadID, "thread id"); err != nil {
		return nil, err
	}

	relatedRefs := normalizeStringFilters(refFlags.values)
	for _, ref := range relatedRefs {
		if err := validateTypedRef(ref); err != nil {
			return nil, errnorm.Usage("invalid_request", err.Error())
		}
	}
	refs := uniqueStringsInOrder(append([]string{"thread:" + threadID, subjectRef}, relatedRefs...))

	requesterActorID := firstNonEmpty(strings.TrimSpace(requesterActorIDFlag.value), strings.TrimSpace(cfg.ActorID))
	requesterAgentID := firstNonEmpty(strings.TrimSpace(requesterAgentIDFlag.value), strings.TrimSpace(cfg.AgentID), strings.TrimSpace(cfg.Agent))
	requesterLabel := firstNonEmpty(strings.TrimSpace(requesterLabelFlag.value), strings.TrimSpace(cfg.Agent), requesterAgentID, requesterActorID)

	payload := map[string]any{
		"kind":               kind,
		"title":              title,
		"subject_ref":        subjectRef,
		"related_refs":       relatedRefs,
		"requester_actor_id": requesterActorID,
		"requester_agent_id": requesterAgentID,
		"requester_label":    requesterLabel,
	}
	if body != "" {
		payload["body"] = body
	}
	if requestID := strings.TrimSpace(requestIDFlag.value); requestID != "" {
		if err := validateID(requestID, "request id"); err != nil {
			return nil, err
		}
		payload["request_id"] = requestID
	}
	if kind == "ask" {
		if coverageHint := strings.TrimSpace(coverageHintFlag.value); coverageHint != "" {
			payload["coverage_hint"] = coverageHint
		}
	}
	if kind == "escalate" {
		severity := strings.ToLower(strings.TrimSpace(severityFlag.value))
		if severity == "" {
			severity = "high"
		}
		if !isHumanEscalationSeverity(severity) {
			return nil, errnorm.Usage("invalid_request", "--severity must be low, medium, high, or critical")
		}
		payload["severity"] = severity
	}

	bodyMap := map[string]any{
		"event": map[string]any{
			"type":      humanAttentionRequestedEventType,
			"thread_id": threadID,
			"summary":   title,
			"refs":      refs,
			"payload":   payload,
			"provenance": map[string]any{
				"sources": []string{"actor_statement:cli.human"},
			},
		},
	}
	if actorIDFlag.set {
		bodyMap["actor_id"] = strings.TrimSpace(actorIDFlag.value)
	}
	if err := finalizeMutationActorID(bodyMap, cfg); err != nil {
		return nil, err
	}
	if err := validateEventsCreateInput(bodyMap, "human "+kind); err != nil {
		return nil, err
	}

	return a.invokeTypedJSON(ctx, cfg, "human "+kind, "events.create", nil, nil, bodyMap)
}

func isHumanEscalationSeverity(value string) bool {
	switch strings.TrimSpace(value) {
	case "low", "medium", "high", "critical":
		return true
	default:
		return false
	}
}

func splitTypedRef(ref string) (prefix string, id string, err error) {
	ref = strings.TrimSpace(ref)
	idx := strings.Index(ref, ":")
	if idx <= 0 || idx >= len(ref)-1 {
		return "", "", fmt.Errorf("typed ref %q must be in \"<prefix>:<value>\" form", ref)
	}
	return strings.TrimSpace(ref[:idx]), strings.TrimSpace(ref[idx+1:]), nil
}

func uniqueStringsInOrder(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func splitLeadingPositionals(args []string) (leading []string, rest []string) {
	leading = make([]string, 0, len(args))
	for idx, arg := range args {
		if strings.HasPrefix(strings.TrimSpace(arg), "-") {
			return leading, args[idx:]
		}
		leading = append(leading, arg)
	}
	return leading, []string{}
}
