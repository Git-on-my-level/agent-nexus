package router

import (
	"encoding/json"
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
		if text := strings.TrimSpace(anyString(payload["text"])); text != "" {
			return text
		}
	}
	return strings.TrimSpace(anyString(event["summary"]))
}

func stableJSON(value any) ([]byte, error) {
	return json.Marshal(value)
}

func asMap(value any) map[string]any {
	out, _ := value.(map[string]any)
	return out
}

func asSlice(value any) []any {
	out, _ := value.([]any)
	return out
}

func anyString(value any) string {
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
