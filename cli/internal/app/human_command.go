package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"unicode/utf8"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/output"
	"agent-nexus-cli/runcontext"
)

const humanAttentionRequestedEventType = "human_attention_requested"

func humanUsageText() string {
	return strings.TrimSpace(`Write for a busy boss with no context: what is happening, who is affected, what you recommend and why, alternatives and what happens without an answer.
anx ask "Approve the deletion guard?" --situation "Deleted users' data can reappear." --impact "This breaks our deletion promise." --recommend "Check whether the user was deleted before writing." --why "Reuse the existing guard; about 3 extra reads per write." --if-no-answer "The cost change stays blocked until Friday."
Required by CLI lint: --situation, --impact, --recommend and --why. Optional: --alternative "Plain action=consequence" (repeatable), --if-no-answer, --details. Use plain sentences for --recommend and --alt answer buttons. Technical identifiers, code and internal history belong in collapsed --details and --evidence Label=URL.
Use a one-line question or decision near 80 characters (hard maximum 160); keep situation to one or two sentences, about 60 words.
anx ask --template decision > ask.md prints a filled neutral before/after example with YAML frontmatter; edit it, then use --from-file ask.md.
Before: "Approve PR123 existing-fence reuse?" / "Recovered c10041; reuse external_write_fence for monthly_usage_totals."
After: "Deleted users' usage data can reappear." Explain that background jobs recreate erased totals (privacy risk), recommend the existing user-deleted guard (about 3 extra reads per write), describe the slower alternative and say the cost change stays blocked until Friday without an answer.
ask|review|escalate share these flags. --preview prints Decide copy plus all lint results, completely locally and without creating anything (JSON includes preview, rendered and lint). --dry-run validates and prints the API request without sending it. --force --reason <explanation> records an authoring override; it cannot bypass hard limits.
Attach to --subject-ref card:<slug>, or the current card; otherwise create a ready card on the default board. --from-file reads YAML frontmatter: situation, impact, recommendation: {action, why}, alternatives: [{action, consequence}], if_no_answer, details, subject_ref and evidence. Body Markdown remains supported for historical asks.
Supply 1–6 distinct responses as answer buttons, at most 240 Unicode characters each. --by <when> sets the machine-readable deadline (date, RFC3339 or duration like 2d); --blocks <ref> names blocked work (repeatable, at most 20); --priority p0..p3 sets an optional badge. --supersedes event:<old-ask> replaces an old request.
Withdraw: anx ask withdraw <event:ask-id> --reason <text>.
Batch: anx ask batch --from-file asks.json files 2-10 related asks as one inbox item the human answers in a single pass; every ask passes the same authoring lint and shares one thread. The result lists each ask id with its delivery follow-up.
Delivery: --delivery await|resume|batch|none records how the answer reaches you (default: resume when the ask has executor wiring via --on-answer, ANX_RESUME_CMD or an agentctl continuation; else the workspace default from anx config ask-delivery; else batch). Every result prints the mode and the exact follow-up. resume registers an executor command fired by anx bridge run; set the bridge up once with anx bridge install. Await: anx await <ask-id> or anx await --answers. Read: anx inbox list --status answered; anx inbox read <ask-id>.
`)
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
	if hasAskTemplate(args) {
		if err := validateAskTemplate(args); err != nil {
			return nil, err
		}
		if !cfg.JSON {
			a.writeOutput(a.Stdout, askDecisionTemplate)
			return &commandResult{RawWritten: true}, nil
		}
		return &commandResult{Data: map[string]any{"text": askDecisionTemplate}}, nil
	}
	leadingPositionals, flagArgs := splitLeadingPositionals(args)

	fs := newSilentFlagSet(kind)
	var (
		topLevelFlag                                                    trackedBool
		underFlag                                                       trackedString
		threadIDFlag                                                    trackedString
		subjectRefFlag                                                  trackedString
		bodyFlag                                                        trackedString
		bodyFileFlag                                                    trackedString
		titleFlag                                                       trackedString
		requestIDFlag                                                   trackedString
		requesterActorIDFlag                                            trackedString
		requesterAgentIDFlag                                            trackedString
		requesterLabelFlag                                              trackedString
		coverageHintFlag                                                trackedString
		severityFlag                                                    trackedString
		actorIDFlag                                                     trackedString
		refFlags                                                        trackedStrings
		fromFileFlag                                                    trackedString
		recommendedResponseFlag                                         trackedString
		proposalFlags                                                   trackedStrings
		previewFlag                                                     trackedBool
		situationFlag, impactFlag, whyFlag, ifNoAnswerFlag, detailsFlag trackedString
		alternativeFlags                                                trackedStrings
		dryRunFlag                                                      trackedBool
		forceFlag                                                       trackedBool
		reasonFlag, supersedesFlag, onAnswerFlag, webhookFlag           trackedString
		deliveryFlag                                                    trackedString
		evidenceFlags                                                   trackedStrings
		byFlag, priorityFlag                                            trackedString
		blocksFlags                                                     trackedStrings
	)
	fs.Var(&previewFlag, "preview", "Show Decide copy and all lint results without any network calls or writes")
	fs.Var(&situationFlag, "situation", "What is happening in one or two plain sentences")
	fs.Var(&impactFlag, "impact", "Who or what is affected: users, money, risk or time")
	fs.Var(&whyFlag, "why", "Why the recommended action is best, including cost")
	fs.Var(&ifNoAnswerFlag, "if-no-answer", "Default or blocked work and by when")
	fs.Var(&detailsFlag, "details", "Technical Markdown, collapsed with evidence")
	fs.Var(&alternativeFlags, "alternative", "Action=consequence in plain words (repeatable)")
	fs.Var(&topLevelFlag, "top-level", "Ignore the run-label parent default for an auto-created subject")
	fs.Var(&underFlag, "under", "Parent for an auto-created subject card")
	fs.Var(&threadIDFlag, "thread-id", "Backing thread id")
	fs.Var(&byFlag, "by", "When the human should decide: YYYY-MM-DD, RFC3339, or a duration like 2d")
	fs.Var(&blocksFlags, "blocks", "Typed ref this decision blocks (repeatable, at most 20)")
	fs.Var(&priorityFlag, "priority", "Optional badge: p0..p3 (critical/high/medium/low are accepted and sent as p0..p3)")
	fs.Var(&subjectRefFlag, "subject-ref", "Initiative card ref (card:<slug>); omit only when the current card should own the ask")
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
	fs.Var(&forceFlag, "force", "Override authoring lint with --reason")
	fs.Var(&reasonFlag, "reason", "Recorded authoring override reason")
	fs.Var(&supersedesFlag, "supersedes", "Previous ask event ref")
	fs.Var(&evidenceFlags, "evidence", "Label=URL evidence (repeatable)")
	fs.Var(&onAnswerFlag, "on-answer", "Host-only command to resume on answer")
	fs.Var(&webhookFlag, "webhook", "HTTPS answer subscription URL")
	fs.Var(&deliveryFlag, "delivery", "Answer delivery mode: await, resume, batch, or none")
	fs.Var(&dryRunFlag, "dry-run", "Validate and render the request without sending it")
	if err := fs.Parse(flagArgs); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}

	// Go's flag parser stops at a trailing positional. Never let a misplaced
	// preview flag become title text and fall through to the mutation path.
	if hasAskPreview(args) && !previewFlag.value {
		return nil, errnorm.Usage("invalid_flags", "put the title before flags so --preview is parsed")
	}
	if previewFlag.value && (dryRunFlag.set || deliveryFlag.set || onAnswerFlag.set || webhookFlag.set) {
		return nil, errnorm.Usage("invalid_flags", "--preview cannot be combined with --dry-run, --delivery, --on-answer or --webhook; preview performs no delivery")
	}
	if topLevelFlag.value && underFlag.set {
		return nil, errnorm.Usage("invalid_request", "--top-level and --under cannot be combined")
	}
	ctx = runcontext.WithTopLevel(ctx, topLevelFlag.value)
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

	if fromFileFlag.set && (situationFlag.set || impactFlag.set || whyFlag.set || ifNoAnswerFlag.set || detailsFlag.set || len(alternativeFlags.values) > 0) {
		return nil, errnorm.Usage("invalid_request", "--from-file cannot be combined with structured authoring flags; put them in frontmatter")
	}
	var presentation askPresentation
	var proposalLintErr error
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
		fm, mdBody, proposals, err := a.loadHumanAttentionFromMarkdownFile(strings.TrimSpace(fromFileFlag.value), kind, previewFlag.value)
		if err != nil {
			return nil, err
		}
		fmFromFile = fm
		presentation = fm.askPresentation
		title = fm.Title
		body = strings.TrimSpace(mdBody)
		subjectRef = fm.SubjectRef
		threadID = fm.ThreadID
		relatedRefs = normalizeStringFilters(fm.Refs)
		responseProposals = proposals
		if previewFlag.value {
			_, proposalLintErr = buildCLIHumanAttentionResponseProposals(fm.RecommendedResponse, fm.Proposals)
		}
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
		presentation = askPresentation{Situation: strings.TrimSpace(situationFlag.value), Impact: strings.TrimSpace(impactFlag.value), Recommendation: &askRecommendation{Action: strings.TrimSpace(recommendedResponseFlag.value), Why: strings.TrimSpace(whyFlag.value)}, IfNoAnswer: strings.TrimSpace(ifNoAnswerFlag.value), Details: strings.TrimSpace(detailsFlag.value)}
		for _, raw := range alternativeFlags.values {
			action, consequence, ok := strings.Cut(raw, "=")
			if !ok || strings.TrimSpace(action) == "" || strings.TrimSpace(consequence) == "" {
				return nil, errnorm.Usage("invalid_request", "--alternative requires Action=consequence")
			}
			presentation.Alternatives = append(presentation.Alternatives, askAlternative{Action: strings.TrimSpace(action), Consequence: strings.TrimSpace(consequence)})
			proposalFlags.values = append(proposalFlags.values, strings.TrimSpace(action))
		}
		var err error
		responseProposals, err = buildCLIHumanAttentionResponseProposals(recommendedResponseFlag.value, proposalFlags.values)
		if err != nil && !previewFlag.value {
			return nil, err
		}
		if previewFlag.value {
			proposalLintErr = err
			if err != nil {
				responseProposals = nil
				for _, option := range append([]string{recommendedResponseFlag.value}, proposalFlags.values...) {
					if strings.TrimSpace(option) != "" {
						responseProposals = append(responseProposals, option)
					}
				}
			}
		}
	}

	if err := presentation.validateBounds(); err != nil {
		return nil, errnorm.Usage("invalid_request", err.Error())
	}
	links := append([]askEvidenceLink{}, fmFromFile.Evidence...)
	for _, raw := range evidenceFlags.values {
		label, u, ok := strings.Cut(raw, "=")
		if !ok {
			return nil, errnorm.Usage("invalid_request", "--evidence requires Label=URL")
		}
		links = append(links, askEvidenceLink{Label: label, URL: u})
	}
	if strings.ContainsAny(title, "\r\n") {
		return nil, errnorm.Usage("ask_title_invalid", "ask title must be one line; move detail into --body or --body-file ask.md")
	}
	if utf8.RuneCountInString(title) > 160 {
		return nil, errnorm.Usage("ask_title_too_long", "ask title exceeds the hard maximum of 160 characters; move detail into --body or --body-file ask.md")
	}
	decision, err := parseAskDecisionFlags(byFlag, blocksFlags.values, priorityFlag, a.clockNow())
	if err != nil {
		return nil, err
	}
	warnings, lintErr := lintAskAuthoring(strings.TrimSpace(body+"\n"+presentation.Details), relatedRefs, links, a.workspaceForges(cfg))
	plainWarnings, lintErrors := lintAskPresentation(title, presentation, responseProposals)
	warnings = append(warnings, plainWarnings...)
	if proposalLintErr != nil {
		lintErrors = append(lintErrors, proposalLintErr.Error())
	}
	if lintErr != nil {
		lintErrors = append(lintErrors, lintErr.Error())
	}
	if len(lintErrors) > 0 {
		lintErr = fmt.Errorf("%s", strings.Join(lintErrors, "; "))
	}
	if previewFlag.value {
		rendered := renderAskPreview(title, body, presentation, responseProposals, links)
		if !cfg.JSON {
			a.writeOutput(a.Stdout, rendered+"\nLint:\n")
			for _, e := range lintErrors {
				a.writeOutput(a.Stdout, "error: "+e+"\n")
			}
			for _, w := range warnings {
				a.writeOutput(a.Stdout, "warning: "+w+"\n")
			}
			if len(lintErrors) == 0 && len(warnings) == 0 {
				a.writeOutput(a.Stdout, "No issues.\n")
			}
			return &commandResult{RawWritten: true}, nil
		}
		preview := map[string]any{"title": title, "body": body, "response_proposals": responseProposals, "evidence": links}
		presentation.apply(preview)
		return &commandResult{Data: map[string]any{"preview": preview, "rendered": rendered, "lint": map[string]any{"errors": lintErrors, "warnings": warnings, "valid": len(lintErrors) == 0}}}, nil
	}
	if forceFlag.value && strings.TrimSpace(reasonFlag.value) == "" {
		return nil, errnorm.Usage("invalid_request", "--force requires --reason")
	}
	if lintErr != nil && !forceFlag.value {
		return nil, errnorm.Usage("ask_authoring", lintErr.Error()+"; or use --force --reason <explanation>")
	}
	var loopWarnings []output.Warning
	if kind == "ask" && decision.empty() {
		loopWarnings = append(loopWarnings, askWarning("ask_no_deadline", "no --by or --blocks; this sorts below every decision with a deadline or blocked work", nil))
	}
	autoSubject := false
	var autoHomes any
	var autoDefault any
	var autoGroupingActions []output.NextAction
	if subjectRef == "" {
		var err error
		subjectRef, err = a.currentCardRef(ctx, cfg)
		if err != nil {
			var typed *errnorm.Error
			if !errors.As(err, &typed) || typed.Code != "no_current_task" {
				return nil, err
			}
			input := map[string]any{"create_surface": "ask.auto_create", "title": title, "summary": body, "phase": "ready", "owner": requesterActorID}
			if board := strings.TrimSpace(a.Getenv("ANX_ASK_DEFAULT_BOARD")); board != "" {
				input["board_ref"] = board
			}
			if underFlag.set {
				input["under"] = underFlag.value
			}
			if dryRunFlag.value {
				hint := a.prepareRunParent(ctx, cfg, input, false)
				result := &commandResult{Data: map[string]any{"auto_create_subject": true, "title": title, "default_board": a.Getenv("ANX_ASK_DEFAULT_BOARD"), "body": body, "under": input["under"]}, Warnings: askAuthoringWarnings(warnings)}
				for _, warning := range hint.Warnings {
					result.Warnings = append(result.Warnings, output.Warning{Code: warning.Code, Message: warning.Message})
				}
				return result, nil
			}
			if e := ensureGroupingRequestKey(input); e != nil {
				return nil, e
			}
			created, e := a.invokeRawJSON(ctx, cfg, "ask subject", "POST", "/work", input)
			if e != nil {
				return nil, e
			}
			autoBody := commandResultBody(created)
			autoHomes = autoBody["likely_home"]
			autoDefault = autoBody["run_parent_default"]
			loopWarnings = append(loopWarnings, created.Warnings...)
			autoGroupingActions = append(autoGroupingActions, created.NextActions...)
			for _, raw := range asSlice(autoBody["next_actions"]) {
				if argv := stringList(asMap(raw)["argv"]); len(argv) > 0 {
					autoGroupingActions = append(autoGroupingActions, action("Group the new subject", argv...))
				}
			}
			work := asMap(autoBody["work"])
			subjectRef = firstNonEmpty(anyString(work["ref"]), anyString(work["card_ref"]))
			if subjectRef == "" {
				return nil, errnorm.Local("invalid_response", "created subject has no card ref")
			}
			autoSubject = true
			board := firstNonEmpty(anyString(work["board_ref"]), strings.TrimSpace(a.Getenv("ANX_ASK_DEFAULT_BOARD")))
			// One bounded work page. A failed read leaves the warning without suggestions.
			suggestions := a.suggestOpenInitiativeCards(ctx, cfg, subjectRef)
			loopWarnings = append(loopWarnings, askAutoCreatedCardWarning(subjectRef, board, suggestions))
		}
	}
	if !strings.HasPrefix(subjectRef, "card:") {
		return nil, errnorm.Usage("invalid_request", "asks require a card subject; put documents or topics in --ref evidence")
	}
	if err := validateTypedRefShape(subjectRef); err != nil {
		return nil, err
	}
	if prefix, id, splitErr := splitTypedRef(subjectRef); splitErr == nil && prefix == "thread" && threadID == "" {
		threadID = strings.TrimSpace(id)
	}
	var subjectBody map[string]any
	if threadID == "" {
		resolvedThreadID, body, err := a.resolveHumanAttentionSubject(ctx, cfg, subjectRef)
		if err != nil {
			return nil, err
		}
		threadID, subjectBody = resolvedThreadID, body
	}
	if !autoSubject && kind == "ask" {
		// Reuse the subject read that grounded the thread; an explicit
		// --thread-id skips it rather than adding a request.
		if moot := asMap(subjectBody["possibly_moot"]); moot["flagged"] == true {
			reason := firstNonEmpty(strings.TrimSpace(anyString(moot["reason"])), "its linked sources moved")
			loopWarnings = append(loopWarnings, askWarning("ask_overtaken_subject", reason+" since the subject last moved; if this decision is overtaken, run anx ask withdraw <ask-id> --reason overtaken", []string{"anx", "ask", "withdraw", "<ask-id>", "--reason", "overtaken"}))
		}
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
	presentation.apply(payload)
	payload["evidence"] = links
	payload["authoring_warnings"] = warnings
	if forceFlag.value {
		payload["authoring_override_reason"] = strings.TrimSpace(reasonFlag.value)
	}
	if supersedes := firstNonEmpty(supersedesFlag.value, fmFromFile.Supersedes); supersedes != "" {
		if !strings.HasPrefix(supersedes, "event:") {
			return nil, errnorm.Usage("invalid_request", "--supersedes requires event:<ask-id>")
		}
		payload["supersedes"] = supersedes
		refs = uniqueStringsInOrder(append(refs, supersedes))
	}
	if body != "" {
		payload["body"] = body
	}
	decision.apply(payload)
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
	// Allocate the local registry key before publishing the ask. The command
	// never enters event JSON or a request to the server.
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return nil, err
	}
	localAskID := "ev_" + hex.EncodeToString(random)
	delivery, err := a.resolveAskDelivery(cfg, askDeliveryInput{
		Explicit:     deliveryFlag.value,
		ExplicitSet:  deliveryFlag.set,
		OnAnswer:     onAnswerFlag.value,
		OnAnswerFlag: onAnswerFlag.set,
		AskID:        localAskID,
	})
	if err != nil {
		return nil, err
	}
	asMap(bodyMap["event"])["id"] = localAskID
	asMap(bodyMap["event"])["payload"].(map[string]any)["delivery"] = delivery.Mode
	if dryRunFlag.set && dryRunFlag.value {
		result := dryRunResult(kind, "events.create", nil, nil, bodyMap)
		result.Warnings = append(result.Warnings, askAuthoringWarnings(warnings)...)
		result.Warnings = append(result.Warnings, loopWarnings...)
		result.Warnings = append(result.Warnings, askDeliveryWarnings(delivery)...)
		if data, ok := result.Data.(map[string]any); ok {
			data["delivery"] = askDeliveryResultView(delivery, "event:"+localAskID)
		}
		return result, nil
	}
	command := delivery.Command
	registryPath := ""
	var registration askResumeRegistration
	if delivery.Mode == askDeliveryResume {
		dir, err := a.askRegistryDir(cfg)
		if err != nil {
			return nil, err
		}
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		registryPath = askRegistryPath(dir, "event:"+localAskID)
		registration = askResumeRegistration{AskID: "event:" + localAskID, BaseURL: cfg.BaseURL, ActorID: cfg.ActorID, Command: command, Dir: cwd, ThreadID: threadID, State: "pending"}
	}
	result, err := a.invokeTypedJSON(ctx, cfg, kind, "events.create", nil, nil, bodyMap)
	if err != nil {
		return nil, err
	}
	event := asMap(asMap(commandResultBody(result))["event"])
	askRef := ""
	if id := strings.TrimSpace(anyString(event["id"])); id != "" {
		askRef = "event:" + id
		if delivery.Mode == askDeliveryResume {
			registration.AskID = askRef
			registration.AskRef = anyString(event["ref"])
			if err = writeAskRegistration(registryPath, registration); err != nil {
				return nil, errnorm.WithDetails(errnorm.Local("registration_failed", "ask created; could not save local resume registration"), map[string]any{"ask_id": askRef})
			}
			_, e := a.invokeRawJSON(ctx, cfg, "ask subscribe", "POST", "/asks/"+url.PathEscape(id)+"/subscriptions", map[string]any{"kind": "bridge", "label": "host resume"})
			if e != nil {
				return nil, errnorm.WithDetails(errnorm.Local("subscription_failed", "ask was created but bridge subscription failed"), map[string]any{"ask_id": askRef})
			}
		}
		asMap(result.Data)["body"] = map[string]any{"ask_id": askRef, "subject_ref": subjectRef, "kind": kind, "event": event, "delivery": askDeliveryResultView(delivery, askRef)}
	}
	if webhookFlag.value != "" {
		id := anyString(event["id"])
		sub, e := a.invokeRawJSON(ctx, cfg, "ask webhook", "POST", "/asks/"+url.PathEscape(id)+"/subscriptions", map[string]any{"kind": "webhook", "label": "answer webhook", "url": webhookFlag.value})
		if e != nil {
			return nil, e
		}
		asMap(asMap(result.Data)["body"])["webhook"] = commandResultBody(sub)
	}
	for i := range loopWarnings {
		if loopWarnings[i].Code == "ask_no_deadline" {
			// Best effort: a warning never rejects a write, and a failed brief
			// read leaves the generic message.
			if count, atLeast, ok := a.briefDeadlineDecisions(ctx, cfg); ok && count > 0 {
				qualifier := ""
				if atLeast {
					qualifier = "at least "
				}
				loopWarnings[i].Message = fmt.Sprintf("no --by or --blocks; it will sort below %s%d decisions that have a deadline", qualifier, count)
			}
		}
		if askRef := anyString(asMap(asMap(result.Data)["body"])["ask_id"]); askRef != "" {
			loopWarnings[i].Message = strings.ReplaceAll(loopWarnings[i].Message, "<ask-id>", askRef)
			if details := asMap(loopWarnings[i].Details); details != nil {
				argv := stringList(details["suggest"])
				for j := range argv {
					if argv[j] == "<ask-id>" {
						argv[j] = askRef
					}
				}
				details["suggest"] = argv
			}
		}
	}
	for _, warning := range warnings {
		result.Warnings = append(result.Warnings, output.Warning{Code: "ask_authoring", Message: warning})
	}
	result.Warnings = append(result.Warnings, loopWarnings...)
	result.Warnings = append(result.Warnings, askDeliveryWarnings(delivery)...)
	if autoSubject {
		if body := commandResultBody(result); body != nil {
			body["likely_home"] = autoHomes
			if autoDefault != nil {
				body["run_parent_default"] = autoDefault
			}
		}
		result.NextActions = append(result.NextActions, autoGroupingActions...)
	}
	return result, nil
}

