package recorder

import (
	"net/http"
	"strings"
	"testing"
)

func TestHostEnrollmentSecretsRedacted(t *testing.T) {
	headers := RedactHeaders(http.Header{"X-Anx-Enrollment-Token": []string{"poll-secret"}})
	if headers["X-Anx-Enrollment-Token"][0] != "REDACTED" {
		t.Fatal(headers)
	}
	for _, raw := range []string{`{"enrollment_token":"fleet-secret","poll_token":"poll-secret"}`, `{"enrollment_token":"fleet-secret`, `{"poll_token":"poll-secret`} {
		redacted, changed := redactJSONFragment(raw)
		if !changed || strings.Contains(redacted, "fleet-secret") || strings.Contains(redacted, "poll-secret") {
			t.Fatalf("unsafe fragment %s", redacted)
		}
	}
	// The valid JSON path uses the same sensitive-field policy.
	for _, field := range []string{"enrollment_token", "poll_token"} {
		if !isSensitiveJSONKey(field) {
			t.Errorf("unprotected field %s", field)
		}
	}
}
