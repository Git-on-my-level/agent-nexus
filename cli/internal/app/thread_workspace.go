package app

import (
	"context"

	"organization-autorunner-cli/internal/config"
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

func (a *App) buildThreadWorkspaceResult(ctx context.Context, cfg config.Resolved, selection threadRecommendationsSelection) (*commandResult, error) {
	threadIDs, err := a.resolveThreadContextSelection(ctx, cfg, "threads workspace", selection.threadContextSelection, false)
	if err != nil {
		return nil, err
	}

	contextResult, callErr := a.invokeTypedJSONWithIDResolution(
		ctx,
		cfg,
		"threads context",
		"threads.context",
		"thread_id",
		threadIDs[0],
		threadIDLookupSpec,
		threadContextQuery(selection.threadContextSelection),
		nil,
	)
	if callErr != nil {
		return nil, callErr
	}

	data := asMap(contextResult.Data)
	body := asMap(data["body"])
	if body == nil {
		return contextResult, nil
	}
	addThreadContextCollaborationSummary(body)

	thread := extractNestedMap(body, "thread")
	resolvedThreadID := firstNonEmpty(resolvedThreadIDFromContextBody(body, threadIDs[0]), threadIDs[0])
	collaboration := asMap(body["collaboration_summary"])
	recommendations := normalizeRecommendationReviewEvents(asSlice(collaboration["recommendations"]))
	decisionRequests := normalizeRecommendationReviewEvents(asSlice(collaboration["decision_requests"]))
	decisions := normalizeRecommendationReviewEvents(asSlice(collaboration["decisions"]))

	inboxResult, err := a.invokeTypedJSON(ctx, cfg, "inbox list", "inbox.list", nil, nil, nil)
	if err != nil {
		return nil, err
	}
	inboxData := asMap(inboxResult.Data)
	inboxBody := extractNestedMap(inboxData, "body")
	inboxItems := filteredInboxItems(asSlice(inboxBody["items"]), []string{resolvedThreadID}, nil)
	pendingDecisions := filteredInboxItems(inboxItems, nil, []string{"decision_needed"})
	relatedThreadReview, err := a.collectRelatedThreadRecommendationReview(ctx, cfg, resolvedThreadID, body, selection.threadContextSelection, selection.includeRelatedEventContent)
	if err != nil {
		return nil, err
	}

	contextSection := cloneMap(body)
	delete(contextSection, "thread")
	delete(contextSection, "collaboration_summary")
	delete(contextSection, "full_id")

	workspaceBody := map[string]any{
		"thread_id":    resolvedThreadID,
		"thread":       thread,
		"full_id":      selection.fullID,
		"full_summary": selection.fullSummary,
		"context":      contextSection,
		"collaboration": map[string]any{
			"recommendations":   recommendations,
			"decision_requests": decisionRequests,
			"decisions":         decisions,
		},
		"inbox": map[string]any{
			"thread_id": resolvedThreadID,
			"items":     inboxItems,
			"count":     len(inboxItems),
			"full_id":   selection.fullID,
		},
		"pending_decisions": map[string]any{
			"items": pendingDecisions,
			"count": len(pendingDecisions),
		},
		"related_threads":           relatedThreadReview["related_threads"],
		"related_recommendations":   relatedThreadReview["related_recommendations"],
		"related_decision_requests": relatedThreadReview["related_decision_requests"],
		"related_decisions":         relatedThreadReview["related_decisions"],
		"total_review_items":        len(recommendations) + len(decisionRequests) + len(decisions) + len(pendingDecisions) + intValue(relatedThreadReview["total_review_items"]),
		"follow_up":                 recommendationFollowUpHints(resolvedThreadID, recommendations, decisionRequests, decisions),
		"context_source":            "threads.context",
		"inbox_source":              "inbox.list",
	}
	if selection.includeRelatedEventContent {
		workspaceBody["related_event_content_enabled"] = true
		workspaceBody["related_event_content_count"] = intValue(relatedThreadReview["related_event_content_count"])
	}
	if warningCount := intValue(relatedThreadReview["warning_count"]); warningCount > 0 {
		workspaceBody["warnings"] = map[string]any{
			"items": relatedThreadReview["warnings"],
			"count": warningCount,
		}
	}

	data["body"] = workspaceBody
	contextResult.Data = data
	return contextResult, nil
}
