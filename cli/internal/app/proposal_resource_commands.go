package app

import (
	"context"
	"fmt"
	"strings"

	"organization-autorunner-cli/internal/config"
	"organization-autorunner-cli/internal/errnorm"
)

func parseProposalIDArg(args []string, commandName string) (string, error) {
	fs := newSilentFlagSet(commandName)
	var proposalIDFlag trackedString
	fs.Var(&proposalIDFlag, "proposal-id", "Proposal id")
	if err := fs.Parse(args); err != nil {
		return "", errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	proposalID := strings.TrimSpace(proposalIDFlag.value)
	if proposalID == "" && len(positionals) > 0 {
		proposalID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if err := validateDraftID(proposalID); err != nil {
		return "", err
	}
	if len(positionals) > 0 {
		return "", errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `oar %s`", commandName))
	}
	return proposalID, nil
}

func mapBody(raw any, commandName string) (map[string]any, error) {
	body, ok := raw.(map[string]any)
	if !ok {
		return nil, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body for `oar %s` must be an object", commandName))
	}
	return body, nil
}

func applyPatchMap(current map[string]any, patch map[string]any) map[string]any {
	out := cloneMap(current)
	if out == nil {
		out = map[string]any{}
	}
	for key, value := range patch {
		out[key] = value
	}
	return out
}

func firstContentValue(values ...any) any {
	for _, value := range values {
		if value == nil {
			continue
		}
		if text, ok := value.(string); ok && strings.TrimSpace(text) == "" {
			continue
		}
		return value
	}
	return nil
}

func docsProposalDiffText(currentBody map[string]any, updateBody map[string]any) string {
	revision := extractNestedMap(currentBody, "revision")
	currentContentRaw := firstContentValue(revision["content"], currentBody["content"], currentBody["body_text"])
	proposedContentRaw := updateBody["content"]
	currentContentType := strings.TrimSpace(firstNonEmpty(anyString(revision["content_type"]), anyString(currentBody["content_type"])))
	proposedContentType := strings.TrimSpace(anyString(updateBody["content_type"]))
	if currentContentType == "text" && proposedContentType == "text" {
		currentContent := strings.TrimSpace(anyString(currentContentRaw))
		proposedContent := strings.TrimSpace(anyString(proposedContentRaw))
		return renderUnifiedDiff("current", currentContent, "proposed", proposedContent)
	}

	currentView := map[string]any{
		"content_type": currentContentType,
		"content":      currentContentRaw,
		"revision_id":  anyString(revision["revision_id"]),
		"refs":         stringList(revision["refs"]),
	}
	proposedView := cloneMap(currentView)
	if proposedContentType != "" {
		proposedView["content_type"] = proposedContentType
	}
	proposedView["content"] = proposedContentRaw
	if refs, ok := updateBody["refs"].([]any); ok {
		proposedView["refs"] = refs
	} else if refs := stringList(updateBody["refs"]); len(refs) > 0 {
		proposedView["refs"] = refs
	}
	proposedView["if_base_revision"] = anyString(updateBody["if_base_revision"])
	return renderUnifiedDiff("current.json", prettyProposalJSON(currentView), "proposed.json", prettyProposalJSON(proposedView))
}

func threadPatchValidationError(commandName string, issues []string) error {
	return errnorm.WithDetails(
		errnorm.Usage("invalid_request", strings.TrimSpace(commandName)+" payload failed local validation"),
		map[string]any{"errors": append([]string(nil), issues...)},
	)
}

func commitmentPatchValidationError(commandName string, issues []string) error {
	return errnorm.WithDetails(
		errnorm.Usage("invalid_request", strings.TrimSpace(commandName)+" payload failed local validation"),
		map[string]any{"errors": append([]string(nil), issues...)},
	)
}

func (a *App) parseThreadPatchInput(args []string, commandName string) (string, map[string]any, error) {
	id, rawBody, err := a.parseIDAndBodyInput(args, "thread-id", "thread id", commandName)
	if err != nil {
		return "", nil, err
	}
	body, err := mapBody(rawBody, commandName)
	if err != nil {
		return "", nil, err
	}
	if validation := validateDraftThreadPatch(body); len(validation) > 0 {
		return "", nil, threadPatchValidationError(commandName, validation)
	}
	return id, body, nil
}

func (a *App) parseCommitmentPatchInput(args []string, commandName string) (string, map[string]any, error) {
	id, rawBody, err := a.parseIDAndBodyInput(args, "commitment-id", "commitment id", commandName)
	if err != nil {
		return "", nil, err
	}
	body, err := mapBody(rawBody, commandName)
	if err != nil {
		return "", nil, err
	}
	if validation := validateDraftCommitmentPatch(body); len(validation) > 0 {
		return "", nil, commitmentPatchValidationError(commandName, validation)
	}
	return id, body, nil
}

func (a *App) parseDocsUpdateInput(args []string, commandName string, cfg config.Resolved) (string, map[string]any, error) {
	id, rawBody, _, err := a.parseIDAndBodyInputWithOptions(args, "document-id", "document id", commandName, jsonBodyInputOptions{
		allowContentFile: true,
	})
	if err != nil {
		return "", nil, err
	}
	body, err := mapBody(rawBody, commandName)
	if err != nil {
		return "", nil, err
	}
	if err := validateDocsUpdateBody(body, commandName); err != nil {
		return "", nil, err
	}
	bodyAny, err := ensureDocsUpdateActorIdentity(body, cfg)
	if err != nil {
		return "", nil, err
	}
	body, err = mapBody(bodyAny, commandName)
	if err != nil {
		return "", nil, err
	}
	return id, body, nil
}

func (a *App) runThreadsProposePatchCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	id, body, err := a.parseThreadPatchInput(args, "threads propose-patch")
	if err != nil {
		return nil, err
	}

	currentResult, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "threads get", "threads.get", "thread_id", id, threadIDLookupSpec, nil, nil)
	if callErr != nil {
		return nil, callErr
	}
	currentData := asMap(currentResult.Data)
	currentBody := extractNestedMap(currentData, "body")
	currentThread := extractNestedMap(currentBody, "thread")
	resolvedID := strings.TrimSpace(anyString(currentThread["id"]))
	if resolvedID == "" {
		resolvedID = strings.TrimSpace(id)
	}
	patch := asMap(body["patch"])
	proposedThread := applyPatchMap(currentThread, patch)
	diffText := renderUnifiedDiff("current.json", prettyProposalJSON(currentThread), "proposed.json", prettyProposalJSON(proposedThread))

	draft, draftPath, err := a.stageProposal("threads.patch", map[string]string{"thread_id": resolvedID}, body, cfg, map[string]any{"resource": "thread"})
	if err != nil {
		return nil, err
	}
	applyCommand := "oar threads apply --proposal-id " + draft.DraftID
	return proposalPreviewResult("threads.patch", "PATCH", resolveCommandPath("threads.patch", map[string]string{"thread_id": resolvedID}, nil), map[string]string{"thread_id": resolvedID}, body, draft.DraftID, draftPath, diffText, applyCommand), nil
}

