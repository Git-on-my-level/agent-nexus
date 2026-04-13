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
	if proposedNested := extractNestedMap(updateBody, "revision"); len(proposedNested) > 0 {
		if proposedContentRaw == nil {
			proposedContentRaw = proposedNested["body_markdown"]
		}
		if proposedContentType == "" {
			proposedContentType = "text"
		}
	}
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

func (a *App) runDocsProposeUpdateCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	id, body, err := a.parseDocsUpdateInput(args, "docs propose-update", cfg)
	if err != nil {
		return nil, err
	}

	currentResult, callErr := a.invokeTypedJSONWithIDResolution(
		ctx,
		cfg,
		"docs get",
		"docs.get",
		"document_id",
		id,
		documentIDLookupSpec,
		nil,
		nil,
	)
	if callErr != nil {
		return nil, callErr
	}
	currentData := asMap(currentResult.Data)
	currentBody := extractNestedMap(currentData, "body")
	document := extractNestedMap(currentBody, "document")
	resolvedID := strings.TrimSpace(firstNonEmpty(anyString(document["id"]), id))
	diffText := docsProposalDiffText(currentBody, body)
	wireBody := normalizeDocsRevisionRequestForContract(body)

	draft, draftPath, err := a.stageProposal("docs.revisions.create", map[string]string{"document_id": resolvedID}, wireBody, cfg, map[string]any{"resource": "document"})
	if err != nil {
		return nil, err
	}
	applyCommand := "oar docs apply --proposal-id " + draft.DraftID
	return proposalPreviewResult("docs.revisions.create", "POST", resolveCommandPath("docs.revisions.create", map[string]string{"document_id": resolvedID}, nil), map[string]string{"document_id": resolvedID}, wireBody, draft.DraftID, draftPath, diffText, applyCommand), nil
}

func (a *App) runDocsApplyCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	proposalID, err := parseProposalIDArg(args, "docs apply")
	if err != nil {
		return nil, err
	}
	_, result, err := a.commitProposal(ctx, proposalID, cfg, "docs.revisions.create")
	return result, err
}
