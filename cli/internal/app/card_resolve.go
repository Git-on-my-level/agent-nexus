// `anx cards resolve` input planning and evidence posting.

package app

import (
	"context"
	"fmt"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

type cardResolvePlan struct {
	cardID               string
	moveBody             map[string]any
	resolutionRefs       []string
	evidenceBody         string
	evidenceSummary      string
	evidenceActorIDRaw   string
	hasEvidenceMessage   bool
	dryRun               bool
	dryRunFlagOverwrites map[string]any
}

func (a *App) parseCardResolveInput(args []string, cfg config.Resolved, commandName string) (cardResolvePlan, error) {
	leadingCardID, args := popLeadingPositional(args)
	fs := newSilentFlagSet(commandName)
	var cardIDFlag, columnFlag, resolutionFlag, ifBoardUpdatedAtFlag, actorIDFlag, bodyFlag, bodyFileFlag, reasonFlag, summaryFlag, fromFileFlag trackedString
	var resolutionRefFlags trackedStrings
	var dryRunFlag trackedBool
	fs.Var(&cardIDFlag, "card-id", "Card ref, handle, or id")
	fs.Var(&columnFlag, "column", "Target board column key; defaults to done")
	fs.Var(&resolutionFlag, "resolution", "Resolution value: done (abandon work with trash, not resolution)")
	fs.Var(&resolutionRefFlags, "resolution-ref", "Evidence event/artifact typed ref (repeatable)")
	fs.Var(&reasonFlag, "reason", "Short audit string stamped on the resolve request (not posted to the backing thread)")
	fs.Var(&bodyFlag, "body", "Evidence body posted to the card thread before resolving")
	fs.Var(&bodyFileFlag, "body-file", "Load evidence body text from a local file or stdin with - before resolving")
	fs.Var(&summaryFlag, "summary", "Optional short evidence event summary")
	fs.Var(&ifBoardUpdatedAtFlag, "if-board-updated-at", "Board updated_at concurrency token; use boards get for the parent board when omitted")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	fs.Var(&fromFileFlag, "from-file", "Advanced JSON move request body from file or stdin with -")
	fs.Var(&dryRunFlag, "dry-run", "Validate and render request without sending the mutation")
	if err := fs.Parse(args); err != nil {
		return cardResolvePlan{}, errnorm.Usage("invalid_flags", err.Error())
	}
	cardID, err := parseCardIDFromFlagOrPositionals(firstNonEmpty(cardIDFlag.value, leadingCardID), fs.Args(), commandName)
	if err != nil {
		return cardResolvePlan{}, err
	}
	dryRun := dryRunFlag.set && dryRunFlag.value
	column := firstNonEmpty(strings.TrimSpace(columnFlag.value), "done")
	resolution := firstNonEmpty(strings.TrimSpace(resolutionFlag.value), "done")
	refs := normalizeStringFilters(resolutionRefFlags.values)
	moveBody := map[string]any{
		"column_key":      column,
		"resolution":      resolution,
		"resolution_refs": refs,
	}
	var dryRunFlagOverwrites map[string]any
	recordOverwrite := func(bodyMap map[string]any, overrides map[string]any, key string, newVal any) map[string]any {
		if prev, ok := bodyMap[key]; ok && strings.TrimSpace(anyString(prev)) != "" {
			if overrides == nil {
				overrides = map[string]any{}
			}
			overrides[key] = newVal
		}
		return overrides
	}
	if strings.TrimSpace(fromFileFlag.value) != "" {
		payload, err := a.readBodyInput(strings.TrimSpace(fromFileFlag.value))
		if err != nil {
			return cardResolvePlan{}, err
		}
		decoded, err := decodeJSONPayload(payload)
		if err != nil {
			return cardResolvePlan{}, err
		}
		bodyMap, ok := decoded.(map[string]any)
		if !ok {
			return cardResolvePlan{}, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body for `anx %s` must be an object", commandName))
		}
		moveBody = bodyMap
		if strings.TrimSpace(columnFlag.value) != "" {
			dryRunFlagOverwrites = recordOverwrite(moveBody, dryRunFlagOverwrites, "column_key", column)
			moveBody["column_key"] = column
		} else if strings.TrimSpace(anyString(moveBody["column_key"])) == "" {
			moveBody["column_key"] = "done"
		}
		if strings.TrimSpace(resolutionFlag.value) != "" {
			dryRunFlagOverwrites = recordOverwrite(moveBody, dryRunFlagOverwrites, "resolution", resolution)
			moveBody["resolution"] = resolution
		} else if strings.TrimSpace(anyString(moveBody["resolution"])) == "" {
			moveBody["resolution"] = "done"
		}
		refs = normalizeStringFilters(append(stringList(moveBody["resolution_refs"]), resolutionRefFlags.values...))
		moveBody["resolution_refs"] = refs
	}
	if ifBoardUpdatedAt := strings.TrimSpace(ifBoardUpdatedAtFlag.value); ifBoardUpdatedAt != "" {
		dryRunFlagOverwrites = recordOverwrite(moveBody, dryRunFlagOverwrites, "if_board_updated_at", ifBoardUpdatedAt)
		moveBody["if_board_updated_at"] = ifBoardUpdatedAt
	}
	actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
	if err != nil {
		return cardResolvePlan{}, err
	}
	if actorID != "" {
		dryRunFlagOverwrites = recordOverwrite(moveBody, dryRunFlagOverwrites, "actor_id", actorID)
		moveBody["actor_id"] = actorID
	}
	reason := strings.TrimSpace(reasonFlag.value)
	bodyInline := strings.TrimSpace(bodyFlag.value)
	bodyFile := strings.TrimSpace(bodyFileFlag.value)
	hasBodyInput := bodyInline != "" || bodyFile != ""
	if bodyInline != "" && bodyFile != "" {
		return cardResolvePlan{}, errnorm.Usage("invalid_request", "`--body` and `--body-file` cannot be combined for `anx cards resolve`")
	}
	refs = normalizeStringFilters(stringList(moveBody["resolution_refs"]))
	moveBody["resolution_refs"] = refs
	hasResolutionRefs := len(refs) > 0
	hasAuditReason := reason != ""
	if !hasAuditReason && !hasBodyInput && !hasResolutionRefs {
		return cardResolvePlan{}, errnorm.Usage("invalid_request", "`anx cards resolve` requires at least one of `--reason`, `--body`, `--body-file`, or `--resolution-ref`")
	}
	if !hasBodyInput && !hasResolutionRefs {
		return cardResolvePlan{}, errnorm.Usage("invalid_request", "`anx cards resolve` requires typed resolution evidence: use `--body`, `--body-file`, or `--resolution-ref` (optional `--reason` stamps the lifecycle audit only)")
	}
	if hasAuditReason {
		moveBody["reason"] = reason
	}
	var evidenceBody string
	hasEvidenceMessage := hasBodyInput
	if hasBodyInput {
		evidenceBody, err = a.readExplicitMessageText(bodyFlag.value, bodyFileFlag.value, commandName)
		if err != nil {
			return cardResolvePlan{}, err
		}
	}
	evidenceActorRaw := strings.TrimSpace(actorIDFlag.value)
	if evidenceActorRaw == "" {
		evidenceActorRaw = strings.TrimSpace(anyString(moveBody["actor_id"]))
	}
	return cardResolvePlan{
		cardID:               cardID,
		moveBody:             moveBody,
		resolutionRefs:       refs,
		evidenceBody:         evidenceBody,
		evidenceSummary:      summaryFlag.value,
		evidenceActorIDRaw:   evidenceActorRaw,
		hasEvidenceMessage:   hasEvidenceMessage,
		dryRun:               dryRun,
		dryRunFlagOverwrites: dryRunFlagOverwrites,
	}, nil
}

func (a *App) postCardResolveEvidence(ctx context.Context, cfg config.Resolved, plan cardResolvePlan) (string, error) {
	card, err := a.fetchCardBody(ctx, cfg, plan.cardID)
	if err != nil {
		return "", err
	}
	target, err := cardMessageTarget(card, plan.cardID)
	if err != nil {
		return "", err
	}
	body, err := buildMessagePostedBody(cfg, plan.evidenceActorIDRaw, target, plan.evidenceBody, plan.evidenceSummary, nil, "")
	if err != nil {
		return "", err
	}
	if err := validateEventsCreateInput(body, "cards resolve"); err != nil {
		return "", err
	}
	result, err := a.invokeTypedJSON(ctx, cfg, "cards resolve", "events.create", nil, nil, body)
	if err != nil {
		return "", err
	}
	event := extractNestedMap(extractNestedMap(asMap(result.Data), "body"), "event")
	eventID := strings.TrimSpace(anyString(event["id"]))
	if eventID == "" {
		return "", errnorm.Usage("invalid_response", "cards resolve evidence event response did not include event id")
	}
	return "event:" + eventID, nil
}
