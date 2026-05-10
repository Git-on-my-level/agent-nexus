package redaction

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValueRedactsSensitivePayloads(t *testing.T) {
	input := map[string]any{
		"access_token":  "access-123",
		"refreshToken":  "refresh-123",
		"invite_token":  "invite-123",
		"private_key":   "-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----",
		"Authorization": "Bearer raw-token",
		"nested": map[string]any{
			"secret_value": "secret-123",
			"environment":  map[string]any{"ANX_SECRET": "value"},
		},
	}

	redacted := Value(input)
	encoded, err := json.Marshal(redacted)
	if err != nil {
		t.Fatalf("marshal redacted: %v", err)
	}
	text := string(encoded)
	for _, leaked := range []string{"access-123", "refresh-123", "invite-123", "raw-token", "secret-123", "ANX_SECRET"} {
		if strings.Contains(text, leaked) {
			t.Fatalf("redacted payload leaked %q: %s", leaked, text)
		}
	}
	if !strings.Contains(text, RedactedValue) || !strings.Contains(text, RedactedEnv) {
		t.Fatalf("missing redaction markers: %s", text)
	}
}

func TestStringRedactsHeadersTokensPrivateKeysAndEnv(t *testing.T) {
	raw := "Authorization: Bearer abc.def\ntoken=plain\n-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----"
	got := String(raw)
	for _, leaked := range []string{"abc.def", "token=plain", "BEGIN PRIVATE KEY"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("String() leaked %q: %s", leaked, got)
		}
	}

	if got := String("ANX_SECRET=value\nOTHER=value"); got != RedactedEnv {
		t.Fatalf("env payload redaction = %q, want %q", got, RedactedEnv)
	}
}
