package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestControlPlaneWorkspaceRestoreRechecksMembershipBeforeFinalization(t *testing.T) {
	scriptsDir := t.TempDir()
	signalDir := t.TempDir()
	writeBlockingRestoreScript(t, scriptsDir)

	t.Setenv("OAR_TEST_RESTORE_SIGNAL_DIR", signalDir)
	env := newControlPlaneTestEnvWithScripts(t, "", scriptsDir)
	defer env.Close()

	ownerAccount, ownerSession := registerAccount(t, env, "restore-owner@example.com", "Restore Owner", "cred-restore-owner")
	ownerToken := asString(t, ownerSession["access_token"])
	ownerID := asString(t, ownerAccount["id"])

	createOrganizationResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations", map[string]any{
		"slug":         "restore-guard",
		"display_name": "Restore Guard",
		"plan_tier":    "team",
	}, http.StatusCreated, authHeaders(ownerToken))
	organizationID := asString(t, asMap(t, createOrganizationResp["organization"])["id"])

	createWorkspaceResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces", workspaceCreatePayload(t, organizationID, "restore", "Restore Workspace", "us-central1", "standard"), http.StatusCreated, authHeaders(ownerToken))
	workspace := asMap(t, createWorkspaceResp["workspace"])
	workspaceID := asString(t, workspace["id"])
	backupDir := t.TempDir()

	type result struct {
		status int
		body   string
		err    error
	}

	restoreDone := make(chan result, 1)
	go func() {
		payload, err := json.Marshal(map[string]any{"backup_dir": backupDir})
		if err != nil {
			restoreDone <- result{err: err}
			return
		}
		req, err := http.NewRequest(http.MethodPost, env.server.URL+"/workspaces/"+workspaceID+"/restore", bytes.NewReader(payload))
		if err != nil {
			restoreDone <- result{err: err}
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+ownerToken)

		client := &http.Client{Timeout: 15 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			restoreDone <- result{err: err}
			return
		}
		defer resp.Body.Close()

		rawBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			restoreDone <- result{err: readErr}
			return
		}
		restoreDone <- result{status: resp.StatusCode, body: string(rawBody)}
	}()

	waitForPath(t, filepath.Join(signalDir, "started"), 10*time.Second)

	if _, err := env.workspace.DB().ExecContext(
		context.Background(),
		`UPDATE organization_memberships SET status = ? WHERE organization_id = ? AND account_id = ?`,
		"suspended",
		organizationID,
		ownerID,
	); err != nil {
		t.Fatalf("suspend membership: %v", err)
	}
	if err := os.WriteFile(filepath.Join(signalDir, "release"), []byte("go"), 0o644); err != nil {
		t.Fatalf("release restore script: %v", err)
	}

	restoreResult := <-restoreDone
	if restoreResult.err != nil {
		t.Fatalf("restore request failed: %v", restoreResult.err)
	}
	if restoreResult.status != http.StatusForbidden {
		t.Fatalf("expected restore to fail with 403 after membership revocation, got %d body=%s", restoreResult.status, restoreResult.body)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(restoreResult.body), &payload); err != nil {
		t.Fatalf("decode restore response: %v", err)
	}
	if got := asString(t, asMap(t, payload["error"])["code"]); got != "access_denied" {
		t.Fatalf("expected access_denied, got %q", got)
	}

	var workspaceStatus string
	if err := env.workspace.DB().QueryRowContext(context.Background(), `SELECT status FROM workspaces WHERE id = ?`, workspaceID).Scan(&workspaceStatus); err != nil {
		t.Fatalf("load workspace status: %v", err)
	}
	if workspaceStatus != "ready" {
		t.Fatalf("expected workspace status to remain ready, got %q", workspaceStatus)
	}

	var jobStatus, failureReason string
	if err := env.workspace.DB().QueryRowContext(context.Background(), `SELECT status, failure_reason FROM provisioning_jobs WHERE workspace_id = ? AND kind = ? ORDER BY requested_at DESC, id DESC LIMIT 1`, workspaceID, "workspace_restore").Scan(&jobStatus, &failureReason); err != nil {
		t.Fatalf("load restore job: %v", err)
	}
	if jobStatus != "failed" {
		t.Fatalf("expected restore job to be failed, got %q", jobStatus)
	}
	if strings.TrimSpace(failureReason) == "" {
		t.Fatal("expected failure_reason on failed restore job")
	}
}

