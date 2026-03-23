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

func refreshDerivedThreadProjection(ctx context.Context, opts handlerOptions, threadID string, now time.Time, actorID string) error {
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
	activityAt := latestActivity[threadID]
	lastStale := latestStaleException[threadID]
	stale := isThreadStaleAt(now, thread, activityAt)

	horizon := opts.inboxRiskHorizon
	if horizon <= 0 {
		horizon = defaultInboxRiskHorizon
	}
	inboxItems, err := deriveThreadInboxItems(ctx, opts, threadID, events, now, horizon)
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
	rawOpenCommitments, _ := extractStringSlice(thread["open_commitments"])
	pendingDecisions := 0
	for _, item := range inboxItems {
		if item.Category == "decision_needed" {
			pendingDecisions++
		}
	}

	generatedAt := now.Format(time.RFC3339Nano)
	projectionItems := make([]primitives.DerivedInboxItem, 0, len(inboxItems))
	for _, item := range inboxItems {
		projectionItems = append(projectionItems, primitives.DerivedInboxItem{
			ID:                 item.ID,
			ThreadID:           threadID,
			Category:           item.Category,
			TriggerAt:          item.TriggerAt.Format(time.RFC3339Nano),
			DueAt:              formatOptionalTime(item.DueAt),
			HasDueAt:           item.HasDueAt,
			SourceEventID:      strings.TrimSpace(anyString(item.Data["source_event_id"])),
			SourceCommitmentID: strings.TrimSpace(anyString(item.Data["commitment_id"])),
			GeneratedAt:        generatedAt,
			Data:               cloneWorkspaceMap(item.Data),
		})
	}

	summary := map[string]any{
		"thread_id":                 threadID,
		"stale":                     stale,
		"inbox_count":               len(projectionItems),
		"pending_decision_count":    pendingDecisions,
		"recommendation_count":      workspaceIntValue(collaboration["recommendation_count"]),
		"decision_request_count":    workspaceIntValue(collaboration["decision_request_count"]),
		"decision_count":            workspaceIntValue(collaboration["decision_count"]),
		"artifact_count":            len(rawKeyArtifacts),
		"open_commitment_count":     len(rawOpenCommitments),
		"document_count":            len(documents),
		"last_activity_at":          formatOptionalTime(activityAt),
		"latest_stale_exception_at": formatOptionalTime(lastStale),
		"generated_at":              generatedAt,
	}

	if err := opts.primitiveStore.ReplaceDerivedInboxItems(ctx, threadID, projectionItems); err != nil {
		return err
	}
	return opts.primitiveStore.PutDerivedThreadProjection(ctx, primitives.DerivedThreadProjection{
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
		OpenCommitmentCount:    len(rawOpenCommitments),
		DocumentCount:          len(documents),
		GeneratedAt:            generatedAt,
		Data:                   summary,
	})
}

