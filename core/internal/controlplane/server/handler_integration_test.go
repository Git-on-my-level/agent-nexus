package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"organization-autorunner-core/internal/controlplane"
	cpstorage "organization-autorunner-core/internal/controlplane/storage"
	"organization-autorunner-core/internal/controlplaneauth"
)

const (
	testRPID   = "control.oar.test"
	testOrigin = "https://control.oar.test"
)

type controlPlaneTestEnv struct {
	t               *testing.T
	root            string
	workspace       *cpstorage.Workspace
	service         *controlplane.Service
	server          *httptest.Server
	grantIssuer     string
	grantAudience   string
	grantPublicKey  ed25519.PublicKey
	grantPrivateKey ed25519.PrivateKey
}

func TestControlPlaneAccountOrganizationWorkspaceInviteJobAuditFlow(t *testing.T) {
	env := newControlPlaneTestEnv(t, "")
	defer env.Close()

	_, ownerSession := registerAccount(t, env, "owner@example.com", "Owner", "cred-owner")

	loginSession := loginAccount(t, env, "owner@example.com", "cred-owner")
	ownerToken := asString(t, loginSession["access_token"])

	createOrganizationResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations", map[string]any{
		"slug":         "acme",
		"display_name": "Acme",
		"plan_tier":    "team",
	}, http.StatusCreated, authHeaders(ownerToken))
	organization := asMap(t, createOrganizationResp["organization"])
	organizationID := asString(t, organization["id"])

	organizationsPage := requestJSON(t, http.MethodGet, env.server.URL+"/organizations?limit=1", nil, http.StatusOK, authHeaders(ownerToken))
	if got := len(asSlice(t, organizationsPage["organizations"])); got != 1 {
		t.Fatalf("expected 1 organization, got %d", got)
	}

	createInviteResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations/"+organizationID+"/invites", map[string]any{
		"email": "member@example.com",
		"role":  "member",
	}, http.StatusCreated, authHeaders(ownerToken))
	if inviteURL := asString(t, createInviteResp["invite_url"]); inviteURL == "" {
		t.Fatal("expected invite_url in create invite response")
	}

	_, memberSession := registerAccount(t, env, "member@example.com", "Member", "cred-member")
	memberToken := asString(t, memberSession["access_token"])

	memberOrganizations := requestJSON(t, http.MethodGet, env.server.URL+"/organizations", nil, http.StatusOK, authHeaders(memberToken))
	if got := len(asSlice(t, memberOrganizations["organizations"])); got != 1 {
		t.Fatalf("expected member to see 1 organization, got %d", got)
	}

	firstMembershipPage := requestJSON(t, http.MethodGet, env.server.URL+"/organizations/"+organizationID+"/memberships?limit=1", nil, http.StatusOK, authHeaders(ownerToken))
	if got := len(asSlice(t, firstMembershipPage["memberships"])); got != 1 {
		t.Fatalf("expected 1 membership on first page, got %d", got)
	}
	membershipCursor := asString(t, firstMembershipPage["next_cursor"])
	if membershipCursor == "" {
		t.Fatal("expected next_cursor for memberships pagination")
	}
	secondMembershipPage := requestJSON(t, http.MethodGet, env.server.URL+"/organizations/"+organizationID+"/memberships?limit=1&cursor="+url.QueryEscape(membershipCursor), nil, http.StatusOK, authHeaders(ownerToken))
	if got := len(asSlice(t, secondMembershipPage["memberships"])); got != 1 {
		t.Fatalf("expected 1 membership on second page, got %d", got)
	}

	firstInvitePage := requestJSON(t, http.MethodGet, env.server.URL+"/organizations/"+organizationID+"/invites?limit=1", nil, http.StatusOK, authHeaders(ownerToken))
	if got := len(asSlice(t, firstInvitePage["invites"])); got != 1 {
		t.Fatalf("expected 1 invite, got %d", got)
	}

	firstWorkspaceCreate := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces", map[string]any{
		"organization_id": organizationID,
		"slug":            "ops",
		"display_name":    "Ops",
		"region":          "us-central1",
		"workspace_tier":  "standard",
	}, http.StatusCreated, authHeaders(ownerToken))
	firstWorkspace := asMap(t, firstWorkspaceCreate["workspace"])
	firstJob := asMap(t, firstWorkspaceCreate["provisioning_job"])

	secondWorkspaceCreate := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces", map[string]any{
		"organization_id": organizationID,
		"slug":            "eng",
		"display_name":    "Engineering",
		"region":          "us-east1",
		"workspace_tier":  "plus",
	}, http.StatusCreated, authHeaders(ownerToken))
	secondWorkspace := asMap(t, secondWorkspaceCreate["workspace"])
	secondJob := asMap(t, secondWorkspaceCreate["provisioning_job"])

	workspacesPage := requestJSON(t, http.MethodGet, env.server.URL+"/workspaces?organization_id="+url.QueryEscape(organizationID)+"&limit=1", nil, http.StatusOK, authHeaders(ownerToken))
	if got := len(asSlice(t, workspacesPage["workspaces"])); got != 1 {
		t.Fatalf("expected 1 workspace on first page, got %d", got)
	}
	if asString(t, workspacesPage["next_cursor"]) == "" {
		t.Fatal("expected next_cursor for workspaces pagination")
	}

	jobResp := requestJSON(t, http.MethodGet, env.server.URL+"/provisioning/jobs/"+asString(t, firstJob["id"]), nil, http.StatusOK, authHeaders(ownerToken))
	if got := asString(t, asMap(t, jobResp["job"])["id"]); got != asString(t, firstJob["id"]) {
		t.Fatalf("expected provisioning job id %q, got %q", asString(t, firstJob["id"]), got)
	}

	firstJobsPage := requestJSON(t, http.MethodGet, env.server.URL+"/provisioning/jobs?organization_id="+url.QueryEscape(organizationID)+"&limit=1", nil, http.StatusOK, authHeaders(ownerToken))
	if got := len(asSlice(t, firstJobsPage["jobs"])); got != 1 {
		t.Fatalf("expected 1 job on first page, got %d", got)
	}
	jobsCursor := asString(t, firstJobsPage["next_cursor"])
	if jobsCursor == "" {
		t.Fatal("expected next_cursor for jobs pagination")
	}
	secondJobsPage := requestJSON(t, http.MethodGet, env.server.URL+"/provisioning/jobs?organization_id="+url.QueryEscape(organizationID)+"&limit=1&cursor="+url.QueryEscape(jobsCursor), nil, http.StatusOK, authHeaders(ownerToken))
	if got := len(asSlice(t, secondJobsPage["jobs"])); got != 1 {
		t.Fatalf("expected 1 job on second page, got %d", got)
	}

	firstAccountsPage := requestJSON(t, http.MethodGet, env.server.URL+"/accounts?limit=1", nil, http.StatusOK, authHeaders(ownerToken))
	if got := len(asSlice(t, firstAccountsPage["accounts"])); got != 1 {
		t.Fatalf("expected 1 account on first page, got %d", got)
	}
	if asString(t, firstAccountsPage["next_cursor"]) == "" {
		t.Fatal("expected next_cursor for accounts pagination")
	}

	firstAuditPage := requestJSON(t, http.MethodGet, env.server.URL+"/audit-events?organization_id="+url.QueryEscape(organizationID)+"&limit=2", nil, http.StatusOK, authHeaders(ownerToken))
	if got := len(asSlice(t, firstAuditPage["events"])); got != 2 {
		t.Fatalf("expected 2 audit events on first page, got %d", got)
	}
	auditCursor := asString(t, firstAuditPage["next_cursor"])
	if auditCursor == "" {
		t.Fatal("expected next_cursor for audit pagination")
	}
	secondAuditPage := requestJSON(t, http.MethodGet, env.server.URL+"/audit-events?organization_id="+url.QueryEscape(organizationID)+"&limit=2&cursor="+url.QueryEscape(auditCursor), nil, http.StatusOK, authHeaders(ownerToken))
	if got := len(asSlice(t, secondAuditPage["events"])); got == 0 {
		t.Fatal("expected more audit events on second page")
	}

	usageResp := requestJSON(t, http.MethodGet, env.server.URL+"/organizations/"+organizationID+"/usage-summary", nil, http.StatusOK, authHeaders(ownerToken))
	usageSummary := asMap(t, usageResp["summary"])
	usage := asMap(t, usageSummary["usage"])
	if got := int(asFloat(t, usage["workspace_count"])); got != 2 {
		t.Fatalf("expected usage workspace_count=2, got %d", got)
	}

	launchResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces/"+asString(t, firstWorkspace["id"])+"/launch-sessions", map[string]any{
		"return_path": "/ws/ops",
	}, http.StatusOK, authHeaders(memberToken))
	launchSession := asMap(t, launchResp["launch_session"])
	exchangeResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces/"+asString(t, firstWorkspace["id"])+"/session-exchange", map[string]any{
		"exchange_token": asString(t, launchSession["exchange_token"]),
	}, http.StatusOK, nil)
	grant := asMap(t, exchangeResp["grant"])
	if got := asString(t, grant["scope"]); got != "workspace:"+asString(t, firstWorkspace["id"]) {
		t.Fatalf("expected workspace grant scope, got %q", got)
	}

	if got := asString(t, secondWorkspace["id"]); got == "" {
		t.Fatal("expected second workspace id")
	}
	if got := asString(t, secondJob["id"]); got == "" {
		t.Fatal("expected second provisioning job id")
	}

	revokeResp := requestJSON(t, http.MethodDelete, env.server.URL+"/account/sessions/current", nil, http.StatusOK, authHeaders(ownerToken))
	if !asBool(t, revokeResp["revoked"]) {
		t.Fatal("expected revoked=true")
	}

	requestJSON(t, http.MethodGet, env.server.URL+"/organizations", nil, http.StatusUnauthorized, authHeaders(ownerToken))

	_ = ownerSession
}

