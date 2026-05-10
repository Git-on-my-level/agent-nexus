package redaction

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	RedactedValue = "[REDACTED]"
	RedactedEnv   = "[REDACTED_ENV]"
)

var (
	bearerPattern     = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	privateKeyPattern = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)
	tokenKVPattern    = regexp.MustCompile(`(?i)\b(access_token|refresh_token|invite_token|token|authorization|private_key|secret|secret_value)\b\s*[:=]\s*("[^"]+"|'[^']+'|[^\s,}]+)`)
	envPayloadPattern = regexp.MustCompile(`(?m)^(ANX_[A-Z0-9_]*|[A-Z][A-Z0-9_]{2,})=.+$`)
)

func Value(v any) any {
	return redactValue(v, "")
}

func String(s string) string {
	if strings.TrimSpace(s) == "" {
		return s
	}
	s = privateKeyPattern.ReplaceAllString(s, RedactedValue)
	s = bearerPattern.ReplaceAllString(s, "Bearer "+RedactedValue)
	s = tokenKVPattern.ReplaceAllString(s, `$1=`+RedactedValue)
	if envPayloadPattern.MatchString(s) {
		return RedactedEnv
	}
	return s
}

func redactValue(v any, parentKey string) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for key, value := range x {
			if isEnvironmentKey(key) {
				out[key] = RedactedEnv
				continue
			}
			if isSensitiveKey(key) {
				out[key] = RedactedValue
				continue
			}
			out[key] = redactValue(value, key)
		}
		return out
	case map[string]string:
		out := make(map[string]any, len(x))
		for key, value := range x {
			if isEnvironmentKey(key) {
				out[key] = RedactedEnv
				continue
			}
			if isSensitiveKey(key) {
				out[key] = RedactedValue
				continue
			}
			out[key] = String(value)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, value := range x {
			out[i] = redactValue(value, parentKey)
		}
		return out
	case []map[string]any:
		out := make([]any, len(x))
		for i, value := range x {
			out[i] = redactValue(value, parentKey)
		}
		return out
	case []string:
		out := make([]string, len(x))
		for i, value := range x {
			out[i] = String(value)
		}
		return out
	case string:
		if isEnvironmentKey(parentKey) {
			return RedactedEnv
		}
		return String(x)
	default:
		return v
	}
}

func isSensitiveKey(key string) bool {
	k := normalizeKey(key)
	if k == "" {
		return false
	}
	if strings.Contains(k, "authorization") || strings.Contains(k, "privatekey") || strings.Contains(k, "secret") {
		return true
	}
	if strings.Contains(k, "token") || strings.Contains(k, "apikey") || strings.Contains(k, "password") {
		return true
	}
	return false
}

func isEnvironmentKey(key string) bool {
	k := normalizeKey(key)
	return k == "env" || k == "environment" || k == "environmentvariables" || k == "rawenv" || k == "envpayload"
}

func normalizeKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.NewReplacer("_", "", "-", "", ".", "", " ", "").Replace(key)
	return key
}

func SafeError(err error) string {
	if err == nil {
		return ""
	}
	return String(fmt.Sprint(err))
}
