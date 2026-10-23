package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-nexus-core/internal/primitives"
)

func TestEventAppendContentionClassification(t *testing.T) {
	for _, code := range []int{5, 6, 261, 517, 262} {
		out := httptest.NewRecorder()
		err := fmt.Errorf("secret-value: %w", sqliteContentionFixture(code))
		if !writeEventAppendContentionError(out, httptest.NewRequest(http.MethodPost, "/events", nil), err) || out.Code != 503 || out.Header().Get("Retry-After") != "1" || !strings.Contains(out.Body.String(), `"recoverable":true`) || !strings.Contains(out.Body.String(), "same request_key") || strings.Contains(out.Body.String(), "secret-value") {
			t.Fatalf("unsafe contention response for %d: %d %s", code, out.Code, out.Body.String())
		}
	}
	for _, err := range []error{sqliteContentionFixture(19), errors.New("database is locked"), context.Canceled, context.DeadlineExceeded} {
		out := httptest.NewRecorder()
		if writeEventAppendContentionError(out, httptest.NewRequest(http.MethodPost, "/events", nil), err) || out.Body.Len() != 0 {
			t.Fatalf("unproven contention invited a retry: %v", err)
		}
	}
	out := httptest.NewRecorder()
	err := &primitives.MutationOutcomeUnknown{Cause: sqliteContentionFixture(5)}
	if !writeEventAppendContentionError(out, httptest.NewRequest(http.MethodPost, "/events", nil), err) || out.Code != 409 || out.Header().Get("Retry-After") != "" || !strings.Contains(out.Body.String(), `"recoverable":false`) {
		t.Fatalf("uncertain outcome invited a retry: %d %s", out.Code, out.Body.String())
	}
}
