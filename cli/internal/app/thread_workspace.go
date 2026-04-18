package app

import (
	"context"
	"fmt"

	"agent-nexus-cli/internal/config"
)

func (a *App) runThreadsWorkspaceCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	selection, err := parseThreadRecommendationsArgs(args)
	if err != nil {
		return nil, err
	}
	result, err := a.buildThreadWorkspaceResult(ctx, cfg, selection)
	if err != nil {
		return nil, err
	}
	data := asMap(result.Data)
	body := extractNestedMap(data, "body")
	if body == nil {
		return result, nil
	}
	result.Text = formatTypedCommandText(
		"threads.workspace",
		intValue(data["status_code"]),
		headerValues(data["headers"]),
		body,
		cfg.Verbose,
		cfg.Headers,
	)
	return result, nil
}

func (a *App) runThreadsReviewCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	selection, err := parseThreadRecommendationsArgs(args)
	if err != nil {
		return nil, err
	}
	selection.includeRelatedEventContent = true
	selection.fullSummary = true

	result, err := a.buildThreadWorkspaceResult(ctx, cfg, selection)
	if err != nil {
		return nil, err
	}
	data := asMap(result.Data)
	body := extractNestedMap(data, "body")
	if body == nil {
		return result, nil
	}
	body["review_mode"] = true
	body["related_event_content_enabled"] = true
	data["body"] = body
	result.Data = data
	result.Text = formatTypedCommandText(
		"threads.review",
		intValue(data["status_code"]),
		headerValues(data["headers"]),
		body,
		cfg.Verbose,
		cfg.Headers,
	)
	return result, nil
}

func threadWorkspaceQuery(selection threadRecommendationsSelection) []queryParam {
	query := threadContextQuery(selection.threadContextSelection)
	if selection.includeRelatedEventContent {
		addSingleQuery(&query, "include_related_event_content", fmt.Sprintf("%t", true))
	}
	return query
}

func (a *App) buildThreadWorkspaceResult(ctx context.Context, cfg config.Resolved, selection threadRecommendationsSelection) (*commandResult, error) {
	threadIDs, err := a.resolveThreadContextSelection(ctx, cfg, "threads workspace", selection.threadContextSelection, false)
	if err != nil {
		return nil, err
	}

	workspaceResult, callErr := a.invokeTypedJSONWithIDResolution(
		ctx,
		cfg,
		"threads workspace",
		"threads.workspace",
		"thread_id",
		threadIDs[0],
		threadIDLookupSpec,
		threadWorkspaceQuery(selection),
		nil,
	)
	if callErr != nil {
		return nil, callErr
	}

	data := asMap(workspaceResult.Data)
	body := asMap(data["body"])
	if body == nil {
		return workspaceResult, nil
	}
	body["full_id"] = selection.fullID
	body["full_summary"] = selection.fullSummary
	if inbox := asMap(body["inbox"]); inbox != nil {
		inbox["full_id"] = selection.fullID
		body["inbox"] = inbox
	}
	data["body"] = body
	workspaceResult.Data = data
	return workspaceResult, nil
}