func TestControlPlaneWorkspaceProvisioningProducesReachableDeploymentAndRoutingManifest(t *testing.T) {
	env := newControlPlaneTestEnv(t, "")
	defer env.Close()

	_, ownerSession := registerAccount(t, env, "owner2@example.com", "Owner Two", "cred-owner-2")
	ownerToken := asString(t, ownerSession["access_token"])

	createOrganizationResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations", map[string]any{
		"slug":         "route-org",
		"display_name": "Route Org",
		"plan_tier":    "team",
	}, http.StatusCreated, authHeaders(ownerToken))
	organizationID := asString(t, asMap(t, createOrganizationResp["organization"])["id"])

	createWorkspaceResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces", map[string]any{
		"organization_id": organizationID,
		"slug":            "route",
		"display_name":    "Route Workspace",
		"region":          "us-central1",
		"workspace_tier":  "standard",
	}, http.StatusCreated, authHeaders(ownerToken))
	t.Log("provisioning response received")
	workspace := asMap(t, createWorkspaceResp["workspace"])
	deploymentRoot := asString(t, workspace["deployment_root"])
	if deploymentRoot == "" {
		t.Fatal("expected deployment_root in workspace response")
	}
	if got := asString(t, workspace["routing_manifest_path"]); got == "" {
		t.Fatal("expected routing_manifest_path in workspace response")
	}

	manifestResp := requestJSON(t, http.MethodGet, env.server.URL+"/workspaces/"+asString(t, workspace["id"])+"/routing-manifest", nil, http.StatusOK, authHeaders(ownerToken))
	t.Log("routing manifest response received")
	manifest := asMap(t, manifestResp["routing_manifest"])
	if got := asString(t, manifest["deployment_root"]); got != deploymentRoot {
		t.Fatalf("expected routing manifest deployment_root %q, got %q", deploymentRoot, got)
	}

	coreWorkspaceRoot := filepath.Join(deploymentRoot, "workspace")
	t.Log("starting core server")
	coreCmd, baseURL, cleanup := startCoreServerForTest(t, coreWorkspaceRoot)
	defer cleanup()
	t.Log("core server ready")

	readyResp := requestJSON(t, http.MethodGet, baseURL+"/readyz", nil, http.StatusOK, nil)
	t.Log("readyz response received")
	if !asBool(t, readyResp["ok"]) {
		t.Fatal("expected reachable workspace core to report ok=true")
	}

	_ = coreCmd
}

