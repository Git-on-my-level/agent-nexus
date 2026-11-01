// `anx docs` dispatch with docs create/revise input parsing and body
// validation.

package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func (a *App) runDocsCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "docs", docsSubcommandSpec.requiredError()
	}
	sub := docsSubcommandSpec.normalize(args[0])
	switch sub {
	case "list":
		fs := newSilentFlagSet("docs list")
		var threadIDFlag, queryFlag, cursorFlag, tagFlag trackedString
		var limitFlag trackedInt
		var includeTrashed, trashedOnly, includeArchived, archivedOnly, knowledge bool
		fs.BoolVar(&includeTrashed, "include-trashed", false, "Include trashed documents")
		fs.BoolVar(&trashedOnly, "trashed-only", false, "Show only trashed documents")
		fs.BoolVar(&includeArchived, "include-archived", false, "Include archived documents")
		fs.BoolVar(&archivedOnly, "archived-only", false, "Show only archived documents")
		fs.Var(&threadIDFlag, "thread-id", "Filter by thread id")
		fs.Var(&queryFlag, "q", "Search by document id or title")
		fs.Var(&tagFlag, "tag", "Restrict results to documents that include this tag")
		fs.Var(&limitFlag, "limit", "Limit the number of returned documents")
		fs.Var(&cursorFlag, "cursor", "Pagination cursor from a previous list response")
		fs.BoolVar(&knowledge, "knowledge", false, "Only documents tagged knowledge")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "docs list", errnorm.Usage("invalid_flags", err.Error())
		}
		if err := validateLifecycleFilterFlags(includeArchived, archivedOnly, includeTrashed, trashedOnly); err != nil {
			return nil, "docs list", err
		}
		if len(fs.Args()) > 0 {
			return nil, "docs list", errnorm.Usage("invalid_args", "unexpected positional arguments for `anx docs list`")
		}
		if limitFlag.set && (limitFlag.value < 1 || limitFlag.value > 1000) {
			return nil, "docs list", errnorm.Usage("invalid_request", "limit must be between 1 and 1000")
		}
		resolvedThreadID := strings.TrimSpace(threadIDFlag.value)
		if resolvedThreadID != "" {
			resolved, err := a.resolveThreadIDFilters(ctx, cfg, []string{resolvedThreadID})
			if err != nil {
				return nil, "docs list", err
			}
			if len(resolved) > 0 {
				resolvedThreadID = resolved[0]
			}
		}
		query := make([]queryParam, 0, 5)
		if err := appendLifecycleStatesForHTTPList(&query, "", includeArchived, archivedOnly, includeTrashed, trashedOnly); err != nil {
			return nil, "docs list", err
		}
		addSingleQuery(&query, "thread_id", resolvedThreadID)
		addSingleQuery(&query, "q", queryFlag.value)
		addSingleQuery(&query, "tag", tagFlag.value)
		if knowledge {
			addSingleQuery(&query, "knowledge", "true")
		}
		if limitFlag.set {
			addSingleQuery(&query, "limit", strconv.Itoa(limitFlag.value))
		}
		addSingleQuery(&query, "cursor", cursorFlag.value)
		result, callErr := a.invokeTypedJSON(ctx, cfg, "docs list", "docs.list", nil, query, nil)
		return result, "docs list", callErr
	case "create":
		body, dryRun, err := a.parseDocsCreateInput(args[1:], cfg)
		if err != nil {
			return nil, "docs create", err
		}
		if err := validateDocsCreateBody(body, "docs create"); err != nil {
			return nil, "docs create", err
		}
		if dryRun {
			result := dryRunResult("docs create", "docs.create", nil, nil, body)
			result.Warnings = reportContentWarning(reportBodyContent(asMap(body)))
			return result, "docs create", nil
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "docs create", "docs.create", nil, nil, body)
		result = addResourceURLToResult(cfg, "docs.create", result)
		if result != nil {
			result.Warnings = reportContentWarning(reportBodyContent(asMap(body)))
		}
		return result, "docs create", callErr
	case "put":
		result, callErr := a.runDocsPutCommand(ctx, args[1:], cfg)
		return result, "docs put", callErr
	case "ingest":
		result, callErr := a.runDocsIngestCommand(ctx, args[1:], cfg)
		return result, "docs ingest", callErr
	case "search":
		result, callErr := a.runDocsSearchCommand(ctx, args[1:], cfg)
		return result, "docs search", callErr
	case "get":
		result, callErr := a.runDocsGetCommand(ctx, args[1:], cfg)
		return result, "docs get", callErr
	case "comment":
		result, callErr := a.runDocsCommentCommand(ctx, args[1:], cfg)
		return result, "docs comment", callErr
	case "comments":
		if len(args) >= 2 && docsCommentsSubcommandSpec.normalize(args[1]) == "reply" {
			result, callErr := a.runDocsCommentsReplyCommand(ctx, args[2:], cfg)
			return result, "docs comments reply", callErr
		}
		if len(args) >= 2 && docsCommentsSubcommandSpec.normalize(args[1]) == "edit" {
			result, callErr := a.runDocsCommentsEditCommand(ctx, args[2:], cfg)
			return result, "docs comments edit", callErr
		}
		if len(args) >= 2 && docsCommentsSubcommandSpec.normalize(args[1]) == "delete" {
			result, callErr := a.runDocsCommentsDeleteCommand(ctx, args[2:], cfg)
			return result, "docs comments delete", callErr
		}
		result, callErr := a.runDocsCommentsListCommand(ctx, args[1:], cfg)
		return result, "docs comments", callErr
	case "content":
		result, callErr := a.runDocsContentCommand(ctx, args[1:], cfg)
		return result, "docs content", callErr
	case "message":
		body, target, dryRun, err := a.parseDocMessageInput(ctx, args[1:], cfg, "docs message", "")
		if err != nil {
			return nil, "docs message", err
		}
		if err := validateEventsCreateInput(body, "docs message"); err != nil {
			return nil, "docs message", err
		}
		if dryRun {
			return dryRunResult("docs message", "events.create", nil, nil, body), "docs message", nil
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "docs message", "events.create", nil, nil, body)
		return decorateThreadMessageWriteResult(result, callErr, cfg, "docs.message", target), "docs message", callErr
	case "messages":
		result, callErr := a.runDocMessagesCommand(ctx, args[1:], cfg)
		return result, "docs messages", callErr
	case "reply":
		body, target, dryRun, err := a.parseDocReplyInput(ctx, args[1:], cfg, "docs reply")
		if err != nil {
			return nil, "docs reply", err
		}
		if err := validateEventsCreateInput(body, "docs reply"); err != nil {
			return nil, "docs reply", err
		}
		if dryRun {
			return dryRunResult("docs reply", "events.create", nil, nil, body), "docs reply", nil
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "docs reply", "events.create", nil, nil, body)
		return decorateThreadMessageWriteResult(result, callErr, cfg, "docs.reply", target), "docs reply", callErr
	case "revise":
		result, callErr := a.runDocsReviseCommand(ctx, args[1:], cfg)
		return result, "docs revise", callErr
	case "history":
		id, err := parseResourceIDArg(args[1:], "document-id", "document id", "document")
		if err != nil {
			return nil, "docs history", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(
			ctx,
			cfg,
			"docs history",
			"docs.revisions.list",
			"document_id",
			id,
			documentIDLookupSpec,
			nil,
			nil,
		)
		return result, "docs history", callErr
	case "revision":
		if len(args) < 2 {
			return nil, "docs revision", docsRevisionSubcommandSpec.requiredError()
		}
		if docsRevisionSubcommandSpec.normalize(args[1]) != "get" {
			return nil, "docs revision", docsRevisionSubcommandSpec.unknownError(args[1])
		}
		fs := newSilentFlagSet("docs revision get")
		var documentIDFlag trackedString
		var revisionIDFlag trackedString
		fs.Var(&documentIDFlag, "document-id", "Document id")
		fs.Var(&revisionIDFlag, "revision-id", "Revision id")
		if err := fs.Parse(args[2:]); err != nil {
			return nil, "docs revision get", errnorm.Usage("invalid_flags", err.Error())
		}
		positionals := fs.Args()

		documentID := strings.TrimSpace(documentIDFlag.value)
		revisionID := strings.TrimSpace(revisionIDFlag.value)
		if documentID == "" && len(positionals) > 0 {
			documentID = strings.TrimSpace(positionals[0])
			positionals = positionals[1:]
		}
		if revisionID == "" && len(positionals) > 0 {
			revisionID = strings.TrimSpace(positionals[0])
			positionals = positionals[1:]
		}
		if err := validateID(documentID, "document id"); err != nil {
			return nil, "docs revision get", err
		}
		if err := validateID(revisionID, "revision id"); err != nil {
			return nil, "docs revision get", err
		}
		if len(positionals) > 0 {
			return nil, "docs revision get", errnorm.Usage("invalid_args", "unexpected positional arguments for `anx docs revision get`")
		}

		result, callErr := a.invokeDocsRevisionGetWithIDResolution(ctx, cfg, documentID, revisionID)
		return result, "docs revision get", callErr
	case "archive", "unarchive", "trash", "restore", "purge":
		spec, ok := lifecycleSpecFor("docs")
		if !ok {
			return nil, "docs", errnorm.Usage("internal_error", "missing lifecycle spec for docs")
		}
		return a.runLifecycleVerb(ctx, cfg, spec, sub, args[1:])
	default:
		return nil, "docs", docsSubcommandSpec.unknownError(args[0])
	}
}

