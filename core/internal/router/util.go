package router

import (
	"fmt"
	"strings"
	"time"
)

func nowUTC() time.Time {
	return time.Now().UTC()
}

func utcNowISO() string {
	return nowUTC().Format(time.RFC3339)
}

func parseUTCISO(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return parsed.UTC(), true
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed.UTC(), true
	}
	return time.Time{}, false
}

func compactText(text string, limit int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if limit <= 0 || len(text) <= limit {
		return text
	}
	if limit == 1 {
		return "…"
	}
	return strings.TrimSpace(text[:limit-1]) + "…"
}

func extractMessageText(event map[string]any) string {
	if payload, ok := event["payload"].(map[string]any); ok {
		for _, key := range []string{"text", "body", "message", "content"} {
			if text := strings.TrimSpace(anyString(payload[key])); text != "" {
				return text
			}
		}
	}
	return strings.TrimSpace(anyString(event["summary"]))
}

func anyString(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