func TestControlPlaneWorkspaceBackupMaintenanceAndRetentionSweep(t *testing.T) {
	env := newControlPlaneTestEnv(t, "")
	defer env.Close()

	_, ownerSession := registerAccount(t, env, "backup-owner@example.com", "Backup Owner", "cred-backup-owner")
	ownerToken := asString(t, ownerSession["access_token"])

	createOrganizationResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations", map[string]any{
		"slug":         "backup-org",
		"display_name": "Backup Org",
		"plan_tier":    "team",
	}, http.StatusCreated, authHeaders(ownerToken))
	organizationID := asString(t, asMap(t, createOrganizationResp["organization"])["id"])

	createWorkspaceResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces", map[string]any{
		"organization_id": organizationID,
		"slug":            "nightly",
		"display_name":    "Nightly Workspace",
		"region":          "us-central1",
		"workspace_tier":  "standard",
	}, http.StatusCreated, authHeaders(ownerToken))
	workspace := asMap(t, createWorkspaceResp["workspace"])
	workspaceID := asString(t, workspace["id"])
	coreWorkspaceRoot := filepath.Join(asString(t, workspace["deployment_root"]), "workspace")
	_, _, cleanup := startCoreServerForTest(t, coreWorkspaceRoot)
	defer cleanup()

	dueAt := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	if _, err := env.workspace.DB().ExecContext(context.Background(), `UPDATE workspace_backup_schedules
		SET next_run_at = ?, retention_days = ?, interval_seconds = ?, schedule_name = ?, updated_at = ?
		WHERE workspace_id = ?`,
		dueAt,
		1,
		int(time.Hour/time.Second),
		"nightly",
		time.Now().UTC().Format(time.RFC3339Nano),
		workspaceID,
	); err != nil {
		t.Fatalf("force workspace backup schedule due: %v", err)
	}

	if err := env.service.RunBackupMaintenancePass(context.Background()); err != nil {
		t.Fatalf("run backup maintenance pass: %v", err)
	}

	var (
		runID              string
		status             string
		backupDir          string
		retentionExpiresAt sql.NullString
		failureReason      sql.NullString
		prunedAt           sql.NullString
	)
	row := env.workspace.DB().QueryRowContext(context.Background(), `SELECT id, status, backup_dir, retention_expires_at, failure_reason, pruned_at
		FROM workspace_backup_runs
		WHERE workspace_id = ?
		ORDER BY requested_at DESC, id DESC
		LIMIT 1`, workspaceID)
	if err := row.Scan(&runID, &status, &backupDir, &retentionExpiresAt, &failureReason, &prunedAt); err != nil {
		t.Fatalf("load workspace backup run: %v", err)
	}
	if status != "succeeded" {
		t.Fatalf("expected scheduled backup run to succeed, got %q (%s)", status, strings.TrimSpace(failureReason.String))
	}
	if backupDir == "" {
		t.Fatal("expected backup_dir in backup run record")
	}
	if !retentionExpiresAt.Valid || strings.TrimSpace(retentionExpiresAt.String) == "" {
		t.Fatal("expected retention_expires_at in backup run record")
	}
	if prunedAt.Valid {
		t.Fatal("did not expect backup run to be pruned before retention expiry")
	}

	inventoryResp := requestJSON(t, http.MethodGet, env.server.URL+"/organizations/"+organizationID+"/workspace-inventory?limit=1", nil, http.StatusOK, authHeaders(ownerToken))
	inventoryItems := asSlice(t, inventoryResp["workspaces"])
	if len(inventoryItems) != 1 {
		t.Fatalf("expected 1 workspace in inventory, got %d", len(inventoryItems))
	}
	inventoryWorkspace := asMap(t, asMap(t, inventoryItems[0])["workspace"])
	if got := asString(t, inventoryWorkspace["last_successful_backup_at"]); got == "" {
		t.Fatal("expected inventory to expose last_successful_backup_at after scheduled backup")
	}

	pastRetention := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	if _, err := env.workspace.DB().ExecContext(context.Background(), `UPDATE workspace_backup_runs
		SET retention_expires_at = ?, updated_at = ?
		WHERE id = ?`, pastRetention, time.Now().UTC().Format(time.RFC3339Nano), runID); err != nil {
		t.Fatalf("force backup retention expiry: %v", err)
	}

	if err := env.service.RunBackupMaintenancePass(context.Background()); err != nil {
		t.Fatalf("run backup retention sweep: %v", err)
	}

	if _, err := os.Stat(backupDir); !os.IsNotExist(err) {
		t.Fatalf("expected backup bundle to be pruned, stat err=%v", err)
	}

	row = env.workspace.DB().QueryRowContext(context.Background(), `SELECT pruned_at, prune_failure_reason
		FROM workspace_backup_runs
		WHERE id = ?`, runID)
	var pruneFailureReason sql.NullString
	if err := row.Scan(&prunedAt, &pruneFailureReason); err != nil {
		t.Fatalf("load pruned backup run: %v", err)
	}
	if !prunedAt.Valid || strings.TrimSpace(prunedAt.String) == "" {
		t.Fatal("expected backup run to be marked as pruned")
	}
}

