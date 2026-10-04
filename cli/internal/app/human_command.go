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
	return strings.TrimSpace(`Ask an operator: anx ask|review|escalate "<title>" --recommend "<answer>" [--alt "<other>"] [--subject-ref <ref>] [--dry-run].
Subject defaults to the current card. Use --from-file <path.md> for a Markdown request with frontmatter.
The recommended response and each alternative are trimmed, empty entries are dropped, and exact duplicates are removed. Supply 1–6 distinct responses; each may contain at most 240 Unicode characters.
Use --dry-run to validate and preview the request without sending it.
The result contains an ask id and a runnable anx await next action.`)
}

func rejectHumanFromFileFlagConflicts(
	fromFile trackedString,
	leadingPositionals, trailingPositionals []string,
	subjectRefFlag, threadIDFlag, bodyFlag, bodyFileFlag, titleFlag, requestIDFlag trackedString,
	requesterActorIDFlag, requesterAgentIDFlag, requesterLabelFlag trackedString,
	coverageHintFlag, severityFlag, recommendedResponseFlag trackedString,
	proposalFlags, refFlags trackedStrings,
) error {
	if !fromFile.set {
		return nil
	}
	if len(leadingPositionals) > 0 || len(trailingPositionals) > 0 {
		return errnorm.Usage("invalid_request", "--from-file does not accept positional title text; put the title in YAML frontmatter")
	}
	var names []string
	add := func(set bool, name string) {
		if set {
			names = append(names, name)
		}
	}
	add(subjectRefFlag.set, "--subject-ref")
	add(threadIDFlag.set, "--thread-id")
	add(bodyFlag.set, "--body")
	add(bodyFileFlag.set, "--body-file")
	add(titleFlag.set, "--title")
	add(requestIDFlag.set, "--request-id")
	add(requesterActorIDFlag.set, "--requester-actor-id")
	add(requesterAgentIDFlag.set, "--requester-agent-id")
	add(requesterLabelFlag.set, "--requester-label")
	add(coverageHintFlag.set, "--coverage-hint")
	add(severityFlag.set, "--severity")
	add(recommendedResponseFlag.set, "--recommend")
	add(len(proposalFlags.values) > 0, "--alt")
	add(len(refFlags.values) > 0, "--ref")
	if len(names) > 0 {
		return errnorm.Usage("invalid_request", fmt.Sprintf("--from-file cannot be combined with %s", strings.Join(names, ", ")))
	}
	return nil
}

