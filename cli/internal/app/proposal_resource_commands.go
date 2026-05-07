package app

import (
	"context"
	"fmt"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
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
		return "", errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `anx %s`", commandName))
	}
	return proposalID, nil
}

func mapBody(raw any, commandName string) (map[string]any, error) {
	body, ok := raw.(map[string]any)
	if !ok {
		return nil, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body for `anx %s` must be an object", commandName))
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

func docsHeadRevisionID(currentData map[string]any) string {
	currentBody := extractNestedMap(currentData, "body")
	document := extractNestedMap(currentBody, "document")
	revision := extractNestedMap(currentBody, "revision")
	return strings.TrimSpace(firstNonEmpty(
		anyString(document["head_revision_id"]),
		anyString(revision["revision_id"]),
	))
}

type docsReviseInput struct {
	documentID string
	proposalID string
	body       map[string]any
	apply      bool
}

func (a *App) parseDocsReviseInput(args []string) (docsReviseInput, error) {
	leadingDocumentID, args := popLeadingPositional(args)
	fs := newSilentFlagSet("docs revise")
	var documentIDFlag, proposalIDFlag, fromFileFlag, contentFileFlag, actorIDFlag trackedString
	var applyFlag, proposeFlag trackedBool
	fs.Var(&documentIDFlag, "document-id", "Document ref, handle, or id")
	fs.Var(&proposalIDFlag, "proposal-id", "Staged revision proposal id")
	fs.Var(&fromFileFlag, "from-file", "Load advanced JSON revision body from file")
	fs.Var(&contentFileFlag, "body-file", "Load revised document content from a local file or stdin with -")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	fs.Var(&applyFlag, "apply", "Apply immediately instead of staging a proposal")
	fs.Var(&proposeFlag, "propose", "Stage a proposal (default)")
	if err := fs.Parse(args); err != nil {
		return docsReviseInput{}, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	documentID := firstNonEmpty(strings.TrimSpace(documentIDFlag.value), leadingDocumentID)
	if documentID == "" && len(positionals) > 0 && strings.TrimSpace(proposalIDFlag.value) == "" {
		documentID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return docsReviseInput{}, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx docs revise`")
	}
	if applyFlag.value && proposeFlag.value {
		return docsReviseInput{}, errnorm.Usage("invalid_flags", "`--apply` and `--propose` cannot be combined")
	}

	proposalID := strings.TrimSpace(proposalIDFlag.value)
	if proposalID != "" {
		if !applyFlag.value {
			return docsReviseInput{}, errnorm.Usage("invalid_flags", "`--proposal-id` applies a staged proposal and must be combined with `--apply`")
		}
		if documentID != "" || strings.TrimSpace(fromFileFlag.value) != "" || strings.TrimSpace(contentFileFlag.value) != "" {
			return docsReviseInput{}, errnorm.Usage("invalid_flags", "`--proposal-id` cannot be combined with document/content inputs")
		}
		if err := validateDraftID(proposalID); err != nil {
			return docsReviseInput{}, err
		}
		return docsReviseInput{proposalID: proposalID, apply: true}, nil
	}
	if err := validateID(documentID, "document id"); err != nil {
		return docsReviseInput{}, err
	}

	_, rawBody, _, err := a.parseIDAndBodyInputWithOptions(append([]string{"--document-id", documentID}, docsReviseBodyArgs(fromFileFlag.value, contentFileFlag.value)...), "document-id", "document id", "docs revise", jsonBodyInputOptions{
		allowContentFile: true,
		allowContentOnly: true,
	})
	if err != nil {
		return docsReviseInput{}, err
	}
	body, err := mapBody(rawBody, "docs revise")
	if err != nil {
		return docsReviseInput{}, err
	}
	if actorID := strings.TrimSpace(actorIDFlag.value); actorID != "" {
		body["actor_id"] = actorID
	}
	return docsReviseInput{documentID: documentID, body: body, apply: applyFlag.value}, nil
}

func docsReviseBodyArgs(fromFile string, contentFile string) []string {
	args := make([]string, 0, 4)
	if strings.TrimSpace(fromFile) != "" {
		args = append(args, "--from-file", strings.TrimSpace(fromFile))
	}
	if strings.TrimSpace(contentFile) != "" {
		args = append(args, "--body-file", strings.TrimSpace(contentFile))
	}
	return args
}

func (a *App) prepareDocsRevisionBody(ctx context.Context, cfg config.Resolved, id string, body map[string]any) (resolvedID string, currentBody map[string]any, prepared map[string]any, err error) {
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
		return "", nil, nil, callErr
	}
	currentData := asMap(currentResult.Data)
	currentBody = extractNestedMap(currentData, "body")
	document := extractNestedMap(currentBody, "document")
	resolvedID = strings.TrimSpace(firstNonEmpty(anyString(document["id"]), id))
	if strings.TrimSpace(anyString(body["if_base_revision"])) == "" {
		if baseRevision := docsHeadRevisionID(currentData); baseRevision != "" {
			body["if_base_revision"] = baseRevision
		}
	}
	if err := validateDocsRevisionBody(body, "docs revise"); err != nil {
		return "", nil, nil, err
	}
	bodyAny, err := ensureDocsRevisionActorIdentity(body, cfg)
	if err != nil {
		return "", nil, nil, err
	}
	prepared, err = mapBody(bodyAny, "docs revise")
	if err != nil {
		return "", nil, nil, err
	}
	return resolvedID, currentBody, prepared, nil
}

func (a *App) runDocsReviseCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	input, err := a.parseDocsReviseInput(args)
	if err != nil {
		return nil, err
	}
	if input.proposalID != "" {
		_, result, err := a.commitProposal(ctx, input.proposalID, cfg, "docs.revisions.create")
		return result, err
	}

	if input.apply {
		body := input.body
		bodyAny, err := ensureDocsRevisionActorIdentity(body, cfg)
		if err != nil {
			return nil, err
		}
		body, err = mapBody(bodyAny, "docs revise")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(anyString(body["if_base_revision"])) == "" {
			currentResult, callErr := a.invokeTypedJSONWithIDResolution(
				ctx,
				cfg,
				"docs get",
				"docs.get",
				"document_id",
				input.documentID,
				documentIDLookupSpec,
				nil,
				nil,
			)
			if callErr != nil {
				return nil, callErr
			}
			if baseRevision := docsHeadRevisionID(asMap(currentResult.Data)); baseRevision != "" {
				body["if_base_revision"] = baseRevision
			}
		}
		if err := validateDocsRevisionBody(body, "docs revise"); err != nil {
			return nil, err
		}
		return a.invokeTypedJSONWithIDResolution(ctx, cfg, "docs revise", "docs.revisions.create", "document_id", input.documentID, documentIDLookupSpec, nil, body)
	}

	resolvedID, currentBody, body, err := a.prepareDocsRevisionBody(ctx, cfg, input.documentID, input.body)
	if err != nil {
		return nil, err
	}
	diffText := docsProposalDiffText(currentBody, body)

	draft, draftPath, err := a.stageProposal("docs.revisions.create", map[string]string{"document_id": resolvedID}, body, cfg, map[string]any{"resource": "document"})
	if err != nil {
		return nil, err
	}
	applyCommand := "anx docs revise --apply --proposal-id " + draft.DraftID
	return proposalPreviewResult("docs.revisions.create", "POST", resolveCommandPath("docs.revisions.create", map[string]string{"document_id": resolvedID}, nil), map[string]string{"document_id": resolvedID}, body, draft.DraftID, draftPath, diffText, applyCommand), nil
}