func TestControlPlaneProvisionRestoreFailureAndRetrySemantics(t *testing.T) {
	env := newControlPlaneTestEnv(t, "")
	defer env.Close()

	_, ownerSession := registerAccount(t, env, "owner3@example.com", "Owner Three", "cred-owner-3")
	ownerToken := asString(t, ownerSession["access_token"])

	createOrganizationResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations", map[string]any{
		"slug":         "retry-org",
		"display_name": "Retry Org",
		"plan_tier":    "team",
	}, http.StatusCreated, authHeaders(ownerToken))
	organizationID := asString(t, asMap(t, createOrganizationResp["organization"])["id"])

	createWorkspaceResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces", map[string]any{
		"organization_id": organizationID,
		"slug":            "retry",
		"display_name":    "Retry Workspace",
		"region":          "us-central1",
		"workspace_tier":  "standard",
	}, http.StatusCreated, authHeaders(ownerToken))
	workspace := asMap(t, createWorkspaceResp["workspace"])
	workspaceID := asString(t, workspace["id"])
	deploymentRoot := asString(t, workspace["deployment_root"])
	if deploymentRoot == "" {
		t.Fatal("expected deployment_root in workspace response")
	}

	coreWorkspaceRoot := filepath.Join(deploymentRoot, "workspace")
	_, _, cleanup := startCoreServerForTest(t, coreWorkspaceRoot)
	defer cleanup()

	repoRoot := findRepoRoot(t)
	backupDir := filepath.Join(t.TempDir(), "backup")
	runScript(t, filepath.Join(repoRoot, "scripts", "hosted", "backup-workspace.sh"), "--instance-root", deploymentRoot, "--output-dir", backupDir)
	cleanup()

	badBackupDir := filepath.Join(t.TempDir(), "backup-bad")
	copyDir(t, backupDir, badBackupDir)
	tamperManifest(t, filepath.Join(badBackupDir, "manifest.env"))

	restoreFailureResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces/"+workspaceID+"/restore", map[string]any{
		"backup_dir": badBackupDir,
	}, http.StatusOK, authHeaders(ownerToken))
	restoreFailureJob := asMap(t, restoreFailureResp["provisioning_job"])
	if got := asString(t, restoreFailureJob["status"]); got != "failed" {
		t.Fatalf("expected failed restore response job, got %q", got)
	}

	failedJobsResp := requestJSON(t, http.MethodGet, env.server.URL+"/provisioning/jobs?workspace_id="+url.QueryEscape(workspaceID), nil, http.StatusOK, authHeaders(ownerToken))
	failedJobs := asSlice(t, failedJobsResp["jobs"])
	if len(failedJobs) != 2 {
		t.Fatalf("expected 2 jobs after failed restore, got %d", len(failedJobs))
	}
	restoreJob := asMap(t, failedJobs[1])
	if got := asString(t, restoreJob["status"]); got != "failed" {
		t.Fatalf("expected failed restore job, got %q", got)
	}
	if got := asString(t, restoreJob["failure_reason"]); got == "" {
		t.Fatal("expected failure_reason for failed restore job")
	}
	if got := asString(t, restoreJob["stderr_tail"]); got == "" {
		t.Fatal("expected stderr_tail for failed restore job")
	}

	requestJSON(t, http.MethodPost, env.server.URL+"/workspaces/"+workspaceID+"/restore", map[string]any{
		"backup_dir": backupDir,
	}, http.StatusOK, authHeaders(ownerToken))

	retryJobsResp := requestJSON(t, http.MethodGet, env.server.URL+"/provisioning/jobs?workspace_id="+url.QueryEscape(workspaceID), nil, http.StatusOK, authHeaders(ownerToken))
	retryJobs := asSlice(t, retryJobsResp["jobs"])
	if len(retryJobs) != 3 {
		t.Fatalf("expected 3 jobs after retry, got %d", len(retryJobs))
	}
	lastJob := asMap(t, retryJobs[2])
	t.Logf("retry job payload: %#v", lastJob)
	if got := asString(t, lastJob["status"]); got != "succeeded" {
		t.Fatalf("expected succeeded retry job, got %q", got)
	}
}

func TestControlPlaneProvisioningFailureIsDurableAndRetryable(t *testing.T) {
	failingScriptsDir := failingProvisionScriptsDir(t)
	env := newControlPlaneTestEnvWithScripts(t, "", failingScriptsDir)
	defer env.Close()

	_, ownerSession := registerAccount(t, env, "owner4@example.com", "Owner Four", "cred-owner-4")
	ownerToken := asString(t, ownerSession["access_token"])

	createOrganizationResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations", map[string]any{
		"slug":         "broken-org",
		"display_name": "Broken Org",
		"plan_tier":    "team",
	}, http.StatusCreated, authHeaders(ownerToken))
	organizationID := asString(t, asMap(t, createOrganizationResp["organization"])["id"])

	provisionFailureResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces", map[string]any{
		"organization_id": organizationID,
		"slug":            "broken",
		"display_name":    "Broken Workspace",
		"region":          "us-central1",
		"workspace_tier":  "standard",
	}, http.StatusCreated, authHeaders(ownerToken))
	provisionFailureJob := asMap(t, provisionFailureResp["provisioning_job"])
	if got := asString(t, provisionFailureJob["status"]); got != "failed" {
		t.Fatalf("expected failed provisioning response job, got %q", got)
	}

	jobsResp := requestJSON(t, http.MethodGet, env.server.URL+"/provisioning/jobs?organization_id="+url.QueryEscape(organizationID), nil, http.StatusOK, authHeaders(ownerToken))
	jobs := asSlice(t, jobsResp["jobs"])
	if len(jobs) != 1 {
		t.Fatalf("expected 1 failed provisioning job, got %d", len(jobs))
	}
	job := asMap(t, jobs[0])
	if got := asString(t, job["status"]); got != "failed" {
		t.Fatalf("expected failed job, got %q", got)
	}
	if got := asString(t, job["failure_reason"]); got == "" {
		t.Fatal("expected failure_reason for failed provisioning job")
	}
	if got := asString(t, job["stderr_tail"]); got == "" {
		t.Fatal("expected stderr_tail for failed provisioning job")
	}
}

