package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"agent-nexus-core/internal/primitives"
)

type threadContextOptions struct {
	MaxEvents              int
	IncludeArtifactContent bool
}

type threadWorkspaceOptions struct {
	threadContextOptions
	IncludeRelatedEventContent bool
}

func resolveThreadContextOptions(w http.ResponseWriter, r *http.Request) (threadContextOptions, bool) {
	options := threadContextOptions{MaxEvents: defaultThreadContextMaxEvents}

	if rawMaxEvents := strings.TrimSpace(r.URL.Query().Get("max_events")); rawMaxEvents != "" {
		parsed, err := strconv.Atoi(rawMaxEvents)
		if err != nil || parsed < 0 {
			writeError(w, http.StatusBadRequest, "invalid_request", "max_events must be a non-negative integer")
			return threadContextOptions{}, false
		}
		options.MaxEvents = parsed
	}

	if rawInclude := strings.TrimSpace(r.URL.Query().Get("include_artifact_content")); rawInclude != "" {
		parsed, err := strconv.ParseBool(rawInclude)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "include_artifact_content must be true or false")
			return threadContextOptions{}, false
		}
		options.IncludeArtifactContent = parsed
	}

	return options, true
}

func resolveThreadWorkspaceOptions(w http.ResponseWriter, r *http.Request) (threadWorkspaceOptions, bool) {
	contextOptions, ok := resolveThreadContextOptions(w, r)
	if !ok {
		return threadWorkspaceOptions{}, false
	}

	options := threadWorkspaceOptions{threadContextOptions: contextOptions}
	if rawInclude := strings.TrimSpace(r.URL.Query().Get("include_related_event_content")); rawInclude != "" {
		parsed, err := strconv.ParseBool(rawInclude)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "include_related_event_content must be true or false")
			return threadWorkspaceOptions{}, false
		}
		options.IncludeRelatedEventContent = parsed
	}

	return options, true
}

func handleThreadWorkspace(w http.ResponseWriter, r *http.Request, opts handlerOptions, threadID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	options, ok := resolveThreadWorkspaceOptions(w, r)
	if !ok {
		return
	}

	body, err := buildThreadWorkspacePayload(r.Context(), opts, threadID, options)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "thread not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load thread workspace")
		return
	}

	writeJSON(w, http.StatusOK, body)
}

func buildThreadContextPayload(ctx context.Context, opts handlerOptions, threadID string, options threadContextOptions) (map[string]any, error) {
	thread, err := opts.primitiveStore.GetThread(ctx, threadID)
	if err != nil {
		return nil, err
	}

	recentEvents, err := opts.primitiveStore.ListRecentEventsByThread(ctx, threadID, options.MaxEvents)
	if err != nil {
		return nil, err
	}

	keyArtifacts, err := buildThreadContextArtifacts(ctx, opts, thread, options.IncludeArtifactContent)
	if err != nil {
		return nil, err
	}

	documents, err := buildThreadContextDocuments(ctx, opts, threadID)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"thread":        thread,
		"recent_events": recentEvents,
		"key_artifacts": keyArtifacts,
		"open_cards":    []map[string]any{},
		"documents":     documents,
	}, nil
}

