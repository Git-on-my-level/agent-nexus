package server

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"organization-autorunner-core/internal/primitives"
)

const derivedProjectionMaxAge = time.Minute

func refreshDerivedTopicProjection(ctx context.Context, opts handlerOptions, threadID string, now time.Time, actorID string) error {
	if opts.primitiveStore == nil {
		return nil
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}

	thread, err := opts.primitiveStore.GetThread(ctx, threadID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			return nil
		}
		return err
	}

	events, err := opts.primitiveStore.ListEventsByThread(ctx, threadID)
	if err != nil {
		return err
	}

	latestActivity := latestThreadActivityFromEvents(events)
	latestStaleException := latestStaleExceptionByThread(events)
	horizon := resolvedInboxRiskHorizon(opts)
	workItems, workItemSummary, latestWorkItemActivity, err := summarizeThreadWorkItems(ctx, opts, threadID, now, horizon)
	if err != nil {
		return err
	}
	activityAt := latestActivity[threadID]
	if latestWorkItemActivity.After(activityAt) {
		activityAt = latestWorkItemActivity
	}
	lastStale := latestStaleException[threadID]
	stale := isThreadStaleAt(now, thread, activityAt)

	inboxItems, err := deriveThreadInboxItems(opts, events, workItems, activityAt, now)
	if err != nil {
		return err
	}

	documents, _, err := opts.primitiveStore.ListDocuments(ctx, primitives.DocumentListFilter{ThreadID: threadID})
	if err != nil {
		return err
	}

	recentEvents, err := opts.primitiveStore.ListRecentEventsByThread(ctx, threadID, defaultThreadContextMaxEvents)
	if err != nil {
		return err
	}
	collaboration := buildThreadWorkspaceCollaborationSummary(map[string]any{"recent_events": recentEvents})

	rawKeyArtifacts, _ := extractStringSlice(thread["key_artifacts"])
	pendingDecisions := 0
	for _, item := range inboxItems {
		if item.Category == "action_needed" {
			pendingDecisions++
		}
	}

	generatedAt := now.Format(time.RFC3339Nano)
	projectionItems := make([]primitives.DerivedInboxItem, 0, len(inboxItems))
	for _, item := range inboxItems {
		projectionItems = append(projectionItems, primitives.DerivedInboxItem{
			ID:            item.ID,
			ThreadID:      threadID,
			Category:      item.Category,
			TriggerAt:     item.TriggerAt.Format(time.RFC3339Nano),
			DueAt:         formatOptionalTime(item.DueAt),
			HasDueAt:      item.HasDueAt,
			SourceEventID: strings.TrimSpace(anyString(item.Data["source_event_id"])),
			SourceCardID:  strings.TrimSpace(anyString(item.Data["card_id"])),
			GeneratedAt:   generatedAt,
			Data:          cloneWorkspaceMap(item.Data),
		})
	}

	summary := map[string]any{
		"thread_id":                  threadID,
		"stale":                      stale,
		"inbox_count":                len(projectionItems),
		"pending_decision_count":     pendingDecisions,
		"recommendation_count":       workspaceIntValue(collaboration["recommendation_count"]),
		"decision_request_count":     workspaceIntValue(collaboration["decision_request_count"]),
		"decision_count":             workspaceIntValue(collaboration["decision_count"]),
		"artifact_count":             len(rawKeyArtifacts),
		"open_work_item_count":       workItemSummary.OpenCount,
		"at_risk_work_item_count":    workItemSummary.AtRiskCount,
		"due_soon_work_item_count":   workItemSummary.DueSoonCount,
		"overdue_work_item_count":    workItemSummary.OverdueCount,
		"blocked_work_item_count":    workItemSummary.BlockedCount,
		"stale_work_item_count":      workItemSummary.StaleCount,
		"document_count":             len(documents),
		"last_activity_at":           formatOptionalTime(activityAt),
		"last_work_item_activity_at": formatOptionalTime(latestWorkItemActivity),
		"latest_stale_exception_at":  formatOptionalTime(lastStale),
		"generated_at":               generatedAt,
	}
	summary["open_card_count"] = workItemSummary.OpenCount

	if err := opts.primitiveStore.ReplaceDerivedInboxItems(ctx, threadID, projectionItems); err != nil {
		return err
	}
	return opts.primitiveStore.PutDerivedTopicProjection(ctx, primitives.DerivedTopicProjection{
		ThreadID:               threadID,
		Stale:                  stale,
		LastActivityAt:         formatOptionalTime(activityAt),
		LatestStaleExceptionAt: formatOptionalTime(lastStale),
		InboxCount:             len(projectionItems),
		PendingDecisionCount:   pendingDecisions,
		RecommendationCount:    workspaceIntValue(collaboration["recommendation_count"]),
		DecisionRequestCount:   workspaceIntValue(collaboration["decision_request_count"]),
		DecisionCount:          workspaceIntValue(collaboration["decision_count"]),
		ArtifactCount:          len(rawKeyArtifacts),
		OpenCardCount:          workItemSummary.OpenCount,
		DocumentCount:          len(documents),
		GeneratedAt:            generatedAt,
		Data:                   summary,
	})
}

