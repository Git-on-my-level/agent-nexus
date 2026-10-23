package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-nexus-core/internal/auth"
)

func TestAuthContentionClassification(t *testing.T) {
	for _, code := range []int{5, 6, 261, 517, 262} {
		w := httptest.NewRecorder()
		err := fmt.Errorf("wrapped: %w", sqliteContentionFixture(code))
		if !writeAuthContentionError(w, httptest.NewRequest(http.MethodPost, "/auth/token", nil), err) {
			t.Fatalf("missed SQLite result code %d", code)
		}
		if w.Code != 503 || w.Header().Get("Retry-After") != "1" || !strings.Contains(w.Body.String(), `"recoverable":true`) || !strings.Contains(w.Body.String(), `"code":"busy"`) || !strings.Contains(w.Body.String(), "fresh proof") || strings.Contains(w.Body.String(), "secret-value") {
			t.Fatalf("unsafe contention response: %d %s", w.Code, w.Body.String())
		}
	}
	for _, err := range []error{sqliteContentionFixture(13), sqliteContentionFixture(19), errors.New("database is locked"), context.Canceled, context.DeadlineExceeded} {
		w := httptest.NewRecorder()
		if writeAuthContentionError(w, httptest.NewRequest(http.MethodPost, "/auth/token", nil), err) || w.Body.Len() != 0 {
			t.Fatalf("classified non-contention error: %v", err)
		}
	}
}

func TestHostAuthMappedErrorsKeepTheirStatus(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{
		{auth.ErrInvalidRequest, 400},
		{auth.ErrKeyMismatch, 401},
		{auth.ErrInvalidToken, 401},
		{auth.ErrHostRevoked, 403},
		{auth.ErrAgentExcluded, 403},
		{auth.ErrAuthAdminRequired, 403},
		{auth.ErrHostSlugTaken, 409},
		{auth.ErrEnrollmentCapacity, 429},
	} {
		w := httptest.NewRecorder()
		hostCeremonyError(w, httptest.NewRequest(http.MethodPost, "/auth/token", nil), tc.err)
		if w.Code != tc.status || w.Header().Get("Retry-After") != "" {
			t.Fatalf("mapped status changed: %v: %d %s", tc.err, w.Code, w.Body.String())
		}
	}
}