func buildThreadWorkspacePayload(ctx context.Context, opts handlerOptions, threadID string, options threadWorkspaceOptions) (map[string]any, error) {
	contextBody, err := buildThreadContextPayload(ctx, opts, threadID, options.threadContextOptions)
	if err != nil {
		return nil, err
	}

	thread, _ := contextBody["thread"].(map[string]any)
	collaboration := buildThreadWorkspaceCollaborationSummary(contextBody)

	now := time.Now().UTC()
	projectionState, err := loadTopicProjectionState(ctx, opts, threadID)
	if err != nil {
		return nil, err
	}
	inboxSection, inboxItems, err := buildThreadWorkspaceInboxSection(ctx, opts, threadID, now, projectionState)
	if err != nil {
		return nil, err
	}
	pendingDecisions := filterThreadWorkspaceInboxItems(inboxItems, []string{"action_needed"})

	relatedThreadReview := buildEmptyRelatedThreadReview()
	if related, err := buildThreadWorkspaceRelatedThreadReview(ctx, opts, threadID, contextBody, options); err != nil {
		return nil, err
	} else {
		relatedThreadReview = related
	}

	recommendations := asWorkspaceEventSlice(collaboration["recommendations"])
	decisionRequests := asWorkspaceEventSlice(collaboration["decision_requests"])
	decisions := asWorkspaceEventSlice(collaboration["decisions"])
	totalReviewItems := len(recommendations) + len(decisionRequests) + len(decisions) + len(pendingDecisions) + workspaceIntValue(relatedThreadReview["total_review_items"])
	boardMemberships, err := opts.primitiveStore.ListBoardMembershipsByThread(ctx, threadID)
	if err != nil {
		return nil, err
	}

	contextSection := cloneWorkspaceMap(contextBody)
	delete(contextSection, "thread")

	workspaceBody := map[string]any{
		"thread_id": firstNonEmptyString(thread["id"], threadID),
		"thread":    thread,
		"context":   contextSection,
		"collaboration": map[string]any{
			"recommendations":        recommendations,
			"decision_requests":      decisionRequests,
			"decisions":              decisions,
			"key_artifacts":          contextBody["key_artifacts"],
			"open_cards":             contextBody["open_cards"],
			"recommendation_count":   len(recommendations),
			"decision_request_count": len(decisionRequests),
			"decision_count":         len(decisions),
			"artifact_count":         workspaceSliceLen(contextBody["key_artifacts"]),
			"open_card_count":        workspaceSliceLen(contextBody["open_cards"]),
		},
		"board_memberships":           boardMembershipSectionResponse(boardMemberships),
		"inbox":                       inboxSection,
		"pending_decisions":           map[string]any{"thread_id": strings.TrimSpace(threadID), "items": pendingDecisions, "count": len(pendingDecisions), "generated_at": nullableStringValue(projectionState.Projection.GeneratedAt), "projection_freshness": cloneWorkspaceMap(projectionState.Freshness)},
		"related_threads":             relatedThreadReview["related_threads"],
		"related_recommendations":     relatedThreadReview["related_recommendations"],
		"related_decision_requests":   relatedThreadReview["related_decision_requests"],
		"related_decisions":           relatedThreadReview["related_decisions"],
		"total_review_items":          totalReviewItems,
		"follow_up":                   buildThreadWorkspaceFollowUpHints(thread, threadID, recommendations, decisionRequests, decisions),
		"workspace_summary":           cloneWorkspaceMap(projectionState.Projection.Data),
		"projection_freshness":        cloneWorkspaceMap(projectionState.Freshness),
		"workspace_summary_freshness": cloneWorkspaceMap(projectionState.Freshness),
		"section_kinds": map[string]any{
			"thread":                    "canonical",
			"context":                   "canonical",
			"collaboration":             "derived",
			"board_memberships":         "canonical",
			"inbox":                     "derived",
			"pending_decisions":         "derived",
			"workspace_summary":         "derived",
			"related_threads":           "derived",
			"related_recommendations":   "derived",
			"related_decision_requests": "derived",
			"related_decisions":         "derived",
			"follow_up":                 "convenience",
		},
		"context_source": "threads.workspace",
		"inbox_source":   "threads.workspace",
		"generated_at":   now.Format(time.RFC3339Nano),
	}

	if options.IncludeRelatedEventContent {
		workspaceBody["related_event_content_enabled"] = true
		workspaceBody["related_event_content_count"] = workspaceIntValue(relatedThreadReview["related_event_content_count"])
	}
	if warningCount := workspaceIntValue(relatedThreadReview["warning_count"]); warningCount > 0 {
		workspaceBody["warnings"] = map[string]any{
			"items": relatedThreadReview["warnings"],
			"count": warningCount,
		}
	}

	return workspaceBody, nil
}

func buildThreadWorkspaceInboxSection(ctx context.Context, opts handlerOptions, threadID string, now time.Time, projectionState topicProjectionState) (map[string]any, []map[string]any, error) {
	items, err := opts.primitiveStore.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{
		ThreadID: threadID,
	})
	if err != nil {
		return nil, nil, err
	}

	filtered := make([]map[string]any, 0, len(items))
	for _, item := range items {
		filtered = append(filtered, payloadFromDerivedInboxItem(item))
	}

	return map[string]any{
		"thread_id":            strings.TrimSpace(threadID),
		"items":                filtered,
		"count":                len(filtered),
		"generated_at":         nullableStringValue(projectionState.Projection.GeneratedAt),
		"projection_freshness": cloneWorkspaceMap(projectionState.Freshness),
	}, filtered, nil
}

