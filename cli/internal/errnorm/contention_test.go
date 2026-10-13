package errnorm

import (
	"net/http"
	"strings"
	"testing"
)

func TestSQLiteWriterBusyIsRetryableUnavailable(t *testing.T) {
	err := FromHTTPFailure(503, []byte(`{"error":{"code":"busy","message":"workspace writer is busy","recoverable":true,"hint":"Wait, refresh the board token and retry."}}`))
	if ExitCode(err) != 6 || err.Recoverable == nil || !*err.Recoverable {
		t.Fatalf("contention should be retryable unavailable: exit=%d error=%+v", ExitCode(err), err)
	}
	if ExitCode(FromHTTPFailure(429, []byte(`{"error":{"code":"busy","message":"PM queue is full"}}`))) != 1 {
		t.Fatal("changed existing PM admission exit code")
	}
}

func TestUnknownOutcomeDoesNotBecomeFreshTokenRetry(t *testing.T) {
	err := FromHTTPFailure(409, []byte(`{"error":{"code":"outcome_unknown","recoverable":false,"hint":"Read back; do not retry if the card is already in the requested column."}}`))
	for _, command := range []string{"cards.move", "work.patch"} {
		EnrichForCommand(err, command)
		if err.Recoverable == nil || *err.Recoverable || strings.Contains(err.Hint, "fresh") || !strings.Contains(strings.ToLower(err.Hint), "do not retry") {
			t.Fatalf("unsafe unknown-outcome advice for %s: %+v", command, err)
		}
	}
	withoutMetadata := FromHTTPFailure(409, []byte(`{"error":{"code":"outcome_unknown"}}`))
	if withoutMetadata.Recoverable == nil || *withoutMetadata.Recoverable || !strings.Contains(strings.ToLower(withoutMetadata.Hint), "do not retry") {
		t.Fatalf("unsafe default uncertainty metadata: %+v", withoutMetadata)
	}
}

func TestRemoteErrorPreservesRetryAfter(t *testing.T) {
	for _, value := range []string{"1", "Wed, 14 Oct 2026 12:00:00 GMT"} {
		err := FromHTTPFailureWithHeaders(503, http.Header{"Retry-After": []string{value}}, []byte(`{"error":{"code":"busy","recoverable":true}}`))
		if err.Details.(map[string]any)["retry_after"] != value {
			t.Fatalf("lost Retry-After %q: %+v", value, err)
		}
	}
}
