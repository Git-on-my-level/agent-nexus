package controlplane

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	cpstorage "organization-autorunner-core/internal/controlplane/storage"
)

func TestCreateOrganizationInviteExpiresStalePendingInviteAndCreatesReplacement(t *testing.T) {
	_, service, now := newRaceTestService(t)
	db := service.db

	identity, organizationID := seedOrganizationAccess(t, db, now)
	email := "member@example.com"
	staleInviteID := "inv_stale"
	staleExpiresAt := now.Add(-time.Hour).Format(time.RFC3339Nano)
	mustExec(t, db, `INSERT INTO organization_invites(
		id, organization_id, email, role, status, token_hash, created_at, expires_at, accepted_at, accepted_by_account_id, revoked_at, revoked_by_account_id
	) VALUES (?, ?, ?, ?, 'pending', ?, ?, ?, NULL, NULL, NULL, NULL)`,
		staleInviteID,
		organizationID,
		email,
		"member",
		"token-stale",
		staleExpiresAt,
		staleExpiresAt,
	)

	invite, _, err := service.CreateOrganizationInvite(context.Background(), identity, organizationID, email, "member")
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	if invite.Status != "pending" {
		t.Fatalf("expected replacement invite to be pending, got %q", invite.Status)
	}

	var staleStatus string
	if err := mustQueryRow(t, db, `SELECT status FROM organization_invites WHERE id = ?`, staleInviteID).Scan(&staleStatus); err != nil {
		t.Fatalf("load stale invite: %v", err)
	}
	if staleStatus != "expired" {
		t.Fatalf("expected stale invite to be marked expired, got %q", staleStatus)
	}

	var pendingCount int
	if err := mustQueryRow(t, db, `SELECT COUNT(1) FROM organization_invites WHERE organization_id = ? AND email = ? AND status = 'pending'`, organizationID, email).Scan(&pendingCount); err != nil {
		t.Fatalf("count pending invites: %v", err)
	}
	if pendingCount != 1 {
		t.Fatalf("expected one pending invite after replacement, got %d", pendingCount)
	}
}

func TestCreateOrganizationInvitePendingUniqueIndexRejectsConcurrentInsert(t *testing.T) {
	_, service, now := newRaceTestService(t)
	db := service.db

	identity, organizationID := seedOrganizationAccess(t, db, now)
	email := "member-race@example.com"
	tx1 := mustBeginTx(t, db)
	mustExecTx(t, tx1, `INSERT INTO organization_invites(
		id, organization_id, email, role, status, token_hash, created_at, expires_at, accepted_at, accepted_by_account_id, revoked_at, revoked_by_account_id
	) VALUES (?, ?, ?, ?, 'pending', ?, ?, ?, NULL, NULL, NULL, NULL)`,
		"inv_primary",
		organizationID,
		email,
		"member",
		"token-a",
		now.Format(time.RFC3339Nano),
		now.Add(time.Hour).Format(time.RFC3339Nano),
	)

	errCh := make(chan error, 1)
	go func() {
		tx2, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			errCh <- err
			return
		}
		_, err = tx2.ExecContext(context.Background(), `INSERT INTO organization_invites(
			id, organization_id, email, role, status, token_hash, created_at, expires_at, accepted_at, accepted_by_account_id, revoked_at, revoked_by_account_id
		) VALUES (?, ?, ?, ?, 'pending', ?, ?, ?, NULL, NULL, NULL, NULL)`,
			"inv_secondary",
			organizationID,
			email,
			"member",
			"token-b",
			now.Add(time.Minute).Format(time.RFC3339Nano),
			now.Add(2*time.Hour).Format(time.RFC3339Nano),
		)
		if err == nil {
			err = tx2.Commit()
		} else {
			_ = tx2.Rollback()
		}
		errCh <- err
	}()

	time.Sleep(100 * time.Millisecond)
	if err := tx1.Commit(); err != nil {
		t.Fatalf("commit first invite tx: %v", err)
	}

	err := <-errCh
	if err == nil {
		t.Fatal("expected duplicate pending invite insert to fail")
	}
	if !isSQLiteConstraint(err) {
		t.Fatalf("expected sqlite constraint error, got %v", err)
	}

	_ = identity

	var pendingCount int
	if err := mustQueryRow(t, db, `SELECT COUNT(1) FROM organization_invites WHERE organization_id = ? AND email = ? AND status = 'pending'`, organizationID, email).Scan(&pendingCount); err != nil {
		t.Fatalf("count pending invites: %v", err)
	}
	if pendingCount != 1 {
		t.Fatalf("expected one pending invite after concurrent insert, got %d", pendingCount)
	}
}

