package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"organization-autorunner-core/internal/auth"
	"organization-autorunner-core/internal/storage"
)

func TestHealthEndpoint(t *testing.T) {
	t.Parallel()

	handler := NewHandler("0.2.2", WithHealthCheck(func(context.Context) error {
		return errors.New("database unavailable")
	}))
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
	if len(payload) != 1 {
		t.Fatalf("expected minimal liveness payload, got %#v", payload)
	}
}

func TestLivezEndpoint(t *testing.T) {
	t.Parallel()

	handler := NewHandler("0.2.2")
	req := httptest.NewRequest(http.MethodGet, "/livez", nil)
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
	if len(payload) != 1 {
		t.Fatalf("expected minimal liveness payload, got %#v", payload)
	}
}

func TestReadyzEndpoint(t *testing.T) {
	t.Parallel()

	handler := NewHandler("0.2.2")
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
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

	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if payload["schema_version"] != "0.2.2" {
		t.Fatalf("unexpected schema_version: got %#v", payload["schema_version"])
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

	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if payload["schema_version"] != "0.2.2" {
		t.Fatalf("unexpected schema_version: got %#v", payload["schema_version"])
	}
	if payload["command_registry_digest"] == "" {
		t.Fatalf("expected command registry digest, payload=%#v", payload)
	}
	if payload["dev_actor_mode"] != false {
		t.Fatalf("expected dev_actor_mode=false by default, got %v", payload["dev_actor_mode"])
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
	req := httptest.NewRequest(http.MethodPost, "/readyz", nil)
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

func TestRequestBodyTooLargeReturnsRequestTooLarge(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	handler := NewHandler(
		"0.2.2",
		WithAuthStore(auth.NewStore(workspace.DB())),
		WithRequestBodyLimits(RequestBodyLimits{Auth: 16}),
	)
	req := httptest.NewRequest(http.MethodPost, "/auth/token", strings.NewReader(`{"grant_type":"refresh_token","refresh_token":"this-is-too-long"}`))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("unexpected status: got %d want %d", rr.Code, http.StatusRequestEntityTooLarge)
	}

	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil {
		t.Fatalf("expected error object, payload=%#v", payload)
	}
	if got := errObj["code"]; got != "request_too_large" {
		t.Fatalf("unexpected error code: %#v", got)
	}
	requestBody, _ := payload["request_body"].(map[string]any)
	if requestBody == nil {
		t.Fatalf("expected request_body details, payload=%#v", payload)
	}
	if limit, _ := requestBody["limit_bytes"].(float64); limit != 16 {
		t.Fatalf("unexpected limit_bytes: %#v", requestBody["limit_bytes"])
	}
}

func TestAuthRouteRateLimitingReturnsRateLimited(t *testing.T) {
	t.Parallel()

	handler := NewHandler(
		"0.2.2",
		WithRouteRateLimits(RouteRateLimits{
			AuthRequestsPerMinute:  1,
			AuthBurst:              1,
			WriteRequestsPerMinute: 1,
			WriteBurst:             1,
		}),
	)

	body := strings.NewReader(`{"grant_type":"refresh_token","refresh_token":"token"}`)
	req1 := httptest.NewRequest(http.MethodPost, "/auth/token", body)
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)

	req2 := httptest.NewRequest(http.MethodPost, "/auth/token", strings.NewReader(`{"grant_type":"refresh_token","refresh_token":"token"}`))
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("unexpected status: got %d want %d", rr2.Code, http.StatusTooManyRequests)
	}

	var payload map[string]any
	if err := json.Unmarshal(rr2.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil {
		t.Fatalf("expected error object, payload=%#v", payload)
	}
	if got := errObj["code"]; got != "rate_limited" {
		t.Fatalf("unexpected error code: %#v", got)
	}
	if retryAfter := rr2.Header().Get("Retry-After"); retryAfter == "" {
		t.Fatalf("expected Retry-After header, payload=%#v", payload)
	}
}

func TestReadyzEndpointStorageError(t *testing.T) {
	t.Parallel()

	handler := NewHandler("0.2.2", WithHealthCheck(func(context.Context) error {
		return errors.New("database unavailable")
	}))
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
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

func TestLoopbackVerificationReadsAllowReadOnlyWorkspaceRoutes(t *testing.T) {
	t.Parallel()

	handler := NewHandler("0.2.2", WithAllowLoopbackVerificationReads(true))
	req := httptest.NewRequest(http.MethodGet, "/artifacts", nil)
	req.RemoteAddr = "127.0.0.1:43123"
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("unexpected status: got %d want %d", rr.Code, http.StatusServiceUnavailable)
	}
}

func TestOpsHealthRejectsUnauthenticatedNonLoopbackRequests(t *testing.T) {
	t.Parallel()

	handler := NewHandler("0.2.2")
	req := httptest.NewRequest(http.MethodGet, "/ops/health", nil)
	req.RemoteAddr = "192.0.2.10:43123"
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected status: got %d want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestLoopbackVerificationReadsAllowOpsHealth(t *testing.T) {
	t.Parallel()

	handler := NewHandler("0.2.2", WithAllowLoopbackVerificationReads(true))
	req := httptest.NewRequest(http.MethodGet, "/ops/health", nil)
	req.RemoteAddr = "127.0.0.1:43123"
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: got %d want %d", rr.Code, http.StatusOK)
	}

	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if payload["ok"] != true {
		t.Fatalf("expected ok=true, got %#v", payload)
	}
}

func TestLoopbackVerificationReadsDoNotAllowNonLoopbackRequests(t *testing.T) {
	t.Parallel()

	handler := NewHandler("0.2.2", WithAllowLoopbackVerificationReads(true))
	req := httptest.NewRequest(http.MethodGet, "/artifacts", nil)
	req.RemoteAddr = "192.0.2.10:43123"
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected status: got %d want %d", rr.Code, http.StatusUnauthorized)
	}
}
