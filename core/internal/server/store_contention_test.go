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
	w := httptest.NewRecorder()
	writeStoreContentionError(w, httptest.NewRequest(http.MethodPost, "/cards/card/move", nil), &primitives.MutationOutcomeUnknown{Cause: sqliteContentionFixture(5)})
	if w.Code != 409 || w.Header().Get("Retry-After") != "" || !strings.Contains(w.Body.String(), "read the card and board") {
		t.Fatalf("uncertain commit invited blind retry: %d %s", w.Code, w.Body.String())
	}
}
