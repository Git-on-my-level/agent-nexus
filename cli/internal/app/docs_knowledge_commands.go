package app

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func (a *App) runDocsGetCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	leadingID, args := popLeadingPositional(args)
	fs := newSilentFlagSet("docs get")
	var documentIDFlag, formatFlag trackedString
	fs.Var(&documentIDFlag, "document-id", "Document id, handle, or ref")
	fs.Var(&formatFlag, "format", "Output format: omit for default text/JSON, `md` for body only")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	documentID := firstNonEmpty(strings.TrimSpace(documentIDFlag.value), leadingID)
	if documentID == "" && len(positionals) > 0 {
		documentID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx docs get`")
	}
	if err := validateID(documentID, "document id"); err != nil {
		return nil, err
	}
	documentID, err := normalizeResourceIDInput(documentID, "document", "document id")
	if err != nil {
		return nil, err
	}
	format := strings.ToLower(strings.TrimSpace(formatFlag.value))
	if format != "" && format != "md" && format != "json" && format != "text" {
		return nil, errnorm.Usage("invalid_request", "`--format` must be md, json, or text")
	}
	result, callErr := a.invokeTypedJSONWithIDResolution(
		ctx,
		cfg,
		"docs get",
		"docs.get",
		"document_id",
		documentID,
		documentIDLookupSpec,
		nil,
		nil,
	)
	if callErr != nil {
		return result, callErr
	}
	if format != "md" {
		return result, nil
	}
	data := asMap(result.Data)
	body := asMap(data["body"])
	revision := extractNestedMap(body, "revision")
	content := firstNonEmpty(anyString(revision["content"]), anyString(body["content"]), anyString(body["body_text"]))
	if a.Stdout != nil {
		if _, err := a.Stdout.Write([]byte(content)); err != nil {
			return nil, err
		}
		if !strings.HasSuffix(content, "\n") {
			_, _ = a.Stdout.Write([]byte("\n"))
		}
	}
	return &commandResult{RawWritten: true}, nil
}

func (a *App) runDocsSearchCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	leadingQuery, args := popLeadingPositional(args)
	fs := newSilentFlagSet("docs search")
	var queryFlag, cursorFlag, tagFlag, hostFlag trackedString
	var limitFlag trackedInt
	var knowledge bool
	fs.Var(&queryFlag, "q", "Search query over title, body, and comments")
	fs.Var(&tagFlag, "tag", "Restrict results to documents that include this tag")
	fs.Var(&hostFlag, "host", "Restrict results to documents whose hosts list includes this name")
	fs.Var(&limitFlag, "limit", "Limit the number of returned documents")
	fs.Var(&cursorFlag, "cursor", "Pagination cursor from a previous search response")
	fs.BoolVar(&knowledge, "knowledge", false, "Only documents tagged knowledge")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	query := strings.TrimSpace(queryFlag.value)
	positionals := fs.Args()
	if query == "" {
		query = strings.TrimSpace(leadingQuery)
	}
	if query == "" && len(positionals) > 0 {
		query = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx docs search`")
	}
	if query == "" {
		return nil, errnorm.Usage("invalid_request", "q is required; pass a positional query or `--q`")
	}
	if limitFlag.set && (limitFlag.value < 1 || limitFlag.value > 1000) {
		return nil, errnorm.Usage("invalid_request", "limit must be between 1 and 1000")
	}
	params := make([]queryParam, 0, 6)
	addSingleQuery(&params, "q", query)
	addSingleQuery(&params, "tag", tagFlag.value)
	addSingleQuery(&params, "host", hostFlag.value)
	if knowledge {
		addSingleQuery(&params, "knowledge", "true")
	}
	if limitFlag.set {
		addSingleQuery(&params, "limit", strconv.Itoa(limitFlag.value))
	}
	addSingleQuery(&params, "cursor", cursorFlag.value)
	return a.invokeTypedJSON(ctx, cfg, "docs search", "docs.search", nil, params, nil)
}