func TestControlPlaneFleetHeartbeatBackupUpgradeDrillAndInventory(t *testing.T) {
	env := newControlPlaneTestEnv(t, "")
	defer env.Close()

	_, ownerSession := registerAccount(t, env, "fleet@example.com", "Fleet Owner", "cred-fleet-owner")
	ownerToken := asString(t, ownerSession["access_token"])

	createOrganizationResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations", map[string]any{
		"slug":         "fleet-org",
		"display_name": "Fleet Org",
		"plan_tier":    "team",
	}, http.StatusCreated, authHeaders(ownerToken))
	organizationID := asString(t, asMap(t, createOrganizationResp["organization"])["id"])

	serviceIdentityID := "svc_fleet_workspace"
	_, serviceIdentityPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate workspace service key: %v", err)
	}
	serviceIdentityKey, err := controlplaneauth.NewWorkspaceServiceIdentity(controlplaneauth.WorkspaceServiceIdentityConfig{
		ID:         serviceIdentityID,
		PrivateKey: serviceIdentityPrivateKey,
	})
	if err != nil {
		t.Fatalf("new workspace service identity: %v", err)
	}

	createWorkspaceResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces", map[string]any{
		"organization_id":             organizationID,
		"slug":                        "fleet",
		"display_name":                "Fleet Workspace",
		"region":                      "us-central1",
		"workspace_tier":              "standard",
		"service_identity_id":         serviceIdentityID,
		"service_identity_public_key": serviceIdentityKey.PublicKeyBase64(),
	}, http.StatusCreated, authHeaders(ownerToken))
	workspace := asMap(t, createWorkspaceResp["workspace"])
	workspaceID := asString(t, workspace["id"])
	deploymentRoot := asString(t, workspace["deployment_root"])
	if deploymentRoot == "" {
		t.Fatal("expected deployment_root in workspace response")
	}

	heartbeatToken, _, err := serviceIdentityKey.SignClientAssertion(controlplaneauth.WorkspaceServiceAssertionAudience, 10*time.Minute, map[string]any{
		"workspace_id":    workspaceID,
		"organization_id": organizationID,
		"purpose":         "heartbeat",
	})
	if err != nil {
		t.Fatalf("sign heartbeat token: %v", err)
	}

	heartbeatResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces/"+workspaceID+"/heartbeat", map[string]any{
		"version": "hosted-instance/v1",
		"build":   "build-001",
		"health_summary": map[string]any{
			"status": "healthy",
			"checks": map[string]any{
				"db": "ok",
			},
		},
		"projection_maintenance_summary": map[string]any{
			"status": "current",
		},
		"usage_summary": map[string]any{
			"usage": map[string]any{
				"blob_bytes":              1024,
				"blob_objects":            3,
				"artifact_count":          1,
				"document_count":          0,
				"document_revision_count": 0,
			},
			"quota": map[string]any{
				"max_blob_bytes":         1024 * 1024,
				"max_artifacts":          100,
				"max_documents":          100,
				"max_document_revisions": 500,
				"max_upload_bytes":       1024 * 1024,
			},
			"generated_at": time.Now().UTC().Format(time.RFC3339Nano),
		},
		"last_successful_backup_at": "2026-03-21T00:00:00Z",
	}, http.StatusOK, map[string]string{"Authorization": "Bearer " + heartbeatToken})
	heartbeatWorkspace := asMap(t, heartbeatResp["workspace"])
	if got := asString(t, heartbeatWorkspace["heartbeat_version"]); got != "hosted-instance/v1" {
		t.Fatalf("expected heartbeat version to be recorded, got %q", got)
	}
	if got := asString(t, heartbeatWorkspace["desired_version"]); got != "hosted-instance/v1" {
		t.Fatalf("expected desired_version to start at current version, got %q", got)
	}

	coreWorkspaceRoot := filepath.Join(deploymentRoot, "workspace")
	_, _, cleanup := startCoreServerForTest(t, coreWorkspaceRoot)
	defer cleanup()

	backupResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces/"+workspaceID+"/backups", map[string]any{
		"schedule_name":  "nightly",
		"retention_days": 30,
	}, http.StatusOK, authHeaders(ownerToken))
	backupJob := asMap(t, backupResp["provisioning_job"])
	if got := asString(t, backupJob["status"]); got != "succeeded" {
		t.Fatalf("expected backup job to succeed, got %q", got)
	}
	backupDir := asString(t, asMap(t, backupJob["result"])["backup_dir"])
	if backupDir == "" {
		t.Fatal("expected backup_dir in backup job result")
	}

	upgradeResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces/"+workspaceID+"/upgrade", map[string]any{
		"desired_version": "hosted-instance/v2",
	}, http.StatusOK, authHeaders(ownerToken))
	upgradeWorkspace := asMap(t, upgradeResp["workspace"])
	if got := asString(t, upgradeWorkspace["deployed_version"]); got != "hosted-instance/v2" {
		t.Fatalf("expected deployed_version to update, got %q", got)
	}
	if got := asString(t, upgradeWorkspace["desired_version"]); got != "hosted-instance/v2" {
		t.Fatalf("expected desired_version to update, got %q", got)
	}

	heartbeatToken, _, err = serviceIdentityKey.SignClientAssertion(controlplaneauth.WorkspaceServiceAssertionAudience, 10*time.Minute, map[string]any{
		"workspace_id":    workspaceID,
		"organization_id": organizationID,
		"purpose":         "heartbeat",
	})
	if err != nil {
		t.Fatalf("sign post-upgrade heartbeat token: %v", err)
	}
	postUpgradeHeartbeatResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces/"+workspaceID+"/heartbeat", map[string]any{
		"version": "hosted-instance/v2",
		"build":   "build-002",
		"health_summary": map[string]any{
			"status": "healthy",
			"checks": map[string]any{
				"db": "ok",
			},
		},
		"projection_maintenance_summary": map[string]any{
			"status": "current",
		},
		"usage_summary": map[string]any{
			"usage": map[string]any{
				"blob_bytes":              2048,
				"blob_objects":            4,
				"artifact_count":          2,
				"document_count":          1,
				"document_revision_count": 1,
			},
			"quota": map[string]any{
				"max_blob_bytes":         1024 * 1024,
				"max_artifacts":          100,
				"max_documents":          100,
				"max_document_revisions": 500,
				"max_upload_bytes":       1024 * 1024,
			},
			"generated_at": time.Now().UTC().Format(time.RFC3339Nano),
		},
		"last_successful_backup_at": time.Now().UTC().Format(time.RFC3339Nano),
	}, http.StatusOK, map[string]string{"Authorization": "Bearer " + heartbeatToken})
	postUpgradeHeartbeatWorkspace := asMap(t, postUpgradeHeartbeatResp["workspace"])
	if got := asString(t, postUpgradeHeartbeatWorkspace["heartbeat_version"]); got != "hosted-instance/v2" {
		t.Fatalf("expected post-upgrade heartbeat version, got %q", got)
	}

	restoreDrillResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces/"+workspaceID+"/restore-drills", map[string]any{
		"backup_dir": backupDir,
	}, http.StatusOK, authHeaders(ownerToken))
	restoreDrillJob := asMap(t, restoreDrillResp["provisioning_job"])
	if got := asString(t, restoreDrillJob["status"]); got != "succeeded" {
		t.Fatalf("expected restore drill to succeed, got %q", got)
	}

	badBackupDir := filepath.Join(t.TempDir(), "backup-bad")
	copyDir(t, backupDir, badBackupDir)
	tamperManifest(t, filepath.Join(badBackupDir, "manifest.env"))

	failedDrillResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces/"+workspaceID+"/restore-drills", map[string]any{
		"backup_dir": badBackupDir,
	}, http.StatusOK, authHeaders(ownerToken))
	failedDrillJob := asMap(t, failedDrillResp["provisioning_job"])
	if got := asString(t, failedDrillJob["status"]); got != "failed" {
		t.Fatalf("expected failed restore drill, got %q", got)
	}
	if got := asString(t, failedDrillJob["failure_reason"]); got == "" {
		t.Fatal("expected failure_reason for failed restore drill")
	}

	inventoryResp := requestJSON(t, http.MethodGet, env.server.URL+"/organizations/"+organizationID+"/workspace-inventory?limit=1", nil, http.StatusOK, authHeaders(ownerToken))
	if got := asString(t, inventoryResp["organization_id"]); got != organizationID {
		t.Fatalf("expected inventory organization_id=%q, got %q", organizationID, got)
	}
	inventoryWorkspaces := asSlice(t, inventoryResp["workspaces"])
	if len(inventoryWorkspaces) != 1 {
		t.Fatalf("expected 1 workspace in inventory, got %d", len(inventoryWorkspaces))
	}
	inventoryItem := asMap(t, inventoryWorkspaces[0])
	inventoryWorkspace := asMap(t, inventoryItem["workspace"])
	if got := asString(t, inventoryWorkspace["heartbeat_version"]); got != "hosted-instance/v2" {
		t.Fatalf("expected inventory heartbeat_version=%q, got %q", "hosted-instance/v2", got)
	}
	if got := asString(t, inventoryWorkspace["desired_version"]); got != "hosted-instance/v2" {
		t.Fatalf("expected inventory desired_version=%q, got %q", "hosted-instance/v2", got)
	}
	if got := int(asFloat(t, inventoryItem["open_failed_job_count"])); got < 1 {
		t.Fatalf("expected at least one open failed job, got %d", got)
	}
	failedJobs := asSlice(t, inventoryItem["open_failed_jobs"])
	if len(failedJobs) == 0 {
		t.Fatal("expected open failed jobs in inventory")
	}
	if got := asString(t, asMap(t, failedJobs[0])["kind"]); got != "workspace_restore_drill" {
		t.Fatalf("expected restore drill in failed jobs, got %q", got)
	}
}

