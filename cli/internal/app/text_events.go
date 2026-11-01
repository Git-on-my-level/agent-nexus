// Default text projections for event lists and event records, including the
// reply target hint.

package app

import (
	"fmt"
	"strings"
)

func formatEventsList(body any) string {
	root := asMap(body)
	lines := make([]string, 0, 16)
	if threadID := strings.TrimSpace(anyString(root["thread_id"])); threadID != "" {
		lines = append(lines, "Thread "+threadID)
	}
	threadIDs := stringList(root["thread_ids"])
	if len(threadIDs) > 1 {
		lines = appendStringList(lines, "thread_ids", threadIDs)
	}
	lines = appendScalar(lines, "total_events", root, "total_events")
	lines = appendScalar(lines, "returned_events", root, "returned_events")
	lines = append(lines, fmt.Sprintf("referenced_artifacts: %d", len(asMap(root["artifacts"]))))
	lines = appendStringList(lines, "types", stringList(root["types"]))
	if actorID := strings.TrimSpace(anyString(root["actor_id"])); actorID != "" {
		lines = append(lines, "actor_id: "+actorID)
	}
	lines = appendEventListSection(lines, "events", asSlice(root["events"]), asBool(root["full_id"]))
	return strings.Join(lines, "\n")
}

func formatEventRecord(event map[string]any) string {
	if event == nil {
		return formatPrettyBody(event)
	}
	lines := []string{"Event " + displayPublicIdentity(event, "event")}
	lines = appendScalar(lines, "type", event, "type")
	lines = appendScalar(lines, "thread_id", event, "thread_id")
	lines = appendScalar(lines, "actor_id", event, "actor_id")
	lines = appendScalar(lines, "ts", event, "ts", "created_at")
	lines = appendScalar(lines, "summary", event, "summary")
	lines = appendStringList(lines, "refs", stringList(event["refs"]))
	if payload := asMap(event["payload"]); len(payload) > 0 {
		lines = append(lines, "payload:")
		lines = append(lines, indentBlock(formatPrettyBody(payload))...)
	}
	if trashedAt := anyString(event["trashed_at"]); trashedAt != "" {
		lines = append(lines, "⚠ TRASHED")
		lines = appendScalar(lines, "trashed_at", event, "trashed_at")
		lines = appendScalar(lines, "trashed_by", event, "trashed_by")
		lines = appendScalar(lines, "trash_reason", event, "trash_reason")
	} else if archivedAt := anyString(event["archived_at"]); archivedAt != "" {
		lines = append(lines, "⚠ ARCHIVED")
		lines = appendScalar(lines, "archived_at", event, "archived_at")
		lines = appendScalar(lines, "archived_by", event, "archived_by")
	}
	return strings.Join(lines, "\n")
}

func renderEventListItem(item map[string]any) string {
	return renderEventListItemWithMode(item, false)
}

func appendEventListSection(lines []string, label string, items []any, fullID bool) []string {
	lines = append(lines, fmt.Sprintf("%s (%d):", label, len(items)))
	for _, raw := range items {
		item := asMap(raw)
		if item == nil {
			continue
		}
		lines = append(lines, "- "+renderEventListItemWithMode(item, fullID))
	}
	return lines
}

func renderEventListItemWithMode(item map[string]any, fullID bool) string {
	summary := firstNonEmpty(anyString(item["summary_preview"]), anyString(item["summary"]), anyString(item["created_at"]))
	if replyTo := eventReplyTarget(item); replyTo != "" {
		summary = strings.TrimSpace(summary + " (reply_to: " + replyTo + ")")
	}
	prefix := ""
	if anyString(item["trashed_at"]) != "" {
		prefix = "[TRASHED] "
	} else if anyString(item["archived_at"]) != "" {
		prefix = "[ARCHIVED] "
	}
	return compactSummary(displayEventID(item, fullID), prefix+anyString(item["type"]), summary)
}

func eventReplyTarget(item map[string]any) string {
	payload := asMap(item["payload"])
	return strings.TrimSpace(anyString(payload["reply_to_event_id"]))
}

func displayEventID(item map[string]any, fullID bool) string {
	if item == nil {
		return ""
	}
	if !fullID {
		if identity := displayPublicIdentity(item, "event"); identity != "" {
			return identity
		}
	}
	id := strings.TrimSpace(anyString(item["id"]))
	if fullID && id != "" {
		return id
	}
	return displayID(item)
}