func deriveThreadInboxItems(opts handlerOptions, events []map[string]any, workItems []map[string]any, activityAt time.Time, now time.Time) ([]derivedInboxItem, error) {
	ackedAt := latestInboxAcknowledgments(events)
	decidedIDs := decidedInboxItemIDs(events)
	items := make([]derivedInboxItem, 0)

	for _, event := range events {
		eventType, _ := event["type"].(string)
		switch eventType {
		case "decision_needed", "intervention_needed", "exception_raised", agentAskRequestedEventType:
			var (
				item derivedInboxItem
				ok   bool
			)
			if eventType == agentAskRequestedEventType {
				item, ok = deriveAgentAskInboxItem(event)
			} else {
				item, ok = deriveEventBackedInboxItem(event)
			}
			if !ok {
				continue
			}
			if eventType == "exception_raised" && isStaleTopicException(event) {
				if !activityAt.IsZero() && activityAt.After(item.TriggerAt) {
					continue
				}
			}
			if isSuppressedByAck(item, ackedAt) {
				continue
			}
			if _, decided := decidedIDs[item.ID]; decided {
				continue
			}
			items = append(items, item)
		}
	}

	for _, workItem := range workItems {
		item, ok := deriveWorkItemRiskInboxItem(workItem, now, resolvedInboxRiskHorizon(opts))
		if !ok || isSuppressedByAck(item, ackedAt) {
			continue
		}
		if _, decided := decidedIDs[item.ID]; decided {
			continue
		}
		items = append(items, item)
	}

	sortInboxItems(items)
	return items, nil
}

func loadDerivedTopicProjection(ctx context.Context, opts handlerOptions, threadID string) (primitives.DerivedTopicProjection, error) {
	projection, err := opts.primitiveStore.GetDerivedTopicProjection(ctx, threadID)
	if err == nil {
		return projection, nil
	}
	if !errors.Is(err, primitives.ErrNotFound) {
		return primitives.DerivedTopicProjection{}, err
	}
	return defaultDerivedTopicProjection(threadID), nil
}

func listDerivedTopicProjections(ctx context.Context, opts handlerOptions, threadIDs []string) (map[string]primitives.DerivedTopicProjection, error) {
	projections, err := opts.primitiveStore.ListDerivedTopicProjections(ctx, threadIDs)
	if err != nil {
		return nil, err
	}
	for _, threadID := range threadIDs {
		threadID = strings.TrimSpace(threadID)
		if threadID == "" {
			continue
		}
		if _, ok := projections[threadID]; !ok {
			projections[threadID] = defaultDerivedTopicProjection(threadID)
		}
	}
	return projections, nil
}

func rebuildDerivedProjections(ctx context.Context, opts handlerOptions, now time.Time, actorID string) error {
	if _, err := emitStaleThreadExceptions(ctx, opts, now, actorID); err != nil {
		return err
	}
	threads, _, err := opts.primitiveStore.ListThreads(ctx, primitives.ThreadListFilter{})
	if err != nil {
		return err
	}
	for _, thread := range threads {
		threadID := strings.TrimSpace(anyString(thread["id"]))
		if threadID == "" {
			continue
		}
		if err := refreshDerivedTopicProjection(ctx, opts, threadID, now, actorID); err != nil {
			return fmt.Errorf("refresh derived projection for thread %s: %w", threadID, err)
		}
	}
	return nil
}

func defaultDerivedTopicProjection(threadID string) primitives.DerivedTopicProjection {
	threadID = strings.TrimSpace(threadID)
	generatedAt := time.Now().UTC().Format(time.RFC3339Nano)
	return primitives.DerivedTopicProjection{
		ThreadID:    threadID,
		GeneratedAt: generatedAt,
		Data: map[string]any{
			"thread_id":                  threadID,
			"stale":                      false,
			"inbox_count":                0,
			"pending_decision_count":     0,
			"recommendation_count":       0,
			"decision_request_count":     0,
			"decision_count":             0,
			"artifact_count":             0,
			"open_work_item_count":       0,
			"at_risk_work_item_count":    0,
			"due_soon_work_item_count":   0,
			"overdue_work_item_count":    0,
			"blocked_work_item_count":    0,
			"stale_work_item_count":      0,
			"document_count":             0,
			"last_activity_at":           "",
			"last_work_item_activity_at": "",
			"latest_stale_exception_at":  "",
			"generated_at":               generatedAt,
			"open_card_count":            0,
		},
	}
}

