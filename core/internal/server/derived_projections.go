package server

import (
	"context"
	"errors"
	"fmt"
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

	documents, err := opts.primitiveStore.ListDocuments(ctx, primitives.DocumentListFilter{ThreadID: threadID})
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
		items = append(items, item)
	}

	sortInboxItems(items)
	return items, nil
}

func ensureDerivedThreadProjection(ctx context.Context, opts handlerOptions, threadID string, now time.Time) (primitives.DerivedThreadProjection, error) {
	projection, err := opts.primitiveStore.GetDerivedThreadProjection(ctx, threadID)
	if err == nil {
		if derivedThreadProjectionExpired(projection, now) {
			if err := refreshDerivedThreadProjection(ctx, opts, threadID, now, ""); err != nil {
				return primitives.DerivedThreadProjection{}, err
			}
			return opts.primitiveStore.GetDerivedThreadProjection(ctx, threadID)
		}
		return projection, nil
	}
	if !errors.Is(err, primitives.ErrNotFound) {
		return primitives.DerivedThreadProjection{}, err
	}
	if err := refreshDerivedThreadProjection(ctx, opts, threadID, now, ""); err != nil {
		return primitives.DerivedThreadProjection{}, err
	}
	return opts.primitiveStore.GetDerivedThreadProjection(ctx, threadID)
}

func ensureDerivedThreadProjections(ctx context.Context, opts handlerOptions, threadIDs []string, now time.Time) (map[string]primitives.DerivedThreadProjection, error) {
	projections, err := opts.primitiveStore.ListDerivedThreadProjections(ctx, threadIDs)
	if err != nil {
		return nil, err
	}
	refreshIDs := make([]string, 0)
	for _, threadID := range threadIDs {
		threadID = strings.TrimSpace(threadID)
		if threadID == "" {
			continue
		}
		projection, ok := projections[threadID]
		if !ok || derivedThreadProjectionExpired(projection, now) {
			refreshIDs = append(refreshIDs, threadID)
		}
	}
	for _, threadID := range refreshIDs {
		if err := refreshDerivedThreadProjection(ctx, opts, threadID, now, ""); err != nil {
			return nil, err
		}
	}
	if len(refreshIDs) == 0 {
		return projections, nil
	}
	return opts.primitiveStore.ListDerivedThreadProjections(ctx, threadIDs)
}

func rebuildDerivedProjections(ctx context.Context, opts handlerOptions, now time.Time, actorID string) error {
	if err := emitStaleThreadExceptions(ctx, opts, now, actorID); err != nil {
		return err
	}
	threads, err := opts.primitiveStore.ListThreads(ctx, primitives.ThreadListFilter{})
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