func (a *App) runDocsContentCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	id, err := parseResourceIDArg(args, "document-id", "document id", "document")
	if err != nil {
		return nil, err
	}
	result, callErr := a.invokeTypedJSONWithIDResolution(
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

	data := asMap(result.Data)
	body := asMap(data["body"])
	document := extractNestedMap(body, "document")
	revision := extractNestedMap(body, "revision")
	content := firstNonEmpty(anyString(revision["content"]), anyString(body["content"]), anyString(body["body_text"]))
	contentBody := map[string]any{
		"document": document,
		"revision": revision,
		"content":  content,
	}
	data["body"] = contentBody
	result.Data = data
	result.Text = formatTypedCommandText(
		"docs.content",
		intValue(data["status_code"]),
		headerValues(data["headers"]),
		contentBody,
		cfg.Verbose,
		cfg.Headers,
	)
	return result, nil
}

func (a *App) fetchDocumentBody(ctx context.Context, cfg config.Resolved, documentID string) (map[string]any, error) {
	result, err := a.invokeTypedJSONWithIDResolution(ctx, cfg, "docs get", "docs.get", "document_id", documentID, documentIDLookupSpec, nil, nil)
	if err != nil {
		return nil, err
	}
	body := extractNestedMap(asMap(result.Data), "body")
	document := extractNestedMap(body, "document")
	if len(document) == 0 {
		return nil, errnorm.Usage("invalid_request", "docs get response did not include document metadata")
	}
	return document, nil
}

