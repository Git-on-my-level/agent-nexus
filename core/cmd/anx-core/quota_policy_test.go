package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/server"
	"agent-nexus-core/internal/storage"
)

func TestCapacityQuotaModesKeepTechnicalSafety(t *testing.T) {
	for _, mode := range []struct {
		name, enforce string
		limited       bool
	}{
		{"self_host_default", "", false},
		{"external_policy", "false", false},
		{"explicit_local_policy", "true", true},
	} {
		t.Run(mode.name, func(t *testing.T) {
			t.Setenv("ANX_ENFORCE_LOCAL_QUOTAS", mode.enforce)
			configured := primitives.WorkspaceQuota{MaxDocuments: 1}
			quota := effectiveWorkspaceQuota(localQuotaEnforcementEnabled(), configured)
			workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer workspace.Close()
			store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir, primitives.WithWorkspaceQuota(quota))
			_, _, err = store.CreateDocument(context.Background(), "actor-1", map[string]any{"id": "one", "title": "One"}, "hello", "text", nil)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = store.CreateDocument(context.Background(), "actor-1", map[string]any{"id": "two", "title": "Two"}, "hello", "text", nil)
			if mode.limited {
				var violation *primitives.QuotaViolation
				if !errors.As(err, &violation) || violation.Code != "workspace_quota_exceeded" {
					t.Fatalf("expected capacity violation, got %v", err)
				}
			} else if err != nil {
				t.Fatalf("capacity quotas must be off: %v", err)
			}

			// Safety middleware applies regardless of the selected capacity policy.
			handler := server.NewHandler("test", server.WithAuthStore(auth.NewStore(workspace.DB())), server.WithRequestBodyLimits(server.RequestBodyLimits{Auth: 16}), server.WithRouteRateLimits(server.RouteRateLimits{AuthRequestsPerMinute: 1, AuthBurst: 1}))
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/auth/token", strings.NewReader(`{"grant_type":"refresh_token","refresh_token":"too-long"}`)))
			if recorder.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("body safety disabled: %d", recorder.Code)
			}
			recorder = httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/auth/token", strings.NewReader("{}")))
			if recorder.Code != http.StatusTooManyRequests {
				t.Fatalf("rate safety disabled: %d", recorder.Code)
			}
		})
	}
}