func (a *App) resolveHumanAttentionThreadIDFromSubjectRef(ctx context.Context, cfg config.Resolved, subjectRef string) (string, error) {
	threadID, _, err := a.resolveHumanAttentionSubject(ctx, cfg, subjectRef)
	return threadID, err
}

// resolveHumanAttentionSubject returns the subject's grounding thread and the
// subject body it read, so callers can reuse it (for example possibly_moot).
func (a *App) resolveHumanAttentionSubject(ctx context.Context, cfg config.Resolved, subjectRef string) (string, map[string]any, error) {
	prefix, _, err := splitTypedRef(subjectRef)
	if err != nil {
		return "", nil, err
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
		if isRemoteNotFound(err) {
			normalized := errnorm.Normalize(err)
			normalized.Message = "asks attach to a card in the same workspace; choose a same-workspace --subject-ref card:<slug>, or rerun with --workspace <alias> for the card's workspace"
			errnorm.AnnotateDetail(normalized, "requested_ref", subjectRef)
			errnorm.AnnotateDetail(normalized, "ask_subject", "card")
			err = normalized
		}
	case "document":
		body, err = a.fetchDocumentBody(ctx, cfg, subjectRef)
	default:
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	threadID := strings.TrimSpace(anyString(body["thread_id"]))
	if threadID == "" {
		return "", nil, errnorm.Usage("invalid_request", fmt.Sprintf("%s does not expose a backing thread_id; pass --thread-id explicitly", subjectRef))
	}
	return threadID, body, nil
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

func hasAskTemplate(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--template" || strings.HasPrefix(arg, "--template=") {
			return true
		}
		// String flag values can themselves be "--template". Leave unknown flags
		// to normal usage validation; the only boolean authoring flags are below.
		if strings.HasPrefix(arg, "--") && !strings.Contains(arg, "=") && arg != "--force" && arg != "--dry-run" && arg != "--preview" {
			i++
		}
	}
	return false
}
func validateAskTemplate(args []string) error {
	if (len(args) == 2 && args[0] == "--template" && args[1] == "decision") || (len(args) == 1 && args[0] == "--template=decision") {
		return nil
	}
	return errnorm.Usage("invalid_flags", "usage: anx ask --template decision (prints Markdown; cannot combine with publishing flags)")
}
func askAuthoringWarnings(warnings []string) []output.Warning {
	out := make([]output.Warning, 0, len(warnings))
	for _, warning := range warnings {
		out = append(out, output.Warning{Code: "ask_authoring", Message: warning})
	}
	return out
}

// briefDeadlineDecisions counts open decisions with a decide_by in one brief.v1
// read. atLeast is true when the brief's decisions section is truncated.
func (a *App) briefDeadlineDecisions(ctx context.Context, cfg config.Resolved) (count int, atLeast bool, ok bool) {
	result, err := a.invokeTypedJSON(ctx, cfg, "orient", "brief.get", nil, nil, nil)
	if err != nil {
		return 0, false, false
	}
	brief := commandResultBody(result)
	if brief == nil {
		return 0, false, false
	}
	for _, raw := range asSlice(brief["decisions"]) {
		if strings.TrimSpace(anyString(asMap(raw)["decide_by"])) != "" {
			count++
		}
	}
	truncated, _ := asMap(brief["truncated"])["decisions"].(bool)
	return count, truncated, true
}
