// Default text projections for threads: context, timeline, inspect, workspace
// and record views.

package app

import (
	"fmt"
	"strings"
)

func formatThreadContext(body any) string {
	root := asMap(body)
	fullID := asBool(root["full_id"])
	if len(asSlice(root["contexts"])) > 1 {
		return formatThreadContextAggregate(root, fullID)
	}
	thread := extractNestedMap(root, "thread")
	lines := make([]string, 0, 24)
	lines = append(lines, formatThreadRecord(thread))
	collaboration := extractNestedMap(root, "collaboration_summary")
	if collaboration != nil {
		lines = appendArtifactListSection(lines, "key_artifacts", asSlice(collaboration["key_artifacts"]), fullID)
		lines = appendOpenCardListSection(lines, "open_cards", asSlice(collaboration["open_cards"]), fullID)
	}
	lines = appendEventListSection(lines, "recent_events", asSlice(root["recent_events"]), fullID)
	lines = appendArtifactListSection(lines, "key_artifacts", asSlice(root["key_artifacts"]), fullID)
	lines = appendOpenCardListSection(lines, "open_cards", asSlice(root["open_cards"]), fullID)
	lines = appendDocumentListSection(lines, "documents", asSlice(root["documents"]), fullID)
	return strings.Join(lines, "\n")
}

func formatThreadContextAggregate(root map[string]any, fullID bool) string {
	contexts := asSlice(root["contexts"])
	lines := []string{
		fmt.Sprintf("Thread contexts (%d):", len(contexts)),
	}
	for _, raw := range contexts {
		context := asMap(raw)
		if context == nil {
			continue
		}
		thread := asMap(context["thread"])
		openCards := len(asSlice(context["open_cards"]))
		documents := len(asSlice(context["documents"]))
		lines = append(lines, fmt.Sprintf(
			"- %s :: open_cards=%d :: documents=%d",
			displayListScanID(thread, "thread", false),
			openCards,
			documents,
		))
	}

	if collaboration := extractNestedMap(root, "collaboration_summary"); collaboration != nil {
		lines = appendArtifactListSection(lines, "key_artifacts", asSlice(collaboration["key_artifacts"]), fullID)
		lines = appendOpenCardListSection(lines, "open_cards", asSlice(collaboration["open_cards"]), fullID)
	}
	lines = appendEventListSection(lines, "recent_events", asSlice(root["recent_events"]), fullID)
	lines = appendArtifactListSection(lines, "key_artifacts", asSlice(root["key_artifacts"]), fullID)
	lines = appendOpenCardListSection(lines, "open_cards", asSlice(root["open_cards"]), fullID)
	lines = appendDocumentListSection(lines, "documents", asSlice(root["documents"]), fullID)
	return strings.Join(lines, "\n")
}

func formatThreadTimeline(body any) string {
	root := asMap(body)
	lines := []string{
		fmt.Sprintf("Timeline events: %d", len(asSlice(root["events"]))),
		fmt.Sprintf("Referenced artifacts: %d", len(asMap(root["artifacts"]))),
	}
	lines = appendListSection(lines, "events", asSlice(root["events"]), renderEventListItem)
	return strings.Join(lines, "\n")
}

func formatThreadInspect(body any) string {
	root := asMap(body)
	lines := make([]string, 0, 28)
	lines = append(lines, formatThreadRecord(extractNestedMap(root, "thread")))
	collaboration := extractNestedMap(root, "collaboration")
	context := extractNestedMap(root, "context")
	fullID := asBool(root["full_id"])
	if collaboration != nil {
		lines = appendArtifactListSection(lines, "key_artifacts", asSlice(collaboration["key_artifacts"]), fullID)
		lines = appendOpenCardListSection(lines, "open_cards", asSlice(collaboration["open_cards"]), fullID)
	}
	if context != nil {
		lines = appendEventListSection(lines, "recent_events", asSlice(context["recent_events"]), fullID)
		lines = appendArtifactListSection(lines, "key_artifacts", asSlice(context["key_artifacts"]), fullID)
		lines = appendOpenCardListSection(lines, "open_cards", asSlice(context["open_cards"]), fullID)
		lines = appendDocumentListSection(lines, "documents", asSlice(context["documents"]), fullID)
	}
	inbox := extractNestedMap(root, "inbox")
	lines = appendInboxListSection(lines, "inbox_items", asSlice(inbox["items"]), fullID)
	return strings.Join(lines, "\n")
}

func formatThreadWorkspace(body any) string {
	root := asMap(body)
	lines := make([]string, 0, 64)
	lines = append(lines, formatThreadRecord(extractNestedMap(root, "thread")))
	fullID := asBool(root["full_id"])

	collaboration := extractNestedMap(root, "collaboration")
	context := extractNestedMap(root, "context")
	if collaboration != nil {
		lines = appendArtifactListSection(lines, "key_artifacts", asSlice(collaboration["key_artifacts"]), fullID)
		lines = appendOpenCardListSection(lines, "open_cards", asSlice(collaboration["open_cards"]), fullID)
	}
	if context != nil {
		lines = appendEventListSection(lines, "recent_events", asSlice(context["recent_events"]), fullID)
		lines = appendArtifactListSection(lines, "key_artifacts", asSlice(context["key_artifacts"]), fullID)
		lines = appendOpenCardListSection(lines, "open_cards", asSlice(context["open_cards"]), fullID)
		lines = appendDocumentListSection(lines, "documents", asSlice(context["documents"]), fullID)
	}
	inbox := extractNestedMap(root, "inbox")
	lines = appendInboxListSection(lines, "inbox_items", extractNestedSlice(inbox, "items"), fullID)
	lines = appendInboxListSection(lines, "pending_attention", extractNestedSlice(extractNestedMap(root, "pending_attention"), "items"), fullID)
	lines = appendWarningListSection(lines, "warnings", extractNestedSlice(extractNestedMap(root, "warnings"), "items"))
	lines = appendScalar(lines, "total_review_items", root, "total_review_items")
	lines = appendFollowUpSection(lines, extractNestedMap(root, "follow_up"))
	return strings.Join(lines, "\n")
}

func formatThreadRecord(thread map[string]any) string {
	if thread == nil {
		return formatPrettyBody(thread)
	}
	lines := []string{"Thread " + displayPublicIdentity(thread, "thread")}
	lines = appendScalar(lines, "title", thread, "title")
	lines = appendScalar(lines, "state", thread, "state")
	lines = appendScalar(lines, "topic_ref", thread, "topic_ref")
	lines = appendScalar(lines, "updated_at", thread, "updated_at")
	lines = appendStringList(lines, "key_artifacts", stringList(thread["key_artifacts"]))
	lines = appendStringList(lines, "open_cards", stringList(thread["open_cards"]))
	if trashedAt := anyString(thread["trashed_at"]); trashedAt != "" {
		lines = append(lines, "⚠ TRASHED")
		lines = appendScalar(lines, "trashed_at", thread, "trashed_at")
		lines = appendScalar(lines, "trashed_by", thread, "trashed_by")
		lines = appendScalar(lines, "trash_reason", thread, "trash_reason")
	} else if archivedAt := anyString(thread["archived_at"]); archivedAt != "" {
		lines = append(lines, "⚠ ARCHIVED")
		lines = appendScalar(lines, "archived_at", thread, "archived_at")
		lines = appendScalar(lines, "archived_by", thread, "archived_by")
	}
	return strings.Join(lines, "\n")
}

func renderThreadListItem(item map[string]any, listID string) string {
	return compactSummary(listID, anyString(item["state"]), anyString(item["title"]))
}
