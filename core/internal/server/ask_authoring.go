package server

import (
	"strings"
	"unicode/utf8"
)

// validateAskText applies only on new HTTP attention requests, never historical reads.
func validateAskText(payload map[string]any) (string, string) {
	for _, key := range []string{"presentation_revision", "presentation_rewrite"} {
		if _, ok := payload[key]; ok {
			return "invalid_request", key + " is maintained by the ask revision endpoint"
		}
	}

	title, ok := payload["title"].(string)
	if !ok || strings.TrimSpace(title) == "" || strings.ContainsAny(title, "\r\n") {
		return "ask_title_invalid", "event.payload.title must be a non-empty one-line decision; move detail into payload.body"
	}
	if utf8.RuneCountInString(title) > 160 {
		return "ask_title_too_long", "event.payload.title exceeds 160 characters; move detail into payload.body"
	}
	if body, present := payload["body"]; present {
		if _, ok := body.(string); !ok {
			return "ask_body_invalid", "event.payload.body must be a Markdown string"
		}
	}
	return "", ""
}