func (a *App) runDocsPutCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	leadingPath, args := popLeadingPositional(args)
	fs := newSilentFlagSet("docs put")
	var titleFlag, sourceFlag, handleFlag, bodyFlag, bodyFileFlag, actorIDFlag, verifiedAtFlag trackedString
	var tagsFlag, hostsFlag trackedStrings
	fs.Var(&titleFlag, "title", "Document title")
	fs.Var(&sourceFlag, "source", "Canonical source URL or ref")
	fs.Var(&tagsFlag, "tags", "Document tags, repeatable or comma-separated")
	fs.Var(&hostsFlag, "hosts", "Host names this knowledge fact applies to, repeatable or comma-separated")
	fs.Var(&verifiedAtFlag, "verified-at", "RFC3339 timestamp when this fact was last verified")
	fs.Var(&handleFlag, "handle", "Public document handle used as the idempotency key")
	fs.Var(&bodyFlag, "body", "Inline document body text")
	fs.Var(&bodyFileFlag, "body-file", "Load document content from a local file or stdin with -")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	inputPath := strings.TrimSpace(leadingPath)
	if inputPath == "" && len(positionals) > 0 {
		inputPath = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx docs put`")
	}
	bodyFile := strings.TrimSpace(bodyFileFlag.value)
	inline := strings.TrimSpace(bodyFlag.value)
	if inputPath != "" && bodyFile != "" {
		return nil, errnorm.Usage("invalid_request", "`anx docs put <path>` cannot be combined with `--body-file`")
	}
	if inputPath != "" && inline != "" {
		return nil, errnorm.Usage("invalid_request", "`anx docs put <path>` cannot be combined with `--body`")
	}
	if bodyFile != "" && inline != "" {
		return nil, errnorm.Usage("invalid_request", "`--body` and `--body-file` cannot be combined for `anx docs put`")
	}
	content := ""
	switch {
	case inputPath != "":
		raw, err := a.readBodyInput(inputPath)
		if err != nil {
			return nil, err
		}
		content = string(raw)
	case bodyFile != "":
		raw, err := a.readBodyInput(bodyFile)
		if err != nil {
			return nil, err
		}
		content = string(raw)
	case inline != "":
		content = inline
	default:
		return nil, errnorm.Usage("invalid_request", "document content is required; pass a path, `-` for stdin, `--body`, or `--body-file`")
	}
	if strings.TrimSpace(content) == "" {
		return nil, errnorm.Usage("invalid_request", "document content is required")
	}

	handle := deriveDocsPutHandle(handleFlag.value, firstNonEmpty(inputPath, bodyFile), titleFlag.value)
	if handle == "" {
		return nil, errnorm.Usage("invalid_request", "document handle is required; pass `--handle`, a filename, or `--title`")
	}

	document := map[string]any{}
	if title := strings.TrimSpace(titleFlag.value); title != "" {
		document["title"] = title
	}
	if source := strings.TrimSpace(sourceFlag.value); source != "" {
		document["source"] = source
	}
	if tags := splitDocsTags(tagsFlag.values); len(tags) > 0 {
		document["tags"] = tags
	}
	if hosts := splitDocsTags(hostsFlag.values); len(hosts) > 0 {
		document["hosts"] = hosts
	}
	if verifiedAt := strings.TrimSpace(verifiedAtFlag.value); verifiedAt != "" {
		document["verified_at"] = verifiedAt
	}
	body := map[string]any{
		"document":     document,
		"content":      content,
		"content_type": "text",
	}
	if actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg); err != nil {
		return nil, err
	} else if actorID != "" {
		body["actor_id"] = actorID
	} else if err := finalizeOptionalMutationBodyActorID(body, cfg); err != nil {
		return nil, err
	}

	result, callErr := a.invokeTypedJSON(ctx, cfg, "docs put", "docs.put", map[string]string{"document_id": handle}, nil, body)
	return addResourceURLToResult(cfg, "docs.put", result), callErr
}

func (a *App) runDocsCommentCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	leadingDocumentID, args := popLeadingPositional(args)
	leadingText, args := popLeadingPositional(args)
	fs := newSilentFlagSet("docs comment")
	var documentIDFlag, bodyFlag, replyToFlag, actorIDFlag trackedString
	fs.Var(&documentIDFlag, "document-id", "Document id, handle, or ref")
	fs.Var(&bodyFlag, "body", "Comment text")
	fs.Var(&replyToFlag, "reply-to", "Parent comment id for a reply")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	documentID := firstNonEmpty(strings.TrimSpace(documentIDFlag.value), leadingDocumentID)
	if documentID == "" && len(positionals) > 0 {
		documentID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	text := firstNonEmpty(strings.TrimSpace(bodyFlag.value), leadingText)
	if text == "" && len(positionals) > 0 {
		text = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx docs comment`")
	}
	if err := validateID(documentID, "document id"); err != nil {
		return nil, err
	}
	if text == "" {
		return nil, errnorm.Usage("invalid_request", "comment text is required")
	}
	payload := map[string]any{"text": text}
	if replyTo := strings.TrimSpace(replyToFlag.value); replyTo != "" {
		payload["reply_to"] = replyTo
		payload["parent_id"] = replyTo
	}
	if actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg); err != nil {
		return nil, err
	} else if actorID != "" {
		payload["actor_id"] = actorID
	} else if err := finalizeOptionalMutationBodyActorID(payload, cfg); err != nil {
		return nil, err
	}
	return a.invokeTypedJSONWithIDResolution(ctx, cfg, "docs comment", "docs.comments.create", "document_id", documentID, documentIDLookupSpec, nil, payload)
}

