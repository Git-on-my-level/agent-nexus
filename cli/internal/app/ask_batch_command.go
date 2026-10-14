package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/output"
	"agent-nexus-cli/internal/workspaceconfig"
)

// askBatchUsageText documents the filing-batch surface: N related asks, one
// human decision pass.
func askBatchUsageText() string {
	return strings.TrimSpace(`anx ask batch --from-file asks.json
Files 2-10 related asks as one batch. The human inbox shows ONE item with one decision section per ask and a single submit; every answer records atomically and releases together to each ask's delivery mode.
asks.json holds {"asks":[...]} where each entry accepts title (required), body, subject_ref, thread_id, refs, recommend (required), alts, request_id, coverage_hint, evidence [{label,url}], supersedes, and a per-ask delivery override. A top-level "delivery" is the default mode. "-" reads the JSON from stdin.
Every ask passes the same authoring lint as ` + "`anx ask`" + ` (--force --reason overrides it for the whole batch), and all asks must share one backing thread.
Follow-up: anx inbox list --status answered, or anx await --answers for batch delivery.
`)
}

type askBatchEntry struct {
	Title        string            `json:"title"`
	Body         string            `json:"body"`
	SubjectRef   string            `json:"subject_ref"`
	ThreadID     string            `json:"thread_id"`
	Refs         []string          `json:"refs"`
	Recommend    string            `json:"recommend"`
	Alts         []string          `json:"alts"`
	RequestID    string            `json:"request_id"`
	CoverageHint string            `json:"coverage_hint"`
	Evidence     []askEvidenceLink `json:"evidence"`
	Supersedes   string            `json:"supersedes"`
	Delivery     string            `json:"delivery"`
	OnAnswer     string            `json:"on_answer"`
	By           string            `json:"by"`
	Blocks       []string          `json:"blocks"`
	Priority     string            `json:"priority"`
	decision     askDecision

	proposals      []any               `json:"-"`
	decided        askDeliveryDecision `json:"-"`
	eventID        string              `json:"-"`
	thread         string              `json:"-"`
	subject        string              `json:"-"`
	requesterActor string              `json:"-"`
	requesterLabel string              `json:"-"`
	overrideReason string              `json:"-"`
	lintWarnings   []string            `json:"-"`
}

