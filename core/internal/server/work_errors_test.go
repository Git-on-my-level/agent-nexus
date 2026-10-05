package server

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-nexus-core/internal/primitives"
	"github.com/google/uuid"
)

type codedWorkError struct{}

func (codedWorkError) Error() string { return "database failure containing secret-value" }
func (codedWorkError) Code() int     { return 5 }

func TestWorkStoreUnmappedErrorsAreLoggedWithoutSecrets(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })
	for _, err := range []error{errors.New("secret-value"), fmt.Errorf("secret-value: %w", codedWorkError{}), fmt.Errorf("secret-value: %w", primitives.ErrHandleAllocation)} {
		output.Reset()
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPatch, "/work/secret-value?token=secret-value", strings.NewReader("secret-value"))
		r.Header.Set("Authorization", "Bearer secret-value")
		r.Header.Set("X-Request-ID", "secret-value")
		workStoreError(w, r, err)
		requestID := w.Header().Get("X-Request-ID")
		if _, err := uuid.Parse(requestID); err != nil {
			t.Fatalf("invalid request ID %q", requestID)
		}
		if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "work operation failed") || !strings.Contains(output.String(), "request_id="+requestID) {
			t.Fatalf("missing correlated error: response=%s log=%s", w.Body.String(), output.String())
		}
		if strings.Contains(output.String()+w.Body.String(), "secret-value") {
			t.Fatal("error log or response leaked request or error content")
		}
		if strings.Contains(output.String(), "codedWorkError") && !strings.Contains(output.String(), "store_code=5") {
			t.Fatal("lost wrapped database error code")
		}
		if errors.Is(err, primitives.ErrHandleAllocation) && !strings.Contains(output.String(), "diagnostic=handle_allocation_exhausted") {
			t.Fatal("lost safe handle allocation diagnostic")
		}
	}
}

func TestWorkStoreMappedErrorsKeepTheirStatus(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{
		{primitives.ErrNotFound, http.StatusNotFound},
		{primitives.ErrConflict, http.StatusConflict},
		{fmt.Errorf("%w: relations ref must resolve", primitives.ErrInvalidWorkRequest), http.StatusBadRequest},
	} {
		w := httptest.NewRecorder()
		workStoreError(w, httptest.NewRequest(http.MethodPatch, "/work/card:fixture", nil), tc.err)
		if w.Code != tc.status || w.Header().Get("X-Request-ID") != "" {
			t.Fatalf("mapped error changed: %d %s", w.Code, w.Body.String())
		}
	}
}
