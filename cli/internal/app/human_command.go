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
Every request must include ordered response_proposals: --recommended-response plus up to five repeatable --proposal alternatives.

Usage:
  anx human ask "<question>" --subject-ref <ref> --recommended-response <text> [flags]
  anx human review "<request>" --subject-ref <ref> --recommended-response <text> [flags]
  anx human escalate "<problem>" --subject-ref <ref> --recommended-response <text> [flags]
  anx human <ask|review|escalate> --from-file <path.md> [--actor-id <id|me>]

Flags:
  --recommended-response <text> Required suggestion shown first (recommended). Max 240 characters after normalization.
  --proposal <text>             Optional alternative suggestion (repeatable, up to five). Same length limits as server.
  --from-file <path>            Markdown with YAML frontmatter; cannot be mixed with field-building flags or positional title.
  --subject-ref <ref>           Subject typed ref for the human attention item.
  --thread-id <thread-id>       Backing thread id when subject_ref is not thread:<id>.
  --ref <typed-ref>             Additional related typed ref (repeatable).
  --body <text>                 Optional detailed body.
  --body-file <path>            Read detailed body from a file.
  --title <text>                Title override; defaults to positional text.
  --request-id <id>             Optional stable request id for inbox identity.
  --requester-actor-id <id>     Override requester actor id (defaults to profile actor_id).
  --requester-agent-id <id>     Override requester agent id (defaults to profile agent id/name).
  --requester-label <text>      Override requester label shown to humans.
  --coverage-hint <text>        Ask-only context hint shown to humans.
  --severity <level>            Escalate-only severity: low, medium, high, critical (default high).
  --actor-id <id|me>            Event author actor_id; defaults from active profile.

Frontmatter (--from-file): required title, subject_ref, recommended_response; optional thread_id, refs, request_id,
requester_actor_id, requester_agent_id, requester_label, proposals (alternatives only), coverage_hint (ask), severity (escalate), kind (must match subcommand).

Examples:
  anx human ask "Which launch date?" --subject-ref topic:launch --recommended-response "Use May 15; avoids release freeze." --proposal "Wait for legal sign-off first."
  anx human review "Review launch notes" --subject-ref document:launch_notes --recommended-response "Approved; publish this version." --ref artifact:draft
  anx human escalate "Possible leaked secret" --subject-ref artifact:scan_result --recommended-response "Treat as incident; rotate keys." --severity high
  anx human ask --from-file launch-date.md
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
		return errnorm.Usage("invalid_request", "`anx human --from-file` does not accept positional title text; put the title in YAML frontmatter")
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
	add(recommendedResponseFlag.set, "--recommended-response")
	add(len(proposalFlags.values) > 0, "--proposal")
	add(len(refFlags.values) > 0, "--ref")
	if len(names) > 0 {
		return errnorm.Usage("invalid_request", fmt.Sprintf("`anx human --from-file` cannot be combined with %s", strings.Join(names, ", ")))
	}
	return nil
}

func (a *App) runHumanAttentionCommand(ctx context.Context, kind string, args []string, cfg config.Resolved) (*commandResult, error) {
	leadingPositionals, flagArgs := splitLeadingPositionals(args)

	fs := newSilentFlagSet("human " + kind)
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
	fs.Var(&recommendedResponseFlag, "recommended-response", "First (recommended) response proposal for operators")
	fs.Var(&proposalFlags, "proposal", "Additional response proposal (repeatable)")
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
	}

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

	if strings.TrimSpace(fromFileFlag.value) == "" {
		var err error
		responseProposals, err = buildCLIHumanAttentionResponseProposals(recommendedResponseFlag.value, proposalFlags.values)
		if err != nil {
			return nil, err
		}
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
				"sources": []string{"event:cli.human"},
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