func buildThreadWorkspaceCollaborationSummary(contextBody map[string]any) map[string]any {
	recentEvents, _ := contextBody["recent_events"].([]map[string]any)
	normalizedEvents := workspaceNormalizeEvents(recentEvents)
	recommendations := filterWorkspaceEventsByType(normalizedEvents, []string{"actor_statement"})
	decisionRequests := filterWorkspaceEventsByType(normalizedEvents, []string{"decision_needed"})
	decisions := filterWorkspaceEventsByType(normalizedEvents, []string{"decision_made"})

	return map[string]any{
		"recommendations":        recommendations,
		"decision_requests":      decisionRequests,
		"decisions":              decisions,
		"recommendation_count":   len(recommendations),
		"decision_request_count": len(decisionRequests),
		"decision_count":         len(decisions),
		"artifact_count":         workspaceSliceLen(contextBody["key_artifacts"]),
		"open_card_count":        workspaceSliceLen(contextBody["open_cards"]),
	}
}

func buildThreadWorkspaceRelatedThreadReview(ctx context.Context, opts handlerOptions, rootThreadID string, rootContextBody map[string]any, options threadWorkspaceOptions) (map[string]any, error) {
	relatedThreadIDs := relatedThreadRefIDs(rootThreadID, rootContextBody)
	items := make([]map[string]any, 0, len(relatedThreadIDs))
	relatedRecommendations := make([]map[string]any, 0)
	relatedDecisionRequests := make([]map[string]any, 0)
	relatedDecisions := make([]map[string]any, 0)
	warnings := make([]map[string]any, 0)
	totalReviewItems := 0
	relatedEventContentCount := 0

	for _, relatedThreadID := range relatedThreadIDs {
		contextBody, err := buildThreadContextPayload(ctx, opts, relatedThreadID, options.threadContextOptions)
		if err != nil {
			warnings = append(warnings, map[string]any{
				"thread_id": relatedThreadID,
				"message":   "skipped related thread " + relatedThreadID + ": " + err.Error(),
			})
			continue
		}

		thread, _ := contextBody["thread"].(map[string]any)
		collaboration := buildThreadWorkspaceCollaborationSummary(contextBody)
		recommendations := annotateWorkspaceEvents(asWorkspaceEventSlice(collaboration["recommendations"]), thread)
		decisionRequests := annotateWorkspaceEvents(asWorkspaceEventSlice(collaboration["decision_requests"]), thread)
		decisions := annotateWorkspaceEvents(asWorkspaceEventSlice(collaboration["decisions"]), thread)

		if options.IncludeRelatedEventContent {
			var hydrateWarnings []map[string]any
			recommendations, hydrateWarnings = hydrateWorkspaceEvents(ctx, opts, relatedThreadID, recommendations)
			warnings = append(warnings, hydrateWarnings...)
			decisionRequests, hydrateWarnings = hydrateWorkspaceEvents(ctx, opts, relatedThreadID, decisionRequests)
			warnings = append(warnings, hydrateWarnings...)
			decisions, hydrateWarnings = hydrateWorkspaceEvents(ctx, opts, relatedThreadID, decisions)
			warnings = append(warnings, hydrateWarnings...)
			relatedEventContentCount += hydratedWorkspaceEventCount(recommendations)
			relatedEventContentCount += hydratedWorkspaceEventCount(decisionRequests)
			relatedEventContentCount += hydratedWorkspaceEventCount(decisions)
		}

		relatedRecommendations = append(relatedRecommendations, recommendations...)
		relatedDecisionRequests = append(relatedDecisionRequests, decisionRequests...)
		relatedDecisions = append(relatedDecisions, decisions...)

		threadReviewCount := len(recommendations) + len(decisionRequests) + len(decisions)
		totalReviewItems += threadReviewCount
		items = append(items, map[string]any{
			"thread_id": firstNonEmptyString(thread["id"], relatedThreadID),
			"thread":    thread,
			"recommendations": map[string]any{
				"items": recommendations,
				"count": len(recommendations),
			},
			"decision_requests": map[string]any{
				"items": decisionRequests,
				"count": len(decisionRequests),
			},
			"decisions": map[string]any{
				"items": decisions,
				"count": len(decisions),
			},
			"total_review_items": threadReviewCount,
		})
	}

	return map[string]any{
		"related_threads": map[string]any{
			"items": items,
			"count": len(items),
		},
		"related_recommendations": map[string]any{
			"items": relatedRecommendations,
			"count": len(relatedRecommendations),
		},
		"related_decision_requests": map[string]any{
			"items": relatedDecisionRequests,
			"count": len(relatedDecisionRequests),
		},
		"related_decisions": map[string]any{
			"items": relatedDecisions,
			"count": len(relatedDecisions),
		},
		"warnings":                    warnings,
		"warning_count":               len(warnings),
		"related_event_content_count": relatedEventContentCount,
		"total_review_items":          totalReviewItems,
	}, nil
}