func TestControlPlaneCeremoniesAndSessionsSurviveRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "control-plane")

	env1 := newControlPlaneTestEnv(t, root)
	startRegistrationResp := requestJSON(t, http.MethodPost, env1.server.URL+"/account/passkeys/registrations/start", map[string]any{
		"email":        "restart@example.com",
		"display_name": "Restart User",
	}, http.StatusOK, originHeaders())
	registrationSessionID := asString(t, startRegistrationResp["registration_session_id"])
	registrationOptions := asMap(t, startRegistrationResp["public_key_options"])
	registrationUser := asMap(t, registrationOptions["user"])
	env1.Close()

	env2 := newControlPlaneTestEnv(t, root)
	finishRegistrationResp := requestJSON(t, http.MethodPost, env2.server.URL+"/account/passkeys/registrations/finish", map[string]any{
		"registration_session_id": registrationSessionID,
		"credential":              registrationCredential(t, registrationOptions, "cred-restart", asString(t, registrationUser["id"])),
	}, http.StatusOK, originHeaders())
	session := asMap(t, finishRegistrationResp["session"])
	accessToken := asString(t, session["access_token"])

	createOrganizationResp := requestJSON(t, http.MethodPost, env2.server.URL+"/organizations", map[string]any{
		"slug":         "restart-org",
		"display_name": "Restart Org",
		"plan_tier":    "starter",
	}, http.StatusCreated, authHeaders(accessToken))
	organizationID := asString(t, asMap(t, createOrganizationResp["organization"])["id"])

	startLoginResp := requestJSON(t, http.MethodPost, env2.server.URL+"/account/sessions/start", map[string]any{
		"email": "restart@example.com",
	}, http.StatusOK, originHeaders())
	loginSessionID := asString(t, startLoginResp["session_id"])
	loginOptions := asMap(t, startLoginResp["public_key_options"])
	env2.Close()

	env3 := newControlPlaneTestEnv(t, root)
	organizationsResp := requestJSON(t, http.MethodGet, env3.server.URL+"/organizations", nil, http.StatusOK, authHeaders(accessToken))
	if got := len(asSlice(t, organizationsResp["organizations"])); got != 1 {
		t.Fatalf("expected persisted session to access 1 organization, got %d", got)
	}

	finishLoginResp := requestJSON(t, http.MethodPost, env3.server.URL+"/account/sessions/finish", map[string]any{
		"session_id": loginSessionID,
		"credential": assertionCredential(t, loginOptions, "cred-restart"),
	}, http.StatusOK, originHeaders())
	restartedLoginSession := asMap(t, finishLoginResp["session"])
	restartedToken := asString(t, restartedLoginSession["access_token"])

	organizationResp := requestJSON(t, http.MethodGet, env3.server.URL+"/organizations/"+organizationID, nil, http.StatusOK, authHeaders(restartedToken))
	if got := asString(t, asMap(t, organizationResp["organization"])["id"]); got != organizationID {
		t.Fatalf("expected organization %q after restarted login, got %q", organizationID, got)
	}
	env3.Close()
}

