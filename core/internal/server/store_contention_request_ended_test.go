package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-nexus-core/internal/primitives"
)

// A mutation whose request ended (client gave up or its deadline expired)
// while the store call was queued behind the workspace writer is contention.
// The response must stay retryable and must never leak request contents.
func TestStoreContentionClassifiesRequestEnded(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		w := httptest.NewRecorder()
		err := fmt.Errorf("begin board card move transaction: %w", cause)
		if !writeStoreContentionError(w, httptest.NewRequest(http.MethodPost, "/cards/card/move", nil), err) {
			t.Fatalf("missed %v", cause)
		}
		if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") != "1" || !strings.Contains(w.Body.String(), `"code":"busy"`) || !strings.Contains(w.Body.String(), `"recoverable":true`) {
			t.Fatalf("request-ended mutation invited an opaque failure: %d %s", w.Code, w.Body.String())
		}
	}
	// An uncertain commit stays outcome_unknown even when the request also ended.
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		w := httptest.NewRecorder()
		err := fmt.Errorf("commit: %w", &primitives.MutationOutcomeUnknown{Cause: cause})
		if !writeStoreContentionError(w, httptest.NewRequest(http.MethodPost, "/cards/card/move", nil), err) || w.Code != http.StatusConflict {
			t.Fatalf("uncertain outcome lost for %v: %d %s", cause, w.Code, w.Body.String())
		}
	}
}