func buildEmptyRelatedThreadReview() map[string]any {
	return map[string]any{
		"related_threads":             map[string]any{"items": []map[string]any{}, "count": 0},
		"related_recommendations":     map[string]any{"items": []map[string]any{}, "count": 0},
		"related_decision_requests":   map[string]any{"items": []map[string]any{}, "count": 0},
		"related_decisions":           map[string]any{"items": []map[string]any{}, "count": 0},
		"warnings":                    []map[string]any{},
		"warning_count":               0,
		"related_event_content_count": 0,
		"total_review_items":          0,
	}
}

func topicIDFromThreadWorkspaceRefs(thread map[string]any) string {
	if thread == nil {
		return ""
	}
	ref := strings.TrimSpace(anyString(thread["subject_ref"]))
	if strings.HasPrefix(ref, "topic:") {
		id := strings.TrimSpace(strings.TrimPrefix(ref, "topic:"))
		if id != "" {
			return id
		}
	}
	return ""
}

func buildThreadWorkspaceFollowUpHints(thread map[string]any, threadID string, sections ...[]map[string]any) map[string]any {
	eventIDs := make([]string, 0, 8)
	seen := make(map[string]struct{})

	for _, section := range sections {
		for _, event := range section {
			eventID := strings.TrimSpace(anyString(event["id"]))
			if eventID == "" {
				continue
			}
			if _, ok := seen[eventID]; ok {
				continue
			}
			seen[eventID] = struct{}{}
			eventIDs = append(eventIDs, eventID)
		}
	}

	examples := make([]string, 0, 3)
	for _, eventID := range eventIDs {
		examples = append(examples, "anx events get --event-id "+eventID+" --json")
		if len(examples) >= 3 {
			break
		}
	}

	hints := map[string]any{
		"events_get_template":       "anx events get --event-id <event-id> --json",
		"events_get_examples":       examples,
		"workspace_refresh_command": "",
	}
	tid := strings.TrimSpace(threadID)
	if topicID := topicIDFromThreadWorkspaceRefs(thread); topicID != "" {
		hints["workspace_refresh_command"] = "anx topics workspace --topic-id " + topicID + " --include-artifact-content --full-id --json"
	} else if tid != "" {
		hints["workspace_refresh_command"] = "anx threads workspace --thread-id " + tid + " --include-artifact-content --full-id --json"
	}
	return hints
}

func filterThreadWorkspaceInboxItems(items []map[string]any, types []string) []map[string]any {
	if len(items) == 0 {
		return []map[string]any{}
	}
	allowed := make(map[string]struct{}, len(types))
	for _, itemType := range types {
		itemType = strings.TrimSpace(itemType)
		if itemType == "" {
			continue
		}
		allowed[itemType] = struct{}{}
	}
	if len(allowed) == 0 {
		return cloneWorkspaceMaps(items)
	}

	filtered := make([]map[string]any, 0, len(items))
	for _, item := range items {
		itemType := firstNonEmptyString(item["type"], item["category"], item["kind"])
		if _, ok := allowed[itemType]; !ok {
			continue
		}
		filtered = append(filtered, cloneWorkspaceMap(item))
	}
	return filtered
}

func filterWorkspaceEventsByType(events []map[string]any, types []string) []map[string]any {
	if len(events) == 0 {
		return []map[string]any{}
	}
	allowed := make(map[string]struct{}, len(types))
	for _, eventType := range types {
		eventType = strings.TrimSpace(eventType)
		if eventType == "" {
			continue
		}
		allowed[eventType] = struct{}{}
	}
	if len(allowed) == 0 {
		return cloneWorkspaceMaps(events)
	}

	filtered := make([]map[string]any, 0, len(events))
	for _, event := range events {
		eventType := strings.TrimSpace(anyString(event["type"]))
		if _, ok := allowed[eventType]; !ok {
			continue
		}
		filtered = append(filtered, cloneWorkspaceMap(event))
	}
	return filtered
}