func deriveThreadInboxItems(ctx context.Context, opts handlerOptions, threadID string, events []map[string]any, now time.Time, riskHorizon time.Duration) ([]derivedInboxItem, error) {
	ackedAt := latestInboxAcknowledgments(events)
	decidedIDs := decidedInboxItemIDs(events)
	latestActivity := latestThreadActivityFromEvents(events)
	items := make([]derivedInboxItem, 0)

	for _, event := range events {
		eventType, _ := event["type"].(string)
		switch eventType {
		case "decision_needed", "exception_raised":
			item, ok := deriveEventBackedInboxItem(event)
			if !ok {
				continue
			}
			if eventType == "exception_raised" && isStaleThreadException(event) {
				if activityAt, exists := latestActivity[threadID]; exists && activityAt.After(item.TriggerAt) {
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

	commitments, err := opts.primitiveStore.ListCommitments(ctx, primitives.CommitmentListFilter{ThreadID: threadID})
	if err != nil {
		return nil, err
	}
	for _, commitment := range commitments {
		item, ok := deriveCommitmentRiskInboxItem(commitment, now, riskHorizon)
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

func loadDerivedThreadProjection(ctx context.Context, opts handlerOptions, threadID string) (primitives.DerivedThreadProjection, error) {
	projection, err := opts.primitiveStore.GetDerivedThreadProjection(ctx, threadID)
	if err == nil {
		return projection, nil
	}
	if !errors.Is(err, primitives.ErrNotFound) {
		return primitives.DerivedThreadProjection{}, err
	}
	return defaultDerivedThreadProjection(threadID), nil
}

func listDerivedThreadProjections(ctx context.Context, opts handlerOptions, threadIDs []string) (map[string]primitives.DerivedThreadProjection, error) {
	projections, err := opts.primitiveStore.ListDerivedThreadProjections(ctx, threadIDs)
	if err != nil {
		return nil, err
	}
	for _, threadID := range threadIDs {
		threadID = strings.TrimSpace(threadID)
		if threadID == "" {
			continue
		}
		if _, ok := projections[threadID]; !ok {
			projections[threadID] = defaultDerivedThreadProjection(threadID)
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
		if err := refreshDerivedThreadProjection(ctx, opts, threadID, now, actorID); err != nil {
			return fmt.Errorf("refresh derived projection for thread %s: %w", threadID, err)
		}
	}
	return nil
}

func defaultDerivedThreadProjection(threadID string) primitives.DerivedThreadProjection {
	threadID = strings.TrimSpace(threadID)
	generatedAt := time.Now().UTC().Format(time.RFC3339Nano)
	return primitives.DerivedThreadProjection{
		ThreadID:    threadID,
		GeneratedAt: generatedAt,
		Data: map[string]any{
			"thread_id":                 threadID,
			"stale":                     false,
			"inbox_count":               0,
			"pending_decision_count":    0,
			"recommendation_count":      0,
			"decision_request_count":    0,
			"decision_count":            0,
			"artifact_count":            0,
			"open_commitment_count":     0,
			"document_count":            0,
			"last_activity_at":          "",
			"latest_stale_exception_at": "",
			"generated_at":              generatedAt,
		},
	}
}
func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func derivedThreadProjectionExpired(projection primitives.DerivedThreadProjection, now time.Time) bool {
	generatedAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(projection.GeneratedAt))
	if err != nil {
		return true
	}
	return now.Sub(generatedAt) >= derivedProjectionMaxAge
}

type threadProjectionState struct {
	Projection primitives.DerivedThreadProjection
	Refresh    primitives.ThreadProjectionRefreshStatus
	Freshness  map[string]any
	Status     string
}

func loadThreadProjectionState(ctx context.Context, opts handlerOptions, threadID string) (threadProjectionState, error) {
	states, err := loadThreadProjectionStates(ctx, opts, []string{threadID})
	if err != nil {
		return threadProjectionState{}, err
	}
	return states[strings.TrimSpace(threadID)], nil
}

func loadThreadProjectionStates(ctx context.Context, opts handlerOptions, threadIDs []string) (map[string]threadProjectionState, error) {
	threadIDs = uniqueServerStrings(threadIDs)
	out := make(map[string]threadProjectionState, len(threadIDs))
	if opts.primitiveStore == nil || len(threadIDs) == 0 {
		for _, threadID := range threadIDs {
			out[threadID] = buildThreadProjectionState(threadID, primitives.DerivedThreadProjection{}, false, primitives.ThreadProjectionRefreshStatus{}, false)
		}
		return out, nil
	}

	projections, err := opts.primitiveStore.ListDerivedThreadProjections(ctx, threadIDs)
	if err != nil {
		return nil, err
	}
	refreshStatuses, err := opts.primitiveStore.GetThreadProjectionRefreshStatuses(ctx, threadIDs)
	if err != nil {
		return nil, err
	}

	for _, threadID := range threadIDs {
		projection, hasProjection := projections[threadID]
		refresh, hasRefresh := refreshStatuses[threadID]
		out[threadID] = buildThreadProjectionState(threadID, projection, hasProjection, refresh, hasRefresh)
	}
	return out, nil
}

func buildThreadProjectionState(threadID string, projection primitives.DerivedThreadProjection, hasProjection bool, refresh primitives.ThreadProjectionRefreshStatus, hasRefresh bool) threadProjectionState {
	threadID = strings.TrimSpace(threadID)
	if !hasProjection {
		projection = primitives.DerivedThreadProjection{
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

	return threadProjectionState{
		Projection: projection,
		Refresh:    refresh,
		Freshness:  freshness,
		Status:     status,
	}
}

func aggregateThreadProjectionFreshness(states map[string]threadProjectionState, threadIDs []string) map[string]any {
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

func markThreadProjectionsDirty(ctx context.Context, opts handlerOptions, queuedAt time.Time, threadIDs ...string) error {
	if opts.primitiveStore == nil {
		return nil
	}

	threadIDs = uniqueServerStrings(threadIDs)
	if len(threadIDs) == 0 {
		return nil
	}
	if err := opts.primitiveStore.MarkThreadProjectionsDirty(ctx, threadIDs, queuedAt); err != nil {
		return err
	}
	if opts.projectionMaintainer != nil {
		opts.projectionMaintainer.Notify()
	}
	return nil
}

func enqueueThreadProjectionsBestEffort(ctx context.Context, opts handlerOptions, threadIDs []string, queuedAt time.Time) {
	_ = markThreadProjectionsDirty(ctx, opts, queuedAt, threadIDs...)
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