type threadWorkItemSummary struct {
	OpenCount    int
	AtRiskCount  int
	DueSoonCount int
	OverdueCount int
	BlockedCount int
	StaleCount   int
}

func summarizeThreadWorkItems(ctx context.Context, opts handlerOptions, threadID string, now time.Time, riskHorizon time.Duration) ([]map[string]any, threadWorkItemSummary, time.Time, error) {
	if opts.primitiveStore == nil {
		return nil, threadWorkItemSummary{}, time.Time{}, nil
	}

	memberships, err := opts.primitiveStore.ListBoardMembershipsByThread(ctx, threadID)
	if err != nil {
		return nil, threadWorkItemSummary{}, time.Time{}, err
	}

	workItems := make([]map[string]any, 0, len(memberships))
	summary := threadWorkItemSummary{}
	latestActivity := time.Time{}
	windowStart, hasWindow := threadFreshnessWindowStart(ctx, opts, threadID, now)
	for _, membership := range memberships {
		card := cloneWorkspaceMap(membership.Card)
		if card == nil {
			continue
		}
		workItems = append(workItems, card)
		if updatedAt, ok := parseTimestamp(card["updated_at"]); ok && updatedAt.After(latestActivity) {
			latestActivity = updatedAt
		}
		if !boardCardCountsAsOpenWorkItem(card) {
			continue
		}
		summary.OpenCount++
		riskState, _, _ := boardCardRiskState(card, now, riskHorizon)
		switch riskState {
		case "overdue":
			summary.AtRiskCount++
			summary.OverdueCount++
		case "due_soon":
			summary.AtRiskCount++
			summary.DueSoonCount++
		case "blocked":
			summary.AtRiskCount++
			summary.BlockedCount++
		}
		if hasWindow {
			if updatedAt, ok := parseTimestamp(card["updated_at"]); !ok || updatedAt.Before(windowStart) {
				summary.StaleCount++
			}
		}
	}
	return workItems, summary, latestActivity, nil
}

func resolvedInboxRiskHorizon(opts handlerOptions) time.Duration {
	horizon := opts.inboxRiskHorizon
	if horizon <= 0 {
		horizon = defaultInboxRiskHorizon
	}
	return horizon
}

func threadFreshnessWindowStart(ctx context.Context, opts handlerOptions, threadID string, now time.Time) (time.Time, bool) {
	if opts.primitiveStore == nil {
		return time.Time{}, false
	}
	thread, err := opts.primitiveStore.GetThread(ctx, threadID)
	if err != nil {
		return time.Time{}, false
	}
	cadence, _ := thread["cadence"].(string)
	nextCheckInText, _ := thread["next_check_in_at"].(string)
	nextCheckInAt, err := time.Parse(time.RFC3339, strings.TrimSpace(nextCheckInText))
	if err != nil {
		return time.Time{}, false
	}
	return cadenceWindowStart(cadence, now, nextCheckInAt)
}
func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func derivedTopicProjectionExpired(projection primitives.DerivedTopicProjection, now time.Time) bool {
	generatedAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(projection.GeneratedAt))
	if err != nil {
		return true
	}
	return now.Sub(generatedAt) >= derivedProjectionMaxAge
}

type topicProjectionState struct {
	Projection primitives.DerivedTopicProjection
	Refresh    primitives.TopicProjectionRefreshStatus
	Freshness  map[string]any
	Status     string
}

func loadTopicProjectionState(ctx context.Context, opts handlerOptions, threadID string) (topicProjectionState, error) {
	states, err := loadTopicProjectionStates(ctx, opts, []string{threadID})
	if err != nil {
		return topicProjectionState{}, err
	}
	return states[strings.TrimSpace(threadID)], nil
}

func loadTopicProjectionStates(ctx context.Context, opts handlerOptions, threadIDs []string) (map[string]topicProjectionState, error) {
	threadIDs = uniqueServerStrings(threadIDs)
	out := make(map[string]topicProjectionState, len(threadIDs))
	if opts.primitiveStore == nil || len(threadIDs) == 0 {
		for _, threadID := range threadIDs {
			out[threadID] = buildTopicProjectionState(threadID, primitives.DerivedTopicProjection{}, false, primitives.TopicProjectionRefreshStatus{}, false)
		}
		return out, nil
	}

	projections, err := opts.primitiveStore.ListDerivedTopicProjections(ctx, threadIDs)
	if err != nil {
		return nil, err
	}
	refreshStatuses, err := opts.primitiveStore.GetTopicProjectionRefreshStatuses(ctx, threadIDs)
	if err != nil {
		return nil, err
	}

	for _, threadID := range threadIDs {
		projection, hasProjection := projections[threadID]
		refresh, hasRefresh := refreshStatuses[threadID]
		out[threadID] = buildTopicProjectionState(threadID, projection, hasProjection, refresh, hasRefresh)
	}
	return out, nil
}

