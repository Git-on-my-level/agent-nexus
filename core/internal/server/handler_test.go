package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestHealthEndpoint(t *testing.T) {
	t.Parallel()

	handler := NewHandler("0.2.2")
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: got %d", rr.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if payload["ok"] != true {
		t.Fatalf("expected ok=true, got %#v", payload)
	}
}

func TestVersionEndpoint(t *testing.T) {
	t.Parallel()

	handler := NewHandler("0.2.2")
	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: got %d", rr.Code)
	}

	var payload map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if payload["schema_version"] != "0.2.2" {
		t.Fatalf("unexpected schema_version: got %q", payload["schema_version"])
	}
	if payload["command_registry_digest"] == "" {
		t.Fatalf("expected command registry digest, payload=%#v", payload)
	}
}

func TestHandshakeIncludesCommandRegistryDigest(t *testing.T) {
	t.Parallel()

	handler := NewHandler("0.2.2")
	req := httptest.NewRequest(http.MethodGet, "/meta/handshake", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: got %d", rr.Code)
	}

	var payload map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if payload["schema_version"] != "0.2.2" {
		t.Fatalf("unexpected schema_version: got %q", payload["schema_version"])
	}
	if payload["command_registry_digest"] == "" {
		t.Fatalf("expected command registry digest, payload=%#v", payload)
	}
}

func TestVersionEndpointReturnsServiceUnavailableWithoutCommandMetadata(t *testing.T) {
	t.Parallel()

	handler := NewHandler("0.2.2", WithMetaCommandsPath(filepath.Join(t.TempDir(), "missing-commands.json")))
	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("unexpected status: got %d", rr.Code)
	}
}

func TestHandshakeReturnsServiceUnavailableWithoutCommandMetadata(t *testing.T) {
	t.Parallel()

	handler := NewHandler("0.2.2", WithMetaCommandsPath(filepath.Join(t.TempDir(), "missing-commands.json")))
	req := httptest.NewRequest(http.MethodGet, "/meta/handshake", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("unexpected status: got %d", rr.Code)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	t.Parallel()

	handler := NewHandler("0.2.2")
	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("unexpected status: got %d", rr.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil {
		t.Fatalf("expected error object, payload=%#v", payload)
	}
	if _, ok := errObj["recoverable"].(bool); !ok {
		t.Fatalf("expected recoverable boolean, payload=%#v", errObj)
	}
	if hint, _ := errObj["hint"].(string); hint == "" {
		t.Fatalf("expected non-empty hint, payload=%#v", errObj)
	}
}

func TestHealthEndpointStorageError(t *testing.T) {
	t.Parallel()

	handler := NewHandler("0.2.2", WithHealthCheck(func(context.Context) error {
		return errors.New("database unavailable")
	}))
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("unexpected status: got %d", rr.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if payload["ok"] != false {
		t.Fatalf("expected ok=false, got %#v", payload["ok"])
	}
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil {
		t.Fatalf("expected error object, payload=%#v", payload)
	}
	if _, ok := errObj["recoverable"].(bool); !ok {
		t.Fatalf("expected recoverable boolean, payload=%#v", errObj)
	}
	if hint, _ := errObj["hint"].(string); hint == "" {
		t.Fatalf("expected non-empty hint, payload=%#v", errObj)
	}
}
