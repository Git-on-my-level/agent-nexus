package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"agent-nexus-core/internal/primitives"
)

func TestWriteInvalidDocumentRequestIncludesValidatorErrors(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	writeInvalidDocumentRequest(rec, &primitives.VisualReportValidationError{Errors: []string{"title: required"}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	errObj, _ := payload["error"].(map[string]any)
	details, _ := errObj["details"].(map[string]any)
	listed, _ := details["errors"].([]any)
	if errObj["code"] != "invalid_request" || len(listed) != 1 || listed[0] != "title: required" {
		t.Fatalf("payload=%s", rec.Body.String())
	}

	plain := httptest.NewRecorder()
	writeInvalidDocumentRequest(plain, primitives.ErrInvalidDocumentRequest)
	if plain.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", plain.Code)
	}
	payload = nil
	if err := json.Unmarshal(plain.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	errObj, _ = payload["error"].(map[string]any)
	if errObj["code"] != "invalid_request" || errObj["details"] != nil {
		t.Fatalf("plain invalid request gained details: %s", plain.Body.String())
	}
}