// runAskBatch files 2..10 related asks as one atomic batch through
// POST /asks/batch. Per-request cost is bounded by the batch cap (10).
func (a *App) runAskBatch(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("ask batch")
	var (
		fromFileFlag trackedString
		deliveryFlag trackedString
		forceFlag    trackedBool
		reasonFlag   trackedString
		dryRunFlag   trackedBool
		actorIDFlag  trackedString
		onAnswerFlag trackedString
	)
	fs.Var(&fromFileFlag, "from-file", "JSON file (or - for stdin) with {\"asks\":[...]}")
	fs.Var(&deliveryFlag, "delivery", "Default answer delivery mode: await, resume, batch, or none")
	fs.Var(&onAnswerFlag, "on-answer", "Default host-only resume command for members without their own")
	fs.Var(&forceFlag, "force", "Override authoring lint for every member with --reason")
	fs.Var(&reasonFlag, "reason", "Recorded authoring override reason")
	fs.Var(&actorIDFlag, "actor-id", "Actor id for the batch author")
	fs.Var(&dryRunFlag, "dry-run", "Validate and render the batch without sending it")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_request", "ask batch takes its asks from --from-file; positional text belongs to individual asks")
	}
	if strings.TrimSpace(fromFileFlag.value) == "" {
		return nil, errnorm.Usage("invalid_request", "--from-file <asks.json> is required (use - for stdin)")
	}
	if forceFlag.value && strings.TrimSpace(reasonFlag.value) == "" {
		return nil, errnorm.Usage("invalid_request", "--force requires --reason")
	}

	raw, err := a.readBodyInput(strings.TrimSpace(fromFileFlag.value))
	if err != nil {
		return nil, err
	}
	decoded, err := decodeJSONPayload(raw)
	if err != nil {
		return nil, errnorm.Usage("invalid_request", "--from-file must hold JSON: "+err.Error())
	}
	document, _ := decoded.(map[string]any)
	if document == nil {
		return nil, errnorm.Usage("invalid_request", "--from-file JSON must be an object with an asks array")
	}
	rawAsks, _ := document["asks"].([]any)
	if len(rawAsks) == 0 {
		return nil, errnorm.Usage("invalid_request", "asks must contain 2 to 10 ask entries")
	}
	if len(rawAsks) < askBatchMinMembers || len(rawAsks) > askBatchMaxMembers {
		return nil, errnorm.Usage("invalid_request", fmt.Sprintf("asks must contain between %d and %d ask entries (a single ask belongs to anx ask)", askBatchMinMembers, askBatchMaxMembers))
	}
	batchDefaultDelivery, _ := document["delivery"].(string)

	entries := make([]askBatchEntry, 0, len(rawAsks))
	for index, rawAsk := range rawAsks {
		entry, err := decodeAskBatchEntry(rawAsk, index)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}

	// One random id per member before delivery resolution and publishing:
	// resume commands embed the real ask id in their request keys, exactly
	// like the single-ask path.
	for index := range entries {
		random := make([]byte, 16)
		if _, err := rand.Read(random); err != nil {
			return nil, errnorm.Local("request_key_failed", "failed to allocate ask id")
		}
		entries[index].eventID = "ev_" + hex.EncodeToString(random)
	}

	forges := a.workspaceForges(cfg)
	sharedThread := ""
	threadCache := map[string]string{}
	for index := range entries {
		entry := &entries[index]
		if err := a.prepareAskBatchEntry(cfg, entry, index, batchDefaultDelivery, deliveryFlag, onAnswerFlag, forceFlag, reasonFlag, threadCache, forges); err != nil {
			return nil, err
		}
		if sharedThread == "" {
			sharedThread = entry.thread
		} else if entry.thread != sharedThread {
			return nil, errnorm.Usage("invalid_request", fmt.Sprintf("asks[%d] lands on thread %s but the batch is grounded on %s; every ask in a batch must share one thread", index, entry.thread, sharedThread))
		}
	}

	members := make([]map[string]any, 0, len(entries))
	for index := range entries {
		members = append(members, a.buildAskBatchMember(&entries[index]))
	}
	bodyMap := map[string]any{"asks": members}
	if actorIDFlag.set {
		bodyMap["actor_id"] = strings.TrimSpace(actorIDFlag.value)
	}
	if err := finalizeMutationActorID(bodyMap, cfg); err != nil {
		return nil, err
	}

	if dryRunFlag.set && dryRunFlag.value {
		result := dryRunResult("ask batch", "asks.batch.create", nil, nil, bodyMap)
		if data, ok := result.Data.(map[string]any); ok {
			data["batch_size"] = len(entries)
		}
		result.Warnings = append(result.Warnings, askBatchWarnings(entries)...)
		return result, nil
	}

	result, err := a.invokeTypedJSON(ctx, cfg, "ask batch", "asks.batch.create", nil, nil, bodyMap)
	if err != nil {
		return nil, err
	}
	body := commandResultBody(result)
	batchID := anyString(body["batch_id"])
	askViews := make([]map[string]any, 0, len(entries))
	registrationFailures := 0
	for index := range entries {
		entry := &entries[index]
		askRef := "event:" + entry.eventID
		askView := map[string]any{
			"ask_id":      askRef,
			"title":       entry.Title,
			"subject_ref": entry.subject,
			"delivery":    askDeliveryResultView(entry.decided, askRef),
		}
		if entry.decided.Mode == askDeliveryResume {
			if err := a.registerAskBatchResume(ctx, cfg, entry, askRef); err != nil {
				registrationFailures++
				askView["registration_error"] = err.Error()
			}
		}
		askViews = append(askViews, askView)
	}
	if len(askViews) != len(entries) {
		return nil, errnorm.Local("invalid_response", "ask batch result is incomplete")
	}
	asMap(result.Data)["body"] = map[string]any{
		"batch_id":  batchID,
		"ask_count": len(askViews),
		"asks":      askViews,
	}
	result.Warnings = append(result.Warnings, askBatchWarnings(entries)...)
	if registrationFailures > 0 {
		result.Warnings = append(result.Warnings, output.Warning{
			Code:    "registration_failed",
			Message: fmt.Sprintf("%d ask(s) were created but their resume registration failed; delivery falls back to anx inbox list --status answered", registrationFailures),
		})
	}
	return result, nil
}

const askBatchMinMembers = 2
const askBatchMaxMembers = 10