func buildTopicProjectionState(threadID string, projection primitives.DerivedTopicProjection, hasProjection bool, refresh primitives.TopicProjectionRefreshStatus, hasRefresh bool) topicProjectionState {
	threadID = strings.TrimSpace(threadID)
	if !hasProjection {
		projection = primitives.DerivedTopicProjection{
			ThreadID: threadID,
			Data:     map[string]any{"thread_id": threadID},
		}
	}
	if projection.Data == nil {
		projection.Data = map[string]any{"thread_id": threadID}
	}

	isDirty := hasRefresh && refresh.IsDirty()
	inProgress := hasRefresh && refresh.InProgress()
	hasError := hasRefresh && strings.TrimSpace(refresh.LastErrorMessage) != "" && isDirty

	status := "missing"
	switch {
	case hasError:
		status = "error"
	case inProgress || isDirty:
		status = "pending"
	case hasProjection || (hasRefresh && refresh.MaterializedGeneration > 0):
		status = "current"
	}

	freshness := map[string]any{
		"thread_id":               threadID,
		"status":                  status,
		"generated_at":            nullableStringValue(projection.GeneratedAt),
		"queued_at":               nullableStringValue(refresh.QueuedAt),
		"started_at":              nullableStringValue(refresh.StartedAt),
		"completed_at":            nullableStringValue(refresh.LastCompletedAt),
		"last_error_at":           nullableStringValue(refresh.LastErrorAt),
		"last_error":              nullableStringValue(refresh.LastErrorMessage),
		"materialized":            hasProjection,
		"refresh_in_flight":       inProgress,
		"is_dirty":                isDirty,
		"in_progress":             inProgress,
		"desired_generation":      refresh.DesiredGeneration,
		"materialized_generation": refresh.MaterializedGeneration,
	}
	if refresh.InProgressGeneration != nil {
		freshness["in_progress_generation"] = *refresh.InProgressGeneration
	} else {
		freshness["in_progress_generation"] = nil
	}

	return topicProjectionState{
		Projection: projection,
		Refresh:    refresh,
		Freshness:  freshness,
		Status:     status,
	}
}

func aggregateTopicProjectionFreshness(states map[string]topicProjectionState, threadIDs []string) map[string]any {
	threadIDs = uniqueServerStrings(threadIDs)
	if len(threadIDs) == 0 {
		return map[string]any{
			"status":       "current",
			"thread_count": 0,
			"threads":      []map[string]any{},
		}
	}

	threads := make([]map[string]any, 0, len(threadIDs))
	aggregateStatus := "current"
	for _, threadID := range threadIDs {
		state := states[threadID]
		threads = append(threads, cloneWorkspaceMap(state.Freshness))
		if projectionFreshnessRank(state.Status) > projectionFreshnessRank(aggregateStatus) {
			aggregateStatus = state.Status
		}
	}
	sort.SliceStable(threads, func(i int, j int) bool {
		return strings.TrimSpace(anyString(threads[i]["thread_id"])) < strings.TrimSpace(anyString(threads[j]["thread_id"]))
	})

	return map[string]any{
		"status":       aggregateStatus,
		"thread_count": len(threadIDs),
		"threads":      threads,
	}
}

func projectionFreshnessRank(status string) int {
	switch strings.TrimSpace(status) {
	case "error":
		return 3
	case "pending":
		return 2
	case "missing":
		return 1
	default:
		return 0
	}
}

func markTopicProjectionsDirty(ctx context.Context, opts handlerOptions, queuedAt time.Time, threadIDs ...string) error {
	if opts.primitiveStore == nil {
		return nil
	}

	threadIDs = uniqueServerStrings(threadIDs)
	if len(threadIDs) == 0 {
		return nil
	}
	if err := opts.primitiveStore.MarkTopicProjectionsDirty(ctx, threadIDs, queuedAt); err != nil {
		return err
	}
	if opts.projectionMaintainer != nil {
		opts.projectionMaintainer.Notify()
	}
	return nil
}

func enqueueTopicProjectionsBestEffort(ctx context.Context, opts handlerOptions, threadIDs []string, queuedAt time.Time) {
	_ = markTopicProjectionsDirty(ctx, opts, queuedAt, threadIDs...)
}

func uniqueServerStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
