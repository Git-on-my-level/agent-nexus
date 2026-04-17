package app

import (
	"context"
	"fmt"
	"strings"

	"organization-autorunner-cli/internal/config"
	"organization-autorunner-cli/internal/errnorm"
)

const agentAskRequestedEventType = "agent_ask_requested"

func askUsageText() string {
	return strings.TrimSpace(`Local Help: ask

Capture a human-needed question as an ask event so it appears in Inbox as an ASK item.

Usage:
  oar ask "<question>" --thread-id <thread-id> [flags]
  oar ask --question "<question>" --thread-id <thread-id> [flags]

Flags:
  --question <text>           Question text (alternative to positional question).
  --thread-id <thread-id>     Backing thread id (required unless --subject-ref is thread:<id>).
  --subject-ref <ref>         Subject typed ref for the ask (optional).
  --ref <typed-ref>           Additional related typed ref (repeatable).
  --coverage-hint <text>      Optional coverage hint shown in capture UI.
  --summary <text>            Optional event summary (defaults to question).
  --ask-id <id>               Optional stable ask id for inbox item identity.
  --asking-agent-id <id>      Override payload asking_agent_id (defaults to profile actor_id).
  --asking-session-id <id>    Override payload asking_session_id (defaults to active profile name).
  --actor-id <id|me>          Event author actor_id; defaults from active profile.

Examples:
  oar ask "Should we approve Friday launch?" --thread-id thread_launch
  oar ask --question "Can we deprecate this endpoint?" --thread-id thread_api --subject-ref topic:api
  oar ask "Need billing policy decision" --thread-id thread_billing --ref topic:billing --coverage-hint "thin - 1 artifact, 0 decisions"
`)
}

func (a *App) runAskCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	leadingPositionals, flagArgs := splitLeadingPositionals(args)

	fs := newSilentFlagSet("ask")
	var (
		threadIDFlag        trackedString
		subjectRefFlag      trackedString
		coverageHintFlag    trackedString
		askingAgentIDFlag   trackedString
		askingSessionIDFlag trackedString
		actorIDFlag         trackedString
		questionFlag        trackedString
		summaryFlag         trackedString
		askIDFlag           trackedString
		refFlags            trackedStrings
	)
	fs.Var(&threadIDFlag, "thread-id", "Backing thread id")
	fs.Var(&subjectRefFlag, "subject-ref", "Subject typed ref")
	fs.Var(&coverageHintFlag, "coverage-hint", "Coverage hint shown in capture UI")
	fs.Var(&askingAgentIDFlag, "asking-agent-id", "Asking agent id (defaults to active actor_id)")
	fs.Var(&askingSessionIDFlag, "asking-session-id", "Asking session id (defaults to active profile name)")
	fs.Var(&actorIDFlag, "actor-id", "Actor id for the event author")
	fs.Var(&questionFlag, "question", "Question text")
	fs.Var(&summaryFlag, "summary", "Event summary override")
	fs.Var(&askIDFlag, "ask-id", "Optional ask identifier")
	fs.Var(&refFlags, "ref", "Additional related typed ref (repeatable)")
	if err := fs.Parse(flagArgs); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}

	trailingPositionals := fs.Args()
	if strings.TrimSpace(questionFlag.value) != "" && (len(leadingPositionals) > 0 || len(trailingPositionals) > 0) {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments when --question is provided")
	}

	question := strings.TrimSpace(questionFlag.value)
	if question == "" {
		questionTokens := append([]string{}, leadingPositionals...)
		questionTokens = append(questionTokens, trailingPositionals...)
		question = strings.TrimSpace(strings.Join(questionTokens, " "))
	}
	if question == "" {
		return nil, errnorm.Usage("invalid_request", "question is required (positional or --question)")
	}

	threadID := strings.TrimSpace(threadIDFlag.value)
	subjectRef := strings.TrimSpace(subjectRefFlag.value)
	if subjectRef != "" {
		if err := validateTypedRefShape(subjectRef); err != nil {
			return nil, err
		}
		if prefix, id, splitErr := splitTypedRef(subjectRef); splitErr == nil && prefix == "thread" && threadID == "" {
			threadID = strings.TrimSpace(id)
		}
	}
	if err := validateID(threadID, "thread id"); err != nil {
		return nil, err
	}
	if subjectRef != "" {
		if prefix, id, splitErr := splitTypedRef(subjectRef); splitErr == nil && prefix == "thread" && strings.TrimSpace(id) != threadID {
			return nil, errnorm.Usage(
				"invalid_request",
				fmt.Sprintf("subject_ref %q conflicts with --thread-id %q", subjectRef, threadID),
			)
		}
	}

	relatedRefs := normalizeStringFilters(refFlags.values)
	for _, ref := range relatedRefs {
		if err := validateTypedRef(ref); err != nil {
			return nil, errnorm.Usage("invalid_request", err.Error())
		}
	}

	refs := uniqueStringsInOrder(append([]string{"thread:" + threadID}, append([]string{subjectRef}, relatedRefs...)...))

	summary := strings.TrimSpace(summaryFlag.value)
	if summary == "" {
		summary = question
	}

	askingAgentID := strings.TrimSpace(askingAgentIDFlag.value)
	if askingAgentID == "" {
		askingAgentID = strings.TrimSpace(cfg.ActorID)
	}
	if askingAgentID == "" {
		askingAgentID = strings.TrimSpace(cfg.Agent)
	}

	askingSessionID := strings.TrimSpace(askingSessionIDFlag.value)
	if askingSessionID == "" {
		askingSessionID = strings.TrimSpace(cfg.Agent)
	}
	if askingSessionID == "" {
		askingSessionID = strings.TrimSpace(cfg.AgentID)
	}

	payload := map[string]any{
		"query_text": question,
	}
	if askingAgentID != "" {
		payload["asking_agent_id"] = askingAgentID
	}
	if askingSessionID != "" {
		payload["asking_session_id"] = askingSessionID
	}
	if coverageHint := strings.TrimSpace(coverageHintFlag.value); coverageHint != "" {
		payload["coverage_hint"] = coverageHint
	}
	if subjectRef != "" {
		payload["subject_ref"] = subjectRef
	}
	if len(relatedRefs) > 0 {
		payload["related_refs"] = relatedRefs
	}
	if askID := strings.TrimSpace(askIDFlag.value); askID != "" {
		if err := validateID(askID, "ask id"); err != nil {
			return nil, err
		}
		payload["ask_id"] = askID
	}

	body := map[string]any{
		"event": map[string]any{
			"type":      agentAskRequestedEventType,
			"thread_id": threadID,
			"summary":   summary,
			"refs":      refs,
			"payload":   payload,
			"provenance": map[string]any{
				"sources": []string{"actor_statement:cli.ask"},
			},
		},
	}
	if actorIDFlag.set {
		body["actor_id"] = strings.TrimSpace(actorIDFlag.value)
	}
	if err := finalizeMutationActorID(body, cfg); err != nil {
		return nil, err
	}
	if err := validateEventsCreateInput(body, "ask"); err != nil {
		return nil, err
	}

	return a.invokeTypedJSON(ctx, cfg, "ask", "events.create", nil, nil, body)
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