func (a *App) runThreadsApplyCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	proposalID, err := parseProposalIDArg(args, "threads apply")
	if err != nil {
		return nil, err
	}
	_, result, err := a.commitProposal(ctx, proposalID, cfg, "threads.patch")
	return result, err
}

func (a *App) runCommitmentsProposePatchCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	id, body, err := a.parseCommitmentPatchInput(args, "commitments propose-patch")
	if err != nil {
		return nil, err
	}
	currentResult, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "commitments get", "commitments.get", "commitment_id", id, commitmentIDLookupSpec, nil, nil)
	if callErr != nil {
		return nil, callErr
	}
	currentData := asMap(currentResult.Data)
	currentBody := extractNestedMap(currentData, "body")
	currentCommitment := extractNestedMap(currentBody, "commitment")
	resolvedID := strings.TrimSpace(anyString(currentCommitment["id"]))
	if resolvedID == "" {
		resolvedID = strings.TrimSpace(id)
	}
	patch := asMap(body["patch"])
	proposedCommitment := applyPatchMap(currentCommitment, patch)
	if refs, exists := body["refs"]; exists {
		proposedCommitment["refs"] = refs
	}
	diffText := renderUnifiedDiff("current.json", prettyProposalJSON(currentCommitment), "proposed.json", prettyProposalJSON(proposedCommitment))

	draft, draftPath, err := a.stageProposal("commitments.patch", map[string]string{"commitment_id": resolvedID}, body, cfg, map[string]any{"resource": "commitment"})
	if err != nil {
		return nil, err
	}
	applyCommand := "oar commitments apply --proposal-id " + draft.DraftID
	return proposalPreviewResult("commitments.patch", "PATCH", resolveCommandPath("commitments.patch", map[string]string{"commitment_id": resolvedID}, nil), map[string]string{"commitment_id": resolvedID}, body, draft.DraftID, draftPath, diffText, applyCommand), nil
}

func (a *App) runCommitmentsApplyCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	proposalID, err := parseProposalIDArg(args, "commitments apply")
	if err != nil {
		return nil, err
	}
	_, result, err := a.commitProposal(ctx, proposalID, cfg, "commitments.patch")
	return result, err
}

func (a *App) runDocsProposeUpdateCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	id, body, err := a.parseDocsUpdateInput(args, "docs propose-update", cfg)
	if err != nil {
		return nil, err
	}

	currentResult, callErr := a.invokeTypedJSON(ctx, cfg, "docs get", "docs.get", map[string]string{"document_id": id}, nil, nil)
	if callErr != nil {
		return nil, callErr
	}
	currentData := asMap(currentResult.Data)
	currentBody := extractNestedMap(currentData, "body")
	document := extractNestedMap(currentBody, "document")
	resolvedID := strings.TrimSpace(firstNonEmpty(anyString(document["id"]), id))
	diffText := docsProposalDiffText(currentBody, body)

	draft, draftPath, err := a.stageProposal("docs.update", map[string]string{"document_id": resolvedID}, body, cfg, map[string]any{"resource": "document"})
	if err != nil {
		return nil, err
	}
	applyCommand := "oar docs apply --proposal-id " + draft.DraftID
	return proposalPreviewResult("docs.update", "PATCH", resolveCommandPath("docs.update", map[string]string{"document_id": resolvedID}, nil), map[string]string{"document_id": resolvedID}, body, draft.DraftID, draftPath, diffText, applyCommand), nil
}

func (a *App) runDocsApplyCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	proposalID, err := parseProposalIDArg(args, "docs apply")
	if err != nil {
		return nil, err
	}
	_, result, err := a.commitProposal(ctx, proposalID, cfg, "docs.update")
	return result, err
}
