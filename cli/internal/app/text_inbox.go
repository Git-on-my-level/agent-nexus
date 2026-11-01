// Default text projections for inbox lists and inbox items.

package app

import (
	"fmt"
	"strings"
)

func formatInboxList(body any) string {
	root := asMap(body)
	lines := make([]string, 0, 16)
	if viewingAs := strings.TrimSpace(formatViewingAsSummary(root["viewing_as"])); viewingAs != "" {
		lines = append(lines, "viewing_as: "+viewingAs)
	}
	if threadID := strings.TrimSpace(anyString(root["thread_id"])); threadID != "" {
		lines = append(lines, "Thread "+threadID)
	}
	threadIDs := stringList(root["thread_ids"])
	if len(threadIDs) > 1 {
		lines = appendStringList(lines, "thread_ids", threadIDs)
	}
	lines = appendScalar(lines, "total_items", root, "total_items")
	lines = appendScalar(lines, "returned_items", root, "returned_items")
	lines = appendStringList(lines, "types", stringList(root["types"]))
	lines = appendInboxListSection(lines, "items", asSlice(root["items"]), asBool(root["full_id"]))
	return strings.Join(lines, "\n")
}

func renderInboxItem(item map[string]any) string {
	return renderInboxItemWithMode(item, false)
}

func renderInboxListRow(item map[string]any, listID string) string {
	return compactSummary(
		listID,
		firstNonEmpty(anyString(item["type"]), anyString(item["category"]), anyString(item["kind"])),
		firstNonEmpty(anyString(item["title"]), anyString(item["summary"]), anyString(item["thread_id"])),
	)
}

func renderInboxItemWithMode(item map[string]any, fullID bool) string {
	identifier := displayPublicIdentity(item, "inbox")
	if fullID {
		if id := strings.TrimSpace(anyString(item["id"])); id != "" {
			identifier = id
		}
	}
	return compactSummary(
		identifier,
		firstNonEmpty(anyString(item["type"]), anyString(item["category"]), anyString(item["kind"])),
		firstNonEmpty(anyString(item["title"]), anyString(item["summary"]), anyString(item["thread_id"])),
	)
}

func appendInboxListSection(lines []string, label string, items []any, fullID bool) []string {
	lines = append(lines, fmt.Sprintf("%s (%d):", label, len(items)))
	for _, raw := range items {
		item := asMap(raw)
		if item == nil {
			continue
		}
		lines = append(lines, "- "+renderInboxItemWithMode(item, fullID))
	}
	return lines
}
