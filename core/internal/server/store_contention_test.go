package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-nexus-core/internal/primitives"
)

type sqliteContentionFixture int

func (e sqliteContentionFixture) Code() int     { return int(e) }
func (e sqliteContentionFixture) Error() string { return "secret-value" }

func TestStoreContentionClassification(t *testing.T) {
	for _, code := range []int{5, 6, 261, 517, 262} {
		w := httptest.NewRecorder()
		err := fmt.Errorf("secret-value: %w", sqliteContentionFixture(code))
		if !writeStoreContentionError(w, httptest.NewRequest(http.MethodPost, "/cards/secret-value/move", nil), err) {
			t.Fatalf("missed SQLite code %d", code)
		}
		if w.Code != 503 || w.Header().Get("Retry-After") != "1" || !strings.Contains(w.Body.String(), `"recoverable":true`) || !strings.Contains(w.Body.String(), `"code":"busy"`) || strings.Contains(w.Body.String(), "secret-value") || w.Header().Get("X-Request-ID") == "" {
			t.Fatalf("unsafe contention response: %d %s", w.Code, w.Body.String())
		}
	}
	for _, err := range []error{sqliteContentionFixture(19), errors.New("database is locked")} {
		w := httptest.NewRecorder()
		if writeStoreContentionError(w, httptest.NewRequest(http.MethodPost, "/cards/card/move", nil), err) || w.Body.Len() != 0 {
			t.Fatal("classified a non-contention error")
		}
	}
	for _, cause := range []error{sqliteContentionFixture(5), errors.New("secret-value"), primitives.ErrNotFound, primitives.ErrConflict} {
		for _, work := range []bool{false, true} {
			w := httptest.NewRecorder()
			err := fmt.Errorf("wrapped: %w", &primitives.MutationOutcomeUnknown{Cause: cause})
			r := httptest.NewRequest(http.MethodPost, "/cards/card/move", nil)
			if work {
				workStoreError(w, r, err)
			} else if !writeStoreContentionError(w, r, err) {
				t.Fatal("missed uncertain outcome")
			}
			if w.Code != 409 || w.Header().Get("Retry-After") != "" || !strings.Contains(w.Body.String(), `"code":"outcome_unknown"`) || !strings.Contains(w.Body.String(), `"recoverable":false`) || strings.Contains(w.Body.String(), "secret-value") {
				t.Fatalf("uncertain commit invited blind retry: %d %s", w.Code, w.Body.String())
			}
		}
	}
}
