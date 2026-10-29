package errnorm

import (
	"strings"
	"testing"
)

func TestHumanRequiredNamesBrowserSession(t *testing.T) {
	for _, body := range []string{
		`{"error":{"code":"human_required","message":"human only"}}`,
		`{"error":{"code":"human_required","message":"human only","hint":"Re-enroll this host","recoverable":true}}`,
	} {
		err := FromHTTPFailure(403, []byte(body))
		if !strings.Contains(err.Hint, "needs a signed-in browser session") || strings.Contains(strings.ToLower(err.Hint), "enroll") {
			t.Fatalf("wrong repair: %q", err.Hint)
		}
		if ExitCode(err) != 5 || err.Recoverable == nil || *err.Recoverable {
			t.Fatalf("wrong error classification: %+v", err)
		}
		if err.Details.(map[string]any)["hint"] != err.Hint {
			t.Fatalf("text/JSON hint mismatch: %+v", err)
		}
	}
}

func TestHostEditMissingSessionDoesNotSuggestEnrollment(t *testing.T) {
	for _, command := range []string{"host.exclude", "host.include"} {
		err := FromHTTPFailure(401, []byte(`{"error":{"code":"auth_required","message":"authorization token is required"}}`))
		EnrichForCommand(err, command)
		if !strings.Contains(err.Hint, "signed-in browser session") || !strings.Contains(err.Hint, "Access > Hosts") || strings.Contains(err.Hint, "auth whoami") {
			t.Fatalf("host edit has wrong repair: %s", err.Hint)
		}
	}
}
