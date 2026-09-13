package observation

import (
	"errors"
	"strings"
	"time"
	"unicode"
)

// PersistableRefreshError maps a collector/read failure into the work
// refresh.last_error / freshness.last_error object. Codes match ErrorKind.
func PersistableRefreshError(err error, source string, nextDue time.Time) map[string]any {
	code, message := HumanRefreshError(err, source, nextDue)
	return map[string]any{"code": code, "message": message}
}

// HumanRefreshError returns a stable code and a sentence suitable for UI/CLI.
func HumanRefreshError(err error, source string, nextDue time.Time) (code, message string) {
	typed := &ReadError{Kind: ErrUnavailable, Message: "Source read or observation persistence failed"}
	var re *ReadError
	if errors.As(err, &re) && re != nil && re.Kind != "" {
		copy := *re
		typed = &copy
	}
	code = string(typed.Kind)
	lower := strings.ToLower(typed.Message)
	switch typed.Kind {
	case ErrPolicy:
		message = sentenceOr(typed.Message, "Refresh was denied by policy")
	case ErrRateLimit:
		label := "Source"
		if source == "github" {
			label = "GitHub"
		}
		message = label + " rate limit reached"
		when := nextDue
		if when.IsZero() && typed.RetryAfter > 0 {
			when = time.Now().UTC().Add(typed.RetryAfter)
		}
		if !when.IsZero() {
			message += "; next attempt at " + when.UTC().Format(time.RFC3339)
		}
	case ErrPermission:
		if strings.Contains(lower, "credential") {
			message = "Source credential unavailable"
		} else {
			message = sentenceOr(typed.Message, "Source authentication failed")
			if strings.Contains(lower, "request failed") {
				message = "Source authentication failed"
			}
		}
	case ErrNotFound:
		message = sentenceOr(typed.Message, "Source item was not found")
	case ErrIsolation:
		message = sentenceOr(typed.Message, "Generated reader isolation is unavailable")
	default:
		if strings.EqualFold(strings.TrimSpace(typed.Message), "source request failed") {
			message = "Source read or observation persistence failed"
		} else {
			message = sentenceOr(typed.Message, "Source read or observation persistence failed")
		}
	}
	return code, message
}

func sentenceOr(msg, fallback string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return fallback
	}
	runes := []rune(msg)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