func TestRunWorkspaceBackupJobReturnsConflictWhenRunningJobExists(t *testing.T) {
	_, service, now := newRaceTestService(t)
	db := service.db

	identity, organizationID := seedOrganizationAccess(t, db, now)
	workspace := seedWorkspace(t, db, organizationID, now)
	mustExec(t, db, `INSERT INTO provisioning_jobs(
		id, organization_id, workspace_id, kind, status, requested_at, started_at, finished_at, failure_reason,
		progress_message, stdout_tail, stderr_tail, retryable, parameters_json, result_json
	) VALUES (?, ?, ?, ?, 'running', ?, ?, NULL, NULL, ?, ?, ?, 1, '{}', '{}')`,
		"job_running",
		organizationID,
		workspace.ID,
		"workspace_backup",
		now.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
		"workspace backup started",
		"",
		"",
	)

	_, _, err := service.runWorkspaceBackupJob(context.Background(), workspace, "nightly", 30, nil, false)
	expectAPIError(t, err, http.StatusConflict, "backup_in_progress")

	_ = identity
}

func TestWorkspaceBackupRunningUniqueIndexRejectsConcurrentInsert(t *testing.T) {
	_, service, now := newRaceTestService(t)
	db := service.db

	identity, organizationID := seedOrganizationAccess(t, db, now)
	workspace := seedWorkspace(t, db, organizationID, now)
	tx1 := mustBeginTx(t, db)
	mustExecTx(t, tx1, `INSERT INTO provisioning_jobs(
		id, organization_id, workspace_id, kind, status, requested_at, started_at, finished_at, failure_reason,
		progress_message, stdout_tail, stderr_tail, retryable, parameters_json, result_json
	) VALUES (?, ?, ?, ?, 'running', ?, ?, NULL, NULL, ?, ?, ?, 1, '{}', '{}')`,
		"job_primary",
		organizationID,
		workspace.ID,
		"workspace_backup",
		now.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
		"workspace backup started",
		"",
		"",
	)

	errCh := make(chan error, 1)
	go func() {
		tx2, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			errCh <- err
			return
		}
		_, err = tx2.ExecContext(context.Background(), `INSERT INTO provisioning_jobs(
			id, organization_id, workspace_id, kind, status, requested_at, started_at, finished_at, failure_reason,
			progress_message, stdout_tail, stderr_tail, retryable, parameters_json, result_json
		) VALUES (?, ?, ?, ?, 'running', ?, ?, NULL, NULL, ?, ?, ?, 1, '{}', '{}')`,
			"job_secondary",
			organizationID,
			workspace.ID,
			"workspace_backup",
			now.Add(time.Minute).Format(time.RFC3339Nano),
			now.Add(time.Minute).Format(time.RFC3339Nano),
			"workspace backup started",
			"",
			"",
		)
		if err == nil {
			err = tx2.Commit()
		} else {
			_ = tx2.Rollback()
		}
		errCh <- err
	}()

	time.Sleep(100 * time.Millisecond)
	if err := tx1.Commit(); err != nil {
		t.Fatalf("commit first backup tx: %v", err)
	}

	err := <-errCh
	if err == nil {
		t.Fatal("expected duplicate running backup insert to fail")
	}
	if !isSQLiteConstraint(err) {
		t.Fatalf("expected sqlite constraint error, got %v", err)
	}

	_ = identity
}

func newRaceTestService(t *testing.T) (*cpstorage.Workspace, *Service, time.Time) {
	t.Helper()

	workspace, err := cpstorage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	now := time.Date(2026, 3, 22, 12, 0, 0, 0, time.UTC)
	service := NewService(workspace, Config{
		Now: func() time.Time {
			return now
		},
	})
	return workspace, service, now
}

