// Default text projections for topics and cards: list items, records,
// timelines and workspace views.

package app

import (
	"fmt"
	"strings"
)

func renderTopicListItem(item map[string]any, listID string) string {
	return compactSummary(
		listID,
		firstNonEmpty(anyString(item["state"]), anyString(item["type"])),
		firstNonEmpty(anyString(item["title"]), anyString(item["summary"])),
	)
}

func renderCardListItem(item map[string]any, listID string) string {
	subject := firstNonEmpty(anyString(item["title"]), anyString(item["summary"]))
	refs := make([]string, 0, 4)
	if ref := strings.TrimSpace(anyString(item["board_ref"])); ref != "" {
		refs = append(refs, "board="+ref)
	}
	if ref := strings.TrimSpace(anyString(item["topic_ref"])); ref != "" {
		refs = append(refs, "topic="+ref)
	}
	if threadID := strings.TrimSpace(anyString(item["thread_id"])); threadID != "" {
		refs = append(refs, "thread=thread:"+threadID)
	}
	if ref := strings.TrimSpace(anyString(item["column_key"])); ref != "" {
		refs = append(refs, "column="+ref)
	}
	if rank := strings.TrimSpace(anyString(item["rank"])); rank != "" {
		refs = append(refs, "rank="+rank)
	}
	return compactSummary(listID, subject, strings.Join(refs, " :: "))
}

func formatTopicRecord(topic map[string]any) string {
	if topic == nil {
		return formatPrettyBody(topic)
	}
	lines := []string{"Topic " + displayPublicIdentity(topic, "topic")}
	lines = appendScalar(lines, "title", topic, "title")
	lines = appendScalar(lines, "state", topic, "state")
	lines = appendScalar(lines, "summary", topic, "summary")
	lines = appendStringList(lines, "owner_refs", stringList(topic["owner_refs"]))
	lines = appendScalar(lines, "thread_id", topic, "thread_id")
	lines = appendStringList(lines, "document_refs", stringList(topic["document_refs"]))
	lines = appendStringList(lines, "board_refs", stringList(topic["board_refs"]))
	lines = appendStringList(lines, "related_refs", stringList(topic["related_refs"]))
	lines = appendScalar(lines, "updated_at", topic, "updated_at")
	return strings.Join(lines, "\n")
}

func formatCardRecord(card map[string]any) string {
	if card == nil {
		return formatPrettyBody(card)
	}
	lines := []string{"Card " + displayPublicIdentity(card, "card")}
	lines = appendScalar(lines, "title", card, "title")
	lines = appendScalar(lines, "summary", card, "summary")
	lines = appendScalar(lines, "board_ref", card, "board_ref")
	lines = appendScalar(lines, "topic_ref", card, "topic_ref")
	lines = appendScalar(lines, "thread_id", card, "thread_id")
	lines = appendScalar(lines, "document_ref", card, "document_ref")
	lines = appendScalar(lines, "column_key", card, "column_key")
	lines = appendScalar(lines, "status", card, "status")
	lines = appendScalar(lines, "rank", card, "rank")
	lines = appendScalar(lines, "risk", card, "risk")
	lines = appendScalar(lines, "resolution", card, "resolution")
	lines = appendStringList(lines, "assignee_refs", stringList(card["assignee_refs"]))
	lines = appendStringList(lines, "resolution_refs", stringList(card["resolution_refs"]))
	lines = appendStringList(lines, "related_refs", stringList(card["related_refs"]))
	if trashedAt := anyString(card["trashed_at"]); trashedAt != "" {
		lines = append(lines, "⚠ TRASHED")
		lines = appendScalar(lines, "trashed_at", card, "trashed_at")
		lines = appendScalar(lines, "trashed_by", card, "trashed_by")
		lines = appendScalar(lines, "trash_reason", card, "trash_reason")
	}
	lines = appendScalar(lines, "created_at", card, "created_at")
	lines = appendScalar(lines, "updated_at", card, "updated_at")
	return strings.Join(lines, "\n")
}