func newControlPlaneTestEnv(t *testing.T, root string) *controlPlaneTestEnv {
	return newControlPlaneTestEnvWithScripts(t, root, "")
}

func newControlPlaneTestEnvWithScripts(t *testing.T, root string, hostedScriptsDir string) *controlPlaneTestEnv {
	t.Helper()

	if root == "" {
		root = t.TempDir()
	}
	workspace, err := cpstorage.InitializeWorkspace(context.Background(), root)
	if err != nil {
		t.Fatalf("initialize control-plane workspace: %v", err)
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate workspace grant key: %v", err)
	}
	grantIssuer := "https://control-plane.example.test"
	grantAudience := "oar-core"
	grantSigner, err := controlplaneauth.NewWorkspaceHumanGrantSigner(controlplaneauth.WorkspaceHumanGrantSignerConfig{
		Issuer:     grantIssuer,
		Audience:   grantAudience,
		PrivateKey: privateKey,
	})
	if err != nil {
		t.Fatalf("new workspace grant signer: %v", err)
	}
	service := controlplane.NewService(workspace, controlplane.Config{
		WorkspaceGrantSigner: grantSigner,
		HostedScriptsDir:     hostedScriptsDir,
	})
	server := httptest.NewServer(NewHandler(service, Config{
		HealthCheck: workspace.Ping,
		WebAuthnConfig: WebAuthnConfig{
			RPID:     testRPID,
			RPOrigin: testOrigin,
		},
	}))

	return &controlPlaneTestEnv{
		t:               t,
		root:            root,
		workspace:       workspace,
		service:         service,
		server:          server,
		grantIssuer:     grantIssuer,
		grantAudience:   grantAudience,
		grantPublicKey:  publicKey,
		grantPrivateKey: privateKey,
	}
}

func (env *controlPlaneTestEnv) Close() {
	if env == nil {
		return
	}
	if env.server != nil {
		env.server.Close()
		env.server = nil
	}
	if env.workspace != nil {
		if err := env.workspace.Close(); err != nil {
			env.t.Fatalf("close workspace: %v", err)
		}
		env.workspace = nil
	}
}

func registerAccount(t *testing.T, env *controlPlaneTestEnv, email string, displayName string, credentialID string) (map[string]any, map[string]any) {
	t.Helper()

	startResp := requestJSON(t, http.MethodPost, env.server.URL+"/account/passkeys/registrations/start", map[string]any{
		"email":        email,
		"display_name": displayName,
	}, http.StatusOK, originHeaders())
	options := asMap(t, startResp["public_key_options"])
	user := asMap(t, options["user"])

	finishResp := requestJSON(t, http.MethodPost, env.server.URL+"/account/passkeys/registrations/finish", map[string]any{
		"registration_session_id": asString(t, startResp["registration_session_id"]),
		"credential":              registrationCredential(t, options, credentialID, asString(t, user["id"])),
	}, http.StatusOK, originHeaders())

	return asMap(t, finishResp["account"]), asMap(t, finishResp["session"])
}

func loginAccount(t *testing.T, env *controlPlaneTestEnv, email string, credentialID string) map[string]any {
	t.Helper()

	startResp := requestJSON(t, http.MethodPost, env.server.URL+"/account/sessions/start", map[string]any{
		"email": email,
	}, http.StatusOK, originHeaders())
	options := asMap(t, startResp["public_key_options"])

	finishResp := requestJSON(t, http.MethodPost, env.server.URL+"/account/sessions/finish", map[string]any{
		"session_id": asString(t, startResp["session_id"]),
		"credential": assertionCredential(t, options, credentialID),
	}, http.StatusOK, originHeaders())

	return asMap(t, finishResp["session"])
}

