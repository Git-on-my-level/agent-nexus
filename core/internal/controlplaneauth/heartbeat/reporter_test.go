package heartbeat

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"organization-autorunner-core/internal/controlplaneauth"
)

func TestReporterReportOnceSignsAndPostsHeartbeat(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate service identity key: %v", err)
	}
	identity, err := controlplaneauth.NewWorkspaceServiceIdentity(controlplaneauth.WorkspaceServiceIdentityConfig{
		ID:         "svc_ws_test",
		PrivateKey: privateKey,
	})
	if err != nil {
		t.Fatalf("new workspace service identity: %v", err)
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	verifier, err := controlplaneauth.NewWorkspaceServiceAssertionVerifier(controlplaneauth.WorkspaceServiceAssertionVerifierConfig{
		IdentityID:  identity.ID(),
		WorkspaceID: "ws_test",
		Audience:    controlplaneauth.WorkspaceServiceAssertionAudience,
		PublicKey:   publicKey,
	})
	if err != nil {
		t.Fatalf("new workspace service assertion verifier: %v", err)
	}

	requestSeen := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestSeen = true
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if got := r.URL.Path; got != "/workspaces/ws_test/heartbeat" {
			t.Fatalf("expected heartbeat path, got %q", got)
		}
		token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		verified, err := verifier.Verify(token)
		if err != nil {
			t.Fatalf("verify service assertion: %v", err)
		}
		if verified.Purpose != "heartbeat" {
			t.Fatalf("expected purpose heartbeat, got %q", verified.Purpose)
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode heartbeat payload: %v", err)
		}
		if got := asString(payload["version"]); got != "core/v1" {
			t.Fatalf("expected version core/v1, got %q", got)
		}
		if got := asString(payload["build"]); got != "build-123" {
			t.Fatalf("expected build build-123, got %q", got)
		}
		health := asMap(payload["health_summary"])
		if !asBool(health["ok"]) {
			t.Fatalf("expected readiness ok=true, got %#v", health)
		}
		projection := asMap(payload["projection_maintenance_summary"])
		if got := asString(projection["mode"]); got != "background" {
			t.Fatalf("expected projection mode background, got %q", got)
		}
		usage := asMap(payload["usage_summary"])
		if got := asString(usage["generated_at"]); got == "" {
			t.Fatalf("expected usage generated_at, got %#v", usage)
		}
		if got := asString(payload["last_successful_backup_at"]); got != "2026-03-24T01:02:03Z" {
			t.Fatalf("expected backup timestamp to be forwarded, got %q", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"workspace":{"id":"ws_test"}}`))
	}))
	defer server.Close()

	reporter, err := NewReporter(ReporterConfig{
		BaseURL:     server.URL,
		WorkspaceID: "ws_test",
		Interval:    time.Second,
		Version:     "core/v1",
		Build:       "build-123",
		Identity:    identity,
		ReadinessSummary: func(ctx context.Context) map[string]any {
			_ = ctx
			return map[string]any{"ok": true}
		},
		ProjectionMaintenanceSummary: func(ctx context.Context, now time.Time) map[string]any {
			_ = ctx
			_ = now
			return map[string]any{"mode": "background", "pending_dirty_count": 0}
		},
		UsageSummary: func(ctx context.Context) (map[string]any, error) {
			_ = ctx
			return map[string]any{
				"usage":        map[string]any{"blob_bytes": 64},
				"quota":        map[string]any{"max_blob_bytes": 128},
				"generated_at": "2026-03-24T01:02:03Z",
			}, nil
		},
		LastSuccessfulBackupAt: func(ctx context.Context) (*string, error) {
			_ = ctx
			value := "2026-03-24T01:02:03Z"
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
	if !requestSeen {
		t.Fatal("expected heartbeat request to be sent")
	}
}

func TestDiscoverLastSuccessfulBackupAtFindsNewestMatchingManifest(t *testing.T) {
	root := t.TempDir()
	workspaceRoot := filepath.Join(root, "workspace")
	backupRoot := filepath.Join(root, "backups")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace root: %v", err)
	}
	firstManifest := filepath.Join(backupRoot, "bundle-old", "manifest.env")
	secondManifest := filepath.Join(backupRoot, "bundle-new", "manifest.env")
	otherManifest := filepath.Join(backupRoot, "bundle-other", "manifest.env")
	for _, manifest := range []string{firstManifest, secondManifest, otherManifest} {
		if err := os.MkdirAll(filepath.Dir(manifest), 0o755); err != nil {
			t.Fatalf("mkdir manifest dir: %v", err)
		}
	}
	writeManifest(t, firstManifest, "2026-03-23T01:00:00Z", workspaceRoot)
	writeManifest(t, secondManifest, "2026-03-24T04:05:06Z", workspaceRoot)
	writeManifest(t, otherManifest, "2026-03-25T00:00:00Z", filepath.Join(root, "different-workspace"))

	got, err := DiscoverLastSuccessfulBackupAt(workspaceRoot)
	if err != nil {
		t.Fatalf("discover backup timestamp: %v", err)
	}
	if got == nil || *got != "2026-03-24T04:05:06Z" {
		t.Fatalf("expected newest matching backup timestamp, got %#v", got)
	}
}

func writeManifest(t *testing.T, path string, createdAt string, sourceWorkspaceRoot string) {
	t.Helper()
	content := "CREATED_AT=" + createdAt + "\nSOURCE_WORKSPACE_ROOT=" + sourceWorkspaceRoot + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write manifest %s: %v", path, err)
	}
}

func asMap(value any) map[string]any {
	out, _ := value.(map[string]any)
	return out
}

func asString(value any) string {
	out, _ := value.(string)
	return out
}

func asBool(value any) bool {
	out, _ := value.(bool)
	return out
}
