// Client-side enrichment of event list results: preview compaction, thread
// context summaries, and expanded timeline object merging.

package app

import (
	"encoding/json"
	"fmt"
	"strings"
)

func enrichEventsForList(events []any) []any {
	if len(events) == 0 {
		return []any{}
	}
	out := make([]any, 0, len(events))
	for _, raw := range events {
		event := asMap(raw)
		if event == nil {
			continue
		}
		copy := cloneMap(event)
		addPublicIdentityToObjectIfPresent(copy, "event")
		if preview := eventSummaryPreview(copy); preview != "" {
			copy["summary_preview"] = preview
		}
		out = append(out, copy)
	}
	return out
}

func eventSummaryPreview(event map[string]any) string {
	preview := strings.TrimSpace(anyString(event["summary"]))
	if preview != "" {
		return truncatePreview(preview)
	}
	payload := asMap(event["payload"])
	if payload != nil {
		for _, key := range []string{"recommendation", "decision", "summary", "statement", "message", "title", "content", "text"} {
			if value := compactPreviewValue(payload[key]); value != "" {
				return truncatePreview(value)
			}
		}
		if encoded, err := json.Marshal(payload); err == nil {
			if value := strings.TrimSpace(string(encoded)); value != "" && value != "{}" {
				return truncatePreview(value)
			}
		}
	}
	refs := stringList(event["refs"])
	if len(refs) > 0 {
		return truncatePreview(strings.Join(refs, ", "))
	}
	return truncatePreview(firstNonEmpty(
		strings.TrimSpace(anyString(event["ts"])),
		strings.TrimSpace(anyString(event["created_at"])),
	))
}

func compactPreviewValue(raw any) string {
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
			return strings.TrimSpace(fmt.Sprintf("%v", raw))
		}
		return strings.TrimSpace(string(encoded))
	}
}

func truncatePreview(raw string) string {
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

func addThreadContextCollaborationSummary(body map[string]any) bool {
	if body == nil {
		return false
	}
	recentEvents := enrichEventsForList(asSlice(body["recent_events"]))
	body["recent_events"] = recentEvents

	collaboration := map[string]any{
		"key_artifacts": asSlice(body["key_artifacts"]),
		"open_cards":    asSlice(body["open_cards"]),
	}
	collaboration["artifact_count"] = len(asSlice(collaboration["key_artifacts"]))
	collaboration["open_card_count"] = len(asSlice(collaboration["open_cards"]))

	body["collaboration_summary"] = collaboration
	return true
}

func threadIDsFromThreadsList(result *commandResult) []string {
	if result == nil {
		return nil
	}
	data := asMap(result.Data)
	body := asMap(data["body"])
	if body == nil {
		return nil
	}
	items := asSlice(body["threads"])
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, raw := range items {
		thread := asMap(raw)
		if thread == nil {
			continue
		}
		threadID := strings.TrimSpace(anyString(thread["id"]))
		if threadID == "" {
			continue
		}
		out = append(out, threadID)
	}
	return normalizeIDFilters(out)
}

func uniqueMapsByID(items []any) []any {
	if len(items) == 0 {
		return []any{}
	}
	out := make([]any, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, raw := range items {
		item := asMap(raw)
		if item == nil {
			continue
		}
		id := strings.TrimSpace(anyString(item["id"]))
		if id == "" {
			out = append(out, item)
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, item)
	}
	return out
}

func mergeExpandedTimelineObjects(dst map[string]any, src map[string]any) {
	if len(dst) == 0 && len(src) == 0 {
		return
	}
	for id, raw := range src {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, exists := dst[id]; exists {
			continue
		}
		dst[id] = raw
	}
}

func uniqueContextArtifactItems(items []any) []any {
	if len(items) == 0 {
		return []any{}
	}
	out := make([]any, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, raw := range items {
		item := asMap(raw)
		if item == nil {
			continue
		}
		artifact := asMap(item["artifact"])
		key := firstNonEmpty(
			strings.TrimSpace(anyString(item["id"])),
			strings.TrimSpace(anyString(item["ref"])),
			strings.TrimSpace(anyString(artifact["id"])),
		)
		if key == "" {
			out = append(out, item)
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}