func registrationCredential(t *testing.T, options map[string]any, credentialID string, userHandle string) map[string]any {
	t.Helper()
	return map[string]any{
		"id":    credentialID,
		"rawId": credentialID,
		"type":  "public-key",
		"response": map[string]any{
			"clientDataJSON": encodeClientData(t, asString(t, options["challenge"]), "webauthn.create"),
			"userHandle":     userHandle,
		},
	}
}

func assertionCredential(t *testing.T, options map[string]any, credentialID string) map[string]any {
	t.Helper()
	return map[string]any{
		"id":    credentialID,
		"rawId": credentialID,
		"type":  "public-key",
		"response": map[string]any{
			"clientDataJSON": encodeClientData(t, asString(t, options["challenge"]), "webauthn.get"),
		},
	}
}

func encodeClientData(t *testing.T, challenge string, credentialType string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"challenge": challenge,
		"origin":    testOrigin,
		"type":      credentialType,
	})
	if err != nil {
		t.Fatalf("marshal client data: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func requestJSON(t *testing.T, method string, endpoint string, payload any, wantStatus int, headers map[string]string) map[string]any {
	t.Helper()

	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		body = bytes.NewReader(raw)
	}

	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		t.Fatalf("new %s request: %v", method, err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, endpoint, err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s: expected status %d, got %d: %s", method, endpoint, wantStatus, resp.StatusCode, string(rawBody))
	}

	if len(rawBody) == 0 {
		return map[string]any{}
	}
	var decoded map[string]any
	if err := json.Unmarshal(rawBody, &decoded); err != nil {
		t.Fatalf("decode response JSON: %v\nbody=%s", err, string(rawBody))
	}
	return decoded
}

func authHeaders(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func originHeaders() map[string]string {
	return map[string]string{"Origin": testOrigin}
}

func asMap(t *testing.T, raw any) map[string]any {
	t.Helper()
	value, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %#v", raw)
	}
	return value
}

func asSlice(t *testing.T, raw any) []any {
	t.Helper()
	value, ok := raw.([]any)
	if !ok {
		t.Fatalf("expected []any, got %#v", raw)
	}
	return value
}

func asString(t *testing.T, raw any) string {
	t.Helper()
	value, ok := raw.(string)
	if !ok {
		t.Fatalf("expected string, got %#v", raw)
	}
	return value
}

func asBool(t *testing.T, raw any) bool {
	t.Helper()
	value, ok := raw.(bool)
	if !ok {
		t.Fatalf("expected bool, got %#v", raw)
	}
	return value
}

func asFloat(t *testing.T, raw any) float64 {
	t.Helper()
	value, ok := raw.(float64)
	if !ok {
		t.Fatalf("expected float64, got %#v", raw)
	}
	return value
}

func startCoreServerForTest(t *testing.T, workspaceRoot string) (*exec.Cmd, string, func()) {
	t.Helper()

	repoRoot := findRepoRoot(t)
	schemaPath := filepath.Join(repoRoot, "contracts", "oar-schema.yaml")
	if _, err := os.Stat(schemaPath); err != nil {
		t.Fatalf("schema path not found: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate listener: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	cmd := exec.Command("go", "run", "./cmd/oar-core", "--listen-addr", addr, "--workspace-root", workspaceRoot, "--schema-path", schemaPath)
	cmd.Dir = filepath.Join(repoRoot, "core")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdoutStderr := &bytes.Buffer{}
	cmd.Stdout = stdoutStderr
	cmd.Stderr = stdoutStderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start core server: %v", err)
	}

	baseURL := "http://" + addr
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/readyz")
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	resp, err := http.Get(baseURL + "/readyz")
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil && resp.Body != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		if cmd.Process != nil {
			terminateProcessGroup(cmd)
		}
		t.Fatalf("core server did not become ready; logs:\n%s", stdoutStderr.String())
	}
	if resp != nil && resp.Body != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	cleanup := func() {
		terminateProcessGroup(cmd)
	}
	return cmd, baseURL, cleanup
}

func terminateProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		candidate := filepath.Join(dir, "contracts", "oar-schema.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("repository root not found from %s", dir)
		}
		dir = parent
	}
}

func runScript(t *testing.T, scriptPath string, args ...string) {
	t.Helper()

	cmd := exec.Command(scriptPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s failed: %v\n%s", scriptPath, err, string(output))
	}
}

func copyDir(t *testing.T, sourceDir string, targetDir string) {
	t.Helper()

	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("create target dir: %v", err)
	}
	err := filepath.WalkDir(sourceDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(targetDir, rel)
		if d.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		perm := info.Mode().Perm()
		if perm == 0 {
			perm = 0o644
		}
		if perm&0o111 != 0 {
			perm = 0o755
		}
		return os.WriteFile(targetPath, data, perm)
	})
	if err != nil {
		t.Fatalf("copy directory: %v", err)
	}
}

func tamperManifest(t *testing.T, manifestPath string) {
	t.Helper()

	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	updated := strings.ReplaceAll(string(raw), "FORMAT_VERSION=hosted-ops-backup/v1", "FORMAT_VERSION=hosted-ops-backup/v999")
	if updated == string(raw) {
		t.Fatalf("expected to update manifest format version in %s", manifestPath)
	}
	if err := os.WriteFile(manifestPath, []byte(updated), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func failingProvisionScriptsDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "provision-workspace.sh")
	content := []byte("#!/usr/bin/env bash\nset -euo pipefail\nprintf 'simulated provision failure\\n' >&2\nexit 23\n")
	if err := os.WriteFile(scriptPath, content, 0o755); err != nil {
		t.Fatalf("write failing provision script: %v", err)
	}
	return dir
}