func formatTopicTimeline(body any) string {
	root := asMap(body)
	lines := []string{formatTopicRecord(extractNestedMap(root, "topic"))}
	lines = append(lines, fmt.Sprintf("events: %d", len(asSlice(root["events"]))))
	lines = append(lines, fmt.Sprintf("artifacts: %d", len(asSlice(root["artifacts"]))))
	lines = append(lines, fmt.Sprintf("cards: %d", len(asSlice(root["cards"]))))
	lines = append(lines, fmt.Sprintf("documents: %d", len(asSlice(root["documents"]))))
	lines = append(lines, fmt.Sprintf("threads: %d", len(asSlice(root["threads"]))))
	return strings.Join(lines, "\n")
}

func formatCardTimeline(body any) string {
	root := asMap(body)
	lines := []string{formatCardRecord(extractNestedMap(root, "card"))}
	lines = append(lines, fmt.Sprintf("events: %d", len(asSlice(root["events"]))))
	lines = append(lines, fmt.Sprintf("artifacts: %d", len(asSlice(root["artifacts"]))))
	lines = append(lines, fmt.Sprintf("cards: %d", len(asSlice(root["cards"]))))
	lines = append(lines, fmt.Sprintf("documents: %d", len(asSlice(root["documents"]))))
	lines = append(lines, fmt.Sprintf("threads: %d", len(asSlice(root["threads"]))))
	return strings.Join(lines, "\n")
}

func formatTopicWorkspace(body any) string {
	root := asMap(body)
	fullID := asBool(root["full_id"])
	lines := []string{formatTopicRecord(extractNestedMap(root, "topic"))}
	for _, field := range []string{"cards_truncated", "resources_truncated"} {
		if value, present := root[field]; present {
			lines = append(lines, fmt.Sprintf("%s: %t", field, asBool(value)))
		}
	}
	if asBool(root["cards_truncated"]) || asBool(root["resources_truncated"]) {
		lines = append(lines, "Bounded workspace preview. Read more via GET /cards, /boards, /documents, /threads, /inbox and their detail routes.")
	}
	lines = appendGroupedScanList(lines, "Cards", asSlice(root["cards"]), "card", fullID, renderCardListItem)
	lines = appendGroupedScanList(lines, "Boards", asSlice(root["boards"]), "board", fullID, renderBoardRowFlat)
	lines = appendGroupedScanList(lines, "Documents", asSlice(root["documents"]), "document", fullID, renderDocumentListItem)
	lines = appendGroupedScanList(lines, "Threads", asSlice(root["threads"]), "thread", fullID, renderThreadListItem)
	lines = appendGroupedScanList(lines, "Inbox", asSlice(root["inbox"]), "inbox", fullID, renderInboxListRow)
	if generatedAt := strings.TrimSpace(anyString(root["generated_at"])); generatedAt != "" {
		lines = append(lines, "")
		lines = append(lines, "generated_at: "+generatedAt)
	}
	return strings.Join(lines, "\n")
}

func renderOpenCardListItem(item map[string]any) string {
	return renderOpenCardListItemWithMode(item, false)
}

func renderOpenCardListItemWithMode(item map[string]any, fullID bool) string {
	return compactSummary(displayCompactIDWithMode(item, fullID), firstNonEmpty(anyString(item["column_key"]), anyString(item["resolution"])), firstNonEmpty(anyString(item["title"]), anyString(item["summary"])))
}

func appendOpenCardListSection(lines []string, label string, items []any, fullID bool) []string {
	lines = append(lines, fmt.Sprintf("%s (%d):", label, len(items)))
	for _, raw := range items {
		item := asMap(raw)
		if item == nil {
			continue
		}
		lines = append(lines, "- "+renderOpenCardListItemWithMode(item, fullID))
	}
	return lines
}