func seedOrganizationAccess(t *testing.T, db *sql.DB, now time.Time) (RequestIdentity, string) {
	t.Helper()

	accountID := "acct_owner"
	accountEmail := "owner@example.com"
	organizationID := "org_race"
	nowText := now.Format(time.RFC3339Nano)
	mustExec(t, db, `INSERT INTO accounts(id, email, display_name, status, created_at, last_login_at, passkey_registered_at)
		VALUES (?, ?, ?, ?, ?, NULL, NULL)`,
		accountID,
		accountEmail,
		"Owner",
		"active",
		nowText,
	)
	mustExec(t, db, `INSERT INTO organizations(id, slug, display_name, plan_tier, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		organizationID,
		"race-org",
		"Race Org",
		"team",
		"active",
		nowText,
		nowText,
	)
	mustExec(t, db, `INSERT INTO organization_memberships(id, organization_id, account_id, role, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		"mem_owner",
		organizationID,
		accountID,
		"owner",
		"active",
		nowText,
	)
	return RequestIdentity{Account: Account{ID: accountID, Email: accountEmail}}, organizationID
}

func seedWorkspace(t *testing.T, db *sql.DB, organizationID string, now time.Time) Workspace {
	t.Helper()

	workspaceID := "ws_race"
	deploymentRoot := filepath.Join(t.TempDir(), "deployment")
	nowText := now.Format(time.RFC3339Nano)
	workspace := Workspace{
		ID:                       workspaceID,
		OrganizationID:           organizationID,
		Slug:                     "race",
		DisplayName:              "Race Workspace",
		Status:                   "ready",
		Region:                   "us-central1",
		WorkspaceTier:            "standard",
		WorkspacePath:            "/race",
		BaseURL:                  "https://race.example.test/race",
		PublicOrigin:             "https://race.example.test",
		CoreOrigin:               "https://core.race.example.test",
		DeploymentRoot:           deploymentRoot,
		InstanceID:               workspaceID,
		ServiceIdentityID:        "svc_race",
		ServiceIdentityPublicKey: "race-public-key",
		DesiredState:             "ready",
		DesiredVersion:           hostedInstanceVersion,
		QuotaConfigRef:           "plan:team",
		QuotaEnvelopeRef:         "organization:" + organizationID + ":quota",
		DeployedVersion:          hostedInstanceVersion,
		RoutingManifestPath:      filepath.Join(deploymentRoot, "routing-manifest.json"),
		CreatedAt:                nowText,
		UpdatedAt:                nowText,
	}
	mustExec(t, db, `INSERT INTO workspaces(
		id, organization_id, slug, display_name, status, region, workspace_tier, workspace_path, base_url,
		public_origin, core_origin, deployment_root, instance_id, service_identity_id, service_identity_public_key,
		desired_state, desired_version, quota_config_ref, quota_envelope_ref, deployed_version, routing_manifest_path,
		last_heartbeat_at, heartbeat_version, heartbeat_build, heartbeat_health_summary_json,
		heartbeat_projection_maintenance_summary_json, heartbeat_usage_summary_json, last_successful_backup_at,
		routing_manifest_json, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?, ?, NULL, ?, ?, ?)`,
		workspace.ID,
		workspace.OrganizationID,
		workspace.Slug,
		workspace.DisplayName,
		workspace.Status,
		workspace.Region,
		workspace.WorkspaceTier,
		workspace.WorkspacePath,
		workspace.BaseURL,
		workspace.PublicOrigin,
		workspace.CoreOrigin,
		workspace.DeploymentRoot,
		workspace.InstanceID,
		workspace.ServiceIdentityID,
		workspace.ServiceIdentityPublicKey,
		workspace.DesiredState,
		workspace.DesiredVersion,
		workspace.QuotaConfigRef,
		workspace.QuotaEnvelopeRef,
		workspace.DeployedVersion,
		workspace.RoutingManifestPath,
		"",
		"",
		"{}",
		"{}",
		"{}",
		"{}",
		workspace.CreatedAt,
		workspace.UpdatedAt,
	)
	return workspace
}

func mustBeginTx(t *testing.T, db *sql.DB) *sql.Tx {
	t.Helper()

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	return tx
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()

	if _, err := db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("exec %s: %v", query, err)
	}
}

func mustExecTx(t *testing.T, tx *sql.Tx, query string, args ...any) {
	t.Helper()

	if _, err := tx.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("exec %s: %v", query, err)
	}
}

func mustQueryRow(t *testing.T, db *sql.DB, query string, args ...any) *sql.Row {
	t.Helper()

	return db.QueryRowContext(context.Background(), query, args...)
}

func expectAPIError(t *testing.T, err error, status int, code string) {
	t.Helper()

	if err == nil {
		t.Fatalf("expected %s error", code)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T: %v", err, err)
	}
	if apiErr.Status != status || apiErr.Code != code {
		t.Fatalf("expected %d/%s, got %d/%s", status, code, apiErr.Status, apiErr.Code)
	}
}