func TestControlPlaneProvisioningJobReadsRequireWorkspaceAccess(t *testing.T) {
	env := newControlPlaneTestEnv(t, "")
	defer env.Close()

	_, ownerSession := registerAccount(t, env, "job-reader@example.com", "Job Reader", "cred-job-reader")
	ownerToken := asString(t, ownerSession["access_token"])

	createOrganizationResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations", map[string]any{
		"slug":         "job-guard",
		"display_name": "Job Guard",
		"plan_tier":    "team",
	}, http.StatusCreated, authHeaders(ownerToken))
	organizationID := asString(t, asMap(t, createOrganizationResp["organization"])["id"])

	createWorkspaceResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces", workspaceCreatePayload(t, organizationID, "jobguard", "Job Guard Workspace", "us-central1", "standard"), http.StatusCreated, authHeaders(ownerToken))
	workspace := asMap(t, createWorkspaceResp["workspace"])
	workspaceID := asString(t, workspace["id"])
	jobID := asString(t, asMap(t, createWorkspaceResp["provisioning_job"])["id"])

	if _, err := env.workspace.DB().ExecContext(
		context.Background(),
		`UPDATE workspaces SET status = ?, updated_at = ? WHERE id = ?`,
		"archived",
		time.Now().UTC().Format(time.RFC3339Nano),
		workspaceID,
	); err != nil {
		t.Fatalf("archive workspace: %v", err)
	}

	workspaceResp := requestJSON(t, http.MethodGet, env.server.URL+"/workspaces/"+workspaceID, nil, http.StatusForbidden, authHeaders(ownerToken))
	if got := asString(t, asMap(t, workspaceResp["error"])["code"]); got != "access_denied" {
		t.Fatalf("expected workspace access_denied, got %q", got)
	}

	jobResp := requestJSON(t, http.MethodGet, env.server.URL+"/provisioning/jobs/"+jobID, nil, http.StatusForbidden, authHeaders(ownerToken))
	if got := asString(t, asMap(t, jobResp["error"])["code"]); got != "access_denied" {
		t.Fatalf("expected job access_denied, got %q", got)
	}

	listResp := requestJSON(t, http.MethodGet, env.server.URL+"/provisioning/jobs?workspace_id="+workspaceID, nil, http.StatusForbidden, authHeaders(ownerToken))
	if got := asString(t, asMap(t, listResp["error"])["code"]); got != "access_denied" {
		t.Fatalf("expected filtered job list access_denied, got %q", got)
	}
}

func writeBlockingRestoreScript(t *testing.T, scriptsDir string) {
	t.Helper()

	provisionScript := `#!/bin/sh
set -eu
exit 0
`
	provisionPath := filepath.Join(scriptsDir, "provision-workspace.sh")
	if err := os.WriteFile(provisionPath, []byte(provisionScript), 0o755); err != nil {
		t.Fatalf("write provision script: %v", err)
	}

	script := `#!/bin/sh
set -eu

signal_dir="${OAR_TEST_RESTORE_SIGNAL_DIR:?restore signal dir required}"
mkdir -p "$signal_dir"
: > "$signal_dir/started"

while [ ! -f "$signal_dir/release" ]; do
	sleep 0.05
done
`
	scriptPath := filepath.Join(scriptsDir, "restore-workspace.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write restore script: %v", err)
	}
}

func waitForPath(t *testing.T, path string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}
