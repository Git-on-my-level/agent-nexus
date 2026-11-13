package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"agent-nexus-cli/internal/config"
)

const (
	notesJSONLimit = 20
	notesTextLimit = 5
)

func (a *App) attachCardNotes(ctx context.Context, cfg config.Resolved, fallbackID string, body map[string]any) {
	if body == nil {
		return
	}
	cardRef, threadID := notesTargetFromBody(body, fallbackID)
	body["notes"] = a.loadCardNotes(ctx, cfg, cardRef, threadID)
}

func notesTargetFromBody(body map[string]any, fallbackID string) (cardRef, threadID string) {
	work := extractNestedMap(body, "work")
	card := extractNestedMap(body, "card")
	cardRef = firstNonEmpty(
		strings.TrimSpace(anyString(work["ref"])),
		strings.TrimSpace(anyString(work["id"])),
		strings.TrimSpace(anyString(card["ref"])),
		strings.TrimSpace(anyString(card["id"])),
		strings.TrimSpace(fallbackID),
	)
	threadID = firstNonEmpty(
		strings.TrimSpace(anyString(work["thread_id"])),
		strings.TrimSpace(anyString(card["thread_id"])),
		strings.TrimSpace(anyString(body["thread_id"])),
	)
	return cardRef, threadID
}

func (a *App) loadCardNotes(ctx context.Context, cfg config.Resolved, cardRef, threadID string) map[string]any {
	cardRef = strings.TrimSpace(cardRef)
	followUp := notesFollowUp(cardRef)
	threadID = strings.TrimSpace(threadID)
	if threadID == "" && cardRef != "" {
		card, err := a.fetchCardBody(ctx, cfg, cardRef)
		if err != nil {
			return notesErrorResult(err, followUp)
		}
		target, err := cardMessageTarget(card, cardRef)
		if err != nil {
			return notesErrorResult(err, followUp)
		}
		threadID = target.ThreadID
		if cardRef == "" {
			cardRef = target.SubjectID
			followUp = notesFollowUp(cardRef)
		}
	}
	if threadID == "" {
		return notesErrorResult(fmt.Errorf("card has no thread_id for notes"), followUp)
	}
	result, err := a.runEventsListCommand(ctx, []string{
		"--thread-id", threadID,
		"--type", "message_posted",
		"--max-events", strconv.Itoa(notesJSONLimit),
	}, cfg)
	if err != nil {
		return notesErrorResult(err, followUp)
	}
	events := asSlice(commandResultBody(result)["events"])
	items := compactNoteItems(events)
	out := map[string]any{
		"items":     items,
		"count":     len(items),
		"follow_up": followUp,
	}
	if len(items) >= notesJSONLimit {
		out["truncated"] = true
	}
	return out
}

func notesFollowUp(cardRef string) string {
	if strings.TrimSpace(cardRef) == "" {
		cardRef = "<card>"
	}
	return fmt.Sprintf("anx cards messages %s --max-events %d", cardRef, notesJSONLimit)
}

func notesErrorResult(err error, followUp string) map[string]any {
	msg := "notes read failed"
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		msg = err.Error()
	}
	return map[string]any{
		"items":     []any{},
		"count":     0,
		"error":     msg,
		"follow_up": followUp,
	}
}

func compactNoteItems(events []any) []any {
	items := make([]any, 0, len(events))
	for _, raw := range events {
		event := asMap(raw)
		if len(event) == 0 {
			continue
		}
		payload := asMap(event["payload"])
		item := map[string]any{
			"id": firstNonEmpty(anyString(event["id"]), anyString(event["event_id"])),
		}
		if ts := firstNonEmpty(anyString(event["occurred_at"]), anyString(event["created_at"]), anyString(event["ts"])); ts != "" {
			item["ts"] = ts
		}
		if actor := firstNonEmpty(anyString(event["actor_id"]), anyString(payload["actor_id"])); actor != "" {
			item["actor_id"] = actor
		}
		if decided := firstNonEmpty(anyString(event["decided_by"]), anyString(payload["decided_by"])); decided != "" {
			item["decided_by"] = decided
		}
		if text := firstNonEmpty(anyString(payload["text"]), anyString(payload["body"]), anyString(event["text"]), anyString(event["summary"])); text != "" {
			item["text"] = text
		}
		items = append(items, item)
	}
	// events list keeps chronological order and max-events takes the tail;
	// present notes newest-first so the default 5-line preview is the latest.
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	return items
}

func formatNotesSection(notes map[string]any, cardRef string) string {
	if len(notes) == 0 {
		return ""
	}
	followUp := firstNonEmpty(anyString(notes["follow_up"]), notesFollowUp(cardRef))
	if errText := strings.TrimSpace(anyString(notes["error"])); errText != "" {
		return "notes: error: " + errText + "\nRetry with `" + followUp + "`."
	}
	items := asSlice(notes["items"])
	count := len(items)
	if n := intValue(notes["count"]); n > count {
		count = n
	}
	lines := []string{fmt.Sprintf("notes: %d", count)}
	shown := items
	if len(shown) > notesTextLimit {
		shown = shown[:notesTextLimit]
	}
	for _, raw := range shown {
		item := asMap(raw)
		line := firstNonEmpty(anyString(item["ts"]), anyString(item["id"]), "note")
		if actor := firstNonEmpty(anyString(item["actor_id"]), anyString(item["decided_by"])); actor != "" {
			line += "  " + actor
		}
		if text := strings.Join(strings.Fields(anyString(item["text"])), " "); text != "" {
			line += "  " + text
		}
		lines = append(lines, line)
	}
	if len(items) > notesTextLimit || asBool(notes["truncated"]) {
		lines = append(lines, "More notes: `"+followUp+"`")
	}
	return strings.Join(lines, "\n")
}

func appendNotesText(text string, body map[string]any, fallbackID string) string {
	section := formatNotesSection(asMap(body["notes"]), firstNonEmpty(notesTargetFromBody(body, fallbackID)))
	if section == "" {
		return text
	}
	if strings.TrimSpace(text) == "" {
		return section
	}
	return text + "\n" + section
}

func copyMapWithoutNotes(body map[string]any) map[string]any {
	out := make(map[string]any, len(body))
	for key, value := range body {
		if key == "notes" {
			continue
		}
		out[key] = value
	}
	return out
}