func (a *App) runDocsCommentsListCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	leadingDocumentID, args := popLeadingPositional(args)
	fs := newSilentFlagSet("docs comments")
	var documentIDFlag, cursorFlag trackedString
	var limitFlag trackedInt
	fs.Var(&documentIDFlag, "document-id", "Document id, handle, or ref")
	fs.Var(&limitFlag, "limit", "Limit the number of returned comments")
	fs.Var(&cursorFlag, "cursor", "Pagination cursor from a previous comments response")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	documentID := firstNonEmpty(strings.TrimSpace(documentIDFlag.value), leadingDocumentID)
	if documentID == "" && len(positionals) > 0 {
		documentID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx docs comments`")
	}
	if err := validateID(documentID, "document id"); err != nil {
		return nil, err
	}
	if limitFlag.set && (limitFlag.value < 1 || limitFlag.value > 1000) {
		return nil, errnorm.Usage("invalid_request", "limit must be between 1 and 1000")
	}
	query := make([]queryParam, 0, 2)
	if limitFlag.set {
		addSingleQuery(&query, "limit", strconv.Itoa(limitFlag.value))
	}
	addSingleQuery(&query, "cursor", cursorFlag.value)
	return a.invokeTypedJSONWithIDResolution(ctx, cfg, "docs comments", "docs.comments.list", "document_id", documentID, documentIDLookupSpec, query, nil)
}

func (a *App) runDocsCommentsEditCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	leadingDocumentID, args := popLeadingPositional(args)
	leadingCommentID, args := popLeadingPositional(args)
	fs := newSilentFlagSet("docs comments edit")
	var documentIDFlag, commentIDFlag, bodyFlag, actorIDFlag trackedString
	fs.Var(&documentIDFlag, "document-id", "Document id, handle, or ref")
	fs.Var(&commentIDFlag, "comment-id", "Comment id or event ref")
	fs.Var(&bodyFlag, "body", "Replacement comment text")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	documentID := firstNonEmpty(strings.TrimSpace(documentIDFlag.value), leadingDocumentID)
	if documentID == "" && len(positionals) > 0 {
		documentID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	commentID := firstNonEmpty(strings.TrimSpace(commentIDFlag.value), leadingCommentID)
	if commentID == "" && len(positionals) > 0 {
		commentID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	text := strings.TrimSpace(bodyFlag.value)
	if text == "" && len(positionals) > 0 {
		text = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx docs comments edit`")
	}
	if err := validateID(documentID, "document id"); err != nil {
		return nil, err
	}
	if err := validateID(commentID, "comment id"); err != nil {
		return nil, err
	}
	if text == "" {
		return nil, errnorm.Usage("invalid_request", "comment text is required")
	}
	payload := map[string]any{"text": text}
	if actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg); err != nil {
		return nil, err
	} else if actorID != "" {
		payload["actor_id"] = actorID
	} else if err := finalizeOptionalMutationBodyActorID(payload, cfg); err != nil {
		return nil, err
	}
	pathParams := map[string]string{"document_id": documentID, "comment_id": commentID}
	result, err := a.invokeTypedJSON(ctx, cfg, "docs comments edit", "docs.comments.update", pathParams, nil, payload)
	if err == nil || !isRemoteNotFound(err) {
		return result, err
	}
	resolvedID, resolveErr := a.resolveUniqueResourcePrefix(ctx, cfg, documentIDLookupSpec, documentID, pathParams)
	if resolveErr != nil {
		return nil, resolveErr
	}
	if strings.TrimSpace(resolvedID) == "" || resolvedID == documentID {
		return result, err
	}
	pathParams["document_id"] = resolvedID
	return a.invokeTypedJSON(ctx, cfg, "docs comments edit", "docs.comments.update", pathParams, nil, payload)
}

func (a *App) runDocsCommentsDeleteCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	leadingDocumentID, args := popLeadingPositional(args)
	leadingCommentID, args := popLeadingPositional(args)
	fs := newSilentFlagSet("docs comments delete")
	var documentIDFlag, commentIDFlag, actorIDFlag trackedString
	fs.Var(&documentIDFlag, "document-id", "Document id, handle, or ref")
	fs.Var(&commentIDFlag, "comment-id", "Comment id or event ref")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	documentID := firstNonEmpty(strings.TrimSpace(documentIDFlag.value), leadingDocumentID)
	if documentID == "" && len(positionals) > 0 {
		documentID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	commentID := firstNonEmpty(strings.TrimSpace(commentIDFlag.value), leadingCommentID)
	if commentID == "" && len(positionals) > 0 {
		commentID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx docs comments delete`")
	}
	if err := validateID(documentID, "document id"); err != nil {
		return nil, err
	}
	if err := validateID(commentID, "comment id"); err != nil {
		return nil, err
	}
	payload := map[string]any{}
	if actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg); err != nil {
		return nil, err
	} else if actorID != "" {
		payload["actor_id"] = actorID
	} else if err := finalizeOptionalMutationBodyActorID(payload, cfg); err != nil {
		return nil, err
	}
	if len(payload) == 0 {
		payload = nil
	}
	pathParams := map[string]string{"document_id": documentID, "comment_id": commentID}
	result, err := a.invokeTypedJSON(ctx, cfg, "docs comments delete", "docs.comments.delete", pathParams, nil, payload)
	if err == nil || !isRemoteNotFound(err) {
		return result, err
	}
	resolvedID, resolveErr := a.resolveUniqueResourcePrefix(ctx, cfg, documentIDLookupSpec, documentID, pathParams)
	if resolveErr != nil {
		return nil, resolveErr
	}
	if strings.TrimSpace(resolvedID) == "" || resolvedID == documentID {
		return result, err
	}
	pathParams["document_id"] = resolvedID
	return a.invokeTypedJSON(ctx, cfg, "docs comments delete", "docs.comments.delete", pathParams, nil, payload)
}

func (a *App) runDocsCommentsReplyCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	leadingDocumentID, args := popLeadingPositional(args)
	leadingCommentID, args := popLeadingPositional(args)
	leadingText, args := popLeadingPositional(args)
	fs := newSilentFlagSet("docs comments reply")
	var documentIDFlag, commentIDFlag, bodyFlag, actorIDFlag trackedString
	fs.Var(&documentIDFlag, "document-id", "Document id, handle, or ref")
	fs.Var(&commentIDFlag, "comment-id", "Parent comment id")
	fs.Var(&bodyFlag, "body", "Reply text")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	documentID := firstNonEmpty(strings.TrimSpace(documentIDFlag.value), leadingDocumentID)
	if documentID == "" && len(positionals) > 0 {
		documentID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	commentID := firstNonEmpty(strings.TrimSpace(commentIDFlag.value), leadingCommentID)
	if commentID == "" && len(positionals) > 0 {
		commentID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	text := firstNonEmpty(strings.TrimSpace(bodyFlag.value), leadingText)
	if text == "" && len(positionals) > 0 {
		text = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx docs comments reply`")
	}
	if err := validateID(documentID, "document id"); err != nil {
		return nil, err
	}
	if err := validateID(commentID, "comment id"); err != nil {
		return nil, err
	}
	if text == "" {
		return nil, errnorm.Usage("invalid_request", "reply text is required")
	}
	payload := map[string]any{"text": text}
	if actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg); err != nil {
		return nil, err
	} else if actorID != "" {
		payload["actor_id"] = actorID
	} else if err := finalizeOptionalMutationBodyActorID(payload, cfg); err != nil {
		return nil, err
	}
	pathParams := map[string]string{"document_id": documentID, "comment_id": commentID}
	result, err := a.invokeTypedJSON(ctx, cfg, "docs comments reply", "docs.comments.reply", pathParams, nil, payload)
	if err == nil || !isRemoteNotFound(err) {
		return result, err
	}
	resolvedID, resolveErr := a.resolveUniqueResourcePrefix(ctx, cfg, documentIDLookupSpec, documentID, pathParams)
	if resolveErr != nil {
		return nil, resolveErr
	}
	if strings.TrimSpace(resolvedID) == "" || resolvedID == documentID {
		return result, err
	}
	pathParams["document_id"] = resolvedID
	return a.invokeTypedJSON(ctx, cfg, "docs comments reply", "docs.comments.reply", pathParams, nil, payload)
}

func deriveDocsPutHandle(handleFlag, path, title string) string {
	if handle := strings.TrimSpace(strings.TrimPrefix(handleFlag, "document:")); handle != "" {
		return handle
	}
	if path != "" && path != "-" {
		stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if slug := slugifyDocsHandle(stem); slug != "" {
			return slug
		}
	}
	return slugifyDocsHandle(title)
}

func slugifyDocsHandle(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	var b strings.Builder
	lastDash := false
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case unicode.IsSpace(r) || r == '-' || r == '_' || r == '/' || r == '.':
			if b.Len() > 0 && !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		default:
			if b.Len() > 0 && !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "search" {
		return ""
	}
	return out
}

func splitDocsTags(values []string) []string {
	out := make([]string, 0, len(values))
	for _, raw := range values {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return uniqueStringsInOrder(out)
}