func (a *App) parseDocsCreateInput(args []string, cfg config.Resolved) (any, bool, error) {
	fs := newSilentFlagSet("docs create")
	var fromFileFlag, contentFileFlag trackedString
	var titleFlag, summaryFlag, topicFlag, subjectFlag, bodyFlag, actorIDFlag trackedString
	var dryRunFlag trackedBool
	var refFlags trackedStrings
	fs.Var(&fromFileFlag, "from-file", "Advanced JSON body from file path or stdin with -")
	fs.Var(&contentFileFlag, "body-file", "Load document content from a local file or stdin with -")
	fs.Var(&titleFlag, "title", "Document title")
	fs.Var(&summaryFlag, "summary", "Document summary")
	fs.Var(&bodyFlag, "body", "Inline document body text (text/Markdown)")
	fs.Var(&topicFlag, "topic", "Topic typed ref or handle to anchor the document")
	fs.Var(&subjectFlag, "subject-ref", "Explicit typed subject ref")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	fs.Var(&refFlags, "ref", "Additional typed ref (repeatable)")
	fs.Var(&dryRunFlag, "dry-run", "Validate and render request without sending the mutation")
	if err := fs.Parse(args); err != nil {
		return nil, false, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, false, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx docs create`")
	}

	fromFile := strings.TrimSpace(fromFileFlag.value)
	contentFile := strings.TrimSpace(contentFileFlag.value)
	var payload []byte
	var err error
	if skipJSONStdinForContentFile(fromFile, contentFile) {
		payload = nil
	} else {
		payload, err = a.readBodyInput(fromFile)
	}
	if err != nil {
		return nil, false, err
	}
	if len(payload) > 0 && strings.TrimSpace(bodyFlag.value) != "" {
		return nil, false, errnorm.Usage("invalid_args", "`--body` cannot be combined with JSON body input for `anx docs create`")
	}
	var body any
	if len(payload) > 0 {
		body, err = decodeJSONPayload(payload)
		if err != nil {
			return nil, false, err
		}
	} else {
		if strings.TrimSpace(bodyFlag.value) != "" && contentFile != "" {
			return nil, false, errnorm.Usage("invalid_request", "`--body` and `--body-file` cannot be combined for `anx docs create`")
		}
		body = docsCreateBodyFromFlags(titleFlag.value, summaryFlag.value, topicFlag.value, subjectFlag.value, refFlags.values)
		if inline := strings.TrimSpace(bodyFlag.value); inline != "" {
			if bodyMap, ok := body.(map[string]any); ok {
				bodyMap["content"] = inline
			}
		}
	}
	body, err = a.applyContentFileOverride(body, contentFile, "docs create")
	if err != nil {
		return nil, false, err
	}
	if bodyMap, ok := body.(map[string]any); ok {
		actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
		if err != nil {
			return nil, false, err
		}
		if actorID != "" {
			bodyMap["actor_id"] = actorID
		}
		if err := finalizeOptionalMutationBodyActorID(bodyMap, cfg); err != nil {
			return nil, false, err
		}
	}
	return body, dryRunFlag.set && dryRunFlag.value, nil
}

func docsCreateBodyFromFlags(title string, summary string, topic string, subject string, refs []string) map[string]any {
	title = strings.TrimSpace(title)
	summary = strings.TrimSpace(summary)
	topicRef := normalizeTypedRefFlag(topic, "topic")
	subjectRef := strings.TrimSpace(subject)
	if subjectRef == "" {
		subjectRef = topicRef
	}
	refSet := make([]string, 0, len(refs)+1)
	if topicRef != "" {
		refSet = append(refSet, topicRef)
	}
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref != "" {
			refSet = append(refSet, ref)
		}
	}
	document := map[string]any{
		"title":       title,
		"subject_ref": subjectRef,
		"refs":        uniqueStrings(refSet),
		"provenance":  map[string]any{"sources": []any{"event:anx-cli"}},
	}
	if summary != "" {
		document["summary"] = summary
	}
	return map[string]any{
		"document":     document,
		"content_type": "text",
	}
}

func (a *App) invokeDocsRevisionGetWithIDResolution(ctx context.Context, cfg config.Resolved, documentID, revisionID string) (*commandResult, error) {
	return a.invokeTypedJSON(ctx, cfg, "docs revision get", "docs.revisions.get", map[string]string{
		"document_id": strings.TrimSpace(documentID),
		"revision_id": strings.TrimSpace(revisionID),
	}, nil, nil)
}

// normalizeDocsMutationContentType maps common MIME-style aliases to the canonical
// API values (text, structured, binary). Mutates payload in place.
func normalizeDocsMutationContentType(payload map[string]any) {
	raw, ok := payload["content_type"]
	if !ok || raw == nil {
		return
	}
	s, ok := raw.(string)
	if !ok {
		return
	}
	s = strings.TrimSpace(s)
	if idx := strings.Index(s, ";"); idx >= 0 {
		s = strings.TrimSpace(s[:idx])
	}
	switch strings.ToLower(s) {
	case "text/markdown":
		payload["content_type"] = "text"
	}
}

func validateDocsCreateBody(body any, commandName string) error {
	payload, ok := body.(map[string]any)
	if !ok {
		return errnorm.Usage("invalid_request", fmt.Sprintf("JSON body for `anx %s` must be an object", commandName))
	}
	normalizeDocsMutationContentType(payload)

	issues := make([]string, 0, 8)
	rawDocument, hasDocument := payload["document"]
	if !hasDocument || rawDocument == nil {
		issues = append(issues, "document is required")
	} else {
		if _, ok := rawDocument.(map[string]any); !ok {
			issues = append(issues, "document must be an object")
		}
	}

	rawContent, hasContent := payload["content"]
	if !hasContent || rawContent == nil {
		issues = append(issues, "content is required")
	}

	rawContentType, hasContentType := payload["content_type"]
	contentType := strings.TrimSpace(anyString(rawContentType))
	if !hasContentType {
		issues = append(issues, "content_type is required")
	} else if contentType == "" {
		issues = append(issues, "content_type must be a non-empty string")
	} else if contentType != "text" && contentType != "structured" && contentType != "binary" {
		issues = append(issues, fmt.Sprintf("content_type %q must be one of: text, structured, binary", contentType))
	}

	appendDocsCommonValidationIssues(payload, &issues)
	if len(issues) > 0 {
		return errnorm.Usage("invalid_request", fmt.Sprintf("docs create payload failed local validation: %s", strings.Join(issues, "; ")))
	}
	if hasContent {
		if err := visualReportContentError(rawContent); err != nil {
			return err
		}
	}
	return nil
}

func validateDocsRevisionBody(body any, commandName string) error {
	payload, ok := body.(map[string]any)
	if !ok {
		return errnorm.Usage("invalid_request", fmt.Sprintf("JSON body for `anx %s` must be an object", commandName))
	}
	normalizeDocsMutationContentType(payload)

	issues := make([]string, 0, 8)
	rawContent, hasContent := payload["content"]
	if !hasContent || rawContent == nil {
		issues = append(issues, "content is required")
	}

	rawContentType, hasContentType := payload["content_type"]
	contentType := strings.TrimSpace(anyString(rawContentType))
	if !hasContentType {
		issues = append(issues, "content_type is required")
	} else if contentType == "" {
		issues = append(issues, "content_type must be a non-empty string")
	} else if contentType != "text" && contentType != "structured" && contentType != "binary" {
		issues = append(issues, fmt.Sprintf("content_type %q must be one of: text, structured, binary", contentType))
	}

	rawBaseRevision, hasBaseRevision := payload["if_base_revision"]
	baseRevision := strings.TrimSpace(anyString(rawBaseRevision))
	if !hasBaseRevision {
		issues = append(issues, "if_base_revision is required")
	} else if baseRevision == "" {
		issues = append(issues, "if_base_revision must be a non-empty string")
	}

	appendDocsCommonValidationIssues(payload, &issues)
	if len(issues) > 0 {
		return errnorm.Usage("invalid_request", fmt.Sprintf("docs.revisions.create payload failed local validation: %s", strings.Join(issues, "; ")))
	}
	if err := visualReportContentError(rawContent); err != nil {
		return err
	}
	return nil
}

func appendDocsCommonValidationIssues(payload map[string]any, issues *[]string) {
	if rawDocument, hasDocument := payload["document"]; hasDocument {
		if _, ok := rawDocument.(map[string]any); !ok {
			*issues = append(*issues, "document must be an object when provided")
		}
	}

	if rawActorID, hasActorID := payload["actor_id"]; hasActorID {
		if strings.TrimSpace(anyString(rawActorID)) == "" {
			*issues = append(*issues, "actor_id must be a non-empty string when provided")
		}
	}

	if rawRefs, hasRefs := payload["refs"]; hasRefs {
		refs, ok := asStringList(rawRefs)
		if !ok {
			*issues = append(*issues, "refs must be an array of strings when provided")
		} else {
			for _, ref := range refs {
				if err := validateTypedRef(ref); err != nil {
					*issues = append(*issues, fmt.Sprintf("refs contains invalid typed ref %q", ref))
				}
			}
		}
	}
}

func ensureDocsRevisionActorIdentity(body any, cfg config.Resolved) (any, error) {
	payload, ok := body.(map[string]any)
	if !ok {
		return nil, errnorm.Usage("invalid_request", "JSON body for `anx docs revise` must be an object")
	}

	if actorID := strings.TrimSpace(anyString(payload["actor_id"])); actorID != "" {
		return payload, nil
	}
	if actorID := strings.TrimSpace(cfg.ActorID); actorID != "" {
		payload["actor_id"] = actorID
		return payload, nil
	}

	return nil, errnorm.Usage(
		"invalid_request",
		"No active actor identity. Run: anx host enroll, then anx --as <name> auth whoami.",
	)
}