func (a *App) runHumanAttentionCommand(ctx context.Context, kind string, args []string, cfg config.Resolved) (*commandResult, error) {
	leadingPositionals, flagArgs := splitLeadingPositionals(args)

	fs := newSilentFlagSet(kind)
	var (
		threadIDFlag            trackedString
		subjectRefFlag          trackedString
		bodyFlag                trackedString
		bodyFileFlag            trackedString
		titleFlag               trackedString
		requestIDFlag           trackedString
		requesterActorIDFlag    trackedString
		requesterAgentIDFlag    trackedString
		requesterLabelFlag      trackedString
		coverageHintFlag        trackedString
		severityFlag            trackedString
		actorIDFlag             trackedString
		refFlags                trackedStrings
		fromFileFlag            trackedString
		recommendedResponseFlag trackedString
		proposalFlags           trackedStrings
		dryRunFlag              trackedBool
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
	fs.Var(&fromFileFlag, "from-file", "Markdown file with YAML frontmatter for the human attention request")
	fs.Var(&recommendedResponseFlag, "recommend", "First (recommended) response proposal for operators")
	fs.Var(&proposalFlags, "alt", "Additional response proposal (repeatable)")
	fs.Var(&dryRunFlag, "dry-run", "Validate and render the request without sending it")
	if err := fs.Parse(flagArgs); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}

	trailingPositionals := fs.Args()
	if err := rejectHumanFromFileFlagConflicts(
		fromFileFlag,
		leadingPositionals,
		trailingPositionals,
		subjectRefFlag,
		threadIDFlag,
		bodyFlag,
		bodyFileFlag,
		titleFlag,
		requestIDFlag,
		requesterActorIDFlag,
		requesterAgentIDFlag,
		requesterLabelFlag,
		coverageHintFlag,
		severityFlag,
		recommendedResponseFlag,
		proposalFlags,
		refFlags,
	); err != nil {
		return nil, err
	}

	var (
		title             string
		body              string
		threadID          string
		subjectRef        string
		relatedRefs       []string
		responseProposals []any
		requestID         string
		requesterActorID  string
		requesterAgentID  string
		requesterLabel    string
		fmFromFile        humanAttentionFileFrontmatter
	)

	if strings.TrimSpace(fromFileFlag.value) != "" {
		fm, mdBody, proposals, err := loadHumanAttentionFromMarkdownFile(strings.TrimSpace(fromFileFlag.value), kind)
		if err != nil {
			return nil, err
		}
		fmFromFile = fm
		title = fm.Title
		body = strings.TrimSpace(mdBody)
		subjectRef = fm.SubjectRef
		threadID = fm.ThreadID
		relatedRefs = normalizeStringFilters(fm.Refs)
		responseProposals = proposals
		requestID = fm.RequestID
		requesterActorID = firstNonEmpty(fm.RequesterActorID, strings.TrimSpace(cfg.ActorID))
		requesterAgentID = firstNonEmpty(fm.RequesterAgentID, strings.TrimSpace(cfg.AgentID), strings.TrimSpace(cfg.Agent))
		requesterLabel = firstNonEmpty(fm.RequesterLabel, strings.TrimSpace(cfg.Agent), requesterAgentID, requesterActorID)
	} else {
		title = strings.TrimSpace(titleFlag.value)
		if title == "" {
			titleTokens := append([]string{}, leadingPositionals...)
			titleTokens = append(titleTokens, trailingPositionals...)
			title = strings.TrimSpace(strings.Join(titleTokens, " "))
		}
		if title == "" {
			return nil, errnorm.Usage("invalid_request", "title/question text is required")
		}

		body = strings.TrimSpace(bodyFlag.value)
		if strings.TrimSpace(bodyFileFlag.value) != "" {
			content, err := a.readBodyInput(bodyFileFlag.value)
			if err != nil {
				return nil, err
			}
			body = strings.TrimSpace(string(content))
		}

		threadID = strings.TrimSpace(threadIDFlag.value)
		subjectRef = strings.TrimSpace(subjectRefFlag.value)
		relatedRefs = normalizeStringFilters(refFlags.values)

		requesterActorID = firstNonEmpty(strings.TrimSpace(requesterActorIDFlag.value), strings.TrimSpace(cfg.ActorID))
		requesterAgentID = firstNonEmpty(strings.TrimSpace(requesterAgentIDFlag.value), strings.TrimSpace(cfg.AgentID), strings.TrimSpace(cfg.Agent))
		requesterLabel = firstNonEmpty(strings.TrimSpace(requesterLabelFlag.value), strings.TrimSpace(cfg.Agent), requesterAgentID, requesterActorID)
		requestID = strings.TrimSpace(requestIDFlag.value)
		var err error
		responseProposals, err = buildCLIHumanAttentionResponseProposals(recommendedResponseFlag.value, proposalFlags.values)
		if err != nil {
			return nil, err
		}
	}

	if subjectRef == "" {
		var err error
		subjectRef, err = a.currentCardRef(ctx, cfg)
		if err != nil {
			return nil, err
		}
	}
	if err := validateTypedRefShape(subjectRef); err != nil {
		return nil, err
	}
	if prefix, id, splitErr := splitTypedRef(subjectRef); splitErr == nil && prefix == "thread" && threadID == "" {
		threadID = strings.TrimSpace(id)
	}
	if threadID == "" {
		resolvedThreadID, err := a.resolveHumanAttentionThreadIDFromSubjectRef(ctx, cfg, subjectRef)
		if err != nil {
			return nil, err
		}
		threadID = resolvedThreadID
	}
	if err := validateID(threadID, "thread id"); err != nil {
		return nil, err
	}

	for _, ref := range relatedRefs {
		if err := validateTypedRef(ref); err != nil {
			return nil, errnorm.Usage("invalid_request", err.Error())
		}
	}
	refs := uniqueStringsInOrder(append([]string{"thread:" + threadID, subjectRef}, relatedRefs...))

	payload := map[string]any{
		"kind":               kind,
		"title":              title,
		"subject_ref":        subjectRef,
		"related_refs":       relatedRefs,
		"requester_actor_id": requesterActorID,
		"requester_agent_id": requesterAgentID,
		"requester_label":    requesterLabel,
		"response_proposals": responseProposals,
	}
	if body != "" {
		payload["body"] = body
	}
	if requestID != "" {
		if err := validateID(requestID, "request id"); err != nil {
			return nil, err
		}
		payload["request_id"] = requestID
	}
	if kind == "ask" {
		coverageHint := strings.TrimSpace(coverageHintFlag.value)
		if strings.TrimSpace(fromFileFlag.value) != "" {
			coverageHint = strings.TrimSpace(fmFromFile.CoverageHint)
		}
		if coverageHint != "" {
			payload["coverage_hint"] = coverageHint
		}
	}
	if kind == "escalate" {
		severity := strings.ToLower(strings.TrimSpace(severityFlag.value))
		if strings.TrimSpace(fromFileFlag.value) != "" {
			severity = strings.ToLower(strings.TrimSpace(fmFromFile.Severity))
		}
		if severity == "" {
			severity = "high"
		}
		if !isHumanEscalationSeverity(severity) {
			return nil, errnorm.Usage("invalid_request", "severity must be low, medium, high, or critical")
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
				"sources": []string{"event:cli.ask"},
			},
		},
	}
	if actorIDFlag.set {
		bodyMap["actor_id"] = strings.TrimSpace(actorIDFlag.value)
	}
	if err := finalizeMutationActorID(bodyMap, cfg); err != nil {
		return nil, err
	}
	if err := validateEventsCreateInput(bodyMap, kind); err != nil {
		return nil, err
	}
	if dryRunFlag.set && dryRunFlag.value {
		return dryRunResult(kind, "events.create", nil, nil, bodyMap), nil
	}
	result, err := a.invokeTypedJSON(ctx, cfg, kind, "events.create", nil, nil, bodyMap)
	if err != nil {
		return nil, err
	}
	event := asMap(asMap(commandResultBody(result))["event"])
	if id := strings.TrimSpace(anyString(event["id"])); id != "" {
		asMap(result.Data)["body"] = map[string]any{"ask_id": "event:" + id, "subject_ref": subjectRef, "kind": kind, "event": event}
	}
	return result, nil
}

func (a *App) resolveHumanAttentionThreadIDFromSubjectRef(ctx context.Context, cfg config.Resolved, subjectRef string) (string, error) {
	prefix, _, err := splitTypedRef(subjectRef)
	if err != nil {
		return "", err
	}
	// Every thread-backed subject resolves its own grounding thread. Asking an
	// agent to discover a thread id in order to ask a human about a Task or a
	// Doc would push a purely internal noun into the conversation — the whole
	// point of these commands is that the agent names the subject an operator
	// can see and the CLI grounds it.
	var body map[string]any
	switch strings.TrimSpace(prefix) {
	case "topic":
		body, err = a.fetchTopicBody(ctx, cfg, subjectRef)
	case "card":
		body, err = a.fetchCardBody(ctx, cfg, subjectRef)
	case "document":
		body, err = a.fetchDocumentBody(ctx, cfg, subjectRef)
	default:
		return "", nil
	}
	if err != nil {
		return "", err
	}
	threadID := strings.TrimSpace(anyString(body["thread_id"]))
	if threadID == "" {
		return "", errnorm.Usage("invalid_request", fmt.Sprintf("%s does not expose a backing thread_id; pass --thread-id explicitly", subjectRef))
	}
	return threadID, nil
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