func workspaceNormalizeEvents(events []map[string]any) []map[string]any {
	if len(events) == 0 {
		return []map[string]any{}
	}

	out := make([]map[string]any, 0, len(events))
	for _, event := range events {
		copy := cloneWorkspaceMap(event)
		if copy == nil {
			continue
		}
		if id := strings.TrimSpace(anyString(copy["id"])); id != "" && strings.TrimSpace(anyString(copy["short_id"])) == "" {
			copy["short_id"] = workspaceShortID(id)
		}
		if preview := workspaceEventSummaryPreview(copy); preview != "" && strings.TrimSpace(anyString(copy["summary_preview"])) == "" {
			copy["summary_preview"] = preview
		}
		if _, ok := copy["provenance_sources"]; !ok {
			provenance, _ := copy["provenance"].(map[string]any)
			if len(provenance) > 0 {
				if sources, err := extractStringSlice(provenance["sources"]); err == nil && len(sources) > 0 {
					copy["provenance_sources"] = sources
				}
			}
		}
		out = append(out, copy)
	}

	sort.SliceStable(out, func(i int, j int) bool {
		leftTS, leftOK := workspaceEventCanonicalTimestamp(out[i])
		rightTS, rightOK := workspaceEventCanonicalTimestamp(out[j])
		if leftOK && rightOK {
			if leftTS.Equal(rightTS) {
				return strings.TrimSpace(anyString(out[i]["id"])) < strings.TrimSpace(anyString(out[j]["id"]))
			}
			return leftTS.Before(rightTS)
		}
		if leftOK != rightOK {
			return leftOK
		}
		return false
	})

	return out
}

func relatedThreadRefIDs(rootThreadID string, value any) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	collectThreadRefIDs(value, rootThreadID, seen, &out)
	sort.Strings(out)
	return out
}

func collectThreadRefIDs(value any, rootThreadID string, seen map[string]struct{}, out *[]string) {
	switch typed := value.(type) {
	case []map[string]any:
		for _, item := range typed {
			collectThreadRefIDs(item, rootThreadID, seen, out)
		}
	case []any:
		for _, item := range typed {
			collectThreadRefIDs(item, rootThreadID, seen, out)
		}
	case map[string]any:
		for _, nested := range typed {
			collectThreadRefIDs(nested, rootThreadID, seen, out)
		}
	case string:
		ref := strings.TrimSpace(typed)
		if !strings.HasPrefix(ref, "thread:") {
			return
		}
		threadID := strings.TrimSpace(strings.TrimPrefix(ref, "thread:"))
		if threadID == "" || threadID == strings.TrimSpace(rootThreadID) {
			return
		}
		if _, ok := seen[threadID]; ok {
			return
		}
		seen[threadID] = struct{}{}
		*out = append(*out, threadID)
	}
}

func annotateWorkspaceEvents(events []map[string]any, thread map[string]any) []map[string]any {
	if len(events) == 0 {
		return []map[string]any{}
	}

	threadID := firstNonEmptyString(thread["thread_id"], thread["id"])
	threadTitle := strings.TrimSpace(anyString(thread["title"]))
	out := make([]map[string]any, 0, len(events))
	for _, event := range events {
		copy := cloneWorkspaceMap(event)
		if copy == nil {
			continue
		}
		if threadID != "" {
			copy["source_thread_id"] = threadID
		}
		if threadTitle != "" {
			copy["source_thread_title"] = threadTitle
		}
		out = append(out, copy)
	}
	return out
}

