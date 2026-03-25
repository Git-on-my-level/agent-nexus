package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"testing"
	"time"

	"organization-autorunner-core/internal/controlplaneauth"
	"organization-autorunner-core/internal/controlplaneauth/heartbeat"
)

func TestHeartbeatReporterPostsAcceptedHeartbeatAndStoresWorkspaceSummaries(t *testing.T) {
	env := newControlPlaneTestEnv(t, "")
	defer env.Close()

	_, ownerSession := registerAccount(t, env, "heartbeat-owner@example.com", "Heartbeat Owner", "cred-heartbeat-owner")
	ownerToken := asString(t, ownerSession["access_token"])

	createOrganizationResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations", map[string]any{
		"slug":         "heartbeat-org",
		"display_name": "Heartbeat Org",
		"plan_tier":    "team",
	}, http.StatusCreated, authHeaders(ownerToken))
	organizationID := asString(t, asMap(t, createOrganizationResp["organization"])["id"])

	_, serviceIdentityPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate workspace service key: %v", err)
	}
	serviceIdentity, err := controlplaneauth.NewWorkspaceServiceIdentity(controlplaneauth.WorkspaceServiceIdentityConfig{
		ID:         "svc_ws_heartbeat",
		PrivateKey: serviceIdentityPrivateKey,
	})
	if err != nil {
		t.Fatalf("new workspace service identity: %v", err)
	}

	createWorkspaceResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces", map[string]any{
		"organization_id":             organizationID,
		"slug":                        "heartbeat",
		"display_name":                "Heartbeat Workspace",
		"region":                      "us-central1",
		"workspace_tier":              "standard",
		"service_identity_id":         serviceIdentity.ID(),
		"service_identity_public_key": serviceIdentity.PublicKeyBase64(),
	}, http.StatusCreated, authHeaders(ownerToken))
	workspaceID := asString(t, asMap(t, createWorkspaceResp["workspace"])["id"])

	reporter, err := heartbeat.NewReporter(heartbeat.ReporterConfig{
		BaseURL:     env.server.URL,
		WorkspaceID: workspaceID,
		Interval:    time.Second,
		Version:     "core/v9",
		Build:       "build-heartbeat",
		Identity:    serviceIdentity,
		ReadinessSummary: func(ctx context.Context) map[string]any {
			_ = ctx
			return map[string]any{"ok": true, "status": "ready"}
		},
		ProjectionMaintenanceSummary: func(ctx context.Context, now time.Time) map[string]any {
			_ = ctx
			return map[string]any{
				"mode":                          "background",
				"pending_dirty_count":           0,
				"last_successful_stale_scan_at": now.UTC().Format(time.RFC3339Nano),
			}
		},
		UsageSummary: func(ctx context.Context) (map[string]any, error) {
			_ = ctx
			return map[string]any{
				"usage": map[string]any{
					"blob_bytes":              256,
					"blob_objects":            2,
					"artifact_count":          1,
					"document_count":          1,
					"document_revision_count": 1,
				},
				"quota": map[string]any{
					"max_blob_bytes":         1024,
					"max_artifacts":          10,
					"max_documents":          10,
					"max_document_revisions": 10,
					"max_upload_bytes":       1024,
				},
				"generated_at": "2026-03-24T03:04:05Z",
			}, nil
		},
		LastSuccessfulBackupAt: func(ctx context.Context) (*string, error) {
			_ = ctx
			value := "2026-03-24T02:03:04Z"
			return &value, nil
		},
		JitterFraction: -1,
	})
	if err != nil {
		t.Fatalf("new reporter: %v", err)
	}

	if err := reporter.ReportOnce(context.Background()); err != nil {
		t.Fatalf("report once: %v", err)
	}

	workspaceResp := requestJSON(t, http.MethodGet, env.server.URL+"/workspaces/"+workspaceID, nil, http.StatusOK, authHeaders(ownerToken))
	workspace := asMap(t, workspaceResp["workspace"])
	if got := asString(t, workspace["last_heartbeat_at"]); got == "" {
		t.Fatal("expected last_heartbeat_at to be recorded")
	}
	if got := asString(t, workspace["heartbeat_version"]); got != "core/v9" {
		t.Fatalf("expected heartbeat_version core/v9, got %q", got)
	}
	if got := asString(t, workspace["heartbeat_build"]); got != "build-heartbeat" {
		t.Fatalf("expected heartbeat_build build-heartbeat, got %q", got)
	}
	if got := asString(t, workspace["last_successful_backup_at"]); got != "2026-03-24T02:03:04Z" {
		t.Fatalf("expected last_successful_backup_at to be stored, got %q", got)
	}
	health := asMap(t, workspace["heartbeat_health_summary"])
	if got := asString(t, health["status"]); got != "ready" {
		t.Fatalf("expected heartbeat health status ready, got %q", got)
	}
	projection := asMap(t, workspace["heartbeat_projection_maintenance_summary"])
	if got := asString(t, projection["mode"]); got != "background" {
		t.Fatalf("expected heartbeat projection mode background, got %q", got)
	}
	usage := asMap(t, workspace["heartbeat_usage_summary"])
	if got := asString(t, usage["generated_at"]); got != "2026-03-24T03:04:05Z" {
		t.Fatalf("expected heartbeat usage summary generated_at, got %q", got)
	}
}