func decodeAskBatchEntry(raw any, index int) (askBatchEntry, error) {
	invalid := func(message string) (askBatchEntry, error) {
		return askBatchEntry{}, errnorm.Usage("invalid_request", fmt.Sprintf("asks[%d]: %s", index, message))
	}
	entry := askBatchEntry{}
	object, ok := raw.(map[string]any)
	if !ok {
		return invalid("each entry must be an object")
	}
	entry.Title = strings.TrimSpace(anyString(object["title"]))
	entry.Body = strings.TrimSpace(anyString(object["body"]))
	entry.SubjectRef = strings.TrimSpace(anyString(object["subject_ref"]))
	entry.ThreadID = strings.TrimSpace(anyString(object["thread_id"]))
	entry.Recommend = strings.TrimSpace(anyString(object["recommend"]))
	entry.RequestID = strings.TrimSpace(anyString(object["request_id"]))
	entry.CoverageHint = strings.TrimSpace(anyString(object["coverage_hint"]))
	entry.Supersedes = strings.TrimSpace(anyString(object["supersedes"]))
	entry.Delivery = strings.TrimSpace(anyString(object["delivery"]))
	entry.OnAnswer = strings.TrimSpace(anyString(object["on_answer"]))
	if rawRefs, ok := object["refs"].([]any); ok {
		for _, value := range rawRefs {
			if ref := strings.TrimSpace(anyString(value)); ref != "" {
				entry.Refs = append(entry.Refs, ref)
			}
		}
	}
	if rawAlts, ok := object["alts"].([]any); ok {
		for _, value := range rawAlts {
			if alt := strings.TrimSpace(anyString(value)); alt != "" {
				entry.Alts = append(entry.Alts, alt)
			}
		}
	}
	if rawEvidence, ok := object["evidence"].([]any); ok {
		for _, value := range rawEvidence {
			link, ok := value.(map[string]any)
			if !ok {
				return invalid("evidence entries must be objects with label and url")
			}
			entry.Evidence = append(entry.Evidence, askEvidenceLink{Label: anyString(link["label"]), URL: anyString(link["url"])})
		}
	}
	entry.By = strings.TrimSpace(anyString(object["by"]))
	entry.Priority = strings.TrimSpace(anyString(object["priority"]))
	if rawBlocks, ok := object["blocks"].([]any); ok {
		for _, value := range rawBlocks {
			entry.Blocks = append(entry.Blocks, anyString(value))
		}
	}
	decision, err := parseAskDecisionFlags(trackedString{value: entry.By, set: entry.By != ""}, entry.Blocks, trackedString{value: entry.Priority, set: entry.Priority != ""}, time.Now())
	if err != nil {
		return invalid(err.Error())
	}
	entry.decision = decision
	return entry, nil
}

// prepareAskBatchEntry applies the same per-ask authoring rules as `anx ask`:
// title rules, authoring lint, card subject, thread grounding and delivery
// resolution. Shared state (threads, threads cache) keeps cost bounded by the
// batch size.
func (a *App) prepareAskBatchEntry(
	cfg config.Resolved,
	entry *askBatchEntry,
	index int,
	batchDefaultDelivery string,
	deliveryFlag trackedString,
	onAnswerFlag trackedString,
	forceFlag trackedBool,
	reasonFlag trackedString,
	threadCache map[string]string,
	forges []workspaceconfig.Forge,
) error {
	if entry.Title == "" {
		return errnorm.Usage("invalid_request", fmt.Sprintf("asks[%d].title is required", index))
	}
	if strings.ContainsAny(entry.Title, "\r\n") {
		return errnorm.Usage("ask_title_invalid", fmt.Sprintf("asks[%d].title must be one line; move detail into body", index))
	}
	if utf8.RuneCountInString(entry.Title) > 160 {
		return errnorm.Usage("ask_title_too_long", fmt.Sprintf("asks[%d].title exceeds the hard maximum of 160 characters", index))
	}
	for _, ref := range entry.Refs {
		if err := validateTypedRef(ref); err != nil {
			return errnorm.Usage("invalid_request", fmt.Sprintf("asks[%d]: %v", index, err))
		}
	}
	warnings, lintErr := lintAskAuthoring(entry.Body, entry.Refs, entry.Evidence, forges)
	if titleErr := lintAskTitle(entry.Title); titleErr != nil {
		lintErr = titleErr
	}
	if lintErr != nil && !forceFlag.value {
		return errnorm.Usage("ask_authoring", fmt.Sprintf("asks[%d]: %s; or use --force --reason <explanation>", index, lintErr.Error()))
	}
	entry.lintWarnings = warnings

	if entry.SubjectRef == "" {
		return errnorm.Usage("invalid_request", fmt.Sprintf("asks[%d].subject_ref is required; ask batch always names its subject", index))
	}
	if !strings.HasPrefix(entry.SubjectRef, "card:") {
		return errnorm.Usage("invalid_request", fmt.Sprintf("asks[%d].subject_ref must be card:<ref>; put documents or topics in refs evidence", index))
	}
	if err := validateTypedRefShape(entry.SubjectRef); err != nil {
		return errnorm.Usage("invalid_request", fmt.Sprintf("asks[%d]: %v", index, err))
	}
	entry.subject = entry.SubjectRef
	entry.requesterActor = strings.TrimSpace(cfg.ActorID)
	entry.requesterLabel = firstNonEmpty(strings.TrimSpace(cfg.Agent), entry.requesterActor)

	threadID := strings.TrimSpace(entry.ThreadID)
	if threadID == "" {
		if cached, ok := threadCache[entry.subject]; ok {
			threadID = cached
		} else {
			resolved, err := a.resolveHumanAttentionThreadIDFromSubjectRef(context.Background(), cfg, entry.subject)
			if err != nil {
				return err
			}
			threadID = resolved
			threadCache[entry.subject] = resolved
		}
	}
	if err := validateID(threadID, "thread id"); err != nil {
		return errnorm.Usage("invalid_request", fmt.Sprintf("asks[%d]: %v", index, err))
	}
	entry.thread = threadID

	proposals, err := buildCLIHumanAttentionResponseProposals(entry.Recommend, entry.Alts)
	if err != nil {
		return errnorm.Usage("invalid_request", fmt.Sprintf("asks[%d]: %v", index, err))
	}
	entry.proposals = proposals

	explicitDelivery := entry.Delivery
	explicitSet := explicitDelivery != ""
	if !explicitSet && deliveryFlag.set {
		explicitDelivery = deliveryFlag.value
		explicitSet = true
	}
	if !explicitSet && strings.TrimSpace(batchDefaultDelivery) != "" {
		explicitDelivery = batchDefaultDelivery
		explicitSet = true
	}
	onAnswer := entry.OnAnswer
	if strings.TrimSpace(onAnswer) == "" {
		onAnswer = onAnswerFlag.value
	}
	decision, err := a.resolveAskDelivery(cfg, askDeliveryInput{
		Explicit:     explicitDelivery,
		ExplicitSet:  explicitSet,
		OnAnswer:     onAnswer,
		OnAnswerFlag: strings.TrimSpace(onAnswer) != "",
		AskID:        entry.eventID,
	})
	if err != nil {
		return errnorm.Usage("invalid_request", fmt.Sprintf("asks[%d]: %v", index, err))
	}
	entry.decided = decision
	if forceFlag.value {
		entry.overrideReason = strings.TrimSpace(reasonFlag.value)
	}
	return nil
}