func hydrateWorkspaceEvents(ctx context.Context, opts handlerOptions, threadID string, events []map[string]any) ([]map[string]any, []map[string]any) {
	if len(events) == 0 {
		return []map[string]any{}, nil
	}

	out := make([]map[string]any, 0, len(events))
	warnings := make([]map[string]any, 0)
	for _, event := range events {
		item := cloneWorkspaceMap(event)
		if item == nil {
			continue
		}
		eventID := strings.TrimSpace(anyString(item["id"]))
		if eventID == "" {
			out = append(out, item)
			continue
		}

		fullEvent, err := opts.primitiveStore.GetEvent(ctx, eventID)
		if err != nil {
			warnings = append(warnings, map[string]any{
				"thread_id": threadID,
				"event_id":  eventID,
				"message":   "kept summary-only related event " + eventID + ": " + err.Error(),
			})
			out = append(out, item)
			continue
		}

		item["event"] = fullEvent
		if strings.TrimSpace(anyString(item["summary"])) == "" {
			item["summary"] = anyString(fullEvent["summary"])
		}
		if strings.TrimSpace(anyString(item["summary_preview"])) == "" {
			if preview := workspaceEventSummaryPreview(fullEvent); preview != "" {
				item["summary_preview"] = preview
			}
		}
		out = append(out, item)
	}

	return out, warnings
}

func hydratedWorkspaceEventCount(events []map[string]any) int {
	count := 0
	for _, event := range events {
		if _, ok := event["event"].(map[string]any); ok {
			count++
		}
	}
	return count
}

func asWorkspaceEventSlice(raw any) []map[string]any {
	events, _ := raw.([]map[string]any)
	if len(events) == 0 {
		return []map[string]any{}
	}
	return cloneWorkspaceMaps(events)
}

func cloneWorkspaceMaps(items []map[string]any) []map[string]any {
	if len(items) == 0 {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if copy := cloneWorkspaceMap(item); copy != nil {
			out = append(out, copy)
		}
	}
	return out
}

func cloneWorkspaceMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func workspaceEventCanonicalTimestamp(event map[string]any) (time.Time, bool) {
	for _, field := range []string{"ts", "created_at"} {
		raw := strings.TrimSpace(anyString(event[field]))
		if raw == "" {
			continue
		}
		if ts, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			return ts, true
		}
		if ts, err := time.Parse(time.RFC3339, raw); err == nil {
			return ts, true
		}
	}
	return time.Time{}, false
}

func workspaceEventSummaryPreview(event map[string]any) string {
	preview := strings.TrimSpace(anyString(event["summary"]))
	if preview != "" {
		return workspaceTruncatePreview(preview)
	}

	payload, _ := event["payload"].(map[string]any)
	if payload != nil {
		for _, key := range []string{"recommendation", "decision", "summary", "statement", "message", "title", "content", "text"} {
			if value := workspaceCompactPreviewValue(payload[key]); value != "" {
				return workspaceTruncatePreview(value)
			}
		}
		if encoded, err := json.Marshal(payload); err == nil {
			if value := strings.TrimSpace(string(encoded)); value != "" && value != "{}" {
				return workspaceTruncatePreview(value)
			}
		}
	}

	if refs, err := extractStringSlice(event["refs"]); err == nil && len(refs) > 0 {
		return workspaceTruncatePreview(strings.Join(refs, ", "))
	}

	return workspaceTruncatePreview(firstNonEmptyString(event["ts"], event["created_at"]))
}

func workspaceCompactPreviewValue(raw any) string {
	switch typed := raw.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []string:
		return strings.TrimSpace(strings.Join(typed, "; "))
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			text := strings.TrimSpace(anyString(item))
			if text == "" {
				continue
			}
			parts = append(parts, text)
			if len(parts) >= 3 {
				break
			}
		}
		return strings.TrimSpace(strings.Join(parts, "; "))
	default:
		if raw == nil {
			return ""
		}
		encoded, err := json.Marshal(raw)
		if err != nil {
			return strings.TrimSpace(anyString(raw))
		}
		return strings.TrimSpace(string(encoded))
	}
}

func workspaceTruncatePreview(raw string) string {
	const maxRunes = 120
	normalized := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if normalized == "" {
		return ""
	}
	runes := []rune(normalized)
	if len(runes) <= maxRunes {
		return normalized
	}
	return strings.TrimSpace(string(runes[:maxRunes])) + "..."
}

func workspaceShortID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) <= 10 {
		return id
	}
	return id[:10]
}

func workspaceIntValue(raw any) int {
	switch typed := raw.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func workspaceSliceLen(raw any) int {
	switch typed := raw.(type) {
	case []map[string]any:
		return len(typed)
	case []any:
		return len(typed)
	default:
		return 0
	}
}