// buildAskBatchMember assembles one human_attention_requested event in the
// same shape `anx ask` publishes.
func (a *App) buildAskBatchMember(entry *askBatchEntry) map[string]any {
	relatedRefs := normalizeStringFilters(entry.Refs)
	refs := uniqueStringsInOrder(append([]string{"thread:" + entry.thread, entry.subject}, relatedRefs...))
	payload := map[string]any{
		"kind":               "ask",
		"title":              entry.Title,
		"subject_ref":        entry.subject,
		"related_refs":       relatedRefs,
		"requester_actor_id": entry.requesterActor,
		"requester_label":    entry.requesterLabel,
		"response_proposals": entry.proposals,
		"evidence":           entry.Evidence,
		"authoring_warnings": entry.lintWarnings,
		"delivery":           entry.decided.Mode,
	}
	if entry.overrideReason != "" {
		payload["authoring_override_reason"] = entry.overrideReason
	}
	if entry.Body != "" {
		payload["body"] = entry.Body
	}
	if entry.RequestID != "" {
		payload["request_id"] = entry.RequestID
	}
	if entry.CoverageHint != "" {
		payload["coverage_hint"] = entry.CoverageHint
	}
	entry.decision.apply(payload)
	if entry.Supersedes != "" {
		payload["supersedes"] = entry.Supersedes
		refs = uniqueStringsInOrder(append(refs, entry.Supersedes))
	}
	return map[string]any{
		"type":      humanAttentionRequestedEventType,
		"id":        entry.eventID,
		"thread_id": entry.thread,
		"summary":   entry.Title,
		"refs":      refs,
		"payload":   payload,
		"provenance": map[string]any{
			"sources": []string{"event:cli.ask"},
		},
	}
}

func (a *App) registerAskBatchResume(ctx context.Context, cfg config.Resolved, entry *askBatchEntry, askRef string) error {
	dir, err := a.askRegistryDir(cfg)
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	askID := strings.TrimPrefix(askRef, "event:")
	registryPath := askRegistryPath(dir, askRef)
	registration := askResumeRegistration{AskID: askRef, BaseURL: cfg.BaseURL, ActorID: cfg.ActorID, Command: entry.decided.Command, Dir: cwd, ThreadID: entry.thread, State: "pending"}
	if err := writeAskRegistration(registryPath, registration); err != nil {
		return err
	}
	if _, err := a.invokeRawJSON(ctx, cfg, "ask subscribe", "POST", "/asks/"+url.PathEscape(askID)+"/subscriptions", map[string]any{"kind": "bridge", "label": "host resume"}); err != nil {
		return err
	}
	return nil
}

func askBatchWarnings(entries []askBatchEntry) []output.Warning {
	warnings := make([]output.Warning, 0)
	for index, entry := range entries {
		for _, message := range entry.lintWarnings {
			warnings = append(warnings, output.Warning{Code: "ask_authoring", Message: fmt.Sprintf("asks[%d]: %s", index, message)})
		}
		warnings = append(warnings, askDeliveryWarnings(entry.decided)...)
	}
	return warnings
}
